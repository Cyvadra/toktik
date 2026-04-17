package usmarket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	MissingBarAssetStocks  = "stocks"
	MissingBarAssetOptions = "options"
	missingDayDateLayout   = "2006-01-02"
)

type missingBarAssetMetadata struct {
	table        string
	filterColumn string
}

// FindMissingBarDays returns trading days within [fromDate, toDate] that have
// no 1-minute bar data for the requested US asset class.
func FindMissingBarDays(ctx context.Context, conn driver.Conn, assetClass string, fromDate, toDate time.Time, asset string) ([]time.Time, error) {
	startDate := normalizeMarketDate(fromDate)
	endDate := normalizeMarketDate(toDate)
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end date %s is before start date %s", endDate.Format(missingDayDateLayout), startDate.Format(missingDayDateLayout))
	}

	assetMeta, err := missingBarAssetInfo(assetClass)
	if err != nil {
		return nil, err
	}

	expectedDays := tradingDaysInRange(startDate, endDate)
	if len(expectedDays) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`SELECT DISTINCT market_date
FROM %s
WHERE market_date >= @startDate
  AND market_date <= @endDate`, assetMeta.table)
	args := []interface{}{
		clickhouse.Named("startDate", startDate.Format(missingDayDateLayout)),
		clickhouse.Named("endDate", endDate.Format(missingDayDateLayout)),
	}
	if asset != "" {
		query += fmt.Sprintf("\n  AND %s = @asset", assetMeta.filterColumn)
		args = append(args, clickhouse.Named("asset", strings.ToUpper(strings.TrimSpace(asset))))
	}
	query += "\nORDER BY market_date"

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query missing bar days: %w", err)
	}
	defer rows.Close()

	existing := make(map[string]struct{}, len(expectedDays))
	for rows.Next() {
		var marketDate time.Time
		if err := rows.Scan(&marketDate); err != nil {
			return nil, fmt.Errorf("scan missing bar day: %w", err)
		}
		existing[normalizeMarketDate(marketDate).Format(missingDayDateLayout)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing bar days: %w", err)
	}

	missingDays := make([]time.Time, 0)
	for _, day := range expectedDays {
		if _, ok := existing[day.Format(missingDayDateLayout)]; ok {
			continue
		}
		missingDays = append(missingDays, day)
	}

	return missingDays, nil
}

func missingBarAssetInfo(assetClass string) (missingBarAssetMetadata, error) {
	switch strings.ToLower(strings.TrimSpace(assetClass)) {
	case MissingBarAssetStocks:
		return missingBarAssetMetadata{table: "us_stocks_bar_1m", filterColumn: "symbol"}, nil
	case MissingBarAssetOptions:
		return missingBarAssetMetadata{table: "us_options_bar_1m", filterColumn: "underlying"}, nil
	default:
		return missingBarAssetMetadata{}, fmt.Errorf("unsupported asset class %q (expected stocks|options)", assetClass)
	}
}

func tradingDaysInRange(fromDate, toDate time.Time) []time.Time {
	sessions := GenerateSessionCalendar(fromDate.Year(), toDate.Year())
	days := make([]time.Time, 0, len(sessions))
	for _, session := range sessions {
		if session == nil || session.IsHoliday {
			continue
		}
		marketDate := normalizeMarketDate(session.MarketDate)
		if marketDate.Before(fromDate) || marketDate.After(toDate) {
			continue
		}
		days = append(days, marketDate)
	}
	return days
}

func normalizeMarketDate(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
