package cryptooptions

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// FindMissingBarDays returns the list of UTC calendar days within [fromDate, toDate]
// that have no bar data in crypto_options_bar_1m.
func FindMissingBarDays(ctx context.Context, conn driver.Conn, fromDate, toDate time.Time, baseAsset string) ([]time.Time, error) {
	startDate := normalizeUTCDate(fromDate)
	endDate := normalizeUTCDate(toDate)
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end date %s is before start date %s", endDate.Format(dateLayout), startDate.Format(dateLayout))
	}

	endExclusive := endDate.AddDate(0, 0, 1)
	baseAssetFilter := ""
	if baseAsset != "" {
		baseAssetFilter = fmt.Sprintf("\n      AND base_asset = '%s'", escapeSingleQuote(baseAsset))
	}

	query := fmt.Sprintf(`WITH
    toDate('%s') AS start_date,
    toDate('%s') AS end_date
SELECT day
FROM
(
	SELECT addDays(start_date, number) AS day
	FROM numbers(dateDiff('day', start_date, end_date) + 1)
)
WHERE day NOT IN
(
	SELECT DISTINCT toDate(timestamp)
	FROM crypto_options_bar_1m
	WHERE timestamp >= toDateTime('%s')
	  AND timestamp < toDateTime('%s')%s
)
ORDER BY day`,
		startDate.Format(dateLayout),
		endDate.Format(dateLayout),
		startDate.Format(dateLayout),
		endExclusive.Format(dateLayout),
		baseAssetFilter,
	)

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query missing bar days: %w", err)
	}
	defer rows.Close()

	missingDays := make([]time.Time, 0)
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return nil, fmt.Errorf("scan missing bar day: %w", err)
		}
		missingDays = append(missingDays, normalizeUTCDate(day))
	}

	return missingDays, nil
}

func normalizeUTCDate(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
