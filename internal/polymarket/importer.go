package polymarket

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const eventInsertSQL = `INSERT INTO polymarket_l2_event (
    condition_id, asset_id, event_type, timestamp, timestamp_received,
	source_file, import_file, source_row_number, event_id, side, price_e4, size_e6,
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

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if hour, parseErr := archiveFileHour(file.Name); parseErr == nil {
			processor.AdvanceWatermark(hour)
		}
		if file.Warmup {
			rows, scanErr := ScanRawEvents(ctx, file.Path, catalog.Conditions, func(event RawEvent) error {
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
			completedMiB += fileProgressMiB(file.SizeBytes)
			if options.Progress != nil {
				options.Progress.AdvanceUnitProgress("polymarket warm "+file.Name, completedMiB)
			}
			continue
		}

		processorBeforeFile := processor.clone()
		fileResult, fileErr := importArchiveFile(ctx, conn, file, processor, options)
		result.RowsScanned += fileResult.SourceRows
		if fileErr != nil {
			if errors.Is(fileErr, ErrPMXTFileRead) {
				processor = processorBeforeFile
				if !options.DryRun {
					if catalogErr := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, fileResult, "skipped", fileErr.Error()); catalogErr != nil {
						return result, fmt.Errorf("record skipped Polymarket file %s: %w", file.Name, catalogErr)
					}
				}
				result.FilesSkipped++
				completedMiB += fileProgressMiB(file.SizeBytes)
				if options.Progress != nil {
					options.Progress.AdvanceUnitProgress("polymarket skip "+file.Name, completedMiB)
				}
				continue
			}
			return result, fileErr
		}
		result.RowsInserted += fileResult.InsertedRows
		result.FilesProcessed++
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

func fileProgressMiB(sizeBytes int64) int {
	const mib = int64(1024 * 1024)
	if sizeBytes <= 0 {
		return 1
	}
	return int((sizeBytes + mib - 1) / mib)
}

func archiveFileHour(name string) (time.Time, error) {
	const prefix = "polymarket_orderbook_"
	const suffix = ".parquet"
	value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if value == name || !strings.HasSuffix(name, suffix) {
		return time.Time{}, fmt.Errorf("invalid Polymarket archive filename %q", name)
	}
	return time.ParseInLocation("2006-01-02T15", value, time.UTC)
}

func importArchiveFile(ctx context.Context, conn driver.Conn, file RawFileRef, processor *ArchiveProcessor, options ArchiveImportOptions) (result ImportResult, err error) {
	result.SourceFile = file.Name
	if !options.DryRun {
		defer func() {
			if err == nil {
				return
			}
			if catalogErr := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, result, "failed", err.Error()); catalogErr != nil {
				err = fmt.Errorf("%w; record Polymarket file failure: %v", err, catalogErr)
			}
		}()
		if err := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, result, "pending", ""); err != nil {
			return result, fmt.Errorf("record pending Polymarket file %s: %w", file.Name, err)
		}
		if file.Replace {
			if err := clearImportedFileEvents(ctx, conn, file.Name); err != nil {
				return result, fmt.Errorf("clear Polymarket event scope %s: %w", file.Name, err)
			}
		}
	}

	var eventWriter *batchWriter
	if !options.DryRun {
		if eventWriter, err = newBatchWriter(ctx, conn, eventInsertSQL, "event", options.BatchSize); err != nil {
			return result, err
		}
		defer eventWriter.Abort()
	}

	rows, scanErr := ScanRawEvents(ctx, file.Path, processor.catalog.Conditions, func(event RawEvent) error {
		emitted, err := processor.Process(event, true)
		if err != nil {
			return err
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
				if err := appendEvent(eventWriter, file.Name, selected); err != nil {
					return err
				}
			}
			result.SelectedRows++
		}
		return nil
	})
	result.SourceRows = rows
	result.InsertedRows = result.SelectedRows
	if scanErr != nil {
		if !options.DryRun && errors.Is(scanErr, ErrPMXTFileRead) {
			if cleanupErr := clearImportedFileEvents(ctx, conn, file.Name); cleanupErr != nil {
				return result, fmt.Errorf("import Polymarket file %s: %w; clear partial events: %v", file.Name, scanErr, cleanupErr)
			}
		}
		return result, fmt.Errorf("import Polymarket file %s: %w", file.Name, scanErr)
	}
	if options.DryRun {
		return result, nil
	}
	if err := eventWriter.Close(); err != nil {
		return result, err
	}
	if err := writeRawFileCatalog(ctx, conn, file.Path, options.SelectionHash, result, "success", ""); err != nil {
		return result, err
	}
	return result, nil
}

func clearImportedFileEvents(ctx context.Context, conn driver.Conn, fileName string) error {
	return conn.Exec(ctx, `ALTER TABLE polymarket_l2_event DELETE WHERE import_file = {import_file:String} SETTINGS mutations_sync = 2`, clickhouse.Named("import_file", fileName))
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

func appendEvent(writer *batchWriter, importFile string, event RawEvent) error {
	if err := writer.Append(
		event.ConditionID,
		event.AssetID,
		string(event.Type),
		event.Key.ExchangeTime,
		event.Key.ReceivedTime,
		event.Key.SourceFile,
		importFile,
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
	if err := writer.batch.Send(); err != nil {
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
