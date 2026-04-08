package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/pkg/polygon"
)

type polygonService interface {
	DownloadStockMinuteAggregates(date time.Time, force bool) (string, error)
	DownloadOptionMinuteAggregates(date time.Time, force bool) (string, error)
	StockSnapshot(symbol string) (*polygon.StockSnapshot, error)
	StockAggregates(req polygon.AggregateRequest) ([]polygon.AggregateBar, error)
	StockQuotes(symbol string, req polygon.QuoteRequest) ([]polygon.Quote, error)
	StockTrades(symbol string, req polygon.TradeRequest) ([]polygon.Trade, error)
	OptionContract(ticker string) (*polygon.OptionContract, error)
	OptionChain(req polygon.OptionChainRequest) ([]polygon.OptionChainContract, error)
	OptionAggregates(req polygon.AggregateRequest) ([]polygon.AggregateBar, error)
	OptionQuotes(ticker string, req polygon.QuoteRequest) ([]polygon.Quote, error)
	OptionTrades(ticker string, req polygon.TradeRequest) ([]polygon.Trade, error)
}

type app struct {
	stdout    io.Writer
	stderr    io.Writer
	newClient func() (polygonService, error)
}

func main() {
	cli := app{
		stdout: os.Stdout,
		stderr: os.Stderr,
		newClient: func() (polygonService, error) {
			return polygon.NewFromEnv()
		},
	}
	os.Exit(cli.run(os.Args[1:]))
}

func (a app) run(args []string) int {
	if len(args) > 0 && strings.TrimSpace(args[0]) == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		a.printUsage()
		return 1
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	if command == "help" || command == "-h" || command == "--help" {
		a.printUsage()
		return 0
	}

	client, err := a.newClient()
	if err != nil {
		return a.failf("init polygon client: %v", err)
	}

	switch command {
	case "stock-minute-flatfile":
		return a.runStockMinuteFlatFile(client, args[1:])
	case "stock-snapshot":
		return a.runStockSnapshot(client, args[1:])
	case "stock-aggregates":
		return a.runStockAggregates(client, args[1:])
	case "stock-quotes":
		return a.runStockQuotes(client, args[1:])
	case "stock-trades":
		return a.runStockTrades(client, args[1:])
	case "option-minute-flatfile":
		return a.runOptionMinuteFlatFile(client, args[1:])
	case "option-contract":
		return a.runOptionContract(client, args[1:])
	case "option-chain":
		return a.runOptionChain(client, args[1:])
	case "option-aggregates":
		return a.runOptionAggregates(client, args[1:])
	case "option-quotes":
		return a.runOptionQuotes(client, args[1:])
	case "option-trades":
		return a.runOptionTrades(client, args[1:])
	default:
		a.printUsage()
		return a.failf("unknown command %q", command)
	}
}

func (a app) runStockMinuteFlatFile(client polygonService, args []string) int {
	fs := flag.NewFlagSet("stock-minute-flatfile", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	date, force, err := parseFlatFileFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	path, runErr := client.DownloadStockMinuteAggregates(date, force)
	if runErr != nil {
		return a.failf("stock-minute-flatfile failed: %v", runErr)
	}
	return a.writeJSON(map[string]any{"path": path})
}

func (a app) runStockSnapshot(client polygonService, args []string) int {
	fs := flag.NewFlagSet("stock-snapshot", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Stock symbol, e.g. AAPL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("stock-snapshot: --symbol is required")
	}
	data, err := client.StockSnapshot(*symbol)
	if err != nil {
		return a.failf("stock-snapshot failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockAggregates(client polygonService, args []string) int {
	fs := flag.NewFlagSet("stock-aggregates", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	req, err := parseAggregateFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	data, runErr := client.StockAggregates(req)
	if runErr != nil {
		return a.failf("stock-aggregates failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func (a app) runStockQuotes(client polygonService, args []string) int {
	fs := flag.NewFlagSet("stock-quotes", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Stock symbol, e.g. AAPL")
	req, err := parseQuoteFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("stock-quotes: --symbol is required")
	}
	data, runErr := client.StockQuotes(*symbol, req)
	if runErr != nil {
		return a.failf("stock-quotes failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func (a app) runStockTrades(client polygonService, args []string) int {
	fs := flag.NewFlagSet("stock-trades", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Stock symbol, e.g. AAPL")
	req, err := parseTradeFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("stock-trades: --symbol is required")
	}
	data, runErr := client.StockTrades(*symbol, req)
	if runErr != nil {
		return a.failf("stock-trades failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func (a app) runOptionMinuteFlatFile(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-minute-flatfile", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	date, force, err := parseFlatFileFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	path, runErr := client.DownloadOptionMinuteAggregates(date, force)
	if runErr != nil {
		return a.failf("option-minute-flatfile failed: %v", runErr)
	}
	return a.writeJSON(map[string]any{"path": path})
}

func (a app) runOptionContract(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-contract", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	ticker := fs.String("ticker", "", "Option ticker, e.g. O:SPY251219C00650000")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*ticker) == "" {
		return a.failf("option-contract: --ticker is required")
	}
	data, err := client.OptionContract(*ticker)
	if err != nil {
		return a.failf("option-contract failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionChain(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-chain", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	underlying := fs.String("underlying", "", "Underlying symbol, e.g. SPY")
	expirationDate := fs.String("expiration-date", "", "Exact expiration date, e.g. 2025-12-19")
	expirationDateGte := fs.String("expiration-date-gte", "", "Minimum expiration date")
	expirationDateGt := fs.String("expiration-date-gt", "", "Expiration date greater than")
	expirationDateLte := fs.String("expiration-date-lte", "", "Maximum expiration date")
	expirationDateLt := fs.String("expiration-date-lt", "", "Expiration date less than")
	contractType := fs.String("contract-type", "", "Contract type: call or put")
	strikePrice := fs.String("strike-price", "", "Exact strike price")
	strikePriceGte := fs.String("strike-price-gte", "", "Minimum strike price")
	strikePriceGt := fs.String("strike-price-gt", "", "Strike price greater than")
	strikePriceLte := fs.String("strike-price-lte", "", "Maximum strike price")
	strikePriceLt := fs.String("strike-price-lt", "", "Strike price less than")
	order := fs.String("order", "", "Order direction: asc or desc")
	sort := fs.String("sort", "", "Sort field")
	limit := fs.Int("limit", 0, "Page size limit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*underlying) == "" {
		return a.failf("option-chain: --underlying is required")
	}

	req := polygon.OptionChainRequest{
		Underlying:        *underlying,
		ExpirationDate:    *expirationDate,
		ExpirationDateGte: *expirationDateGte,
		ExpirationDateGt:  *expirationDateGt,
		ExpirationDateLte: *expirationDateLte,
		ExpirationDateLt:  *expirationDateLt,
		ContractType:      *contractType,
		Order:             *order,
		Sort:              *sort,
		Limit:             *limit,
	}
	var parseErr *cliParseError
	req.StrikePrice, parseErr = optionalFloat64(*strikePrice, "option-chain: invalid --strike-price")
	if parseErr != nil {
		return parseErr.code(a)
	}
	req.StrikePriceGte, parseErr = optionalFloat64(*strikePriceGte, "option-chain: invalid --strike-price-gte")
	if parseErr != nil {
		return parseErr.code(a)
	}
	req.StrikePriceGt, parseErr = optionalFloat64(*strikePriceGt, "option-chain: invalid --strike-price-gt")
	if parseErr != nil {
		return parseErr.code(a)
	}
	req.StrikePriceLte, parseErr = optionalFloat64(*strikePriceLte, "option-chain: invalid --strike-price-lte")
	if parseErr != nil {
		return parseErr.code(a)
	}
	req.StrikePriceLt, parseErr = optionalFloat64(*strikePriceLt, "option-chain: invalid --strike-price-lt")
	if parseErr != nil {
		return parseErr.code(a)
	}

	data, err := client.OptionChain(req)
	if err != nil {
		return a.failf("option-chain failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionAggregates(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-aggregates", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	req, err := parseAggregateFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	data, runErr := client.OptionAggregates(req)
	if runErr != nil {
		return a.failf("option-aggregates failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func (a app) runOptionQuotes(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-quotes", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	ticker := fs.String("ticker", "", "Option ticker, e.g. O:SPY251219C00650000")
	req, err := parseQuoteFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	if strings.TrimSpace(*ticker) == "" {
		return a.failf("option-quotes: --ticker is required")
	}
	data, runErr := client.OptionQuotes(*ticker, req)
	if runErr != nil {
		return a.failf("option-quotes failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func (a app) runOptionTrades(client polygonService, args []string) int {
	fs := flag.NewFlagSet("option-trades", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	ticker := fs.String("ticker", "", "Option ticker, e.g. O:SPY251219C00650000")
	req, err := parseTradeFlags(fs, args)
	if err != nil {
		return err.code(a)
	}
	if strings.TrimSpace(*ticker) == "" {
		return a.failf("option-trades: --ticker is required")
	}
	data, runErr := client.OptionTrades(*ticker, req)
	if runErr != nil {
		return a.failf("option-trades failed: %v", runErr)
	}
	return a.writeJSON(data)
}

func parseAggregateFlags(fs *flag.FlagSet, args []string) (polygon.AggregateRequest, *cliParseError) {
	ticker := fs.String("ticker", "", "Ticker symbol")
	multiplier := fs.Int("multiplier", 1, "Aggregate multiplier")
	timespan := fs.String("timespan", "", "Timespan, e.g. minute, hour, day")
	from := fs.String("from", "", "Start date/time")
	to := fs.String("to", "", "End date/time")
	adjusted := fs.String("adjusted", "", "Optional adjusted flag: true or false")
	sort := fs.String("sort", "", "Sort order")
	limit := fs.Int("limit", 0, "Page size limit")
	if err := fs.Parse(args); err != nil {
		return polygon.AggregateRequest{}, &cliParseError{message: err.Error(), exitCode: 2}
	}
	if strings.TrimSpace(*ticker) == "" || strings.TrimSpace(*timespan) == "" || strings.TrimSpace(*from) == "" || strings.TrimSpace(*to) == "" {
		return polygon.AggregateRequest{}, &cliParseError{message: fs.Name() + ": --ticker, --timespan, --from, and --to are required"}
	}
	adjustedPtr, err := optionalBool(*adjusted, fs.Name()+": invalid --adjusted")
	if err != nil {
		return polygon.AggregateRequest{}, err
	}
	return polygon.AggregateRequest{
		Ticker:     *ticker,
		Multiplier: *multiplier,
		Timespan:   *timespan,
		From:       *from,
		To:         *to,
		Adjusted:   adjustedPtr,
		Sort:       *sort,
		Limit:      *limit,
	}, nil
}

func parseFlatFileFlags(fs *flag.FlagSet, args []string) (time.Time, bool, *cliParseError) {
	dateValue := fs.String("date", "", "Market date in YYYY-MM-DD")
	force := fs.Bool("force", false, "Force re-download even if the cache file already exists")
	if err := fs.Parse(args); err != nil {
		return time.Time{}, false, &cliParseError{message: err.Error(), exitCode: 2}
	}
	if strings.TrimSpace(*dateValue) == "" {
		return time.Time{}, false, &cliParseError{message: fs.Name() + ": --date is required"}
	}
	date, err := time.Parse("2006-01-02", strings.TrimSpace(*dateValue))
	if err != nil {
		return time.Time{}, false, &cliParseError{message: fmt.Sprintf("%s: invalid --date: %v", fs.Name(), err)}
	}
	return date, *force, nil
}

func parseQuoteFlags(fs *flag.FlagSet, args []string) (polygon.QuoteRequest, *cliParseError) {
	timestamp := fs.String("timestamp", "", "Exact timestamp")
	timestampGte := fs.String("timestamp-gte", "", "Timestamp greater than or equal")
	timestampGt := fs.String("timestamp-gt", "", "Timestamp greater than")
	timestampLte := fs.String("timestamp-lte", "", "Timestamp less than or equal")
	timestampLt := fs.String("timestamp-lt", "", "Timestamp less than")
	order := fs.String("order", "", "Order direction: asc or desc")
	sort := fs.String("sort", "", "Sort field")
	limit := fs.Int("limit", 0, "Page size limit")
	if err := fs.Parse(args); err != nil {
		return polygon.QuoteRequest{}, &cliParseError{message: err.Error(), exitCode: 2}
	}
	return polygon.QuoteRequest{
		Timestamp:    *timestamp,
		TimestampGte: *timestampGte,
		TimestampGt:  *timestampGt,
		TimestampLte: *timestampLte,
		TimestampLt:  *timestampLt,
		Order:        *order,
		Sort:         *sort,
		Limit:        *limit,
	}, nil
}

func parseTradeFlags(fs *flag.FlagSet, args []string) (polygon.TradeRequest, *cliParseError) {
	timestamp := fs.String("timestamp", "", "Exact timestamp")
	timestampGte := fs.String("timestamp-gte", "", "Timestamp greater than or equal")
	timestampGt := fs.String("timestamp-gt", "", "Timestamp greater than")
	timestampLte := fs.String("timestamp-lte", "", "Timestamp less than or equal")
	timestampLt := fs.String("timestamp-lt", "", "Timestamp less than")
	order := fs.String("order", "", "Order direction: asc or desc")
	sort := fs.String("sort", "", "Sort field")
	limit := fs.Int("limit", 0, "Page size limit")
	if err := fs.Parse(args); err != nil {
		return polygon.TradeRequest{}, &cliParseError{message: err.Error(), exitCode: 2}
	}
	return polygon.TradeRequest{
		Timestamp:    *timestamp,
		TimestampGte: *timestampGte,
		TimestampGt:  *timestampGt,
		TimestampLte: *timestampLte,
		TimestampLt:  *timestampLt,
		Order:        *order,
		Sort:         *sort,
		Limit:        *limit,
	}, nil
}

type cliParseError struct {
	message  string
	exitCode int
}

func (e *cliParseError) code(a app) int {
	if e == nil {
		return 0
	}
	if e.message != "" {
		_, _ = fmt.Fprintln(a.stderr, e.message)
	}
	if e.exitCode != 0 {
		return e.exitCode
	}
	return 1
}

func optionalBool(raw string, errPrefix string) (*bool, *cliParseError) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, &cliParseError{message: fmt.Sprintf("%s: %v", errPrefix, err)}
	}
	return &parsed, nil
}

func optionalFloat64(raw string, errPrefix string) (*float64, *cliParseError) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, &cliParseError{message: fmt.Sprintf("%s: %v", errPrefix, err)}
	}
	return &parsed, nil
}

func (a app) writeJSON(v any) int {
	encoder := json.NewEncoder(a.stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return a.failf("write response: %v", err)
	}
	return 0
}

func (a app) failf(format string, args ...any) int {
	_, _ = fmt.Fprintf(a.stderr, format+"\n", args...)
	return 1
}

func (a app) printUsage() {
	commands := []string{
		"option-minute-flatfile",
		"option-aggregates",
		"option-chain",
		"option-contract",
		"option-quotes",
		"option-trades",
		"stock-minute-flatfile",
		"stock-aggregates",
		"stock-quotes",
		"stock-snapshot",
		"stock-trades",
	}
	sort.Strings(commands)
	_, _ = fmt.Fprintln(a.stderr, "Usage: go run ./cmd/tools/polygon -- <command> [flags]")
	_, _ = fmt.Fprintln(a.stderr, "")
	_, _ = fmt.Fprintln(a.stderr, "Commands:")
	for _, command := range commands {
		_, _ = fmt.Fprintf(a.stderr, "  %s\n", command)
	}
	_, _ = fmt.Fprintln(a.stderr, "")
	_, _ = fmt.Fprintln(a.stderr, "Examples:")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- stock-snapshot --symbol AAPL")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- stock-minute-flatfile --date 2026-04-07")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- stock-aggregates --ticker AAPL --multiplier 1 --timespan minute --from 2025-11-03 --to 2025-11-28")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- option-chain --underlying SPY --expiration-date 2025-12-19 --contract-type call")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- option-minute-flatfile --date 2026-04-07")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/polygon -- option-trades --ticker O:SPY251219C00650000 --limit 10")
}
