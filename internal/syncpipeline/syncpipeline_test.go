package syncpipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type stubSyncer struct {
	cursor     time.Time
	hasCursor  bool
	syncCalled bool
}

func (s *stubSyncer) Name() string { return "stub" }

func (s *stubSyncer) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return []string{SingletonSourceKey}, nil
}

func (s *stubSyncer) ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error) {
	return s.cursor, s.hasCursor, nil
}

func (s *stubSyncer) ColdStartFloor(string) time.Time { return time.Time{} }

func (s *stubSyncer) Sync(context.Context, driver.Conn, SyncRequest) (SyncResult, error) {
	s.syncCalled = true
	return SyncResult{}, nil
}

func (s *stubSyncer) AuditTargets(string) []AuditTarget { return nil }

func (s *stubSyncer) MaxConcurrency() int { return 1 }

func TestShouldSkipRecentCursorUsesCoverageEndOfDay(t *testing.T) {
	cursor := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	if !shouldSkipRecentCursor(cursor, time.Date(2026, 5, 19, 18, 0, 0, 0, time.UTC)) {
		t.Fatal("expected cursor within 20h of coverage end to be skipped")
	}
	if shouldSkipRecentCursor(cursor, time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)) {
		t.Fatal("expected cursor older than 20h past coverage end to sync")
	}
}

func TestRunSourceSkipsRecentCursorAtRunnerStart(t *testing.T) {
	syncer := &stubSyncer{cursor: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), hasCursor: true}
	runner := NewRunner(nil, RunnerOptions{})
	runner.startedAt = time.Date(2026, 5, 19, 18, 0, 0, 0, time.UTC)

	report := runner.runSource(context.Background(), JobSpec{Name: "polygon_us_flatfiles", Syncer: syncer}, nil, SingletonSourceKey)
	if report.Status != JobStatusSkipped {
		t.Fatalf("expected skipped status, got %s", report.Status)
	}
	if syncer.syncCalled {
		t.Fatal("expected sync not to be called when cursor is fresh")
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "skip recent sync") {
		t.Fatalf("expected skip note, got %#v", report.Notes)
	}
}

func TestRunSourceDoesNotSkipWhenFromOverrideProvided(t *testing.T) {
	syncer := &stubSyncer{cursor: time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC), hasCursor: true}
	runner := NewRunner(nil, RunnerOptions{FromOverride: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Force: true, DryRun: true})
	runner.startedAt = time.Date(2026, 5, 19, 18, 0, 0, 0, time.UTC)

	report := runner.runSource(context.Background(), JobSpec{Name: "polygon_us_flatfiles", Syncer: syncer}, nil, SingletonSourceKey)
	if report.Status != JobStatusSuccess {
		t.Fatalf("expected success status, got %s err=%s", report.Status, report.Err)
	}
	if !syncer.syncCalled {
		t.Fatal("expected sync to be called when from override is provided")
	}
}
