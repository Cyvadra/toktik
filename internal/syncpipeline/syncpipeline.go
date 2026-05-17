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
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/importledger"
)

const SingletonSourceKey = "_default"

type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusFailed  JobStatus = "failed"
	JobStatusSkipped JobStatus = "skipped"
)

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
	Logger            *slog.Logger
	MaxJobConcurrency int
	DryRun            bool
	Force             bool
	FromOverride      time.Time
	ToOverride        time.Time
	LockOptions       LockOptions
	AuditEnabled      bool
	AuditOptions      AuditOptions
}

type Runner struct {
	conn driver.Conn
	opts RunnerOptions
}

func NewRunner(conn driver.Conn, opts RunnerOptions) *Runner {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Runner{conn: conn, opts: opts}
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
	conn driver.Conn
	opts LockOptions
}

func NewLedgerHooks(conn driver.Conn, opts LockOptions) *LedgerHooks {
	if opts.TTL <= 0 {
		opts.TTL = 2 * time.Hour
	}
	return &LedgerHooks{conn: conn, opts: opts}
}

func (h *LedgerHooks) ClearStaleLocks(ctx context.Context) ([]ClearedLock, error) {
	cutoff := time.Now().UTC().Add(-h.opts.TTL)
	rows, err := h.conn.Query(ctx, `SELECT importer_name, source_key, scope_key, started_at
FROM import_ledger FINAL
WHERE status = 'pending' AND started_at < parseDateTimeBestEffortOrNull({cutoff:String})`,
		clickhouse.Named("cutoff", cutoff.Format("2006-01-02 15:04:05")),
	)
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
	for _, lock := range locks {
		_ = importledger.New(h.conn).MarkFailed(ctx, importledger.CompletionRequest{
			ImporterName: lock.ImporterName,
			SourceKey:    lock.SourceKey,
			ScopeKey:     lock.ScopeKey,
			ImportID:     "force-unlock",
			ErrorMessage: "force-unlock stale pending row",
			CompletedAt:  time.Now().UTC(),
		})
	}
	return locks, nil
}

func (r *Runner) Run(ctx context.Context, specs []JobSpec) (RunReport, error) {
	started := time.Now().UTC()
	ordered, err := topoSort(specs)
	if err != nil {
		return RunReport{}, err
	}
	if err := importledger.EnsureSchema(ctx, r.conn); err != nil {
		return RunReport{}, err
	}
	report := RunReport{StartedAt: started}
	for _, spec := range ordered {
		jobReport := r.runJob(ctx, spec)
		report.Jobs = append(report.Jobs, jobReport)
		if jobReport.Status == JobStatusFailed {
			report.FinishedAt = time.Now().UTC()
			return report, fmt.Errorf("job %s failed: %s", spec.Name, jobReport.Err)
		}
	}
	report.FinishedAt = time.Now().UTC()
	return report, nil
}

func (r *Runner) runJob(ctx context.Context, spec JobSpec) JobReport {
	jobCtx := ctx
	if spec.PerJobTimeout > 0 {
		var cancel context.CancelFunc
		jobCtx, cancel = context.WithTimeout(ctx, spec.PerJobTimeout)
		defer cancel()
	}
	keys, err := spec.Syncer.SourceKeys(jobCtx, r.conn)
	if err != nil {
		return JobReport{Job: spec.Name, Status: JobStatusFailed, Err: err.Error()}
	}
	if len(keys) == 0 {
		return JobReport{Job: spec.Name, Status: JobStatusSkipped}
	}
	ledger := importledger.New(r.conn)
	report := JobReport{Job: spec.Name, Status: JobStatusSuccess}
	for _, rawKey := range keys {
		key := NormalizeSourceKey(rawKey)
		sourceReport := r.runSource(jobCtx, spec, ledger, key)
		report.Sources = append(report.Sources, sourceReport)
		report.RowsInserted += sourceReport.RowsInserted
		if sourceReport.Status == JobStatusFailed {
			report.Status = JobStatusFailed
			report.Err = sourceReport.Err
			return report
		}
	}
	if r.opts.AuditEnabled {
		findings, err := NewAuditor(r.conn, r.opts.Logger).AuditJob(jobCtx, spec, report.Sources, r.opts.AuditOptions)
		if err != nil {
			r.opts.Logger.Warn("audit failed", "job", spec.Name, "err", err)
		} else {
			report.AuditFindings = findings
		}
	}
	return report
}

func (r *Runner) runSource(ctx context.Context, spec JobSpec, ledger *importledger.Repository, sourceKey string) SourceReport {
	sourceCtx := ctx
	if spec.PerSourceTimeout > 0 {
		var cancel context.CancelFunc
		sourceCtx, cancel = context.WithTimeout(ctx, spec.PerSourceTimeout)
		defer cancel()
	}
	from, to, err := r.resolveWindow(sourceCtx, spec, sourceKey)
	if err != nil {
		return SourceReport{SourceKey: sourceKey, Status: JobStatusFailed, Err: err.Error()}
	}
	scope := ScopeKeyForRange(from, to)
	if !r.opts.Force {
		ok, err := ledger.AlreadySucceeded(sourceCtx, spec.Name, sourceKey, scope)
		if err != nil {
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, Err: err.Error()}
		}
		if ok {
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusSkipped}
		}
	}
	importID := "dry-run"
	sourceHash := SourceHashFor(spec.Name, sourceKey, scope)
	if !r.opts.DryRun {
		var err error
		importID, err = ledger.Start(sourceCtx, importledger.StartRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, SourceHash: sourceHash, StartedAt: time.Now().UTC()})
		if err != nil {
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, Err: err.Error()}
		}
	}
	res, err := spec.Syncer.Sync(sourceCtx, r.conn, SyncRequest{SourceKey: sourceKey, From: from, To: to, DryRun: r.opts.DryRun})
	if err != nil {
		if !r.opts.DryRun {
			_ = ledger.MarkFailed(context.Background(), importledger.CompletionRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, ImportID: importID, SourceHash: sourceHash, RowsInserted: importledger.NonNegativeRows(res.RowsInserted), ErrorMessage: err.Error(), CompletedAt: time.Now().UTC()})
		}
		return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, RowsInserted: res.RowsInserted, Err: err.Error(), Notes: res.Notes}
	}
	if !r.opts.DryRun {
		if err := ledger.MarkSuccess(sourceCtx, importledger.CompletionRequest{ImporterName: spec.Name, SourceKey: sourceKey, ScopeKey: scope, ImportID: importID, SourceHash: sourceHash, RowsInserted: importledger.NonNegativeRows(res.RowsInserted), CompletedAt: time.Now().UTC()}); err != nil {
			return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusFailed, RowsInserted: res.RowsInserted, Err: err.Error(), Notes: res.Notes}
		}
	}
	return SourceReport{SourceKey: sourceKey, From: from, To: to, Status: JobStatusSuccess, RowsInserted: res.RowsInserted, Notes: res.Notes}
}

func (r *Runner) resolveWindow(ctx context.Context, spec JobSpec, sourceKey string) (time.Time, time.Time, error) {
	to := r.opts.ToOverride
	if to.IsZero() {
		to = time.Now().UTC()
	}
	from := r.opts.FromOverride
	if from.IsZero() {
		cursor, ok, err := spec.Syncer.ResolveCursor(ctx, r.conn, sourceKey)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if ok {
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
		return time.Time{}, time.Time{}, fmt.Errorf("to %s before from %s", to.Format("2006-01-02"), from.Format("2006-01-02"))
	}
	return from, to, nil
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
		if source.Status != JobStatusSuccess {
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
