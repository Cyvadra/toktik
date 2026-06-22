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

func TestUSOptionSpreadRealizedPnLUsesContractMultiplier(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 10000})
	tracker := NewSpreadTracker()
	now := time.Unix(0, 0)
	bc := &BarContext{barTime: now, broker: broker, spreadTracker: tracker}

	contract := OptionContract{Symbol: "O:SPY260120C00500000", UnderlyingMarket: "us", Type: Call, MarkPrice: 1.25}
	spreadID := bc.OpenSpread([]SpreadLeg{{Contract: contract, Side: Buy, Qty: 1, EntryPrice: 1.00}}, "long-call")
	if spreadID <= 0 {
		t.Fatal("OpenSpread() failed")
	}
	bc.barTime = now.Add(time.Hour)
	if !bc.CloseSpreadLeg(spreadID, 0, 1.25) {
		t.Fatal("CloseSpreadLeg() failed")
	}

	spread := tracker.Get(spreadID)
	if spread == nil {
		t.Fatal("spread not found")
	}
	if pnl := spread.TotalRealizedPnL(); math.Abs(pnl-25) > 1e-9 {
		t.Fatalf("spread realized pnl = %.12f, want 25", pnl)
	}
	if cash := broker.Cash(); math.Abs(cash-10025) > 1e-9 {
		t.Fatalf("broker cash = %.12f, want 10025", cash)
	}
}

func TestSpreadFlatCommissionAppliesOnOpenAndClose(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 10000, CommissionModel: CommissionFlat, CommissionValue: 0.65})
	tracker := NewSpreadTracker()
	now := time.Unix(0, 0)
	bc := &BarContext{barTime: now, broker: broker, spreadTracker: tracker}

	contract := OptionContract{Symbol: "O:SPY260120C00500000", UnderlyingMarket: "us", Type: Call}
	spreadID := bc.OpenSpread([]SpreadLeg{{Contract: contract, Side: Buy, Qty: 1, EntryPrice: 1.00}}, "long-call")
	if spreadID <= 0 {
		t.Fatal("OpenSpread() failed")
	}
	bc.barTime = now.Add(time.Hour)
	if !bc.CloseSpreadLeg(spreadID, 0, 1.25) {
		t.Fatal("CloseSpreadLeg() failed")
	}

	spread := tracker.Get(spreadID)
	if spread == nil {
		t.Fatal("spread not found")
	}
	if fees := spread.TotalFees(); math.Abs(fees-1.30) > 1e-9 {
		t.Fatalf("spread fees = %.12f, want 1.30", fees)
	}
	if pnl := spread.TotalRealizedPnL(); math.Abs(pnl-23.70) > 1e-9 {
		t.Fatalf("spread realized pnl = %.12f, want 23.70", pnl)
	}
	if cash := broker.Cash(); math.Abs(cash-10023.70) > 1e-9 {
		t.Fatalf("broker cash = %.12f, want 10023.70", cash)
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

func TestBrokerBaseAssetAccountingMarksUnderlyingByBTCPnL(t *testing.T) {
	ref := SecurityRef{Market: "crypto-underlying", Symbol: "BTC", Interval: "5m", Index: 0}
	broker := NewBroker(Config{InitialCapital: 100, AccountUnit: "BTC"})
	closePrice := 100.0
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices {
		return BarPrices{Open: closePrice, High: closePrice, Low: closePrice, Close: closePrice}
	})

	if _, ok := broker.ExecuteOrderAtCloseNow(Order{Security: ref, Side: Buy, Type: MarketOrder, Qty: 1}, 0, time.Unix(0, 0)); !ok {
		t.Fatal("expected entry fill")
	}
	if cash := broker.Cash(); math.Abs(cash-100) > 1e-9 {
		t.Fatalf("cash after base-asset entry = %.12f, want 100", cash)
	}
	if equity := broker.Equity(); math.Abs(equity-100) > 1e-9 {
		t.Fatalf("equity at entry = %.12f, want 100", equity)
	}

	closePrice = 110
	wantPnL := (110.0 - 100.0) / 110.0
	if pnl := broker.PositionUnrealizedPnL(ref); math.Abs(pnl-wantPnL) > 1e-9 {
		t.Fatalf("position unrealized pnl = %.12f, want %.12f", pnl, wantPnL)
	}
	if totalPnL := broker.TotalPnL(); math.Abs(totalPnL-wantPnL) > 1e-9 {
		t.Fatalf("total pnl = %.12f, want %.12f", totalPnL, wantPnL)
	}
	if equity := broker.Equity(); math.Abs(equity-(100+wantPnL)) > 1e-9 {
		t.Fatalf("equity after mark = %.12f, want %.12f", equity, 100+wantPnL)
	}
}

func TestBrokerBaseAssetAccountingRealizesUnderlyingPnLInBTC(t *testing.T) {
	ref := SecurityRef{Market: "crypto-underlying", Symbol: "BTC", Interval: "5m", Index: 0}
	broker := NewBroker(Config{InitialCapital: 100, AccountUnit: "BTC"})
	closePrice := 100.0
	broker.SetPriceFunc(func(_ SecurityRef) BarPrices {
		return BarPrices{Open: closePrice, High: closePrice, Low: closePrice, Close: closePrice}
	})

	if _, ok := broker.ExecuteOrderAtCloseNow(Order{Security: ref, Side: Buy, Type: MarketOrder, Qty: 1}, 0, time.Unix(0, 0)); !ok {
		t.Fatal("expected entry fill")
	}
	closePrice = 110
	if _, ok := broker.ExecuteOrderAtCloseNow(Order{Security: ref, Side: Sell, Type: MarketOrder, Qty: 1}, 1, time.Unix(3600, 0)); !ok {
		t.Fatal("expected exit fill")
	}

	wantPnL := (110.0 - 100.0) / 110.0
	if cash := broker.Cash(); math.Abs(cash-(100+wantPnL)) > 1e-9 {
		t.Fatalf("cash after close = %.12f, want %.12f", cash, 100+wantPnL)
	}
	if qty := broker.Positions().Get(ref).Qty; qty != 0 {
		t.Fatalf("position qty = %.12f, want 0", qty)
	}
	if equity := broker.Equity(); math.Abs(equity-(100+wantPnL)) > 1e-9 {
		t.Fatalf("equity after close = %.12f, want %.12f", equity, 100+wantPnL)
	}
}
