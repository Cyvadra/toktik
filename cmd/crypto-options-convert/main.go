package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

	zstFiles, err := findZSTFiles(*inputDir)
	if err != nil {
		log.Fatalf("scan input dir: %v", err)
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

			if err := processFile(*inputDir, path, *outputDir); err != nil {
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

func findZSTFiles(inputDir string) ([]string, error) {
	var zstFiles []string

	err := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".zst") {
			zstFiles = append(zstFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(zstFiles)
	return zstFiles, nil
}

func processFile(inputDir, zstPath, outputDir string) error {
	baseName := filepath.Base(zstPath)
	relPath, err := filepath.Rel(inputDir, zstPath)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}
	stem := strings.TrimSuffix(relPath, filepath.Ext(relPath))
	outPath := filepath.Join(outputDir, stem+".parquet")
	spotOutPath := filepath.Join(outputDir, "spot", stem+".parquet")

	// Skip if both output files already exist (resume support)
	if st1, err1 := os.Stat(outPath); err1 == nil {
		if st2, err2 := os.Stat(spotOutPath); err2 == nil {
			if min(st1.Size(), st2.Size()) > 32*1024*1024 {
				log.Printf("[SKIP] %s: output already exists", baseName)
				return nil
			}
		}
	}

	log.Printf("[START] %s", baseName)
	fileStart := time.Now()

	tickCh, closer, err := cryptooptions.ParseCSVFromZST(context.Background(), zstPath)
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
			log.Printf("[PROGRESS] %s: %dM ticks processed, %d active option bars, %d active spot bars",
				baseName, tickCount/1_000_000, agg.OptionCount(), agg.SpotCount())
		}
	}

	barCount := agg.OptionCount()
	spotBarCount := agg.SpotCount()
	log.Printf("[AGGREGATE] %s: %d ticks -> %d option bars, %d spot bars in %s",
		baseName, tickCount, barCount, spotBarCount, time.Since(fileStart).Round(time.Second))

	if barCount == 0 && spotBarCount == 0 {
		log.Printf("[SKIP] %s: no bars produced", baseName)
		return nil
	}

	writeStart := time.Now()
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("create output subdir: %w", err)
	}
	writer, err := cryptooptions.NewBarWriter(outPath)
	if err != nil {
		return fmt.Errorf("open parquet writer: %w", err)
	}
	defer func() {
		_ = writer.Close()
	}()
	if err := os.MkdirAll(filepath.Dir(spotOutPath), 0755); err != nil {
		return fmt.Errorf("create spot output subdir: %w", err)
	}
	spotWriter, err := cryptooptions.NewSpotBarWriter(spotOutPath)
	if err != nil {
		return fmt.Errorf("open spot parquet writer: %w", err)
	}
	defer func() {
		_ = spotWriter.Close()
	}()

	if _, err := agg.FlushSortedOptionBatches(100_000, writer.WriteRows); err != nil {
		return fmt.Errorf("write parquet: %w", err)
	}
	if _, err := agg.FlushSortedSpotBatches(100_000, spotWriter.WriteRows); err != nil {
		return fmt.Errorf("write spot parquet: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close parquet writer: %w", err)
	}
	if err := spotWriter.Close(); err != nil {
		return fmt.Errorf("close spot parquet writer: %w", err)
	}

	info, _ := os.Stat(outPath)
	spotInfo, _ := os.Stat(spotOutPath)
	var sizeMB float64
	if info != nil {
		sizeMB = float64(info.Size()) / (1024 * 1024)
	}
	var spotSizeMB float64
	if spotInfo != nil {
		spotSizeMB = float64(spotInfo.Size()) / (1024 * 1024)
	}

	log.Printf("[WRITE] %s -> %s (%.1f MB, %d option bars); %s (%.1f MB, %d spot bars); write took %s",
		baseName, filepath.Base(outPath), sizeMB, barCount,
		filepath.Join("spot", filepath.Base(spotOutPath)), spotSizeMB, spotBarCount,
		time.Since(writeStart).Round(time.Millisecond))

	return nil
}
