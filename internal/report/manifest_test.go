package report

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestRunManifestRoundTrip(t *testing.T) {
	completedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	status := &dto.StrategyBacktestRunStatus{
		RunID:       "0123456789abcdef0123456789abcdef",
		Status:      "completed",
		Request:     dto.StrategyBacktestRunRequest{Asset: "BTC", DSL: "strategy(\"test\")"},
		CreatedAt:   completedAt.Add(-time.Minute),
		UpdatedAt:   completedAt,
		CompletedAt: &completedAt,
		Result: &dto.StrategyBacktestRunResult{Summaries: []dto.StrategyBacktestSummary{
			{StrategyName: "test", FinalEquity: 101},
		}},
	}
	manifest := NewRunManifest(status)
	if manifest.DSLSHA256 != DSLHash(status.Request.DSL) {
		t.Fatalf("DSLSHA256 = %q, want hash of DSL", manifest.DSLSHA256)
	}
	dir := t.TempDir()
	if err := WriteRunManifest(dir, manifest); err != nil {
		t.Fatalf("WriteRunManifest returned error: %v", err)
	}
	got, err := ReadRunManifest(dir)
	if err != nil {
		t.Fatalf("ReadRunManifest returned error: %v", err)
	}
	if got.Status.RunID != status.RunID || got.Status.Result.Summaries[0].FinalEquity != 101 {
		t.Fatalf("round-trip manifest = %+v", got)
	}
}

func TestReadRunManifestRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	status := &dto.StrategyBacktestRunStatus{RunID: "0123456789abcdef0123456789abcdef"}
	if err := WriteRunManifest(dir, RunManifest{Version: 2, Status: status}); err != nil {
		t.Fatalf("WriteRunManifest returned error: %v", err)
	}
	if _, err := ReadRunManifest(dir); err == nil {
		t.Fatal("expected unsupported manifest version error")
	}
}
