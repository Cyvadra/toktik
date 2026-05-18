package jobs

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestFMPForexSourceKeysUsesSymbolsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forex.txt")
	content := "# watchlist\nEURUSD\naudusd\nEURUSD\n\nUSDJPY\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write symbols file: %v", err)
	}

	syncer, err := NewFMPForex(FMPForexConfig{
		APIKey:           "test-key",
		SymbolsFile:      path,
		ResolveAtStartup: true,
		Interval:         fmp.Interval1Min,
	})
	if err != nil {
		t.Fatalf("NewFMPForex returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"AUDUSD", "EURUSD", "USDJPY"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
}

func TestFMPForexSourceKeysPrefersExplicitSymbols(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forex.txt")
	if err := os.WriteFile(path, []byte("EURUSD\nAUDUSD\n"), 0o644); err != nil {
		t.Fatalf("write symbols file: %v", err)
	}

	syncer, err := NewFMPForex(FMPForexConfig{
		APIKey:           "test-key",
		Symbols:          []string{"USDJPY", "EURUSD"},
		SymbolsFile:      path,
		ResolveAtStartup: true,
		Interval:         fmp.Interval1Min,
	})
	if err != nil {
		t.Fatalf("NewFMPForex returned error: %v", err)
	}

	keys, err := syncer.SourceKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("SourceKeys returned error: %v", err)
	}
	want := []string{"USDJPY", "EURUSD"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("SourceKeys = %#v, want %#v", keys, want)
	}
}
