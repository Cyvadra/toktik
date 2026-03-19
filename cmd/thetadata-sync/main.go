package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Cyvadra/toktik/pkg/thetadata"
)

func main() {
	roots := flag.String("roots", "AAPL,SPY", "Comma-separated root symbols (or * for all)")
	startDate := flag.String("start-date", "2019-01-01", "Start date (YYYY-MM-DD)")
	endDate := flag.String("end-date", "2026-02-28", "End date (YYYY-MM-DD)")
	baseURL := flag.String("base-url", "http://127.0.0.1:25503", "Theta Data terminal base URL")
	chDSN := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
	workers := flag.Int("workers", 4, "Concurrent workers")
	rateLimit := flag.Float64("rate-limit", 10.0, "Max total requests/sec")
	progressDir := flag.String("progress-dir", ".thetadata-progress", "Progress tracking directory")
	schemaFile := flag.String("schema", "", "ClickHouse DDL SQL file path")
	debug := flag.Bool("debug", false, "Enable verbose logging")
	flag.Parse()

	sd, err := time.Parse("2006-01-02", *startDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid start-date: %v\n", err)
		os.Exit(1)
	}
	ed, err := time.Parse("2006-01-02", *endDate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid end-date: %v\n", err)
		os.Exit(1)
	}

	// Auto-detect schema file.
	if *schemaFile == "" {
		for _, c := range []string{
			"schema/clickhouse/equity_options.sql",
			"../schema/clickhouse/equity_options.sql",
			"../../schema/clickhouse/equity_options.sql",
		} {
			if _, err := os.Stat(c); err == nil {
				*schemaFile = c
				break
			}
		}
	}

	client := thetadata.NewClient(*baseURL)

	// Resolve root list.
	rootList := parseRoots(*roots)
	if len(rootList) == 1 && (rootList[0] == "*" || strings.EqualFold(rootList[0], "all")) {
		ctx := context.Background()
		var discoverErr error
		rootList, discoverErr = client.ListSymbols(ctx)
		if discoverErr != nil {
			log.Fatalf("Discover all roots: %v", discoverErr)
		}
		log.Printf("Discovered %d option roots from Theta Data", len(rootList))
	}

	if len(rootList) == 0 {
		log.Fatalf("No root symbols specified")
	}

	cfg := thetadata.SyncConfig{
		Roots:       rootList,
		StartDate:   sd,
		EndDate:     ed,
		BaseURL:     *baseURL,
		CHDSN:       *chDSN,
		Workers:     *workers,
		RateLimit:   *rateLimit,
		ProgressDir: *progressDir,
		SchemaFile:  *schemaFile,
		Debug:       *debug,
	}

	log.Printf("Theta Data Sync v2")
	log.Printf("  Roots:      %v", cfg.Roots)
	log.Printf("  Date range: %s to %s", cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"))
	log.Printf("  Base URL:   %s", cfg.BaseURL)
	log.Printf("  Workers:    %d", cfg.Workers)
	log.Printf("  Rate limit: %.1f req/s", cfg.RateLimit)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %v, shutting down...", sig)
		cancel()
	}()

	store, err := thetadata.NewStore(ctx, cfg.CHDSN)
	if err != nil {
		log.Fatalf("ClickHouse: %v", err)
	}
	defer store.Close()
	log.Printf("Connected to ClickHouse")

	if cfg.SchemaFile != "" {
		if err := store.InitSchema(ctx, cfg.SchemaFile); err != nil {
			log.Fatalf("Schema init: %v", err)
		}
		log.Printf("Schema initialized")
	}

	progress, err := thetadata.NewProgress(cfg.ProgressDir)
	if err != nil {
		log.Fatalf("Progress init: %v", err)
	}
	log.Printf("Progress: %d dates completed", progress.CompletedCount())

	pipeline := thetadata.NewPipeline(cfg, client, store, progress)
	if err := pipeline.Run(ctx); err != nil {
		if ctx.Err() != nil {
			log.Printf("Interrupted. Progress saved. Re-run to resume.")
			os.Exit(0)
		}
		log.Fatalf("Pipeline: %v", err)
	}

	log.Printf("Done! Total: %d dates completed", progress.CompletedCount())
}

func parseRoots(input string) []string {
	parts := strings.Split(input, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		root := strings.TrimSpace(part)
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		result = append(result, root)
	}
	return result
}
