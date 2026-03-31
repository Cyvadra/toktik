package datafeed

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/backtest"
)

// CryptoUnderlyingDataFeed implements backtest.DataFeed using the dedicated
// standalone spot-like bar table for option underlyings.
type CryptoUnderlyingDataFeed struct {
	conn driver.Conn
}

// NewCryptoUnderlyingDataFeed creates a DataFeed for underlying price data.
func NewCryptoUnderlyingDataFeed(conn driver.Conn) *CryptoUnderlyingDataFeed {
	return &CryptoUnderlyingDataFeed{conn: conn}
}

// Fields returns the list of available fields.
func (f *CryptoUnderlyingDataFeed) Fields() []string {
	return []string{"open", "high", "low", "close", "tick_count", "volume", "compat_fallback"}
}

// Load fetches the underlying asset's OHLC from options data.
// The req.Symbol should be the base asset name (e.g. "BTC").
func (f *CryptoUnderlyingDataFeed) Load(ctx context.Context, req backtest.DataRequest) (*backtest.DataSet, error) {
	baseAsset := req.Symbol
	interval := req.Interval

	query, degraded, err := buildSpotSourceSQLWithFallback(ctx, f.conn, interval, baseAsset, req.From, req.To)
	hasNativeVolume := query != ""
	if err != nil {
		return nil, fmt.Errorf("resolve underlying source for %s: %w", baseAsset, err)
	}
	if query != "" {
		// Spot source returns 8 columns; project down to the 6 the scan expects.
		query = fmt.Sprintf(`SELECT timestamp, open, close, high, low, tick_count FROM (%s) ORDER BY timestamp`, query)
	} else {
		query, degraded, err = buildLegacyUnderlyingSeriesSQL(ctx, f.conn, interval, baseAsset, req.From, req.To)
		if err != nil {
			return nil, fmt.Errorf("resolve legacy underlying source for %s: %w", baseAsset, err)
		}
		if query == "" {
			return nil, fmt.Errorf("load underlying for %s: no compatible spot or legacy underlying source found", baseAsset)
		}
	}

	if degraded {
		log.Printf("[compat] underlying feed for %s/%s is using a compatibility fallback source", baseAsset, interval)
	}

	rows, err := f.conn.Query(ctx, query,
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("base_asset", baseAsset),
		clickhouse.Named("from", backtestTimeParam(req.From)),
		clickhouse.Named("to", backtestTimeParam(req.To)),
	)
	if err != nil {
		return nil, fmt.Errorf("load underlying for %s: %w", baseAsset, err)
	}
	defer rows.Close()

	timestamps := make([]time.Time, 0, 4096)
	opens := make([]float64, 0, 4096)
	highs := make([]float64, 0, 4096)
	lows := make([]float64, 0, 4096)
	closes := make([]float64, 0, 4096)
	tickCounts := make([]float64, 0, 4096)
	fallbackMode := make([]float64, 0, 4096)
	fallbackValue := 0.0
	if degraded {
		fallbackValue = 1.0
	}

	for rows.Next() {
		var (
			ts                         time.Time
			uopen, uclose, uhigh, ulow float32
			tickCount                  uint64
		)
		if hasNativeVolume {
			if err := rows.Scan(&ts, &uopen, &uclose, &uhigh, &ulow, &tickCount); err != nil {
				return nil, fmt.Errorf("scan underlying row with volume: %w", err)
			}
		} else {
			if err := rows.Scan(&ts, &uopen, &uclose, &uhigh, &ulow); err != nil {
				return nil, fmt.Errorf("scan underlying row: %w", err)
			}
			tickCount = 0
		}
		timestamps = append(timestamps, ts)
		opens = append(opens, float64(uopen))
		highs = append(highs, float64(uhigh))
		lows = append(lows, float64(ulow))
		closes = append(closes, float64(uclose))
		if hasNativeVolume {
			tickCounts = append(tickCounts, float64(tickCount))
		} else {
			tickCounts = append(tickCounts, math.NaN())
		}
		fallbackMode = append(fallbackMode, fallbackValue)
	}

	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	for name, col := range map[string][]float64{
		"open":           opens,
		"high":           highs,
		"low":            lows,
		"close":          closes,
		"tick_count":     tickCounts,
		"volume":         tickCounts,
		"compat_fallback": fallbackMode,
	} {
		if err := ds.AddColumn(name, col); err != nil {
			return nil, fmt.Errorf("build underlying dataset: %w", err)
		}
	}

	return ds, nil
}

func backtestTimeParam(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}
