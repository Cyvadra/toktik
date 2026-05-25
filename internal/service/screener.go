package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	usTurnoverIntersectionCacheTTL       = 20 * time.Hour
	usTurnoverIntersectionSharedTopLimit = 60
)

// ScreenerService provides condition-based screening for underlyings and options.
type ScreenerService struct {
	repo        *chrepo.Repo
	cache       cache.Store
	companyInfo usStockCompanyProfileProvider
}

type usTurnoverStockCandidate struct {
	Underlying       string
	JoinUnderlying   string
	StockTurnoverUSD float64
	StockVolume      float64
	StockTradingDays int
}

type usTurnoverOptionAggregate struct {
	JoinUnderlying    string
	OptionTurnoverUSD float64
	OptionVolume      float64
	OptionTradingDays int
}

func NewScreenerService(repo *chrepo.Repo, cacheStore ...cache.Store) *ScreenerService {
	service := &ScreenerService{repo: repo}
	if len(cacheStore) > 0 {
		service.cache = cacheStore[0]
	}
	return service
}

func (s *ScreenerService) WithCompanyProfileProvider(provider usStockCompanyProfileProvider) *ScreenerService {
	if s == nil {
		return nil
	}
	s.companyInfo = provider
	return s
}

func (s *ScreenerService) ScreenUSTurnoverIntersection(ctx context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	lookbackDays := clamp(req.LookbackDays, 20, 252)
	candidateLimit := turnoverIntersectionCandidateLimit(limit)
	cacheKey := usTurnoverIntersectionCacheKey(lookbackDays, req.NonETFOnly)
	stockUniverseFilter := ""
	if req.NonETFOnly {
		stockUniverseFilter = usStocksFundamentalsUniverseFilterClause("b.symbol")
	}

	if cached, ok, err := s.loadUSTurnoverIntersectionFromCache(ctx, cacheKey, limit); err == nil && ok {
		return cached, nil
	}

	stockCandidates, err := s.loadUSTurnoverIntersectionStockCandidates(ctx, lookbackDays, candidateLimit, stockUniverseFilter)
	if err != nil {
		return nil, fmt.Errorf("screen us turnover intersection stock candidates: %w", err)
	}
	results := make([]dto.ScreenedUSTurnoverIntersectionRow, 0, limit)
	if len(stockCandidates) > 0 {
		joinUnderlyings := make([]string, 0, len(stockCandidates))
		for _, candidate := range stockCandidates {
			joinUnderlyings = append(joinUnderlyings, candidate.JoinUnderlying)
		}
		optionAggregates, err := s.loadUSTurnoverIntersectionOptionAggregates(ctx, lookbackDays, joinUnderlyings)
		if err != nil {
			return nil, fmt.Errorf("screen us turnover intersection option aggregates: %w", err)
		}
		for _, candidate := range stockCandidates {
			optionAggregate, ok := optionAggregates[candidate.JoinUnderlying]
			if !ok {
				continue
			}
			row := dto.ScreenedUSTurnoverIntersectionRow{
				Underlying:          candidate.Underlying,
				StockTurnoverUSD:    sanitizeFloatValue(candidate.StockTurnoverUSD),
				StockVolume:         sanitizeFloatValue(candidate.StockVolume),
				StockTradingDays:    candidate.StockTradingDays,
				OptionTurnoverUSD:   sanitizeFloatValue(optionAggregate.OptionTurnoverUSD),
				OptionVolume:        sanitizeFloatValue(optionAggregate.OptionVolume),
				OptionTradingDays:   optionAggregate.OptionTradingDays,
				CombinedTurnoverUSD: sanitizeFloatValue(candidate.StockTurnoverUSD + optionAggregate.OptionTurnoverUSD),
			}
			results = append(results, row)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].CombinedTurnoverUSD == results[j].CombinedTurnoverUSD {
			return results[i].Underlying < results[j].Underlying
		}
		return results[i].CombinedTurnoverUSD > results[j].CombinedTurnoverUSD
	})
	if len(results) > limit {
		results = results[:limit]
	}
	if req.NonETFOnly {
		results = s.filterNonETFUSTurnoverResults(ctx, results)
		if len(results) > limit {
			results = results[:limit]
		}
	}

	resp := &dto.ScreenUSTurnoverIntersectionResponse{
		LookbackDays:   lookbackDays,
		Limit:          limit,
		CandidateLimit: candidateLimit,
		Data:           results,
	}
	_ = s.storeUSTurnoverIntersectionInCache(ctx, cacheKey, resp)
	return resp, nil
}

func (s *ScreenerService) loadUSTurnoverIntersectionStockCandidates(ctx context.Context, lookbackDays, candidateLimit int, stockUniverseFilter string) ([]usTurnoverStockCandidate, error) {
	query := fmt.Sprintf(`
WITH stock_start_date AS (
	SELECT min(market_ts) AS start_ts
	FROM (
		SELECT DISTINCT timestamp AS market_ts
		FROM us_stocks_bar_1d
		ORDER BY market_ts DESC
		LIMIT %d
	)
)
SELECT
	underlying,
	%s AS join_underlying,
	sum(close * volume) AS stock_turnover_usd,
	sum(volume) AS stock_volume,
	toUInt32(countDistinct(day)) AS stock_trading_days
FROM (
	SELECT
		b.symbol AS underlying,
		b.timestamp AS day,
		%s AS close,
		toFloat64(b.volume) AS volume
	FROM us_stocks_bar_1d AS b
	%s
	WHERE b.timestamp >= (
		SELECT start_ts
		FROM stock_start_date
	)
	%s
	GROUP BY b.symbol, b.timestamp, b.close, b.volume
)
GROUP BY underlying, join_underlying
ORDER BY stock_turnover_usd DESC, underlying ASC
LIMIT %d`, lookbackDays, stockUnderlyingOptionAliasExpr("underlying"), chquery.USStockAdjustedPriceSQL("b", "close", "sp"), chquery.USStockSplitJoinSQL("b", "sp"), stockUniverseFilter, candidateLimit)

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]usTurnoverStockCandidate, 0, candidateLimit)
	for rows.Next() {
		var candidate usTurnoverStockCandidate
		var stockTradingDays uint32
		if err := rows.Scan(
			&candidate.Underlying,
			&candidate.JoinUnderlying,
			&candidate.StockTurnoverUSD,
			&candidate.StockVolume,
			&stockTradingDays,
		); err != nil {
			return nil, fmt.Errorf("scan us turnover stock candidate: %w", err)
		}
		candidate.StockTradingDays = int(stockTradingDays)
		results = append(results, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate us turnover stock candidates: %w", err)
	}
	return results, nil
}

func (s *ScreenerService) loadUSTurnoverIntersectionOptionAggregates(ctx context.Context, lookbackDays int, joinUnderlyings []string) (map[string]usTurnoverOptionAggregate, error) {
	if len(joinUnderlyings) == 0 {
		return map[string]usTurnoverOptionAggregate{}, nil
	}
	query := fmt.Sprintf(`
WITH option_start_date AS (
	SELECT min(market_ts) AS start_ts
	FROM (
		SELECT DISTINCT timestamp AS market_ts
		FROM us_options_bar_1d
		ORDER BY market_ts DESC
		LIMIT %d
	)
)
SELECT
	underlying AS join_underlying,
	sum(toFloat64(close) * toFloat64(volume) * 100.0) AS option_turnover_usd,
	sum(toFloat64(volume)) AS option_volume,
	toUInt32(countDistinct(timestamp)) AS option_trading_days
FROM us_options_bar_1d
WHERE underlying IN ({underlyings:Array(String)})
	AND timestamp >= (
		SELECT start_ts
		FROM option_start_date
	)
GROUP BY join_underlying
ORDER BY option_turnover_usd DESC, join_underlying ASC`, lookbackDays)

	rows, err := s.repo.Query(ctx, query, clickhouse.Named("underlyings", joinUnderlyings))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[string]usTurnoverOptionAggregate, len(joinUnderlyings))
	for rows.Next() {
		var aggregate usTurnoverOptionAggregate
		var optionTradingDays uint32
		if err := rows.Scan(
			&aggregate.JoinUnderlying,
			&aggregate.OptionTurnoverUSD,
			&aggregate.OptionVolume,
			&optionTradingDays,
		); err != nil {
			return nil, fmt.Errorf("scan us turnover option aggregate: %w", err)
		}
		aggregate.OptionTradingDays = int(optionTradingDays)
		results[aggregate.JoinUnderlying] = aggregate
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate us turnover option aggregates: %w", err)
	}
	return results, nil
}

func (s *ScreenerService) ScreenUnderlyings(ctx context.Context, req dto.ScreenUnderlyingRequest) (*dto.ScreenUnderlyingResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	marketLiteral := clickHouseStringLiteral(req.Market)

	// Join latest volatility snapshot with latest aggregated liquidity data.
	// The volatility snapshot is per-underlying; liquidity is per-expiration, so we aggregate.
	query := fmt.Sprintf(chquery.ScreenUnderlyingsBase, marketLiteral, marketLiteral, marketLiteral, marketLiteral, marketLiteral)

	if req.IVPercentileMin != nil {
		query += fmt.Sprintf(` AND v.iv_percentile >= %.17g`, *req.IVPercentileMin)
	}
	if req.IVPercentileMax != nil {
		query += fmt.Sprintf(` AND v.iv_percentile <= %.17g`, *req.IVPercentileMax)
	}
	if req.IVRankMin != nil {
		query += fmt.Sprintf(` AND v.iv_rank >= %.17g`, *req.IVRankMin)
	}
	if req.IVRankMax != nil {
		query += fmt.Sprintf(` AND v.iv_rank <= %.17g`, *req.IVRankMax)
	}
	if req.HV20Min != nil {
		query += fmt.Sprintf(` AND v.hv20 >= %.17g`, *req.HV20Min)
	}
	if req.HV20Max != nil {
		query += fmt.Sprintf(` AND v.hv20 <= %.17g`, *req.HV20Max)
	}
	if req.VolumeMin != nil {
		query += fmt.Sprintf(` AND l.total_volume >= %.17g`, *req.VolumeMin)
	}
	if req.OpenInterestMin != nil {
		query += fmt.Sprintf(` AND l.total_oi >= %.17g`, *req.OpenInterestMin)
	}
	if req.ActivityRatioMin != nil {
		query += fmt.Sprintf(` AND l.avg_activity_ratio >= %.17g`, *req.ActivityRatioMin)
	}
	if req.TradabilityRatioMin != nil {
		query += fmt.Sprintf(` AND l.avg_tradability_ratio >= %.17g`, *req.TradabilityRatioMin)
	}

	if req.Cursor != "" {
		cursorUnderlying, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += ` AND v.underlying > ` + clickHouseStringLiteral(cursorUnderlying)
	}

	sortBy := "v.underlying"
	switch strings.ToLower(req.SortBy) {
	case "iv_percentile":
		sortBy = "v.iv_percentile DESC"
	case "iv_rank":
		sortBy = "v.iv_rank DESC"
	case "hv20":
		sortBy = "v.hv20 DESC"
	case "volume":
		sortBy = "l.total_volume DESC"
	case "open_interest":
		sortBy = "l.total_oi DESC"
	}
	query += fmt.Sprintf(` ORDER BY %s LIMIT %d`, sortBy, limit+1)

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("screen underlyings: %w", err)
	}
	defer rows.Close()

	results := make([]dto.ScreenedUnderlying, 0, limit)
	for rows.Next() {
		var r dto.ScreenedUnderlying
		var volume uint64
		if err := rows.Scan(
			&r.Market, &r.Underlying, &r.AsOfDate,
			&r.HV10, &r.HV20, &r.HV30,
			&r.CurrentIV, &r.IVPercentile, &r.IVRank,
			&r.OpenInterest, &volume,
			&r.ActivityRatio, &r.TradabilityRatio,
		); err != nil {
			return nil, fmt.Errorf("scan screened underlying: %w", err)
		}
		r.AsOfDate = dateAsUTC(r.AsOfDate)
		volumeInt := int(volume)
		r.Volume = &volumeInt
		sanitizeUnderlyingResult(&r)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screened underlyings: %w", err)
	}

	resp := &dto.ScreenUnderlyingResponse{Data: make([]dto.ScreenedUnderlying, 0)}
	if len(results) > limit {
		resp.NextCursor = encodeCursorString(results[limit-1].Underlying)
		resp.Data = results[:limit]
	} else {
		resp.Data = results
	}
	return resp, nil
}

func (s *ScreenerService) ScreenOptions(ctx context.Context, req dto.ScreenOptionRequest) (*dto.ScreenOptionResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, 500)

	// Determine chain table based on market
	var chainTable string
	switch req.Market {
	case "crypto-options":
		chainTable = "crypto_options_chain_1d"
	case "us-options":
		chainTable = "us_options_chain_1d"
	default:
		return nil, dto.NewValidationError("unsupported market %q for options screener", req.Market)
	}

	isCrypto := req.Market == "crypto-options"

	var query string
	if isCrypto {
		query = fmt.Sprintf(`
WITH latest AS (
	SELECT max(timestamp) AS ts FROM %s WHERE base_asset = %s
)
SELECT
    m.symbol,
    m.base_asset AS underlying,
    m.option_type,
    m.expiration,
    toInt32(dateDiff('day', now(), m.expiration)) AS days_to_expiry,
    m.strike_price AS strike,
    c.mark_close AS close,
    c.bid_close,
    c.ask_close,
    c.mark_iv AS implied_volatility,
    c.delta,
    c.gamma,
    c.vega,
    c.theta,
    c.open_interest,
	toFloat64(c.volume) AS volume,
    if(c.mark_close > 0, (c.ask_close - c.bid_close) / c.mark_close, 0) AS relative_spread,
    c.underlying_close
FROM %s AS chain
ARRAY JOIN
    chain.symbol_ids AS sid,
    chain.deltas AS delta_val,
    chain.gammas AS gamma_val,
    chain.vegas AS vega_val,
    chain.thetas AS theta_val,
    chain.rhos AS rho_val,
    chain.bid_closes AS bid_close_val,
    chain.ask_closes AS ask_close_val,
    chain.mark_closes AS mark_close_val,
    chain.mark_ivs AS mark_iv_val,
    chain.open_interests AS oi_val,
	chain.volumes AS volume_val
INNER JOIN crypto_options_symbol_meta FINAL AS m ON m.symbol_id = sid
CROSS JOIN latest
WHERE chain.timestamp = latest.ts
	AND chain.base_asset = %s
`, chainTable, clickHouseStringLiteral(req.Underlying), chainTable, clickHouseStringLiteral(req.Underlying))
	} else {
		query = fmt.Sprintf(`
WITH latest AS (
		SELECT max(timestamp) AS ts FROM %s WHERE underlying = %s
)
SELECT
	symbol_val AS symbol,
	chain.underlying,
	lower(toString(option_type_val)) AS option_type,
	expiration_val AS expiration,
	toInt32(dateDiff('day', now(), expiration_val)) AS days_to_expiry,
	strike_val AS strike,
	toFloat64(close_price_val) AS close,
    toFloat64(0) AS bid_close,
    toFloat64(0) AS ask_close,
	toFloat64(implied_volatility_val) AS implied_volatility,
	toFloat64(delta_val) AS delta,
	toFloat64(gamma_val) AS gamma,
	toFloat64(vega_val) AS vega,
	toFloat64(theta_val) AS theta,
    toFloat64(0) AS open_interest,
	toFloat64(volume_val) AS volume,
    toFloat64(0) AS relative_spread,
	toFloat64(underlying_close_val) AS underlying_close
FROM %s AS chain
ARRAY JOIN
	chain.symbols AS symbol_val,
	chain.option_types AS option_type_val,
	chain.expirations AS expiration_val,
	chain.strikes AS strike_val,
	chain.close_prices AS close_price_val,
	chain.underlying_closes AS underlying_close_val,
	chain.implied_volatilities AS implied_volatility_val,
	chain.deltas AS delta_val,
	chain.gammas AS gamma_val,
	chain.vegas AS vega_val,
	chain.thetas AS theta_val,
	chain.volumes AS volume_val
CROSS JOIN latest
WHERE chain.timestamp = latest.ts
	AND chain.underlying = %s
`, chainTable, clickHouseStringLiteral(req.Underlying), chainTable, clickHouseStringLiteral(req.Underlying))
	}

	// Apply filters
	deltaCol := "delta_val"
	ivCol := "mark_iv_val"
	premiumCol := "mark_close_val"
	volumeCol := "volume_val"
	oiCol := "oi_val"
	if !isCrypto {
		deltaCol = "delta_val"
		ivCol = "implied_volatility_val"
		premiumCol = "close_price_val"
		volumeCol = "volume_val"
		oiCol = "0"
	}

	if req.OptionType != "" {
		optType := strings.ToLower(req.OptionType)
		if isCrypto {
			query += ` AND m.option_type = ` + clickHouseStringLiteral(optType)
		} else {
			query += ` AND option_type_val = ` + clickHouseStringLiteral(strings.ToUpper(optType))
		}
	}
	if req.DTEMin != nil {
		query += fmt.Sprintf(` AND toInt32(dateDiff('day', now(), %s)) >= %d`, expirationCol(isCrypto), *req.DTEMin)
	}
	if req.DTEMax != nil {
		query += fmt.Sprintf(` AND toInt32(dateDiff('day', now(), %s)) <= %d`, expirationCol(isCrypto), *req.DTEMax)
	}
	if req.DeltaMin != nil {
		query += fmt.Sprintf(` AND %s >= %.17g`, deltaCol, *req.DeltaMin)
	}
	if req.DeltaMax != nil {
		query += fmt.Sprintf(` AND %s <= %.17g`, deltaCol, *req.DeltaMax)
	}
	if req.IVMin != nil {
		query += fmt.Sprintf(` AND %s >= %.17g`, ivCol, *req.IVMin)
	}
	if req.IVMax != nil {
		query += fmt.Sprintf(` AND %s <= %.17g`, ivCol, *req.IVMax)
	}
	if req.PremiumMin != nil {
		query += fmt.Sprintf(` AND %s >= %.17g`, premiumCol, *req.PremiumMin)
	}
	if req.PremiumMax != nil {
		query += fmt.Sprintf(` AND %s <= %.17g`, premiumCol, *req.PremiumMax)
	}
	if req.VolumeMin != nil {
		query += fmt.Sprintf(` AND %s >= %.17g`, volumeCol, *req.VolumeMin)
	}
	if req.OpenInterestMin != nil {
		query += fmt.Sprintf(` AND %s >= %.17g`, oiCol, *req.OpenInterestMin)
	}

	_ = premiumCol
	_ = volumeCol
	_ = oiCol

	sortBy := "days_to_expiry ASC, strike ASC"
	switch strings.ToLower(req.SortBy) {
	case "delta":
		sortBy = "delta DESC"
	case "iv", "implied_volatility":
		sortBy = "implied_volatility DESC"
	case "volume":
		sortBy = "volume DESC"
	case "dte":
		sortBy = "days_to_expiry ASC"
	case "premium":
		sortBy = "close DESC"
	}

	query += fmt.Sprintf(` ORDER BY %s LIMIT %d`, sortBy, limit+1)

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("screen options: %w", err)
	}
	defer rows.Close()

	results := make([]dto.ScreenedOption, 0, limit)
	for rows.Next() {
		var r dto.ScreenedOption
		var daysToExpiry int32
		if err := rows.Scan(
			&r.Symbol, &r.Underlying, &r.OptionType,
			&r.Expiration, &daysToExpiry,
			&r.Strike, &r.Close,
			&r.BidClose, &r.AskClose,
			&r.ImpliedVolatility,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta,
			&r.OpenInterest, &r.Volume,
			&r.RelativeSpread, &r.UnderlyingClose,
		); err != nil {
			return nil, fmt.Errorf("scan screened option: %w", err)
		}
		r.Expiration = dateAsUTC(r.Expiration)
		r.DaysToExpiry = int(daysToExpiry)
		sanitizeOptionResult(&r)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screened options: %w", err)
	}

	resp := &dto.ScreenOptionResponse{Data: make([]dto.ScreenedOption, 0)}
	if len(results) > limit {
		resp.Data = results[:limit]
		// Cursor-based pagination by symbol
		resp.NextCursor = encodeCursorString(results[limit-1].Symbol)
	} else {
		resp.Data = results
	}
	return resp, nil
}

func expirationCol(isCrypto bool) string {
	if isCrypto {
		return "m.expiration"
	}
	return "expiration_val"
}

func clickHouseStringLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

func sanitizeUnderlyingResult(r *dto.ScreenedUnderlying) {
	r.HV10 = sanitizeFloatPointer(r.HV10)
	r.HV20 = sanitizeFloatPointer(r.HV20)
	r.HV30 = sanitizeFloatPointer(r.HV30)
	r.CurrentIV = sanitizeFloatPointer(r.CurrentIV)
	r.IVPercentile = sanitizeFloatPointer(r.IVPercentile)
	r.IVRank = sanitizeFloatPointer(r.IVRank)
	r.OpenInterest = sanitizeFloatPointer(r.OpenInterest)
	r.ActivityRatio = sanitizeFloatPointer(r.ActivityRatio)
	r.TradabilityRatio = sanitizeFloatPointer(r.TradabilityRatio)
}

func sanitizeOptionResult(r *dto.ScreenedOption) {
	r.Strike = sanitizeFloatValue(r.Strike)
	r.Close = sanitizeFloatValue(r.Close)
	r.BidClose = sanitizeFloatValue(r.BidClose)
	r.AskClose = sanitizeFloatValue(r.AskClose)
	r.ImpliedVolatility = sanitizeFloatValue(r.ImpliedVolatility)
	r.Delta = sanitizeFloatValue(r.Delta)
	r.Gamma = sanitizeFloatValue(r.Gamma)
	r.Vega = sanitizeFloatValue(r.Vega)
	r.Theta = sanitizeFloatValue(r.Theta)
	r.OpenInterest = sanitizeFloatValue(r.OpenInterest)
	r.Volume = sanitizeFloatValue(r.Volume)
	r.RelativeSpread = sanitizeFloatPointer(r.RelativeSpread)
	r.UnderlyingClose = sanitizeFloatValue(r.UnderlyingClose)
}

func sanitizeFloatPointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := sanitizeFloatValue(*value)
	return &v
}

func sanitizeFloatValue(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func turnoverIntersectionCandidateLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	// 1.35 * N
	return (limit*27 + 19) / 20
}

func canonicalUSTurnoverIntersectionCacheLimit(limit int) int {
	if limit <= 0 {
		return limit
	}
	if limit <= usTurnoverIntersectionSharedTopLimit {
		return usTurnoverIntersectionSharedTopLimit
	}
	return limit
}

func usTurnoverIntersectionCacheKey(lookbackDays int, nonETFOnly bool) string {
	return fmt.Sprintf("screener:us-turnover-intersection:v6:lookback_days=%d:non_etf_only=%t", lookbackDays, nonETFOnly)
}

func cachedUSTurnoverIntersectionCoverageLimit(resp *dto.ScreenUSTurnoverIntersectionResponse) int {
	if resp == nil {
		return 0
	}
	storedLimit := resp.Limit
	if storedLimit <= 0 {
		storedLimit = len(resp.Data)
	}
	if len(resp.Data) == 0 {
		return 0
	}
	if len(resp.Data) < storedLimit {
		return maxSymbolLimit
	}
	return storedLimit
}

func cachedUSTurnoverIntersectionCanServe(resp *dto.ScreenUSTurnoverIntersectionResponse, requestedLimit int) bool {
	if requestedLimit <= 0 {
		return resp != nil && len(resp.Data) > 0
	}
	return cachedUSTurnoverIntersectionCoverageLimit(resp) >= requestedLimit
}

func usStocksFundamentalsUniverseFilterClause(symbolColumn string) string {
	return fmt.Sprintf(`
		AND %s IN (
			SELECT symbol
			FROM fundamental_observation
			WHERE market = 'us-stocks'
				AND factor_code IN ('pe', 'pb')
			GROUP BY symbol
			HAVING countDistinct(factor_code) = 2
		)`, symbolColumn)
}

func (s *ScreenerService) filterNonETFUSTurnoverResults(ctx context.Context, rows []dto.ScreenedUSTurnoverIntersectionRow) []dto.ScreenedUSTurnoverIntersectionRow {
	if s == nil || s.companyInfo == nil || len(rows) == 0 {
		return rows
	}
	symbols := make([]string, 0, len(rows))
	for _, row := range rows {
		symbols = append(symbols, row.Underlying)
	}
	excludedBySymbol, err := s.companyInfo.IsETFLikeBySymbol(ctx, symbols)
	if err != nil {
		return rows
	}
	filtered := make([]dto.ScreenedUSTurnoverIntersectionRow, 0, len(rows))
	for _, row := range rows {
		if excludedBySymbol[row.Underlying] {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func stockUnderlyingOptionAliasExpr(column string) string {
	return fmt.Sprintf("if(match(%s, '^[A-Z]+\\\\.[ABC]$'), replaceAll(%s, '.', ''), %s)", column, column, column)
}

func (s *ScreenerService) loadUSTurnoverIntersectionFromCache(ctx context.Context, key string, requestedLimit int) (*dto.ScreenUSTurnoverIntersectionResponse, bool, error) {
	if s == nil || s.cache == nil {
		return nil, false, nil
	}
	payload, ok, err := s.cache.Get(ctx, key)
	if err != nil || !ok {
		return nil, false, err
	}
	var resp dto.ScreenUSTurnoverIntersectionResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, false, err
	}
	if len(resp.Data) == 0 {
		return nil, false, nil
	}
	if !cachedUSTurnoverIntersectionCanServe(&resp, requestedLimit) {
		return nil, false, nil
	}
	if requestedLimit > 0 && len(resp.Data) > requestedLimit {
		resp.Data = resp.Data[:requestedLimit]
	}
	if requestedLimit > 0 {
		resp.Limit = requestedLimit
		resp.CandidateLimit = turnoverIntersectionCandidateLimit(requestedLimit)
	}
	return &resp, true, nil
}

func (s *ScreenerService) storeUSTurnoverIntersectionInCache(ctx context.Context, key string, resp *dto.ScreenUSTurnoverIntersectionResponse) error {
	if s == nil || s.cache == nil || resp == nil || len(resp.Data) == 0 {
		return nil
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return s.cache.Set(ctx, key, payload, usTurnoverIntersectionCacheTTL)
}
