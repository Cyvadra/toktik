package chquery

import "fmt"

const (
	MacroFactorCatalog = "macro_factor_catalog"
	MacroObservation   = "macro_observation"
)

func MacroFactorCatalogQuery() string {
	return fmt.Sprintf(`SELECT
	dataset,
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
	reference_market,
	reference_symbol,
	realtime_mode,
	active,
	sla_hours,
	metadata,
	updated_at
FROM %s FINAL
WHERE ({dataset:String} = '' OR dataset = {dataset:String})
  AND active = 1
ORDER BY dataset, factor_code`, MacroFactorCatalog)
}

func MacroSeriesEventQuery() string {
	return fmt.Sprintf(`SELECT
	factor_code,
	event_ts,
	known_at,
	value,
	source,
	reference_market,
	reference_symbol,
	anchor_value,
	revision
FROM %s
WHERE dataset = {dataset:String}
  AND known_at <= parseDateTimeBestEffort({as_of:String}, 'UTC')
  AND event_ts >= parseDateTimeBestEffort({from:String}, 'UTC')
  AND event_ts <  parseDateTimeBestEffort({to:String}, 'UTC')
  AND (length({factors:Array(String)}) = 0
	       OR has({factors:Array(String)}, factor_code))
ORDER BY factor_code ASC, event_ts ASC, known_at ASC, revision ASC`, MacroObservation)
}

func MacroLatestKnownAtQuery() string {
	return fmt.Sprintf(`SELECT
	dataset,
	factor_code,
	max(known_at) AS last_known_at
FROM %s
WHERE ({dataset:String} = '' OR dataset = {dataset:String})
  AND (length({factors:Array(String)}) = 0
	       OR has({factors:Array(String)}, factor_code))
GROUP BY dataset, factor_code
ORDER BY dataset, factor_code`, MacroObservation)
}
