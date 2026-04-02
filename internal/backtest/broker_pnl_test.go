package backtest

import (
	"math"
	"testing"
	"time"
)

func TestBrokerPositionAndTotalPnL(t *testing.T) {
	ref := SecurityRef{Market: "m", Symbol: "s", Interval: "1h", Index: 0}
	broker := NewBroker(Config{InitialCapital: 1000})

	open := 100.0
	close := 110.0
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices {
		return BarPrices{Open: open, High: close, Low: open, Close: close}
	})

	broker.SubmitOrder(Order{Security: ref, Side: Buy, Type: MarketOrder, Qty: 1})
	fills := broker.ProcessPending(1, time.Unix(0, 0))
	if len(fills) != 1 {
		t.Fatalf("expected one fill, got %d", len(fills))
	}

	posPnL := broker.PositionUnrealizedPnL(ref)
	if posPnL != 10 {
		t.Fatalf("expected position unrealized pnl 10, got %v", posPnL)
	}

	totalPnL := broker.TotalPnL()
	if totalPnL != 10 {
		t.Fatalf("expected total pnl 10, got %v", totalPnL)
	}
}

func TestBarContextTotalPnLIncludesOpenSpreadMark(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 1000})
	tracker := NewSpreadTracker()
	now := time.Unix(0, 0)

	contract := OptionContract{Symbol: "OPT", MarkPrice: 8, BidPrice: 7.5, AskPrice: 8.5}
	tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Buy,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  now,
	}}, now, 0, "t")
	broker.AdjustCash(-5)

	bc := &BarContext{
		barTime:       now,
		broker:        broker,
		spreadTracker: tracker,
	}

	if pnl := bc.TotalPnL(); pnl != 3 {
		t.Fatalf("expected total pnl 3, got %v", pnl)
	}
}

func TestBarContextTotalPnLUsesLastSeenSpreadContractWhenSnapshotMissing(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 1000})
	tracker := NewSpreadTracker()
	now := time.Unix(0, 0)

	tracker.Open([]SpreadLeg{{
		Contract:   OptionContract{Symbol: "OPT", MarkPrice: 5, BidPrice: 4.9, AskPrice: 5.1},
		Side:       Buy,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  now,
	}}, now, 0, "t")
	broker.AdjustCash(-5)

	refreshOpenSpreadContracts(tracker, map[string]OptionContract{
		"OPT": {Symbol: "OPT", MarkPrice: 8, BidPrice: 7.9, AskPrice: 8.1},
	})

	bc := &BarContext{
		barTime:       now.Add(time.Hour),
		broker:        broker,
		spreadTracker: tracker,
	}

	if pnl := bc.TotalPnL(); pnl != 3 {
		t.Fatalf("expected total pnl 3 after missing snapshot fallback, got %v", pnl)
	}
}

func TestClosePositionStopNowWithExtraSlippage(t *testing.T) {
	ref := SecurityRef{Market: "m", Symbol: "s", Interval: "1h", Index: 0}
	broker := NewBroker(Config{InitialCapital: 1000, SlippagePct: 0.001})
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices {
		return BarPrices{Open: 100, High: 101, Low: 94, Close: 96}
	})

	broker.SubmitOrder(Order{Security: ref, Side: Buy, Type: MarketOrder, Qty: 1})
	broker.ProcessPending(0, time.Unix(0, 0))

	bc := &BarContext{
		barIndex: 1,
		barTime:  time.Unix(3600, 0),
		broker:   broker,
	}
	if !bc.ClosePositionStopNowWithNote(ref, 95, 0.005, "stop") {
		t.Fatal("expected stop close to execute")
	}
	trades := broker.Trades()
	if len(trades) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(trades))
	}
	stopTrade := trades[1]
	wantFill := 95.0 * (1 - 0.006)
	if math.Abs(stopTrade.FillPrice-wantFill) > 1e-9 {
		t.Fatalf("stopTrade.FillPrice = %.12f, want %.12f", stopTrade.FillPrice, wantFill)
	}
	if qty := broker.Positions().Get(ref).Qty; qty != 0 {
		t.Fatalf("position qty = %v, want 0", qty)
	}
}

func TestCloseSpreadLegStopNowWithExtraSlippage(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 1000, SlippagePct: 0.001})
	tracker := NewSpreadTracker()
	now := time.Unix(0, 0)
	contract := OptionContract{Symbol: "OPT", MarkPrice: 8}
	spreadID := tracker.Open([]SpreadLeg{{
		Contract:   contract,
		Side:       Buy,
		Qty:        1,
		EntryPrice: 5,
		EntryTime:  now,
	}}, now, 0, "t")
	broker.AdjustCash(-5)

	bc := &BarContext{
		barIndex:      1,
		barTime:       time.Unix(3600, 0),
		broker:        broker,
		spreadTracker: tracker,
		primary: map[string][]float64{
			"open":  {100, 100},
			"high":  {101, 101},
			"low":   {94, 94},
			"close": {96, 96},
		},
	}
	if !bc.CloseSpreadLegStopNowWithReason(spreadID, 0, 95, 4.5, 0.01, "spread stop") {
		t.Fatal("expected spread leg stop to execute")
	}
	if got := len(tracker.open); got != 0 {
		t.Fatalf("expected no open spreads, got %d", got)
	}
	if len(tracker.closed) != 1 {
		t.Fatalf("expected 1 closed spread, got %d", len(tracker.closed))
	}
	wantClose := 4.5 * (1 - 0.011)
	if math.Abs(tracker.closed[0].Legs[0].ClosePrice-wantClose) > 1e-9 {
		t.Fatalf("close price = %.12f, want %.12f", tracker.closed[0].Legs[0].ClosePrice, wantClose)
	}
}
