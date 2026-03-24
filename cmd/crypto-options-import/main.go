package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

const (
	sampleCountPerRegion = 24
	sampleWindowSize     = 256
)

func main() {
	inputDir := flag.String("input-dir", "", "Directory containing .parquet files")
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
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

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		DDLFile:   ddlFile,
		Kline:     true,
		SpotKline: true,
	})
	if err != nil {
		log.Fatalf("%v", err)
	}
	_ = conn // schema-only; workers open their own connections

	optionFiles, spotFiles, err := collectParquetFiles(*inputDir)
	if err != nil {
		log.Fatalf("scan input dir: %v", err)
	}

	if len(optionFiles) == 0 && len(spotFiles) == 0 {
		log.Fatalf("no .parquet files found in %s", *inputDir)
	}

	totalFiles := len(optionFiles) + len(spotFiles)
	log.Printf("Found %d option parquet files and %d spot parquet files, importing with %d workers", len(optionFiles), len(spotFiles), *workers)

	var (
		wg         sync.WaitGroup
		sem        = make(chan struct{}, *workers)
		completed  int64
		skipped    int64
		failed     int64
		optionRows int64
		spotRows   int64
	)

	startTime := time.Now()

	for _, pqPath := range optionFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, skippedFile, err := importOptionFile(ctx, *dsn, path, *batchSize)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else if skippedFile {
				n := atomic.AddInt64(&skipped, 1)
				log.Printf("[SKIPPED] %d/%d files skipped", n, totalFiles)
			} else {
				atomic.AddInt64(&optionRows, rows)
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %d/%d files imported (%d option rows)", n, totalFiles, rows)
			}
		}(pqPath)
	}

	for _, pqPath := range spotFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			rows, skippedFile, err := importSpotFile(ctx, *dsn, path, *batchSize)
			if err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else if skippedFile {
				n := atomic.AddInt64(&skipped, 1)
				log.Printf("[SKIPPED] %d/%d files skipped", n, totalFiles)
			} else {
				atomic.AddInt64(&spotRows, rows)
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %d/%d files imported (%d spot rows)", n, totalFiles, rows)
			}
		}(pqPath)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	log.Printf("Import complete: %d files succeeded, %d skipped, %d failed, %d option rows, %d spot rows, elapsed %s",
		completed, skipped, failed, optionRows, spotRows, elapsed.Round(time.Second))
}

func collectParquetFiles(root string) ([]string, []string, error) {
	var optionFiles []string
	var spotFiles []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".parquet") {
			relPath, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			spotPrefix := "spot" + string(os.PathSeparator)
			if relPath == "spot" || strings.HasPrefix(relPath, spotPrefix) {
				spotFiles = append(spotFiles, path)
			} else {
				optionFiles = append(optionFiles, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	sort.Strings(optionFiles)
	sort.Strings(spotFiles)
	return optionFiles, spotFiles, nil
}

func importOptionFile(ctx context.Context, dsn, pqPath string, batchSize int) (int64, bool, error) {
	baseName := filepath.Base(pqPath)
	log.Printf("[START] importing %s", baseName)
	fileStart := time.Now()

	conn, err := cryptooptions.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	barCh, closer, readErr, err := cryptooptions.ReadParquet(pqPath)
	if err != nil {
		return 0, false, fmt.Errorf("read parquet: %w", err)
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

	if *readErr != nil {
		return 0, false, fmt.Errorf("read parquet: %w", *readErr)
	}

	if len(bars) == 0 {
		log.Printf("[SKIP] %s: no bars found in parquet file", baseName)
		return 0, true, nil
	}

	samples := selectExistenceSamples(bars)
	existingSamples, err := cryptooptions.CountExistingBars(ctx, conn, samples)
	if err != nil {
		return 0, false, fmt.Errorf("check existing bars: %w", err)
	}
	if existingSamples > 0 {
		status := "partially"
		if existingSamples == len(samples) {
			status = "fully"
		}
		log.Printf("[SKIP] %s: %d/%d sampled bars already exist, file appears %s imported",
			baseName, existingSamples, len(samples), status)
		return 0, true, nil
	}

	symbols := make([]cryptooptions.SymbolMeta, 0, len(symbolMap))
	for _, s := range symbolMap {
		symbols = append(symbols, s)
	}
	if err := cryptooptions.InsertSymbols(ctx, conn, symbols); err != nil {
		return 0, false, fmt.Errorf("insert symbols: %w", err)
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
		return rowCount, false, fmt.Errorf("insert bars: %w", err)
	}

	log.Printf("[IMPORT] %s: %d bars, %d symbols in %s",
		baseName, rowCount, len(symbols), time.Since(fileStart).Round(time.Second))

	return rowCount, false, nil
}

func importSpotFile(ctx context.Context, dsn, pqPath string, batchSize int) (int64, bool, error) {
	baseName := filepath.Base(pqPath)
	log.Printf("[START] importing spot %s", baseName)
	fileStart := time.Now()

	conn, err := cryptooptions.ConnectClickHouse(ctx, dsn)
	if err != nil {
		return 0, false, fmt.Errorf("connect: %w", err)
	}

	barCh, closer, readErr, err := cryptooptions.ReadSpotParquet(pqPath)
	if err != nil {
		return 0, false, fmt.Errorf("read spot parquet: %w", err)
	}
	defer closer()

	var bars []cryptooptions.SpotBar1m
	for bar := range barCh {
		bars = append(bars, bar)
	}

	if *readErr != nil {
		return 0, false, fmt.Errorf("read spot parquet: %w", *readErr)
	}

	if len(bars) == 0 {
		log.Printf("[SKIP] %s: no spot bars found in parquet file", baseName)
		return 0, true, nil
	}

	samples := selectExistenceSamples(bars)
	existingSamples, err := cryptooptions.CountExistingSpotBars(ctx, conn, samples)
	if err != nil {
		return 0, false, fmt.Errorf("check existing spot bars: %w", err)
	}
	if existingSamples > 0 {
		status := "partially"
		if existingSamples == len(samples) {
			status = "fully"
		}
		log.Printf("[SKIP] %s: %d/%d sampled spot bars already exist, file appears %s imported",
			baseName, existingSamples, len(samples), status)
		return 0, true, nil
	}

	barSendCh := make(chan cryptooptions.SpotBar1m, 4096)
	go func() {
		defer close(barSendCh)
		for i := range bars {
			barSendCh <- bars[i]
		}
	}()

	rowCount, err := cryptooptions.InsertSpotBars(ctx, conn, barSendCh, batchSize)
	if err != nil {
		return rowCount, false, fmt.Errorf("insert spot bars: %w", err)
	}

	log.Printf("[IMPORT] %s: %d spot bars in %s",
		baseName, rowCount, time.Since(fileStart).Round(time.Second))

	return rowCount, false, nil
}

// selectExistenceSamples picks a random sample of bars from the beginning and
// end of the slice to use as existence probes against ClickHouse.
// It is generic over the bar type so the same logic works for both option and
// spot bars without duplication.
func selectExistenceSamples[T any](bars []T) []T {
	if len(bars) == 0 {
		return nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	selected := make(map[int]struct{}, sampleCountPerRegion*2)
	indices := make([]int, 0, sampleCountPerRegion*2)

	indices = append(indices, randomIndices(rng, 0, min(len(bars), sampleWindowSize), sampleCountPerRegion, selected)...)
	indices = append(indices, randomIndices(rng, max(0, len(bars)-sampleWindowSize), len(bars), sampleCountPerRegion, selected)...)

	if len(indices) == 0 {
		indices = append(indices, 0)
	}

	samples := make([]T, 0, len(indices))
	for _, idx := range indices {
		samples = append(samples, bars[idx])
	}

	return samples
}

func randomIndices(rng *rand.Rand, start, end, count int, selected map[int]struct{}) []int {
	if start >= end || count <= 0 {
		return nil
	}

	pool := make([]int, 0, end-start)
	for idx := start; idx < end; idx++ {
		if _, exists := selected[idx]; exists {
			continue
		}
		pool = append(pool, idx)
	}

	if len(pool) == 0 {
		return nil
	}

	if count > len(pool) {
		count = len(pool)
	}

	rng.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})

	chosen := pool[:count]
	for _, idx := range chosen {
		selected[idx] = struct{}{}
	}

	return chosen
}
