package main

import (
	"path/filepath"
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

func TestParseCommissionModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    backtest.CommissionModel
		wantErr bool
	}{
		{name: "default none", input: "", want: backtest.CommissionNone},
		{name: "none", input: "none", want: backtest.CommissionNone},
		{name: "flat", input: "flat", want: backtest.CommissionFlat},
		{name: "percent", input: "percent", want: backtest.CommissionPercent},
		{name: "per-unit", input: "per-unit", want: backtest.CommissionPerUnit},
		{name: "perunit", input: "perunit", want: backtest.CommissionPerUnit},
		{name: "invalid", input: "fixed", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCommissionModel(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseCommissionModel(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommissionModel(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseCommissionModel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseTradeDirection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    strategies.TradeDirection
		wantErr bool
	}{
		{name: "both", input: "both", want: strategies.DirectionBoth},
		{name: "normalized uppercase", input: "LONG_ONLY", want: strategies.DirectionLongOnly},
		{name: "normalized spaces", input: " short_only ", want: strategies.DirectionShortOnly},
		{name: "invalid", input: "long", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTradeDirection(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseTradeDirection(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTradeDirection(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseTradeDirection(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestEnsureParentDir(t *testing.T) {
	t.Parallel()

	t.Run("no parent returns nil", func(t *testing.T) {
		t.Parallel()
		if err := ensureParentDir("out.json"); err != nil {
			t.Fatalf("ensureParentDir(out.json) unexpected error: %v", err)
		}
	})

	t.Run("creates nested dir", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		path := filepath.Join(root, "a", "b", "result.json")
		if err := ensureParentDir(path); err != nil {
			t.Fatalf("ensureParentDir(%q) unexpected error: %v", path, err)
		}
	})
}
