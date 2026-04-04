package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

// USOptionsService provides low-level US option market-data queries.
type USOptionsService struct {
	conn driver.Conn
}

func NewUSOptionsService(conn driver.Conn) *USOptionsService {
	return &USOptionsService{conn: conn}
}

func (s *USOptionsService) QuerySymbols(ctx context.Context, req dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}

	query := `SELECT
    symbol,
    anyLast(underlying) AS underlying,
    CAST(anyLast(option_type), 'String') AS option_type,
    anyLast(expiration) AS expiration,
    anyLast(strike) AS strike
FROM us_options_bar_1m
WHERE underlying = {underlying:String}`

	args := []interface{}{clickhouse.Named("underlying", underlying)}
	if req.Search != "" {
		query += ` AND symbol ILIKE {search:String}`
		args = append(args, clickhouse.Named("search", "%"+req.Search+"%"))
	}
	if req.Cursor != "" {
		cursorSymbol, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += ` AND symbol > {cursor_symbol:String}`
		args = append(args, clickhouse.Named("cursor_symbol", cursorSymbol))
	}

	query += fmt.Sprintf(`
GROUP BY symbol
ORDER BY symbol
LIMIT %d`, limit+1)

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query US option symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.USOptionSymbolRow, 0, limit)
	for rows.Next() {
		var row dto.USOptionSymbolRow
		if err := rows.Scan(&row.Symbol, &row.Underlying, &row.OptionType, &row.Expiration, &row.Strike); err != nil {
			return nil, fmt.Errorf("scan US option symbol row: %w", err)
		}
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option symbol rows: %w", err)
	}

	resp := &dto.USOptionSymbolResponse{}
	if len(symbols) > limit {
		resp.NextCursor = encodeCursorString(symbols[limit-1].Symbol)
		resp.Data = symbols[:limit]
	} else {
		resp.Data = symbols
	}
	return resp, nil
}

func (s *USOptionsService) QueryBars(ctx context.Context, req dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := normalizeUSOptionInterval(req.Interval)
	tableName, err := resolveUSBarTable(interval, usOptionBarIntervals, "US option")
	if err != nil {
		return nil, err
	}
	session, err := normalizeUSSession(req.Session, interval)
	if err != nil {
		return nil, err
	}
	limit := usBarLimit(req.Limit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    underlying,
    CAST(option_type, 'String') AS option_type,
    expiration,
    strike,
    open,
    high,
    low,
    close,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
    toUInt64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT %d`, tableName, usSessionCondition(session), limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(fromT)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(toT)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US option bars: %w", err)
	}
	defer rows.Close()

	bars := make([]dto.USOptionBarRow, 0, limit)
	for rows.Next() {
		var row dto.USOptionBarRow
		if err := rows.Scan(
			&row.Timestamp,
			&row.Symbol,
			&row.Underlying,
			&row.OptionType,
			&row.Expiration,
			&row.Strike,
			&row.Open,
			&row.High,
			&row.Low,
			&row.Close,
			&row.UnderlyingClose,
			&row.ImpliedVolatility,
			&row.Delta,
			&row.Gamma,
			&row.Vega,
			&row.Theta,
			&row.Rho,
			&row.Volume,
			&row.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US option bar row: %w", err)
		}
		bars = append(bars, row)
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

func (s *USOptionsService) QueryGreeks(ctx context.Context, req dto.USOptionGreeksRequest) (*dto.USOptionGreeksResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := normalizeUSOptionInterval(req.Interval)
	tableName, err := resolveUSBarTable(interval, usOptionBarIntervals, "US option")
	if err != nil {
		return nil, err
	}
	session, err := normalizeUSSession(req.Session, interval)
	if err != nil {
		return nil, err
	}
	limit := usBarLimit(req.Limit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    underlying,
    CAST(option_type, 'String') AS option_type,
    expiration,
    strike,
    underlying_close,
    implied_volatility,
    delta,
    gamma,
    vega,
    theta,
    rho,
    toUInt64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = {symbol:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')%s
ORDER BY timestamp
LIMIT %d`, tableName, usSessionCondition(session), limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("symbol", req.Symbol),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(fromT)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(toT)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US option greeks: %w", err)
	}
	defer rows.Close()

	greeks := make([]dto.USOptionGreeksRow, 0, limit)
	for rows.Next() {
		var row dto.USOptionGreeksRow
		if err := rows.Scan(
			&row.Timestamp,
			&row.Symbol,
			&row.Underlying,
			&row.OptionType,
			&row.Expiration,
			&row.Strike,
			&row.UnderlyingClose,
			&row.ImpliedVolatility,
			&row.Delta,
			&row.Gamma,
			&row.Vega,
			&row.Theta,
			&row.Rho,
			&row.Volume,
			&row.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US option greeks row: %w", err)
		}
		greeks = append(greeks, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option greeks rows: %w", err)
	}

	resp := &dto.USOptionGreeksResponse{}
	if len(greeks) > limit {
		resp.NextCursor = encodeCursor(greeks[limit-1].Timestamp)
		resp.Data = greeks[:limit]
	} else {
		resp.Data = greeks
	}
	return resp, nil
}

func (s *USOptionsService) QueryChain(ctx context.Context, req dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval, err := normalizeUSChainInterval(req.Interval)
	if err != nil {
		return nil, err
	}
	viewName, ok := usmarket.ChainPrecomputedIntervals[interval]
	if !ok {
		return nil, dto.NewValidationError("unsupported us-options chain interval %q", interval)
	}
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}
	limit := usBarLimit(req.Limit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    underlying,
    symbols,
    arrayMap(x -> CAST(x, 'String'), option_types) AS option_types,
    expirations,
    strikes,
    close_prices,
    underlying_closes,
    implied_volatilities,
    deltas,
    gammas,
    vegas,
    thetas,
    rhos,
    volumes,
    transactions
FROM %s
WHERE underlying = {underlying:String}
  AND timestamp >= toDateTime({from:String}, 'UTC')
  AND timestamp < toDateTime({to:String}, 'UTC')
ORDER BY timestamp
LIMIT %d`, viewName, limit+1)

	rows, err := s.conn.Query(ctx, query,
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", cryptooptions.ClickHouseTimeParam(fromT)),
		clickhouse.Named("to", cryptooptions.ClickHouseTimeParam(toT)),
	)
	if err != nil {
		return nil, fmt.Errorf("query US option chain: %w", err)
	}
	defer rows.Close()

	snapshots := make([]dto.USOptionChainSnapshot, 0, limit)
	for rows.Next() {
		var (
			snapshot         dto.USOptionChainSnapshot
			symbols          []string
			types            []string
			expirations      []time.Time
			strikes          []float64
			closes           []float32
			underlyingCloses []float32
			ivs              []float32
			deltas           []float32
			gammas           []float32
			vegas            []float32
			thetas           []float32
			rhos             []float32
			volumes          []uint64
			transactions     []uint64
		)
		if err := rows.Scan(
			&snapshot.Timestamp,
			&snapshot.Underlying,
			&symbols,
			&types,
			&expirations,
			&strikes,
			&closes,
			&underlyingCloses,
			&ivs,
			&deltas,
			&gammas,
			&vegas,
			&thetas,
			&rhos,
			&volumes,
			&transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US option chain row: %w", err)
		}

		n := len(symbols)
		if len(types) != n || len(expirations) != n || len(strikes) != n || len(closes) != n || len(underlyingCloses) != n || len(ivs) != n || len(deltas) != n || len(gammas) != n || len(vegas) != n || len(thetas) != n || len(rhos) != n || len(volumes) != n || len(transactions) != n {
			return nil, fmt.Errorf("invalid US option chain row at %s: array lengths mismatch", snapshot.Timestamp.UTC().Format(time.RFC3339))
		}

		snapshot.Contracts = make([]dto.USOptionChainContract, 0, n)
		for i := 0; i < n; i++ {
			snapshot.Contracts = append(snapshot.Contracts, dto.USOptionChainContract{
				Symbol:            symbols[i],
				OptionType:        types[i],
				Expiration:        expirations[i],
				Strike:            strikes[i],
				Close:             closes[i],
				UnderlyingClose:   underlyingCloses[i],
				ImpliedVolatility: ivs[i],
				Delta:             deltas[i],
				Gamma:             gammas[i],
				Vega:              vegas[i],
				Theta:             thetas[i],
				Rho:               rhos[i],
				Volume:            volumes[i],
				Transactions:      transactions[i],
			})
		}

		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option chain rows: %w", err)
	}

	resp := &dto.USOptionChainResponse{}
	if len(snapshots) > limit {
		resp.NextCursor = encodeCursor(snapshots[limit-1].Timestamp)
		resp.Data = snapshots[:limit]
	} else {
		resp.Data = snapshots
	}
	return resp, nil
}
