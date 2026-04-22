package usmarket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	usBackfillRetryDelay     = 5 * time.Second
	usChainBackfillChunkSize = 7 * 24 * time.Hour // weekly chunks for heavy chain queries
)

// DefaultBackfillWindows covers every precomputed US stock/option kline interval.
var DefaultBackfillWindows = []string{"5m", "15m", "30m", "1h", "2h", "4h", "1d"}

// KlineBackfillOptions controls US kline/chain backfill behavior.
type KlineBackfillOptions struct {
	Intervals []string
	From      time.Time // inclusive, UTC; zero means no lower bound
	To        time.Time // exclusive, UTC; zero means no upper bound
	Asset     string    // optional underlying/symbol filter
	Replace   bool
}

// EnsurePrecomputedKlineCoverage bootstraps precomputed US stock/option
// aggregates when the 1m source history predates the current aggregate
// coverage. This handles the case where materialized views are added after
// historical 1m rows already exist in ClickHouse.
func EnsurePrecomputedKlineCoverage(ctx context.Context, conn driver.Conn) error {
	return EnsurePrecomputedKlineCoverageInScope(ctx, conn, KlineBackfillOptions{})
}

func EnsurePrecomputedKlineCoverageInScope(ctx context.Context, conn driver.Conn, opts KlineBackfillOptions) error {
	for _, iv := range KlineIntervals {
		if err := ensureOptionIntervalCoverage(ctx, conn, iv, opts); err != nil {
			return fmt.Errorf("ensure option interval %s coverage: %w", iv.Suffix, err)
		}
		if _, ok := ChainPrecomputedIntervals[iv.Suffix]; ok {
			if err := ensureOptionChainIntervalCoverage(ctx, conn, iv, opts); err != nil {
				return fmt.Errorf("ensure option chain interval %s coverage: %w", iv.Suffix, err)
			}
		}
		if err := ensureStockIntervalCoverage(ctx, conn, iv, opts); err != nil {
			return fmt.Errorf("ensure stock interval %s coverage: %w", iv.Suffix, err)
		}
	}

	return nil
}

func BackfillKlineWindows(ctx context.Context, conn driver.Conn, opts KlineBackfillOptions) error {
	intervals, err := normalizeBackfillIntervals(opts.Intervals)
	if err != nil {
		return err
	}

	if !opts.From.IsZero() && !opts.To.IsZero() && !opts.To.After(opts.From) {
		return fmt.Errorf("invalid time range: to must be after from")
	}

	asset := strings.ToUpper(strings.TrimSpace(opts.Asset))
	intervalToConfig := make(map[string]KlineInterval, len(KlineIntervals))
	for _, iv := range KlineIntervals {
		intervalToConfig[iv.Suffix] = iv
	}

	for _, interval := range intervals {
		iv, ok := intervalToConfig[interval]
		if !ok {
			return fmt.Errorf("interval %q is not precomputed for us market", interval)
		}

		if err := backfillOptionInterval(ctx, conn, iv, opts.From, opts.To, asset, opts.Replace); err != nil {
			return fmt.Errorf("backfill us option interval %s: %w", interval, err)
		}
		if _, ok := ChainPrecomputedIntervals[interval]; ok {
			if err := backfillOptionChainInterval(ctx, conn, iv, opts.From, opts.To, asset, opts.Replace); err != nil {
				return fmt.Errorf("backfill us option chain interval %s: %w", interval, err)
			}
		}
		if err := backfillStockInterval(ctx, conn, iv, opts.From, opts.To, asset, opts.Replace); err != nil {
			return fmt.Errorf("backfill us stock interval %s: %w", interval, err)
		}
	}

	return nil
}

func normalizeBackfillIntervals(input []string) ([]string, error) {
	if len(input) == 0 {
		out := make([]string, len(DefaultBackfillWindows))
		copy(out, DefaultBackfillWindows)
		return out, nil
	}

	allowed := make(map[string]struct{}, len(DefaultBackfillWindows))
	for _, iv := range DefaultBackfillWindows {
		allowed[iv] = struct{}{}
	}

	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, raw := range input {
		iv := strings.ToLower(strings.TrimSpace(raw))
		if iv == "" {
			continue
		}
		if _, ok := allowed[iv]; !ok {
			return nil, fmt.Errorf("unsupported interval %q for us market backfill", iv)
		}
		if _, ok := seen[iv]; ok {
			continue
		}
		seen[iv] = struct{}{}
		out = append(out, iv)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid intervals provided")
	}
	return out, nil
}

func ensureOptionIntervalCoverage(ctx context.Context, conn driver.Conn, iv KlineInterval, opts KlineBackfillOptions) error {
	sourceFrom, sourceTo, hasRows, err := resolveUSSourceBoundsInScope(ctx, conn, "us_options_bar_1m", "underlying", strings.ToUpper(strings.TrimSpace(opts.Asset)), opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("resolve option source bounds: %w", err)
	}
	if !hasRows {
		return nil
	}

	aggTable := "us_options_bar_" + iv.Suffix + "_agg"
	needsBackfill, reason, err := usAggCoverageNeedsBackfill(ctx, conn, aggTable, sourceFrom, sourceTo)
	if err != nil {
		return err
	}
	if !needsBackfill {
		return nil
	}

	log.Printf("[us-kline-bootstrap] backfill options %s: %s", iv.Suffix, reason)
	return backfillOptionInterval(ctx, conn, iv, sourceFrom, sourceTo, strings.ToUpper(strings.TrimSpace(opts.Asset)), false)
}

func ensureOptionChainIntervalCoverage(ctx context.Context, conn driver.Conn, iv KlineInterval, opts KlineBackfillOptions) error {
	sourceFrom, sourceTo, hasRows, err := resolveUSSourceBoundsInScope(ctx, conn, "us_options_bar_1m", "underlying", strings.ToUpper(strings.TrimSpace(opts.Asset)), opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("resolve option chain source bounds: %w", err)
	}
	if !hasRows {
		return nil
	}

	aggTable := "us_options_chain_" + iv.Suffix + "_agg"
	needsBackfill, reason, err := usAggCoverageNeedsBackfill(ctx, conn, aggTable, sourceFrom, sourceTo)
	if err != nil {
		return err
	}
	if !needsBackfill {
		return nil
	}

	log.Printf("[us-kline-bootstrap] backfill option chain %s: %s", iv.Suffix, reason)
	return backfillOptionChainInterval(ctx, conn, iv, sourceFrom, sourceTo, strings.ToUpper(strings.TrimSpace(opts.Asset)), false)
}

func ensureStockIntervalCoverage(ctx context.Context, conn driver.Conn, iv KlineInterval, opts KlineBackfillOptions) error {
	sourceFrom, sourceTo, hasRows, err := resolveUSSourceBoundsInScope(ctx, conn, "us_stocks_bar_1m", "symbol", strings.ToUpper(strings.TrimSpace(opts.Asset)), opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("resolve stock source bounds: %w", err)
	}
	if !hasRows {
		return nil
	}

	aggTable := "us_stocks_bar_" + iv.Suffix + "_agg"
	needsBackfill, reason, err := usAggCoverageNeedsBackfill(ctx, conn, aggTable, sourceFrom, sourceTo)
	if err != nil {
		return err
	}
	if !needsBackfill {
		return nil
	}

	log.Printf("[us-kline-bootstrap] backfill stocks %s: %s", iv.Suffix, reason)
	return backfillStockInterval(ctx, conn, iv, sourceFrom, sourceTo, strings.ToUpper(strings.TrimSpace(opts.Asset)), false)
}

func usAggCoverageNeedsBackfill(ctx context.Context, conn driver.Conn, aggTable string, sourceFrom, sourceTo time.Time) (bool, string, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT count(), ifNull(minOrNull(ts), toDateTime('1970-01-01 00:00:00', 'UTC')), ifNull(maxOrNull(ts), toDateTime('1970-01-01 00:00:00', 'UTC')) FROM %s`, aggTable))
	if err != nil {
		return false, "", fmt.Errorf("query %s coverage bounds: %w", aggTable, err)
	}
	defer rows.Close()

	var (
		count  uint64
		aggMin time.Time
		aggMax time.Time
	)
	if rows.Next() {
		if err := rows.Scan(&count, &aggMin, &aggMax); err != nil {
			return false, "", fmt.Errorf("scan %s coverage bounds: %w", aggTable, err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, "", fmt.Errorf("iterate %s coverage bounds: %w", aggTable, err)
	}

	return needsAggregateCoverageBackfill(sourceFrom, sourceTo, count, aggMin, aggMax)
}

func needsAggregateCoverageBackfill(sourceFrom, sourceTo time.Time, aggCount uint64, aggMin, aggMax time.Time) (bool, string, error) {
	if sourceFrom.IsZero() || sourceTo.IsZero() {
		return false, "", fmt.Errorf("source coverage bounds are required")
	}
	if !sourceTo.After(sourceFrom) {
		return false, "", fmt.Errorf("source coverage upper bound must be after lower bound")
	}
	if aggCount == 0 {
		return true, "aggregate table is empty while source 1m rows exist", nil
	}

	sourceFirstDay := normalizeUTCDay(sourceFrom)
	sourceLastDay := normalizeUTCDay(sourceTo.Add(-24 * time.Hour))
	aggFirstDay := normalizeUTCDay(aggMin)
	aggLastDay := normalizeUTCDay(aggMax)

	if aggFirstDay.After(sourceFirstDay) {
		return true, fmt.Sprintf("aggregate starts at %s but source starts at %s", aggFirstDay.Format("2006-01-02"), sourceFirstDay.Format("2006-01-02")), nil
	}
	if aggLastDay.Before(sourceLastDay) {
		return true, fmt.Sprintf("aggregate ends at %s but source ends at %s", aggLastDay.Format("2006-01-02"), sourceLastDay.Format("2006-01-02")), nil
	}

	return false, "", nil
}

type usBackfillWindow struct {
	From time.Time
	To   time.Time
}

func backfillOptionInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_options_bar_" + iv.Suffix + "_agg"
	windows, err := resolveUSBackfillInsertWindows(ctx, conn, "us_options_bar_1m", aggTable, "underlying", "underlying", asset, from, to, replace)
	if err != nil {
		return fmt.Errorf("resolve option backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip options %s: no source rows or missing aggregate days in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		if replace {
			hasRows, err := usOptionAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
			if err != nil {
				return err
			}
			if hasRows {
				if err := usOptionAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
					return fmt.Errorf("clear existing options %s scope: %w", iv.Suffix, err)
				}
			}
		}

		query := fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    underlying,
    option_type,
    expiration,
    strike,
    argMinState(open, timestamp)               AS open_state,
    maxState(high)                             AS high_state,
    minState(low)                              AS low_state,
    argMaxState(close, timestamp)              AS close_state,
    argMaxState(underlying_close, timestamp)   AS underlying_close_state,
    argMaxState(implied_volatility, timestamp) AS implied_volatility_state,
    argMaxState(delta, timestamp)              AS delta_state,
    argMaxState(gamma, timestamp)              AS gamma_state,
    argMaxState(vega, timestamp)               AS vega_state,
    argMaxState(theta, timestamp)              AS theta_state,
    argMaxState(rho, timestamp)                AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(transactions)                     AS transactions_state
FROM us_options_bar_1m
%s
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, aggTable, klineTimeFunc(iv), usOptionSourceWhere(window.From, window.To, asset))

		if err := retryUSBackfillTimeout(ctx, fmt.Sprintf("insert us options %s rows chunk %d/%d", iv.Suffix, idx+1, len(windows)), func() error {
			return conn.Exec(ctx, query, usOptionSourceArgs(window.From, window.To, asset)...)
		}); err != nil {
			return fmt.Errorf("insert us options %s rows: %w", iv.Suffix, err)
		}
		log.Printf("[us-kline-backfill] options interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[us-kline-backfill] options interval %s completed", iv.Suffix)
	return nil
}

func backfillOptionChainInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_options_chain_" + iv.Suffix + "_agg"
	windows, err := resolveUSChainBackfillInsertWindows(ctx, conn, "us_options_bar_1m", aggTable, "underlying", "underlying", asset, from, to, replace)
	if err != nil {
		return fmt.Errorf("resolve option chain backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip option chain %s: no source rows or missing aggregate days in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		if replace {
			hasRows, err := usOptionChainAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
			if err != nil {
				return err
			}
			if hasRows {
				if err := usOptionChainAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
					return fmt.Errorf("clear existing option chain %s scope: %w", iv.Suffix, err)
				}
			}
		}

		query := fmt.Sprintf(`INSERT INTO %s
SELECT
    ts,
    underlying,
    symbol,
    argMaxState(option_type, last_ts)          AS option_type_state,
    argMaxState(expiration, last_ts)           AS expiration_state,
    argMaxState(strike, last_ts)               AS strike_state,
    argMaxState(close, last_ts)                AS close_state,
    argMaxState(underlying_close, last_ts)     AS underlying_close_state,
    argMaxState(implied_volatility, last_ts)   AS implied_volatility_state,
    argMaxState(delta, last_ts)                AS delta_state,
    argMaxState(gamma, last_ts)                AS gamma_state,
    argMaxState(vega, last_ts)                 AS vega_state,
    argMaxState(theta, last_ts)                AS theta_state,
    argMaxState(rho, last_ts)                  AS rho_state,
    sumState(volume)                           AS volume_state,
    sumState(transactions)                     AS transactions_state
FROM
(
    SELECT
        %s AS ts,
        symbol,
        underlying,
        option_type,
        expiration,
        strike,
        argMax(close, timestamp)              AS close,
        argMax(underlying_close, timestamp)   AS underlying_close,
        argMax(implied_volatility, timestamp) AS implied_volatility,
        argMax(delta, timestamp)              AS delta,
        argMax(gamma, timestamp)              AS gamma,
        argMax(vega, timestamp)               AS vega,
        argMax(theta, timestamp)              AS theta,
        argMax(rho, timestamp)                AS rho,
        sum(volume)                           AS volume,
        sum(transactions)                     AS transactions,
        max(timestamp)                        AS last_ts
    FROM us_options_bar_1m
    %s
    GROUP BY ts, symbol, underlying, option_type, expiration, strike
)
GROUP BY ts, underlying, symbol`, aggTable, klineTimeFunc(iv), usOptionSourceWhere(window.From, window.To, asset))

		if err := retryUSBackfillTimeout(ctx, fmt.Sprintf("insert us option chain %s rows chunk %d/%d", iv.Suffix, idx+1, len(windows)), func() error {
			return conn.Exec(ctx, query, usOptionSourceArgs(window.From, window.To, asset)...)
		}); err != nil {
			return fmt.Errorf("insert us option chain %s rows: %w", iv.Suffix, err)
		}
		log.Printf("[us-kline-backfill] option chain interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[us-kline-backfill] option chain interval %s completed", iv.Suffix)
	return nil
}

func backfillStockInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_stocks_bar_" + iv.Suffix + "_agg"
	windows, err := resolveUSBackfillInsertWindows(ctx, conn, "us_stocks_bar_1m", aggTable, "symbol", "symbol", asset, from, to, replace)
	if err != nil {
		return fmt.Errorf("resolve stock backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip stocks %s: no source rows or missing aggregate days in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		if replace {
			hasRows, err := usStockAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
			if err != nil {
				return err
			}
			if hasRows {
				if err := usStockAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
					return fmt.Errorf("clear existing stocks %s scope: %w", iv.Suffix, err)
				}
			}
		}

		query := fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    argMinState(open, timestamp)              AS open_state,
    maxState(high)                            AS high_state,
    minState(low)                             AS low_state,
    argMaxState(close, timestamp)             AS close_state,
    sumState(volume)                          AS volume_state,
    sumState(transactions)                    AS transactions_state
FROM us_stocks_bar_1m
%s
GROUP BY ts, symbol`, aggTable, klineTimeFunc(iv), usStockSourceWhere(window.From, window.To, asset))

		if err := retryUSBackfillTimeout(ctx, fmt.Sprintf("insert us stocks %s rows chunk %d/%d", iv.Suffix, idx+1, len(windows)), func() error {
			return conn.Exec(ctx, query, usStockSourceArgs(window.From, window.To, asset)...)
		}); err != nil {
			return fmt.Errorf("insert us stocks %s rows: %w", iv.Suffix, err)
		}
		log.Printf("[us-kline-backfill] stocks interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[us-kline-backfill] stocks interval %s completed", iv.Suffix)
	return nil
}

func resolveUSBackfillWindows(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string, from, to time.Time) ([]usBackfillWindow, error) {
	if !from.IsZero() || !to.IsZero() {
		return splitUSBackfillWindows(from, to), nil
	}

	fromBound, toBound, hasRows, err := resolveUSSourceBounds(ctx, conn, tableName, assetColumn, asset)
	if err != nil {
		return nil, err
	}
	if !hasRows {
		return nil, nil
	}
	return splitUSBackfillWindows(fromBound, toBound), nil
}

func resolveUSBackfillInsertWindows(ctx context.Context, conn driver.Conn, sourceTable, aggTable, sourceAssetColumn, aggAssetColumn, asset string, from, to time.Time, replace bool) ([]usBackfillWindow, error) {
	baseWindows, err := resolveUSBackfillWindows(ctx, conn, sourceTable, sourceAssetColumn, asset, from, to)
	if err != nil {
		return nil, err
	}
	if replace || len(baseWindows) == 0 {
		return baseWindows, nil
	}

	insertWindows := make([]usBackfillWindow, 0, len(baseWindows))
	for _, window := range baseWindows {
		missingWindows, err := resolveUSMissingTradingDayWindows(ctx, conn, sourceTable, aggTable, sourceAssetColumn, aggAssetColumn, asset, window.From, window.To)
		if err != nil {
			return nil, err
		}
		insertWindows = append(insertWindows, missingWindows...)
	}
	return insertWindows, nil
}

func resolveUSMissingTradingDayWindows(ctx context.Context, conn driver.Conn, sourceTable, aggTable, sourceAssetColumn, aggAssetColumn, asset string, from, to time.Time) ([]usBackfillWindow, error) {
	sourceDays, err := queryUSSourceTradingDays(ctx, conn, sourceTable, sourceAssetColumn, asset, from, to)
	if err != nil {
		return nil, err
	}
	if len(sourceDays) == 0 {
		return nil, nil
	}

	aggDays, err := queryUSAggregateTradingDays(ctx, conn, aggTable, aggAssetColumn, asset, from, to)
	if err != nil {
		return nil, err
	}
	return missingTradingDayWindows(sourceDays, aggDays), nil
}

func queryUSSourceTradingDays(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string, from, to time.Time) ([]time.Time, error) {
	query := fmt.Sprintf(`SELECT DISTINCT toDateTime(market_date, 'UTC') AS day
FROM %s
WHERE is_regular_session = 1`, tableName)
	args := make([]interface{}, 0, 3)
	if asset != "" {
		query += fmt.Sprintf(" AND %s = {asset:String}", assetColumn)
		args = append(args, clickhouse.Named("asset", asset))
	}
	if !from.IsZero() {
		query += " AND market_date >= {from:Date}"
		args = append(args, clickhouse.Named("from", normalizeUTCDay(from).Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += " AND market_date < {to:Date}"
		args = append(args, clickhouse.Named("to", normalizeUTCDay(to).Format("2006-01-02")))
	}
	query += " ORDER BY day"
	return queryUSTradingDays(ctx, conn, query, args, fmt.Sprintf("query %s source trading days", tableName))
}

func queryUSAggregateTradingDays(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string, from, to time.Time) ([]time.Time, error) {
	query := fmt.Sprintf(`SELECT DISTINCT toDateTime(toDate(ts), 'UTC') AS day
FROM %s
WHERE 1 = 1`, tableName)
	args := make([]interface{}, 0, 3)
	if asset != "" {
		query += fmt.Sprintf(" AND %s = {asset:String}", assetColumn)
		args = append(args, clickhouse.Named("asset", asset))
	}
	if !from.IsZero() {
		query += " AND ts >= toDateTime({from:String}, 'UTC')"
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		query += " AND ts < toDateTime({to:String}, 'UTC')"
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	query += " ORDER BY day"
	return queryUSTradingDays(ctx, conn, query, args, fmt.Sprintf("query %s aggregate trading days", tableName))
}

func queryUSTradingDays(ctx context.Context, conn driver.Conn, query string, args []interface{}, operation string) ([]time.Time, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	defer rows.Close()

	days := make([]time.Time, 0, 32)
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("scan %s: %w", operation, err)
		}
		days = append(days, normalizeUTCDay(day))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", operation, err)
	}
	return normalizeAndSortTradingDays(days), nil
}

func resolveUSSourceBounds(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string) (time.Time, time.Time, bool, error) {
	return resolveUSSourceBoundsInScope(ctx, conn, tableName, assetColumn, asset, time.Time{}, time.Time{})
}

func resolveUSSourceBoundsInScope(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string, from, to time.Time) (time.Time, time.Time, bool, error) {
	query := fmt.Sprintf(`SELECT
    count(),
    ifNull(minOrNull(toDateTime(market_date, 'UTC')), toDateTime('1970-01-01 00:00:00', 'UTC')),
    ifNull(maxOrNull(toDateTime(market_date, 'UTC')), toDateTime('1970-01-01 00:00:00', 'UTC'))
FROM %s
WHERE is_regular_session = 1`, tableName)
	args := make([]interface{}, 0, 3)
	if asset != "" {
		query += fmt.Sprintf(" AND %s = {asset:String}", assetColumn)
		args = append(args, clickhouse.Named("asset", asset))
	}
	if !from.IsZero() {
		query += " AND market_date >= {from:Date}"
		args = append(args, clickhouse.Named("from", normalizeUTCDay(from).Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += " AND market_date < {to:Date}"
		args = append(args, clickhouse.Named("to", normalizeUTCDay(to).Format("2006-01-02")))
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("query %s source bounds: %w", tableName, err)
	}
	defer rows.Close()

	var (
		count   uint64
		minDate time.Time
		maxDate time.Time
	)
	if rows.Next() {
		if err := rows.Scan(&count, &minDate, &maxDate); err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("scan %s source bounds: %w", tableName, err)
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("iterate %s source bounds: %w", tableName, err)
	}
	return scopedSourceBounds(count, minDate, maxDate)
}

func scopedSourceBounds(count uint64, minDate, maxDate time.Time) (time.Time, time.Time, bool, error) {
	if count == 0 || minDate.IsZero() || maxDate.IsZero() {
		return time.Time{}, time.Time{}, false, nil
	}
	fromBound := normalizeUTCDay(minDate)
	toBound := normalizeUTCDay(maxDate).Add(24 * time.Hour)
	if !toBound.After(fromBound) {
		return time.Time{}, time.Time{}, false, fmt.Errorf("source bounds upper bound must be after lower bound")
	}
	return fromBound, toBound, true, nil
}

func splitUSBackfillWindows(from, to time.Time) []usBackfillWindow {
	if from.IsZero() && to.IsZero() {
		return nil
	}
	if from.IsZero() {
		from = normalizeUTCDay(to.Add(-24 * time.Hour))
	}
	if to.IsZero() {
		to = normalizeUTCDay(from).Add(24 * time.Hour)
	}
	if !to.After(from) {
		return nil
	}

	approxMonths := (to.Year()-from.Year())*12 + int(to.Month()-from.Month()) + 2
	if approxMonths < 1 {
		approxMonths = 1
	}
	windows := make([]usBackfillWindow, 0, approxMonths)
	cursor := from.UTC()
	for cursor.Before(to) {
		next := startOfNextMonth(cursor)
		if next.After(to) {
			next = to.UTC()
		}
		windows = append(windows, usBackfillWindow{From: cursor, To: next})
		cursor = next
	}

	return windows
}

func missingTradingDayWindows(sourceDays, aggDays []time.Time) []usBackfillWindow {
	sourceDays = normalizeAndSortTradingDays(sourceDays)
	if len(sourceDays) == 0 {
		return nil
	}

	aggSet := make(map[int64]struct{}, len(aggDays))
	for _, day := range normalizeAndSortTradingDays(aggDays) {
		aggSet[day.Unix()] = struct{}{}
	}

	missingDays := make([]time.Time, 0, len(sourceDays))
	for _, day := range sourceDays {
		if _, ok := aggSet[day.Unix()]; ok {
			continue
		}
		missingDays = append(missingDays, day)
	}
	if len(missingDays) == 0 {
		return nil
	}

	windows := make([]usBackfillWindow, 0, len(missingDays))
	windowStart := missingDays[0]
	windowEnd := windowStart.Add(24 * time.Hour)
	for _, day := range missingDays[1:] {
		if day.Equal(windowEnd) {
			windowEnd = windowEnd.Add(24 * time.Hour)
			continue
		}
		windows = append(windows, usBackfillWindow{From: windowStart, To: windowEnd})
		windowStart = day
		windowEnd = day.Add(24 * time.Hour)
	}
	windows = append(windows, usBackfillWindow{From: windowStart, To: windowEnd})
	return windows
}

func normalizeAndSortTradingDays(days []time.Time) []time.Time {
	if len(days) == 0 {
		return nil
	}

	uniqueDays := make(map[int64]time.Time, len(days))
	for _, day := range days {
		normalized := normalizeUTCDay(day)
		if normalized.IsZero() {
			continue
		}
		uniqueDays[normalized.Unix()] = normalized
	}
	if len(uniqueDays) == 0 {
		return nil
	}

	sorted := make([]time.Time, 0, len(uniqueDays))
	for _, day := range uniqueDays {
		sorted = append(sorted, day)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Before(sorted[j])
	})
	return sorted
}

func startOfNextMonth(ts time.Time) time.Time {
	ts = ts.UTC()
	return time.Date(ts.Year(), ts.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func normalizeUTCDay(ts time.Time) time.Time {
	ts = ts.UTC()
	return time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
}

// chunkUSBackfillWindows sub-divides each window in the input slice into
// chunks of at most step duration. This is used for heavy queries (e.g. option
// chain double-aggregation) to avoid single INSERT…SELECT statements that
// span an entire month and exceed the server read timeout.
func chunkUSBackfillWindows(windows []usBackfillWindow, step time.Duration) []usBackfillWindow {
	if len(windows) == 0 || step <= 0 {
		return windows
	}
	out := make([]usBackfillWindow, 0, len(windows)*2)
	for _, w := range windows {
		cursor := w.From
		for cursor.Before(w.To) {
			next := cursor.Add(step)
			if next.After(w.To) {
				next = w.To
			}
			out = append(out, usBackfillWindow{From: cursor, To: next})
			cursor = next
		}
	}
	return out
}

// resolveUSChainBackfillInsertWindows is like resolveUSBackfillInsertWindows but
// sub-divides each monthly base window into weekly chunks to keep each
// INSERT…SELECT manageable.
func resolveUSChainBackfillInsertWindows(ctx context.Context, conn driver.Conn, sourceTable, aggTable, sourceAssetColumn, aggAssetColumn, asset string, from, to time.Time, replace bool) ([]usBackfillWindow, error) {
	baseWindows, err := resolveUSBackfillWindows(ctx, conn, sourceTable, sourceAssetColumn, asset, from, to)
	if err != nil {
		return nil, err
	}
	if len(baseWindows) == 0 {
		return nil, nil
	}

	// Sub-divide each monthly base window into weekly chunks.
	weeklyWindows := chunkUSBackfillWindows(baseWindows, usChainBackfillChunkSize)

	if replace {
		return weeklyWindows, nil
	}

	insertWindows := make([]usBackfillWindow, 0, len(weeklyWindows))
	for _, window := range weeklyWindows {
		missingWindows, err := resolveUSMissingTradingDayWindows(ctx, conn, sourceTable, aggTable, sourceAssetColumn, aggAssetColumn, asset, window.From, window.To)
		if err != nil {
			return nil, err
		}
		insertWindows = append(insertWindows, missingWindows...)
	}
	return insertWindows, nil
}

func retryUSBackfillTimeout(ctx context.Context, operation string, fn func() error) error {
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isUSRetryableTimeout(err) {
			return err
		}
		log.Printf("[us-kline-backfill] warning: %s timed out on attempt %d, retrying in %s: %v", operation, attempt, usBackfillRetryDelay, err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context canceled while waiting to retry: %w", operation, ctx.Err())
		case <-time.After(usBackfillRetryDelay):
		}
	}
}

func isUSRetryableTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "read timeout")
}

func usOptionSourceWhere(from, to time.Time, asset string) string {
	parts := []string{"is_regular_session = 1"}
	if !from.IsZero() {
		parts = append(parts, "timestamp >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "timestamp < toDateTime({to:String}, 'UTC')")
	}
	if asset != "" {
		parts = append(parts, "underlying = {asset:String}")
	}
	return "WHERE " + strings.Join(parts, " AND ")
}

func usStockSourceWhere(from, to time.Time, asset string) string {
	parts := []string{"is_regular_session = 1"}
	if !from.IsZero() {
		parts = append(parts, "timestamp >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "timestamp < toDateTime({to:String}, 'UTC')")
	}
	if asset != "" {
		parts = append(parts, "symbol = {asset:String}")
	}
	return "WHERE " + strings.Join(parts, " AND ")
}

func usOptionSourceArgs(from, to time.Time, asset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	if asset != "" {
		args = append(args, clickhouse.Named("asset", asset))
	}
	return args
}

func usStockSourceArgs(from, to time.Time, asset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	if asset != "" {
		args = append(args, clickhouse.Named("asset", asset))
	}
	return args
}

func usOptionAggScopeWhere(from, to time.Time, asset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "ts >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "ts < toDateTime({to:String}, 'UTC')")
	}
	if asset != "" {
		parts = append(parts, "underlying = {asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

func usStockAggScopeWhere(from, to time.Time, asset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "ts >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "ts < toDateTime({to:String}, 'UTC')")
	}
	if asset != "" {
		parts = append(parts, "symbol = {asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

func usOptionAggScopeArgs(from, to time.Time, asset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	if asset != "" {
		args = append(args, clickhouse.Named("asset", asset))
	}
	return args
}

func usStockAggScopeArgs(from, to time.Time, asset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
	}
	if asset != "" {
		args = append(args, clickhouse.Named("asset", asset))
	}
	return args
}

func usOptionAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, usOptionAggScopeWhere(from, to, asset))
	rows, err := conn.Query(ctx, query, usOptionAggScopeArgs(from, to, asset)...)
	if err != nil {
		return false, fmt.Errorf("query us option agg row count: %w", err)
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, fmt.Errorf("scan us option agg row count: %w", err)
		}
	}
	return count > 0, nil
}

func usOptionChainAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, usOptionAggScopeWhere(from, to, asset))
	rows, err := conn.Query(ctx, query, usOptionAggScopeArgs(from, to, asset)...)
	if err != nil {
		return false, fmt.Errorf("query us option chain agg row count: %w", err)
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, fmt.Errorf("scan us option chain agg row count: %w", err)
		}
	}
	return count > 0, nil
}

func usStockAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, usStockAggScopeWhere(from, to, asset))
	rows, err := conn.Query(ctx, query, usStockAggScopeArgs(from, to, asset)...)
	if err != nil {
		return false, fmt.Errorf("query us stock agg row count: %w", err)
	}
	defer rows.Close()

	var count uint64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, fmt.Errorf("scan us stock agg row count: %w", err)
		}
	}
	return count > 0, nil
}

func usOptionAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, usOptionAggScopeWhere(from, to, asset))
	if err := conn.Exec(ctx, query, usOptionAggScopeArgs(from, to, asset)...); err != nil {
		return fmt.Errorf("delete us option agg scope: %w", err)
	}
	return nil
}

func usOptionChainAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, usOptionAggScopeWhere(from, to, asset))
	if err := conn.Exec(ctx, query, usOptionAggScopeArgs(from, to, asset)...); err != nil {
		return fmt.Errorf("delete us option chain agg scope: %w", err)
	}
	return nil
}

func usStockAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, asset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, usStockAggScopeWhere(from, to, asset))
	if err := conn.Exec(ctx, query, usStockAggScopeArgs(from, to, asset)...); err != nil {
		return fmt.Errorf("delete us stock agg scope: %w", err)
	}
	return nil
}
