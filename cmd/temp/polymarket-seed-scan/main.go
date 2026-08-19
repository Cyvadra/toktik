package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/parquet-go/parquet-go"
)

type conditionRecord struct {
	Period      string `json:"period"`
	Asset       string `json:"asset"`
	Slug        string `json:"slug"`
	WindowStart int64  `json:"window_start"`
	MarketID    string `json:"market_id"`
	EventID     string `json:"event_id"`
	TokenUp     string `json:"token_up"`
	TokenDown   string `json:"token_down"`
	Resolved    bool   `json:"resolved"`
	Winner      string `json:"winner"`
}

type marketMeta struct {
	ConditionID string    `json:"condition_id"`
	MarketID    string    `json:"market_id"`
	EventID     string    `json:"event_id"`
	Slug        string    `json:"slug"`
	Asset       string    `json:"asset"`
	Period      string    `json:"period"`
	WindowStart time.Time `json:"window_start"`
	TokenUp     string    `json:"token_up"`
	TokenDown   string    `json:"token_down"`
}

type initEvent struct {
	ConditionID  string    `json:"condition_id"`
	MarketID     string    `json:"market_id"`
	AssetID      string    `json:"asset_id"`
	OutcomeIndex uint8     `json:"outcome_index"`
	Asset        string    `json:"asset"`
	Period       string    `json:"period"`
	WindowStart  time.Time `json:"window_start"`
	InitTime     time.Time `json:"init_time"`
	SourceFile   string    `json:"source_file"`
	SourceRow    uint64    `json:"source_row"`
}

type scanTargets struct {
	markets map[string]marketMeta
	tokens  map[string]tokenRef
}

type tokenRef struct {
	ConditionID  string
	OutcomeIndex uint8
}

type bucket struct {
	Name string
	Max  time.Duration
}

var buckets = []bucket{
	{Name: "<5m", Max: 5 * time.Minute},
	{Name: "5-15m", Max: 15 * time.Minute},
	{Name: "15-30m", Max: 30 * time.Minute},
	{Name: "30-60m", Max: time.Hour},
	{Name: "1-2h", Max: 2 * time.Hour},
	{Name: "2-4h", Max: 4 * time.Hour},
	{Name: "4-8h", Max: 8 * time.Hour},
	{Name: "8-12h", Max: 12 * time.Hour},
	{Name: "12-24h", Max: 24 * time.Hour},
	{Name: "1-2d", Max: 48 * time.Hour},
	{Name: "2-7d", Max: 7 * 24 * time.Hour},
}

func main() {
	mode := flag.String("mode", "scan", "scan or summarize")
	conditionMap := flag.String("condition-map", "/mnt/hdd/hephaestus/pmxt/generated/2026-08-17/pmxt_condition_map/condition_map_by_condition.json", "condition_map_by_condition.json path")
	rawRoot := flag.String("raw-root", "/mnt/hdd/pmxt/raw", "raw parquet directory")
	cachePath := flag.String("cache", "tmp/polymarket_seed_scan/init_events.jsonl", "init event cache JSONL path")
	workers := flag.Int("workers", 2, "scan worker count")
	assetFilter := flag.String("asset", "", "optional uppercase crypto asset filter, e.g. BTC")
	limitFiles := flag.Int("limit-files", 0, "optional number of parquet files to scan")
	flag.Parse()

	targets, err := loadTargets(*conditionMap, strings.ToUpper(strings.TrimSpace(*assetFilter)))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("loaded markets=%d tokens=%d", len(targets.markets), len(targets.tokens))

	switch *mode {
	case "scan":
		if err := scan(context.Background(), targets, *rawRoot, *cachePath, *workers, *limitFiles); err != nil {
			log.Fatal(err)
		}
	case "summarize":
		if err := summarize(targets, *cachePath); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unsupported mode %q", *mode)
	}
}

func loadTargets(path, assetFilter string) (scanTargets, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return scanTargets{}, err
	}
	defer file.Close()
	var records map[string]conditionRecord
	if err := json.NewDecoder(file).Decode(&records); err != nil {
		return scanTargets{}, err
	}
	targets := scanTargets{markets: make(map[string]marketMeta, len(records)), tokens: make(map[string]tokenRef, len(records)*2)}
	for conditionID, record := range records {
		if record.TokenUp == "" || record.TokenDown == "" {
			continue
		}
		asset := strings.ToUpper(record.Asset)
		if assetFilter != "" && asset != assetFilter {
			continue
		}
		meta := marketMeta{
			ConditionID: conditionID,
			MarketID:    record.MarketID,
			EventID:     record.EventID,
			Slug:        record.Slug,
			Asset:       asset,
			Period:      strings.ToLower(record.Period),
			WindowStart: time.Unix(record.WindowStart, 0).UTC(),
			TokenUp:     record.TokenUp,
			TokenDown:   record.TokenDown,
		}
		targets.markets[conditionID] = meta
		targets.tokens[record.TokenUp] = tokenRef{ConditionID: conditionID, OutcomeIndex: 0}
		targets.tokens[record.TokenDown] = tokenRef{ConditionID: conditionID, OutcomeIndex: 1}
	}
	return targets, nil
}

func scan(ctx context.Context, targets scanTargets, rawRoot, cachePath string, workers, limitFiles int) error {
	files, err := filepath.Glob(filepath.Join(rawRoot, "polymarket_orderbook_*.parquet"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	if limitFiles > 0 && limitFiles < len(files) {
		files = files[:limitFiles]
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(filepath.Clean(cachePath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	writer := bufio.NewWriterSize(out, 4*1024*1024)
	defer writer.Flush()
	seenFiles, err := cachedFiles(cachePath)
	if err != nil {
		return err
	}
	jobs := make(chan string)
	var writeMu sync.Mutex
	var done atomic.Int64
	var matched atomic.Int64
	var workerGroup sync.WaitGroup
	if workers <= 0 {
		workers = 1
	}
	for index := 0; index < workers; index++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for path := range jobs {
				count, err := scanFile(ctx, targets, path, func(event initEvent) error {
					writeMu.Lock()
					defer writeMu.Unlock()
					encoded, err := json.Marshal(event)
					if err != nil {
						return err
					}
					if _, err := writer.Write(encoded); err != nil {
						return err
					}
					return writer.WriteByte('\n')
				})
				if err != nil {
					log.Printf("scan failed file=%s err=%v", filepath.Base(path), err)
					continue
				}
				matched.Add(int64(count))
				current := done.Add(1)
				if current%25 == 0 || current == int64(len(files)) {
					log.Printf("progress files=%d/%d matched=%d last=%s", current, len(files), matched.Load(), filepath.Base(path))
				}
			}
		}()
	}
	enqueued := 0
	for _, path := range files {
		if seenFiles[filepath.Base(path)] {
			continue
		}
		enqueued++
		jobs <- path
	}
	close(jobs)
	workerGroup.Wait()
	log.Printf("scan complete candidates=%d enqueued=%d matched=%d cache=%s", len(files), enqueued, matched.Load(), cachePath)
	return nil
}

func cachedFiles(cachePath string) (map[string]bool, error) {
	seen := make(map[string]bool)
	file, err := os.Open(filepath.Clean(cachePath))
	if errors.Is(err, os.ErrNotExist) {
		return seen, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event initEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err == nil && event.SourceFile != "" {
			seen[event.SourceFile] = true
		}
	}
	return seen, scanner.Err()
}

func scanFile(ctx context.Context, targets scanTargets, path string, consume func(initEvent) error) (uint64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	parquetFile, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		return 0, err
	}
	sourceFile := filepath.Base(path)
	rowBuffer := make([]parquet.Row, 8192)
	var sourceRow uint64
	var matched uint64
	for _, rowGroup := range parquetFile.RowGroups() {
		rows := rowGroup.Rows()
		for {
			n, readErr := rows.ReadRows(rowBuffer)
			for index := 0; index < n; index++ {
				if err := ctx.Err(); err != nil {
					rows.Close()
					return matched, err
				}
				row := rowBuffer[index]
				if len(row) != 16 {
					rows.Close()
					return matched, fmt.Errorf("row %d has %d columns", sourceRow, len(row))
				}
				if row[3].String() == "book" {
					conditionID := string(row[2].Bytes())
					token, ok := targets.tokens[row[4].String()]
					if ok && token.ConditionID == conditionID {
						meta := targets.markets[conditionID]
						initTime := time.UnixMilli(row[1].Int64()).UTC()
						if initTime.Before(meta.WindowStart) {
							if err := consume(initEvent{ConditionID: conditionID, MarketID: meta.MarketID, AssetID: row[4].String(), OutcomeIndex: token.OutcomeIndex, Asset: meta.Asset, Period: meta.Period, WindowStart: meta.WindowStart, InitTime: initTime, SourceFile: sourceFile, SourceRow: sourceRow}); err != nil {
								rows.Close()
								return matched, err
							}
							matched++
						}
					}
				}
				sourceRow++
				rowBuffer[index] = row[:0]
			}
			if readErr != nil {
				if closeErr := rows.Close(); closeErr != nil && readErr == io.EOF {
					return matched, closeErr
				}
				if readErr == io.EOF {
					break
				}
				return matched, readErr
			}
		}
	}
	return matched, nil
}

func summarize(targets scanTargets, cachePath string) error {
	latest := make(map[string]initEvent)
	file, err := os.Open(filepath.Clean(cachePath))
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var event initEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		key := event.ConditionID + "\x00" + event.AssetID
		if previous, ok := latest[key]; !ok || event.InitTime.After(previous.InitTime) || event.InitTime.Equal(previous.InitTime) && event.SourceRow > previous.SourceRow {
			latest[key] = event
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	bucketCounts := make(map[string]int)
	periodCounts := make(map[string]map[string]int)
	assetCounts := make(map[string]map[string]int)
	missing := 0
	present := 0
	for _, meta := range targets.markets {
		for _, assetID := range []string{meta.TokenUp, meta.TokenDown} {
			key := meta.ConditionID + "\x00" + assetID
			event, ok := latest[key]
			if !ok {
				missing++
				continue
			}
			present++
			lookback := meta.WindowStart.Sub(event.InitTime)
			bucketName := lookbackBucket(lookback)
			bucketCounts[bucketName]++
			addNested(periodCounts, meta.Period, bucketName)
			addNested(assetCounts, meta.Asset, bucketName)
		}
	}
	fmt.Printf("targets_assets=%d present=%d missing=%d\n", len(targets.markets)*2, present, missing)
	printCounts("lookback", bucketCounts)
	for period, counts := range periodCounts {
		printCounts("period="+period, counts)
	}
	for asset, counts := range assetCounts {
		printCounts("asset="+asset, counts)
	}
	return nil
}

func lookbackBucket(value time.Duration) string {
	if value < 0 {
		return "after_start"
	}
	for _, bucket := range buckets {
		if value < bucket.Max || value == bucket.Max {
			return bucket.Name
		}
	}
	return ">7d"
}

func addNested(values map[string]map[string]int, outer, inner string) {
	if values[outer] == nil {
		values[outer] = make(map[string]int)
	}
	values[outer][inner]++
}

func printCounts(label string, counts map[string]int) {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("%s", label)
	for _, name := range names {
		fmt.Printf(" %s=%d", name, counts[name])
	}
	fmt.Println()
}
