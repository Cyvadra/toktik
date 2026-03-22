package backtest

import (
	"context"
	"testing"
	"time"
)

type scheduledSpreadStrategy struct {
	opened bool
}

func (s *scheduledSpreadStrategy) Name() string { return "scheduled-spread" }

func (s *scheduledSpreadStrategy) Init(_ *SetupContext) error { return nil }

func (s *scheduledSpreadStrategy) OnBar(ctx *BarContext) {
	if s.opened || ctx.BarIndex() != 0 {
		return
	}
	s.opened = true
	contract := OptionContract{
		Symbol:    "OPT-TEST",
		Type:      Call,
		MarkPrice: 10,
		AskPrice:  10,
		BidPrice:  10,
	}
	ctx.ScheduleOpenSpreadWithRef(ctx.Time().Add(time.Nanosecond), []SpreadLeg{{
		Contract:   contract,
		Side:       Buy,
		Qty:        1,
		EntryPrice: 10,
	}}, "scheduled-open", "ref-1")
}

func (s *scheduledSpreadStrategy) SpreadPricingConfig() SpreadPricingConfig {
	return SpreadPricingConfig{
		EntryMode:     OptionPriceMarkClose,
		ExitMode:      OptionPriceMarkClose,
		ValuationMode: OptionPriceMarkClose,
	}
}

func TestScheduleOpenSpreadWithRefExecutesOnNextBar(t *testing.T) {
	engine := NewEngine(Config{InitialCapital: 1000})
	engine.RegisterDataFeed("test", &stubDataFeed{fields: []string{"open", "high", "low", "close", "volume"}})

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	result, err := engine.Run(context.Background(), "test", "TEST", "1h", from, to, &scheduledSpreadStrategy{}, nil)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(result.SpreadPositions) != 1 {
		t.Fatalf("expected 1 spread position, got %d", len(result.SpreadPositions))
	}
	spread := result.SpreadPositions[0]
	wantOpen := from.Add(time.Hour)
	if !spread.OpenTime.Equal(wantOpen) {
		t.Fatalf("spread open time = %v, want %v", spread.OpenTime, wantOpen)
	}
	if len(spread.Legs) != 1 {
		t.Fatalf("expected 1 spread leg, got %d", len(spread.Legs))
	}
	if !spread.Legs[0].EntryTime.Equal(wantOpen) {
		t.Fatalf("spread leg entry time = %v, want %v", spread.Legs[0].EntryTime, wantOpen)
	}
}
