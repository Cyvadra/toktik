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
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

// Default crypto pair coverage when --symbols is not specified. Matches the
// majors that the existing crypto-options + crypto-spot pipelines already
// quote in USDT.
var defaultCryptoSymbols = []string{"BTCUSD", "ETHUSD", "SOLUSD"}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated FMP pair symbols (e.g. BTCUSD,ETHUSD); defaults to majors")
	startDateFlag := flag.String("start-date", "", "Sync start date (YYYY-MM-DD), required")
	endDateFlag := flag.String("end-date", "", "Sync end date (YYYY-MM-DD); defaults to today UTC")
	intervalFlag := flag.String("interval", "1min", "FMP intraday interval (1min, 5min, 15min, 30min, 1hour, 4hour)")
	batchSize := flag.Int("batch-size", 50000, "Rows per ClickHouse INSERT batch")
	priceSource := flag.String("price-source", cryptooptions.FMPSpotPriceSource, "Value for crypto_spot_bar_1m.price_source")
	dryRun := flag.Bool("dry-run", false, "Fetch and report without inserting into ClickHouse")
	initSchema := flag.Bool("init-schema", true, "Initialize crypto base + spot kline schema before insert")
	schemaFile := flag.String("schema", "", "Path to crypto_options.sql DDL (auto-detected if empty)")
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

	symbols := parseSymbols(*symbolsFlag)
	if len(symbols) == 0 {
		symbols = append(symbols, defaultCryptoSymbols...)
	}

	ctx := context.Background()

	var schema *appCli.SchemaInit
	if *initSchema {
		ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
		if err != nil {
			log.Fatalf("resolve crypto_options.sql schema: %v", err)
		}
		schema = &appCli.SchemaInit{DDLFile: ddlFile, SpotKline: true}
	}

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, schema)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	result, err := cryptooptions.SyncFMPSpotBars(ctx, conn, cryptooptions.FMPSpotSyncConfig{
		APIKey:      apiKey,
		Symbols:     symbols,
		From:        from,
		To:          to,
		Interval:    interval,
		BatchSize:   *batchSize,
		PriceSource: *priceSource,
		DryRun:      *dryRun,
	})
	if err != nil {
		log.Fatalf("sync FMP crypto spot bars: %v", err)
	}

	log.Printf("FMP crypto spot sync complete: processed=%d failed=%d fetched=%d inserted=%d dry_run=%v",
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
