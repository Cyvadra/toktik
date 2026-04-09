package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

type juliaSpotJSON struct {
	Low       []float64     `json:"Low"`
	Close     []float64     `json:"Close"`
	Open      []float64     `json:"Open"`
	High      []float64     `json:"High"`
	Volume    []float64     `json:"Volume"`
	Timestamp []json.Number `json:"Timestamp"`
}

type jsonMinuteBar struct {
	Timestamp  time.Time
	Open       float32
	High       float32
	Low        float32
	Close      float32
	VolumeSeed float64
}

type volumeRemainder struct {
	Offset    int
	Remainder float64
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	jsonFile := flag.String("json-file", "btc2023_2025.json", "Path to JSON file providing 1m OHLC and minute volume weights")
	csvFile := flag.String("csv-file", "BTCUSDT_1h.csv", "Path to CSV file providing 1h volume totals")
	inputFile := flag.String("input-file", "", "Deprecated legacy single-source input file (.json or .csv)")
	inputFormat := flag.String("format", "auto", "Deprecated legacy input format for --input-file: auto|json|csv")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbol := flag.String("symbol", "BTC", "Spot symbol written to crypto_spot_bar_1m.symbol")
	priceSource := flag.String("price-source", "julia-json+csv-volume", "Value for crypto_spot_bar_1m.price_source")
	jsonTimeOffset := flag.Duration("json-time-offset", 0, "Offset applied to JSON timestamps before merging/import, e.g. -8h")
	batchSize := flag.Int("batch-size", 50000, "Rows per INSERT batch")
	overwrite := flag.Bool("overwrite", true, "Delete existing rows for symbol across 1m and all spot interval agg tables before import")
	initSchema := flag.Bool("init-schema", true, "Initialize base schema + spot kline schema before import")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	if *batchSize < 1 {
		*batchSize = 50000
	}

	ctx := context.Background()

	var schema *appCli.SchemaInit
	if *initSchema {
		ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.CryptoOptionsSchemaFile)
		if err != nil {
			log.Fatalf("%v", err)
		}
		schema = &appCli.SchemaInit{
			DDLFile:   ddlFile,
			SpotKline: true,
		}
	}

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, schema)
	if err != nil {
		log.Fatalf("%v", err)
	}

	if *overwrite {
		beforeCount, err := countSpotRows1mBySymbol(ctx, conn, *symbol)
		if err != nil {
			log.Fatalf("count existing 1m rows: %v", err)
		}
		affected, err := deleteAllSpotRowsBySymbol(ctx, conn, *symbol)
		if err != nil {
			log.Fatalf("delete existing rows by symbol: %v", err)
		}
		log.Printf("Overwrite enabled: deleted symbol=%s from %d spot tables (1m before=%d rows)", *symbol, affected, beforeCount)
	}

	checkSpotKlineMVStatus(ctx, conn)

	var bars []cryptooptions.SpotBar1m
	if strings.TrimSpace(*inputFile) != "" {
		format, err := resolveInputFormat(*inputFile, *inputFormat)
		if err != nil {
			log.Fatalf("resolve input format: %v", err)
		}
		log.Printf("Legacy single-source import: file=%s format=%s", *inputFile, format)
		bars, err = loadInputBars(*inputFile, format, *symbol, *priceSource)
		if err != nil {
			log.Fatalf("load input bars: %v", err)
		}
	} else {
		if strings.TrimSpace(*jsonFile) == "" {
			log.Fatalf("json-file is required when --input-file is not set")
		}
		if strings.TrimSpace(*csvFile) == "" {
			log.Printf("CSV file not provided; falling back to JSON-only import")
			bars, err = parseJSONBars(*jsonFile, *symbol, *priceSource, *jsonTimeOffset)
			if err != nil {
				log.Fatalf("load json bars: %v", err)
			}
		} else {
			log.Printf("Dual-source import: json=%s csv=%s", *jsonFile, *csvFile)
			bars, err = loadMergedInputBars(*jsonFile, *csvFile, *symbol, *priceSource, *jsonTimeOffset)
			if err != nil {
				log.Fatalf("load merged input bars: %v", err)
			}
		}
	}

	if len(bars) == 0 {
		log.Fatalf("no valid rows to import")
	}

	sortSpotBarsByTimestamp(bars)
	warnMissingBarsAndIrregularGaps(bars)

	barCh := make(chan cryptooptions.SpotBar1m, 8192)

	go func() {
		defer close(barCh)
		for i := range bars {
			barCh <- bars[i]
		}
	}()

	start := time.Now()
	rows, err := cryptooptions.InsertSpotBars(ctx, conn, barCh, *batchSize)
	if err != nil {
		log.Fatalf("insert spot bars: %v", err)
	}

	log.Printf("Import done: inserted %d rows in %s", rows, time.Since(start).Round(time.Second))
}

func loadInputBars(path, format, symbol, priceSource string) ([]cryptooptions.SpotBar1m, error) {
	switch format {
	case "json":
		return parseJSONBars(path, symbol, priceSource, 0)
	case "csv":
		return parseCSVBars(path, symbol, priceSource)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

func loadMergedInputBars(jsonPath, csvPath, symbol, priceSource string, jsonTimeOffset time.Duration) ([]cryptooptions.SpotBar1m, error) {
	priceBars, err := parseJSONMinuteBars(jsonPath, jsonTimeOffset)
	if err != nil {
		return nil, fmt.Errorf("parse json minute bars: %w", err)
	}

	hourlyVolumes, err := parseCSVHourlyVolumes(csvPath)
	if err != nil {
		return nil, fmt.Errorf("parse csv hourly volumes: %w", err)
	}

	bars, usedHours, ignoredHours, skippedHours, err := mergeJSONPriceBarsWithCSVVolumes(priceBars, hourlyVolumes, symbol, priceSource)
	if err != nil {
		return nil, err
	}

	log.Printf("Merged %d minute bars across %d overlapping hours with %d CSV hourly rows (%d CSV rows unused, %d JSON hours skipped)", len(bars), usedHours, len(hourlyVolumes), ignoredHours, skippedHours)
	return bars, nil
}

func resolveInputFormat(path, requested string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(requested))

	switch format {
	case "", "auto":
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".json":
			return "json", nil
		case ".csv":
			return "csv", nil
		default:
			return "", fmt.Errorf("cannot infer input format from extension %q, set --format", ext)
		}
	case "json", "csv":
		return format, nil
	default:
		return "", fmt.Errorf("invalid --format=%q, expected auto|json|csv", requested)
	}
}

func countSpotRows1mBySymbol(ctx context.Context, conn driver.Conn, symbol string) (uint64, error) {
	rows, err := conn.Query(ctx, `SELECT count() FROM crypto_spot_bar_1m WHERE symbol = ?`, symbol)
	if err != nil {
		return 0, fmt.Errorf("query count: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return 0, nil
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan count: %w", err)
	}

	return count, nil
}

func deleteAllSpotRowsBySymbol(ctx context.Context, conn driver.Conn, symbol string) (int, error) {
	tables := []string{"crypto_spot_bar_1m"}
	for _, iv := range cryptooptions.KlineIntervals {
		tables = append(tables, fmt.Sprintf("crypto_spot_bar_%s_agg", iv.Suffix))
	}

	for _, table := range tables {
		stmt := fmt.Sprintf("ALTER TABLE %s DELETE WHERE symbol = ? SETTINGS mutations_sync=1", table)
		if err := conn.Exec(ctx, stmt, symbol); err != nil {
			return 0, fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	return len(tables), nil
}

func loadJSON(path string) (*juliaSpotJSON, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 4<<20))
	dec.UseNumber()

	var p juliaSpotJSON
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return &p, nil
}

func parseJSONMinuteBars(path string, timeOffset time.Duration) ([]jsonMinuteBar, error) {
	payload, err := loadJSON(path)
	if err != nil {
		return nil, fmt.Errorf("load json: %w", err)
	}

	n, err := effectiveLength(payload)
	if err != nil {
		return nil, fmt.Errorf("invalid json series: %w", err)
	}
	if len(payload.Timestamp) != len(payload.Open) || len(payload.Timestamp) != len(payload.Low) || len(payload.Timestamp) != len(payload.Close) {
		log.Printf("[WARN] JSON required arrays have different lengths (Timestamp=%d Open=%d Low=%d Close=%d), truncating to %d", len(payload.Timestamp), len(payload.Open), len(payload.Low), len(payload.Close), n)
	}
	if len(payload.High) > 0 && len(payload.High) < n {
		log.Printf("[WARN] JSON High shorter than required arrays (High=%d), deriving missing highs from OHLC", len(payload.High))
	}
	if len(payload.Volume) > 0 && len(payload.Volume) < n {
		log.Printf("[WARN] JSON Volume shorter than price arrays (Volume=%d), falling back to zero weights for trailing rows", len(payload.Volume))
	}
	log.Printf("JSON minute rows: %d", n)

	bars := make([]jsonMinuteBar, 0, n)
	skipped := 0

	for i := 0; i < n; i++ {
		ts, err := parseEpoch(payload.Timestamp[i])
		if err != nil {
			log.Printf("[WARN] skip row=%d bad timestamp=%q err=%v", i, payload.Timestamp[i], err)
			skipped++
			continue
		}

		open := float32(payload.Open[i])
		low := float32(payload.Low[i])
		closeP := float32(payload.Close[i])

		var high float32
		if len(payload.High) > i {
			high = float32(payload.High[i])
		} else {
			high = max3(open, low, closeP)
		}

		volumeSeed := 0.0
		if len(payload.Volume) > i {
			volumeSeed = sanitizeWeightSeed(payload.Volume[i])
		}

		bars = append(bars, jsonMinuteBar{
			Timestamp:  ts.UTC().Add(timeOffset),
			Open:       open,
			High:       high,
			Low:        low,
			Close:      closeP,
			VolumeSeed: volumeSeed,
		})

		if (i+1)%500000 == 0 {
			log.Printf("prepared %d JSON minute rows", i+1)
		}
	}
	if skipped > 0 {
		log.Printf("[WARN] JSON rows skipped: %d", skipped)
	}

	return bars, nil
}

func parseJSONBars(path, symbol, priceSource string, jsonTimeOffset time.Duration) ([]cryptooptions.SpotBar1m, error) {
	minuteBars, err := parseJSONMinuteBars(path, jsonTimeOffset)
	if err != nil {
		return nil, err
	}

	bars := make([]cryptooptions.SpotBar1m, 0, len(minuteBars))
	for i := range minuteBars {
		bars = append(bars, cryptooptions.SpotBar1m{
			Timestamp:   minuteBars[i].Timestamp,
			Symbol:      symbol,
			PriceSource: priceSource,
			Open:        minuteBars[i].Open,
			High:        minuteBars[i].High,
			Low:         minuteBars[i].Low,
			Close:       minuteBars[i].Close,
			TickCount:   volumeToUInt32(minuteBars[i].VolumeSeed),
		})
	}

	return bars, nil
}

func parseCSVHourlyVolumes(path string) (map[int64]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 4<<20))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("csv is empty")
		}
		return nil, fmt.Errorf("read csv header: %w", err)
	}

	headerMap := make(map[string]int, len(header))
	for i, h := range header {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		headerMap[norm] = i
	}

	timestampIdx, ok := headerMap["timestamp"]
	if !ok {
		return nil, fmt.Errorf("csv missing required column %q", "timestamp")
	}

	volumeIdx := -1
	for _, candidate := range []string{"volume_from", "volume", "tick_count"} {
		if idx, ok := headerMap[candidate]; ok {
			volumeIdx = idx
			break
		}
	}
	if volumeIdx < 0 {
		return nil, fmt.Errorf("csv missing hourly volume column (expected one of volume_from, volume, tick_count)")
	}

	hourlyVolumes := make(map[int64]float64, 1024)
	prepared := 0
	skipped := 0
	rowNum := 1
	for {
		rowNum++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row %d: %w", rowNum, err)
		}

		ts, err := parseCSVTimestamp(csvField(rec, timestampIdx))
		if err != nil {
			log.Printf("[WARN] skip hourly row=%d bad timestamp=%q err=%v", rowNum, csvField(rec, timestampIdx), err)
			skipped++
			continue
		}

		volume, err := parseCSVFloat64(csvField(rec, volumeIdx))
		if err != nil {
			log.Printf("[WARN] skip hourly row=%d bad volume=%q err=%v", rowNum, csvField(rec, volumeIdx), err)
			skipped++
			continue
		}

		hourTS := ts.UTC().Truncate(time.Hour)
		if existing, exists := hourlyVolumes[hourTS.Unix()]; exists {
			return nil, fmt.Errorf("duplicate csv hour %s (existing=%f new=%f)", hourTS.Format(time.RFC3339), existing, volume)
		}
		hourlyVolumes[hourTS.Unix()] = volume
		prepared++
	}

	log.Printf("CSV hourly volume rows: %d", prepared)
	if skipped > 0 {
		log.Printf("[WARN] CSV hourly rows skipped: %d", skipped)
	}

	return hourlyVolumes, nil
}

func mergeJSONPriceBarsWithCSVVolumes(priceBars []jsonMinuteBar, hourlyVolumes map[int64]float64, symbol, priceSource string) ([]cryptooptions.SpotBar1m, int, int, int, error) {
	if len(priceBars) == 0 {
		return nil, 0, len(hourlyVolumes), 0, nil
	}

	hourToIndexes := make(map[int64][]int)
	usedHours := make(map[int64]struct{})
	for i := range priceBars {

		hourKey := priceBars[i].Timestamp.UTC().Truncate(time.Hour).Unix()
		hourToIndexes[hourKey] = append(hourToIndexes[hourKey], i)
	}

	bars := make([]cryptooptions.SpotBar1m, 0, len(priceBars))
	skippedHours := 0
	for hourKey, indexes := range hourToIndexes {
		hourVolume, ok := hourlyVolumes[hourKey]
		if !ok {
			skippedHours++
			continue
		}

		allocations := allocateHourlyVolume(priceBars, indexes, hourVolume)
		for offset, index := range indexes {
			bars = append(bars, cryptooptions.SpotBar1m{
				Timestamp:   priceBars[index].Timestamp.UTC(),
				Symbol:      symbol,
				PriceSource: priceSource,
				Open:        priceBars[index].Open,
				High:        priceBars[index].High,
				Low:         priceBars[index].Low,
				Close:       priceBars[index].Close,
				TickCount:   allocations[offset],
			})
		}
		usedHours[hourKey] = struct{}{}
	}

	ignoredHours := 0
	for hourKey := range hourlyVolumes {
		if _, ok := usedHours[hourKey]; !ok {
			ignoredHours++
		}
	}
	if len(bars) == 0 {
		return nil, 0, ignoredHours, skippedHours, fmt.Errorf("no overlapping hours between JSON minute bars and CSV hourly volumes")
	}

	return bars, len(usedHours), ignoredHours, skippedHours, nil
}

func allocateHourlyVolume(priceBars []jsonMinuteBar, indexes []int, totalVolume float64) []uint32 {
	allocations := make([]uint32, len(indexes))
	totalUnits := roundedNonNegativeUInt32(totalVolume)
	if len(indexes) == 0 || totalUnits == 0 {
		return allocations
	}

	weightSum := 0.0
	for _, index := range indexes {
		weightSum += sanitizeWeightSeed(priceBars[index].VolumeSeed)
	}

	useUniformWeights := weightSum <= 0
	if useUniformWeights {
		weightSum = float64(len(indexes))
	}

	remainders := make([]volumeRemainder, 0, len(indexes))
	remaining := int(totalUnits)
	for offset, index := range indexes {
		weight := 1.0 / float64(len(indexes))
		if !useUniformWeights {
			weight = sanitizeWeightSeed(priceBars[index].VolumeSeed) / weightSum
		}

		exact := float64(totalUnits) * weight
		base := uint32(math.Floor(exact))
		allocations[offset] = base
		remaining -= int(base)
		remainders = append(remainders, volumeRemainder{
			Offset:    offset,
			Remainder: exact - float64(base),
		})
	}

	sort.SliceStable(remainders, func(i, j int) bool {
		return remainders[i].Remainder > remainders[j].Remainder
	})

	for i := 0; i < remaining && i < len(remainders); i++ {
		allocations[remainders[i].Offset]++
	}

	return allocations
}

func parseCSVBars(path, symbol, priceSource string) ([]cryptooptions.SpotBar1m, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 4<<20))
	r.FieldsPerRecord = -1

	header, err := r.Read()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("csv is empty")
		}
		return nil, fmt.Errorf("read csv header: %w", err)
	}

	headerMap := make(map[string]int, len(header))
	for i, h := range header {
		norm := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		headerMap[norm] = i
	}

	required := []string{"timestamp", "open", "high", "low", "close"}
	for _, c := range required {
		if _, ok := headerMap[c]; !ok {
			return nil, fmt.Errorf("csv missing required column %q", c)
		}
	}

	volumeIdx := -1
	if idx, ok := headerMap["volume_from"]; ok {
		volumeIdx = idx
	}

	prepared := 0
	skipped := 0
	rowNum := 1
	bars := make([]cryptooptions.SpotBar1m, 0, 1024)
	for {
		rowNum++
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row %d: %w", rowNum, err)
		}

		ts, err := parseCSVTimestamp(csvField(rec, headerMap["timestamp"]))
		if err != nil {
			log.Printf("[WARN] skip row=%d bad timestamp=%q err=%v", rowNum, csvField(rec, headerMap["timestamp"]), err)
			skipped++
			continue
		}

		open, err := parseCSVFloat32(csvField(rec, headerMap["open"]))
		if err != nil {
			log.Printf("[WARN] skip row=%d bad open=%q err=%v", rowNum, csvField(rec, headerMap["open"]), err)
			skipped++
			continue
		}
		high, err := parseCSVFloat32(csvField(rec, headerMap["high"]))
		if err != nil {
			log.Printf("[WARN] skip row=%d bad high=%q err=%v", rowNum, csvField(rec, headerMap["high"]), err)
			skipped++
			continue
		}
		low, err := parseCSVFloat32(csvField(rec, headerMap["low"]))
		if err != nil {
			log.Printf("[WARN] skip row=%d bad low=%q err=%v", rowNum, csvField(rec, headerMap["low"]), err)
			skipped++
			continue
		}
		closeP, err := parseCSVFloat32(csvField(rec, headerMap["close"]))
		if err != nil {
			log.Printf("[WARN] skip row=%d bad close=%q err=%v", rowNum, csvField(rec, headerMap["close"]), err)
			skipped++
			continue
		}

		tickCount := uint32(1)
		if volumeIdx >= 0 {
			if v, err := parseCSVFloat64(csvField(rec, volumeIdx)); err == nil {
				tickCount = volumeToUInt32(v)
			} else {
				log.Printf("[WARN] row=%d bad volume_from=%q err=%v, fallback tickCount=1", rowNum, csvField(rec, volumeIdx), err)
			}
		}

		bars = append(bars, cryptooptions.SpotBar1m{
			Timestamp:   ts.UTC(),
			Symbol:      symbol,
			PriceSource: priceSource,
			Open:        open,
			High:        high,
			Low:         low,
			Close:       closeP,
			TickCount:   tickCount,
		})

		prepared++
		if prepared%500000 == 0 {
			log.Printf("prepared %d rows", prepared)
		}
	}

	log.Printf("Input rows: %d", prepared)
	if skipped > 0 {
		log.Printf("[WARN] CSV rows skipped: %d", skipped)
	}
	return bars, nil
}

func sortSpotBarsByTimestamp(bars []cryptooptions.SpotBar1m) {
	sort.SliceStable(bars, func(i, j int) bool {
		return bars[i].Timestamp.Before(bars[j].Timestamp)
	})
	log.Printf("Sorted rows by timestamp")
}

func warnMissingBarsAndIrregularGaps(bars []cryptooptions.SpotBar1m) {
	if len(bars) < 2 {
		return
	}

	deltaFreq := make(map[int64]int)
	duplicates := 0
	for i := 1; i < len(bars); i++ {
		deltaSec := int64(bars[i].Timestamp.Sub(bars[i-1].Timestamp) / time.Second)
		if deltaSec <= 0 {
			duplicates++
			continue
		}
		deltaFreq[deltaSec]++
	}

	if duplicates > 0 {
		log.Printf("[WARN] detected %d duplicate/non-increasing timestamps after sorting", duplicates)
	}

	expectedSec, hasExpected := dominantDeltaSeconds(deltaFreq)
	if !hasExpected {
		return
	}

	missingBars := 0
	gapWarnings := 0
	for i := 1; i < len(bars); i++ {
		deltaSec := int64(bars[i].Timestamp.Sub(bars[i-1].Timestamp) / time.Second)
		if deltaSec <= expectedSec {
			continue
		}

		missing := int(deltaSec/expectedSec) - 1
		if missing <= 0 {
			log.Printf("[WARN] irregular interval: prev=%s curr=%s delta=%ds expected=%ds", bars[i-1].Timestamp.Format(time.RFC3339), bars[i].Timestamp.Format(time.RFC3339), deltaSec, expectedSec)
			continue
		}

		missingBars += missing
		if gapWarnings < 10 {
			log.Printf("[WARN] missing bars between %s and %s: missing=%d (expected step=%ds)", bars[i-1].Timestamp.Format(time.RFC3339), bars[i].Timestamp.Format(time.RFC3339), missing, expectedSec)
			gapWarnings++
		}
	}

	if missingBars > 0 {
		log.Printf("[WARN] total missing bars detected: %d (expected interval=%ds)", missingBars, expectedSec)
	} else {
		log.Printf("No missing bars detected (expected interval=%ds)", expectedSec)
	}
}

func dominantDeltaSeconds(freq map[int64]int) (int64, bool) {
	var bestDelta int64
	bestCount := 0
	for delta, count := range freq {
		if delta <= 0 {
			continue
		}
		if count > bestCount || (count == bestCount && (bestDelta == 0 || delta < bestDelta)) {
			bestDelta = delta
			bestCount = count
		}
	}
	if bestDelta <= 0 {
		return 0, false
	}
	return bestDelta, true
}

func checkSpotKlineMVStatus(ctx context.Context, conn driver.Conn) {
	rows, err := conn.Query(ctx, `
SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name LIKE 'crypto_spot_bar_%_mv'
`)
	if err != nil {
		log.Printf("[WARN] cannot verify spot kline materialized views: %v", err)
		return
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			log.Printf("[WARN] cannot scan spot kline materialized view count: %v", err)
			return
		}
	}

	expected := uint64(len(cryptooptions.KlineIntervals))
	if count < expected {
		log.Printf("[WARN] spot multi-interval views incomplete: found=%d expected=%d; run with --init-schema=true to auto-create", count, expected)
		return
	}

	log.Printf("Spot multi-interval generation is enabled via materialized views (%d/%d)", count, expected)
}

func parseCSVTimestamp(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format")
}

func parseCSVFloat32(v string) (float32, error) {
	f, err := parseCSVFloat64(v)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

func parseCSVFloat64(v string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid float")
	}
	return f, nil
}

func csvField(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return rec[idx]
}

func effectiveLength(p *juliaSpotJSON) (int, error) {
	if len(p.Timestamp) == 0 || len(p.Open) == 0 || len(p.Low) == 0 || len(p.Close) == 0 {
		return 0, fmt.Errorf("required arrays must not be empty: Timestamp/Open/Low/Close")
	}

	n := len(p.Timestamp)
	if len(p.Open) < n {
		n = len(p.Open)
	}
	if len(p.Low) < n {
		n = len(p.Low)
	}
	if len(p.Close) < n {
		n = len(p.Close)
	}
	return n, nil
}

func parseEpoch(v json.Number) (time.Time, error) {
	i, err := v.Int64()
	if err != nil {
		f, ferr := v.Float64()
		if ferr != nil {
			return time.Time{}, err
		}
		i = int64(math.Round(f))
	}

	switch {
	case i > 1e18:
		return time.Unix(0, i), nil // ns
	case i > 1e15:
		return time.UnixMicro(i), nil // us
	case i > 1e12:
		return time.UnixMilli(i), nil // ms
	default:
		return time.Unix(i, 0), nil // sec
	}
}

func volumeToUInt32(v float64) uint32 {
	if math.IsNaN(v) || v <= 0 {
		return 1
	}
	if v > float64(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(math.Round(v))
}

func roundedNonNegativeUInt32(v float64) uint32 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	if v > float64(math.MaxUint32) {
		return math.MaxUint32
	}
	return uint32(math.Round(v))
}

func sanitizeWeightSeed(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	return v
}

func max3(a, b, c float32) float32 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
