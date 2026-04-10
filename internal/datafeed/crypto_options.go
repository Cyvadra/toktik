package datafeed

import (
	"context"
	"fmt"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
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
	"volume": "volume",
}

// allBarColumns lists every numeric column in the crypto_options_bar tables,
// in the order they appear in SELECT queries.
var allBarColumns = []string{
	"mark_open", "mark_high", "mark_low", "mark_close",
	"last_open", "last_high", "last_low", "last_close",
	"bid_open", "bid_high", "bid_low", "bid_close",
	"ask_open", "ask_high", "ask_low", "ask_close",
	"mark_iv_open", "mark_iv_close", "bid_iv_open", "ask_iv_open",
	"delta", "gamma", "vega", "theta", "rho",
	"underlying_price_open", "underlying_price_high", "underlying_price_low", "underlying_price_close",
	"volume", "open_interest", "tick_count",
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
	baseAsset := cryptooptions.ExtractBaseAsset(req.Symbol)
	interval := req.Interval

	barSourceSQL, err := cryptooptions.BuildOptionBarSubquery(interval)
	if err != nil {
		return nil, fmt.Errorf("unsupported interval %q: %w", interval, err)
	}
	spotSourceSQL, err := cryptooptions.BuildSpotBarSubquery(interval)
	if err != nil {
		return nil, fmt.Errorf("unsupported spot interval %q: %w", interval, err)
	}

	query := fmt.Sprintf(`SELECT
    b.timestamp, b.symbol_id, b.base_asset,
    b.mark_open, b.mark_high, b.mark_low, b.mark_close,
    b.last_open, b.last_high, b.last_low, b.last_close,
    b.bid_open, b.bid_high, b.bid_low, b.bid_close,
    b.ask_open, b.ask_high, b.ask_low, b.ask_close,
    b.mark_iv_open, b.mark_iv_close, b.bid_iv_open, b.ask_iv_open,
    b.delta, b.gamma, b.vega, b.theta, b.rho,
    ifNull(u.open, toFloat32(0))  AS underlying_price_open,
    ifNull(u.high, toFloat32(0))  AS underlying_price_high,
    ifNull(u.low, toFloat32(0))   AS underlying_price_low,
    ifNull(u.close, toFloat32(0)) AS underlying_price_close,
	b.volume, b.open_interest, b.tick_count
FROM (%s) AS b
LEFT JOIN (%s) AS u
    ON u.timestamp = b.timestamp AND u.symbol = b.base_asset
ORDER BY b.timestamp`, barSourceSQL, spotSourceSQL)

	rows, err := f.conn.Query(ctx, query,
		clickhouse.Named("symbol_id", symbolID),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(req.From)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(req.To)),
	)
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
			symbolID  uint64
			baseAsset string
			// Precomputed/ad-hoc views expose tick_count as UInt64 via sumMerge.
			markOpen, markHigh, markLow, markClose   float32
			lastOpen, lastHigh, lastLow, lastClose   float32
			bidOpen, bidHigh, bidLow, bidClose       float32
			askOpen, askHigh, askLow, askClose       float32
			markIVOpen, markIVClose                  float32
			bidIVOpen, askIVOpen                     float32
			delta, gamma, vega, theta, rho           float32
			underlyingPriceOpen, underlyingPriceHigh float32
			underlyingPriceLow, underlyingPriceClose float32
			volume                                   float64
			openInterest                             float32
			tickCount                                uint64
		)

		if err := rows.Scan(
			&ts, &symbolID, &baseAsset,
			&markOpen, &markHigh, &markLow, &markClose,
			&lastOpen, &lastHigh, &lastLow, &lastClose,
			&bidOpen, &bidHigh, &bidLow, &bidClose,
			&askOpen, &askHigh, &askLow, &askClose,
			&markIVOpen, &markIVClose, &bidIVOpen, &askIVOpen,
			&delta, &gamma, &vega, &theta, &rho,
			&underlyingPriceOpen, &underlyingPriceHigh, &underlyingPriceLow, &underlyingPriceClose,
			&volume, &openInterest, &tickCount,
		); err != nil {
			return nil, fmt.Errorf("scan bar: %w", err)
		}

		timestamps = append(timestamps, ts)

		vals := []float64{
			float64(markOpen), float64(markHigh), float64(markLow), float64(markClose),
			float64(lastOpen), float64(lastHigh), float64(lastLow), float64(lastClose),
			float64(bidOpen), float64(bidHigh), float64(bidLow), float64(bidClose),
			float64(askOpen), float64(askHigh), float64(askLow), float64(askClose),
			float64(markIVOpen), float64(markIVClose), float64(bidIVOpen), float64(askIVOpen),
			float64(delta), float64(gamma), float64(vega), float64(theta), float64(rho),
			float64(underlyingPriceOpen), float64(underlyingPriceHigh), float64(underlyingPriceLow), float64(underlyingPriceClose),
			volume, float64(openInterest), float64(tickCount),
		}
		for i, v := range vals {
			colData[i] = append(colData[i], v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bar rows: %w", err)
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
