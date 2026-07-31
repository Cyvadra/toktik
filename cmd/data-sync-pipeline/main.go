package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chpriority"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dataintegrity"
	"github.com/Cyvadra/toktik/internal/forexmarket"
	"github.com/Cyvadra/toktik/internal/requestpriority"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
	pipelinejobs "github.com/Cyvadra/toktik/internal/syncpipeline/jobs"
	"github.com/Cyvadra/toktik/internal/usexport"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/internal/usmarket/macro"
	"github.com/Cyvadra/toktik/pkg/fmp"
	"github.com/schollz/progressbar/v3"
	"gopkg.in/yaml.v3"
)

const defaultPipelineConfigPath = "configs/data-sync-pipeline.yaml"

type usStockSourcePolicy string

const usStockSourcePolygon usStockSourcePolicy = "polygon"

type pipelineConfig struct {
	Runner runnerConfig         `yaml:"runner"`
	Jobs   map[string]jobConfig `yaml:"jobs"`
}

type runnerConfig struct {
	MaxSourceConcurrency    int    `yaml:"max_source_concurrency"`
	MaxJobConcurrency       int    `yaml:"max_job_concurrency"`
	OverlapDays             int    `yaml:"overlap_days"`
	AuditEnabled            bool   `yaml:"audit_enabled"`
	AuditLookbackDays       int    `yaml:"audit_lookback_days"`
	AuditMaxFindings        int    `yaml:"audit_max_findings"`
	LockTTL                 string `yaml:"lock_ttl"`
	DBRetryMaxAttempts      int    `yaml:"db_retry_max_attempts"`
	DBRetryInitialDelay     string `yaml:"db_retry_initial_delay"`
	DBRetryMaxDelay         string `yaml:"db_retry_max_delay"`
	DefaultPerJobTimeout    string `yaml:"default_per_job_timeout"`
	DefaultPerSourceTimeout string `yaml:"default_per_source_timeout"`
}

func (c *runnerConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawRunnerConfig runnerConfig
	next := rawRunnerConfig(*c)
	if err := value.Decode(&next); err != nil {
		return err
	}
	seenMaxSource := false
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "max_source_concurrency" {
			seenMaxSource = true
			break
		}
	}
	*c = runnerConfig(next)
	if !seenMaxSource && c.MaxJobConcurrency > 0 {
		c.MaxSourceConcurrency = c.MaxJobConcurrency
	}
	return nil
}

type jobConfig struct {
	Enabled                  bool              `yaml:"enabled"`
	DependsOn                []string          `yaml:"depends_on"`
	OverlapDays              int               `yaml:"overlap_days"`
	PerJobTimeout            string            `yaml:"per_job_timeout"`
	PerSourceTimeout         string            `yaml:"per_source_timeout"`
	Symbols                  []string          `yaml:"symbols"`
	SymbolsFile              string            `yaml:"symbols_file"`
	ResolveAtStartup         bool              `yaml:"resolve_at_startup"`
	IncludeOptionGapMappings bool              `yaml:"include_option_gap_mappings"`
	LimitSymbols             int               `yaml:"limit_symbols"`
	Interval                 string            `yaml:"interval"`
	BatchSize                int               `yaml:"batch_size"`
	Workers                  int               `yaml:"workers"`
	PageSize                 int               `yaml:"page_size"`
	QPS                      int               `yaml:"qps"`
	PriceSource              string            `yaml:"price_source"`
	Provider                 string            `yaml:"provider"`
	FMPQuarterLimit          int               `yaml:"fmp_quarter_limit"`
	IncrementalMode          string            `yaml:"incremental_mode"`
	DiscoveryPageSize        int               `yaml:"discovery_page_size"`
	DiscoveryPageLimit       int               `yaml:"discovery_page_limit"`
	SymbolMappings           map[string]string `yaml:"symbol_mappings"`
	MinCoverage              float64           `yaml:"min_coverage"`
	RiskFreeRate             float64           `yaml:"risk_free_rate"`
	ForceDownload            bool              `yaml:"force_download"`
	SyncStocks               bool              `yaml:"sync_stocks"`
	SyncOptions              *bool             `yaml:"sync_options"`
	SourceInterval           string            `yaml:"source_interval"`
	RebuildAggregates        bool              `yaml:"rebuild_aggregates"`
	Replace                  bool              `yaml:"replace"`
	Underlyings              []string          `yaml:"underlyings"`
	Markets                  []string          `yaml:"markets"`
	PriorityOrder            string            `yaml:"priority_order"`
	LookbackDays             int               `yaml:"lookback_days"`
	MinDaysToExpiry          int               `yaml:"min_days_to_expiry"`
	MaxDaysToExpiry          int               `yaml:"max_days_to_expiry"`
	Dataset                  string            `yaml:"dataset"`
	URL                      string            `yaml:"url"`
	ConstituentUniverse      string            `yaml:"constituent_universe"`
	PriceSymbol              string            `yaml:"price_symbol"`
	ReferenceSymbol          string            `yaml:"reference_symbol"`
	RollingQuarters          int               `yaml:"rolling_quarters"`
	MinQuarters              int               `yaml:"min_quarters"`
	ColdStartFloor           string            `yaml:"cold_start_floor"`
	CalendarChunkDays        int               `yaml:"calendar_chunk_days"`
	RepairFrom               string            `yaml:"repair_from"`
	RepairTo                 string            `yaml:"repair_to"`
}

type optionalBoolFlag struct {
	value bool
	set   bool
}

func (f *optionalBoolFlag) String() string {
	if !f.set {
		return ""
	}
	if f.value {
		return "true"
	}
	return "false"
}

func (f *optionalBoolFlag) Set(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		f.value = true
	case "0", "f", "false", "n", "no", "off":
		f.value = false
	default:
		return fmt.Errorf("invalid boolean value %q", value)
	}
	f.set = true
	return nil
}

func (f *optionalBoolFlag) IsBoolFlag() bool { return true }

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	appCli.SetupLogger(false, slog.LevelInfo)
	var err error
	switch os.Args[1] {
	case "run":
		err = runCommand(os.Args[2:])
	case "status":
		err = statusCommand(os.Args[2:])
	case "audit":
		err = auditCommand(os.Args[2:])
	case "integrity":
		err = integrityCommand(os.Args[2:])
	case "list-jobs":
		err = listJobsCommand(os.Args[2:])
	case "us-market-export", "rus-market-export":
		err = usMarketExportCommand(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "data-sync-pipeline: %v\n", err)
		os.Exit(1)
	}
}

func usMarketExportCommand(args []string) error {
	fs := flag.NewFlagSet("us-market-export", flag.ContinueOnError)
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	symbolsFlag := fs.String("symbols", "", "Comma-separated US stock/option underlying symbols to export, e.g. AAPL,MSFT,SPY")
	startDateFlag := fs.String("start-date", "", "Inclusive market date start (YYYY-MM-DD)")
	endDateFlag := fs.String("end-date", "", "Inclusive market date end (YYYY-MM-DD); defaults to --start-date")
	intervalFlag := fs.String("interval", "1m", "Bar interval to export (1m,5m,15m,30m,1h,2h,4h,1d)")
	outputDir := fs.String("output-dir", "", "Output directory; defaults to exports/us-market-<symbols>-<start>-<end>-<interval>")
	regularOnly := fs.Bool("regular-session-only", false, "Export regular-session rows only. Only valid for 1m data because higher interval views are already regular-session aggregates")
	includeStocks := fs.Bool("include-stocks", true, "Export stock bars")
	includeContracts := fs.Bool("include-option-contracts", true, "Export distinct option contracts seen in the date range")
	includeOptions := fs.Bool("include-option-bars", true, "Export option bars")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	runtimeCfg := appCli.MustLoadRuntime()
	clickHouseDSN := strings.TrimSpace(*dsn)
	if clickHouseDSN == "" {
		clickHouseDSN = runtimeCfg.ClickHouse.DSN
	}
	symbols := usexport.NormalizeSymbols([]string{*symbolsFlag})
	if len(symbols) == 0 {
		return fmt.Errorf("--symbols is required")
	}
	if strings.TrimSpace(*startDateFlag) == "" {
		return fmt.Errorf("--start-date is required")
	}
	startDate := appCli.ParseDate(*startDateFlag, "--start-date")
	endDate := startDate
	if strings.TrimSpace(*endDateFlag) != "" {
		endDate = appCli.ParseDate(*endDateFlag, "--end-date")
	}
	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, clickHouseDSN)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
	}
	defer conn.Close()
	result, err := usexport.Run(ctx, conn, usexport.Config{Symbols: symbols, StartDate: startDate, EndDate: endDate, Interval: *intervalFlag, OutputDir: *outputDir, RegularSessionOnly: *regularOnly, IncludeStocks: *includeStocks, IncludeOptionContracts: *includeContracts, IncludeOptionBars: *includeOptions})
	if err != nil {
		return err
	}
	for _, file := range result.Files {
		fmt.Printf("exported %s rows=%d path=%s\n", file.Name, file.Rows, file.Path)
	}
	fmt.Printf("US market export complete: files=%d dir=%s\n", len(result.Files), result.OutputDir)
	return nil
}

func integrityCommand(args []string) error {
	fs := flag.NewFlagSet("integrity", flag.ContinueOnError)
	configPath := fs.String("config", defaultPipelineConfigPath, "Pipeline YAML config path")
	fromValue := fs.String("from", "", "Inclusive start date (YYYY-MM-DD); default is seven days before --to")
	toValue := fs.String("to", "", "Inclusive end date (YYYY-MM-DD); default is today UTC")
	targetsCSV := fs.String("targets", "all", "Comma-separated checks: all, us-options-aggregates, us-stocks-aggregates, chain-cache, fundamentals, features")
	underlyingsCSV := fs.String("underlyings", "", "Comma-separated US option underlyings to check")
	symbolsCSV := fs.String("symbols", "", "Comma-separated US stock symbols to check")
	repair := fs.Bool("repair", false, "Repair rebuildable aggregate/cache findings")
	dryRun := fs.Bool("dry-run", false, "Print planned repairs without mutating data")
	format := fs.String("format", "text", "Output format: text or json")
	maxSamples := fs.Int("max-samples", 10, "Maximum sample missing keys per finding")
	lookbackDays := fs.Int("lookback-days", 252, "Feature volatility lookback_days to validate")
	minDTE := fs.Int("min-days-to-expiry", 0, "Feature daily panel min_days_to_expiry to validate")
	maxDTE := fs.Int("max-days-to-expiry", 365, "Feature daily panel max_days_to_expiry to validate")
	fundamentalStale := fs.Duration("fundamental-stale", 120*24*time.Hour, "Flag PE/PB observations older than this duration")
	featureStale := fs.Duration("feature-stale", 48*time.Hour, "Flag feature rows whose latest updated_at is older than this duration")
	maxMemoryGB := fs.Float64("max-memory-gb", 12, "ClickHouse SETTING max_memory_usage in GiB (0 disables the cap)")
	externalGroupByGB := fs.Float64("external-group-by-gb", 8, "ClickHouse SETTING max_bytes_before_external_group_by in GiB (0 disables spill-to-disk)")
	maxThreads := fs.Int("max-threads", 4, "ClickHouse SETTING max_threads (0 leaves the server default)")
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cmdCtx, err := preparePipelineCommand(*configPath, *dsn)
	if err != nil {
		return err
	}
	fmpAPIKey, err := cmdCtx.Runtime.FMPAPIKey()
	if err != nil {
		return fmt.Errorf("read FMP api key: %w", err)
	}
	from, err := parseOptionalDate(*fromValue, "--from")
	if err != nil {
		return err
	}
	to, err := parseOptionalDate(*toValue, "--to")
	if err != nil {
		return err
	}
	retryOpts, err := resolveDBRetryOptions(cmdCtx.Config.Runner, 0, 0, 0)
	if err != nil {
		return err
	}
	ctx := requestpriority.WithBackground(context.Background())
	conn, err := connectPipelineClickHouse(ctx, cmdCtx.Runtime, cmdCtx.ClickHouseDSN, retryOpts)
	if err != nil {
		return err
	}
	defer conn.Close()
	report, err := dataintegrity.NewChecker(conn).Run(ctx, dataintegrity.Request{
		From:          from,
		To:            to,
		Targets:       splitCSV(*targetsCSV),
		Underlyings:   splitCSV(*underlyingsCSV),
		Symbols:       splitCSV(*symbolsCSV),
		ClickHouseDSN: cmdCtx.ClickHouseDSN,
		FMPAPIKey:     fmpAPIKey,
		Progress: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		},
		Repair:                        *repair,
		DryRun:                        *dryRun,
		MaxSamples:                    *maxSamples,
		FMPQuarterLimit:               40,
		FundamentalWorkers:            2,
		FundamentalBatchSize:          1000,
		FundamentalPageSize:           251,
		FundamentalQPS:                5,
		LookbackDays:                  *lookbackDays,
		MinDaysToExpiry:               *minDTE,
		MaxDaysToExpiry:               *maxDTE,
		FundamentalStale:              *fundamentalStale,
		FeatureStale:                  *featureStale,
		FundamentalDistributedLimiter: distributedLimiterConfig(cmdCtx.Runtime),
		MaxMemoryUsageBytes:           gibToBytes(*maxMemoryGB),
		MaxBytesBeforeExternalGroupBy: gibToBytes(*externalGroupByGB),
		MaxThreads:                    *maxThreads,
	})
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "", "text":
		printIntegrityReport(report)
	case "json":
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
	default:
		return fmt.Errorf("unknown --format %q", *format)
	}
	return integrityExitError(report)
}

// gibToBytes converts a GiB value provided on the CLI into bytes. Non-positive
// values disable the corresponding ClickHouse SETTING.
func gibToBytes(gib float64) uint64 {
	if gib <= 0 {
		return 0
	}
	return uint64(gib * float64(1<<30))
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", defaultPipelineConfigPath, "Pipeline YAML config path")
	jobsCSV := fs.String("jobs", "", "Comma-separated job names to run")
	fromValue := fs.String("from", "", "Explicit inclusive start date (YYYY-MM-DD); default uses cursor")
	toValue := fs.String("to", "", "Explicit inclusive end date (YYYY-MM-DD); default is today UTC")
	overlapDays := fs.Int("overlap-days", -1, "Override overlap days for all selected jobs")
	workers := fs.Int("workers", 0, "Override max concurrent sources per job")
	dependencyModeValue := fs.String("dependency-mode", string(syncpipeline.DependencyModePermissive), "Dependency handling mode: permissive allows unselected dependencies, strict skips jobs whose dependencies were not selected")
	dryRun := fs.Bool("dry-run", false, "Run without writing data rows")
	force := fs.Bool("force", false, "Ignore successful ledger short-circuit")
	forceUnlock := fs.Bool("force-unlock", false, "Clear stale pending ledger rows older than lock TTL and ignore the lock")
	initSchema := fs.Bool("init-schema", true, "Initialize selected job schemas before running")
	dbRetryAttempts := fs.Int("db-retry-attempts", 0, "Override retry attempts for transient ClickHouse errors")
	dbRetryInitialDelay := fs.Duration("db-retry-initial-delay", 0, "Override initial delay for transient ClickHouse retries")
	dbRetryMaxDelay := fs.Duration("db-retry-max-delay", 0, "Override maximum delay for transient ClickHouse retries")
	var auditFlag optionalBoolFlag
	fs.Var(&auditFlag, "audit", "Run post-sync duplicate audit; defaults to runner.audit_enabled")
	auditReportDir := fs.String("audit-report-dir", "reports", "Directory to write the audit CSV report into when findings are non-empty")
	auditReportPath := fs.String("audit-report", "", "Explicit audit report path (overrides --audit-report-dir)")
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cmdCtx, err := preparePipelineCommand(*configPath, *dsn)
	if err != nil {
		return err
	}
	cfg := cmdCtx.Config
	applyGlobalOverlapOverride(&cfg, *overlapDays)
	auditEnabled := resolveAuditEnabled(cfg.Runner, auditFlag)
	selected := selectedSet(*jobsCSV)
	printMissingSelectedDependencyWarnings(cfg, selected)
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx = requestpriority.WithBackground(ctx)
	retryOpts, err := resolveDBRetryOptions(cfg.Runner, *dbRetryAttempts, *dbRetryInitialDelay, *dbRetryMaxDelay)
	if err != nil {
		return err
	}
	conn, err := connectPipelineClickHouse(ctx, cmdCtx.Runtime, cmdCtx.ClickHouseDSN, retryOpts)
	if err != nil {
		return err
	}
	defer conn.Close()
	sessions, err := initSelectedSchemas(ctx, conn, cfg, selected, *initSchema, retryOpts)
	if err != nil {
		return err
	}
	specs, err := buildJobSpecs(cmdCtx.Runtime, cmdCtx.ClickHouseDSN, cfg, selected, sessions)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return fmt.Errorf("no jobs selected; enable jobs in %s or pass --jobs", *configPath)
	}
	from, err := parseOptionalDate(*fromValue, "--from")
	if err != nil {
		return err
	}
	to, err := parseOptionalDate(*toValue, "--to")
	if err != nil {
		return err
	}
	lockTTL, err := parseDurationDefault(cfg.Runner.LockTTL, 2*time.Hour)
	if err != nil {
		return fmt.Errorf("runner.lock_ttl: %w", err)
	}
	maxWorkers := resolveSourceConcurrency(cfg.Runner, *workers)
	dependencyMode, err := syncpipeline.ParseDependencyMode(*dependencyModeValue)
	if err != nil {
		return err
	}
	if *forceUnlock {
		cleared, err := syncpipeline.NewLedgerHooksWithRetry(conn, syncpipeline.LockOptions{TTL: lockTTL, ForceUnlock: true}, retryOpts, slog.Default()).ClearStaleLocks(ctx)
		if err != nil {
			return fmt.Errorf("clear stale locks: %w", err)
		}
		for _, lock := range cleared {
			fmt.Fprintf(os.Stderr, "force-unlock cleared %s/%s/%s started_at=%s\n", lock.ImporterName, lock.SourceKey, lock.ScopeKey, lock.StartedAt.UTC().Format(time.RFC3339))
		}
	}
	if !*dryRun {
		if err := printPreRunSnapshotAndWait(ctx, conn, specs, 5*time.Second, retryOpts); err != nil {
			return err
		}
	}
	progress := newTerminalProgress(os.Stderr)
	runnerLogger := slog.New(slog.NewTextHandler(progress.LogWriter(), &slog.HandlerOptions{Level: slog.LevelInfo}))
	previousLogger := slog.Default()
	slog.SetDefault(runnerLogger)
	report, err := syncpipeline.NewRunner(conn, syncpipeline.RunnerOptions{
		Logger:               runnerLogger,
		Progress:             progress,
		MaxSourceConcurrency: maxWorkers,
		DependencyMode:       dependencyMode,
		DryRun:               *dryRun,
		Force:                *force,
		FromOverride:         from,
		ToOverride:           to,
		LockOptions:          syncpipeline.LockOptions{TTL: lockTTL, ForceUnlock: *forceUnlock},
		DBRetry:              retryOpts,
		AuditEnabled:         auditEnabled,
		AuditOptions: syncpipeline.AuditOptions{
			LookbackDays:         cfg.Runner.AuditLookbackDays,
			MaxFindingsPerTarget: cfg.Runner.AuditMaxFindings,
		},
	}).Run(ctx, specs)
	progress.Close()
	slog.SetDefault(previousLogger)
	if err != nil {
		return err
	}
	printRunReport(report)
	if err := writeRunAuditReport(report, *auditReportPath, *auditReportDir); err != nil {
		return err
	}
	if err := printPostRunOptionCoverageWarnings(ctx, conn, specs, retryOpts); err != nil {
		return err
	}
	return nil
}

type terminalProgress struct {
	mu     sync.Mutex
	out    io.Writer
	bar    *progressbar.ProgressBar
	active bool
}

type progressLogWriter struct {
	progress *terminalProgress
}

func newTerminalProgress(out io.Writer) *terminalProgress {
	if out == nil {
		out = io.Discard
	}
	return &terminalProgress{out: out}
}

func (p *terminalProgress) LogWriter() io.Writer {
	return progressLogWriter{progress: p}
}

func (w progressLogWriter) Write(data []byte) (int, error) {
	w.progress.mu.Lock()
	defer w.progress.mu.Unlock()
	w.progress.clearForLogLocked()
	_, err := w.progress.out.Write(data)
	w.progress.renderAfterLogLocked()
	return len(data), err
}

func (p *terminalProgress) StartJob(job string, totalSources int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if totalSources <= 1 {
		p.bar = progressbar.NewOptions(-1,
			progressbar.OptionSetWriter(p.out),
			progressbar.OptionSetDescription(job+" running"),
			progressbar.OptionSetWidth(28),
			progressbar.OptionThrottle(100*time.Millisecond),
			progressbar.OptionClearOnFinish(),
		)
		p.active = true
		p.renderAfterLogLocked()
		return
	}
	p.bar = progressbar.NewOptions(totalSources,
		progressbar.OptionSetWriter(p.out),
		progressbar.OptionSetDescription(job),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString("sources"),
		progressbar.OptionSetWidth(28),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionClearOnFinish(),
	)
	p.active = true
	p.renderAfterLogLocked()
}

func (p *terminalProgress) SourceDone(job string, report syncpipeline.SourceReport, completedSources int, totalSources int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bar == nil {
		return
	}
	description := job
	if report.SourceKey != "" {
		description += " " + report.SourceKey
	}
	if report.Status != "" {
		description += " " + string(report.Status)
	}
	p.bar.Describe(description)
	_ = p.bar.Set(completedSources)
	p.active = completedSources < totalSources
}

func (p *terminalProgress) StartUnitProgress(description, unit string, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if total <= 0 {
		return
	}
	if p.bar != nil {
		_ = p.bar.Clear()
	}
	if strings.TrimSpace(unit) == "" {
		unit = "items"
	}
	p.bar = progressbar.NewOptions(total,
		progressbar.OptionSetWriter(p.out),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionShowIts(),
		progressbar.OptionSetItsString(unit),
		progressbar.OptionSetWidth(28),
		progressbar.OptionThrottle(100*time.Millisecond),
		progressbar.OptionClearOnFinish(),
	)
	p.active = true
	p.renderAfterLogLocked()
}

func (p *terminalProgress) AdvanceUnitProgress(description string, completed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bar == nil {
		return
	}
	if strings.TrimSpace(description) != "" {
		p.bar.Describe(description)
	}
	_ = p.bar.Set(completed)
}

func (p *terminalProgress) FinishJob(job string, report syncpipeline.JobReport) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bar != nil {
		p.bar.Describe(fmt.Sprintf("%s %s", job, report.Status))
		_ = p.bar.Finish()
	}
	p.active = false
	p.bar = nil
}

func (p *terminalProgress) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bar != nil {
		_ = p.bar.Clear()
	}
	p.active = false
	p.bar = nil
}

func (p *terminalProgress) clearForLogLocked() {
	if !p.active || p.bar == nil {
		return
	}
	_ = p.bar.Clear()
}

func (p *terminalProgress) renderAfterLogLocked() {
	if !p.active || p.bar == nil {
		return
	}
	_ = p.bar.RenderBlank()
}

func statusCommand(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	configPath := fs.String("config", defaultPipelineConfigPath, "Pipeline YAML config path")
	jobsCSV := fs.String("jobs", "", "Comma-separated job names to filter")
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cmdCtx, err := preparePipelineCommand(*configPath, *dsn)
	if err != nil {
		return err
	}
	retryOpts, err := resolveDBRetryOptions(cmdCtx.Config.Runner, 0, 0, 0)
	if err != nil {
		return err
	}
	ctx := requestpriority.WithBackground(context.Background())
	conn, err := connectPipelineClickHouse(ctx, cmdCtx.Runtime, cmdCtx.ClickHouseDSN, retryOpts)
	if err != nil {
		return err
	}
	defer conn.Close()
	return printStatus(ctx, conn, selectedSet(*jobsCSV))
}

func auditCommand(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	configPath := fs.String("config", defaultPipelineConfigPath, "Pipeline YAML config path")
	jobsCSV := fs.String("jobs", "", "Comma-separated job names to audit")
	fromValue := fs.String("from", time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02"), "Audit start date")
	toValue := fs.String("to", time.Now().UTC().Format("2006-01-02"), "Audit end date")
	auditReportDir := fs.String("audit-report-dir", "reports", "Directory to write the audit CSV report into when findings are non-empty")
	auditReportPath := fs.String("audit-report", "", "Explicit audit report path (overrides --audit-report-dir)")
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cmdCtx, err := preparePipelineCommand(*configPath, *dsn)
	if err != nil {
		return err
	}
	cfg := cmdCtx.Config
	from, err := parseRequiredDate(*fromValue, "--from")
	if err != nil {
		return err
	}
	to, err := parseRequiredDate(*toValue, "--to")
	if err != nil {
		return err
	}
	retryOpts, err := resolveDBRetryOptions(cfg.Runner, 0, 0, 0)
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := connectPipelineClickHouse(ctx, cmdCtx.Runtime, cmdCtx.ClickHouseDSN, retryOpts)
	if err != nil {
		return err
	}
	defer conn.Close()
	selected := selectedSet(*jobsCSV)
	sessions, err := initSelectedSchemas(ctx, conn, cfg, selected, true, retryOpts)
	if err != nil {
		return err
	}
	specs, err := buildJobSpecs(cmdCtx.Runtime, cmdCtx.ClickHouseDSN, cfg, selected, sessions)
	if err != nil {
		return err
	}
	auditor := syncpipeline.NewAuditor(conn, slog.Default())
	var all []syncpipeline.DuplicateFinding
	for _, spec := range specs {
		keys, err := spec.Syncer.SourceKeys(ctx, conn)
		if err != nil {
			return fmt.Errorf("%s source keys: %w", spec.Name, err)
		}
		sources := make([]syncpipeline.SourceReport, 0, len(keys))
		for _, key := range keys {
			sources = append(sources, syncpipeline.SourceReport{SourceKey: key, From: from, To: to, Status: syncpipeline.JobStatusSuccess})
		}
		findings, err := auditor.AuditJob(ctx, spec, sources, syncpipeline.AuditOptions{MaxFindingsPerTarget: cfg.Runner.AuditMaxFindings})
		if err != nil {
			return err
		}
		all = append(all, findings...)
	}
	printAuditFindings(all)
	if _, err := writeAuditReportFile(all, *auditReportPath, *auditReportDir); err != nil {
		return err
	}
	return nil
}

func writeRunAuditReport(report syncpipeline.RunReport, explicit, dir string) error {
	var all []syncpipeline.DuplicateFinding
	for _, job := range report.Jobs {
		all = append(all, job.AuditFindings...)
	}
	_, err := writeAuditReportFile(all, explicit, dir)
	return err
}

func writeAuditReportFile(findings []syncpipeline.DuplicateFinding, explicit, dir string) (string, error) {
	if len(findings) == 0 {
		return "", nil
	}
	path := strings.TrimSpace(explicit)
	if path == "" {
		path = syncpipeline.DefaultAuditReportPath(dir, time.Now())
	}
	written, err := syncpipeline.WriteAuditReportCSV(path, findings)
	if err != nil {
		return "", fmt.Errorf("write audit report: %w", err)
	}
	if written != "" {
		fmt.Fprintf(os.Stderr, "audit report: %s (%d findings)\n", written, len(findings))
	}
	return written, nil
}

func listJobsCommand(args []string) error {
	fs := flag.NewFlagSet("list-jobs", flag.ContinueOnError)
	configPath := fs.String("config", defaultPipelineConfigPath, "Pipeline YAML config path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfg, err := loadPipelineConfig(*configPath)
	if err != nil {
		return err
	}
	for _, name := range sortedJobNames(cfg) {
		job := cfg.Jobs[name]
		status := "disabled"
		if job.Enabled {
			status = "enabled"
		}
		deps := "-"
		if len(job.DependsOn) > 0 {
			deps = strings.Join(job.DependsOn, ",")
		}
		fmt.Printf("%-24s %-8s deps=%s\n", name, status, deps)
	}
	return nil
}

func loadPipelineConfig(path string) (pipelineConfig, error) {
	cfg := defaultPipelineConfig()
	if strings.TrimSpace(path) == "" {
		path = defaultPipelineConfigPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && path == defaultPipelineConfigPath {
			return cfg, nil
		}
		return pipelineConfig{}, fmt.Errorf("read pipeline config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return pipelineConfig{}, fmt.Errorf("parse pipeline config %s: %w", path, err)
	}
	if cfg.Jobs == nil {
		cfg.Jobs = map[string]jobConfig{}
	}
	if err := normalizePipelineConfig(&cfg); err != nil {
		return pipelineConfig{}, fmt.Errorf("normalize pipeline config %s: %w", path, err)
	}
	return cfg, nil
}

func defaultPipelineConfig() pipelineConfig {
	cfg := pipelineConfig{
		Runner: runnerConfig{MaxSourceConcurrency: 1, OverlapDays: 1, AuditEnabled: true, AuditLookbackDays: 7, AuditMaxFindings: 50, LockTTL: "2h"},
		Jobs: map[string]jobConfig{
			// Keep the Gurufocus job for macro CAPE/pe10 only. Symbol-level `pe`
			// for ETF underlyings such as SPY/IWM/QQQ now comes from
			// fmp_etf_fundamentals into fundamental_observation.
			"guru_macro":             {Enabled: true, BatchSize: 1000, URL: macro.DefaultGurufocusShillerURL, ReferenceSymbol: macro.DefaultReferenceSymbol, Dataset: macro.DefaultGurufocusShillerDataset},
			"fmp_sp500_macro":        {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, Dataset: macro.DefaultFMPSP500Dataset, ConstituentUniverse: "sp500", PriceSymbol: "SPY", ReferenceSymbol: "SPY", Workers: 6, BatchSize: 1000, RollingQuarters: 40, MinQuarters: 40, ColdStartFloor: "2016-01-01"},
			"fmp_nasdaq100_macro":    {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, Dataset: macro.DefaultFMPNasdaq100Dataset, ConstituentUniverse: "nasdaq100", PriceSymbol: "QQQ", ReferenceSymbol: "QQQ", Workers: 6, BatchSize: 1000, RollingQuarters: 40, MinQuarters: 40, ColdStartFloor: "2016-01-01"},
			"fmp_crypto_spot":        {Enabled: true, ResolveAtStartup: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
			"fmp_forex":              {Enabled: true, ResolveAtStartup: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
			"fmp_us_stocks":          {Enabled: true, DependsOn: []string{"polygon_us_flatfiles"}, ResolveAtStartup: true, IncludeOptionGapMappings: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
			"fmp_us_stock_splits":    {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, ResolveAtStartup: true, BatchSize: 1000, OverlapDays: 3, ColdStartFloor: "1990-01-01"},
			"fmp_us_stock_profiles":  {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, ResolveAtStartup: true, IncludeOptionGapMappings: true, BatchSize: 25, Workers: 4},
			"fmp_us_fundamentals":    {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, Provider: "fmp", Workers: 2, BatchSize: 1000, PageSize: 251, QPS: 10, FMPQuarterLimit: 40, IncrementalMode: "sec-filings-financials", DiscoveryPageSize: 250, DiscoveryPageLimit: 0},
			"fmp_etf_fundamentals":   {Enabled: true, DependsOn: []string{"fmp_us_fundamentals"}, Symbols: []string{"SPY", "IWM", "NDX", "FIX", "KWEB"}, SymbolMappings: map[string]string{"NDX": "QQQ"}, BatchSize: 1000, QPS: 10, MinCoverage: 0.8},
			"polygon_us_flatfiles":   {Enabled: true, BatchSize: 100000, Workers: 2, RiskFreeRate: 0.05, SyncStocks: false},
			"polygon_us_greeks":      {Enabled: true, DependsOn: []string{"polygon_us_flatfiles", "fmp_us_stocks"}, BatchSize: 100000, Workers: 2, RiskFreeRate: 0.05, RebuildAggregates: true},
			"feature_store_backfill": {Enabled: false, DependsOn: []string{"polygon_us_greeks"}, Markets: []string{"us-options"}, PriorityOrder: usmarket.PriorityOrderUSDefault, LookbackDays: 252, MinDaysToExpiry: 0, MaxDaysToExpiry: 365, Workers: 4, Replace: true, ColdStartFloor: "2022-01-01"},
		},
	}
	_ = normalizePipelineConfig(&cfg)
	return cfg
}

func normalizePipelineConfig(cfg *pipelineConfig) error {
	if cfg.Jobs == nil {
		cfg.Jobs = map[string]jobConfig{}
	}
	if cfg.Runner.MaxSourceConcurrency <= 0 && cfg.Runner.MaxJobConcurrency > 0 {
		cfg.Runner.MaxSourceConcurrency = cfg.Runner.MaxJobConcurrency
	}

	applyUSStockSourcePolicy(cfg, usStockSourcePolygon)

	if job, ok := cfg.Jobs["fmp_us_fundamentals"]; ok && job.Enabled {
		job.DependsOn = dependOnPolygonStockSource(job.DependsOn)
		cfg.Jobs["fmp_us_fundamentals"] = job
	}
	if job, ok := cfg.Jobs["fmp_us_stock_splits"]; ok && job.Enabled {
		job.DependsOn = dependOnPolygonStockSource(job.DependsOn)
		cfg.Jobs["fmp_us_stock_splits"] = job
	}
	if job, ok := cfg.Jobs["fmp_us_stock_profiles"]; ok && job.Enabled {
		job.DependsOn = dependOnPolygonStockSource(job.DependsOn)
		cfg.Jobs["fmp_us_stock_profiles"] = job
	}
	for _, name := range []string{"fmp_sp500_macro", "fmp_nasdaq100_macro"} {
		if job, ok := cfg.Jobs[name]; ok && job.Enabled {
			job.DependsOn = dependOnPolygonStockSource(job.DependsOn)
			cfg.Jobs[name] = job
		}
	}
	if job, ok := cfg.Jobs["polygon_us_greeks"]; ok && job.Enabled {
		job.DependsOn = dependOnPolygonStockSource(job.DependsOn)
		cfg.Jobs["polygon_us_greeks"] = job
	}
	if job, ok := cfg.Jobs["feature_store_backfill"]; ok && job.Enabled {
		if len(job.Markets) == 0 {
			job.Markets = []string{"us-options"}
		}
		if containsString(job.Markets, "us-options") {
			job.DependsOn = ensureDependency(job.DependsOn, "polygon_us_greeks")
		}
		if containsString(job.Markets, "crypto-options") {
			job.DependsOn = ensureDependency(job.DependsOn, "fmp_crypto_spot")
		}
		cfg.Jobs["feature_store_backfill"] = job
	}
	return nil
}

func applyUSStockSourcePolicy(cfg *pipelineConfig, policy usStockSourcePolicy) {
	if cfg == nil || policy != usStockSourcePolygon {
		return
	}
	polygonJob := cfg.Jobs["polygon_us_flatfiles"]
	polygonJob.Enabled = true
	polygonJob.SyncStocks = true
	cfg.Jobs["polygon_us_flatfiles"] = polygonJob

	fmpStocksJob := cfg.Jobs["fmp_us_stocks"]
	fmpStocksJob.Enabled = false
	cfg.Jobs["fmp_us_stocks"] = fmpStocksJob
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func resolveOptionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func applyGlobalOverlapOverride(cfg *pipelineConfig, overlapDays int) {
	if cfg == nil || overlapDays < 0 {
		return
	}
	for name, job := range cfg.Jobs {
		job.OverlapDays = overlapDays
		cfg.Jobs[name] = job
	}
}

func resolveAuditEnabled(cfg runnerConfig, auditFlag optionalBoolFlag) bool {
	if auditFlag.set {
		return auditFlag.value
	}
	return cfg.AuditEnabled
}

func resolveSourceConcurrency(cfg runnerConfig, override int) int {
	concurrency := cfg.MaxSourceConcurrency
	if override > 0 {
		concurrency = override
	}
	if concurrency <= 0 {
		return 1
	}
	return concurrency
}

func dependOnPolygonStockSource(deps []string) []string {
	out := make([]string, 0, len(deps)+1)
	seen := map[string]struct{}{}
	for _, dep := range deps {
		candidate := strings.TrimSpace(dep)
		if candidate == "" {
			continue
		}
		if candidate == "fmp_us_stocks" {
			candidate = "polygon_us_flatfiles"
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	if _, ok := seen["polygon_us_flatfiles"]; !ok {
		out = append(out, "polygon_us_flatfiles")
	}
	return out
}

func ensureDependency(deps []string, dep string) []string {
	for _, existing := range deps {
		if existing == dep {
			return deps
		}
	}
	return append(deps, dep)
}

type pipelineCommandContext struct {
	Runtime       config.Runtime
	ClickHouseDSN string
	Config        pipelineConfig
}

func preparePipelineCommand(configPath, rawDSN string) (pipelineCommandContext, error) {
	runtimeCfg, clickHouseDSN := resolveRuntimeClickHouseDSN(rawDSN)
	cfg, err := loadPipelineConfig(configPath)
	if err != nil {
		return pipelineCommandContext{}, err
	}
	return pipelineCommandContext{Runtime: runtimeCfg, ClickHouseDSN: clickHouseDSN, Config: cfg}, nil
}

func resolveRuntimeClickHouseDSN(rawDSN string) (config.Runtime, string) {
	runtimeCfg := appCli.MustLoadRuntime()
	if strings.TrimSpace(rawDSN) != "" {
		return runtimeCfg, strings.TrimSpace(rawDSN)
	}
	return runtimeCfg, runtimeCfg.ClickHouse.DSN
}

func connectPipelineClickHouse(ctx context.Context, runtimeCfg config.Runtime, dsn string, retry syncpipeline.RetryOptions) (driver.Conn, error) {
	conn, err := syncpipeline.RetryValue(ctx, retry, slog.Default(), "connect ClickHouse", func(ctx context.Context) (driver.Conn, error) {
		return usmarket.ConnectClickHouse(ctx, dsn)
	})
	if err != nil {
		return nil, fmt.Errorf("connect ClickHouse: %w", err)
	}
	if runtimeCfg.ClickHouse.Priority.Enabled {
		conn = chpriority.Wrap(conn, chpriority.DefaultWorkloads())
	}
	return conn, nil
}

func buildJobSpecs(runtimeCfg config.Runtime, dsn string, cfg pipelineConfig, selected map[string]bool, sessions usmarket.SessionMap) ([]syncpipeline.JobSpec, error) {
	apiKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return nil, err
	}
	buildCtx := syncerBuildContext{Runtime: runtimeCfg, APIKey: apiKey, ClickHouseDSN: dsn, Sessions: sessions, Limiter: distributedLimiterConfig(runtimeCfg)}
	var specs []syncpipeline.JobSpec
	for _, name := range sortedJobNames(cfg) {
		job := cfg.Jobs[name]
		if !shouldIncludeJob(name, job, selected) {
			continue
		}
		syncer, err := buildSyncer(buildCtx, name, job)
		if err != nil {
			return nil, err
		}
		spec, err := makeJobSpec(name, job, cfg.Runner, syncer)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

type syncerBuildContext struct {
	Runtime       config.Runtime
	APIKey        string
	ClickHouseDSN string
	Sessions      usmarket.SessionMap
	Limiter       usmarket.DistributedRateLimitConfig
}

func buildSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, error) {
	if syncer, ok, err := buildFMPSyncer(buildCtx, name, job); ok {
		return syncer, err
	}
	if syncer, ok, err := buildCalendarSyncer(buildCtx, name, job); ok {
		return syncer, err
	}
	if syncer, ok, err := buildMacroSyncer(buildCtx, name, job); ok {
		return syncer, err
	}
	if syncer, ok, err := buildPolygonSyncer(buildCtx, name, job); ok {
		return syncer, err
	}
	if syncer, ok, err := buildFeatureSyncer(buildCtx, name, job); ok {
		return syncer, err
	}
	return nil, fmt.Errorf("unknown job %q", name)
}

func buildFMPSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, bool, error) {
	switch name {
	case "fmp_crypto_spot":
		syncer, err := pipelinejobs.NewFMPCryptoSpot(pipelinejobs.FMPCryptoSpotConfig{APIKey: buildCtx.APIKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, PriceSource: job.PriceSource, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_forex":
		syncer, err := pipelinejobs.NewFMPForex(pipelinejobs.FMPForexConfig{APIKey: buildCtx.APIKey, Symbols: job.Symbols, SymbolsFile: job.SymbolsFile, ResolveAtStartup: job.ResolveAtStartup, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_us_stocks":
		syncer, err := pipelinejobs.NewFMPUSStocks(pipelinejobs.FMPUSStocksConfig{APIKey: buildCtx.APIKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, IncludeOptionGapMappings: job.IncludeOptionGapMappings, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_us_stock_splits":
		syncer, err := pipelinejobs.NewFMPUSStockSplits(pipelinejobs.FMPUSStockSplitsConfig{APIKey: buildCtx.APIKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, IncludeOptionGapMappings: job.IncludeOptionGapMappings, LimitSymbols: job.LimitSymbols, BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_us_stock_profiles":
		syncer, err := pipelinejobs.NewFMPUSStockProfiles(pipelinejobs.FMPUSStockProfilesConfig{APIKey: buildCtx.APIKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, IncludeOptionGapMappings: job.IncludeOptionGapMappings, LimitSymbols: job.LimitSymbols, BatchSize: job.BatchSize, Workers: job.Workers, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_us_fundamentals":
		syncer, err := pipelinejobs.NewFMPUSFundamentals(pipelinejobs.FMPUSFundamentalsConfig{Provider: usmarket.NewFMPPEBackfillProvider(buildCtx.APIKey, job.FMPQuarterLimit), DSN: buildCtx.ClickHouseDSN, Symbols: job.Symbols, IncrementalMode: job.IncrementalMode, DiscoveryPageSize: job.DiscoveryPageSize, DiscoveryPageLimit: job.DiscoveryPageLimit, Workers: job.Workers, BatchSize: job.BatchSize, PageSize: job.PageSize, QPS: job.QPS, LimitSymbols: job.LimitSymbols, DistributedLimiter: buildCtx.Limiter, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_etf_fundamentals":
		syncer, err := pipelinejobs.NewFMPETFFundamentals(pipelinejobs.FMPETFFundamentalsConfig{APIKey: buildCtx.APIKey, DSN: buildCtx.ClickHouseDSN, Symbols: job.Symbols, SymbolMappings: job.SymbolMappings, BatchSize: job.BatchSize, QPS: job.QPS, MinCoverage: job.MinCoverage, DistributedLimiter: buildCtx.Limiter, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	default:
		return nil, false, nil
	}
}

func buildCalendarSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, bool, error) {
	calendarCfg, ok, err := newCalendarSyncerConfig(buildCtx, name, job)
	if !ok || err != nil {
		return nil, ok, err
	}
	switch name {
	case "fmp_economic_calendar":
		syncer, err := pipelinejobs.NewFMPEconomicCalendar(calendarCfg)
		return syncer, true, err
	case "fmp_observed_stock_calendar":
		syncer, err := pipelinejobs.NewFMPObservedStockCalendar(pipelinejobs.FMPObservedStockCalendarConfig{APIKey: calendarCfg.APIKey, FMPCacheDir: calendarCfg.FMPCacheDir, MySQLDSN: calendarCfg.MySQLDSN, Cache: calendarCfg.Cache, ColdStartFloorUTC: calendarCfg.ColdStartFloorUTC})
		return syncer, true, err
	case "fmp_stock_earnings_calendar_backfill":
		syncer, err := pipelinejobs.NewFMPStockEarningsCalendarBackfill(pipelinejobs.FMPStockEarningsCalendarBackfillConfig{APIKey: calendarCfg.APIKey, FMPCacheDir: calendarCfg.FMPCacheDir, MySQLDSN: calendarCfg.MySQLDSN, ChunkDays: job.CalendarChunkDays, RepairFromUTC: parseColdStart(job.RepairFrom), RepairToUTC: parseColdStart(job.RepairTo), ColdStartFloorUTC: calendarCfg.ColdStartFloorUTC})
		return syncer, true, err
	default:
		return nil, false, nil
	}
}

func newCalendarSyncerConfig(buildCtx syncerBuildContext, name string, job jobConfig) (pipelinejobs.FMPEconomicCalendarConfig, bool, error) {
	switch name {
	case "fmp_economic_calendar", "fmp_observed_stock_calendar", "fmp_stock_earnings_calendar_backfill":
		calendarCfg, err := newCalendarPipelineConfig(buildCtx.Runtime, buildCtx.APIKey, parseColdStart(job.ColdStartFloor))
		return calendarCfg, true, err
	default:
		return pipelinejobs.FMPEconomicCalendarConfig{}, false, nil
	}
}

func buildMacroSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, bool, error) {
	switch name {
	case "cboe_vix_macro":
		syncer, err := pipelinejobs.NewGuruMacro(pipelinejobs.GuruMacroConfig{Dataset: job.Dataset, Source: macro.DefaultCBOEVIXSource, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor), SyncFunc: func(ctx context.Context, conn driver.Conn, from, to time.Time, dryRun bool) (int64, error) {
			res, err := macro.SyncCBOEVIX(ctx, conn, macro.CBOEVIXConfig{HistoryURL: job.URL, ReferenceSymbol: job.ReferenceSymbol, BatchSize: job.BatchSize, PreferredDataset: job.Dataset}, from, to, dryRun)
			return int64(res.ObservationRows), err
		}})
		return syncer, true, err
	case "deribit_dvol_macro":
		syncer, err := pipelinejobs.NewDeribitDVOLMacro(pipelinejobs.DeribitDVOLMacroConfig{Symbols: job.Symbols, BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "fmp_sp500_macro", "fmp_nasdaq100_macro":
		syncer, err := pipelinejobs.NewGuruMacro(pipelinejobs.GuruMacroConfig{Dataset: job.Dataset, Source: "fmp", ColdStartFloorUTC: parseColdStart(job.ColdStartFloor), SyncFunc: func(ctx context.Context, conn driver.Conn, from, to time.Time, dryRun bool) (int64, error) {
			res, err := macro.SyncFMPIndexShiller(ctx, conn, macro.FMPIndexShillerConfig{APIKey: buildCtx.APIKey, Dataset: job.Dataset, ConstituentUniverse: job.ConstituentUniverse, PriceSymbol: job.PriceSymbol, ReferenceSymbol: job.ReferenceSymbol, BatchSize: job.BatchSize, Workers: job.Workers, RollingQuarters: job.RollingQuarters, MinQuarters: job.MinQuarters}, from, to, dryRun)
			return int64(res.ObservationRows), err
		}})
		return syncer, true, err
	case "guru_macro":
		syncer, err := pipelinejobs.NewGuruMacro(pipelinejobs.GuruMacroConfig{Dataset: job.Dataset, Source: "gurufocus", ColdStartFloorUTC: parseColdStart(job.ColdStartFloor), SyncFunc: func(ctx context.Context, conn driver.Conn, from, to time.Time, dryRun bool) (int64, error) {
			res, err := macro.SyncGurufocusShiller(ctx, conn, macro.GurufocusShillerConfig{URL: job.URL, ReferenceSymbol: job.ReferenceSymbol, BatchSize: job.BatchSize}, from, to, dryRun)
			return int64(res.ObservationRows), err
		}})
		return syncer, true, err
	default:
		return nil, false, nil
	}
}

func buildPolygonSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, bool, error) {
	switch name {
	case "polygon_us_flatfiles":
		polygonSvc, err := service.NewPolygonServiceFromConfig(buildCtx.Runtime, nil)
		if err != nil {
			return nil, true, err
		}
		syncer, err := pipelinejobs.NewPolygonUSFlatFiles(pipelinejobs.PolygonUSFlatFilesConfig{Downloader: polygonSvc, Sessions: buildCtx.Sessions, DSN: buildCtx.ClickHouseDSN, BatchSize: job.BatchSize, Workers: job.Workers, RiskFreeRate: job.RiskFreeRate, ForceDownload: job.ForceDownload, SyncStocks: job.SyncStocks, SyncOptions: resolveOptionalBool(job.SyncOptions, true), SourceInterval: job.SourceInterval, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	case "polygon_us_greeks":
		syncer, err := pipelinejobs.NewPolygonUSGreeks(pipelinejobs.PolygonUSGreeksConfig{DSN: buildCtx.ClickHouseDSN, BatchSize: job.BatchSize, Workers: job.Workers, RiskFreeRate: job.RiskFreeRate, Underlyings: job.Underlyings, LimitTasks: job.LimitSymbols, RebuildAggregates: job.RebuildAggregates, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
		return syncer, true, err
	default:
		return nil, false, nil
	}
}

func buildFeatureSyncer(buildCtx syncerBuildContext, name string, job jobConfig) (syncpipeline.Syncer, bool, error) {
	if name != "feature_store_backfill" {
		return nil, false, nil
	}
	syncer, err := pipelinejobs.NewFeatureStoreBackfill(pipelinejobs.FeatureStoreBackfillConfig{DSN: buildCtx.ClickHouseDSN, Markets: job.Markets, Underlyings: job.Underlyings, PriorityOrder: job.PriorityOrder, LookbackDays: job.LookbackDays, MinDaysToExpiry: job.MinDaysToExpiry, MaxDaysToExpiry: job.MaxDaysToExpiry, Workers: job.Workers, Replace: job.Replace, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	return syncer, true, err
}

func newPipelineCache(runtimeCfg config.Runtime) (cache.Store, error) {
	return cache.NewStore(context.Background(), runtimeCfg)
}

func distributedLimiterConfig(runtimeCfg config.Runtime) usmarket.DistributedRateLimitConfig {
	return usmarket.DistributedRateLimitConfig{
		Enabled:      runtimeCfg.Redis.Enabled,
		Addr:         runtimeCfg.Redis.Addr,
		Password:     runtimeCfg.Redis.Password,
		DB:           runtimeCfg.Redis.DB,
		KeyPrefix:    runtimeCfg.Redis.KeyPrefix,
		DialTimeout:  runtimeCfg.RedisDialTimeout(),
		ReadTimeout:  runtimeCfg.RedisReadTimeout(),
		WriteTimeout: runtimeCfg.RedisWriteTimeout(),
	}
}

func newCalendarPipelineConfig(runtimeCfg config.Runtime, apiKey string, coldStart time.Time) (pipelinejobs.FMPEconomicCalendarConfig, error) {
	mysqlDSN, err := runtimeCfg.MySQLDSN()
	if err != nil {
		return pipelinejobs.FMPEconomicCalendarConfig{}, err
	}
	cacheStore, err := newPipelineCache(runtimeCfg)
	if err != nil {
		return pipelinejobs.FMPEconomicCalendarConfig{}, fmt.Errorf("calendar cache: %w", err)
	}
	return pipelinejobs.FMPEconomicCalendarConfig{APIKey: apiKey, FMPCacheDir: runtimeCfg.FMP.CacheDir, MySQLDSN: mysqlDSN, Cache: cacheStore, ColdStartFloorUTC: coldStart}, nil
}

func makeJobSpec(name string, job jobConfig, runner runnerConfig, syncer syncpipeline.Syncer) (syncpipeline.JobSpec, error) {
	perJobTimeout, err := parseDurationDefault(firstNonEmpty(job.PerJobTimeout, runner.DefaultPerJobTimeout), 0)
	if err != nil {
		return syncpipeline.JobSpec{}, fmt.Errorf("%s per_job_timeout: %w", name, err)
	}
	perSourceTimeout, err := parseDurationDefault(firstNonEmpty(job.PerSourceTimeout, runner.DefaultPerSourceTimeout), 0)
	if err != nil {
		return syncpipeline.JobSpec{}, fmt.Errorf("%s per_source_timeout: %w", name, err)
	}
	overlap := job.OverlapDays
	if overlap == 0 {
		overlap = runner.OverlapDays
	}
	return syncpipeline.JobSpec{Name: name, Syncer: syncer, DependsOn: job.DependsOn, OverlapDays: overlap, PerJobTimeout: perJobTimeout, PerSourceTimeout: perSourceTimeout}, nil
}

func initSelectedSchemas(ctx context.Context, conn driver.Conn, cfg pipelineConfig, selected map[string]bool, enabled bool, retry syncpipeline.RetryOptions) (usmarket.SessionMap, error) {
	if !enabled {
		return nil, nil
	}
	requirements := resolveSchemaRequirements(cfg, selected)
	return syncpipeline.RetryValue(ctx, retry, slog.Default(), "initialize selected schemas", func(ctx context.Context) (usmarket.SessionMap, error) {
		return initializeSelectedSchemas(ctx, conn, requirements)
	})
}

type schemaRequirements struct {
	NeedsUSMarket     bool
	NeedsFundamentals bool
	NeedsForex        bool
	NeedsCrypto       bool
	NeedsFeatureStore bool
}

func resolveSchemaRequirements(cfg pipelineConfig, selected map[string]bool) schemaRequirements {
	requirements := schemaRequirements{}
	for name, job := range cfg.Jobs {
		if !shouldIncludeJob(name, job, selected) {
			continue
		}
		switch name {
		case "fmp_us_stocks", "fmp_us_stock_splits", "polygon_us_flatfiles", "polygon_us_greeks", "guru_macro", "fmp_sp500_macro", "fmp_nasdaq100_macro":
			requirements.NeedsUSMarket = true
		case "feature_store_backfill":
			requirements.NeedsFeatureStore = true
			if containsString(job.Markets, "us-options") {
				requirements.NeedsUSMarket = true
			}
			if containsString(job.Markets, "crypto-options") {
				requirements.NeedsCrypto = true
			}
		}
		switch name {
		case "fmp_us_fundamentals", "fmp_etf_fundamentals", "guru_macro", "fmp_sp500_macro", "fmp_nasdaq100_macro":
			requirements.NeedsFundamentals = true
		case "fmp_forex":
			requirements.NeedsForex = true
		case "fmp_crypto_spot":
			requirements.NeedsCrypto = true
		}
	}
	return requirements
}

func initializeSelectedSchemas(ctx context.Context, conn driver.Conn, requirements schemaRequirements) (usmarket.SessionMap, error) {
	var sessions usmarket.SessionMap
	if requirements.NeedsUSMarket {
		ddl, err := appCli.ResolveSchemaFile("", appCli.UsMarketSchemaFile)
		if err != nil {
			return nil, err
		}
		loaded, err := usmarket.InitializeImportStorageWithOptions(ctx, conn, ddl, usmarket.ImportStorageOptions{})
		if err != nil {
			return nil, fmt.Errorf("initialize us-market schema: %w", err)
		}
		sessions = loaded
	}
	if requirements.NeedsFundamentals {
		ddl, err := appCli.ResolveSchemaFile("", appCli.FundamentalsSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := usmarket.InitFundamentalsSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize fundamentals schema: %w", err)
		}
	}
	if requirements.NeedsForex {
		ddl, err := appCli.ResolveSchemaFile("", appCli.ForexMarketSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := forexmarket.InitSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize forex schema: %w", err)
		}
		if err := forexmarket.InitKlineSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("initialize forex kline schema: %w", err)
		}
	}
	if requirements.NeedsCrypto {
		ddl, err := appCli.ResolveSchemaFile("", appCli.CryptoOptionsSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := cryptooptions.InitSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize crypto schema: %w", err)
		}
		if err := cryptooptions.InitSpotKlineSchema(ctx, conn); err != nil {
			return nil, fmt.Errorf("initialize crypto spot kline schema: %w", err)
		}
	}
	if requirements.NeedsFeatureStore {
		ddl, err := appCli.ResolveSchemaFile("", appCli.FeatureStoreSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := usmarket.InitFundamentalsSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize feature store schema: %w", err)
		}
	}
	return sessions, nil
}

func printMissingSelectedDependencyWarnings(cfg pipelineConfig, selected map[string]bool) {
	for _, warning := range missingSelectedDependencyWarnings(cfg, selected) {
		fmt.Fprintln(os.Stderr, warning)
	}
}

func missingSelectedDependencyWarnings(cfg pipelineConfig, selected map[string]bool) []string {
	if len(selected) == 0 {
		return nil
	}
	var warnings []string
	for _, name := range sortedJobNames(cfg) {
		if !selected[name] {
			continue
		}
		for _, dep := range cfg.Jobs[name].DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || selected[dep] {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("warning: selected job %s depends on %s, but %s was not selected; keeping legacy --jobs behavior", name, dep, dep))
		}
	}
	return warnings
}

func printStatus(ctx context.Context, conn driver.Conn, selected map[string]bool) error {
	where := ""
	args := []any{}
	if len(selected) > 0 {
		where = "WHERE importer_name IN {jobs:Array(String)}"
		args = append(args, clickhouse.Named("jobs", mapKeys(selected)))
	}
	q := fmt.Sprintf(`SELECT importer_name, source_key, scope_key, status, rows_inserted, error_message, started_at, completed_at
FROM import_ledger FINAL
%s
ORDER BY importer_name ASC, source_key ASC, started_at DESC
LIMIT 1 BY importer_name, source_key`, where)
	rows, err := conn.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	fmt.Printf("%-24s %-18s %-23s %-8s %-8s %-20s %s\n", "job", "source", "scope", "status", "rows", "started", "error")
	for rows.Next() {
		var job, source, scope, status, errMsg string
		var rowsInserted uint64
		var startedAt, completedAt time.Time
		if err := rows.Scan(&job, &source, &scope, &status, &rowsInserted, &errMsg, &startedAt, &completedAt); err != nil {
			return err
		}
		_ = completedAt
		fmt.Printf("%-24s %-18s %-23s %-8s %-8d %-20s %s\n", job, source, scope, status, rowsInserted, startedAt.UTC().Format("2006-01-02T15:04:05Z"), errMsg)
	}
	return rows.Err()
}

func printRunReport(report syncpipeline.RunReport) {
	fmt.Printf("run started=%s finished=%s jobs=%d\n", report.StartedAt.Format(time.RFC3339), report.FinishedAt.Format(time.RFC3339), len(report.Jobs))
	for _, job := range report.Jobs {
		fmt.Printf("%-24s status=%-8s rows=%d sources=%d duplicates=%d", job.Job, job.Status, job.RowsInserted, len(job.Sources), len(job.AuditFindings))
		if job.Err != "" {
			fmt.Printf(" err=%q", job.Err)
		}
		fmt.Println()
		for _, source := range job.Sources {
			fmt.Printf("  %-18s %s..%s status=%-8s rows=%d", source.SourceKey, source.From.Format("2006-01-02"), source.To.Format("2006-01-02"), source.Status, source.RowsInserted)
			if source.Err != "" {
				fmt.Printf(" err=%q", source.Err)
			}
			fmt.Println()
			for _, note := range source.Notes {
				fmt.Printf("    note: %s\n", note)
			}
		}
	}
}

func printIntegrityReport(report dataintegrity.Report) {
	fmt.Printf("integrity started=%s finished=%s window=%s..%s targets=%s findings=%d repairs=%d\n",
		report.StartedAt.Format(time.RFC3339),
		report.FinishedAt.Format(time.RFC3339),
		report.From.Format("2006-01-02"),
		report.To.Format("2006-01-02"),
		strings.Join(report.Targets, ","),
		len(report.Findings),
		len(report.Repairs),
	)
	for _, finding := range report.Findings {
		fmt.Printf("%-22s %-32s severity=%-8s", finding.Target, finding.Check, finding.Severity)
		if finding.Interval != "" {
			fmt.Printf(" interval=%s", finding.Interval)
		}
		if finding.Table != "" {
			fmt.Printf(" table=%s", finding.Table)
		}
		if finding.BaseKeys > 0 || finding.TargetKeys > 0 || finding.MissingKeys > 0 {
			fmt.Printf(" base=%d target=%d missing=%d", finding.BaseKeys, finding.TargetKeys, finding.MissingKeys)
		}
		if finding.MissingRatio > 0 {
			fmt.Printf(" missing_ratio=%.4f", finding.MissingRatio)
		}
		if finding.FirstMissingDate != "" || finding.LastMissingDate != "" {
			fmt.Printf(" missing_window=%s..%s", finding.FirstMissingDate, finding.LastMissingDate)
		}
		fmt.Printf(" msg=%q\n", finding.Message)
		if len(finding.Samples) > 0 {
			fmt.Printf("  samples: %s\n", strings.Join(finding.Samples, ", "))
		}
		if len(finding.Offenders) > 0 {
			fmt.Printf("  offenders: %s\n", strings.Join(finding.Offenders, ", "))
		}
	}
	for _, repair := range report.Repairs {
		fmt.Printf("repair %-22s action=%q status=%s", repair.Target, repair.Action, repair.Status)
		if repair.Message != "" {
			fmt.Printf(" msg=%q", repair.Message)
		}
		fmt.Println()
	}
}

func integrityExitError(report dataintegrity.Report) error {
	repairedTargets := map[string]struct{}{}
	for _, repair := range report.Repairs {
		if repair.Status == "failed" {
			return fmt.Errorf("integrity repair failed: %s: %s", repair.Action, repair.Message)
		}
		if repair.Status == "completed" {
			repairedTargets[repair.Target] = struct{}{}
		}
	}
	for _, finding := range report.Findings {
		if _, repaired := repairedTargets[finding.Target]; repaired {
			continue
		}
		if finding.Severity == dataintegrity.SeverityCritical {
			return fmt.Errorf("integrity critical findings detected")
		}
	}
	return nil
}

type preRunSnapshotRow struct {
	Job       string
	Dataset   string
	Table     string
	Latest    time.Time
	HasData   bool
	RowCount  uint64
	Qualifier string
}

func printPreRunSnapshotAndWait(ctx context.Context, conn driver.Conn, specs []syncpipeline.JobSpec, wait time.Duration, retry syncpipeline.RetryOptions) error {
	rows, err := syncpipeline.RetryValue(ctx, retry, slog.Default(), "pre-run snapshot", func(ctx context.Context) ([]preRunSnapshotRow, error) {
		return collectPreRunSnapshot(ctx, conn, specs)
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "latest records before sync:")
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "  no ClickHouse data tables selected; selected jobs may write external storage such as MySQL")
	} else {
		fmt.Fprintf(os.Stderr, "  %-24s %-24s %-26s %-12s %s\n", "job", "dataset", "table", "rows", "latest")
		for _, row := range rows {
			latest := "empty"
			if row.HasData {
				latest = row.Latest.UTC().Format("2006-01-02")
			}
			dataset := row.Dataset
			if row.Qualifier != "" {
				dataset += " " + row.Qualifier
			}
			fmt.Fprintf(os.Stderr, "  %-24s %-24s %-26s %-12d %s\n", row.Job, dataset, row.Table, row.RowCount, latest)
		}
	}
	if wait <= 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "starting real sync in %s; press Ctrl+C to cancel...\n", wait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func collectPreRunSnapshot(ctx context.Context, conn driver.Conn, specs []syncpipeline.JobSpec) ([]preRunSnapshotRow, error) {
	seen := map[string]struct{}{}
	rows := make([]preRunSnapshotRow, 0)
	for _, spec := range specs {
		for _, target := range snapshotTargetsForJob(spec) {
			key := spec.Name + "\x00" + target.Dataset + "\x00" + target.Table + "\x00" + target.WhereSQL
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			row, err := queryPreRunSnapshot(ctx, conn, target)
			if err != nil {
				return nil, fmt.Errorf("pre-run snapshot %s/%s: %w", spec.Name, target.Table, err)
			}
			row.Job = spec.Name
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type snapshotTarget struct {
	Dataset   string
	Table     string
	DateExpr  string
	WhereSQL  string
	Args      []any
	Qualifier string
}

func snapshotTargetsForJob(spec syncpipeline.JobSpec) []snapshotTarget {
	switch spec.Name {
	case "fmp_crypto_spot":
		return []snapshotTarget{{Dataset: "crypto spot", Table: "crypto_spot_bar_1m", DateExpr: "timestamp"}}
	case "fmp_forex":
		return []snapshotTarget{{Dataset: "forex", Table: "forex_bar_1m", DateExpr: "market_date"}}
	case "fmp_us_stocks":
		return []snapshotTarget{{Dataset: "US stocks", Table: "us_stocks_bar_1m", DateExpr: "market_date"}}
	case "fmp_us_stock_splits":
		return []snapshotTarget{{Dataset: "US stock splits", Table: "us_stock_splits", DateExpr: "updated_at"}}
	case "fmp_us_fundamentals":
		return []snapshotTarget{{Dataset: "US fundamentals", Table: "fundamental_observation", DateExpr: "event_ts", WhereSQL: "market = {market:String} AND factor_code IN ('pe','pb')", Args: []any{clickhouse.Named("market", "us-stocks")}, Qualifier: "pe/pb"}}
	case "guru_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "dataset = {dataset:String} AND source = {source:String}", Args: []any{clickhouse.Named("dataset", macro.DefaultGurufocusShillerDataset), clickhouse.Named("source", "gurufocus")}, Qualifier: "gurufocus-shiller"}}
	case "cboe_vix_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "dataset = {dataset:String} AND source = {source:String}", Args: []any{clickhouse.Named("dataset", macro.DefaultCBOEVIXDataset), clickhouse.Named("source", macro.DefaultCBOEVIXSource)}, Qualifier: macro.DefaultCBOEVIXDataset}}
	case "deribit_dvol_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "source = {source:String} AND dataset IN {datasets:Array(String)}", Args: []any{clickhouse.Named("source", macro.DefaultDeribitDVOLSource), clickhouse.Named("datasets", []string{macro.DefaultDeribitDVOLBTCDataset, macro.DefaultDeribitDVOLETHDataset})}, Qualifier: "deribit-dvol"}}
	case "fmp_sp500_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "dataset = {dataset:String} AND source = {source:String}", Args: []any{clickhouse.Named("dataset", macro.DefaultFMPSP500Dataset), clickhouse.Named("source", "fmp")}, Qualifier: "fmp-sp500-shiller"}}
	case "fmp_nasdaq100_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "dataset = {dataset:String} AND source = {source:String}", Args: []any{clickhouse.Named("dataset", macro.DefaultFMPNasdaq100Dataset), clickhouse.Named("source", "fmp")}, Qualifier: "fmp-nasdaq100-shiller"}}
	case "polygon_us_flatfiles":
		targets := make([]snapshotTarget, 0, 2)
		if syncerHasAuditTarget(spec.Syncer, "us_stocks_bar_1m") {
			targets = append(targets, snapshotTarget{Dataset: "US stocks", Table: "us_stocks_bar_1m", DateExpr: "market_date"})
		}
		if syncerHasAuditTarget(spec.Syncer, "us_options_bar_1m") {
			targets = append(targets, snapshotTarget{Dataset: "US options", Table: "us_options_bar_1m", DateExpr: "market_date"})
		}
		return targets
	case "polygon_us_greeks":
		return []snapshotTarget{{Dataset: "US option greeks", Table: "us_options_bar_1m", DateExpr: "market_date", WhereSQL: "isNaN(delta) OR isNaN(gamma) OR isNaN(theta) OR isNaN(vega) OR isNaN(rho)", Qualifier: "missing"}}
	default:
		return nil
	}
}

func syncerHasAuditTarget(syncer syncpipeline.Syncer, table string) bool {
	if syncer == nil || strings.TrimSpace(table) == "" {
		return false
	}
	for _, target := range syncer.AuditTargets(syncpipeline.SingletonSourceKey) {
		if target.Table == table {
			return true
		}
	}
	return false
}

func queryPreRunSnapshot(ctx context.Context, conn driver.Conn, target snapshotTarget) (preRunSnapshotRow, error) {
	query := fmt.Sprintf("SELECT count() AS c, toString(ifNull(maxOrNull(toDate(%s)), toDate('1970-01-01'))) AS d FROM %s", target.DateExpr, target.Table)
	if target.WhereSQL != "" {
		query += " WHERE " + target.WhereSQL
	}
	var count uint64
	var latest string
	if err := conn.QueryRow(ctx, query, target.Args...).Scan(&count, &latest); err != nil {
		return preRunSnapshotRow{}, err
	}
	row := preRunSnapshotRow{Dataset: target.Dataset, Table: target.Table, RowCount: count, HasData: count > 0, Qualifier: target.Qualifier}
	if count == 0 {
		return row, nil
	}
	parsed, err := time.Parse("2006-01-02", latest)
	if err != nil {
		return preRunSnapshotRow{}, fmt.Errorf("parse latest date %q: %w", latest, err)
	}
	row.Latest = parsed.UTC()
	return row, nil
}

func printAuditFindings(findings []syncpipeline.DuplicateFinding) {
	if len(findings) == 0 {
		fmt.Println("audit findings: none")
		return
	}
	fmt.Printf("audit findings: %d\n", len(findings))
	for _, finding := range findings {
		fmt.Printf("job=%s source=%s table=%s count=%d key=%v window=%s..%s\n", finding.Job, finding.SourceKey, finding.Table, finding.Count, finding.KeyValues, finding.WindowFrom.Format("2006-01-02"), finding.WindowTo.Format("2006-01-02"))
	}
}

func printPostRunOptionCoverageWarnings(ctx context.Context, conn driver.Conn, specs []syncpipeline.JobSpec, retry syncpipeline.RetryOptions) error {
	if !needsOptionCoverageWarning(specs) {
		return nil
	}
	missing, err := syncpipeline.RetryValue(ctx, retry, slog.Default(), "option underlying stock coverage check", func(ctx context.Context) ([]string, error) {
		return usmarket.ListUSOptionUnderlyingsMissingStockCoverage(ctx, conn)
	})
	if err != nil {
		return fmt.Errorf("check option underlying stock coverage: %w", err)
	}
	if len(missing) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "warning: %d option underlyings still have no stock coverage in us_stocks_bar_1m after sync: %s\n", len(missing), formatOptionCoverageWarningSymbols(missing))
	return nil
}

func formatOptionCoverageWarningSymbols(missing []string) string {
	return usmarket.FormatSymbolPreview(missing, 50)
}

func needsOptionCoverageWarning(specs []syncpipeline.JobSpec) bool {
	for _, spec := range specs {
		switch spec.Name {
		case "polygon_us_flatfiles", "fmp_us_stocks", "polygon_us_greeks":
			return true
		}
	}
	return false
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: data-sync-pipeline <command> [flags]

Commands:
	run          Execute configured jobs through syncpipeline.Runner
	status       Show latest import_ledger row per job/source
	audit        Run duplicate audit over an explicit window
	integrity    Check core market-data completeness and optionally repair aggregates
	list-jobs    Print configured jobs and dependencies
	us-market-export  Export US market CSV bundle; alias: rus-market-export
`)
}

func sortedJobNames(cfg pipelineConfig) []string {
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func selectedSet(csv string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(csv, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func splitCSV(csv string) []string {
	var out []string
	for _, part := range strings.Split(csv, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func shouldIncludeJob(name string, job jobConfig, selected map[string]bool) bool {
	if len(selected) > 0 {
		return selected[name]
	}
	return job.Enabled
}

func parseOptionalDate(value, flagName string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return parseRequiredDate(value, flagName)
}

func parseRequiredDate(value, flagName string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s %q: %w", flagName, value, err)
	}
	return parsed.UTC(), nil
}

func parseColdStart(value string) time.Time {
	parsed, _ := parseOptionalDate(value, "cold_start_floor")
	return parsed
}

func parseDurationDefault(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.ParseDuration(strings.TrimSpace(value))
}

func resolveDBRetryOptions(cfg runnerConfig, attemptsOverride int, initialDelayOverride, maxDelayOverride time.Duration) (syncpipeline.RetryOptions, error) {
	initialDelay, err := parseDurationDefault(cfg.DBRetryInitialDelay, 0)
	if err != nil {
		return syncpipeline.RetryOptions{}, fmt.Errorf("runner.db_retry_initial_delay: %w", err)
	}
	maxDelay, err := parseDurationDefault(cfg.DBRetryMaxDelay, 0)
	if err != nil {
		return syncpipeline.RetryOptions{}, fmt.Errorf("runner.db_retry_max_delay: %w", err)
	}
	if attemptsOverride > 0 {
		cfg.DBRetryMaxAttempts = attemptsOverride
	}
	if initialDelayOverride > 0 {
		initialDelay = initialDelayOverride
	}
	if maxDelayOverride > 0 {
		maxDelay = maxDelayOverride
	}
	return syncpipeline.RetryOptions{MaxAttempts: cfg.DBRetryMaxAttempts, InitialDelay: initialDelay, MaxDelay: maxDelay}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mapKeys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
