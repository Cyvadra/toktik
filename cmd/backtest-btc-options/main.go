package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/report"
	"github.com/Cyvadra/toktik/internal/strategies"
)

func main() {
	dsn := flag.String("clickhouse-dsn", "clickhouse://localhost:9000/default", "ClickHouse DSN")
	baseAsset := flag.String("asset", "BTC", "Underlying base asset (e.g. BTC)")
	interval := flag.String("interval", "1h", "Bar interval for the strategy (e.g. 1h)")
	fromStr := flag.String("from", "", "Start date YYYY-MM-DD (required)")
	toStr := flag.String("to", "", "End date YYYY-MM-DD (required)")
	capital := flag.Float64("capital", 200000, "Initial capital (USD)")
	stratName := flag.String("strategy", "both",
		"Strategy: bull-put-spread | bear-call-spread | both")
	commModel := flag.String("commission-model", "percent",
		"Commission model: none | flat | percent | per-unit")
	commValue := flag.Float64("commission-value", 0.0003, "Commission value")
	slippagePct := flag.Float64("slippage-pct", 0.0, "Slippage fraction (0 = none)")
	outputJSON := flag.String("output", "", "Optional JSON output file path")
	outputHTML := flag.String("html-output", "", "Optional HTML report output path (defaults to reports/backtests/<strategy>_<period>.html)")
	flag.Parse()

	if *fromStr == "" || *toStr == "" {
		fmt.Fprintln(os.Stderr, "error: --from and --to are required (YYYY-MM-DD)")
		flag.Usage()
		os.Exit(1)
	}
	from, err := time.Parse("2006-01-02", *fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --from %q: %v\n", *fromStr, err)
		os.Exit(1)
	}
	to, err := time.Parse("2006-01-02", *toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --to %q: %v\n", *toStr, err)
		os.Exit(1)
	}
	if !from.Before(to) {
		fmt.Fprintln(os.Stderr, "error: --from must be before --to")
		os.Exit(1)
	}

	strats := resolveStrategies(*stratName)
	if len(strats) == 0 {
		fmt.Fprintf(os.Stderr, "unknown strategy %q; supported: bull-put-spread, bear-call-spread, both\n", *stratName)
		os.Exit(1)
	}

	ctx := context.Background()

	log.Printf("Connecting to ClickHouse: %s", sanitizeDSN(*dsn))
	conn, err := cryptooptions.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("ClickHouse connection failed: %v", err)
	}

	cfg := backtest.Config{
		InitialCapital:  *capital,
		CommissionModel: parseCommissionModel(*commModel),
		CommissionValue: *commValue,
		SlippagePct:     *slippagePct,
		ExecutionMode:   backtest.ExecutionPriceCanonical,
		ValuationMode:   backtest.ValuationPriceClose,
		TriggerMode:     backtest.TriggerPriceCanonical,
	}

	log.Printf("Loading options chain for %s (%s) [%s → %s]...",
		*baseAsset, *interval, from.Format("2006-01-02"), to.Format("2006-01-02"))
	chainProvider, err := datafeed.NewCryptoOptionsChainProvider(ctx, conn, *baseAsset, *interval, from, to)
	if err != nil {
		log.Fatalf("Failed to load options chain: %v", err)
	}

	overviewItems := make([]report.OverviewItem, 0, len(strats))

	for i, strat := range strats {
		log.Printf("--- Running strategy: %s ---", strat.Name())
		result, runErr := runOne(ctx, conn, cfg, *baseAsset, *interval, from, to, strat, chainProvider)
		if runErr != nil {
			log.Fatalf("Backtest failed [%s]: %v", strat.Name(), runErr)
		}

		fmt.Println()
		fmt.Println(result.Summary())
		if result.SpreadSummary != nil {
			printSpreadSummary(result.SpreadSummary)
		}

		if *outputJSON != "" {
			outPath := resolveOutputPath(*outputJSON, i, len(strats))
			if writeErr := result.ExportJSON(outPath); writeErr != nil {
				log.Printf("Warning: failed to write JSON to %s: %v", outPath, writeErr)
			} else {
				log.Printf("Results written to %s", outPath)
			}
		}

		htmlPath := resolveHTMLOutputPath(*outputHTML, strat.Name(), *baseAsset, *interval, from, to, i, len(strats))
		if writeErr := report.WriteBacktestHTML(htmlPath, result, report.HTMLMeta{
			Asset:       *baseAsset,
			Interval:    *interval,
			GeneratedAt: time.Now(),
		}); writeErr != nil {
			log.Printf("Warning: failed to write HTML report to %s: %v", htmlPath, writeErr)
		} else {
			log.Printf("HTML report written to %s", htmlPath)
			overviewItems = append(overviewItems, report.OverviewItem{Result: result, HTMLPath: htmlPath})
		}
	}

	if len(overviewItems) > 1 {
		overviewPath := resolveHTMLOverviewPath(*outputHTML, *baseAsset, *interval, from, to)
		if writeErr := report.WriteBacktestOverviewHTML(overviewPath, overviewItems, report.HTMLMeta{
			Asset:       *baseAsset,
			Interval:    *interval,
			GeneratedAt: time.Now(),
		}); writeErr != nil {
			log.Printf("Warning: failed to write HTML overview to %s: %v", overviewPath, writeErr)
		} else {
			log.Printf("HTML overview written to %s", overviewPath)
		}
	}
}

// runOne wires the engine for a single strategy run and executes it.
func runOne(
	ctx context.Context,
	conn driver.Conn,
	cfg backtest.Config,
	baseAsset, interval string,
	from, to time.Time,
	strat backtest.Strategy,
	chainProvider backtest.OptionsChainProvider,
) (*backtest.Result, error) {
	engine := backtest.NewEngine(cfg)
	engine.RegisterDataFeed("crypto-underlying", datafeed.NewCryptoUnderlyingDataFeed(conn))
	engine.SetOptionsChainProvider(chainProvider)
	return engine.Run(ctx, "crypto-underlying", baseAsset, interval, from, to, strat, nil)
}

// resolveStrategies maps a strategy name to the concrete strategy instances.
func resolveStrategies(name string) []backtest.Strategy {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bull-put-spread", "ma-deviation-bull", "bull":
		return []backtest.Strategy{strategies.NewBullPutSpreadStrategy()}
	case "bear-call-spread", "ma-deviation-bear", "bear":
		return []backtest.Strategy{strategies.NewBearCallSpreadStrategy()}
	case "both", "all":
		return []backtest.Strategy{
			strategies.NewBullPutSpreadStrategy(),
			strategies.NewBearCallSpreadStrategy(),
		}
	default:
		return nil
	}
}

// parseCommissionModel converts a string flag to the CommissionModel enum.
func parseCommissionModel(s string) backtest.CommissionModel {
	switch strings.ToLower(s) {
	case "flat":
		return backtest.CommissionFlat
	case "percent":
		return backtest.CommissionPercent
	case "per-unit", "perunit":
		return backtest.CommissionPerUnit
	default:
		return backtest.CommissionNone
	}
}

// printSpreadSummary prints a human-readable options spread summary.
func printSpreadSummary(s *backtest.SpreadSummary) {
	fmt.Printf("Spread Summary:\n")
	fmt.Printf("  Total spreads:    %d\n", s.TotalSpreads)
	fmt.Printf("  Closed:           %d\n", s.ClosedSpreads)
	fmt.Printf("  Open at end:      %d\n", s.OpenSpreads)
	fmt.Printf("  Winning:          %d\n", s.WinningSpreads)
	fmt.Printf("  Losing:           %d\n", s.LosingSpreads)
	fmt.Printf("  Win rate:         %.1f%%\n", s.WinRate*100)
	fmt.Printf("  Total spread PnL: %.4f\n", s.TotalPnL)
}

// resolveOutputPath appends an index suffix when multiple strategies share one output flag.
func resolveOutputPath(base string, index, total int) string {
	if total == 1 {
		return base
	}
	dot := strings.LastIndex(base, ".")
	if dot < 0 {
		return fmt.Sprintf("%s_%d", base, index+1)
	}
	return fmt.Sprintf("%s_%d%s", base[:dot], index+1, base[dot:])
}

func resolveHTMLOutputPath(base, strategyName, asset, interval string, from, to time.Time, index, total int) string {
	if strings.TrimSpace(base) != "" {
		return resolveOutputPath(base, index, total)
	}
	fileName := fmt.Sprintf(
		"%s_%s_%s_%s_%s.html",
		slugify(strategyName),
		strings.ToLower(asset),
		slugify(interval),
		from.Format("20060102"),
		to.Format("20060102"),
	)
	return filepath.Join("reports", "backtests", fileName)
}

func resolveHTMLOverviewPath(base, asset, interval string, from, to time.Time) string {
	if strings.TrimSpace(base) != "" {
		dot := strings.LastIndex(base, ".")
		if dot < 0 {
			return base + "_overview"
		}
		return fmt.Sprintf("%s_overview%s", base[:dot], base[dot:])
	}
	fileName := fmt.Sprintf(
		"overview_%s_%s_%s_%s.html",
		strings.ToLower(asset),
		slugify(interval),
		from.Format("20060102"),
		to.Format("20060102"),
	)
	return filepath.Join("reports", "backtests", fileName)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-", ":", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

// sanitizeDSN redacts any password from the DSN before logging.
func sanitizeDSN(dsn string) string {
	if at := strings.Index(dsn, "@"); at >= 0 {
		if scheme := strings.Index(dsn, "://"); scheme >= 0 {
			return dsn[:scheme+3] + "***@" + dsn[at+1:]
		}
	}
	return dsn
}
