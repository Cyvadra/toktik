package datafeed

import (
	"context"
	"fmt"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/chquery"
)

var usStockPrecomputedIntervals = map[string]string{
	"5m":  "us_stocks_bar_5m",
	"15m": "us_stocks_bar_15m",
	"30m": "us_stocks_bar_30m",
	"1h":  "us_stocks_bar_1h",
	"2h":  "us_stocks_bar_2h",
	"4h":  "us_stocks_bar_4h",
	"1d":  "us_stocks_bar_1d",
}

// USUnderlyingDataFeed implements backtest.DataFeed using US stock bar tables.
type USUnderlyingDataFeed struct {
	conn driver.Conn
}

// NewUSUnderlyingDataFeed creates a DataFeed for US underlyings.
func NewUSUnderlyingDataFeed(conn driver.Conn) *USUnderlyingDataFeed {
	return &USUnderlyingDataFeed{conn: conn}
}

// Fields returns the list of available fields.
func (f *USUnderlyingDataFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "volume", "transactions"}
}

// Load fetches underlying OHLC bars from US stock tables/views.
func (f *USUnderlyingDataFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	tableName, err := resolveUSStockTableName(req.Interval)
	if err != nil {
		return nil, err
	}
	exists, err := tableExists(ctx, f.conn, tableName)
	if err != nil {
		return nil, fmt.Errorf("check table %s: %w", tableName, err)
	}
	if !exists {
		return nil, fmt.Errorf("required US stock interval table/view %s not found; run us-market-import schema init first", tableName)
	}

	query := fmt.Sprintf(`SELECT
		b.timestamp,
		toFloat32(%s) AS open,
		toFloat32(%s) AS high,
		toFloat32(%s) AS low,
		toFloat32(%s) AS close,
		b.volume,
		b.transactions
FROM %s AS b
%s
WHERE b.symbol = {symbol:String}
	AND b.timestamp >= toDateTime({from:String}, 'UTC')
	AND b.timestamp < toDateTime({to:String}, 'UTC')
GROUP BY b.timestamp, b.symbol, b.open, b.high, b.low, b.close, b.volume, b.transactions
ORDER BY b.timestamp`, chquery.USStockAdjustedPriceSQL("b", "open", "sp"), chquery.USStockAdjustedPriceSQL("b", "high", "sp"), chquery.USStockAdjustedPriceSQL("b", "low", "sp"), chquery.USStockAdjustedPriceSQL("b", "close", "sp"), tableName, chquery.USStockSplitJoinSQL("b", "sp"))

	rows, err := f.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", backtestTimeParam(req.From)),
		clickhouse.Named("to", backtestTimeParam(req.To)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US underlying bars for %s/%s: %w", req.Symbol, req.Interval, err)
	}
	defer rows.Close()

	timestamps := make([]time.Time, 0, 2048)
	opens := make([]float64, 0, 2048)
	highs := make([]float64, 0, 2048)
	lows := make([]float64, 0, 2048)
	closes := make([]float64, 0, 2048)
	volumes := make([]float64, 0, 2048)
	transactions := make([]float64, 0, 2048)

	for rows.Next() {
		var (
			ts                     time.Time
			open, high, low, close float32
			volume                 float64
			tx                     uint64
		)
		if err := rows.Scan(&ts, &open, &high, &low, &close, &volume, &tx); err != nil {
			return nil, fmt.Errorf("scan US underlying row: %w", err)
		}
		timestamps = append(timestamps, ts)
		opens = append(opens, float64(open))
		highs = append(highs, float64(high))
		lows = append(lows, float64(low))
		closes = append(closes, float64(close))
		volumes = append(volumes, float64(volume))
		transactions = append(transactions, float64(tx))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US underlying rows: %w", err)
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	ds.AddColumn("open", opens)
	ds.AddColumn("high", highs)
	ds.AddColumn("low", lows)
	ds.AddColumn("close", closes)
	ds.AddColumn("volume", volumes)
	ds.AddColumn("transactions", transactions)
	return ds, nil
}

func resolveUSStockTableName(interval string) (string, error) {
	if interval == "1m" {
		return "us_stocks_bar_1m", nil
	}
	if table, ok := usStockPrecomputedIntervals[interval]; ok {
		return table, nil
	}
	return "", fmt.Errorf("unsupported US stock interval %q (supported: 1m,5m,15m,30m,1h,2h,4h,1d)", interval)
}
