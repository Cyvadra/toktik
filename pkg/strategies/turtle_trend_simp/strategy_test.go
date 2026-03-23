package turtletrendsimp

import (
	"math"
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
		longAddCount:         2,
		longSpreadEntryPrice: 123.45,
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
	if strategy.longSpreadEntryPrice != 0 {
		t.Fatalf("longSpreadEntryPrice = %f, want 0", strategy.longSpreadEntryPrice)
	}
}

func TestTurtleTrendSimpDetachShortSeriesResetsActiveState(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{
		shortSlots: [2]*slotState{
			{spreadID: 21, entryPrice: 99},
			{spreadID: 22, entryPrice: 95},
		},
		shortAddCount:         1,
		shortSpreadEntryPrice: 87.6,
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
	if strategy.shortSpreadEntryPrice != 0 {
		t.Fatalf("shortSpreadEntryPrice = %f, want 0", strategy.shortSpreadEntryPrice)
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

func TestTurtleTrendSimpConsumeHTFSignal(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{lastHTFSignalIndex: -1}

	if strategy.consumeHTFSignal(math.NaN()) {
		t.Fatal("expected NaN signal index to be ignored")
	}
	if !strategy.consumeHTFSignal(0) {
		t.Fatal("expected first HTF signal index to be consumed")
	}
	if strategy.consumeHTFSignal(0) {
		t.Fatal("expected duplicate HTF signal index to be ignored")
	}
	if !strategy.consumeHTFSignal(1) {
		t.Fatal("expected next HTF signal index to be consumed")
	}
}

func TestTurtleTrendSimpAllowInitialEntry(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{}

	if strategy.allowInitialEntry(0) {
		t.Fatal("expected low-vol filter to block initial entries when flag is 0")
	}
	if strategy.allowInitialEntry(math.NaN()) {
		t.Fatal("expected low-vol filter to block initial entries when flag is NaN")
	}
	if !strategy.allowInitialEntry(1) {
		t.Fatal("expected low-vol filter to allow initial entries when flag is 1")
	}
}

func TestTurtleTrendSimpApplyDefaultsSetsTinySpotSignalNotional(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{}

	strategy.applyDefaults()

	if strategy.SpotSignalNotional != turtleTrendSimpSpotSignalNotional {
		t.Fatalf("SpotSignalNotional = %g, want %g", strategy.SpotSignalNotional, turtleTrendSimpSpotSignalNotional)
	}
	if strategy.SpotSignalNotional >= 1 {
		t.Fatalf("SpotSignalNotional = %g, want a negligible signal-only notional", strategy.SpotSignalNotional)
	}
}

func TestTurtleTrendSimpShouldOpenLongSpreadAddRequiresFreshCross(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{
		longSpreadEntryPrice: 100,
		longAddCount:         0,
	}

	if !strategy.shouldOpenLongSpreadAdd(100, 116, 10) {
		t.Fatal("expected first long spread add to trigger on the first fresh threshold cross")
	}

	strategy.longAddCount = 1
	if strategy.shouldOpenLongSpreadAdd(116, 117, 10) {
		t.Fatal("expected second long spread add to stay blocked without a fresh cross on the next bar")
	}
	if !strategy.shouldOpenLongSpreadAdd(114, 116, 10) {
		t.Fatal("expected second long spread add to trigger once price freshly crosses the next ladder")
	}
	strategy.longAddCount = 2
	if strategy.shouldOpenLongSpreadAdd(114, 130, 10) {
		t.Fatal("expected long spread add to stop after the configured maximum")
	}
}

func TestTurtleTrendSimpShouldOpenShortSpreadAddRequiresFreshCross(t *testing.T) {
	strategy := &turtleTrendSimpStrategy{
		shortSpreadEntryPrice: 100,
		shortAddCount:         0,
	}

	if !strategy.shouldOpenShortSpreadAdd(100, 92, 10) {
		t.Fatal("expected short spread add to trigger on the first fresh threshold cross")
	}
	strategy.shortAddCount = 1
	if strategy.shouldOpenShortSpreadAdd(92, 91, 10) {
		t.Fatal("expected short spread add to stay blocked after the maximum add count")
	}
}
