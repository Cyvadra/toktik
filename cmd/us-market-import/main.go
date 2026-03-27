package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	optionsDir := flag.String("options-dir", "", "Directory containing Polygon OPRA option CSV files (*.csv or *.csv.gz)")
	stocksDir := flag.String("stocks-dir", "", "Directory containing Polygon SIP stock CSV files (*.csv or *.csv.gz)")
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
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
	ddlFile := *schemaFile
	if ddlFile == "" {
		candidates := []string{
			"schema/clickhouse/us_market.sql",
			"../schema/clickhouse/us_market.sql",
			"../../schema/clickhouse/us_market.sql",
		}
		found, err := appCli.FindSchemaFile(candidates)
		if err != nil {
			log.Fatalf("Cannot find us_market.sql schema: %v", err)
		}
		ddlFile = found
	}

	// Connect and init schema
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if err := usmarket.InitSchema(ctx, conn, ddlFile); err != nil {
		log.Fatalf("init schema: %v", err)
	}
	if err := usmarket.EnsureOptionGreeksColumns(ctx, conn); err != nil {
		log.Fatalf("ensure option greeks columns: %v", err)
	}
	log.Println("Base schema initialized")

	if err := usmarket.InitOptionKlineSchema(ctx, conn); err != nil {
		log.Fatalf("init option kline schema: %v", err)
	}
	if err := usmarket.InitStockKlineSchema(ctx, conn); err != nil {
		log.Fatalf("init stock kline schema: %v", err)
	}
	log.Println("Kline schemas initialized")

	// Collect files
	var optionFiles, stockFiles []string
	if *optionsDir != "" {
		optionFiles = collectCSVFiles(*optionsDir)
	}
	if *stocksDir != "" {
		stockFiles = collectCSVFiles(*stocksDir)
	}

	totalFiles := len(optionFiles) + len(stockFiles)
	if totalFiles == 0 {
		log.Fatalf("No CSV files found")
	}
	log.Printf("Found %d option files and %d stock files, importing with %d workers",
		len(optionFiles), len(stockFiles), *workers)

	var (
		wg         sync.WaitGroup
		sem        = make(chan struct{}, *workers)
		completed  int64
		skipped    int64
		failed     int64
		optionRows int64
		stockRows  int64
	)

	startTime := time.Now()

	// Import stock files first so option greeks can be derived from underlying bars.
	for _, csvPath := range stockFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, wasSkipped, err := importStockFile(ctx, *dsn, path, *batchSize, *skipExisting)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else if wasSkipped {
				n := atomic.AddInt64(&skipped, 1)
				done := atomic.LoadInt64(&completed)
				log.Printf("[SKIP] %s (total: %d done, %d skipped / %d)", filepath.Base(path), done, n, totalFiles)
			} else {
				atomic.AddInt64(&stockRows, rows)
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %s: %d rows (%d/%d completed)", filepath.Base(path), rows, n, totalFiles)
			}
		}(csvPath)
	}

	// Import option files after stock data is available.
	for _, csvPath := range optionFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, wasSkipped, err := importOptionFile(ctx, *dsn, path, *batchSize, *skipExisting, *riskFreeRate)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else if wasSkipped {
				n := atomic.AddInt64(&skipped, 1)
				done := atomic.LoadInt64(&completed)
				log.Printf("[SKIP] %s (total: %d done, %d skipped / %d)", filepath.Base(path), done, n, totalFiles)
			} else {
				atomic.AddInt64(&optionRows, rows)
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %s: %d rows (%d/%d completed)", filepath.Base(path), rows, n, totalFiles)
			}
		}(csvPath)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	log.Printf("Import complete: %d files succeeded, %d skipped, %d failed, %d option rows, %d stock rows, elapsed %s",
		completed, skipped, failed, optionRows, stockRows, elapsed.Round(time.Second))
}

func collectCSVFiles(dir string) []string {
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Warning: cannot read directory %s: %v", dir, err)
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".csv") || strings.HasSuffix(name, ".csv.gz") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	sort.Strings(files)
	return files
}

// extractDateFromFilename parses a date from filenames like "2023-01-03.csv" or "2023-01-03.csv.gz".
func extractDateFromFilename(path string) (time.Time, error) {
	base := filepath.Base(path)
	// Strip extensions
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".gz"), ".csv")
	t, err := time.Parse("2006-01-02", name)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse date from filename %q: %w", base, err)
	}
	return t.UTC(), nil
}

func importOptionFile(ctx context.Context, dsn, path string, batchSize int, skipExisting bool, riskFreeRate float64) (int64, bool, error) {
	baseName := filepath.Base(path)
	log.Printf("[START] option %s", baseName)
	fileStart := time.Now()

	conn, err := usmarket.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	fileDate, err := extractDateFromFilename(path)
	if err != nil {
		return 0, false, fmt.Errorf("extract file date: %w", err)
	}
	nextDay := fileDate.AddDate(0, 0, 1)

	if skipExisting {
		count, err := usmarket.CountExistingOptionBars(ctx, conn, fileDate, nextDay)
		if err != nil {
			return 0, false, fmt.Errorf("check existing: %w", err)
		}
		if count > 0 {
			return 0, true, nil
		}
	}

	stockCloses, err := usmarket.ValidateOptionStockCoverage(ctx, conn, path, fileDate)
	if err != nil {
		return 0, false, fmt.Errorf("validate stock coverage: %w", err)
	}

	barCh, readErr, err := usmarket.ParseOptionCSV(path)
	if err != nil {
		return 0, false, fmt.Errorf("parse CSV: %w", err)
	}

	enrichedBarCh, enrichErr := usmarket.EnrichOptionBarsWithGreeks(barCh, stockCloses, usmarket.GreeksConfig{RiskFreeRate: riskFreeRate})

	rows, err := usmarket.InsertOptionBars(ctx, conn, enrichedBarCh, batchSize)
	if err != nil {
		return rows, false, fmt.Errorf("insert: %w", err)
	}
	if *readErr != nil {
		return rows, false, fmt.Errorf("read: %w", *readErr)
	}
	if *enrichErr != nil {
		return rows, false, fmt.Errorf("greeks: %w", *enrichErr)
	}

	log.Printf("[IMPORT] %s: %d option rows with greeks in %s", baseName, rows, time.Since(fileStart).Round(time.Second))
	return rows, false, nil
}

func importStockFile(ctx context.Context, dsn, path string, batchSize int, skipExisting bool) (int64, bool, error) {
	baseName := filepath.Base(path)
	log.Printf("[START] stock %s", baseName)
	fileStart := time.Now()

	conn, err := usmarket.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	if skipExisting {
		fileDate, err := extractDateFromFilename(path)
		if err == nil {
			nextDay := fileDate.AddDate(0, 0, 1)
			count, err := usmarket.CountExistingStockBars(ctx, conn, fileDate, nextDay)
			if err != nil {
				return 0, false, fmt.Errorf("check existing: %w", err)
			}
			if count > 0 {
				return 0, true, nil
			}
		}
	}

	barCh, readErr, err := usmarket.ParseStockCSV(path)
	if err != nil {
		return 0, false, fmt.Errorf("parse CSV: %w", err)
	}

	rows, err := usmarket.InsertStockBars(ctx, conn, barCh, batchSize)
	if err != nil {
		return rows, false, fmt.Errorf("insert: %w", err)
	}
	if *readErr != nil {
		return rows, false, fmt.Errorf("read: %w", *readErr)
	}

	log.Printf("[IMPORT] %s: %d stock rows in %s", baseName, rows, time.Since(fileStart).Round(time.Second))
	return rows, false, nil
}
