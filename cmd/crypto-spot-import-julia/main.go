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

func main() {
	jsonFile := flag.String("json-file", "btc2023_2025.json", "Path to Julia-exported JSON file (deprecated, use --input-file)")
	inputFile := flag.String("input-file", "", "Path to input file (.json or .csv). If empty, --json-file is used")
	inputFormat := flag.String("format", "auto", "Input format: auto|json|csv")
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
	symbol := flag.String("symbol", "BTC", "Spot symbol written to crypto_spot_bar_1m.symbol")
	priceSource := flag.String("price-source", "julia-json", "Value for crypto_spot_bar_1m.price_source")
	batchSize := flag.Int("batch-size", 50000, "Rows per INSERT batch")
	overwrite := flag.Bool("overwrite", true, "Delete existing rows for symbol across 1m and all spot interval agg tables before import")
	initSchema := flag.Bool("init-schema", true, "Initialize base schema + spot kline schema before import")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	flag.Parse()

	if *batchSize < 1 {
		*batchSize = 50000
	}

	path := *inputFile
	if path == "" {
		path = *jsonFile
	}

	format, err := resolveInputFormat(path, *inputFormat)
	if err != nil {
		log.Fatalf("resolve input format: %v", err)
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

	log.Printf("Input file: %s (format=%s)", path, format)
	checkSpotKlineMVStatus(ctx, conn)

	bars, err := loadInputBars(path, format, *symbol, *priceSource)
	if err != nil {
		log.Fatalf("load input bars: %v", err)
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
		return parseJSONBars(path, symbol, priceSource)
	case "csv":
		return parseCSVBars(path, symbol, priceSource)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
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

func parseJSONBars(path, symbol, priceSource string) ([]cryptooptions.SpotBar1m, error) {
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
	log.Printf("Input rows: %d", n)

	bars := make([]cryptooptions.SpotBar1m, 0, n)
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

		tickCount := uint32(1)
		if len(payload.Volume) > i {
			tickCount = volumeToUInt32(payload.Volume[i])
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

		if (i+1)%500000 == 0 {
			log.Printf("prepared %d rows", i+1)
		}
	}
	if skipped > 0 {
		log.Printf("[WARN] JSON rows skipped: %d", skipped)
	}

	return bars, nil
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
	if len(p.High) > 0 && len(p.High) < n {
		n = len(p.High)
	}
	if len(p.Volume) > 0 && len(p.Volume) < n {
		n = len(p.Volume)
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
