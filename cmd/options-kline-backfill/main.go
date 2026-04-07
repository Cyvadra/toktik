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
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	dsn := flag.String("clickhouse-dsn", appCli.DefaultDSN, "ClickHouse DSN")
	market := flag.String("market", "crypto", "Market: crypto | us")
	from := flag.String("from", "", "Optional start date/time (YYYY-MM-DD or RFC3339), inclusive")
	to := flag.String("to", "", "Optional end date/time (YYYY-MM-DD or RFC3339), exclusive for RFC3339, next-day exclusive for YYYY-MM-DD")
	baseAsset := flag.String("base-asset", "", "Optional asset filter, e.g. BTC (crypto base_asset / US underlying+symbol)")
	intervals := flag.String("intervals", "", "Comma-separated intervals to generate (defaults depend on market)")
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

	ctx := context.Background()
	switch strings.ToLower(strings.TrimSpace(*market)) {
	case "crypto":
		if len(ivList) == 0 {
			ivList = append([]string(nil), cryptooptions.DefaultKlineWindows...)
		}
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
			log.Fatalf("backfill crypto kline windows: %v", err)
		}
	case "us":
		conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
		if err != nil {
			log.Fatalf("connect ClickHouse: %v", err)
		}
		if err := usmarket.InitOptionKlineSchema(ctx, conn); err != nil {
			log.Fatalf("init us option kline schema: %v", err)
		}
		if err := usmarket.InitStockKlineSchema(ctx, conn); err != nil {
			log.Fatalf("init us stock kline schema: %v", err)
		}
		if err := usmarket.InitOptionChainCacheSchema(ctx, conn); err != nil {
			log.Fatalf("init us chain cache schema: %v", err)
		}
		if len(ivList) == 0 {
			ivList = append([]string(nil), usmarket.DefaultBackfillWindows...)
		}

		opts := usmarket.KlineBackfillOptions{
			Intervals: ivList,
			From:      fromTime,
			To:        toTime,
			Asset:     strings.TrimSpace(*baseAsset),
			Replace:   *replace,
		}
		if err := usmarket.BackfillKlineWindows(ctx, conn, opts); err != nil {
			log.Fatalf("backfill us kline windows: %v", err)
		}
	default:
		log.Fatalf("unsupported --market %q (expected crypto|us)", *market)
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
