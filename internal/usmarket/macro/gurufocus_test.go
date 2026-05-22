package macro

import (
	"testing"
	"time"
)

func TestSelectGurufocusMonthAnchorUsesPublicationDay(t *testing.T) {
	month := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	anchor, ok := selectGurufocusMonthAnchor(month, []tradingDayAnchor{
		{TradingDay: time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC), FirstTS: time.Date(2026, 5, 8, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 5, 8, 19, 59, 0, 0, time.UTC), LastClose: 5700},
		{TradingDay: time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC), FirstTS: time.Date(2026, 5, 12, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 5, 12, 19, 59, 0, 0, time.UTC), LastClose: 5800},
		{TradingDay: time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC), FirstTS: time.Date(2026, 5, 13, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 5, 13, 19, 59, 0, 0, time.UTC), LastClose: 5825},
	})
	if !ok {
		t.Fatal("expected anchor")
	}
	if got := anchor.FirstTS; !got.Equal(time.Date(2026, 5, 12, 13, 30, 0, 0, time.UTC)) {
		t.Fatalf("FirstTS=%s want 2026-05-12 13:30:00Z", got)
	}
	if got := anchor.LastTS; !got.Equal(time.Date(2026, 5, 12, 19, 59, 0, 0, time.UTC)) {
		t.Fatalf("LastTS=%s want 2026-05-12 19:59:00Z", got)
	}
	if got := anchor.LastClose; got != 5800 {
		t.Fatalf("LastClose=%v want 5800", got)
	}
}

func TestSelectGurufocusMonthAnchorFallsForwardToNextTradingDay(t *testing.T) {
	month := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	anchor, ok := selectGurufocusMonthAnchor(month, []tradingDayAnchor{
		{TradingDay: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), FirstTS: time.Date(2026, 7, 10, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 7, 10, 19, 59, 0, 0, time.UTC), LastClose: 6200},
		{TradingDay: time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC), FirstTS: time.Date(2026, 7, 13, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 7, 13, 19, 59, 0, 0, time.UTC), LastClose: 6250},
	})
	if !ok {
		t.Fatal("expected anchor")
	}
	if got := anchor.LastTS; !got.Equal(time.Date(2026, 7, 13, 19, 59, 0, 0, time.UTC)) {
		t.Fatalf("LastTS=%s want 2026-07-13 19:59:00Z", got)
	}
}

func TestBuildRowsUsesPublicationDayAnchor(t *testing.T) {
	month := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	_, observations, err := buildRows([]rawMonthlyRecord{{
		"date":  []byte(`"2026-05"`),
		"pe10":  []byte(`39.58`),
		"sp500": []byte(`7259.22`),
	}}, map[string]monthAnchor{
		"2026-05": {StartMonth: month, FirstTS: time.Date(2026, 5, 12, 13, 30, 0, 0, time.UTC), LastTS: time.Date(2026, 5, 12, 19, 59, 0, 0, time.UTC), LastClose: 5800},
	}, "SPX")
	if err != nil {
		t.Fatalf("buildRows returned error: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("len(observations)=%d want 2", len(observations))
	}
	for _, row := range observations {
		if !row.KnownAt.Equal(time.Date(2026, 5, 12, 13, 30, 0, 0, time.UTC)) {
			t.Fatalf("KnownAt=%s want 2026-05-12 13:30:00Z", row.KnownAt)
		}
		if !row.EventTS.Equal(time.Date(2026, 5, 12, 19, 59, 0, 0, time.UTC)) {
			t.Fatalf("EventTS=%s want 2026-05-12 19:59:00Z", row.EventTS)
		}
		if row.FactorCode == "pe10" && row.AnchorValue != 5800 {
			t.Fatalf("pe10 AnchorValue=%v want 5800", row.AnchorValue)
		}
	}
}
