package datafeed

import (
	"context"
	"fmt"
	"math"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/forexmarket"
)

// ForexUnderlyingDataFeed implements backtest.DataFeed using forex bar tables.
type ForexUnderlyingDataFeed struct {
	conn driver.Conn
}

// NewForexUnderlyingDataFeed creates a DataFeed for forex underlyings.
func NewForexUnderlyingDataFeed(conn driver.Conn) *ForexUnderlyingDataFeed {
	return &ForexUnderlyingDataFeed{conn: conn}
}

// Fields returns the list of available fields.
func (f *ForexUnderlyingDataFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume", "transactions", "compat_fallback"}
}

// Load fetches OHLC bars from forex base tables or precomputed interval views.
func (f *ForexUnderlyingDataFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	query, degraded, err := buildForexSourceSQLWithFallback(ctx, f.conn, req.Interval)
	if err != nil {
		return nil, fmt.Errorf("resolve forex source for %s/%s: %w", req.Symbol, req.Interval, err)
	}
	if query == "" {
		return nil, fmt.Errorf("load forex underlying for %s: no compatible forex source found", req.Symbol)
	}

	rows, err := f.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", backtestTimeParam(req.From)),
		clickhouse.Named("to", backtestTimeParam(req.To)),
	)
	if err != nil {
		return nil, fmt.Errorf("query forex underlying bars for %s/%s: %w", req.Symbol, req.Interval, err)
	}
	defer rows.Close()

	timestamps := make([]time.Time, 0, 2048)
	opens := make([]float64, 0, 2048)
	highs := make([]float64, 0, 2048)
	lows := make([]float64, 0, 2048)
	closes := make([]float64, 0, 2048)
	volumes := make([]float64, 0, 2048)
	transactions := make([]float64, 0, 2048)
	fallbackMode := make([]float64, 0, 2048)

	for rows.Next() {
		var (
			ts                     time.Time
			open, high, low, close float32
			volume                 float64
			tx                     uint64
		)
		if err := rows.Scan(&ts, &open, &high, &low, &close, &volume, &tx); err != nil {
			return nil, fmt.Errorf("scan forex underlying row: %w", err)
		}
		timestamps = append(timestamps, ts)
		opens = append(opens, float64(open))
		highs = append(highs, float64(high))
		lows = append(lows, float64(low))
		closes = append(closes, float64(close))
		volumes = append(volumes, volume)
		transactions = append(transactions, float64(tx))
		if degraded {
			fallbackMode = append(fallbackMode, 1)
		} else {
			fallbackMode = append(fallbackMode, 0)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate forex underlying rows: %w", err)
	}

	if len(transactions) == 0 && degraded {
		transactions = []float64{}
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	ds.AddColumn("open", opens)
	ds.AddColumn("high", highs)
	ds.AddColumn("low", lows)
	ds.AddColumn("close", closes)
	ds.AddColumn("volume", volumes)
	ds.AddColumn("transactions", transactions)
	ds.AddColumn("compat_fallback", fallbackMode)
	return ds, nil
}

func resolveForexTableName(interval string) string {
	if interval == "1m" {
		return "forex_bar_1m"
	}
	if table, ok := forexmarket.PrecomputedIntervals[interval]; ok {
		return table
	}
	return ""
}

func buildForexSourceSQLWithFallback(ctx context.Context, conn driver.Conn, interval string) (string, bool, error) {
	if tableName := resolveForexTableName(interval); tableName != "" {
		exists, err := tableExists(ctx, conn, tableName)
		if err != nil {
			return "", false, err
		}
		if exists {
			return fmt.Sprintf(`SELECT
    timestamp,
    open,
    high,
    low,
    close,
    volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp`, tableName), false, nil
		}
	}

	baseExists, err := tableExists(ctx, conn, "forex_bar_1m")
	if err != nil {
		return "", false, err
	}
	if !baseExists {
		return "", false, nil
	}
	if interval == "1m" {
		return `SELECT
    timestamp,
    open,
    high,
    low,
    close,
    volume,
    toUInt64(transactions) AS transactions
FROM forex_bar_1m
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp`, false, nil
	}

	adhocSQL, err := forexmarket.QueryAggregationSQL(interval)
	if err != nil {
		return "", false, err
	}
	return adhocSQL, true, nil
}

func normalizeForexFallbackTransactions(values []float64, size int, degraded bool) []float64 {
	if len(values) > 0 {
		return values
	}
	values = make([]float64, size)
	for index := range values {
		if degraded {
			values[index] = math.NaN()
		}
	}
	return values
}
