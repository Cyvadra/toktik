package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStrategyDirUsesWorkingDirectory(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("strategies", 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveStrategyDir("strategies")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "strategies" {
		t.Fatalf("resolved directory = %q", resolved)
	}
	_ = workingDir
}

func TestResolveStrategyDirAcceptsAbsolutePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-is-validated-by-store")
	resolved, err := resolveStrategyDir(dir)
	if err != nil || resolved != dir {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
}

func TestResolveAPIKeyPrefersExplicitValue(t *testing.T) {
	key, err := resolveAPIKey("from-environment", t.TempDir())
	if err != nil || key != "from-environment" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}

func TestResolveAPIKeyLoadsRepositoryRootFile(t *testing.T) {
	repositoryRoot := t.TempDir()
	strategyDir := filepath.Join(repositoryRoot, "pkg", "dsl", "scripts", "strategies")
	if err := os.MkdirAll(strategyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repositoryRoot, "toktik-api-key"), []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	key, err := resolveAPIKey("", strategyDir)
	if err != nil || key != "from-file" {
		t.Fatalf("key=%q err=%v", key, err)
	}
}
