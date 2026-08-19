package polymarket

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const eventInsertSQL = `INSERT INTO polymarket_l2_event (
    condition_id, asset_id, event_type, timestamp, timestamp_received,
	source_file, import_file, import_hour, source_row_number, event_id, side, price_e4, size_e6,
    best_bid_e4, best_ask_e4, fee_rate_bps, transaction_hash,
    old_tick_size_e4, new_tick_size_e4, bids_json, asks_json
)`

const conditionInsertSQL = `INSERT INTO polymarket_condition (
	condition_id, event_id, market_id, slug, underlying, contract_interval,
	window_start, window_end, market_start, market_end, closed,
	resolved, winner, metadata_version
)`

const outcomeInsertSQL = `INSERT INTO polymarket_outcome (
	asset_id, condition_id, outcome_index, outcome_name, metadata_version
)`

const rawFileCatalogInsertSQL = `INSERT INTO polymarket_raw_file_catalog (
	source_file, source_hash, selection_hash, file_size, row_count, target_row_count,
	first_received_at, last_received_at, schema_version, import_status,
	error_message, checkpoint_version
)`

const metadataCatalogInsertSQL = `INSERT INTO polymarket_metadata_catalog (
	selection_hash, schema_version, condition_count, outcome_count, import_status,
	error_message, checkpoint_version
)`

const MetadataSchemaVersion uint16 = 5
const RawFileCatalogSchemaVersion uint16 = 3

var lastMetadataVersion atomic.Uint64

type ImportResult struct {
	SourceFile      string
	SourceRows      uint64
	SelectedRows    uint64
	InsertedRows    uint64
	FirstReceivedAt *time.Time
	LastReceivedAt  *time.Time
	WriterBatches   uint64
	WriterWait      time.Duration
}

type MetadataImportResult struct {
	Conditions uint64
	Outcomes   uint64
	Version    uint64
}

type RawFileCheckpoint struct {
	SourceHash    string
	SelectionHash string
	SchemaVersion uint16
	Status        string
}

func MetadataCurrent(ctx context.Context, conn driver.Conn, selectionHash string) (bool, error) {
	var current uint8
	if err := conn.QueryRow(ctx, `SELECT count() > 0
		FROM polymarket_metadata_catalog FINAL
		WHERE selection_hash = {selection_hash:String}
			AND schema_version = {schema_version:UInt16}
			AND import_status = 'success'`,
		clickhouse.Named("selection_hash", selectionHash),
		clickhouse.Named("schema_version", MetadataSchemaVersion),
	).Scan(&current); err != nil {
		return false, fmt.Errorf("check Polymarket metadata checkpoint: %w", err)
	}
	return current != 0, nil
}

func LoadRawFileCheckpoints(ctx context.Context, conn driver.Conn) (map[string]RawFileCheckpoint, error) {
	checkpoints := make(map[string]RawFileCheckpoint)
	if conn == nil {
		return checkpoints, nil
	}
	rows, err := conn.Query(ctx, `SELECT
		source_file,
		checkpoint.1,
		checkpoint.2,
		checkpoint.3,
		toString(checkpoint.4)
	FROM
	(
		SELECT
			source_file,
			argMax(
				tuple(source_hash, selection_hash, schema_version, import_status),
				tuple(checkpoint_version, updated_at)
			) AS checkpoint
	FROM polymarket_raw_file_catalog
	GROUP BY source_file
	)`)
	if err != nil {
		return nil, fmt.Errorf("load Polymarket file checkpoints: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceFile string
		var checkpoint RawFileCheckpoint
		if err := rows.Scan(&sourceFile, &checkpoint.SourceHash, &checkpoint.SelectionHash, &checkpoint.SchemaVersion, &checkpoint.Status); err != nil {
			return nil, fmt.Errorf("scan Polymarket file checkpoint: %w", err)
		}
		checkpoints[sourceFile] = checkpoint
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Polymarket file checkpoints: %w", err)
	}
	return checkpoints, nil
}

func ImportConditionMetadata(ctx context.Context, conn driver.Conn, catalog *ConditionCatalog, batchSize int, dryRun bool) (MetadataImportResult, error) {
	if catalog == nil {
		return MetadataImportResult{}, fmt.Errorf("condition catalog is required")
	}
	result := MetadataImportResult{Conditions: uint64(len(catalog.Conditions)), Outcomes: uint64(len(catalog.Assets))}
	if dryRun {
		return result, nil
	}
	if conn == nil {
		return result, fmt.Errorf("ClickHouse connection is required")
	}
	if batchSize <= 0 {
		batchSize = 50_000
	}
	result.Version = metadataVersion(time.Now().UTC())

	conditionWriter, err := newBatchWriter(ctx, conn, conditionInsertSQL, "condition", batchSize)
	if err != nil {
		return result, err
	}
	outcomeWriter, err := newBatchWriter(ctx, conn, outcomeInsertSQL, "outcome", batchSize)
	if err != nil {
		return result, err
	}
	defer conditionWriter.Abort()
	defer outcomeWriter.Abort()
	for _, meta := range catalog.Conditions {
		if meta.WindowEnd.IsZero() {
			return result, fmt.Errorf("condition %s has no window end", meta.ConditionID)
		}
		if err := conditionWriter.Append(meta.ConditionID, meta.EventID, meta.MarketID, meta.Slug, meta.Underlying, meta.Interval, meta.WindowStart, meta.WindowEnd, meta.StartDate, meta.EndDate, meta.Closed, meta.Resolved, meta.Winner, result.Version); err != nil {
			return result, fmt.Errorf("append Polymarket condition %s: %w", meta.ConditionID, err)
		}
		for index, assetID := range meta.OutcomeAsset {
			if err := outcomeWriter.Append(assetID, meta.ConditionID, uint8(index), meta.Outcomes[index], result.Version); err != nil {
				return result, fmt.Errorf("append Polymarket outcome %s: %w", assetID, err)
			}
		}
	}
	if err := conditionWriter.Close(); err != nil {
		return result, err
	}
	if err := outcomeWriter.Close(); err != nil {
		return result, err
	}
	return result, nil
}

func ImportArchive(ctx context.Context, conn driver.Conn, files []RawFileRef, catalog *ConditionCatalog, options ArchiveImportOptions) (result ArchiveImportResult, err error) {
	startedAt := time.Now()
	defer func() { result.Elapsed = time.Since(startedAt) }()
	if catalog == nil {
		return result, fmt.Errorf("condition catalog is required")
	}
	if !options.DryRun && conn == nil {
		return result, fmt.Errorf("ClickHouse connection is required")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 50_000
	}
	if options.Horizon <= 0 {
		options.Horizon = 49 * time.Hour
	}
	if len(files) == 0 {
		return result, nil
	}
	var stagedFiles []<-chan stagedArchiveFileResult
	if options.StageRoot != "" {
		var cancelStaging context.CancelFunc
		var waitStaging func()
		stagedFiles, cancelStaging, waitStaging = startArchiveStaging(ctx, files, catalog.Conditions, options.SelectionHash, options.StageRoot, options.StageWorkers)
		defer func() {
			cancelStaging()
			waitStaging()
		}()
	}
	processor := NewArchiveProcessor(catalog, options.Horizon)
	metadataCurrent := false
	if !options.DryRun {
		metadataCurrent, err = MetadataCurrent(ctx, conn, options.SelectionHash)
		if err != nil {
			return result, err
		}
	}
	if !metadataCurrent {
		metadataResult, metadataErr := ImportConditionMetadata(ctx, conn, catalog, options.BatchSize, options.DryRun)
		if metadataErr != nil {
			if !options.DryRun {
				_ = writeMetadataCatalog(ctx, conn, options.SelectionHash, metadataResult, "failed", metadataErr.Error())
			}
			return result, metadataErr
		}
		if !options.DryRun {
			if err := writeMetadataCatalog(ctx, conn, options.SelectionHash, metadataResult, "success", ""); err != nil {
				return result, err
			}
		}
		result.ConditionsInserted = metadataResult.Conditions
		result.OutcomesInserted = metadataResult.Outcomes
	}
	totalMiB := 0
	for _, file := range files {
		totalMiB += fileProgressMiB(file.SizeBytes)
	}
	completedMiB := 0
	if options.Progress != nil && totalMiB > 0 {
		options.Progress.StartUnitProgress("polymarket archive", "MiB", totalMiB)
	}

	for index, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if stagedFiles != nil {
			stageWaitStartedAt := time.Now()
			staged := <-stagedFiles[index]
			stageWait := time.Since(stageWaitStartedAt)
			if staged.err != nil {
				return result, staged.err
			}
			file = staged.file
			file.StageWait = stageWait
		}
		fileStartedAt := time.Now()
		if hour, parseErr := ParseArchiveFileHour(file.Name); parseErr == nil {
			processor.AdvanceWatermark(hour)
		}
		if file.Warmup {
			rows, scanErr := scanArchiveEvents(ctx, file, catalog.Conditions, func(event RawEvent) error {
				if file.Committed {
					return processor.ReplayCommitted(event)
				}
				_, processErr := processor.Process(event, false)
				return processErr
			})
			result.RowsScanned += rows
			if scanErr != nil {
				return result, fmt.Errorf("warm Polymarket file %s: %w", file.Name, scanErr)
			}
			result.FilesWarmed++
			reportArchiveFile(options, ArchiveFileMetrics{
				Name: file.Name, Status: ArchiveFileWarmed, SizeBytes: file.SizeBytes,
				SourceRows: rows, Elapsed: time.Since(fileStartedAt),
				StageCacheHit: file.StageCacheHit, StageWait: file.StageWait,
			})
			completedMiB += fileProgressMiB(file.SizeBytes)
			if options.Progress != nil {
				options.Progress.AdvanceUnitProgress("polymarket warm "+file.Name, completedMiB)
			}
			continue
		}

		processor.BeginFile()
		fileResult, fileErr := importArchiveFile(ctx, conn, file, processor, options)
		result.RowsScanned += fileResult.SourceRows
		if fileErr != nil {
			if errors.Is(fileErr, ErrPMXTFileRead) {
				processor.RollbackFile()
				if !options.DryRun {
					if catalogErr := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, fileResult, "skipped", fileErr.Error()); catalogErr != nil {
						return result, fmt.Errorf("record skipped Polymarket file %s: %w", file.Name, catalogErr)
					}
				}
				result.FilesSkipped++
				reportArchiveFile(options, ArchiveFileMetrics{
					Name: file.Name, Status: ArchiveFileSkipped, SizeBytes: file.SizeBytes,
					SourceRows: fileResult.SourceRows, SelectedRows: fileResult.SelectedRows,
					Elapsed:       time.Since(fileStartedAt),
					StageCacheHit: file.StageCacheHit, StageWait: file.StageWait,
				})
				completedMiB += fileProgressMiB(file.SizeBytes)
				if options.Progress != nil {
					options.Progress.AdvanceUnitProgress("polymarket skip "+file.Name, completedMiB)
				}
				continue
			}
			processor.RollbackFile()
			return result, fileErr
		}
		processor.CommitFile()
		result.RowsInserted += fileResult.InsertedRows
		result.WriterBatches += fileResult.WriterBatches
		result.WriterWait += fileResult.WriterWait
		result.FilesProcessed++
		reportArchiveFile(options, ArchiveFileMetrics{
			Name: file.Name, Status: ArchiveFileImported, SizeBytes: file.SizeBytes,
			SourceRows: fileResult.SourceRows, SelectedRows: fileResult.SelectedRows,
			WriterBatches: fileResult.WriterBatches, WriterWait: fileResult.WriterWait,
			Elapsed:       time.Since(fileStartedAt),
			StageCacheHit: file.StageCacheHit, StageWait: file.StageWait,
		})
		completedMiB += fileProgressMiB(file.SizeBytes)
		if options.Progress != nil {
			options.Progress.AdvanceUnitProgress("polymarket import "+file.Name, completedMiB)
		}
	}
	stats := processor.Stats()
	result.SyntheticBooksInserted = stats.SyntheticBooksInserted
	result.PreInitializationSkipped = stats.PreInitializationSkipped
	result.LateRowsSkipped = stats.LateRowsSkipped
	return result, nil
}

type stagedArchiveFileResult struct {
	file RawFileRef
	err  error
}

func startArchiveStaging(ctx context.Context, files []RawFileRef, conditions map[string]ConditionMeta, selectionHash, stageRoot string, workers int) ([]<-chan stagedArchiveFileResult, context.CancelFunc, func()) {
	if workers <= 0 {
		workers = 4
	}
	if workers > len(files) {
		workers = len(files)
	}
	stageCtx, cancel := context.WithCancel(ctx)
	jobs := make(chan int)
	results := make([]chan stagedArchiveFileResult, len(files))
	readOnlyResults := make([]<-chan stagedArchiveFileResult, len(files))
	for index := range results {
		results[index] = make(chan stagedArchiveFileResult, 1)
		readOnlyResults[index] = results[index]
	}
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				file := files[index]
				stagePath, manifest, cacheHit, err := EnsureRawEventStage(stageCtx, stageRoot, file, selectionHash, conditions)
				if err != nil {
					results[index] <- stagedArchiveFileResult{err: fmt.Errorf("stage Polymarket file %s: %w", file.Name, err)}
					cancel()
					continue
				}
				file.StagePath = stagePath
				file.StageRows = manifest.SourceRows
				file.StageCacheHit = cacheHit
				results[index] <- stagedArchiveFileResult{file: file}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range files {
			select {
			case jobs <- index:
			case <-stageCtx.Done():
				for remaining := index; remaining < len(files); remaining++ {
					results[remaining] <- stagedArchiveFileResult{err: stageCtx.Err()}
				}
				return
			}
		}
	}()
	return readOnlyResults, cancel, group.Wait
}

func reportArchiveFile(options ArchiveImportOptions, metrics ArchiveFileMetrics) {
	if options.FileCompleted != nil {
		options.FileCompleted(metrics)
	}
}

func fileProgressMiB(sizeBytes int64) int {
	const mib = int64(1024 * 1024)
	if sizeBytes <= 0 {
		return 1
	}
	return int((sizeBytes + mib - 1) / mib)
}

func importArchiveFile(ctx context.Context, conn driver.Conn, file RawFileRef, processor *ArchiveProcessor, options ArchiveImportOptions) (result ImportResult, err error) {
	result.SourceFile = file.Name
	importHour, err := ParseArchiveFileHour(file.Name)
	if err != nil {
		return result, err
	}
	if !options.DryRun {
		if err := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, result, "pending", ""); err != nil {
			return result, fmt.Errorf("record pending Polymarket file %s: %w", file.Name, err)
		}
		if file.Replace {
			if err := clearImportedFileEvents(ctx, conn, importHour); err != nil {
				return result, fmt.Errorf("clear Polymarket event scope %s: %w", file.Name, err)
			}
		}
	}

	var eventWriter eventWriter
	if !options.DryRun {
		eventWriter, err = newEventWriter(ctx, conn, options.EventConns, options.BatchSize)
		if err != nil {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			defer cancelCleanup()
			if catalogErr := writeRawFileCatalog(cleanupCtx, conn, file.Path, options.SelectionHash, result, "failed", err.Error()); catalogErr != nil {
				return result, fmt.Errorf("%w; record Polymarket file failure: %v", err, catalogErr)
			}
			return result, err
		}
		defer func() {
			abortErr := eventWriter.Abort()
			if err == nil {
				if abortErr != nil {
					err = fmt.Errorf("abort closed Polymarket event writer: %w", abortErr)
				}
				return
			}
			if abortErr != nil {
				err = fmt.Errorf("%w; drain Polymarket event writer: %v", err, abortErr)
			}
			cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			defer cancelCleanup()
			if cleanupErr := clearImportedFileEvents(cleanupCtx, conn, importHour); cleanupErr != nil {
				err = fmt.Errorf("%w; clear partial Polymarket events: %v", err, cleanupErr)
			}
			if catalogErr := writeRawFileCatalog(cleanupCtx, conn, file.Path, options.SelectionHash, result, "failed", err.Error()); catalogErr != nil {
				err = fmt.Errorf("%w; record Polymarket file failure: %v", err, catalogErr)
			}
		}()
	}

	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, scanResult := streamArchiveEvents(processCtx, file, processor.catalog.Conditions)
	var processErr error
	for event := range events {
		emitted, err := processor.Process(event, true)
		if err != nil {
			processErr = err
			cancel()
			break
		}
		for _, selected := range emitted {
			if result.FirstReceivedAt == nil || selected.Key.ReceivedTime.Before(*result.FirstReceivedAt) {
				value := selected.Key.ReceivedTime
				result.FirstReceivedAt = &value
			}
			if result.LastReceivedAt == nil || selected.Key.ReceivedTime.After(*result.LastReceivedAt) {
				value := selected.Key.ReceivedTime
				result.LastReceivedAt = &value
			}
			if !options.DryRun {
				if err := eventWriter.Append(file.Name, importHour, selected); err != nil {
					processErr = err
					cancel()
					break
				}
			}
			result.SelectedRows++
		}
		if processErr != nil {
			break
		}
	}
	scan := <-scanResult
	result.SourceRows = scan.rows
	result.InsertedRows = result.SelectedRows
	if processErr != nil {
		return result, fmt.Errorf("import Polymarket file %s: %w", file.Name, processErr)
	}
	if scan.err != nil {
		return result, fmt.Errorf("import Polymarket file %s: %w", file.Name, scan.err)
	}
	if options.DryRun {
		return result, nil
	}
	if err := eventWriter.Close(); err != nil {
		return result, err
	}
	writerStats := eventWriter.Stats()
	result.WriterBatches = writerStats.Batches
	result.WriterWait = writerStats.Wait
	if err := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, result, "success", ""); err != nil {
		return result, err
	}
	return result, nil
}

func scanArchiveEvents(ctx context.Context, file RawFileRef, conditions map[string]ConditionMeta, consume func(RawEvent) error) (uint64, error) {
	if file.StagePath == "" {
		return ScanRawEvents(ctx, file.Path, conditions, consume)
	}
	_, err := ReadRawEventStage(ctx, file.StagePath, consume)
	return file.StageRows, err
}

func clearImportedFileEvents(ctx context.Context, conn driver.Conn, importHour time.Time) error {
	partition := importHour.UTC().Format("2006-01-02 15:04:05")
	return conn.Exec(ctx, fmt.Sprintf("ALTER TABLE polymarket_l2_event DROP PARTITION '%s'", partition))
}

func writeRawFileCatalog(ctx context.Context, conn driver.Conn, parquetPath, selectionHash string, result ImportResult, status, errorMessage string) error {
	info, err := os.Stat(parquetPath)
	if err != nil {
		return fmt.Errorf("stat imported Polymarket file: %w", err)
	}
	batch, err := conn.PrepareBatch(ctx, rawFileCatalogInsertSQL)
	if err != nil {
		return fmt.Errorf("prepare Polymarket file catalog batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	sourceFingerprint, err := FileFingerprint(parquetPath, info)
	if err != nil {
		return fmt.Errorf("fingerprint imported Polymarket file: %w", err)
	}
	if err := batch.Append(result.SourceFile, sourceFingerprint, selectionHash, uint64(info.Size()), result.SourceRows, result.SelectedRows, result.FirstReceivedAt, result.LastReceivedAt, RawFileCatalogSchemaVersion, status, errorMessage, metadataVersion(time.Now().UTC())); err != nil {
		return fmt.Errorf("append Polymarket file catalog: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send Polymarket file catalog: %w", err)
	}
	return nil
}

func writeMetadataCatalog(ctx context.Context, conn driver.Conn, selectionHash string, result MetadataImportResult, status, errorMessage string) error {
	batch, err := conn.PrepareBatch(ctx, metadataCatalogInsertSQL)
	if err != nil {
		return fmt.Errorf("prepare Polymarket metadata catalog batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(selectionHash, MetadataSchemaVersion, result.Conditions, result.Outcomes, status, errorMessage, metadataVersion(time.Now().UTC())); err != nil {
		return fmt.Errorf("append Polymarket metadata catalog: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send Polymarket metadata catalog: %w", err)
	}
	return nil
}

func FileFingerprint(path string, info os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	const sampleSize = int64(64 * 1024)
	hash := sha256.New()
	if _, err := io.CopyN(hash, file, min(info.Size(), sampleSize)); err != nil {
		return "", err
	}
	if info.Size() > sampleSize {
		if _, err := file.Seek(max(sampleSize, info.Size()-sampleSize), io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.Copy(hash, file); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("size=%d;modified=%d;sample=%x", info.Size(), info.ModTime().UTC().UnixNano(), hash.Sum(nil)), nil
}

func metadataVersion(importedAt time.Time) uint64 {
	candidate := uint64(importedAt.UTC().UnixNano())
	for {
		previous := lastMetadataVersion.Load()
		if candidate <= previous {
			candidate = previous + 1
		}
		if lastMetadataVersion.CompareAndSwap(previous, candidate) {
			return candidate
		}
	}
}

type batchWriter struct {
	ctx       context.Context
	conn      driver.Conn
	batch     driver.Batch
	batchSize int
	name      string
	query     string
	pending   int
	inserted  uint64
	batches   uint64
	wait      time.Duration
}

func newBatchWriter(ctx context.Context, conn driver.Conn, query, name string, batchSize int) (*batchWriter, error) {
	writer := &batchWriter{ctx: ctx, conn: conn, batchSize: batchSize, name: name, query: query}
	if err := writer.prepare(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *batchWriter) prepare() error {
	batch, err := writer.conn.PrepareBatch(writer.ctx, writer.query)
	if err != nil {
		return fmt.Errorf("prepare Polymarket %s batch: %w", writer.name, err)
	}
	writer.batch = batch
	return nil
}

type eventWriter interface {
	Append(importFile string, importHour time.Time, event RawEvent) error
	Close() error
	Abort() error
	Stats() eventWriterStats
}

type eventWriterStats struct {
	Batches uint64
	Wait    time.Duration
}

type synchronousEventWriter struct{ writer *batchWriter }

func (writer synchronousEventWriter) Append(importFile string, importHour time.Time, event RawEvent) error {
	return appendEvent(writer.writer, importFile, importHour, event)
}

func (writer synchronousEventWriter) Close() error { return writer.writer.Close() }

func (writer synchronousEventWriter) Abort() error { return writer.writer.Abort() }

func (writer synchronousEventWriter) Stats() eventWriterStats {
	return eventWriterStats{Batches: writer.writer.batches, Wait: writer.writer.wait}
}

func newEventWriter(ctx context.Context, conn driver.Conn, eventConns []driver.Conn, batchSize int) (eventWriter, error) {
	if len(eventConns) < 2 {
		writer, err := newBatchWriter(ctx, conn, eventInsertSQL, "event", batchSize)
		if err != nil {
			return nil, err
		}
		return synchronousEventWriter{writer: writer}, nil
	}
	return newAsyncEventWriter(ctx, eventConns, batchSize)
}

func appendEvent(writer *batchWriter, importFile string, importHour time.Time, event RawEvent) error {
	if err := appendEventBatch(writer.batch, importFile, importHour, event); err != nil {
		return err
	}
	writer.pending++
	if writer.pending >= writer.batchSize {
		return writer.flush(true)
	}
	return nil
}

func appendEventBatch(batch driver.Batch, importFile string, importHour time.Time, event RawEvent) error {
	if err := batch.Append(
		event.ConditionID,
		event.AssetID,
		string(event.Type),
		event.Key.ExchangeTime,
		event.Key.ReceivedTime,
		event.Key.SourceFile,
		importFile,
		importHour,
		event.Key.SourceRow,
		event.Key.EventID(),
		nullableStringValue(event.Side),
		nullableInt64Value(event.PriceE4),
		nullableInt64Value(event.SizeE6),
		nullableInt64Value(event.BestBidE4),
		nullableInt64Value(event.BestAskE4),
		nullableUint16Value(event.FeeRateBPS),
		nullableStringValue(event.TransactionHash),
		nullableInt64Value(event.OldTickSizeE4),
		nullableInt64Value(event.NewTickSizeE4),
		nullableStringValue(event.BidsJSON),
		nullableStringValue(event.AsksJSON),
	); err != nil {
		return fmt.Errorf("append Polymarket event row %d: %w", event.Key.SourceRow, err)
	}
	return nil
}

type asyncEventBatch struct {
	conn    driver.Conn
	batch   driver.Batch
	pending int
	sending chan error
}

type asyncEventWriter struct {
	ctx          context.Context
	batchSize    int
	batches      []asyncEventBatch
	current      int
	sent         uint64
	waitDuration time.Duration
}

func newAsyncEventWriter(ctx context.Context, conns []driver.Conn, batchSize int) (*asyncEventWriter, error) {
	if len(conns) < 2 {
		return nil, fmt.Errorf("async Polymarket event writer requires at least two connections")
	}
	writer := &asyncEventWriter{ctx: ctx, batchSize: batchSize, batches: make([]asyncEventBatch, len(conns))}
	for index, conn := range conns {
		writer.batches[index].conn = conn
		if err := writer.prepare(index); err != nil {
			_ = writer.Abort()
			return nil, err
		}
	}
	return writer, nil
}

func (writer *asyncEventWriter) Append(importFile string, importHour time.Time, event RawEvent) error {
	if err := writer.ready(writer.current); err != nil {
		return err
	}
	batch := &writer.batches[writer.current]
	if err := appendEventBatch(batch.batch, importFile, importHour, event); err != nil {
		return err
	}
	batch.pending++
	if batch.pending >= writer.batchSize {
		writer.send(writer.current)
		writer.current = (writer.current + 1) % len(writer.batches)
	}
	return nil
}

func (writer *asyncEventWriter) Close() error {
	if writer.batches[writer.current].pending > 0 {
		writer.send(writer.current)
	}
	for index := range writer.batches {
		if err := writer.wait(index); err != nil {
			return err
		}
		batch := &writer.batches[index]
		if batch.batch != nil {
			if err := batch.batch.Abort(); err != nil {
				return err
			}
			batch.batch = nil
		}
	}
	return nil
}

func (writer *asyncEventWriter) Abort() error {
	var firstErr error
	for index := range writer.batches {
		batch := &writer.batches[index]
		if batch.sending != nil {
			if err := <-batch.sending; err != nil && firstErr == nil {
				firstErr = err
			}
			batch.sending = nil
		}
		if batch.batch != nil {
			if err := batch.batch.Abort(); err != nil && firstErr == nil {
				firstErr = err
			}
			batch.batch = nil
		}
	}
	return firstErr
}

func (writer *asyncEventWriter) Stats() eventWriterStats {
	return eventWriterStats{Batches: writer.sent, Wait: writer.waitDuration}
}

func (writer *asyncEventWriter) send(index int) {
	batch := &writer.batches[index]
	sending := make(chan error, 1)
	pendingBatch := batch.batch
	batch.sending = sending
	batch.batch = nil
	batch.pending = 0
	writer.sent++
	go func(batch driver.Batch) { sending <- batch.Send() }(pendingBatch)
}

func (writer *asyncEventWriter) ready(index int) error {
	if err := writer.wait(index); err != nil {
		return err
	}
	if writer.batches[index].batch == nil {
		return writer.prepare(index)
	}
	return nil
}

func (writer *asyncEventWriter) wait(index int) error {
	batch := &writer.batches[index]
	if batch.sending == nil {
		return nil
	}
	startedAt := time.Now()
	err := <-batch.sending
	writer.waitDuration += time.Since(startedAt)
	batch.sending = nil
	if err != nil {
		return fmt.Errorf("send Polymarket event batch: %w", err)
	}
	return nil
}

func (writer *asyncEventWriter) prepare(index int) error {
	batch, err := writer.batches[index].conn.PrepareBatch(writer.ctx, eventInsertSQL)
	if err != nil {
		return fmt.Errorf("prepare Polymarket event batch: %w", err)
	}
	writer.batches[index].batch = batch
	return nil
}

type rawEventScanResult struct {
	rows uint64
	err  error
}

func streamRawEvents(ctx context.Context, path string, conditions map[string]ConditionMeta) (<-chan RawEvent, <-chan rawEventScanResult) {
	events := make(chan RawEvent, 4_096)
	result := make(chan rawEventScanResult, 1)
	go func() {
		defer close(events)
		rows, err := ScanRawEvents(ctx, path, conditions, func(event RawEvent) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		result <- rawEventScanResult{rows: rows, err: err}
	}()
	return events, result
}

func streamArchiveEvents(ctx context.Context, file RawFileRef, conditions map[string]ConditionMeta) (<-chan RawEvent, <-chan rawEventScanResult) {
	if file.StagePath == "" {
		return streamRawEvents(ctx, file.Path, conditions)
	}
	events := make(chan RawEvent, 4_096)
	result := make(chan rawEventScanResult, 1)
	go func() {
		defer close(events)
		_, err := ReadRawEventStage(ctx, file.StagePath, func(event RawEvent) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		result <- rawEventScanResult{rows: file.StageRows, err: err}
	}()
	return events, result
}

func (writer *batchWriter) Append(values ...any) error {
	if err := writer.batch.Append(values...); err != nil {
		return fmt.Errorf("append Polymarket %s batch: %w", writer.name, err)
	}
	writer.pending++
	if writer.pending >= writer.batchSize {
		return writer.flush(true)
	}
	return nil
}

func (writer *batchWriter) Close() error {
	if writer.pending == 0 {
		return writer.Abort()
	}
	if err := writer.flush(false); err != nil {
		return err
	}
	return nil
}

func (writer *batchWriter) Abort() error {
	if writer.batch == nil {
		return nil
	}
	err := writer.batch.Abort()
	writer.batch = nil
	return err
}

func (writer *batchWriter) flush(prepareNext bool) error {
	if writer.pending == 0 {
		return nil
	}
	startedAt := time.Now()
	err := writer.batch.Send()
	writer.wait += time.Since(startedAt)
	writer.batches++
	if err != nil {
		return fmt.Errorf("send Polymarket %s batch: %w", writer.name, err)
	}
	writer.inserted += uint64(writer.pending)
	writer.pending = 0
	if prepareNext {
		return writer.prepare()
	}
	writer.batch = nil
	return nil
}

func nullableStringValue(value NullableString) any {
	if !value.Valid {
		return nil
	}
	return value.Value
}

func nullableInt64Value(value NullableInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Value
}

func nullableUint16Value(value NullableUint16) any {
	if !value.Valid {
		return nil
	}
	return value.Value
}
