package service

import (
	"context"
	"fmt"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

// ScreenerService provides condition-based screening for underlyings and options.
type ScreenerService struct {
	repo *chrepo.Repo
}

func NewScreenerService(repo *chrepo.Repo) *ScreenerService {
	return &ScreenerService{repo: repo}
}

func (s *ScreenerService) ScreenUnderlyings(ctx context.Context, req dto.ScreenUnderlyingRequest) (*dto.ScreenUnderlyingResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)

	// Join latest volatility snapshot with latest aggregated liquidity data.
	// The volatility snapshot is per-underlying; liquidity is per-expiration, so we aggregate.
	query := chquery.ScreenUnderlyingsBase

	args := []interface{}{
		clickhouse.Named("market", req.Market),
	}

	if req.IVPercentileMin != nil {
		query += ` AND v.iv_percentile >= {iv_pctl_min:Float64}`
		args = append(args, clickhouse.Named("iv_pctl_min", *req.IVPercentileMin))
	}
	if req.IVPercentileMax != nil {
		query += ` AND v.iv_percentile <= {iv_pctl_max:Float64}`
		args = append(args, clickhouse.Named("iv_pctl_max", *req.IVPercentileMax))
	}
	if req.IVRankMin != nil {
		query += ` AND v.iv_rank >= {iv_rank_min:Float64}`
		args = append(args, clickhouse.Named("iv_rank_min", *req.IVRankMin))
	}
	if req.IVRankMax != nil {
		query += ` AND v.iv_rank <= {iv_rank_max:Float64}`
		args = append(args, clickhouse.Named("iv_rank_max", *req.IVRankMax))
	}
	if req.HV20Min != nil {
		query += ` AND v.hv20 >= {hv20_min:Float64}`
		args = append(args, clickhouse.Named("hv20_min", *req.HV20Min))
	}
	if req.HV20Max != nil {
		query += ` AND v.hv20 <= {hv20_max:Float64}`
		args = append(args, clickhouse.Named("hv20_max", *req.HV20Max))
	}
	if req.VolumeMin != nil {
		query += ` AND l.total_volume >= {vol_min:Float64}`
		args = append(args, clickhouse.Named("vol_min", *req.VolumeMin))
	}
	if req.OpenInterestMin != nil {
		query += ` AND l.total_oi >= {oi_min:Float64}`
		args = append(args, clickhouse.Named("oi_min", *req.OpenInterestMin))
	}
	if req.ActivityRatioMin != nil {
		query += ` AND l.avg_activity_ratio >= {act_min:Float64}`
		args = append(args, clickhouse.Named("act_min", *req.ActivityRatioMin))
	}
	if req.TradabilityRatioMin != nil {
		query += ` AND l.avg_tradability_ratio >= {trd_min:Float64}`
		args = append(args, clickhouse.Named("trd_min", *req.TradabilityRatioMin))
	}

	if req.Cursor != "" {
		cursorUnderlying, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += ` AND v.underlying > {cursor:String}`
		args = append(args, clickhouse.Named("cursor", cursorUnderlying))
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
	query += fmt.Sprintf(` ORDER BY %s LIMIT {limit:UInt32}`, sortBy)
	args = append(args, clickhouse.Named("limit", limit+1))

	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("screen underlyings: %w", err)
	}
	defer rows.Close()

	results := make([]dto.ScreenedUnderlying, 0, limit)
	for rows.Next() {
		var r dto.ScreenedUnderlying
		if err := rows.Scan(
			&r.Market, &r.Underlying, &r.AsOfDate,
			&r.HV10, &r.HV20, &r.HV30,
			&r.CurrentIV, &r.IVPercentile, &r.IVRank,
			&r.OpenInterest, &r.Volume,
			&r.ActivityRatio, &r.TradabilityRatio,
		); err != nil {
			return nil, fmt.Errorf("scan screened underlying: %w", err)
		}
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
    SELECT max(timestamp) AS ts FROM %s WHERE base_asset = {underlying:String}
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
  AND chain.base_asset = {underlying:String}
`, chainTable, chainTable)
	} else {
		query = fmt.Sprintf(`
WITH latest AS (
    SELECT max(timestamp) AS ts FROM %s WHERE underlying = {underlying:String}
)
SELECT
    chain.symbol,
    chain.underlying,
    chain.option_type,
    chain.expiration,
    toInt32(dateDiff('day', now(), chain.expiration)) AS days_to_expiry,
    chain.strike,
    chain.close,
    toFloat64(0) AS bid_close,
    toFloat64(0) AS ask_close,
    chain.implied_volatility,
    chain.delta,
    chain.gamma,
    chain.vega,
    chain.theta,
    toFloat64(0) AS open_interest,
	toFloat64(chain.volume) AS volume,
    toFloat64(0) AS relative_spread,
    chain.underlying_close
FROM %s AS chain
CROSS JOIN latest
WHERE chain.timestamp = latest.ts
  AND chain.underlying = {underlying:String}
`, chainTable, chainTable)
	}

	args := []interface{}{
		clickhouse.Named("underlying", req.Underlying),
	}

	// Apply filters
	deltaCol := "delta_val"
	ivCol := "mark_iv_val"
	premiumCol := "mark_close_val"
	volumeCol := "volume_val"
	oiCol := "oi_val"
	if !isCrypto {
		deltaCol = "chain.delta"
		ivCol = "chain.implied_volatility"
		premiumCol = "chain.close"
		volumeCol = "chain.volume"
		oiCol = "0"
	}

	if req.OptionType != "" {
		optType := strings.ToLower(req.OptionType)
		if isCrypto {
			query += ` AND m.option_type = {opt_type:String}`
		} else {
			query += ` AND chain.option_type = {opt_type:String}`
		}
		args = append(args, clickhouse.Named("opt_type", optType))
	}
	if req.DTEMin != nil {
		query += ` AND toInt32(dateDiff('day', now(), ` + expirationCol(isCrypto) + `)) >= {dte_min:Int32}`
		args = append(args, clickhouse.Named("dte_min", int32(*req.DTEMin)))
	}
	if req.DTEMax != nil {
		query += ` AND toInt32(dateDiff('day', now(), ` + expirationCol(isCrypto) + `)) <= {dte_max:Int32}`
		args = append(args, clickhouse.Named("dte_max", int32(*req.DTEMax)))
	}
	if req.DeltaMin != nil {
		query += fmt.Sprintf(` AND %s >= {delta_min:Float64}`, deltaCol)
		args = append(args, clickhouse.Named("delta_min", *req.DeltaMin))
	}
	if req.DeltaMax != nil {
		query += fmt.Sprintf(` AND %s <= {delta_max:Float64}`, deltaCol)
		args = append(args, clickhouse.Named("delta_max", *req.DeltaMax))
	}
	if req.IVMin != nil {
		query += fmt.Sprintf(` AND %s >= {iv_min:Float64}`, ivCol)
		args = append(args, clickhouse.Named("iv_min", *req.IVMin))
	}
	if req.IVMax != nil {
		query += fmt.Sprintf(` AND %s <= {iv_max:Float64}`, ivCol)
		args = append(args, clickhouse.Named("iv_max", *req.IVMax))
	}
	if req.PremiumMin != nil {
		query += fmt.Sprintf(` AND %s >= {prem_min:Float64}`, premiumCol)
		args = append(args, clickhouse.Named("prem_min", *req.PremiumMin))
	}
	if req.PremiumMax != nil {
		query += fmt.Sprintf(` AND %s <= {prem_max:Float64}`, premiumCol)
		args = append(args, clickhouse.Named("prem_max", *req.PremiumMax))
	}
	if req.VolumeMin != nil {
		query += fmt.Sprintf(` AND %s >= {vol_min:Float64}`, volumeCol)
		args = append(args, clickhouse.Named("vol_min", *req.VolumeMin))
	}
	if req.OpenInterestMin != nil {
		query += fmt.Sprintf(` AND %s >= {oi_min:Float64}`, oiCol)
		args = append(args, clickhouse.Named("oi_min", *req.OpenInterestMin))
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

	query += fmt.Sprintf(` ORDER BY %s LIMIT {limit:UInt32}`, sortBy)
	args = append(args, clickhouse.Named("limit", limit+1))

	rows, err := s.repo.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("screen options: %w", err)
	}
	defer rows.Close()

	results := make([]dto.ScreenedOption, 0, limit)
	for rows.Next() {
		var r dto.ScreenedOption
		if err := rows.Scan(
			&r.Symbol, &r.Underlying, &r.OptionType,
			&r.Expiration, &r.DaysToExpiry,
			&r.Strike, &r.Close,
			&r.BidClose, &r.AskClose,
			&r.ImpliedVolatility,
			&r.Delta, &r.Gamma, &r.Vega, &r.Theta,
			&r.OpenInterest, &r.Volume,
			&r.RelativeSpread, &r.UnderlyingClose,
		); err != nil {
			return nil, fmt.Errorf("scan screened option: %w", err)
		}
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
	return "chain.expiration"
}
