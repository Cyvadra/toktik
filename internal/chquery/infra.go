package chquery

import "fmt"

// ----- Infra SQL -----

// RelationExists checks whether a table/view exists in the current database.
const RelationExists = `SELECT count()
FROM system.tables
WHERE database = currentDatabase()
  AND name = {relation:String}`

// RelationLastTimestamp returns the most recent timestamp in a relation.
// Requires fmt.Sprintf with (timeField, tableName).
func RelationLastTimestamp(timeField, relation string) string {
	return fmt.Sprintf(
		`SELECT ifNull(toDateTime(maxOrNull(%s), 'UTC'), toDateTime(0, 'UTC')) AS last_ts FROM %s`,
		timeField, relation,
	)
}

// RelationRowCount returns the total row count of a relation.
// Requires fmt.Sprintf with tableName.
func RelationRowCount(relation string) string {
	return fmt.Sprintf(`SELECT toUInt64(count()) FROM %s`, relation)
}
