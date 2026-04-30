package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated tickers (e.g. AAPL,MSFT,QQQ); empty means all stored US stock symbols from ClickHouse")
	startDateFlag := flag.String("start-date", "", "Sync start date (YYYY-MM-DD), required")
	endDateFlag := flag.String("end-date", "", "Sync end date (YYYY-MM-DD); defaults to today UTC")
	intervalFlag := flag.String("interval", "1min", "FMP intraday interval (1min, 5min, 15min, 30min, 1hour, 4hour)")
	batchSize := flag.Int("batch-size", 50000, "Rows per ClickHouse INSERT batch")
	limitSymbols := flag.Int("limit-symbols", 0, "Optional limit when resolving all stored US stock symbols from ClickHouse")
	dryRun := flag.Bool("dry-run", false, "Fetch and report without inserting into ClickHouse")
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

	ctx := context.Background()
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

	symbols, err := usmarket.ResolveUSStockSymbols(ctx, conn, parseSymbols(*symbolsFlag), *limitSymbols)
	if err != nil {
		log.Fatalf("resolve US stock symbols: %v", err)
	}
	if len(symbols) == 0 {
		log.Fatal("no US stock symbols resolved; pass --symbols explicitly or seed us_stocks_bar_1m first")
	}
	if strings.TrimSpace(*symbolsFlag) == "" {
		log.Printf("Resolved %d stored US stock symbols from ClickHouse", len(symbols))
	}

	result, err := usmarket.SyncFMPStockKlines(ctx, conn, usmarket.FMPStockKlineSyncConfig{
		APIKey:    apiKey,
		Symbols:   symbols,
		From:      from,
		To:        to,
		Interval:  interval,
		BatchSize: *batchSize,
		DryRun:    *dryRun,
	})
	if err != nil {
		log.Fatalf("sync FMP US stock klines: %v", err)
	}

	log.Printf("FMP US stock kline sync complete: processed=%d failed=%d fetched=%d inserted=%d dry_run=%v",
		result.ProcessedSymbols, result.FailedSymbols, result.FetchedBars, result.InsertedRows, *dryRun)
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
