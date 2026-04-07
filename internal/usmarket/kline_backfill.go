package usmarket

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
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

type usBackfillWindow struct {
	From time.Time
	To   time.Time
}

func backfillOptionInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_options_bar_" + iv.Suffix + "_agg"
	windows, err := resolveUSBackfillWindows(ctx, conn, "us_options_bar_1m", "underlying", asset, from, to)
	if err != nil {
		return fmt.Errorf("resolve option backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip options %s: no source rows in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		hasRows, err := usOptionAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
		if err != nil {
			return err
		}
		if hasRows && !replace {
			log.Printf("[us-kline-backfill] skip options %s chunk %d/%d: target rows already exist in %s to %s", iv.Suffix, idx+1, len(windows), window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
			continue
		}
		if hasRows && replace {
			if err := usOptionAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
				return fmt.Errorf("clear existing options %s scope: %w", iv.Suffix, err)
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

		if err := conn.Exec(ctx, query, usOptionSourceArgs(window.From, window.To, asset)...); err != nil {
			return fmt.Errorf("insert us options %s rows: %w", iv.Suffix, err)
		}
		log.Printf("[us-kline-backfill] options interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[us-kline-backfill] options interval %s completed", iv.Suffix)
	return nil
}

func backfillOptionChainInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_options_chain_" + iv.Suffix + "_agg"
	windows, err := resolveUSBackfillWindows(ctx, conn, "us_options_bar_1m", "underlying", asset, from, to)
	if err != nil {
		return fmt.Errorf("resolve option chain backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip option chain %s: no source rows in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		hasRows, err := usOptionChainAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
		if err != nil {
			return err
		}
		if hasRows && !replace {
			log.Printf("[us-kline-backfill] skip option chain %s chunk %d/%d: target rows already exist in %s to %s", iv.Suffix, idx+1, len(windows), window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
			continue
		}
		if hasRows && replace {
			if err := usOptionChainAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
				return fmt.Errorf("clear existing option chain %s scope: %w", iv.Suffix, err)
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

		if err := conn.Exec(ctx, query, usOptionSourceArgs(window.From, window.To, asset)...); err != nil {
			return fmt.Errorf("insert us option chain %s rows: %w", iv.Suffix, err)
		}
		log.Printf("[us-kline-backfill] option chain interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[us-kline-backfill] option chain interval %s completed", iv.Suffix)
	return nil
}

func backfillStockInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, asset string, replace bool) error {
	aggTable := "us_stocks_bar_" + iv.Suffix + "_agg"
	windows, err := resolveUSBackfillWindows(ctx, conn, "us_stocks_bar_1m", "symbol", asset, from, to)
	if err != nil {
		return fmt.Errorf("resolve stock backfill windows: %w", err)
	}
	if len(windows) == 0 {
		log.Printf("[us-kline-backfill] skip stocks %s: no source rows in selected scope", iv.Suffix)
		return nil
	}

	for idx, window := range windows {
		hasRows, err := usStockAggHasRows(ctx, conn, aggTable, window.From, window.To, asset)
		if err != nil {
			return err
		}
		if hasRows && !replace {
			log.Printf("[us-kline-backfill] skip stocks %s chunk %d/%d: target rows already exist in %s to %s", iv.Suffix, idx+1, len(windows), window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
			continue
		}
		if hasRows && replace {
			if err := usStockAggDeleteScope(ctx, conn, aggTable, window.From, window.To, asset); err != nil {
				return fmt.Errorf("clear existing stocks %s scope: %w", iv.Suffix, err)
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

		if err := conn.Exec(ctx, query, usStockSourceArgs(window.From, window.To, asset)...); err != nil {
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

func resolveUSSourceBounds(ctx context.Context, conn driver.Conn, tableName, assetColumn, asset string) (time.Time, time.Time, bool, error) {
	query := fmt.Sprintf(`SELECT min(market_date), max(market_date)
FROM %s
WHERE is_regular_session = 1`, tableName)
	args := make([]interface{}, 0, 1)
	if asset != "" {
		query += fmt.Sprintf(" AND %s = {asset:String}", assetColumn)
		args = append(args, clickhouse.Named("asset", asset))
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("query %s source bounds: %w", tableName, err)
	}
	defer rows.Close()

	var (
		minDate time.Time
		maxDate time.Time
	)
	if rows.Next() {
		if err := rows.Scan(&minDate, &maxDate); err != nil {
			return time.Time{}, time.Time{}, false, fmt.Errorf("scan %s source bounds: %w", tableName, err)
		}
	}
	if minDate.IsZero() || maxDate.IsZero() {
		return time.Time{}, time.Time{}, false, nil
	}

	from := normalizeUTCDay(minDate)
	to := normalizeUTCDay(maxDate).Add(24 * time.Hour)
	return from, to, true, nil
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

	windows := make([]usBackfillWindow, 0, int(to.Sub(from)/(24*time.Hour))+1)
	cursor := from.UTC()
	for cursor.Before(to) {
		next := normalizeUTCDay(cursor).Add(24 * time.Hour)
		if !next.After(cursor) {
			next = cursor.Add(24 * time.Hour)
		}
		if next.After(to) {
			next = to.UTC()
		}
		windows = append(windows, usBackfillWindow{From: cursor, To: next})
		cursor = next
	}

	return windows
}

func normalizeUTCDay(ts time.Time) time.Time {
	ts = ts.UTC()
	return time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
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
