package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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
	jsonFile := flag.String("json-file", "btc2023_2025.json", "Path to Julia-exported JSON file")
	dsn := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
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

	ctx := context.Background()

	conn, err := cryptooptions.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	log.Printf("Connected to ClickHouse")

	if *initSchema {
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
				log.Fatalf("cannot find schema SQL file, set --schema")
			}
		}

		if err := cryptooptions.InitSchema(ctx, conn, *schemaFile); err != nil {
			log.Fatalf("init schema: %v", err)
		}
		if err := cryptooptions.InitSpotKlineSchema(ctx, conn); err != nil {
			log.Fatalf("init spot kline schema: %v", err)
		}
		log.Printf("Schema initialized")
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

	payload, err := loadJSON(*jsonFile)
	if err != nil {
		log.Fatalf("load json: %v", err)
	}

	n, err := effectiveLength(payload)
	if err != nil {
		log.Fatalf("invalid json series: %v", err)
	}
	log.Printf("Input rows: %d", n)

	barCh := make(chan cryptooptions.SpotBar1m, 8192)

	go func() {
		defer close(barCh)

		for i := 0; i < n; i++ {
			ts, err := parseEpoch(payload.Timestamp[i])
			if err != nil {
				log.Printf("[WARN] skip row=%d bad timestamp=%q err=%v", i, payload.Timestamp[i], err)
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

			barCh <- cryptooptions.SpotBar1m{
				Timestamp:   ts.UTC(),
				Symbol:      *symbol,
				PriceSource: *priceSource,
				Open:        open,
				High:        high,
				Low:         low,
				Close:       closeP,
				TickCount:   tickCount,
			}

			if (i+1)%500000 == 0 {
				log.Printf("prepared %d rows", i+1)
			}
		}
	}()

	start := time.Now()
	rows, err := cryptooptions.InsertSpotBars(ctx, conn, barCh, *batchSize)
	if err != nil {
		log.Fatalf("insert spot bars: %v", err)
	}

	log.Printf("Import done: inserted %d rows in %s", rows, time.Since(start).Round(time.Second))
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
