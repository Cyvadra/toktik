package report

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestBuildHTMLViewIncludesHoverColumns(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":                {60000, 60100},
			"high":                {60200, 60300},
			"low":                 {59900, 60050},
			"close":               {60150, 60250},
			"htf_dc20_upper_prev": {62000, 62100},
			"htf_atr20_prev":      {850, 860},
		},
		ReportColumns: []backtest.ReportColumn{
			{Source: "htf_dc20_upper_prev", Label: "Donchian Upper", Decimals: 2},
			{Source: "htf_atr20_prev", Label: "ATR", Decimals: 2},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasHoverColumns {
		t.Fatal("view.HasHoverColumns = false, want true")
	}

	var payload []hoverColumnPayload
	if err := json.Unmarshal([]byte(view.HoverColumnsData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(HoverColumnsData) error = %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("len(payload) = %d, want 2", len(payload))
	}
	if payload[0].Overlay {
		t.Fatalf("payload[0].Overlay = true, want false")
	}
	if payload[0].Label != "Donchian Upper" || len(payload[0].Values) != 2 {
		t.Fatalf("unexpected first hover column payload: %#v", payload[0])
	}
	if payload[1].Label != "ATR" || payload[1].Values[1].Value == nil || *payload[1].Values[1].Value != 860 {
		t.Fatalf("unexpected second hover column payload: %#v", payload[1])
	}
}

func TestBuildHTMLViewPreservesLeadingWhitespaceForFeatureSeries(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC),
		BarsCount:      3,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 100.5, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":    {60000, 60100, 60200},
			"high":    {60200, 60300, 60400},
			"low":     {59900, 60050, 60150},
			"close":   {60150, 60250, 60350},
			"feature": {math.NaN(), math.NaN(), 42},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "feature",
			Label:    "Feature",
			Decimals: 1,
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})

	var payload []hoverColumnPayload
	if err := json.Unmarshal([]byte(view.HoverColumnsData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(HoverColumnsData) error = %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("len(payload) = %d, want 1", len(payload))
	}
	if len(payload[0].Values) != len(result.Timestamps) {
		t.Fatalf("len(payload[0].Values) = %d, want %d", len(payload[0].Values), len(result.Timestamps))
	}
	if payload[0].Values[0].Value != nil || payload[0].Values[1].Value != nil {
		t.Fatalf("expected leading whitespace points, got %#v", payload[0].Values)
	}
	if payload[0].Values[2].Value == nil || *payload[0].Values[2].Value != 42 {
		t.Fatalf("unexpected final feature point: %#v", payload[0].Values[2])
	}
	if payload[0].Values[0].Time != result.Timestamps[0].Unix() || payload[0].Values[1].Time != result.Timestamps[1].Unix() {
		t.Fatalf("expected whitespace points to retain original timestamps, got %#v", payload[0].Values)
	}
}

func TestBuildHTMLViewIncludesOverlayColumns(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":     {60000, 60100},
			"high":     {60200, 60300},
			"low":      {59900, 60050},
			"close":    {60150, 60250},
			"ema_fast": {60100, 60200},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "ema_fast",
			Label:    "EMA 20",
			Decimals: 2,
			Overlay:  true,
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasHoverColumns {
		t.Fatal("view.HasHoverColumns = false, want true")
	}
	if view.HasFeatureColumns {
		t.Fatal("view.HasFeatureColumns = true, want false")
	}

	var payload []hoverColumnPayload
	if err := json.Unmarshal([]byte(view.HoverColumnsData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(HoverColumnsData) error = %v", err)
	}
	if len(payload) != 1 || !payload[0].Overlay {
		t.Fatalf("unexpected overlay payload: %#v", payload)
	}
}

func TestBuildHTMLViewIncludesCalmarRatio(t *testing.T) {
	result := &backtest.Result{
		StrategyName:         "calmar-view",
		StartTime:            time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:              time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
		InitialCapital:       100,
		FinalEquity:          110,
		AnnualizedReturn:     0.30,
		AnnualizedVolatility: 0.18,
		CalmarRatio:          2.5,
		SharpeRatio:          1.67,
		MaxDrawdown:          0.12,
		EquityCurve:          []float64{100, 105, 110},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
		},
	}
	view := buildHTMLView(result, HTMLMeta{Asset: "BTC", Interval: "1h"})
	if view.CalmarRatio != "2.50" {
		t.Fatalf("view.CalmarRatio = %q, want 2.50", view.CalmarRatio)
	}
	if view.AnnualizedVolatility == "" {
		t.Fatal("view.AnnualizedVolatility is empty, want populated")
	}

	infinite := buildHTMLView(&backtest.Result{StrategyName: "inf", CalmarRatio: math.Inf(1)}, HTMLMeta{})
	if infinite.CalmarRatio != "∞" {
		t.Fatalf("infinite.CalmarRatio = %q, want ∞", infinite.CalmarRatio)
	}
}

func TestBuildHTMLViewIncludesQuoteAndBuyHoldPerformance(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "quote-view",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC),
		InitialCapital: 1,
		FinalEquity:    1.08,
		AccountUnit:    "BTC",
		UnderlyingUnit: "BTC",
		EquityCurve:    []float64{1, 1.04, 1.08},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"close": {40000, 41000, 42000},
		},
	}
	backtest.ApplyDerivedPerformance(result)

	view := buildHTMLView(result, HTMLMeta{Asset: "BTC", Interval: "1h"})
	if !view.HasQuoteNetValue {
		t.Fatal("view.HasQuoteNetValue = false, want true")
	}
	if !view.HasBuyHoldPerformance {
		t.Fatal("view.HasBuyHoldPerformance = false, want true")
	}
	if !view.HasDailyQuoteNetValue {
		t.Fatal("view.HasDailyQuoteNetValue = false, want true for sub-daily result")
	}
	if !view.HasDailyAssetPnL {
		t.Fatal("view.HasDailyAssetPnL = false, want true")
	}
	if view.QuotePerformance.SharpeRatio == "" || view.BuyHoldPerformance.CalmarRatio == "" {
		t.Fatalf("quote/buyhold metric cards not populated: %#v %#v", view.QuotePerformance, view.BuyHoldPerformance)
	}
	if string(view.DailyQuoteNetValueSeriesData) == "[]" {
		t.Fatal("DailyQuoteNetValueSeriesData = [], want populated series")
	}
	if string(view.DailyAssetPnLSeriesData) == "[]" {
		t.Fatal("DailyAssetPnLSeriesData = [], want populated series")
	}
	if string(view.QuoteNetValueSeriesData) == "[]" {
		t.Fatal("QuoteNetValueSeriesData = [], want populated series")
	}
	if view.HasAssetPerformance != true {
		t.Fatal("view.HasAssetPerformance = false, want true")
	}
}

func TestBuildHTMLViewIncludesDailyAssetPnLSeries(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "asset-pnl-view",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC),
		InitialCapital: 100,
		FinalEquity:    182,
		AccountUnit:    "USD",
		UnderlyingUnit: "BTC",
		EquityCurve:    []float64{100, 110, 156, 182},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"close": {10, 11, 12, 14},
		},
	}

	view := buildHTMLView(result, HTMLMeta{Asset: "BTC", Interval: "1h"})
	if !view.HasDailyAssetPnL {
		t.Fatal("view.HasDailyAssetPnL = false, want true")
	}

	var series []chartLinePoint
	if err := json.Unmarshal([]byte(view.DailyAssetPnLSeriesData), &series); err != nil {
		t.Fatalf("json.Unmarshal(DailyAssetPnLSeriesData) error = %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("len(series) = %d, want 2", len(series))
	}
	if series[0].Time != time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC).Unix() || series[0].Value == nil || math.Abs(*series[0].Value-0) > 1e-9 {
		t.Fatalf("series[0] = %#v, want day-1 EOD asset pnl of 0", series[0])
	}
	if series[1].Time != time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC).Unix() || series[1].Value == nil || math.Abs(*series[1].Value-3) > 1e-9 {
		t.Fatalf("series[1] = %#v, want day-2 EOD asset pnl of 3", series[1])
	}
	if !strings.Contains(view.DailyAssetPnLNote, "PnL 而非 equity") {
		t.Fatalf("DailyAssetPnLNote = %q, want note clarifying pnl vs equity", view.DailyAssetPnLNote)
	}
}

func TestBuildOverviewViewIncludesCalmarRatio(t *testing.T) {
	items := []OverviewItem{
		{
			Result: &backtest.Result{
				StrategyName:     "alpha",
				StartTime:        time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
				EndTime:          time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
				InitialCapital:   100,
				FinalEquity:      112,
				TotalReturn:      0.12,
				AnnualizedReturn: 0.30,
				SharpeRatio:      1.8,
				CalmarRatio:      3.0,
				MaxDrawdown:      0.10,
				EquityCurve:      []float64{100, 104, 112},
				Timestamps: []time.Time{
					time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
					time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
					time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
				},
			},
			HTMLPath: "/tmp/alpha.html",
		},
		{
			Result: &backtest.Result{
				StrategyName:     "beta",
				StartTime:        time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
				EndTime:          time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
				InitialCapital:   100,
				FinalEquity:      108,
				TotalReturn:      0.08,
				AnnualizedReturn: 0.20,
				SharpeRatio:      1.2,
				CalmarRatio:      4.0,
				MaxDrawdown:      0.05,
				EquityCurve:      []float64{100, 103, 108},
				Timestamps: []time.Time{
					time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
					time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
					time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
				},
			},
			HTMLPath: "/tmp/beta.html",
		},
	}

	view := buildOverviewView("/tmp/overview.html", items, HTMLMeta{Asset: "BTC", Interval: "1h"})
	if view.BestCalmar != "beta · 4.00" {
		t.Fatalf("view.BestCalmar = %q, want beta · 4.00", view.BestCalmar)
	}
	if len(view.Strategies) != 2 || view.Strategies[0].Calmar == "" {
		t.Fatalf("overview strategies missing calmar values: %#v", view.Strategies)
	}
}

func TestBuildHTMLViewIncludesSettledEquitySeries(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "settled-series",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC),
		BarsCount:      5,
		InitialCapital: 100,
		FinalEquity:    110,
		EquityCurve:    []float64{100, 106, 109, 105, 110},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 2, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 3, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {60000, 60100, 60200, 60300, 60400},
			"high":  {60200, 60300, 60400, 60500, 60600},
			"low":   {59900, 60050, 60100, 60200, 60300},
			"close": {60150, 60250, 60350, 60450, 60550},
		},
		Trades: []backtest.Trade{
			{
				ID:         1,
				Security:   backtest.SecurityRef{Market: "crypto", Symbol: "BTC", Interval: "1h"},
				Side:       backtest.Buy,
				Qty:        1,
				FillPrice:  10,
				Commission: 1,
				Timestamp:  time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:         2,
				Security:   backtest.SecurityRef{Market: "crypto", Symbol: "BTC", Interval: "1h"},
				Side:       backtest.Sell,
				Qty:        1,
				FillPrice:  15,
				Commission: 1,
				Timestamp:  time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC),
			},
		},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:          1,
			Status:      "closed",
			OpenTime:    time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			CloseTime:   ptrTime(time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC)),
			RealizedPnL: 7,
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})

	var settledSeries []chartLinePoint
	if err := json.Unmarshal([]byte(view.SettledEquitySeriesData), &settledSeries); err != nil {
		t.Fatalf("json.Unmarshal(SettledEquitySeriesData) error = %v", err)
	}
	if len(settledSeries) != 3 {
		t.Fatalf("len(settledSeries) = %d, want 3", len(settledSeries))
	}
	if settledSeries[0].Value == nil || *settledSeries[0].Value != 100 {
		t.Fatalf("settledSeries[0] = %#v, want initial capital point", settledSeries[0])
	}
	if settledSeries[1].Time != time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC).Unix() || settledSeries[1].Value == nil || *settledSeries[1].Value != 107 {
		t.Fatalf("settledSeries[1] = %#v, want spread-settled point", settledSeries[1])
	}
	if settledSeries[2].Time != time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC).Unix() || settledSeries[2].Value == nil || *settledSeries[2].Value != 110 {
		t.Fatalf("settledSeries[2] = %#v, want trade-settled point", settledSeries[2])
	}

	var floatingProfit []chartHistogramPoint
	if err := json.Unmarshal([]byte(view.SettledFloatingProfitData), &floatingProfit); err != nil {
		t.Fatalf("json.Unmarshal(SettledFloatingProfitData) error = %v", err)
	}
	if len(floatingProfit) != 1 {
		t.Fatalf("len(floatingProfit) = %d, want 1", len(floatingProfit))
	}
	if floatingProfit[0].Time != time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC).Unix() || floatingProfit[0].Value != 2 {
		t.Fatalf("floatingProfit[0] = %#v, want floating gain context before final settlement", floatingProfit[0])
	}

	var floatingLoss []chartHistogramPoint
	if err := json.Unmarshal([]byte(view.SettledFloatingLossData), &floatingLoss); err != nil {
		t.Fatalf("json.Unmarshal(SettledFloatingLossData) error = %v", err)
	}
	if len(floatingLoss) != 1 {
		t.Fatalf("len(floatingLoss) = %d, want 1", len(floatingLoss))
	}
	if floatingLoss[0].Time != time.Date(2024, time.January, 1, 4, 0, 0, 0, time.UTC).Unix() || floatingLoss[0].Value != -2 {
		t.Fatalf("floatingLoss[0] = %#v, want floating loss context before final settlement", floatingLoss[0])
	}

	var exposure []chartLinePoint
	if err := json.Unmarshal([]byte(view.SettledExposureData), &exposure); err != nil {
		t.Fatalf("json.Unmarshal(SettledExposureData) error = %v", err)
	}
	if len(exposure) != 5 {
		t.Fatalf("len(exposure) = %d, want 5", len(exposure))
	}
	wantExposure := []float64{2, 2, 1, 1, 0}
	for index, want := range wantExposure {
		if exposure[index].Value == nil || *exposure[index].Value != want {
			t.Fatalf("exposure[%d] = %#v, want %.0f", index, exposure[index], want)
		}
	}
}

func TestBuildHTMLViewIncludesUnderlyingVolumeHistogram(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":   {60000, 60200},
			"high":   {60300, 60400},
			"low":    {59900, 60100},
			"close":  {60200, 60150},
			"volume": {1234, 4567},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasUnderlyingVolume {
		t.Fatal("view.HasUnderlyingVolume = false, want true")
	}
	if view.UnderlyingVolumeLabel != "成交量" {
		t.Fatalf("view.UnderlyingVolumeLabel = %q, want %q", view.UnderlyingVolumeLabel, "成交量")
	}

	var payload []chartHistogramPoint
	if err := json.Unmarshal([]byte(view.UnderlyingVolumeData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(UnderlyingVolumeData) error = %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("len(payload) = %d, want 2", len(payload))
	}
	if payload[0].Value != 1234 || payload[0].Color != "rgba(34,197,94,0.52)" {
		t.Fatalf("unexpected first volume bar payload: %#v", payload[0])
	}
	if payload[1].Value != 4567 || payload[1].Color != "rgba(249,115,22,0.52)" {
		t.Fatalf("unexpected second volume bar payload: %#v", payload[1])
	}
}

func TestBuildHTMLViewNotesCompatibilityFallbackAndMissingVolume(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":            {60000, 60100},
			"high":            {60200, 60300},
			"low":             {59900, 60050},
			"close":           {60150, 60250},
			"compat_fallback": {1, 1},
			"volume":          {math.NaN(), math.NaN()},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.Notes) < 2 {
		t.Fatalf("len(view.Notes) = %d, want at least 2", len(view.Notes))
	}

	joined := strings.Join(view.Notes, "\n")
	if !strings.Contains(joined, "兼容性回退市场数据源") {
		t.Fatalf("expected compatibility fallback note, got %q", joined)
	}
	if !strings.Contains(joined, "没有可用的原生成交量序列") {
		t.Fatalf("expected missing volume note, got %q", joined)
	}
}

func TestBuildHTMLViewSkipsMissingVolumeNoteWhenVolumeExists(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":   {60000, 60100},
			"high":   {60200, 60300},
			"low":    {59900, 60050},
			"close":  {60150, 60250},
			"volume": {123, 456},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	joined := strings.Join(view.Notes, "\n")
	if strings.Contains(joined, "没有可用的原生成交量序列") {
		t.Fatalf("did not expect missing volume note, got %q", joined)
	}
}

func TestWriteBacktestHTMLIncludesTimezoneControls(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.March, 31, 12, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.March, 31, 12, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "id=\"timezone-select\"") {
		t.Fatalf("expected generated html to include timezone selector")
	}
	if !strings.Contains(html, "detectDefaultTimeZoneMode") {
		t.Fatalf("expected generated html to detect default timezone mode")
	}
	if !strings.Contains(html, "rewriteVisibleDateText") {
		t.Fatalf("expected generated html to rewrite visible UTC date strings")
	}
	if !strings.Contains(html, "formatDateTimeForMode") {
		t.Fatalf("expected generated html to include timezone-aware datetime formatter")
	}
	if !strings.Contains(html, "applyTimeZoneMode(detectDefaultTimeZoneMode());") {
		t.Fatalf("expected generated html to align timezone display to the browser by default")
	}
}

func TestWriteBacktestHTMLIncludesDailyAssetPnLChart(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "asset-pnl-html",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC),
		InitialCapital: 100,
		FinalEquity:    182,
		AccountUnit:    "USD",
		UnderlyingUnit: "BTC",
		EquityCurve:    []float64{100, 110, 156, 182},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {10, 10.5, 11.5, 13},
			"high":  {10.2, 11.1, 12.2, 14.2},
			"low":   {9.8, 10.2, 11.2, 12.8},
			"close": {10, 11, 12, 14},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{Asset: "BTC", Interval: "1h"}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "asset-daily-pnl-chart") {
		t.Fatalf("expected generated html to include daily asset pnl chart container")
	}
	if !strings.Contains(html, "dailyAssetPnLSeries") {
		t.Fatalf("expected generated html to include daily asset pnl series payload")
	}
	if !strings.Contains(html, "1 Day Asset 本位 PnL") {
		t.Fatalf("expected generated html to include daily asset pnl section title")
	}
}

func TestWriteBacktestHTMLKeepsDailyAssetPnLChartIndependent(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "asset-pnl-independent",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 3, 12, 0, 0, 0, time.UTC),
		InitialCapital: 100,
		FinalEquity:    182,
		AccountUnit:    "USD",
		UnderlyingUnit: "BTC",
		EquityCurve:    []float64{100, 110, 156, 182},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 12, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 12, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 3, 12, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {10, 10.5, 11.5, 13},
			"high":  {10.2, 11.1, 12.2, 14.2},
			"low":   {9.8, 10.2, 11.2, 12.8},
			"close": {10, 11, 12, 14},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{Asset: "BTC", Interval: "1h"}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "var syncedCharts = [];") {
		t.Fatalf("expected generated html to separate synced charts from standalone charts")
	}
	if !strings.Contains(html, "createChart('asset-daily-pnl-chart', 280, { handleScroll: false, handleScale: false })") {
		t.Fatalf("expected generated html to disable manual zoom for daily asset pnl chart")
	}
	if !strings.Contains(html, "addChart(apc, { sync: false });") {
		t.Fatalf("expected generated html to exclude daily asset pnl chart from linked chart sync")
	}
	if !strings.Contains(html, "applyChartFullRange(assetDailyPnLChart, [dailyAssetPnLSeries]);") {
		t.Fatalf("expected generated html to pin daily asset pnl chart to the full report range")
	}
	if !strings.Contains(html, "syncCharts(syncedCharts);") {
		t.Fatalf("expected generated html to synchronize only the linked chart set")
	}
}

func TestWriteBacktestHTMLIncludesSettledEquityToggle(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "data-equity-mode=\"settled\"") {
		t.Fatalf("expected generated html to include settled equity mode toggle")
	}
	if !strings.Contains(html, "settledEquitySeries") {
		t.Fatalf("expected generated html to include settled equity series payload")
	}
	if !strings.Contains(html, "renderEquitySeriesMode") {
		t.Fatalf("expected generated html to include settled equity rendering logic")
	}
	if !strings.Contains(html, "settled-context-chart") {
		t.Fatalf("expected generated html to include settled-mode context chart")
	}
	if !strings.Contains(html, "settledFloatingProfitSeries") {
		t.Fatalf("expected generated html to include settled floating profit payload")
	}
	if !strings.Contains(html, "fitChartsToVisibleData") {
		t.Fatalf("expected generated html to refit charts after time-axis changes")
	}
	if !strings.Contains(html, "subscribeVisibleTimeRangeChange") {
		t.Fatalf("expected generated html to synchronize charts by visible time range")
	}
	if strings.Contains(html, "subscribeVisibleLogicalRangeChange") {
		t.Fatalf("did not expect generated html to synchronize charts by logical range")
	}
}

func TestWriteBacktestHTMLIncludesHoverColumnSubplotControls(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":            {60000, 60100},
			"high":            {60200, 60300},
			"low":             {59900, 60050},
			"close":           {60150, 60250},
			"signal_strength": {0.25, 0.75},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "signal_strength",
			Label:    "Signal Strength",
			Decimals: 2,
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "underlying-feature-panel") {
		t.Fatalf("expected generated html to include hover column subplot panel")
	}
	if !strings.Contains(html, "data-hover-source") {
		t.Fatalf("expected generated html to include clickable hover column cards")
	}
	if !strings.Contains(html, "selectedHoverColumnSources") {
		t.Fatalf("expected generated html to include multi-select hover column subplot state")
	}
	if !strings.Contains(html, "preserveVisibleRanges") {
		t.Fatalf("expected generated html to preserve x-axis range during hover column updates")
	}
	if !strings.Contains(html, "priceScaleId: 'volume'") {
		t.Fatalf("expected generated html to merge volume histogram into the underlying chart")
	}
	if !strings.Contains(html, "feature-legend-value") {
		t.Fatalf("expected generated html to include live feature legend values")
	}
	if !strings.Contains(html, "featureChart.subscribeCrosshairMove") {
		t.Fatalf("expected generated html to sync subplot hover with the shared data window")
	}
}

func TestWriteBacktestHTMLIncludesOverlaySeriesSupport(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":     {60000, 60100},
			"high":     {60200, 60300},
			"low":      {59900, 60050},
			"close":    {60150, 60250},
			"ema_fast": {60100, 60200},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "ema_fast",
			Label:    "EMA 20",
			Decimals: 2,
			Overlay:  true,
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "var overlayPlots = new Map();") {
		t.Fatalf("expected generated html to include overlay plot state")
	}
	if !strings.Contains(html, "function renderOverlayPlots()") {
		t.Fatalf("expected generated html to include overlay rendering function")
	}
	if !strings.Contains(html, "column.overlay === true") {
		t.Fatalf("expected generated html to recognize overlay columns in payload")
	}
	if !strings.Contains(html, "叠加</div>") {
		t.Fatalf("expected generated html to label overlay cards in the data window")
	}
	if strings.Contains(html, "<div id=\"underlying-feature-panel\"") {
		t.Fatalf("did not expect subplot panel when all report columns are overlays")
	}
}

func TestWriteBacktestHTMLPlacesSpreadOpenTimeBesideHeaderStatus(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	triggerTime := time.Date(2024, time.January, 3, 4, 30, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 3, 5, 6, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-test",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    102,
		EquityCurve:    []float64{100, 102},
		Timestamps:     []time.Time{openTime, closeTime},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:               1,
			Tag:              "short call spread",
			Status:           "closed",
			OpenTime:         openTime,
			CloseTriggerTime: &triggerTime,
			CloseTime:        &closeTime,
			DaysHeld:         1.08,
			RealizedPnL:      12.34,
			Legs: []backtest.SpreadLegReport{{
				Symbol:           "BTC-20240105-50000-C",
				Side:             "sell",
				Type:             backtest.Call,
				StrikePrice:      50000,
				Expiration:       time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
				Delta:            0.25,
				Qty:              1,
				EntryPrice:       10,
				EntryTime:        openTime,
				Closed:           true,
				ClosePrice:       4,
				CloseTriggerTime: &triggerTime,
				CloseTime:        &closeTime,
				CloseReason:      "tp",
				RealizedPnL:      6,
			}},
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "下单 2024-01-02 03:04 UTC") {
		t.Fatalf("expected generated html to place spread open time in the card header")
	}
	if !strings.Contains(html, "平仓触发 2024-01-03 04:30 UTC") {
		t.Fatalf("expected generated html to show the close trigger time in the close card header")
	}
	if strings.Contains(html, "下单 2024-01-02 03:04 UTC</span></div><div class=\"flex gap-5 text-xs text-slate-400\"><span>盈亏") {
		t.Fatalf("did not expect close cards to keep showing the original order time in the header")
	}
	if !strings.Contains(html, "平仓触发时间") {
		t.Fatalf("expected generated html to relabel leg close time column to close trigger time when available")
	}
	if !strings.Contains(html, "定位到图表") {
		t.Fatalf("expected generated html to include spread-to-chart location button")
	}
	if !strings.Contains(html, fmt.Sprintf("data-chart-jump-time=\"%d\"", openTime.Unix())) {
		t.Fatalf("expected generated html to include chart jump timestamp for spread events")
	}
}

func TestBuildHTMLViewGroupsRelatedSpreads(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 9, 3, 4, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-group-test",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    104,
		EquityCurve:    []float64{100, 104},
		Timestamps:     []time.Time{openTime, closeTime},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadGroups: []backtest.SpreadGroupReport{{
			ID:          7,
			Tag:         "bull-call",
			SpreadIDs:   []int{11, 12},
			InitAmount:  2,
			DecayFactor: 0.8,
			RollCount:   1,
			TotalPnL:    6,
			Status:      "closed",
			OpenTime:    openTime,
			CloseTime:   &closeTime,
		}, {
			ID:          8,
			Tag:         "empty-group",
			InitAmount:  1,
			DecayFactor: 0.9,
			Status:      "closed",
			OpenTime:    openTime,
			CloseTime:   &closeTime,
		}},
		SpreadPositions: []backtest.SpreadPositionReport{
			{
				ID:          11,
				Tag:         "开仓|first",
				Status:      "closed",
				OpenTime:    openTime,
				CloseTime:   &closeTime,
				DaysHeld:    7,
				RealizedPnL: 2,
				GroupID:     7,
				Legs: []backtest.SpreadLegReport{{
					Symbol:      "BTC-20240105-50000-C",
					Side:        "buy",
					Type:        backtest.Call,
					StrikePrice: 50000,
					Expiration:  time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
					Delta:       0.33,
					Qty:         1,
					EntryPrice:  10,
					EntryTime:   openTime,
					Closed:      true,
					ClosePrice:  12,
					CloseTime:   &closeTime,
					RealizedPnL: 2,
				}},
			},
			{
				ID:          12,
				Tag:         "换仓|second",
				Status:      "open",
				OpenTime:    openTime.Add(24 * time.Hour),
				DaysHeld:    6,
				RealizedPnL: 4,
				GroupID:     7,
				Legs: []backtest.SpreadLegReport{{
					Symbol:      "BTC-20240112-52000-C",
					Side:        "sell",
					Type:        backtest.Call,
					StrikePrice: 52000,
					Expiration:  time.Date(2024, time.January, 12, 0, 0, 0, 0, time.UTC),
					Delta:       0.10,
					Qty:         1,
					EntryPrice:  6,
					EntryTime:   openTime.Add(24 * time.Hour),
					Closed:      false,
				}},
			},
			{
				ID:          13,
				Tag:         "standalone",
				Status:      "open",
				OpenTime:    openTime.Add(48 * time.Hour),
				DaysHeld:    5,
				RealizedPnL: 0,
				Legs: []backtest.SpreadLegReport{{
					Symbol:      "BTC-20240119-53000-P",
					Side:        "buy",
					Type:        backtest.Put,
					StrikePrice: 53000,
					Expiration:  time.Date(2024, time.January, 19, 0, 0, 0, 0, time.UTC),
					Delta:       -0.2,
					Qty:         1,
					EntryPrice:  8,
					EntryTime:   openTime.Add(48 * time.Hour),
					Closed:      false,
				}},
			},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.SpreadGroups) != 1 {
		t.Fatalf("len(view.SpreadGroups) = %d, want 1", len(view.SpreadGroups))
	}
	if view.SpreadGroups[0].ID != 7 {
		t.Fatalf("view.SpreadGroups[0].ID = %d, want 7", view.SpreadGroups[0].ID)
	}
	if view.SpreadGroups[0].SpreadCount != 2 {
		t.Fatalf("view.SpreadGroups[0].SpreadCount = %d, want 2", view.SpreadGroups[0].SpreadCount)
	}
	if len(view.SpreadGroups[0].Spreads) != 3 {
		t.Fatalf("len(view.SpreadGroups[0].Spreads) = %d, want 3", len(view.SpreadGroups[0].Spreads))
	}
	if len(view.UngroupedSpreads) != 1 {
		t.Fatalf("len(view.UngroupedSpreads) = %d, want 1", len(view.UngroupedSpreads))
	}
}

func TestBuildHTMLViewIncludesSpreadReportMetricSnapshots(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 0, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 2, 5, 0, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-metrics",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      3,
		InitialCapital: 100,
		FinalEquity:    103,
		EquityCurve:    []float64{100, 101, 103},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 2, 3, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 4, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 2, 5, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":     {70000, 70100, 70200},
			"high":     {70200, 70300, 70400},
			"low":      {69900, 70050, 70150},
			"close":    {70150, 70250, 70350},
			"ema_fast": {10, 11, 12},
			"score":    {0.5, 0.75, 1.25},
		},
		ReportColumns: []backtest.ReportColumn{
			{Source: "ema_fast", Label: "EMA Fast", Decimals: 0, Overlay: true},
			{Source: "score", Label: "Signal Score", Decimals: 2},
		},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:          1,
			Tag:         "metric-spread",
			Status:      "closed",
			OpenTime:    openTime,
			CloseTime:   &closeTime,
			DaysHeld:    0.08,
			RealizedPnL: 3,
			Legs: []backtest.SpreadLegReport{{
				Symbol:      "BTC-20240105-50000-C",
				Side:        "buy",
				Type:        backtest.Call,
				StrikePrice: 50000,
				Expiration:  time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
				Delta:       0.33,
				Qty:         1,
				EntryPrice:  10,
				EntryTime:   openTime,
				Closed:      true,
				ClosePrice:  13,
				CloseTime:   &closeTime,
				RealizedPnL: 3,
			}},
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.Spreads) != 2 {
		t.Fatalf("len(view.Spreads) = %d, want 2", len(view.Spreads))
	}
	if len(view.Spreads[0].ReportMetrics) != 2 {
		t.Fatalf("len(view.Spreads[0].ReportMetrics) = %d, want 2", len(view.Spreads[0].ReportMetrics))
	}
	if view.Spreads[0].ReportMetrics[0].Label != "EMA Fast" || view.Spreads[0].ReportMetrics[0].Value != "10" {
		t.Fatalf("unexpected open snapshot metric: %#v", view.Spreads[0].ReportMetrics[0])
	}
	if view.Spreads[0].ReportMetrics[0].KindLabel != "叠加" {
		t.Fatalf("view.Spreads[0].ReportMetrics[0].KindLabel = %q, want 叠加", view.Spreads[0].ReportMetrics[0].KindLabel)
	}
	if view.Spreads[1].ReportMetrics[1].Label != "Signal Score" || view.Spreads[1].ReportMetrics[1].Value != "1.25" {
		t.Fatalf("unexpected close snapshot metric: %#v", view.Spreads[1].ReportMetrics[1])
	}
	if view.Spreads[1].ReportMetrics[1].KindLabel != "子图" {
		t.Fatalf("view.Spreads[1].ReportMetrics[1].KindLabel = %q, want 子图", view.Spreads[1].ReportMetrics[1].KindLabel)
	}
}

func TestWriteBacktestHTMLIncludesSpreadGroupSections(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 9, 3, 4, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-group-html",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps:     []time.Time{openTime, closeTime},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadGroups: []backtest.SpreadGroupReport{{
			ID:            3,
			Tag:           "bull-call",
			SpreadIDs:     []int{1},
			InitAmount:    2,
			HighestEquity: 2.4,
			LowestEquity:  1.6,
			MaxDrawdown:   0.15,
			DecayFactor:   0.8,
			RollCount:     0,
			TotalPnL:      1,
			Status:        "closed",
			OpenTime:      openTime,
			CloseTime:     &closeTime,
		}},
		SpreadPositions: []backtest.SpreadPositionReport{
			{
				ID:          1,
				Tag:         "grouped-spread",
				Status:      "closed",
				OpenTime:    openTime,
				CloseTime:   &closeTime,
				DaysHeld:    7,
				RealizedPnL: 1,
				GroupID:     3,
				Legs: []backtest.SpreadLegReport{{
					Symbol:      "BTC-20240105-50000-C",
					Side:        "buy",
					Type:        backtest.Call,
					StrikePrice: 50000,
					Expiration:  time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
					Delta:       0.33,
					Qty:         1,
					EntryPrice:  10,
					EntryTime:   openTime,
					Closed:      true,
					ClosePrice:  11,
					CloseTime:   &closeTime,
					RealizedPnL: 1,
				}},
			},
			{
				ID:       2,
				Tag:      "standalone",
				Status:   "open",
				OpenTime: openTime.Add(24 * time.Hour),
				DaysHeld: 6,
				Legs: []backtest.SpreadLegReport{{
					Symbol:      "BTC-20240112-52000-P",
					Side:        "buy",
					Type:        backtest.Put,
					StrikePrice: 52000,
					Expiration:  time.Date(2024, time.January, 12, 0, 0, 0, 0, time.UTC),
					Delta:       -0.2,
					Qty:         1,
					EntryPrice:  8,
					EntryTime:   openTime.Add(24 * time.Hour),
					Closed:      false,
				}},
			},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "grouped-report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "组 #3 bull-call") {
		t.Fatalf("expected generated html to include spread group header")
	}
	if !strings.Contains(html, "最大回撤前 5 订单组") {
		t.Fatalf("expected generated html to include spread group drawdown section")
	}
	if !strings.Contains(html, "最高权益") || !strings.Contains(html, "最低权益") || !strings.Contains(html, "15.00%") {
		t.Fatalf("expected generated html to include spread group equity stats")
	}
	if !strings.Contains(html, "未分组持仓") {
		t.Fatalf("expected generated html to include ungrouped spread section")
	}
	if !strings.Contains(html, "grouped-spread") || !strings.Contains(html, "standalone") {
		t.Fatalf("expected generated html to include both grouped and ungrouped spread tags")
	}
}

func TestBuildHTMLViewRanksTopSpreadGroupDrawdowns(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-group-ranking",
		StartTime:      openTime,
		EndTime:        openTime.Add(6 * time.Hour),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		AccountUnit:    "USD",
		EquityCurve:    []float64{100, 101},
		Timestamps:     []time.Time{openTime, openTime.Add(6 * time.Hour)},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadGroups: []backtest.SpreadGroupReport{
			{ID: 1, Tag: "g1", MaxDrawdown: 0.05, HighestEquity: 110, LowestEquity: 100, TotalPnL: 3, Status: "closed"},
			{ID: 2, Tag: "g2", MaxDrawdown: 0.18, HighestEquity: 112, LowestEquity: 92, TotalPnL: -4, Status: "closed"},
			{ID: 3, Tag: "g3", MaxDrawdown: 0.12, HighestEquity: 108, LowestEquity: 95, TotalPnL: 1, Status: "open"},
			{ID: 4, Tag: "g4", MaxDrawdown: 0.22, HighestEquity: 115, LowestEquity: 88, TotalPnL: -8, Status: "closed"},
			{ID: 5, Tag: "g5", MaxDrawdown: 0.09, HighestEquity: 109, LowestEquity: 99, TotalPnL: 2, Status: "closed"},
			{ID: 6, Tag: "g6", MaxDrawdown: 0.02, HighestEquity: 103, LowestEquity: 100, TotalPnL: 1, Status: "closed"},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.TopDrawdownGroups) != 5 {
		t.Fatalf("len(view.TopDrawdownGroups) = %d, want 5", len(view.TopDrawdownGroups))
	}
	gotOrder := []int{
		view.TopDrawdownGroups[0].ID,
		view.TopDrawdownGroups[1].ID,
		view.TopDrawdownGroups[2].ID,
		view.TopDrawdownGroups[3].ID,
		view.TopDrawdownGroups[4].ID,
	}
	wantOrder := []int{4, 2, 3, 5, 1}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("TopDrawdownGroups order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestWriteBacktestHTMLIncludesSpreadReportMetricSnapshots(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 0, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 2, 5, 0, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-metric-html",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      3,
		InitialCapital: 100,
		FinalEquity:    102,
		EquityCurve:    []float64{100, 101, 102},
		Timestamps:     []time.Time{openTime, openTime.Add(time.Hour), closeTime},
		Series: map[string][]float64{
			"open":        {70000, 70100, 70200},
			"high":        {70200, 70300, 70400},
			"low":         {69900, 70050, 70150},
			"close":       {70150, 70250, 70350},
			"signal_edge": {1.11, 1.22, 1.33},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "signal_edge",
			Label:    "Signal Edge",
			Decimals: 2,
		}},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:          1,
			Tag:         "metric-spread",
			Status:      "closed",
			OpenTime:    openTime,
			CloseTime:   &closeTime,
			DaysHeld:    0.08,
			RealizedPnL: 2,
			Legs: []backtest.SpreadLegReport{{
				Symbol:      "BTC-20240105-50000-C",
				Side:        "buy",
				Type:        backtest.Call,
				StrikePrice: 50000,
				Expiration:  time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
				Delta:       0.33,
				Qty:         1,
				EntryPrice:  10,
				EntryTime:   openTime,
				Closed:      true,
				ClosePrice:  12,
				CloseTime:   &closeTime,
				RealizedPnL: 2,
			}},
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "spread-metric-report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "策略列快照") {
		t.Fatalf("expected generated html to include spread metric snapshot section")
	}
	if !strings.Contains(html, "Signal Edge") || !strings.Contains(html, "signal_edge") {
		t.Fatalf("expected generated html to include spread metric label and source")
	}
	if !strings.Contains(html, ">1.11<") || !strings.Contains(html, ">1.33<") {
		t.Fatalf("expected generated html to include open and close metric values")
	}
}

func TestBuildHTMLViewUsesEntryDeltaForSpreadSelectionDisplay(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	entryDelta := 0.19
	currentDelta := 0.01
	result := &backtest.Result{
		StrategyName:   "spread-entry-delta",
		StartTime:      openTime,
		EndTime:        openTime.Add(time.Hour),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    100,
		EquityCurve:    []float64{100, 100},
		Timestamps:     []time.Time{openTime, openTime.Add(time.Hour)},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:          1,
			Tag:         "开仓|A=0.0|B=4.0|d0.19/d0.03|n=150.38",
			Status:      "open",
			OpenTime:    openTime,
			DaysHeld:    1,
			RealizedPnL: 0,
			Legs: []backtest.SpreadLegReport{{
				Symbol:      "BTC-20240105-50000-C",
				Side:        "buy",
				Type:        backtest.Call,
				StrikePrice: 50000,
				Expiration:  openTime.Add(7 * 24 * time.Hour),
				EntryDelta:  &entryDelta,
				Delta:       currentDelta,
				Qty:         1,
				EntryPrice:  10,
				EntryTime:   openTime,
				Closed:      false,
			}},
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.UngroupedSpreads) != 1 {
		t.Fatalf("len(view.UngroupedSpreads) = %d, want 1", len(view.UngroupedSpreads))
	}
	if len(view.UngroupedSpreads[0].Legs) != 1 {
		t.Fatalf("len(view.UngroupedSpreads[0].Legs) = %d, want 1", len(view.UngroupedSpreads[0].Legs))
	}
	got := view.UngroupedSpreads[0].Legs[0].OpenSelect
	if got != "7.00 天 | Δ 0.19" {
		t.Fatalf("OpenSelect = %q, want %q", got, "7.00 天 | Δ 0.19")
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
