package service

import (
	"context"
	"fmt"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	defaultUSBarLimit   = 1000
	maxUSBarLimit       = 10000
	defaultUSChainLimit = 500
	maxUSChainLimit     = 5000
)

// USMarketService provides US stock and option market data queries backed by ClickHouse.
type USMarketService struct {
	conn driver.Conn
}

// NewUSMarketService creates a USMarketService backed by the provided ClickHouse connection.
func NewUSMarketService(conn driver.Conn) *USMarketService {
	return &USMarketService{conn: conn}
}

// QueryUSStockBars returns OHLCV bars for a US stock symbol.
func (s *USMarketService) QueryUSStockBars(ctx context.Context, req dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	limit := clamp(req.Limit, defaultUSBarLimit, maxUSBarLimit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	tableName, err := resolveUSStockTable(req.Interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", req.Interval)
	}

	query := fmt.Sprintf(`SELECT
    timestamp, symbol, open, high, low, close, volume, transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT %d`, tableName, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", fromT.Format(time.RFC3339)),
		clickhouse.Named("to", toT.Format(time.RFC3339)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US stock bars: %w", err)
	}
	defer rows.Close()

	var bars []dto.USStockBarRow
	for rows.Next() {
		var r dto.USStockBarRow
		if err := rows.Scan(
			&r.Timestamp, &r.Symbol,
			&r.Open, &r.High, &r.Low, &r.Close,
			&r.Volume, &r.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US stock bar row: %w", err)
		}
		bars = append(bars, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock bar rows: %w", err)
	}

	resp := &dto.USStockBarResponse{}
	if len(bars) > limit {
		resp.NextCursor = encodeCursor(bars[limit-1].Timestamp)
		resp.Data = bars[:limit]
	} else {
		resp.Data = bars
	}
	return resp, nil
}

// QueryUSOptionBars returns OHLCV + greeks bars for a single US option contract.
func (s *USMarketService) QueryUSOptionBars(ctx context.Context, req dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	limit := clamp(req.Limit, defaultUSBarLimit, maxUSBarLimit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	tableName, err := resolveUSOptionTable(req.Interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", req.Interval)
	}

	query := fmt.Sprintf(`SELECT
    timestamp, symbol, underlying, option_type, expiration, strike,
    open, high, low, close,
    underlying_close, implied_volatility,
    delta, gamma, vega, theta, rho,
    volume, transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT %d`, tableName, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", fromT.Format(time.RFC3339)),
		clickhouse.Named("to", toT.Format(time.RFC3339)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US option bars: %w", err)
	}
	defer rows.Close()

	var bars []dto.USOptionBarRow
	for rows.Next() {
		var r dto.USOptionBarRow
		if err := rows.Scan(
			&r.Timestamp, &r.Symbol, &r.Underlying, &r.OptionType, &r.Expiration, &r.Strike,
			&r.Open, &r.High, &r.Low, &r.Close,
			&r.UnderlyingClose, &r.ImpliedVolatility,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.Volume, &r.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US option bar row: %w", err)
		}
		bars = append(bars, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option bar rows: %w", err)
	}

	resp := &dto.USOptionBarResponse{}
	if len(bars) > limit {
		resp.NextCursor = encodeCursor(bars[limit-1].Timestamp)
		resp.Data = bars[:limit]
	} else {
		resp.Data = bars
	}
	return resp, nil
}

// QueryUSOptionChain returns timestamped option chain snapshots for an underlying.
func (s *USMarketService) QueryUSOptionChain(ctx context.Context, req dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	limit := clamp(req.Limit, defaultUSChainLimit, maxUSChainLimit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	chainView, ok := usmarket.ChainPrecomputedIntervals[req.Interval]
	if !ok {
		return nil, dto.NewValidationError("unsupported chain interval %q (supported: 1m,5m,15m,30m,1h,2h,4h,1d)", req.Interval)
	}

	query := fmt.Sprintf(`SELECT
    timestamp, underlying,
    symbols, option_types, expirations, strikes, close_prices,
    underlying_closes, implied_volatilities,
    deltas, gammas, vegas, thetas, rhos,
    volumes, transactions
FROM %s
WHERE underlying = {underlying:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT %d`, chainView, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("underlying", req.Underlying),
		clickhouse.Named("from", fromT.Format(time.RFC3339)),
		clickhouse.Named("to", toT.Format(time.RFC3339)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US option chain: %w", err)
	}
	defer rows.Close()

	var chains []dto.USOptionChainRow
	for rows.Next() {
		var r dto.USOptionChainRow
		if err := rows.Scan(
			&r.Timestamp, &r.Underlying,
			&r.Symbols, &r.OptionTypes, &r.Expirations, &r.Strikes, &r.ClosePrices,
			&r.UnderlyingCloses, &r.ImpliedVolatilities,
			&r.Deltas, &r.Gammas, &r.Vegas, &r.Thetas, &r.Rhos,
			&r.Volumes, &r.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US option chain row: %w", err)
		}
		chains = append(chains, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option chain rows: %w", err)
	}

	resp := &dto.USOptionChainResponse{}
	if len(chains) > limit {
		resp.NextCursor = encodeCursor(chains[limit-1].Timestamp)
		resp.Data = chains[:limit]
	} else {
		resp.Data = chains
	}
	return resp, nil
}

// --- helpers ---

var usStockViews = map[string]string{
	"5m":  "us_stocks_bar_5m",
	"15m": "us_stocks_bar_15m",
	"30m": "us_stocks_bar_30m",
	"1h":  "us_stocks_bar_1h",
	"2h":  "us_stocks_bar_2h",
	"4h":  "us_stocks_bar_4h",
	"1d":  "us_stocks_bar_1d",
}

var usOptionViews = map[string]string{
	"5m":  "us_options_bar_5m",
	"15m": "us_options_bar_15m",
	"30m": "us_options_bar_30m",
	"1h":  "us_options_bar_1h",
	"2h":  "us_options_bar_2h",
	"4h":  "us_options_bar_4h",
	"1d":  "us_options_bar_1d",
}

func resolveUSStockTable(interval string) (string, error) {
	if interval == "1m" {
		return "us_stocks_bar_1m", nil
	}
	if t, ok := usStockViews[interval]; ok {
		return t, nil
	}
	return "", fmt.Errorf("unsupported US stock interval %q", interval)
}

func resolveUSOptionTable(interval string) (string, error) {
	if interval == "1m" {
		return "us_options_bar_1m", nil
	}
	if t, ok := usOptionViews[interval]; ok {
		return t, nil
	}
	return "", fmt.Errorf("unsupported US option interval %q", interval)
}
