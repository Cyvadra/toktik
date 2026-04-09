package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/Cyvadra/toktik/internal/backtest"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/datafeed"
	"github.com/Cyvadra/toktik/internal/report"
	_ "github.com/Cyvadra/toktik/pkg/dsl/catalog"
	"github.com/Cyvadra/toktik/pkg/feeds"
	_ "github.com/Cyvadra/toktik/pkg/feeds/dvol"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

const defaultBacktestHTMLDir = "reports/backtests"

type marketSpec struct {
	name           string
	underlyingFeed string
}

type instrumentScope string

type capitalProfile struct {
	mode string
	unit string
	note string
}

const (
	marketCrypto         = "crypto"
	marketUS             = "us"
	cryptoUnderlyingFeed = "crypto-underlying"
	usUnderlyingFeed     = "us-underlying"

	instrumentAuto     instrumentScope = "auto"
	instrumentSpot     instrumentScope = "spot"
	instrumentContract instrumentScope = "contract"
	instrumentMixed    instrumentScope = "mixed"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	market := flag.String("market", marketCrypto, "Backtest market: crypto | us")
	instrument := flag.String("instrument", string(instrumentAuto), "Trading scope: auto | spot | contract | mixed (contract currently means option-contract strategies)")
	baseAsset := flag.String("asset", "BTC", "Underlying symbol or base asset (e.g. BTC, ETH, AAPL)")
	interval := flag.String("interval", "1h", "Bar interval for the strategy (e.g. 1h)")
	fromStr := flag.String("from", "", "Start date YYYY-MM-DD (required)")
	toStr := flag.String("to", "", "End date YYYY-MM-DD (required)")
	capital := flag.Float64("capital", 0, "Initial capital. Spot-only strategies use USD; crypto option-contract strategies use the underlying asset unit; US option-contract strategies use USD. (required)")
	strategyHelp := fmt.Sprintf(
		"Strategy selector: name, alias, group, or comma list. Available names: %s. Group aliases: both=spread, all=all",
		strings.Join(strategies.Available(), " | "),
	)
	stratName := flag.String("strategy", "both", strategyHelp)
	commModel := flag.String("commission-model", "none", "Commission model: none | flat | percent | per-unit")
	commValue := flag.Float64("commission-value", 0, "Commission value")
	slippagePct := flag.Float64("slippage-pct", 0, "Slippage fraction (0 = none)")
	outputJSON := flag.String("output", "", "Optional JSON output file path")
	tradeCSVOutput := flag.String("trade-csv-output", "", "Optional compact CSV trade ledger path (single strategy: exact file; multi-strategy: adjacent numbered files)")
	outputHTML := flag.String("html-output", "", "Optional HTML report output path (single strategy: detail report; multi-strategy: overview page with adjacent detail pages)")
	clearPreviousData := flag.Bool("clear-previous-data", false, "Clear existing CSV and JSON files under reports/backtests before writing new outputs")
	positionSize := flag.Float64("position-size", 0, "Contracts per leg when opening a spread; when unset, the strategy decides")
	maxHoldHours := flag.Float64("max-hold-hours", 0, "Maximum holding time in hours; when unset, the strategy decides")
	targetExpiryDays := flag.Int("target-expiry-days", 0, "Target days to expiry when selecting contracts; when unset, the strategy decides")
	minExpiryDays := flag.Int("min-expiry-days", 0, "Minimum days to expiry when selecting contracts; when unset, the strategy decides")
	minPremium := flag.Float64("min-premium", 0, "Minimum bid premium required for the short leg; when unset, the strategy decides")
	shortDeltaMin := flag.Float64("short-delta-min", 0, "Minimum absolute delta for the short leg; when unset, the strategy decides")
	shortDeltaMax := flag.Float64("short-delta-max", 0, "Maximum absolute delta for the short leg; when unset, the strategy decides")
	longDeltaMin := flag.Float64("long-delta-min", 0, "Minimum absolute delta for the long leg; when unset, the strategy decides")
	longDeltaMax := flag.Float64("long-delta-max", 0, "Maximum absolute delta for the long leg; when unset, the strategy decides")
	spreadEntryPriceMode := flag.String("spread-entry-price-mode", "mark_close", "Spread entry pricing: mark_close or bidask")
	spreadExitPriceMode := flag.String("spread-exit-price-mode", "mark_close", "Spread exit pricing: mark_close or bidask")
	spreadValuationPriceMode := flag.String("spread-valuation-price-mode", "mark_close", "Spread mark-to-market pricing: mark_close or bidask")
	maPeriod := flag.Int("ma-period", 0, "SMA period for MA deviation signal; when unset, the strategy decides")
	pThreshold := flag.Float64("p-threshold", 0, "MA deviation ratio threshold for signal entry; when unset, the strategy decides")
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
	if *capital <= 0 {
		fmt.Fprintln(os.Stderr, "error: --capital must be > 0")
		os.Exit(1)
	}
	if *positionSize < 0 {
		fmt.Fprintln(os.Stderr, "error: --position-size must be >= 0")
		os.Exit(1)
	}
	if *maxHoldHours < 0 {
		fmt.Fprintln(os.Stderr, "error: --max-hold-hours must be >= 0")
		os.Exit(1)
	}
	if *targetExpiryDays < 0 {
		fmt.Fprintln(os.Stderr, "error: --target-expiry-days must be >= 0")
		os.Exit(1)
	}
	if *minExpiryDays < 0 {
		fmt.Fprintln(os.Stderr, "error: --min-expiry-days must be >= 0")
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
	if *targetExpiryDays > 0 && *minExpiryDays > 0 && *targetExpiryDays < *minExpiryDays {
		fmt.Fprintln(os.Stderr, "error: --target-expiry-days must be >= --min-expiry-days")
		os.Exit(1)
	}
	if *shortDeltaMax > 0 && *shortDeltaMin > *shortDeltaMax {
		fmt.Fprintln(os.Stderr, "error: --short-delta-min must be <= --short-delta-max")
		os.Exit(1)
	}
	if *longDeltaMax > 0 && *longDeltaMin > *longDeltaMax {
		fmt.Fprintln(os.Stderr, "error: --long-delta-min must be <= --long-delta-max")
		os.Exit(1)
	}

	entryPriceMode := mustParseOptionPriceMode(*spreadEntryPriceMode, "--spread-entry-price-mode")
	exitPriceMode := mustParseOptionPriceMode(*spreadExitPriceMode, "--spread-exit-price-mode")
	valuationPriceMode := mustParseOptionPriceMode(*spreadValuationPriceMode, "--spread-valuation-price-mode")
	commissionModel, err := parseCommissionModel(*commModel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	tradeDirection, err := parseTradeDirection(*direction)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

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
	strategyCfg.Direction = tradeDirection

	primaryMarket, err := parsePrimaryMarket(*market)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	tradeScope, err := parseInstrumentScope(*instrument)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	underlyingSymbol := strings.ToUpper(strings.TrimSpace(*baseAsset))
	if underlyingSymbol == "" {
		fmt.Fprintln(os.Stderr, "error: --asset must not be empty")
		os.Exit(1)
	}

	resolved, err := strategies.ResolveDetailed(*stratName, strategyCfg, underlyingSymbol)
	if err != nil {
		fmt.Fprintf(os.Stderr, "strategy resolve failed: %v\n", err)
		os.Exit(1)
	}
	if err := validateInstrumentScope(tradeScope, resolved); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *clearPreviousData {
		if err := clearBacktestDataFiles(defaultBacktestHTMLDir); err != nil {
			log.Fatalf("Failed to clear prior backtest data in %s: %v", defaultBacktestHTMLDir, err)
		}
	}

	ctx := context.Background()

	log.Printf("Connecting to ClickHouse: %s", sanitizeDSN(*dsn))
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	factorStore, err := feeds.NewStore(ctx, *dsn)
	if err != nil {
		log.Fatalf("Factor store connection failed: %v", err)
	}
	defer func() {
		if closeErr := factorStore.Close(); closeErr != nil {
			log.Printf("Warning: failed to close factor store: %v", closeErr)
		}
	}()
	interactiveProgress := supportsTerminalProgress(os.Stderr)

	var chainProvider backtest.OptionsChainProvider
	if shouldLoadOptionChain(tradeScope, resolved) {
		chainLabel := fmt.Sprintf("Loading options chain for %s [%s/%s, %s -> %s]",
			underlyingSymbol, primaryMarket.name, *interval, from.Format("2006-01-02"), to.Format("2006-01-02"))
		if interactiveProgress {
			err = runWithTerminalSpinner(os.Stderr, chainLabel, func() error {
				if primaryMarket.name == marketUS {
					chainProvider, err = datafeed.NewUSOptionsChainProvider(ctx, conn, underlyingSymbol, *interval, from, to)
				} else {
					chainProvider, err = datafeed.NewCryptoOptionsChainProvider(ctx, conn, underlyingSymbol, *interval, from, to)
				}
				return err
			})
		} else {
			log.Printf("%s...", chainLabel)
			if primaryMarket.name == marketUS {
				chainProvider, err = datafeed.NewUSOptionsChainProvider(ctx, conn, underlyingSymbol, *interval, from, to)
			} else {
				chainProvider, err = datafeed.NewCryptoOptionsChainProvider(ctx, conn, underlyingSymbol, *interval, from, to)
			}
		}
		if err != nil {
			log.Fatalf("Failed to load options chain: %v", err)
		}
	}

	results := make([]*backtest.Result, 0, len(resolved))
	overviewItems := make([]report.OverviewItem, 0, len(resolved))
	htmlMeta := report.HTMLMeta{
		Asset:       underlyingSymbol,
		Interval:    *interval,
		GeneratedAt: time.Now(),
	}

	for index, item := range resolved {
		capitalProfile := resolveCapitalProfile(primaryMarket, item.Profile, underlyingSymbol)
		cfg := backtest.Config{
			InitialCapital:  *capital,
			AccountUnit:     capitalProfile.unit,
			CommissionModel: commissionModel,
			CommissionValue: *commValue,
			SlippagePct:     *slippagePct,
			ExecutionMode:   backtest.ExecutionPriceCanonical,
			ValuationMode:   backtest.ValuationPriceClose,
			TriggerMode:     backtest.TriggerPriceCanonical,
		}

		engine := newEngine(cfg, conn, factorStore, chainProvider, item.Profile.UsesOptions)
		if interactiveProgress {
			title := item.Strategy.Name()
			if strings.TrimSpace(item.Runtime.ProfileLabel) != "" {
				title += " [" + item.Runtime.ProfileLabel + "]"
			}
			engine.SetProgressFunc(newTerminalProgressRenderer(os.Stderr, title).Report)
		}

		log.Printf("--- Running strategy: %s [%s, %s] ---", item.Strategy.Name(), item.Runtime.ProfileLabel, strings.ToUpper(capitalProfile.unit))
		result, runErr := engine.Run(ctx, primaryMarket.underlyingFeed, underlyingSymbol, *interval, from, to, item.Strategy, nil)
		if runErr != nil {
			log.Fatalf("Backtest failed [%s]: %v", item.Strategy.Name(), runErr)
		}
		result.CapitalMode = strings.ToUpper(capitalProfile.unit)
		result.CapitalProfile = item.Runtime.ProfileLabel
		result.CapitalNote = capitalProfile.note
		results = append(results, result)

		fmt.Println()
		fmt.Println(result.Summary())
		if result.SpreadSummary != nil {
			printSpreadSummary(result.SpreadSummary, result.AccountUnit)
		}

		if *outputJSON != "" {
			outPath := resolveOutputPath(*outputJSON, index, len(resolved))
			if prepErr := ensureParentDir(outPath); prepErr != nil {
				log.Printf("Warning: failed to prepare output directory for %s: %v", outPath, prepErr)
			} else if writeErr := result.ExportJSON(outPath); writeErr != nil {
				log.Printf("Warning: failed to write JSON to %s: %v", outPath, writeErr)
			} else {
				log.Printf("Results written to %s", outPath)
			}
		}

		tradeCSVPath := resolveTradeCSVOutputPath(*tradeCSVOutput, result.StrategyName, underlyingSymbol, *interval, from, to, index, len(resolved))
		if prepErr := ensureParentDir(tradeCSVPath); prepErr != nil {
			log.Printf("Warning: failed to prepare trade CSV directory for %s: %v", tradeCSVPath, prepErr)
		} else if writeErr := result.ExportTradesCSV(tradeCSVPath); writeErr != nil {
			log.Printf("Warning: failed to write trade CSV to %s: %v", tradeCSVPath, writeErr)
		} else {
			log.Printf("Trade CSV written to %s", tradeCSVPath)
		}

		htmlPath := resolveHTMLOutputPath(*outputHTML, result.StrategyName, underlyingSymbol, *interval, from, to, index, len(resolved))
		if writeErr := report.WriteBacktestHTML(htmlPath, result, htmlMeta); writeErr != nil {
			log.Printf("Warning: failed to write HTML report to %s: %v", htmlPath, writeErr)
		} else {
			log.Printf("HTML report written to %s", htmlPath)
			overviewItems = append(overviewItems, report.OverviewItem{Result: result, HTMLPath: htmlPath})
		}
	}

	if len(results) == 1 {
		return
	}

	overviewPath := resolveOverviewHTMLOutputPath(*outputHTML, *stratName, underlyingSymbol, *interval, from, to)
	if writeErr := report.WriteBacktestOverviewHTML(overviewPath, overviewItems, htmlMeta); writeErr != nil {
		log.Printf("Warning: failed to write overview HTML report to %s: %v", overviewPath, writeErr)
	} else {
		log.Printf("Overview HTML report written to %s", overviewPath)
	}
}

func newEngine(cfg backtest.Config, conn driver.Conn, factorStore *feeds.Store, chainProvider backtest.OptionsChainProvider, usesOptions bool) *backtest.Engine {
	engine := backtest.NewEngine(cfg)
	engine.RegisterDataFeed(cryptoUnderlyingFeed, datafeed.NewCryptoUnderlyingDataFeed(conn))
	engine.RegisterDataFeed(usUnderlyingFeed, datafeed.NewUSUnderlyingDataFeed(conn))
	engine.RegisterFactorFeed("dvol", datafeed.NewFeedFactorBridge("dvol", factorStore))
	if usesOptions && chainProvider != nil {
		engine.SetOptionsChainProvider(chainProvider)
	}
	return engine
}

func parsePrimaryMarket(raw string) (marketSpec, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", marketCrypto, cryptoUnderlyingFeed:
		return marketSpec{name: marketCrypto, underlyingFeed: cryptoUnderlyingFeed}, nil
	case marketUS, usUnderlyingFeed:
		return marketSpec{name: marketUS, underlyingFeed: usUnderlyingFeed}, nil
	default:
		return marketSpec{}, fmt.Errorf("--market %q is invalid; want crypto|us", raw)
	}
}

func parseInstrumentScope(raw string) (instrumentScope, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(instrumentAuto):
		return instrumentAuto, nil
	case string(instrumentSpot), "underlying":
		return instrumentSpot, nil
	case string(instrumentContract), "contracts", "option", "options":
		return instrumentContract, nil
	case string(instrumentMixed), "both":
		return instrumentMixed, nil
	default:
		return "", fmt.Errorf("--instrument %q is invalid; want auto|spot|contract|mixed", raw)
	}
}

func validateInstrumentScope(scope instrumentScope, items []strategies.ResolvedStrategy) error {
	switch scope {
	case instrumentSpot:
		if strategiesNeedOptions(items) {
			return fmt.Errorf("--instrument=spot does not support option-contract strategies")
		}
	case instrumentContract:
		for _, item := range items {
			if item.Profile.UsesOptions {
				continue
			}
			name := strings.TrimSpace(item.CanonicalName)
			if name == "" {
				name = "selected strategy"
			}
			return fmt.Errorf("--instrument=contract requires option-contract strategies only; %s uses regular underlying trades", name)
		}
	}
	return nil
}

func shouldLoadOptionChain(scope instrumentScope, items []strategies.ResolvedStrategy) bool {
	if scope == instrumentSpot {
		return false
	}
	return strategiesNeedOptions(items)
}

func resolveCapitalProfile(market marketSpec, profile strategies.StrategyProfile, underlyingSymbol string) capitalProfile {
	underlyingSymbol = strings.ToUpper(strings.TrimSpace(underlyingSymbol))
	if underlyingSymbol == "" {
		underlyingSymbol = "BTC"
	}
	if !profile.UsesOptions {
		return capitalProfile{
			mode: "usd",
			unit: "USD",
			note: "该策略不包含合约逻辑，-capital 按 USD 计价。",
		}
	}
	if market.name == marketUS {
		note := "该策略包含期权合约逻辑；在美股市场，-capital 按 USD 计价。"
		if profile.UsesSignalOnlyRegularTrades() {
			note = "该策略包含期权合约逻辑，且现货腿仅用于信号跟踪；在美股市场，-capital 按 USD 计价。"
		}
		return capitalProfile{
			mode: "usd",
			unit: "USD",
			note: note,
		}
	}
	note := fmt.Sprintf("该策略包含期权合约逻辑；在加密市场，-capital 按 %s 计价。", underlyingSymbol)
	if profile.UsesSignalOnlyRegularTrades() {
		note = fmt.Sprintf("该策略包含期权合约逻辑，且现货腿仅用于信号跟踪；在加密市场，-capital 按 %s 计价。", underlyingSymbol)
	}
	return capitalProfile{
		mode: "base_asset",
		unit: underlyingSymbol,
		note: note,
	}
}

func strategiesNeedOptions(items []strategies.ResolvedStrategy) bool {
	for _, item := range items {
		if item.Profile.UsesOptions {
			return true
		}
	}
	return false
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

func parseCommissionModel(s string) (backtest.CommissionModel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "":
		return backtest.CommissionNone, nil
	case "flat":
		return backtest.CommissionFlat, nil
	case "percent":
		return backtest.CommissionPercent, nil
	case "per-unit", "perunit":
		return backtest.CommissionPerUnit, nil
	default:
		return backtest.CommissionNone, fmt.Errorf("--commission-model %q is invalid; want none|flat|percent|per-unit", s)
	}
}

func parseTradeDirection(raw string) (strategies.TradeDirection, error) {
	direction := strategies.TradeDirection(strings.ToLower(strings.TrimSpace(raw)))
	switch direction {
	case strategies.DirectionBoth, strategies.DirectionLongOnly, strategies.DirectionShortOnly:
		return direction, nil
	default:
		return strategies.DirectionBoth, fmt.Errorf("--direction %q is invalid; want both|long_only|short_only", raw)
	}
}

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

func ensureParentDir(path string) error {
	dir := filepath.Dir(path)
	if strings.TrimSpace(dir) == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func clearBacktestDataFiles(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".csv" && ext != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func resolveHTMLTargetDir(path string) string {
	if strings.TrimSpace(path) == "" {
		return defaultBacktestHTMLDir
	}
	return filepath.Dir(path)
}

func clearHTMLFiles(dir string) error {
	return clearBacktestDataFiles(dir)
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
	return filepath.Join(defaultBacktestHTMLDir, fileName)
}

func resolveTradeCSVOutputPath(base, strategyName, asset, interval string, from, to time.Time, index, total int) string {
	if strings.TrimSpace(base) != "" {
		return resolveOutputPath(base, index, total)
	}
	fileName := fmt.Sprintf(
		"%s_%s_%s_%s_%s_trades.csv",
		slugify(strategyName),
		strings.ToLower(asset),
		slugify(interval),
		from.Format("20060102"),
		to.Format("20060102"),
	)
	return filepath.Join(defaultBacktestHTMLDir, fileName)
}

func resolveOverviewHTMLOutputPath(base, strategyName, asset, interval string, from, to time.Time) string {
	if strings.TrimSpace(base) != "" {
		return base
	}
	name := slugify(strategyName)
	if name == "" {
		name = "overview"
	}
	fileName := fmt.Sprintf(
		"%s_%s_%s_%s_%s.html",
		name,
		strings.ToLower(asset),
		slugify(interval),
		from.Format("20060102"),
		to.Format("20060102"),
	)
	return filepath.Join(defaultBacktestHTMLDir, fileName)
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

func sanitizeDSN(dsn string) string {
	if at := strings.Index(dsn, "@"); at >= 0 {
		if scheme := strings.Index(dsn, "://"); scheme >= 0 {
			return dsn[:scheme+3] + "***@" + dsn[at+1:]
		}
	}
	return dsn
}

type terminalProgressRenderer struct {
	out     *os.File
	title   string
	enabled bool
	lastLen int
}

func newTerminalProgressRenderer(out *os.File, title string) *terminalProgressRenderer {
	return &terminalProgressRenderer{
		out:     out,
		title:   strings.TrimSpace(title),
		enabled: supportsTerminalProgress(out),
	}
}

func (r *terminalProgressRenderer) Report(update backtest.ProgressUpdate) {
	if !r.enabled || update.Total <= 0 {
		return
	}
	current := update.Current
	if current < 0 {
		current = 0
	}
	if current > update.Total {
		current = update.Total
	}
	phase := string(update.Phase)
	if phase == "" {
		phase = "progress"
	}
	percent := 100.0
	if update.Total > 0 {
		percent = float64(current) / float64(update.Total) * 100
	}
	barWidth := 28
	filled := barWidth
	if update.Total > 0 {
		filled = int(math.Round(float64(barWidth) * float64(current) / float64(update.Total)))
		if filled < 0 {
			filled = 0
		}
		if filled > barWidth {
			filled = barWidth
		}
	}
	bar := strings.Repeat("=", filled)
	if filled < barWidth {
		bar += strings.Repeat(" ", barWidth-filled)
	}
	elapsed := time.Duration(0)
	if !update.StartedAt.IsZero() {
		elapsed = time.Since(update.StartedAt).Round(time.Second)
	}
	line := fmt.Sprintf("%s %s [%s] %6.2f%% (%d/%d) %s", phaseLabel(phase), r.title, bar, percent, current, update.Total, elapsed)
	if detail := strings.TrimSpace(update.Message); detail != "" && update.Phase == backtest.ProgressPhasePrepare {
		line += " | " + detail
	}
	padding := ""
	if extra := r.lastLen - len(line); extra > 0 {
		padding = strings.Repeat(" ", extra)
	}
	fmt.Fprintf(r.out, "\r%s%s", line, padding)
	r.lastLen = len(line)
	if update.Completed {
		fmt.Fprintln(r.out)
		r.lastLen = 0
	}
}

func phaseLabel(phase string) string {
	switch phase {
	case string(backtest.ProgressPhasePrepare):
		return "PREP"
	case string(backtest.ProgressPhaseReplay):
		return "RUN "
	default:
		return strings.ToUpper(phase)
	}
}

func supportsTerminalProgress(out *os.File) bool {
	if out == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	info, err := out.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runWithTerminalSpinner(out *os.File, label string, fn func() error) error {
	startedAt := time.Now()
	frames := []string{"|", "/", "-", "\\"}
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		frameIndex := 0
		lastLen := 0
		for {
			line := fmt.Sprintf("CHAIN %s %s %s", frames[frameIndex], label, time.Since(startedAt).Round(time.Second))
			padding := ""
			if extra := lastLen - len(line); extra > 0 {
				padding = strings.Repeat(" ", extra)
			}
			fmt.Fprintf(out, "\r%s%s", line, padding)
			lastLen = len(line)
			frameIndex = (frameIndex + 1) % len(frames)

			select {
			case <-done:
				finished := fmt.Sprintf("CHAIN done %s %s", label, time.Since(startedAt).Round(time.Second))
				padding := ""
				if extra := lastLen - len(finished); extra > 0 {
					padding = strings.Repeat(" ", extra)
				}
				fmt.Fprintf(out, "\r%s%s\n", finished, padding)
				return
			case <-ticker.C:
			}
		}
	}()

	err := fn()
	close(done)
	<-stopped
	return err
}
