package datafeed

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

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
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = '%s'`, escapeString(tableName)))
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
	return count > 0, nil
}

func columnExists(ctx context.Context, conn driver.Conn, tableName, columnName string) (bool, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf(`SELECT count()
FROM system.columns
WHERE database = currentDatabase()
  AND table = '%s'
  AND name = '%s'`, escapeString(tableName), escapeString(columnName)))
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
	return count > 0, nil
}

func buildSpotSourceSQLWithFallback(ctx context.Context, conn driver.Conn, interval, symbol string, from, to time.Time) (string, bool, error) {
	if tableName := resolveSpotTableName(interval); tableName != "" {
		exists, err := tableExists(ctx, conn, tableName)
		if err != nil {
			return "", false, err
		}
		if exists {
			return fmt.Sprintf(`SELECT
    timestamp, symbol, price_source, open, high, low, close, tick_count
FROM %s
WHERE symbol = '%s'
  AND timestamp >= '%s'
  AND timestamp < '%s'`,
				tableName,
				escapeString(symbol),
				from.Format("2006-01-02 15:04:05"),
				to.Format("2006-01-02 15:04:05"),
			), false, nil
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
		return fmt.Sprintf(`SELECT
    timestamp, symbol, price_source, open, high, low, close, tick_count
FROM crypto_spot_bar_1m
WHERE symbol = '%s'
  AND timestamp >= '%s'
  AND timestamp < '%s'`,
			escapeString(symbol),
			from.Format("2006-01-02 15:04:05"),
			to.Format("2006-01-02 15:04:05"),
		), false, nil
	}

	adhocSQL, err := cryptooptions.QuerySpotAggregationSQL(interval)
	if err != nil {
		return "", false, err
	}

	log.Printf("[compat] spot view for %s missing; falling back to query-time aggregation from crypto_spot_bar_1m", interval)
	return replaceNamedStringParamsCompat(adhocSQL, symbol, from, to), true, nil
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
WHERE base_asset = '%s'
  AND timestamp >= '%s'
  AND timestamp < '%s'
  AND underlying_price_close > 0
GROUP BY timestamp
ORDER BY timestamp`,
		tableName,
		escapeString(baseAsset),
		from.Format("2006-01-02 15:04:05"),
		to.Format("2006-01-02 15:04:05"),
	), true, nil
}

func replaceNamedStringParamsCompat(sql, symbol string, from, to time.Time) string {
	sql = replaceParamCompat(sql, "{symbol:String}", "'"+escapeString(symbol)+"'")
	sql = replaceParamCompat(sql, "{from:DateTime}", "'"+from.Format("2006-01-02 15:04:05")+"'")
	sql = replaceParamCompat(sql, "{to:DateTime}", "'"+to.Format("2006-01-02 15:04:05")+"'")
	return sql
}

func replaceParamCompat(sql, old, new string) string {
	for {
		idx := indexOfCompat(sql, old)
		if idx < 0 {
			return sql
		}
		sql = sql[:idx] + new + sql[idx+len(old):]
	}
}

func indexOfCompat(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}