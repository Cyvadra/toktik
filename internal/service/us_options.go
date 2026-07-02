package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

const (
	optionWallCacheTTL      = 24 * time.Hour
	optionWallSnapshotLimit = 250
)

type optionWallPolygonClient interface {
	OptionChain(ctx context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error)
}

type optionWallStrikeAccumulator struct {
	row        dto.USOptionWallStrikeRow
	quoteCount int
}

// USOptionsService provides low-level US option market-data queries.
type USOptionsService struct {
	repo                  *chrepo.Repo
	polygon               optionWallPolygonClient
	cache                 cache.Store
	latest                LatestUSMarketCacheReader
	now                   func() time.Time
	ensureWallSchemaOnce  sync.Once
	ensureWallSchemaError error
}

func NewUSOptionsService(repo *chrepo.Repo) *USOptionsService {
	return &USOptionsService{repo: repo, now: time.Now}
}

func (s *USOptionsService) WithPolygonClient(client optionWallPolygonClient) *USOptionsService {
	if s == nil {
		return nil
	}
	s.polygon = client
	return s
}

func (s *USOptionsService) WithCache(store cache.Store) *USOptionsService {
	if s == nil {
		return nil
	}
	s.cache = store
	return s
}

func (s *USOptionsService) WithLatestMarketCache(reader LatestUSMarketCacheReader) *USOptionsService {
	if s == nil {
		return nil
	}
	s.latest = reader
	return s
}

func (s *USOptionsService) QuerySymbols(ctx context.Context, req dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	underlying := resolveUSOptionUnderlying(req.Underlying, req.Root)
	search := strings.TrimSpace(req.Search)
	if underlying == "" && search == "" {
		return nil, dto.NewValidationError("underlying is required")
	}

	query := `SELECT
    symbol,
	underlying,
	CAST(option_type, 'String') AS option_type,
	expiration,
	strike
FROM us_options_bar_1m
WHERE 1 = 1`
	if underlying != "" {
		query += ` AND underlying = ` + clickhouseStringLiteral(underlying)
	}
	if search != "" {
		query += ` AND symbol ILIKE ` + clickhouseStringLiteral("%"+search+"%")
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
	if s.shouldMergeLatestOptionSymbols(req, underlying) {
		merged, err := s.mergeLatestOptionSymbols(ctx, underlying, search, symbols)
		if err != nil {
			return nil, err
		}
		symbols = merged
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
	if s.shouldMergeLatestOptionBars(req, interval) {
		if merged, _, err := s.latest.MergeOptionBars(ctx, req.Symbol, fromT, toT, bars); err != nil {
			return nil, err
		} else {
			bars = merged
		}
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
	limit := usBarLimit(req.SnapshotLimit)

	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		latest, hasData, err := s.latestUSOptionChainTimestamp(ctx, viewName, underlying)
		if err != nil {
			return nil, err
		}
		if !hasData {
			if !req.IncludeLatest || s.latest == nil {
				return &dto.USOptionChainResponse{Data: make([]dto.USOptionChainSnapshot, 0)}, nil
			}
			latest = time.Now().UTC()
		}
		req.From, req.To = defaultUSOptionChainWindow(latest)
		if s.shouldMergeLatestOptionChain(req, interval) {
			req.To = time.Now().UTC().AddDate(0, 0, 1).Format(time.RFC3339)
		}
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
	if s.shouldMergeLatestOptionChain(req, interval) {
		if merged, _, err := s.latest.MergeOptionChain(ctx, underlying, expiration, fromT, toT, snapshots); err != nil {
			return nil, err
		} else {
			snapshots = merged
		}
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

func (s *USOptionsService) shouldMergeLatestOptionSymbols(req dto.USOptionSymbolRequest, underlying string) bool {
	return s != nil && s.latest != nil && req.IncludeLatest && strings.TrimSpace(underlying) != ""
}

func (s *USOptionsService) shouldMergeLatestOptionBars(req dto.USOptionBarRequest, interval string) bool {
	return s != nil && s.latest != nil && req.IncludeLatest && interval == "1d"
}

func (s *USOptionsService) shouldMergeLatestOptionChain(req dto.USOptionChainRequest, interval string) bool {
	return s != nil && s.latest != nil && req.IncludeLatest && interval == "1d"
}

func (s *USOptionsService) mergeLatestOptionSymbols(ctx context.Context, underlying, search string, rows []dto.USOptionSymbolRow) ([]dto.USOptionSymbolRow, error) {
	snapshot, ok, err := s.latest.LatestOptionChainSnapshot(ctx, underlying, time.Time{})
	if err != nil || !ok {
		return rows, err
	}
	search = strings.ToUpper(strings.TrimSpace(search))
	merged := make(map[string]dto.USOptionSymbolRow, len(rows)+len(snapshot.Contracts))
	for _, row := range rows {
		merged[strings.ToUpper(strings.TrimSpace(row.Symbol))] = row
	}
	for _, contract := range snapshot.Contracts {
		symbol := strings.ToUpper(strings.TrimSpace(contract.Symbol))
		if symbol == "" {
			continue
		}
		if search != "" && !strings.Contains(symbol, search) {
			continue
		}
		merged[symbol] = dto.USOptionSymbolRow{Symbol: symbol, Underlying: strings.ToUpper(strings.TrimSpace(underlying)), OptionType: contract.OptionType, Expiration: dateAsUTC(contract.Expiration), Strike: sanitizeFloat64(contract.Strike)}
	}
	out := make([]dto.USOptionSymbolRow, 0, len(merged))
	for _, row := range merged {
		out = append(out, row)
	}
	sortUSOptionSymbolsForDiscovery(out, s.now())
	return out, nil
}

func sortUSOptionSymbolsForDiscovery(rows []dto.USOptionSymbolRow, now time.Time) {
	today := normalizeCalendarDate(now.UTC())
	sort.SliceStable(rows, func(i, j int) bool {
		leftFuture := !normalizeCalendarDate(rows[i].Expiration).Before(today)
		rightFuture := !normalizeCalendarDate(rows[j].Expiration).Before(today)
		if leftFuture != rightFuture {
			return leftFuture
		}
		leftExpiration := normalizeCalendarDate(rows[i].Expiration)
		rightExpiration := normalizeCalendarDate(rows[j].Expiration)
		if !leftExpiration.Equal(rightExpiration) {
			if leftFuture {
				return leftExpiration.Before(rightExpiration)
			}
			return leftExpiration.After(rightExpiration)
		}
		if rows[i].Strike != rows[j].Strike {
			return rows[i].Strike < rows[j].Strike
		}
		if rows[i].OptionType != rows[j].OptionType {
			return rows[i].OptionType < rows[j].OptionType
		}
		return rows[i].Symbol < rows[j].Symbol
	})
}

func (s *USOptionsService) QueryOptionWall(ctx context.Context, req dto.USOptionWallRequest) (*dto.USOptionWallResponse, error) {
	if s.polygon == nil {
		return nil, dto.NewValidationError("polygon option wall provider is not configured")
	}
	symbol := resolveUSOptionUnderlying(req.Symbol, "")
	if symbol == "" {
		return nil, dto.NewValidationError("symbol is required")
	}
	minDTE := req.MinDTE
	maxDTE := req.MaxDTE
	if minDTE < 0 {
		return nil, dto.NewValidationError("min_dte must be greater than or equal to zero")
	}
	if maxDTE <= 0 {
		maxDTE = minDTE
	}
	if maxDTE < minDTE {
		return nil, dto.NewValidationError("max_dte must be greater than or equal to min_dte")
	}
	snapshotDay := normalizeCalendarDate(s.now().UTC())
	expirations, err := s.listOptionWallExpirations(ctx, symbol, snapshotDay, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	resp := &dto.USOptionWallResponse{Symbol: symbol, SnapshotDay: snapshotDay, Data: make([]dto.USOptionWall, 0, len(expirations))}
	for _, expiration := range expirations {
		wall, err := s.loadOrComputeOptionWall(ctx, symbol, expiration, snapshotDay)
		if err != nil {
			return nil, err
		}
		resp.Data = append(resp.Data, *wall)
	}
	return resp, nil
}

func (s *USOptionsService) listOptionWallExpirations(ctx context.Context, symbol string, snapshotDay time.Time, minDTE, maxDTE int) ([]time.Time, error) {
	minExpiration := snapshotDay.AddDate(0, 0, minDTE)
	maxExpiration := snapshotDay.AddDate(0, 0, maxDTE)
	rows, err := s.repo.Query(ctx, `SELECT expiration
FROM us_options_bar_1d
WHERE underlying = {symbol:String}
  AND expiration >= toDate({min_expiration:String})
  AND expiration <= toDate({max_expiration:String})
GROUP BY expiration
ORDER BY expiration`,
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("min_expiration", minExpiration.Format("2006-01-02")),
		clickhouse.Named("max_expiration", maxExpiration.Format("2006-01-02")),
	)
	if err != nil {
		return nil, fmt.Errorf("query option wall expirations: %w", err)
	}
	defer rows.Close()
	expirations := make([]time.Time, 0)
	for rows.Next() {
		var expiration time.Time
		if err := rows.Scan(&expiration); err != nil {
			return nil, fmt.Errorf("scan option wall expiration: %w", err)
		}
		expirations = append(expirations, normalizeCalendarDate(expiration))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate option wall expirations: %w", err)
	}
	return expirations, nil
}

func (s *USOptionsService) loadOrComputeOptionWall(ctx context.Context, symbol string, expiration, snapshotDay time.Time) (*dto.USOptionWall, error) {
	if wall, ok, err := s.loadOptionWallFromCache(ctx, symbol, expiration, snapshotDay); err != nil {
		return nil, err
	} else if ok {
		return wall, nil
	}
	if wall, ok, err := s.loadOptionWallFromClickHouse(ctx, symbol, expiration, snapshotDay); err != nil {
		return nil, err
	} else if ok {
		_ = s.storeOptionWallInCache(ctx, wall)
		return wall, nil
	}
	wall, err := s.computeOptionWall(ctx, symbol, expiration, snapshotDay)
	if err != nil {
		return nil, err
	}
	if err := s.storeOptionWallInClickHouse(ctx, wall); err != nil {
		return nil, err
	}
	if err := s.storeOptionWallInCache(ctx, wall); err != nil {
		return nil, err
	}
	return wall, nil
}

func (s *USOptionsService) computeOptionWall(ctx context.Context, symbol string, expiration, snapshotDay time.Time) (*dto.USOptionWall, error) {
	contracts, err := s.fetchOptionWallContracts(ctx, symbol, expiration)
	if err != nil {
		return nil, err
	}
	buckets := make(map[float64]*optionWallStrikeAccumulator)
	for _, contract := range contracts {
		strike := sanitizeFloat64(contract.Contract.StrikePrice)
		bucket := buckets[strike]
		if bucket == nil {
			bucket = &optionWallStrikeAccumulator{row: dto.USOptionWallStrikeRow{Strike: strike}}
			buckets[strike] = bucket
		}
		bucket.row.TotalOpenInterest += contract.OpenInterest
		switch strings.ToLower(strings.TrimSpace(contract.Contract.ContractType)) {
		case "c", "call":
			bucket.row.CallOpenInterest += contract.OpenInterest
			bucket.row.CallContractCount++
		case "p", "put":
			bucket.row.PutOpenInterest += contract.OpenInterest
			bucket.row.PutContractCount++
		}
		accumulateWallQuote(bucket, contract.LastQuote)
	}
	strikes := make([]dto.USOptionWallStrikeRow, 0, len(buckets))
	for _, bucket := range buckets {
		strikes = append(strikes, finalizeWallStrike(*bucket))
	}
	sort.Slice(strikes, func(i, j int) bool { return strikes[i].Strike < strikes[j].Strike })
	return &dto.USOptionWall{
		Symbol:       symbol,
		Expiration:   normalizeCalendarDate(expiration),
		SnapshotDay:  snapshotDay,
		DaysToExpiry: int(normalizeCalendarDate(expiration).Sub(snapshotDay).Hours() / 24),
		Strikes:      strikes,
	}, nil
}

func (s *USOptionsService) fetchOptionWallContracts(ctx context.Context, symbol string, expiration time.Time) ([]polygonpkg.OptionChainContract, error) {
	merged := make(map[string]polygonpkg.OptionChainContract)
	for _, order := range []string{"asc", "desc"} {
		page, err := s.polygon.OptionChain(ctx, polygonpkg.OptionChainRequest{
			Underlying:        symbol,
			ExpirationDate:    expiration.Format("2006-01-02"),
			Order:             order,
			Sort:              "ticker",
			Limit:             optionWallSnapshotLimit,
			DisablePagination: true,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch option wall snapshot %s %s: %w", symbol, expiration.Format("2006-01-02"), err)
		}
		for _, contract := range page {
			merged[contract.Contract.Ticker] = contract
		}
	}
	contracts := make([]polygonpkg.OptionChainContract, 0, len(merged))
	for _, contract := range merged {
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(i, j int) bool {
		return contracts[i].Contract.Ticker < contracts[j].Contract.Ticker
	})
	return contracts, nil
}

func accumulateWallQuote(bucket *optionWallStrikeAccumulator, quote polygonpkg.Quote) {
	if bucket == nil {
		return
	}
	if quote.BidPrice == nil || quote.AskPrice == nil {
		return
	}
	bid := *quote.BidPrice
	ask := *quote.AskPrice
	mid := (bid + ask) / 2
	spread := ask - bid
	if bucket.row.AverageBid == nil {
		bucket.row.AverageBid = ptrFloat64(0)
		bucket.row.AverageAsk = ptrFloat64(0)
		bucket.row.AverageMidpoint = ptrFloat64(0)
		bucket.row.AverageSpread = ptrFloat64(0)
	}
	*bucket.row.AverageBid += bid
	*bucket.row.AverageAsk += ask
	*bucket.row.AverageMidpoint += mid
	*bucket.row.AverageSpread += spread
	bucket.quoteCount++
}

func finalizeWallStrike(bucket optionWallStrikeAccumulator) dto.USOptionWallStrikeRow {
	if bucket.quoteCount > 0 && bucket.row.AverageBid != nil && bucket.row.AverageAsk != nil && bucket.row.AverageMidpoint != nil && bucket.row.AverageSpread != nil {
		count := float64(bucket.quoteCount)
		*bucket.row.AverageBid /= count
		*bucket.row.AverageAsk /= count
		*bucket.row.AverageMidpoint /= count
		*bucket.row.AverageSpread /= count
	}
	return bucket.row
}

func (s *USOptionsService) loadOptionWallFromCache(ctx context.Context, symbol string, expiration, snapshotDay time.Time) (*dto.USOptionWall, bool, error) {
	if s.cache == nil {
		return nil, false, nil
	}
	raw, ok, err := s.cache.Get(ctx, optionWallCacheKey(symbol, expiration, snapshotDay))
	if err != nil || !ok {
		return nil, ok, err
	}
	var wall dto.USOptionWall
	if err := json.Unmarshal(raw, &wall); err != nil {
		return nil, false, nil
	}
	wall.Expiration = normalizeCalendarDate(wall.Expiration)
	wall.SnapshotDay = normalizeCalendarDate(wall.SnapshotDay)
	return &wall, true, nil
}

func (s *USOptionsService) storeOptionWallInCache(ctx context.Context, wall *dto.USOptionWall) error {
	if s.cache == nil || wall == nil {
		return nil
	}
	payload, err := json.Marshal(wall)
	if err != nil {
		return fmt.Errorf("marshal option wall cache payload: %w", err)
	}
	if err := s.cache.Set(ctx, optionWallCacheKey(wall.Symbol, wall.Expiration, wall.SnapshotDay), payload, optionWallCacheTTL); err != nil {
		return fmt.Errorf("store option wall cache: %w", err)
	}
	return nil
}

func (s *USOptionsService) loadOptionWallFromClickHouse(ctx context.Context, symbol string, expiration, snapshotDay time.Time) (*dto.USOptionWall, bool, error) {
	if s.repo == nil {
		return nil, false, nil
	}
	if err := s.ensureOptionWallSchema(ctx); err != nil {
		return nil, false, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT payload
FROM %s
WHERE symbol = {symbol:String}
  AND expiration = toDate({expiration:String})
  AND snapshot_day = toDate({snapshot_day:String})
ORDER BY updated_at DESC
LIMIT 1`, usmarket.OptionWallTable),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("expiration", expiration.Format("2006-01-02")),
		clickhouse.Named("snapshot_day", snapshotDay.Format("2006-01-02")),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query option wall cache table: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, nil
	}
	var payload string
	if err := rows.Scan(&payload); err != nil {
		return nil, false, fmt.Errorf("scan option wall payload: %w", err)
	}
	var wall dto.USOptionWall
	if err := json.Unmarshal([]byte(payload), &wall); err != nil {
		return nil, false, fmt.Errorf("decode option wall payload: %w", err)
	}
	wall.Expiration = normalizeCalendarDate(wall.Expiration)
	wall.SnapshotDay = normalizeCalendarDate(wall.SnapshotDay)
	return &wall, true, nil
}

func (s *USOptionsService) storeOptionWallInClickHouse(ctx context.Context, wall *dto.USOptionWall) error {
	if s.repo == nil || wall == nil {
		return nil
	}
	if err := s.ensureOptionWallSchema(ctx); err != nil {
		return err
	}
	payload, err := json.Marshal(wall)
	if err != nil {
		return fmt.Errorf("marshal option wall payload: %w", err)
	}
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	snapshot_day,
	symbol,
	expiration,
	payload,
	updated_at
)`, usmarket.OptionWallTable))
	if err != nil {
		return fmt.Errorf("prepare option wall batch: %w", err)
	}
	if err := batch.Append(
		normalizeCalendarDate(wall.SnapshotDay),
		wall.Symbol,
		normalizeCalendarDate(wall.Expiration),
		string(payload),
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("append option wall row: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send option wall batch: %w", err)
	}
	return nil
}

func (s *USOptionsService) ensureOptionWallSchema(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	s.ensureWallSchemaOnce.Do(func() {
		s.ensureWallSchemaError = usmarket.InitOptionWallSchema(ctx, s.repo.Conn)
	})
	if s.ensureWallSchemaError != nil {
		return fmt.Errorf("ensure option wall schema: %w", s.ensureWallSchemaError)
	}
	return nil
}

func optionWallCacheKey(symbol string, expiration, snapshotDay time.Time) string {
	return fmt.Sprintf("us-options:option-wall:%s:%s:%s", strings.ToUpper(strings.TrimSpace(symbol)), normalizeCalendarDate(expiration).Format("2006-01-02"), normalizeCalendarDate(snapshotDay).Format("2006-01-02"))
}

func ptrFloat64(value float64) *float64 {
	return &value
}
