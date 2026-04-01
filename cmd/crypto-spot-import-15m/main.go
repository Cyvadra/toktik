package main

import (
	"bufio"
	"context"
	"encoding/csv"
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

func main() {
	dataDir := flag.String("data-dir", "data/crypto-15m", "Directory containing 15m CSV files (<SYMBOL>USDT.csv)")
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
	priceSource := flag.String("price-source", "binance-15m-csv", "Value for price_source column")
	batchSize := flag.Int("batch-size", 50000, "Rows per INSERT batch")
	overwrite := flag.Bool("overwrite", false, "Delete existing rows for each symbol before import")
	initSchema := flag.Bool("init-schema", true, "Initialize base schema + spot kline schema before import")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	symbolFilter := flag.String("symbol", "", "Import only this symbol (e.g. ETH); empty imports all files")
	btcVolumeDistribute := flag.Bool("btc-volume-distribute", true, "For BTC: distribute 15m volume across existing 1m bars instead of inserting 15m rows")
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

	files, err := filepath.Glob(filepath.Join(*dataDir, "*USDT.csv"))
	if err != nil {
		log.Fatalf("glob data dir: %v", err)
	}
	if len(files) == 0 {
		log.Fatalf("no *USDT.csv files found in %s", *dataDir)
	}

	sort.Strings(files)
	log.Printf("Found %d CSV files in %s", len(files), *dataDir)

	totalImported := int64(0)
	totalSkipped := 0
	for _, csvPath := range files {
		symbol := extractSymbol(csvPath)
		if symbol == "" {
			log.Printf("[WARN] cannot extract symbol from %s, skipping", csvPath)
			totalSkipped++
			continue
		}

		if *symbolFilter != "" && !strings.EqualFold(symbol, *symbolFilter) {
			continue
		}

		if strings.EqualFold(symbol, "BTC") && *btcVolumeDistribute {
			n, err := distributeBTCVolume(ctx, conn, csvPath, *batchSize)
			if err != nil {
				log.Printf("[ERROR] BTC volume distribute from %s: %v", csvPath, err)
				continue
			}
			log.Printf("[BTC] distributed volume across %d 15m windows from %s", n, csvPath)
			totalImported += int64(n)
			continue
		}

		if *overwrite {
			if _, err := deleteSpotRowsBySymbol(ctx, conn, symbol); err != nil {
				log.Printf("[ERROR] delete %s: %v", symbol, err)
				continue
			}
		}

		n, err := importCSV15m(ctx, conn, csvPath, symbol, *priceSource, *batchSize)
		if err != nil {
			log.Printf("[ERROR] import %s from %s: %v", symbol, csvPath, err)
			continue
		}
		totalImported += n
		log.Printf("Imported %s: %d rows from %s", symbol, n, filepath.Base(csvPath))
	}

	log.Printf("Done: imported %d total rows, skipped %d files", totalImported, totalSkipped)
}

func extractSymbol(csvPath string) string {
	base := filepath.Base(csvPath)
	base = strings.TrimSuffix(base, ".csv")
	base = strings.TrimSuffix(base, ".CSV")
	if strings.HasSuffix(base, "USDT") {
		sym := strings.TrimSuffix(base, "USDT")
		if sym == "" {
			return ""
		}
		return strings.ToUpper(sym)
	}
	return ""
}

type csv15mRow struct {
	Timestamp   time.Time
	Open        float32
	High        float32
	Low         float32
	Close       float32
	VolumeBase  float64
	VolumeQuote float64
}

func importCSV15m(ctx context.Context, conn driver.Conn, csvPath, symbol, priceSource string, batchSize int) (int64, error) {
	rows, err := parseCSV15m(csvPath)
	if err != nil {
		return 0, err
	}

	barCh := make(chan cryptooptions.SpotBar1m, 8192)
	go func() {
		defer close(barCh)
		for i := range rows {
			barCh <- cryptooptions.SpotBar1m{
				Timestamp:   rows[i].Timestamp,
				Symbol:      symbol,
				PriceSource: priceSource,
				Open:        rows[i].Open,
				High:        rows[i].High,
				Low:         rows[i].Low,
				Close:       rows[i].Close,
				TickCount:   1,
				VolumeBase:  rows[i].VolumeBase,
				VolumeQuote: rows[i].VolumeQuote,
				BarInterval: "15m",
			}
		}
	}()

	return cryptooptions.InsertSpotBars(ctx, conn, barCh, batchSize)
}

func parseCSV15m(csvPath string) ([]csv15mRow, error) {
	f, err := os.Open(csvPath)
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

	// Required columns
	for _, col := range []string{"timestamp", "open", "high", "low", "close", "volumebase", "volumequote"} {
		if _, ok := headerMap[col]; !ok {
			return nil, fmt.Errorf("csv missing required column %q (have: %v)", col, header)
		}
	}

	tsIdx := headerMap["timestamp"]
	openIdx := headerMap["open"]
	highIdx := headerMap["high"]
	lowIdx := headerMap["low"]
	closeIdx := headerMap["close"]
	vbIdx := headerMap["volumebase"]
	vqIdx := headerMap["volumequote"]

	rows := make([]csv15mRow, 0, 4096)
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

		ts, err := parseTimestamp(csvField(rec, tsIdx))
		if err != nil {
			skipped++
			continue
		}

		open, err := parseFloat32(csvField(rec, openIdx))
		if err != nil {
			skipped++
			continue
		}
		high, err := parseFloat32(csvField(rec, highIdx))
		if err != nil {
			skipped++
			continue
		}
		low, err := parseFloat32(csvField(rec, lowIdx))
		if err != nil {
			skipped++
			continue
		}
		closeP, err := parseFloat32(csvField(rec, closeIdx))
		if err != nil {
			skipped++
			continue
		}
		vb, err := parseFloat64(csvField(rec, vbIdx))
		if err != nil {
			skipped++
			continue
		}
		vq, err := parseFloat64(csvField(rec, vqIdx))
		if err != nil {
			skipped++
			continue
		}

		rows = append(rows, csv15mRow{
			Timestamp:   ts.UTC(),
			Open:        open,
			High:        high,
			Low:         low,
			Close:       closeP,
			VolumeBase:  vb,
			VolumeQuote: vq,
		})
	}

	if skipped > 0 {
		log.Printf("[WARN] %s: skipped %d rows", csvPath, skipped)
	}
	return rows, nil
}

// distributeBTCVolume reads 15m CSV volume data and distributes it across
// existing 1m BTC bars using tick_count as weights.
func distributeBTCVolume(ctx context.Context, conn driver.Conn, csvPath string, batchSize int) (int, error) {
	csv15mRows, err := parseCSV15m(csvPath)
	if err != nil {
		return 0, fmt.Errorf("parse csv: %w", err)
	}

	if len(csv15mRows) == 0 {
		return 0, nil
	}

	sort.Slice(csv15mRows, func(i, j int) bool {
		return csv15mRows[i].Timestamp.Before(csv15mRows[j].Timestamp)
	})

	updated := 0
	for _, row := range csv15mRows {
		windowStart := row.Timestamp
		windowEnd := windowStart.Add(15 * time.Minute)

		// Query existing 1m bars in this 15m window
		minuteBars, err := queryMinuteBars(ctx, conn, "BTC", windowStart, windowEnd)
		if err != nil {
			log.Printf("[WARN] BTC query 1m bars for window %s: %v", windowStart.Format(time.RFC3339), err)
			continue
		}

		if len(minuteBars) == 0 {
			continue
		}

		// Compute total tick_count as weight sum
		totalWeight := float64(0)
		for _, mb := range minuteBars {
			totalWeight += float64(mb.TickCount)
		}
		if totalWeight <= 0 {
			totalWeight = float64(len(minuteBars))
		}

		// Distribute volume proportionally and update
		for _, mb := range minuteBars {
			weight := float64(mb.TickCount) / totalWeight
			if totalWeight == float64(len(minuteBars)) {
				weight = 1.0 / float64(len(minuteBars))
			}

			vb := row.VolumeBase * weight
			vq := row.VolumeQuote * weight

			err := updateSpotBarVolume(ctx, conn, mb.Timestamp, "BTC", vb, vq)
			if err != nil {
				log.Printf("[WARN] BTC update volume at %s: %v", mb.Timestamp.Format(time.RFC3339), err)
				continue
			}
		}
		updated++
	}

	return updated, nil
}

type minuteBarRef struct {
	Timestamp time.Time
	TickCount uint32
}

func queryMinuteBars(ctx context.Context, conn driver.Conn, symbol string, from, to time.Time) ([]minuteBarRef, error) {
	rows, err := conn.Query(ctx,
		`SELECT timestamp, tick_count FROM crypto_spot_bar_1m
		 WHERE symbol = ? AND timestamp >= ? AND timestamp < ?
		 ORDER BY timestamp`,
		symbol, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bars []minuteBarRef
	for rows.Next() {
		var mb minuteBarRef
		if err := rows.Scan(&mb.Timestamp, &mb.TickCount); err != nil {
			return nil, err
		}
		bars = append(bars, mb)
	}
	return bars, nil
}

func updateSpotBarVolume(ctx context.Context, conn driver.Conn, ts time.Time, symbol string, volumeBase, volumeQuote float64) error {
	return conn.Exec(ctx,
		`ALTER TABLE crypto_spot_bar_1m UPDATE
			volume_base = ?,
			volume_quote = ?
		 WHERE symbol = ? AND timestamp = ?
		 SETTINGS mutations_sync=1`,
		volumeBase, volumeQuote, symbol, ts)
}

func deleteSpotRowsBySymbol(ctx context.Context, conn driver.Conn, symbol string) (int, error) {
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

func parseTimestamp(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	// Try epoch seconds first (the 15m CSV uses unix timestamps)
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 1e8 {
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC(), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format: %q", v)
}

func parseFloat32(v string) (float32, error) {
	f, err := parseFloat64(v)
	if err != nil {
		return 0, err
	}
	return float32(f), nil
}

func parseFloat64(v string) (float64, error) {
	v = strings.TrimSpace(v)
	// Handle Julia scientific notation like 1.3769081e6
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("invalid float: %q", v)
	}
	return f, nil
}

func csvField(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return rec[idx]
}
