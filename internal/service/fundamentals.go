package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	fundamentalSeriesModeEvent  = "event"
	fundamentalSeriesModeAsOf   = "as_of"
	fundamentalSeriesModeFilled = "filled"

	fundamentalFillEventOnly      = "event_only"
	fundamentalFillForwardFill    = "forward_fill"
	fundamentalFillForwardLimited = "limited_forward_fill"
)

// FundamentalsService exposes catalog, series, snapshot and panel queries
// over the symbol-bound fundamental observation store.
type FundamentalsService struct {
	repo     *chrepo.Repo
	virtuals *virtualFundamentalsProvider
}

// NewFundamentalsService builds the service over the shared ClickHouse repo.
func NewFundamentalsService(repo *chrepo.Repo) *FundamentalsService {
	return &FundamentalsService{
		repo:     repo,
		virtuals: newVirtualFundamentalsProvider(NewMacroService(repo)),
	}
}

// ----- Catalog -----

// ListFactors returns active factor catalog entries, optionally filtered by market.
func (s *FundamentalsService) ListFactors(ctx context.Context, req dto.FundamentalFactorCatalogRequest) (*dto.FundamentalFactorCatalogResponse, error) {
	market := strings.TrimSpace(req.Market)
	if market != "" {
		if err := validateFundamentalMarket(market); err != nil {
			return nil, err
		}
	}

	rows, err := s.repo.Query(ctx, chquery.FundamentalFactorCatalogQuery(),
		clickhouse.Named("market", market),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental catalog: %w", err)
	}
	defer rows.Close()

	out := &dto.FundamentalFactorCatalogResponse{Data: []dto.FundamentalFactorCatalogEntry{}}
	for rows.Next() {
		var (
			e           dto.FundamentalFactorCatalogEntry
			fillMaxDays uint16
			pointInTime uint8
			active      uint8
			slaHours    uint32
		)
		if err := rows.Scan(
			&e.Market, &e.FactorCode, &e.DisplayName, &e.Description,
			&e.ValueType, &e.Unit, &e.PreferredFrequency, &e.FillPolicy,
			&fillMaxDays, &pointInTime, &e.Source, &active, &slaHours,
			&e.Metadata, &e.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan fundamental catalog: %w", err)
		}
		e.FillMaxDays = int(fillMaxDays)
		e.PointInTime = pointInTime != 0
		e.Active = active != 0
		e.SLAHours = int(slaHours)
		out.Data = append(out.Data, e)
	}
	out.Data = s.virtuals.appendCatalogEntries(out.Data, market)
	return out, rows.Err()
}

// ----- Series -----

// QuerySeries returns sparse, as-of, or forward-filled values for one (market,
// symbol, factor) over a time range. Forward-fill respects the catalog policy.
func (s *FundamentalsService) QuerySeries(ctx context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error) {
	market, symbol, factor, err := normalizeFundamentalKey(req.Market, req.Symbol, req.Factor)
	if err != nil {
		return nil, err
	}
	storageSymbol := resolveFundamentalStorageSymbol(market, symbol, factor)
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	asOf, err := resolveAsOf(req.AsOf, to)
	if err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = fundamentalSeriesModeFilled
	}
	switch mode {
	case fundamentalSeriesModeEvent, fundamentalSeriesModeAsOf, fundamentalSeriesModeFilled:
	default:
		return nil, dto.NewValidationError("invalid mode %q (event|as_of|filled)", mode)
	}

	resp := &dto.FundamentalSeriesResponse{
		Market: market, Symbol: symbol, Factor: factor,
		Mode: mode, AsOf: asOf,
		Data: []dto.FundamentalSeriesPoint{},
	}

	if points, fillPolicy, handled, err := s.virtuals.querySeries(ctx, req, market, symbol, factor, mode); handled {
		if err != nil {
			return nil, err
		}
		if fillPolicy != "" {
			resp.FillPolicy = fillPolicy
		}
		resp.Data = points
		return resp, nil
	}

	if mode == fundamentalSeriesModeEvent {
		points, err := s.queryEventSeries(ctx, market, storageSymbol, factor, from, to, asOf)
		if err != nil {
			return nil, err
		}
		resp.Data = points
		return resp, nil
	}

	asOfPoints, err := s.queryAsOfSeries(ctx, market, storageSymbol, factor, from, to, asOf)
	if err != nil {
		return nil, err
	}

	if mode == fundamentalSeriesModeAsOf {
		resp.Data = asOfPoints
		return resp, nil
	}

	policy, maxDays, err := s.lookupFillPolicy(ctx, market, factor)
	if err != nil {
		return nil, err
	}
	resp.FillPolicy = policy
	filled, err := s.buildFilledSeries(ctx, market, symbol, factor, from, to, asOf, asOfPoints, policy, maxDays)
	if err != nil {
		return nil, err
	}
	resp.Data = filled
	return resp, nil
}

func (s *FundamentalsService) queryEventSeries(ctx context.Context, market, symbol, factor string, from, to, asOf time.Time) ([]dto.FundamentalSeriesPoint, error) {
	rows, err := s.repo.Query(ctx, chquery.FundamentalSeriesEventQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("factor", factor),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("from", from.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("to", to.UTC().Format(time.RFC3339Nano)),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental event series: %w", err)
	}
	defer rows.Close()
	out := []dto.FundamentalSeriesPoint{}
	for rows.Next() {
		var p dto.FundamentalSeriesPoint
		var rev uint32
		if err := rows.Scan(&p.EventTS, &p.KnownAt, &p.Value, &p.Source, &rev); err != nil {
			return nil, fmt.Errorf("scan fundamental event series: %w", err)
		}
		p.Revision = int(rev)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *FundamentalsService) queryAsOfSeries(ctx context.Context, market, symbol, factor string, from, to, asOf time.Time) ([]dto.FundamentalSeriesPoint, error) {
	rows, err := s.repo.Query(ctx, chquery.FundamentalSeriesAsOfQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("factor", factor),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("from", from.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("to", to.UTC().Format(time.RFC3339Nano)),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental as_of series: %w", err)
	}
	defer rows.Close()
	out := []dto.FundamentalSeriesPoint{}
	for rows.Next() {
		var p dto.FundamentalSeriesPoint
		if err := rows.Scan(&p.EventTS, &p.KnownAt, &p.Value, &p.Source); err != nil {
			return nil, fmt.Errorf("scan fundamental as_of series: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ----- Snapshot -----

// QuerySnapshot returns the latest known value per factor for one symbol.
func (s *FundamentalsService) QuerySnapshot(ctx context.Context, req dto.FundamentalSnapshotRequest) (*dto.FundamentalSnapshotResponse, error) {
	market, symbol, _, err := normalizeFundamentalKey(req.Market, req.Symbol, "x") // dummy factor for shared validator
	if err != nil {
		return nil, err
	}
	storageSymbol := resolveFundamentalStorageSymbol(market, symbol, "")
	asOf, err := resolveAsOf(req.AsOf, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	factors := splitFundamentalFactorSelection(normalizeStringList(req.Factors))

	out := &dto.FundamentalSnapshotResponse{Market: market, Symbol: symbol, AsOf: asOf, Data: []dto.FundamentalSnapshotEntry{}}
	shouldQueryBase := len(req.Factors) == 0 || len(factors.base) > 0
	if shouldQueryBase {
		rows, err := s.repo.Query(ctx, chquery.FundamentalSnapshotQuery(),
			clickhouse.Named("market", market),
			clickhouse.Named("symbol", storageSymbol),
			clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
			clickhouse.Named("factors", factors.base),
		)
		if err != nil {
			return nil, fmt.Errorf("query fundamental snapshot: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var e dto.FundamentalSnapshotEntry
			if err := rows.Scan(&e.Factor, &e.EventTS, &e.KnownAt, &e.Value, &e.Source); err != nil {
				return nil, fmt.Errorf("scan fundamental snapshot: %w", err)
			}
			out.Data = append(out.Data, e)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(req.Factors) == 0 || factors.includePE {
		entry, handled, err := s.virtuals.querySnapshot(ctx, market, symbol, virtualFundamentalFactorPE, asOf)
		if err != nil {
			return nil, err
		}
		if handled && entry != nil {
			upsertFundamentalSnapshotEntry(&out.Data, *entry)
		}
	}
	if len(req.Factors) == 0 || factors.includePE10Live {
		entry, handled, err := s.virtuals.querySnapshot(ctx, market, symbol, virtualFundamentalFactorPE10Live, asOf)
		if err != nil {
			return nil, err
		}
		if handled && entry != nil {
			upsertFundamentalSnapshotEntry(&out.Data, *entry)
		}
	}
	if market == "us-stocks" {
		out.Data, err = s.revalueSnapshotEntries(ctx, symbol, asOf, out.Data)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out.Data, func(i, j int) bool {
		return out.Data[i].Factor < out.Data[j].Factor
	})
	return out, nil
}

// ----- Panel -----

// QueryPanel returns the latest known values across many symbols for the
// supplied factor list (or all factors when empty).
func (s *FundamentalsService) QueryPanel(ctx context.Context, req dto.FundamentalPanelRequest) (*dto.FundamentalPanelResponse, error) {
	market := strings.TrimSpace(req.Market)
	if err := validateFundamentalMarket(market); err != nil {
		return nil, err
	}
	symbols := normalizeStringList(req.Symbols)
	if len(symbols) == 0 {
		return nil, dto.NewValidationError("symbols must be non-empty")
	}
	if len(symbols) > 500 {
		return nil, dto.NewValidationError("symbols list capped at 500 per request")
	}
	asOf, err := resolveAsOf(req.AsOf, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	factors := splitFundamentalFactorSelection(normalizeStringList(req.Factors))

	out := &dto.FundamentalPanelResponse{Market: market, AsOf: asOf, Data: []dto.FundamentalPanelRow{}}
	shouldQueryBase := len(req.Factors) == 0 || len(factors.base) > 0
	if shouldQueryBase {
		rows, err := s.repo.Query(ctx, chquery.FundamentalPanelQuery(),
			clickhouse.Named("market", market),
			clickhouse.Named("symbols", symbols),
			clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
			clickhouse.Named("factors", factors.base),
		)
		if err != nil {
			return nil, fmt.Errorf("query fundamental panel: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r dto.FundamentalPanelRow
			if err := rows.Scan(&r.Symbol, &r.Factor, &r.EventTS, &r.KnownAt, &r.Value); err != nil {
				return nil, fmt.Errorf("scan fundamental panel: %w", err)
			}
			out.Data = append(out.Data, r)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(req.Factors) == 0 || factors.includePE {
		rows, err := s.virtuals.queryPanelRows(ctx, market, symbols, virtualFundamentalFactorPE, asOf)
		if err != nil {
			return nil, err
		}
		upsertFundamentalPanelRows(&out.Data, rows)
	}
	if len(req.Factors) == 0 || factors.includePE10Live {
		rows, err := s.virtuals.queryPanelRows(ctx, market, symbols, virtualFundamentalFactorPE10Live, asOf)
		if err != nil {
			return nil, err
		}
		upsertFundamentalPanelRows(&out.Data, rows)
	}
	if market == "us-stocks" {
		out.Data, err = s.revaluePanelRows(ctx, asOf, out.Data)
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out.Data, func(i, j int) bool {
		if out.Data[i].Symbol == out.Data[j].Symbol {
			return out.Data[i].Factor < out.Data[j].Factor
		}
		return out.Data[i].Symbol < out.Data[j].Symbol
	})
	return out, nil
}

func upsertFundamentalSnapshotEntry(entries *[]dto.FundamentalSnapshotEntry, entry dto.FundamentalSnapshotEntry) {
	for index := range *entries {
		if strings.EqualFold((*entries)[index].Factor, entry.Factor) {
			(*entries)[index] = entry
			return
		}
	}
	*entries = append(*entries, entry)
}

func upsertFundamentalPanelRows(existing *[]dto.FundamentalPanelRow, rows []dto.FundamentalPanelRow) {
	for _, row := range rows {
		updated := false
		for index := range *existing {
			if strings.EqualFold((*existing)[index].Symbol, row.Symbol) && strings.EqualFold((*existing)[index].Factor, row.Factor) {
				(*existing)[index] = row
				updated = true
				break
			}
		}
		if !updated {
			*existing = append(*existing, row)
		}
	}
}

// ----- Freshness -----

// QueryFreshness reports the most recent known_at per factor with optional
// SLA evaluation derived from the catalog.
func (s *FundamentalsService) QueryFreshness(ctx context.Context, req dto.FundamentalFreshnessRequest) (*dto.FundamentalFreshnessResponse, error) {
	market := strings.TrimSpace(req.Market)
	if market != "" {
		if err := validateFundamentalMarket(market); err != nil {
			return nil, err
		}
	}
	factor := strings.TrimSpace(req.Factor)

	// SLA + catalog lookup table
	slaByKey := map[string]int{}
	catalog, err := s.ListFactors(ctx, dto.FundamentalFactorCatalogRequest{Market: market})
	if err == nil {
		for _, e := range catalog.Data {
			if e.SLAHours > 0 {
				slaByKey[e.Market+"|"+e.FactorCode] = e.SLAHours
			}
		}
	}

	rows, err := s.repo.Query(ctx, chquery.FundamentalLatestKnownAtQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("factor", factor),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental freshness: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	out := &dto.FundamentalFreshnessResponse{Data: []dto.FundamentalFreshnessEntry{}}
	for rows.Next() {
		var e dto.FundamentalFreshnessEntry
		if err := rows.Scan(&e.Market, &e.Factor, &e.LastKnownAt); err != nil {
			return nil, fmt.Errorf("scan fundamental freshness: %w", err)
		}
		if sla, ok := slaByKey[e.Market+"|"+e.Factor]; ok {
			hours := now.Sub(e.LastKnownAt.UTC()).Hours()
			stale := hours > float64(sla)
			e.SLAHours = sla
			e.StaleHours = &hours
			e.Stale = &stale
		}
		out.Data = append(out.Data, e)
	}
	return out, rows.Err()
}

// ----- Helpers -----

func (s *FundamentalsService) lookupFillPolicy(ctx context.Context, market, factor string) (string, int, error) {
	rows, err := s.repo.Query(ctx, chquery.FundamentalFactorCatalogQuery(),
		clickhouse.Named("market", market),
	)
	if err != nil {
		return "", 0, fmt.Errorf("query fundamental fill policy catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			e           dto.FundamentalFactorCatalogEntry
			fillMaxDays uint16
			pit, active uint8
			sla         uint32
		)
		if err := rows.Scan(
			&e.Market, &e.FactorCode, &e.DisplayName, &e.Description,
			&e.ValueType, &e.Unit, &e.PreferredFrequency, &e.FillPolicy,
			&fillMaxDays, &pit, &e.Source, &active, &sla,
			&e.Metadata, &e.UpdatedAt,
		); err != nil {
			return "", 0, fmt.Errorf("scan fundamental fill policy catalog: %w", err)
		}
		if e.FactorCode == factor {
			return e.FillPolicy, int(fillMaxDays), nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, fmt.Errorf("iterate fundamental fill policy catalog: %w", err)
	}
	return fundamentalFillForwardFill, 0, nil
}

func (s *FundamentalsService) buildFilledSeries(ctx context.Context, market, symbol, factor string, from, to, asOf time.Time, asOfPoints []dto.FundamentalSeriesPoint, policy string, maxDays int) ([]dto.FundamentalSeriesPoint, error) {
	points, err := s.withSeriesSeed(ctx, market, symbol, factor, from, asOf, asOfPoints)
	if err != nil {
		return nil, err
	}
	if len(points) == 0 || policy == fundamentalFillEventOnly {
		return asOfPoints, nil
	}
	grid, prices, err := s.resolveFillGrid(ctx, market, symbol, from, to)
	if err != nil {
		return nil, err
	}
	if len(grid) == 0 {
		return asOfPoints, nil
	}
	var transform func(time.Time, dto.FundamentalSeriesPoint) float64
	if market == "us-stocks" && defaultUSStockPriceDerivedFundamentalFactor(factor) {
		denominators, err := s.loadPriceDerivedDenominators(ctx, symbol, factor, points, grid)
		if err != nil {
			return nil, err
		}
		transform = func(gridTS time.Time, point dto.FundamentalSeriesPoint) float64 {
			return revaluePriceDerivedFundamental(factor, gridTS, point, denominators, prices)
		}
	}
	return buildFilledFundamentalSeries(grid, points, policy, maxDays, transform), nil
}

func (s *FundamentalsService) withSeriesSeed(ctx context.Context, market, symbol, factor string, from, asOf time.Time, points []dto.FundamentalSeriesPoint) ([]dto.FundamentalSeriesPoint, error) {
	out := append([]dto.FundamentalSeriesPoint(nil), points...)
	seed, err := s.querySnapshotEntry(ctx, market, resolveFundamentalStorageSymbol(market, symbol, factor), factor, from)
	if err != nil {
		return nil, err
	}
	if seed == nil || seed.EventTS.After(from) || seed.EventTS.Equal(from) {
		return out, nil
	}
	for _, point := range out {
		if point.EventTS.Equal(seed.EventTS) && point.KnownAt.Equal(seed.KnownAt) {
			return out, nil
		}
	}
	seedPoint := dto.FundamentalSeriesPoint{
		EventTS: seed.EventTS,
		KnownAt: seed.KnownAt,
		Value:   seed.Value,
		Source:  seed.Source,
	}
	out = append([]dto.FundamentalSeriesPoint{seedPoint}, out...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].EventTS.Equal(out[j].EventTS) {
			return out[i].KnownAt.Before(out[j].KnownAt)
		}
		return out[i].EventTS.Before(out[j].EventTS)
	})
	_ = asOf
	return out, nil
}

func (s *FundamentalsService) resolveFillGrid(ctx context.Context, market, symbol string, from, to time.Time) ([]time.Time, map[time.Time]float64, error) {
	if market == "us-stocks" {
		series, err := NewUSStocksService(s.repo).loadUSStockDailyCloses(ctx, symbol, from, to)
		if err != nil {
			return nil, nil, err
		}
		grid := make([]time.Time, 0, len(series))
		prices := make(map[time.Time]float64, len(series))
		for _, point := range series {
			ts := point.Timestamp.UTC()
			grid = append(grid, ts)
			prices[ts] = point.Close
		}
		return grid, prices, nil
	}
	grid := make([]time.Time, 0, int(to.Sub(from).Hours()/24)+1)
	prices := map[time.Time]float64{}
	for day := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC); day.Before(to); day = day.AddDate(0, 0, 1) {
		grid = append(grid, day)
	}
	return grid, prices, nil
}

func (s *FundamentalsService) loadPriceDerivedDenominators(ctx context.Context, symbol, factor string, points []dto.FundamentalSeriesPoint, grid []time.Time) (map[string]float64, error) {
	denominators := map[string]float64{}
	if len(points) == 0 {
		return denominators, nil
	}
	minTS := points[0].EventTS.UTC()
	maxTS := points[0].EventTS.UTC()
	for _, point := range points[1:] {
		if point.EventTS.Before(minTS) {
			minTS = point.EventTS.UTC()
		}
		if point.EventTS.After(maxTS) {
			maxTS = point.EventTS.UTC()
		}
	}
	if len(grid) > 0 && grid[len(grid)-1].After(maxTS) {
		maxTS = grid[len(grid)-1].UTC()
	}
	series, err := NewUSStocksService(s.repo).loadUSStockDailyCloses(ctx, symbol, minTS.AddDate(0, 0, -14), maxTS.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	for _, point := range points {
		if point.Value == 0 {
			continue
		}
		if point.Source == "fmp_statement_derived_v2" {
			denominator, ok, err := s.loadFMPStatementDenominator(ctx, symbol, factor, point.EventTS)
			if err != nil {
				return nil, err
			}
			if ok {
				denominators[fundamentalObservationKey(factor, point.EventTS)] = denominator
				continue
			}
		}
		if closePrice, ok := series.closeOnOrBefore(point.EventTS); ok && closePrice != 0 {
			denominators[fundamentalObservationKey(factor, point.EventTS)] = closePrice / point.Value
		}
	}
	return denominators, nil
}

func (s *FundamentalsService) loadFMPStatementDenominator(ctx context.Context, symbol, factor string, eventTS time.Time) (float64, bool, error) {
	switch factor {
	case "pe":
		return s.loadFMPStatementTTMEPS(ctx, symbol, eventTS)
	case "pb":
		return s.loadFMPStatementBookValuePerShare(ctx, symbol, eventTS)
	default:
		return 0, false, nil
	}
}

func (s *FundamentalsService) loadFMPStatementTTMEPS(ctx context.Context, symbol string, eventTS time.Time) (float64, bool, error) {
	rows, err := s.repo.Query(ctx, `SELECT
	date,
	argMax(eps, (accepted_date, revision, ingested_at)) AS eps,
	argMax(net_income, (accepted_date, revision, ingested_at)) AS net_income,
	argMax(weighted_average_shs_out, (accepted_date, revision, ingested_at)) AS shares
FROM fmp_income_statement_quarterly
WHERE symbol = {symbol:String}
  AND source = 'fmp'
  AND date <= {event_date:Date}
GROUP BY date
ORDER BY date DESC
LIMIT 4`,
		clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(symbol))),
		clickhouse.Named("event_date", eventTS.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return 0, false, fmt.Errorf("query FMP statement TTM EPS denominator: %w", err)
	}
	defer rows.Close()
	total := 0.0
	count := 0
	for rows.Next() {
		var date time.Time
		var eps, netIncome, shares float64
		if err := rows.Scan(&date, &eps, &netIncome, &shares); err != nil {
			return 0, false, fmt.Errorf("scan FMP statement TTM EPS denominator: %w", err)
		}
		value := eps
		if !validStatementDenominatorComponent(value) {
			if shares <= 0 || !validStatementDenominatorComponent(netIncome) {
				if eps == 0 && !math.IsNaN(eps) && !math.IsInf(eps, 0) {
					value = 0
				} else {
					return 0, false, nil
				}
			} else {
				value = netIncome / shares
			}
		}
		if value != 0 && !validStatementDenominatorComponent(value) {
			return 0, false, nil
		}
		total += value
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if count != 4 || !validStatementDenominatorComponent(total) {
		return 0, false, nil
	}
	return total, true, nil
}

func (s *FundamentalsService) loadFMPStatementBookValuePerShare(ctx context.Context, symbol string, eventTS time.Time) (float64, bool, error) {
	rows, err := s.repo.Query(ctx, `WITH
income AS (
	SELECT
		date,
		argMax(weighted_average_shs_out, (accepted_date, revision, ingested_at)) AS shares
	FROM fmp_income_statement_quarterly
	WHERE symbol = {symbol:String}
	  AND source = 'fmp'
	  AND date = {event_date:Date}
	GROUP BY date
),
balance AS (
	SELECT
		date,
		argMax(total_stockholders_equity, (accepted_date, revision, ingested_at)) AS stockholders_equity,
		argMax(total_equity, (accepted_date, revision, ingested_at)) AS total_equity,
		argMax(total_assets, (accepted_date, revision, ingested_at)) AS total_assets,
		argMax(total_liabilities, (accepted_date, revision, ingested_at)) AS total_liabilities
	FROM fmp_balance_sheet_quarterly
	WHERE symbol = {symbol:String}
	  AND source = 'fmp'
	  AND date = {event_date:Date}
	GROUP BY date
)
SELECT income.shares, balance.stockholders_equity, balance.total_equity, balance.total_assets, balance.total_liabilities
FROM income INNER JOIN balance USING (date)
LIMIT 1`,
		clickhouse.Named("symbol", strings.ToUpper(strings.TrimSpace(symbol))),
		clickhouse.Named("event_date", eventTS.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return 0, false, fmt.Errorf("query FMP statement PB denominator: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	var shares, stockholdersEquity, totalEquity, totalAssets, totalLiabilities float64
	if err := rows.Scan(&shares, &stockholdersEquity, &totalEquity, &totalAssets, &totalLiabilities); err != nil {
		return 0, false, fmt.Errorf("scan FMP statement PB denominator: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	equity := stockholdersEquity
	if equity == 0 {
		equity = totalEquity
	}
	if equity == 0 {
		equity = totalAssets - totalLiabilities
	}
	if shares <= 0 || equity == 0 {
		return 0, false, nil
	}
	value := equity / shares
	if !validStatementDenominatorComponent(value) {
		return 0, false, nil
	}
	return value, true, nil
}

func validStatementDenominatorComponent(value float64) bool {
	return value != 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *FundamentalsService) querySnapshotEntry(ctx context.Context, market, symbol, factor string, asOf time.Time) (*dto.FundamentalSnapshotEntry, error) {
	rows, err := s.repo.Query(ctx, chquery.FundamentalSnapshotQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("factors", []string{factor}),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental snapshot entry: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var entry dto.FundamentalSnapshotEntry
		if err := rows.Scan(&entry.Factor, &entry.EventTS, &entry.KnownAt, &entry.Value, &entry.Source); err != nil {
			return nil, fmt.Errorf("scan fundamental snapshot entry: %w", err)
		}
		return &entry, nil
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *FundamentalsService) revalueSnapshotEntries(ctx context.Context, symbol string, asOf time.Time, entries []dto.FundamentalSnapshotEntry) ([]dto.FundamentalSnapshotEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}
	minEventTS := time.Time{}
	hasPriceDerived := false
	for _, entry := range entries {
		if !defaultUSStockPriceDerivedFundamentalFactor(entry.Factor) {
			continue
		}
		hasPriceDerived = true
		if minEventTS.IsZero() || entry.EventTS.Before(minEventTS) {
			minEventTS = entry.EventTS.UTC()
		}
	}
	if !hasPriceDerived {
		return entries, nil
	}
	prices, err := NewUSStocksService(s.repo).loadUSStockDailyCloses(ctx, symbol, minEventTS.AddDate(0, 0, -14), asOf.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	currentPrice, ok := prices.closeOnOrBefore(asOf)
	if !ok || currentPrice == 0 {
		return entries, nil
	}
	for index, entry := range entries {
		if !defaultUSStockPriceDerivedFundamentalFactor(entry.Factor) || entry.Value == 0 {
			continue
		}
		if entry.Source == "fmp_statement_derived_v2" {
			denominator, ok, err := s.loadFMPStatementDenominator(ctx, symbol, entry.Factor, entry.EventTS)
			if err != nil {
				return nil, err
			}
			if ok {
				entries[index].Value = currentPrice / denominator
				continue
			}
		}
		if closePrice, ok := prices.closeOnOrBefore(entry.EventTS); ok && closePrice != 0 {
			entries[index].Value = currentPrice / (closePrice / entry.Value)
		}
	}
	return entries, nil
}

func (s *FundamentalsService) revaluePanelRows(ctx context.Context, asOf time.Time, rows []dto.FundamentalPanelRow) ([]dto.FundamentalPanelRow, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	bySymbol := map[string][]int{}
	for index, row := range rows {
		if !defaultUSStockPriceDerivedFundamentalFactor(row.Factor) {
			continue
		}
		bySymbol[row.Symbol] = append(bySymbol[row.Symbol], index)
	}
	for symbol, indexes := range bySymbol {
		minEventTS := rows[indexes[0]].EventTS.UTC()
		for _, index := range indexes[1:] {
			if rows[index].EventTS.Before(minEventTS) {
				minEventTS = rows[index].EventTS.UTC()
			}
		}
		prices, err := NewUSStocksService(s.repo).loadUSStockDailyCloses(ctx, symbol, minEventTS.AddDate(0, 0, -14), asOf.AddDate(0, 0, 1))
		if err != nil {
			return nil, err
		}
		currentPrice, ok := prices.closeOnOrBefore(asOf)
		if !ok || currentPrice == 0 {
			continue
		}
		for _, index := range indexes {
			if rows[index].Value == 0 {
				continue
			}
			if closePrice, ok := prices.closeOnOrBefore(rows[index].EventTS); ok && closePrice != 0 {
				rows[index].Value = currentPrice / (closePrice / rows[index].Value)
			}
		}
	}
	return rows, nil
}

func buildFilledFundamentalSeries(grid []time.Time, points []dto.FundamentalSeriesPoint, policy string, maxDays int, valueTransform func(time.Time, dto.FundamentalSeriesPoint) float64) []dto.FundamentalSeriesPoint {
	if len(points) == 0 {
		return nil
	}
	if len(grid) == 0 || policy == fundamentalFillEventOnly {
		return append([]dto.FundamentalSeriesPoint(nil), points...)
	}
	out := make([]dto.FundamentalSeriesPoint, 0, len(grid))
	currentIndex := -1
	for _, gridTS := range grid {
		for currentIndex+1 < len(points) && !points[currentIndex+1].EventTS.After(gridTS) {
			currentIndex++
		}
		if currentIndex < 0 {
			continue
		}
		current := points[currentIndex]
		if policy == fundamentalFillForwardLimited && maxDays > 0 {
			if gridTS.Sub(current.EventTS.UTC()) > time.Duration(maxDays)*24*time.Hour {
				continue
			}
		}
		filledPoint := dto.FundamentalSeriesPoint{
			EventTS: gridTS.UTC(),
			KnownAt: current.KnownAt,
			Value:   current.Value,
			Source:  current.Source,
			Filled:  !current.EventTS.Equal(gridTS),
		}
		if valueTransform != nil {
			filledPoint.Value = valueTransform(gridTS, current)
		}
		out = append(out, filledPoint)
	}
	return out
}

func revaluePriceDerivedFundamental(factor string, gridTS time.Time, point dto.FundamentalSeriesPoint, denominators map[string]float64, prices map[time.Time]float64) float64 {
	if !defaultUSStockPriceDerivedFundamentalFactor(factor) {
		return point.Value
	}
	denominator, ok := denominators[fundamentalObservationKey(factor, point.EventTS)]
	if !ok || denominator == 0 {
		return point.Value
	}
	price, ok := prices[gridTS.UTC()]
	if !ok || price == 0 {
		return point.Value
	}
	return price / denominator
}

func normalizeFundamentalKey(market, symbol, factor string) (string, string, string, error) {
	market = strings.TrimSpace(market)
	symbol = strings.TrimSpace(symbol)
	factor = strings.TrimSpace(factor)
	if err := validateFundamentalMarket(market); err != nil {
		return "", "", "", err
	}
	if symbol == "" {
		return "", "", "", dto.NewValidationError("symbol is required")
	}
	if factor == "" {
		return "", "", "", dto.NewValidationError("factor is required")
	}
	return market, symbol, factor, nil
}

func resolveFundamentalStorageSymbol(market, symbol, factor string) string {
	if !strings.EqualFold(market, "us-stocks") {
		return symbol
	}
	if strings.EqualFold(factor, virtualFundamentalFactorPE10Live) {
		return symbol
	}
	switch strings.ToUpper(strings.TrimSpace(symbol)) {
	case "SPX":
		return "SPY"
	case "NDX":
		return "QQQ"
	default:
		return symbol
	}
}

func validateFundamentalMarket(market string) error {
	if market == "" {
		return dto.NewValidationError("market is required")
	}
	if !chquery.FundamentalsSupportedMarkets[market] {
		return dto.NewValidationError("unsupported fundamentals market %q", market)
	}
	return nil
}

func resolveAsOf(s string, fallback time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback.UTC(), nil
	}
	t, err := parseFundamentalTime(s)
	if err != nil {
		return time.Time{}, dto.NewValidationError("invalid as_of: %v", err)
	}
	return t, nil
}

func parseFundamentalTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339 or YYYY-MM-DD, got %q", s)
}

func normalizeStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		// allow comma-separated form fields too
		for _, part := range strings.Split(raw, ",") {
			v := strings.TrimSpace(part)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
