package cryptooptions

import (
"math"
"sort"
"time"
)

type aggregatorKey struct {
Symbol   string
MinuteTS int64
}

type barAccumulator struct {
FirstTimestamp time.Time
bar            Bar1m
hasFirst       bool
}

// Aggregator collects ticks and produces 1-minute aggregated bars.
// Not safe for concurrent use.
type Aggregator struct {
accumulators map[aggregatorKey]*barAccumulator
}

func NewAggregator() *Aggregator {
return &Aggregator{
accumulators: make(map[aggregatorKey]*barAccumulator),
}
}

func maxf32(a, b float32) float32 {
if a > b {
return a
}
return b
}

func minf32NonZero(current, new float32) float32 {
if current == 0 {
return new
}
if new == 0 {
return current
}
return float32(math.Min(float64(current), float64(new)))
}

func (a *Aggregator) Add(tick TickRow) {
minuteTS := tick.Timestamp.Truncate(time.Minute)
key := aggregatorKey{
Symbol:   tick.Symbol,
MinuteTS: minuteTS.Unix(),
}

acc, exists := a.accumulators[key]
if !exists {
acc = &barAccumulator{}
acc.bar.Timestamp = minuteTS
acc.bar.Symbol = tick.Symbol
acc.bar.SymbolID = SymbolID(tick.Symbol)
acc.bar.BaseAsset = ExtractBaseAsset(tick.Symbol)
acc.bar.OptionType = tick.OptionType
acc.bar.StrikePrice = tick.StrikePrice
acc.bar.Expiration = tick.Expiration
acc.bar.UnderlyingIndex = tick.UnderlyingIndex
a.accumulators[key] = acc
}

if !acc.hasFirst {
acc.FirstTimestamp = tick.Timestamp
acc.hasFirst = true

acc.bar.MarkOpen = tick.MarkPrice
acc.bar.MarkHigh = tick.MarkPrice
acc.bar.MarkLow = tick.MarkPrice

acc.bar.LastOpen = tick.LastPrice
acc.bar.LastHigh = tick.LastPrice
acc.bar.LastLow = tick.LastPrice

acc.bar.BidOpen = tick.BidPrice
acc.bar.AskOpen = tick.AskPrice

acc.bar.MarkIVOpen = tick.MarkIV
acc.bar.BidIVOpen = tick.BidIV
acc.bar.AskIVOpen = tick.AskIV

acc.bar.UnderlyingPriceOpen = tick.UnderlyingPrice

acc.bar.Delta = tick.Delta
acc.bar.Gamma = tick.Gamma
acc.bar.Vega = tick.Vega
acc.bar.Theta = tick.Theta
acc.bar.Rho = tick.Rho
} else {
if tick.Timestamp.Before(acc.FirstTimestamp) {
acc.FirstTimestamp = tick.Timestamp
acc.bar.Delta = tick.Delta
acc.bar.Gamma = tick.Gamma
acc.bar.Vega = tick.Vega
acc.bar.Theta = tick.Theta
acc.bar.Rho = tick.Rho

acc.bar.MarkOpen = tick.MarkPrice
acc.bar.LastOpen = tick.LastPrice
acc.bar.BidOpen = tick.BidPrice
acc.bar.AskOpen = tick.AskPrice
acc.bar.MarkIVOpen = tick.MarkIV
acc.bar.BidIVOpen = tick.BidIV
acc.bar.AskIVOpen = tick.AskIV
acc.bar.UnderlyingPriceOpen = tick.UnderlyingPrice
}

acc.bar.MarkHigh = maxf32(acc.bar.MarkHigh, tick.MarkPrice)
acc.bar.MarkLow = minf32NonZero(acc.bar.MarkLow, tick.MarkPrice)

acc.bar.LastHigh = maxf32(acc.bar.LastHigh, tick.LastPrice)
acc.bar.LastLow = minf32NonZero(acc.bar.LastLow, tick.LastPrice)
}

acc.bar.MarkClose = tick.MarkPrice
acc.bar.LastClose = tick.LastPrice
acc.bar.BidClose = tick.BidPrice
acc.bar.AskClose = tick.AskPrice
acc.bar.MarkIVClose = tick.MarkIV
acc.bar.UnderlyingPriceClose = tick.UnderlyingPrice
acc.bar.OpenInterest = tick.OpenInterest

acc.bar.TickCount++
}

// Flush returns all accumulated bars sorted by (base_asset, symbol, timestamp)
// and resets the aggregator.
func (a *Aggregator) Flush() []Bar1m {
bars := make([]Bar1m, 0, len(a.accumulators))
for _, acc := range a.accumulators {
bars = append(bars, acc.bar)
}
a.accumulators = make(map[aggregatorKey]*barAccumulator)

sort.Slice(bars, func(i, j int) bool {
if bars[i].BaseAsset != bars[j].BaseAsset {
return bars[i].BaseAsset < bars[j].BaseAsset
}
if bars[i].Symbol != bars[j].Symbol {
return bars[i].Symbol < bars[j].Symbol
}
return bars[i].Timestamp.Before(bars[j].Timestamp)
})

return bars
}

func (a *Aggregator) Count() int {
return len(a.accumulators)
}
