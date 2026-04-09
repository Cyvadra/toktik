package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	thetaBaseURL := flag.String("theta-base-url", runtimeCfg.ThetaData.BaseURL, "ThetaData v3 base URL")
	workers := flag.Int("workers", 2, "Number of parallel backfill workers")
	batchSize := flag.Int("batch-size", 100000, "Rows per ClickHouse INSERT batch")
	limitTasks := flag.Int("limit-tasks", 0, "Optional limit of underlying/day tasks to process")
	dryRun := flag.Bool("dry-run", false, "Only report matches without writing to ClickHouse")
	rebuildKlines := flag.Bool("rebuild-klines", true, "Rebuild higher-interval option kline aggregates after a successful backfill run")
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

	underlyings := parseSymbols(*symbolsFlag)
	tasks, err := usmarket.ListMissingOptionGreeksTasks(ctx, conn, from, to, underlyings, *limitTasks)
	if err != nil {
		log.Fatalf("list missing option greek tasks: %v", err)
	}
	if len(tasks) == 0 {
		log.Printf("No missing option Greek tasks found between %s and %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
		return
	}

	log.Printf("Found %d underlying/day tasks to backfill between %s and %s", len(tasks), from.Format("2006-01-02"), to.Format("2006-01-02"))

	httpClient := &http.Client{Timeout: runtimeCfg.ThetaDataTimeout()}
	taskCh := make(chan usmarket.MissingOptionGreeksTask)

	var (
		wg                 sync.WaitGroup
		processedTasks     int64
		failedTasks        int64
		matchedRows        int64
		backfilledRows     int64
		matchedContracts   int64
		unmatchedContracts int64
		fallbackRows       int64
		fallbackContracts  int64
	)

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			workerConn, err := usmarket.ConnectClickHouse(ctx, *dsn)
			if err != nil {
				log.Printf("[ERROR] worker %d connect ClickHouse: %v", workerID, err)
				atomic.AddInt64(&failedTasks, int64(len(tasks)))
				return
			}

			for task := range taskCh {
				stats, err := usmarket.BackfillOptionGreeksWithThetaData(ctx, workerConn, httpClient, *thetaBaseURL, task, *batchSize, *dryRun)
				if err != nil {
					log.Printf("[ERROR] %s %s: %v", task.MarketDate.Format("2006-01-02"), task.Underlying, err)
					atomic.AddInt64(&failedTasks, 1)
					continue
				}
				if stats.NoData {
					atomic.AddInt64(&processedTasks, 1)
					log.Printf("[SKIPPED] %s %s: scanned=%d matched_rows=0 backfilled_rows=0 matched_contracts=0 unmatched_contracts=%d reason=no_theta_data",
						task.MarketDate.Format("2006-01-02"),
						task.Underlying,
						stats.RowsScanned,
						stats.ContractsUnmatched,
					)
					continue
				}

				atomic.AddInt64(&processedTasks, 1)
				atomic.AddInt64(&matchedRows, int64(stats.RowsMatched))
				atomic.AddInt64(&backfilledRows, int64(stats.RowsBackfilled))
				atomic.AddInt64(&matchedContracts, int64(stats.ContractsMatched))
				atomic.AddInt64(&unmatchedContracts, int64(stats.ContractsUnmatched))
				atomic.AddInt64(&fallbackRows, int64(stats.RowsFallback))
				atomic.AddInt64(&fallbackContracts, int64(stats.ContractsFallback))

				mode := "BACKFILLED"
				if *dryRun {
					mode = "DRYRUN"
				}
				log.Printf("[%s] %s %s: scanned=%d matched_rows=%d backfilled_rows=%d matched_contracts=%d unmatched_contracts=%d fallback_rows=%d fallback_contracts=%d",
					mode,
					task.MarketDate.Format("2006-01-02"),
					task.Underlying,
					stats.RowsScanned,
					stats.RowsMatched,
					stats.RowsBackfilled,
					stats.ContractsMatched,
					stats.ContractsUnmatched,
					stats.RowsFallback,
					stats.ContractsFallback,
				)
			}
		}(i + 1)
	}

	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)
	wg.Wait()

	remainingTasks, err := usmarket.ListMissingOptionGreeksTasks(ctx, conn, from, to, underlyings, 0)
	if err != nil {
		log.Fatalf("re-check missing option greek tasks: %v", err)
	}
	remainingCount := len(remainingTasks)
	if !*dryRun && remainingCount == 0 && failedTasks == 0 && *rebuildKlines {
		log.Printf("Rebuilding higher-interval option kline + chain cache aggregates from clean 1m data")
		if err := usmarket.RebuildOptionKlineAggregates(ctx, conn); err != nil {
			log.Fatalf("rebuild option kline aggregates: %v", err)
		}
		if err := usmarket.RebuildOptionChainCaches(ctx, conn); err != nil {
			log.Fatalf("rebuild option chain caches: %v", err)
		}
	}

	log.Printf("ThetaData Greek backfill complete: processed=%d failed=%d matched_rows=%d backfilled_rows=%d matched_contracts=%d unmatched_contracts=%d fallback_rows=%d fallback_contracts=%d remaining_tasks=%d dry_run=%v",
		processedTasks,
		failedTasks,
		matchedRows,
		backfilledRows,
		matchedContracts,
		unmatchedContracts,
		fallbackRows,
		fallbackContracts,
		remainingCount,
		*dryRun,
	)
	if !*dryRun && remainingCount > 0 {
		log.Printf("Residual missing greek tasks remain after backfill; refusing to leave higher-interval aggregates stale or NaN-contaminated")
		os.Exit(1)
	}
	if failedTasks > 0 {
		os.Exit(1)
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
