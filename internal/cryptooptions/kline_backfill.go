package cryptooptions

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// DefaultKlineWindows is the default window set used by import/backfill flows.
// The 1m source tables are already materialized, so backfill only needs derived windows.
var DefaultKlineWindows = []string{"5m", "15m", "30m", "1h", "2h", "3h", "4h", "6h", "8h", "12h", "1d"}

const (
	backfillTimeoutRetryDelay = 5 * time.Second
	chainBackfillChunkSize    = 24 * time.Hour
)

// KlineBackfillOptions controls manual K-line backfill behavior.
type KlineBackfillOptions struct {
	Intervals []string
	From      time.Time // inclusive, UTC; zero means no lower bound
	To        time.Time // exclusive, UTC; zero means no upper bound
	BaseAsset string
	Replace   bool
}

// BackfillKlineWindows fills precomputed K-line aggregation tables from 1m base tables.
func BackfillKlineWindows(ctx context.Context, conn driver.Conn, opts KlineBackfillOptions) error {
	intervals, err := normalizeBackfillIntervals(opts.Intervals)
	if err != nil {
		return err
	}

	if !opts.From.IsZero() && !opts.To.IsZero() && !opts.To.After(opts.From) {
		return fmt.Errorf("invalid time range: to must be after from")
	}

	baseAsset := strings.ToUpper(strings.TrimSpace(opts.BaseAsset))
	intervalToConfig := make(map[string]KlineInterval, len(KlineIntervals))
	for _, iv := range KlineIntervals {
		intervalToConfig[iv.Suffix] = iv
	}

	for _, interval := range intervals {
		iv, ok := intervalToConfig[interval]
		if !ok {
			return fmt.Errorf("interval %q is not precomputed", interval)
		}

		if err := backfillOptionInterval(ctx, conn, iv, opts.From, opts.To, baseAsset, opts.Replace); err != nil {
			return fmt.Errorf("backfill option interval %s: %w", interval, err)
		}
		if _, ok := ChainPrecomputedIntervals[iv.Suffix]; ok {
			if err := backfillChainInterval(ctx, conn, iv, opts.From, opts.To, baseAsset, opts.Replace); err != nil {
				return fmt.Errorf("backfill chain interval %s: %w", interval, err)
			}
		}
		if err := backfillSpotInterval(ctx, conn, iv, opts.From, opts.To, baseAsset, opts.Replace); err != nil {
			return fmt.Errorf("backfill spot interval %s: %w", interval, err)
		}
	}

	return nil
}

func normalizeBackfillIntervals(input []string) ([]string, error) {
	if len(input) == 0 {
		out := make([]string, len(DefaultKlineWindows))
		copy(out, DefaultKlineWindows)
		return out, nil
	}

	allowed := make(map[string]struct{}, len(DefaultKlineWindows))
	for _, iv := range DefaultKlineWindows {
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
			return nil, fmt.Errorf("unsupported interval %q", iv)
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

func backfillOptionInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, baseAsset string, replace bool) error {
	aggTable := fmt.Sprintf("crypto_options_bar_%s_agg", iv.Suffix)

	hasRows, err := optionAggHasRows(ctx, conn, aggTable, from, to, baseAsset)
	if err != nil {
		return err
	}
	if hasRows && !replace {
		log.Printf("[kline-backfill] skip option %s: target table already has rows in selected scope", iv.Suffix)
		return nil
	}

	if hasRows && replace {
		if err := optionAggDeleteScope(ctx, conn, aggTable, from, to, baseAsset); err != nil {
			return fmt.Errorf("clear existing option scope: %w", err)
		}
	}

	query := fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol_id,
    base_asset,
    argMinState(mark_open, timestamp)              AS mark_open_state,
    maxState(mark_high)                            AS mark_high_state,
    minState(mark_low)                             AS mark_low_state,
    argMaxState(mark_close, timestamp)             AS mark_close_state,
    argMinState(last_open, timestamp)              AS last_open_state,
    maxState(last_high)                            AS last_high_state,
    minState(last_low)                             AS last_low_state,
    argMaxState(last_close, timestamp)             AS last_close_state,
    argMinState(bid_open, timestamp)               AS bid_open_state,
    maxState(bid_high)                             AS bid_high_state,
    minState(bid_low)                              AS bid_low_state,
    argMaxState(bid_close, timestamp)              AS bid_close_state,
    argMinState(ask_open, timestamp)               AS ask_open_state,
    maxState(ask_high)                             AS ask_high_state,
    minState(ask_low)                              AS ask_low_state,
    argMaxState(ask_close, timestamp)              AS ask_close_state,
    argMinState(mark_iv_open, timestamp)           AS mark_iv_open_state,
    argMaxState(mark_iv_close, timestamp)          AS mark_iv_close_state,
    argMinState(bid_iv_open, timestamp)            AS bid_iv_open_state,
    argMinState(ask_iv_open, timestamp)            AS ask_iv_open_state,
    argMinState(delta, timestamp)                  AS delta_state,
    argMinState(gamma, timestamp)                  AS gamma_state,
    argMinState(vega, timestamp)                   AS vega_state,
    argMinState(theta, timestamp)                  AS theta_state,
    argMinState(rho, timestamp)                    AS rho_state,
    argMaxState(open_interest, timestamp)          AS open_interest_state,
    sumState(tick_count)                           AS tick_count_state
FROM crypto_options_bar_1m
%s
GROUP BY ts, symbol_id, base_asset`, aggTable, iv.TimeFunc, optionSourceWhere(from, to, baseAsset))

	args := optionSourceArgs(from, to, baseAsset)
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("insert aggregated option rows for %s", iv.Suffix), func() error {
		return conn.Exec(ctx, query, args...)
	}); err != nil {
		return fmt.Errorf("insert aggregated option rows: %w", err)
	}

	log.Printf("[kline-backfill] option interval %s completed", iv.Suffix)
	return nil
}

func backfillSpotInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, baseAsset string, replace bool) error {
	aggTable := fmt.Sprintf("crypto_spot_bar_%s_agg", iv.Suffix)

	hasRows, err := spotAggHasRows(ctx, conn, aggTable, from, to, baseAsset)
	if err != nil {
		return err
	}
	if hasRows && !replace {
		log.Printf("[kline-backfill] skip spot %s: target table already has rows in selected scope", iv.Suffix)
		return nil
	}

	if hasRows && replace {
		if err := spotAggDeleteScope(ctx, conn, aggTable, from, to, baseAsset); err != nil {
			return fmt.Errorf("clear existing spot scope: %w", err)
		}
	}

	query := fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    any(price_source)                            AS price_source,
    argMinState(open, timestamp)                 AS open_state,
    maxState(high)                               AS high_state,
    minState(low)                                AS low_state,
    argMaxState(close, timestamp)                AS close_state,
    sumState(tick_count)                         AS tick_count_state
FROM crypto_spot_bar_1m
%s
GROUP BY ts, symbol`, aggTable, iv.TimeFunc, spotSourceWhere(from, to, baseAsset))

	args := spotSourceArgs(from, to, baseAsset)
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("insert aggregated spot rows for %s", iv.Suffix), func() error {
		return conn.Exec(ctx, query, args...)
	}); err != nil {
		return fmt.Errorf("insert aggregated spot rows: %w", err)
	}

	log.Printf("[kline-backfill] spot interval %s completed", iv.Suffix)
	return nil
}

func backfillChainInterval(ctx context.Context, conn driver.Conn, iv KlineInterval, from, to time.Time, baseAsset string, replace bool) error {
	aggTable := fmt.Sprintf("crypto_options_chain_%s_agg", iv.Suffix)

	effectiveFrom, effectiveTo, hasSourceRows, err := resolveOptionSourceBounds(ctx, conn, from, to, baseAsset)
	if err != nil {
		return err
	}
	if !hasSourceRows {
		log.Printf("[kline-backfill] skip chain %s: no source rows in selected scope", iv.Suffix)
		return nil
	}

	windows := splitBackfillWindows(effectiveFrom, effectiveTo, chainBackfillChunkSize)
	if len(windows) == 0 {
		log.Printf("[kline-backfill] skip chain %s: no backfill windows generated", iv.Suffix)
		return nil
	}

	log.Printf("[kline-backfill] chain interval %s processing %d day-sized chunks from %s to %s", iv.Suffix, len(windows), effectiveFrom.Format(time.RFC3339), effectiveTo.Format(time.RFC3339))

	for idx, window := range windows {
		hasRows, err := chainAggHasRows(ctx, conn, aggTable, window.From, window.To, baseAsset)
		if err != nil {
			return err
		}
		if hasRows && !replace {
			log.Printf("[kline-backfill] skip chain %s chunk %d/%d: target table already has rows in %s to %s", iv.Suffix, idx+1, len(windows), window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
			continue
		}

		if hasRows && replace {
			if err := chainAggDeleteScope(ctx, conn, aggTable, window.From, window.To, baseAsset); err != nil {
				return fmt.Errorf("clear existing chain scope: %w", err)
			}
		}

		query := buildChainBackfillInsertSQL(aggTable, iv, window.From, window.To, baseAsset)
		args := optionSourceArgs(window.From, window.To, baseAsset)
		log.Printf("[kline-backfill] chain interval %s chunk %d/%d started: %s to %s", iv.Suffix, idx+1, len(windows), window.From.Format(time.RFC3339), window.To.Format(time.RFC3339))
		if err := retryBackfillTimeout(ctx, fmt.Sprintf("insert chain cache rows for %s chunk %d/%d", iv.Suffix, idx+1, len(windows)), func() error {
			return conn.Exec(ctx, query, args...)
		}); err != nil {
			return fmt.Errorf("insert chain cache rows: %w", err)
		}

		log.Printf("[kline-backfill] chain interval %s chunk %d/%d completed", iv.Suffix, idx+1, len(windows))
	}

	log.Printf("[kline-backfill] chain interval %s completed", iv.Suffix)
	return nil
}

func buildChainBackfillInsertSQL(aggTable string, iv KlineInterval, from, to time.Time, baseAsset string) string {
	whereClause := optionSourceWhere(from, to, baseAsset)
	if iv.Suffix == "1m" {
		return fmt.Sprintf(`INSERT INTO %s
SELECT
	    timestamp AS ts,
    base_asset,
    symbol_id,
	argMinState(delta, timestamp)         AS delta_state,
	argMinState(gamma, timestamp)         AS gamma_state,
	argMinState(vega, timestamp)          AS vega_state,
	argMinState(theta, timestamp)         AS theta_state,
	argMinState(rho, timestamp)           AS rho_state,
	argMaxState(bid_close, timestamp)     AS bid_close_state,
	argMaxState(ask_close, timestamp)     AS ask_close_state,
	argMaxState(mark_close, timestamp)    AS mark_close_state,
	argMaxState(mark_iv_close, timestamp) AS mark_iv_close_state,
	    sumState(toUInt64(tick_count))   AS tick_count_state,
	argMaxState(open_interest, timestamp) AS open_interest_state
FROM crypto_options_bar_1m
%s
GROUP BY ts, base_asset, symbol_id`, aggTable, whereClause)
	}

	return fmt.Sprintf(`INSERT INTO %s
SELECT
	 ts,
	 base_asset,
	 symbol_id,
	 argMinState(delta, first_ts)          AS delta_state,
	 argMinState(gamma, first_ts)          AS gamma_state,
	 argMinState(vega, first_ts)           AS vega_state,
	 argMinState(theta, first_ts)          AS theta_state,
	 argMinState(rho, first_ts)            AS rho_state,
	 argMaxState(bid_close, last_ts)       AS bid_close_state,
	 argMaxState(ask_close, last_ts)       AS ask_close_state,
	 argMaxState(mark_close, last_ts)      AS mark_close_state,
	 argMaxState(mark_iv_close, last_ts)   AS mark_iv_close_state,
	 sumState(tick_count)                  AS tick_count_state,
	 argMaxState(open_interest, last_ts)   AS open_interest_state
FROM
(
	 SELECT
	 	%s AS ts,
	 	symbol_id,
	 	base_asset,
	 	min(timestamp)                    AS first_ts,
	 	max(timestamp)                    AS last_ts,
	 	argMin(delta, timestamp)         AS delta,
	 	argMin(gamma, timestamp)         AS gamma,
	 	argMin(vega, timestamp)          AS vega,
	 	argMin(theta, timestamp)         AS theta,
	 	argMin(rho, timestamp)           AS rho,
	 	argMax(bid_close, timestamp)     AS bid_close,
	 	argMax(ask_close, timestamp)     AS ask_close,
	 	argMax(mark_close, timestamp)    AS mark_close,
	 	argMax(mark_iv_close, timestamp) AS mark_iv_close,
	 	sum(toUInt64(tick_count))        AS tick_count,
	 	argMax(open_interest, timestamp) AS open_interest
	 FROM crypto_options_bar_1m
	 %s
	 GROUP BY ts, symbol_id, base_asset
)
GROUP BY ts, base_asset, symbol_id`, aggTable, iv.TimeFunc, whereClause)
}

type backfillWindow struct {
	From time.Time
	To   time.Time
}

func splitBackfillWindows(from, to time.Time, step time.Duration) []backfillWindow {
	if from.IsZero() || to.IsZero() || !to.After(from) || step <= 0 {
		return nil
	}

	windows := make([]backfillWindow, 0)
	for current := from.UTC(); current.Before(to.UTC()); current = current.Add(step) {
		next := current.Add(step)
		if next.After(to.UTC()) {
			next = to.UTC()
		}
		windows = append(windows, backfillWindow{From: current, To: next})
	}
	return windows
}

func resolveOptionSourceBounds(ctx context.Context, conn driver.Conn, from, to time.Time, baseAsset string) (time.Time, time.Time, bool, error) {
	if !from.IsZero() && !to.IsZero() {
		return from.UTC(), to.UTC(), true, nil
	}

	query := fmt.Sprintf(`SELECT count(), min(timestamp), max(timestamp) FROM crypto_options_bar_1m %s`, optionSourceWhere(from, to, baseAsset))
	args := optionSourceArgs(from, to, baseAsset)

	var count uint64
	var minTS time.Time
	var maxTS time.Time
	if err := retryBackfillTimeout(ctx, "resolve option source bounds", func() error {
		rows, err := conn.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query option source bounds: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			count = 0
			return nil
		}
		if err := rows.Scan(&count, &minTS, &maxTS); err != nil {
			return fmt.Errorf("scan option source bounds: %w", err)
		}
		return nil
	}); err != nil {
		return time.Time{}, time.Time{}, false, err
	}

	if count == 0 {
		return time.Time{}, time.Time{}, false, nil
	}

	resolvedFrom := from.UTC()
	if resolvedFrom.IsZero() {
		resolvedFrom = minTS.UTC().Truncate(chainBackfillChunkSize)
	}
	resolvedTo := to.UTC()
	if resolvedTo.IsZero() {
		resolvedTo = maxTS.UTC().Add(time.Minute)
	}
	if !resolvedTo.After(resolvedFrom) {
		return time.Time{}, time.Time{}, false, fmt.Errorf("resolved option source bounds are invalid: from=%s to=%s", resolvedFrom.Format(time.RFC3339), resolvedTo.Format(time.RFC3339))
	}
	return resolvedFrom, resolvedTo, true, nil
}

func optionAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, optionAggScopeWhere(from, to, baseAsset))
	var count uint64
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("query option agg row count for %s", aggTable), func() error {
		rows, err := conn.Query(ctx, query, optionAggScopeArgs(from, to, baseAsset)...)
		if err != nil {
			return fmt.Errorf("query option agg row count: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			count = 0
			return nil
		}
		if err := rows.Scan(&count); err != nil {
			return fmt.Errorf("scan option agg row count: %w", err)
		}
		return nil
	}); err != nil {
		return false, err
	}
	return count > 0, nil
}

func spotAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, spotAggScopeWhere(from, to, baseAsset))
	var count uint64
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("query spot agg row count for %s", aggTable), func() error {
		rows, err := conn.Query(ctx, query, spotAggScopeArgs(from, to, baseAsset)...)
		if err != nil {
			return fmt.Errorf("query spot agg row count: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			count = 0
			return nil
		}
		if err := rows.Scan(&count); err != nil {
			return fmt.Errorf("scan spot agg row count: %w", err)
		}
		return nil
	}); err != nil {
		return false, err
	}
	return count > 0, nil
}

func chainAggHasRows(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) (bool, error) {
	query := fmt.Sprintf("SELECT count() FROM %s%s", aggTable, optionAggScopeWhere(from, to, baseAsset))
	var count uint64
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("query chain agg row count for %s", aggTable), func() error {
		rows, err := conn.Query(ctx, query, optionAggScopeArgs(from, to, baseAsset)...)
		if err != nil {
			return fmt.Errorf("query chain agg row count: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			count = 0
			return nil
		}
		if err := rows.Scan(&count); err != nil {
			return fmt.Errorf("scan chain agg row count: %w", err)
		}
		return nil
	}); err != nil {
		return false, err
	}
	return count > 0, nil
}

func optionAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, optionAggScopeWhere(from, to, baseAsset))
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("delete option agg scope for %s", aggTable), func() error {
		return conn.Exec(ctx, query, optionAggScopeArgs(from, to, baseAsset)...)
	}); err != nil {
		return fmt.Errorf("delete option agg scope: %w", err)
	}
	return nil
}

func spotAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, spotAggScopeWhere(from, to, baseAsset))
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("delete spot agg scope for %s", aggTable), func() error {
		return conn.Exec(ctx, query, spotAggScopeArgs(from, to, baseAsset)...)
	}); err != nil {
		return fmt.Errorf("delete spot agg scope: %w", err)
	}
	return nil
}

func chainAggDeleteScope(ctx context.Context, conn driver.Conn, aggTable string, from, to time.Time, baseAsset string) error {
	query := fmt.Sprintf("ALTER TABLE %s DELETE%s SETTINGS mutations_sync = 1", aggTable, optionAggScopeWhere(from, to, baseAsset))
	if err := retryBackfillTimeout(ctx, fmt.Sprintf("delete chain agg scope for %s", aggTable), func() error {
		return conn.Exec(ctx, query, optionAggScopeArgs(from, to, baseAsset)...)
	}); err != nil {
		return fmt.Errorf("delete chain agg scope: %w", err)
	}
	return nil
}

func retryBackfillTimeout(ctx context.Context, operation string, fn func() error) error {
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isRetryableTimeout(err) {
			return err
		}

		log.Printf("[kline-backfill] warning: %s timed out on attempt %d, retrying in %s: %v", operation, attempt, backfillTimeoutRetryDelay, err)

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: context canceled while waiting to retry: %w", operation, ctx.Err())
		case <-time.After(backfillTimeoutRetryDelay):
		}
	}
}

func isRetryableTimeout(err error) bool {
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
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "i/o timeout") ||
		strings.Contains(message, "context deadline exceeded") ||
		strings.Contains(message, "read timeout")
}

func optionSourceWhere(from, to time.Time, baseAsset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "timestamp >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "timestamp < toDateTime({to:String}, 'UTC')")
	}
	if baseAsset != "" {
		parts = append(parts, "base_asset = {base_asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(parts, " AND ")
}

func spotSourceWhere(from, to time.Time, baseAsset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "timestamp >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "timestamp < toDateTime({to:String}, 'UTC')")
	}
	if baseAsset != "" {
		parts = append(parts, "symbol = {base_asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(parts, " AND ")
}

func optionSourceArgs(from, to time.Time, baseAsset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", ClickHouseTimeParam(from)))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", ClickHouseTimeParam(to)))
	}
	if baseAsset != "" {
		args = append(args, clickhouse.Named("base_asset", baseAsset))
	}
	return args
}

func spotSourceArgs(from, to time.Time, baseAsset string) []interface{} {
	args := make([]interface{}, 0, 3)
	if !from.IsZero() {
		args = append(args, clickhouse.Named("from", ClickHouseTimeParam(from)))
	}
	if !to.IsZero() {
		args = append(args, clickhouse.Named("to", ClickHouseTimeParam(to)))
	}
	if baseAsset != "" {
		args = append(args, clickhouse.Named("base_asset", baseAsset))
	}
	return args
}

func optionAggScopeWhere(from, to time.Time, baseAsset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "ts >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "ts < toDateTime({to:String}, 'UTC')")
	}
	if baseAsset != "" {
		parts = append(parts, "base_asset = {base_asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

func spotAggScopeWhere(from, to time.Time, baseAsset string) string {
	parts := make([]string, 0, 3)
	if !from.IsZero() {
		parts = append(parts, "ts >= toDateTime({from:String}, 'UTC')")
	}
	if !to.IsZero() {
		parts = append(parts, "ts < toDateTime({to:String}, 'UTC')")
	}
	if baseAsset != "" {
		parts = append(parts, "symbol = {base_asset:String}")
	}
	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

func optionAggScopeArgs(from, to time.Time, baseAsset string) []interface{} {
	return optionSourceArgs(from, to, baseAsset)
}

func spotAggScopeArgs(from, to time.Time, baseAsset string) []interface{} {
	return spotSourceArgs(from, to, baseAsset)
}
