package datafeed

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// schemaCache caches table/column existence checks for the lifetime of the process.
// Schema is assumed to be stable within a single run.
var schemaCache sync.Map

func resolveOptionTableName(interval string) string {
	if interval == "1m" {
		return "crypto_options_bar_1m"
	}
	if name, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
		return name
	}
	return "crypto_options_bar_1m"
}

func resolveSpotTableName(interval string) string {
	if interval == "1m" {
		return "crypto_spot_bar_1m"
	}
	if name, ok := cryptooptions.SpotPrecomputedIntervals[interval]; ok {
		return name
	}
	return ""
}

func tableExists(ctx context.Context, conn driver.Conn, tableName string) (bool, error) {
	cacheKey := "table:" + tableName
	if v, ok := schemaCache.Load(cacheKey); ok {
		return v.(bool), nil
	}

	rows, err := conn.Query(ctx, `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {table_name:String}`, clickhouse.Named("table_name", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if !rows.Next() {
		return false, nil
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	exists := count > 0
	schemaCache.Store(cacheKey, exists)
	return exists, nil
}

func columnExists(ctx context.Context, conn driver.Conn, tableName, columnName string) (bool, error) {
	cacheKey := "column:" + tableName + ":" + columnName
	if v, ok := schemaCache.Load(cacheKey); ok {
		return v.(bool), nil
	}

	rows, err := conn.Query(ctx, `SELECT count()
FROM system.columns
WHERE database = currentDatabase()
  AND table = {table_name:String}
  AND name = {column_name:String}`,
		clickhouse.Named("table_name", tableName),
		clickhouse.Named("column_name", columnName))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	if !rows.Next() {
		return false, nil
	}

	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	exists := count > 0
	schemaCache.Store(cacheKey, exists)
	return exists, nil
}

func buildSpotSourceSQLWithFallback(ctx context.Context, conn driver.Conn, interval, symbol string, from, to time.Time) (string, bool, error) {
	if tableName := resolveSpotTableName(interval); tableName != "" {
		exists, err := tableExists(ctx, conn, tableName)
		if err != nil {
			return "", false, err
		}
		if exists {
			return fmt.Sprintf(`SELECT
    timestamp, symbol, price_source, open, high, low, close, tick_count, volume_base, volume_quote
FROM %s
WHERE symbol = {symbol:String}
	AND timestamp >= toDateTime({from:String}, 'UTC')
	AND timestamp < toDateTime({to:String}, 'UTC')`, tableName), false, nil
		}
	}

	baseExists, err := tableExists(ctx, conn, "crypto_spot_bar_1m")
	if err != nil {
		return "", false, err
	}
	if !baseExists {
		return "", false, nil
	}

	if interval == "1m" {
		return `SELECT
    timestamp, symbol, price_source, open, high, low, close, tick_count, volume_base, volume_quote
FROM crypto_spot_bar_1m
WHERE symbol = {symbol:String}
	AND timestamp >= toDateTime({from:String}, 'UTC')
	AND timestamp < toDateTime({to:String}, 'UTC')`, false, nil
	}

	adhocSQL, err := cryptooptions.QuerySpotAggregationSQL(interval)
	if err != nil {
		return "", false, err
	}

	log.Printf("[compat] spot view for %s missing; falling back to query-time aggregation from crypto_spot_bar_1m", interval)
	return adhocSQL, true, nil
}

func legacyUnderlyingCloseExpr(ctx context.Context, conn driver.Conn, interval string) (string, bool, error) {
	tableName := resolveOptionTableName(interval)
	exists, err := columnExists(ctx, conn, tableName, "underlying_price_close")
	if err != nil {
		return "", false, err
	}
	if !exists {
		return "", false, nil
	}
	log.Printf("[compat] spot bars unavailable for %s; falling back to legacy underlying_price_close from %s", interval, tableName)
	return "ifNull(b.underlying_price_close, toFloat32(0))", true, nil
}

func buildLegacyUnderlyingSeriesSQL(ctx context.Context, conn driver.Conn, interval, baseAsset string, from, to time.Time) (string, bool, error) {
	tableName := resolveOptionTableName(interval)
	hasOpen, err := columnExists(ctx, conn, tableName, "underlying_price_open")
	if err != nil {
		return "", false, err
	}
	hasClose, err := columnExists(ctx, conn, tableName, "underlying_price_close")
	if err != nil {
		return "", false, err
	}
	if !hasOpen || !hasClose {
		return "", false, nil
	}

	log.Printf("[compat] standalone spot bars unavailable for %s; rebuilding underlying OHLC from legacy option bars in %s", interval, tableName)
	return fmt.Sprintf(`SELECT
    timestamp,
    argMin(underlying_price_open, symbol_id)  AS open,
    argMax(underlying_price_close, symbol_id) AS close,
    greatest(max(underlying_price_open), max(underlying_price_close)) AS high,
    least(
        min(if(underlying_price_open > 0, underlying_price_open, underlying_price_close)),
        min(if(underlying_price_close > 0, underlying_price_close, underlying_price_open))
    ) AS low
FROM %s
WHERE base_asset = {base_asset:String}
	AND timestamp >= toDateTime({from:String}, 'UTC')
	AND timestamp < toDateTime({to:String}, 'UTC')
  AND underlying_price_close > 0
GROUP BY timestamp
ORDER BY timestamp`, tableName), true, nil
}
