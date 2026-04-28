package chquery

import "fmt"

// ----- Fundamentals table names -----

const (
	FundamentalFactorCatalog = "fundamental_factor_catalog"
	FundamentalObservation   = "fundamental_observation"
)

// FundamentalFactorCatalogQuery returns active factor definitions, optionally filtered by market.
// When market is empty, all markets are returned.
func FundamentalFactorCatalogQuery() string {
	return fmt.Sprintf(`SELECT
	market,
	factor_code,
	display_name,
	description,
	value_type,
	unit,
	preferred_frequency,
	fill_policy,
	fill_max_days,
	point_in_time,
	source,
	active,
	sla_hours,
	metadata,
	updated_at
FROM %s FINAL
WHERE ({market:String} = '' OR market = {market:String})
  AND active = 1
ORDER BY market, factor_code`, FundamentalFactorCatalog)
}

// FundamentalSeriesEventQuery returns the raw sparse event series for one
// (market, symbol, factor) restricted by [from, to) on event_ts and respecting
// the point-in-time `known_at <= as_of` rule.
func FundamentalSeriesEventQuery() string {
	return fmt.Sprintf(`SELECT
	event_ts,
	known_at,
	value,
	source,
	revision
FROM %s
WHERE market = {market:String}
  AND symbol = {symbol:String}
  AND factor_code = {factor:String}
  AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')
  AND event_ts >= parseDateTimeBestEffort({from:String}, 'UTC')
  AND event_ts <  parseDateTimeBestEffort({to:String}, 'UTC')
ORDER BY event_ts ASC, known_at ASC, revision ASC`, FundamentalObservation)
}

// FundamentalSeriesAsOfQuery returns the latest known value per event_ts for
// one (market, symbol, factor). Use the result on the client side to forward-
// fill according to the catalog fill policy.
func FundamentalSeriesAsOfQuery() string {
	return fmt.Sprintf(`SELECT
	event_ts,
	argMax(known_at, (known_at, revision)) AS known_at,
	argMax(value,    (known_at, revision)) AS value,
	argMax(source,   (known_at, revision)) AS source
FROM %s
WHERE market = {market:String}
  AND symbol = {symbol:String}
  AND factor_code = {factor:String}
  AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')
  AND event_ts >= parseDateTimeBestEffort({from:String}, 'UTC')
  AND event_ts <  parseDateTimeBestEffort({to:String}, 'UTC')
GROUP BY event_ts
ORDER BY event_ts ASC`, FundamentalObservation)
}

// FundamentalSnapshotQuery returns the latest known value (per factor) for a
// single symbol at the given as-of time. When factors is empty, all are returned.
func FundamentalSnapshotQuery() string {
	return fmt.Sprintf(`SELECT
	factor_code,
	argMax(event_ts, (known_at, revision))  AS event_ts,
	argMax(known_at, (known_at, revision))  AS known_at,
	argMax(value,    (known_at, revision))  AS value,
	argMax(source,   (known_at, revision))  AS source
FROM %s
WHERE market = {market:String}
  AND symbol = {symbol:String}
  AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')
  AND (length({factors:Array(String)}) = 0
       OR has({factors:Array(String)}, factor_code))
GROUP BY factor_code
ORDER BY factor_code`, FundamentalObservation)
}

// FundamentalPanelQuery returns the latest known value per (symbol, factor) at
// the given as-of time for many symbols. When factors is empty, all are returned.
func FundamentalPanelQuery() string {
	return fmt.Sprintf(`SELECT
	symbol,
	factor_code,
	argMax(event_ts, (known_at, revision))  AS event_ts,
	argMax(known_at, (known_at, revision))  AS known_at,
	argMax(value,    (known_at, revision))  AS value
FROM %s
WHERE market = {market:String}
  AND has({symbols:Array(String)}, symbol)
  AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')
  AND (length({factors:Array(String)}) = 0
       OR has({factors:Array(String)}, factor_code))
GROUP BY symbol, factor_code
ORDER BY symbol, factor_code`, FundamentalObservation)
}

// FundamentalLatestKnownAtQuery returns the maximum known_at per (market,
// factor) for freshness reporting. Both filters are optional (empty = all).
func FundamentalLatestKnownAtQuery() string {
	return fmt.Sprintf(`SELECT
	market,
	factor_code,
	max(known_at) AS last_known_at
FROM %s
WHERE ({market:String} = '' OR market = {market:String})
  AND ({factor:String} = '' OR factor_code = {factor:String})
GROUP BY market, factor_code
ORDER BY market, factor_code`, FundamentalObservation)
}
