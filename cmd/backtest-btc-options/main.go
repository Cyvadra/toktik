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
	capital := flag.Float64("capital", 1.0, "Initial capital in base asset units (e.g. BTC)")
	strategyHelp := fmt.Sprintf(
		"Strategy selector: name, alias, group, or comma list. Available names: %s. Group aliases: both=spread, all=all",
		strings.Join(strategies.Available(), " | "),
	)
	stratName := flag.String("strategy", "both",
		strategyHelp)
	commModel := flag.String("commission-model", "none",
		"Commission model: none | flat | percent | per-unit")
	commValue := flag.Float64("commission-value", 0, "Commission value")
	slippagePct := flag.Float64("slippage-pct", 0.0, "Slippage fraction (0 = none)")
	outputJSON := flag.String("output", "", "Optional JSON output file path")
	outputHTML := flag.String("html-output", "", "Optional HTML report output path (defaults to reports/backtests/<strategy>_<period>.html; multi-strategy runs emit one combined file)")
	positionSize := flag.Float64("position-size", 1, "Contracts per leg when opening a spread (1 = 1 coin contract per leg, e.g. 1 BTC + 1 BTC)")
	maxHoldHours := flag.Float64("max-hold-hours", 48, "Maximum holding time in hours")
	targetExpiryDays := flag.Int("target-expiry-days", 15, "Target days to expiry when selecting contracts")
	minExpiryDays := flag.Int("min-expiry-days", 7, "Minimum days to expiry when selecting contracts")
	minPremium := flag.Float64("min-premium", 0.025, "Minimum bid premium required for the short leg")
	shortDeltaMin := flag.Float64("short-delta-min", 0.4, "Minimum absolute delta for the short leg")
	shortDeltaMax := flag.Float64("short-delta-max", 0.5, "Maximum absolute delta for the short leg")
	longDeltaMin := flag.Float64("long-delta-min", 0.1, "Minimum absolute delta for the long leg")
	longDeltaMax := flag.Float64("long-delta-max", 0.15, "Maximum absolute delta for the long leg")
	spreadEntryPriceMode := flag.String("spread-entry-price-mode", "mark_close", "Spread entry pricing: mark_close or bidask")
	spreadExitPriceMode := flag.String("spread-exit-price-mode", "mark_close", "Spread exit pricing: mark_close or bidask")
	spreadValuationPriceMode := flag.String("spread-valuation-price-mode", "mark_close", "Spread mark-to-market pricing: mark_close or bidask")
	maPeriod := flag.Int("ma-period", 120, "SMA period for MA deviation signal")
	pThreshold := flag.Float64("p-threshold", 0.15, "MA deviation ratio threshold for signal entry")
	direction := flag.String("direction", "both", "Trade direction: both | long_only | short_only")
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
	if *positionSize <= 0 {
		fmt.Fprintln(os.Stderr, "error: --position-size must be > 0")
		os.Exit(1)
	}
	if *maxHoldHours <= 0 {
		fmt.Fprintln(os.Stderr, "error: --max-hold-hours must be > 0")
		os.Exit(1)
	}
	if *targetExpiryDays <= 0 {
		fmt.Fprintln(os.Stderr, "error: --target-expiry-days must be >= 1")
		os.Exit(1)
	}
	if *minExpiryDays <= 0 {
		fmt.Fprintln(os.Stderr, "error: --min-expiry-days must be >= 1")
		os.Exit(1)
	}
	if *targetExpiryDays < *minExpiryDays {
		fmt.Fprintln(os.Stderr, "error: --target-expiry-days must be >= --min-expiry-days")
		os.Exit(1)
	}
	if *minPremium < 0 {
		fmt.Fprintln(os.Stderr, "error: --min-premium must be >= 0")
		os.Exit(1)
	}
	if *shortDeltaMin < 0 || *shortDeltaMax < 0 || *longDeltaMin < 0 || *longDeltaMax < 0 {
		fmt.Fprintln(os.Stderr, "error: delta bounds must be >= 0")
		os.Exit(1)
	}
	if *shortDeltaMin > *shortDeltaMax {
		fmt.Fprintln(os.Stderr, "error: --short-delta-min must be <= --short-delta-max")
		os.Exit(1)
	}
	if *longDeltaMin > *longDeltaMax {
		fmt.Fprintln(os.Stderr, "error: --long-delta-min must be <= --long-delta-max")
		os.Exit(1)
	}
	entryPriceMode := mustParseOptionPriceMode(*spreadEntryPriceMode, "--spread-entry-price-mode")
	exitPriceMode := mustParseOptionPriceMode(*spreadExitPriceMode, "--spread-exit-price-mode")
	valuationPriceMode := mustParseOptionPriceMode(*spreadValuationPriceMode, "--spread-valuation-price-mode")

	strategyCfg := strategies.DefaultConfig()
	strategyCfg.PositionSize = *positionSize
	strategyCfg.MaxHoldTime = time.Duration(*maxHoldHours * float64(time.Hour))
	strategyCfg.TargetExpiryDays = *targetExpiryDays
	strategyCfg.MinExpiryDays = *minExpiryDays
	strategyCfg.MinPremium = *minPremium
	strategyCfg.ShortDeltaMin = *shortDeltaMin
	strategyCfg.ShortDeltaMax = *shortDeltaMax
	strategyCfg.LongDeltaMin = *longDeltaMin
	strategyCfg.LongDeltaMax = *longDeltaMax
	strategyCfg.EntryPriceMode = entryPriceMode
	strategyCfg.ExitPriceMode = exitPriceMode
	strategyCfg.ValuationPriceMode = valuationPriceMode
	strategyCfg.MAPeriod = *maPeriod
	strategyCfg.PThreshold = *pThreshold

	switch strategies.TradeDirection(strings.ToLower(*direction)) {
	case strategies.DirectionBoth, strategies.DirectionLongOnly, strategies.DirectionShortOnly:
		strategyCfg.Direction = strategies.TradeDirection(*direction)
	default:
		fmt.Fprintf(os.Stderr, "error: --direction %q is invalid; want both|long_only|short_only\n", *direction)
		os.Exit(1)
	}

	strats, err := strategies.Resolve(*stratName, strategyCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strategy resolve failed: %v\n", err)
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
		AccountUnit:     strings.ToUpper(strings.TrimSpace(*baseAsset)),
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

	results := make([]*backtest.Result, 0, len(strats))

	for i, strat := range strats {
		log.Printf("--- Running strategy: %s ---", strat.Name())
		result, runErr := runOne(ctx, conn, cfg, *baseAsset, *interval, from, to, strat, chainProvider)
		if runErr != nil {
			log.Fatalf("Backtest failed [%s]: %v", strat.Name(), runErr)
		}
		results = append(results, result)

		fmt.Println()
		fmt.Println(result.Summary())
		if result.SpreadSummary != nil {
			printSpreadSummary(result.SpreadSummary, result.AccountUnit)
		}

		if *outputJSON != "" {
			outPath := resolveOutputPath(*outputJSON, i, len(strats))
			if writeErr := result.ExportJSON(outPath); writeErr != nil {
				log.Printf("Warning: failed to write JSON to %s: %v", outPath, writeErr)
			} else {
				log.Printf("Results written to %s", outPath)
			}
		}
	}

	htmlMeta := report.HTMLMeta{
		Asset:       *baseAsset,
		Interval:    *interval,
		GeneratedAt: time.Now(),
	}
	if len(results) == 1 {
		htmlPath := resolveHTMLOutputPath(*outputHTML, results[0].StrategyName, *baseAsset, *interval, from, to, 0, 1)
		if writeErr := report.WriteBacktestHTML(htmlPath, results[0], htmlMeta); writeErr != nil {
			log.Printf("Warning: failed to write HTML report to %s: %v", htmlPath, writeErr)
		} else {
			log.Printf("HTML report written to %s", htmlPath)
		}
		return
	}

	htmlPath := resolveCombinedHTMLOutputPath(*outputHTML, *stratName, *baseAsset, *interval, from, to)
	if writeErr := report.WriteCombinedBacktestHTML(htmlPath, results, htmlMeta); writeErr != nil {
		log.Printf("Warning: failed to write combined HTML report to %s: %v", htmlPath, writeErr)
	} else {
		log.Printf("Combined HTML report written to %s", htmlPath)
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

func mustParseOptionPriceMode(value, flagName string) backtest.OptionPriceMode {
	mode, err := strategies.ParseOptionPriceMode(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unsupported %s: %v\n", flagName, err)
		os.Exit(1)
		return backtest.OptionPriceModeUnspecified
	}
	return mode
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
func printSpreadSummary(s *backtest.SpreadSummary, unit string) {
	fmt.Printf("Spread Summary:\n")
	fmt.Printf("  Total spreads:    %d\n", s.TotalSpreads)
	fmt.Printf("  Closed:           %d\n", s.ClosedSpreads)
	fmt.Printf("  Open at end:      %d\n", s.OpenSpreads)
	fmt.Printf("  Winning:          %d\n", s.WinningSpreads)
	fmt.Printf("  Losing:           %d\n", s.LosingSpreads)
	fmt.Printf("  Win rate:         %.1f%%\n", s.WinRate*100)
	if strings.TrimSpace(unit) == "" {
		fmt.Printf("  Total spread PnL: %.4f\n", s.TotalPnL)
		return
	}
	fmt.Printf("  Total spread PnL: %.4f %s\n", s.TotalPnL, unit)
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

func resolveCombinedHTMLOutputPath(base, strategyName, asset, interval string, from, to time.Time) string {
	if strings.TrimSpace(base) != "" {
		return base
	}
	name := slugify(strategyName)
	if name == "" {
		name = "combined"
	}
	fileName := fmt.Sprintf(
		"%s_%s_%s_%s_%s.html",
		name,
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
