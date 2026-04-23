package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

var coldStartDate = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	batchSize := flag.Int("batch-size", 100000, "Rows per INSERT batch")
	workers := flag.Int("workers", 2, "Number of parallel file importers")
	riskFreeRate := flag.Float64("risk-free-rate", 0.05, "Annualized risk-free rate used for option greeks")
	schemaFile := flag.String("schema", "", "Path to DDL SQL file (auto-detected if empty)")
	skipExisting := flag.Bool("skip-existing", true, "Skip files whose date already has data in ClickHouse")
	skipGreeksBackfill := flag.Bool("skip-greeks-backfill", false, "Skip the automatic local recalculation of missing option greeks after import")
	forceDownload := flag.Bool("force-download", false, "Force re-download even if a flat file already exists in the local cache")
	startDateFlag := flag.String("start-date", "", "Override sync start market date (YYYY-MM-DD)")
	endDateFlag := flag.String("end-date", "", "Override sync end market date (YYYY-MM-DD); defaults to yesterday when omitted")
	dateListFlag := flag.String("dates", "", "Comma-separated explicit market dates to sync (YYYY-MM-DD,YYYY-MM-DD)")
	dateFileFlag := flag.String("dates-file", "", "Path to a file containing one explicit market date per line")
	confirmColdStart := flag.Bool("confirm-cold-start", false, "Acknowledge that empty us-market tables will start syncing from the requested start date")
	flag.Parse()

	if *workers < 1 {
		*workers = 1
	}
	if strings.TrimSpace(runtimeCfg.Polygon.FlatFilesCacheDir) == "" {
		log.Fatal("polygon.flat_files_cache_dir is required; specify a local cache directory in runtime config")
	}
	requestedStartDate, requestedEndDate, specificDates, err := resolveRequestedSyncScope(*startDateFlag, *endDateFlag, *dateListFlag, *dateFileFlag)
	if err != nil {
		log.Fatalf("parse sync scope: %v", err)
	}
	effectiveColdStartDate := coldStartDate
	if len(specificDates) > 0 {
		effectiveColdStartDate = specificDates[0]
	} else if !requestedStartDate.IsZero() {
		effectiveColdStartDate = requestedStartDate
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
	assetStates, err := usmarket.InspectFlatFileAssetStates(ctx, conn)
	if err != nil {
		log.Fatalf("inspect existing us-market data: %v", err)
	}
	if err := requireColdStartConfirmation(assetStates, *confirmColdStart, effectiveColdStartDate); err != nil {
		log.Fatal(err)
	}

	sessions, err := usmarket.InitializeImportStorageWithOptions(ctx, conn, ddlFile, usmarket.ImportStorageOptions{
		PrecomputedCoverageScope: coverageBootstrapScope(requestedStartDate, requestedEndDate, specificDates),
	})
	if err != nil {
		log.Fatalf("initialize import storage: %v", err)
	}

	result, err := usmarket.SyncPolygonFlatFiles(ctx, usmarket.FlatFileSyncConfig{
		Downloader:    polygonSvc,
		Conn:          conn,
		Sessions:      sessions,
		ForceDownload: *forceDownload,
		ColdStartDate: effectiveColdStartDate,
		StartDate:     requestedStartDate,
		EndDate:       requestedEndDate,
		SpecificDates: specificDates,
		Import: usmarket.ImportConfig{
			DSN:          *dsn,
			BatchSize:    *batchSize,
			Workers:      *workers,
			SkipExisting: *skipExisting && len(specificDates) == 0,
			ReplaceDates: len(specificDates) > 0,
			RiskFreeRate: *riskFreeRate,
		},
	})
	if err != nil {
		log.Fatalf("sync Polygon flat files: %v", err)
	}

	log.Printf("Stocks sync: start=%s last_imported=%s last_downloaded=%s scan_end=%s downloaded=%d",
		formatDate(result.Stocks.StartDate),
		formatDateIfPresent(result.Stocks.LastImported, result.Stocks.HasImportedData),
		formatDate(result.Stocks.LastDownloaded),
		formatDate(result.Stocks.ScanEnd),
		len(result.Stocks.Files),
	)
	log.Printf("Options sync: start=%s last_imported=%s last_downloaded=%s scan_end=%s downloaded=%d",
		formatDate(result.Options.StartDate),
		formatDateIfPresent(result.Options.LastImported, result.Options.HasImportedData),
		formatDate(result.Options.LastDownloaded),
		formatDate(result.Options.ScanEnd),
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

	if !*skipGreeksBackfill {
		backfillPaths := append([]string{}, result.Stocks.Files...)
		backfillPaths = append(backfillPaths, result.Options.Files...)
		from, to, ok, err := usmarket.ResolveCSVDateRange(backfillPaths)
		if err != nil {
			log.Fatalf("resolve local greek backfill range: %v", err)
		}
		if ok {
			backfillResult, err := usmarket.BackfillMissingOptionGreeks(ctx, usmarket.OptionGreeksBackfillConfig{
				Conn:              conn,
				DSN:               *dsn,
				StartDate:         from,
				EndDate:           to,
				Workers:           *workers,
				BatchSize:         *batchSize,
				RiskFreeRate:      *riskFreeRate,
				RebuildAggregates: true,
			})
			if err != nil {
				log.Fatalf("auto backfill missing option greeks: %v", err)
			}
			log.Printf("Auto greek backfill: range=%s..%s processed=%d backfilled_rows=%d remaining_tasks=%d",
				from.Format("2006-01-02"),
				to.Format("2006-01-02"),
				backfillResult.ProcessedTasks,
				backfillResult.BackfilledRows,
				backfillResult.RemainingTasks,
			)
			if backfillResult.RemainingTasks > 0 {
				log.Printf("Auto greek backfill left unresolved tasks in %s..%s; those rows still have no local underlying minute data to support calculation", from.Format("2006-01-02"), to.Format("2006-01-02"))
			}
		}
	}
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

func requireColdStartConfirmation(states []usmarket.FlatFileAssetState, confirmed bool, startDate time.Time) error {
	missingAssets := coldStartAssetClasses(states)
	if len(missingAssets) == 0 {
		return nil
	}
	if startDate.IsZero() {
		startDate = coldStartDate
	}
	startLabel := startDate.Format("2006-01-02")
	if !confirmed {
		return fmt.Errorf("no existing data found for %s; rerun with --confirm-cold-start to allow syncing from %s", strings.Join(missingAssets, ", "), startLabel)
	}

	reader := bufio.NewReader(os.Stdin)
	_, _ = fmt.Fprintf(os.Stdout, "No existing data found for %s. This will start syncing from %s. Type %q to continue: ", strings.Join(missingAssets, ", "), startLabel, startLabel)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read cold-start confirmation: %w", err)
	}
	if strings.TrimSpace(response) != startLabel {
		return fmt.Errorf("cold-start confirmation declined")
	}
	return nil
}

func resolveRequestedSyncScope(startValue, endValue, datesValue, dateFile string) (time.Time, time.Time, []time.Time, error) {
	specificDates, err := resolveSpecificDates(datesValue, dateFile)
	if err != nil {
		return time.Time{}, time.Time{}, nil, err
	}
	if len(specificDates) > 0 {
		if strings.TrimSpace(startValue) != "" || strings.TrimSpace(endValue) != "" {
			return time.Time{}, time.Time{}, nil, fmt.Errorf("--dates/--dates-file cannot be combined with --start-date/--end-date")
		}
		return time.Time{}, time.Time{}, specificDates, nil
	}

	start, err := parseOptionalDate(startValue)
	if err != nil {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("start-date: %w", err)
	}
	end, err := parseOptionalDate(endValue)
	if err != nil {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("end-date: %w", err)
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("end-date %s is before start-date %s", end.Format("2006-01-02"), start.Format("2006-01-02"))
	}
	return start, end, nil, nil
}

func parseOptionalDate(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("must be YYYY-MM-DD: %w", err)
	}
	return parsed.UTC(), nil
}

func coverageBootstrapScope(startDate, endDate time.Time, specificDates []time.Time) usmarket.KlineBackfillOptions {
	if len(specificDates) > 0 {
		return usmarket.KlineBackfillOptions{}
	}
	if startDate.IsZero() && endDate.IsZero() {
		return usmarket.KlineBackfillOptions{}
	}
	scope := usmarket.KlineBackfillOptions{From: startDate}
	if !endDate.IsZero() {
		scope.To = endDate.Add(24 * time.Hour)
	}
	return scope
}

func resolveSpecificDates(datesValue, dateFile string) ([]time.Time, error) {
	var raw []string
	if trimmed := strings.TrimSpace(datesValue); trimmed != "" {
		for _, part := range strings.Split(trimmed, ",") {
			raw = append(raw, part)
		}
	}
	if trimmed := strings.TrimSpace(dateFile); trimmed != "" {
		file, err := os.Open(trimmed)
		if err != nil {
			return nil, fmt.Errorf("open dates-file: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			raw = append(raw, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read dates-file: %w", err)
		}
	}
	if len(raw) == 0 {
		return nil, nil
	}

	seen := make(map[string]time.Time, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parsed, err := time.Parse("2006-01-02", trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid explicit date %q: %w", trimmed, err)
		}
		parsed = parsed.UTC()
		seen[parsed.Format("2006-01-02")] = parsed
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]time.Time, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

func coldStartAssetClasses(states []usmarket.FlatFileAssetState) []string {
	assets := make([]string, 0)
	for _, state := range states {
		if state.HasData {
			continue
		}
		assets = append(assets, state.AssetClass)
	}
	return assets
}
