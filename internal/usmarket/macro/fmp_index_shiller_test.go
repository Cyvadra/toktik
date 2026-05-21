package macro

import (
	"testing"
	"time"
)

func TestFMPShillerDefaultsUseTruePE10Window(t *testing.T) {
	if defaultFMPRollingQuarters != 40 {
		t.Fatalf("defaultFMPRollingQuarters = %d, want 40", defaultFMPRollingQuarters)
	}
	if defaultFMPMinimumQuarters != 40 {
		t.Fatalf("defaultFMPMinimumQuarters = %d, want 40", defaultFMPMinimumQuarters)
	}
}

func TestBuildFMPIndexCatalogRowsIncludesTrailingPE(t *testing.T) {
	rows := buildFMPIndexCatalogRows(DefaultFMPNasdaq100Dataset, fmpMacroUniverses["nasdaq100"], "QQQ")
	for _, row := range rows {
		if row.FactorCode != "pe" {
			continue
		}
		if row.RealtimeMode != realtimePriceScaled {
			t.Fatalf("pe realtime_mode = %q, want %q", row.RealtimeMode, realtimePriceScaled)
		}
		return
	}
	t.Fatal("expected pe factor in catalog rows")
}

func TestBuildFMPIndexObservationRowsIncludesTrailingPE(t *testing.T) {
	month := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := buildFMPIndexObservationRows(
		DefaultFMPNasdaq100Dataset,
		[]fmpMonthlyPoint{{
			Month:           month,
			PeriodEnd:       month.AddDate(0, 1, 0),
			KnownAt:         month.AddDate(0, 1, 0),
			AnchorValue:     500,
			Price:           500,
			CPI:             320,
			RateGS10:        4.5,
			NominalEarnings: 15.625,
			PE:              32,
			RealSP:          500,
			RealEarnings:    15.625,
			PE10:            40,
			ExcessCAPEYield: -2,
		}},
		map[string]fmpMonthAnchor{"2026-01": {StartMonth: month, LastTS: month.AddDate(0, 1, 0).Add(-time.Second), FirstTS: month.AddDate(0, 1, 0), LastClose: 500}},
		"QQQ",
		"ndx",
	)
	for _, row := range rows {
		if row.FactorCode == "pe" && row.Value == 32 {
			return
		}
	}
	t.Fatalf("expected trailing pe observation row, got %#v", rows)
}

func TestBuildFMPMonthlyPointsPE10DoesNotUseFutureCPI(t *testing.T) {
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	prices := map[string]float64{
		"2020-01": 20,
		"2020-02": 20,
		"2020-03": 20,
		"2020-04": 20,
	}
	cpi := map[string]float64{
		"2020-01": 100,
		"2020-02": 125,
		"2020-03": 150,
		"2020-04": 200,
	}
	rate := map[string]float64{
		"2020-01": 4,
		"2020-02": 4,
		"2020-03": 4,
		"2020-04": 4,
	}
	memberships := map[string][]string{
		"2020-01": {"QQQ"},
		"2020-02": {"QQQ"},
		"2020-03": {"QQQ"},
		"2020-04": {"QQQ"},
	}
	symbols := map[string]fmpSymbolData{
		"QQQ": {
			Symbol: "QQQ",
			QuarterlyEarnings: []fmpQuarterlyEarningsRecord{
				{KnownAt: time.Date(2019, 2, 1, 0, 0, 0, 0, time.UTC), NetIncome: 10},
				{KnownAt: time.Date(2019, 5, 1, 0, 0, 0, 0, time.UTC), NetIncome: 10},
				{KnownAt: time.Date(2019, 8, 1, 0, 0, 0, 0, time.UTC), NetIncome: 10},
				{KnownAt: time.Date(2019, 11, 1, 0, 0, 0, 0, time.UTC), NetIncome: 10},
			},
			MonthMarketCap: map[string]float64{
				"2020-01": 80,
				"2020-02": 80,
				"2020-03": 80,
				"2020-04": 80,
			},
		},
	}

	points, err := buildFMPMonthlyPoints(from, to, prices, cpi, rate, nil, memberships, symbols, 1, 1)
	if err != nil {
		t.Fatalf("buildFMPMonthlyPoints returned error: %v", err)
	}
	if len(points) != 4 {
		t.Fatalf("len(points)=%d want 4", len(points))
	}
	if got := points[2].PE10; got < 1.62 || got > 1.63 {
		t.Fatalf("march pe10=%v want about 1.6216; future CPI leakage likely present", got)
	}
}

func TestParseFMPMacroKnownAtDoesNotFallbackToPeriodEnd(t *testing.T) {
	if got := parseFMPMacroKnownAt("", ""); !got.IsZero() {
		t.Fatalf("expected zero knownAt without accepted/filing dates, got %s", got)
	}
	got := parseFMPMacroKnownAt("", "2026-02-03")
	if got.IsZero() || got.UTC().Format("2006-01-02") != "2026-02-03" {
		t.Fatalf("expected filing date fallback, got %s", got)
	}
}
