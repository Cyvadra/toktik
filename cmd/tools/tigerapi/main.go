package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/pkg/tigerapi"
)

type tigerService interface {
	MarketState(market string) ([]tigerapi.MarketState, error)
	StockQuotes(symbols []string) ([]tigerapi.StockQuote, error)
	StockKlines(req tigerapi.StockKlineRequest) ([]tigerapi.KlineBar, error)
	StockTimeline(symbols []string) ([]tigerapi.TimelinePoint, error)
	StockTradeTicks(symbols []string) ([]tigerapi.TradeTick, error)
	StockDepth(symbol string) (*tigerapi.QuoteDepth, error)
	OptionExpirations(underlying string) ([]string, error)
	OptionChain(underlying string, expiry string) ([]tigerapi.OptionContract, error)
	OptionQuotes(identifiers []string) ([]tigerapi.OptionQuote, error)
	OptionKlines(req tigerapi.OptionKlineRequest) ([]tigerapi.KlineBar, error)
	ExecuteRawResponse(method string, bizParams any) (string, error)
	ExecuteRawResponseVersioned(method string, bizParams any, version string) (string, error)
}

type app struct {
	stdout    io.Writer
	stderr    io.Writer
	newClient func() (tigerService, error)
}

func main() {
	cli := app{
		stdout: os.Stdout,
		stderr: os.Stderr,
		newClient: func() (tigerService, error) {
			return tigerapi.NewFromRuntime(appCli.MustLoadRuntime())
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
		return a.failf("init tigerapi client: %v", err)
	}

	switch command {
	case "market-state":
		return a.runMarketState(client, args[1:])
	case "stock-quote":
		return a.runStockQuote(client, args[1:])
	case "stock-kline":
		return a.runStockKline(client, args[1:])
	case "stock-timeline":
		return a.runStockTimeline(client, args[1:])
	case "stock-trade-tick":
		return a.runStockTradeTick(client, args[1:])
	case "stock-depth":
		return a.runStockDepth(client, args[1:])
	case "option-expirations":
		return a.runOptionExpirations(client, args[1:])
	case "option-chain":
		return a.runOptionChain(client, args[1:])
	case "option-quote":
		return a.runOptionQuote(client, args[1:])
	case "option-kline":
		return a.runOptionKline(client, args[1:])
	case "raw":
		return a.runRaw(client, args[1:])
	default:
		a.printUsage()
		return a.failf("unknown command %q", command)
	}
}

func (a app) runMarketState(client tigerService, args []string) int {
	fs := flag.NewFlagSet("market-state", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	market := fs.String("market", "US", "Market code, e.g. US")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	data, err := client.MarketState(*market)
	if err != nil {
		return a.failf("market-state failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockQuote(client tigerService, args []string) int {
	fs := flag.NewFlagSet("stock-quote", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbolsArg := fs.String("symbols", "", "Comma-separated stock symbols, e.g. AAPL,MSFT")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	symbols, err := parseCSVList(*symbolsArg)
	if err != nil {
		return a.failf("stock-quote: %v", err)
	}
	data, err := client.StockQuotes(symbols)
	if err != nil {
		return a.failf("stock-quote failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockKline(client tigerService, args []string) int {
	fs := flag.NewFlagSet("stock-kline", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Stock symbol, e.g. AAPL")
	period := fs.String("period", "day", "Kline period, e.g. day, week, month")
	withFundamental := fs.Bool("with-fundamental", false, "Include bar-level fundamentals such as PE ratio and turnover rate when Tiger provides them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("stock-kline: --symbol is required")
	}
	data, err := client.StockKlines(tigerapi.StockKlineRequest{Symbol: *symbol, Period: *period, WithFundamental: *withFundamental})
	if err != nil {
		return a.failf("stock-kline failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockTimeline(client tigerService, args []string) int {
	fs := flag.NewFlagSet("stock-timeline", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbolsArg := fs.String("symbols", "", "Comma-separated stock symbols, e.g. AAPL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	symbols, err := parseCSVList(*symbolsArg)
	if err != nil {
		return a.failf("stock-timeline: %v", err)
	}
	data, err := client.StockTimeline(symbols)
	if err != nil {
		return a.failf("stock-timeline failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockTradeTick(client tigerService, args []string) int {
	fs := flag.NewFlagSet("stock-trade-tick", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbolsArg := fs.String("symbols", "", "Comma-separated stock symbols, e.g. AAPL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	symbols, err := parseCSVList(*symbolsArg)
	if err != nil {
		return a.failf("stock-trade-tick: %v", err)
	}
	data, err := client.StockTradeTicks(symbols)
	if err != nil {
		return a.failf("stock-trade-tick failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runStockDepth(client tigerService, args []string) int {
	fs := flag.NewFlagSet("stock-depth", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Stock symbol, e.g. AAPL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("stock-depth: --symbol is required")
	}
	data, err := client.StockDepth(*symbol)
	if err != nil {
		return a.failf("stock-depth failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionExpirations(client tigerService, args []string) int {
	fs := flag.NewFlagSet("option-expirations", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Underlying stock symbol, e.g. AAPL")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*symbol) == "" {
		return a.failf("option-expirations: --symbol is required")
	}
	data, err := client.OptionExpirations(*symbol)
	if err != nil {
		return a.failf("option-expirations failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionChain(client tigerService, args []string) int {
	fs := flag.NewFlagSet("option-chain", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	symbol := fs.String("symbol", "", "Underlying stock symbol, e.g. AAPL")
	expiry := fs.String("expiry", "", "Expiry date, e.g. 2026-04-17")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*symbol) == "" || strings.TrimSpace(*expiry) == "" {
		return a.failf("option-chain: --symbol and --expiry are required")
	}
	data, err := client.OptionChain(*symbol, *expiry)
	if err != nil {
		return a.failf("option-chain failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionQuote(client tigerService, args []string) int {
	fs := flag.NewFlagSet("option-quote", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	identifiersArg := fs.String("identifiers", "", "Comma-separated option identifiers")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	identifiers, err := parseCSVList(*identifiersArg)
	if err != nil {
		return a.failf("option-quote: %v", err)
	}
	data, err := client.OptionQuotes(identifiers)
	if err != nil {
		return a.failf("option-quote failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runOptionKline(client tigerService, args []string) int {
	fs := flag.NewFlagSet("option-kline", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	identifier := fs.String("identifier", "", "Option identifier, e.g. AAPL 260417C00200000")
	period := fs.String("period", "day", "Kline period, e.g. day")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*identifier) == "" {
		return a.failf("option-kline: --identifier is required")
	}
	data, err := client.OptionKlines(tigerapi.OptionKlineRequest{Identifier: *identifier, Period: *period})
	if err != nil {
		return a.failf("option-kline failed: %v", err)
	}
	return a.writeJSON(data)
}

func (a app) runRaw(client tigerService, args []string) int {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	method := fs.String("method", "", "Tiger API method name, e.g. option_expiration")
	bizContent := fs.String("biz-content", "{}", "Raw JSON biz_content payload")
	version := fs.String("version", "", "Optional Tiger API version, e.g. 2.0 or 3.0")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*method) == "" {
		return a.failf("raw: --method is required")
	}
	var payload any
	if err := json.Unmarshal([]byte(*bizContent), &payload); err != nil {
		return a.failf("raw: invalid --biz-content JSON: %v", err)
	}
	response, err := client.ExecuteRawResponseVersioned(*method, payload, strings.TrimSpace(*version))
	if err != nil {
		return a.failf("raw failed: %v", err)
	}
	_, _ = io.WriteString(a.stdout, response)
	if !strings.HasSuffix(response, "\n") {
		_, _ = io.WriteString(a.stdout, "\n")
	}
	return 0
}

func parseCSVList(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("a non-empty comma-separated list is required")
	}
	return out, nil
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
		"market-state",
		"stock-quote",
		"stock-kline",
		"stock-timeline",
		"stock-trade-tick",
		"stock-depth",
		"option-expirations",
		"option-chain",
		"option-quote",
		"option-kline",
		"raw",
	}
	sort.Strings(commands)
	_, _ = fmt.Fprintln(a.stderr, "Usage: go run ./cmd/tools/tigerapi -- <command> [flags]")
	_, _ = fmt.Fprintln(a.stderr, "")
	_, _ = fmt.Fprintln(a.stderr, "Commands:")
	for _, command := range commands {
		_, _ = fmt.Fprintf(a.stderr, "  %s\n", command)
	}
	_, _ = fmt.Fprintln(a.stderr, "")
	_, _ = fmt.Fprintln(a.stderr, "Examples:")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- stock-quote --symbols AAPL,MSFT")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- stock-kline --symbol AAPL --period day")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- option-chain --symbol AAPL --expiry 2026-04-17")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- option-kline --identifier 'AAPL 260417C00200000' --period day")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- raw --method option_expiration --biz-content '{\"symbols\":[\"AAPL\"]}'")
	_, _ = fmt.Fprintln(a.stderr, "  go run ./cmd/tools/tigerapi -- raw --method option_kline --version 2.0 --biz-content '{\"option_query\":[{\"symbol\":\"AAPL\",\"expiry\":1776398400000,\"right\":\"CALL\",\"strike\":200,\"period\":\"day\",\"begin_time\":-1,\"end_time\":4070880000000}]}'")
}
