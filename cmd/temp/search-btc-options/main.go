package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/pkg/strategies"
)

const (
	defaultTargetDTE  = 33
	defaultBiasDTE    = 10
	defaultLongDelta  = 0.33
	defaultShortDelta = 0.10
)

type symbolMetaRecord struct {
	symbol     string
	optionType string
	strike     float32
	expiration time.Time
}

type chainRow struct {
	timestamp    time.Time
	symbolID     uint32
	delta        float32
	bidClose     float32
	askClose     float32
	markClose    float32
	markIVClose  float32
	tickCount    uint64
	openInterest float32
	meta         symbolMetaRecord
	contract     backtest.OptionContract
}

type expirySummary struct {
	expiry    time.Time
	contracts int
	dte       float64
	minDelta  float64
	maxDelta  float64
	minMark   float64
	maxMark   float64
	avgOI     float64
	avgVolume float64
}

type expiryLifecycle struct {
	expiration time.Time
	firstBar   time.Time
	lastBar    time.Time
	barCount   uint64
	rowCount   uint64
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	asset := flag.String("asset", "BTC", "Base asset")
	interval := flag.String("interval", "1h", "Options bar interval")
	dateStr := flag.String("date", "2023-01-04", "UTC date in YYYY-MM-DD")
	timeStr := flag.String("time", "00:00", "UTC time in HH:MM")
	targetDTE := flag.Int("target-dte", defaultTargetDTE, "Target DTE")
	biasDTE := flag.Int("bias-dte", defaultBiasDTE, "Allowed DTE bias on both sides")
	longDelta := flag.Float64("long-delta", defaultLongDelta, "Target delta for long call")
	shortDelta := flag.Float64("short-delta", defaultShortDelta, "Target delta for short call")
	entryModeFlag := flag.String("entry-price-mode", "mark_close", "Entry pricing mode: mark_close or bidask")
	top := flag.Int("top", 8, "Number of delta candidates to print")
	flag.Parse()

	inspectTime, err := parseInspectTime(*dateStr, *timeStr)
	if err != nil {
		log.Fatalf("parse inspect time: %v", err)
	}
	entryMode, err := strategies.ParseOptionPriceMode(*entryModeFlag)
	if err != nil {
		log.Fatalf("parse entry price mode: %v", err)
	}

	ctx := context.Background()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}

	baseAsset := strings.ToUpper(strings.TrimSpace(*asset))
	log.Printf("inspect base=%s interval=%s timestamp=%s target_dte=%d bias=%d long_delta=%.2f short_delta=%.2f",
		baseAsset, *interval, inspectTime.Format(time.RFC3339), *targetDTE, *biasDTE, *longDelta, *shortDelta)

	metaMap, err := loadSymbolMeta(ctx, conn, baseAsset)
	if err != nil {
		log.Fatalf("load symbol metadata: %v", err)
	}
	rows, err := loadBarRows(ctx, conn, baseAsset, *interval, inspectTime, metaMap)
	if err != nil {
		log.Fatalf("load option rows: %v", err)
	}

	printNearestMetadataExpiries(metaMap, inspectTime, *targetDTE, 12)
	chain := buildOptionsChain(rows, inspectTime)
	debugSelection(chain, inspectTime, entryMode, *targetDTE, *biasDTE, *longDelta, *shortDelta, *top)
	printLifecycleDiagnostics(ctx, conn, baseAsset, *interval, inspectTime, metaMap, *targetDTE)
}

func parseInspectTime(dateStr, timeStr string) (time.Time, error) {
	ts, err := time.ParseInLocation("2006-01-02 15:04", dateStr+" "+timeStr, time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

func loadSymbolMeta(ctx context.Context, conn driver.Conn, baseAsset string) (map[uint32]symbolMetaRecord, error) {
	query := `SELECT
    symbol_id,
    anyLast(symbol)       AS symbol,
    anyLast(option_type)  AS option_type,
    anyLast(strike_price) AS strike_price,
    anyLast(expiration)   AS expiration
FROM crypto_options_symbol_meta
WHERE base_asset = {base_asset:String}
GROUP BY symbol_id`
	params := map[string]string{"base_asset": baseAsset}
	logQuery("load symbol metadata", query, params)

	rows, err := conn.Query(ctx, query, clickhouse.Named("base_asset", baseAsset))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metaMap := make(map[uint32]symbolMetaRecord)
	for rows.Next() {
		var id uint32
		var meta symbolMetaRecord
		if err := rows.Scan(&id, &meta.symbol, &meta.optionType, &meta.strike, &meta.expiration); err != nil {
			return nil, fmt.Errorf("scan symbol meta: %w", err)
		}
		metaMap[id] = meta
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	log.Printf("[result] symbol_meta rows=%d distinct_expiries=%d", len(metaMap), countDistinctMetaExpiries(metaMap))
	return metaMap, nil
}

func loadBarRows(ctx context.Context, conn driver.Conn, baseAsset, interval string, inspectTime time.Time, metaMap map[uint32]symbolMetaRecord) ([]chainRow, error) {
	tableName := resolveOptionTableName(interval)
	fromParam := cryptooptions.ClickHouseTimeParam(inspectTime)
	toParam := cryptooptions.ClickHouseTimeParam(inspectTime.Add(intervalDuration(interval)))

	query := fmt.Sprintf(`SELECT
    b.timestamp,
    b.symbol_id,
    b.delta,
    b.bid_close,
    b.ask_close,
    b.mark_close,
    b.mark_iv_close,
    b.tick_count,
    b.open_interest
FROM %s AS b
WHERE b.base_asset = {base_asset:String}
  AND b.timestamp >= toDateTime({from:String}, 'UTC')
  AND b.timestamp < toDateTime({to:String}, 'UTC')`, tableName)
	params := map[string]string{
		"base_asset": baseAsset,
		"from":       fromParam,
		"to":         toParam,
	}
	logQuery("load option rows at inspected bar", query, params)

	rows, err := conn.Query(ctx, query,
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("from", fromParam),
		clickhouse.Named("to", toParam),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]chainRow, 0, 1024)
	missingMeta := 0
	for rows.Next() {
		var row chainRow
		if err := rows.Scan(
			&row.timestamp,
			&row.symbolID,
			&row.delta,
			&row.bidClose,
			&row.askClose,
			&row.markClose,
			&row.markIVClose,
			&row.tickCount,
			&row.openInterest,
		); err != nil {
			return nil, fmt.Errorf("scan option row: %w", err)
		}
		meta, ok := metaMap[row.symbolID]
		if !ok {
			missingMeta++
			continue
		}
		row.meta = meta
		row.contract = toOptionContract(row)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	log.Printf("[result] option_rows=%d missing_meta=%d distinct_expiries=%d", len(result), missingMeta, countDistinctRowExpiries(result))
	return result, nil
}

func toOptionContract(row chainRow) backtest.OptionContract {
	optionType := backtest.Call
	if row.meta.optionType == "put" {
		optionType = backtest.Put
	}
	return backtest.OptionContract{
		Symbol:       row.meta.symbol,
		Ref:          backtest.SecurityRef{Market: "crypto-options", Symbol: row.meta.symbol},
		Type:         optionType,
		StrikePrice:  float64(row.meta.strike),
		Expiration:   row.meta.expiration,
		Delta:        float64(row.delta),
		BidPrice:     float64(row.bidClose),
		AskPrice:     float64(row.askClose),
		MarkPrice:    float64(row.markClose),
		IV:           float64(row.markIVClose),
		Volume:       float64(row.tickCount),
		OpenInterest: float64(row.openInterest),
	}
}

func buildOptionsChain(rows []chainRow, now time.Time) *backtest.OptionsChain {
	contracts := make([]backtest.OptionContract, 0, len(rows))
	for _, row := range rows {
		contracts = append(contracts, row.contract)
	}
	return backtest.NewOptionsChain(contracts, now)
}

func debugSelection(chain *backtest.OptionsChain, inspectTime time.Time, entryMode backtest.OptionPriceMode, targetDTE, biasDTE int, longDelta, shortDelta float64, top int) {
	log.Printf("[step] raw chain contracts=%d", chain.Len())
	printExpiryBreakdown("raw chain", chain.Contracts(), inspectTime, targetDTE)

	calls := chain.Calls()
	log.Printf("[step] after Calls() contracts=%d", calls.Len())
	printExpiryBreakdown("calls only", calls.Contracts(), inspectTime, targetDTE)

	maxFiltered := calls.ExpiryMax(targetDTE + biasDTE)
	log.Printf("[step] after ExpiryMax(%d) contracts=%d", targetDTE+biasDTE, maxFiltered.Len())
	printExpiryBreakdown("after ExpiryMax", maxFiltered.Contracts(), inspectTime, targetDTE)

	minFiltered := maxFiltered.ExpiryMin(targetDTE - biasDTE)
	log.Printf("[step] after ExpiryMin(%d) contracts=%d", targetDTE-biasDTE, minFiltered.Len())
	printExpiryBreakdown("after ExpiryMin", minFiltered.Contracts(), inspectTime, targetDTE)

	nearest := minFiltered.ExpiryNearest(targetDTE)
	log.Printf("[step] after ExpiryNearest(%d) contracts=%d", targetDTE, nearest.Len())
	printExpiryBreakdown("after ExpiryNearest", nearest.Contracts(), inspectTime, targetDTE)

	longCandidates := nearest.SortByDelta(longDelta)
	shortCandidates := nearest.SortByDelta(shortDelta)
	printDeltaCandidates("long", longDelta, longCandidates, inspectTime, entryMode, backtest.Buy, top)
	printDeltaCandidates("short", shortDelta, shortCandidates, inspectTime, entryMode, backtest.Sell, top)

	longOpt, longPrice, longReason := pickOption(longCandidates, entryMode, backtest.Buy)
	shortOpt, shortPrice, shortReason := pickOption(shortCandidates, entryMode, backtest.Sell)
	if longOpt == nil || shortOpt == nil {
		log.Printf("[selection] no spread selected long_reason=%q short_reason=%q", longReason, shortReason)
		return
	}

	log.Printf("[selection] selected expiry=%s long=%s delta=%.4f price=%.6f short=%s delta=%.4f price=%.6f spread_cost=%.6f",
		longOpt.Expiration.Format(time.RFC3339),
		longOpt.Symbol, longOpt.Delta, longPrice,
		shortOpt.Symbol, shortOpt.Delta, shortPrice,
		longPrice-shortPrice,
	)
}

func pickOption(candidates []backtest.OptionContract, entryMode backtest.OptionPriceMode, side backtest.Side) (*backtest.OptionContract, float64, string) {
	for idx := range candidates {
		price := entryMode.EntryPrice(side, candidates[idx])
		if !math.IsNaN(price) && price > 0 {
			selected := candidates[idx]
			return &selected, price, ""
		}
	}
	return nil, 0, "no valid contract"
}

func printExpiryBreakdown(scope string, contracts []backtest.OptionContract, inspectTime time.Time, targetDTE int) {
	if len(contracts) == 0 {
		log.Printf("[summary] %s: no contracts", scope)
		return
	}
	grouped := make(map[time.Time]*expirySummary)
	for _, contract := range contracts {
		summary := grouped[contract.Expiration]
		if summary == nil {
			summary = &expirySummary{
				expiry:   contract.Expiration,
				minDelta: math.Inf(1),
				maxDelta: math.Inf(-1),
				minMark:  math.Inf(1),
				maxMark:  math.Inf(-1),
			}
			grouped[contract.Expiration] = summary
		}
		summary.contracts++
		summary.dte = contract.DaysToExpiry(inspectTime)
		summary.minDelta = math.Min(summary.minDelta, contract.Delta)
		summary.maxDelta = math.Max(summary.maxDelta, contract.Delta)
		summary.minMark = math.Min(summary.minMark, contract.MarkPrice)
		summary.maxMark = math.Max(summary.maxMark, contract.MarkPrice)
		summary.avgOI += contract.OpenInterest
		summary.avgVolume += contract.Volume
	}
	summaries := make([]expirySummary, 0, len(grouped))
	for _, summary := range grouped {
		summary.avgOI /= float64(summary.contracts)
		summary.avgVolume /= float64(summary.contracts)
		summaries = append(summaries, *summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		di := math.Abs(summaries[i].dte - float64(targetDTE))
		dj := math.Abs(summaries[j].dte - float64(targetDTE))
		if di != dj {
			return di < dj
		}
		return summaries[i].expiry.Before(summaries[j].expiry)
	})

	log.Printf("[summary] %s: %d expiries", scope, len(summaries))
	for _, summary := range summaries {
		log.Printf("[summary] expiry=%s dte=%.2f contracts=%d delta_range=[%.4f, %.4f] mark_range=[%.6f, %.6f] avg_oi=%.2f avg_volume=%.2f",
			summary.expiry.Format(time.RFC3339),
			summary.dte,
			summary.contracts,
			summary.minDelta,
			summary.maxDelta,
			summary.minMark,
			summary.maxMark,
			summary.avgOI,
			summary.avgVolume,
		)
	}
}

func printDeltaCandidates(label string, target float64, contracts []backtest.OptionContract, inspectTime time.Time, entryMode backtest.OptionPriceMode, side backtest.Side, top int) {
	if len(contracts) == 0 {
		log.Printf("[delta] %s target=%.2f no contracts", label, target)
		return
	}
	if top > len(contracts) {
		top = len(contracts)
	}
	for idx := 0; idx < top; idx++ {
		contract := contracts[idx]
		entryPrice := entryMode.EntryPrice(side, contract)
		log.Printf("[delta] %s #%d symbol=%s expiry=%s dte=%.2f delta=%.4f delta_diff=%.4f bid=%.6f ask=%.6f mark=%.6f spread_ratio=%.6f entry_price=%.6f valid_entry=%t oi=%.2f volume=%.2f",
			label,
			idx+1,
			contract.Symbol,
			contract.Expiration.Format(time.RFC3339),
			contract.DaysToExpiry(inspectTime),
			contract.Delta,
			math.Abs(contract.Delta-target),
			contract.BidPrice,
			contract.AskPrice,
			contract.MarkPrice,
			contract.SpreadRatio(),
			entryPrice,
			!math.IsNaN(entryPrice) && entryPrice > 0,
			contract.OpenInterest,
			contract.Volume,
		)
	}
}

func printNearestMetadataExpiries(metaMap map[uint32]symbolMetaRecord, inspectTime time.Time, targetDTE, top int) {
	expiries := distinctMetaExpiries(metaMap)
	sort.Slice(expiries, func(i, j int) bool {
		di := math.Abs(expiries[i].Sub(inspectTime).Hours()/24 - float64(targetDTE))
		dj := math.Abs(expiries[j].Sub(inspectTime).Hours()/24 - float64(targetDTE))
		if di != dj {
			return di < dj
		}
		return expiries[i].Before(expiries[j])
	})
	if top > len(expiries) {
		top = len(expiries)
	}
	log.Printf("[meta] nearest expiries in symbol_meta around target_dte=%d", targetDTE)
	for idx := 0; idx < top; idx++ {
		expiry := expiries[idx]
		log.Printf("[meta] #%d expiry=%s dte=%.2f", idx+1, expiry.Format(time.RFC3339), expiry.Sub(inspectTime).Hours()/24)
	}
}

func printLifecycleDiagnostics(ctx context.Context, conn driver.Conn, baseAsset, interval string, inspectTime time.Time, metaMap map[uint32]symbolMetaRecord, targetDTE int) {
	expiries := distinctMetaExpiries(metaMap)
	sort.Slice(expiries, func(i, j int) bool {
		di := math.Abs(expiries[i].Sub(inspectTime).Hours()/24 - float64(targetDTE))
		dj := math.Abs(expiries[j].Sub(inspectTime).Hours()/24 - float64(targetDTE))
		if di != dj {
			return di < dj
		}
		return expiries[i].Before(expiries[j])
	})
	if len(expiries) > 8 {
		expiries = expiries[:8]
	}

	expiryArraySQL := formatDateTimeArray(expiries)
	query := fmt.Sprintf(`SELECT
    m.expiration,
    min(b.timestamp) AS first_bar,
    max(b.timestamp) AS last_bar,
    countDistinct(b.timestamp) AS bar_count,
    count() AS row_count
FROM %s AS b
INNER JOIN crypto_options_symbol_meta AS m ON b.symbol_id = m.symbol_id
WHERE b.base_asset = {base_asset:String}
  AND m.expiration IN %s
GROUP BY m.expiration
ORDER BY m.expiration`, resolveOptionTableName(interval), expiryArraySQL)
	params := map[string]string{
		"base_asset": baseAsset,
	}
	logQuery("diagnose lifecycle of nearest metadata expiries", query, params)

	rows, err := conn.Query(ctx, query, clickhouse.Named("base_asset", baseAsset))
	if err != nil {
		log.Printf("[diagnose] query failed: %v", err)
		return
	}
	defer rows.Close()

	lifecycles := make(map[time.Time]expiryLifecycle)
	for rows.Next() {
		var item expiryLifecycle
		if err := rows.Scan(&item.expiration, &item.firstBar, &item.lastBar, &item.barCount, &item.rowCount); err != nil {
			log.Printf("[diagnose] scan failed: %v", err)
			return
		}
		lifecycles[item.expiration] = item
	}
	if err := rows.Err(); err != nil {
		log.Printf("[diagnose] rows failed: %v", err)
		return
	}

	log.Printf("[diagnose] nearest metadata expiries and first appearance in %s", interval)
	for _, expiry := range expiries {
		item, ok := lifecycles[expiry]
		if !ok {
			log.Printf("[diagnose] expiry=%s dte=%.2f has metadata but no bars in %s", expiry.Format(time.RFC3339), expiry.Sub(inspectTime).Hours()/24, interval)
			continue
		}
		log.Printf("[diagnose] expiry=%s dte=%.2f first_bar=%s last_bar=%s bar_count=%d row_count=%d live_at_inspect=%t",
			expiry.Format(time.RFC3339),
			expiry.Sub(inspectTime).Hours()/24,
			item.firstBar.Format(time.RFC3339),
			item.lastBar.Format(time.RFC3339),
			item.barCount,
			item.rowCount,
			!inspectTime.Before(item.firstBar),
		)
	}
}

func distinctMetaExpiries(metaMap map[uint32]symbolMetaRecord) []time.Time {
	seen := make(map[time.Time]struct{})
	expiries := make([]time.Time, 0, len(metaMap))
	for _, meta := range metaMap {
		if _, ok := seen[meta.expiration]; ok {
			continue
		}
		seen[meta.expiration] = struct{}{}
		expiries = append(expiries, meta.expiration)
	}
	return expiries
}

func countDistinctMetaExpiries(metaMap map[uint32]symbolMetaRecord) int {
	return len(distinctMetaExpiries(metaMap))
}

func countDistinctRowExpiries(rows []chainRow) int {
	seen := make(map[time.Time]struct{})
	for _, row := range rows {
		seen[row.meta.expiration] = struct{}{}
	}
	return len(seen)
}

func resolveOptionTableName(interval string) string {
	if interval == "1m" {
		return "crypto_options_bar_1m"
	}
	if name, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
		return name
	}
	return "crypto_options_bar_1m"
}

func intervalDuration(interval string) time.Duration {
	switch interval {
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "3h":
		return 3 * time.Hour
	case "4h":
		return 4 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "8h":
		return 8 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "1d":
		return 24 * time.Hour
	default:
		log.Fatalf("unsupported interval: %s", interval)
		return 0
	}
}

func logQuery(title, query string, params map[string]string) {
	log.Printf("[query] %s", title)
	log.Printf("[query] sql:\n%s", renderNamedQuery(query, params))
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		log.Printf("[query] param %s=%s", key, params[key])
	}
}

func renderNamedQuery(query string, params map[string]string) string {
	rendered := query
	for key, value := range params {
		rendered = strings.ReplaceAll(rendered, "{"+key+":String}", quoteString(value))
		rendered = strings.ReplaceAll(rendered, "{"+key+":Array(DateTime)}", value)
	}
	return rendered
}

func quoteString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "\\'") + "'"
}

func formatDateTimeArray(values []time.Time) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("toDateTime('%s', 'UTC')", value.UTC().Format("2006-01-02 15:04:05")))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
