package service

import (
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

// --- parseEnum / parse* helpers ---

func TestParseCommissionModel(t *testing.T) {
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
	for _, tt := range tests {
		got, err := parseCommissionModel(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCommissionModel(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseCommissionModel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseExecutionMode(t *testing.T) {
	tests := []struct {
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
	for _, tt := range tests {
		got, err := parseExecutionMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseExecutionMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseExecutionMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseValuationMode(t *testing.T) {
	tests := []struct {
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
	for _, tt := range tests {
		got, err := parseValuationMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseValuationMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseValuationMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseTriggerMode(t *testing.T) {
	tests := []struct {
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
	for _, tt := range tests {
		got, err := parseTriggerMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseTriggerMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("parseTriggerMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- validateBacktestRequest ---

func ptrFloat(v float64) *float64 { return &v }

func TestValidateBacktestRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.BacktestRequest
		wantErr bool
	}{
		{
			name:    "valid defaults",
			req:     dto.BacktestRequest{Symbol: "BTC-20250328-90000-C", Interval: "1m", From: "2025-01-01", To: "2025-03-01"},
			wantErr: false,
		},
		{
			name:    "valid with explicit params",
			req:     dto.BacktestRequest{Symbol: "BTC-20250328-90000-C", Interval: "1m", From: "2025-01-01", To: "2025-03-01", Capital: ptrFloat(10), CommissionValue: ptrFloat(0.01), SlippagePct: ptrFloat(0.001)},
			wantErr: false,
		},
		{
			name:    "negative capital",
			req:     dto.BacktestRequest{Capital: ptrFloat(-1)},
			wantErr: true,
		},
		{
			name:    "zero capital",
			req:     dto.BacktestRequest{Capital: ptrFloat(0)},
			wantErr: true,
		},
		{
			name:    "negative commission",
			req:     dto.BacktestRequest{CommissionValue: ptrFloat(-0.01)},
			wantErr: true,
		},
		{
			name:    "negative slippage",
			req:     dto.BacktestRequest{SlippagePct: ptrFloat(-0.01)},
			wantErr: true,
		},
		{
			name:    "slippage > 1",
			req:     dto.BacktestRequest{SlippagePct: ptrFloat(1.5)},
			wantErr: true,
		},
		{
			name:    "slippage exactly 1",
			req:     dto.BacktestRequest{SlippagePct: ptrFloat(1.0)},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBacktestRequest(tt.req)
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
