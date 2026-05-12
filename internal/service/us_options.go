package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

// USOptionsService provides low-level US option market-data queries.
type USOptionsService struct {
	repo *chrepo.Repo
}

func NewUSOptionsService(repo *chrepo.Repo) *USOptionsService {
	return &USOptionsService{repo: repo}
}

func (s *USOptionsService) QuerySymbols(ctx context.Context, req dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	underlying := resolveUSOptionUnderlying(req.Underlying, req.Root)
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}

	query := `SELECT
    symbol,
	underlying,
	CAST(option_type, 'String') AS option_type,
	expiration,
	strike
FROM us_options_bar_1m
WHERE underlying = ` + clickhouseStringLiteral(underlying)
	if req.Search != "" {
		query += ` AND symbol ILIKE ` + clickhouseStringLiteral("%"+req.Search+"%")
	}
	if req.Cursor != "" {
		cursorSymbol, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += ` AND symbol > ` + clickhouseStringLiteral(cursorSymbol)
	}

	query += fmt.Sprintf(`
GROUP BY symbol, underlying, option_type, expiration, strike
ORDER BY symbol
LIMIT %s`, clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
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
		row.Expiration = dateAsUTC(row.Expiration)
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option symbol rows: %w", err)
	}

	resp := &dto.USOptionSymbolResponse{Data: make([]dto.USOptionSymbolRow, 0)}
	resp.Data, resp.NextCursor = applySymbolCursorPagination(symbols, limit, func(r dto.USOptionSymbolRow) string {
		return encodeCursorString(r.Symbol)
	})
	return resp, nil
}

func (s *USOptionsService) QueryBars(ctx context.Context, req dto.USOptionBarRequest) (*dto.USOptionBarResponse, error) {
	normalizedSymbol, err := normalizeUSOptionQuerySymbol(req.Symbol)
	if err != nil {
		return nil, err
	}
	req.Symbol = normalizedSymbol

	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := normalizeUSOptionInterval(req.Interval)
	tableName, err := resolveUSBarTable(interval, chquery.USOptionIntervals, "US option")
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
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')%s
ORDER BY timestamp
LIMIT %s`, tableName, clickhouseStringLiteral(req.Symbol), clickhouseDateTimeLiteral(fromT), clickhouseDateTimeLiteral(toT), usSessionCondition(session), clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
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
		row.Timestamp = row.Timestamp.UTC()
		row.Expiration = dateAsUTC(row.Expiration)
		row.Strike = sanitizeFloat64(row.Strike)
		row.Open = sanitizeFloat32(row.Open)
		row.High = sanitizeFloat32(row.High)
		row.Low = sanitizeFloat32(row.Low)
		row.Close = sanitizeFloat32(row.Close)
		row.UnderlyingClose = sanitizeFloat32(row.UnderlyingClose)
		row.ImpliedVolatility = sanitizeFloat32(row.ImpliedVolatility)
		row.Delta = sanitizeFloat32(row.Delta)
		row.Gamma = sanitizeFloat32(row.Gamma)
		row.Vega = sanitizeFloat32(row.Vega)
		row.Theta = sanitizeFloat32(row.Theta)
		row.Rho = sanitizeFloat32(row.Rho)
		row.Volume = sanitizeFloat64(row.Volume)
		bars = append(bars, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option bar rows: %w", err)
	}

	resp := &dto.USOptionBarResponse{Data: make([]dto.USOptionBarRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(bars, limit, func(r dto.USOptionBarRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

func (s *USOptionsService) QueryGreeks(ctx context.Context, req dto.USOptionGreeksRequest) (*dto.USOptionGreeksResponse, error) {
	normalizedSymbol, err := normalizeUSOptionQuerySymbol(req.Symbol)
	if err != nil {
		return nil, err
	}
	req.Symbol = normalizedSymbol

	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	interval := normalizeUSOptionInterval(req.Interval)
	tableName, err := resolveUSBarTable(interval, chquery.USOptionIntervals, "US option")
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
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')%s
ORDER BY timestamp
LIMIT %s`, tableName, clickhouseStringLiteral(req.Symbol), clickhouseDateTimeLiteral(fromT), clickhouseDateTimeLiteral(toT), usSessionCondition(session), clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
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
		row.Timestamp = row.Timestamp.UTC()
		row.Expiration = dateAsUTC(row.Expiration)
		row.Strike = sanitizeFloat64(row.Strike)
		row.UnderlyingClose = sanitizeFloat32(row.UnderlyingClose)
		row.ImpliedVolatility = sanitizeFloat32(row.ImpliedVolatility)
		row.Delta = sanitizeFloat32(row.Delta)
		row.Gamma = sanitizeFloat32(row.Gamma)
		row.Vega = sanitizeFloat32(row.Vega)
		row.Theta = sanitizeFloat32(row.Theta)
		row.Rho = sanitizeFloat32(row.Rho)
		row.Volume = sanitizeFloat64(row.Volume)
		greeks = append(greeks, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option greeks rows: %w", err)
	}

	resp := &dto.USOptionGreeksResponse{Data: make([]dto.USOptionGreeksRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(greeks, limit, func(r dto.USOptionGreeksRow) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

func (s *USOptionsService) QueryChain(ctx context.Context, req dto.USOptionChainRequest) (*dto.USOptionChainResponse, error) {
	interval, err := normalizeUSChainInterval(req.Interval)
	if err != nil {
		return nil, err
	}
	viewName, ok := usmarket.ChainPrecomputedIntervals[interval]
	if !ok {
		return nil, dto.NewValidationError("unsupported us-options chain interval %q", interval)
	}
	underlying := resolveUSOptionUnderlying(req.Underlying, "")
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}
	expiration, err := parseUSOptionExpirationDate(req.Expiration)
	if err != nil {
		return nil, err
	}
	limit := usBarLimit(req.Limit)

	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		latest, hasData, err := s.latestUSOptionChainTimestamp(ctx, viewName, underlying)
		if err != nil {
			return nil, err
		}
		if !hasData {
			return &dto.USOptionChainResponse{Data: make([]dto.USOptionChainSnapshot, 0)}, nil
		}
		req.From, req.To = defaultUSOptionChainWindow(latest)
	}

	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

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
WHERE underlying = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')
ORDER BY timestamp
LIMIT %s`, viewName, clickhouseStringLiteral(underlying), clickhouseDateTimeLiteral(fromT), clickhouseDateTimeLiteral(toT), clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
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
			volumes          []float64
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
			if !expiration.IsZero() && !expirations[i].Equal(expiration) {
				continue
			}
			snapshot.Contracts = append(snapshot.Contracts, dto.USOptionChainContract{
				Symbol:            symbols[i],
				OptionType:        types[i],
				Expiration:        expirations[i],
				Strike:            sanitizeFloat64(strikes[i]),
				Close:             sanitizeFloat32(closes[i]),
				UnderlyingClose:   sanitizeFloat32(underlyingCloses[i]),
				ImpliedVolatility: sanitizeFloat32(ivs[i]),
				Delta:             sanitizeFloat32(deltas[i]),
				Gamma:             sanitizeFloat32(gammas[i]),
				Vega:              sanitizeFloat32(vegas[i]),
				Theta:             sanitizeFloat32(thetas[i]),
				Rho:               sanitizeFloat32(rhos[i]),
				Volume:            sanitizeFloat64(volumes[i]),
				Transactions:      transactions[i],
			})
		}
		if len(snapshot.Contracts) == 0 {
			continue
		}

		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option chain rows: %w", err)
	}

	resp := &dto.USOptionChainResponse{Data: make([]dto.USOptionChainSnapshot, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(snapshots, limit, func(r dto.USOptionChainSnapshot) string {
		return encodeCursor(r.Timestamp)
	})
	return resp, nil
}

func (s *USOptionsService) latestUSOptionChainTimestamp(ctx context.Context, viewName, underlying string) (time.Time, bool, error) {
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT ifNull(maxOrNull(timestamp), toDateTime('1970-01-01 00:00:00', 'UTC'))
FROM %s
WHERE underlying = %s`, viewName, clickhouseStringLiteral(underlying)))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest US option chain timestamp: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var latest time.Time
	if err := rows.Scan(&latest); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest US option chain timestamp: %w", err)
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, fmt.Errorf("iterate latest US option chain timestamp: %w", err)
	}
	if latest.IsZero() || latest.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return latest.UTC(), true, nil
}
