package polymarket

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestDecodePMXTRow(t *testing.T) {
	row := parquet.Row{
		parquet.Int64Value(1_786_165_201_822).Level(0, 0, 0),
		parquet.Int64Value(1_786_165_201_699).Level(0, 0, 1),
		parquet.FixedLenByteArrayValue([]byte("0x00000977017fa72fb6b1908ae694000d3b51f442c2552656b10bdbbfd16ff707")).Level(0, 0, 2),
		parquet.ByteArrayValue([]byte("price_change")).Level(0, 0, 3),
		parquet.ByteArrayValue([]byte("4455")).Level(0, 0, 4),
		parquet.NullValue().Level(0, 0, 5),
		parquet.NullValue().Level(0, 0, 6),
		parquet.FixedLenByteArrayValue([]byte{0, 0, 0x1c, 0xe8}).Level(0, 1, 7),
		parquet.FixedLenByteArrayValue([]byte{0, 0, 0, 0, 0x5b, 0xac, 0x04, 0x80}).Level(0, 1, 8),
		parquet.ByteArrayValue([]byte("SELL")).Level(0, 1, 9),
		parquet.FixedLenByteArrayValue([]byte{0, 0, 0, 0x28}).Level(0, 1, 10),
		parquet.FixedLenByteArrayValue([]byte{0, 0, 0, 0x46}).Level(0, 1, 11),
		parquet.NullValue().Level(0, 0, 12),
		parquet.NullValue().Level(0, 0, 13),
		parquet.NullValue().Level(0, 0, 14),
		parquet.NullValue().Level(0, 0, 15),
	}
	conditionID := string(row[2].Bytes())
	event, err := decodePMXTRow(row, conditionID, "sample.parquet", 7)
	if err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if event.Type != EventPriceChange || event.Side.Value != "SELL" || event.PriceE4.Value != 7_400 || event.SizeE6.Value != 1_538_000_000 {
		t.Fatalf("unexpected decoded event: %+v", event)
	}
	if event.BestBidE4.Value != 40 || event.BestAskE4.Value != 70 || event.BidsJSON.Valid {
		t.Fatalf("unexpected nullable fields: %+v", event)
	}
	if got := event.Key.ReceivedTime; !got.Equal(time.UnixMilli(1_786_165_201_822)) {
		t.Fatalf("received time = %s", got)
	}
}

func TestSignedBigEndianHandlesNegativeDecimal(t *testing.T) {
	if got, err := signedBigEndian([]byte{0xff, 0xff, 0xff, 0xff}); err != nil || got != -1 {
		t.Fatalf("signed decimal = %d, %v; want -1", got, err)
	}
	if _, err := signedBigEndian([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("expected decimal wider than int64 to be rejected")
	}
}

func TestLoadConditionCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conditions.jsonl")
	data := `{"status":"empty","condition_id":"ignored"}
{"status":"ok","event_id":"1","market_id":"2","condition_id":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","slug":"btc-updown-5m-1","asset":"btc","period":"5m","window_start":1,"start_date":"2026-04-13T19:00:00Z","end_date":"2026-04-13T19:05:00Z","closed":true,"outcomes":["Up","Down"],"clob_token_ids":["up-token","down-token"]}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	catalog, err := LoadConditionCatalog(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(catalog.Conditions) != 1 || catalog.Assets["up-token"] == "" || catalog.Conditions[catalog.Assets["up-token"]].Underlying != "BTC" {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	meta := catalog.Conditions[catalog.Assets["up-token"]]
	if got := meta.WindowEnd.Sub(meta.WindowStart); got != 5*time.Minute {
		t.Fatalf("contract window duration = %s, want 5m", got)
	}
}

func TestLoadConditionCatalogByConditionJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conditions.json")
	data := `{
"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": {"event_id":"1","market_id":"2","slug":"eth-updown-5m-1","asset":"eth","period":"5m","window_start":1,"closed":true,"token_up":"up-token","token_down":"down-token","resolved":true,"winner":"Down"}
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	catalog, err := LoadConditionCatalog(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	meta := catalog.Conditions["0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"]
	if !meta.Resolved || meta.Winner != 2 || len(meta.Outcomes) != 2 || meta.Outcomes[0] != "Up" || catalog.Assets["down-token"] != meta.ConditionID {
		t.Fatalf("unexpected keyed catalog metadata: %+v assets=%+v", meta, catalog.Assets)
	}
}

func TestBinaryWinnerTreatsVoidAndUnknownAsZero(t *testing.T) {
	for _, test := range []struct {
		winner string
		want   uint8
	}{
		{winner: "Up", want: 1},
		{winner: "down", want: 2},
		{winner: "", want: 0},
		{winner: "Void", want: 0},
	} {
		if got := binaryWinner(test.winner); got != test.want {
			t.Fatalf("binaryWinner(%q) = %d, want %d", test.winner, got, test.want)
		}
	}
}

func TestMetadataVersionIsMonotonicByImportTime(t *testing.T) {
	older := metadataVersion(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	newer := metadataVersion(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	if newer <= older {
		t.Fatalf("newer metadata version %d must exceed older version %d", newer, older)
	}
}

func TestRawEventReplayEventMapsBookSide(t *testing.T) {
	replay, err := (RawEvent{
		Type:      EventPriceChange,
		Side:      NullableString{Value: "BUY", Valid: true},
		PriceE4:   NullableInt64{Value: 4_000, Valid: true},
		SizeE6:    NullableInt64{Value: 2_000_000, Valid: true},
		BestBidE4: NullableInt64{Value: 4_000, Valid: true},
		BestAskE4: NullableInt64{Value: 6_000, Valid: true},
	}).ReplayEvent()
	if err != nil {
		t.Fatalf("map replay event: %v", err)
	}
	if replay.Side != SideBid || replay.Price != 4_000 || !replay.HasBestBid || !replay.HasBestAsk {
		t.Fatalf("unexpected replay event: %+v", replay)
	}
	if _, err := (RawEvent{Side: NullableString{Value: "HOLD", Valid: true}}).ReplayEvent(); err == nil {
		t.Fatal("expected unsupported side to fail")
	}
}

func TestEventWithinConditionWindowUsesHalfOpenBounds(t *testing.T) {
	start := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	meta := ConditionMeta{WindowStart: start, WindowEnd: start.Add(5 * time.Minute)}
	for _, test := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{name: "before", at: start.Add(-time.Nanosecond), want: false},
		{name: "start inclusive", at: start, want: true},
		{name: "inside", at: start.Add(time.Minute), want: true},
		{name: "end exclusive", at: meta.WindowEnd, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := eventWithinConditionWindow(RawEvent{Key: EventKey{ExchangeTime: test.at}}, meta)
			if got != test.want {
				t.Fatalf("eventWithinConditionWindow() = %v, want %v", got, test.want)
			}
		})
	}
}
