package backtest

import (
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
