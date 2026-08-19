package polymarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

const RawEventStageSchemaVersion uint16 = 1

type RawEventStageManifest struct {
	SchemaVersion uint16 `json:"schema_version"`
	SourceFile    string `json:"source_file"`
	SourceHash    string `json:"source_hash"`
	SelectionHash string `json:"selection_hash"`
	SourceRows    uint64 `json:"source_rows"`
	SelectedRows  uint64 `json:"selected_rows"`
}

func RawEventStagePath(root, sourceFile, sourceHash, selectionHash string) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", RawEventStageSchemaVersion, sourceFile, sourceHash, selectionHash)))
	return filepath.Join(root, fmt.Sprintf("v%d", RawEventStageSchemaVersion), hex.EncodeToString(hash[:])+".parquet")
}

func LoadRawEventStageManifest(path string) (RawEventStageManifest, error) {
	data, err := os.ReadFile(path + ".json")
	if err != nil {
		return RawEventStageManifest{}, err
	}
	var manifest RawEventStageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return RawEventStageManifest{}, fmt.Errorf("decode Polymarket stage manifest: %w", err)
	}
	if manifest.SchemaVersion != RawEventStageSchemaVersion {
		return RawEventStageManifest{}, fmt.Errorf("Polymarket stage schema version %d, want %d", manifest.SchemaVersion, RawEventStageSchemaVersion)
	}
	if _, err := os.Stat(path); err != nil {
		return RawEventStageManifest{}, err
	}
	return manifest, nil
}

func CommitRawEventStageManifest(path string, manifest RawEventStageManifest) error {
	manifest.SchemaVersion = RawEventStageSchemaVersion
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode Polymarket stage manifest: %w", err)
	}
	temporary := path + ".json.tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write Polymarket stage manifest: %w", err)
	}
	if err := os.Rename(temporary, path+".json"); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("commit Polymarket stage manifest: %w", err)
	}
	return nil
}

func EnsureRawEventStage(ctx context.Context, root string, file RawFileRef, selectionHash string, conditions map[string]ConditionMeta) (string, RawEventStageManifest, bool, error) {
	path := RawEventStagePath(root, file.Name, file.Fingerprint, selectionHash)
	if manifest, err := LoadRawEventStageManifest(path); err == nil {
		if manifest.SourceFile == file.Name && manifest.SourceHash == file.Fingerprint && manifest.SelectionHash == selectionHash {
			return path, manifest, true, nil
		}
	} else if !os.IsNotExist(err) {
		return "", RawEventStageManifest{}, false, err
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, scanResult := streamRawEvents(processCtx, file.Path, conditions)
	selectedRows, writeErr := WriteRawEventStage(processCtx, path, events)
	if writeErr != nil {
		cancel()
	}
	scan := <-scanResult
	if writeErr != nil {
		return "", RawEventStageManifest{}, false, writeErr
	}
	if scan.err != nil {
		_ = os.Remove(path)
		return "", RawEventStageManifest{}, false, scan.err
	}
	manifest := RawEventStageManifest{
		SourceFile: file.Name, SourceHash: file.Fingerprint, SelectionHash: selectionHash,
		SourceRows: scan.rows, SelectedRows: selectedRows,
	}
	if err := CommitRawEventStageManifest(path, manifest); err != nil {
		_ = os.Remove(path)
		return "", RawEventStageManifest{}, false, err
	}
	return path, manifest, false, nil
}

type stagedRawEvent struct {
	ExchangeTimeMS   int64  `parquet:"exchange_time_ms"`
	ReceivedTimeMS   int64  `parquet:"received_time_ms"`
	SourceFile       string `parquet:"source_file"`
	SourceRow        uint64 `parquet:"source_row"`
	ConditionID      string `parquet:"condition_id"`
	AssetID          string `parquet:"asset_id"`
	Type             string `parquet:"event_type"`
	BidsJSON         string `parquet:"bids_json"`
	BidsJSONValid    bool   `parquet:"bids_json_valid"`
	AsksJSON         string `parquet:"asks_json"`
	AsksJSONValid    bool   `parquet:"asks_json_valid"`
	PriceE4          int64  `parquet:"price_e4"`
	PriceE4Valid     bool   `parquet:"price_e4_valid"`
	SizeE6           int64  `parquet:"size_e6"`
	SizeE6Valid      bool   `parquet:"size_e6_valid"`
	Side             string `parquet:"side"`
	SideValid        bool   `parquet:"side_valid"`
	BestBidE4        int64  `parquet:"best_bid_e4"`
	BestBidE4Valid   bool   `parquet:"best_bid_e4_valid"`
	BestAskE4        int64  `parquet:"best_ask_e4"`
	BestAskE4Valid   bool   `parquet:"best_ask_e4_valid"`
	FeeRateBPS       uint16 `parquet:"fee_rate_bps"`
	FeeRateValid     bool   `parquet:"fee_rate_bps_valid"`
	TransactionHash  string `parquet:"transaction_hash"`
	TransactionValid bool   `parquet:"transaction_hash_valid"`
	OldTickSizeE4    int64  `parquet:"old_tick_size_e4"`
	OldTickSizeValid bool   `parquet:"old_tick_size_e4_valid"`
	NewTickSizeE4    int64  `parquet:"new_tick_size_e4"`
	NewTickSizeValid bool   `parquet:"new_tick_size_e4_valid"`
}

func WriteRawEventStage(ctx context.Context, path string, events <-chan RawEvent) (uint64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, fmt.Errorf("create Polymarket stage directory: %w", err)
	}
	temporary := path + ".tmp"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("create Polymarket stage %s: %w", temporary, err)
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	writer := parquet.NewGenericWriter[stagedRawEvent](file,
		parquet.Compression(&zstd.Codec{}),
		parquet.CreatedBy("toktik-polymarket-stage", fmt.Sprint(RawEventStageSchemaVersion), ""),
	)
	buffer := make([]stagedRawEvent, 0, 4096)
	var rows uint64
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}
		if _, err := writer.Write(buffer); err != nil {
			return err
		}
		buffer = buffer[:0]
		return nil
	}
	for event := range events {
		if err := ctx.Err(); err != nil {
			_ = writer.Close()
			return rows, err
		}
		buffer = append(buffer, stageRawEvent(event))
		rows++
		if len(buffer) == cap(buffer) {
			if err := flush(); err != nil {
				_ = writer.Close()
				return rows, fmt.Errorf("write Polymarket stage rows: %w", err)
			}
		}
	}
	if err := flush(); err != nil {
		_ = writer.Close()
		return rows, fmt.Errorf("write Polymarket stage rows: %w", err)
	}
	if err := writer.Close(); err != nil {
		return rows, fmt.Errorf("close Polymarket stage writer: %w", err)
	}
	if err := file.Sync(); err != nil {
		return rows, fmt.Errorf("sync Polymarket stage: %w", err)
	}
	if err := file.Close(); err != nil {
		return rows, fmt.Errorf("close Polymarket stage: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return rows, fmt.Errorf("commit Polymarket stage: %w", err)
	}
	committed = true
	return rows, nil
}

func ReadRawEventStage(ctx context.Context, path string, consume func(RawEvent) error) (uint64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("open Polymarket stage %s: %w", path, err)
	}
	defer file.Close()
	reader := parquet.NewGenericReader[stagedRawEvent](file)
	defer reader.Close()
	buffer := make([]stagedRawEvent, 4096)
	var rows uint64
	for {
		count, readErr := reader.Read(buffer)
		for index := 0; index < count; index++ {
			if err := ctx.Err(); err != nil {
				return rows, err
			}
			if err := consume(unstageRawEvent(buffer[index])); err != nil {
				return rows, err
			}
			rows++
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return rows, nil
			}
			return rows, fmt.Errorf("read Polymarket stage %s: %w", path, readErr)
		}
	}
}

func stageRawEvent(event RawEvent) stagedRawEvent {
	return stagedRawEvent{
		ExchangeTimeMS: event.Key.ExchangeTime.UnixMilli(), ReceivedTimeMS: event.Key.ReceivedTime.UnixMilli(),
		SourceFile: event.Key.SourceFile, SourceRow: event.Key.SourceRow, ConditionID: event.ConditionID,
		AssetID: event.AssetID, Type: string(event.Type), BidsJSON: event.BidsJSON.Value, BidsJSONValid: event.BidsJSON.Valid,
		AsksJSON: event.AsksJSON.Value, AsksJSONValid: event.AsksJSON.Valid, PriceE4: event.PriceE4.Value, PriceE4Valid: event.PriceE4.Valid,
		SizeE6: event.SizeE6.Value, SizeE6Valid: event.SizeE6.Valid, Side: event.Side.Value, SideValid: event.Side.Valid,
		BestBidE4: event.BestBidE4.Value, BestBidE4Valid: event.BestBidE4.Valid, BestAskE4: event.BestAskE4.Value, BestAskE4Valid: event.BestAskE4.Valid,
		FeeRateBPS: event.FeeRateBPS.Value, FeeRateValid: event.FeeRateBPS.Valid, TransactionHash: event.TransactionHash.Value, TransactionValid: event.TransactionHash.Valid,
		OldTickSizeE4: event.OldTickSizeE4.Value, OldTickSizeValid: event.OldTickSizeE4.Valid, NewTickSizeE4: event.NewTickSizeE4.Value, NewTickSizeValid: event.NewTickSizeE4.Valid,
	}
}

func unstageRawEvent(event stagedRawEvent) RawEvent {
	return RawEvent{
		Key:         EventKey{ExchangeTime: time.UnixMilli(event.ExchangeTimeMS).UTC(), ReceivedTime: time.UnixMilli(event.ReceivedTimeMS).UTC(), SourceFile: event.SourceFile, SourceRow: event.SourceRow},
		ConditionID: event.ConditionID, AssetID: event.AssetID, Type: EventType(event.Type),
		BidsJSON: NullableString{Value: event.BidsJSON, Valid: event.BidsJSONValid}, AsksJSON: NullableString{Value: event.AsksJSON, Valid: event.AsksJSONValid},
		PriceE4: NullableInt64{Value: event.PriceE4, Valid: event.PriceE4Valid}, SizeE6: NullableInt64{Value: event.SizeE6, Valid: event.SizeE6Valid},
		Side: NullableString{Value: event.Side, Valid: event.SideValid}, BestBidE4: NullableInt64{Value: event.BestBidE4, Valid: event.BestBidE4Valid},
		BestAskE4: NullableInt64{Value: event.BestAskE4, Valid: event.BestAskE4Valid}, FeeRateBPS: NullableUint16{Value: event.FeeRateBPS, Valid: event.FeeRateValid},
		TransactionHash: NullableString{Value: event.TransactionHash, Valid: event.TransactionValid}, OldTickSizeE4: NullableInt64{Value: event.OldTickSizeE4, Valid: event.OldTickSizeValid},
		NewTickSizeE4: NullableInt64{Value: event.NewTickSizeE4, Valid: event.NewTickSizeValid},
	}
}
