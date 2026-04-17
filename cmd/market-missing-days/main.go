package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/cryptooptions"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

const (
	dateLayout           = "2006-01-02"
	marketCryptoOptions  = "crypto-options"
	marketUSStocks       = "us-stocks"
	marketUSOptions      = "us-options"
	defaultMissingMarket = marketCryptoOptions
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	from := flag.String("from", "", "Start date in YYYY-MM-DD")
	to := flag.String("to", "", "End date in YYYY-MM-DD")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	market := flag.String("market", defaultMissingMarket, "Dataset to scan: crypto-options | us-stocks | us-options")
	asset := flag.String("asset", "", "Optional asset filter, e.g. BTC or AAPL")
	baseAsset := flag.String("base-asset", "", "Deprecated alias for --asset")
	flag.Parse()

	if *from == "" || *to == "" {
		fmt.Fprintf(os.Stderr, "Usage: market-missing-days --from <YYYY-MM-DD> --to <YYYY-MM-DD> [--market crypto-options|us-stocks|us-options] [--clickhouse-dsn DSN] [--asset BTC|AAPL]\n")
		os.Exit(1)
	}

	selectedMarket, err := parseMarket(*market)
	if err != nil {
		log.Fatalf("%v", err)
	}

	fromDate, err := time.Parse(dateLayout, *from)
	if err != nil {
		log.Fatalf("invalid --from date %q: %v", *from, err)
	}
	toDate, err := time.Parse(dateLayout, *to)
	if err != nil {
		log.Fatalf("invalid --to date %q: %v", *to, err)
	}

	ctx := context.Background()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	assetFilter := normalizeAssetFilter(*asset, *baseAsset)
	missingDays, scopeLabel, err := findMissingDays(ctx, conn, selectedMarket, fromDate, toDate, assetFilter)
	if err != nil {
		log.Fatalf("find missing days: %v", err)
	}

	if len(missingDays) == 0 {
		fmt.Printf("No missing days found in [%s, %s] for %s.\n", fromDate.Format(dateLayout), toDate.Format(dateLayout), scopeLabel)
		return
	}

	fmt.Printf("Missing days in [%s, %s] for %s: %d\n", fromDate.Format(dateLayout), toDate.Format(dateLayout), scopeLabel, len(missingDays))
	for _, day := range missingDays {
		fmt.Println(day.Format(dateLayout))
	}
}

func parseMarket(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "crypto", marketCryptoOptions:
		return marketCryptoOptions, nil
	case "us-stock", "stock", "stocks", marketUSStocks:
		return marketUSStocks, nil
	case "us-option", "option", "options", marketUSOptions:
		return marketUSOptions, nil
	default:
		return "", fmt.Errorf("unsupported --market %q (expected crypto-options|us-stocks|us-options)", value)
	}
}

func normalizeAssetFilter(asset, baseAsset string) string {
	if trimmed := strings.TrimSpace(asset); trimmed != "" {
		return strings.ToUpper(trimmed)
	}
	return strings.ToUpper(strings.TrimSpace(baseAsset))
}

func findMissingDays(ctx context.Context, conn driver.Conn, market string, fromDate, toDate time.Time, assetFilter string) ([]time.Time, string, error) {
	scopeLabel := market
	if assetFilter == "" {
		scopeLabel += " (all assets)"
	} else {
		scopeLabel += " (" + assetFilter + ")"
	}

	switch market {
	case marketCryptoOptions:
		missingDays, err := cryptooptions.FindMissingBarDays(ctx, conn, fromDate, toDate, assetFilter)
		return missingDays, scopeLabel, err
	case marketUSStocks:
		missingDays, err := usmarket.FindMissingBarDays(ctx, conn, usmarket.MissingBarAssetStocks, fromDate, toDate, assetFilter)
		return missingDays, scopeLabel, err
	case marketUSOptions:
		missingDays, err := usmarket.FindMissingBarDays(ctx, conn, usmarket.MissingBarAssetOptions, fromDate, toDate, assetFilter)
		return missingDays, scopeLabel, err
	default:
		return nil, "", fmt.Errorf("unsupported market %q", market)
	}
}
