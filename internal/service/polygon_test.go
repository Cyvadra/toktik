package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

type stubPolygonClient struct {
	stockSnapshotCalls  int
	stockAggregateCalls int
}

func (s *stubPolygonClient) DownloadStockMinuteAggregates(date time.Time, force bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) DownloadOptionMinuteAggregates(date time.Time, force bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) StockSnapshot(symbol string) (*polygonpkg.StockSnapshot, error) {
	s.stockSnapshotCalls++
	return &polygonpkg.StockSnapshot{Ticker: symbol}, nil
}

func (s *stubPolygonClient) StockAggregates(req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	s.stockAggregateCalls++
	return []polygonpkg.AggregateBar{{Ticker: req.Ticker, Close: 123.4}}, nil
}

func (s *stubPolygonClient) StockQuotes(symbol string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	return nil, nil
}

func (s *stubPolygonClient) StockTrades(symbol string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionContract(ticker string) (*polygonpkg.OptionContract, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionChain(req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionAggregates(req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionQuotes(ticker string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionTrades(ticker string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	return nil, nil
}

func TestPolygonServiceStockSnapshotUsesCache(t *testing.T) {
	client := &stubPolygonClient{}
	svc := NewPolygonService(client, cache.NewMemoryStore())

	ctx := context.Background()
	first, err := svc.StockSnapshot(ctx, "AAPL")
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	second, err := svc.StockSnapshot(ctx, "AAPL")
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if first == nil || second == nil || first.Ticker != "AAPL" || second.Ticker != "AAPL" {
		t.Fatalf("unexpected snapshot payloads: first=%#v second=%#v", first, second)
	}
	if client.stockSnapshotCalls != 1 {
		t.Fatalf("expected one upstream snapshot call, got %d", client.stockSnapshotCalls)
	}
}

func TestPolygonServiceWindowTTL(t *testing.T) {
	svc := NewPolygonService(&stubPolygonClient{}, nil)
	now := time.Date(2026, 4, 9, 15, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want time.Duration
	}{
		{name: "realtime", from: now.Add(-2 * time.Minute), to: now.Add(-30 * time.Second), want: polygonRealtimeTTL},
		{name: "recent", from: now.Add(-3 * time.Hour), to: now.Add(-2 * time.Hour), want: polygonRecentTTL},
		{name: "historical", from: now.Add(-72 * time.Hour), to: now.Add(-48 * time.Hour), want: polygonHistoricalTTL},
	}

	for _, tt := range tests {
		if got := svc.windowTTL(tt.from, tt.to); got != tt.want {
			t.Fatalf("%s: windowTTL() = %s, want %s", tt.name, got, tt.want)
		}
	}
}
