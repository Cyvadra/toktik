package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Cyvadra/toktik/pkg/tigerapi"
)

type stubTigerService struct{}

func (s stubTigerService) MarketState(market string) ([]tigerapi.MarketState, error) {
	return []tigerapi.MarketState{{Market: market, Status: "Trading"}}, nil
}

func (s stubTigerService) StockQuotes(symbols []string) ([]tigerapi.StockQuote, error) {
	return []tigerapi.StockQuote{{Symbol: strings.ToUpper(symbols[0]), LatestPrice: 197.12}}, nil
}

func (s stubTigerService) StockKlines(req tigerapi.StockKlineRequest) ([]tigerapi.KlineBar, error) {
	return []tigerapi.KlineBar{{Symbol: strings.ToUpper(req.Symbol), Close: 197.12}}, nil
}

func (s stubTigerService) StockTimeline(symbols []string) ([]tigerapi.TimelinePoint, error) {
	return []tigerapi.TimelinePoint{{Symbol: strings.ToUpper(symbols[0]), Price: 197.12}}, nil
}

func (s stubTigerService) StockTradeTicks(symbols []string) ([]tigerapi.TradeTick, error) {
	return []tigerapi.TradeTick{{Symbol: strings.ToUpper(symbols[0]), Direction: "BUY"}}, nil
}

func (s stubTigerService) StockDepth(symbol string) (*tigerapi.QuoteDepth, error) {
	return &tigerapi.QuoteDepth{Symbol: strings.ToUpper(symbol)}, nil
}

func (s stubTigerService) OptionExpirations(underlying string) ([]string, error) {
	return []string{"2026-04-17"}, nil
}

func (s stubTigerService) OptionChain(underlying string, expiry string) ([]tigerapi.OptionContract, error) {
	return []tigerapi.OptionContract{{Identifier: "AAPL 260417C00200000", Symbol: strings.ToUpper(underlying), Expiry: expiry}}, nil
}

func (s stubTigerService) OptionQuotes(identifiers []string) ([]tigerapi.OptionQuote, error) {
	return []tigerapi.OptionQuote{{Identifier: identifiers[0], LatestPrice: 12.4}}, nil
}

func (s stubTigerService) OptionKlines(req tigerapi.OptionKlineRequest) ([]tigerapi.KlineBar, error) {
	return []tigerapi.KlineBar{{Symbol: req.Identifier, Close: 12.4}}, nil
}

func (s stubTigerService) ExecuteRawResponse(method string, bizParams any) (string, error) {
	return "{\"method\":\"" + method + "\"}", nil
}

func (s stubTigerService) ExecuteRawResponseVersioned(method string, bizParams any, version string) (string, error) {
	if version == "" {
		return s.ExecuteRawResponse(method, bizParams)
	}
	return "{\"method\":\"" + method + "\",\"version\":\"" + version + "\"}", nil
}

func TestRunStockQuote(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"stock-quote", "--symbols", "AAPL,MSFT"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"AAPL\"") {
		t.Fatalf("expected output to contain AAPL, got %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %s", stderr.String())
	}
}

func TestRunOptionKline(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"option-kline", "--identifier", "AAPL 260417C00200000", "--period", "day"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "12.4") {
		t.Fatalf("expected option kline output, got %s", stdout.String())
	}
}

func TestRunRaw(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"raw", "--method", "option_expiration", "--biz-content", `{"symbols":["AAPL"]}`})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "option_expiration") {
		t.Fatalf("expected raw output, got %s", stdout.String())
	}
}

func TestRunRawWithVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"raw", "--method", "option_kline", "--version", "2.0", "--biz-content", `{"option_query":[{"symbol":"AAPL"}]}`})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"version":"2.0"`) {
		t.Fatalf("expected raw versioned output, got %s", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"nope"})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected unknown command message, got %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected usage in stderr, got %s", stderr.String())
	}
}

func TestRunSupportsGoRunDoubleDash(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return stubTigerService{}, nil
		},
	}

	exitCode := cli.run([]string{"--", "stock-quote", "--symbols", "AAPL"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"AAPL\"") {
		t.Fatalf("expected stock quote output, got %s", stdout.String())
	}
}

func TestRunClientInitError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (tigerService, error) {
			return nil, errors.New("boom")
		},
	}

	exitCode := cli.run([]string{"stock-quote", "--symbols", "AAPL"})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "init tigerapi client") {
		t.Fatalf("expected init error, got %s", stderr.String())
	}
}
