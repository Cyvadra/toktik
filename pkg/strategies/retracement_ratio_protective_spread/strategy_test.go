package retracementratioprotectivespread

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSignalSource(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "12h", raw: "12h", want: "12h"},
		{name: "1d", raw: "1d", want: "1d"},
		{name: "trim and lower", raw: " 12H ", want: "12h"},
		{name: "missing", raw: "", wantErr: true},
		{name: "invalid", raw: "6h", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSignalSource(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSignalSource(%q) expected error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSignalSource(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("parseSignalSource(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLoadSignalTimesFromCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.csv")
	content := "交易 #,类型,日期和时间,信号\n1,多头进场,2023-01-06 08:00,Long_Init\n2,多头进场,2023-01-06 08:00,Long_Init\n3,多头进场,2023-05-03 08:00,Long_Init\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	times, err := loadSignalTimesFromCSV(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromCSV() error = %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len(times) = %d, want 2", len(times))
	}

	utc8 := time.FixedZone("UTC+8", 8*3600)
	for _, raw := range []string{"2023-01-06 08:00", "2023-05-03 08:00"} {
		ts, err := time.ParseInLocation(signalTimeLayout, raw, utc8)
		if err != nil {
			t.Fatalf("parse time %q: %v", raw, err)
		}
		if _, ok := times[ts.UTC().Unix()]; !ok {
			t.Fatalf("missing expected timestamp for %s", raw)
		}
	}
}
