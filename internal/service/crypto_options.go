package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
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
	baseAsset := cryptooptions.ExtractBaseAsset(req.Symbol)

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

	interval := req.Interval

	barSourceSQL, err := cryptooptions.BuildOptionBarSubquery(interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}
	spotSourceSQL, err := cryptooptions.BuildSpotBarSubquery(interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
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
    b.open_interest,
    toUInt16(b.tick_count) AS tick_count
FROM (%s) AS b
LEFT JOIN (%s) AS u
    ON u.timestamp = b.timestamp AND u.symbol = b.base_asset
ORDER BY b.timestamp
LIMIT %d`, barSourceSQL, spotSourceSQL, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol_id", symbolID),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", fromT),
		clickhouse.Named("to", toT),
	)
	if err != nil {
		return nil, fmt.Errorf("query bars: %w", err)
	}
	defer rows.Close()

	bars, err := scanBarRows(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bar rows: %w", err)
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
	var args []interface{}

	if req.BaseAsset != "" {
		conditions = append(conditions, "base_asset = {base_asset:String}")
		args = append(args, clickhouse.Named("base_asset", req.BaseAsset))
	}
	if req.Search != "" {
		conditions = append(conditions, "symbol ILIKE {search:String}")
		args = append(args, clickhouse.Named("search", "%"+req.Search+"%"))
	}
	if req.Cursor != "" {
		cursorID, err := decodeCursorUint32(req.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		conditions = append(conditions, "symbol_id > {cursor_id:UInt32}")
		args = append(args, clickhouse.Named("cursor_id", cursorID))
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

	rows, err := s.conn.Query(ctx, query, args...)
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbol rows: %w", err)
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
	baseAsset := cryptooptions.ExtractBaseAsset(req.Symbol)
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

	barSourceSQL, err := cryptooptions.BuildOptionBarSubquery(interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}
	spotSourceSQL, err := cryptooptions.BuildSpotBarSubquery(interval)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}

	query := fmt.Sprintf(`SELECT
    b.timestamp, b.symbol_id,
    b.delta, b.gamma, b.vega, b.theta, b.rho,
    b.mark_iv_open, b.mark_iv_close,
    ifNull(u.open, toFloat32(0))  AS underlying_price_open,
    ifNull(u.high, toFloat32(0))  AS underlying_price_high,
    ifNull(u.low, toFloat32(0))   AS underlying_price_low,
    ifNull(u.close, toFloat32(0)) AS underlying_price_close,
    b.open_interest
FROM (%s) AS b
LEFT JOIN (%s) AS u
    ON u.timestamp = b.timestamp AND u.symbol = b.base_asset
ORDER BY b.timestamp
LIMIT %d`, barSourceSQL, spotSourceSQL, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol_id", symbolID),
		clickhouse.Named("symbol", baseAsset),
		clickhouse.Named("from", fromT),
		clickhouse.Named("to", toT),
	)
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
			&r.UnderlyingPriceOpen, &r.UnderlyingPriceHigh, &r.UnderlyingPriceLow, &r.UnderlyingPriceClose,
			&r.OpenInterest,
		); err != nil {
			return nil, fmt.Errorf("scan greeks row: %w", err)
		}
		greeks = append(greeks, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate greeks rows: %w", err)
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
			&r.BidOpen, &r.BidHigh, &r.BidLow, &r.BidClose,
			&r.AskOpen, &r.AskHigh, &r.AskLow, &r.AskClose,
			&r.MarkIVOpen, &r.MarkIVClose, &r.BidIVOpen, &r.AskIVOpen,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.UnderlyingPriceOpen, &r.UnderlyingPriceHigh, &r.UnderlyingPriceLow, &r.UnderlyingPriceClose,
			&r.OpenInterest, &r.TickCount,
		); err != nil {
			return nil, fmt.Errorf("scan bar row: %w", err)
		}
		bars = append(bars, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bar rows: %w", err)
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
