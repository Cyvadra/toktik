package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

func main() {
	inputDir := flag.String("input-dir", "", "Directory containing .zst files")
	outputDir := flag.String("output-dir", "", "Directory for output .parquet files")
	workers := flag.Int("workers", runtime.NumCPU()/2, "Number of parallel workers")
	flag.Parse()

	if *inputDir == "" || *outputDir == "" {
		fmt.Fprintf(os.Stderr, "Usage: crypto-options-convert --input-dir <dir> --output-dir <dir> [--workers N]\n")
		os.Exit(1)
	}
	if *workers < 1 {
		*workers = 1
	}

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	entries, err := os.ReadDir(*inputDir)
	if err != nil {
		log.Fatalf("read input dir: %v", err)
	}

	var zstFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".zst") {
			zstFiles = append(zstFiles, filepath.Join(*inputDir, e.Name()))
		}
	}

	if len(zstFiles) == 0 {
		log.Fatalf("no .zst files found in %s", *inputDir)
	}

	log.Printf("Found %d .zst files, processing with %d workers", len(zstFiles), *workers)

	var (
		wg        sync.WaitGroup
		sem       = make(chan struct{}, *workers)
		completed int64
		failed    int64
	)

	startTime := time.Now()

	for _, zstPath := range zstFiles {
		wg.Add(1)
		sem <- struct{}{}

		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := processFile(path, *outputDir); err != nil {
				log.Printf("[ERROR] %s: %v", filepath.Base(path), err)
				atomic.AddInt64(&failed, 1)
			} else {
				n := atomic.AddInt64(&completed, 1)
				log.Printf("[DONE] %d/%d files completed", n, len(zstFiles))
			}
		}(zstPath)
	}

	wg.Wait()

	elapsed := time.Since(startTime)
	log.Printf("Completed: %d succeeded, %d failed, elapsed %s",
		completed, failed, elapsed.Round(time.Second))
}

func processFile(zstPath, outputDir string) error {
	baseName := filepath.Base(zstPath)
	stem := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	outPath := filepath.Join(outputDir, stem+".parquet")

	log.Printf("[START] %s", baseName)
	fileStart := time.Now()

	tickCh, closer, err := cryptooptions.ParseCSVFromZST(zstPath)
	if err != nil {
		return fmt.Errorf("open zst: %w", err)
	}
	defer closer()

	agg := cryptooptions.NewAggregator()
	var tickCount int64
	for tick := range tickCh {
		agg.Add(tick)
		tickCount++
		if tickCount%10_000_000 == 0 {
			log.Printf("[PROGRESS] %s: %dM ticks processed, %d active bars",
				baseName, tickCount/1_000_000, agg.Count())
		}
	}

	barCount := agg.Count()
	log.Printf("[AGGREGATE] %s: %d ticks -> %d bars in %s",
		baseName, tickCount, barCount, time.Since(fileStart).Round(time.Second))

	if barCount == 0 {
		log.Printf("[SKIP] %s: no bars produced", baseName)
		return nil
	}

	writeStart := time.Now()
	writer, err := cryptooptions.NewBarWriter(outPath)
	if err != nil {
		return fmt.Errorf("open parquet writer: %w", err)
	}
	defer func() {
		_ = writer.Close()
	}()

	if _, err := agg.FlushSortedBatches(100_000, writer.WriteRows); err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close parquet writer: %w", err)
	}

	info, _ := os.Stat(outPath)
	var sizeMB float64
	if info != nil {
		sizeMB = float64(info.Size()) / (1024 * 1024)
	}

	log.Printf("[WRITE] %s -> %s (%.1f MB, %d bars, write took %s)",
		baseName, filepath.Base(outPath), sizeMB, barCount,
		time.Since(writeStart).Round(time.Millisecond))

	return nil
}
