package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePolymarketFileHour(t *testing.T) {
	got, err := parsePolymarketFileHour("/raw/polymarket_orderbook_2026-08-08T05.parquet")
	if err != nil {
		t.Fatalf("parse file hour: %v", err)
	}
	want := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("file hour = %s, want %s", got, want)
	}
}

func TestPolymarketArchiveSourceKeysIncludeMetadataAndRespectLimit(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	for _, name := range []string{"polymarket_orderbook_2026-08-08T06.parquet", "polymarket_orderbook_2026-08-08T05.parquet"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ConditionMap: conditionMap, LimitFiles: 1})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("source keys: %v", err)
	}
	if len(keys) != 2 || !isPolymarketMetadataSource(keys[0]) || polymarketArchiveFileName(keys[1]) != "polymarket_orderbook_2026-08-08T05.parquet" {
		t.Fatalf("unexpected source keys: %v", keys)
	}
}

func TestPolymarketArchiveSourceKeysFilterHourRange(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	for _, hour := range []string{"04", "05", "06", "07"} {
		name := "polymarket_orderbook_2026-08-08T" + hour + ".parquet"
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:      dir,
		ConditionMap: conditionMap,
		ArchiveFrom:  time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC),
		ArchiveTo:    time.Date(2026, 8, 8, 7, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("source keys: %v", err)
	}
	if len(keys) != 3 || polymarketArchiveFileName(keys[1]) != "polymarket_orderbook_2026-08-08T05.parquet" || polymarketArchiveFileName(keys[2]) != "polymarket_orderbook_2026-08-08T06.parquet" {
		t.Fatalf("unexpected ranged source keys: %v", keys)
	}
}

func TestPolymarketArchiveSourceKeysAreStableAcrossRawRootChanges(t *testing.T) {
	conditionMap := filepath.Join(t.TempDir(), "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	var keys []string
	for _, root := range []string{t.TempDir(), t.TempDir()} {
		if err := os.WriteFile(filepath.Join(root, "polymarket_orderbook_2026-08-08T05.parquet"), nil, 0o600); err != nil {
			t.Fatalf("write parquet placeholder: %v", err)
		}
		syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: root, ConditionMap: conditionMap})
		if err != nil {
			t.Fatalf("create syncer: %v", err)
		}
		resolved, err := syncer.SourceKeys(context.Background(), nil)
		if err != nil {
			t.Fatalf("source keys: %v", err)
		}
		keys = append(keys, resolved[1])
	}
	if keys[0] != keys[1] {
		t.Fatalf("source key changed after raw root move: %q != %q", keys[0], keys[1])
	}
}

func TestPolymarketArchiveSourceKeyChangesWithFileContent(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	parquetPath := filepath.Join(dir, "polymarket_orderbook_2026-08-08T05.parquet")
	if err := os.WriteFile(parquetPath, []byte("first"), 0o600); err != nil {
		t.Fatalf("write first parquet: %v", err)
	}
	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{RawRoot: dir, ConditionMap: conditionMap})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	first, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("first source keys: %v", err)
	}
	if err := os.WriteFile(parquetPath, []byte("corrected"), 0o600); err != nil {
		t.Fatalf("write corrected parquet: %v", err)
	}
	second, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("second source keys: %v", err)
	}
	if first[1] == second[1] {
		t.Fatalf("source key must change after file correction: %q", first[1])
	}
}

func TestPolymarketArchiveUsesStableScopes(t *testing.T) {
	syncer := &polymarketArchive{conditionMapHash: "map-hash"}
	if scope, ok := syncer.StableScope("polymarket_orderbook_2026-08-08T05.parquet@map-hash@file-hash"); !ok || scope != "file:polymarket_orderbook_2026-08-08T05.parquet@map-hash@file-hash" {
		t.Fatalf("unexpected file stable scope: %q ok=%v", scope, ok)
	}
	if scope, ok := syncer.StableScope("_metadata:4:map-hash"); !ok || scope != "metadata:map-hash" {
		t.Fatalf("unexpected metadata stable scope: %q ok=%v", scope, ok)
	}
}

func TestPolymarketArchiveConcurrencyRespectsMemoryBudget(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}

	syncer, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:         dir,
		ConditionMap:    conditionMap,
		Workers:         100,
		EstimatedHourMB: 300,
		MemoryBudgetMB:  20_000,
	})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	if got := syncer.MaxConcurrency(); got != 66 {
		t.Fatalf("max concurrency = %d, want floor(20000/300)=66", got)
	}

	syncer, err = NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:         dir,
		ConditionMap:    conditionMap,
		Workers:         8,
		EstimatedHourMB: 300,
		MemoryBudgetMB:  20_000,
	})
	if err != nil {
		t.Fatalf("create syncer: %v", err)
	}
	if got := syncer.MaxConcurrency(); got != 8 {
		t.Fatalf("max concurrency = %d, want configured worker limit 8", got)
	}
}

func TestPolymarketArchiveRejectsTaskLargerThanMemoryBudget(t *testing.T) {
	dir := t.TempDir()
	conditionMap := filepath.Join(dir, "conditions.jsonl")
	if err := os.WriteFile(conditionMap, nil, 0o600); err != nil {
		t.Fatalf("write condition map: %v", err)
	}
	_, err := NewPolymarketArchive(PolymarketArchiveConfig{
		RawRoot:         dir,
		ConditionMap:    conditionMap,
		Workers:         1,
		EstimatedHourMB: 20_001,
		MemoryBudgetMB:  20_000,
	})
	if err == nil {
		t.Fatal("expected oversized hourly task to be rejected")
	}
}
