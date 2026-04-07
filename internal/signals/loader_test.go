package signals

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTimesFromTextWithOptionalIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.txt")
	if err := os.WriteFile(path, []byte("1 Jan 2, 2024, 15:04\nJan 3, 2024, 15:04\ninvalid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := LoadTimes(Config{
		Paths:             []string{path},
		TimeLayouts:       []string{"Jan 2, 2006, 15:04"},
		Location:          time.UTC,
		TextOptionalIndex: true,
	})
	if err != nil {
		t.Fatalf("LoadTimes() error = %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len(times) = %d, want 2", len(times))
	}
}

func TestLoadTimesFromCSVWithEntryMatchers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.csv")
	content := "日期和时间,类型,信号\n2026/4/1 08:00,进场,做空\n2026/4/1 12:00,出场,做空\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := LoadTimes(Config{
		Paths:            []string{path},
		TimestampColumns: []string{"日期和时间"},
		TypeColumns:      []string{"类型"},
		SignalColumns:    []string{"信号"},
		TimeLayouts:      []string{"2006/1/2 15:04"},
		Location:         time.FixedZone("UTC+8", 8*3600),
		EntryMatchers:    []string{"进场", "开仓", "做空", "bearish", "divergence"},
	})
	if err != nil {
		t.Fatalf("LoadTimes() error = %v", err)
	}
	if len(times) != 1 {
		t.Fatalf("len(times) = %d, want 1", len(times))
	}
	if _, ok := times[time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC).Unix()]; !ok {
		t.Fatalf("expected UTC-converted timestamp to exist")
	}
}

func TestBuildBinarySeries(t *testing.T) {
	ts := []time.Time{
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 1, 0, 0, 0, time.UTC),
	}
	series := BuildBinarySeries(ts, map[int64]struct{}{ts[1].Unix(): {}})
	if len(series) != 2 || series[0] != 0 || series[1] != 1 {
		t.Fatalf("BuildBinarySeries() = %#v, want [0 1]", series)
	}
}
