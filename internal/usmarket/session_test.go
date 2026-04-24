package usmarket

import (
	"testing"
	"time"
)

func TestEasterDate(t *testing.T) {
	// Known Easter dates
	tests := map[int]string{
		2023: "2023-04-09",
		2024: "2024-03-31",
		2025: "2025-04-20",
		2026: "2026-04-05",
	}
	for year, want := range tests {
		got := easterDate(year).Format("2006-01-02")
		if got != want {
			t.Errorf("easterDate(%d) = %s, want %s", year, got, want)
		}
	}
}

func TestGoodFriday(t *testing.T) {
	tests := map[int]string{
		2023: "2023-04-07",
		2024: "2024-03-29",
		2025: "2025-04-18",
	}
	for year, want := range tests {
		got := goodFriday(year).Format("2006-01-02")
		if got != want {
			t.Errorf("goodFriday(%d) = %s, want %s", year, got, want)
		}
	}
}

func TestNthWeekday(t *testing.T) {
	// MLK Day 2024 — 3rd Monday of January
	mlk := nthWeekday(2024, time.January, time.Monday, 3)
	if mlk.Format("2006-01-02") != "2024-01-15" {
		t.Errorf("MLK 2024 = %s, want 2024-01-15", mlk.Format("2006-01-02"))
	}

	// Thanksgiving 2024 — 4th Thursday of November
	tg := nthWeekday(2024, time.November, time.Thursday, 4)
	if tg.Format("2006-01-02") != "2024-11-28" {
		t.Errorf("Thanksgiving 2024 = %s, want 2024-11-28", tg.Format("2006-01-02"))
	}
}

func TestLastWeekday(t *testing.T) {
	// Memorial Day 2024 — last Monday of May
	mem := lastWeekday(2024, time.May, time.Monday)
	if mem.Format("2006-01-02") != "2024-05-27" {
		t.Errorf("Memorial Day 2024 = %s, want 2024-05-27", mem.Format("2006-01-02"))
	}
}

func TestObservedDate(t *testing.T) {
	// Saturday → Friday
	sat := time.Date(2023, 1, 7, 0, 0, 0, 0, time.UTC)
	if got := observedDate(sat); got.Weekday() != time.Friday {
		t.Errorf("Saturday observed: got %s (%s)", got.Format("2006-01-02"), got.Weekday())
	}
	// Sunday → Monday
	sun := time.Date(2023, 1, 8, 0, 0, 0, 0, time.UTC)
	if got := observedDate(sun); got.Weekday() != time.Monday {
		t.Errorf("Sunday observed: got %s (%s)", got.Format("2006-01-02"), got.Weekday())
	}
}

func TestUSMarketHolidays(t *testing.T) {
	h := usMarketHolidays(2024)

	// Christmas 2024: Dec 25 is Wednesday → observed Dec 25
	if !h["2024-12-25"] {
		t.Error("2024-12-25 (Christmas) should be a holiday")
	}
	// New Year's 2024: Jan 1 is Monday → observed Jan 1
	if !h["2024-01-01"] {
		t.Error("2024-01-01 (New Year) should be a holiday")
	}
	// Good Friday 2024: March 29
	if !h["2024-03-29"] {
		t.Error("2024-03-29 (Good Friday) should be a holiday")
	}
	// A regular trading day should NOT be in the map
	if h["2024-01-02"] {
		t.Error("2024-01-02 should not be a holiday")
	}
}

func TestUSMarketHolidaysIncludesJimmyCarterMourningDay(t *testing.T) {
	h := usMarketHolidays(2025)
	if !h["2025-01-09"] {
		t.Error("2025-01-09 should be a holiday due to the national day of mourning for President Jimmy Carter")
	}
	if h["2025-01-08"] {
		t.Error("2025-01-08 should remain a trading day")
	}
	if h["2025-01-10"] {
		t.Error("2025-01-10 should remain a trading day")
	}
}

func TestClassifyTimestamp(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	sm := make(SessionMap)
	for _, s := range sessions {
		sm[s.MarketDate.Format("2006-01-02")] = s
	}

	loc := newYorkLocation

	// Jan 2, 2024 is a Tuesday (regular trading day)
	tests := []struct {
		name        string
		ts          time.Time
		wantKind    string
		wantRegular uint8
		wantMarket  string
	}{
		{
			name:        "premarket 07:00 ET",
			ts:          time.Date(2024, 1, 2, 7, 0, 0, 0, loc).UTC(),
			wantKind:    "premarket",
			wantRegular: 0,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "regular open 09:30 ET",
			ts:          time.Date(2024, 1, 2, 9, 30, 0, 0, loc).UTC(),
			wantKind:    "regular",
			wantRegular: 1,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "regular 12:00 ET",
			ts:          time.Date(2024, 1, 2, 12, 0, 0, 0, loc).UTC(),
			wantKind:    "regular",
			wantRegular: 1,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "regular last min 15:59 ET",
			ts:          time.Date(2024, 1, 2, 15, 59, 0, 0, loc).UTC(),
			wantKind:    "regular",
			wantRegular: 1,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "postmarket 16:00 ET",
			ts:          time.Date(2024, 1, 2, 16, 0, 0, 0, loc).UTC(),
			wantKind:    "postmarket",
			wantRegular: 0,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "postmarket 18:00 ET",
			ts:          time.Date(2024, 1, 2, 18, 0, 0, 0, loc).UTC(),
			wantKind:    "postmarket",
			wantRegular: 0,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "closed after 20:00 ET",
			ts:          time.Date(2024, 1, 2, 20, 30, 0, 0, loc).UTC(),
			wantKind:    "closed",
			wantRegular: 0,
			wantMarket:  "2024-01-02",
		},
		{
			name:        "closed before premarket 03:00 ET",
			ts:          time.Date(2024, 1, 2, 3, 0, 0, 0, loc).UTC(),
			wantKind:    "closed",
			wantRegular: 0,
			wantMarket:  "2024-01-02",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md, kind, reg, _, _ := sm.ClassifyTimestamp(tt.ts)
			if kind != tt.wantKind {
				t.Errorf("session_kind: got %q, want %q", kind, tt.wantKind)
			}
			if reg != tt.wantRegular {
				t.Errorf("is_regular: got %d, want %d", reg, tt.wantRegular)
			}
			if md.Format("2006-01-02") != tt.wantMarket {
				t.Errorf("market_date: got %s, want %s", md.Format("2006-01-02"), tt.wantMarket)
			}
		})
	}
}

func TestClassifyTimestampHoliday(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	sm := make(SessionMap)
	for _, s := range sessions {
		sm[s.MarketDate.Format("2006-01-02")] = s
	}

	// Good Friday 2024-03-29 — market is closed
	ts := time.Date(2024, 3, 29, 14, 30, 0, 0, time.UTC) // would be 10:30 ET
	_, kind, reg, _, _ := sm.ClassifyTimestamp(ts)
	if kind != "closed" {
		t.Errorf("Good Friday: got %q, want closed", kind)
	}
	if reg != 0 {
		t.Errorf("Good Friday: is_regular = %d, want 0", reg)
	}
}

func TestClassifyTimestampWeekend(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	sm := make(SessionMap)
	for _, s := range sessions {
		sm[s.MarketDate.Format("2006-01-02")] = s
	}

	// Saturday 2024-01-06
	ts := time.Date(2024, 1, 6, 15, 0, 0, 0, time.UTC)
	_, kind, reg, _, _ := sm.ClassifyTimestamp(ts)
	if kind != "closed" {
		t.Errorf("Saturday: got %q, want closed", kind)
	}
	if reg != 0 {
		t.Errorf("Saturday: is_regular = %d, want 0", reg)
	}
}

func TestSessionSeq(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	sm := make(SessionMap)
	for _, s := range sessions {
		sm[s.MarketDate.Format("2006-01-02")] = s
	}

	loc := newYorkLocation

	// 09:30 ET → seq 0
	ts0 := time.Date(2024, 1, 2, 9, 30, 0, 0, loc).UTC()
	_, _, _, _, seq0 := sm.ClassifyTimestamp(ts0)
	if seq0 != 0 {
		t.Errorf("09:30 ET seq: got %d, want 0", seq0)
	}

	// 10:30 ET → seq 60
	ts60 := time.Date(2024, 1, 2, 10, 30, 0, 0, loc).UTC()
	_, _, _, _, seq60 := sm.ClassifyTimestamp(ts60)
	if seq60 != 60 {
		t.Errorf("10:30 ET seq: got %d, want 60", seq60)
	}

	// 15:59 ET → seq 389
	ts389 := time.Date(2024, 1, 2, 15, 59, 0, 0, loc).UTC()
	_, _, _, _, seq389 := sm.ClassifyTimestamp(ts389)
	if seq389 != 389 {
		t.Errorf("15:59 ET seq: got %d, want 389", seq389)
	}
}

func TestEarlyCloseSession(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	sm := make(SessionMap)
	for _, s := range sessions {
		sm[s.MarketDate.Format("2006-01-02")] = s
	}

	loc := newYorkLocation

	// Black Friday 2024: Nov 29 — early close at 13:00 ET
	// 12:59 ET should be regular, 13:00 ET should be postmarket
	ts1259 := time.Date(2024, 11, 29, 12, 59, 0, 0, loc).UTC()
	_, kind, reg, _, _ := sm.ClassifyTimestamp(ts1259)
	if kind != "regular" || reg != 1 {
		t.Errorf("Black Friday 12:59 ET: got kind=%q reg=%d, want regular/1", kind, reg)
	}

	ts1300 := time.Date(2024, 11, 29, 13, 0, 0, 0, loc).UTC()
	_, kind2, reg2, _, _ := sm.ClassifyTimestamp(ts1300)
	if kind2 != "postmarket" || reg2 != 0 {
		t.Errorf("Black Friday 13:00 ET: got kind=%q reg=%d, want postmarket/0", kind2, reg2)
	}
}

func TestGenerateSessionCalendarNoWeekends(t *testing.T) {
	sessions := GenerateSessionCalendar(2024, 2024)
	for _, s := range sessions {
		wd := s.MarketDate.Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			t.Errorf("session calendar contains weekend: %s (%s)", s.MarketDate.Format("2006-01-02"), wd)
		}
	}
}

func TestKlineTimeFuncSubHour(t *testing.T) {
	// Verify sub-hour intervals use natural time functions
	iv5m := KlineInterval{Suffix: "5m", Seconds: 300}
	got := klineTimeFunc(iv5m)
	if got != "toStartOfFiveMinutes(timestamp)" {
		t.Errorf("5m timeFunc = %q", got)
	}
}

func TestKlineTimeFuncSessionAligned(t *testing.T) {
	iv1h := KlineInterval{Suffix: "1h", Seconds: 3600}
	got := klineTimeFunc(iv1h)
	if got == "toStartOfHour(timestamp)" {
		t.Error("1h should NOT use toStartOfHour anymore")
	}
	// Should reference session_open
	if !contains(got, "session_open") {
		t.Errorf("1h timeFunc should reference session_open: %q", got)
	}
}

func TestKlineTimeFuncDaily(t *testing.T) {
	iv1d := KlineInterval{Suffix: "1d", Seconds: 0}
	got := klineTimeFunc(iv1d)
	if !contains(got, "market_date") {
		t.Errorf("1d timeFunc should reference market_date: %q", got)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
