package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/pkg/feeds"
	"github.com/Cyvadra/toktik/pkg/feeds/dvol"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	currenciesArg := flag.String("currencies", strings.Join(dvol.DefaultCurrencies, ","), "Comma-separated currencies to sync")
	startDate := flag.String("start-date", "2020-01-01", "Start date (YYYY-MM-DD, UTC)")
	endDate := flag.String("end-date", time.Now().UTC().Format("2006-01-02"), "End date inclusive (YYYY-MM-DD, UTC)")
	baseURL := flag.String("base-url", runtimeCfg.Deribit.BaseURL, "Deribit API base URL")
	chDSN := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	flag.Parse()

	startDay, err := time.Parse("2006-01-02", *startDate)
	if err != nil {
		log.Fatalf("invalid start-date: %v", err)
	}
	endDay, err := time.Parse("2006-01-02", *endDate)
	if err != nil {
		log.Fatalf("invalid end-date: %v", err)
	}
	if endDay.Before(startDay) {
		log.Fatalf("end-date must be >= start-date")
	}

	rangeStart := startDay.UTC()
	rangeEnd := endDay.UTC().Add(24 * time.Hour)

	currencies := parseCurrencies(*currenciesArg)
	if len(currencies) == 0 {
		log.Fatalf("no currencies specified")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received %v, shutting down...", sig)
		cancel()
	}()

	// Open feed store and ensure all window tables exist.
	store, err := feeds.NewStore(ctx, *chDSN)
	if err != nil {
		log.Fatalf("connect clickhouse: %v", err)
	}
	defer store.Close()

	feed := dvol.NewFeedWithClient(*baseURL)
	if err := store.EnsureAllTables(ctx, feed.Name()); err != nil {
		log.Fatalf("ensure tables: %v", err)
	}

	log.Printf("Deribit DVOL sync (feeds platform)")
	log.Printf("  base URL:    %s", *baseURL)
	log.Printf("  range:       %s to %s", rangeStart.Format(time.RFC3339), rangeEnd.Format(time.RFC3339))
	log.Printf("  currencies:  %v", currencies)

	totalInserted := 0
	for _, currency := range currencies {
		if err := ctx.Err(); err != nil {
			log.Printf("context canceled, stopping")
			break
		}

		n, err := store.SyncFeed(ctx, feed, currency, rangeStart, rangeEnd)
		if err != nil {
			log.Printf("[%s] sync error: %v", currency, err)
			continue
		}
		totalInserted += n
		log.Printf("[%s] synced %d total rows across all windows", currency, n)
	}

	log.Printf("sync finished: total_inserted=%d", totalInserted)
	if totalInserted == 0 {
		fmt.Fprintln(os.Stderr, "warning: no data synced")
	}
}

func parseCurrencies(input string) []string {
	parts := strings.Split(input, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		c := strings.ToUpper(strings.TrimSpace(p))
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}
