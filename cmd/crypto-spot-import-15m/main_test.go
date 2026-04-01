package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSymbol(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"data/crypto-15m/BTCUSDT.csv", "BTC"},
		{"data/crypto-15m/ETHUSDT.csv", "ETH"},
		{"data/crypto-15m/1000BONKUSDT.csv", "1000BONK"},
		{"data/crypto-15m/0GUSDT.csv", "0G"},
		{"data/crypto-15m/USDT.csv", ""},
		{"data/crypto-15m/foo.csv", ""},
	}
	for _, tt := range tests {
		got := extractSymbol(tt.path)
		if got != tt.want {
			t.Errorf("extractSymbol(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestParseTimestampEpoch(t *testing.T) {
	ts, err := parseTimestamp("1758168900")
	if err != nil {
		t.Fatalf("parseTimestamp epoch: %v", err)
	}
	if ts.Year() < 2020 || ts.Year() > 2030 {
		t.Fatalf("timestamp out of expected range: %v", ts)
	}
}

func TestParseTimestampDatetime(t *testing.T) {
	ts, err := parseTimestamp("2025-06-06 16:00:00")
	if err != nil {
		t.Fatalf("parseTimestamp datetime: %v", err)
	}
	if ts.Year() != 2025 || ts.Month() != 6 || ts.Day() != 6 {
		t.Fatalf("unexpected date: %v", ts)
	}
}

func TestParseCSV15m(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "TESTUSDT.csv")
	content := `Close,High,Low,Open,Timestamp,VolumeBase,VolumeQuote
100.5,101.0,99.5,100.0,1758168900,310.0,828.589
101.0,102.0,100.0,100.5,1758169800,3544.0,9164.826
`
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := parseCSV15m(csvPath)
	if err != nil {
		t.Fatalf("parseCSV15m: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	if rows[0].Open != 100.0 {
		t.Errorf("row[0].Open = %f, want 100.0", rows[0].Open)
	}
	if rows[0].Close != 100.5 {
		t.Errorf("row[0].Close = %f, want 100.5", rows[0].Close)
	}
	if rows[0].VolumeBase != 310.0 {
		t.Errorf("row[0].VolumeBase = %f, want 310.0", rows[0].VolumeBase)
	}
	if rows[0].VolumeQuote != 828.589 {
		t.Errorf("row[0].VolumeQuote = %f, want 828.589", rows[0].VolumeQuote)
	}
}

func TestParseCSV15mScientificNotation(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "TESTUSDT.csv")
	content := `Close,High,Low,Open,Timestamp,VolumeBase,VolumeQuote
2.3972,2.6202,2.3675,2.5919,1758176100,558226.0,1.3769081e6
`
	if err := os.WriteFile(csvPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rows, err := parseCSV15m(csvPath)
	if err != nil {
		t.Fatalf("parseCSV15m: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	if rows[0].VolumeQuote < 1376908.0 || rows[0].VolumeQuote > 1376909.0 {
		t.Errorf("row[0].VolumeQuote = %f, expected ~1376908.1", rows[0].VolumeQuote)
	}
}
