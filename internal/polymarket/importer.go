package polymarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/parquet-go/parquet-go"
)

const eventInsertSQL = `INSERT INTO polymarket_l2_event (
    condition_id, asset_id, event_type, timestamp, timestamp_received,
    source_file, source_row_number, event_id, side, price_e4, size_e6,
    best_bid_e4, best_ask_e4, fee_rate_bps, transaction_hash,
    old_tick_size_e4, new_tick_size_e4, bids_json, asks_json
)`

const conditionInsertSQL = `INSERT INTO polymarket_condition (
	condition_id, event_id, market_id, slug, underlying, contract_interval,
	window_start, window_end, market_start, market_end, closed,
	resolved_outcome, metadata_version
)`

const outcomeInsertSQL = `INSERT INTO polymarket_outcome (
	asset_id, condition_id, outcome_index, outcome_name, metadata_version
)`

const rawFileCatalogInsertSQL = `INSERT INTO polymarket_raw_file_catalog (
	source_file, source_hash, selection_hash, file_size, row_count, target_row_count,
	first_received_at, last_received_at, schema_version, import_status,
	error_message
)`

const MetadataSchemaVersion uint16 = 4
const RawFileCatalogSchemaVersion uint16 = 2

var lastMetadataVersion atomic.Uint64

type ImportOptions struct {
	BatchSize     int
	DryRun        bool
	SelectionHash string
}

type ImportResult struct {
	SourceFile      string
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

func ImportConditionMetadata(ctx context.Context, conn driver.Conn, catalog *ConditionCatalog, conditionMapHash string, batchSize int, dryRun bool) (MetadataImportResult, error) {
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
	result.Version = metadataVersion(time.Now().UTC(), MetadataSchemaVersion)

	conditionBatch, err := conn.PrepareBatch(ctx, conditionInsertSQL)
	if err != nil {
		return result, fmt.Errorf("prepare Polymarket condition batch: %w", err)
	}
	outcomeBatch, err := conn.PrepareBatch(ctx, outcomeInsertSQL)
	if err != nil {
		return result, fmt.Errorf("prepare Polymarket outcome batch: %w", err)
	}
	defer func() { _ = conditionBatch.Abort() }()
	defer func() { _ = outcomeBatch.Abort() }()
	conditionPending := 0
	outcomePending := 0
	flushConditions := func() error {
		if conditionPending == 0 {
			return nil
		}
		if err := conditionBatch.Send(); err != nil {
			return fmt.Errorf("send Polymarket condition batch: %w", err)
		}
		conditionPending = 0
		return nil
	}
	flushOutcomes := func() error {
		if outcomePending == 0 {
			return nil
		}
		if err := outcomeBatch.Send(); err != nil {
			return fmt.Errorf("send Polymarket outcome batch: %w", err)
		}
		outcomePending = 0
		return nil
	}
	for _, meta := range catalog.Conditions {
		if meta.WindowEnd.IsZero() {
			return result, fmt.Errorf("condition %s has no window end", meta.ConditionID)
		}
		if err := conditionBatch.Append(meta.ConditionID, meta.EventID, meta.MarketID, meta.Slug, meta.Underlying, meta.Interval, meta.WindowStart, meta.WindowEnd, meta.StartDate, meta.EndDate, meta.Closed, nil, result.Version); err != nil {
			return result, fmt.Errorf("append Polymarket condition %s: %w", meta.ConditionID, err)
		}
		conditionPending++
		for index, assetID := range meta.OutcomeAsset {
			if err := outcomeBatch.Append(assetID, meta.ConditionID, uint8(index), meta.Outcomes[index], result.Version); err != nil {
				return result, fmt.Errorf("append Polymarket outcome %s: %w", assetID, err)
			}
			outcomePending++
			if outcomePending >= batchSize {
				if err := flushOutcomes(); err != nil {
					return result, err
				}
				outcomeBatch, err = conn.PrepareBatch(ctx, outcomeInsertSQL)
				if err != nil {
					return result, fmt.Errorf("prepare Polymarket outcome batch: %w", err)
				}
			}
		}
		if conditionPending >= batchSize {
			if err := flushConditions(); err != nil {
				return result, err
			}
			conditionBatch, err = conn.PrepareBatch(ctx, conditionInsertSQL)
			if err != nil {
				return result, fmt.Errorf("prepare Polymarket condition batch: %w", err)
			}
		}
	}
	if err := flushConditions(); err != nil {
		return result, err
	}
	if err := flushOutcomes(); err != nil {
		return result, err
	}
	return result, nil
}

func ImportSelectedEvents(ctx context.Context, conn driver.Conn, parquetPath string, catalog *ConditionCatalog, options ImportOptions) (ImportResult, error) {
	if catalog == nil {
		return ImportResult{}, fmt.Errorf("condition catalog is required")
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 50_000
	}
	sourceFile := filepath.Base(parquetPath)
	result := ImportResult{SourceFile: sourceFile}
	if options.DryRun {
		selected, err := StreamSelectedEvents(ctx, parquetPath, catalog.Conditions, func(RawEvent) error { return nil })
		result.SelectedRows = selected
		return result, err
	}
	if conn == nil {
		return result, fmt.Errorf("ClickHouse connection is required")
	}
	if err := conn.Exec(ctx, `ALTER TABLE polymarket_l2_event DELETE WHERE source_file = {source_file:String} SETTINGS mutations_sync = 2`, clickhouse.Named("source_file", sourceFile)); err != nil {
		return result, fmt.Errorf("clear Polymarket event scope %s: %w", sourceFile, err)
	}

	writer, err := newEventBatchWriter(ctx, conn, options.BatchSize)
	if err != nil {
		return result, err
	}
	defer writer.Abort()
	selected, streamErr := StreamSelectedEvents(ctx, parquetPath, catalog.Conditions, func(event RawEvent) error {
		if result.FirstReceivedAt == nil || event.Key.ReceivedTime.Before(*result.FirstReceivedAt) {
			value := event.Key.ReceivedTime
			result.FirstReceivedAt = &value
		}
		if result.LastReceivedAt == nil || event.Key.ReceivedTime.After(*result.LastReceivedAt) {
			value := event.Key.ReceivedTime
			result.LastReceivedAt = &value
		}
		return writer.Append(event)
	})
	result.SelectedRows = selected
	if streamErr != nil {
		return result, streamErr
	}
	if err := writer.Close(); err != nil {
		return result, err
	}
	result.InsertedRows = writer.inserted
	if err := writeRawFileCatalog(ctx, conn, parquetPath, options.SelectionHash, result); err != nil {
		return result, err
	}
	return result, nil
}

func writeRawFileCatalog(ctx context.Context, conn driver.Conn, parquetPath, selectionHash string, result ImportResult) error {
	info, err := os.Stat(parquetPath)
	if err != nil {
		return fmt.Errorf("stat imported Polymarket file: %w", err)
	}
	rowCount, err := sourceRowCount(parquetPath)
	if err != nil {
		return err
	}
	hash, err := fileSHA256(parquetPath)
	if err != nil {
		return fmt.Errorf("hash imported Polymarket file: %w", err)
	}
	batch, err := conn.PrepareBatch(ctx, rawFileCatalogInsertSQL)
	if err != nil {
		return fmt.Errorf("prepare Polymarket file catalog batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(result.SourceFile, hash, selectionHash, uint64(info.Size()), rowCount, result.SelectedRows, result.FirstReceivedAt, result.LastReceivedAt, RawFileCatalogSchemaVersion, "success", ""); err != nil {
		return fmt.Errorf("append Polymarket file catalog: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send Polymarket file catalog: %w", err)
	}
	return nil
}

func sourceRowCount(parquetPath string) (uint64, error) {
	rows, err := readOKRowCount(parquetPath + ".ok")
	if err == nil {
		return rows, nil
	}
	file, openErr := os.Open(filepath.Clean(parquetPath))
	if openErr != nil {
		return 0, fmt.Errorf("open PMXT parquet for row count: %w", openErr)
	}
	defer file.Close()
	info, statErr := file.Stat()
	if statErr != nil {
		return 0, fmt.Errorf("stat PMXT parquet for row count: %w", statErr)
	}
	parquetFile, openErr := parquet.OpenFile(file, info.Size())
	if openErr != nil {
		return 0, fmt.Errorf("read PMXT parquet footer after marker error %v: %w", err, openErr)
	}
	return uint64(parquetFile.NumRows()), nil
}

func readOKRowCount(path string) (uint64, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("read PMXT marker %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key == "rows" {
			rows, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse PMXT marker rows: %w", err)
			}
			return rows, nil
		}
	}
	return 0, fmt.Errorf("PMXT marker %s has no rows", path)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func metadataVersion(importedAt time.Time, schemaVersion uint16) uint64 {
	_ = schemaVersion
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

type eventBatchWriter struct {
	ctx       context.Context
	conn      driver.Conn
	batch     driver.Batch
	batchSize int
	pending   int
	inserted  uint64
}

func newEventBatchWriter(ctx context.Context, conn driver.Conn, batchSize int) (*eventBatchWriter, error) {
	writer := &eventBatchWriter{ctx: ctx, conn: conn, batchSize: batchSize}
	if err := writer.prepare(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *eventBatchWriter) prepare() error {
	batch, err := writer.conn.PrepareBatch(writer.ctx, eventInsertSQL)
	if err != nil {
		return fmt.Errorf("prepare Polymarket event batch: %w", err)
	}
	writer.batch = batch
	return nil
}

func (writer *eventBatchWriter) Append(event RawEvent) error {
	if err := writer.batch.Append(
		event.ConditionID,
		event.AssetID,
		string(event.Type),
		event.Key.ExchangeTime,
		event.Key.ReceivedTime,
		event.Key.SourceFile,
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
	writer.pending++
	if writer.pending >= writer.batchSize {
		return writer.flush(true)
	}
	return nil
}

func (writer *eventBatchWriter) Close() error {
	if writer.pending == 0 {
		return writer.Abort()
	}
	if err := writer.flush(false); err != nil {
		return err
	}
	return nil
}

func (writer *eventBatchWriter) Abort() error {
	if writer.batch == nil {
		return nil
	}
	err := writer.batch.Abort()
	writer.batch = nil
	return err
}

func (writer *eventBatchWriter) flush(prepareNext bool) error {
	if writer.pending == 0 {
		return nil
	}
	if err := writer.batch.Send(); err != nil {
		return fmt.Errorf("send Polymarket event batch: %w", err)
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
