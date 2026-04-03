package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestParsePrimaryMarket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    marketSpec
		wantErr bool
	}{
		{name: "default crypto", input: "", want: marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed}},
		{name: "crypto alias", input: "crypto-underlying", want: marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed}},
		{name: "us", input: "us", want: marketSpec{name: marketUS, underlyingFeed: usUnderlyingFeed}},
		{name: "invalid", input: "hk", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePrimaryMarket(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parsePrimaryMarket(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePrimaryMarket(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parsePrimaryMarket(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseInstrumentScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    instrumentScope
		wantErr bool
	}{
		{name: "default auto", input: "", want: instrumentAuto},
		{name: "spot", input: "spot", want: instrumentSpot},
		{name: "options alias", input: "options", want: instrumentContract},
		{name: "mixed alias", input: "both", want: instrumentMixed},
		{name: "invalid", input: "futures", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseInstrumentScope(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseInstrumentScope(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseInstrumentScope(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("parseInstrumentScope(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidateInstrumentScope(t *testing.T) {
	t.Parallel()

	spotOnly := []strategies.ResolvedStrategy{{CanonicalName: "golden-cross", Profile: strategies.StrategyProfile{RegularTrade: strategies.RegularTradeMaterial}}}
	contractOnly := []strategies.ResolvedStrategy{{CanonicalName: "bull-put-spread", Profile: strategies.StrategyProfile{UsesOptions: true}}}
	mixed := append([]strategies.ResolvedStrategy{}, spotOnly...)
	mixed = append(mixed, contractOnly...)

	if err := validateInstrumentScope(instrumentSpot, spotOnly); err != nil {
		t.Fatalf("validateInstrumentScope(spot, spotOnly) unexpected error: %v", err)
	}
	if err := validateInstrumentScope(instrumentSpot, mixed); err == nil {
		t.Fatalf("validateInstrumentScope(spot, mixed) expected error, got nil")
	}
	if err := validateInstrumentScope(instrumentContract, contractOnly); err != nil {
		t.Fatalf("validateInstrumentScope(contract, contractOnly) unexpected error: %v", err)
	}
	if err := validateInstrumentScope(instrumentContract, mixed); err == nil {
		t.Fatalf("validateInstrumentScope(contract, mixed) expected error, got nil")
	}
}

func TestResolveCapitalProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		market     marketSpec
		profile    strategies.StrategyProfile
		asset      string
		wantUnit   string
		wantMode   string
		wantInNote string
	}{
		{
			name:       "crypto spot uses usd",
			market:     marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed},
			profile:    strategies.StrategyProfile{RegularTrade: strategies.RegularTradeMaterial},
			asset:      "BTC",
			wantUnit:   "USD",
			wantMode:   "usd",
			wantInNote: "USD",
		},
		{
			name:       "crypto contracts use asset unit",
			market:     marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed},
			profile:    strategies.StrategyProfile{UsesOptions: true},
			asset:      "ETH",
			wantUnit:   "ETH",
			wantMode:   "base_asset",
			wantInNote: "ETH",
		},
		{
			name:       "us contracts use usd",
			market:     marketSpec{name: marketUS, underlyingFeed: usUnderlyingFeed},
			profile:    strategies.StrategyProfile{UsesOptions: true},
			asset:      "AAPL",
			wantUnit:   "USD",
			wantMode:   "usd",
			wantInNote: "美股市场",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveCapitalProfile(tc.market, tc.profile, tc.asset)
			if got.unit != tc.wantUnit {
				t.Fatalf("resolveCapitalProfile().unit = %q, want %q", got.unit, tc.wantUnit)
			}
			if got.mode != tc.wantMode {
				t.Fatalf("resolveCapitalProfile().mode = %q, want %q", got.mode, tc.wantMode)
			}
			if !strings.Contains(got.note, tc.wantInNote) {
				t.Fatalf("resolveCapitalProfile().note = %q, want substring %q", got.note, tc.wantInNote)
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

func TestResolveHTMLTargetDir(t *testing.T) {
	t.Parallel()

	t.Run("default directory", func(t *testing.T) {
		t.Parallel()
		if got := resolveHTMLTargetDir(""); got != defaultBacktestHTMLDir {
			t.Fatalf("resolveHTMLTargetDir(\"\") = %q, want %q", got, defaultBacktestHTMLDir)
		}
	})

	t.Run("custom file path uses parent", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join("custom", "reports", "result.html")
		want := filepath.Join("custom", "reports")
		if got := resolveHTMLTargetDir(path); got != want {
			t.Fatalf("resolveHTMLTargetDir(%q) = %q, want %q", path, got, want)
		}
	})

	t.Run("bare filename uses cwd", func(t *testing.T) {
		t.Parallel()
		if got := resolveHTMLTargetDir("result.html"); got != "." {
			t.Fatalf("resolveHTMLTargetDir(result.html) = %q, want .", got)
		}
	})
}

func TestClearHTMLFiles(t *testing.T) {
	t.Parallel()

	t.Run("missing directory is ignored", func(t *testing.T) {
		t.Parallel()
		missing := filepath.Join(t.TempDir(), "missing")
		if err := clearHTMLFiles(missing); err != nil {
			t.Fatalf("clearHTMLFiles(%q) unexpected error: %v", missing, err)
		}
	})

	t.Run("removes only html files in target directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		htmlPath := filepath.Join(root, "report.html")
		htmlUpperPath := filepath.Join(root, "summary.HTML")
		jsonPath := filepath.Join(root, "result.json")
		nestedDir := filepath.Join(root, "nested")
		nestedHTMLPath := filepath.Join(nestedDir, "keep.html")

		if err := os.WriteFile(htmlPath, []byte("html"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) unexpected error: %v", htmlPath, err)
		}
		if err := os.WriteFile(htmlUpperPath, []byte("html"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) unexpected error: %v", htmlUpperPath, err)
		}
		if err := os.WriteFile(jsonPath, []byte("json"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) unexpected error: %v", jsonPath, err)
		}
		if err := os.MkdirAll(nestedDir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) unexpected error: %v", nestedDir, err)
		}
		if err := os.WriteFile(nestedHTMLPath, []byte("nested"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) unexpected error: %v", nestedHTMLPath, err)
		}

		if err := clearHTMLFiles(root); err != nil {
			t.Fatalf("clearHTMLFiles(%q) unexpected error: %v", root, err)
		}

		if _, err := os.Stat(htmlPath); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat err = %v", htmlPath, err)
		}
		if _, err := os.Stat(htmlUpperPath); !os.IsNotExist(err) {
			t.Fatalf("expected %q to be removed, stat err = %v", htmlUpperPath, err)
		}
		if _, err := os.Stat(jsonPath); err != nil {
			t.Fatalf("expected %q to remain, stat err = %v", jsonPath, err)
		}
		if _, err := os.Stat(nestedHTMLPath); err != nil {
			t.Fatalf("expected nested file %q to remain, stat err = %v", nestedHTMLPath, err)
		}
	})
}

func TestStrategiesNeedOptions(t *testing.T) {
	t.Parallel()

	items := []strategies.ResolvedStrategy{{Profile: strategies.StrategyProfile{RegularTrade: strategies.RegularTradeMaterial}}}
	if strategiesNeedOptions(items) {
		t.Fatalf("strategiesNeedOptions() = true, want false")
	}

	items = append(items, strategies.ResolvedStrategy{Profile: strategies.StrategyProfile{UsesOptions: true}})
	if !strategiesNeedOptions(items) {
		t.Fatalf("strategiesNeedOptions() = false, want true")
	}
}
