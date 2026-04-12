package backtest

import (
	"encoding/csv"
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

func TestResultExportTradesCSVIncludesRegularAndOptionRows(t *testing.T) {
	entryTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	closeTime := entryTime.Add(6 * time.Hour)
	expiration := entryTime.Add(7 * 24 * time.Hour)
	closeDelta := -0.43219

	result := &Result{
		Trades: []Trade{{
			ID:         7,
			OrderID:    11,
			Security:   SecurityRef{Market: "crypto-underlying", Symbol: "BTCUSDT", Interval: "1h"},
			Side:       Buy,
			Note:       "  add   long  ",
			Qty:        1.234567,
			FillPrice:  62000.1234567,
			Commission: 0.1234567,
			Timestamp:  entryTime,
		}},
		SpreadPositions: []SpreadPositionReport{{
			ID:          3,
			Tag:         " credit   spread ",
			CloseNote:   " close all ",
			Status:      "closed",
			OpenTime:    entryTime,
			CloseTime:   &closeTime,
			DaysHeld:    0.25,
			NetPremium:  1.25,
			RealizedPnL: 5.5,
			GroupID:     2,
			Legs: []SpreadLegReport{{
				Symbol:      "BTC-28JUN24-65000-C",
				Side:        "sell",
				Type:        Call,
				StrikePrice: 65000,
				Expiration:  expiration,
				Delta:       0.123456,
				Qty:         1.234567,
				EntryPrice:  10.1234567,
				EntryTime:   entryTime,
				Closed:      true,
				ClosePrice:  4.1234567,
				CloseTime:   &closeTime,
				CloseDelta:  &closeDelta,
				CloseReason: " take   profit ",
				RealizedPnL: 6.1234567,
			}},
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "trades.csv")
	if err := result.ExportTradesCSV(outputPath); err != nil {
		t.Fatalf("ExportTradesCSV() error = %v", err)
	}

	f, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	if got := strings.Join(rows[0], ","); got != strings.Join(tradeCSVHeader, ",") {
		t.Fatalf("header = %q, want %q", got, strings.Join(tradeCSVHeader, ","))
	}
	if got := rows[1][1]; got != "trade" {
		t.Fatalf("rows[1][1] = %q, want trade", got)
	}
	if got := rows[1][5]; got != "BTCUSDT" {
		t.Fatalf("rows[1][5] = %q, want BTCUSDT", got)
	}
	if got := rows[1][8]; got != "1.2346" {
		t.Fatalf("rows[1][8] = %q, want 1.2346", got)
	}
	if got := rows[1][9]; got != "62000.123457" {
		t.Fatalf("rows[1][9] = %q, want 62000.123457", got)
	}
	if got := rows[1][10]; got != "0.123457" {
		t.Fatalf("rows[1][10] = %q, want 0.123457", got)
	}
	if got := rows[1][16]; got != "add long" {
		t.Fatalf("rows[1][16] = %q, want add long", got)
	}
	if got := rows[2][1]; got != "option_open" {
		t.Fatalf("rows[2][1] = %q, want option_open", got)
	}
	if got := rows[2][3]; got != "2" {
		t.Fatalf("rows[2][3] = %q, want 2", got)
	}
	if got := rows[2][8]; got != "1.2346" {
		t.Fatalf("rows[2][8] = %q, want 1.2346", got)
	}
	if got := rows[2][9]; got != "10.123457" {
		t.Fatalf("rows[2][9] = %q, want 10.123457", got)
	}
	if got := rows[2][12]; got != "0.1235" {
		t.Fatalf("rows[2][12] = %q, want 0.1235", got)
	}
	if got := rows[2][16]; got != "credit spread" {
		t.Fatalf("rows[2][16] = %q, want credit spread", got)
	}
	if got := rows[3][1]; got != "option_close" {
		t.Fatalf("rows[3][1] = %q, want option_close", got)
	}
	if got := rows[3][6]; got != "buy" {
		t.Fatalf("rows[3][6] = %q, want buy", got)
	}
	if got := rows[3][11]; got != "6.123457" {
		t.Fatalf("rows[3][11] = %q, want 6.123457", got)
	}
	if got := rows[3][12]; got != "-0.4322" {
		t.Fatalf("rows[3][12] = %q, want -0.4322", got)
	}
	if got := rows[3][16]; got != "take profit" {
		t.Fatalf("rows[3][16] = %q, want take profit", got)
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
			{Source: "atr", Label: "ATR", Decimals: 2, Overlay: true},
			{Source: "missing", Label: "Missing", Decimals: 2},
			{Source: "close", Decimals: -1},
		},
	)

	if len(result.ReportColumns) != 2 {
		t.Fatalf("len(result.ReportColumns) = %d, want 2", len(result.ReportColumns))
	}
	if result.ReportColumns[0].Source != "atr" || result.ReportColumns[0].Label != "ATR" || result.ReportColumns[0].Decimals != 2 || !result.ReportColumns[0].Overlay {
		t.Fatalf("unexpected first report column: %#v", result.ReportColumns[0])
	}
	if result.ReportColumns[1].Source != "close" || result.ReportColumns[1].Label != "close" || result.ReportColumns[1].Decimals != 0 || result.ReportColumns[1].Overlay {
		t.Fatalf("unexpected second report column: %#v", result.ReportColumns[1])
	}
}

func TestBuildReportSeriesIncludesFactorColumns(t *testing.T) {
	primary := map[string][]float64{
		"close": {60000, 60100},
		"atr":   {1000, 1005},
	}
	factors := []factorRegistration{{
		ref: FactorRef{Name: "dvol", Interval: "1d", Index: 0},
	}}
	factorColumns := []map[string][]float64{{
		"close":      {50, 51},
		"dvol":       {50, 51},
		"dvol_pr_90": {70, 71},
	}}

	alignMaps := [][]int{nil}
	series := buildReportSeries(primary, 2, factorColumns, alignMaps, factors)

	if got := series["close"]; len(got) != 2 || got[0] != 60000 || got[1] != 60100 {
		t.Fatalf("series[close] = %#v, want primary close series", got)
	}
	if got := series["dvol"]; len(got) != 2 || got[0] != 50 || got[1] != 51 {
		t.Fatalf("series[dvol] = %#v, want factor alias series", got)
	}
	if got := series[factorSeriesKey(factors[0].ref, "dvol_pr_90")]; len(got) != 2 || got[0] != 70 || got[1] != 71 {
		t.Fatalf("series[namespaced factor key] = %#v, want namespaced factor series", got)
	}
	if got := series[factorSeriesKey(factors[0].ref, "close")]; len(got) != 2 || got[0] != 50 || got[1] != 51 {
		t.Fatalf("series[namespaced factor close] = %#v, want namespaced factor close series", got)
	}
}

func TestBuildReportSeriesAlignsFactorColumnsToPrimaryBars(t *testing.T) {
	primary := map[string][]float64{
		"close": {60000, 60100, 60200, 60300},
	}
	factors := []factorRegistration{{
		ref: FactorRef{Name: "dvol", Interval: "1d", Index: 0},
	}}
	factorColumns := []map[string][]float64{{
		"close": {50, 51},
	}}
	alignMaps := [][]int{{0, 0, 1, 1}}

	series := buildReportSeries(primary, 4, factorColumns, alignMaps, factors)

	if got := series[factorSeriesKey(factors[0].ref, "close")]; len(got) != 4 || got[0] != 50 || got[1] != 50 || got[2] != 51 || got[3] != 51 {
		t.Fatalf("series[namespaced factor close] = %#v, want aligned factor series", got)
	}
}

func TestBuildSpreadPositionReportsIncludesCloseNote(t *testing.T) {
	tracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	closeTime := openTime.Add(48 * time.Hour)
	contract := OptionContract{
		Symbol:      "BTC-OPT-C-100",
		Type:        Call,
		StrikePrice: 100,
		Expiration:  openTime.Add(7 * 24 * time.Hour),
		Delta:       0.3,
	}
	spreadID := tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Sell,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  openTime,
	}}, openTime, 0, "首仓开仓")

	if !tracker.CloseLegWithReason(spreadID, 0, 2, closeTime, "到期平仓") {
		t.Fatal("CloseLegWithReason() = false, want true")
	}

	reports := buildSpreadPositionReports(tracker, closeTime)
	if len(reports) != 1 {
		t.Fatalf("len(reports) = %d, want 1", len(reports))
	}
	if reports[0].Tag != "首仓开仓" {
		t.Fatalf("reports[0].Tag = %q, want %q", reports[0].Tag, "首仓开仓")
	}
	if reports[0].CloseNote != "到期平仓" {
		t.Fatalf("reports[0].CloseNote = %q, want %q", reports[0].CloseNote, "到期平仓")
	}
}

func TestBuildSpreadGroupReportsSkipsEmptyGroups(t *testing.T) {
	groupTracker := NewSpreadGroupTracker()
	spreadTracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)

	emptyGroupID := groupTracker.Open("retracement-ratio-protective-spread|trend", 2, 0.9, openTime)
	groupTracker.Close(emptyGroupID)

	nonEmptyGroupID := groupTracker.Open("retracement-ratio-protective-spread|ambush", 5, 1, openTime)
	spreadID := spreadTracker.OpenFull([]SpreadLeg{{
		Contract: OptionContract{
			Symbol:      "BTC-OPT-C-100",
			Type:        Call,
			StrikePrice: 100,
			Expiration:  openTime.Add(7 * 24 * time.Hour),
			Delta:       0.3,
		},
		Side:       Sell,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  openTime,
	}}, openTime, 0, "首仓开仓", "", nonEmptyGroupID)
	groupTracker.AddSpread(nonEmptyGroupID, spreadID)

	reports := buildSpreadGroupReports(groupTracker, spreadTracker, openTime.Add(24*time.Hour))
	if len(reports) != 1 {
		t.Fatalf("len(reports) = %d, want 1", len(reports))
	}
	if reports[0].ID != nonEmptyGroupID {
		t.Fatalf("reports[0].ID = %d, want %d", reports[0].ID, nonEmptyGroupID)
	}
}

func TestSpreadGroupEquityAccumulatorTracksHighLowAndDrawdown(t *testing.T) {
	groupTracker := NewSpreadGroupTracker()
	spreadTracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	groupID := groupTracker.Open("drawdown-group", 100, 1, openTime)
	spreadID := spreadTracker.OpenFull([]SpreadLeg{{
		Contract: OptionContract{
			Symbol:      "BTC-OPT-C-100",
			Type:        Call,
			StrikePrice: 100,
			Expiration:  openTime.Add(7 * 24 * time.Hour),
			MarkPrice:   10,
		},
		Side:       Buy,
		Qty:        1,
		EntryPrice: 10,
		EntryTime:  openTime,
	}}, openTime, 0, "首仓开仓", "", groupID)
	groupTracker.AddSpread(groupID, spreadID)

	accumulator := newSpreadGroupEquityAccumulator()
	pricing := DefaultSpreadPricingConfig()

	accumulator.Observe(groupTracker, spreadTracker, map[string]OptionContract{
		"BTC-OPT-C-100": {Symbol: "BTC-OPT-C-100", MarkPrice: 10},
	}, pricing, openTime)
	accumulator.Observe(groupTracker, spreadTracker, map[string]OptionContract{
		"BTC-OPT-C-100": {Symbol: "BTC-OPT-C-100", MarkPrice: 15},
	}, pricing, openTime.Add(time.Hour))
	accumulator.Observe(groupTracker, spreadTracker, map[string]OptionContract{
		"BTC-OPT-C-100": {Symbol: "BTC-OPT-C-100", MarkPrice: 8},
	}, pricing, openTime.Add(2*time.Hour))

	snapshots := accumulator.Snapshot()
	snapshot, ok := snapshots[groupID]
	if !ok {
		t.Fatalf("Snapshot() missing group %d", groupID)
	}
	if snapshot.HighestEquity != 105 {
		t.Fatalf("snapshot.HighestEquity = %v, want 105", snapshot.HighestEquity)
	}
	if snapshot.LowestEquity != 98 {
		t.Fatalf("snapshot.LowestEquity = %v, want 98", snapshot.LowestEquity)
	}
	wantDrawdown := (105.0 - 98.0) / 105.0
	if math.Abs(snapshot.MaxDrawdown-wantDrawdown) > 1e-9 {
		t.Fatalf("snapshot.MaxDrawdown = %.12f, want %.12f", snapshot.MaxDrawdown, wantDrawdown)
	}

	reports := applySpreadGroupEquityStats([]SpreadGroupReport{{ID: groupID}}, snapshots)
	if reports[0].HighestEquity != 105 || reports[0].LowestEquity != 98 {
		t.Fatalf("applySpreadGroupEquityStats() = %#v", reports[0])
	}
}

func TestBuildSpreadPositionReportsIncludesCloseDelta(t *testing.T) {
	tracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	closeTime := openTime.Add(2 * time.Hour)
	contract := OptionContract{
		Symbol:      "BTC-OPT-C-100",
		Type:        Call,
		StrikePrice: 100,
		Expiration:  openTime.Add(7 * 24 * time.Hour),
		Delta:       0.3,
	}
	spreadID := tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Sell,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  openTime,
	}}, openTime, 0, "首仓开仓")

	closeDelta := "-0.4321"
	if !tracker.CloseLegWithReasonAndData(spreadID, 0, 2, closeTime, "止盈平仓", []TradeCustomData{{
		Key:   TradeCustomDataKeyCloseDelta,
		Value: closeDelta,
	}}) {
		t.Fatal("CloseLegWithReasonAndData() = false, want true")
	}

	reports := buildSpreadPositionReports(tracker, closeTime)
	if len(reports) != 1 || len(reports[0].Legs) != 1 {
		t.Fatalf("unexpected reports shape: %#v", reports)
	}
	if reports[0].Legs[0].CloseDelta == nil {
		t.Fatal("reports[0].Legs[0].CloseDelta = nil, want value")
	}
	if *reports[0].Legs[0].CloseDelta != -0.4321 {
		t.Fatalf("*reports[0].Legs[0].CloseDelta = %v, want %v", *reports[0].Legs[0].CloseDelta, -0.4321)
	}
}

func TestBuildSpreadPositionReportsIncludesEntryDeltaSnapshot(t *testing.T) {
	tracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	contract := OptionContract{
		Symbol:      "BTC-OPT-C-100",
		Type:        Call,
		StrikePrice: 100,
		Expiration:  openTime.Add(7 * 24 * time.Hour),
		Delta:       0.3,
	}
	spreadID := tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Sell,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  openTime,
		EntryCustomData: []TradeCustomData{{
			Key:   TradeCustomDataKeyEntryDelta,
			Value: "0.1900",
		}},
	}}, openTime, 0, "首仓开仓")

	spread := tracker.Get(spreadID)
	if spread == nil {
		t.Fatal("tracker.Get() = nil, want spread")
	}
	spread.Legs[0].Contract.Delta = 0.01

	reports := buildSpreadPositionReports(tracker, openTime.Add(time.Hour))
	if len(reports) != 1 || len(reports[0].Legs) != 1 {
		t.Fatalf("unexpected reports shape: %#v", reports)
	}
	if reports[0].Legs[0].EntryDelta == nil {
		t.Fatal("reports[0].Legs[0].EntryDelta = nil, want value")
	}
	if *reports[0].Legs[0].EntryDelta != 0.19 {
		t.Fatalf("*reports[0].Legs[0].EntryDelta = %v, want %v", *reports[0].Legs[0].EntryDelta, 0.19)
	}
	if reports[0].Legs[0].Delta != 0.01 {
		t.Fatalf("reports[0].Legs[0].Delta = %v, want %v", reports[0].Legs[0].Delta, 0.01)
	}
}

func TestBuildSpreadPositionReportsIncludesCloseTriggerTime(t *testing.T) {
	tracker := NewSpreadTracker()
	openTime := time.Date(2024, time.January, 3, 9, 0, 0, 0, time.UTC)
	triggerTime := openTime.Add(90 * time.Minute)
	closeTime := triggerTime.Add(30 * time.Minute)
	contract := OptionContract{
		Symbol:      "BTC-OPT-C-100",
		Type:        Call,
		StrikePrice: 100,
		Expiration:  openTime.Add(7 * 24 * time.Hour),
		Delta:       0.3,
	}
	spreadID := tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Sell,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  openTime,
	}}, openTime, 0, "首仓开仓")

	if !tracker.CloseLegWithReasonAndData(spreadID, 0, 2, closeTime, "换仓平仓", []TradeCustomData{{
		Key:   TradeCustomDataKeyCloseTriggerTime,
		Value: triggerTime.Format(time.RFC3339Nano),
	}}) {
		t.Fatal("CloseLegWithReasonAndData() = false, want true")
	}

	reports := buildSpreadPositionReports(tracker, closeTime)
	if len(reports) != 1 || len(reports[0].Legs) != 1 {
		t.Fatalf("unexpected reports shape: %#v", reports)
	}
	if reports[0].CloseTriggerTime == nil {
		t.Fatal("reports[0].CloseTriggerTime = nil, want value")
	}
	if !reports[0].CloseTriggerTime.Equal(triggerTime) {
		t.Fatalf("reports[0].CloseTriggerTime = %v, want %v", *reports[0].CloseTriggerTime, triggerTime)
	}
	if reports[0].Legs[0].CloseTriggerTime == nil {
		t.Fatal("reports[0].Legs[0].CloseTriggerTime = nil, want value")
	}
	if !reports[0].Legs[0].CloseTriggerTime.Equal(triggerTime) {
		t.Fatalf("reports[0].Legs[0].CloseTriggerTime = %v, want %v", *reports[0].Legs[0].CloseTriggerTime, triggerTime)
	}
}
