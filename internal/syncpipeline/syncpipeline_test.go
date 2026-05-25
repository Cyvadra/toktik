package syncpipeline

import (
	"context"
	"fmt"
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

func TestShouldAuditSourceIncludesPartialFailedWrites(t *testing.T) {
	if !shouldAuditSource(SourceReport{Status: JobStatusSuccess}) {
		t.Fatal("expected successful source to be audited")
	}
	if !shouldAuditSource(SourceReport{Status: JobStatusFailed, RowsInserted: 2}) {
		t.Fatal("expected failed source with inserted rows to be audited")
	}
	if shouldAuditSource(SourceReport{Status: JobStatusFailed}) {
		t.Fatal("expected failed source without inserted rows to be skipped")
	}
	if shouldAuditSource(SourceReport{Status: JobStatusSkipped}) {
		t.Fatal("expected skipped source to be skipped")
	}
}

func TestRunJobContinuesAfterSourceFailure(t *testing.T) {
	syncer := &sourceFailureSyncer{keys: []string{"A", "B", "C"}, failKey: "B"}
	runner := NewRunner(nil, RunnerOptions{DryRun: true, Force: true, FromOverride: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)})

	report := runner.runJob(context.Background(), JobSpec{Name: "job", Syncer: syncer})
	if report.Status != JobStatusFailed {
		t.Fatalf("expected failed job report, got %s", report.Status)
	}
	if len(report.Sources) != 3 {
		t.Fatalf("expected all sources to run, got %#v", report.Sources)
	}
	if got := strings.Join(syncer.called, ","); got != "A,B,C" {
		t.Fatalf("expected all sources called in order, got %q", got)
	}
}

func TestDependencyBlockedReportSkipsFailedDependency(t *testing.T) {
	report, blocked := dependencyBlockedReport(JobSpec{Name: "child", DependsOn: []string{"parent"}}, map[string]JobReport{
		"parent": {Job: "parent", Status: JobStatusFailed, Err: "boom"},
	})
	if !blocked {
		t.Fatal("expected child job to be blocked")
	}
	if report.Status != JobStatusSkipped {
		t.Fatalf("expected skipped status, got %s", report.Status)
	}
	if !strings.Contains(report.Err, "dependency parent failed: boom") {
		t.Fatalf("unexpected dependency error: %q", report.Err)
	}
}

func TestDependencyBlockedReportIgnoresUnselectedDependency(t *testing.T) {
	_, blocked := dependencyBlockedReport(JobSpec{Name: "child", DependsOn: []string{"parent"}}, map[string]JobReport{})
	if blocked {
		t.Fatal("expected missing dependency to stay compatible and not block")
	}
}

type sourceFailureSyncer struct {
	keys    []string
	failKey string
	called  []string
}

func (s *sourceFailureSyncer) Name() string { return "source-failure" }
func (s *sourceFailureSyncer) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	return s.keys, nil
}
func (s *sourceFailureSyncer) ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (s *sourceFailureSyncer) ColdStartFloor(string) time.Time {
	return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
}
func (s *sourceFailureSyncer) Sync(_ context.Context, _ driver.Conn, req SyncRequest) (SyncResult, error) {
	s.called = append(s.called, req.SourceKey)
	if req.SourceKey == s.failKey {
		return SyncResult{RowsInserted: 2}, fmt.Errorf("source failed")
	}
	return SyncResult{RowsInserted: 1}, nil
}
func (s *sourceFailureSyncer) AuditTargets(string) []AuditTarget { return nil }
func (s *sourceFailureSyncer) MaxConcurrency() int               { return 1 }
