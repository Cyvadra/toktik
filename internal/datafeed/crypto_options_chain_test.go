package datafeed

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestResolveCryptoChainCacheIntervalFallsBackToDaily(t *testing.T) {
	t.Parallel()

	if got := resolveCryptoChainCacheInterval("1h"); got != "1d" {
		t.Fatalf("expected 1h to fall back to 1d cache, got %q", got)
	}
	if got := resolveCryptoChainCacheInterval("1d"); got != "1d" {
		t.Fatalf("expected 1d to resolve to itself, got %q", got)
	}
}

func TestExpandCachedChainContractsReplicatesDailyCacheAcrossIntradayBuckets(t *testing.T) {
	t.Parallel()

	contracts := []backtest.OptionContract{{Symbol: "BTC-TEST"}}
	byTimestamp := make(map[int64][]backtest.OptionContract)
	from := time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	expandCachedChainContracts(byTimestamp, contracts, from, from, to, time.Hour, 24*time.Hour)

	if len(byTimestamp) != 24 {
		t.Fatalf("expected 24 hourly buckets, got %d", len(byTimestamp))
	}
	if got := len(byTimestamp[from.Unix()]); got != 1 {
		t.Fatalf("expected contracts at day start, got %d", got)
	}
	if got := len(byTimestamp[from.Add(23*time.Hour).Unix()]); got != 1 {
		t.Fatalf("expected contracts at last hourly bucket, got %d", got)
	}
}
