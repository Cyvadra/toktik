package backtest

import (
	"math"
	"time"
)

// ExecutionPriceModel controls which bar price is used for market-style fills.
type ExecutionPriceModel int

const (
	ExecutionPriceCanonical ExecutionPriceModel = iota
	ExecutionPriceBidAsk
)

// ValuationPriceModel controls how open positions are marked to market.
type ValuationPriceModel int

const (
	ValuationPriceClose ValuationPriceModel = iota
	ValuationPriceMid
	ValuationPriceExit
)

// TriggerPriceMode controls which prices are used to trigger stop/limit orders.
type TriggerPriceMode int

const (
	TriggerPriceCanonical      TriggerPriceMode = iota
	TriggerPriceBidAskEnvelope                  // uses bid/ask open-close envelope when bid/ask high-low is unavailable
)

// BarPrices holds all bar-level price snapshots the broker may need.
type BarPrices struct {
	Open     float64
	High     float64
	Low      float64
	Close    float64
	BidOpen  float64
	BidClose float64
	AskOpen  float64
	AskClose float64
}

// CommissionModel defines how trading commissions are calculated.
type CommissionModel int

const (
	CommissionNone    CommissionModel = iota
	CommissionFlat                    // fixed amount per trade
	CommissionPercent                 // percentage of trade value
	CommissionPerUnit                 // amount per unit traded
)

// Broker simulates order execution with realistic fills.
type Broker struct {
	config    Config
	cash      float64
	positions *PositionTracker
	pending   []Order
	trades    []Trade
	nextOID   int
	nextTID   int

	// priceFunc resolves current bar prices for a security.
	priceFunc func(SecurityRef) BarPrices
}

// NewBroker creates a broker with the given config.
func NewBroker(cfg Config) *Broker {
	return &Broker{
		config:    cfg,
		cash:      cfg.InitialCapital,
		positions: NewPositionTracker(),
		nextOID:   1,
		nextTID:   1,
	}
}

// SetPriceFunc sets the function used to resolve OHLC prices per security.
func (b *Broker) SetPriceFunc(fn func(SecurityRef) BarPrices) {
	b.priceFunc = fn
}

// SubmitOrder queues an order for processing on the next bar.
func (b *Broker) SubmitOrder(o Order) int {
	o.ID = b.nextOID
	b.nextOID++
	b.pending = append(b.pending, o)
	return o.ID
}

// CancelOrders removes all pending orders for a security.
func (b *Broker) CancelOrders(ref SecurityRef) {
	filtered := b.pending[:0]
	for _, o := range b.pending {
		if o.Security != ref {
			filtered = append(filtered, o)
		}
	}
	b.pending = filtered
}

// ProcessPending attempts to fill pending orders against current bar prices.
// Call this at the start of each bar, before OnBar.
func (b *Broker) ProcessPending(barIndex int, barTime time.Time) []Trade {
	if b.priceFunc == nil || len(b.pending) == 0 {
		return nil
	}

	var fills []Trade
	var remaining []Order

	for _, o := range b.pending {
		prices := b.priceFunc(o.Security)
		open := prices.executionOpen(o.Side, b.config.ExecutionMode)

		if !isValidPrice(open) {
			remaining = append(remaining, o)
			continue
		}

		var fillPrice float64
		fillQty := o.Qty
		filled := false

		switch o.Type {
		case MarketOrder:
			fillPrice = b.applySlippage(open, o.Side)
			filled = true

		case TWAPMarketOrder:
			slicesLeft := o.TWAPBars
			if slicesLeft <= 0 {
				slicesLeft = 1
			}
			fillQty = o.Qty / float64(slicesLeft)
			fillPrice = b.applySlippage(open, o.Side)
			filled = true

		case LimitOrder:
			low, high := prices.triggerRange(o.Side, b.config.TriggerMode)
			if o.Side == Buy && isValidPrice(low) && low <= o.Price {
				fillPrice = minFloat(open, o.Price)
				filled = true
			} else if o.Side == Sell && isValidPrice(high) && high >= o.Price {
				fillPrice = maxFloat(open, o.Price)
				filled = true
			}

		case StopOrder:
			_, high := prices.triggerRange(o.Side, b.config.TriggerMode)
			low, _ := prices.triggerRange(o.Side, b.config.TriggerMode)
			if o.Side == Buy && isValidPrice(high) && high >= o.StopPrice {
				fillPrice = b.applySlippage(maxFloat(open, o.StopPrice), o.Side)
				filled = true
			} else if o.Side == Sell && isValidPrice(low) && low <= o.StopPrice {
				fillPrice = b.applySlippage(minFloat(open, o.StopPrice), o.Side)
				filled = true
			}

		case StopLimitOrder:
			low, high := prices.triggerRange(o.Side, b.config.TriggerMode)
			triggered := false
			if o.Side == Buy && isValidPrice(high) && high >= o.StopPrice {
				triggered = true
			} else if o.Side == Sell && isValidPrice(low) && low <= o.StopPrice {
				triggered = true
			}
			if triggered {
				if o.Side == Buy && isValidPrice(low) && low <= o.Price {
					fillPrice = minFloat(open, o.Price)
					filled = true
				} else if o.Side == Sell && isValidPrice(high) && high >= o.Price {
					fillPrice = maxFloat(open, o.Price)
					filled = true
				}
			}
		}

		if filled {
			commission := b.calcCommission(fillQty, fillPrice)
			trade := Trade{
				ID:         b.nextTID,
				OrderID:    o.ID,
				Security:   o.Security,
				Side:       o.Side,
				Qty:        fillQty,
				FillPrice:  fillPrice,
				Commission: commission,
				Slippage:   abs(fillPrice - open),
				BarIndex:   barIndex,
				Timestamp:  barTime,
			}
			b.nextTID++

			b.cash += trade.NetAmount()
			b.positions.Update(trade)
			fills = append(fills, trade)

			if o.Type == TWAPMarketOrder {
				remainingQty := o.Qty - fillQty
				remainingBars := o.TWAPBars - 1
				if remainingQty > 0 && remainingBars > 0 {
					o.Qty = remainingQty
					o.TWAPBars = remainingBars
					remaining = append(remaining, o)
				}
			}
		} else {
			remaining = append(remaining, o)
		}
	}

	b.pending = remaining
	b.trades = append(b.trades, fills...)
	return fills
}

// Equity returns total account value (cash + unrealized PnL).
func (b *Broker) Equity() float64 {
	return b.cash + b.positionMarketValue()
}

// Cash returns current cash balance.
func (b *Broker) Cash() float64 {
	return b.cash
}

// AdjustCash modifies the broker's cash balance by the given amount.
// Positive adds cash; negative removes it.
func (b *Broker) AdjustCash(amount float64) {
	b.cash += amount
}

// Positions returns the position tracker.
func (b *Broker) Positions() *PositionTracker {
	return b.positions
}

// Trades returns all filled trades.
func (b *Broker) Trades() []Trade {
	return b.trades
}

func (b *Broker) positionMarketValue() float64 {
	total := 0.0
	for _, pos := range b.positions.All() {
		if b.priceFunc != nil {
			total += pos.Qty * b.markPriceForPosition(pos.Qty, b.priceFunc(pos.Security))
		}
	}
	return total
}

func (b *Broker) markPriceForPosition(qty float64, prices BarPrices) float64 {
	switch b.config.ValuationMode {
	case ValuationPriceExit:
		if qty >= 0 {
			return fallbackPrice(prices.BidClose, prices.Close)
		}
		return fallbackPrice(prices.AskClose, prices.Close)
	case ValuationPriceMid:
		if isValidPrice(prices.BidClose) && isValidPrice(prices.AskClose) {
			return (prices.BidClose + prices.AskClose) / 2
		}
		return prices.Close
	default:
		return prices.Close
	}
}

func (b *Broker) calcCommission(qty, price float64) float64 {
	switch b.config.CommissionModel {
	case CommissionFlat:
		return b.config.CommissionValue
	case CommissionPercent:
		return qty * price * b.config.CommissionValue
	case CommissionPerUnit:
		return qty * b.config.CommissionValue
	default:
		return 0
	}
}

func (b *Broker) applySlippage(price float64, side Side) float64 {
	slip := price * b.config.SlippagePct
	if side == Buy {
		return price + slip
	}
	return price - slip
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (bp BarPrices) executionOpen(side Side, mode ExecutionPriceModel) float64 {
	if mode == ExecutionPriceBidAsk {
		if side == Buy {
			return fallbackPrice(bp.AskOpen, bp.Open)
		}
		return fallbackPrice(bp.BidOpen, bp.Open)
	}
	return bp.Open
}

func (bp BarPrices) triggerRange(side Side, mode TriggerPriceMode) (float64, float64) {
	if mode == TriggerPriceBidAskEnvelope {
		if side == Buy {
			return minAvailable(bp.AskOpen, bp.AskClose, bp.Open, bp.Close), maxAvailable(bp.AskOpen, bp.AskClose, bp.Open, bp.Close)
		}
		return minAvailable(bp.BidOpen, bp.BidClose, bp.Open, bp.Close), maxAvailable(bp.BidOpen, bp.BidClose, bp.Open, bp.Close)
	}
	return bp.Low, bp.High
}

func fallbackPrice(primary, secondary float64) float64 {
	if isValidPrice(primary) {
		return primary
	}
	return secondary
}

func minAvailable(values ...float64) float64 {
	best := math.NaN()
	for _, value := range values {
		if !isValidPrice(value) {
			continue
		}
		if math.IsNaN(best) || value < best {
			best = value
		}
	}
	return best
}

func maxAvailable(values ...float64) float64 {
	best := math.NaN()
	for _, value := range values {
		if !isValidPrice(value) {
			continue
		}
		if math.IsNaN(best) || value > best {
			best = value
		}
	}
	return best
}

func isValidPrice(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
