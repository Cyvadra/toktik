package chquery

import "fmt"

// RelationColumns returns schema metadata for one table or view in the current database.
const RelationColumns = `SELECT
    name,
    type,
    position,
    default_kind,
    default_expression,
    comment,
		'' AS codec_expression
FROM system.columns
WHERE database = currentDatabase()
  AND table = {relation:String}
ORDER BY position`

func RelationPreview(relation string, columns []string, whereSQL, orderSQL string, limit int) string {
	return fmt.Sprintf(
		`SELECT %s FROM (SELECT %s FROM %s%s%s LIMIT %d) AS preview_rows`,
		joinPreviewExpressions(columns),
		joinIdentifiers(columns),
		relation,
		whereSQL,
		orderSQL,
		limit,
	)
}

func RelationCoverageSummary(relation, timeField, whereSQL string) string {
	return fmt.Sprintf(`SELECT
    toUInt64(count()) AS row_count,
    ifNull(toDateTime(minOrNull(%[2]s), 'UTC'), toDateTime(0, 'UTC')) AS first_ts,
    ifNull(toDateTime(maxOrNull(%[2]s), 'UTC'), toDateTime(0, 'UTC')) AS last_ts
FROM %[1]s%[3]s`, relation, timeField, whereSQL)
}

func RelationDailyCoverage(relation, timeField, whereSQL string) string {
	return fmt.Sprintf(`SELECT
    toDate(%[2]s) AS date,
    toUInt64(count()) AS row_count
FROM %[1]s%[3]s
GROUP BY date
ORDER BY date`, relation, timeField, whereSQL)
}

func RelationFieldProfile(relation, field, fieldType, whereSQL string) string {
	zeroExpr := "toUInt64(0)"
	emptyExpr := "toUInt64(0)"
	if isNumericClickHouseType(fieldType) {
		zeroExpr = fmt.Sprintf("toUInt64(countIf(%s = 0))", field)
	}
	if isStringClickHouseType(fieldType) {
		emptyExpr = fmt.Sprintf("toUInt64(countIf(%s = ''))", field)
	}
	return fmt.Sprintf(`SELECT
    toUInt64(count()) AS row_count,
    toUInt64(countIf(isNull(%[2]s))) AS null_count,
    %[4]s AS zero_count,
    %[5]s AS empty_count,
    toUInt64(uniqCombined64(%[2]s)) AS distinct_count,
	CAST(minOrNull(%[2]s), 'Nullable(String)') AS min_value,
	CAST(maxOrNull(%[2]s), 'Nullable(String)') AS max_value
FROM %[1]s%[3]s`, relation, field, whereSQL, zeroExpr, emptyExpr)
}

func RelationValidCount(relation, validExpr, whereSQL string) string {
	return fmt.Sprintf(`SELECT
    toUInt64(count()) AS row_count,
    toUInt64(countIf(%[2]s)) AS valid_count
FROM %[1]s%[3]s`, relation, validExpr, whereSQL)
}

func RelationFieldValues(relation, field, timeField, searchSQL string, limit int) string {
	lastTimestampExpr := "toDateTime(0, 'UTC') AS last_ts"
	if timeField != "" {
		lastTimestampExpr = fmt.Sprintf("ifNull(toDateTime(maxOrNull(%s), 'UTC'), toDateTime(0, 'UTC')) AS last_ts", timeField)
	}
	return fmt.Sprintf(`SELECT
    CAST(%[2]s, 'String') AS value,
    toUInt64(count()) AS row_count,
    %[4]s
FROM %[1]s
WHERE CAST(%[2]s, 'String') != ''%[3]s
GROUP BY value
ORDER BY value ASC
LIMIT %[5]d`, relation, field, searchSQL, lastTimestampExpr, limit)
}

func joinIdentifiers(columns []string) string {
	if len(columns) == 0 {
		return "*"
	}
	joined := columns[0]
	for _, column := range columns[1:] {
		joined += ", " + column
	}
	return joined
}

func joinPreviewExpressions(columns []string) string {
	if len(columns) == 0 {
		return "*"
	}
	joined := previewExpression(columns[0])
	for _, column := range columns[1:] {
		joined += ", " + previewExpression(column)
	}
	return joined
}

func previewExpression(column string) string {
	return fmt.Sprintf("CAST(%[1]s, 'Nullable(String)') AS %[1]s", column)
}

func isNumericClickHouseType(t string) bool {
	switch {
	case len(t) >= 4 && t[:4] == "UInt":
		return true
	case len(t) >= 3 && t[:3] == "Int":
		return true
	case len(t) >= 5 && t[:5] == "Float":
		return true
	case len(t) >= 7 && t[:7] == "Decimal":
		return true
	case len(t) > 9 && t[:9] == "Nullable(":
		return isNumericClickHouseType(t[9 : len(t)-1])
	default:
		return false
	}
}

func isStringClickHouseType(t string) bool {
	if t == "String" || t == "LowCardinality(String)" {
		return true
	}
	if len(t) > 9 && t[:9] == "Nullable(" {
		return isStringClickHouseType(t[9 : len(t)-1])
	}
	return false
}
