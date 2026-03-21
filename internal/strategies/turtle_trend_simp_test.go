package strategies

import (
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestTurtleTrendSimpDetachLongSeriesResetsActiveState(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{
		longSlots: [3]*slotState{
			{spreadID: 11, entryPrice: 100},
			nil,
			{spreadID: 13, entryPrice: 103},
		},
		longAddCount:       2,
		lastLongEntryPrice: 123.45,
	}

	strategy.detachLongSeries(10)

	if got := strategy.countLongSlots(); got != 0 {
		t.Fatalf("countLongSlots() = %d, want 0", got)
	}
	if got := len(strategy.longRemoved); got != 2 {
		t.Fatalf("len(longRemoved) = %d, want 2", got)
	}
	if strategy.longRemoved[0].spreadID != 11 || strategy.longRemoved[1].spreadID != 13 {
		t.Fatalf("unexpected detached long spread ids: %+v", strategy.longRemoved)
	}
	if strategy.longAddCount != 0 {
		t.Fatalf("longAddCount = %d, want 0", strategy.longAddCount)
	}
	if strategy.lastLongEntryPrice != 0 {
		t.Fatalf("lastLongEntryPrice = %f, want 0", strategy.lastLongEntryPrice)
	}
}

func TestTurtleTrendSimpDetachShortSeriesResetsActiveState(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{
		shortSlots: [2]*slotState{
			{spreadID: 21, entryPrice: 99},
			{spreadID: 22, entryPrice: 95},
		},
		shortAddCount:       1,
		lastShortEntryPrice: 87.6,
	}

	strategy.detachShortSeries(20)

	if got := strategy.countShortSlots(); got != 0 {
		t.Fatalf("countShortSlots() = %d, want 0", got)
	}
	if got := len(strategy.shortRemoved); got != 2 {
		t.Fatalf("len(shortRemoved) = %d, want 2", got)
	}
	if strategy.shortRemoved[0].spreadID != 21 || strategy.shortRemoved[1].spreadID != 22 {
		t.Fatalf("unexpected detached short spread ids: %+v", strategy.shortRemoved)
	}
	if strategy.shortAddCount != 0 {
		t.Fatalf("shortAddCount = %d, want 0", strategy.shortAddCount)
	}
	if strategy.lastShortEntryPrice != 0 {
		t.Fatalf("lastShortEntryPrice = %f, want 0", strategy.lastShortEntryPrice)
	}
}

func TestTurtleTrendSimpShouldCloseForExpiry(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{}
	now := time.Date(2026, time.March, 21, 9, 0, 0, 0, time.UTC)

	if !strategy.shouldCloseForExpiry(backtest.OptionContract{Expiration: now.Add(24 * time.Hour)}, now) {
		t.Fatal("expected contract with 1 day to expiry to be closed")
	}
	if strategy.shouldCloseForExpiry(backtest.OptionContract{Expiration: now.Add(25 * time.Hour)}, now) {
		t.Fatal("expected contract with more than 1 day to expiry to remain open")
	}
}
