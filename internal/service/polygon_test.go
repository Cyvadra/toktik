package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
)

type stubPolygonClient struct {
	stockSnapshotCalls  int
	stockAggregateCalls int
	optionChainReq      polygonpkg.OptionChainRequest
}

func (s *stubPolygonClient) DownloadStockMinuteAggregates(context.Context, time.Time, bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) DownloadOptionMinuteAggregates(context.Context, time.Time, bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) DownloadStockDailyAggregates(context.Context, time.Time, bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) DownloadOptionDailyAggregates(context.Context, time.Time, bool) (string, error) {
	return "", nil
}

func (s *stubPolygonClient) StockSnapshot(ctx context.Context, symbol string) (*polygonpkg.StockSnapshot, error) {
	s.stockSnapshotCalls++
	return &polygonpkg.StockSnapshot{Ticker: symbol}, nil
}

func (s *stubPolygonClient) StockAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	s.stockAggregateCalls++
	return []polygonpkg.AggregateBar{{Ticker: req.Ticker, Close: 123.4}}, nil
}

func (s *stubPolygonClient) StockQuotes(ctx context.Context, symbol string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	return nil, nil
}

func (s *stubPolygonClient) StockTrades(ctx context.Context, symbol string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionContract(ctx context.Context, ticker string) (*polygonpkg.OptionContract, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionChain(ctx context.Context, req polygonpkg.OptionChainRequest) ([]polygonpkg.OptionChainContract, error) {
	s.optionChainReq = req
	return nil, nil
}

func (s *stubPolygonClient) OptionAggregates(ctx context.Context, req polygonpkg.AggregateRequest) ([]polygonpkg.AggregateBar, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionQuotes(ctx context.Context, ticker string, req polygonpkg.QuoteRequest) ([]polygonpkg.Quote, error) {
	return nil, nil
}

func (s *stubPolygonClient) OptionTrades(ctx context.Context, ticker string, req polygonpkg.TradeRequest) ([]polygonpkg.Trade, error) {
	return nil, nil
}

func (s *stubPolygonClient) MarketStatusNow(ctx context.Context) (*polygonpkg.MarketStatus, error) {
	return &polygonpkg.MarketStatus{Market: "open"}, nil
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

func TestPolygonServiceQueryOptionChainClampsLimit(t *testing.T) {
	client := &stubPolygonClient{}
	svc := NewPolygonService(client, nil)

	_, err := svc.QueryOptionChain(context.Background(), dto.PolygonOptionChainRequest{
		Underlying:        "EWH",
		ExpirationDateGte: "2026-05-07",
		ExpirationDateLte: "2026-05-31",
		Limit:             500,
	})
	if err != nil {
		t.Fatalf("QueryOptionChain failed: %v", err)
	}
	if client.optionChainReq.Limit != polygonOptionChainMaxLimit {
		t.Fatalf("expected option chain limit to clamp to %d, got %d", polygonOptionChainMaxLimit, client.optionChainReq.Limit)
	}
}
