package usmarket

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type ImportConfig struct {
	DSN          string
	BatchSize    int
	Workers      int
	SkipExisting bool
	RiskFreeRate float64
}

type ImportResult struct {
	CompletedFiles int64
	SkippedFiles   int64
	FailedFiles    int64
	OptionRows     int64
	StockRows      int64
	Elapsed        time.Duration
}

func InitializeImportStorage(ctx context.Context, conn driver.Conn, ddlPath string) (SessionMap, error) {
	if err := InitSchema(ctx, conn, ddlPath); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}
	if err := EnsureOptionGreeksColumns(ctx, conn); err != nil {
		return nil, fmt.Errorf("ensure option greeks columns: %w", err)
	}
	if err := EnsureSessionColumns(ctx, conn); err != nil {
		return nil, fmt.Errorf("ensure session columns: %w", err)
	}
	if err := InitSessionCalendar(ctx, conn, 2003, 2035); err != nil {
		return nil, fmt.Errorf("init session calendar: %w", err)
	}
	sessions, err := LoadSessionMap(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("load session map: %w", err)
	}
	if err := InitOptionKlineSchema(ctx, conn); err != nil {
		return nil, fmt.Errorf("init option kline schema: %w", err)
	}
	if err := InitOptionChainCacheSchema(ctx, conn); err != nil {
		return nil, fmt.Errorf("init option chain cache schema: %w", err)
	}
	if err := InitStockKlineSchema(ctx, conn); err != nil {
		return nil, fmt.Errorf("init stock kline schema: %w", err)
	}
	return sessions, nil
}

func ImportFiles(ctx context.Context, cfg ImportConfig, stockFiles, optionFiles []string, sessions SessionMap) (ImportResult, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return ImportResult{}, fmt.Errorf("import DSN is required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100000
	}
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}

	totalFiles := len(stockFiles) + len(optionFiles)
	if totalFiles == 0 {
		return ImportResult{}, nil
	}

	var (
		wg         sync.WaitGroup
		sem        = make(chan struct{}, cfg.Workers)
		completed  int64
		skipped    int64
		failed     int64
		stockRows  int64
		optionRows int64
	)

	startTime := time.Now()

	for _, csvPath := range stockFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, wasSkipped, err := ImportStockFile(ctx, cfg.DSN, path, cfg.BatchSize, cfg.SkipExisting, sessions)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
				return
			}
			if wasSkipped {
				n := atomic.AddInt64(&skipped, 1)
				done := atomic.LoadInt64(&completed)
				log.Printf("[SKIP] %s (total: %d done, %d skipped / %d)", filepath.Base(path), done, n, totalFiles)
				return
			}
			atomic.AddInt64(&stockRows, rows)
			n := atomic.AddInt64(&completed, 1)
			log.Printf("[DONE] %s: %d rows (%d/%d completed)", filepath.Base(path), rows, n, totalFiles)
		}(csvPath)
	}

	wg.Wait()
	log.Printf("Stock import phase complete: %d files succeeded, %d skipped, %d failed", completed, skipped, failed)
	if failed > 0 {
		return ImportResult{
			CompletedFiles: completed,
			SkippedFiles:   skipped,
			FailedFiles:    failed,
			OptionRows:     optionRows,
			StockRows:      stockRows,
			Elapsed:        time.Since(startTime),
		}, fmt.Errorf("stock import phase failed; refusing to import options against incomplete underlying data")
	}

	for _, csvPath := range optionFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, wasSkipped, err := ImportOptionFile(ctx, cfg.DSN, path, cfg.BatchSize, cfg.SkipExisting, cfg.RiskFreeRate, sessions)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
				return
			}
			if wasSkipped {
				n := atomic.AddInt64(&skipped, 1)
				done := atomic.LoadInt64(&completed)
				log.Printf("[SKIP] %s (total: %d done, %d skipped / %d)", filepath.Base(path), done, n, totalFiles)
				return
			}
			atomic.AddInt64(&optionRows, rows)
			n := atomic.AddInt64(&completed, 1)
			log.Printf("[DONE] %s: %d rows (%d/%d completed)", filepath.Base(path), rows, n, totalFiles)
		}(csvPath)
	}

	wg.Wait()

	result := ImportResult{
		CompletedFiles: completed,
		SkippedFiles:   skipped,
		FailedFiles:    failed,
		OptionRows:     optionRows,
		StockRows:      stockRows,
		Elapsed:        time.Since(startTime),
	}
	if failed > 0 {
		return result, fmt.Errorf("import finished with %d failed files", failed)
	}
	return result, nil
}

func CollectCSVFiles(dir string) []string {
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

func ExtractDateFromFilename(path string) (time.Time, error) {
	base := filepath.Base(path)
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".gz"), ".csv")
	t, err := time.Parse("2006-01-02", name)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse date from filename %q: %w", base, err)
	}
	return t.UTC(), nil
}

func ImportOptionFile(ctx context.Context, dsn, path string, batchSize int, skipExisting bool, riskFreeRate float64, sessions SessionMap) (int64, bool, error) {
	baseName := filepath.Base(path)
	log.Printf("[START] option %s", baseName)
	fileStart := time.Now()

	conn, err := ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	marketDate, err := ExtractDateFromFilename(path)
	if err != nil {
		return 0, false, fmt.Errorf("extract market date: %w", err)
	}

	if skipExisting {
		count, err := CountExistingOptionBars(ctx, conn, marketDate)
		if err != nil {
			return 0, false, fmt.Errorf("check existing: %w", err)
		}
		if count > 0 {
			return 0, true, nil
		}
	}

	stockCloses, missingSymbols, err := ValidateOptionStockCoverage(ctx, conn, path, marketDate)
	if err != nil {
		return 0, false, fmt.Errorf("validate stock coverage: %w", err)
	}
	if len(missingSymbols) > 0 {
		log.Printf("[WARN] %s: missing stock coverage for %d underlyings, affected option bars will be imported with NaN greeks: %v", baseName, len(missingSymbols), missingSymbols)
	}

	barCh, readErr, err := ParseOptionCSV(path)
	if err != nil {
		return 0, false, fmt.Errorf("parse CSV: %w", err)
	}

	sessionedBarCh := EnrichOptionBarsWithSession(barCh, sessions)
	enrichedBarCh, enrichWarn := EnrichOptionBarsWithGreeks(sessionedBarCh, stockCloses, GreeksConfig{RiskFreeRate: riskFreeRate})

	rows, err := InsertOptionBars(ctx, conn, enrichedBarCh, batchSize)
	if err != nil {
		return rows, false, fmt.Errorf("insert: %w", err)
	}
	if *readErr != nil {
		return rows, false, fmt.Errorf("read: %w", *readErr)
	}
	if *enrichWarn != nil {
		log.Printf("[WARN] %s: %v", baseName, *enrichWarn)
	}

	log.Printf("[IMPORT] %s: %d option rows with enrichment in %s", baseName, rows, time.Since(fileStart).Round(time.Second))
	return rows, false, nil
}

func ImportStockFile(ctx context.Context, dsn, path string, batchSize int, skipExisting bool, sessions SessionMap) (int64, bool, error) {
	baseName := filepath.Base(path)
	log.Printf("[START] stock %s", baseName)
	fileStart := time.Now()

	conn, err := ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	if skipExisting {
		marketDate, err := ExtractDateFromFilename(path)
		if err == nil {
			count, err := CountExistingStockBars(ctx, conn, marketDate)
			if err != nil {
				return 0, false, fmt.Errorf("check existing: %w", err)
			}
			if count > 0 {
				return 0, true, nil
			}
		}
	}

	barCh, readErr, err := ParseStockCSV(path)
	if err != nil {
		return 0, false, fmt.Errorf("parse CSV: %w", err)
	}

	sessionedBarCh := EnrichStockBarsWithSession(barCh, sessions)
	rows, err := InsertStockBars(ctx, conn, sessionedBarCh, batchSize)
	if err != nil {
		return rows, false, fmt.Errorf("insert: %w", err)
	}
	if *readErr != nil {
		return rows, false, fmt.Errorf("read: %w", *readErr)
	}

	log.Printf("[IMPORT] %s: %d stock rows in %s", baseName, rows, time.Since(fileStart).Round(time.Second))
	return rows, false, nil
}
