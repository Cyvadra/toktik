package jobs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/importledger"
	"github.com/Cyvadra/toktik/internal/polymarket"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
)

const (
	polymarketMetadataSourcePrefix = "_metadata:"
)

type PolymarketArchiveConfig struct {
	RawRoot         string
	ConditionMap    string
	ArchiveFrom     time.Time
	ArchiveTo       time.Time
	BatchSize       int
	LimitFiles      int
	Workers         int
	EstimatedHourMB int
	MemoryBudgetMB  int
	ColdStartFloor  time.Time
}

type polymarketArchive struct {
	cfg              PolymarketArchiveConfig
	catalog          *polymarket.ConditionCatalog
	conditionMapHash string
}

func NewPolymarketArchive(cfg PolymarketArchiveConfig) (syncpipeline.Syncer, error) {
	cfg.RawRoot = filepath.Clean(strings.TrimSpace(cfg.RawRoot))
	cfg.ConditionMap = filepath.Clean(strings.TrimSpace(cfg.ConditionMap))
	if cfg.RawRoot == "." || cfg.ConditionMap == "." {
		return nil, fmt.Errorf("polymarket_archive: raw_root and condition_map_path are required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50_000
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.EstimatedHourMB <= 0 {
		cfg.EstimatedHourMB = 300
	}
	if cfg.MemoryBudgetMB <= 0 {
		cfg.MemoryBudgetMB = 20_000
	}
	if cfg.EstimatedHourMB > cfg.MemoryBudgetMB {
		return nil, fmt.Errorf("polymarket_archive: estimated_hour_mb %d exceeds memory_budget_mb %d", cfg.EstimatedHourMB, cfg.MemoryBudgetMB)
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
	matches, err := filepath.Glob(filepath.Join(syncer.cfg.RawRoot, "polymarket_orderbook_*.parquet"))
	if err != nil {
		return nil, fmt.Errorf("list Polymarket parquet files: %w", err)
	}
	sort.Strings(matches)
	if !syncer.cfg.ArchiveFrom.IsZero() || !syncer.cfg.ArchiveTo.IsZero() {
		filtered := matches[:0]
		for _, path := range matches {
			hour, err := parsePolymarketFileHour(path)
			if err != nil {
				return nil, err
			}
			if !syncer.cfg.ArchiveFrom.IsZero() && hour.Before(syncer.cfg.ArchiveFrom) {
				continue
			}
			if !syncer.cfg.ArchiveTo.IsZero() && !hour.Before(syncer.cfg.ArchiveTo) {
				continue
			}
			filtered = append(filtered, path)
		}
		matches = filtered
	}
	if syncer.cfg.LimitFiles > 0 && len(matches) > syncer.cfg.LimitFiles {
		matches = matches[:syncer.cfg.LimitFiles]
	}
	keys := make([]string, 0, len(matches)+1)
	keys = append(keys, fmt.Sprintf("%s%d:%s", polymarketMetadataSourcePrefix, polymarket.MetadataSchemaVersion, syncer.conditionMapHash))
	for _, path := range matches {
		fileHash, err := importledger.SourceHash(path)
		if err != nil {
			return nil, fmt.Errorf("hash Polymarket parquet %s: %w", path, err)
		}
		keys = append(keys, polymarketArchiveSourceKey(filepath.Base(path), syncer.conditionMapHash, fileHash))
	}
	return keys, nil
}

func (syncer *polymarketArchive) ResolveCursor(_ context.Context, _ driver.Conn, sourceKey string) (time.Time, bool, error) {
	if isPolymarketMetadataSource(sourceKey) {
		return syncer.cfg.ColdStartFloor, false, nil
	}
	timestamp, err := parsePolymarketFileHour(polymarketArchiveFileName(sourceKey))
	return timestamp, err == nil, err
}

func (syncer *polymarketArchive) ColdStartFloor(sourceKey string) time.Time {
	if isPolymarketMetadataSource(sourceKey) {
		return syncer.cfg.ColdStartFloor
	}
	if timestamp, err := parsePolymarketFileHour(polymarketArchiveFileName(sourceKey)); err == nil {
		return timestamp
	}
	return syncer.cfg.ColdStartFloor
}

func (syncer *polymarketArchive) Sync(ctx context.Context, conn driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	if isPolymarketMetadataSource(req.SourceKey) {
		result, err := polymarket.ImportConditionMetadata(ctx, conn, syncer.catalog, syncer.conditionMapHash, syncer.cfg.BatchSize, req.DryRun)
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: req.From, To: req.To, RowsInserted: int64(result.Conditions + result.Outcomes), Notes: []string{fmt.Sprintf("conditions=%d", result.Conditions), fmt.Sprintf("outcomes=%d", result.Outcomes), fmt.Sprintf("metadata_version=%d", result.Version)}}, err
	}
	parquetPath := filepath.Join(syncer.cfg.RawRoot, polymarketArchiveFileName(req.SourceKey))
	result, err := polymarket.ImportSelectedEvents(ctx, conn, parquetPath, syncer.catalog, polymarket.ImportOptions{
		BatchSize:     syncer.cfg.BatchSize,
		DryRun:        req.DryRun,
		SelectionHash: syncer.conditionMapHash,
	})
	return syncpipeline.SyncResult{
		SourceKey:    req.SourceKey,
		From:         req.From,
		To:           req.To,
		RowsInserted: int64(result.InsertedRows),
		Notes: []string{
			fmt.Sprintf("selected_rows=%d", result.SelectedRows),
			"source scope is the hourly parquet filename; runner date window is informational",
		},
	}, err
}

func (syncer *polymarketArchive) StableScope(sourceKey string) (string, bool) {
	if isPolymarketMetadataSource(sourceKey) {
		return "metadata:" + syncer.conditionMapHash, true
	}
	return "file:" + sourceKey, true
}

func (syncer *polymarketArchive) AuditTargets(sourceKey string) []syncpipeline.AuditTarget {
	if isPolymarketMetadataSource(sourceKey) {
		return nil
	}
	fileName := strings.ReplaceAll(polymarketArchiveFileName(sourceKey), "'", "''")
	return []syncpipeline.AuditTarget{{
		Table:        "polymarket_l2_event",
		DateColumn:   "toDate(timestamp_received)",
		KeyColumns:   []string{"event_id"},
		SourceFilter: fmt.Sprintf("source_file = '%s'", fileName),
	}}
}

func (syncer *polymarketArchive) MaxConcurrency() int {
	budgetLimit := syncer.cfg.MemoryBudgetMB / syncer.cfg.EstimatedHourMB
	if budgetLimit < 1 {
		return 1
	}
	if syncer.cfg.Workers < budgetLimit {
		return syncer.cfg.Workers
	}
	return budgetLimit
}

func isPolymarketMetadataSource(sourceKey string) bool {
	return strings.HasPrefix(sourceKey, polymarketMetadataSourcePrefix)
}

func parsePolymarketFileHour(path string) (time.Time, error) {
	name := filepath.Base(path)
	const prefix = "polymarket_orderbook_"
	const suffix = ".parquet"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, fmt.Errorf("invalid Polymarket archive filename %q", name)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	timestamp, err := time.Parse("2006-01-02T15", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Polymarket archive filename %q: %w", name, err)
	}
	return timestamp.UTC(), nil
}

func polymarketArchiveSourceKey(fileName, conditionMapHash, fileHash string) string {
	return fileName + "@" + conditionMapHash + "@" + fileHash
}

func polymarketArchiveFileName(sourceKey string) string {
	fileName, _, found := strings.Cut(sourceKey, "@")
	if found {
		return fileName
	}
	return filepath.Base(sourceKey)
}
