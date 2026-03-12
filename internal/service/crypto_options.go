package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	defaultBarLimit    = 1000
	maxBarLimit        = 10000
	defaultSymbolLimit = 100
	maxSymbolLimit     = 1000
)

// CryptoOptionsService provides market data queries backed by ClickHouse.
type CryptoOptionsService struct {
	conn driver.Conn
}

func NewCryptoOptionsService(conn driver.Conn) *CryptoOptionsService {
	return &CryptoOptionsService{conn: conn}
}

// QueryBars returns OHLCV bars for a symbol, time range, and interval.
func (s *CryptoOptionsService) QueryBars(ctx context.Context, req dto.BarRequest) (*dto.BarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	limit := clamp(req.Limit, defaultBarLimit, maxBarLimit)
	symbolID := cryptooptions.SymbolID(req.Symbol)

	// Apply cursor: the cursor is the RFC3339 timestamp of the last row seen.
	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	var query string
	interval := req.Interval

	// For 1m, query the base table directly.
	if interval == "1m" {
		query = fmt.Sprintf(`SELECT
    timestamp, symbol_id, base_asset,
    mark_open, mark_high, mark_low, mark_close,
    last_open, last_high, last_low, last_close,
    bid_open, bid_close, ask_open, ask_close,
    mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
    delta, gamma, vega, theta, rho,
    underlying_price_open, underlying_price_close,
    open_interest, tick_count
FROM crypto_options_bar_1m
WHERE symbol_id = %d
  AND timestamp >= '%s'
  AND timestamp < '%s'
ORDER BY timestamp
LIMIT %d`,
			symbolID,
			fromT.Format("2006-01-02 15:04:05"),
			toT.Format("2006-01-02 15:04:05"),
			limit+1, // fetch one extra to determine next cursor
		)
	} else if viewName, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
		// Use pre-computed materialized view.
		query = fmt.Sprintf(`SELECT
    timestamp, symbol_id, base_asset,
    mark_open, mark_high, mark_low, mark_close,
    last_open, last_high, last_low, last_close,
    bid_open, bid_close, ask_open, ask_close,
    mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
    delta, gamma, vega, theta, rho,
    underlying_price_open, underlying_price_close,
    open_interest, tick_count
FROM %s
WHERE symbol_id = %d
  AND timestamp >= '%s'
  AND timestamp < '%s'
ORDER BY timestamp
LIMIT %d`,
			viewName,
			symbolID,
			fromT.Format("2006-01-02 15:04:05"),
			toT.Format("2006-01-02 15:04:05"),
			limit+1,
		)
	} else {
		// Ad-hoc interval: use query-time aggregation.
		adhocSQL, err := cryptooptions.QueryTimeAggregationSQL(interval)
		if err != nil {
			return nil, err
		}
		// QueryTimeAggregationSQL uses named parameters; we replace them.
		query = replaceNamedParams(adhocSQL, symbolID, fromT, toT)
		query += fmt.Sprintf("\nLIMIT %d", limit+1)
	}

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query bars: %w", err)
	}
	defer rows.Close()

	bars, err := scanBarRows(rows)
	if err != nil {
		return nil, err
	}

	resp := &dto.BarResponse{}
	if len(bars) > limit {
		resp.NextCursor = encodeCursor(bars[limit-1].Timestamp)
		resp.Data = bars[:limit]
	} else {
		resp.Data = bars
	}
	return resp, nil
}

// QuerySymbols returns symbol metadata with optional search/filter.
func (s *CryptoOptionsService) QuerySymbols(ctx context.Context, req dto.SymbolRequest) (*dto.SymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)

	query := `SELECT symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
FROM crypto_options_symbol_meta FINAL`

	var conditions []string

	if req.BaseAsset != "" {
		conditions = append(conditions, fmt.Sprintf("base_asset = '%s'", escapeSingleQuote(req.BaseAsset)))
	}
	if req.Search != "" {
		conditions = append(conditions, fmt.Sprintf("symbol ILIKE '%%%s%%'", escapeSingleQuote(req.Search)))
	}
	if req.Cursor != "" {
		cursorID, err := decodeCursorUint32(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf("symbol_id > %d", cursorID))
	}

	if len(conditions) > 0 {
		query += " WHERE "
		for i, c := range conditions {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}

	query += fmt.Sprintf(" ORDER BY symbol_id LIMIT %d", limit+1)

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query symbols: %w", err)
	}
	defer rows.Close()

	var symbols []dto.SymbolRow
	for rows.Next() {
		var r dto.SymbolRow
		if err := rows.Scan(
			&r.SymbolID, &r.Symbol, &r.BaseAsset, &r.OptionType,
			&r.StrikePrice, &r.Expiration, &r.UnderlyingIndex,
		); err != nil {
			return nil, fmt.Errorf("scan symbol row: %w", err)
		}
		symbols = append(symbols, r)
	}

	resp := &dto.SymbolResponse{}
	if len(symbols) > limit {
		resp.NextCursor = encodeCursorUint32(symbols[limit-1].SymbolID)
		resp.Data = symbols[:limit]
	} else {
		resp.Data = symbols
	}
	return resp, nil
}

// QueryGreeks returns greeks time series for a symbol.
func (s *CryptoOptionsService) QueryGreeks(ctx context.Context, req dto.GreeksRequest) (*dto.GreeksResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	limit := clamp(req.Limit, defaultBarLimit, maxBarLimit)
	symbolID := cryptooptions.SymbolID(req.Symbol)
	interval := req.Interval
	if interval == "" {
		interval = "1m"
	}

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	table := "crypto_options_bar_1m"
	if interval != "1m" {
		if viewName, ok := cryptooptions.PrecomputedIntervals[interval]; ok {
			table = viewName
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp, symbol_id,
    delta, gamma, vega, theta, rho,
    mark_iv_open, mark_iv_close,
    underlying_price_open, underlying_price_close,
    open_interest
FROM %s
WHERE symbol_id = %d
  AND timestamp >= '%s'
  AND timestamp < '%s'
ORDER BY timestamp
LIMIT %d`,
		table,
		symbolID,
		fromT.Format("2006-01-02 15:04:05"),
		toT.Format("2006-01-02 15:04:05"),
		limit+1,
	)

	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query greeks: %w", err)
	}
	defer rows.Close()

	var greeks []dto.GreeksRow
	for rows.Next() {
		var r dto.GreeksRow
		if err := rows.Scan(
			&r.Timestamp, &r.SymbolID,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.MarkIVOpen, &r.MarkIVClose,
			&r.UnderlyingPriceOpen, &r.UnderlyingPriceClose,
			&r.OpenInterest,
		); err != nil {
			return nil, fmt.Errorf("scan greeks row: %w", err)
		}
		greeks = append(greeks, r)
	}

	resp := &dto.GreeksResponse{}
	if len(greeks) > limit {
		resp.NextCursor = encodeCursor(greeks[limit-1].Timestamp)
		resp.Data = greeks[:limit]
	} else {
		resp.Data = greeks
	}
	return resp, nil
}

// --- helpers ---

func scanBarRows(rows driver.Rows) ([]dto.BarRow, error) {
	var bars []dto.BarRow
	for rows.Next() {
		var r dto.BarRow
		if err := rows.Scan(
			&r.Timestamp, &r.SymbolID, &r.BaseAsset,
			&r.MarkOpen, &r.MarkHigh, &r.MarkLow, &r.MarkClose,
			&r.LastOpen, &r.LastHigh, &r.LastLow, &r.LastClose,
			&r.BidOpen, &r.BidClose, &r.AskOpen, &r.AskClose,
			&r.MarkIVOpen, &r.MarkIVClose, &r.BidIVOpen, &r.AskIVOpen,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.UnderlyingPriceOpen, &r.UnderlyingPriceClose,
			&r.OpenInterest, &r.TickCount,
		); err != nil {
			return nil, fmt.Errorf("scan bar row: %w", err)
		}
		bars = append(bars, r)
	}
	return bars, nil
}

func clamp(val, defaultVal, maxVal int) int {
	if val <= 0 {
		return defaultVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func encodeCursor(t time.Time) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.Format(time.RFC3339)))
}

func decodeCursor(cursor string) (time.Time, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, string(b))
}

func encodeCursorUint32(id uint32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(id), 10)))
}

func decodeCursorUint32(cursor string) (uint32, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(string(b), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

// escapeSingleQuote escapes single quotes in string values used in SQL.
// This prevents SQL injection for string literals.
func escapeSingleQuote(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			result = append(result, '\'', '\'')
		} else {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// replaceNamedParams replaces ClickHouse named parameters with literal values.
func replaceNamedParams(sql string, symbolID uint32, from, to time.Time) string {
	sql = replaceParam(sql, "{symbol_id:UInt32}", strconv.FormatUint(uint64(symbolID), 10))
	sql = replaceParam(sql, "{from:DateTime}", "'"+from.Format("2006-01-02 15:04:05")+"'")
	sql = replaceParam(sql, "{to:DateTime}", "'"+to.Format("2006-01-02 15:04:05")+"'")
	return sql
}

func replaceParam(sql, param, value string) string {
	for {
		i := indexOf(sql, param)
		if i < 0 {
			return sql
		}
		sql = sql[:i] + value + sql[i+len(param):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
