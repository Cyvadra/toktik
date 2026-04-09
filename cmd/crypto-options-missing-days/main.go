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
	runtimeCfg := appCli.MustLoadRuntime()
	from := flag.String("from", "", "Start date in YYYY-MM-DD")
	to := flag.String("to", "", "End date in YYYY-MM-DD")
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	baseAsset := flag.String("base-asset", "", "Optional base asset filter, e.g. BTC or ETH")
	flag.Parse()

	if *from == "" || *to == "" {
		fmt.Fprintf(os.Stderr, "Usage: crypto-options-missing-days --from <YYYY-MM-DD> --to <YYYY-MM-DD> [--clickhouse-dsn DSN] [--base-asset BTC]\n")
		os.Exit(1)
	}

	fromDate, err := time.Parse(cryptooptionsDateLayout(), *from)
	if err != nil {
		log.Fatalf("invalid --from date %q: %v", *from, err)
	}
	toDate, err := time.Parse(cryptooptionsDateLayout(), *to)
	if err != nil {
		log.Fatalf("invalid --to date %q: %v", *to, err)
	}

	ctx := context.Background()
	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	missingDays, err := cryptooptions.FindMissingBarDays(ctx, conn, fromDate, toDate, strings.TrimSpace(*baseAsset))
	if err != nil {
		log.Fatalf("find missing days: %v", err)
	}

	filterLabel := "all assets"
	if strings.TrimSpace(*baseAsset) != "" {
		filterLabel = strings.ToUpper(strings.TrimSpace(*baseAsset))
	}

	if len(missingDays) == 0 {
		fmt.Printf("No missing days found in [%s, %s] for %s.\n", fromDate.Format(cryptooptionsDateLayout()), toDate.Format(cryptooptionsDateLayout()), filterLabel)
		return
	}

	fmt.Printf("Missing days in [%s, %s] for %s: %d\n", fromDate.Format(cryptooptionsDateLayout()), toDate.Format(cryptooptionsDateLayout()), filterLabel, len(missingDays))
	for _, day := range missingDays {
		fmt.Println(day.Format(cryptooptionsDateLayout()))
	}
}

func cryptooptionsDateLayout() string {
	return "2006-01-02"
}
