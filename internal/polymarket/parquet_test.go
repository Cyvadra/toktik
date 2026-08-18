package polymarket

import (
	"bytes"
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
	event, err := decodePMXTRow(row, "sample.parquet", 7)
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
	if got := signedBigEndian([]byte{0xff, 0xff, 0xff, 0xff}); got != -1 {
		t.Fatalf("signed decimal = %d, want -1", got)
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

func TestReadOKRowCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.parquet.ok")
	if err := os.WriteFile(path, []byte("size=123\nrows=19356105\ntime=now\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	rows, err := readOKRowCount(path)
	if err != nil || rows != 19_356_105 {
		t.Fatalf("read marker rows = %d, %v", rows, err)
	}
}

func TestMetadataVersionIsMonotonicByImportTime(t *testing.T) {
	older := metadataVersion(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC), MetadataSchemaVersion)
	newer := metadataVersion(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC), MetadataSchemaVersion)
	if newer <= older {
		t.Fatalf("newer metadata version %d must exceed older version %d", newer, older)
	}
}

func TestSourceRowCountFallsBackToParquetFooter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create parquet: %v", err)
	}
	type sampleRow struct {
		Value int64 `parquet:"value"`
	}
	writer := parquet.NewGenericWriter[sampleRow](file)
	if _, err := writer.Write([]sampleRow{{Value: 1}, {Value: 2}, {Value: 3}}); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close parquet file: %v", err)
	}
	if err := os.WriteFile(path+".ok", bytes.NewBufferString("size=1\n").Bytes(), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	rows, err := sourceRowCount(path)
	if err != nil || rows != 3 {
		t.Fatalf("source row count = %d, %v", rows, err)
	}
}
