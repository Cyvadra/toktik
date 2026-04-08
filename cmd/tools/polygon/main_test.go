package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/pkg/polygon"
)

type stubPolygonService struct {
	stockAggregateReq polygon.AggregateRequest
	optionChainReq    polygon.OptionChainRequest
	stockMinuteDate   time.Time
	optionMinuteDate  time.Time
	forceDownload     bool
}

func (s *stubPolygonService) DownloadStockMinuteAggregates(date time.Time, force bool) (string, error) {
	s.stockMinuteDate = date
	s.forceDownload = force
	return "/tmp/polygon/stocks/2026-04-07.csv.gz", nil
}

func (s *stubPolygonService) DownloadOptionMinuteAggregates(date time.Time, force bool) (string, error) {
	s.optionMinuteDate = date
	s.forceDownload = force
	return "/tmp/polygon/options/2026-04-07.csv.gz", nil
}

func (s *stubPolygonService) StockSnapshot(symbol string) (*polygon.StockSnapshot, error) {
	return &polygon.StockSnapshot{Ticker: strings.ToUpper(symbol)}, nil
}

func (s *stubPolygonService) StockAggregates(req polygon.AggregateRequest) ([]polygon.AggregateBar, error) {
	s.stockAggregateReq = req
	return []polygon.AggregateBar{{Ticker: strings.ToUpper(req.Ticker), Close: 191.2}}, nil
}

func (s *stubPolygonService) StockQuotes(symbol string, req polygon.QuoteRequest) ([]polygon.Quote, error) {
	return []polygon.Quote{{SequenceNumber: 10}}, nil
}

func (s *stubPolygonService) StockTrades(symbol string, req polygon.TradeRequest) ([]polygon.Trade, error) {
	return []polygon.Trade{{ID: "trade-1", Price: 197.12}}, nil
}

func (s *stubPolygonService) OptionContract(ticker string) (*polygon.OptionContract, error) {
	return &polygon.OptionContract{Ticker: strings.ToUpper(ticker), StrikePrice: 650}, nil
}

func (s *stubPolygonService) OptionChain(req polygon.OptionChainRequest) ([]polygon.OptionChainContract, error) {
	s.optionChainReq = req
	return []polygon.OptionChainContract{{Contract: polygon.OptionContract{Ticker: "O:SPY251219C00650000", StrikePrice: 650}}}, nil
}

func (s *stubPolygonService) OptionAggregates(req polygon.AggregateRequest) ([]polygon.AggregateBar, error) {
	return []polygon.AggregateBar{{Ticker: strings.ToUpper(req.Ticker), Close: 11.2}}, nil
}

func (s *stubPolygonService) OptionQuotes(ticker string, req polygon.QuoteRequest) ([]polygon.Quote, error) {
	return []polygon.Quote{{SequenceNumber: 31}}, nil
}

func (s *stubPolygonService) OptionTrades(ticker string, req polygon.TradeRequest) ([]polygon.Trade, error) {
	return []polygon.Trade{{Price: 11.15}}, nil
}

func TestRunStockSnapshot(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return &stubPolygonService{}, nil
		},
	}

	exitCode := cli.run([]string{"stock-snapshot", "--symbol", "AAPL"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"AAPL\"") {
		t.Fatalf("expected snapshot output, got %s", stdout.String())
	}
}

func TestRunStockAggregatesParsesFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	service := &stubPolygonService{}
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return service, nil
		},
	}

	exitCode := cli.run([]string{"stock-aggregates", "--ticker", "AAPL", "--multiplier", "2", "--timespan", "minute", "--from", "2025-11-03", "--to", "2025-11-28", "--adjusted", "true", "--sort", "asc", "--limit", "100"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if service.stockAggregateReq.Multiplier != 2 || service.stockAggregateReq.Limit != 100 || service.stockAggregateReq.Adjusted == nil || !*service.stockAggregateReq.Adjusted {
		t.Fatalf("unexpected aggregate request: %#v", service.stockAggregateReq)
	}
	if !strings.Contains(stdout.String(), "191.2") {
		t.Fatalf("expected aggregate output, got %s", stdout.String())
	}
}

func TestRunStockMinuteFlatFileParsesFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	service := &stubPolygonService{}
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return service, nil
		},
	}

	exitCode := cli.run([]string{"stock-minute-flatfile", "--date", "2026-04-07", "--force"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if service.stockMinuteDate.Format("2006-01-02") != "2026-04-07" || !service.forceDownload {
		t.Fatalf("unexpected stock flatfile request: date=%s force=%v", service.stockMinuteDate.Format("2006-01-02"), service.forceDownload)
	}
	if !strings.Contains(stdout.String(), "/tmp/polygon/stocks/2026-04-07.csv.gz") {
		t.Fatalf("expected stock flatfile output, got %s", stdout.String())
	}
}

func TestRunOptionChainParsesFilters(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	service := &stubPolygonService{}
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return service, nil
		},
	}

	exitCode := cli.run([]string{"option-chain", "--underlying", "SPY", "--expiration-date", "2025-12-19", "--contract-type", "call", "--strike-price-gte", "600", "--limit", "25"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if service.optionChainReq.Underlying != "SPY" || service.optionChainReq.ContractType != "call" || service.optionChainReq.StrikePriceGte == nil || *service.optionChainReq.StrikePriceGte != 600 || service.optionChainReq.Limit != 25 {
		t.Fatalf("unexpected option chain request: %#v", service.optionChainReq)
	}
	if !strings.Contains(stdout.String(), "O:SPY251219C00650000") {
		t.Fatalf("expected option chain output, got %s", stdout.String())
	}
}

func TestRunOptionMinuteFlatFileParsesFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	service := &stubPolygonService{}
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return service, nil
		},
	}

	exitCode := cli.run([]string{"option-minute-flatfile", "--date", "2026-04-07"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if service.optionMinuteDate.Format("2006-01-02") != "2026-04-07" {
		t.Fatalf("unexpected option flatfile request: date=%s", service.optionMinuteDate.Format("2006-01-02"))
	}
	if !strings.Contains(stdout.String(), "/tmp/polygon/options/2026-04-07.csv.gz") {
		t.Fatalf("expected option flatfile output, got %s", stdout.String())
	}
}

func TestRunSupportsGoRunDoubleDash(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return &stubPolygonService{}, nil
		},
	}

	exitCode := cli.run([]string{"--", "option-contract", "--ticker", "O:SPY251219C00650000"})
	if exitCode != 0 {
		t.Fatalf("run exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "650") {
		t.Fatalf("expected option contract output, got %s", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return &stubPolygonService{}, nil
		},
	}

	exitCode := cli.run([]string{"nope"})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "unknown command") || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected usage and unknown command output, got %s", stderr.String())
	}
}

func TestRunClientInitError(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cli := app{
		stdout: &stdout,
		stderr: &stderr,
		newClient: func() (polygonService, error) {
			return nil, errors.New("boom")
		},
	}

	exitCode := cli.run([]string{"stock-snapshot", "--symbol", "AAPL"})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "init polygon client") {
		t.Fatalf("expected init error, got %s", stderr.String())
	}
}
