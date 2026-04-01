package usmarket

import (
	"testing"
	"time"
)

func TestParseOptionTicker(t *testing.T) {
	tests := []struct {
		ticker     string
		underlying string
		expDate    string
		optType    string
		strike     float64
		wantErr    bool
	}{
		{"O:AAPL230120C00130000", "AAPL", "2023-01-20", "C", 130.0, false},
		{"O:A230120C00135000", "A", "2023-01-20", "C", 135.0, false},
		{"O:AA230106P00038000", "AA", "2023-01-06", "P", 38.0, false},
		{"O:TSLA250321C00250000", "TSLA", "2025-03-21", "C", 250.0, false},
		{"O:SPY240119P00450000", "SPY", "2024-01-19", "P", 450.0, false},
		{"O:AAPL230217C00155000", "AAPL", "2023-02-17", "C", 155.0, false},
		// Fractional strike
		{"O:AAPL230120C00130500", "AAPL", "2023-01-20", "C", 130.5, false},
		// Invalid
		{"AAPL", "", "", "", 0, true},
		{"O:AAPL", "", "", "", 0, true},
		{"", "", "", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.ticker, func(t *testing.T) {
			underlying, exp, optType, strike, err := ParseOptionTicker(tt.ticker)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.ticker)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.ticker, err)
			}
			if underlying != tt.underlying {
				t.Errorf("underlying: got %q, want %q", underlying, tt.underlying)
			}
			wantExp, _ := time.Parse("2006-01-02", tt.expDate)
			if !exp.Equal(wantExp) {
				t.Errorf("expiration: got %v, want %v", exp, wantExp)
			}
			if optType != tt.optType {
				t.Errorf("optionType: got %q, want %q", optType, tt.optType)
			}
			if strike != tt.strike {
				t.Errorf("strike: got %f, want %f", strike, tt.strike)
			}
		})
	}
}

func TestOptionUnderlyingFallbackStockSymbol(t *testing.T) {
	tests := []struct {
		underlying string
		want       string
		ok         bool
	}{
		{"BRKB", "BRK.B", true},
		{"BFB", "BF.B", true},
		{"CWENA", "CWEN.A", true},
		{"UHALB", "UHAL.B", true},
		{"SPY", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.underlying, func(t *testing.T) {
			got, ok := OptionUnderlyingFallbackStockSymbol(tt.underlying)
			if ok != tt.ok {
				t.Fatalf("ok: got %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseNanosTimestamp(t *testing.T) {
	ts := parseNanosTimestamp("1672756200000000000")
	if ts.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
	// 2023-01-03 14:30:00 UTC
	expected := time.Date(2023, 1, 3, 14, 30, 0, 0, time.UTC)
	if !ts.Equal(expected) {
		t.Errorf("got %v, want %v", ts, expected)
	}
}
