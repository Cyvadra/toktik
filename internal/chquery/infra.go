package chquery

import "fmt"

// ----- Infra SQL -----

// RelationExists checks whether a table/view exists in the current database.
const RelationExists = `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {relation:String}`

// RelationRowCount returns the stored total row count from ClickHouse metadata.
const RelationRowCount = `SELECT toUInt64(ifNull(total_rows, 0))
FROM system.tables
WHERE database = currentDatabase()
  AND name = {relation:String}`

// RelationPartLastTimestamp returns the latest max_time from active parts for MergeTree-backed tables.
const RelationPartLastTimestamp = `SELECT ifNull(max(max_time), toDateTime(0, 'UTC'))
FROM system.parts
WHERE database = currentDatabase()
	AND table = {relation:String}
	AND active`

// RelationLastTimestamp returns the most recent timestamp in a relation.
// Arguments are (relation, timeField) to mirror "FROM relation" / timestamp field reading order.
func RelationLastTimestamp(relation, timeField string) string {
	return fmt.Sprintf(
		`SELECT %s AS last_ts FROM %s ORDER BY %s DESC LIMIT 1`,
		timeField,
		relation,
		timeField,
	)
}
