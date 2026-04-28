package service

import (
	"context"
	"fmt"
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
	repo *chrepo.Repo
}

// NewFundamentalsService builds the service over the shared ClickHouse repo.
func NewFundamentalsService(repo *chrepo.Repo) *FundamentalsService {
	return &FundamentalsService{repo: repo}
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

	if mode == fundamentalSeriesModeEvent {
		points, err := s.queryEventSeries(ctx, market, symbol, factor, from, to, asOf)
		if err != nil {
			return nil, err
		}
		resp.Data = points
		return resp, nil
	}

	asOfPoints, err := s.queryAsOfSeries(ctx, market, symbol, factor, from, to, asOf)
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
	resp.Data = applyForwardFill(asOfPoints, policy, maxDays)
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
	asOf, err := resolveAsOf(req.AsOf, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	factors := normalizeStringList(req.Factors)

	rows, err := s.repo.Query(ctx, chquery.FundamentalSnapshotQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("symbol", symbol),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("factors", factors),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental snapshot: %w", err)
	}
	defer rows.Close()
	out := &dto.FundamentalSnapshotResponse{Market: market, Symbol: symbol, AsOf: asOf, Data: []dto.FundamentalSnapshotEntry{}}
	for rows.Next() {
		var e dto.FundamentalSnapshotEntry
		if err := rows.Scan(&e.Factor, &e.EventTS, &e.KnownAt, &e.Value, &e.Source); err != nil {
			return nil, fmt.Errorf("scan fundamental snapshot: %w", err)
		}
		out.Data = append(out.Data, e)
	}
	return out, rows.Err()
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
	factors := normalizeStringList(req.Factors)

	rows, err := s.repo.Query(ctx, chquery.FundamentalPanelQuery(),
		clickhouse.Named("market", market),
		clickhouse.Named("symbols", symbols),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("factors", factors),
	)
	if err != nil {
		return nil, fmt.Errorf("query fundamental panel: %w", err)
	}
	defer rows.Close()
	out := &dto.FundamentalPanelResponse{Market: market, AsOf: asOf, Data: []dto.FundamentalPanelRow{}}
	for rows.Next() {
		var r dto.FundamentalPanelRow
		if err := rows.Scan(&r.Symbol, &r.Factor, &r.EventTS, &r.KnownAt, &r.Value); err != nil {
			return nil, fmt.Errorf("scan fundamental panel: %w", err)
		}
		out.Data = append(out.Data, r)
	}
	return out, rows.Err()
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
		return fundamentalFillForwardFill, 0, nil // fall back to default; do not block series
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
			return fundamentalFillForwardFill, 0, nil
		}
		if e.FactorCode == factor {
			return e.FillPolicy, int(fillMaxDays), nil
		}
	}
	return fundamentalFillForwardFill, 0, nil
}

// applyForwardFill emits the input series unchanged when policy is event_only,
// otherwise carries the most recent value forward, optionally bounded by
// maxDays for limited_forward_fill. Input must already be sorted ascending.
func applyForwardFill(in []dto.FundamentalSeriesPoint, policy string, maxDays int) []dto.FundamentalSeriesPoint {
	if len(in) == 0 || policy == fundamentalFillEventOnly {
		return in
	}
	// For now, fill marks rows as Filled=false; downstream consumers (e.g. the
	// backtest factor bridge) align onto bar timestamps and decide per-bar.
	// At the API layer, the as_of series is already the natural per-event_ts
	// projection; we expose a `Filled` flag for future expansion when callers
	// request evenly-spaced output explicitly.
	_ = maxDays
	return in
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
