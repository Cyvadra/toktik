package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Cyvadra/toktik/internal/chrepo"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	schemaFile := flag.String("schema", "", "Path to feature store DDL SQL file (auto-detected if empty)")
	marketsFlag := flag.String("markets", "crypto-options,us-options", "Comma-separated markets to backfill")
	underlyingsFlag := flag.String("underlyings", "", "Optional comma-separated underlying filter")
	priorityOrder := flag.String("priority-order", usmarket.PriorityOrderUSDefault, "Underlying execution priority order for us-options (none, us-default)")
	fromFlag := flag.String("from", "", "Optional start date (YYYY-MM-DD), inclusive")
	toFlag := flag.String("to", "", "Optional end date (YYYY-MM-DD), exclusive via next-day rule")
	incrementalDays := flag.Int("incremental-days", 0, "If > 0 and --from/--to are omitted, backfill only the last N calendar days")
	lookbackDays := flag.Int("lookback-days", 252, "Lookback window for IV percentile/rank")
	minDaysToExpiry := flag.Int("min-days-to-expiry", 0, "Minimum days to expiry for precomputed daily panels")
	maxDaysToExpiry := flag.Int("max-days-to-expiry", 365, "Maximum days to expiry for precomputed daily panels")
	workers := flag.Int("workers", 4, "Number of underlyings to backfill concurrently")
	replace := flag.Bool("replace", false, "Replace existing rows in the selected scope before backfill")
	flag.Parse()
	startedAt := time.Now().UTC()

	from, to, err := resolveBackfillRange(*fromFlag, *toFlag, *incrementalDays, time.Now().UTC())
	if err != nil {
		log.Fatalf("resolve backfill range: %v", err)
	}
	resolvedPriorityOrder, err := usmarket.NormalizeUSPriorityOrder(*priorityOrder)
	if err != nil {
		log.Fatalf("resolve priority order: %v", err)
	}
	if *workers < 1 {
		*workers = 1
	}

	ddlFile, err := appCli.ResolveSchemaFile(*schemaFile, appCli.FeatureStoreSchemaFile)
	if err != nil {
		log.Fatalf("resolve feature store schema: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	if err := cryptooptions.InitSchema(ctx, conn, ddlFile); err != nil {
		log.Fatalf("init feature store schema: %v", err)
	}

	featureSvc := service.NewFeatureService(chrepo.NewRepo(conn))
	stats, err := featureSvc.BackfillFeatureSnapshots(ctx, service.FeatureBackfillOptions{
		Markets:         splitCSV(*marketsFlag),
		Underlyings:     splitCSV(*underlyingsFlag),
		PriorityOrder:   resolvedPriorityOrder,
		ClickHouseDSN:   *dsn,
		From:            from,
		To:              to,
		LookbackDays:    *lookbackDays,
		MinDaysToExpiry: *minDaysToExpiry,
		MaxDaysToExpiry: *maxDaysToExpiry,
		Workers:         *workers,
		Replace:         *replace,
		ContinueOnError: true,
		Progress: func(progress service.FeatureBackfillProgress) {
			slog.Info(formatBackfillProgress(progress))
		},
	})
	for _, failure := range stats.Failures {
		fmt.Fprintf(os.Stderr, "feature-store-backfill failure: market=%s underlying=%s stage=%s error=%s\n", failure.Market, failure.Underlying, failure.Stage, failure.Error)
	}
	if err != nil {
		fmt.Fprintln(os.Stdout, formatBackfillSummary(stats, startedAt, time.Now().UTC(), from, to, *replace))
		if errors.Is(err, context.Canceled) {
			log.Printf("feature store backfill interrupted")
			os.Exit(130)
		}
		log.Fatalf("backfill feature store datasets: %v", err)
	}

	fmt.Fprintln(os.Stdout, formatBackfillSummary(stats, startedAt, time.Now().UTC(), from, to, *replace))
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		normalized := strings.ToLower(trimmed)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		items = append(items, normalized)
	}
	sort.Strings(items)
	return items
}

func parseOptionalDate(value string, isFrom bool) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return time.Time{}, err
	}
	if isFrom {
		return t.UTC(), nil
	}
	return t.UTC().AddDate(0, 0, 1), nil
}

func resolveBackfillRange(fromValue, toValue string, incrementalDays int, now time.Time) (time.Time, time.Time, error) {
	if strings.TrimSpace(fromValue) != "" || strings.TrimSpace(toValue) != "" {
		from, err := parseOptionalDate(fromValue, true)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from: %w", err)
		}
		to, err := parseOptionalDate(toValue, false)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --to: %w", err)
		}
		if !from.IsZero() && !to.IsZero() && !to.After(from) {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid time range: --to must be after --from")
		}
		return from, to, nil
	}
	if incrementalDays < 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("--incremental-days must be >= 0")
	}
	if incrementalDays == 0 {
		return time.Time{}, time.Time{}, nil
	}
	today := now.UTC().Truncate(24 * time.Hour)
	to := today.AddDate(0, 0, 1)
	from := today.AddDate(0, 0, -(incrementalDays - 1))
	return from, to, nil
}

func formatBackfillSummary(stats service.FeatureBackfillStats, startedAt, finishedAt, from, to time.Time, replace bool) string {
	duration := finishedAt.Sub(startedAt).Round(time.Millisecond)
	parts := []string{
		"Feature store backfill completed:",
		fmt.Sprintf("markets=%d", stats.MarketsProcessed),
		fmt.Sprintf("underlyings_considered=%d", stats.UnderlyingsConsidered),
		fmt.Sprintf("underlyings_written=%d", stats.UnderlyingsWritten),
		fmt.Sprintf("underlyings_skipped=%d", stats.UnderlyingsSkipped),
		fmt.Sprintf("underlyings_empty=%d", stats.UnderlyingsEmpty),
		fmt.Sprintf("rows_written=%d", stats.RowsWritten),
		fmt.Sprintf("scopes_replaced=%d", stats.ScopesReplaced),
		fmt.Sprintf("lookback_days=%d", stats.LookbackDays),
		fmt.Sprintf("failures=%d", len(stats.Failures)),
		fmt.Sprintf("replace=%t", replace),
		fmt.Sprintf("elapsed=%s", duration),
	}
	if !from.IsZero() {
		parts = append(parts, fmt.Sprintf("from=%s", from.Format("2006-01-02")))
	}
	if !to.IsZero() {
		parts = append(parts, fmt.Sprintf("to=%s", to.AddDate(0, 0, -1).Format("2006-01-02")))
	}
	return strings.Join(parts, " ")
}

func formatBackfillProgress(progress service.FeatureBackfillProgress) string {
	parts := []string{"feature-store-backfill progress:"}
	if progress.Market != "" {
		parts = append(parts, fmt.Sprintf("market=%s", progress.Market))
	}
	if progress.MarketCount > 0 {
		parts = append(parts, fmt.Sprintf("market_index=%d/%d", progress.MarketIndex, progress.MarketCount))
	}
	if progress.Underlying != "" {
		parts = append(parts, fmt.Sprintf("underlying=%s", progress.Underlying))
	}
	if progress.UnderlyingCount > 0 && progress.UnderlyingIndex > 0 {
		parts = append(parts, fmt.Sprintf("underlying_index=%d/%d", progress.UnderlyingIndex, progress.UnderlyingCount))
	} else if progress.UnderlyingCount > 0 {
		parts = append(parts, fmt.Sprintf("underlyings=%d", progress.UnderlyingCount))
	}
	if progress.Scope != "" {
		parts = append(parts, fmt.Sprintf("scope=%s", progress.Scope))
	}
	if progress.Stage != "" {
		parts = append(parts, fmt.Sprintf("stage=%s", progress.Stage))
	}
	if progress.Phase != "" {
		parts = append(parts, fmt.Sprintf("phase=%s", progress.Phase))
	}
	if progress.Outcome != "" {
		parts = append(parts, fmt.Sprintf("outcome=%s", progress.Outcome))
	}
	if progress.RowsWritten > 0 {
		parts = append(parts, fmt.Sprintf("rows_written=%d", progress.RowsWritten))
	}
	if progress.ScopesReplaced > 0 {
		parts = append(parts, fmt.Sprintf("scopes_replaced=%d", progress.ScopesReplaced))
	}
	if progress.Elapsed > 0 {
		parts = append(parts, fmt.Sprintf("elapsed=%s", progress.Elapsed.Round(time.Millisecond)))
	}
	if progress.Error != "" {
		parts = append(parts, fmt.Sprintf("error=%s", progress.Error))
	}
	return strings.Join(parts, " ")
}
