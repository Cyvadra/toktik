package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated logical symbols (e.g. AAPL,SPX,BRKB); empty means stored US stock symbols plus supported option-underlying gaps")
	startDateFlag := flag.String("start-date", "", "Sync start date (YYYY-MM-DD), required")
	endDateFlag := flag.String("end-date", "", "Sync end date (YYYY-MM-DD); defaults to today UTC")
	intervalFlag := flag.String("interval", "1min", "FMP intraday interval (1min, 5min, 15min, 30min, 1hour, 4hour)")
	batchSize := flag.Int("batch-size", 50000, "Rows per ClickHouse INSERT batch")
	limitSymbols := flag.Int("limit-symbols", 0, "Optional limit when resolving all stored US stock symbols from ClickHouse")
	includeOptionGapMappings := flag.Bool("include-option-gap-mappings", true, "When --symbols is empty, append deterministic option-underlying gap targets backed by direct/index/fallback mappings")
	dryRun := flag.Bool("dry-run", false, "Fetch and report without inserting into ClickHouse")
	replace := flag.Bool("replace", false, "Delete existing 1m rows for each symbol in the date range before re-inserting, then regenerate all kline aggregates (1d, 4h, etc.) from scratch for the range")
	schemaFile := flag.String("schema", "", "Path to us_market.sql DDL (auto-detected if empty)")
	flag.Parse()

	apiKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		log.Fatalf("load FMP config: %v", err)
	}
	if strings.TrimSpace(apiKey) == "" {
		log.Fatal("FMP API key is required in runtime config (fmp.api_key) or FMP_API_KEY env")
	}

	from, to := mustParseDateRange(*startDateFlag, *endDateFlag)
	interval := fmp.IntradayInterval(strings.TrimSpace(*intervalFlag))
	if !isValidIntradayInterval(interval) {
		log.Fatalf("invalid --interval %q; must be one of 1min,5min,15min,30min,1hour,4hour", *intervalFlag)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.UsMarketSchemaFile)
	if err != nil {
		log.Fatalf("resolve us_market.sql schema: %v", err)
	}

	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if _, err := usmarket.InitializeImportStorageWithOptions(ctx, conn, ddlFile, usmarket.ImportStorageOptions{}); err != nil {
		log.Fatalf("initialize import storage: %v", err)
	}

	targets, err := usmarket.ResolveUSStockSyncTargets(ctx, conn, parseSymbols(*symbolsFlag), *limitSymbols, *includeOptionGapMappings)
	if err != nil {
		log.Fatalf("resolve US stock sync targets: %v", err)
	}
	if len(targets) == 0 {
		log.Fatal("no US stock sync targets resolved; pass --symbols explicitly or seed us_stocks_bar_1m first")
	}
	if strings.TrimSpace(*symbolsFlag) == "" {
		extraTargets := 0
		for _, target := range targets {
			if target.Source != "stored-stock" {
				extraTargets++
			}
		}
		log.Printf("Resolved %d US stock sync targets from ClickHouse (%d stored-stock, %d mapped option-underlying gaps)", len(targets), len(targets)-extraTargets, extraTargets)
	}

	result, err := usmarket.SyncFMPStockKlines(ctx, conn, usmarket.FMPStockKlineSyncConfig{
		APIKey:    apiKey,
		Targets:   targets,
		From:      from,
		To:        to,
		Interval:  interval,
		BatchSize: *batchSize,
		DryRun:    *dryRun,
		Replace:   *replace,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Printf("sync FMP US stock klines interrupted")
			os.Exit(130)
		}
		log.Fatalf("sync FMP US stock klines: %v", err)
	}

	log.Printf("FMP US stock kline sync complete: processed=%d failed=%d fetched=%d inserted=%d dry_run=%v replace=%v",
		result.ProcessedSymbols, result.FailedSymbols, result.FetchedBars, result.InsertedRows, *dryRun, *replace)
	if len(result.ThrottledSymbols) > 0 {
		log.Printf("FMP 429 throttled symbols (%d): %s", len(result.ThrottledSymbols), strings.Join(result.ThrottledSymbols, ","))
	}

	// After a replace-sync, regenerate all kline aggregates (5m → 1d) from the
	// freshly imported 1m data.  The window [from, to] is inclusive on both ends;
	// BackfillKlineWindows expects an exclusive upper bound, so add one day.
	// We intentionally omit an asset filter here so that every symbol whose 1m
	// data overlaps the window is rebuilt — this fixes any stale partial-state
	// entries left in the aggregate tables by prior runs.
	//
	// Note: bars are aggregated from whatever regular-session 1m rows exist for a
	// day, so a daily bar is produced whenever >0 bars are available — partial
	// data (e.g. >50% of intraday bars) is fully sufficient.
	if *replace && !*dryRun {
		backfillTo := to.AddDate(0, 0, 1) // exclusive upper bound
		log.Printf("replace=true: regenerating kline aggregates for %s..%s", from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err := usmarket.BackfillKlineWindows(ctx, conn, usmarket.KlineBackfillOptions{
			From:    from,
			To:      backfillTo,
			Replace: true,
		}); err != nil {
			if errors.Is(err, context.Canceled) {
				log.Printf("kline aggregate regeneration interrupted")
				os.Exit(130)
			}
			log.Fatalf("backfill kline windows after replace-sync: %v", err)
		}
		log.Printf("kline aggregate regeneration complete")
	}
}

func mustParseDateRange(startStr, endStr string) (time.Time, time.Time) {
	if strings.TrimSpace(startStr) == "" {
		fmt.Fprintln(os.Stderr, "--start-date is required (YYYY-MM-DD)")
		os.Exit(1)
	}
	from := appCli.ParseDate(startStr, "--start-date")
	var to time.Time
	if strings.TrimSpace(endStr) == "" {
		to = time.Now().UTC().Truncate(24 * time.Hour)
	} else {
		to = appCli.ParseDate(endStr, "--end-date")
	}
	if to.Before(from) {
		fmt.Fprintln(os.Stderr, "--end-date must be on or after --start-date")
		os.Exit(1)
	}
	return from, to
}

func parseSymbols(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		s := strings.ToUpper(strings.TrimSpace(p))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func isValidIntradayInterval(iv fmp.IntradayInterval) bool {
	switch iv {
	case fmp.Interval1Min, fmp.Interval5Min, fmp.Interval15Min,
		fmp.Interval30Min, fmp.Interval1Hour, fmp.Interval4Hour:
		return true
	}
	return false
}
