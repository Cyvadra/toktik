package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

func main() {
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
	from := flag.String("from", "", "Optional start date/time (YYYY-MM-DD or RFC3339), inclusive")
	to := flag.String("to", "", "Optional end date/time (YYYY-MM-DD or RFC3339), exclusive for RFC3339, next-day exclusive for YYYY-MM-DD")
	baseAsset := flag.String("base-asset", "", "Optional base asset filter, e.g. BTC")
	intervals := flag.String("intervals", strings.Join(cryptooptions.DefaultKlineWindows, ","), "Comma-separated intervals to generate")
	replace := flag.Bool("replace", false, "Replace existing rows in target aggregation scope before backfill")
	flag.Parse()

	fromTime, err := parseOptionalTime(*from, true)
	if err != nil {
		log.Fatalf("invalid --from: %v", err)
	}
	toTime, err := parseOptionalTime(*to, false)
	if err != nil {
		log.Fatalf("invalid --to: %v", err)
	}
	if !fromTime.IsZero() && !toTime.IsZero() && !toTime.After(fromTime) {
		log.Fatalf("invalid time range: --to must be after --from")
	}

	ivList := splitCSV(*intervals)
	if len(ivList) == 0 {
		log.Fatalf("--intervals cannot be empty")
	}

	ctx := context.Background()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, &appCli.SchemaInit{
		Kline:      true,
		SpotKline:  true,
		ChainCache: true,
	})
	if err != nil {
		log.Fatalf("%v", err)
	}

	opts := cryptooptions.KlineBackfillOptions{
		Intervals: ivList,
		From:      fromTime,
		To:        toTime,
		BaseAsset: strings.TrimSpace(*baseAsset),
		Replace:   *replace,
	}
	if err := cryptooptions.BackfillKlineWindows(ctx, conn, opts); err != nil {
		log.Fatalf("backfill kline windows: %v", err)
	}

	fmt.Fprintln(os.Stdout, "K-line backfill completed")
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, strings.ToLower(trimmed))
	}
	return out
}

func parseOptionalTime(v string, isFrom bool) (time.Time, error) {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return time.Time{}, nil
	}

	if t, err := time.Parse("2006-01-02", trimmed); err == nil {
		if isFrom {
			return t.UTC(), nil
		}
		return t.UTC().AddDate(0, 0, 1), nil
	}

	t, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
