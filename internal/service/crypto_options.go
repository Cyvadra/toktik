package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
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
	repo *chrepo.Repo
}

func NewCryptoOptionsService(repo *chrepo.Repo) *CryptoOptionsService {
	return &CryptoOptionsService{repo: repo}
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
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = nextCursorTime(cursorTime)
		}
	}

	interval := req.Interval

	barSourceSQL, err := chquery.BuildOptionBarSubquery(interval, symbolID, fromT, toT)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}
	spotSourceSQL, err := chquery.BuildSpotBarSubquery(interval, baseAsset, fromT, toT)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}

	query := chquery.CryptoOptionsBarsWithUnderlyingSQL(barSourceSQL, spotSourceSQL, limit+1)

	rows, err := s.repo.Query(ctx, query)
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

	resp := &dto.BarResponse{Data: make([]dto.BarRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(bars, limit, func(r dto.BarRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

// QuerySymbols returns symbol metadata with optional search/filter.
func (s *CryptoOptionsService) QuerySymbols(ctx context.Context, req dto.SymbolRequest) (*dto.SymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)

	query := `SELECT symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
FROM crypto_options_symbol_meta FINAL`

	var conditions []string

	if req.BaseAsset != "" {
		conditions = append(conditions, fmt.Sprintf("base_asset = %s", clickhouseStringLiteral(req.BaseAsset)))
	}
	if req.Search != "" {
		conditions = append(conditions, fmt.Sprintf("symbol ILIKE %s", clickhouseStringLiteral("%"+req.Search+"%")))
	}
	if req.Cursor != "" {
		cursorID, err := decodeCursorUint64(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		conditions = append(conditions, fmt.Sprintf("symbol_id > %s", chquery.UInt64Literal(cursorID)))
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

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.SymbolRow, 0, limit)
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

	resp := &dto.SymbolResponse{Data: make([]dto.SymbolRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(symbols, limit, func(r dto.SymbolRow) string {
		return encodeCursorUint64(r.SymbolID)
	})
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
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = nextCursorTime(cursorTime)
		}
	}

	barSourceSQL, err := chquery.BuildOptionBarSubquery(interval, symbolID, fromT, toT)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}
	spotSourceSQL, err := chquery.BuildSpotBarSubquery(interval, baseAsset, fromT, toT)
	if err != nil {
		return nil, dto.NewValidationError("unsupported interval %q", interval)
	}

	query := chquery.CryptoOptionsGreeksSQL(barSourceSQL, spotSourceSQL, limit+1)

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query greeks: %w", err)
	}
	defer rows.Close()

	greeks := make([]dto.GreeksRow, 0, limit)
	for rows.Next() {
		var r dto.GreeksRow
		if err := rows.Scan(
			&r.Timestamp, &r.SymbolID,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.ImpliedVolatility,
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

	resp := &dto.GreeksResponse{Data: make([]dto.GreeksRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(greeks, limit, func(r dto.GreeksRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

// --- helpers ---

func scanBarRows(rows driver.Rows) ([]dto.BarRow, error) {
	bars := make([]dto.BarRow, 0)
	for rows.Next() {
		var r dto.BarRow
		if err := rows.Scan(
			&r.Timestamp, &r.SymbolID, &r.BaseAsset,
			&r.MarkOpen, &r.MarkHigh, &r.MarkLow, &r.MarkClose,
			&r.LastOpen, &r.LastHigh, &r.LastLow, &r.LastClose,
			&r.BidOpen, &r.BidHigh, &r.BidLow, &r.BidClose,
			&r.AskOpen, &r.AskHigh, &r.AskLow, &r.AskClose,
			&r.ImpliedVolatility,
			&r.MarkIVOpen, &r.MarkIVClose, &r.BidIVOpen, &r.AskIVOpen,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta, &r.Rho,
			&r.UnderlyingPriceOpen, &r.UnderlyingPriceHigh, &r.UnderlyingPriceLow, &r.UnderlyingPriceClose,
			&r.Volume, &r.OpenInterest, &r.TickCount,
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

func nextCursorTime(cursor time.Time) time.Time {
	return cursor.UTC().Add(time.Second)
}

func encodeCursorUint64(id uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(id), 10)))
}

func decodeCursorUint64(cursor string) (uint64, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// QueryChain returns crypto option chain snapshots for a base asset over a time range.
func (s *CryptoOptionsService) QueryChain(ctx context.Context, req dto.CryptoOptionChainRequest) (*dto.CryptoOptionChainResponse, error) {
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("invalid time range: %v", err)}
	}
	limit := clamp(req.Limit, defaultBarLimit, maxBarLimit)

	interval := req.Interval
	if interval == "" {
		interval = "1d"
	}
	chainView, ok := cryptooptions.ChainPrecomputedIntervals[interval]
	if !ok {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("unsupported chain interval %q", interval)}
	}

	if req.Cursor != "" {
		cursorTime, cerr := decodeCursor(req.Cursor)
		if cerr != nil {
			return nil, invalidCursorError(cerr)
		}
		from = nextCursorTime(cursorTime)
	}

	spotTable := fmt.Sprintf("crypto_spot_bar_%s", interval)
	query := chquery.CryptoOptionsChainSQL(chainView, spotTable, req.BaseAsset, from, to, limit+1)

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query crypto option chain: %w", err)
	}
	defer rows.Close()

	type rawRow struct {
		timestamp       time.Time
		symbolID        uint64
		symbol          string
		optionType      string
		expiration      time.Time
		strike          float32
		markClose       float32
		bidClose        float32
		askClose        float32
		markIV          float32
		delta           float32
		gamma           float32
		vega            float32
		theta           float32
		rho             float32
		volume          float64
		openInterest    float32
		tickCount       uint16
		underlyingClose float32
	}
	var allRows []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(
			&r.timestamp, &r.symbolID, &r.symbol, &r.optionType,
			&r.expiration, &r.strike,
			&r.markClose, &r.bidClose, &r.askClose,
			&r.markIV, &r.delta, &r.gamma, &r.vega, &r.theta, &r.rho,
			&r.volume, &r.openInterest, &r.tickCount, &r.underlyingClose,
		); err != nil {
			return nil, fmt.Errorf("scan chain row: %w", err)
		}
		allRows = append(allRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chain rows: %w", err)
	}

	// Group by timestamp into snapshots
	snapshots := make([]dto.CryptoOptionChainSnapshot, 0)
	var cur *dto.CryptoOptionChainSnapshot
	for _, r := range allRows {
		if cur == nil || !cur.Timestamp.Equal(r.timestamp) {
			if cur != nil {
				snapshots = append(snapshots, *cur)
			}
			cur = &dto.CryptoOptionChainSnapshot{
				Timestamp: r.timestamp,
				BaseAsset: req.BaseAsset,
				Contracts: make([]dto.CryptoOptionChainContract, 0),
			}
		}
		cur.Contracts = append(cur.Contracts, dto.CryptoOptionChainContract{
			SymbolID:        r.symbolID,
			Symbol:          r.symbol,
			OptionType:      r.optionType,
			Expiration:      r.expiration,
			Strike:          r.strike,
			MarkClose:       r.markClose,
			BidClose:        r.bidClose,
			AskClose:        r.askClose,
			MarkIV:          r.markIV,
			Delta:           r.delta,
			Gamma:           r.gamma,
			Vega:            r.vega,
			Theta:           r.theta,
			Rho:             r.rho,
			Volume:          r.volume,
			OpenInterest:    r.openInterest,
			TickCount:       r.tickCount,
			UnderlyingClose: r.underlyingClose,
		})
	}
	if cur != nil {
		snapshots = append(snapshots, *cur)
	}

	resp := &dto.CryptoOptionChainResponse{Data: make([]dto.CryptoOptionChainSnapshot, 0)}
	if len(allRows) > limit {
		// Trim last snapshot if it exceeds the limit
		resp.Data = snapshots
		lastTs := allRows[limit-1].timestamp
		resp.NextCursor = encodeCursor(lastTs)
	} else {
		resp.Data = snapshots
	}
	return resp, nil
}
