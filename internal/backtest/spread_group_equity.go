package backtest

import (
	"math"
	"time"
)

type spreadGroupEquitySnapshot struct {
	HighestEquity float64
	LowestEquity  float64
	MaxDrawdown   float64
}

type spreadGroupEquityState struct {
	initialized bool
	peak        float64
	highest     float64
	lowest      float64
	maxDrawdown float64
}

type spreadGroupEquityAccumulator struct {
	states map[int]*spreadGroupEquityState
}

func newSpreadGroupEquityAccumulator() *spreadGroupEquityAccumulator {
	return &spreadGroupEquityAccumulator{states: make(map[int]*spreadGroupEquityState)}
}

func (a *spreadGroupEquityAccumulator) Observe(groupTracker *SpreadGroupTracker, spreadTracker *SpreadTracker, contractMap map[string]OptionContract, pricing SpreadPricingConfig, now time.Time) {
	if a == nil || groupTracker == nil || spreadTracker == nil {
		return
	}
	for _, group := range groupTracker.All() {
		if group == nil || len(group.SpreadIDs) == 0 || now.Before(group.OpenTime) {
			continue
		}
		equity := group.InitAmount + spreadGroupPnL(group, spreadTracker, contractMap, pricing)
		a.stateFor(group.ID).observe(group.InitAmount, equity)
	}
}

func (a *spreadGroupEquityAccumulator) Snapshot() map[int]spreadGroupEquitySnapshot {
	if a == nil || len(a.states) == 0 {
		return nil
	}
	out := make(map[int]spreadGroupEquitySnapshot, len(a.states))
	for id, state := range a.states {
		if state == nil || !state.initialized {
			continue
		}
		out[id] = spreadGroupEquitySnapshot{
			HighestEquity: state.highest,
			LowestEquity:  state.lowest,
			MaxDrawdown:   state.maxDrawdown,
		}
	}
	return out
}

func (a *spreadGroupEquityAccumulator) stateFor(groupID int) *spreadGroupEquityState {
	if state, ok := a.states[groupID]; ok {
		return state
	}
	state := &spreadGroupEquityState{}
	a.states[groupID] = state
	return state
}

func (s *spreadGroupEquityState) observe(initialEquity, equity float64) {
	if !s.initialized {
		baseline := initialEquity
		if baseline == 0 || math.IsNaN(baseline) || math.IsInf(baseline, 0) {
			baseline = equity
		}
		s.initialized = true
		s.peak = baseline
		s.highest = baseline
		s.lowest = baseline
	}
	if equity > s.highest {
		s.highest = equity
	}
	if equity < s.lowest {
		s.lowest = equity
	}
	if equity > s.peak {
		s.peak = equity
	}
	if s.peak > 0 {
		drawdown := (s.peak - equity) / s.peak
		if drawdown > s.maxDrawdown {
			s.maxDrawdown = drawdown
		}
	}
}

func spreadGroupPnL(group *SpreadGroup, spreadTracker *SpreadTracker, contractMap map[string]OptionContract, pricing SpreadPricingConfig) float64 {
	if group == nil || spreadTracker == nil {
		return 0
	}
	total := 0.0
	for _, spreadID := range group.SpreadIDs {
		spread := spreadTracker.Get(spreadID)
		if spread == nil {
			continue
		}
		total += spread.TotalRealizedPnL()
		for _, leg := range spread.Legs {
			if leg.Closed {
				continue
			}
			contract := leg.Contract
			if updated, ok := contractMap[contract.Symbol]; ok {
				contract = updated
			}
			markPrice := pricing.ValuationMode.ExitPrice(leg.Side, contract)
			if !optionPriceValid(markPrice) {
				continue
			}
			total += leg.UnrealizedPnL(markPrice)
		}
	}
	return total
}

func applySpreadGroupEquityStats(reports []SpreadGroupReport, snapshots map[int]spreadGroupEquitySnapshot) []SpreadGroupReport {
	if len(reports) == 0 || len(snapshots) == 0 {
		return reports
	}
	for i := range reports {
		snapshot, ok := snapshots[reports[i].ID]
		if !ok {
			continue
		}
		reports[i].HighestEquity = snapshot.HighestEquity
		reports[i].LowestEquity = snapshot.LowestEquity
		reports[i].MaxDrawdown = snapshot.MaxDrawdown
	}
	return reports
}
