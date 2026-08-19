package polymarket

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestRawEventStageRoundTrip(t *testing.T) {
	path := t.TempDir() + "/events.parquet"
	want := RawEvent{
		Key:         EventKey{ExchangeTime: time.UnixMilli(1_700_000_000_123).UTC(), ReceivedTime: time.UnixMilli(1_700_000_000_456).UTC(), SourceFile: "source.parquet", SourceRow: 42},
		ConditionID: "condition", AssetID: "asset", Type: EventPriceChange,
		BidsJSON: NullableString{Value: `[["0.4","2"]]`, Valid: true},
		PriceE4:  NullableInt64{Value: 4_000, Valid: true}, SizeE6: NullableInt64{Value: 2_000_000, Valid: true},
		Side: NullableString{Value: "BUY", Valid: true}, BestBidE4: NullableInt64{Value: 4_000, Valid: true},
		FeeRateBPS: NullableUint16{Value: 25, Valid: true}, TransactionHash: NullableString{Value: "0xabc", Valid: true},
		NewTickSizeE4: NullableInt64{Value: 10, Valid: true},
	}
	events := make(chan RawEvent, 1)
	events <- want
	close(events)
	if rows, err := WriteRawEventStage(context.Background(), path, events); err != nil || rows != 1 {
		t.Fatalf("write stage rows=%d err=%v", rows, err)
	}
	var got []RawEvent
	if rows, err := ReadRawEventStage(context.Background(), path, func(event RawEvent) error {
		got = append(got, event)
		return nil
	}); err != nil || rows != 1 {
		t.Fatalf("read stage rows=%d err=%v", rows, err)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("round trip event = %+v, want %+v", got, want)
	}
	if got[0].Key.EventID() != want.Key.EventID() {
		t.Fatalf("event ID changed: %s != %s", got[0].Key.EventID(), want.Key.EventID())
	}
}

func TestRawEventStageCancellationRemovesTemporaryFile(t *testing.T) {
	path := t.TempDir() + "/events.parquet"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := make(chan RawEvent, 1)
	events <- RawEvent{}
	close(events)
	if _, err := WriteRawEventStage(ctx, path, events); err == nil {
		t.Fatal("expected canceled stage write")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary stage remains: %v", err)
	}
}

func TestRawEventStageManifestRequiresArtifact(t *testing.T) {
	root := t.TempDir()
	path := RawEventStagePath(root, "source.parquet", "source-hash", "selection-hash")
	if filepath.Ext(path) != ".parquet" {
		t.Fatalf("stage path = %q", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := RawEventStageManifest{SourceFile: "source.parquet", SourceHash: "source-hash", SelectionHash: "selection-hash", SourceRows: 10, SelectedRows: 2}
	if err := CommitRawEventStageManifest(path, manifest); err != nil {
		t.Fatalf("commit manifest: %v", err)
	}
	if _, err := LoadRawEventStageManifest(path); !os.IsNotExist(err) {
		t.Fatalf("manifest without artifact error = %v, want not exist", err)
	}
	if err := os.WriteFile(path, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadRawEventStageManifest(path)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if loaded.SourceRows != 10 || loaded.SelectedRows != 2 || loaded.SchemaVersion != RawEventStageSchemaVersion {
		t.Fatalf("loaded manifest = %+v", loaded)
	}
}

func TestStartArchiveStagingResolvesAllResultsAfterFailure(t *testing.T) {
	files := []RawFileRef{
		{Path: filepath.Join(t.TempDir(), "missing-1.parquet"), Name: "missing-1.parquet", Fingerprint: "one"},
		{Path: filepath.Join(t.TempDir(), "missing-2.parquet"), Name: "missing-2.parquet", Fingerprint: "two"},
		{Path: filepath.Join(t.TempDir(), "missing-3.parquet"), Name: "missing-3.parquet", Fingerprint: "three"},
	}
	results, cancel, wait := startArchiveStaging(context.Background(), files, map[string]ConditionMeta{}, "selection", t.TempDir(), 1)
	defer cancel()
	for index, result := range results {
		select {
		case completed := <-result:
			if completed.err == nil {
				t.Fatalf("result %d unexpectedly succeeded", index)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("result %d did not resolve after cancellation", index)
		}
	}
	wait()
}
