package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Cyvadra/toktik/pkg/dvol"
)

func main() {
	currenciesArg := flag.String("currencies", strings.Join(dvol.DefaultCurrencies, ","), "Comma-separated currencies to sync")
	resolution := flag.String("resolution", "60", "DVOL resolution: 1, 60, 3600, 43200, 86400 (or aliases 1s/1m/1h/12h/1d)")
	startDate := flag.String("start-date", "2020-01-01", "Start date (YYYY-MM-DD, UTC)")
	endDate := flag.String("end-date", time.Now().UTC().Format("2006-01-02"), "End date inclusive (YYYY-MM-DD, UTC)")
	baseURL := flag.String("base-url", dvol.DefaultBaseURL, "Deribit API base URL")
	chDSN := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
	schemaFile := flag.String("schema", "", "ClickHouse DDL file path")
	batchSize := flag.Int("batch-size", 5000, "Insert batch size")
	flag.Parse()

	startDay, err := time.Parse("2006-01-02", *startDate)
	if err != nil {
		log.Fatalf("invalid start-date: %v", err)
	}
	endDay, err := time.Parse("2006-01-02", *endDate)
	if err != nil {
		log.Fatalf("invalid end-date: %v", err)
	}
	if endDay.Before(startDay) {
		log.Fatalf("end-date must be >= start-date")
	}

	// Convert end date to inclusive range end: [start, end+1day).
	rangeStart := startDay.UTC()
	rangeEnd := endDay.UTC().Add(24 * time.Hour)

	if *schemaFile == "" {
		for _, c := range []string{
			"schema/clickhouse/deribit_dvol.sql",
			"../../../../../schema/clickhouse/deribit_dvol.sql",
			"../../../../schema/clickhouse/deribit_dvol.sql",
			"../../../schema/clickhouse/deribit_dvol.sql",
			"../../schema/clickhouse/deribit_dvol.sql",
			"../schema/clickhouse/deribit_dvol.sql",
		} {
			if _, err := os.Stat(c); err == nil {
				*schemaFile = c
				break
			}
		}
	}

	currencies := parseCurrencies(*currenciesArg)
	if len(currencies) == 0 {
		log.Fatalf("no currencies specified")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %v, shutting down...", sig)
		cancel()
	}()

	client := dvol.NewClient(*baseURL)
	store, err := dvol.NewStore(ctx, *chDSN)
	if err != nil {
		log.Fatalf("connect clickhouse: %v", err)
	}
	defer store.Close()

	if *schemaFile != "" {
		abs := *schemaFile
		if !filepath.IsAbs(abs) {
			if p, err := filepath.Abs(abs); err == nil {
				abs = p
			}
		}
		if err := store.InitSchema(ctx, abs); err != nil {
			log.Fatalf("init schema: %v", err)
		}
		log.Printf("schema initialized from %s", abs)
	}

	log.Printf("Deribit DVOL sync")
	log.Printf("  base URL:    %s", *baseURL)
	log.Printf("  resolution:  %s", *resolution)
	log.Printf("  range:       %s to %s", rangeStart.Format(time.RFC3339), rangeEnd.Format(time.RFC3339))
	log.Printf("  accepted currencies (probe): %v", dvol.AcceptedCurrencies)
	log.Printf("  accepted resolutions: %v", dvol.AcceptedResolutions)
	log.Printf("  currencies:  %v", currencies)

	totalInserted := 0
	supported := 0
	for _, currency := range currencies {
		if err := ctx.Err(); err != nil {
			log.Printf("context canceled, stopping")
			break
		}

		ok, err := client.SupportsCurrency(ctx, currency, *resolution)
		if err != nil {
			log.Printf("[%s] support probe error: %v", currency, err)
			continue
		}
		if !ok {
			log.Printf("[%s] not supported by Deribit DVOL endpoint, skip", currency)
			continue
		}
		supported++

		rows, err := client.GetHistory(ctx, currency, *resolution, rangeStart, rangeEnd)
		if err != nil {
			log.Printf("[%s] fetch error: %v", currency, err)
			continue
		}
		if len(rows) == 0 {
			log.Printf("[%s] supported but no rows in requested range", currency)
			continue
		}

		inserted, err := store.InsertBars(ctx, rows, *batchSize)
		if err != nil {
			log.Printf("[%s] insert error after %d rows: %v", currency, inserted, err)
			continue
		}
		totalInserted += inserted
		log.Printf("[%s] fetched=%d inserted=%d first=%s last=%s", currency, len(rows), inserted, rows[0].Timestamp.Format(time.RFC3339), rows[len(rows)-1].Timestamp.Format(time.RFC3339))
	}

	log.Printf("sync finished: supported=%d/%d total_inserted=%d", supported, len(currencies), totalInserted)
	if supported == 0 {
		fmt.Fprintln(os.Stderr, "warning: no supported currencies found for current endpoint/inputs")
	}
}

func parseCurrencies(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		c := strings.ToUpper(strings.TrimSpace(p))
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
