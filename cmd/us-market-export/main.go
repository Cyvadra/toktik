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

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/usexport"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: us-market-export --symbols AAPL,MSFT --start-date 2024-01-01 --end-date 2024-01-31 [flags]

Exports a compact offline bundle for investor delivery. The command does not expose an API endpoint and does not emit JSON. Output files are flat csv.gz: simple schema, good compression, and easy consumption from Python, R, Excel, DuckDB, ClickHouse, or pandas.

The export intentionally contains atomic tables only. Date-specific option selection, expiry/moneyness/volume filters, and other derived contract universes should be handled by the recipient from the exported rows.

Files written to --output-dir:
	manifest.txt              Export parameters, source tables, file list, row counts.
	stocks_bars.csv.gz        Stock OHLCV bars for requested symbols.
	option_contracts.csv.gz   Distinct option contracts observed in the requested date range.
	options_bars.csv.gz       Option OHLCV, underlying close, and Greek bars for requested underlyings.

Date filters are inclusive. --symbols is interpreted as stock symbols for stocks_bars and option underlyings for option exports.

Examples:
	go run ./cmd/us-market-export \
		--symbols AAPL,MSFT,SPY \
		--start-date 2024-01-01 \
		--end-date 2024-01-31 \
		--interval 1m \
		--output-dir exports/investor-jan-2024

	go run ./cmd/us-market-export \
		--symbols AAPL,MSFT \
		--start-date 2024-01-01 \
		--end-date 2024-01-31 \
		--include-stocks=false

Supported intervals: 1m,5m,15m,30m,1h,2h,4h,1d. Higher interval views are regular-session aggregates. Use --regular-session-only only with --interval=1m.

When stocks_bars includes VIX, the command uses the same US stock bars service path as /markets/us-stocks/bars?symbol=VIX, including synthetic VIX gap filling.

The command reads ClickHouse configuration from toktik.yaml or CLICKHOUSE_DSN. --clickhouse-dsn overrides it for one run.

Flags:
`)
		flag.PrintDefaults()
	}
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbolsFlag := flag.String("symbols", "", "Comma-separated US stock/option underlying symbols to export, e.g. AAPL,MSFT,SPY")
	startDateFlag := flag.String("start-date", "", "Inclusive market date start (YYYY-MM-DD)")
	endDateFlag := flag.String("end-date", "", "Inclusive market date end (YYYY-MM-DD); defaults to --start-date")
	intervalFlag := flag.String("interval", "1m", "Bar interval to export (1m,5m,15m,30m,1h,2h,4h,1d)")
	outputDir := flag.String("output-dir", "", "Output directory; defaults to exports/us-market-<symbols>-<start>-<end>-<interval>")
	regularOnly := flag.Bool("regular-session-only", false, "Export regular-session rows only. Only valid for 1m data because higher interval views are already regular-session aggregates")
	allowAmbiguousCryptoSymbols := flag.Bool("allow-ambiguous-crypto-symbols", false, "Allow symbols such as BTC or ETH to be treated as US-listed stock/option tickers instead of crypto assets")
	includeStocks := flag.Bool("include-stocks", true, "Export stock bars")
	includeContracts := flag.Bool("include-option-contracts", true, "Export distinct option contracts seen in the date range")
	includeOptions := flag.Bool("include-option-bars", true, "Export option bars")
	flag.Parse()

	symbols := usexport.NormalizeSymbols([]string{*symbolsFlag})
	if len(symbols) == 0 {
		fatalUsage("--symbols is required")
	}
	if strings.TrimSpace(*startDateFlag) == "" {
		fatalUsage("--start-date is required")
	}
	startDate := appCli.ParseDate(*startDateFlag, "--start-date")
	endDate := startDate
	if strings.TrimSpace(*endDateFlag) != "" {
		endDate = appCli.ParseDate(*endDateFlag, "--end-date")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := usmarket.ConnectClickHouse(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect ClickHouse: %v", err)
	}
	defer conn.Close()

	result, err := usexport.Run(ctx, conn, usexport.Config{Symbols: symbols, StartDate: startDate, EndDate: endDate, Interval: *intervalFlag, OutputDir: *outputDir, RegularSessionOnly: *regularOnly, AllowAmbiguousCryptoSymbols: *allowAmbiguousCryptoSymbols, IncludeStocks: *includeStocks, IncludeOptionContracts: *includeContracts, IncludeOptionBars: *includeOptions})
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range result.Files {
		log.Printf("exported %s rows=%d path=%s", file.Name, file.Rows, file.Path)
	}
	log.Printf("US market export complete: files=%d dir=%s", len(result.Files), result.OutputDir)
}

func fatalUsage(message string) {
	fmt.Fprintf(os.Stderr, "%s\n\n", message)
	fmt.Fprintln(os.Stderr, "Usage: us-market-export --symbols AAPL,MSFT --start-date 2024-01-01 --end-date 2024-01-31 [flags]")
	os.Exit(2)
}
