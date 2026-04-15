package service

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

// --- parseEnum ---

func testParseEnum[T comparable](t *testing.T, name string, mappings map[string]T, defaultVal T, fieldName string, tests []struct {
	input   string
	want    T
	wantErr bool
}) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		for _, tt := range tests {
			got, err := parseEnum(tt.input, mappings, defaultVal, fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseEnum(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				continue
			}
			if got != tt.want {
				t.Errorf("parseEnum(%q) = %v, want %v", tt.input, got, tt.want)
			}
		}
	})
}

func TestParseEnum(t *testing.T) {
	tests := []struct {
		input   string
		want    backtest.CommissionModel
		wantErr bool
	}{
		{"", backtest.CommissionPercent, false},
		{"percent", backtest.CommissionPercent, false},
		{"PERCENT", backtest.CommissionPercent, false},
		{" Percent ", backtest.CommissionPercent, false},
		{"none", backtest.CommissionNone, false},
		{"flat", backtest.CommissionFlat, false},
		{"per-unit", backtest.CommissionPerUnit, false},
		{"unknown", backtest.CommissionPercent, true},
	}
	testParseEnum(t, "commission model", commissionModelMap, backtest.CommissionPercent, "commission_model", tests)

	executionTests := []struct {
		input   string
		want    backtest.ExecutionPriceModel
		wantErr bool
	}{
		{"", backtest.ExecutionPriceBidAsk, false},
		{"bidask", backtest.ExecutionPriceBidAsk, false},
		{"canonical", backtest.ExecutionPriceCanonical, false},
		{"CANONICAL", backtest.ExecutionPriceCanonical, false},
		{"invalid", backtest.ExecutionPriceBidAsk, true},
	}
	testParseEnum(t, "execution mode", executionModeMap, backtest.ExecutionPriceBidAsk, "fill_mode", executionTests)

	valuationTests := []struct {
		input   string
		want    backtest.ValuationPriceModel
		wantErr bool
	}{
		{"", backtest.ValuationPriceExit, false},
		{"exit", backtest.ValuationPriceExit, false},
		{"close", backtest.ValuationPriceClose, false},
		{"mid", backtest.ValuationPriceMid, false},
		{"MID", backtest.ValuationPriceMid, false},
		{"bad", backtest.ValuationPriceExit, true},
	}
	testParseEnum(t, "valuation mode", valuationModeMap, backtest.ValuationPriceExit, "valuation_mode", valuationTests)

	triggerTests := []struct {
		input   string
		want    backtest.TriggerPriceMode
		wantErr bool
	}{
		{"", backtest.TriggerPriceCanonical, false},
		{"canonical", backtest.TriggerPriceCanonical, false},
		{"bidask-envelope", backtest.TriggerPriceBidAskEnvelope, false},
		{"BIDASK-ENVELOPE", backtest.TriggerPriceBidAskEnvelope, false},
		{"foo", backtest.TriggerPriceCanonical, true},
	}
	testParseEnum(t, "trigger mode", triggerModeMap, backtest.TriggerPriceCanonical, "trigger_mode", triggerTests)
}

// --- validateBacktestRequest ---

func ptrFloat(v float64) *float64 { return &v }

func TestValidateBacktestRequest(t *testing.T) {
	defaultFrom := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultTo := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		req     dto.BacktestRequest
		from    time.Time
		to      time.Time
		wantErr bool
	}{
		{
			name:    "valid defaults",
			req:     dto.BacktestRequest{Symbol: "BTC-20250328-90000-C", Interval: "1m", From: "2025-01-01", To: "2025-03-01"},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: false,
		},
		{
			name:    "valid with explicit params",
			req:     dto.BacktestRequest{Symbol: "BTC-20250328-90000-C", Interval: "1m", From: "2025-01-01", To: "2025-03-01", Capital: ptrFloat(10), CommissionValue: ptrFloat(0.01), SlippagePct: ptrFloat(0.001)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: false,
		},
		{
			name:    "empty symbol",
			req:     dto.BacktestRequest{},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "range exceeds 5 years",
			req:     dto.BacktestRequest{Symbol: "BTC-20250328-90000-C"},
			from:    time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC),
			to:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantErr: true,
		},
		{
			name:    "negative capital",
			req:     dto.BacktestRequest{Symbol: "BTC", Capital: ptrFloat(-1)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "zero capital",
			req:     dto.BacktestRequest{Symbol: "BTC", Capital: ptrFloat(0)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "negative commission",
			req:     dto.BacktestRequest{Symbol: "BTC", CommissionValue: ptrFloat(-0.01)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "negative slippage",
			req:     dto.BacktestRequest{Symbol: "BTC", SlippagePct: ptrFloat(-0.01)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "slippage > 1",
			req:     dto.BacktestRequest{Symbol: "BTC", SlippagePct: ptrFloat(1.5)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: true,
		},
		{
			name:    "slippage exactly 1",
			req:     dto.BacktestRequest{Symbol: "BTC", SlippagePct: ptrFloat(1.0)},
			from:    defaultFrom,
			to:      defaultTo,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBacktestRequest(tt.req, tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBacktestRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- floatDefault ---

func TestFloatDefault(t *testing.T) {
	if got := floatDefault(nil, 1.5); got != 1.5 {
		t.Errorf("floatDefault(nil, 1.5) = %v, want 1.5", got)
	}
	v := 2.5
	if got := floatDefault(&v, 1.5); got != 2.5 {
		t.Errorf("floatDefault(&2.5, 1.5) = %v, want 2.5", got)
	}
}

// --- resolveBacktestAccountUnit ---

func TestResolveBacktestAccountUnit(t *testing.T) {
	tests := []struct {
		symbol string
		want   string
	}{
		{"BTC-20250328-90000-C", "BTC"},
		{"ETH-20250630-3000-P", "ETH"},
		{"", ""},
	}
	for _, tt := range tests {
		got := resolveBacktestAccountUnit(tt.symbol)
		if got != tt.want {
			t.Errorf("resolveBacktestAccountUnit(%q) = %q, want %q", tt.symbol, got, tt.want)
		}
	}
}
