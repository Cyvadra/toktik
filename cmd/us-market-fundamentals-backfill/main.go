package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/internal/usmarket"
	"github.com/Cyvadra/toktik/pkg/tigerapi"
)

const tigerConfirmationPhrase = "ENABLE_TIGER_PROVIDER"

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	date := flag.String("date", "", "Single market date to backfill (YYYY-MM-DD)")
	startDate := flag.String("start-date", "", "Start market date to backfill (YYYY-MM-DD)")
	endDate := flag.String("end-date", "", "End market date to backfill (YYYY-MM-DD)")
	providerName := flag.String("provider", "disabled", "Fundamentals source provider. Default is disabled; Tiger is sealed behind explicit interactive confirmation")
	symbolsFlag := flag.String("symbols", "", "Optional comma-separated symbols to restrict backfill")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	workers := flag.Int("workers", 2, "Number of parallel backfill workers")
	batchSize := flag.Int("batch-size", 1000, "Rows per ClickHouse INSERT batch")
	pageSize := flag.Int("page-size", 251, "Tiger kline page size per request")
	qps := flag.Int("qps", 5, "Global Tiger request QPS cap across all workers")
	limitSymbols := flag.Int("limit-symbols", 0, "Optional limit of symbols to process")
	dryRun := flag.Bool("dry-run", false, "Only report candidate PE rows without writing to ClickHouse")
	flag.Parse()

	from, to := resolveDateRange(*date, *startDate, *endDate)
	if *workers < 1 {
		*workers = 1
	}

	ctx := context.Background()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	provider, err := resolveBackfillProvider(runtimeCfg, *providerName, os.Stdin, os.Stderr)
	if err != nil {
		log.Fatalf("resolve fundamentals provider: %v", err)
	}

	result, err := usmarket.BackfillUSStockPE(ctx, usmarket.USFundamentalsBackfillConfig{
		Conn:         conn,
		DSN:          *dsn,
		Provider:     provider,
		StartDate:    from,
		EndDate:      to,
		Symbols:      parseSymbols(*symbolsFlag),
		Workers:      *workers,
		BatchSize:    *batchSize,
		PageSize:     *pageSize,
		QPS:          *qps,
		LimitSymbols: *limitSymbols,
		DryRun:       *dryRun,
	})
	if err != nil {
		log.Fatalf("backfill US PE fundamentals: %v", err)
	}

	log.Printf("US PE backfill complete: processed_symbols=%d failed_symbols=%d scanned_bars=%d candidate_rows=%d inserted_rows=%d skipped_rows=%d dry_run=%v",
		result.ProcessedSymbols,
		result.FailedSymbols,
		result.ScannedBars,
		result.CandidateRows,
		result.InsertedRows,
		result.SkippedRows,
		*dryRun,
	)
}

func resolveBackfillProvider(runtimeCfg config.Runtime, providerName string, in io.Reader, out io.Writer) (usmarket.PEBackfillProvider, error) {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "", "disabled", "none":
		return nil, fmt.Errorf("no provider enabled; Tiger is sealed by default because its API is quota-limited and usually requires a higher-volume subscribed account")
	case "tiger":
		if err := confirmTigerProviderUsage(in, out); err != nil {
			return nil, err
		}
		cfg, err := tigerapi.LoadConfigFromRuntime(runtimeCfg)
		if err != nil {
			return nil, fmt.Errorf("load Tiger config: %w", err)
		}
		return usmarket.NewTigerPEBackfillProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q; available values: disabled, tiger", providerName)
	}
}

// confirmTigerProviderUsage forces an explicit terminal acknowledgement before
// Tiger can be used for backfill. Tiger is intentionally not the default bulk
// sync source because quotas and entitlements often block market-wide jobs.
func confirmTigerProviderUsage(in io.Reader, out io.Writer) error {
	if in == nil {
		return fmt.Errorf("interactive confirmation input is unavailable")
	}
	if out == nil {
		out = io.Discard
	}
	if _, err := fmt.Fprintln(out, "WARNING: Tiger API is not suitable as the default bulk sync source."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, "Reason: access depends on subscription quotas/entitlements and typically requires a larger trading-volume account for stable market-wide syncing."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Type %s to continue: ", tigerConfirmationPhrase); err != nil {
		return err
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read Tiger confirmation: %w", err)
		}
		return fmt.Errorf("Tiger provider confirmation aborted")
	}
	if strings.TrimSpace(scanner.Text()) != tigerConfirmationPhrase {
		return fmt.Errorf("Tiger provider not enabled: confirmation phrase mismatch")
	}
	return nil
}

func resolveDateRange(dateValue, startValue, endValue string) (time.Time, time.Time) {
	if strings.TrimSpace(dateValue) != "" {
		parsed := appCli.ParseDate(dateValue, "--date")
		return parsed, parsed
	}
	if strings.TrimSpace(startValue) == "" || strings.TrimSpace(endValue) == "" {
		fmt.Fprintln(os.Stderr, "Usage: us-market-fundamentals-backfill --date <YYYY-MM-DD> [flags]")
		fmt.Fprintln(os.Stderr, "   or: us-market-fundamentals-backfill --start-date <YYYY-MM-DD> --end-date <YYYY-MM-DD> [flags]")
		os.Exit(1)
	}
	from := appCli.ParseDate(startValue, "--start-date")
	to := appCli.ParseDate(endValue, "--end-date")
	if to.Before(from) {
		fmt.Fprintln(os.Stderr, "--end-date must be on or after --start-date")
		os.Exit(1)
	}
	return from, to
}

func parseSymbols(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	symbols := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		symbol := strings.ToUpper(strings.TrimSpace(part))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}
