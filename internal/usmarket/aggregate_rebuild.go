package usmarket

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// AggregateRebuildOptions configures scoped aggregate rebuilds.
//
// All option/stock aggregate tables (us_options_bar_*_agg, us_stocks_bar_*_agg,
// us_options_chain_*_agg) are PARTITION BY toYYYYMM(ts). Because the bucket
// timestamp `ts` derived from base `timestamp` always lands in the same
// calendar month as the source bar, dropping an aggregate partition for
// YYYYMM=M and re-inserting only the rows where toYYYYMM(timestamp)=M from the
// base table rebuilds exactly that partition deterministically.
//
// When Months is empty the full table is rebuilt (legacy behaviour).
// When Intervals is empty all intervals registered with the rebuild function
// are processed.
type AggregateRebuildOptions struct {
	// Months is a list of partitions to rebuild, formatted as "YYYYMM"
	// (e.g. "202405"). Order is normalised, duplicates removed.
	Months []string

	// Intervals optionally restricts the rebuild to a subset of interval
	// suffixes (e.g. ["1d", "5m"]). Empty means all intervals.
	Intervals []string

	// MaxMemoryUsageBytes maps to ClickHouse SETTING max_memory_usage.
	// Zero leaves the server default in place (NOT recommended for large
	// rebuilds).
	MaxMemoryUsageBytes uint64

	// MaxBytesBeforeExternalGroupBy maps to SETTING
	// max_bytes_before_external_group_by. Zero leaves server default.
	MaxBytesBeforeExternalGroupBy uint64

	// MaxThreads maps to SETTING max_threads. Zero leaves server default.
	MaxThreads int

	// DryRun, when true, only logs what would be executed.
	DryRun bool
}

func (o AggregateRebuildOptions) settingsSQL() string {
	parts := make([]string, 0, 3)
	if o.MaxMemoryUsageBytes > 0 {
		parts = append(parts, fmt.Sprintf("max_memory_usage = %d", o.MaxMemoryUsageBytes))
	}
	if o.MaxBytesBeforeExternalGroupBy > 0 {
		parts = append(parts, fmt.Sprintf("max_bytes_before_external_group_by = %d", o.MaxBytesBeforeExternalGroupBy))
	}
	if o.MaxThreads > 0 {
		parts = append(parts, fmt.Sprintf("max_threads = %d", o.MaxThreads))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\nSETTINGS " + strings.Join(parts, ", ")
}

// normalizeMonths uppercases, deduplicates and sorts month strings.
// Invalid entries (not 6 digits) are dropped silently.
func normalizeMonths(months []string) []string {
	if len(months) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(months))
	for _, m := range months {
		m = strings.TrimSpace(m)
		if len(m) != 6 {
			continue
		}
		ok := true
		for _, ch := range m {
			if ch < '0' || ch > '9' {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// filterIntervals returns the subset of `all` whose Suffix appears in `wanted`.
// Empty `wanted` returns all intervals.
func filterIntervals(all []KlineInterval, wanted []string) []KlineInterval {
	if len(wanted) == 0 {
		return all
	}
	want := map[string]struct{}{}
	for _, s := range wanted {
		want[strings.TrimSpace(s)] = struct{}{}
	}
	out := make([]KlineInterval, 0, len(wanted))
	for _, iv := range all {
		if _, ok := want[iv.Suffix]; ok {
			out = append(out, iv)
		}
	}
	return out
}

// partitionExists reports whether the named ClickHouse table currently has any
// active parts in the given YYYYMM partition. It is used to skip DROP PARTITION
// for partitions that are absent (otherwise ClickHouse returns an error).
func partitionExists(ctx context.Context, conn driver.Conn, table, partition string) (bool, error) {
	var c uint64
	q := `SELECT count() FROM system.parts
WHERE database = currentDatabase()
  AND table = ?
  AND active
  AND partition = ?`
	if err := conn.QueryRow(ctx, q, table, partition).Scan(&c); err != nil {
		return false, fmt.Errorf("probe partition %s/%s: %w", table, partition, err)
	}
	return c > 0, nil
}

// rebuildAggregateTable handles the partition-scoped rebuild flow for a single
// aggregate table: drop affected partitions (when present) and insert fresh
// rows produced by `insertSQLFunc(partition)`.
//
// `insertSQLFunc` MUST return an INSERT statement that already filters the
// source by toYYYYMM(timestamp) = partition. Memory SETTINGS are appended
// automatically.
//
// When opts.Months is empty the function falls back to TRUNCATE + full INSERT
// produced by `insertSQLFunc("")`. Use only when scoping is unavailable.
func rebuildAggregateTable(
	ctx context.Context,
	conn driver.Conn,
	logTag string,
	table string,
	opts AggregateRebuildOptions,
	insertSQLFunc func(partition string) string,
) error {
	settings := opts.settingsSQL()
	months := normalizeMonths(opts.Months)
	if len(months) == 0 {
		if opts.DryRun {
			log.Printf("[%s] dry-run: would TRUNCATE %s and re-insert full history", logTag, table)
			return nil
		}
		if err := conn.Exec(ctx, "TRUNCATE TABLE "+table); err != nil {
			return fmt.Errorf("truncate %s: %w", table, err)
		}
		sql := insertSQLFunc("") + settings
		if err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("rebuild %s (full): %w", table, err)
		}
		log.Printf("[%s] rebuilt %s (full history)", logTag, table)
		return nil
	}
	for _, m := range months {
		if opts.DryRun {
			log.Printf("[%s] dry-run: would DROP PARTITION '%s' and re-insert from base for %s", logTag, m, table)
			continue
		}
		exists, err := partitionExists(ctx, conn, table, m)
		if err != nil {
			return err
		}
		if exists {
			drop := fmt.Sprintf("ALTER TABLE %s DROP PARTITION '%s'", table, m)
			if err := conn.Exec(ctx, drop); err != nil {
				return fmt.Errorf("drop partition %s/%s: %w", table, m, err)
			}
		}
		sql := insertSQLFunc(m) + settings
		if err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("rebuild %s partition %s: %w", table, m, err)
		}
		log.Printf("[%s] rebuilt %s partition %s", logTag, table, m)
	}
	return nil
}

// monthFilterClause returns a WHERE-clause fragment selecting only the rows
// whose base timestamp falls in the given YYYYMM partition. The leading "AND "
// is included. An empty partition returns "" so callers can compose the same
// template for full rebuilds.
func monthFilterClause(partition string) string {
	if partition == "" {
		return ""
	}
	return fmt.Sprintf(" AND toYYYYMM(timestamp) = %s", partition)
}

// RebuildOptionKlineAggregatesScoped rebuilds us_options_bar_*_agg partitions
// for the supplied months/intervals. Use this from the data-integrity repair
// path to avoid full-history rebuilds.
func RebuildOptionKlineAggregatesScoped(ctx context.Context, conn driver.Conn, opts AggregateRebuildOptions) error {
	intervals := filterIntervals(KlineIntervals, opts.Intervals)
	for _, iv := range intervals {
		table := "us_options_bar_" + iv.Suffix + "_agg"
		err := rebuildAggregateTable(ctx, conn, "us-options-kline", table, opts, func(partition string) string {
			return optionKlineRebuildSQLScoped(iv, partition)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RebuildStockKlineAggregatesScoped rebuilds us_stocks_bar_*_agg partitions.
func RebuildStockKlineAggregatesScoped(ctx context.Context, conn driver.Conn, opts AggregateRebuildOptions) error {
	intervals := filterIntervals(KlineIntervals, opts.Intervals)
	for _, iv := range intervals {
		table := "us_stocks_bar_" + iv.Suffix + "_agg"
		err := rebuildAggregateTable(ctx, conn, "us-stocks-kline", table, opts, func(partition string) string {
			return stockKlineRebuildSQLScoped(iv, partition)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RebuildOptionChainCachesScoped rebuilds us_options_chain_*_agg partitions.
func RebuildOptionChainCachesScoped(ctx context.Context, conn driver.Conn, opts AggregateRebuildOptions) error {
	intervals := filterIntervals(DefaultChainCacheIntervals, opts.Intervals)
	for _, iv := range intervals {
		table := "us_options_chain_" + iv.Suffix + "_agg"
		err := rebuildAggregateTable(ctx, conn, "us-options-chain", table, opts, func(partition string) string {
			return optionChainRebuildSQLScoped(iv, partition)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func optionKlineRebuildSQLScoped(iv KlineInterval, partition string) string {
	agg := "us_options_bar_" + iv.Suffix + "_agg"
	timeFunc := klineTimeFunc(iv)
	return fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    underlying,
    option_type,
    expiration,
    strike,
    argMinState(open, timestamp)       AS open_state,
    maxState(high)                     AS high_state,
    minState(low)                      AS low_state,
    argMaxState(close, timestamp)      AS close_state,
    argMaxState(underlying_close, timestamp)   AS underlying_close_state,
    argMaxState(implied_volatility, timestamp) AS implied_volatility_state,
    argMaxState(delta, timestamp)      AS delta_state,
    argMaxState(gamma, timestamp)      AS gamma_state,
    argMaxState(vega, timestamp)       AS vega_state,
    argMaxState(theta, timestamp)      AS theta_state,
    argMaxState(rho, timestamp)        AS rho_state,
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM us_options_bar_1m
WHERE is_regular_session = 1%s
GROUP BY ts, symbol, underlying, option_type, expiration, strike`, agg, timeFunc, monthFilterClause(partition))
}

func stockKlineRebuildSQLScoped(iv KlineInterval, partition string) string {
	agg := "us_stocks_bar_" + iv.Suffix + "_agg"
	timeFunc := klineTimeFunc(iv)
	return fmt.Sprintf(`INSERT INTO %s
SELECT
    %s AS ts,
    symbol,
    argMinState(open, timestamp)       AS open_state,
    maxState(high)                     AS high_state,
    minState(low)                      AS low_state,
    argMaxState(close, timestamp)      AS close_state,
    sumState(volume)                   AS volume_state,
    sumState(transactions)             AS transactions_state
FROM us_stocks_bar_1m
WHERE is_regular_session = 1%s
GROUP BY ts, symbol`, agg, timeFunc, monthFilterClause(partition))
}

func optionChainRebuildSQLScoped(iv KlineInterval, partition string) string {
	agg := "us_options_chain_" + iv.Suffix + "_agg"
	timeFunc := "timestamp"
	if iv.Suffix != "1m" {
		timeFunc = klineTimeFunc(iv)
	}
	return fmt.Sprintf(`INSERT INTO %s
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
    sumState(toUInt64(transactions))           AS transactions_state
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
        sum(toUInt64(transactions))           AS transactions,
        max(timestamp)                        AS last_ts
    FROM us_options_bar_1m
    WHERE is_regular_session = 1%s
    GROUP BY ts, symbol, underlying, option_type, expiration, strike
)
GROUP BY ts, underlying, symbol`, agg, timeFunc, monthFilterClause(partition))
}
