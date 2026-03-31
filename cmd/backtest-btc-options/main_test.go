package main

import (
	"os"
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
