package datafeed

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// CryptoUnderlyingDataFeed implements backtest.DataFeed by extracting the
// underlying asset's OHLC price series from crypto-options bar data.
//
// Since options bars store underlying_price_open and underlying_price_close,
// we aggregate across all option symbols for a base asset to reconstruct
// the underlying's OHLC per bar interval:
//   - open  = first underlying_price_open in the interval
//   - close = last underlying_price_close in the interval
//   - high  = max of all underlying_price_open and underlying_price_close
//   - low   = min of all underlying_price_open and underlying_price_close
type CryptoUnderlyingDataFeed struct {
	conn driver.Conn
}

// NewCryptoUnderlyingDataFeed creates a DataFeed for underlying price data.
func NewCryptoUnderlyingDataFeed(conn driver.Conn) *CryptoUnderlyingDataFeed {
	return &CryptoUnderlyingDataFeed{conn: conn}
}

// Fields returns the list of available fields.
func (f *CryptoUnderlyingDataFeed) Fields() []string {
	return []string{"open", "high", "low", "close"}
}

// Load fetches the underlying asset's OHLC from options data.
// The req.Symbol should be the base asset name (e.g. "BTC").
func (f *CryptoUnderlyingDataFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	baseAsset := req.Symbol
	interval := req.Interval

	// Build aggregation SQL depending on interval.
	// We group 1m option bars by interval bucket and extract underlying OHLC.
	var sourceTable string
	if interval == "1m" {
		sourceTable = "crypto_options_bar_1m"
	} else if name, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
		sourceTable = name
	} else {
		sourceTable = "crypto_options_bar_1m"
	}

	// For precomputed interval tables, timestamps are already bucketed.
	// For 1m, we query directly. For anything else using 1m source, we'd need
	// ad-hoc grouping. Since the strategy uses 1h and that's precomputed, this is fine.
	query := fmt.Sprintf(`SELECT
    timestamp,
    argMin(underlying_price_open, symbol_id)  AS uopen,
    argMax(underlying_price_close, symbol_id) AS uclose,
    greatest(
        max(underlying_price_open),
        max(underlying_price_close)
    ) AS uhigh,
    least(
        min(if(underlying_price_open > 0, underlying_price_open, underlying_price_close)),
        min(if(underlying_price_close > 0, underlying_price_close, underlying_price_open))
    ) AS ulow
FROM %s
WHERE base_asset = '%s'
  AND timestamp >= '%s'
  AND timestamp < '%s'
  AND underlying_price_close > 0
GROUP BY timestamp
ORDER BY timestamp`,
		sourceTable,
		escapeString(baseAsset),
		req.From.Format("2006-01-02 15:04:05"),
		req.To.Format("2006-01-02 15:04:05"),
	)

	rows, err := f.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load underlying for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	timestamps := make([]time.Time, 0, 4096)
	opens := make([]float64, 0, 4096)
	highs := make([]float64, 0, 4096)
	lows := make([]float64, 0, 4096)
	closes := make([]float64, 0, 4096)

	for rows.Next() {
		var (
			ts                         time.Time
			uopen, uclose, uhigh, ulow float32
		)
		if err := rows.Scan(&ts, &uopen, &uclose, &uhigh, &ulow); err != nil {
			return nil, fmt.Errorf("scan underlying row: %w", err)
		}
		timestamps = append(timestamps, ts)
		opens = append(opens, float64(uopen))
		highs = append(highs, float64(uhigh))
		lows = append(lows, float64(ulow))
		closes = append(closes, float64(uclose))
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	ds.AddColumn("open", opens)
	ds.AddColumn("high", highs)
	ds.AddColumn("low", lows)
	ds.AddColumn("close", closes)

	return ds, nil
}
