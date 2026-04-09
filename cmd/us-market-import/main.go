package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	optionsDir := flag.String("options-dir", "", "Directory containing Polygon OPRA option CSV files (*.csv or *.csv.gz)")
	stocksDir := flag.String("stocks-dir", "", "Directory containing Polygon SIP stock CSV files (*.csv or *.csv.gz)")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	batchSize := flag.Int("batch-size", 100000, "Rows per INSERT batch")
	workers := flag.Int("workers", 2, "Number of parallel file importers")
	riskFreeRate := flag.Float64("risk-free-rate", 0.05, "Annualized risk-free rate used for option greeks")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	skipExisting := flag.Bool("skip-existing", true, "Skip files whose date already has data in ClickHouse")
	flag.Parse()

	if *optionsDir == "" && *stocksDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: us-market-import --options-dir <dir> [--stocks-dir <dir>] [flags]\n")
		fmt.Fprintf(os.Stderr, "  At least one of --options-dir or --stocks-dir is required.\n")
		os.Exit(1)
	}
	if *workers < 1 {
		*workers = 1
	}

	ctx := context.Background()

	// Resolve schema file
	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.UsMarketSchemaFile)
	if err != nil {
		log.Fatalf("resolve us_market.sql schema: %v", err)
	}

	// Connect and init schema
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	sessions, err := usmarket.InitializeImportStorage(ctx, conn, ddlFile)
	if err != nil {
		log.Fatalf("initialize import storage: %v", err)
	}
	log.Printf("Import storage initialized; session calendar loaded (%d entries)", len(sessions))

	// Collect files
	var optionFiles, stockFiles []string
	if *optionsDir != "" {
		optionFiles = usmarket.CollectCSVFiles(*optionsDir)
	}
	if *stocksDir != "" {
		stockFiles = usmarket.CollectCSVFiles(*stocksDir)
	}

	totalFiles := len(optionFiles) + len(stockFiles)
	if totalFiles == 0 {
		log.Fatalf("No CSV files found")
	}
	log.Printf("Found %d option files and %d stock files, importing with %d workers",
		len(optionFiles), len(stockFiles), *workers)

	result, err := usmarket.ImportFiles(ctx, usmarket.ImportConfig{
		DSN:          *dsn,
		BatchSize:    *batchSize,
		Workers:      *workers,
		SkipExisting: *skipExisting,
		RiskFreeRate: *riskFreeRate,
	}, stockFiles, optionFiles, sessions)
	if err != nil {
		log.Fatalf("import files: %v", err)
	}

	log.Printf("Import complete: %d files succeeded, %d skipped, %d failed, %d option rows, %d stock rows, elapsed %s",
		result.CompletedFiles, result.SkippedFiles, result.FailedFiles, result.OptionRows, result.StockRows, result.Elapsed.Round(time.Second))
}
