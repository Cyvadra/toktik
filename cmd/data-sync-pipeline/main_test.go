package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
)

func TestDefaultPipelineConfigEnablesETFFundamentalsForSPYAndIWM(t *testing.T) {
	cfg := defaultPipelineConfig()
	job, ok := cfg.Jobs["fmp_etf_fundamentals"]
	if !ok {
		t.Fatal("expected fmp_etf_fundamentals job in default config")
	}
	if !job.Enabled {
		t.Fatal("expected fmp_etf_fundamentals to be enabled by default")
	}
	wantSymbols := map[string]bool{"SPY": false, "IWM": false}
	for _, symbol := range job.Symbols {
		if _, ok := wantSymbols[symbol]; ok {
			wantSymbols[symbol] = true
		}
	}
	for symbol, found := range wantSymbols {
		if !found {
			t.Fatalf("expected default ETF fundamentals symbols to include %s, got %#v", symbol, job.Symbols)
		}
	}
}

func TestDefaultPipelineConfigEnablesFMPMacroJobs(t *testing.T) {
	cfg := defaultPipelineConfig()
	for _, name := range []string{"fmp_sp500_macro", "fmp_nasdaq100_macro"} {
		job, ok := cfg.Jobs[name]
		if !ok {
			t.Fatalf("expected %s job in default config", name)
		}
		if !job.Enabled {
			t.Fatalf("expected %s enabled by default", name)
		}
		if job.ReferenceSymbol == "" || job.PriceSymbol == "" || job.ConstituentUniverse == "" {
			t.Fatalf("expected %s to define reference_symbol, price_symbol, and constituent_universe", name)
		}
		if job.RollingQuarters != 40 || job.MinQuarters != 40 {
			t.Fatalf("expected %s to use a full 10-year window, got rolling=%d min=%d", name, job.RollingQuarters, job.MinQuarters)
		}
		if job.ColdStartFloor != "2016-01-01" {
			t.Fatalf("expected %s cold_start_floor=2016-01-01, got %q", name, job.ColdStartFloor)
		}
	}
}

func TestDefaultPipelineConfigDefinesFeatureStoreBackfillJob(t *testing.T) {
	cfg := defaultPipelineConfig()
	job, ok := cfg.Jobs["feature_store_backfill"]
	if !ok {
		t.Fatal("expected feature_store_backfill job in default config")
	}
	if job.Enabled {
		t.Fatal("expected feature_store_backfill disabled by default")
	}
	if job.Workers != 4 {
		t.Fatalf("expected feature_store_backfill workers=4, got %d", job.Workers)
	}
	if job.PriorityOrder != "us-default" {
		t.Fatalf("expected feature_store_backfill priority_order=us-default, got %q", job.PriorityOrder)
	}
	if len(job.Markets) != 1 || job.Markets[0] != "us-options" {
		t.Fatalf("unexpected feature_store_backfill markets: %#v", job.Markets)
	}
}

func TestFormatOptionCoverageWarningSymbols(t *testing.T) {
	got := formatOptionCoverageWarningSymbols([]string{"ACHHY", "AKTSQ", "ALLGF"})
	want := "ACHHY, \tAKTSQ, \tALLGF"
	if got != want {
		t.Fatalf("unexpected warning format: got=%q want=%q", got, want)
	}
}

func TestFormatOptionCoverageWarningSymbolsTruncates(t *testing.T) {
	var missing []string
	for i := 0; i < 52; i++ {
		missing = append(missing, "SYM")
	}
	got := formatOptionCoverageWarningSymbols(missing)
	if strings.Count(got, "SYM") != 50 {
		t.Fatalf("expected 50 preview symbols, got %q", got)
	}
	if !strings.Contains(got, "... (+2 more)") {
		t.Fatalf("expected omitted count, got %q", got)
	}
}

func TestNormalizePipelineConfigSelectsPolygonUSStocks(t *testing.T) {
	cfg := defaultPipelineConfig()
	cfg.MarketDataSources.USStocks = "polygon"
	if err := normalizePipelineConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Jobs["fmp_us_stocks"].Enabled {
		t.Fatal("expected fmp_us_stocks disabled when US stocks source is Polygon")
	}
	polygonJob := cfg.Jobs["polygon_us_flatfiles"]
	if !polygonJob.Enabled {
		t.Fatal("expected polygon_us_flatfiles enabled")
	}
	if !polygonJob.SyncStocks {
		t.Fatal("expected polygon_us_flatfiles.sync_stocks enabled")
	}
	if got := strings.Join(cfg.Jobs["polygon_us_greeks"].DependsOn, ","); got != "polygon_us_flatfiles" {
		t.Fatalf("expected greeks to depend only on polygon flatfiles, got %q", got)
	}
	if got := strings.Join(cfg.Jobs["fmp_us_fundamentals"].DependsOn, ","); got != "polygon_us_flatfiles" {
		t.Fatalf("expected fundamentals to depend on polygon stock bars, got %q", got)
	}
	if got := strings.Join(cfg.Jobs["fmp_sp500_macro"].DependsOn, ","); got != "polygon_us_flatfiles" {
		t.Fatalf("expected fmp_sp500_macro to depend on polygon stock bars, got %q", got)
	}
	if got := strings.Join(cfg.Jobs["fmp_nasdaq100_macro"].DependsOn, ","); got != "polygon_us_flatfiles" {
		t.Fatalf("expected fmp_nasdaq100_macro to depend on polygon stock bars, got %q", got)
	}
}

func TestLoadPipelineConfigAcceptsPolygonSourceAliases(t *testing.T) {
	path := writeTempPipelineConfig(t, `
market_data_sources:
  us_stocks: polygon_flatfiles
  us_options: polygon
jobs:
  fmp_us_stocks:
    enabled: true
  polygon_us_flatfiles:
    enabled: false
`)
	cfg, err := loadPipelineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MarketDataSources.USStocks != "polygon" {
		t.Fatalf("expected normalized polygon source, got %q", cfg.MarketDataSources.USStocks)
	}
	if cfg.Jobs["fmp_us_stocks"].Enabled {
		t.Fatal("expected source selection to disable fmp_us_stocks")
	}
	if !cfg.Jobs["polygon_us_flatfiles"].Enabled || !cfg.Jobs["polygon_us_flatfiles"].SyncStocks {
		t.Fatalf("expected polygon flatfiles enabled with stock sync, got %#v", cfg.Jobs["polygon_us_flatfiles"])
	}
}

func TestLoadPipelineConfigAllowsPolygonStockOnlyFlatfiles(t *testing.T) {
	path := writeTempPipelineConfig(t, `
market_data_sources:
  us_stocks: polygon
  us_options: polygon
jobs:
  polygon_us_flatfiles:
    sync_options: false
`)
	cfg, err := loadPipelineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	job := cfg.Jobs["polygon_us_flatfiles"]
	if !job.Enabled || !job.SyncStocks {
		t.Fatalf("expected polygon flatfiles enabled with stock sync, got %#v", job)
	}
	if job.SyncOptions == nil || *job.SyncOptions {
		t.Fatalf("expected polygon flatfiles sync_options=false, got %#v", job.SyncOptions)
	}
}

func TestLoadPipelineConfigRejectsUnsupportedUSStockSource(t *testing.T) {
	path := writeTempPipelineConfig(t, `
market_data_sources:
  us_stocks: unknown
`)
	_, err := loadPipelineConfig(path)
	if err == nil || !strings.Contains(err.Error(), "market_data_sources.us_stocks") {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
}

func TestLoadPipelineConfigAcceptsLegacyMaxJobConcurrency(t *testing.T) {
	path := writeTempPipelineConfig(t, `
runner:
  max_job_concurrency: 3
`)
	cfg, err := loadPipelineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.MaxSourceConcurrency != 3 {
		t.Fatalf("expected legacy max_job_concurrency to populate max_source_concurrency, got %d", cfg.Runner.MaxSourceConcurrency)
	}
}

func TestLoadPipelineConfigPrefersMaxSourceConcurrency(t *testing.T) {
	path := writeTempPipelineConfig(t, `
runner:
  max_source_concurrency: 4
  max_job_concurrency: 2
`)
	cfg, err := loadPipelineConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.MaxSourceConcurrency != 4 {
		t.Fatalf("expected max_source_concurrency to win, got %d", cfg.Runner.MaxSourceConcurrency)
	}
}

func TestMissingSelectedDependencyWarningsKeepsLegacySelectionBehavior(t *testing.T) {
	cfg := pipelineConfig{Jobs: map[string]jobConfig{
		"parent": {Enabled: true},
		"child":  {Enabled: true, DependsOn: []string{"parent"}},
	}}
	warnings := missingSelectedDependencyWarnings(cfg, map[string]bool{"child": true})
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %#v", warnings)
	}
	if !strings.Contains(warnings[0], "selected job child depends on parent") {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}
}

func TestResolveDBRetryOptionsUsesConfig(t *testing.T) {
	opts, err := resolveDBRetryOptions(runnerConfig{DBRetryMaxAttempts: 5, DBRetryInitialDelay: "10s", DBRetryMaxDelay: "2m"}, 0, 0, 0)
	if err != nil {
		t.Fatalf("resolveDBRetryOptions() returned error: %v", err)
	}
	if opts.MaxAttempts != 5 || opts.InitialDelay != 10*time.Second || opts.MaxDelay != 2*time.Minute {
		t.Fatalf("unexpected retry options: %#v", opts)
	}
}

func TestResolveDBRetryOptionsPrefersOverrides(t *testing.T) {
	opts, err := resolveDBRetryOptions(runnerConfig{DBRetryMaxAttempts: 5, DBRetryInitialDelay: "10s", DBRetryMaxDelay: "2m"}, 2, time.Second, 3*time.Second)
	if err != nil {
		t.Fatalf("resolveDBRetryOptions() returned error: %v", err)
	}
	if opts.MaxAttempts != 2 || opts.InitialDelay != time.Second || opts.MaxDelay != 3*time.Second {
		t.Fatalf("unexpected retry options: %#v", opts)
	}
}

func TestResolveSchemaRequirementsIncludesFeatureStoreDependencies(t *testing.T) {
	requirements := resolveSchemaRequirements(pipelineConfig{Jobs: map[string]jobConfig{
		"feature_store_backfill": {Enabled: true, Markets: []string{"us-options", "crypto-options"}},
	}}, nil)
	if !requirements.NeedsFeatureStore {
		t.Fatal("expected feature store schema to be required")
	}
	if !requirements.NeedsUSMarket {
		t.Fatal("expected us-market schema to be required for us-options feature store backfill")
	}
	if !requirements.NeedsCrypto {
		t.Fatal("expected crypto schema to be required for crypto-options feature store backfill")
	}
}

func TestResolveSourceConcurrencyUsesOverrideAndMinimum(t *testing.T) {
	if got := resolveSourceConcurrency(runnerConfig{MaxSourceConcurrency: 4}, 2); got != 2 {
		t.Fatalf("expected override to win, got %d", got)
	}
	if got := resolveSourceConcurrency(runnerConfig{}, 0); got != 1 {
		t.Fatalf("expected minimum concurrency of 1, got %d", got)
	}
}

func TestOptionalBoolFlagTracksExplicitOverride(t *testing.T) {
	var flag optionalBoolFlag
	if flag.set {
		t.Fatal("expected unset flag initially")
	}
	if err := flag.Set("false"); err != nil {
		t.Fatal(err)
	}
	if !flag.set || flag.value {
		t.Fatalf("expected explicit false, got %#v", flag)
	}
	if err := flag.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !flag.value {
		t.Fatalf("expected explicit true, got %#v", flag)
	}
	if err := flag.Set("maybe"); err == nil {
		t.Fatal("expected invalid bool error")
	}
}

func TestSnapshotTargetsForPolygonFlatfilesIncludesStocksWhenSyncerAuditsStocks(t *testing.T) {
	targets := snapshotTargetsForJob(syncpipeline.JobSpec{
		Name:   "polygon_us_flatfiles",
		Syncer: snapshotSyncer{targets: []syncpipeline.AuditTarget{{Table: "us_options_bar_1m"}, {Table: "us_stocks_bar_1m"}}},
	})
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %#v", targets)
	}
	if targets[0].Dataset != "US stocks" || targets[1].Dataset != "US options" {
		t.Fatalf("unexpected targets: %#v", targets)
	}
}

func TestSnapshotTargetsForFMPUSStockSplitsUsesUpdatedAt(t *testing.T) {
	targets := snapshotTargetsForJob(syncpipeline.JobSpec{Name: "fmp_us_stock_splits"})
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %#v", targets)
	}
	if targets[0].DateExpr != "updated_at" {
		t.Fatalf("expected updated_at snapshot target, got %#v", targets)
	}
}

type snapshotSyncer struct {
	targets []syncpipeline.AuditTarget
}

func (s snapshotSyncer) Name() string                                              { return "snapshot" }
func (s snapshotSyncer) SourceKeys(context.Context, driver.Conn) ([]string, error) { return nil, nil }
func (s snapshotSyncer) ResolveCursor(context.Context, driver.Conn, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (s snapshotSyncer) ColdStartFloor(string) time.Time { return time.Time{} }
func (s snapshotSyncer) Sync(context.Context, driver.Conn, syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	return syncpipeline.SyncResult{}, nil
}
func (s snapshotSyncer) AuditTargets(string) []syncpipeline.AuditTarget { return s.targets }
func (s snapshotSyncer) MaxConcurrency() int                            { return 1 }

func writeTempPipelineConfig(t *testing.T, content string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "pipeline-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}
