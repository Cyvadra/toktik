package service

import (
	"context"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

type stubOptionsChainProvider struct {
	id int
}

func (p *stubOptionsChainProvider) AvailableContracts(time.Time) []backtest.OptionContract {
	return nil
}

func TestPortfolioBacktestServiceLoadOptionsChainProviderReusesCachedProvider(t *testing.T) {
	svc := NewPortfolioBacktestService(nil, nil)
	t.Cleanup(func() { _ = svc.Close() })
	var loadCalls int
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loadCalls++
		return &stubOptionsChainProvider{id: loadCalls}, nil
	}

	from := time.Date(2023, 1, 11, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC)

	first, err := svc.loadOptionsChainProvider(context.Background(), marketCrypto, "BTC", "15m", from, to)
	if err != nil {
		t.Fatalf("first loadOptionsChainProvider returned error: %v", err)
	}
	second, err := svc.loadOptionsChainProvider(context.Background(), marketCrypto, "BTC", "15m", from, to)
	if err != nil {
		t.Fatalf("second loadOptionsChainProvider returned error: %v", err)
	}
	if loadCalls != 1 {
		t.Fatalf("expected one loader call, got %d", loadCalls)
	}
	if first != second {
		t.Fatal("expected cached provider instance to be reused")
	}
}

func TestPortfolioBacktestServiceLoadOptionsChainProviderReloadsExpiredEntry(t *testing.T) {
	now := time.Date(2026, 4, 14, 9, 0, 0, 0, time.UTC)
	svc := NewPortfolioBacktestService(nil, nil)
	t.Cleanup(func() { _ = svc.Close() })
	svc.now = func() time.Time { return now }
	svc.chainCache = newOptionsChainProviderCache(svc.now, time.Minute, 4)

	var loadCalls int
	svc.chainLoader = func(_ context.Context, marketName, asset, interval string, from, to time.Time) (backtest.OptionsChainProvider, error) {
		loadCalls++
		return &stubOptionsChainProvider{id: loadCalls}, nil
	}

	from := time.Date(2023, 1, 11, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC)

	first, err := svc.loadOptionsChainProvider(context.Background(), marketCrypto, "BTC", "15m", from, to)
	if err != nil {
		t.Fatalf("first loadOptionsChainProvider returned error: %v", err)
	}
	now = now.Add(30 * time.Second)
	second, err := svc.loadOptionsChainProvider(context.Background(), marketCrypto, "BTC", "15m", from, to)
	if err != nil {
		t.Fatalf("second loadOptionsChainProvider returned error: %v", err)
	}
	now = now.Add(31 * time.Second)
	third, err := svc.loadOptionsChainProvider(context.Background(), marketCrypto, "BTC", "15m", from, to)
	if err != nil {
		t.Fatalf("third loadOptionsChainProvider returned error: %v", err)
	}

	if loadCalls != 2 {
		t.Fatalf("expected two loader calls after expiry, got %d", loadCalls)
	}
	if first != second {
		t.Fatal("expected cached provider before TTL expiry")
	}
	if third == second {
		t.Fatal("expected provider to reload after TTL expiry")
	}
}

func TestPortfolioBacktestServiceCloseIsIdempotent(t *testing.T) {
	svc := NewPortfolioBacktestService(nil, nil)
	if err := svc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}
