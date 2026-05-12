package importledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeKeyDefaultsScope(t *testing.T) {
	importerName, sourceKey, scopeKey, err := normalizeKey(" crypto-options ", " file.parquet ", " ")
	if err != nil {
		t.Fatalf("normalizeKey returned error: %v", err)
	}
	if importerName != "crypto-options" || sourceKey != "file.parquet" || scopeKey != "default" {
		t.Fatalf("unexpected normalized key: %q %q %q", importerName, sourceKey, scopeKey)
	}
}

func TestNormalizeKeyRequiresImporterAndSource(t *testing.T) {
	if _, _, _, err := normalizeKey("", "source", "scope"); err == nil {
		t.Fatal("expected missing importer error")
	}
	if _, _, _, err := normalizeKey("importer", "", "scope"); err == nil {
		t.Fatal("expected missing source error")
	}
}

func TestNewImportIDLooksLikeUUID(t *testing.T) {
	importID, err := newImportID()
	if err != nil {
		t.Fatalf("newImportID returned error: %v", err)
	}
	if len(importID) != 36 || strings.Count(importID, "-") != 4 {
		t.Fatalf("unexpected import ID format: %q", importID)
	}
	if importID[14] != '4' {
		t.Fatalf("expected version 4 UUID, got %q", importID)
	}
}

func TestVersionFromTimeUsesUTCUnixNano(t *testing.T) {
	value := time.Date(2026, 5, 12, 1, 2, 3, 4, time.FixedZone("offset", 8*60*60))
	if got := versionFromTime(value); got != uint64(value.UTC().UnixNano()) {
		t.Fatalf("unexpected version: got %d want %d", got, value.UTC().UnixNano())
	}
}

func TestSourceHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("toktik"), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	hash, err := SourceHash(path)
	if err != nil {
		t.Fatalf("SourceHash returned error: %v", err)
	}
	if hash != "2a429be6ec30924041841720435b60c4c44c9e256ed74650b6f0294a427a1cc2" {
		t.Fatalf("unexpected hash: %s", hash)
	}
}

func TestNonNegativeRows(t *testing.T) {
	if got := NonNegativeRows(-2); got != 0 {
		t.Fatalf("NonNegativeRows(-2) = %d, want 0", got)
	}
	if got := NonNegativeRows(7); got != 7 {
		t.Fatalf("NonNegativeRows(7) = %d, want 7", got)
	}
}
