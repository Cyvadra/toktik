package backtest

import (
	"testing"
	"time"
)

func TestOrderBuilder(t *testing.T) {
	broker := NewBroker(Config{InitialCapital: 10000})
	ctx := &BarContext{
		barIndex: 5,
		barTime:  time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		broker:   broker,
	}
	ref := SecurityRef{Market: "test", Symbol: "TEST", Index: 0}

	t.Run("market buy order", func(t *testing.T) {
		orderID := ctx.Order(ref).
			Buy().
			Qty(100).
			Note("test buy").
			Submit()

		if orderID == 0 {
			t.Fatal("expected non-zero order ID")
		}

		pending := broker.pending
		if len(pending) != 1 {
			t.Fatalf("expected 1 pending order, got %d", len(pending))
		}

		order := pending[0]
		if order.Side != Buy {
			t.Errorf("expected Buy, got %v", order.Side)
		}
		if order.Qty != 100 {
			t.Errorf("expected qty 100, got %v", order.Qty)
		}
		if order.Note != "test buy" {
			t.Errorf("expected note 'test buy', got %q", order.Note)
		}
		if order.Type != MarketOrder {
			t.Errorf("expected MarketOrder, got %v", order.Type)
		}
	})

	// Clear pending for next test
	broker.pending = nil

	t.Run("limit sell order", func(t *testing.T) {
		orderID := ctx.Order(ref).
			Sell().
			Qty(50).
			Limit(150.0).
			Submit()

		if orderID == 0 {
			t.Fatal("expected non-zero order ID")
		}

		pending := broker.pending
		if len(pending) != 1 {
			t.Fatalf("expected 1 pending order, got %d", len(pending))
		}

		order := pending[0]
		if order.Side != Sell {
			t.Errorf("expected Sell, got %v", order.Side)
		}
		if order.Type != LimitOrder {
			t.Errorf("expected LimitOrder, got %v", order.Type)
		}
		if order.Price != 150.0 {
			t.Errorf("expected price 150.0, got %v", order.Price)
		}
	})

	broker.pending = nil

	t.Run("TWAP order", func(t *testing.T) {
		ctx.Order(ref).
			Buy().
			Qty(1000).
			TWAP(5).
			Submit()

		pending := broker.pending
		if len(pending) != 1 {
			t.Fatalf("expected 1 pending order, got %d", len(pending))
		}

		order := pending[0]
		if order.Type != TWAPMarketOrder {
			t.Errorf("expected TWAPMarketOrder, got %v", order.Type)
		}
		if order.TWAPBars != 5 {
			t.Errorf("expected TWAPBars 5, got %v", order.TWAPBars)
		}
	})

	broker.pending = nil

	t.Run("stop-limit order", func(t *testing.T) {
		ctx.Order(ref).
			Buy().
			Qty(100).
			StopLimit(105.0, 106.0).
			Submit()

		pending := broker.pending
		if len(pending) != 1 {
			t.Fatalf("expected 1 pending order, got %d", len(pending))
		}

		order := pending[0]
		if order.Type != StopLimitOrder {
			t.Errorf("expected StopLimitOrder, got %v", order.Type)
		}
		if order.StopPrice != 105.0 {
			t.Errorf("expected stop price 105.0, got %v", order.StopPrice)
		}
		if order.Price != 106.0 {
			t.Errorf("expected limit price 106.0, got %v", order.Price)
		}
	})
}

func TestSpreadOrderBuilder(t *testing.T) {
	var scheduledActions []ScheduledAction
	ctx := &BarContext{
		barIndex:         5,
		barTime:          time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		scheduledActions: &scheduledActions,
	}

	t.Run("schedule close spread", func(t *testing.T) {
		triggerTime := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
		ctx.ScheduleSpread().
			At(triggerTime).
			CloseSpread(42).
			Reason("expiry").
			Submit()

		if len(scheduledActions) != 1 {
			t.Fatalf("expected 1 scheduled action, got %d", len(scheduledActions))
		}

		action := scheduledActions[0]
		if action.ActionType != ScheduleCloseSpread {
			t.Errorf("expected ScheduleCloseSpread, got %v", action.ActionType)
		}
		if action.SpreadID != 42 {
			t.Errorf("expected spread ID 42, got %d", action.SpreadID)
		}
		if action.CloseReason != "expiry" {
			t.Errorf("expected reason 'expiry', got %q", action.CloseReason)
		}
	})

	scheduledActions = scheduledActions[:0]

	t.Run("schedule close leg with stop", func(t *testing.T) {
		triggerTime := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
		ctx.ScheduleSpread().
			At(triggerTime).
			CloseLeg(42, 0).
			StopTrigger(Sell, 100.0).
			Slippage(0.002).
			Submit()

		if len(scheduledActions) != 1 {
			t.Fatalf("expected 1 scheduled action, got %d", len(scheduledActions))
		}

		action := scheduledActions[0]
		if action.ActionType != ScheduleCloseLeg {
			t.Errorf("expected ScheduleCloseLeg, got %v", action.ActionType)
		}
		if action.OrderType != SpreadOrderStop {
			t.Errorf("expected SpreadOrderStop, got %v", action.OrderType)
		}
		if action.TriggerPrice != 100.0 {
			t.Errorf("expected trigger price 100.0, got %v", action.TriggerPrice)
		}
		if action.SlippagePct != 0.002 {
			t.Errorf("expected slippage 0.002, got %v", action.SlippagePct)
		}
	})
}
