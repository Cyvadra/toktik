package syncpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/importledger"
)

const SingletonSourceKey = "_default"

const recentCursorSkipThreshold = 20 * time.Hour

const defaultLockTTL = 2 * time.Hour

const defaultFailureCleanupTimeout = 30 * time.Second

type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusFailed  JobStatus = "failed"
	JobStatusSkipped JobStatus = "skipped"
)

type DependencyMode string

const (
	// DependencyModePermissive preserves the experimental CLI behavior: jobs may
	// run even when declared dependencies were not selected for the current run.
	DependencyModePermissive DependencyMode = "permissive"
	DependencyModeStrict     DependencyMode = "strict"
)

func ParseDependencyMode(value string) (DependencyMode, error) {
	switch mode := DependencyMode(strings.ToLower(strings.TrimSpace(value))); mode {
	case "", DependencyModePermissive:
		return DependencyModePermissive, nil
	case DependencyModeStrict:
		return DependencyModeStrict, nil
	default:
		return "", fmt.Errorf("invalid dependency mode %q (permissive|strict)", value)
	}
}

type Syncer interface {
	Name() string
	SourceKeys(context.Context, driver.Conn) ([]string, error)
	ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error)
	ColdStartFloor(string) time.Time
	Sync(context.Context, driver.Conn, SyncRequest) (SyncResult, error)
	AuditTargets(string) []AuditTarget
	MaxConcurrency() int
}

type SyncRequest struct {
	SourceKey string
	From      time.Time
	To        time.Time
	DryRun    bool
}

type SyncResult struct {
	SourceKey    string
	From         time.Time
	To           time.Time
	RowsInserted int64
	Notes        []string
}

type AuditTarget struct {
	Table        string
	DateColumn   string
	KeyColumns   []string
	SourceFilter string
}

type JobSpec struct {
	Name             string
	Syncer           Syncer
	DependsOn        []string
	OverlapDays      int
	PerJobTimeout    time.Duration
	PerSourceTimeout time.Duration
}

type RunnerOptions struct {
	Logger               *slog.Logger
	Progress             ProgressReporter
	MaxSourceConcurrency int
	DependencyMode       DependencyMode
	DryRun               bool
	Force                bool
	FromOverride         time.Time
	ToOverride           time.Time
	LockOptions          LockOptions
	DBRetry              RetryOptions
	AuditEnabled         bool
	AuditOptions         AuditOptions
}

type ProgressReporter interface {
	StartJob(job string, totalSources int)
	SourceDone(job string, report SourceReport, completedSources int, totalSources int)
	FinishJob(job string, report JobReport)
}

type Runner struct {
	conn      driver.Conn
	opts      RunnerOptions
	startedAt time.Time
}

func NewRunner(conn driver.Conn, opts RunnerOptions) *Runner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.LockOptions.TTL <= 0 {
		opts.LockOptions.TTL = defaultLockTTL
	}
	if opts.DependencyMode == "" {
		opts.DependencyMode = DependencyModePermissive
	}
	return &Runner{conn: conn, opts: opts, startedAt: time.Now().UTC()}
}

type resolvedWindow struct {
	From       time.Time
	To         time.Time
	SkipReason string
}

type RunReport struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Jobs       []JobReport
}

type JobReport struct {
	Job           string
	Status        JobStatus
	RowsInserted  int64
	Sources       []SourceReport
	AuditFindings []DuplicateFinding
	Err           string
}

type SourceReport struct {
	SourceKey    string
	From         time.Time
	To           time.Time
	Status       JobStatus
	RowsInserted int64
	Err          string
	Notes        []string
}

type LockOptions struct {
	TTL         time.Duration
	ForceUnlock bool
}

type ClearedLock struct {
	ImporterName string
	SourceKey    string
	ScopeKey     string
	StartedAt    time.Time
}

type LedgerHooks struct {
	conn   driver.Conn
	opts   LockOptions
	retry  RetryOptions
	logger *slog.Logger
}

func NewLedgerHooks(conn driver.Conn, opts LockOptions) *LedgerHooks {
	return NewLedgerHooksWithRetry(conn, opts, RetryOptions{}, slog.Default())
}

func NewLedgerHooksWithRetry(conn driver.Conn, opts LockOptions, retry RetryOptions, logger *slog.Logger) *LedgerHooks {
	if opts.TTL <= 0 {
		opts.TTL = defaultLockTTL
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LedgerHooks{conn: conn, opts: opts, retry: retry, logger: logger}
}

func (h *LedgerHooks) ClearStaleLocks(ctx context.Context) ([]ClearedLock, error) {
	cutoff := time.Now().UTC().Add(-h.opts.TTL)
	rows, err := RetryValue(ctx, h.retry, h.logger, "import_ledger clear stale locks query", func(ctx context.Context) (driver.Rows, error) {
		return h.conn.Query(ctx, `SELECT importer_name, source_key, scope_key, started_at
FROM import_ledger FINAL
WHERE status = 'pending' AND started_at < parseDateTimeBestEffortOrNull({cutoff:String})`,
			clickhouse.Named("cutoff", cutoff.Format("2006-01-02 15:04:05")),
		)
	})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var locks []ClearedLock
	for rows.Next() {
		var lock ClearedLock
		if err := rows.Scan(&lock.ImporterName, &lock.SourceKey, &lock.ScopeKey, &lock.StartedAt); err != nil {
			return nil, err
		}
		locks = append(locks, lock)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var markErr error
	for _, lock := range locks {
		lock := lock
		if err := Retry(ctx, h.retry, h.logger, "import_ledger clear stale lock mark failed", func(ctx context.Context) error {
			return importledger.New(h.conn).MarkFailed(ctx, importledger.CompletionRequest{
				ImporterName: lock.ImporterName,
				SourceKey:    lock.SourceKey,
				ScopeKey:     lock.ScopeKey,
				ImportID:     "force-unlock",
				ErrorMessage: "force-unlock stale pending row",
				CompletedAt:  time.Now().UTC(),
			})
		}); err != nil {
			markErr = errors.Join(markErr, fmt.Errorf("mark stale lock failed for %s/%s/%s: %w", lock.ImporterName, lock.SourceKey, lock.ScopeKey, err))
		}
	}
	return locks, markErr
}

func (r *Runner) Run(ctx context.Context, specs []JobSpec) (RunReport, error) {
	started := time.Now().UTC()
	ordered, err := topoSort(specs)
	if err != nil {
		return RunReport{}, err
	}
	if err := r.retry(ctx, "import_ledger ensure schema", func(ctx context.Context) error {
		return importledger.EnsureSchema(ctx, r.conn)
	}); err != nil {
		return RunReport{}, err
	}
	r.opts.Logger.Info("sync pipeline started", "jobs", len(ordered), "dry_run", r.opts.DryRun, "force", r.opts.Force, "dependency_mode", r.opts.DependencyMode)
	report := RunReport{StartedAt: started}
	completed := make(map[string]JobReport, len(ordered))
	for _, spec := range ordered {
		if blocked, ok := dependencyBlockedReport(spec, completed, r.opts.DependencyMode); ok {
			r.opts.Logger.Warn("sync job skipped", "job", spec.Name, "reason", blocked.Err)
			report.Jobs = append(report.Jobs, blocked)
			completed[spec.Name] = blocked
			continue
		}
		jobReport := r.runJob(ctx, spec)
		report.Jobs = append(report.Jobs, jobReport)
		completed[spec.Name] = jobReport
	}
	report.FinishedAt = time.Now().UTC()
	r.opts.Logger.Info("sync pipeline finished", "jobs", len(report.Jobs), "elapsed", report.FinishedAt.Sub(report.StartedAt).Round(time.Second))
	return report, nil
}

func dependencyBlockedReport(spec JobSpec, completed map[string]JobReport, mode DependencyMode) (JobReport, bool) {
	if mode == "" {
		mode = DependencyModePermissive
	}
	for _, dep := range spec.DependsOn {
		depReport, ok := completed[dep]
		if !ok {
			if mode == DependencyModeStrict {
				return JobReport{Job: spec.Name, Status: JobStatusSkipped, Err: fmt.Sprintf("dependency %s was not selected", dep)}, true
			}
			continue
		}
		if depReport.Status != JobStatusFailed {
			continue
		}
		reason := fmt.Sprintf("dependency %s failed", dep)
		if strings.TrimSpace(depReport.Err) != "" {
			reason += ": " + depReport.Err
		}
		return JobReport{Job: spec.Name, Status: JobStatusSkipped, Err: reason}, true
	}
	return JobReport{}, false
}

func (r *Runner) runJob(ctx context.Context, spec JobSpec) JobReport {
	jobStarted := time.Now().UTC()
	r.opts.Logger.Info("sync job started", "job", spec.Name)
	jobCtx := ctx
	if spec.PerJobTimeout > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(ctx, spec.PerJobTimeout)
		defer cancel()
	}
	keys, err := RetryValue(jobCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" source keys", func(ctx context.Context) ([]string, error) {
		return spec.Syncer.SourceKeys(ctx, r.conn)
	})
	if err != nil {
		r.opts.Logger.Error("sync job source resolution failed", "job", spec.Name, "err", err)
		return JobReport{Job: spec.Name, Status: JobStatusFailed, Err: err.Error()}
	}
	if len(keys) == 0 {
		r.opts.Logger.Info("sync job skipped", "job", spec.Name, "reason", "no source keys")
		return JobReport{Job: spec.Name, Status: JobStatusSkipped}
	}
	ledger := importledger.New(r.conn)
	report := JobReport{Job: spec.Name, Status: JobStatusSuccess}
	sourceConcurrency := r.sourceConcurrency(spec, len(keys))
	r.opts.Logger.Info("sync job sources resolved", "job", spec.Name, "sources", len(keys), "source_concurrency", sourceConcurrency)
	if r.opts.Progress != nil {
		r.opts.Progress.StartJob(spec.Name, len(keys))
	}
	completedSources := 0
	if sourceConcurrency <= 1 {
		for _, rawKey := range keys {
			key := NormalizeSourceKey(rawKey)
			sourceReport := r.runSource(jobCtx, spec, ledger, key)
			report.Sources = append(report.Sources, sourceReport)
			report.RowsInserted += sourceReport.RowsInserted
			completedSources++
			if r.opts.Progress != nil {
				r.opts.Progress.SourceDone(spec.Name, sourceReport, completedSources, len(keys))
			}
			if sourceReport.Status == JobStatusFailed {
				report.Status = JobStatusFailed
				report.Err = sourceReport.Err
			}
		}
	} else {
		type sourceTask struct {
			index int
			key   string
		}
		type sourceResult struct {
			index  int
			report SourceReport
		}
		jobs := make(chan sourceTask)
		results := make(chan sourceResult, sourceConcurrency)
		var wg sync.WaitGroup
		for i := 0; i < sourceConcurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for task := range jobs {
					sourceReport := r.runSource(jobCtx, spec, ledger, task.key)
					select {
					case results <- sourceResult{index: task.index, report: sourceReport}:
					case <-jobCtx.Done():
						return
					}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for index, rawKey := range keys {
				select {
				case jobs <- sourceTask{index: index, key: NormalizeSourceKey(rawKey)}:
				case <-jobCtx.Done():
					return
				}
			}
		}()
		go func() {
			wg.Wait()
			close(results)
		}()

		orderedReports := make([]SourceReport, len(keys))
		for result := range results {
			orderedReports[result.index] = result.report
			report.RowsInserted += result.report.RowsInserted
			completedSources++
			if r.opts.Progress != nil {
				r.opts.Progress.SourceDone(spec.Name, result.report, completedSources, len(keys))
			}
			if report.Status != JobStatusFailed && result.report.Status == JobStatusFailed {
				report.Status = JobStatusFailed
				report.Err = result.report.Err
			}
		}
		for index, sourceReport := range orderedReports {
			if strings.TrimSpace(sourceReport.SourceKey) == "" {
				sourceReport = canceledSourceReport(NormalizeSourceKey(keys[index]), jobCtx.Err())
				orderedReports[index] = sourceReport
				completedSources++
				if r.opts.Progress != nil {
					r.opts.Progress.SourceDone(spec.Name, sourceReport, completedSources, len(keys))
				}
			}
			if report.Status != JobStatusFailed && sourceReport.Status == JobStatusFailed {
				report.Status = JobStatusFailed
				report.Err = sourceReport.Err
			}
			report.Sources = append(report.Sources, sourceReport)
		}
		if report.Status == JobStatusFailed {
			return report
		}
	}
	if r.opts.AuditEnabled {
		findings, err := RetryValue(jobCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" audit", func(ctx context.Context) ([]DuplicateFinding, error) {
			return NewAuditor(r.conn, r.opts.Logger).AuditJob(ctx, spec, report.Sources, r.opts.AuditOptions)
		})
		if err != nil {
			r.opts.Logger.Warn("audit failed", "job", spec.Name, "err", err)
		} else {
			report.AuditFindings = findings
		}
	}
	r.opts.Logger.Info("sync job finished", "job", spec.Name, "status", report.Status, "rows", report.RowsInserted, "sources", len(report.Sources), "audit_findings", len(report.AuditFindings), "elapsed", time.Since(jobStarted).Round(time.Second))
	if r.opts.Progress != nil {
		r.opts.Progress.FinishJob(spec.Name, report)
	}
	return report
}

func canceledSourceReport(sourceKey string, err error) SourceReport {
	if err == nil {
		err = context.Canceled
	}
	return SourceReport{SourceKey: sourceKey, Status: JobStatusFailed, Err: err.Error()}
}

func (r *Runner) sourceConcurrency(spec JobSpec, keyCount int) int {
	concurrency := spec.Syncer.MaxConcurrency()
	if concurrency <= 0 {
		concurrency = 1
	}
	if r.opts.MaxSourceConcurrency > 0 && r.opts.MaxSourceConcurrency < concurrency {
		concurrency = r.opts.MaxSourceConcurrency
	}
	if keyCount > 0 && concurrency > keyCount {
		concurrency = keyCount
	}
	if concurrency <= 0 {
		return 1
	}
	return concurrency
}

func (r *Runner) runSource(ctx context.Context, spec JobSpec, ledger *importledger.Repository, sourceKey string) SourceReport {
	sourceCtx := ctx
	if spec.PerSourceTimeout > 0 {
		var cancel context.CancelFunc
		sourceCtx, cancel = context.WithTimeout(ctx, spec.PerSourceTimeout)
		defer cancel()
	}
	window, err := RetryValue(sourceCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" resolve window", func(ctx context.Context) (resolvedWindow, error) {
		return r.resolveWindow(ctx, spec, sourceKey)
	})
	if err != nil {
		r.opts.Logger.Error("sync source window failed", "job", spec.Name, "source", sourceKey, "err", err)
		return SourceReport{SourceKey: sourceKey, Status: JobStatusFailed, Err: err.Error()}
	}
	if window.SkipReason != "" {
		r.opts.Logger.Info("sync source skipped", "job", spec.Name, "source", sourceKey, "from", formatLogDate(window.From), "to", formatLogDate(window.To), "reason", window.SkipReason)
		return SourceReport{SourceKey: sourceKey, From: window.From, To: window.To, Status: JobStatusSkipped, Notes: []string{window.SkipReason}}
	}
	from, to := window.From, window.To
	scope := ScopeKeyForRange(from, to)
	sourceStarted := time.Now().UTC()
	r.opts.Logger.Info("sync source started", "job", spec.Name, "source", sourceKey, "scope", scope, "from", formatLogDate(from), "to", formatLogDate(to), "dry_run", r.opts.DryRun, "force", r.opts.Force)
	if !r.opts.Force {
		ok, err := RetryValue(sourceCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" ledger check", func(ctx context.Context) (bool, error) {
			return ledger.AlreadySucceeded(ctx, spec.Name, sourceKey, scope)
		})
		if err != nil {
			r.opts.Logger.Error("sync source ledger check failed", "job", spec.Name, "source", sourceKey, "scope", scope, "err", err)
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, Err: err.Error()}
		}
		if ok {
			r.opts.Logger.Info("sync source skipped", "job", spec.Name, "source", sourceKey, "scope", scope, "reason", "ledger already succeeded")
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusSkipped}
		}
	}
	importID := "dry-run"
	sourceHash := SourceHashFor(spec.Name, sourceKey, scope)
	if !r.opts.DryRun {
		var err error
		importID, err = RetryValue(sourceCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" ledger start", func(ctx context.Context) (string, error) {
			return ledger.Start(ctx, importledger.StartRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, SourceHash: sourceHash, StartedAt: time.Now().UTC(), PendingTTL: r.opts.LockOptions.TTL, IgnorePending: r.opts.LockOptions.ForceUnlock})
		})
		if err != nil {
			r.opts.Logger.Error("sync source ledger start failed", "job", spec.Name, "source", sourceKey, "scope", scope, "err", err)
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, Err: err.Error()}
		}
	}
	res, err := spec.Syncer.Sync(sourceCtx, r.conn, SyncRequest{SourceKey: sourceKey, From: from, To: to, DryRun: r.opts.DryRun})
	if err != nil {
		if !r.opts.DryRun {
			syncErr := err
			cleanupCtx, cancel := failureCleanupContext(sourceCtx, defaultFailureCleanupTimeout)
			err = importledger.RecordFailure(cleanupCtx, retryFailureRecorder{recorder: ledger, retry: r.opts.DBRetry, logger: r.opts.Logger, operation: spec.Name + " ledger failure"}, importledger.CompletionRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, ImportID: importID, SourceHash: sourceHash, RowsInserted: importledger.NonNegativeRows(res.RowsInserted), ErrorMessage: syncErr.Error(), CompletedAt: time.Now().UTC()}, syncErr)
			cancel()
		}
		r.opts.Logger.Error("sync source failed", "job", spec.Name, "source", sourceKey, "scope", scope, "rows", res.RowsInserted, "elapsed", time.Since(sourceStarted).Round(time.Second), "err", err)
		return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, RowsInserted: res.RowsInserted, Err: err.Error(), Notes: res.Notes}
	}
	if !r.opts.DryRun {
		if err := Retry(sourceCtx, r.opts.DBRetry, r.opts.Logger, spec.Name+" ledger success", func(ctx context.Context) error {
			return ledger.MarkSuccess(ctx, importledger.CompletionRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, ImportID: importID, SourceHash: sourceHash, RowsInserted: importledger.NonNegativeRows(res.RowsInserted), CompletedAt: time.Now().UTC()})
		}); err != nil {
			r.opts.Logger.Error("sync source ledger success failed", "job", spec.Name, "source", sourceKey, "scope", scope, "rows", res.RowsInserted, "err", err)
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, RowsInserted: res.RowsInserted, Err: err.Error(), Notes: res.Notes}
		}
	}
	r.opts.Logger.Info("sync source finished", "job", spec.Name, "source", sourceKey, "scope", scope, "rows", res.RowsInserted, "notes", len(res.Notes), "elapsed", time.Since(sourceStarted).Round(time.Second))
	return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusSuccess, RowsInserted: res.RowsInserted, Notes: res.Notes}
}

func failureCleanupContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultFailureCleanupTimeout
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func (r *Runner) retry(ctx context.Context, operation string, fn func(context.Context) error) error {
	return Retry(ctx, r.opts.DBRetry, r.opts.Logger, operation, fn)
}

type retryFailureRecorder struct {
	recorder  importledger.FailureRecorder
	retry     RetryOptions
	logger    *slog.Logger
	operation string
}

func (r retryFailureRecorder) MarkFailed(ctx context.Context, req importledger.CompletionRequest) error {
	return Retry(ctx, r.retry, r.logger, r.operation, func(ctx context.Context) error {
		return r.recorder.MarkFailed(ctx, req)
	})
}

func formatLogDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02")
}

func (r *Runner) resolveWindow(ctx context.Context, spec JobSpec, sourceKey string) (resolvedWindow, error) {
	to := r.opts.ToOverride
	if to.IsZero() {
		to = r.startedAt
	}
	from := r.opts.FromOverride
	if from.IsZero() {
		cursor, ok, err := spec.Syncer.ResolveCursor(ctx, r.conn, sourceKey)
		if err != nil {
			return resolvedWindow{}, err
		}
		if ok {
			if !r.opts.Force && r.opts.FromOverride.IsZero() && r.opts.ToOverride.IsZero() && shouldSkipRecentCursor(cursor, r.startedAt) {
				return resolvedWindow{From: dateOnly(cursor), To: dateOnly(to), SkipReason: recentCursorSkipReason(spec.Name, sourceKey, cursor, r.startedAt)}, nil
			}
			from = cursor.AddDate(0, 0, -spec.OverlapDays)
		} else {
			from = spec.Syncer.ColdStartFloor(sourceKey)
		}
		floor := spec.Syncer.ColdStartFloor(sourceKey)
		if !floor.IsZero() && from.Before(floor) {
			from = floor
		}
	}
	from = dateOnly(from)
	to = dateOnly(to)
	if to.Before(from) {
		return resolvedWindow{}, fmt.Errorf("to %s before from %s", to.Format("2006-01-02"), from.Format("2006-01-02"))
	}
	return resolvedWindow{From: from, To: to}, nil
}

func shouldSkipRecentCursor(cursor, startedAt time.Time) bool {
	if cursor.IsZero() || startedAt.IsZero() {
		return false
	}
	coverageEnd := dateOnly(cursor).Add(24 * time.Hour)
	if !startedAt.After(coverageEnd) {
		return true
	}
	return startedAt.Sub(coverageEnd) <= recentCursorSkipThreshold
}

func recentCursorSkipReason(jobName, sourceKey string, cursor, startedAt time.Time) string {
	return fmt.Sprintf("skip recent sync: latest cursor %s for %s/%s is within %s of runner start %s", dateOnly(cursor).Format("2006-01-02"), jobName, NormalizeSourceKey(sourceKey), recentCursorSkipThreshold, startedAt.UTC().Format(time.RFC3339))
}

func topoSort(specs []JobSpec) ([]JobSpec, error) {
	byName := make(map[string]JobSpec, len(specs))
	for _, spec := range specs {
		if spec.Name == "" || spec.Syncer == nil {
			return nil, fmt.Errorf("invalid job spec")
		}
		byName[spec.Name] = spec
	}
	visited := map[string]int{}
	var out []JobSpec
	var visit func(string) error
	visit = func(name string) error {
		switch visited[name] {
		case 1:
			return fmt.Errorf("dependency cycle at %s", name)
		case 2:
			return nil
		}
		spec, ok := byName[name]
		if !ok {
			return fmt.Errorf("unknown dependency %s", name)
		}
		visited[name] = 1
		for _, dep := range spec.DependsOn {
			if _, selected := byName[dep]; selected {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visited[name] = 2
		out = append(out, spec)
		return nil
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func NormalizeSourceKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return SingletonSourceKey
	}
	return value
}

func ScopeKeyForRange(from, to time.Time) string {
	return dateOnly(from).Format("2006-01-02") + ".." + dateOnly(to).Format("2006-01-02")
}

func SourceHashFor(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func dateOnly(value time.Time) time.Time {
	value = value.UTC()
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type AuditOptions struct {
	LookbackDays         int
	MaxFindingsPerTarget int
}

type DuplicateFinding struct {
	Job        string
	SourceKey  string
	Table      string
	Count      uint64
	KeyValues  []string
	WindowFrom time.Time
	WindowTo   time.Time
}

type Auditor struct {
	conn   driver.Conn
	logger *slog.Logger
}

func NewAuditor(conn driver.Conn, logger *slog.Logger) *Auditor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Auditor{conn: conn, logger: logger}
}

func (a *Auditor) AuditJob(ctx context.Context, spec JobSpec, sources []SourceReport, opts AuditOptions) ([]DuplicateFinding, error) {
	limit := opts.MaxFindingsPerTarget
	if limit <= 0 {
		limit = 50
	}
	lookback := opts.LookbackDays
	if lookback < 0 {
		lookback = 0
	}
	var findings []DuplicateFinding
	for _, source := range sources {
		if !shouldAuditSource(source) {
			continue
		}
		from := source.From.AddDate(0, 0, -lookback)
		for _, target := range spec.Syncer.AuditTargets(source.SourceKey) {
			if target.Table == "" || target.DateColumn == "" || len(target.KeyColumns) == 0 {
				continue
			}
			rows, err := a.auditTarget(ctx, spec.Name, source.SourceKey, target, from, source.To, limit)
			if err != nil {
				return findings, err
			}
			findings = append(findings, rows...)
		}
	}
	return findings, nil
}

func shouldAuditSource(source SourceReport) bool {
	return source.Status == JobStatusSuccess || (source.Status == JobStatusFailed && source.RowsInserted > 0)
}

func (a *Auditor) auditTarget(ctx context.Context, job, sourceKey string, target AuditTarget, from, to time.Time, limit int) ([]DuplicateFinding, error) {
	cols := strings.Join(target.KeyColumns, ", ")
	query := fmt.Sprintf("SELECT %s, count() AS c FROM %s WHERE %s >= {from:Date} AND %s <= {to:Date}", cols, target.Table, target.DateColumn, target.DateColumn)
	if strings.TrimSpace(target.SourceFilter) != "" {
		query += " AND " + target.SourceFilter
	}
	query += fmt.Sprintf(" GROUP BY %s HAVING c > 1 LIMIT %d", cols, limit)
	rows, err := a.conn.Query(ctx, query, clickhouse.Named("from", from.Format("2006-01-02")), clickhouse.Named("to", to.Format("2006-01-02")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var findings []DuplicateFinding
	for rows.Next() {
		values := make([]any, len(target.KeyColumns)+1)
		scans := make([]any, len(values))
		for i := range values {
			scans[i] = &values[i]
		}
		if err := rows.Scan(scans...); err != nil {
			return nil, err
		}
		var count uint64
		switch v := values[len(values)-1].(type) {
		case uint64:
			count = v
		case uint32:
			count = uint64(v)
		case int64:
			count = uint64(v)
		default:
			count = 0
		}
		keyValues := make([]string, 0, len(target.KeyColumns))
		for _, value := range values[:len(target.KeyColumns)] {
			keyValues = append(keyValues, fmt.Sprint(value))
		}
		findings = append(findings, DuplicateFinding{Job: job, SourceKey: sourceKey, Table: target.Table, Count: count, KeyValues: keyValues, WindowFrom: from, WindowTo: to})
	}
	return findings, rows.Err()
}

func DefaultAuditReportPath(dir string, now time.Time) string {
	if strings.TrimSpace(dir) == "" {
		dir = "reports"
	}
	return filepath.Join(dir, "data-sync-audit-"+now.UTC().Format("20060102-150405")+".csv")
}

func WriteAuditReportCSV(path string, findings []DuplicateFinding) (string, error) {
	if len(findings) == 0 {
		return "", nil
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("audit report path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"job", "source_key", "table", "count", "key_values", "window_from", "window_to"}); err != nil {
		return "", err
	}
	for _, f := range findings {
		if err := writer.Write([]string{f.Job, f.SourceKey, f.Table, fmt.Sprint(f.Count), strings.Join(f.KeyValues, "|"), f.WindowFrom.Format("2006-01-02"), f.WindowTo.Format("2006-01-02")}); err != nil {
			return "", err
		}
	}
	if err := writer.Error(); err != nil {
		return "", err
	}
	return path, nil
}
