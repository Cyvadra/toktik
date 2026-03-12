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

	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

func main() {
	inputDir := flag.String("input-dir", "", "Directory containing .parquet files")
	dsn := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
	batchSize := flag.Int("batch-size", 50000, "Rows per INSERT batch")
	workers := flag.Int("workers", 2, "Number of parallel file importers")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	if *inputDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: crypto-options-import --input-dir <dir> [--clickhouse-dsn DSN] [--batch-size N] [--workers N]\n")
		os.Exit(1)
	}
	if *workers < 1 {
		*workers = 1
	}

	if *schemaFile == "" {
		candidates := []string{
			"schema/clickhouse/crypto_options.sql",
			"../schema/clickhouse/crypto_options.sql",
			"../../schema/clickhouse/crypto_options.sql",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				*schemaFile = c
				break
			}
		}
		if *schemaFile == "" {
			log.Fatalf("cannot find schema SQL file; specify --schema path")
		}
	}

	ctx := context.Background()

	conn, err := cryptooptions.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect to ClickHouse: %v", err)
	}
	log.Printf("Connected to ClickHouse")

	if err := cryptooptions.InitSchema(ctx, conn, *schemaFile); err != nil {
		log.Fatalf("init schema: %v", err)
	}
	log.Printf("Schema initialized from %s", *schemaFile)

	if err := cryptooptions.InitKlineSchema(ctx, conn); err != nil {
		log.Fatalf("init kline schema: %v", err)
	}
	log.Printf("K-line materialized views initialized")

	parquetFiles, err := collectParquetFiles(*inputDir)
	if err != nil {
		log.Fatalf("scan input dir: %v", err)
	}

	if len(parquetFiles) == 0 {
		log.Fatalf("no .parquet files found in %s", *inputDir)
	}

	log.Printf("Found %d .parquet files, importing with %d workers", len(parquetFiles), *workers)

	var (
		wg        sync.WaitGroup
		sem       = make(chan struct{}, *workers)
		completed int64
		failed    int64
		totalRows int64
	)

	startTime := time.Now()

	for _, pqPath := range parquetFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, err := importFile(ctx, *dsn, path, *batchSize)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else {
				atomic.AddInt64(&totalRows, rows)
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %d/%d files imported (%d rows)", n, len(parquetFiles), rows)
			}
		}(pqPath)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	log.Printf("Import complete: %d files succeeded, %d failed, %d total rows, elapsed %s",
		completed, failed, totalRows, elapsed.Round(time.Second))
}

func collectParquetFiles(root string) ([]string, error) {
	var parquetFiles []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".parquet") {
			parquetFiles = append(parquetFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(parquetFiles)
	return parquetFiles, nil
}

func importFile(ctx context.Context, dsn, pqPath string, batchSize int) (int64, error) {
	baseName := filepath.Base(pqPath)
	log.Printf("[START] importing %s", baseName)
	fileStart := time.Now()

	conn, err := cryptooptions.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}

	barCh, closer, err := cryptooptions.ReadParquet(pqPath)
	if err != nil {
		return 0, fmt.Errorf("read parquet: %w", err)
	}
	defer closer()

	var bars []cryptooptions.Bar1m
	symbolMap := make(map[uint32]cryptooptions.SymbolMeta)

	for bar := range barCh {
		bars = append(bars, bar)
		if _, exists := symbolMap[bar.SymbolID]; !exists {
			symbolMap[bar.SymbolID] = cryptooptions.SymbolMeta{
				SymbolID:        bar.SymbolID,
				Symbol:          bar.Symbol,
				BaseAsset:       bar.BaseAsset,
				OptionType:      bar.OptionType,
				StrikePrice:     bar.StrikePrice,
				Expiration:      bar.Expiration,
				UnderlyingIndex: bar.UnderlyingIndex,
			}
		}
	}

	symbols := make([]cryptooptions.SymbolMeta, 0, len(symbolMap))
	for _, s := range symbolMap {
		symbols = append(symbols, s)
	}
	if err := cryptooptions.InsertSymbols(ctx, conn, symbols); err != nil {
		return 0, fmt.Errorf("insert symbols: %w", err)
	}
	log.Printf("[SYMBOLS] %s: inserted %d unique symbols", baseName, len(symbols))

	barSendCh := make(chan cryptooptions.Bar1m, 4096)
	go func() {
		defer close(barSendCh)
		for i := range bars {
			barSendCh <- bars[i]
		}
	}()

	rowCount, err := cryptooptions.InsertBars(ctx, conn, barSendCh, batchSize)
	if err != nil {
		return rowCount, fmt.Errorf("insert bars: %w", err)
	}

	log.Printf("[IMPORT] %s: %d bars, %d symbols in %s",
		baseName, rowCount, len(symbols), time.Since(fileStart).Round(time.Second))

	return rowCount, nil
}
