package polymarket

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveProcessorSynthesizesUpdatedWarmupBook(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	warmupBook := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(-time.Hour), ReceivedTime: start.Add(-time.Hour)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[["0.4","2"]]`, Valid: true},
		AsksJSON:    NullableString{Value: `[["0.6","3"]]`, Valid: true},
	}
	if emitted, err := processor.Process(warmupBook, false); err != nil || len(emitted) != 0 {
		t.Fatalf("warmup book emitted=%v err=%v", emitted, err)
	}
	warmupDelta := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(-time.Minute), ReceivedTime: start.Add(-time.Minute)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventPriceChange,
		Side:        NullableString{Value: "BUY", Valid: true},
		PriceE4:     NullableInt64{Value: 4_500, Valid: true},
		SizeE6:      NullableInt64{Value: 4_000_000, Valid: true},
	}
	if emitted, err := processor.Process(warmupDelta, false); err != nil || len(emitted) != 0 {
		t.Fatalf("warmup delta emitted=%v err=%v", emitted, err)
	}
	trigger := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventLastTradePrice,
	}
	emitted, err := processor.Process(trigger, true)
	if err != nil {
		t.Fatalf("process trigger: %v", err)
	}
	if len(emitted) != 2 || emitted[0].Type != EventBook || emitted[0].Key.SourceFile[:len(syntheticSourcePrefix)] != syntheticSourcePrefix {
		t.Fatalf("unexpected emitted events: %+v", emitted)
	}
	if emitted[0].BidsJSON.Value != `[["0.45","4"],["0.4","2"]]` {
		t.Fatalf("synthetic bids = %s", emitted[0].BidsJSON.Value)
	}
	if got := processor.Stats(); got.SyntheticBooksInserted != 1 || got.RowsInserted != 2 {
		t.Fatalf("unexpected stats: %+v", got)
	}
	if processor.InitializedStreams() != 1 {
		t.Fatalf("initialized streams = %d, want 1", processor.InitializedStreams())
	}
}

func TestArchiveProcessorUsesFirstInWindowBookDirectly(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	event := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[]`, Valid: true},
		AsksJSON:    NullableString{Value: `[]`, Valid: true},
	}
	emitted, err := processor.Process(event, true)
	if err != nil || len(emitted) != 1 || emitted[0].Key.SourceFile != event.Key.SourceFile {
		t.Fatalf("emitted=%+v err=%v", emitted, err)
	}
	if processor.Stats().SyntheticBooksInserted != 0 {
		t.Fatal("real first book must not create synthetic snapshot")
	}
}

func TestArchiveProcessorDoesNotRebuildEmittedBook(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	book := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[]`, Valid: true},
		AsksJSON:    NullableString{Value: `[]`, Valid: true},
	}
	if _, err := processor.Process(book, true); err != nil {
		t.Fatalf("process first book: %v", err)
	}
	invalidDelta := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(time.Second), ReceivedTime: start.Add(time.Second)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventPriceChange,
		Side:        NullableString{Value: "HOLD", Valid: true},
	}
	emitted, err := processor.Process(invalidDelta, true)
	if err != nil || len(emitted) != 1 || emitted[0].Key != invalidDelta.Key {
		t.Fatalf("emitted=%+v err=%v", emitted, err)
	}
}

func TestArchiveProcessorRollsBackOnlyFileMutations(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	processor.BeginFile()
	book := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[]`, Valid: true},
		AsksJSON:    NullableString{Value: `[]`, Valid: true},
	}
	if _, err := processor.Process(book, false); err != nil {
		t.Fatalf("process book: %v", err)
	}
	processor.RollbackFile()
	if processor.ActiveBooks() != 0 || processor.Stats().RowsMatched != 0 {
		t.Fatalf("rollback left state: books=%d stats=%+v", processor.ActiveBooks(), processor.Stats())
	}
}

func TestArchiveProcessorDefersUntilInWindowBook(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	delta := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventPriceChange,
		Side:        NullableString{Value: "BUY", Valid: true},
		PriceE4:     NullableInt64{Value: 4_000, Valid: true},
		SizeE6:      NullableInt64{Value: 1_000_000, Valid: true},
	}
	if emitted, err := processor.Process(delta, true); err != nil || len(emitted) != 0 {
		t.Fatalf("delta emitted=%v err=%v", emitted, err)
	}
	book := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(time.Second), ReceivedTime: start.Add(time.Second)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[]`, Valid: true},
		AsksJSON:    NullableString{Value: `[]`, Valid: true},
	}
	emitted, err := processor.Process(book, true)
	if err != nil || len(emitted) != 1 || emitted[0].Type != EventBook {
		t.Fatalf("book emitted=%v err=%v", emitted, err)
	}
	if processor.Stats().PreInitializationSkipped != 1 {
		t.Fatalf("unexpected stats: %+v", processor.Stats())
	}
}

func TestArchiveProcessorEvictsBooksAfterHorizon(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	book := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[]`, Valid: true},
		AsksJSON:    NullableString{Value: `[]`, Valid: true},
	}
	if _, err := processor.Process(book, false); err != nil {
		t.Fatalf("process book: %v", err)
	}
	if processor.ActiveBooks() != 1 {
		t.Fatalf("active books = %d, want 1", processor.ActiveBooks())
	}
	processor.AdvanceWatermark(start.Add(50 * time.Hour))
	if processor.ActiveBooks() != 0 {
		t.Fatalf("active books = %d after horizon, want 0", processor.ActiveBooks())
	}
	if _, ok := processor.catalog.Conditions["condition"]; !ok {
		t.Fatal("condition metadata must remain after book eviction")
	}
}

func TestArchiveProcessorCommittedReplayPreventsDuplicateSyntheticBook(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	processor := newTestArchiveProcessor(start)
	book := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(-time.Minute), ReceivedTime: start.Add(-time.Minute)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventBook,
		BidsJSON:    NullableString{Value: `[["0.4","2"]]`, Valid: true},
		AsksJSON:    NullableString{Value: `[["0.6","3"]]`, Valid: true},
	}
	if _, err := processor.Process(book, false); err != nil {
		t.Fatalf("warm book: %v", err)
	}
	committed := RawEvent{
		Key:         EventKey{ExchangeTime: start, ReceivedTime: start},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventLastTradePrice,
	}
	if err := processor.ReplayCommitted(committed); err != nil {
		t.Fatalf("replay committed event: %v", err)
	}
	late := RawEvent{
		Key:         EventKey{ExchangeTime: start.Add(time.Second), ReceivedTime: start.Add(time.Hour)},
		ConditionID: "condition",
		AssetID:     "asset",
		Type:        EventLastTradePrice,
	}
	emitted, err := processor.Process(late, true)
	if err != nil || len(emitted) != 1 || emitted[0].Key.SourceFile != late.Key.SourceFile {
		t.Fatalf("late emitted=%+v err=%v", emitted, err)
	}
	if processor.Stats().SyntheticBooksInserted != 0 {
		t.Fatal("committed replay must prevent a second synthetic snapshot")
	}
}

func TestFileProgressMiBRoundsUpAndNeverReturnsZero(t *testing.T) {
	for _, test := range []struct {
		bytes int64
		want  int
	}{
		{bytes: 0, want: 1},
		{bytes: 1, want: 1},
		{bytes: 1024 * 1024, want: 1},
		{bytes: 1024*1024 + 1, want: 2},
	} {
		if got := fileProgressMiB(test.bytes); got != test.want {
			t.Fatalf("fileProgressMiB(%d) = %d, want %d", test.bytes, got, test.want)
		}
	}
}

func TestImportArchiveSkipsUnreadableWritableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "polymarket_orderbook_2026-08-18T15.parquet")
	if err := os.WriteFile(path, []byte("not a parquet file"), 0o600); err != nil {
		t.Fatalf("write corrupt parquet: %v", err)
	}
	result, err := ImportArchive(context.Background(), nil, []RawFileRef{{Path: path, Name: filepath.Base(path)}}, newTestArchiveProcessor(time.Now().UTC()).catalog, ArchiveImportOptions{DryRun: true})
	if err != nil {
		t.Fatalf("import archive: %v", err)
	}
	if result.FilesSkipped != 1 || result.FilesProcessed != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
}

func newTestArchiveProcessor(start time.Time) *ArchiveProcessor {
	return NewArchiveProcessor(&ConditionCatalog{Conditions: map[string]ConditionMeta{
		"condition": {
			ConditionID: "condition",
			WindowStart: start,
			WindowEnd:   start.Add(5 * time.Minute),
		},
	}}, 49*time.Hour)
}
