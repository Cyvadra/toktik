package backtest

import (
	"math"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fix 1: positionMarketValue NaN guard
// ---------------------------------------------------------------------------

// TestBrokerEquityNaNMarkPrice ensures that a NaN mark price does not corrupt
// the equity curve (the position should simply be excluded from the total).
func TestBrokerEquityNaNMarkPrice(t *testing.T) {
	ref := SecurityRef{Market: "m", Symbol: "s", Interval: "1h", Index: 0}
	broker := NewBroker(Config{InitialCapital: 1000})

	// Force NaN close price
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices {
		return BarPrices{Open: 100, High: 101, Low: 99, Close: math.NaN()}
	})

	broker.SubmitOrder(Order{Security: ref, Side: Buy, Type: MarketOrder, Qty: 1})
	broker.ProcessPending(1, time.Unix(0, 0))

	equity := broker.Equity()
	if math.IsNaN(equity) {
		t.Fatalf("Equity() returned NaN when mark price is NaN; expected finite fallback")
	}
	// Cash should have decreased by fill price (100), position market value
	// excluded due to NaN, so equity ≈ 1000 - 100 = 900.
	if equity != 900 {
		t.Fatalf("expected equity 900, got %v", equity)
	}
}

// ---------------------------------------------------------------------------
// Fix 2: computeTradePnL includes entry commission
// ---------------------------------------------------------------------------

// TestComputeTradePnLIncludesEntryCommission checks that the entry commission
// is charged on a round trip so that win/loss stats reflect true net PnL.
func TestComputeTradePnLIncludesEntryCommission(t *testing.T) {
	ref := SecurityRef{Market: "m", Symbol: "s", Interval: "1h", Index: 0}

	trades := []Trade{
		{Security: ref, Side: Buy, Qty: 1, FillPrice: 100, Commission: 1},  // entry: pay $1
		{Security: ref, Side: Sell, Qty: 1, FillPrice: 105, Commission: 1}, // exit:  pay $1
	}
	// Gross PnL = 5, total commission = 2 → net = 3
	pnls := computeTradePnL(trades)
	if len(pnls) != 1 {
		t.Fatalf("expected 1 round trip, got %d", len(pnls))
	}
	if pnls[0] != 3 {
		t.Fatalf("expected net pnl 3 (entry+exit commission), got %v", pnls[0])
	}
}

// TestComputeTradePnLPartialCloseCommission verifies proportional entry
// commission deduction when a position is partially closed.
func TestComputeTradePnLPartialCloseCommission(t *testing.T) {
	ref := SecurityRef{Market: "m", Symbol: "s", Interval: "1h", Index: 0}

	trades := []Trade{
		{Security: ref, Side: Buy, Qty: 2, FillPrice: 100, Commission: 2}, // entry 2 units @ $100, $2 commission
		{Security: ref, Side: Sell, Qty: 1, FillPrice: 110, Commission: 1}, // close 1 unit @ $110, $1 commission
	}
	// Gross PnL for 1 unit = 10
	// Proportional entry commission = 2 * (1/2) = 1
	// Exit commission = 1
	// Net = 10 - 1 - 1 = 8
	pnls := computeTradePnL(trades)
	if len(pnls) != 1 {
		t.Fatalf("expected 1 round trip, got %d", len(pnls))
	}
	if pnls[0] != 8 {
		t.Fatalf("expected net pnl 8, got %v", pnls[0])
	}
}

// ---------------------------------------------------------------------------
// Fix 3: Sharpe ratio annualization factor
// ---------------------------------------------------------------------------

// TestInferBarsPerYearHourly verifies that hourly bars yield ~8766 bars/year.
func TestInferBarsPerYearHourly(t *testing.T) {
	timestamps := make([]time.Time, 100)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range timestamps {
		timestamps[i] = base.Add(time.Duration(i) * time.Hour)
	}
	bpy := inferBarsPerYear(timestamps)
	// 365.25 * 24 ≈ 8766 bars/year
	expected := 365.25 * 24.0
	if math.Abs(bpy-expected)/expected > 0.01 {
		t.Fatalf("expected ~%.1f bars/year for hourly data, got %.1f", expected, bpy)
	}
}

// TestInferBarsPerYearDaily verifies that daily bars yield ~365.25 bars/year.
func TestInferBarsPerYearDaily(t *testing.T) {
	timestamps := make([]time.Time, 100)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range timestamps {
		timestamps[i] = base.Add(time.Duration(i) * 24 * time.Hour)
	}
	bpy := inferBarsPerYear(timestamps)
	expected := 365.25
	if math.Abs(bpy-expected)/expected > 0.01 {
		t.Fatalf("expected ~%.2f bars/year for daily data, got %.2f", expected, bpy)
	}
}

// TestInferBarsPerYearFallback checks the fallback for < 2 timestamps.
func TestInferBarsPerYearFallback(t *testing.T) {
	if got := inferBarsPerYear(nil); got != 252 {
		t.Fatalf("expected fallback 252 for empty slice, got %v", got)
	}
	if got := inferBarsPerYear([]time.Time{time.Now()}); got != 252 {
		t.Fatalf("expected fallback 252 for single-element slice, got %v", got)
	}
}

// TestSharpeRatioScalesWithInterval confirms that running the same strategy
// on hourly bars produces a Sharpe that is scaled by sqrt(8766/252) compared
// to daily bars (all else equal, same price path, same returns).
func TestSharpeRatioAnnualizationDiffersForDailyVsHourly(t *testing.T) {
	nBars := 50
	curve := make([]float64, nBars)
	equity := 10000.0
	for i := range curve {
		equity *= 1.001
		curve[i] = equity
	}

	dailyTS := make([]time.Time, nBars)
	hourlyTS := make([]time.Time, nBars)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range dailyTS {
		dailyTS[i] = base.Add(time.Duration(i) * 24 * time.Hour)
		hourlyTS[i] = base.Add(time.Duration(i) * time.Hour)
	}

	rDaily := computeResult("s", nil, curve, dailyTS, 10000, "", nil, nil)
	rHourly := computeResult("s", nil, curve, hourlyTS, 10000, "", nil, nil)

	// Hourly Sharpe should be larger because more bars/year (same return pattern)
	if rHourly.SharpeRatio <= rDaily.SharpeRatio {
		t.Fatalf("expected hourly Sharpe (%.4f) > daily Sharpe (%.4f)", rHourly.SharpeRatio, rDaily.SharpeRatio)
	}

	// Ratio should be sqrt(8766/365.25) ≈ sqrt(24) ≈ 4.899
	ratio := rHourly.SharpeRatio / rDaily.SharpeRatio
	expectedRatio := math.Sqrt(24.0)
	if math.Abs(ratio-expectedRatio)/expectedRatio > 0.05 {
		t.Fatalf("expected Sharpe ratio hourly/daily ≈ sqrt(24) = %.3f, got %.3f", expectedRatio, ratio)
	}
}

// ---------------------------------------------------------------------------
// Fix 4: meanStd uses sample std dev (N-1)
// ---------------------------------------------------------------------------

func TestMeanStdSampleVariance(t *testing.T) {
	data := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	_, std := meanStd(data)
	// Population std = 2.0; sample std = sqrt(32/7) ≈ 2.1381
	expected := math.Sqrt(32.0 / 7.0)
	if math.Abs(std-expected) > 1e-10 {
		t.Fatalf("expected sample std %.10f, got %.10f", expected, std)
	}
}

func TestMeanStdSingleElement(t *testing.T) {
	mean, std := meanStd([]float64{42})
	if mean != 42 {
		t.Fatalf("expected mean 42, got %v", mean)
	}
	if std != 0 {
		t.Fatalf("expected std 0 for single element, got %v", std)
	}
}

// ---------------------------------------------------------------------------
// Fix 5: SpreadTracker O(1) Get
// ---------------------------------------------------------------------------

func TestSpreadTrackerGetO1(t *testing.T) {
	st := NewSpreadTracker()
	now := time.Unix(0, 0)
	contract := OptionContract{Symbol: "OPT"}

	// Open several spreads
	var ids []int
	for i := 0; i < 5; i++ {
		id := st.Open([]SpreadLeg{{Contract: contract, Side: Sell, Qty: 1, EntryPrice: 1}}, now, i, "t")
		ids = append(ids, id)
	}

	// All should be retrievable via map in O(1)
	for _, id := range ids {
		sp := st.Get(id)
		if sp == nil {
			t.Fatalf("Get(%d) returned nil", id)
		}
		if sp.ID != id {
			t.Fatalf("Get(%d) returned spread with ID %d", id, sp.ID)
		}
	}

	// Non-existent ID should return nil
	if st.Get(9999) != nil {
		t.Fatal("expected nil for unknown ID")
	}
}

// TestSpreadTrackerGetAfterClose ensures Get still works after a spread is closed.
func TestSpreadTrackerGetAfterClose(t *testing.T) {
	st := NewSpreadTracker()
	now := time.Unix(0, 0)
	contract := OptionContract{Symbol: "OPT"}

	id := st.Open([]SpreadLeg{{Contract: contract, Side: Sell, Qty: 1, EntryPrice: 2}}, now, 0, "t")
	st.CloseLeg(id, 0, 1, now.Add(time.Hour))

	sp := st.Get(id)
	if sp == nil {
		t.Fatal("Get returned nil for closed spread")
	}
	if !sp.IsFullyClosed() {
		t.Fatal("expected spread to be fully closed")
	}
}

// ---------------------------------------------------------------------------
// Fix 7: OpenSpreadWithRef does not mutate caller's legs slice
// ---------------------------------------------------------------------------

func TestOpenSpreadWithRefDoesNotMutateCallerSlice(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 10000})
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices { return BarPrices{Close: 100} })

	tracker := NewSpreadTracker()
	now := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	bc := &BarContext{
		barTime:       now,
		broker:        broker,
		spreadTracker: tracker,
	}

	contract := OptionContract{Symbol: "OPT-C-100", MarkPrice: 5, BidPrice: 4.9, AskPrice: 5.1}
	legs := []SpreadLeg{
		{Contract: contract, Side: Buy, Qty: 1, EntryPrice: 5},
	}
	zeroTime := legs[0].EntryTime // should be zero value

	bc.OpenSpreadWithRef(legs, "test", "")

	// Caller's slice should not be modified
	if !legs[0].EntryTime.Equal(zeroTime) {
		t.Fatalf("OpenSpreadWithRef mutated caller's legs[0].EntryTime: got %v, want zero", legs[0].EntryTime)
	}

	// The spread should have been opened with the bar time
	sp := tracker.Get(1)
	if sp == nil {
		t.Fatal("spread not found")
	}
	if !sp.Legs[0].EntryTime.Equal(now) {
		t.Fatalf("spread leg EntryTime = %v, want %v", sp.Legs[0].EntryTime, now)
	}
}
