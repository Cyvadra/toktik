package datafeed

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// CryptoOptionsFieldAliases maps canonical OHLCV names to crypto-options-specific fields.
var CryptoOptionsFieldAliases = map[string]string{
	"open":   "mark_open",
	"high":   "mark_high",
	"low":    "mark_low",
	"close":  "mark_close",
	"volume": "tick_count",
}

// allBarColumns lists every numeric column in the crypto_options_bar tables,
// in the order they appear in SELECT queries.
var allBarColumns = []string{
	"mark_open", "mark_high", "mark_low", "mark_close",
	"last_open", "last_high", "last_low", "last_close",
	"bid_open", "bid_close", "ask_open", "ask_close",
	"mark_iv_open", "mark_iv_close", "bid_iv_open", "ask_iv_open",
	"delta", "gamma", "vega", "theta", "rho",
	"underlying_price_open", "underlying_price_close",
	"open_interest", "tick_count",
}

// CryptoOptionsDataFeed implements backtest.DataFeed for crypto options data
// stored in ClickHouse.
type CryptoOptionsDataFeed struct {
	conn driver.Conn
}

// NewCryptoOptionsDataFeed creates a DataFeed backed by ClickHouse.
func NewCryptoOptionsDataFeed(conn driver.Conn) *CryptoOptionsDataFeed {
	return &CryptoOptionsDataFeed{conn: conn}
}

// Fields returns all available field names including canonical aliases.
func (f *CryptoOptionsDataFeed) Fields() []string {
	fields := make([]string, 0, len(allBarColumns)+len(CryptoOptionsFieldAliases))
	fields = append(fields, allBarColumns...)
	for alias := range CryptoOptionsFieldAliases {
		fields = append(fields, alias)
	}
	return fields
}

// Load fetches bar data from ClickHouse into a columnar DataSet.
func (f *CryptoOptionsDataFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	symbolID := cryptooptions.SymbolID(req.Symbol)
	interval := req.Interval

	selectCols := `timestamp, symbol_id, base_asset,
    mark_open, mark_high, mark_low, mark_close,
    last_open, last_high, last_low, last_close,
    bid_open, bid_close, ask_open, ask_close,
    mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
    delta, gamma, vega, theta, rho,
    underlying_price_open, underlying_price_close,
    open_interest, tick_count`

	var query string

	if interval == "1m" {
		query = fmt.Sprintf(`SELECT %s
FROM crypto_options_bar_1m
WHERE symbol_id = %d
  AND timestamp >= '%s'
  AND timestamp < '%s'
ORDER BY timestamp`,
			selectCols,
			symbolID,
			req.From.Format("2006-01-02 15:04:05"),
			req.To.Format("2006-01-02 15:04:05"),
		)
	} else if viewName, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
		query = fmt.Sprintf(`SELECT %s
FROM %s
WHERE symbol_id = %d
  AND timestamp >= '%s'
  AND timestamp < '%s'
ORDER BY timestamp`,
			selectCols, viewName,
			symbolID,
			req.From.Format("2006-01-02 15:04:05"),
			req.To.Format("2006-01-02 15:04:05"),
		)
	} else {
		adhocSQL, err := cryptooptions.QueryTimeAggregationSQL(interval)
		if err != nil {
			return nil, fmt.Errorf("unsupported interval %q: %w", interval, err)
		}
		query = replaceNamedParams(adhocSQL, symbolID, req.From, req.To)
	}

	rows, err := f.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query bars for %s/%s: %w", req.Symbol, interval, err)
	}
	defer rows.Close()

	// Pre-allocate slices
	timestamps := make([]time.Time, 0, 4096)
	colData := make([][]float64, len(allBarColumns))
	for i := range colData {
		colData[i] = make([]float64, 0, 4096)
	}

	for rows.Next() {
		var (
			ts        time.Time
			symbolID  uint32
			baseAsset string
			// Precomputed/ad-hoc views expose tick_count as UInt64 via sumMerge.
			markOpen, markHigh, markLow, markClose    float32
			lastOpen, lastHigh, lastLow, lastClose    float32
			bidOpen, bidClose, askOpen, askClose      float32
			markIVOpen, markIVClose                   float32
			bidIVOpen, askIVOpen                      float32
			delta, gamma, vega, theta, rho            float32
			underlyingPriceOpen, underlyingPriceClose float32
			openInterest                              float32
			tickCount                                 uint64
		)

		if err := rows.Scan(
			&ts, &symbolID, &baseAsset,
			&markOpen, &markHigh, &markLow, &markClose,
			&lastOpen, &lastHigh, &lastLow, &lastClose,
			&bidOpen, &bidClose, &askOpen, &askClose,
			&markIVOpen, &markIVClose, &bidIVOpen, &askIVOpen,
			&delta, &gamma, &vega, &theta, &rho,
			&underlyingPriceOpen, &underlyingPriceClose,
			&openInterest, &tickCount,
		); err != nil {
			return nil, fmt.Errorf("scan bar: %w", err)
		}

		timestamps = append(timestamps, ts)

		vals := []float64{
			float64(markOpen), float64(markHigh), float64(markLow), float64(markClose),
			float64(lastOpen), float64(lastHigh), float64(lastLow), float64(lastClose),
			float64(bidOpen), float64(bidClose), float64(askOpen), float64(askClose),
			float64(markIVOpen), float64(markIVClose), float64(bidIVOpen), float64(askIVOpen),
			float64(delta), float64(gamma), float64(vega), float64(theta), float64(rho),
			float64(underlyingPriceOpen), float64(underlyingPriceClose),
			float64(openInterest), float64(tickCount),
		}
		for i, v := range vals {
			colData[i] = append(colData[i], v)
		}
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)

	for i, name := range allBarColumns {
		ds.AddColumn(name, colData[i])
	}

	// Add canonical aliases
	for alias, target := range CryptoOptionsFieldAliases {
		if col := ds.Column(target); col != nil {
			ds.AddColumn(alias, col)
		}
	}

	return ds, nil
}

// replaceNamedParams replaces ClickHouse named parameters in ad-hoc aggregation SQL.
func replaceNamedParams(sql string, symbolID uint32, from, to time.Time) string {
	result := sql
	// Replace named parameters with literal values
	result = replaceParam(result, "{symbol_id:UInt32}", fmt.Sprintf("%d", symbolID))
	result = replaceParam(result, "{from:DateTime}", "'"+from.Format("2006-01-02 15:04:05")+"'")
	result = replaceParam(result, "{to:DateTime}", "'"+to.Format("2006-01-02 15:04:05")+"'")
	return result
}

func replaceParam(s, old, new string) string {
	for {
		idx := indexOf(s, old)
		if idx < 0 {
			return s
		}
		s = s[:idx] + new + s[idx+len(old):]
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
