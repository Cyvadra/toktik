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
	roots := flag.String("roots", "AAPL,SPY", "Comma-separated root symbols")
	startDate := flag.String("start-date", "2019-01-01", "Start date (YYYY-MM-DD)")
	endDate := flag.String("end-date", "2026-02-28", "End date (YYYY-MM-DD)")
	mcpURL := flag.String("mcp-url", "http://127.0.0.1:25503", "Theta Data MCP server URL")
	chDSN := flag.String("clickhouse-dsn", "clickhouse://default:@localhost:9000/default", "ClickHouse DSN")
	workers := flag.Int("workers", 4, "Concurrent download workers")
	progressDir := flag.String("progress-dir", ".thetadata-progress", "Progress tracking directory")
	minVolume := flag.Int("min-volume", 1, "Min daily volume for 1m download")
	rateLimit := flag.Float64("rate-limit", 5.0, "Max requests/sec per worker")
	schemaFile := flag.String("schema", "", "ClickHouse DDL SQL file path")
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

	if *schemaFile == "" {
		for _, c := range []string{
			"schema/clickhouse/crypto_options.sql",
			"../schema/clickhouse/crypto_options.sql",
			"../../schema/clickhouse/crypto_options.sql",
		} {
			if _, err := os.Stat(c); err == nil {
				*schemaFile = c
				break
			}
		}
	}

	rootList := strings.Split(*roots, ",")
	for i := range rootList {
		rootList[i] = strings.TrimSpace(rootList[i])
	}

	cfg := thetadata.SyncConfig{
		Roots:       rootList,
		StartDate:   sd,
		EndDate:     ed,
		MCPURL:      *mcpURL,
		CHDSN:       *chDSN,
		Workers:     *workers,
		ProgressDir: *progressDir,
		MinVolume:   *minVolume,
		RateLimit:   *rateLimit,
		SchemaFile:  *schemaFile,
	}

	log.Printf("Theta Data Sync")
	log.Printf("  Roots:      %v", cfg.Roots)
	log.Printf("  Date range: %s to %s",
		cfg.StartDate.Format("2006-01-02"), cfg.EndDate.Format("2006-01-02"))
	log.Printf("  MCP URL:    %s", cfg.MCPURL)
	log.Printf("  Workers:    %d", cfg.Workers)
	log.Printf("  Rate limit: %.1f req/s/worker", cfg.RateLimit)

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

	pipeline := thetadata.NewPipeline(cfg, store, progress)
	if err := pipeline.Run(ctx); err != nil {
		if ctx.Err() != nil {
			log.Printf("Interrupted. Progress saved. Re-run to resume.")
			os.Exit(0)
		}
		log.Fatalf("Pipeline: %v", err)
	}

	log.Printf("Done! Total: %d dates", progress.CompletedCount())
}
