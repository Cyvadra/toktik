package backtest

import (
	"math"
	"strconv"
	"strings"
	"time"
)

func buildSpreadPositionReports(tracker *SpreadTracker, endTime time.Time) []SpreadPositionReport {
	if tracker == nil || len(tracker.All()) == 0 {
		return nil
	}
	reports := make([]SpreadPositionReport, 0, len(tracker.All()))
	for _, spread := range tracker.All() {
		report := SpreadPositionReport{
			ID:          spread.ID,
			Tag:         spread.Tag,
			CloseNote:   spreadCloseNote(spread),
			Status:      "open",
			OpenTime:    spread.OpenTime,
			NetPremium:  spreadNetPremium(spread),
			RealizedPnL: spread.TotalRealizedPnL(),
			GroupID:     spread.GroupID,
			Legs:        make([]SpreadLegReport, 0, len(spread.Legs)),
		}
		closeTriggerTime := latestSpreadCloseTriggerTime(spread)
		if closeTriggerTime != nil {
			report.CloseTriggerTime = closeTriggerTime
		}
		closeTime := latestSpreadCloseTime(spread)
		if closeTime != nil {
			report.CloseTime = closeTime
		}
		if spread.IsFullyClosed() {
			report.Status = "closed"
		}
		referenceEnd := endTime
		if closeTime != nil {
			referenceEnd = *closeTime
		}
		report.DaysHeld = referenceEnd.Sub(spread.OpenTime).Hours() / 24

		for _, leg := range spread.Legs {
			legReport := SpreadLegReport{
				Symbol:      leg.Contract.Symbol,
				Side:        leg.Side.String(),
				Type:        leg.Contract.Type,
				StrikePrice: leg.Contract.StrikePrice,
				Expiration:  leg.Contract.Expiration,
				EntryDelta:  tradeCustomDataFloat(leg.EntryCustomData, TradeCustomDataKeyEntryDelta),
				Delta:       leg.Contract.Delta,
				Qty:         leg.Qty,
				EntryPrice:  leg.EntryPrice,
				EntryTime:   leg.EntryTime,
				Closed:      leg.Closed,
				CloseReason: leg.CloseReason,
				RealizedPnL: leg.RealizedPnL(),
			}
			if leg.Closed {
				closeAt := leg.CloseTime
				legReport.ClosePrice = leg.ClosePrice
				legReport.CloseTriggerTime = tradeCustomDataTime(leg.CloseCustomData, TradeCustomDataKeyCloseTriggerTime)
				legReport.CloseTime = &closeAt
				legReport.CloseDelta = tradeCustomDataFloat(leg.CloseCustomData, TradeCustomDataKeyCloseDelta)
			}
			report.Legs = append(report.Legs, legReport)
		}
		reports = append(reports, report)
	}
	return reports
}

func latestSpreadCloseTime(spread *SpreadPosition) *time.Time {
	if spread == nil {
		return nil
	}
	var latest time.Time
	closed := false
	for _, leg := range spread.Legs {
		if leg.Closed && (!closed || leg.CloseTime.After(latest)) {
			latest = leg.CloseTime
			closed = true
		}
	}
	if !closed {
		return nil
	}
	return &latest
}

func latestSpreadCloseTriggerTime(spread *SpreadPosition) *time.Time {
	if spread == nil {
		return nil
	}
	var latest time.Time
	found := false
	for _, leg := range spread.Legs {
		triggerTime := tradeCustomDataTime(leg.CloseCustomData, TradeCustomDataKeyCloseTriggerTime)
		if triggerTime == nil {
			continue
		}
		if !found || triggerTime.After(latest) {
			latest = *triggerTime
			found = true
		}
	}
	if !found {
		return nil
	}
	return &latest
}

func tradeCustomDataFloat(items []TradeCustomData, key string) *float64 {
	for _, item := range items {
		if item.Key != key {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(item.Value), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil
		}
		return &value
	}
	return nil
}

func tradeCustomDataTime(items []TradeCustomData, key string) *time.Time {
	for _, item := range items {
		if item.Key != key {
			continue
		}
		value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.Value))
		if err != nil {
			return nil
		}
		return &value
	}
	return nil
}

func spreadCloseNote(spread *SpreadPosition) string {
	if spread == nil {
		return ""
	}
	seen := make(map[string]struct{}, len(spread.Legs))
	notes := make([]string, 0, len(spread.Legs))
	for _, leg := range spread.Legs {
		note := strings.TrimSpace(leg.CloseReason)
		if note == "" {
			continue
		}
		if _, ok := seen[note]; ok {
			continue
		}
		seen[note] = struct{}{}
		notes = append(notes, note)
	}
	return strings.Join(notes, " | ")
}

func spreadNetPremium(spread *SpreadPosition) float64 {
	if spread == nil {
		return 0
	}
	total := 0.0
	for _, leg := range spread.Legs {
		amount := leg.Qty * leg.EntryPrice
		if leg.Side == Sell {
			total += amount
		} else {
			total -= amount
		}
	}
	return total
}

// computeSpreadSummary aggregates metrics from the spread tracker.
func computeSpreadSummary(tracker *SpreadTracker) *SpreadSummary {
	if tracker == nil || len(tracker.All()) == 0 {
		return nil
	}

	s := &SpreadSummary{
		TotalSpreads: len(tracker.All()),
	}

	for _, sp := range tracker.All() {
		if sp.IsFullyClosed() {
			s.ClosedSpreads++
			pnl := sp.TotalRealizedPnL()
			s.TotalPnL += pnl
			if pnl > 0 {
				s.WinningSpreads++
			} else if pnl < 0 {
				s.LosingSpreads++
			}
		} else {
			s.OpenSpreads++
		}
	}

	if s.ClosedSpreads > 0 {
		s.WinRate = float64(s.WinningSpreads) / float64(s.ClosedSpreads)
	}

	return s
}

// buildSpreadGroupReports creates report snapshots for spread groups.
func buildSpreadGroupReports(groupTracker *SpreadGroupTracker, spreadTracker *SpreadTracker, endTime time.Time) []SpreadGroupReport {
	if groupTracker == nil || len(groupTracker.All()) == 0 {
		return nil
	}
	reports := make([]SpreadGroupReport, 0, len(groupTracker.All()))
	for _, group := range groupTracker.All() {
		if len(group.SpreadIDs) == 0 {
			continue
		}

		report := SpreadGroupReport{
			ID:          group.ID,
			Tag:         group.Tag,
			SpreadIDs:   append([]int(nil), group.SpreadIDs...),
			InitAmount:  group.InitAmount,
			DecayFactor: group.DecayFactor,
			RollCount:   group.RollCount,
			Status:      "open",
			OpenTime:    group.OpenTime,
		}
		if group.Closed {
			report.Status = "closed"
		}

		// Sum realized PnL from all spreads in the group
		var latestClose time.Time
		allClosed := true
		for _, spreadID := range group.SpreadIDs {
			sp := spreadTracker.Get(spreadID)
			if sp == nil {
				continue
			}
			report.TotalPnL += sp.TotalRealizedPnL()
			if !sp.IsFullyClosed() {
				allClosed = false
			}
			for _, leg := range sp.Legs {
				if leg.Closed && leg.CloseTime.After(latestClose) {
					latestClose = leg.CloseTime
				}
			}
		}
		if allClosed && !latestClose.IsZero() {
			report.CloseTime = &latestClose
			report.Status = "closed"
		}
		reports = append(reports, report)
	}
	return reports
}
