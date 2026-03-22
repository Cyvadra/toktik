package strategies

import (
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestBTCCoinEnhancedManageCallPositionsHalfThenFullClose(t *testing.T) {
	now := time.Date(2026, time.March, 22, 0, 0, 0, 0, time.UTC)
	broker := backtest.NewBroker(backtest.Config{InitialCapital: 1000})
	tracker := backtest.NewSpreadTracker()

	entryContract := backtest.OptionContract{
		Symbol:     "BTC-CALL",
		Type:       backtest.Call,
		Expiration: now.Add(30 * 24 * time.Hour),
		MarkPrice:  10,
	}

	firstID := tracker.Open([]backtest.SpreadLeg{{
		Contract:   entryContract,
		Side:       backtest.Sell,
		Qty:        5,
		EntryPrice: 10,
		EntryTime:  now,
	}}, now, 0, "call-1")
	secondID := tracker.Open([]backtest.SpreadLeg{{
		Contract:   entryContract,
		Side:       backtest.Sell,
		Qty:        5,
		EntryPrice: 10,
		EntryTime:  now,
	}}, now, 0, "call-2")

	strategy := &btcCoinEnhancedStrategy{
		CallHalfProfit: 0.70,
		CallFullProfit: 0.88,
		activeCalls: [2]*enhancedSlot{
			{spreadID: firstID, entryPrice: 10, qty: 5},
			{spreadID: secondID, entryPrice: 10, qty: 5},
		},
	}

	ctx := newTestBarContext(1, now, nil, broker, tracker)

	strategy.manageCallPositions(ctx, map[string]backtest.OptionContract{
		"BTC-CALL": {
			Symbol:     "BTC-CALL",
			Type:       backtest.Call,
			Expiration: now.Add(30 * 24 * time.Hour),
			MarkPrice:  2,
		},
	})

	if got := strategy.countActiveCalls(); got != 1 {
		t.Fatalf("countActiveCalls() after half close = %d, want 1", got)
	}

	closed := 0
	for _, spreadID := range []int{firstID, secondID} {
		sp := tracker.Get(spreadID)
		if sp == nil {
			t.Fatalf("spread %d not found", spreadID)
		}
		if sp.Legs[0].Closed {
			closed++
		}
	}
	if closed != 1 {
		t.Fatalf("closed call tranches after half close = %d, want 1", closed)
	}

	ctx = newTestBarContext(2, now, nil, broker, tracker)
	strategy.manageCallPositions(ctx, map[string]backtest.OptionContract{
		"BTC-CALL": {
			Symbol:     "BTC-CALL",
			Type:       backtest.Call,
			Expiration: now.Add(30 * 24 * time.Hour),
			MarkPrice:  1,
		},
	})

	if got := strategy.countActiveCalls(); got != 0 {
		t.Fatalf("countActiveCalls() after full close = %d, want 0", got)
	}
	for _, spreadID := range []int{firstID, secondID} {
		sp := tracker.Get(spreadID)
		if sp == nil {
			t.Fatalf("spread %d not found", spreadID)
		}
		if !sp.Legs[0].Closed {
			t.Fatalf("spread %d should be closed after full close", spreadID)
		}
	}
}

func TestBTCCoinEnhancedHasOpenExposureIncludesPutOnly(t *testing.T) {
	strategy := &btcCoinEnhancedStrategy{}
	if strategy.hasOpenExposure() {
		t.Fatal("expected no active exposure")
	}

	strategy.activePut = &enhancedSlot{spreadID: 42}
	if !strategy.hasOpenExposure() {
		t.Fatal("expected put-only state to count as active exposure")
	}
}

func TestBTCCoinEnhancedCheckDivergenceMatchesDocumentFormula(t *testing.T) {
	strategy := &btcCoinEnhancedStrategy{DivergencePeriod: 30}
	primary := map[string][]float64{
		"high": {95, 100, 98, 99, 110},
		"hh30": {95, 100, 100, 100, 110},
		"macd": {1, 5, 4.5, 4.2, 4},
	}
	ctx := newTestBarContext(4, time.Time{}, primary, nil, nil)

	if !strategy.checkDivergence(ctx, "high", "hh30", "macd") {
		t.Fatal("expected divergence when current high breaks hh30 and MACD DIFF is below the prior high's DIFF")
	}

	primary["macd"][4] = 6
	if strategy.checkDivergence(ctx, "high", "hh30", "macd") {
		t.Fatal("expected no divergence when current MACD DIFF is above the prior high's DIFF")
	}
}

func TestBTCCoinEnhancedCheckVolConditionUsesStdToMAStdRatio(t *testing.T) {
	strategy := &btcCoinEnhancedStrategy{VolQuantilePeriod: 4, VolQuantileMin: 0.50}
	primary := map[string][]float64{
		"stdma20":  {0.20, 0.30, 0.40, 0.60},
		"ma_std20": {10, 9, 8, 1},
	}
	ctx := newTestBarContext(3, time.Time{}, primary, nil, nil)

	if !strategy.checkVolCondition(ctx, "stdma20") {
		t.Fatal("expected volatility condition to pass when stdma20 ratio is above the median of the lookback")
	}

	primary["stdma20"][3] = 0.10
	if strategy.checkVolCondition(ctx, "stdma20") {
		t.Fatal("expected volatility condition to fail when stdma20 ratio falls below the threshold")
	}
}

func TestBTCCoinEnhancedResolveHigherTimeframe(t *testing.T) {
	strategy := &btcCoinEnhancedStrategy{}

	got, err := strategy.resolveHigherTimeframe("3h")
	if err != nil || got != "12h" {
		t.Fatalf("resolveHigherTimeframe(3h) = (%q, %v), want (12h, nil)", got, err)
	}

	got, err = strategy.resolveHigherTimeframe("6h")
	if err != nil || got != "1d" {
		t.Fatalf("resolveHigherTimeframe(6h) = (%q, %v), want (1d, nil)", got, err)
	}

	if _, err := strategy.resolveHigherTimeframe("1h"); err == nil {
		t.Fatal("expected unsupported primary interval to return an error")
	}
}

func newTestBarContext(barIndex int, barTime time.Time, primary map[string][]float64, broker *backtest.Broker, tracker *backtest.SpreadTracker) *backtest.BarContext {
	ctx := &backtest.BarContext{}
	setUnexportedField(ctx, "barIndex", barIndex)
	setUnexportedField(ctx, "barTime", barTime)
	setUnexportedField(ctx, "primary", primary)
	setUnexportedField(ctx, "broker", broker)
	setUnexportedField(ctx, "spreadTracker", tracker)
	return ctx
}

func setUnexportedField(target interface{}, fieldName string, value interface{}) {
	v := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	dst := reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
	if value == nil {
		dst.Set(reflect.Zero(v.Type()))
		return
	}
	dst.Set(reflect.ValueOf(value))
}
