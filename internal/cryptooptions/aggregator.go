package cryptooptions

import (
	"math"
	"sort"
	"strings"
	"time"
)

type aggregatorKey struct {
	Symbol   string
	MinuteTS int64
}

type barAccumulator struct {
	FirstTimestamp time.Time
	LastTimestamp  time.Time
	bar            Bar1m
	hasFirst       bool
}

type spotAccumulator struct {
	FirstTimestamp time.Time
	LastTimestamp  time.Time
	bar            SpotBar1m
	hasFirst       bool
	allPrices      []float32
	firstPrices    []float32
	lastPrices     []float32
	openWindow     []float32
	closeWindow    []float32
}

// Aggregator collects ticks and produces 1-minute aggregated bars.
// Not safe for concurrent use.
type Aggregator struct {
	optionAccumulators map[aggregatorKey]*barAccumulator
	spotAccumulators   map[aggregatorKey]*spotAccumulator
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		optionAccumulators: make(map[aggregatorKey]*barAccumulator),
		spotAccumulators:   make(map[aggregatorKey]*spotAccumulator),
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

func shouldReplaceClose(lastTimestamp, tickTimestamp time.Time) bool {
	return lastTimestamp.IsZero() || !tickTimestamp.Before(lastTimestamp)
}

func percentileSortedF32(sorted []float32, percentile float64) float32 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 1 {
		return sorted[n-1]
	}

	pos := percentile * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}

	w := float32(pos - float64(lo))
	return sorted[lo]*(1-w) + sorted[hi]*w
}

func medianF32(values []float32) float32 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]float32(nil), values...)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i] < cp[j]
	})
	return percentileSortedF32(cp, 0.5)
}

func (s *spotAccumulator) finalize() {
	if len(s.allPrices) == 0 {
		return
	}

	sorted := append([]float32(nil), s.allPrices...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	s.bar.High = percentileSortedF32(sorted, 0.95)
	s.bar.Low = percentileSortedF32(sorted, 0.05)

	if len(s.openWindow) > 0 {
		s.bar.Open = medianF32(s.openWindow)
	} else {
		s.bar.Open = medianF32(s.firstPrices)
	}

	if len(s.closeWindow) > 0 {
		s.bar.Close = medianF32(s.closeWindow)
	} else {
		s.bar.Close = medianF32(s.lastPrices)
	}
}

func (a *Aggregator) Add(tick TickRow) {
	minuteTS := tick.Timestamp.Truncate(time.Minute)
	key := aggregatorKey{
		Symbol:   tick.Symbol,
		MinuteTS: minuteTS.Unix(),
	}

	acc, exists := a.optionAccumulators[key]
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
		a.optionAccumulators[key] = acc
	}

	if !acc.hasFirst {
		acc.FirstTimestamp = tick.Timestamp
		acc.LastTimestamp = tick.Timestamp
		acc.hasFirst = true

		acc.bar.MarkOpen = tick.MarkPrice
		acc.bar.MarkHigh = tick.MarkPrice
		acc.bar.MarkLow = tick.MarkPrice

		acc.bar.LastOpen = tick.LastPrice
		acc.bar.LastHigh = tick.LastPrice
		acc.bar.LastLow = tick.LastPrice

		acc.bar.BidOpen = tick.BidPrice
		acc.bar.BidHigh = tick.BidPrice
		acc.bar.BidLow = tick.BidPrice
		acc.bar.AskOpen = tick.AskPrice
		acc.bar.AskHigh = tick.AskPrice
		acc.bar.AskLow = tick.AskPrice

		acc.bar.MarkIVOpen = tick.MarkIV
		acc.bar.BidIVOpen = tick.BidIV
		acc.bar.AskIVOpen = tick.AskIV

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
		}

		acc.bar.MarkHigh = maxf32(acc.bar.MarkHigh, tick.MarkPrice)
		acc.bar.MarkLow = minf32NonZero(acc.bar.MarkLow, tick.MarkPrice)

		acc.bar.LastHigh = maxf32(acc.bar.LastHigh, tick.LastPrice)
		acc.bar.LastLow = minf32NonZero(acc.bar.LastLow, tick.LastPrice)

		acc.bar.BidHigh = maxf32(acc.bar.BidHigh, tick.BidPrice)
		acc.bar.BidLow = minf32NonZero(acc.bar.BidLow, tick.BidPrice)

		acc.bar.AskHigh = maxf32(acc.bar.AskHigh, tick.AskPrice)
		acc.bar.AskLow = minf32NonZero(acc.bar.AskLow, tick.AskPrice)
	}

	if shouldReplaceClose(acc.LastTimestamp, tick.Timestamp) {
		acc.LastTimestamp = tick.Timestamp
		acc.bar.MarkClose = tick.MarkPrice
		acc.bar.LastClose = tick.LastPrice
		acc.bar.BidClose = tick.BidPrice
		acc.bar.AskClose = tick.AskPrice
		acc.bar.MarkIVClose = tick.MarkIV
		acc.bar.OpenInterest = tick.OpenInterest
	}

	acc.bar.TickCount++

	if tick.UnderlyingPrice <= 0 || tick.Timestamp.UnixNano() <= 0 {
		return
	}

	spotKey := aggregatorKey{
		Symbol:   acc.bar.BaseAsset,
		MinuteTS: minuteTS.Unix(),
	}
	spotAcc, exists := a.spotAccumulators[spotKey]
	if !exists {
		spotAcc = &spotAccumulator{}
		spotAcc.bar.Timestamp = minuteTS
		spotAcc.bar.Symbol = acc.bar.BaseAsset
		spotAcc.bar.PriceSource = strings.TrimSpace(tick.UnderlyingIndex)
		a.spotAccumulators[spotKey] = spotAcc
	}

	underlyingIndex := strings.TrimSpace(tick.UnderlyingIndex)
	if underlyingIndex != "" {
		if spotAcc.bar.PriceSource == "" {
			spotAcc.bar.PriceSource = underlyingIndex
		} else if !strings.EqualFold(spotAcc.bar.PriceSource, underlyingIndex) {
			spotAcc.bar.PriceSource = "mixed"
		}
	}

	spotAcc.allPrices = append(spotAcc.allPrices, tick.UnderlyingPrice)
	minuteEnd := minuteTS.Add(time.Minute)
	openWindowEnd := minuteTS.Add(500 * time.Millisecond)
	closeWindowStart := minuteEnd.Add(-500 * time.Millisecond)
	if !tick.Timestamp.Before(minuteTS) && tick.Timestamp.Before(openWindowEnd) {
		spotAcc.openWindow = append(spotAcc.openWindow, tick.UnderlyingPrice)
	}
	if !tick.Timestamp.Before(closeWindowStart) && tick.Timestamp.Before(minuteEnd) {
		spotAcc.closeWindow = append(spotAcc.closeWindow, tick.UnderlyingPrice)
	}

	if !spotAcc.hasFirst {
		spotAcc.FirstTimestamp = tick.Timestamp
		spotAcc.LastTimestamp = tick.Timestamp
		spotAcc.hasFirst = true
		spotAcc.firstPrices = append(spotAcc.firstPrices, tick.UnderlyingPrice)
		spotAcc.lastPrices = append(spotAcc.lastPrices, tick.UnderlyingPrice)
	} else {
		if tick.Timestamp.Before(spotAcc.FirstTimestamp) {
			spotAcc.FirstTimestamp = tick.Timestamp
			spotAcc.firstPrices = spotAcc.firstPrices[:0]
			spotAcc.firstPrices = append(spotAcc.firstPrices, tick.UnderlyingPrice)
		} else if tick.Timestamp.Equal(spotAcc.FirstTimestamp) {
			spotAcc.firstPrices = append(spotAcc.firstPrices, tick.UnderlyingPrice)
		}

		if tick.Timestamp.After(spotAcc.LastTimestamp) {
			spotAcc.LastTimestamp = tick.Timestamp
			spotAcc.lastPrices = spotAcc.lastPrices[:0]
			spotAcc.lastPrices = append(spotAcc.lastPrices, tick.UnderlyingPrice)
		} else if tick.Timestamp.Equal(spotAcc.LastTimestamp) {
			spotAcc.lastPrices = append(spotAcc.lastPrices, tick.UnderlyingPrice)
		}
	}

	spotAcc.bar.TickCount++
}

// FlushSortedOptionBatches sorts option bars by (base_asset, symbol, timestamp),
// passes them to writeBatch in bounded chunks, and clears the option state.
func (a *Aggregator) FlushSortedOptionBatches(batchSize int, writeBatch func([]Bar1m) error) (int, error) {
	if len(a.optionAccumulators) == 0 {
		a.optionAccumulators = make(map[aggregatorKey]*barAccumulator)
		return 0, nil
	}
	if batchSize < 1 {
		batchSize = len(a.optionAccumulators)
	}

	ordered := make([]*barAccumulator, 0, len(a.optionAccumulators))
	for _, acc := range a.optionAccumulators {
		ordered = append(ordered, acc)
	}
	a.optionAccumulators = make(map[aggregatorKey]*barAccumulator)

	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].bar
		right := ordered[j].bar
		if left.BaseAsset != right.BaseAsset {
			return left.BaseAsset < right.BaseAsset
		}
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		return left.Timestamp.Before(right.Timestamp)
	})

	batch := make([]Bar1m, 0, min(batchSize, len(ordered)))
	total := 0
	for _, acc := range ordered {
		batch = append(batch, acc.bar)
		if len(batch) == batchSize {
			if err := writeBatch(batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := writeBatch(batch); err != nil {
			return total, err
		}
		total += len(batch)
	}

	return total, nil
}

// FlushSortedSpotBatches sorts spot bars by (symbol, timestamp),
// passes them to writeBatch in bounded chunks, and clears the spot state.
func (a *Aggregator) FlushSortedSpotBatches(batchSize int, writeBatch func([]SpotBar1m) error) (int, error) {
	if len(a.spotAccumulators) == 0 {
		a.spotAccumulators = make(map[aggregatorKey]*spotAccumulator)
		return 0, nil
	}
	if batchSize < 1 {
		batchSize = len(a.spotAccumulators)
	}

	ordered := make([]*spotAccumulator, 0, len(a.spotAccumulators))
	for _, acc := range a.spotAccumulators {
		ordered = append(ordered, acc)
	}
	a.spotAccumulators = make(map[aggregatorKey]*spotAccumulator)

	sort.Slice(ordered, func(i, j int) bool {
		left := ordered[i].bar
		right := ordered[j].bar
		if left.Symbol != right.Symbol {
			return left.Symbol < right.Symbol
		}
		return left.Timestamp.Before(right.Timestamp)
	})

	batch := make([]SpotBar1m, 0, min(batchSize, len(ordered)))
	total := 0
	for _, acc := range ordered {
		acc.finalize()
		batch = append(batch, acc.bar)
		if len(batch) == batchSize {
			if err := writeBatch(batch); err != nil {
				return total, err
			}
			total += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := writeBatch(batch); err != nil {
			return total, err
		}
		total += len(batch)
	}

	return total, nil
}

func (a *Aggregator) OptionCount() int {
	return len(a.optionAccumulators)
}

func (a *Aggregator) SpotCount() int {
	return len(a.spotAccumulators)
}
