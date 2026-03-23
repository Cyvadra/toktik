package report

import (
	"encoding/json"
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
	if payload[0].Label != "Donchian Upper" || len(payload[0].Values) != 2 {
		t.Fatalf("unexpected first hover column payload: %#v", payload[0])
	}
	if payload[1].Label != "ATR" || payload[1].Values[1].Value != 860 {
		t.Fatalf("unexpected second hover column payload: %#v", payload[1])
	}
}
