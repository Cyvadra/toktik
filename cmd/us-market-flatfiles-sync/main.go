package main

import (
	"context"
	"flag"
	"log"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	batchSize := flag.Int("batch-size", 100000, "Rows per INSERT batch")
	workers := flag.Int("workers", 2, "Number of parallel file importers")
	riskFreeRate := flag.Float64("risk-free-rate", 0.05, "Annualized risk-free rate used for option greeks")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	skipExisting := flag.Bool("skip-existing", true, "Skip files whose date already has data in ClickHouse")
	forceDownload := flag.Bool("force-download", false, "Force re-download even if a flat file already exists in the local cache")
	flag.Parse()

	if *workers < 1 {
		*workers = 1
	}

	ctx := context.Background()
	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.UsMarketSchemaFile)
	if err != nil {
		log.Fatalf("resolve us_market.sql schema: %v", err)
	}

	polygonSvc, err := service.NewPolygonServiceFromConfig(runtimeCfg, nil)
	if err != nil {
		log.Fatalf("init polygon flatfile client: %v", err)
	}

	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	sessions, err := usmarket.InitializeImportStorage(ctx, conn, ddlFile)
	if err != nil {
		log.Fatalf("initialize import storage: %v", err)
	}

	result, err := usmarket.SyncPolygonFlatFiles(ctx, usmarket.FlatFileSyncConfig{
		Downloader:    polygonSvc,
		Conn:          conn,
		Sessions:      sessions,
		ForceDownload: *forceDownload,
		Import: usmarket.ImportConfig{
			DSN:          *dsn,
			BatchSize:    *batchSize,
			Workers:      *workers,
			SkipExisting: *skipExisting,
			RiskFreeRate: *riskFreeRate,
		},
	})
	if err != nil {
		log.Fatalf("sync Polygon flat files: %v", err)
	}

	log.Printf("Stocks sync: start=%s last_imported=%s last_available=%s downloaded=%d",
		formatDate(result.Stocks.StartDate),
		formatDateIfPresent(result.Stocks.LastImported, result.Stocks.HasImportedData),
		formatDate(result.Stocks.LastAvailable),
		len(result.Stocks.Files),
	)
	log.Printf("Options sync: start=%s last_imported=%s last_available=%s downloaded=%d",
		formatDate(result.Options.StartDate),
		formatDateIfPresent(result.Options.LastImported, result.Options.HasImportedData),
		formatDate(result.Options.LastAvailable),
		len(result.Options.Files),
	)
	log.Printf("Import complete: %d files succeeded, %d skipped, %d failed, %d option rows, %d stock rows, elapsed %s",
		result.Import.CompletedFiles,
		result.Import.SkippedFiles,
		result.Import.FailedFiles,
		result.Import.OptionRows,
		result.Import.StockRows,
		result.Import.Elapsed.Round(time.Second),
	)
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "n/a"
	}
	return value.UTC().Format("2006-01-02")
}

func formatDateIfPresent(value time.Time, ok bool) string {
	if !ok {
		return "n/a"
	}
	return formatDate(value)
}
