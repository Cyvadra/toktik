package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/forexmarket"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const defaultWatchlistFile = "signal-list/forex-fmp-watchlist.txt"

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated forex symbols (e.g. EURUSD,USDJPY,XAUUSD)")
	symbolsFile := flag.String("symbols-file", "", "Optional path to newline-delimited forex symbols; defaults to signal-list/forex-fmp-watchlist.txt when --symbols is empty")
	startDateFlag := flag.String("start-date", "", "Sync start date (YYYY-MM-DD), required")
	endDateFlag := flag.String("end-date", "", "Sync end date (YYYY-MM-DD); defaults to today UTC")
	intervalFlag := flag.String("interval", "1min", "FMP intraday interval (1min, 5min, 15min, 30min, 1hour, 4hour)")
	batchSize := flag.Int("batch-size", 50000, "Rows per ClickHouse INSERT batch")
	dryRun := flag.Bool("dry-run", false, "Fetch and report without inserting into ClickHouse")
	replace := flag.Bool("replace", false, "Delete existing forex 1m rows and precomputed aggregates for each symbol/date scope before inserting")
	schemaFile := flag.String("schema", "", "Path to forex_market.sql DDL (auto-detected if empty)")
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

	symbols, resolvedFrom, err := resolveSymbols(*symbolsFlag, *symbolsFile)
	if err != nil {
		log.Fatalf("resolve forex symbols: %v", err)
	}
	if len(symbols) == 0 {
		log.Fatal("no forex symbols resolved; pass --symbols or provide a non-empty watchlist file")
	}
	log.Printf("Resolved %d forex symbols from %s", len(symbols), resolvedFrom)

	ctx := context.Background()
	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.ForexMarketSchemaFile)
	if err != nil {
		log.Fatalf("resolve forex_market.sql schema: %v", err)
	}

	conn, err := forexmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if err := forexmarket.InitSchema(ctx, conn, ddlFile); err != nil {
		log.Fatalf("initialize forex storage: %v", err)
	}

	result, err := forexmarket.SyncFMPKlines(ctx, conn, forexmarket.FMPKlineSyncConfig{
		APIKey:    apiKey,
		Symbols:   symbols,
		From:      from,
		To:        to,
		Interval:  interval,
		BatchSize: *batchSize,
		DryRun:    *dryRun,
		Replace:   *replace,
	})
	if err != nil {
		log.Fatalf("sync FMP forex klines: %v", err)
	}

	log.Printf("FMP forex kline sync complete: processed=%d failed=%d fetched=%d inserted=%d dry_run=%v replace=%v",
		result.ProcessedSymbols, result.FailedSymbols, result.FetchedBars, result.InsertedRows, *dryRun, *replace)
	if len(result.ThrottledSymbols) > 0 {
		log.Printf("FMP 429 throttled symbols (%d): %s", len(result.ThrottledSymbols), strings.Join(result.ThrottledSymbols, ","))
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

func isValidIntradayInterval(iv fmp.IntradayInterval) bool {
	switch iv {
	case fmp.Interval1Min, fmp.Interval5Min, fmp.Interval15Min,
		fmp.Interval30Min, fmp.Interval1Hour, fmp.Interval4Hour:
		return true
	}
	return false
}

func resolveSymbols(rawSymbols, rawFile string) ([]string, string, error) {
	if strings.TrimSpace(rawSymbols) != "" {
		return parseSymbols(rawSymbols), "--symbols", nil
	}

	path := strings.TrimSpace(rawFile)
	if path == "" {
		path = defaultWatchlistFile
	}
	resolved, err := readSymbolFile(path)
	if err != nil {
		return nil, "", err
	}
	return resolved, path, nil
}

func parseSymbols(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}

func readSymbolFile(path string) ([]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open symbols file %s: %w", path, err)
	}
	defer file.Close()

	seen := make(map[string]struct{})
	var symbols []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		symbol := strings.ToUpper(strings.TrimSpace(line))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan symbols file %s: %w", path, err)
	}
	sort.Strings(symbols)
	return symbols, nil
}
