package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dataintegrity"
	"github.com/Cyvadra/toktik/internal/forexmarket"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
	pipelinejobs "github.com/Cyvadra/toktik/internal/syncpipeline/jobs"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/internal/usmarket/macro"
	"github.com/Cyvadra/toktik/pkg/fmp"
	"gopkg.in/yaml.v3"
)

const defaultPipelineConfigPath = "configs/data-sync-pipeline.yaml"

type pipelineConfig struct {
	Runner            runnerConfig            `yaml:"runner"`
	MarketDataSources marketDataSourcesConfig `yaml:"market_data_sources"`
	Jobs              map[string]jobConfig    `yaml:"jobs"`
}

type marketDataSourcesConfig struct {
	USStocks  string `yaml:"us_stocks"`
	USOptions string `yaml:"us_options"`
}

type runnerConfig struct {
	MaxJobConcurrency       int    `yaml:"max_job_concurrency"`
	OverlapDays             int    `yaml:"overlap_days"`
	AuditEnabled            bool   `yaml:"audit_enabled"`
	AuditLookbackDays       int    `yaml:"audit_lookback_days"`
	AuditMaxFindings        int    `yaml:"audit_max_findings"`
	LockTTL                 string `yaml:"lock_ttl"`
	DefaultPerJobTimeout    string `yaml:"default_per_job_timeout"`
	DefaultPerSourceTimeout string `yaml:"default_per_source_timeout"`
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
}

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
	runtimeCfg := appCli.MustLoadRuntime()
	if strings.TrimSpace(*dsn) == "" {
		*dsn = runtimeCfg.ClickHouse.DSN
	}
	fmpAPIKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return fmt.Errorf("read FMP api key: %w", err)
	}
	if _, err := loadPipelineConfig(*configPath); err != nil {
		return err
	}
	from, err := parseOptionalDate(*fromValue, "--from")
	if err != nil {
		return err
	}
	to, err := parseOptionalDate(*toValue, "--to")
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
	}
	defer conn.Close()
	report, err := dataintegrity.NewChecker(conn).Run(ctx, dataintegrity.Request{
		From:          from,
		To:            to,
		Targets:       splitCSV(*targetsCSV),
		Underlyings:   splitCSV(*underlyingsCSV),
		Symbols:       splitCSV(*symbolsCSV),
		ClickHouseDSN: *dsn,
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
		FundamentalDistributedLimiter: usmarket.DistributedRateLimitConfig{Enabled: runtimeCfg.Redis.Enabled, Addr: runtimeCfg.Redis.Addr, Password: runtimeCfg.Redis.Password, DB: runtimeCfg.Redis.DB, KeyPrefix: runtimeCfg.Redis.KeyPrefix, DialTimeout: runtimeCfg.RedisDialTimeout(), ReadTimeout: runtimeCfg.RedisReadTimeout(), WriteTimeout: runtimeCfg.RedisWriteTimeout()},
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
	workers := fs.Int("workers", 0, "Override max concurrent jobs")
	dryRun := fs.Bool("dry-run", false, "Run without writing data rows")
	force := fs.Bool("force", false, "Ignore successful ledger short-circuit")
	forceUnlock := fs.Bool("force-unlock", false, "Clear stale pending ledger rows older than lock TTL and ignore the lock")
	initSchema := fs.Bool("init-schema", true, "Initialize selected job schemas before running")
	auditEnabled := fs.Bool("audit", true, "Run post-sync duplicate audit")
	auditReportDir := fs.String("audit-report-dir", "reports", "Directory to write the audit CSV report into when findings are non-empty")
	auditReportPath := fs.String("audit-report", "", "Explicit audit report path (overrides --audit-report-dir)")
	dsn := fs.String("clickhouse-dsn", "", "ClickHouse DSN; default comes from runtime config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	runtimeCfg := appCli.MustLoadRuntime()
	if strings.TrimSpace(*dsn) == "" {
		*dsn = runtimeCfg.ClickHouse.DSN
	}
	cfg, err := loadPipelineConfig(*configPath)
	if err != nil {
		return err
	}
	if *overlapDays >= 0 {
		for name, job := range cfg.Jobs {
			job.OverlapDays = *overlapDays
			cfg.Jobs[name] = job
		}
	}
	selected := selectedSet(*jobsCSV)
	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
	}
	defer conn.Close()
	sessions, err := initSelectedSchemas(ctx, conn, cfg, selected, *initSchema)
	if err != nil {
		return err
	}
	specs, err := buildJobSpecs(runtimeCfg, *dsn, cfg, selected, sessions)
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
	maxWorkers := cfg.Runner.MaxJobConcurrency
	if *workers > 0 {
		maxWorkers = *workers
	}
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if *forceUnlock {
		cleared, err := syncpipeline.NewLedgerHooks(conn, syncpipeline.LockOptions{TTL: lockTTL, ForceUnlock: true}).ClearStaleLocks(ctx)
		if err != nil {
			return fmt.Errorf("clear stale locks: %w", err)
		}
		for _, lock := range cleared {
			fmt.Fprintf(os.Stderr, "force-unlock cleared %s/%s/%s started_at=%s\n", lock.ImporterName, lock.SourceKey, lock.ScopeKey, lock.StartedAt.UTC().Format(time.RFC3339))
		}
	}
	if !*dryRun {
		if err := printPreRunSnapshotAndWait(ctx, conn, specs, 5*time.Second); err != nil {
			return err
		}
	}
	report, err := syncpipeline.NewRunner(conn, syncpipeline.RunnerOptions{
		Logger:            slog.Default(),
		MaxJobConcurrency: maxWorkers,
		DryRun:            *dryRun,
		Force:             *force,
		FromOverride:      from,
		ToOverride:        to,
		LockOptions:       syncpipeline.LockOptions{TTL: lockTTL, ForceUnlock: *forceUnlock},
		AuditEnabled:      *auditEnabled,
		AuditOptions: syncpipeline.AuditOptions{
			LookbackDays:         cfg.Runner.AuditLookbackDays,
			MaxFindingsPerTarget: cfg.Runner.AuditMaxFindings,
		},
	}).Run(ctx, specs)
	if err != nil {
		return err
	}
	printRunReport(report)
	if err := writeRunAuditReport(report, *auditReportPath, *auditReportDir); err != nil {
		return err
	}
	if err := printPostRunOptionCoverageWarnings(ctx, conn, specs); err != nil {
		return err
	}
	return nil
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
	runtimeCfg := appCli.MustLoadRuntime()
	if strings.TrimSpace(*dsn) == "" {
		*dsn = runtimeCfg.ClickHouse.DSN
	}
	if _, err := loadPipelineConfig(*configPath); err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
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
	runtimeCfg := appCli.MustLoadRuntime()
	if strings.TrimSpace(*dsn) == "" {
		*dsn = runtimeCfg.ClickHouse.DSN
	}
	cfg, err := loadPipelineConfig(*configPath)
	if err != nil {
		return err
	}
	from, err := parseRequiredDate(*fromValue, "--from")
	if err != nil {
		return err
	}
	to, err := parseRequiredDate(*toValue, "--to")
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect ClickHouse: %w", err)
	}
	defer conn.Close()
	selected := selectedSet(*jobsCSV)
	sessions, err := initSelectedSchemas(ctx, conn, cfg, selected, false)
	if err != nil {
		return err
	}
	specs, err := buildJobSpecs(runtimeCfg, *dsn, cfg, selected, sessions)
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
		Runner: runnerConfig{MaxJobConcurrency: 1, OverlapDays: 1, AuditEnabled: true, AuditLookbackDays: 7, AuditMaxFindings: 50, LockTTL: "2h"},
		MarketDataSources: marketDataSourcesConfig{
			USStocks:  "fmp",
			USOptions: "polygon",
		},
		Jobs: map[string]jobConfig{
			// Keep the Gurufocus job for macro CAPE/pe10 only. Symbol-level `pe`
			// for ETF underlyings such as SPY/IWM/QQQ now comes from
			// fmp_etf_fundamentals into fundamental_observation.
			"guru_macro":             {Enabled: true, BatchSize: 1000, URL: macro.DefaultGurufocusShillerURL, ReferenceSymbol: macro.DefaultReferenceSymbol, Dataset: macro.DefaultGurufocusShillerDataset},
			"fmp_sp500_macro":        {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, Dataset: macro.DefaultFMPSP500Dataset, ConstituentUniverse: "sp500", PriceSymbol: "SPY", ReferenceSymbol: "SPY", Workers: 6, BatchSize: 1000, RollingQuarters: 8, MinQuarters: 4, ColdStartFloor: "2023-05-01"},
			"fmp_nasdaq100_macro":    {Enabled: true, DependsOn: []string{"fmp_us_stocks"}, Dataset: macro.DefaultFMPNasdaq100Dataset, ConstituentUniverse: "nasdaq100", PriceSymbol: "QQQ", ReferenceSymbol: "QQQ", Workers: 6, BatchSize: 1000, RollingQuarters: 8, MinQuarters: 4, ColdStartFloor: "2023-05-01"},
			"fmp_crypto_spot":        {Enabled: true, ResolveAtStartup: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
			"fmp_forex":              {Enabled: true, ResolveAtStartup: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
			"fmp_us_stocks":          {Enabled: true, DependsOn: []string{"polygon_us_flatfiles"}, ResolveAtStartup: true, IncludeOptionGapMappings: true, BatchSize: 50000, Interval: string(fmp.Interval1Min)},
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
	stockSource := normalizeMarketDataSource(cfg.MarketDataSources.USStocks)
	if stockSource == "" {
		stockSource = "fmp"
	}
	optionSource := normalizeMarketDataSource(cfg.MarketDataSources.USOptions)
	if optionSource == "" {
		optionSource = "polygon"
	}
	if stockSource != "fmp" && stockSource != "polygon" {
		return fmt.Errorf("market_data_sources.us_stocks must be one of fmp, polygon; got %q", cfg.MarketDataSources.USStocks)
	}
	if optionSource != "polygon" {
		return fmt.Errorf("market_data_sources.us_options must be polygon; got %q", cfg.MarketDataSources.USOptions)
	}
	cfg.MarketDataSources.USStocks = stockSource
	cfg.MarketDataSources.USOptions = optionSource

	polygonJob := cfg.Jobs["polygon_us_flatfiles"]
	polygonJob.Enabled = polygonJob.Enabled || optionSource == "polygon" || stockSource == "polygon"
	polygonJob.SyncStocks = stockSource == "polygon"
	cfg.Jobs["polygon_us_flatfiles"] = polygonJob

	fmpStocksJob := cfg.Jobs["fmp_us_stocks"]
	fmpStocksJob.Enabled = stockSource == "fmp"
	cfg.Jobs["fmp_us_stocks"] = fmpStocksJob

	stockDependency := "fmp_us_stocks"
	if stockSource == "polygon" {
		stockDependency = "polygon_us_flatfiles"
	}

	if job, ok := cfg.Jobs["fmp_us_fundamentals"]; ok && job.Enabled {
		job.DependsOn = replaceDependency(job.DependsOn, "fmp_us_stocks", stockDependency)
		cfg.Jobs["fmp_us_fundamentals"] = job
	}
	for _, name := range []string{"fmp_sp500_macro", "fmp_nasdaq100_macro"} {
		if job, ok := cfg.Jobs[name]; ok && job.Enabled {
			job.DependsOn = replaceDependency(job.DependsOn, "fmp_us_stocks", stockDependency)
			cfg.Jobs[name] = job
		}
	}
	if job, ok := cfg.Jobs["polygon_us_greeks"]; ok && job.Enabled {
		job.DependsOn = replaceDependency(job.DependsOn, "fmp_us_stocks", stockDependency)
		job.DependsOn = ensureDependency(job.DependsOn, "polygon_us_flatfiles")
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func normalizeMarketDataSource(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "polygon_flatfiles", "polygon_flat_files", "polygon-flatfiles", "polygon-flat-files":
		return "polygon"
	default:
		return value
	}
}

func replaceDependency(deps []string, oldName, newName string) []string {
	out := make([]string, 0, len(deps)+1)
	seen := map[string]struct{}{}
	for _, dep := range deps {
		candidate := strings.TrimSpace(dep)
		if candidate == "" {
			continue
		}
		if candidate == oldName {
			candidate = newName
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	if _, ok := seen[newName]; !ok {
		out = append(out, newName)
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

func buildJobSpecs(runtimeCfg config.Runtime, dsn string, cfg pipelineConfig, selected map[string]bool, sessions usmarket.SessionMap) ([]syncpipeline.JobSpec, error) {
	apiKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		return nil, err
	}
	var specs []syncpipeline.JobSpec
	for _, name := range sortedJobNames(cfg) {
		job := cfg.Jobs[name]
		if !shouldIncludeJob(name, job, selected) {
			continue
		}
		syncer, err := buildSyncer(runtimeCfg, name, job, apiKey, dsn, sessions)
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

func buildSyncer(runtimeCfg config.Runtime, name string, job jobConfig, apiKey, dsn string, sessions usmarket.SessionMap) (syncpipeline.Syncer, error) {
	limiterCfg := usmarket.DistributedRateLimitConfig{
		Enabled:      runtimeCfg.Redis.Enabled,
		Addr:         runtimeCfg.Redis.Addr,
		Password:     runtimeCfg.Redis.Password,
		DB:           runtimeCfg.Redis.DB,
		KeyPrefix:    runtimeCfg.Redis.KeyPrefix,
		DialTimeout:  runtimeCfg.RedisDialTimeout(),
		ReadTimeout:  runtimeCfg.RedisReadTimeout(),
		WriteTimeout: runtimeCfg.RedisWriteTimeout(),
	}
	switch name {
	case "fmp_crypto_spot":
		return pipelinejobs.NewFMPCryptoSpot(pipelinejobs.FMPCryptoSpotConfig{APIKey: apiKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, PriceSource: job.PriceSource, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "fmp_forex":
		return pipelinejobs.NewFMPForex(pipelinejobs.FMPForexConfig{APIKey: apiKey, Symbols: job.Symbols, SymbolsFile: job.SymbolsFile, ResolveAtStartup: job.ResolveAtStartup, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "fmp_us_stocks":
		return pipelinejobs.NewFMPUSStocks(pipelinejobs.FMPUSStocksConfig{APIKey: apiKey, Symbols: job.Symbols, ResolveAtStartup: job.ResolveAtStartup, IncludeOptionGapMappings: job.IncludeOptionGapMappings, LimitSymbols: job.LimitSymbols, Interval: fmp.IntradayInterval(job.Interval), BatchSize: job.BatchSize, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "fmp_us_fundamentals":
		return pipelinejobs.NewFMPUSFundamentals(pipelinejobs.FMPUSFundamentalsConfig{Provider: usmarket.NewFMPPEBackfillProvider(apiKey, job.FMPQuarterLimit), DSN: dsn, Symbols: job.Symbols, IncrementalMode: job.IncrementalMode, DiscoveryPageSize: job.DiscoveryPageSize, DiscoveryPageLimit: job.DiscoveryPageLimit, Workers: job.Workers, BatchSize: job.BatchSize, PageSize: job.PageSize, QPS: job.QPS, LimitSymbols: job.LimitSymbols, DistributedLimiter: limiterCfg, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "fmp_etf_fundamentals":
		return pipelinejobs.NewFMPETFFundamentals(pipelinejobs.FMPETFFundamentalsConfig{APIKey: apiKey, DSN: dsn, Symbols: job.Symbols, SymbolMappings: job.SymbolMappings, BatchSize: job.BatchSize, QPS: job.QPS, MinCoverage: job.MinCoverage, DistributedLimiter: limiterCfg, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "fmp_sp500_macro", "fmp_nasdaq100_macro":
		return pipelinejobs.NewGuruMacro(pipelinejobs.GuruMacroConfig{Dataset: job.Dataset, Source: "fmp", ColdStartFloorUTC: parseColdStart(job.ColdStartFloor), SyncFunc: func(ctx context.Context, conn driver.Conn, from, to time.Time, dryRun bool) (int64, error) {
			res, err := macro.SyncFMPIndexShiller(ctx, conn, macro.FMPIndexShillerConfig{APIKey: apiKey, Dataset: job.Dataset, ConstituentUniverse: job.ConstituentUniverse, PriceSymbol: job.PriceSymbol, ReferenceSymbol: job.ReferenceSymbol, BatchSize: job.BatchSize, Workers: job.Workers, RollingQuarters: job.RollingQuarters, MinQuarters: job.MinQuarters}, from, to, dryRun)
			return int64(res.ObservationRows), err
		}})
	case "polygon_us_flatfiles":
		polygonSvc, err := service.NewPolygonServiceFromConfig(runtimeCfg, nil)
		if err != nil {
			return nil, err
		}
		return pipelinejobs.NewPolygonUSFlatFiles(pipelinejobs.PolygonUSFlatFilesConfig{Downloader: polygonSvc, Sessions: sessions, DSN: dsn, BatchSize: job.BatchSize, Workers: job.Workers, RiskFreeRate: job.RiskFreeRate, ForceDownload: job.ForceDownload, SyncStocks: job.SyncStocks, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "polygon_us_greeks":
		return pipelinejobs.NewPolygonUSGreeks(pipelinejobs.PolygonUSGreeksConfig{DSN: dsn, BatchSize: job.BatchSize, Workers: job.Workers, RiskFreeRate: job.RiskFreeRate, Underlyings: job.Underlyings, LimitTasks: job.LimitSymbols, RebuildAggregates: job.RebuildAggregates, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "feature_store_backfill":
		return pipelinejobs.NewFeatureStoreBackfill(pipelinejobs.FeatureStoreBackfillConfig{DSN: dsn, Markets: job.Markets, Underlyings: job.Underlyings, PriorityOrder: job.PriorityOrder, LookbackDays: job.LookbackDays, MinDaysToExpiry: job.MinDaysToExpiry, MaxDaysToExpiry: job.MaxDaysToExpiry, Workers: job.Workers, Replace: job.Replace, ColdStartFloorUTC: parseColdStart(job.ColdStartFloor)})
	case "guru_macro":
		return pipelinejobs.NewGuruMacro(pipelinejobs.GuruMacroConfig{Dataset: job.Dataset, Source: "gurufocus", ColdStartFloorUTC: parseColdStart(job.ColdStartFloor), SyncFunc: func(ctx context.Context, conn driver.Conn, from, to time.Time, dryRun bool) (int64, error) {
			res, err := macro.SyncGurufocusShiller(ctx, conn, macro.GurufocusShillerConfig{URL: job.URL, ReferenceSymbol: job.ReferenceSymbol, BatchSize: job.BatchSize}, from, to, dryRun)
			return int64(res.ObservationRows), err
		}})
	default:
		return nil, fmt.Errorf("unknown job %q", name)
	}
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

func initSelectedSchemas(ctx context.Context, conn driver.Conn, cfg pipelineConfig, selected map[string]bool, enabled bool) (usmarket.SessionMap, error) {
	if !enabled {
		return nil, nil
	}
	needsUSMarket, needsFundamentals, needsForex, needsCrypto, needsFeatureStore := false, false, false, false, false
	for name, job := range cfg.Jobs {
		if !shouldIncludeJob(name, job, selected) {
			continue
		}
		switch name {
		case "fmp_us_stocks", "polygon_us_flatfiles", "polygon_us_greeks", "guru_macro", "fmp_sp500_macro", "fmp_nasdaq100_macro":
			needsUSMarket = true
		case "feature_store_backfill":
			needsFeatureStore = true
			if containsString(job.Markets, "us-options") {
				needsUSMarket = true
			}
			if containsString(job.Markets, "crypto-options") {
				needsCrypto = true
			}
		}
		switch name {
		case "fmp_us_fundamentals", "fmp_etf_fundamentals", "guru_macro", "fmp_sp500_macro", "fmp_nasdaq100_macro":
			needsFundamentals = true
		case "fmp_forex":
			needsForex = true
		case "fmp_crypto_spot":
			needsCrypto = true
		}
	}
	var sessions usmarket.SessionMap
	if needsUSMarket {
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
	if needsFundamentals {
		ddl, err := appCli.ResolveSchemaFile("", appCli.FundamentalsSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := usmarket.InitFundamentalsSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize fundamentals schema: %w", err)
		}
	}
	if needsForex {
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
	if needsCrypto {
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
	if needsFeatureStore {
		ddl, err := appCli.ResolveSchemaFile("", appCli.FeatureStoreSchemaFile)
		if err != nil {
			return nil, err
		}
		if err := cryptooptions.InitSchema(ctx, conn, ddl); err != nil {
			return nil, fmt.Errorf("initialize feature store schema: %w", err)
		}
	}
	return sessions, nil
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

func printPreRunSnapshotAndWait(ctx context.Context, conn driver.Conn, specs []syncpipeline.JobSpec, wait time.Duration) error {
	rows, err := collectPreRunSnapshot(ctx, conn, specs)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "latest records before sync:")
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "  no data tables selected")
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
	case "fmp_us_fundamentals":
		return []snapshotTarget{{Dataset: "US fundamentals", Table: "fundamental_observation", DateExpr: "event_ts", WhereSQL: "market = {market:String} AND factor_code IN ('pe','pb')", Args: []any{clickhouse.Named("market", "us-stocks")}, Qualifier: "pe/pb"}}
	case "guru_macro":
		return []snapshotTarget{{Dataset: "macro", Table: "macro_observation", DateExpr: "event_ts", WhereSQL: "dataset = {dataset:String} AND source = {source:String}", Args: []any{clickhouse.Named("dataset", macro.DefaultGurufocusShillerDataset), clickhouse.Named("source", "gurufocus")}, Qualifier: "gurufocus-shiller"}}
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

func printPostRunOptionCoverageWarnings(ctx context.Context, conn driver.Conn, specs []syncpipeline.JobSpec) error {
	if !needsOptionCoverageWarning(specs) {
		return nil
	}
	missing, err := usmarket.ListUSOptionUnderlyingsMissingStockCoverage(ctx, conn)
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
