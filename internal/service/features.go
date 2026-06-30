package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	defaultFeatureLookbackDays = 252
	maxFeatureLookbackDays     = 2000
	featureSnapshotTable       = "feature_volatility_snapshot_daily"
	featureTermStructureTable  = "feature_term_structure_snapshot_daily"
	featureSkewTable           = "feature_skew_snapshot_daily"
	featureLiquidityTable      = "feature_liquidity_snapshot_daily"
	featureDailyPanelTable     = "feature_daily_panel_daily"
)

type FeaturePolicy struct {
	DefaultLookbackDays int
	MaxLookbackDays     int
	DefaultMaxDTE       int
	FallbackWindowDays  int
	USOptionsATMWindows [][2]float64
}

func DefaultFeaturePolicy() FeaturePolicy {
	return FeaturePolicy{
		DefaultLookbackDays: defaultFeatureLookbackDays,
		MaxLookbackDays:     maxFeatureLookbackDays,
		DefaultMaxDTE:       365,
		FallbackWindowDays:  7,
		USOptionsATMWindows: [][2]float64{{0.98, 1.02}, {0.97, 1.03}, {0.95, 1.05}},
	}
}

func (p FeaturePolicy) normalized() FeaturePolicy {
	defaults := DefaultFeaturePolicy()
	if p.DefaultLookbackDays <= 0 {
		p.DefaultLookbackDays = defaults.DefaultLookbackDays
	}
	if p.MaxLookbackDays <= 0 {
		p.MaxLookbackDays = defaults.MaxLookbackDays
	}
	if p.MaxLookbackDays < p.DefaultLookbackDays {
		p.MaxLookbackDays = p.DefaultLookbackDays
	}
	if p.DefaultMaxDTE <= 0 {
		p.DefaultMaxDTE = defaults.DefaultMaxDTE
	}
	if p.FallbackWindowDays <= 0 {
		p.FallbackWindowDays = defaults.FallbackWindowDays
	}
	if len(p.USOptionsATMWindows) == 0 {
		p.USOptionsATMWindows = defaults.USOptionsATMWindows
	}
	return p
}

type featurePoint struct {
	Date  time.Time
	Value float64
}

type featureBackfillScopeError struct {
	Stage string
	Err   error
}

func (e featureBackfillScopeError) Error() string {
	return e.Err.Error()
}

func (e featureBackfillScopeError) Unwrap() error {
	return e.Err
}

type featureScopeResult struct {
	Status               string
	RowsWritten          int
	ScopesReplaced       int
	VolatilityHistory    []dto.FeatureVolatilityHistoryRow
	HasVolatilityHistory bool
	LiquidityHistory     []dto.FeatureLiquidityHistoryRow
	HasLiquidityHistory  bool
	SurfaceAggregates    []usOptionsSurfaceAggregateRow
	HasSurfaceAggregates bool
	SurfaceSummary       map[string]featureSurfacePanelSummary
	HasSurfaceSummary    bool
}

type featureBackfillUnderlyingRequest struct {
	Market          string
	MarketIndex     int
	MarketCount     int
	Underlying      string
	UnderlyingIndex int
	UnderlyingCount int
}

type featureBackfillUnderlyingResult struct {
	Stats FeatureBackfillStats
	Err   error
}

type featureBackfillPanelInputs struct {
	VolatilityHistory    []dto.FeatureVolatilityHistoryRow
	HasVolatilityHistory bool
	LiquidityHistory     []dto.FeatureLiquidityHistoryRow
	HasLiquidityHistory  bool
	SurfaceSummary       map[string]featureSurfacePanelSummary
	HasSurfaceSummary    bool
}

type usOptionsSurfaceAggregateRow struct {
	AsOfDate      time.Time
	Expiration    time.Time
	DaysToExpiry  int
	ATMIV         *float64
	CallIV        *float64
	PutIV         *float64
	OTMCallIV     *float64
	OTMPutIV      *float64
	ContractCount int
}

type cryptoLiquidityAggregateRow struct {
	AsOfDate              time.Time
	Expiration            time.Time
	DaysToExpiry          int
	AvgBidClose           *float64
	AvgAskClose           *float64
	AvgMarkClose          *float64
	RelativeSpread        *float64
	OpenInterest          *float64
	TickCount             int
	Volume                int
	Transactions          int
	ContractCount         int
	ActiveContractCount   int
	TradableContractCount int
}

type featureSurfacePanelSummary struct {
	Expiration    *time.Time
	DaysToExpiry  *int
	ATMIV         *float64
	PutCallSkew   *float64
	ContractCount *int
}

type featureLiquidityPanelSummary struct {
	OpenInterest      *float64
	RelativeSpread    *float64
	TickCount         int
	Volume            int
	Transactions      int
	ContractCount     int
	ActiveContracts   int
	TradableContracts int
	ActivityRatio     *float64
	TradabilityRatio  *float64
}

// FeatureBackfillProgress describes one observable backfill step.
type FeatureBackfillProgress struct {
	Market          string
	MarketIndex     int
	MarketCount     int
	Underlying      string
	UnderlyingIndex int
	UnderlyingCount int
	Scope           string
	Stage           string
	Phase           string
	Outcome         string
	RowsWritten     int
	ScopesReplaced  int
	Error           string
	Elapsed         time.Duration
}

// FeatureBackfillOptions controls precomputed volatility snapshot generation.
type FeatureBackfillOptions struct {
	Markets         []string
	Underlyings     []string
	PriorityOrder   string
	ClickHouseDSN   string
	From            time.Time
	To              time.Time
	LookbackDays    int
	MinDaysToExpiry int
	MaxDaysToExpiry int
	Workers         int
	Replace         bool
	ContinueOnError bool
	Progress        func(FeatureBackfillProgress)
}

// FeatureBackfillFailure captures one failed backfill scope.
type FeatureBackfillFailure struct {
	Market     string
	Underlying string
	Stage      string
	Error      string
}

// FeatureBackfillStats summarizes one feature-store backfill run.
type FeatureBackfillStats struct {
	MarketsProcessed      int
	UnderlyingsConsidered int
	UnderlyingsWritten    int
	UnderlyingsSkipped    int
	UnderlyingsEmpty      int
	ScopesReplaced        int
	RowsWritten           int
	LookbackDays          int
	Failures              []FeatureBackfillFailure
}

// FeatureService exposes read-only derived feature APIs.
type FeatureService struct {
	repo   *chrepo.Repo
	policy FeaturePolicy
}

func NewFeatureService(repo *chrepo.Repo) *FeatureService {
	return &FeatureService{repo: repo, policy: DefaultFeaturePolicy()}
}

func (s *FeatureService) WithPolicy(policy FeaturePolicy) *FeatureService {
	if s == nil {
		return nil
	}
	s.policy = policy.normalized()
	return s
}

func (s *FeatureService) QueryVolatilitySnapshot(ctx context.Context, req dto.FeatureVolatilitySnapshotRequest) (*dto.FeatureVolatilitySnapshotResponse, error) {
	market, underlying, lookbackDays, err := s.normalizeFeatureRequest(req.Market, req.Underlying, req.LookbackDays)
	if err != nil {
		return nil, err
	}

	if precomputed, ok, err := s.queryPrecomputedVolatilitySnapshot(ctx, market, underlying, lookbackDays); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}

	history, priceSeries, ivSeries, err := s.computeVolatilityHistory(ctx, market, underlying, time.Time{}, time.Time{}, lookbackDays)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureVolatilitySnapshotResponse{
		Market:            market,
		Underlying:        underlying,
		LookbackDays:      lookbackDays,
		PriceObservations: len(priceSeries),
		IVObservations:    len(ivSeries),
	}
	if len(priceSeries) > 0 {
		priceAsOf := priceSeries[len(priceSeries)-1].Date.UTC()
		resp.PriceAsOf = &priceAsOf
	}
	if len(ivSeries) > 0 {
		ivAsOf := ivSeries[len(ivSeries)-1].Date.UTC()
		resp.IVAsOf = &ivAsOf
	}
	if len(history) == 0 {
		return resp, nil
	}
	last := history[len(history)-1]
	resp.PriceObservations = last.PriceObservations
	resp.IVObservations = last.IVObservations
	resp.HV10 = last.HV10
	resp.HV20 = last.HV20
	resp.HV30 = last.HV30
	resp.CurrentIV = last.CurrentIV
	resp.IVPercentile = last.IVPercentile
	resp.IVRank = last.IVRank
	if last.HV10 != nil || last.HV20 != nil || last.HV30 != nil {
		date := last.Date.UTC()
		resp.PriceAsOf = &date
	}
	if last.CurrentIV != nil || last.IVPercentile != nil || last.IVRank != nil {
		date := last.Date.UTC()
		resp.IVAsOf = &date
	}
	return resp, nil
}

func (s *FeatureService) QueryVolatilityHistory(ctx context.Context, req dto.FeatureVolatilityHistoryRequest) (*dto.FeatureVolatilityHistoryResponse, error) {
	market, underlying, lookbackDays, err := s.normalizeFeatureRequest(req.Market, req.Underlying, req.LookbackDays)
	if err != nil {
		return nil, err
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	if precomputed, ok, err := s.queryPrecomputedVolatilityHistory(ctx, market, underlying, fromT, toT, lookbackDays); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}

	history, _, _, err := s.computeVolatilityHistory(ctx, market, underlying, fromT, toT, lookbackDays)
	if err != nil {
		return nil, err
	}
	return &dto.FeatureVolatilityHistoryResponse{
		Market:       market,
		Underlying:   underlying,
		LookbackDays: lookbackDays,
		Data:         history,
	}, nil
}

func (s *FeatureService) QueryTermStructureSnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureTermStructureSnapshotResponse, error) {
	market, underlying, minDTE, maxDTE, err := normalizeFeatureSurfaceRequest(req)
	if err != nil {
		return nil, err
	}
	if market != "us-options" && market != "crypto-options" {
		return nil, dto.NewValidationError("unsupported term structure market %q", market)
	}
	if precomputed, ok, err := s.queryPrecomputedTermStructureSnapshot(ctx, market, underlying, minDTE, maxDTE); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}
	// On-the-fly computation is currently only available for US options.
	if market != "us-options" {
		return &dto.FeatureTermStructureSnapshotResponse{Market: market, Underlying: underlying, Data: []dto.FeatureTermStructureSnapshotRow{}}, nil
	}
	asOf, hasData, err := s.latestUSOptionsFeatureDate(ctx, underlying)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureTermStructureSnapshotResponse{Market: market, Underlying: underlying}
	if !hasData {
		return resp, nil
	}
	resp.AsOf = &asOf
	aggregates, err := s.queryUSOptionsSurfaceAggregates(ctx, underlying, time.Time{}, time.Time{}, asOf, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	resp.Data = buildTermStructureSnapshotRows(aggregates)
	return resp, nil
}

func (s *FeatureService) QuerySkewSnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureSkewSnapshotResponse, error) {
	market, underlying, minDTE, maxDTE, err := normalizeFeatureSurfaceRequest(req)
	if err != nil {
		return nil, err
	}
	if market != "us-options" && market != "crypto-options" {
		return nil, dto.NewValidationError("unsupported skew market %q", market)
	}
	if precomputed, ok, err := s.queryPrecomputedSkewSnapshot(ctx, market, underlying, minDTE, maxDTE); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}
	// On-the-fly computation is currently only available for US options.
	if market != "us-options" {
		return &dto.FeatureSkewSnapshotResponse{Market: market, Underlying: underlying, Data: []dto.FeatureSkewSnapshotRow{}}, nil
	}
	asOf, hasData, err := s.latestUSOptionsFeatureDate(ctx, underlying)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureSkewSnapshotResponse{Market: market, Underlying: underlying}
	if !hasData {
		return resp, nil
	}
	resp.AsOf = &asOf
	aggregates, err := s.queryUSOptionsSurfaceAggregates(ctx, underlying, time.Time{}, time.Time{}, asOf, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	resp.Data = buildSkewSnapshotRows(aggregates)
	return resp, nil
}

func (s *FeatureService) QueryLiquiditySnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureLiquiditySnapshotResponse, error) {
	market, underlying, minDTE, maxDTE, err := normalizeFeatureSurfaceRequest(req)
	if err != nil {
		return nil, err
	}
	if market != "crypto-options" && market != "us-options" {
		return nil, dto.NewValidationError("unsupported liquidity market %q", market)
	}
	if precomputed, ok, err := s.queryPrecomputedLiquiditySnapshot(ctx, market, underlying, minDTE, maxDTE); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}
	asOf, hasData, err := s.latestLiquidityFeatureDate(ctx, market, underlying)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureLiquiditySnapshotResponse{Market: market, Underlying: underlying}
	if !hasData {
		return resp, nil
	}
	resp.AsOf = &asOf
	aggregates, err := s.queryLiquidityAggregates(ctx, market, underlying, time.Time{}, time.Time{}, asOf, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	resp.Data = buildLiquiditySnapshotRows(aggregates)
	return resp, nil
}

func (s *FeatureService) QueryLiquidityHistory(ctx context.Context, req dto.FeatureLiquidityHistoryRequest) (*dto.FeatureLiquidityHistoryResponse, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if market != "crypto-options" && market != "us-options" {
		return nil, dto.NewValidationError("unsupported liquidity market %q", market)
	}
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	minDTE := req.MinDaysToExpiry
	maxDTE := req.MaxDaysToExpiry
	if minDTE < 0 {
		return nil, dto.NewValidationError("min_days_to_expiry must be >= 0")
	}
	if maxDTE <= 0 {
		maxDTE = 365
	}
	if maxDTE < minDTE {
		return nil, dto.NewValidationError("max_days_to_expiry must be >= min_days_to_expiry")
	}
	if precomputed, ok, err := s.queryPrecomputedLiquidityHistory(ctx, market, underlying, fromT, toT, int32(minDTE), int32(maxDTE)); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}
	aggregates, err := s.queryLiquidityAggregates(ctx, market, underlying, fromT, toT, time.Time{}, int32(minDTE), int32(maxDTE))
	if err != nil {
		return nil, err
	}
	return &dto.FeatureLiquidityHistoryResponse{
		Market:     market,
		Underlying: underlying,
		Data:       buildLiquidityHistoryRows(aggregates),
	}, nil
}

func (s *FeatureService) QueryEventWindowSnapshot(ctx context.Context, req dto.FeatureUnderlyingSnapshotRequest) (*dto.FeatureEventWindowSnapshotResponse, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}
	var (
		asOf    time.Time
		hasData bool
		err     error
	)
	switch market {
	case "us-options":
		asOf, hasData, err = s.latestUSOptionsFeatureDate(ctx, underlying)
	case "us-stocks":
		asOf, hasData, err = s.latestUSStocksFeatureDate(ctx, underlying)
	default:
		return nil, dto.NewValidationError("unsupported event-window market %q", market)
	}
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureEventWindowSnapshotResponse{Market: market, Underlying: underlying}
	if !hasData {
		return resp, nil
	}
	resp.AsOfDate = &asOf
	if err := s.populateEventWindowFlags(ctx, resp, asOf); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *FeatureService) QueryEventWindowHistory(ctx context.Context, req dto.FeatureUnderlyingHistoryRequest) (*dto.FeatureEventWindowHistoryResponse, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if underlying == "" {
		return nil, dto.NewValidationError("underlying is required")
	}
	if market != "us-options" && market != "us-stocks" {
		return nil, dto.NewValidationError("unsupported event-window market %q", market)
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	rows, err := s.queryEventWindowHistoryRows(ctx, market, underlying, fromT, toT)
	if err != nil {
		return nil, err
	}
	return &dto.FeatureEventWindowHistoryResponse{Market: market, Underlying: underlying, Data: rows}, nil
}

func (s *FeatureService) QueryDailyFeaturePanel(ctx context.Context, req dto.FeatureDailyPanelRequest) (*dto.FeatureDailyPanelResponse, error) {
	market, underlying, lookbackDays, err := s.normalizeFeatureRequest(req.Market, req.Underlying, req.LookbackDays)
	if err != nil {
		return nil, err
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	if market != "crypto-options" && market != "us-options" {
		return nil, dto.NewValidationError("unsupported daily panel market %q", market)
	}
	minDTE, maxDTE, err := s.normalizeFeatureDTEBounds(req.MinDaysToExpiry, req.MaxDaysToExpiry)
	if err != nil {
		return nil, err
	}
	if precomputed, ok, err := s.queryPrecomputedDailyFeaturePanel(ctx, market, underlying, fromT, toT, lookbackDays, minDTE, maxDTE); err != nil {
		return nil, err
	} else if ok {
		return precomputed, nil
	}
	panelRows, err := s.buildDailyFeaturePanelRows(ctx, market, underlying, fromT, toT, lookbackDays, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	return &dto.FeatureDailyPanelResponse{Market: market, Underlying: underlying, LookbackDays: lookbackDays, Data: panelRows}, nil
}

// QueryTermStructureHistory returns a range of pre-computed term structure rows.
func (s *FeatureService) QueryTermStructureHistory(ctx context.Context, req dto.FeatureTermStructureHistoryRequest) (*dto.FeatureTermStructureHistoryResponse, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if market == "" || underlying == "" {
		return nil, dto.NewValidationError("market and underlying are required")
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	minDTE, maxDTE, err := s.normalizeFeatureDTEBounds(req.MinDaysToExpiry, req.MaxDaysToExpiry)
	if err != nil {
		return nil, err
	}

	exists, err := s.featureStoreRelationExists(ctx, featureTermStructureTable)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureTermStructureHistoryResponse{Market: market, Underlying: underlying, Data: make([]dto.FeatureTermStructureHistoryRow, 0)}
	if !exists {
		return resp, nil
	}

	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	expiration,
	days_to_expiry,
	atm_iv,
	call_iv,
	put_iv,
	contract_count
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date >= toDate({from:String})
  AND as_of_date <= toDate({to:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY as_of_date ASC, expiration ASC`, featureTermStructureTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", fromT.Format("2006-01-02")),
		clickhouse.Named("to", toT.Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, fmt.Errorf("query term structure history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			row           dto.FeatureTermStructureHistoryRow
			daysToExpiry  uint16
			contractCount uint32
		)
		if err := rows.Scan(&row.AsOfDate, &row.Expiration, &daysToExpiry, &row.ATMIV, &row.CallIV, &row.PutIV, &contractCount); err != nil {
			return nil, fmt.Errorf("scan term structure history row: %w", err)
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.ContractCount = int(contractCount)
		row.ATMIV = sanitizeF64Ptr(row.ATMIV)
		row.CallIV = sanitizeF64Ptr(row.CallIV)
		row.PutIV = sanitizeF64Ptr(row.PutIV)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate term structure history rows: %w", err)
	}
	return resp, nil
}

// QuerySkewHistory returns a range of pre-computed skew rows.
func (s *FeatureService) QuerySkewHistory(ctx context.Context, req dto.FeatureSkewHistoryRequest) (*dto.FeatureSkewHistoryResponse, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if market == "" || underlying == "" {
		return nil, dto.NewValidationError("market and underlying are required")
	}
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	minDTE, maxDTE, err := s.normalizeFeatureDTEBounds(req.MinDaysToExpiry, req.MaxDaysToExpiry)
	if err != nil {
		return nil, err
	}

	exists, err := s.featureStoreRelationExists(ctx, featureSkewTable)
	if err != nil {
		return nil, err
	}
	resp := &dto.FeatureSkewHistoryResponse{Market: market, Underlying: underlying, Data: make([]dto.FeatureSkewHistoryRow, 0)}
	if !exists {
		return resp, nil
	}

	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	expiration,
	days_to_expiry,
	otm_call_iv,
	otm_put_iv,
	put_call_skew,
	contract_count
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date >= toDate({from:String})
  AND as_of_date <= toDate({to:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY as_of_date ASC, expiration ASC`, featureSkewTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", fromT.Format("2006-01-02")),
		clickhouse.Named("to", toT.Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, fmt.Errorf("query skew history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			row           dto.FeatureSkewHistoryRow
			daysToExpiry  uint16
			contractCount uint32
		)
		if err := rows.Scan(&row.AsOfDate, &row.Expiration, &daysToExpiry, &row.OTMCallIV, &row.OTMPutIV, &row.PutCallSkew, &contractCount); err != nil {
			return nil, fmt.Errorf("scan skew history row: %w", err)
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.ContractCount = int(contractCount)
		row.OTMCallIV = sanitizeF64Ptr(row.OTMCallIV)
		row.OTMPutIV = sanitizeF64Ptr(row.OTMPutIV)
		row.PutCallSkew = sanitizeF64Ptr(row.PutCallSkew)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate skew history rows: %w", err)
	}
	return resp, nil
}

// BackfillFeatureSnapshots computes and stores all currently supported feature-store snapshots.
func (s *FeatureService) BackfillFeatureSnapshots(ctx context.Context, opts FeatureBackfillOptions) (FeatureBackfillStats, error) {
	lookbackDays := clamp(opts.LookbackDays, s.policy.DefaultLookbackDays, s.policy.MaxLookbackDays)
	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}
	markets, err := normalizeFeatureMarkets(opts.Markets)
	if err != nil {
		return FeatureBackfillStats{}, err
	}
	minDTE, maxDTE, err := s.normalizeFeatureDTEBounds(opts.MinDaysToExpiry, opts.MaxDaysToExpiry)
	if err != nil {
		return FeatureBackfillStats{}, err
	}
	if !opts.From.IsZero() && !opts.To.IsZero() && !opts.To.After(opts.From) {
		return FeatureBackfillStats{}, dto.NewValidationError("invalid time range: to must be after from")
	}
	stats := FeatureBackfillStats{MarketsProcessed: len(markets), LookbackDays: lookbackDays}

	for marketIndex, market := range markets {
		underlyings := normalizeUnderlyingList(opts.Underlyings)
		if len(underlyings) == 0 {
			underlyings, err = s.listFeatureUnderlyings(ctx, market)
			if err != nil {
				return FeatureBackfillStats{}, err
			}
		}
		underlyings, err = prioritizeFeatureUnderlyings(ctx, s.repo.Conn, market, underlyings, opts.PriorityOrder)
		if err != nil {
			return FeatureBackfillStats{}, err
		}
		emitFeatureBackfillProgress(opts, FeatureBackfillProgress{
			Market:          market,
			MarketIndex:     marketIndex + 1,
			MarketCount:     len(markets),
			UnderlyingCount: len(underlyings),
			Scope:           "market",
			Phase:           "start",
		})
		requests := make([]featureBackfillUnderlyingRequest, 0, len(underlyings))
		for underlyingIndex, underlying := range underlyings {
			requests = append(requests, featureBackfillUnderlyingRequest{
				Market:          market,
				MarketIndex:     marketIndex + 1,
				MarketCount:     len(markets),
				Underlying:      underlying,
				UnderlyingIndex: underlyingIndex + 1,
				UnderlyingCount: len(underlyings),
			})
		}
		marketOpts := opts
		if opts.Replace {
			clearedScopes, err := s.preclearFeatureBackfillScopes(ctx, market, underlyings, opts.From, opts.To, lookbackDays, minDTE, maxDTE)
			if err != nil {
				return stats, err
			}
			marketOpts.Replace = false
			stats.ScopesReplaced += clearedScopes
		}
		marketStats, err := s.backfillFeatureUnderlyingBatch(ctx, marketOpts, requests, lookbackDays, minDTE, maxDTE, workers)
		mergeFeatureBackfillStats(&stats, marketStats)
		if err != nil {
			return stats, err
		}
	}
	if len(stats.Failures) > 0 {
		return stats, fmt.Errorf("%d feature-store backfill scopes failed", len(stats.Failures))
	}
	return stats, nil
}

func (s *FeatureService) backfillFeatureUnderlyingBatch(ctx context.Context, opts FeatureBackfillOptions, requests []featureBackfillUnderlyingRequest, lookbackDays int, minDTE, maxDTE int32, workers int) (FeatureBackfillStats, error) {
	if len(requests) == 0 {
		return FeatureBackfillStats{}, nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(requests) {
		workers = len(requests)
	}
	if workers == 1 {
		var stats FeatureBackfillStats
		for _, req := range requests {
			result, err := s.backfillFeatureUnderlying(ctx, opts, req, lookbackDays, minDTE, maxDTE)
			mergeFeatureBackfillStats(&stats, result)
			if err != nil {
				return stats, err
			}
		}
		return stats, nil
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan featureBackfillUnderlyingRequest)
	results := make(chan featureBackfillUnderlyingResult)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerService := s
			var closeConn func()
			if strings.TrimSpace(opts.ClickHouseDSN) != "" {
				workerConn, err := usmarket.ConnectClickHouse(workerCtx, opts.ClickHouseDSN)
				if err != nil {
					select {
					case results <- featureBackfillUnderlyingResult{Err: fmt.Errorf("connect ClickHouse worker: %w", err)}:
					case <-workerCtx.Done():
					}
					cancel()
					return
				}
				workerService = NewFeatureService(chrepo.NewRepo(workerConn))
				closeConn = func() { _ = workerConn.Close() }
			}
			if closeConn != nil {
				defer closeConn()
			}
			for req := range jobs {
				stats, err := workerService.backfillFeatureUnderlying(workerCtx, opts, req, lookbackDays, minDTE, maxDTE)
				select {
				case results <- featureBackfillUnderlyingResult{Stats: stats, Err: err}:
				case <-workerCtx.Done():
					return
				}
				if err != nil && !opts.ContinueOnError {
					cancel()
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, req := range requests {
			select {
			case jobs <- req:
			case <-workerCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var (
		stats    FeatureBackfillStats
		firstErr error
	)
	for result := range results {
		mergeFeatureBackfillStats(&stats, result.Stats)
		if result.Err != nil && firstErr == nil {
			firstErr = result.Err
			if !opts.ContinueOnError {
				cancel()
			}
		}
	}
	if firstErr != nil {
		return stats, firstErr
	}
	return stats, ctx.Err()
}

func (s *FeatureService) backfillFeatureUnderlying(ctx context.Context, opts FeatureBackfillOptions, req featureBackfillUnderlyingRequest, lookbackDays int, minDTE, maxDTE int32) (FeatureBackfillStats, error) {
	stats := FeatureBackfillStats{UnderlyingsConsidered: 1}
	wroteAny := false
	allSkipped := true
	allEmpty := true
	panelInputs := featureBackfillPanelInputs{}
	emitFeatureBackfillProgress(opts, FeatureBackfillProgress{
		Market:          req.Market,
		MarketIndex:     req.MarketIndex,
		MarketCount:     req.MarketCount,
		Underlying:      req.Underlying,
		UnderlyingIndex: req.UnderlyingIndex,
		UnderlyingCount: req.UnderlyingCount,
		Scope:           "underlying",
		Phase:           "start",
	})

	runScope := func(scope string, fn func() (featureScopeResult, error)) (featureScopeResult, error) {
		emitFeatureBackfillProgress(opts, FeatureBackfillProgress{
			Market:          req.Market,
			MarketIndex:     req.MarketIndex,
			MarketCount:     req.MarketCount,
			Underlying:      req.Underlying,
			UnderlyingIndex: req.UnderlyingIndex,
			UnderlyingCount: req.UnderlyingCount,
			Scope:           scope,
			Phase:           "start",
		})
		startedAt := time.Now()
		result, runErr := fn()
		progress := FeatureBackfillProgress{
			Market:          req.Market,
			MarketIndex:     req.MarketIndex,
			MarketCount:     req.MarketCount,
			Underlying:      req.Underlying,
			UnderlyingIndex: req.UnderlyingIndex,
			UnderlyingCount: req.UnderlyingCount,
			Scope:           scope,
			Phase:           "end",
			Elapsed:         time.Since(startedAt),
			RowsWritten:     result.RowsWritten,
			ScopesReplaced:  result.ScopesReplaced,
			Outcome:         result.Status,
		}
		if runErr != nil {
			progress.Outcome = "failed"
			if scopeErr, ok := runErr.(featureBackfillScopeError); ok {
				progress.Stage = scopeErr.Stage
				progress.Error = scopeErr.Err.Error()
			} else {
				progress.Error = runErr.Error()
			}
		}
		emitFeatureBackfillProgress(opts, progress)
		return result, runErr
	}

	applyResult := func(result featureScopeResult) {
		applyFeatureScopeResult(&stats, &wroteAny, &allSkipped, &allEmpty, result)
		if result.HasVolatilityHistory {
			panelInputs.HasVolatilityHistory = true
			panelInputs.VolatilityHistory = result.VolatilityHistory
		}
		if result.HasLiquidityHistory {
			panelInputs.HasLiquidityHistory = true
			panelInputs.LiquidityHistory = result.LiquidityHistory
		}
		if result.HasSurfaceSummary {
			panelInputs.HasSurfaceSummary = true
			panelInputs.SurfaceSummary = result.SurfaceSummary
		}
	}
	handleErr := func(err error) error {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if opts.ContinueOnError {
			stats.Failures = append(stats.Failures, toFeatureBackfillFailure(req.Market, req.Underlying, err))
			return nil
		}
		return err
	}

	volatilityResult, err := runScope("volatility", func() (featureScopeResult, error) {
		return s.backfillVolatilityScope(ctx, req.Market, req.Underlying, opts.From, opts.To, lookbackDays, opts.Replace)
	})
	if err != nil {
		if err := handleErr(err); err != nil {
			return stats, err
		}
	} else {
		applyResult(volatilityResult)
	}

	if req.Market == "us-options" {
		surfaceResult, err := runScope("surface", func() (featureScopeResult, error) {
			if volatilityResult.HasSurfaceAggregates {
				return s.backfillUSOptionsSurfaceScope(ctx, req.Underlying, opts.From, opts.To, opts.Replace, volatilityResult.SurfaceAggregates)
			}
			return s.backfillUSOptionsSurfaceScope(ctx, req.Underlying, opts.From, opts.To, opts.Replace, nil)
		})
		if err != nil {
			if err := handleErr(err); err != nil {
				return stats, err
			}
		} else {
			applyResult(surfaceResult)
		}
		liquidityResult, err := runScope("liquidity", func() (featureScopeResult, error) {
			return s.backfillUSOptionsLiquidityScope(ctx, req.Underlying, opts.From, opts.To, opts.Replace)
		})
		if err != nil {
			if err := handleErr(err); err != nil {
				return stats, err
			}
		} else {
			applyResult(liquidityResult)
		}
		panelResult, err := runScope("daily-panel", func() (featureScopeResult, error) {
			return s.backfillDailyPanelScope(ctx, req.Market, req.Underlying, opts.From, opts.To, lookbackDays, minDTE, maxDTE, opts.Replace, panelInputs)
		})
		if err != nil {
			if err := handleErr(err); err != nil {
				return stats, err
			}
		} else {
			applyResult(panelResult)
		}
	}
	if req.Market == "crypto-options" {
		liquidityResult, err := runScope("liquidity", func() (featureScopeResult, error) {
			return s.backfillCryptoOptionsLiquidityScope(ctx, req.Underlying, opts.From, opts.To, opts.Replace)
		})
		if err != nil {
			if err := handleErr(err); err != nil {
				return stats, err
			}
		} else {
			applyResult(liquidityResult)
		}
		panelResult, err := runScope("daily-panel", func() (featureScopeResult, error) {
			return s.backfillDailyPanelScope(ctx, req.Market, req.Underlying, opts.From, opts.To, lookbackDays, minDTE, maxDTE, opts.Replace, panelInputs)
		})
		if err != nil {
			if err := handleErr(err); err != nil {
				return stats, err
			}
		} else {
			applyResult(panelResult)
		}
	}

	finalOutcome := "empty"
	if wroteAny {
		finalOutcome = "written"
	} else if len(stats.Failures) > 0 {
		finalOutcome = "failed"
	} else if allSkipped {
		finalOutcome = "skipped"
	}
	emitFeatureBackfillProgress(opts, FeatureBackfillProgress{
		Market:          req.Market,
		MarketIndex:     req.MarketIndex,
		MarketCount:     req.MarketCount,
		Underlying:      req.Underlying,
		UnderlyingIndex: req.UnderlyingIndex,
		UnderlyingCount: req.UnderlyingCount,
		Scope:           "underlying",
		Phase:           "end",
		Outcome:         finalOutcome,
	})

	if wroteAny {
		stats.UnderlyingsWritten++
	} else if len(stats.Failures) == 0 && allSkipped {
		stats.UnderlyingsSkipped++
	} else if len(stats.Failures) == 0 && allEmpty {
		stats.UnderlyingsEmpty++
	}
	return stats, nil
}

func (s *FeatureService) preclearFeatureBackfillScopes(ctx context.Context, market string, underlyings []string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32) (int, error) {
	if len(underlyings) == 0 {
		return 0, nil
	}
	deleteScope := func(table string, includeLookback bool, includeDTE bool) error {
		query := fmt.Sprintf(`DELETE FROM %s WHERE market = {market:String} AND underlying IN ({underlyings:Array(String)})`, table)
		args := []interface{}{
			clickhouse.Named("market", market),
			clickhouse.Named("underlyings", underlyings),
		}
		if includeLookback {
			query += ` AND lookback_days = {lookback_days:UInt16}`
			args = append(args, clickhouse.Named("lookback_days", uint16(lookbackDays)))
		}
		if includeDTE {
			query += ` AND min_days_to_expiry = {min_dte:Int32} AND max_days_to_expiry = {max_dte:Int32}`
			args = append(args,
				clickhouse.Named("min_dte", minDTE),
				clickhouse.Named("max_dte", maxDTE),
			)
		}
		if !from.IsZero() {
			query += ` AND as_of_date >= toDate({from:String})`
			args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
		}
		if !to.IsZero() {
			query += ` AND as_of_date < toDate({to:String})`
			args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
		}
		return s.repo.Exec(ctx, query, args...)
	}

	targets := []struct {
		table           string
		includeLookback bool
		includeDTE      bool
	}{
		{table: featureSnapshotTable, includeLookback: true},
		{table: featureLiquidityTable},
		{table: featureDailyPanelTable, includeLookback: true, includeDTE: true},
	}
	if market == "us-options" {
		targets = append(targets,
			struct {
				table           string
				includeLookback bool
				includeDTE      bool
			}{table: featureTermStructureTable},
			struct {
				table           string
				includeLookback bool
				includeDTE      bool
			}{table: featureSkewTable},
		)
	}
	for _, target := range targets {
		if err := deleteScope(target.table, target.includeLookback, target.includeDTE); err != nil {
			return 0, fmt.Errorf("preclear %s %s: %w", market, target.table, err)
		}
	}
	return len(underlyings) * featureBackfillReplaceScopeCount(market), nil
}

func featureBackfillReplaceScopeCount(market string) int {
	if market == "us-options" {
		return 5
	}
	return 3
}

func mergeFeatureBackfillStats(dst *FeatureBackfillStats, src FeatureBackfillStats) {
	dst.UnderlyingsConsidered += src.UnderlyingsConsidered
	dst.UnderlyingsWritten += src.UnderlyingsWritten
	dst.UnderlyingsSkipped += src.UnderlyingsSkipped
	dst.UnderlyingsEmpty += src.UnderlyingsEmpty
	dst.ScopesReplaced += src.ScopesReplaced
	dst.RowsWritten += src.RowsWritten
	dst.Failures = append(dst.Failures, src.Failures...)
}

func emitFeatureBackfillProgress(opts FeatureBackfillOptions, progress FeatureBackfillProgress) {
	if opts.Progress != nil {
		opts.Progress(progress)
	}
}

// BackfillVolatilitySnapshots computes and stores daily volatility feature rows.
func (s *FeatureService) BackfillVolatilitySnapshots(ctx context.Context, opts FeatureBackfillOptions) (FeatureBackfillStats, error) {
	lookbackDays := clamp(opts.LookbackDays, s.policy.DefaultLookbackDays, s.policy.MaxLookbackDays)
	markets, err := normalizeFeatureMarkets(opts.Markets)
	if err != nil {
		return FeatureBackfillStats{}, err
	}
	if !opts.From.IsZero() && !opts.To.IsZero() && !opts.To.After(opts.From) {
		return FeatureBackfillStats{}, dto.NewValidationError("invalid time range: to must be after from")
	}
	stats := FeatureBackfillStats{MarketsProcessed: len(markets), LookbackDays: lookbackDays}

	for _, market := range markets {
		underlyings := normalizeUnderlyingList(opts.Underlyings)
		if len(underlyings) == 0 {
			underlyings, err = s.listFeatureUnderlyings(ctx, market)
			if err != nil {
				return FeatureBackfillStats{}, err
			}
		}
		underlyings, err = prioritizeFeatureUnderlyings(ctx, s.repo.Conn, market, underlyings, opts.PriorityOrder)
		if err != nil {
			return FeatureBackfillStats{}, err
		}
		for _, underlying := range underlyings {
			stats.UnderlyingsConsidered++
			hasRows, err := s.precomputedVolatilityRowsExist(ctx, market, underlying, opts.From, opts.To, lookbackDays)
			if err != nil {
				if opts.ContinueOnError {
					stats.Failures = append(stats.Failures, FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: "check-existing", Error: err.Error()})
					continue
				}
				return stats, err
			}
			if hasRows && !opts.Replace {
				stats.UnderlyingsSkipped++
				continue
			}
			if hasRows && opts.Replace {
				if err := s.deletePrecomputedVolatilityRows(ctx, market, underlying, opts.From, opts.To, lookbackDays); err != nil {
					if opts.ContinueOnError {
						stats.Failures = append(stats.Failures, FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: "delete-scope", Error: err.Error()})
						continue
					}
					return stats, err
				}
				stats.ScopesReplaced++
			}

			history, _, _, err := s.computeVolatilityHistory(ctx, market, underlying, opts.From, opts.To, lookbackDays)
			if err != nil {
				if opts.ContinueOnError {
					stats.Failures = append(stats.Failures, FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: "compute-history", Error: err.Error()})
					continue
				}
				return stats, err
			}
			if len(history) == 0 {
				stats.UnderlyingsEmpty++
				continue
			}
			if err := s.insertPrecomputedVolatilityRows(ctx, market, underlying, lookbackDays, history); err != nil {
				if opts.ContinueOnError {
					stats.Failures = append(stats.Failures, FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: "insert-rows", Error: err.Error()})
					continue
				}
				return stats, err
			}
			stats.UnderlyingsWritten++
			stats.RowsWritten += len(history)
		}
	}
	if len(stats.Failures) > 0 {
		return stats, fmt.Errorf("%d feature-store backfill scopes failed", len(stats.Failures))
	}
	return stats, nil
}

func (s *FeatureService) backfillVolatilityScope(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, replace bool) (featureScopeResult, error) {
	replaced := 0
	if replace {
		if err := s.deletePrecomputedVolatilityRows(ctx, market, underlying, from, to, lookbackDays); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "delete-scope", Err: err}
		}
		replaced = 1
	} else {
		hasRows, err := s.precomputedVolatilityRowsExist(ctx, market, underlying, from, to, lookbackDays)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "check-existing", Err: err}
		}
		if hasRows {
			return featureScopeResult{Status: "skipped"}, nil
		}
	}
	history, surfaceAggregates, err := s.computeVolatilityHistoryForBackfill(ctx, market, underlying, from, to, lookbackDays)
	if err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "compute-history", Err: err}
	}
	if len(history) == 0 {
		result := featureScopeResult{Status: "empty", ScopesReplaced: replaced, HasVolatilityHistory: true}
		if market == "us-options" {
			result.SurfaceAggregates = surfaceAggregates
			result.HasSurfaceAggregates = true
			result.SurfaceSummary = summarizeUSOptionsSurfaceHistory(surfaceAggregates)
			result.HasSurfaceSummary = true
		}
		return result, nil
	}
	if err := s.insertPrecomputedVolatilityRows(ctx, market, underlying, lookbackDays, history); err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "insert-rows", Err: err}
	}
	result := featureScopeResult{Status: "written", RowsWritten: len(history), ScopesReplaced: replaced, VolatilityHistory: history, HasVolatilityHistory: true}
	if market == "us-options" {
		result.SurfaceAggregates = surfaceAggregates
		result.HasSurfaceAggregates = true
		result.SurfaceSummary = summarizeUSOptionsSurfaceHistory(surfaceAggregates)
		result.HasSurfaceSummary = true
	}
	return result, nil
}

func (s *FeatureService) backfillUSOptionsSurfaceScope(ctx context.Context, underlying string, from, to time.Time, replace bool, precomputed []usOptionsSurfaceAggregateRow) (featureScopeResult, error) {
	replaced := 0
	termHasRows := false
	skewHasRows := false
	if replace {
		if err := s.deletePrecomputedSurfaceRows(ctx, featureTermStructureTable, "us-options", underlying, from, to); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "term-structure-delete-scope", Err: err}
		}
		replaced++
		if err := s.deletePrecomputedSurfaceRows(ctx, featureSkewTable, "us-options", underlying, from, to); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "skew-delete-scope", Err: err}
		}
		replaced++
	} else {
		var err error
		termHasRows, err = s.precomputedSurfaceRowsExist(ctx, featureTermStructureTable, "us-options", underlying, from, to)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "term-structure-check-existing", Err: err}
		}
		skewHasRows, err = s.precomputedSurfaceRowsExist(ctx, featureSkewTable, "us-options", underlying, from, to)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "skew-check-existing", Err: err}
		}
		if termHasRows && skewHasRows {
			return featureScopeResult{Status: "skipped"}, nil
		}
	}
	aggregates := precomputed
	if aggregates == nil {
		var err error
		aggregates, err = s.queryUSOptionsSurfaceAggregates(ctx, underlying, from, to, time.Time{}, 0, 0)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "compute-surface", Err: err}
		}
	}
	surfaceSummary := summarizeUSOptionsSurfaceHistory(aggregates)
	if len(aggregates) == 0 {
		return featureScopeResult{Status: "empty", ScopesReplaced: replaced, SurfaceSummary: surfaceSummary, HasSurfaceSummary: true}, nil
	}
	termRows := aggregates
	skewRows := aggregates
	if !replace && termHasRows {
		termRows = nil
	}
	if !replace && skewHasRows {
		skewRows = nil
	}
	rowsWritten := 0
	if len(termRows) > 0 {
		if err := s.insertPrecomputedTermStructureRows(ctx, "us-options", underlying, termRows); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "term-structure-insert-rows", Err: err}
		}
		rowsWritten += len(termRows)
	}
	if len(skewRows) > 0 {
		if err := s.insertPrecomputedSkewRows(ctx, "us-options", underlying, skewRows); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "skew-insert-rows", Err: err}
		}
		rowsWritten += len(skewRows)
	}
	if rowsWritten == 0 {
		if termHasRows || skewHasRows {
			return featureScopeResult{Status: "skipped", ScopesReplaced: replaced, SurfaceSummary: surfaceSummary, HasSurfaceSummary: true}, nil
		}
		return featureScopeResult{Status: "empty", ScopesReplaced: replaced, SurfaceSummary: surfaceSummary, HasSurfaceSummary: true}, nil
	}
	return featureScopeResult{Status: "written", RowsWritten: rowsWritten, ScopesReplaced: replaced, SurfaceSummary: surfaceSummary, HasSurfaceSummary: true}, nil
}

func (s *FeatureService) computeVolatilityHistoryForBackfill(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) ([]dto.FeatureVolatilityHistoryRow, []usOptionsSurfaceAggregateRow, error) {
	historyStart := from
	if !historyStart.IsZero() {
		historyStart = historyStart.AddDate(0, 0, -(lookbackDays + 31))
	}
	priceSeries, err := s.queryUnderlyingCloseHistory(ctx, market, underlying, historyStart, to)
	if err != nil {
		return nil, nil, err
	}
	if market == "us-options" {
		aggregates, err := s.queryUSOptionsSurfaceAggregates(ctx, underlying, historyStart, to, time.Time{}, 1, int32(s.policy.DefaultMaxDTE))
		if err != nil {
			return nil, nil, err
		}
		ivSeries := buildUSOptionsCurrentIVSeries(aggregates, 30)
		history := buildVolatilityHistoryRows(priceSeries, ivSeries, from, to, lookbackDays, annualizationDays(market))
		return history, aggregates, nil
	}
	ivSeries, err := s.queryImpliedVolatilityHistory(ctx, market, underlying, historyStart, to)
	if err != nil {
		return nil, nil, err
	}
	history := buildVolatilityHistoryRows(priceSeries, ivSeries, from, to, lookbackDays, annualizationDays(market))
	return history, nil, nil
}

func (s *FeatureService) backfillLiquidityScope(ctx context.Context, market, underlying string, from, to time.Time, replace bool,
	computeFn func(context.Context, string, time.Time, time.Time, time.Time, int32, int32) ([]cryptoLiquidityAggregateRow, error),
) (featureScopeResult, error) {
	replaced := 0
	if replace {
		if err := s.deletePrecomputedSurfaceRows(ctx, featureLiquidityTable, market, underlying, from, to); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "liquidity-delete-scope", Err: err}
		}
		replaced = 1
	} else {
		hasRows, err := s.precomputedSurfaceRowsExist(ctx, featureLiquidityTable, market, underlying, from, to)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "liquidity-check-existing", Err: err}
		}
		if hasRows {
			return featureScopeResult{Status: "skipped"}, nil
		}
	}
	aggregates, err := computeFn(ctx, underlying, from, to, time.Time{}, 0, 0)
	if err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "liquidity-compute", Err: err}
	}
	liquidityHistory := buildLiquidityHistoryRows(aggregates)
	if len(aggregates) == 0 {
		return featureScopeResult{Status: "empty", ScopesReplaced: replaced, LiquidityHistory: liquidityHistory, HasLiquidityHistory: true}, nil
	}
	if err := s.insertPrecomputedLiquidityRows(ctx, market, underlying, aggregates); err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "liquidity-insert-rows", Err: err}
	}
	return featureScopeResult{Status: "written", RowsWritten: len(aggregates), ScopesReplaced: replaced, LiquidityHistory: liquidityHistory, HasLiquidityHistory: true}, nil
}

func (s *FeatureService) backfillCryptoOptionsLiquidityScope(ctx context.Context, underlying string, from, to time.Time, replace bool) (featureScopeResult, error) {
	return s.backfillLiquidityScope(ctx, "crypto-options", underlying, from, to, replace, s.queryCryptoOptionsLiquidityAggregates)
}

func (s *FeatureService) backfillUSOptionsLiquidityScope(ctx context.Context, underlying string, from, to time.Time, replace bool) (featureScopeResult, error) {
	return s.backfillLiquidityScope(ctx, "us-options", underlying, from, to, replace, s.queryUSOptionsLiquidityAggregates)
}

func (s *FeatureService) backfillDailyPanelScope(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32, replace bool, inputs featureBackfillPanelInputs) (featureScopeResult, error) {
	replaced := 0
	if replace {
		if err := s.deletePrecomputedDailyPanelRows(ctx, market, underlying, from, to, lookbackDays, minDTE, maxDTE); err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "daily-panel-delete-scope", Err: err}
		}
		replaced = 1
	} else {
		hasRows, err := s.precomputedDailyPanelRowsExist(ctx, market, underlying, from, to, lookbackDays, minDTE, maxDTE)
		if err != nil {
			return featureScopeResult{}, featureBackfillScopeError{Stage: "daily-panel-check-existing", Err: err}
		}
		if hasRows {
			return featureScopeResult{Status: "skipped"}, nil
		}
	}
	rows, err := s.buildDailyFeaturePanelRowsWithInputs(ctx, market, underlying, from, to, lookbackDays, minDTE, maxDTE, inputs)
	if err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "daily-panel-compute", Err: err}
	}
	if len(rows) == 0 {
		return featureScopeResult{Status: "empty", ScopesReplaced: replaced}, nil
	}
	if err := s.insertPrecomputedDailyPanelRows(ctx, market, underlying, lookbackDays, minDTE, maxDTE, rows); err != nil {
		return featureScopeResult{}, featureBackfillScopeError{Stage: "daily-panel-insert-rows", Err: err}
	}
	return featureScopeResult{Status: "written", RowsWritten: len(rows), ScopesReplaced: replaced}, nil
}

func applyFeatureScopeResult(stats *FeatureBackfillStats, wroteAny, allSkipped, allEmpty *bool, result featureScopeResult) {
	stats.RowsWritten += result.RowsWritten
	stats.ScopesReplaced += result.ScopesReplaced
	switch result.Status {
	case "written":
		*wroteAny = true
		*allSkipped = false
		*allEmpty = false
	case "skipped":
		*allEmpty = false
	case "empty":
		*allSkipped = false
	default:
		*allSkipped = false
		*allEmpty = false
	}
}

func toFeatureBackfillFailure(market, underlying string, err error) FeatureBackfillFailure {
	scopeErr, ok := err.(featureBackfillScopeError)
	if ok {
		return FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: scopeErr.Stage, Error: scopeErr.Err.Error()}
	}
	return FeatureBackfillFailure{Market: market, Underlying: underlying, Stage: "unknown", Error: err.Error()}
}

func normalizeFeatureSurfaceRequest(req dto.FeatureSurfaceSnapshotRequest) (string, string, int32, int32, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	underlying := strings.ToUpper(strings.TrimSpace(req.Underlying))
	if market == "" {
		return "", "", 0, 0, dto.NewValidationError("market is required")
	}
	if underlying == "" {
		return "", "", 0, 0, dto.NewValidationError("underlying is required")
	}
	minDTE := req.MinDaysToExpiry
	maxDTE := req.MaxDaysToExpiry
	if minDTE < 0 {
		return "", "", 0, 0, dto.NewValidationError("min_days_to_expiry must be >= 0")
	}
	if maxDTE <= 0 {
		maxDTE = 365
	}
	if maxDTE < minDTE {
		return "", "", 0, 0, dto.NewValidationError("max_days_to_expiry must be >= min_days_to_expiry")
	}
	return market, underlying, int32(minDTE), int32(maxDTE), nil
}

func normalizeFeatureDTEBounds(minDTE, maxDTE int) (int32, int32, error) {
	return DefaultFeaturePolicy().normalizeDTEBounds(minDTE, maxDTE)
}

func (s *FeatureService) normalizeFeatureDTEBounds(minDTE, maxDTE int) (int32, int32, error) {
	return s.policy.normalizeDTEBounds(minDTE, maxDTE)
}

func (p FeaturePolicy) normalizeDTEBounds(minDTE, maxDTE int) (int32, int32, error) {
	p = p.normalized()
	if minDTE < 0 {
		return 0, 0, dto.NewValidationError("min_days_to_expiry must be >= 0")
	}
	if maxDTE <= 0 {
		maxDTE = p.DefaultMaxDTE
	}
	if maxDTE < minDTE {
		return 0, 0, dto.NewValidationError("max_days_to_expiry must be >= min_days_to_expiry")
	}
	return int32(minDTE), int32(maxDTE), nil
}

func (s *FeatureService) latestUSOptionsFeatureDate(ctx context.Context, underlying string) (time.Time, bool, error) {
	rows, err := s.repo.Query(ctx, `SELECT ifNull(maxOrNull(market_date), toDate('1970-01-01'))
FROM us_options_bar_1m
WHERE underlying = {underlying:String}
  AND is_regular_session = 1`, clickhouse.Named("underlying", underlying))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest us-options feature date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var asOf time.Time
	if err := rows.Scan(&asOf); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest us-options feature date: %w", err)
	}
	if asOf.IsZero() || asOf.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return normalizeCalendarDate(asOf), true, nil
}

func (s *FeatureService) latestCryptoOptionsFeatureDate(ctx context.Context, underlying string) (time.Time, bool, error) {
	rows, err := s.repo.Query(ctx, `SELECT ifNull(toDate(maxOrNull(timestamp)), toDate('1970-01-01'))
FROM crypto_options_bar_1m
WHERE base_asset = {underlying:String}`,
		clickhouse.Named("underlying", underlying),
	)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest crypto-options feature date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var asOf time.Time
	if err := rows.Scan(&asOf); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest crypto-options feature date: %w", err)
	}
	if asOf.IsZero() || asOf.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return normalizeCalendarDate(asOf), true, nil
}

func (s *FeatureService) latestUSStocksFeatureDate(ctx context.Context, underlying string) (time.Time, bool, error) {
	rows, err := s.repo.Query(ctx, `SELECT ifNull(maxOrNull(market_date), toDate('1970-01-01'))
FROM us_stocks_bar_1m
WHERE symbol = {underlying:String}
  AND is_regular_session = 1`, clickhouse.Named("underlying", underlying))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest us-stocks feature date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var asOf time.Time
	if err := rows.Scan(&asOf); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest us-stocks feature date: %w", err)
	}
	if asOf.IsZero() || asOf.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return normalizeCalendarDate(asOf), true, nil
}

func (s *FeatureService) latestLiquidityFeatureDate(ctx context.Context, market, underlying string) (time.Time, bool, error) {
	if market == "crypto-options" {
		return s.latestCryptoOptionsFeatureDate(ctx, underlying)
	}
	return s.latestUSOptionsFeatureDate(ctx, underlying)
}

func (s *FeatureService) queryUSOptionsSurfaceAggregates(ctx context.Context, underlying string, from, to, asOf time.Time, minDTE, maxDTE int32) ([]usOptionsSurfaceAggregateRow, error) {
	query := fmt.Sprintf(`SELECT
	toDate(timestamp) AS as_of_date,
	expiration,
	dateDiff('day', toDate(timestamp), expiration) AS dte_days,
	%s AS atm_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'C'), 0) AS call_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'P'), 0) AS put_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'C' AND strike > underlying_close * 1.02 AND strike <= underlying_close * 1.10), 0) AS otm_call_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'P' AND strike < underlying_close * 0.98 AND strike >= underlying_close * 0.90), 0) AS otm_put_iv,
	toUInt32(countIf(isFinite(implied_volatility) AND implied_volatility > 0)) AS contract_count
FROM us_options_bar_1d
WHERE underlying = {underlying:String}
	  AND expiration >= toDate(timestamp)`, buildUSOptionsATMIVExprWithWindows(s.policy.USOptionsATMWindows))
	args := []interface{}{clickhouse.Named("underlying", underlying)}
	if !asOf.IsZero() {
		query += `
	  AND timestamp >= toDateTime({as_of_from:String}, 'UTC')
	  AND timestamp < toDateTime({as_of_to:String}, 'UTC')
  AND toDate(timestamp) = toDate({as_of:String})
  AND dateDiff('day', toDate(timestamp), expiration) >= {min_dte:Int32}
  AND dateDiff('day', toDate(timestamp), expiration) <= {max_dte:Int32}`
		args = append(args,
			clickhouse.Named("as_of_from", asOf.UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("as_of_to", asOf.AddDate(0, 0, 1).UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("as_of", asOf.UTC().Format("2006-01-02")),
			clickhouse.Named("min_dte", minDTE),
			clickhouse.Named("max_dte", maxDTE),
		)
	} else {
		if !from.IsZero() {
			query += `
	  AND timestamp >= toDateTime({from_ts:String}, 'UTC')
  AND toDate(timestamp) >= toDate({from:String})`
			args = append(args,
				clickhouse.Named("from_ts", from.UTC().Format("2006-01-02 15:04:05")),
				clickhouse.Named("from", from.UTC().Format("2006-01-02")),
			)
		}
		if !to.IsZero() {
			query += `
	  AND timestamp < toDateTime({to_ts:String}, 'UTC')
  AND toDate(timestamp) < toDate({to:String})`
			args = append(args,
				clickhouse.Named("to_ts", to.UTC().Format("2006-01-02 15:04:05")),
				clickhouse.Named("to", to.UTC().Format("2006-01-02")),
			)
		}
	}
	query += `
GROUP BY as_of_date, expiration
ORDER BY as_of_date ASC, expiration ASC`
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query us-options surface aggregates: %w", err)
	}
	defer rows.Close()
	aggregates := make([]usOptionsSurfaceAggregateRow, 0)
	for rows.Next() {
		var (
			row           usOptionsSurfaceAggregateRow
			daysToExpiry  int64
			contractCount uint32
		)
		if err := rows.Scan(
			&row.AsOfDate,
			&row.Expiration,
			&daysToExpiry,
			&row.ATMIV,
			&row.CallIV,
			&row.PutIV,
			&row.OTMCallIV,
			&row.OTMPutIV,
			&contractCount,
		); err != nil {
			return nil, fmt.Errorf("scan us-options surface aggregate row: %w", err)
		}
		if contractCount == 0 {
			continue
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = normalizeCalendarDate(row.Expiration)
		row.DaysToExpiry = int(daysToExpiry)
		row.ContractCount = int(contractCount)
		row.ATMIV = sanitizeF64Ptr(row.ATMIV)
		row.CallIV = sanitizeF64Ptr(row.CallIV)
		row.PutIV = sanitizeF64Ptr(row.PutIV)
		row.OTMCallIV = sanitizeF64Ptr(row.OTMCallIV)
		row.OTMPutIV = sanitizeF64Ptr(row.OTMPutIV)
		aggregates = append(aggregates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate us-options surface aggregate rows: %w", err)
	}
	return aggregates, nil
}

func buildUSOptionsATMIVExpr() string {
	return buildUSOptionsATMIVExprWithWindows(DefaultFeaturePolicy().USOptionsATMWindows)
}

func buildUSOptionsATMIVExprWithWindows(windows [][2]float64) string {
	if len(windows) == 0 {
		windows = DefaultFeaturePolicy().USOptionsATMWindows
	}
	parts := make([]string, 0, len(windows))
	for _, window := range windows {
		parts = append(parts, fmt.Sprintf(
			"nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND strike >= underlying_close * %.2f AND strike <= underlying_close * %.2f), 0)",
			window[0],
			window[1],
		))
	}
	return "coalesce(" + strings.Join(parts, ", ") + ")"
}

func (s *FeatureService) queryCryptoOptionsLiquidityAggregates(ctx context.Context, underlying string, from, to, asOf time.Time, minDTE, maxDTE int32) ([]cryptoLiquidityAggregateRow, error) {
	query := `SELECT
	as_of_date,
	expiration,
	dateDiff('day', as_of_date, toDate(expiration)) AS dte_days,
	nullIf(avgIf(toFloat64(bid_close), bid_close > 0), 0) AS avg_bid_close,
	nullIf(avgIf(toFloat64(ask_close), ask_close > 0), 0) AS avg_ask_close,
	nullIf(avgIf(toFloat64(mark_close), mark_close > 0), 0) AS avg_mark_close,
	nullIf(avgIf((toFloat64(ask_close) - toFloat64(bid_close)) / greatest((toFloat64(ask_close) + toFloat64(bid_close)) / 2.0, 1e-9), bid_close > 0 AND ask_close > bid_close), 0) AS relative_spread,
	nullIf(sumIf(toFloat64(open_interest), open_interest > 0), 0) AS open_interest,
	toUInt64(sum(tick_count)) AS tick_count,
	toUInt32(count()) AS contract_count,
	toUInt32(countIf(bid_close > 0 AND ask_close > bid_close)) AS tradable_contract_count
FROM (
	SELECT
		last.as_of_date AS as_of_date,
		meta.expiration AS expiration,
		last.bid_close AS bid_close,
		last.ask_close AS ask_close,
		last.mark_close AS mark_close,
		last.open_interest AS open_interest,
		last.tick_count AS tick_count
	FROM (
		SELECT
			toDate(timestamp) AS as_of_date,
			symbol_id,
			argMax(bid_close, timestamp) AS bid_close,
			argMax(ask_close, timestamp) AS ask_close,
			argMax(mark_close, timestamp) AS mark_close,
			argMax(open_interest, timestamp) AS open_interest,
			sum(toUInt64(tick_count)) AS tick_count
		FROM crypto_options_bar_1m
		WHERE base_asset = {underlying:String}`
	args := []interface{}{clickhouse.Named("underlying", underlying)}
	if !asOf.IsZero() {
		query += `
		  AND timestamp >= toDateTime({as_of_from:String}, 'UTC')
		  AND timestamp < toDateTime({as_of_to:String}, 'UTC')`
		args = append(args,
			clickhouse.Named("as_of_from", asOf.UTC().Format("2006-01-02 00:00:00")),
			clickhouse.Named("as_of_to", asOf.AddDate(0, 0, 1).UTC().Format("2006-01-02 00:00:00")),
		)
	} else {
		if !from.IsZero() {
			query += `
		  AND timestamp >= toDateTime({from:String}, 'UTC')`
			args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
		}
		if !to.IsZero() {
			query += `
		  AND timestamp < toDateTime({to:String}, 'UTC')`
			args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
		}
	}
	query += `
		GROUP BY as_of_date, symbol_id
	) AS last
	INNER JOIN crypto_options_symbol_meta AS meta ON meta.symbol_id = last.symbol_id
	WHERE toDate(meta.expiration) >= last.as_of_date`
	if !asOf.IsZero() {
		query += `
	  AND dateDiff('day', last.as_of_date, toDate(meta.expiration)) >= {min_dte:Int32}
	  AND dateDiff('day', last.as_of_date, toDate(meta.expiration)) <= {max_dte:Int32}`
		args = append(args, clickhouse.Named("min_dte", minDTE), clickhouse.Named("max_dte", maxDTE))
	}
	query += `
)
GROUP BY as_of_date, expiration
ORDER BY as_of_date ASC, expiration ASC`
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query crypto-options liquidity aggregates: %w", err)
	}
	defer rows.Close()
	aggregates := make([]cryptoLiquidityAggregateRow, 0)
	for rows.Next() {
		var (
			row                   cryptoLiquidityAggregateRow
			tickCount             uint64
			contractCount         uint32
			tradableContractCount uint32
		)
		if err := rows.Scan(&row.AsOfDate, &row.Expiration, &row.DaysToExpiry, &row.AvgBidClose, &row.AvgAskClose, &row.AvgMarkClose, &row.RelativeSpread, &row.OpenInterest, &tickCount, &contractCount, &tradableContractCount); err != nil {
			return nil, fmt.Errorf("scan crypto-options liquidity aggregate row: %w", err)
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = row.Expiration.UTC()
		row.TickCount = int(tickCount)
		row.Volume = int(tickCount)
		row.Transactions = int(tickCount)
		row.ContractCount = int(contractCount)
		row.TradableContractCount = int(tradableContractCount)
		row.ActiveContractCount = int(contractCount)
		row.AvgBidClose = sanitizeF64Ptr(row.AvgBidClose)
		row.AvgAskClose = sanitizeF64Ptr(row.AvgAskClose)
		row.AvgMarkClose = sanitizeF64Ptr(row.AvgMarkClose)
		row.RelativeSpread = sanitizeF64Ptr(row.RelativeSpread)
		row.OpenInterest = sanitizeF64Ptr(row.OpenInterest)
		aggregates = append(aggregates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate crypto-options liquidity aggregate rows: %w", err)
	}
	return aggregates, nil
}

func (s *FeatureService) queryUSOptionsLiquidityAggregates(ctx context.Context, underlying string, from, to, asOf time.Time, minDTE, maxDTE int32) ([]cryptoLiquidityAggregateRow, error) {
	query := `SELECT
	toDate(timestamp) AS as_of_date,
	expiration,
	dateDiff('day', toDate(timestamp), expiration) AS dte_days,
	CAST(NULL, 'Nullable(Float64)') AS avg_bid_close,
	CAST(NULL, 'Nullable(Float64)') AS avg_ask_close,
	nullIf(avgIf(toFloat64(close), close > 0), 0) AS avg_mark_close,
	CAST(NULL, 'Nullable(Float64)') AS relative_spread,
	CAST(NULL, 'Nullable(Float64)') AS open_interest,
	toUInt64(sum(volume)) AS total_volume,
	toUInt64(sum(transactions)) AS total_transactions,
	toUInt32(count()) AS contract_count,
	toUInt32(countIf(volume > 0 OR transactions > 0)) AS active_contract_count,
	toUInt32(0) AS tradable_contract_count
FROM us_options_bar_1d
WHERE underlying = {underlying:String}
  AND expiration >= toDate(timestamp)`
	args := []interface{}{clickhouse.Named("underlying", underlying)}
	if !asOf.IsZero() {
		query += `
	  AND timestamp >= toDateTime({as_of_from:String}, 'UTC')
	  AND timestamp < toDateTime({as_of_to:String}, 'UTC')
  AND toDate(timestamp) = toDate({as_of:String})
  AND dateDiff('day', toDate(timestamp), expiration) >= {min_dte:Int32}
  AND dateDiff('day', toDate(timestamp), expiration) <= {max_dte:Int32}`
		args = append(args,
			clickhouse.Named("as_of_from", asOf.UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("as_of_to", asOf.AddDate(0, 0, 1).UTC().Format("2006-01-02 15:04:05")),
			clickhouse.Named("as_of", asOf.UTC().Format("2006-01-02")),
			clickhouse.Named("min_dte", minDTE),
			clickhouse.Named("max_dte", maxDTE),
		)
	} else {
		if !from.IsZero() {
			query += `
	  AND timestamp >= toDateTime({from_ts:String}, 'UTC')
  AND toDate(timestamp) >= toDate({from:String})`
			args = append(args,
				clickhouse.Named("from_ts", from.UTC().Format("2006-01-02 15:04:05")),
				clickhouse.Named("from", from.UTC().Format("2006-01-02")),
			)
		}
		if !to.IsZero() {
			query += `
	  AND timestamp < toDateTime({to_ts:String}, 'UTC')
  AND toDate(timestamp) < toDate({to:String})`
			args = append(args,
				clickhouse.Named("to_ts", to.UTC().Format("2006-01-02 15:04:05")),
				clickhouse.Named("to", to.UTC().Format("2006-01-02")),
			)
		}
	}
	query += `
GROUP BY as_of_date, expiration
ORDER BY as_of_date ASC, expiration ASC`
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query us-options liquidity aggregates: %w", err)
	}
	defer rows.Close()
	aggregates := make([]cryptoLiquidityAggregateRow, 0)
	for rows.Next() {
		var (
			row                   cryptoLiquidityAggregateRow
			daysToExpiry          int64
			openInterest          *float64
			volume                uint64
			transactions          uint64
			contractCount         uint32
			activeContractCount   uint32
			tradableContractCount uint32
		)
		if err := rows.Scan(&row.AsOfDate, &row.Expiration, &daysToExpiry, &row.AvgBidClose, &row.AvgAskClose, &row.AvgMarkClose, &row.RelativeSpread, &openInterest, &volume, &transactions, &contractCount, &activeContractCount, &tradableContractCount); err != nil {
			return nil, fmt.Errorf("scan us-options liquidity aggregate row: %w", err)
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.Volume = int(volume)
		row.Transactions = int(transactions)
		row.ContractCount = int(contractCount)
		row.ActiveContractCount = int(activeContractCount)
		row.TradableContractCount = int(tradableContractCount)
		row.AvgBidClose = sanitizeF64Ptr(row.AvgBidClose)
		row.AvgAskClose = sanitizeF64Ptr(row.AvgAskClose)
		row.AvgMarkClose = sanitizeF64Ptr(row.AvgMarkClose)
		row.RelativeSpread = sanitizeF64Ptr(row.RelativeSpread)
		row.OpenInterest = sanitizeF64Ptr(openInterest)
		aggregates = append(aggregates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate us-options liquidity aggregate rows: %w", err)
	}
	return aggregates, nil
}

func (s *FeatureService) queryLiquidityAggregates(ctx context.Context, market, underlying string, from, to, asOf time.Time, minDTE, maxDTE int32) ([]cryptoLiquidityAggregateRow, error) {
	if market == "crypto-options" {
		return s.queryCryptoOptionsLiquidityAggregates(ctx, underlying, from, to, asOf, minDTE, maxDTE)
	}
	return s.queryUSOptionsLiquidityAggregates(ctx, underlying, from, to, asOf, minDTE, maxDTE)
}

func normalizeFeatureRequest(market, underlying string, lookbackDays int) (string, string, int, error) {
	return DefaultFeaturePolicy().normalizeFeatureRequest(market, underlying, lookbackDays)
}

func (s *FeatureService) normalizeFeatureRequest(market, underlying string, lookbackDays int) (string, string, int, error) {
	return s.policy.normalizeFeatureRequest(market, underlying, lookbackDays)
}

func (p FeaturePolicy) normalizeFeatureRequest(market, underlying string, lookbackDays int) (string, string, int, error) {
	p = p.normalized()
	normalizedMarket, err := normalizeFeatureMarket(market)
	if err != nil {
		return "", "", 0, err
	}
	normalizedUnderlying := strings.ToUpper(strings.TrimSpace(underlying))
	if normalizedUnderlying == "" {
		return "", "", 0, dto.NewValidationError("underlying is required")
	}
	return normalizedMarket, normalizedUnderlying, clamp(lookbackDays, p.DefaultLookbackDays, p.MaxLookbackDays), nil
}

func normalizeFeatureMarket(market string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(market))
	switch value {
	case "crypto-options", "us-options":
		return value, nil
	default:
		return "", dto.NewValidationError("unsupported feature market %q", market)
	}
}

func normalizeFeatureMarkets(markets []string) ([]string, error) {
	if len(markets) == 0 {
		return []string{"crypto-options", "us-options"}, nil
	}
	seen := make(map[string]struct{}, len(markets))
	result := make([]string, 0, len(markets))
	for _, market := range markets {
		normalized, err := normalizeFeatureMarket(market)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func annualizationDays(market string) float64 {
	if market == "crypto-options" {
		return 365
	}
	return 252
}

func normalizeCalendarDate(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func normalizeUnderlyingList(underlyings []string) []string {
	if len(underlyings) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(underlyings))
	result := make([]string, 0, len(underlyings))
	for _, underlying := range underlyings {
		normalized := strings.ToUpper(strings.TrimSpace(underlying))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func prioritizeFeatureUnderlyings(ctx context.Context, conn driver.Conn, market string, underlyings []string, priorityOrder string) ([]string, error) {
	if market != "us-options" {
		return underlyings, nil
	}
	return usmarket.MaybePrioritizeUSSymbols(ctx, conn, underlyings, priorityOrder)
}

func (s *FeatureService) computeVolatilityHistory(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) ([]dto.FeatureVolatilityHistoryRow, []featurePoint, []featurePoint, error) {
	historyStart := from
	if !historyStart.IsZero() {
		historyStart = historyStart.AddDate(0, 0, -(lookbackDays + 31))
	}
	priceSeries, err := s.queryUnderlyingCloseHistory(ctx, market, underlying, historyStart, to)
	if err != nil {
		return nil, nil, nil, err
	}
	ivSeries, err := s.queryImpliedVolatilityHistory(ctx, market, underlying, historyStart, to)
	if err != nil {
		return nil, nil, nil, err
	}
	history := buildVolatilityHistoryRows(priceSeries, ivSeries, from, to, lookbackDays, annualizationDays(market))
	return history, priceSeries, ivSeries, nil
}

func (s *FeatureService) queryUnderlyingCloseHistory(ctx context.Context, market, underlying string, from, to time.Time) ([]featurePoint, error) {
	var (
		query string
		args  []interface{}
	)
	switch market {
	case "crypto-options":
		query = `SELECT day, close
FROM (
	SELECT
		toDate(timestamp) AS day,
		toFloat64(argMax(close, timestamp)) AS close
	FROM crypto_spot_bar_1m
	WHERE symbol = {symbol:String}`
		args = []interface{}{clickhouse.Named("symbol", underlying)}
		if !from.IsZero() {
			query += `
	  AND timestamp >= toDateTime({from:String}, 'UTC')`
			args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
		}
		if !to.IsZero() {
			query += `
	  AND timestamp < toDateTime({to:String}, 'UTC')`
			args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
		}
		query += `
	GROUP BY day
)
ORDER BY day ASC`
	case "us-options":
		query = `SELECT day, close
FROM (
	SELECT
		toDate(b.timestamp) AS day,
		` + chquery.USStockAdjustedPriceSQL("b", "close", "sp") + ` AS close
	FROM us_stocks_bar_1d AS b
	` + chquery.USStockSplitJoinSQL("b", "sp") + `
	WHERE b.symbol = {symbol:String}
	`
		args = []interface{}{clickhouse.Named("symbol", underlying)}
		if !from.IsZero() {
			query += `
	  AND b.timestamp >= toDateTime({from:String}, 'UTC')`
			args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
		}
		if !to.IsZero() {
			query += `
	  AND b.timestamp < toDateTime({to:String}, 'UTC')`
			args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
		}
		query += `
	GROUP BY b.timestamp, b.symbol, b.close
)
ORDER BY day ASC`
	default:
		return nil, dto.NewValidationError("unsupported feature market %q", market)
	}
	return queryFeatureSeries(ctx, s.repo.Conn, query, args...)
}

func (s *FeatureService) queryImpliedVolatilityHistory(ctx context.Context, market, underlying string, from, to time.Time) ([]featurePoint, error) {
	if market == "us-options" {
		aggregates, err := s.queryUSOptionsSurfaceAggregates(ctx, underlying, from, to, time.Time{}, 1, int32(s.policy.DefaultMaxDTE))
		if err != nil {
			return nil, err
		}
		return buildUSOptionsCurrentIVSeries(aggregates, 30), nil
	}

	var (
		query string
		args  []interface{}
	)
	switch market {
	case "crypto-options":
		query = `SELECT day, iv
FROM (
	SELECT
		toDate(timestamp) AS day,
		avgIf(toFloat64(mark_iv_close), isFinite(mark_iv_close) AND mark_iv_close > 0) AS iv
	FROM crypto_options_bar_1m
	WHERE base_asset = {underlying:String}`
		args = []interface{}{clickhouse.Named("underlying", underlying)}
		if !from.IsZero() {
			query += `
	  AND timestamp >= toDateTime({from:String}, 'UTC')`
			args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02 15:04:05")))
		}
		if !to.IsZero() {
			query += `
	  AND timestamp < toDateTime({to:String}, 'UTC')`
			args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02 15:04:05")))
		}
		query += `
	GROUP BY day
)
ORDER BY day ASC`
	default:
		return nil, dto.NewValidationError("unsupported feature market %q", market)
	}
	return queryFeatureSeries(ctx, s.repo.Conn, query, args...)
}

func buildUSOptionsCurrentIVSeries(aggregates []usOptionsSurfaceAggregateRow, targetDTE int) []featurePoint {
	if len(aggregates) == 0 {
		return nil
	}

	series := make([]featurePoint, 0)
	currentDay := ""
	var best featurePoint
	bestDistance := 0
	haveBest := false

	flush := func() {
		if haveBest {
			series = append(series, best)
		}
	}

	for _, row := range aggregates {
		normalizedAsOfDate := normalizeCalendarDate(row.AsOfDate)
		dayKey := normalizedAsOfDate.Format("2006-01-02")
		if dayKey != currentDay {
			flush()
			currentDay = dayKey
			haveBest = false
		}
		if row.ATMIV == nil {
			continue
		}
		distance := row.DaysToExpiry - targetDTE
		if distance < 0 {
			distance = -distance
		}
		if !haveBest || distance < bestDistance {
			best = featurePoint{Date: normalizedAsOfDate, Value: *row.ATMIV}
			bestDistance = distance
			haveBest = true
		}
	}
	flush()
	return series
}

func (s *FeatureService) listFeatureUnderlyings(ctx context.Context, market string) ([]string, error) {
	var query string
	switch market {
	case "crypto-options":
		query = `SELECT base_asset FROM crypto_options_bar_1m GROUP BY base_asset ORDER BY base_asset`
	case "us-options":
		query = `SELECT underlying FROM us_options_bar_1m GROUP BY underlying ORDER BY underlying`
	default:
		return nil, dto.NewValidationError("unsupported feature market %q", market)
	}
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query feature underlyings: %w", err)
	}
	defer rows.Close()

	underlyings := make([]string, 0)
	for rows.Next() {
		var underlying string
		if err := rows.Scan(&underlying); err != nil {
			return nil, fmt.Errorf("scan feature underlying: %w", err)
		}
		underlyings = append(underlyings, underlying)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature underlyings: %w", err)
	}
	return underlyings, nil
}

func (s *FeatureService) queryPrecomputedVolatilitySnapshot(ctx context.Context, market, underlying string, lookbackDays int) (*dto.FeatureVolatilitySnapshotResponse, bool, error) {
	exists, err := s.featureStoreTableExists(ctx)
	if err != nil || !exists {
		return nil, false, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND lookback_days = {lookback_days:UInt16}
ORDER BY as_of_date DESC
LIMIT %d`, featureSnapshotTable, s.policy.FallbackWindowDays),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed volatility snapshot: %w", err)
	}
	defer rows.Close()
	historyDesc := make([]dto.FeatureVolatilityHistoryRow, 0, s.policy.FallbackWindowDays)
	for rows.Next() {
		row, err := scanFeatureHistoryRow(rows)
		if err != nil {
			return nil, false, err
		}
		historyDesc = append(historyDesc, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed volatility snapshot: %w", err)
	}
	if len(historyDesc) == 0 {
		return nil, false, nil
	}
	priceRow, ivRow := latestValidVolatilitySnapshotRows(historyDesc, s.policy.FallbackWindowDays)
	resp := &dto.FeatureVolatilitySnapshotResponse{
		Market:       market,
		Underlying:   underlying,
		LookbackDays: lookbackDays,
	}
	if priceRow != nil {
		resp.PriceObservations = priceRow.PriceObservations
		resp.HV10 = priceRow.HV10
		resp.HV20 = priceRow.HV20
		resp.HV30 = priceRow.HV30
		date := normalizeCalendarDate(priceRow.Date)
		resp.PriceAsOf = &date
	}
	if ivRow != nil {
		resp.IVObservations = ivRow.IVObservations
		resp.CurrentIV = ivRow.CurrentIV
		resp.IVPercentile = ivRow.IVPercentile
		resp.IVRank = ivRow.IVRank
		date := normalizeCalendarDate(ivRow.Date)
		resp.IVAsOf = &date
	}
	return resp, true, nil
}

func (s *FeatureService) queryPrecomputedVolatilityHistory(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) (*dto.FeatureVolatilityHistoryResponse, bool, error) {
	exists, err := s.featureStoreTableExists(ctx)
	if err != nil || !exists {
		return nil, false, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND lookback_days = {lookback_days:UInt16}
  AND as_of_date >= toDate({window_from:String})
  AND as_of_date < toDate({to:String})
ORDER BY as_of_date ASC`, featureSnapshotTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
		clickhouse.Named("window_from", featureFallbackWindowStart(from).Format("2006-01-02")),
		clickhouse.Named("to", to.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed volatility history: %w", err)
	}
	defer rows.Close()
	history := make([]dto.FeatureVolatilityHistoryRow, 0)
	for rows.Next() {
		row, err := scanFeatureHistoryRow(rows)
		if err != nil {
			return nil, false, err
		}
		history = append(history, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed volatility history: %w", err)
	}
	if len(history) == 0 {
		return nil, false, nil
	}
	history = fillVolatilityHistoryFallback(history, s.policy.FallbackWindowDays)
	history = trimVolatilityHistoryRange(history, from, to)
	if len(history) == 0 {
		return nil, false, nil
	}
	return &dto.FeatureVolatilityHistoryResponse{Market: market, Underlying: underlying, LookbackDays: lookbackDays, Data: history}, true, nil
}

func (s *FeatureService) queryPrecomputedTermStructureSnapshot(ctx context.Context, market, underlying string, minDTE, maxDTE int32) (*dto.FeatureTermStructureSnapshotResponse, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureTermStructureTable)
	if err != nil || !exists {
		return nil, false, err
	}
	asOf, ok, err := s.latestPrecomputedSurfaceDate(ctx, featureTermStructureTable, market, underlying)
	if err != nil || !ok {
		return nil, ok, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	expiration,
	days_to_expiry,
	atm_iv,
	call_iv,
	put_iv,
	contract_count
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date = toDate({as_of:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY expiration ASC`, featureTermStructureTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("as_of", asOf.Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed term structure snapshot: %w", err)
	}
	defer rows.Close()
	resp := &dto.FeatureTermStructureSnapshotResponse{Market: market, Underlying: underlying, AsOf: &asOf, Data: make([]dto.FeatureTermStructureSnapshotRow, 0)}
	for rows.Next() {
		var (
			row           dto.FeatureTermStructureSnapshotRow
			daysToExpiry  uint16
			contractCount uint32
		)
		if err := rows.Scan(&row.Expiration, &daysToExpiry, &row.ATMIV, &row.CallIV, &row.PutIV, &contractCount); err != nil {
			return nil, false, fmt.Errorf("scan precomputed term structure row: %w", err)
		}
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.ContractCount = int(contractCount)
		row.ATMIV = sanitizeF64Ptr(row.ATMIV)
		row.CallIV = sanitizeF64Ptr(row.CallIV)
		row.PutIV = sanitizeF64Ptr(row.PutIV)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed term structure rows: %w", err)
	}
	return resp, true, nil
}

func (s *FeatureService) queryPrecomputedSkewSnapshot(ctx context.Context, market, underlying string, minDTE, maxDTE int32) (*dto.FeatureSkewSnapshotResponse, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureSkewTable)
	if err != nil || !exists {
		return nil, false, err
	}
	asOf, ok, err := s.latestPrecomputedSurfaceDate(ctx, featureSkewTable, market, underlying)
	if err != nil || !ok {
		return nil, ok, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	expiration,
	days_to_expiry,
	otm_call_iv,
	otm_put_iv,
	put_call_skew,
	contract_count
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date = toDate({as_of:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY expiration ASC`, featureSkewTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("as_of", asOf.Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed skew snapshot: %w", err)
	}
	defer rows.Close()
	resp := &dto.FeatureSkewSnapshotResponse{Market: market, Underlying: underlying, AsOf: &asOf, Data: make([]dto.FeatureSkewSnapshotRow, 0)}
	for rows.Next() {
		var (
			row           dto.FeatureSkewSnapshotRow
			daysToExpiry  uint16
			contractCount uint32
		)
		if err := rows.Scan(&row.Expiration, &daysToExpiry, &row.OTMCallIV, &row.OTMPutIV, &row.PutCallSkew, &contractCount); err != nil {
			return nil, false, fmt.Errorf("scan precomputed skew row: %w", err)
		}
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.ContractCount = int(contractCount)
		row.OTMCallIV = sanitizeF64Ptr(row.OTMCallIV)
		row.OTMPutIV = sanitizeF64Ptr(row.OTMPutIV)
		row.PutCallSkew = sanitizeF64Ptr(row.PutCallSkew)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed skew rows: %w", err)
	}
	return resp, true, nil
}

func (s *FeatureService) queryPrecomputedLiquiditySnapshot(ctx context.Context, market, underlying string, minDTE, maxDTE int32) (*dto.FeatureLiquiditySnapshotResponse, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureLiquidityTable)
	if err != nil || !exists {
		return nil, false, err
	}
	asOf, ok, err := s.latestPrecomputedSurfaceDate(ctx, featureLiquidityTable, market, underlying)
	if err != nil || !ok {
		return nil, ok, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	expiration,
	days_to_expiry,
	avg_bid_close,
	avg_ask_close,
	avg_mark_close,
	relative_spread,
	open_interest,
	tick_count,
	volume,
	transactions,
	contract_count,
	active_contract_count,
	tradable_contract_count,
	activity_ratio,
	tradability_ratio
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date = toDate({as_of:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY expiration ASC`, featureLiquidityTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("as_of", asOf.Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed liquidity snapshot: %w", err)
	}
	defer rows.Close()
	resp := &dto.FeatureLiquiditySnapshotResponse{Market: market, Underlying: underlying, AsOf: &asOf, Data: make([]dto.FeatureLiquiditySnapshotRow, 0)}
	for rows.Next() {
		var (
			row                   dto.FeatureLiquiditySnapshotRow
			daysToExpiry          uint16
			tickCount             uint64
			volume                uint64
			transactions          uint64
			contractCount         uint32
			activeContractCount   uint32
			tradableContractCount uint32
		)
		if err := rows.Scan(&row.Expiration, &daysToExpiry, &row.AvgBidClose, &row.AvgAskClose, &row.AvgMarkClose, &row.RelativeSpread, &row.OpenInterest, &tickCount, &volume, &transactions, &contractCount, &activeContractCount, &tradableContractCount, &row.ActivityRatio, &row.TradabilityRatio); err != nil {
			return nil, false, fmt.Errorf("scan precomputed liquidity row: %w", err)
		}
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.TickCount = int(tickCount)
		row.Volume = int(volume)
		row.Transactions = int(transactions)
		row.ContractCount = int(contractCount)
		row.ActiveContractCount = int(activeContractCount)
		row.TradableContractCount = int(tradableContractCount)
		row.AvgBidClose = sanitizeF64Ptr(row.AvgBidClose)
		row.AvgAskClose = sanitizeF64Ptr(row.AvgAskClose)
		row.AvgMarkClose = sanitizeF64Ptr(row.AvgMarkClose)
		row.RelativeSpread = sanitizeF64Ptr(row.RelativeSpread)
		row.OpenInterest = sanitizeF64Ptr(row.OpenInterest)
		row.ActivityRatio = sanitizeF64Ptr(row.ActivityRatio)
		row.TradabilityRatio = sanitizeF64Ptr(row.TradabilityRatio)
		resp.Data = append(resp.Data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed liquidity rows: %w", err)
	}
	return resp, true, nil
}

func (s *FeatureService) queryPrecomputedLiquidityHistory(ctx context.Context, market, underlying string, from, to time.Time, minDTE, maxDTE int32) (*dto.FeatureLiquidityHistoryResponse, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureLiquidityTable)
	if err != nil || !exists {
		return nil, false, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	expiration,
	days_to_expiry,
	avg_bid_close,
	avg_ask_close,
	avg_mark_close,
	relative_spread,
	open_interest,
	tick_count,
	volume,
	transactions,
	contract_count,
	active_contract_count,
	tradable_contract_count,
	activity_ratio,
	tradability_ratio
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND as_of_date >= toDate({from:String})
  AND as_of_date < toDate({to:String})
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}
ORDER BY as_of_date ASC, expiration ASC`, featureLiquidityTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("from", from.UTC().Format("2006-01-02")),
		clickhouse.Named("to", to.UTC().Format("2006-01-02")),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed liquidity history: %w", err)
	}
	defer rows.Close()
	history := make([]dto.FeatureLiquidityHistoryRow, 0)
	for rows.Next() {
		var (
			row                   dto.FeatureLiquidityHistoryRow
			daysToExpiry          uint16
			tickCount             uint64
			volume                uint64
			transactions          uint64
			contractCount         uint32
			activeContractCount   uint32
			tradableContractCount uint32
		)
		if err := rows.Scan(&row.AsOfDate, &row.Expiration, &daysToExpiry, &row.AvgBidClose, &row.AvgAskClose, &row.AvgMarkClose, &row.RelativeSpread, &row.OpenInterest, &tickCount, &volume, &transactions, &contractCount, &activeContractCount, &tradableContractCount, &row.ActivityRatio, &row.TradabilityRatio); err != nil {
			return nil, false, fmt.Errorf("scan precomputed liquidity history row: %w", err)
		}
		row.AsOfDate = normalizeCalendarDate(row.AsOfDate)
		row.Expiration = row.Expiration.UTC()
		row.DaysToExpiry = int(daysToExpiry)
		row.TickCount = int(tickCount)
		row.Volume = int(volume)
		row.Transactions = int(transactions)
		row.ContractCount = int(contractCount)
		row.ActiveContractCount = int(activeContractCount)
		row.TradableContractCount = int(tradableContractCount)
		row.AvgBidClose = sanitizeF64Ptr(row.AvgBidClose)
		row.AvgAskClose = sanitizeF64Ptr(row.AvgAskClose)
		row.AvgMarkClose = sanitizeF64Ptr(row.AvgMarkClose)
		row.RelativeSpread = sanitizeF64Ptr(row.RelativeSpread)
		row.OpenInterest = sanitizeF64Ptr(row.OpenInterest)
		row.ActivityRatio = sanitizeF64Ptr(row.ActivityRatio)
		row.TradabilityRatio = sanitizeF64Ptr(row.TradabilityRatio)
		history = append(history, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed liquidity history rows: %w", err)
	}
	if len(history) == 0 {
		return nil, false, nil
	}
	return &dto.FeatureLiquidityHistoryResponse{Market: market, Underlying: underlying, Data: history}, true, nil
}

func (s *FeatureService) queryPrecomputedDailyFeaturePanel(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32) (*dto.FeatureDailyPanelResponse, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureDailyPanelTable)
	if err != nil || !exists {
		return nil, false, err
	}
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank,
	front_expiration,
	front_days_to_expiry,
	front_atm_iv,
	front_put_call_skew,
	surface_contract_count,
	liquidity_open_interest,
	liquidity_relative_spread,
	liquidity_tick_count,
	liquidity_volume,
	liquidity_transactions,
	liquidity_contract_count,
	liquidity_active_contract_count,
	liquidity_tradable_contract_count,
	liquidity_activity_ratio,
	liquidity_tradability_ratio,
	is_early_close,
	days_from_prev_holiday,
	days_to_next_holiday
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND lookback_days = {lookback_days:UInt16}
  AND min_days_to_expiry = {min_dte:Int32}
  AND max_days_to_expiry = {max_dte:Int32}
  AND as_of_date >= toDate({window_from:String})
  AND as_of_date < toDate({to:String})
ORDER BY as_of_date ASC`, featureDailyPanelTable),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
		clickhouse.Named("window_from", featureFallbackWindowStart(from).Format("2006-01-02")),
		clickhouse.Named("to", to.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed daily feature panel: %w", err)
	}
	defer rows.Close()
	data := make([]dto.FeatureDailyPanelRow, 0)
	for rows.Next() {
		var (
			row                            dto.FeatureDailyPanelRow
			priceObservations              uint32
			ivObservations                 uint32
			frontExpiration                time.Time
			frontDaysToExpiry              int32
			surfaceContractCount           int32
			liquidityTickCount             uint64
			liquidityVolume                uint64
			liquidityTransactions          uint64
			liquidityContractCount         uint32
			liquidityActiveContractCount   uint32
			liquidityTradableContractCount uint32
			isEarlyClose                   uint8
			daysFromPrevHoliday            int32
			daysToNextHoliday              int32
		)
		if err := rows.Scan(
			&row.Date,
			&priceObservations,
			&ivObservations,
			&row.HV10,
			&row.HV20,
			&row.HV30,
			&row.CurrentIV,
			&row.IVPercentile,
			&row.IVRank,
			&frontExpiration,
			&frontDaysToExpiry,
			&row.FrontATMIV,
			&row.FrontPutCallSkew,
			&surfaceContractCount,
			&row.LiquidityOpenInterest,
			&row.LiquidityRelativeSpread,
			&liquidityTickCount,
			&liquidityVolume,
			&liquidityTransactions,
			&liquidityContractCount,
			&liquidityActiveContractCount,
			&liquidityTradableContractCount,
			&row.LiquidityActivityRatio,
			&row.LiquidityTradabilityRatio,
			&isEarlyClose,
			&daysFromPrevHoliday,
			&daysToNextHoliday,
		); err != nil {
			return nil, false, fmt.Errorf("scan precomputed daily feature panel row: %w", err)
		}
		row.Date = normalizeCalendarDate(row.Date)
		row.PriceObservations = int(priceObservations)
		row.IVObservations = int(ivObservations)
		// Sanitize float64 pointers that ClickHouse may have stored as NaN/Inf.
		row.HV10 = sanitizeF64Ptr(row.HV10)
		row.HV20 = sanitizeF64Ptr(row.HV20)
		row.HV30 = sanitizeF64Ptr(row.HV30)
		row.CurrentIV = sanitizeF64Ptr(row.CurrentIV)
		row.IVPercentile = sanitizeF64Ptr(row.IVPercentile)
		row.IVRank = sanitizeF64Ptr(row.IVRank)
		row.FrontATMIV = sanitizeF64Ptr(row.FrontATMIV)
		row.FrontPutCallSkew = sanitizeF64Ptr(row.FrontPutCallSkew)
		row.LiquidityOpenInterest = sanitizeF64Ptr(row.LiquidityOpenInterest)
		row.LiquidityRelativeSpread = sanitizeF64Ptr(row.LiquidityRelativeSpread)
		row.LiquidityActivityRatio = sanitizeF64Ptr(row.LiquidityActivityRatio)
		row.LiquidityTradabilityRatio = sanitizeF64Ptr(row.LiquidityTradabilityRatio)
		if !frontExpiration.IsZero() && frontExpiration.UTC().Unix() != 0 {
			expiration := frontExpiration.UTC()
			row.FrontExpiration = &expiration
		}
		if frontDaysToExpiry >= 0 {
			value := int(frontDaysToExpiry)
			row.FrontDaysToExpiry = &value
		}
		if surfaceContractCount >= 0 {
			value := int(surfaceContractCount)
			row.SurfaceContractCount = &value
		}
		row.LiquidityTickCount = int(liquidityTickCount)
		row.LiquidityVolume = int(liquidityVolume)
		row.LiquidityTransactions = int(liquidityTransactions)
		row.LiquidityContractCount = int(liquidityContractCount)
		row.LiquidityActiveContracts = int(liquidityActiveContractCount)
		row.LiquidityTradableContracts = int(liquidityTradableContractCount)
		row.IsEarlyClose = isEarlyClose == 1
		if daysFromPrevHoliday >= 0 {
			value := int(daysFromPrevHoliday)
			row.DaysFromPrevHoliday = &value
		}
		if daysToNextHoliday >= 0 {
			value := int(daysToNextHoliday)
			row.DaysToNextHoliday = &value
		}
		data = append(data, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed daily feature panel rows: %w", err)
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	data = fillDailyPanelFallback(data, s.policy.FallbackWindowDays)
	data = trimDailyPanelRange(data, from, to)
	data = trimDailyPanelTrailingEmptyRows(data)
	if len(data) == 0 {
		return nil, false, nil
	}
	return &dto.FeatureDailyPanelResponse{Market: market, Underlying: underlying, LookbackDays: lookbackDays, Data: data}, true, nil
}

func (s *FeatureService) featureStoreTableExists(ctx context.Context) (bool, error) {
	return s.featureStoreRelationExists(ctx, featureSnapshotTable)
}

func (s *FeatureService) featureStoreRelationExists(ctx context.Context, relation string) (bool, error) {
	rows, err := s.repo.Query(ctx, `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {relation:String}`,
		clickhouse.Named("relation", relation),
	)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *FeatureService) latestPrecomputedSurfaceDate(ctx context.Context, table, market, underlying string) (time.Time, bool, error) {
	rows, err := s.repo.Query(ctx, fmt.Sprintf(`SELECT ifNull(maxOrNull(as_of_date), toDate('1970-01-01'))
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}`, table),
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
	)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest precomputed surface date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var asOf time.Time
	if err := rows.Scan(&asOf); err != nil {
		return time.Time{}, false, fmt.Errorf("scan latest precomputed surface date: %w", err)
	}
	if asOf.IsZero() || asOf.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return normalizeCalendarDate(asOf), true, nil
}

func (s *FeatureService) precomputedVolatilityRowsExist(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) (bool, error) {
	exists, err := s.featureStoreTableExists(ctx)
	if err != nil || !exists {
		return false, err
	}
	query := fmt.Sprintf(`SELECT count() FROM %s WHERE market = {market:String} AND underlying = {underlying:String} AND lookback_days = {lookback_days:UInt16}`, featureSnapshotTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("query precomputed volatility row count: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, fmt.Errorf("scan precomputed volatility row count: %w", err)
	}
	return count > 0, nil
}

func (s *FeatureService) deletePrecomputedVolatilityRows(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE market = {market:String} AND underlying = {underlying:String} AND lookback_days = {lookback_days:UInt16}`, featureSnapshotTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	if err := s.repo.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete precomputed volatility rows: %w", err)
	}
	return nil
}

func (s *FeatureService) precomputedSurfaceRowsExist(ctx context.Context, table, market, underlying string, from, to time.Time) (bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, table)
	if err != nil || !exists {
		return false, err
	}
	query := fmt.Sprintf(`SELECT count() FROM %s WHERE market = {market:String} AND underlying = {underlying:String}`, table)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("query precomputed surface row count: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, fmt.Errorf("scan precomputed surface row count: %w", err)
	}
	return count > 0, nil
}

func (s *FeatureService) precomputedDailyPanelRowsExist(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32) (bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureDailyPanelTable)
	if err != nil || !exists {
		return false, err
	}
	query := fmt.Sprintf(`SELECT count() FROM %s WHERE market = {market:String} AND underlying = {underlying:String} AND lookback_days = {lookback_days:UInt16} AND min_days_to_expiry = {min_dte:Int32} AND max_days_to_expiry = {max_dte:Int32}`, featureDailyPanelTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("query precomputed daily panel row count: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	var count uint64
	if err := rows.Scan(&count); err != nil {
		return false, fmt.Errorf("scan precomputed daily panel row count: %w", err)
	}
	return count > 0, nil
}

func (s *FeatureService) deletePrecomputedSurfaceRows(ctx context.Context, table, market, underlying string, from, to time.Time) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE market = {market:String} AND underlying = {underlying:String}`, table)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	if err := s.repo.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete precomputed surface rows: %w", err)
	}
	return nil
}

func (s *FeatureService) deletePrecomputedDailyPanelRows(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE market = {market:String} AND underlying = {underlying:String} AND lookback_days = {lookback_days:UInt16} AND min_days_to_expiry = {min_dte:Int32} AND max_days_to_expiry = {max_dte:Int32}`, featureDailyPanelTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("lookback_days", uint16(lookbackDays)),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	}
	if !from.IsZero() {
		query += ` AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += ` AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	if err := s.repo.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("delete precomputed daily panel rows: %w", err)
	}
	return nil
}

func (s *FeatureService) insertPrecomputedVolatilityRows(ctx context.Context, market, underlying string, lookbackDays int, history []dto.FeatureVolatilityHistoryRow) error {
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	market,
	underlying,
	lookback_days,
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank,
	updated_at
)`, featureSnapshotTable))
	if err != nil {
		return fmt.Errorf("prepare precomputed volatility batch: %w", err)
	}
	now := time.Now().UTC()
	for _, row := range history {
		if err := batch.Append(
			market,
			underlying,
			uint16(lookbackDays),
			normalizeCalendarDate(row.Date),
			uint32(row.PriceObservations),
			uint32(row.IVObservations),
			nullableFloat64(row.HV10),
			nullableFloat64(row.HV20),
			nullableFloat64(row.HV30),
			nullableFloat64(row.CurrentIV),
			nullableFloat64(row.IVPercentile),
			nullableFloat64(row.IVRank),
			now,
		); err != nil {
			return fmt.Errorf("append precomputed volatility row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send precomputed volatility batch: %w", err)
	}
	return nil
}

func (s *FeatureService) insertPrecomputedTermStructureRows(ctx context.Context, market, underlying string, rows []usOptionsSurfaceAggregateRow) error {
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	market,
	underlying,
	as_of_date,
	expiration,
	days_to_expiry,
	atm_iv,
	call_iv,
	put_iv,
	contract_count,
	updated_at
)`, featureTermStructureTable))
	if err != nil {
		return fmt.Errorf("prepare precomputed term structure batch: %w", err)
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if err := batch.Append(
			market,
			underlying,
			normalizeCalendarDate(row.AsOfDate),
			normalizeCalendarDate(row.Expiration),
			uint16(row.DaysToExpiry),
			nullableFloat64(row.ATMIV),
			nullableFloat64(row.CallIV),
			nullableFloat64(row.PutIV),
			uint32(row.ContractCount),
			now,
		); err != nil {
			return fmt.Errorf("append precomputed term structure row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send precomputed term structure batch: %w", err)
	}
	return nil
}

func (s *FeatureService) insertPrecomputedSkewRows(ctx context.Context, market, underlying string, rows []usOptionsSurfaceAggregateRow) error {
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	market,
	underlying,
	as_of_date,
	expiration,
	days_to_expiry,
	otm_call_iv,
	otm_put_iv,
	put_call_skew,
	contract_count,
	updated_at
)`, featureSkewTable))
	if err != nil {
		return fmt.Errorf("prepare precomputed skew batch: %w", err)
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if err := batch.Append(
			market,
			underlying,
			normalizeCalendarDate(row.AsOfDate),
			normalizeCalendarDate(row.Expiration),
			uint16(row.DaysToExpiry),
			nullableFloat64(row.OTMCallIV),
			nullableFloat64(row.OTMPutIV),
			nullableFloat64(putCallSkewValue(row.OTMCallIV, row.OTMPutIV)),
			uint32(row.ContractCount),
			now,
		); err != nil {
			return fmt.Errorf("append precomputed skew row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send precomputed skew batch: %w", err)
	}
	return nil
}

func (s *FeatureService) insertPrecomputedLiquidityRows(ctx context.Context, market, underlying string, rows []cryptoLiquidityAggregateRow) error {
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	market,
	underlying,
	as_of_date,
	expiration,
	days_to_expiry,
	avg_bid_close,
	avg_ask_close,
	avg_mark_close,
	relative_spread,
	open_interest,
	tick_count,
	volume,
	transactions,
	contract_count,
	active_contract_count,
	tradable_contract_count,
	activity_ratio,
	tradability_ratio,
	updated_at
)`, featureLiquidityTable))
	if err != nil {
		return fmt.Errorf("prepare precomputed liquidity batch: %w", err)
	}
	now := time.Now().UTC()
	for _, row := range rows {
		tradabilityRatio := liquidityTradabilityRatioValue(market, row)
		if err := batch.Append(
			market,
			underlying,
			normalizeCalendarDate(row.AsOfDate),
			row.Expiration.UTC(),
			uint16(row.DaysToExpiry),
			nullableFloat64(row.AvgBidClose),
			nullableFloat64(row.AvgAskClose),
			nullableFloat64(row.AvgMarkClose),
			nullableFloat64(row.RelativeSpread),
			nullableFloat64(row.OpenInterest),
			uint64(row.TickCount),
			uint64(row.Volume),
			uint64(row.Transactions),
			uint32(row.ContractCount),
			uint32(row.ActiveContractCount),
			uint32(row.TradableContractCount),
			nullableFloat64(activityRatioValue(row.ActiveContractCount, row.ContractCount)),
			nullableFloat64(tradabilityRatio),
			now,
		); err != nil {
			return fmt.Errorf("append precomputed liquidity row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send precomputed liquidity batch: %w", err)
	}
	return nil
}

func (s *FeatureService) insertPrecomputedDailyPanelRows(ctx context.Context, market, underlying string, lookbackDays int, minDTE, maxDTE int32, rows []dto.FeatureDailyPanelRow) error {
	batch, err := s.repo.PrepareBatch(ctx, fmt.Sprintf(`INSERT INTO %s (
	market,
	underlying,
	lookback_days,
	min_days_to_expiry,
	max_days_to_expiry,
	as_of_date,
	price_observations,
	iv_observations,
	hv10,
	hv20,
	hv30,
	current_iv,
	iv_percentile,
	iv_rank,
	front_expiration,
	front_days_to_expiry,
	front_atm_iv,
	front_put_call_skew,
	surface_contract_count,
	liquidity_open_interest,
	liquidity_relative_spread,
	liquidity_tick_count,
	liquidity_volume,
	liquidity_transactions,
	liquidity_contract_count,
	liquidity_active_contract_count,
	liquidity_tradable_contract_count,
	liquidity_activity_ratio,
	liquidity_tradability_ratio,
	is_early_close,
	days_from_prev_holiday,
	days_to_next_holiday,
	updated_at
)`, featureDailyPanelTable))
	if err != nil {
		return fmt.Errorf("prepare precomputed daily panel batch: %w", err)
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if err := batch.Append(
			market,
			underlying,
			uint16(lookbackDays),
			minDTE,
			maxDTE,
			normalizeCalendarDate(row.Date),
			uint32(row.PriceObservations),
			uint32(row.IVObservations),
			nullableFloat64(row.HV10),
			nullableFloat64(row.HV20),
			nullableFloat64(row.HV30),
			nullableFloat64(row.CurrentIV),
			nullableFloat64(row.IVPercentile),
			nullableFloat64(row.IVRank),
			panelTimeValue(row.FrontExpiration),
			panelInt32Value(row.FrontDaysToExpiry),
			nullableFloat64(row.FrontATMIV),
			nullableFloat64(row.FrontPutCallSkew),
			panelInt32Value(row.SurfaceContractCount),
			nullableFloat64(row.LiquidityOpenInterest),
			nullableFloat64(row.LiquidityRelativeSpread),
			uint64(row.LiquidityTickCount),
			uint64(row.LiquidityVolume),
			uint64(row.LiquidityTransactions),
			uint32(row.LiquidityContractCount),
			uint32(row.LiquidityActiveContracts),
			uint32(row.LiquidityTradableContracts),
			nullableFloat64(row.LiquidityActivityRatio),
			nullableFloat64(row.LiquidityTradabilityRatio),
			boolToUInt8(row.IsEarlyClose),
			panelInt32Value(row.DaysFromPrevHoliday),
			panelInt32Value(row.DaysToNextHoliday),
			now,
		); err != nil {
			return fmt.Errorf("append precomputed daily panel row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send precomputed daily panel batch: %w", err)
	}
	return nil
}

func scanFeatureHistoryRow(rows driver.Rows) (dto.FeatureVolatilityHistoryRow, error) {
	var (
		row               dto.FeatureVolatilityHistoryRow
		hv10              *float64
		hv20              *float64
		hv30              *float64
		currentIV         *float64
		ivPercentile      *float64
		ivRank            *float64
		priceObservations uint32
		ivObservations    uint32
	)
	if err := rows.Scan(
		&row.Date,
		&priceObservations,
		&ivObservations,
		&hv10,
		&hv20,
		&hv30,
		&currentIV,
		&ivPercentile,
		&ivRank,
	); err != nil {
		return dto.FeatureVolatilityHistoryRow{}, fmt.Errorf("scan feature history row: %w", err)
	}
	row.Date = normalizeCalendarDate(row.Date)
	row.PriceObservations = int(priceObservations)
	row.IVObservations = int(ivObservations)
	row.HV10 = hv10
	row.HV20 = hv20
	row.HV30 = hv30
	row.CurrentIV = currentIV
	row.IVPercentile = ivPercentile
	row.IVRank = ivRank
	return row, nil
}

func queryFeatureSeries(ctx context.Context, conn driver.Conn, query string, args ...interface{}) ([]featurePoint, error) {
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query feature series: %w", err)
	}
	defer rows.Close()

	series := make([]featurePoint, 0)
	for rows.Next() {
		var (
			day   time.Time
			value float64
		)
		if err := rows.Scan(&day, &value); err != nil {
			return nil, fmt.Errorf("scan feature series row: %w", err)
		}
		if !isFinitePositive(value) {
			continue
		}
		series = append(series, featurePoint{Date: normalizeCalendarDate(day), Value: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature series rows: %w", err)
	}
	return series, nil
}

func buildVolatilityHistoryRows(priceSeries, ivSeries []featurePoint, from, to time.Time, lookbackDays int, annualization float64) []dto.FeatureVolatilityHistoryRow {
	priceIndex := make(map[string]int, len(priceSeries))
	ivIndex := make(map[string]int, len(ivSeries))
	targetDates := make(map[string]time.Time, len(priceSeries)+len(ivSeries))
	for index, point := range priceSeries {
		normalizedDate := normalizeCalendarDate(point.Date)
		key := normalizedDate.Format("2006-01-02")
		priceIndex[key] = index
		if withinFeatureRange(point.Date, from, to) {
			targetDates[key] = normalizedDate
		}
	}
	for index, point := range ivSeries {
		normalizedDate := normalizeCalendarDate(point.Date)
		key := normalizedDate.Format("2006-01-02")
		ivIndex[key] = index
		if withinFeatureRange(point.Date, from, to) {
			targetDates[key] = normalizedDate
		}
	}

	sortedDates := make([]time.Time, 0, len(targetDates))
	for _, date := range targetDates {
		sortedDates = append(sortedDates, date)
	}
	sort.Slice(sortedDates, func(i, j int) bool {
		return sortedDates[i].Before(sortedDates[j])
	})

	history := make([]dto.FeatureVolatilityHistoryRow, 0, len(sortedDates))
	for _, date := range sortedDates {
		key := date.Format("2006-01-02")
		row := dto.FeatureVolatilityHistoryRow{Date: date}
		if index, ok := priceIndex[key]; ok {
			row.PriceObservations = index + 1
			row.HV10 = rollingHistoricalVolatilityAt(priceSeries, index, 10, annualization)
			row.HV20 = rollingHistoricalVolatilityAt(priceSeries, index, 20, annualization)
			row.HV30 = rollingHistoricalVolatilityAt(priceSeries, index, 30, annualization)
		}
		if index, ok := ivIndex[key]; ok {
			row.IVObservations = index + 1
			currentIV := ivSeries[index].Value
			row.CurrentIV = &currentIV
			row.IVPercentile = impliedVolatilityPercentileWindow(ivSeries, index, lookbackDays)
			row.IVRank = impliedVolatilityRankWindow(ivSeries, index, lookbackDays)
		}
		history = append(history, row)
	}
	return history
}

func buildTermStructureSnapshotRows(aggregates []usOptionsSurfaceAggregateRow) []dto.FeatureTermStructureSnapshotRow {
	rows := make([]dto.FeatureTermStructureSnapshotRow, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, dto.FeatureTermStructureSnapshotRow{
			Expiration:    normalizeCalendarDate(aggregate.Expiration),
			DaysToExpiry:  aggregate.DaysToExpiry,
			ATMIV:         aggregate.ATMIV,
			CallIV:        aggregate.CallIV,
			PutIV:         aggregate.PutIV,
			ContractCount: aggregate.ContractCount,
		})
	}
	return rows
}

func buildSkewSnapshotRows(aggregates []usOptionsSurfaceAggregateRow) []dto.FeatureSkewSnapshotRow {
	rows := make([]dto.FeatureSkewSnapshotRow, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, dto.FeatureSkewSnapshotRow{
			Expiration:    normalizeCalendarDate(aggregate.Expiration),
			DaysToExpiry:  aggregate.DaysToExpiry,
			OTMCallIV:     aggregate.OTMCallIV,
			OTMPutIV:      aggregate.OTMPutIV,
			PutCallSkew:   putCallSkewValue(aggregate.OTMCallIV, aggregate.OTMPutIV),
			ContractCount: aggregate.ContractCount,
		})
	}
	return rows
}

func buildLiquiditySnapshotRows(aggregates []cryptoLiquidityAggregateRow) []dto.FeatureLiquiditySnapshotRow {
	rows := make([]dto.FeatureLiquiditySnapshotRow, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, dto.FeatureLiquiditySnapshotRow{
			Expiration:            aggregate.Expiration.UTC(),
			DaysToExpiry:          aggregate.DaysToExpiry,
			AvgBidClose:           aggregate.AvgBidClose,
			AvgAskClose:           aggregate.AvgAskClose,
			AvgMarkClose:          aggregate.AvgMarkClose,
			RelativeSpread:        aggregate.RelativeSpread,
			OpenInterest:          aggregate.OpenInterest,
			TickCount:             aggregate.TickCount,
			Volume:                aggregate.Volume,
			Transactions:          aggregate.Transactions,
			ContractCount:         aggregate.ContractCount,
			ActiveContractCount:   aggregate.ActiveContractCount,
			TradableContractCount: aggregate.TradableContractCount,
			ActivityRatio:         activityRatioValue(aggregate.ActiveContractCount, aggregate.ContractCount),
			TradabilityRatio:      tradabilityRatioValue(aggregate.TradableContractCount, aggregate.ContractCount),
		})
	}
	return rows
}

func buildLiquidityHistoryRows(aggregates []cryptoLiquidityAggregateRow) []dto.FeatureLiquidityHistoryRow {
	rows := make([]dto.FeatureLiquidityHistoryRow, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, dto.FeatureLiquidityHistoryRow{
			AsOfDate: normalizeCalendarDate(aggregate.AsOfDate),
			FeatureLiquiditySnapshotRow: dto.FeatureLiquiditySnapshotRow{
				Expiration:            aggregate.Expiration.UTC(),
				DaysToExpiry:          aggregate.DaysToExpiry,
				AvgBidClose:           aggregate.AvgBidClose,
				AvgAskClose:           aggregate.AvgAskClose,
				AvgMarkClose:          aggregate.AvgMarkClose,
				RelativeSpread:        aggregate.RelativeSpread,
				OpenInterest:          aggregate.OpenInterest,
				TickCount:             aggregate.TickCount,
				Volume:                aggregate.Volume,
				Transactions:          aggregate.Transactions,
				ContractCount:         aggregate.ContractCount,
				ActiveContractCount:   aggregate.ActiveContractCount,
				TradableContractCount: aggregate.TradableContractCount,
				ActivityRatio:         activityRatioValue(aggregate.ActiveContractCount, aggregate.ContractCount),
				TradabilityRatio:      tradabilityRatioValue(aggregate.TradableContractCount, aggregate.ContractCount),
			},
		})
	}
	return rows
}

func summarizeLiquidityHistory(rows []dto.FeatureLiquidityHistoryRow) map[string]featureLiquidityPanelSummary {
	type accumulator struct {
		spreadWeighted float64
		spreadWeight   int
		openInterest   float64
		hasOI          bool
		featureLiquidityPanelSummary
	}
	acc := make(map[string]*accumulator)
	for _, row := range rows {
		key := row.AsOfDate.Format("2006-01-02")
		item, ok := acc[key]
		if !ok {
			item = &accumulator{}
			acc[key] = item
		}
		item.TickCount += row.TickCount
		item.Volume += row.Volume
		item.Transactions += row.Transactions
		item.ContractCount += row.ContractCount
		item.ActiveContracts += row.ActiveContractCount
		item.TradableContracts += row.TradableContractCount
		if row.OpenInterest != nil {
			item.openInterest += *row.OpenInterest
			item.hasOI = true
		}
		if row.RelativeSpread != nil && row.ContractCount > 0 {
			item.spreadWeighted += *row.RelativeSpread * float64(row.ContractCount)
			item.spreadWeight += row.ContractCount
		}
	}
	result := make(map[string]featureLiquidityPanelSummary, len(acc))
	for key, item := range acc {
		if item.hasOI {
			value := item.openInterest
			item.OpenInterest = &value
		}
		if item.spreadWeight > 0 {
			value := item.spreadWeighted / float64(item.spreadWeight)
			item.RelativeSpread = &value
		}
		item.ActivityRatio = activityRatioValue(item.ActiveContracts, item.ContractCount)
		item.TradabilityRatio = tradabilityRatioValue(item.TradableContracts, item.ContractCount)
		result[key] = item.featureLiquidityPanelSummary
	}
	return result
}

func featureFallbackWindowStart(from time.Time) time.Time {
	return from.UTC().AddDate(0, 0, -DefaultFeaturePolicy().FallbackWindowDays)
}

func latestValidVolatilitySnapshotRows(rowsDesc []dto.FeatureVolatilityHistoryRow, maxDays int) (*dto.FeatureVolatilityHistoryRow, *dto.FeatureVolatilityHistoryRow) {
	if len(rowsDesc) == 0 {
		return nil, nil
	}
	latestDate := rowsDesc[0].Date.UTC()
	latestDate = normalizeCalendarDate(latestDate)
	var priceRow *dto.FeatureVolatilityHistoryRow
	var ivRow *dto.FeatureVolatilityHistoryRow
	for index := range rowsDesc {
		row := rowsDesc[index]
		if latestDate.Sub(normalizeCalendarDate(row.Date)) > time.Duration(maxDays)*24*time.Hour {
			break
		}
		if priceRow == nil && volatilityPriceGroupPresent(row) {
			copy := row
			priceRow = &copy
		}
		if ivRow == nil && volatilityIVGroupPresent(row) {
			copy := row
			ivRow = &copy
		}
		if priceRow != nil && ivRow != nil {
			break
		}
	}
	return priceRow, ivRow
}

func fillVolatilityHistoryFallback(rows []dto.FeatureVolatilityHistoryRow, maxDays int) []dto.FeatureVolatilityHistoryRow {
	filled := append([]dto.FeatureVolatilityHistoryRow(nil), rows...)
	var lastPrice *dto.FeatureVolatilityHistoryRow
	var lastIV *dto.FeatureVolatilityHistoryRow
	for index := range filled {
		row := &filled[index]
		if volatilityPriceGroupPresent(*row) {
			copy := *row
			lastPrice = &copy
		} else if lastPrice != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastPrice.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.PriceObservations = lastPrice.PriceObservations
			row.HV10 = lastPrice.HV10
			row.HV20 = lastPrice.HV20
			row.HV30 = lastPrice.HV30
		}
		if volatilityIVGroupPresent(*row) {
			copy := *row
			lastIV = &copy
		} else if lastIV != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastIV.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.IVObservations = lastIV.IVObservations
			row.CurrentIV = lastIV.CurrentIV
			row.IVPercentile = lastIV.IVPercentile
			row.IVRank = lastIV.IVRank
		}
	}
	return filled
}

func trimVolatilityHistoryRange(rows []dto.FeatureVolatilityHistoryRow, from, to time.Time) []dto.FeatureVolatilityHistoryRow {
	trimmed := make([]dto.FeatureVolatilityHistoryRow, 0, len(rows))
	for _, row := range rows {
		if !row.Date.Before(from) && row.Date.Before(to) {
			trimmed = append(trimmed, row)
		}
	}
	return trimmed
}

func fillDailyPanelFallback(rows []dto.FeatureDailyPanelRow, maxDays int) []dto.FeatureDailyPanelRow {
	filled := append([]dto.FeatureDailyPanelRow(nil), rows...)
	var lastVolPrice *dto.FeatureDailyPanelRow
	var lastVolIV *dto.FeatureDailyPanelRow
	var lastSurface *dto.FeatureDailyPanelRow
	var lastLiquidity *dto.FeatureDailyPanelRow
	for index := range filled {
		row := &filled[index]
		if dailyPanelVolatilityPricePresent(*row) {
			copy := *row
			lastVolPrice = &copy
		} else if lastVolPrice != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastVolPrice.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.PriceObservations = lastVolPrice.PriceObservations
			row.HV10 = lastVolPrice.HV10
			row.HV20 = lastVolPrice.HV20
			row.HV30 = lastVolPrice.HV30
		}
		if dailyPanelVolatilityIVPresent(*row) {
			copy := *row
			lastVolIV = &copy
		} else if lastVolIV != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastVolIV.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.IVObservations = lastVolIV.IVObservations
			row.CurrentIV = lastVolIV.CurrentIV
			row.IVPercentile = lastVolIV.IVPercentile
			row.IVRank = lastVolIV.IVRank
		}
		if dailyPanelSurfacePresent(*row) {
			copy := *row
			lastSurface = &copy
		} else if lastSurface != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastSurface.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.FrontExpiration = lastSurface.FrontExpiration
			row.FrontDaysToExpiry = lastSurface.FrontDaysToExpiry
			row.FrontATMIV = lastSurface.FrontATMIV
			row.FrontPutCallSkew = lastSurface.FrontPutCallSkew
			row.SurfaceContractCount = lastSurface.SurfaceContractCount
		}
		if dailyPanelLiquidityPresent(*row) {
			copy := *row
			lastLiquidity = &copy
		} else if lastLiquidity != nil && normalizeCalendarDate(row.Date).Sub(normalizeCalendarDate(lastLiquidity.Date)) <= time.Duration(maxDays)*24*time.Hour {
			row.LiquidityOpenInterest = lastLiquidity.LiquidityOpenInterest
			row.LiquidityRelativeSpread = lastLiquidity.LiquidityRelativeSpread
			row.LiquidityTickCount = lastLiquidity.LiquidityTickCount
			row.LiquidityVolume = lastLiquidity.LiquidityVolume
			row.LiquidityTransactions = lastLiquidity.LiquidityTransactions
			row.LiquidityContractCount = lastLiquidity.LiquidityContractCount
			row.LiquidityActiveContracts = lastLiquidity.LiquidityActiveContracts
			row.LiquidityTradableContracts = lastLiquidity.LiquidityTradableContracts
			row.LiquidityActivityRatio = lastLiquidity.LiquidityActivityRatio
			row.LiquidityTradabilityRatio = lastLiquidity.LiquidityTradabilityRatio
		}
	}
	return filled
}

func trimDailyPanelRange(rows []dto.FeatureDailyPanelRow, from, to time.Time) []dto.FeatureDailyPanelRow {
	trimmed := make([]dto.FeatureDailyPanelRow, 0, len(rows))
	for _, row := range rows {
		if !row.Date.Before(from) && row.Date.Before(to) {
			trimmed = append(trimmed, row)
		}
	}
	return trimmed
}

func trimDailyPanelTrailingEmptyRows(rows []dto.FeatureDailyPanelRow) []dto.FeatureDailyPanelRow {
	lastValid := -1
	for index, row := range rows {
		if dailyPanelFeatureDataPresent(row) {
			lastValid = index
		}
	}
	if lastValid < 0 {
		return nil
	}
	return append([]dto.FeatureDailyPanelRow(nil), rows[:lastValid+1]...)
}

func volatilityPriceGroupPresent(row dto.FeatureVolatilityHistoryRow) bool {
	return row.HV10 != nil || row.HV20 != nil || row.HV30 != nil
}

func volatilityIVGroupPresent(row dto.FeatureVolatilityHistoryRow) bool {
	return row.CurrentIV != nil || row.IVPercentile != nil || row.IVRank != nil
}

func dailyPanelVolatilityPricePresent(row dto.FeatureDailyPanelRow) bool {
	return row.HV10 != nil || row.HV20 != nil || row.HV30 != nil
}

func dailyPanelVolatilityIVPresent(row dto.FeatureDailyPanelRow) bool {
	return row.CurrentIV != nil || row.IVPercentile != nil || row.IVRank != nil
}

func dailyPanelSurfacePresent(row dto.FeatureDailyPanelRow) bool {
	return row.FrontExpiration != nil || row.FrontDaysToExpiry != nil || row.FrontATMIV != nil || row.FrontPutCallSkew != nil || row.SurfaceContractCount != nil
}

func dailyPanelLiquidityPresent(row dto.FeatureDailyPanelRow) bool {
	return row.LiquidityOpenInterest != nil || row.LiquidityRelativeSpread != nil || row.LiquidityTickCount > 0 || row.LiquidityVolume > 0 || row.LiquidityTransactions > 0 || row.LiquidityContractCount > 0 || row.LiquidityActiveContracts > 0 || row.LiquidityTradableContracts > 0 || row.LiquidityActivityRatio != nil || row.LiquidityTradabilityRatio != nil
}

func dailyPanelFeatureDataPresent(row dto.FeatureDailyPanelRow) bool {
	return row.PriceObservations > 0 || row.IVObservations > 0 || dailyPanelVolatilityPricePresent(row) || dailyPanelVolatilityIVPresent(row) || dailyPanelSurfacePresent(row) || dailyPanelLiquidityPresent(row)
}

func summarizeUSOptionsSurfaceHistory(rows []usOptionsSurfaceAggregateRow) map[string]featureSurfacePanelSummary {
	result := make(map[string]featureSurfacePanelSummary)
	for _, row := range rows {
		key := row.AsOfDate.Format("2006-01-02")
		if _, exists := result[key]; exists {
			continue
		}
		expiration := normalizeCalendarDate(row.Expiration)
		dte := row.DaysToExpiry
		contracts := row.ContractCount
		result[key] = featureSurfacePanelSummary{
			Expiration:    &expiration,
			DaysToExpiry:  &dte,
			ATMIV:         row.ATMIV,
			PutCallSkew:   putCallSkewValue(row.OTMCallIV, row.OTMPutIV),
			ContractCount: &contracts,
		}
	}
	return result
}

func mergeFeatureSurfacePanelSummaries(base, overlay map[string]featureSurfacePanelSummary) map[string]featureSurfacePanelSummary {
	if len(base) == 0 && len(overlay) == 0 {
		return map[string]featureSurfacePanelSummary{}
	}
	result := make(map[string]featureSurfacePanelSummary, len(base)+len(overlay))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overlay {
		current := result[key]
		if value.Expiration != nil {
			current.Expiration = value.Expiration
		}
		if value.DaysToExpiry != nil {
			current.DaysToExpiry = value.DaysToExpiry
		}
		if value.ATMIV != nil {
			current.ATMIV = value.ATMIV
		}
		if value.PutCallSkew != nil {
			current.PutCallSkew = value.PutCallSkew
		}
		if value.ContractCount != nil {
			current.ContractCount = value.ContractCount
		}
		result[key] = current
	}
	return result
}

func (s *FeatureService) queryPrecomputedPanelTermStructureSummary(ctx context.Context, market, underlying string, from, to time.Time, minDTE, maxDTE int32) (map[string]featureSurfacePanelSummary, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureTermStructureTable)
	if err != nil || !exists {
		return nil, false, err
	}
	query := fmt.Sprintf(`SELECT
	as_of_date,
	expiration,
	days_to_expiry,
	atm_iv,
	contract_count
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}`, featureTermStructureTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	}
	if !from.IsZero() {
		query += `
  AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += `
  AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	query += `
ORDER BY as_of_date ASC, expiration ASC`
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed panel term structure summary: %w", err)
	}
	defer rows.Close()

	result := make(map[string]featureSurfacePanelSummary)
	for rows.Next() {
		var (
			asOfDate      time.Time
			expiration    time.Time
			daysToExpiry  uint16
			atmIV         *float64
			contractCount uint32
		)
		if err := rows.Scan(&asOfDate, &expiration, &daysToExpiry, &atmIV, &contractCount); err != nil {
			return nil, false, fmt.Errorf("scan precomputed panel term structure summary row: %w", err)
		}
		normalizedAsOfDate := normalizeCalendarDate(asOfDate)
		key := normalizedAsOfDate.Format("2006-01-02")
		if _, exists := result[key]; exists {
			continue
		}
		expiration = normalizeCalendarDate(expiration)
		dte := int(daysToExpiry)
		contracts := int(contractCount)
		result[key] = featureSurfacePanelSummary{
			Expiration:    &expiration,
			DaysToExpiry:  &dte,
			ATMIV:         sanitizeF64Ptr(atmIV),
			ContractCount: &contracts,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed panel term structure summary rows: %w", err)
	}
	return result, len(result) > 0, nil
}

func (s *FeatureService) queryPrecomputedPanelSkewSummary(ctx context.Context, market, underlying string, from, to time.Time, minDTE, maxDTE int32) (map[string]featureSurfacePanelSummary, bool, error) {
	exists, err := s.featureStoreRelationExists(ctx, featureSkewTable)
	if err != nil || !exists {
		return nil, false, err
	}
	query := fmt.Sprintf(`SELECT
	as_of_date,
	put_call_skew,
	contract_count,
	expiration
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}
  AND days_to_expiry >= {min_dte:Int32}
  AND days_to_expiry <= {max_dte:Int32}`, featureSkewTable)
	args := []interface{}{
		clickhouse.Named("market", market),
		clickhouse.Named("underlying", underlying),
		clickhouse.Named("min_dte", minDTE),
		clickhouse.Named("max_dte", maxDTE),
	}
	if !from.IsZero() {
		query += `
  AND as_of_date >= toDate({from:String})`
		args = append(args, clickhouse.Named("from", from.UTC().Format("2006-01-02")))
	}
	if !to.IsZero() {
		query += `
  AND as_of_date < toDate({to:String})`
		args = append(args, clickhouse.Named("to", to.UTC().Format("2006-01-02")))
	}
	query += `
ORDER BY as_of_date ASC, expiration ASC`
	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("query precomputed panel skew summary: %w", err)
	}
	defer rows.Close()

	result := make(map[string]featureSurfacePanelSummary)
	for rows.Next() {
		var (
			asOfDate      time.Time
			putCallSkew   *float64
			contractCount uint32
			expiration    time.Time
		)
		if err := rows.Scan(&asOfDate, &putCallSkew, &contractCount, &expiration); err != nil {
			return nil, false, fmt.Errorf("scan precomputed panel skew summary row: %w", err)
		}
		key := normalizeCalendarDate(asOfDate).Format("2006-01-02")
		if _, exists := result[key]; exists {
			continue
		}
		contracts := int(contractCount)
		result[key] = featureSurfacePanelSummary{
			PutCallSkew:   sanitizeF64Ptr(putCallSkew),
			ContractCount: &contracts,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate precomputed panel skew summary rows: %w", err)
	}
	return result, len(result) > 0, nil
}

func (s *FeatureService) loadPanelUSOptionsSurfaceSummary(ctx context.Context, underlying string, from, to time.Time, minDTE, maxDTE int32) (map[string]featureSurfacePanelSummary, error) {
	termSummary, termFound, err := s.queryPrecomputedPanelTermStructureSummary(ctx, "us-options", underlying, from, to, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	skewSummary, skewFound, err := s.queryPrecomputedPanelSkewSummary(ctx, "us-options", underlying, from, to, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	if termFound || skewFound {
		return mergeFeatureSurfacePanelSummaries(termSummary, skewSummary), nil
	}
	aggregates, err := s.queryUSOptionsSurfaceAggregates(ctx, underlying, from, to, time.Time{}, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	return summarizeUSOptionsSurfaceHistory(aggregates), nil
}

func (s *FeatureService) loadPanelVolatilityHistory(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int) ([]dto.FeatureVolatilityHistoryRow, error) {
	if precomputed, ok, err := s.queryPrecomputedVolatilityHistory(ctx, market, underlying, from, to, lookbackDays); err != nil {
		return nil, err
	} else if ok {
		return precomputed.Data, nil
	}
	history, _, _, err := s.computeVolatilityHistory(ctx, market, underlying, from, to, lookbackDays)
	if err != nil {
		return nil, err
	}
	return history, nil
}

func (s *FeatureService) loadPanelLiquidityHistory(ctx context.Context, market, underlying string, from, to time.Time, minDTE, maxDTE int32) ([]dto.FeatureLiquidityHistoryRow, error) {
	if precomputed, ok, err := s.queryPrecomputedLiquidityHistory(ctx, market, underlying, from, to, minDTE, maxDTE); err != nil {
		return nil, err
	} else if ok {
		return precomputed.Data, nil
	}
	aggregates, err := s.queryLiquidityAggregates(ctx, market, underlying, from, to, time.Time{}, minDTE, maxDTE)
	if err != nil {
		return nil, err
	}
	return buildLiquidityHistoryRows(aggregates), nil
}

func (s *FeatureService) buildDailyFeaturePanelRows(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32) ([]dto.FeatureDailyPanelRow, error) {
	return s.buildDailyFeaturePanelRowsWithInputs(ctx, market, underlying, from, to, lookbackDays, minDTE, maxDTE, featureBackfillPanelInputs{})
}

func (s *FeatureService) buildDailyFeaturePanelRowsWithInputs(ctx context.Context, market, underlying string, from, to time.Time, lookbackDays int, minDTE, maxDTE int32, inputs featureBackfillPanelInputs) ([]dto.FeatureDailyPanelRow, error) {
	var (
		volHistory       []dto.FeatureVolatilityHistoryRow
		liquidityHistory []dto.FeatureLiquidityHistoryRow
		err              error
	)
	if inputs.HasVolatilityHistory {
		volHistory = inputs.VolatilityHistory
	} else {
		volHistory, err = s.loadPanelVolatilityHistory(ctx, market, underlying, from, to, lookbackDays)
		if err != nil {
			return nil, err
		}
	}
	if inputs.HasLiquidityHistory {
		liquidityHistory = inputs.LiquidityHistory
	} else {
		liquidityHistory, err = s.loadPanelLiquidityHistory(ctx, market, underlying, from, to, minDTE, maxDTE)
		if err != nil {
			return nil, err
		}
	}
	eventHistory := make(map[string]dto.FeatureEventWindowHistoryRow)
	if market == "us-options" {
		rows, err := s.queryEventWindowHistoryRows(ctx, market, underlying, from, to)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			eventHistory[row.Date.Format("2006-01-02")] = row
		}
	}
	surfaceSummary := make(map[string]featureSurfacePanelSummary)
	if market == "us-options" {
		if inputs.HasSurfaceSummary {
			surfaceSummary = inputs.SurfaceSummary
		} else {
			surfaceSummary, err = s.loadPanelUSOptionsSurfaceSummary(ctx, underlying, from, to, minDTE, maxDTE)
			if err != nil {
				return nil, err
			}
		}
	}
	rows := mergeDailyFeaturePanelRows(volHistory, liquidityHistory, surfaceSummary, eventHistory)
	if market == "us-options" {
		rows = filterDailyPanelRowsToMarketSessions(rows, eventHistory)
	}
	return trimDailyPanelTrailingEmptyRows(rows), nil
}

func mergeDailyFeaturePanelRows(volHistory []dto.FeatureVolatilityHistoryRow, liquidityHistory []dto.FeatureLiquidityHistoryRow, surfaceSummary map[string]featureSurfacePanelSummary, eventHistory map[string]dto.FeatureEventWindowHistoryRow) []dto.FeatureDailyPanelRow {
	dateSet := make(map[string]time.Time)
	for _, row := range volHistory {
		dateSet[row.Date.Format("2006-01-02")] = row.Date.UTC()
	}
	for _, row := range liquidityHistory {
		dateSet[row.AsOfDate.Format("2006-01-02")] = row.AsOfDate.UTC()
	}
	for key, row := range eventHistory {
		dateSet[key] = row.Date.UTC()
	}
	sortedDates := make([]time.Time, 0, len(dateSet))
	for _, date := range dateSet {
		sortedDates = append(sortedDates, date)
	}
	sort.Slice(sortedDates, func(i, j int) bool { return sortedDates[i].Before(sortedDates[j]) })

	volIndex := make(map[string]dto.FeatureVolatilityHistoryRow, len(volHistory))
	for _, row := range volHistory {
		volIndex[row.Date.Format("2006-01-02")] = row
	}
	liquiditySummary := summarizeLiquidityHistory(liquidityHistory)

	panelRows := make([]dto.FeatureDailyPanelRow, 0, len(sortedDates))
	for _, date := range sortedDates {
		key := date.Format("2006-01-02")
		row := dto.FeatureDailyPanelRow{Date: normalizeCalendarDate(date)}
		if volRow, ok := volIndex[key]; ok {
			row.PriceObservations = volRow.PriceObservations
			row.IVObservations = volRow.IVObservations
			row.HV10 = volRow.HV10
			row.HV20 = volRow.HV20
			row.HV30 = volRow.HV30
			row.CurrentIV = volRow.CurrentIV
			row.IVPercentile = volRow.IVPercentile
			row.IVRank = volRow.IVRank
		}
		if liq, ok := liquiditySummary[key]; ok {
			row.LiquidityOpenInterest = liq.OpenInterest
			row.LiquidityRelativeSpread = liq.RelativeSpread
			row.LiquidityTickCount = liq.TickCount
			row.LiquidityVolume = liq.Volume
			row.LiquidityTransactions = liq.Transactions
			row.LiquidityContractCount = liq.ContractCount
			row.LiquidityActiveContracts = liq.ActiveContracts
			row.LiquidityTradableContracts = liq.TradableContracts
			row.LiquidityActivityRatio = liq.ActivityRatio
			row.LiquidityTradabilityRatio = liq.TradabilityRatio
		}
		if surface, ok := surfaceSummary[key]; ok {
			row.FrontExpiration = surface.Expiration
			row.FrontDaysToExpiry = surface.DaysToExpiry
			row.FrontATMIV = surface.ATMIV
			row.FrontPutCallSkew = surface.PutCallSkew
			row.SurfaceContractCount = surface.ContractCount
		}
		if event, ok := eventHistory[key]; ok {
			row.IsEarlyClose = event.IsEarlyClose
			row.DaysFromPrevHoliday = event.DaysFromPrevHoliday
			row.DaysToNextHoliday = event.DaysToNextHoliday
		}
		panelRows = append(panelRows, row)
	}
	return panelRows
}

func filterDailyPanelRowsToMarketSessions(rows []dto.FeatureDailyPanelRow, sessions map[string]dto.FeatureEventWindowHistoryRow) []dto.FeatureDailyPanelRow {
	if len(rows) == 0 || len(sessions) == 0 {
		return rows
	}
	filtered := make([]dto.FeatureDailyPanelRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := sessions[row.Date.Format("2006-01-02")]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (s *FeatureService) queryEventWindowHistoryRows(ctx context.Context, market, underlying string, from, to time.Time) ([]dto.FeatureEventWindowHistoryRow, error) {
	rows, err := s.repo.Query(ctx, `SELECT market_date, is_early_close
FROM us_equity_sessions
WHERE market_date >= toDate({from:String})
  AND market_date < toDate({to:String})
  AND is_holiday = 0
ORDER BY market_date ASC`,
		clickhouse.Named("from", from.UTC().Format("2006-01-02")),
		clickhouse.Named("to", to.UTC().Format("2006-01-02")),
	)
	if err != nil {
		return nil, fmt.Errorf("query event-window history rows: %w", err)
	}
	defer rows.Close()
	holidayRows, err := s.repo.Query(ctx, `SELECT market_date
FROM us_equity_sessions
WHERE is_holiday = 1
ORDER BY market_date ASC`)
	if err != nil {
		return nil, fmt.Errorf("query holiday calendar rows: %w", err)
	}
	defer holidayRows.Close()
	holidays := make([]time.Time, 0)
	for holidayRows.Next() {
		var holiday time.Time
		if err := holidayRows.Scan(&holiday); err != nil {
			return nil, fmt.Errorf("scan holiday calendar row: %w", err)
		}
		holidays = append(holidays, normalizeCalendarDate(holiday))
	}
	if err := holidayRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate holiday calendar rows: %w", err)
	}
	result := make([]dto.FeatureEventWindowHistoryRow, 0)
	for rows.Next() {
		var (
			date         time.Time
			isEarlyClose uint8
		)
		if err := rows.Scan(&date, &isEarlyClose); err != nil {
			return nil, fmt.Errorf("scan event-window history row: %w", err)
		}
		normalizedDate := normalizeCalendarDate(date)
		row := dto.FeatureEventWindowHistoryRow{
			Date: normalizedDate,
			FeatureEventWindowSnapshotResponse: dto.FeatureEventWindowSnapshotResponse{
				Market:       market,
				Underlying:   underlying,
				AsOfDate:     timePtr(normalizedDate),
				IsEarlyClose: isEarlyClose == 1,
			},
		}
		applyHolidayDistance(&row.FeatureEventWindowSnapshotResponse, holidays, normalizedDate)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event-window history rows: %w", err)
	}
	return result, nil
}

func applyHolidayDistance(resp *dto.FeatureEventWindowSnapshotResponse, holidays []time.Time, asOf time.Time) {
	index := sort.Search(len(holidays), func(i int) bool { return !holidays[i].Before(asOf) })
	if index < len(holidays) && holidays[index].Equal(asOf) {
		index++
	}
	if index > 0 {
		prev := normalizeCalendarDate(holidays[index-1])
		resp.PreviousHolidayDate = &prev
		days := int(asOf.Sub(prev).Hours() / 24)
		resp.DaysFromPrevHoliday = &days
	}
	if index < len(holidays) {
		next := normalizeCalendarDate(holidays[index])
		resp.NextHolidayDate = &next
		days := int(next.Sub(asOf).Hours() / 24)
		resp.DaysToNextHoliday = &days
	}
}

func timePtr(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}

func panelTimeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func panelInt32Value(value *int) int32 {
	if value == nil {
		return -1
	}
	return int32(*value)
}

func boolToUInt8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func putCallSkewValue(callIV, putIV *float64) *float64 {
	if callIV == nil || putIV == nil {
		return nil
	}
	value := *putIV - *callIV
	return &value
}

func tradabilityRatioValue(tradableContracts, contractCount int) *float64 {
	if contractCount <= 0 {
		return nil
	}
	value := float64(tradableContracts) / float64(contractCount)
	return &value
}

func liquidityTradabilityRatioValue(market string, row cryptoLiquidityAggregateRow) *float64 {
	if market == "us-options" && row.TradableContractCount == 0 && row.AvgBidClose == nil && row.AvgAskClose == nil {
		return nil
	}
	return tradabilityRatioValue(row.TradableContractCount, row.ContractCount)
}

func activityRatioValue(activeContracts, contractCount int) *float64 {
	if contractCount <= 0 {
		return nil
	}
	value := float64(activeContracts) / float64(contractCount)
	return &value
}

func (s *FeatureService) populateEventWindowFlags(ctx context.Context, resp *dto.FeatureEventWindowSnapshotResponse, asOf time.Time) error {
	rows, err := s.repo.Query(ctx, `SELECT is_early_close
FROM us_equity_sessions
WHERE market_date = toDate({as_of:String})`, clickhouse.Named("as_of", asOf.UTC().Format("2006-01-02")))
	if err != nil {
		return fmt.Errorf("query event-window session row: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var isEarlyClose uint8
		if err := rows.Scan(&isEarlyClose); err != nil {
			return fmt.Errorf("scan event-window session row: %w", err)
		}
		resp.IsEarlyClose = isEarlyClose == 1
	}
	prevHoliday, hasPrev, err := s.nearestHolidayDate(ctx, asOf, false)
	if err != nil {
		return err
	}
	if hasPrev {
		prev := normalizeCalendarDate(prevHoliday)
		resp.PreviousHolidayDate = &prev
		days := int(asOf.Sub(prevHoliday).Hours() / 24)
		resp.DaysFromPrevHoliday = &days
	}
	nextHoliday, hasNext, err := s.nearestHolidayDate(ctx, asOf, true)
	if err != nil {
		return err
	}
	if hasNext {
		next := normalizeCalendarDate(nextHoliday)
		resp.NextHolidayDate = &next
		days := int(nextHoliday.Sub(asOf).Hours() / 24)
		resp.DaysToNextHoliday = &days
	}
	return nil
}

func (s *FeatureService) nearestHolidayDate(ctx context.Context, asOf time.Time, forward bool) (time.Time, bool, error) {
	query := `SELECT ifNull(`
	if forward {
		query += `minOrNull(market_date)`
	} else {
		query += `maxOrNull(market_date)`
	}
	query += `, toDate('1970-01-01'))
FROM us_equity_sessions
WHERE is_holiday = 1`
	argName := "as_of"
	if forward {
		query += ` AND market_date > toDate({as_of:String})`
	} else {
		query += ` AND market_date < toDate({as_of:String})`
	}
	rows, err := s.repo.Query(ctx, query, clickhouse.Named(argName, asOf.UTC().Format("2006-01-02")))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query nearest holiday date: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return time.Time{}, false, nil
	}
	var holiday time.Time
	if err := rows.Scan(&holiday); err != nil {
		return time.Time{}, false, fmt.Errorf("scan nearest holiday date: %w", err)
	}
	if holiday.IsZero() || holiday.UTC().Unix() == 0 {
		return time.Time{}, false, nil
	}
	return normalizeCalendarDate(holiday), true, nil
}

func withinFeatureRange(value, from, to time.Time) bool {
	if !from.IsZero() && value.Before(from) {
		return false
	}
	if !to.IsZero() && !value.Before(to) {
		return false
	}
	return true
}

func rollingHistoricalVolatility(prices []featurePoint, window int, annualization float64) *float64 {
	return rollingHistoricalVolatilityAt(prices, len(prices)-1, window, annualization)
}

func rollingHistoricalVolatilityAt(prices []featurePoint, endIndex, window int, annualization float64) *float64 {
	if endIndex < 0 || len(prices) < window+1 || endIndex < window || window < 2 {
		return nil
	}
	returns := make([]float64, 0, window)
	start := endIndex - window + 1
	for i := start; i <= endIndex; i++ {
		prev := prices[i-1].Value
		curr := prices[i].Value
		if !isFinitePositive(prev) || !isFinitePositive(curr) {
			return nil
		}
		returns = append(returns, math.Log(curr/prev))
	}
	stddev := sampleStdDev(returns)
	if math.IsNaN(stddev) {
		return nil
	}
	value := stddev * math.Sqrt(annualization)
	return &value
}

func impliedVolatilityPercentile(values []featurePoint) *float64 {
	return impliedVolatilityPercentileWindow(values, len(values)-1, len(values))
}

func impliedVolatilityPercentileWindow(values []featurePoint, endIndex, lookbackDays int) *float64 {
	window := featureWindowBounds(len(values), endIndex, lookbackDays)
	if window.start < 0 {
		return nil
	}
	latest := values[endIndex].Value
	count := 0
	total := 0
	for _, point := range values[window.start : endIndex+1] {
		if !isFinitePositive(point.Value) {
			continue
		}
		total++
		if point.Value <= latest {
			count++
		}
	}
	if total == 0 {
		return nil
	}
	percentile := (float64(count) / float64(total)) * 100
	return &percentile
}

func impliedVolatilityRank(values []featurePoint) *float64 {
	return impliedVolatilityRankWindow(values, len(values)-1, len(values))
}

func impliedVolatilityRankWindow(values []featurePoint, endIndex, lookbackDays int) *float64 {
	window := featureWindowBounds(len(values), endIndex, lookbackDays)
	if window.start < 0 {
		return nil
	}
	latest := values[endIndex].Value
	minValue := latest
	maxValue := latest
	found := false
	for _, point := range values[window.start : endIndex+1] {
		if !isFinitePositive(point.Value) {
			continue
		}
		found = true
		if point.Value < minValue {
			minValue = point.Value
		}
		if point.Value > maxValue {
			maxValue = point.Value
		}
	}
	if !found {
		return nil
	}
	if maxValue == minValue {
		value := 100.0
		return &value
	}
	rank := ((latest - minValue) / (maxValue - minValue)) * 100
	return &rank
}

type featureWindow struct {
	start int
}

func featureWindowBounds(length, endIndex, lookbackDays int) featureWindow {
	if length == 0 || endIndex < 0 || endIndex >= length {
		return featureWindow{start: -1}
	}
	if lookbackDays <= 0 || lookbackDays > length {
		lookbackDays = length
	}
	start := endIndex - lookbackDays + 1
	if start < 0 {
		start = 0
	}
	return featureWindow{start: start}
}

func sampleStdDev(values []float64) float64 {
	if len(values) < 2 {
		return math.NaN()
	}
	mean := 0.0
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	variance := 0.0
	for _, value := range values {
		delta := value - mean
		variance += delta * delta
	}
	variance /= float64(len(values) - 1)
	return math.Sqrt(variance)
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

// sanitizeF64Ptr converts a *float64 that holds NaN or ±Inf to nil so it
// serialises as JSON null instead of causing "unsupported value: NaN/Inf".
func sanitizeF64Ptr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	if math.IsNaN(*p) || math.IsInf(*p, 0) {
		return nil
	}
	return p
}
