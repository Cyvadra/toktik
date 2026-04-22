package chquery

import "fmt"

// ----- Feature Store SQL -----

// VolatilitySnapshotQuery returns a query for the latest precomputed volatility snapshot.
func VolatilitySnapshotQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
LIMIT 1`, table)
}

// VolatilityHistoryQuery returns a query for precomputed volatility history.
func VolatilityHistoryQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
  AND as_of_date >= toDate({from:String})
  AND as_of_date < toDate({to:String})
ORDER BY as_of_date ASC`, table)
}

// TermStructureSnapshotQuery returns a query for precomputed term structure data.
func TermStructureSnapshotQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY expiration ASC`, table)
}

// TermStructureHistoryQuery returns a query for term structure history.
func TermStructureHistoryQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY as_of_date ASC, expiration ASC`, table)
}

// SkewSnapshotQuery returns a query for precomputed skew data.
func SkewSnapshotQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY expiration ASC`, table)
}

// SkewHistoryQuery returns a query for skew history.
func SkewHistoryQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY as_of_date ASC, expiration ASC`, table)
}

// LiquiditySnapshotQuery returns a query for precomputed liquidity data.
func LiquiditySnapshotQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY expiration ASC`, table)
}

// LiquidityHistoryQuery returns a query for precomputed liquidity history.
func LiquidityHistoryQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
ORDER BY as_of_date ASC, expiration ASC`, table)
}

// DailyPanelQuery returns a query for precomputed daily feature panels.
func DailyPanelQuery(table string) string {
	return fmt.Sprintf(`SELECT
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
  AND as_of_date >= toDate({from:String})
  AND as_of_date < toDate({to:String})
ORDER BY as_of_date ASC`, table)
}

// LatestPrecomputedSurfaceDate returns a query for the latest as_of_date in a feature table.
func LatestPrecomputedSurfaceDate(table string) string {
	return fmt.Sprintf(`SELECT ifNull(maxOrNull(as_of_date), toDate('1970-01-01'))
FROM %s
WHERE market = {market:String}
  AND underlying = {underlying:String}`, table)
}

// PrecomputedRowCountBase returns a count query for precomputed feature rows.
// Dynamic WHERE clauses for date bounds can be appended by the caller.
func PrecomputedRowCountBase(table string) string {
	return fmt.Sprintf(`SELECT count() FROM %s WHERE market = {market:String} AND underlying = {underlying:String}`, table)
}

// DeletePrecomputedBase returns a DELETE query for precomputed feature rows.
// Dynamic WHERE clauses for date bounds can be appended by the caller.
func DeletePrecomputedBase(table string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE market = {market:String} AND underlying = {underlying:String}`, table)
}

// ----- Feature Insert SQL -----

// InsertVolatilitySnapshot is the INSERT statement for volatility snapshots.
func InsertVolatilitySnapshot(table string) string {
	return fmt.Sprintf(`INSERT INTO %s (
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
)`, table)
}

// InsertTermStructure is the INSERT statement for term structure data.
func InsertTermStructure(table string) string {
	return fmt.Sprintf(`INSERT INTO %s (
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
)`, table)
}

// InsertSkew is the INSERT statement for skew data.
func InsertSkew(table string) string {
	return fmt.Sprintf(`INSERT INTO %s (
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
)`, table)
}

// InsertLiquidity is the INSERT statement for liquidity data.
func InsertLiquidity(table string) string {
	return fmt.Sprintf(`INSERT INTO %s (
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
)`, table)
}

// InsertDailyPanel is the INSERT statement for daily feature panels.
func InsertDailyPanel(table string) string {
	return fmt.Sprintf(`INSERT INTO %s (
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
)`, table)
}

// ----- US Options Surface Aggregation SQL -----

// USOptionsSurfaceAggregatesBase is the base query for computing US options surface aggregates.
const USOptionsSurfaceAggregatesBase = `SELECT
	toDate(timestamp) AS as_of_date,
	expiration,
	dateDiff('day', toDate(timestamp), expiration) AS dte_days,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND strike >= underlying_close * 0.98 AND strike <= underlying_close * 1.02), 0) AS atm_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'C'), 0) AS call_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'P'), 0) AS put_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'C' AND strike > underlying_close * 1.02 AND strike <= underlying_close * 1.10), 0) AS otm_call_iv,
	nullIf(avgIf(toFloat64(implied_volatility), isFinite(implied_volatility) AND implied_volatility > 0 AND option_type = 'P' AND strike < underlying_close * 0.98 AND strike >= underlying_close * 0.90), 0) AS otm_put_iv,
	toUInt32(countIf(isFinite(implied_volatility) AND implied_volatility > 0)) AS contract_count
FROM us_options_bar_1d
WHERE underlying = {underlying:String}
  AND expiration >= toDate(timestamp)`

// USOptionsLiquidityAggregatesBase is the base query for US options liquidity aggregation.
const USOptionsLiquidityAggregatesBase = `SELECT
	toDate(timestamp) AS as_of_date,
	expiration,
	dateDiff('day', toDate(timestamp), expiration) AS dte_days,
	nullIf(avgIf(toFloat64(close), close > 0), 0) AS avg_mark_close,
	toUInt64(sum(volume)) AS total_volume,
	toUInt64(sum(transactions)) AS total_transactions,
	toUInt32(count()) AS contract_count,
	toUInt32(countIf(volume > 0 OR transactions > 0)) AS active_contract_count
FROM us_options_bar_1d
WHERE underlying = {underlying:String}
  AND expiration >= toDate(timestamp)`

// CryptoOptionsLiquidityAggregatesBase is the base query for crypto options liquidity aggregation.
const CryptoOptionsLiquidityAggregatesBase = `SELECT
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

// CryptoOptionsLiquidityAggregatesMiddle is appended after time filters.
const CryptoOptionsLiquidityAggregatesMiddle = `
		GROUP BY as_of_date, symbol_id
	) AS last
	INNER JOIN crypto_options_symbol_meta AS meta ON meta.symbol_id = last.symbol_id
	WHERE toDate(meta.expiration) >= last.as_of_date`

// FeatureAggregatesGroupBy is appended to aggregate queries.
const FeatureAggregatesGroupBy = `
GROUP BY as_of_date, expiration
ORDER BY as_of_date ASC, expiration ASC`

// ----- Screener SQL -----

// ScreenUnderlyingsBase is the base query for underlying screening.
const ScreenUnderlyingsBase = `
SELECT
    v.market,
    v.underlying,
    v.as_of_date,
    v.hv10,
    v.hv20,
    v.hv30,
    v.current_iv,
    v.iv_percentile,
    v.iv_rank,
    l.total_oi,
    l.total_volume,
    l.avg_activity_ratio,
    l.avg_tradability_ratio
FROM (
    SELECT market, underlying, as_of_date, hv10, hv20, hv30, current_iv, iv_percentile, iv_rank
    FROM feature_volatility_snapshot_daily
	WHERE market = %s
      AND as_of_date = (
		  SELECT max(as_of_date) FROM feature_volatility_snapshot_daily WHERE market = %s
      )
    GROUP BY market, underlying, as_of_date, hv10, hv20, hv30, current_iv, iv_percentile, iv_rank
) v
LEFT JOIN (
    SELECT market, underlying,
        sum(open_interest) AS total_oi,
        sum(volume) AS total_volume,
        avg(activity_ratio) AS avg_activity_ratio,
		avg(if(%s = 'us-options' AND avg_bid_close IS NULL AND avg_ask_close IS NULL AND tradable_contract_count = 0, CAST(NULL, 'Nullable(Float64)'), tradability_ratio)) AS avg_tradability_ratio
    FROM feature_liquidity_snapshot_daily
		WHERE market = %s
      AND as_of_date = (
					SELECT max(as_of_date) FROM feature_liquidity_snapshot_daily WHERE market = %s
      )
    GROUP BY market, underlying
) l ON v.market = l.market AND v.underlying = l.underlying
WHERE 1 = 1`

// ScreenOptionsCryptoBase returns the base query for crypto options screening.
func ScreenOptionsCryptoBase(chainTable string) string {
	return fmt.Sprintf(`
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
}

// ScreenOptionsUSBase returns the base query for US options screening.
func ScreenOptionsUSBase(chainTable string) string {
	return fmt.Sprintf(`
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
