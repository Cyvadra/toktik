package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStrategyStoreListsAndSafelyLoadsStrategies(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"zeta.toktik":  `strategy("Zeta Strategy")`,
		"alpha.toktik": "//@version=6\nstrategy('Alpha Strategy')",
		"notes.txt":    "ignored",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := newStrategyStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	items := store.list()
	if len(items) != 2 || items[0].FileName != "alpha.toktik" || items[0].DisplayName != "Alpha Strategy" {
		t.Fatalf("unexpected strategies: %+v", items)
	}
	if _, ok := store.get("../alpha.toktik"); ok {
		t.Fatal("path traversal unexpectedly resolved")
	}
}
