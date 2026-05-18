package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	date := flag.String("date", "", "Single market date to backfill (YYYY-MM-DD)")
	startDate := flag.String("start-date", "", "Start market date to backfill (YYYY-MM-DD)")
	endDate := flag.String("end-date", "", "End market date to backfill (YYYY-MM-DD)")
	symbolsFlag := flag.String("symbols", "", "Optional comma-separated underlying symbols to restrict backfill")
	priorityOrder := flag.String("priority-order", usmarket.PriorityOrderUSDefault, "Underlying execution priority order (none, us-default)")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	workers := flag.Int("workers", 2, "Number of parallel backfill workers")
	batchSize := flag.Int("batch-size", 100000, "Rows per ClickHouse INSERT batch")
	riskFreeRate := flag.Float64("risk-free-rate", 0.05, "Annualized risk-free rate used for option greeks")
	limitTasks := flag.Int("limit-tasks", 0, "Optional limit of underlying/day tasks to process")
	dryRun := flag.Bool("dry-run", false, "Only report matches without writing to ClickHouse")
	rebuildKlines := flag.Bool("rebuild-klines", true, "Rebuild higher-interval option kline aggregates after a successful backfill run")
	flag.Parse()

	from, to := resolveDateRange(*date, *startDate, *endDate)
	if *workers < 1 {
		*workers = 1
	}
	resolvedPriorityOrder, err := usmarket.NormalizeUSPriorityOrder(*priorityOrder)
	if err != nil {
		log.Fatalf("resolve priority order: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	underlyings := parseSymbols(*symbolsFlag)
	result, err := usmarket.BackfillMissingOptionGreeks(ctx, usmarket.OptionGreeksBackfillConfig{
		Conn:              conn,
		DSN:               *dsn,
		StartDate:         from,
		EndDate:           to,
		Underlyings:       underlyings,
		PriorityOrder:     resolvedPriorityOrder,
		Workers:           *workers,
		BatchSize:         *batchSize,
		LimitTasks:        *limitTasks,
		RiskFreeRate:      *riskFreeRate,
		DryRun:            *dryRun,
		RebuildAggregates: *rebuildKlines,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			log.Printf("option greeks backfill interrupted")
			os.Exit(130)
		}
		log.Fatalf("backfill missing option greeks: %v", err)
	}

	log.Printf("Local Greek backfill complete: processed=%d failed=%d matched_rows=%d backfilled_rows=%d matched_contracts=%d unmatched_contracts=%d remaining_tasks=%d dry_run=%v",
		result.ProcessedTasks,
		result.FailedTasks,
		result.MatchedRows,
		result.BackfilledRows,
		result.MatchedContracts,
		result.UnmatchedContracts,
		result.RemainingTasks,
		*dryRun,
	)
	if result.RemainingTasks > 0 {
		log.Printf("Residual missing greek tasks remain after local backfill; these rows still lack usable underlying minute data in ClickHouse")
	}
}

func resolveDateRange(dateValue, startValue, endValue string) (time.Time, time.Time) {
	if strings.TrimSpace(dateValue) != "" {
		parsed := appCli.ParseDate(dateValue, "--date")
		return parsed, parsed
	}
	if strings.TrimSpace(startValue) == "" || strings.TrimSpace(endValue) == "" {
		fmt.Fprintln(os.Stderr, "Usage: us-market-greeks-backfill --date <YYYY-MM-DD> [flags]")
		fmt.Fprintln(os.Stderr, "   or: us-market-greeks-backfill --start-date <YYYY-MM-DD> --end-date <YYYY-MM-DD> [flags]")
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
