package backtest

// Position tracks a single security's open position.
type Position struct {
	Security      SecurityRef
	Qty           float64 // positive = long, negative = short
	AvgEntryPrice float64
	RealizedPnL   float64
	costBasis     float64 // total cost for avg price calculation
}

// UnrealizedPnL returns the mark-to-market unrealized P&L at the given price.
func (p *Position) UnrealizedPnL(currentPrice float64) float64 {
	if p.Qty == 0 {
		return 0
	}
	return p.Qty * (currentPrice - p.AvgEntryPrice)
}

// MarketValue returns the absolute market value at the given price.
func (p *Position) MarketValue(currentPrice float64) float64 {
	if p.Qty < 0 {
		return -p.Qty * currentPrice
	}
	return p.Qty * currentPrice
}

// PositionTracker manages positions across multiple securities.
type PositionTracker struct {
	positions map[SecurityRef]*Position
}

// NewPositionTracker creates an empty tracker.
func NewPositionTracker() *PositionTracker {
	return &PositionTracker{
		positions: make(map[SecurityRef]*Position),
	}
}

// Update adjusts position for a trade using average cost method.
func (pt *PositionTracker) Update(trade Trade) {
	pos, ok := pt.positions[trade.Security]
	if !ok {
		pos = &Position{Security: trade.Security}
		pt.positions[trade.Security] = pos
	}

	fillQty := trade.Qty
	if trade.Side == Sell {
		fillQty = -fillQty
	}

	if pos.Qty == 0 {
		// Opening new position
		pos.Qty = fillQty
		pos.AvgEntryPrice = trade.FillPrice
		pos.costBasis = trade.FillPrice * abs(fillQty)
	} else if (pos.Qty > 0 && fillQty > 0) || (pos.Qty < 0 && fillQty < 0) {
		// Adding to existing position
		pos.costBasis += trade.FillPrice * abs(fillQty)
		pos.Qty += fillQty
		pos.AvgEntryPrice = pos.costBasis / abs(pos.Qty)
	} else {
		// Reducing or reversing position
		closeQty := abs(fillQty)
		if closeQty > abs(pos.Qty) {
			closeQty = abs(pos.Qty)
		}

		// Realized PnL from the closed portion
		if pos.Qty > 0 {
			pos.RealizedPnL += closeQty * (trade.FillPrice - pos.AvgEntryPrice)
		} else {
			pos.RealizedPnL += closeQty * (pos.AvgEntryPrice - trade.FillPrice)
		}

		remainingOld := abs(pos.Qty) - closeQty
		newQty := abs(fillQty) - closeQty // excess that opens opposite side

		if remainingOld > 0 {
			// Partial close, keep same avg entry
			if pos.Qty > 0 {
				pos.Qty = remainingOld
			} else {
				pos.Qty = -remainingOld
			}
			pos.costBasis = pos.AvgEntryPrice * remainingOld
		} else if newQty > 0 {
			// Reversal: closed old position, opening opposite
			if fillQty > 0 {
				pos.Qty = newQty
			} else {
				pos.Qty = -newQty
			}
			pos.AvgEntryPrice = trade.FillPrice
			pos.costBasis = trade.FillPrice * newQty
		} else {
			// Exact close
			pos.Qty = 0
			pos.AvgEntryPrice = 0
			pos.costBasis = 0
		}
	}
}

// Get returns the position for a security (never nil).
func (pt *PositionTracker) Get(ref SecurityRef) *Position {
	if pos, ok := pt.positions[ref]; ok {
		return pos
	}
	return &Position{Security: ref}
}

// All returns all non-zero positions.
func (pt *PositionTracker) All() []*Position {
	var result []*Position
	for _, pos := range pt.positions {
		if pos.Qty != 0 {
			result = append(result, pos)
		}
	}
	return result
}

// TotalUnrealizedPnL returns the sum of unrealized PnL across all positions.
func (pt *PositionTracker) TotalUnrealizedPnL(priceFunc func(SecurityRef) float64) float64 {
	total := 0.0
	for ref, pos := range pt.positions {
		if pos.Qty != 0 {
			total += pos.UnrealizedPnL(priceFunc(ref))
		}
	}
	return total
}

// TotalRealizedPnL returns the sum of realized PnL across all positions.
func (pt *PositionTracker) TotalRealizedPnL() float64 {
	total := 0.0
	for _, pos := range pt.positions {
		total += pos.RealizedPnL
	}
	return total
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
