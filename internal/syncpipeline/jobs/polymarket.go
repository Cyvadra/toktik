package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/importledger"
	"github.com/Cyvadra/toktik/internal/polymarket"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	polymarketArchiveSourcePrefix = "archive:"
	defaultPolymarketStateHorizon = 49 * time.Hour
)

type PolymarketArchiveConfig struct {
	RawRoot           string
	StageRoot         string
	StageWorkers      int
	ConditionMap      string
	ClickHouseDSN     string
	ArchiveFrom       time.Time
	ArchiveTo         time.Time
	BatchSize         int
	WriterConcurrency int
	LimitFiles        int
	StateHorizon      time.Duration
	ColdStartFloor    time.Time
}

type polymarketArchive struct {
	cfg              PolymarketArchiveConfig
	catalog          *polymarket.ConditionCatalog
	conditionMapHash string
	planMu           sync.Mutex
	pendingPlan      *polymarketArchivePlan
}

type polymarketArchiveFile struct {
	Path        string
	Name        string
	Hour        time.Time
	Fingerprint string
	SizeBytes   int64
	Warmup      bool
}

type polymarketArchivePlan struct {
	Files        []polymarketArchiveFile
	WritableFrom int
	ManifestHash string
}

func planPolymarketArchive(cfg PolymarketArchiveConfig, horizon time.Duration) (polymarketArchivePlan, error) {
	matches, err := filepath.Glob(filepath.Join(cfg.RawRoot, "polymarket_orderbook_*.parquet"))
	if err != nil {
		return polymarketArchivePlan{}, fmt.Errorf("list Polymarket parquet files: %w", err)
	}
	files := make([]polymarketArchiveFile, 0, len(matches))
	for _, path := range matches {
		hour, err := polymarket.ParseArchiveFileHour(path)
		if err != nil {
			return polymarketArchivePlan{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return polymarketArchivePlan{}, fmt.Errorf("stat Polymarket parquet %s: %w", path, err)
		}
		fingerprint, err := polymarket.FileFingerprint(path, info)
		if err != nil {
			return polymarketArchivePlan{}, fmt.Errorf("fingerprint Polymarket parquet %s: %w", path, err)
		}
		files = append(files, polymarketArchiveFile{Path: path, Name: filepath.Base(path), Hour: hour, Fingerprint: fingerprint, SizeBytes: info.Size()})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Hour.Before(files[right].Hour) })

	writable := make([]polymarketArchiveFile, 0, len(files))
	for _, file := range files {
		if !cfg.ArchiveFrom.IsZero() && file.Hour.Before(cfg.ArchiveFrom) {
			continue
		}
		if !cfg.ArchiveTo.IsZero() && !file.Hour.Before(cfg.ArchiveTo) {
			continue
		}
		writable = append(writable, file)
	}
	if cfg.LimitFiles > 0 && len(writable) > cfg.LimitFiles {
		writable = writable[:cfg.LimitFiles]
	}
	if len(writable) == 0 {
		return polymarketArchivePlan{}, nil
	}

	warmupFloor := writable[0].Hour.Add(-horizon)
	warmup := make([]polymarketArchiveFile, 0)
	for _, file := range files {
		if file.Hour.Before(warmupFloor) || !file.Hour.Before(writable[0].Hour) {
			continue
		}
		file.Warmup = true
		warmup = append(warmup, file)
	}
	planned := append(warmup, writable...)
	hash := sha256.New()
	for _, file := range planned {
		fmt.Fprintf(hash, "%s\x00%s\x00%t\n", file.Name, file.Fingerprint, file.Warmup)
	}
	return polymarketArchivePlan{Files: planned, WritableFrom: len(warmup), ManifestHash: hex.EncodeToString(hash.Sum(nil))}, nil
}

func dirtyPolymarketFiles(plan polymarketArchivePlan, checkpoints map[string]polymarket.RawFileCheckpoint, selectionHash string, schemaVersion uint16, horizon time.Duration) []polymarket.RawFileRef {
	firstDirty := -1
	for index := plan.WritableFrom; index < len(plan.Files); index++ {
		file := plan.Files[index]
		checkpoint, ok := checkpoints[file.Name]
		if !ok || checkpoint.Status != "success" || checkpoint.SourceHash != file.Fingerprint || checkpoint.SelectionHash != selectionHash || checkpoint.SchemaVersion != schemaVersion {
			firstDirty = index
			break
		}
	}
	if firstDirty < 0 {
		return nil
	}
	warmupFloor := plan.Files[firstDirty].Hour.Add(-horizon)
	start := firstDirty
	for start > 0 && !plan.Files[start-1].Hour.Before(warmupFloor) {
		start--
	}
	files := make([]polymarket.RawFileRef, 0, len(plan.Files)-start)
	for index := start; index < len(plan.Files); index++ {
		file := plan.Files[index]
		_, attempted := checkpoints[file.Name]
		files = append(files, polymarket.RawFileRef{
			Path:        file.Path,
			Name:        file.Name,
			Fingerprint: file.Fingerprint,
			SizeBytes:   file.SizeBytes,
			Warmup:      index < firstDirty,
			Committed:   index >= plan.WritableFrom && index < firstDirty,
			Replace:     index >= firstDirty && attempted,
		})
	}
	return files
}

func NewPolymarketArchive(cfg PolymarketArchiveConfig) (syncpipeline.Syncer, error) {
	cfg.RawRoot = filepath.Clean(strings.TrimSpace(cfg.RawRoot))
	if strings.TrimSpace(cfg.StageRoot) != "" {
		cfg.StageRoot = filepath.Clean(strings.TrimSpace(cfg.StageRoot))
	}
	cfg.ConditionMap = filepath.Clean(strings.TrimSpace(cfg.ConditionMap))
	if cfg.RawRoot == "." || cfg.ConditionMap == "." {
		return nil, fmt.Errorf("polymarket_archive: raw_root and condition_map_path are required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50_000
	}
	if cfg.WriterConcurrency <= 0 {
		cfg.WriterConcurrency = 2
	}
	if cfg.WriterConcurrency > 8 {
		return nil, fmt.Errorf("polymarket_archive: writer_concurrency must not exceed 8")
	}
	if cfg.StageRoot != "" && cfg.StageWorkers <= 0 {
		cfg.StageWorkers = 4
	}
	if cfg.StageWorkers > 16 {
		return nil, fmt.Errorf("polymarket_archive: stage_workers must not exceed 16")
	}
	if cfg.StateHorizon <= 0 {
		cfg.StateHorizon = defaultPolymarketStateHorizon
	}
	if !cfg.ArchiveFrom.IsZero() && !cfg.ArchiveTo.IsZero() && !cfg.ArchiveTo.After(cfg.ArchiveFrom) {
		return nil, fmt.Errorf("polymarket_archive: archive_to must be after archive_from")
	}
	if _, err := os.Stat(cfg.RawRoot); err != nil {
		return nil, fmt.Errorf("polymarket_archive: raw root: %w", err)
	}
	if _, err := os.Stat(cfg.ConditionMap); err != nil {
		return nil, fmt.Errorf("polymarket_archive: condition map: %w", err)
	}
	catalog, err := polymarket.LoadConditionCatalog(cfg.ConditionMap)
	if err != nil {
		return nil, fmt.Errorf("polymarket_archive: load condition catalog: %w", err)
	}
	conditionMapHash, err := importledger.SourceHash(cfg.ConditionMap)
	if err != nil {
		return nil, fmt.Errorf("polymarket_archive: hash condition map: %w", err)
	}
	return &polymarketArchive{cfg: cfg, catalog: catalog, conditionMapHash: conditionMapHash}, nil
}

func (syncer *polymarketArchive) Name() string { return "polymarket_archive" }

func (syncer *polymarketArchive) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	plan, err := planPolymarketArchive(syncer.cfg, syncer.cfg.StateHorizon)
	if err != nil {
		return nil, err
	}
	if len(plan.Files) == plan.WritableFrom {
		return nil, nil
	}
	syncer.planMu.Lock()
	syncer.pendingPlan = &plan
	syncer.planMu.Unlock()
	return []string{fmt.Sprintf("%s%d:%s:%s", polymarketArchiveSourcePrefix, polymarket.RawFileCatalogSchemaVersion, syncer.conditionMapHash, plan.ManifestHash)}, nil
}

func (syncer *polymarketArchive) ResolveCursor(_ context.Context, _ driver.Conn, sourceKey string) (time.Time, bool, error) {
	return syncer.ColdStartFloor(sourceKey), false, nil
}

func (syncer *polymarketArchive) ColdStartFloor(sourceKey string) time.Time {
	if !syncer.cfg.ArchiveFrom.IsZero() {
		return syncer.cfg.ArchiveFrom
	}
	return syncer.cfg.ColdStartFloor
}

func (syncer *polymarketArchive) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	syncer.planMu.Lock()
	cachedPlan := syncer.pendingPlan
	syncer.pendingPlan = nil
	syncer.planMu.Unlock()
	var plan polymarketArchivePlan
	if cachedPlan != nil {
		plan = *cachedPlan
	} else {
		var err error
		plan, err = planPolymarketArchive(syncer.cfg, syncer.cfg.StateHorizon)
		if err != nil {
			return syncpipeline.SyncResult{SourceKey: req.SourceKey}, err
		}
	}
	checkpoints, err := polymarket.LoadRawFileCheckpoints(ctx, conn)
	if err != nil && !req.DryRun {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey}, err
	}
	var eventConns []driver.Conn
	if !req.DryRun && syncer.cfg.ClickHouseDSN != "" {
		for range syncer.cfg.WriterConcurrency {
			eventConn, connectErr := usmarket.ConnectClickHouse(ctx, syncer.cfg.ClickHouseDSN)
			if connectErr != nil {
				for _, opened := range eventConns {
					_ = opened.Close()
				}
				return syncpipeline.SyncResult{SourceKey: req.SourceKey}, fmt.Errorf("connect Polymarket event writer: %w", connectErr)
			}
			eventConns = append(eventConns, eventConn)
		}
		defer func() {
			for _, eventConn := range eventConns {
				_ = eventConn.Close()
			}
		}()
	}
	files := dirtyPolymarketFiles(plan, checkpoints, syncer.conditionMapHash, polymarket.RawFileCatalogSchemaVersion, syncer.cfg.StateHorizon)
	result, err := polymarket.ImportArchive(ctx, conn, files, syncer.catalog, polymarket.ArchiveImportOptions{
		BatchSize:     syncer.cfg.BatchSize,
		DryRun:        req.DryRun,
		SelectionHash: syncer.conditionMapHash,
		Horizon:       syncer.cfg.StateHorizon,
		Progress:      req.Progress,
		EventConns:    eventConns,
		FileCompleted: polymarketArchiveFileLogger(req.Logger),
		StageRoot:     syncer.cfg.StageRoot,
		StageWorkers:  syncer.cfg.StageWorkers,
	})
	return syncpipeline.SyncResult{
		SourceKey:    req.SourceKey,
		From:         req.From,
		To:           req.To,
		RowsInserted: int64(result.RowsInserted + result.ConditionsInserted + result.OutcomesInserted),
		Notes: []string{
			fmt.Sprintf("files_warmed=%d", result.FilesWarmed),
			fmt.Sprintf("files_processed=%d", result.FilesProcessed),
			fmt.Sprintf("files_skipped=%d", result.FilesSkipped),
			fmt.Sprintf("synthetic_books=%d", result.SyntheticBooksInserted),
			fmt.Sprintf("pre_init_skipped=%d", result.PreInitializationSkipped),
			fmt.Sprintf("rows_scanned=%d", result.RowsScanned),
			fmt.Sprintf("writer_batches=%d", result.WriterBatches),
			fmt.Sprintf("writer_wait=%s", result.WriterWait.Round(time.Millisecond)),
			fmt.Sprintf("archive_elapsed=%s", result.Elapsed.Round(time.Millisecond)),
		},
	}, err
}

func polymarketArchiveFileLogger(logger *slog.Logger) func(polymarket.ArchiveFileMetrics) {
	if logger == nil {
		logger = slog.Default()
	}
	return func(metrics polymarket.ArchiveFileMetrics) {
		logger.Info("Polymarket archive file completed",
			"file", metrics.Name,
			"status", metrics.Status,
			"size_bytes", metrics.SizeBytes,
			"source_rows", metrics.SourceRows,
			"selected_rows", metrics.SelectedRows,
			"writer_batches", metrics.WriterBatches,
			"writer_wait", metrics.WriterWait.Round(time.Millisecond),
			"elapsed", metrics.Elapsed.Round(time.Millisecond),
			"mib_per_second", metrics.MiBPerSecond(),
			"rows_per_second", metrics.RowsPerSecond(),
			"writer_wait_ratio", metrics.WriterWaitRatio(),
			"stage_cache_hit", metrics.StageCacheHit,
			"stage_wait", metrics.StageWait.Round(time.Millisecond),
		)
	}
}

func (syncer *polymarketArchive) StableScope(sourceKey string) (string, bool) {
	return "archive:" + syncer.conditionMapHash, true
}

func (syncer *polymarketArchive) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	return []syncpipeline.AuditTarget{{
		Table:      "polymarket_l2_event",
		DateColumn: "toDate(timestamp_received)",
		KeyColumns: []string{"event_id"},
	}}
}

func (syncer *polymarketArchive) MaxConcurrency() int { return 1 }

func parsePolymarketFileHour(path string) (time.Time, error) {
	return polymarket.ParseArchiveFileHour(path)
}
