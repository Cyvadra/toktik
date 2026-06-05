package usmarket

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/config"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

func TestFMPStatementDerivedRatiosAgainstFMPCurrentRatios(t *testing.T) {
	if os.Getenv("TOKTIK_VALIDATE_FMP_RATIOS") == "" {
		t.Skip("set TOKTIK_VALIDATE_FMP_RATIOS=1 to compare local fmp_statement_derived_v2 PE/PB against FMP ratios-ttm")
	}
	runtimeCfg, err := config.LoadRuntime()
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	apiKey, err := runtimeCfg.FMPAPIKey()
	if err != nil {
		t.Fatalf("load FMP API key: %v", err)
	}
	ctx := context.Background()
	conn, err := ConnectClickHouse(ctx, runtimeCfg.ClickHouse.DSN)
	if err != nil {
		t.Fatalf("connect ClickHouse: %v", err)
	}
	defer conn.Close()

	symbols := []string{"AAPL", "MSFT", "NVDA", "GOOGL", "TSLA"}
	if raw := strings.TrimSpace(os.Getenv("TOKTIK_VALIDATE_FMP_SYMBOLS")); raw != "" {
		symbols = nil
		for _, part := range strings.Split(raw, ",") {
			if symbol := strings.ToUpper(strings.TrimSpace(part)); symbol != "" {
				symbols = append(symbols, symbol)
			}
		}
	}
	client := fmp.New(apiKey)
	for _, symbol := range symbols {
		localPE, localPB, ok, err := latestDerivedRatiosForValidation(ctx, conn, symbol)
		if err != nil {
			t.Fatalf("%s: query local derived ratios: %v", symbol, err)
		}
		if !ok {
			t.Logf("%s: no local fmp_statement_derived_v2 PE/PB rows; skipping", symbol)
			continue
		}
		ratios, err := client.RatiosTTM(ctx, symbol)
		if err != nil {
			t.Fatalf("%s: FMP ratios-ttm: %v", symbol, err)
		}
		assertRelativeRatioClose(t, symbol, "pe", localPE, ratios.PriceToEarningsRatioTtm, 0.02)
		assertRelativeRatioClose(t, symbol, "pb", localPB, ratios.PriceToBookRatioTtm, 0.02)
	}
}

func latestDerivedRatiosForValidation(ctx context.Context, conn driver.Conn, symbol string) (float64, float64, bool, error) {
	rows, err := conn.Query(ctx, `WITH latest AS (
	SELECT
		symbol,
		argMaxIf(value, (known_at, revision), factor_code = 'pe') AS pe_raw,
		argMaxIf(event_ts, (known_at, revision), factor_code = 'pe') AS pe_event,
		argMaxIf(value, (known_at, revision), factor_code = 'pb') AS pb_raw
	FROM fundamental_observation
	WHERE market = 'us-stocks'
	  AND symbol = {symbol:String}
	  AND source = 'fmp_statement_derived_v2'
	GROUP BY symbol
),
prices AS (
	SELECT symbol, argMax(close, timestamp) AS current_close
	FROM us_stocks_bar_1d
	WHERE symbol = {symbol:String}
	GROUP BY symbol
),
event_prices AS (
	SELECT l.symbol AS sym, argMaxIf(b.close, b.timestamp, b.timestamp <= l.pe_event) AS event_close
	FROM us_stocks_bar_1d AS b
	INNER JOIN latest AS l ON b.symbol = l.symbol
	GROUP BY l.symbol
)
SELECT
	l.pe_event,
	p.current_close / (e.event_close / l.pe_raw) AS pe_revalued,
	p.current_close / (e.event_close / l.pb_raw) AS pb_revalued
FROM latest AS l
INNER JOIN prices AS p ON p.symbol = l.symbol
INNER JOIN event_prices AS e ON e.sym = l.symbol
LIMIT 1`, clickhouse.Named("symbol", symbol))
	if err != nil {
		return 0, 0, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, 0, false, err
		}
		return 0, 0, false, nil
	}
	var eventTS time.Time
	var pe, pb float64
	if err := rows.Scan(&eventTS, &pe, &pb); err != nil {
		return 0, 0, false, err
	}
	if err := rows.Err(); err != nil {
		return 0, 0, false, err
	}
	return pe, pb, true, nil
}

func assertRelativeRatioClose(t *testing.T, symbol, factor string, got, want, tolerance float64) {
	t.Helper()
	if want == 0 || math.IsNaN(want) || math.IsInf(want, 0) {
		t.Logf("%s %s: FMP ratio unavailable; got local %.6f", symbol, factor, got)
		return
	}
	rel := math.Abs(got-want) / math.Abs(want)
	if rel > tolerance {
		t.Fatalf("%s %s: local %.6f FMP %.6f relative_error %.4f > %.4f", symbol, factor, got, want, rel, tolerance)
	}
	t.Logf("%s %s: local %.6f FMP %.6f relative_error %.4f", symbol, factor, got, want, rel)
}
