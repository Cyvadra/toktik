package backtest

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResultExportJSONSanitizesNaNAndInf(t *testing.T) {
	result := &Result{
		StrategyName:   "test",
		StartTime:      time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2023, time.January, 2, 0, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		TotalReturn:    math.NaN(),
		SharpeRatio:    math.Inf(1),
		EquityCurve:    []float64{100, math.NaN()},
		Timestamps: []time.Time{
			time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2023, time.January, 1, 3, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"close": {30000, math.NaN()},
		},
		TradeOverview: &TradeOverview{
			NetPnL: math.NaN(),
		},
		EquityAnalysis: &EquityAnalysis{
			PeakEquity:          101,
			BestBarReturn:       math.Inf(-1),
			BarReturnVolatility: math.NaN(),
		},
		SpreadSummary: &SpreadSummary{
			TotalPnL: math.NaN(),
		},
	}

	outputPath := filepath.Join(t.TempDir(), "result.json")
	if err := result.ExportJSON(outputPath); err != nil {
		t.Fatalf("ExportJSON() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(data)
	if strings.Contains(text, "NaN") || strings.Contains(text, "+Inf") || strings.Contains(text, "-Inf") {
		t.Fatalf("exported JSON still contains unsupported float literals: %s", text)
	}
	if !strings.Contains(text, "\"total_return\": null") {
		t.Fatalf("expected total_return NaN to be exported as null, got: %s", text)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded["total_return"] != nil {
		t.Fatalf("decoded total_return = %#v, want nil", decoded["total_return"])
	}

	curve, ok := decoded["equity_curve"].([]interface{})
	if !ok || len(curve) != 2 || curve[1] != nil {
		t.Fatalf("decoded equity_curve = %#v, want second element nil", decoded["equity_curve"])
	}

	series, ok := decoded["series"].(map[string]interface{})
	if !ok {
		t.Fatalf("decoded series = %#v, want object", decoded["series"])
	}
	closeSeries, ok := series["close"].([]interface{})
	if !ok || len(closeSeries) != 2 || closeSeries[1] != nil {
		t.Fatalf("decoded series.close = %#v, want second element nil", series["close"])
	}
	tradeOverview, ok := decoded["trade_overview"].(map[string]interface{})
	if !ok || tradeOverview["net_pnl"] != nil {
		t.Fatalf("decoded trade_overview.net_pnl = %#v, want nil", tradeOverview)
	}
}

func TestComputeResultNormalizesReportColumns(t *testing.T) {
	timestamps := []time.Time{
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
	}
	result := computeResult(
		"test",
		nil,
		[]float64{100, 101},
		timestamps,
		100,
		"BTC",
		map[string][]float64{
			"close": {60000, 60100},
			"atr":   {1000, 1005},
		},
		[]ReportColumn{
			{Source: "atr", Label: "ATR", Decimals: 2},
			{Source: "missing", Label: "Missing", Decimals: 2},
			{Source: "close", Decimals: -1},
		},
	)

	if len(result.ReportColumns) != 2 {
		t.Fatalf("len(result.ReportColumns) = %d, want 2", len(result.ReportColumns))
	}
	if result.ReportColumns[0].Source != "atr" || result.ReportColumns[0].Label != "ATR" || result.ReportColumns[0].Decimals != 2 {
		t.Fatalf("unexpected first report column: %#v", result.ReportColumns[0])
	}
	if result.ReportColumns[1].Source != "close" || result.ReportColumns[1].Label != "close" || result.ReportColumns[1].Decimals != 0 {
		t.Fatalf("unexpected second report column: %#v", result.ReportColumns[1])
	}
}
