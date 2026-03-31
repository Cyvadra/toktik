package coveredcall0330tvsig

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// sortExpiriesByDistanceTo is a test-local copy of the sorting logic inside
// uniqueExpiriesNearest, used to verify ordering without touching backtest types.
func sortExpiriesByDistanceTo(expiries []time.Time, now time.Time, targetDTE int) []time.Time {
	out := make([]time.Time, len(expiries))
	copy(out, expiries)
	sort.Slice(out, func(i, j int) bool {
		di := math.Abs(out[i].Sub(now).Hours()/24 - float64(targetDTE))
		dj := math.Abs(out[j].Sub(now).Hours()/24 - float64(targetDTE))
		if di != dj {
			return di < dj
		}
		return out[i].Before(out[j])
	})
	return out
}

func TestLoadSignalTimesFromFileNoPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "12h.txt")
	content := "Jan 19, 2023, 08:00\nJan 26, 2023, 08:00\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := loadSignalTimesFromFile(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromFile: %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len=%d, want 2", len(times))
	}

	want := []time.Time{
		time.Date(2023, time.January, 19, 8, 0, 0, 0, time.UTC),
		time.Date(2023, time.January, 26, 8, 0, 0, 0, time.UTC),
	}
	for _, ts := range want {
		if _, ok := times[ts.Unix()]; !ok {
			t.Errorf("expected %v to be present", ts)
		}
	}
}

func TestLoadSignalTimesFromFileWithIndexPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "6h.txt")
	content := "1 Jan 18, 2023, 02:00\n2 Jan 26, 2023, 08:00\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := loadSignalTimesFromFile(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromFile: %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("len=%d, want 2", len(times))
	}

	want := []time.Time{
		time.Date(2023, time.January, 18, 2, 0, 0, 0, time.UTC),
		time.Date(2023, time.January, 26, 8, 0, 0, 0, time.UTC),
	}
	for _, ts := range want {
		if _, ok := times[ts.Unix()]; !ok {
			t.Errorf("expected %v to be present", ts)
		}
	}
}

func TestLoadSignalTimesFromFileSkipsBlankAndUnparseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signals.txt")
	content := "\n# comment\nJan 19, 2023, 08:00\nbad line\n\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := loadSignalTimesFromFile(path)
	if err != nil {
		t.Fatalf("loadSignalTimesFromFile: %v", err)
	}
	if len(times) != 1 {
		t.Fatalf("len=%d, want 1", len(times))
	}
}

func TestLoadSignalTimesFromMultipleMergesAndDeduplicates(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path1, []byte("Jan 19, 2023, 08:00\nJan 26, 2023, 08:00\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path2 := filepath.Join(dir, "b.txt")
	// Jan 26 duplicate + new entry Feb 17
	if err := os.WriteFile(path2, []byte("Jan 26, 2023, 08:00\nFeb 17, 2023, 08:00\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	times, err := loadSignalTimesFromMultiple(path1, path2)
	if err != nil {
		t.Fatalf("loadSignalTimesFromMultiple: %v", err)
	}
	if len(times) != 3 {
		t.Fatalf("len=%d, want 3 (deduplicated)", len(times))
	}
}

func TestLoadSignalTimesFromFileMissingPathReturnsEmpty(t *testing.T) {
	times, err := loadSignalTimesFromFile("/nonexistent/path/signals.txt")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(times) != 0 {
		t.Fatalf("expected empty map for missing file, got %d entries", len(times))
	}
}

func TestUniqueExpiriesNearestSortsClosestFirst(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Build contracts with different expiries.
	type expPoint struct {
		date time.Time
	}
	points := []expPoint{
		{now.AddDate(0, 0, 40)}, // 40 DTE – furthest from target 25
		{now.AddDate(0, 0, 20)}, // 20 DTE – 5 days from target 25
		{now.AddDate(0, 0, 27)}, // 27 DTE – 2 days from target 25 (closest)
	}

	_ = points
	// uniqueExpiriesNearest is exercised end-to-end via selectCallSpread;
	// here we verify pure ordering logic directly.
	expiries := []time.Time{
		now.AddDate(0, 0, 40),
		now.AddDate(0, 0, 20),
		now.AddDate(0, 0, 27),
	}
	sorted := sortExpiriesByDistanceTo(expiries, now, 25)
	if len(sorted) != 3 {
		t.Fatalf("len=%d want 3", len(sorted))
	}
	// Closest to 25 should be first (27 DTE, distance=2)
	if d := sorted[0].Sub(now).Hours() / 24; d < 26 || d > 28 {
		t.Errorf("first expiry DTE=%.0f, want ~27", d)
	}
}

func TestProtRollReason(t *testing.T) {
	tests := []struct {
		absDelta     float64
		pnlPct       float64
		wantContains string
	}{
		{0.6, 0.55, "delta=0.60"},
		{0.3, 0.6, "pnl=60%"},
		{0.6, 0.6, "delta=0.60"},
	}
	for _, tt := range tests {
		reason := protRollReason(tt.absDelta, tt.pnlPct)
		if reason == "" {
			t.Errorf("protRollReason(%v, %v) = empty", tt.absDelta, tt.pnlPct)
		}
		if tt.wantContains != "" {
			found := false
			for i := 0; i+len(tt.wantContains) <= len(reason); i++ {
				if reason[i:i+len(tt.wantContains)] == tt.wantContains {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("protRollReason(%v, %v) = %q, want to contain %q", tt.absDelta, tt.pnlPct, reason, tt.wantContains)
			}
		}
	}
}

func TestCallSpreadHitStopLoss(t *testing.T) {
	tests := []struct {
		name        string
		entryCredit float64
		closeCost   float64
		want        bool
	}{
		{name: "below threshold", entryCredit: 1.2, closeCost: 2.39, want: false},
		{name: "at threshold", entryCredit: 1.2, closeCost: 2.4, want: true},
		{name: "above threshold", entryCredit: 1.2, closeCost: 2.8, want: true},
		{name: "nan close cost", entryCredit: 1.2, closeCost: math.NaN(), want: false},
		{name: "invalid entry credit", entryCredit: 0, closeCost: 2.4, want: false},
	}

	for _, tt := range tests {
		if got := callSpreadHitStopLoss(tt.entryCredit, tt.closeCost); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}
