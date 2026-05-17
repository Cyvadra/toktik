package usmarket

import (
	"context"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

func ListUSOptionUnderlyingsMissingStockCoverage(ctx context.Context, conn driver.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, `SELECT o.underlying
FROM (SELECT underlying FROM us_options_bar_1m GROUP BY underlying) AS o
LEFT JOIN (SELECT symbol FROM us_stocks_bar_1m GROUP BY symbol) AS s
ON o.underlying = s.symbol
WHERE s.symbol = ''
ORDER BY o.underlying`)
	if err != nil {
		return nil, fmt.Errorf("query option underlyings missing stock coverage: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, err
		}
		if symbol = strings.TrimSpace(symbol); symbol != "" {
			out = append(out, symbol)
		}
	}
	return out, rows.Err()
}

func FormatSymbolPreview(symbols []string, limit int) string {
	if limit <= 0 || limit > len(symbols) {
		limit = len(symbols)
	}
	preview := strings.Join(symbols[:limit], ", \t")
	if len(symbols) > limit {
		preview += fmt.Sprintf(", ... (+%d more)", len(symbols)-limit)
	}
	return preview
}
