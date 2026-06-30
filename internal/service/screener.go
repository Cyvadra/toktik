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
	"github.com/Cyvadra/toktik/internal/usmarket"
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
	latest      LatestUSMarketCacheReader
}

const minLatestOptionChainContractsForUnderlyingOverlay = 100

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

type optionScreenSortSpec struct {
	Name        string
	PrimaryExpr string
	PrimaryDesc bool
	OrderSQL    string
}

type optionScreenCursor struct {
	Sort         string  `json:"sort"`
	PrimaryValue float64 `json:"primary_value,omitempty"`
	DaysToExpiry int     `json:"days_to_expiry"`
	Strike       float64 `json:"strike"`
	Symbol       string  `json:"symbol"`
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

func (s *ScreenerService) WithLatestMarketCache(reader LatestUSMarketCacheReader) *ScreenerService {
	if s == nil {
		return nil
	}
	s.latest = reader
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
		query += fmt.Sprintf(` AND ifNull(l.total_volume, 0) >= %.17g`, *req.VolumeMin)
	}
	if req.OpenInterestMin != nil {
		query += fmt.Sprintf(` AND ifNull(l.total_oi, 0) >= %.17g`, *req.OpenInterestMin)
	}
	if req.ActivityRatioMin != nil {
		query += fmt.Sprintf(` AND ifNull(l.avg_activity_ratio, 0) >= %.17g`, *req.ActivityRatioMin)
	}
	if req.TradabilityRatioMin != nil {
		query += fmt.Sprintf(` AND ifNull(l.avg_tradability_ratio, 0) >= %.17g`, *req.TradabilityRatioMin)
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
		sortBy = "ifNull(l.total_volume, 0) DESC"
	case "open_interest":
		sortBy = "ifNull(l.total_oi, 0) DESC"
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
	if req.IncludeLatest != nil && *req.IncludeLatest && req.Market == "us-options" && s.latest != nil {
		s.applyLatestUnderlyingOverlay(ctx, resp.Data)
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
	if req.RelativeSpreadMax != nil {
		if !isCrypto {
			return nil, dto.NewValidationError("relative_spread_max is not supported for us-options until bid/ask data is available")
		}
		query += fmt.Sprintf(` AND %s <= %.17g`, cryptoOptionRelativeSpreadExpr(), *req.RelativeSpreadMax)
	}
	if !isCrypto && req.DTEMin == nil && req.DTEMax == nil {
		query += ` AND toInt32(dateDiff('day', now(), expiration_val)) >= 0`
	}

	sortSpec := buildOptionScreenSortSpec(req.SortBy, isCrypto)
	if req.Cursor != "" {
		cursor, err := decodeOptionScreenCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursor.Sort != sortSpec.Name {
			return nil, dto.NewValidationError("cursor sort %q does not match requested sort %q", cursor.Sort, sortSpec.Name)
		}
		query += optionScreenCursorWhereSQL(sortSpec, optionScreenSymbolExpr(isCrypto), expirationCol(isCrypto), optionScreenStrikeExpr(isCrypto), cursor)
	}

	query += fmt.Sprintf(` ORDER BY %s LIMIT %d`, sortSpec.OrderSQL, limit+1)

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
	resp.Data, resp.NextCursor = paginateScreenedOptions(results, limit, sortSpec)
	if req.IncludeLatest != nil && *req.IncludeLatest && req.Market == "us-options" && s.latest != nil {
		resp.Data, resp.NextCursor = s.mergeLatestOptionScreenRows(ctx, req, resp.Data, resp.NextCursor, limit)
	}
	return resp, nil
}

func buildOptionScreenSortSpec(sortBy string, isCrypto bool) optionScreenSortSpec {
	symbolExpr := optionScreenSymbolExpr(isCrypto)
	dteExpr := fmt.Sprintf("toInt32(dateDiff('day', now(), %s))", expirationCol(isCrypto))
	strikeExpr := optionScreenStrikeExpr(isCrypto)
	baseOrder := fmt.Sprintf("%s ASC, %s ASC, %s ASC", dteExpr, strikeExpr, symbolExpr)
	spec := optionScreenSortSpec{Name: mapOptionScreenSortName(sortBy), OrderSQL: baseOrder}
	switch spec.Name {
	case "delta":
		spec.PrimaryExpr = optionScreenDeltaExpr(isCrypto)
		spec.PrimaryDesc = true
	case "iv":
		spec.PrimaryExpr = optionScreenIVExpr(isCrypto)
		spec.PrimaryDesc = true
	case "volume":
		spec.PrimaryExpr = optionScreenVolumeExpr(isCrypto)
		spec.PrimaryDesc = true
	case "premium":
		spec.PrimaryExpr = optionScreenPremiumExpr(isCrypto)
		spec.PrimaryDesc = true
	}
	if spec.PrimaryExpr != "" {
		spec.OrderSQL = fmt.Sprintf("%s DESC, %s", spec.PrimaryExpr, baseOrder)
	}
	return spec
}

func mapOptionScreenSortName(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "delta":
		return "delta"
	case "iv", "implied_volatility":
		return "iv"
	case "volume":
		return "volume"
	case "premium":
		return "premium"
	default:
		return "dte"
	}
}

func optionScreenSymbolExpr(isCrypto bool) string {
	if isCrypto {
		return "m.symbol"
	}
	return "symbol_val"
}

func optionScreenStrikeExpr(isCrypto bool) string {
	if isCrypto {
		return "m.strike_price"
	}
	return "strike_val"
}

func optionScreenDeltaExpr(bool) string { return "delta_val" }

func optionScreenIVExpr(isCrypto bool) string {
	if isCrypto {
		return "mark_iv_val"
	}
	return "implied_volatility_val"
}

func optionScreenVolumeExpr(bool) string { return "volume_val" }

func optionScreenPremiumExpr(isCrypto bool) string {
	if isCrypto {
		return "mark_close_val"
	}
	return "close_price_val"
}

func cryptoOptionRelativeSpreadExpr() string {
	return "if(mark_close_val > 0, (ask_close_val - bid_close_val) / mark_close_val, 0)"
}

func optionScreenCursorWhereSQL(spec optionScreenSortSpec, symbolExpr, expirationExpr, strikeExpr string, cursor optionScreenCursor) string {
	dteExpr := fmt.Sprintf("toInt32(dateDiff('day', now(), %s))", expirationExpr)
	tail := fmt.Sprintf(`(%s > %d OR (%s = %d AND (%s > %.17g OR (%s = %.17g AND %s > %s))))`,
		dteExpr, cursor.DaysToExpiry,
		dteExpr, cursor.DaysToExpiry,
		strikeExpr, cursor.Strike,
		strikeExpr, cursor.Strike,
		symbolExpr, clickHouseStringLiteral(cursor.Symbol),
	)
	if spec.PrimaryExpr == "" {
		return " AND " + tail
	}
	primaryCompare := ">"
	if spec.PrimaryDesc {
		primaryCompare = "<"
	}
	return fmt.Sprintf(` AND (%s %s %.17g OR (%s = %.17g AND %s))`, spec.PrimaryExpr, primaryCompare, cursor.PrimaryValue, spec.PrimaryExpr, cursor.PrimaryValue, tail)
}

func paginateScreenedOptions(rows []dto.ScreenedOption, limit int, spec optionScreenSortSpec) ([]dto.ScreenedOption, string) {
	if len(rows) > limit {
		return rows[:limit], encodeOptionScreenCursor(rows[limit-1], spec)
	}
	return rows, ""
}

func encodeOptionScreenCursor(row dto.ScreenedOption, spec optionScreenSortSpec) string {
	cursor := optionScreenCursor{
		Sort:         spec.Name,
		PrimaryValue: optionScreenPrimaryValue(row, spec.Name),
		DaysToExpiry: row.DaysToExpiry,
		Strike:       row.Strike,
		Symbol:       row.Symbol,
	}
	payload, _ := json.Marshal(cursor)
	return encodeCursorString(string(payload))
}

func decodeOptionScreenCursor(raw string) (optionScreenCursor, error) {
	payload, err := decodeCursorString(raw)
	if err != nil {
		return optionScreenCursor{}, err
	}
	var cursor optionScreenCursor
	if err := json.Unmarshal([]byte(payload), &cursor); err != nil {
		return optionScreenCursor{}, err
	}
	cursor.Symbol = strings.TrimSpace(cursor.Symbol)
	if cursor.Sort == "" || cursor.Symbol == "" {
		return optionScreenCursor{}, fmt.Errorf("missing cursor fields")
	}
	return cursor, nil
}

func optionScreenPrimaryValue(row dto.ScreenedOption, sortName string) float64 {
	switch sortName {
	case "delta":
		return row.Delta
	case "iv":
		return row.ImpliedVolatility
	case "volume":
		return row.Volume
	case "premium":
		return row.Close
	default:
		return 0
	}
}

func (s *ScreenerService) applyLatestUnderlyingOverlay(ctx context.Context, rows []dto.ScreenedUnderlying) {
	for i := range rows {
		latest, changed, err := s.latest.MergeOptionChain(ctx, rows[i].Underlying, time.Time{}, time.Time{}, time.Now().UTC().AddDate(0, 0, 1), nil)
		if err != nil || !changed || len(latest) == 0 {
			continue
		}
		snapshot := latest[len(latest)-1]
		if len(snapshot.Contracts) < minLatestOptionChainContractsForUnderlyingOverlay {
			continue
		}
		var totalVolume float64
		for _, contract := range snapshot.Contracts {
			totalVolume += contract.Volume
		}
		if totalVolume > 0 {
			volume := int(totalVolume)
			rows[i].Volume = &volume
		}
		rows[i].AsOfDate = snapshot.Timestamp.UTC()
	}
}

func (s *ScreenerService) mergeLatestOptionScreenRows(ctx context.Context, req dto.ScreenOptionRequest, rows []dto.ScreenedOption, nextCursor string, limit int) ([]dto.ScreenedOption, string) {
	underlying := req.Underlying
	if underlying == "" && len(rows) > 0 {
		underlying = rows[0].Underlying
	}
	latest, changed, err := s.latest.MergeOptionChain(ctx, underlying, time.Time{}, time.Time{}, time.Now().UTC().AddDate(0, 0, 1), nil)
	if err != nil || !changed || len(latest) == 0 {
		return rows, nextCursor
	}
	merged := make(map[string]dto.ScreenedOption, len(rows)+len(latest[len(latest)-1].Contracts))
	for _, row := range rows {
		merged[strings.ToUpper(strings.TrimSpace(row.Symbol))] = row
	}
	for _, contract := range latest[len(latest)-1].Contracts {
		row := screenedOptionFromLatestContract(underlying, contract)
		if row.Symbol == "" || !screenedOptionMatchesRequest(row, req) {
			continue
		}
		merged[strings.ToUpper(strings.TrimSpace(row.Symbol))] = row
	}
	out := make([]dto.ScreenedOption, 0, len(merged))
	for _, row := range merged {
		out = append(out, row)
	}
	sortScreenedOptions(out, req.SortBy)
	sortSpec := optionScreenSortSpec{Name: mapOptionScreenSortName(req.SortBy)}
	if req.Cursor != "" {
		cursor, err := decodeOptionScreenCursor(req.Cursor)
		if err == nil && cursor.Sort == sortSpec.Name {
			out = filterScreenedOptionsAfterCursor(out, cursor, sortSpec)
		}
	}
	return paginateScreenedOptions(out, limit, sortSpec)
}

func filterScreenedOptionsAfterCursor(rows []dto.ScreenedOption, cursor optionScreenCursor, spec optionScreenSortSpec) []dto.ScreenedOption {
	filtered := rows[:0]
	for _, row := range rows {
		if screenedOptionAfterCursor(row, cursor, spec.Name) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func screenedOptionAfterCursor(row dto.ScreenedOption, cursor optionScreenCursor, sortName string) bool {
	primary := optionScreenPrimaryValue(row, sortName)
	if sortName != "dte" && primary != cursor.PrimaryValue {
		return primary < cursor.PrimaryValue
	}
	if row.DaysToExpiry != cursor.DaysToExpiry {
		return row.DaysToExpiry > cursor.DaysToExpiry
	}
	if row.Strike != cursor.Strike {
		return row.Strike > cursor.Strike
	}
	return row.Symbol > cursor.Symbol
}

func screenedOptionFromLatestContract(underlying string, contract dto.USOptionChainContract) dto.ScreenedOption {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if underlying == "" {
		underlying = inferUnderlyingFromUSOptionTicker(contract.Symbol)
	}
	row := dto.ScreenedOption{
		Symbol:            strings.ToUpper(strings.TrimSpace(contract.Symbol)),
		Underlying:        underlying,
		OptionType:        strings.ToLower(strings.TrimSpace(contract.OptionType)),
		Expiration:        dateAsUTC(contract.Expiration),
		Strike:            sanitizeFloat64(contract.Strike),
		Close:             float64(contract.Close),
		ImpliedVolatility: float64(contract.ImpliedVolatility),
		Delta:             float64(contract.Delta),
		Gamma:             float64(contract.Gamma),
		Vega:              float64(contract.Vega),
		Theta:             float64(contract.Theta),
		Volume:            sanitizeFloat64(contract.Volume),
		UnderlyingClose:   float64(contract.UnderlyingClose),
	}
	row.DaysToExpiry = int(normalizeCalendarDate(row.Expiration).Sub(normalizeCalendarDate(time.Now().UTC())).Hours() / 24)
	sanitizeOptionResult(&row)
	return row
}

func inferUnderlyingFromUSOptionTicker(symbol string) string {
	underlying, _, _, _, err := usmarket.ParseOptionTicker(strings.ToUpper(strings.TrimSpace(symbol)))
	if err != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(underlying))
}

func screenedOptionMatchesRequest(row dto.ScreenedOption, req dto.ScreenOptionRequest) bool {
	if req.Underlying != "" && !strings.EqualFold(row.Underlying, req.Underlying) {
		return false
	}
	if req.OptionType != "" && !strings.EqualFold(row.OptionType, req.OptionType) {
		return false
	}
	if req.DTEMin == nil && req.DTEMax == nil && row.DaysToExpiry < 0 {
		return false
	}
	if req.DTEMin != nil && row.DaysToExpiry < *req.DTEMin {
		return false
	}
	if req.DTEMax != nil && row.DaysToExpiry > *req.DTEMax {
		return false
	}
	if req.DeltaMin != nil && row.Delta < *req.DeltaMin {
		return false
	}
	if req.DeltaMax != nil && row.Delta > *req.DeltaMax {
		return false
	}
	if req.IVMin != nil && row.ImpliedVolatility < *req.IVMin {
		return false
	}
	if req.IVMax != nil && row.ImpliedVolatility > *req.IVMax {
		return false
	}
	if req.PremiumMin != nil && row.Close < *req.PremiumMin {
		return false
	}
	if req.PremiumMax != nil && row.Close > *req.PremiumMax {
		return false
	}
	if req.VolumeMin != nil && row.Volume < *req.VolumeMin {
		return false
	}
	if req.OpenInterestMin != nil && row.OpenInterest < *req.OpenInterestMin {
		return false
	}
	if req.RelativeSpreadMax != nil {
		if row.RelativeSpread == nil || *row.RelativeSpread > *req.RelativeSpreadMax {
			return false
		}
	}
	return true
}

func sortScreenedOptions(rows []dto.ScreenedOption, sortBy string) {
	sort.SliceStable(rows, func(i, j int) bool {
		switch strings.ToLower(sortBy) {
		case "delta":
			if rows[i].Delta != rows[j].Delta {
				return rows[i].Delta > rows[j].Delta
			}
		case "iv", "implied_volatility":
			if rows[i].ImpliedVolatility != rows[j].ImpliedVolatility {
				return rows[i].ImpliedVolatility > rows[j].ImpliedVolatility
			}
		case "volume":
			if rows[i].Volume != rows[j].Volume {
				return rows[i].Volume > rows[j].Volume
			}
		case "premium":
			if rows[i].Close != rows[j].Close {
				return rows[i].Close > rows[j].Close
			}
		}
		if rows[i].DaysToExpiry != rows[j].DaysToExpiry {
			return rows[i].DaysToExpiry < rows[j].DaysToExpiry
		}
		if rows[i].Strike != rows[j].Strike {
			return rows[i].Strike < rows[j].Strike
		}
		return rows[i].Symbol < rows[j].Symbol
	})
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
