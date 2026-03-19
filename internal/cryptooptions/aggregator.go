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

func isCanonicalUnderlyingIndex(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "index_price")
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

	if tick.UnderlyingPrice <= 0 || !isCanonicalUnderlyingIndex(tick.UnderlyingIndex) {
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
		spotAcc.bar.PriceSource = tick.UnderlyingIndex
		a.spotAccumulators[spotKey] = spotAcc
	}

	if !spotAcc.hasFirst {
		spotAcc.FirstTimestamp = tick.Timestamp
		spotAcc.LastTimestamp = tick.Timestamp
		spotAcc.hasFirst = true
		spotAcc.bar.Open = tick.UnderlyingPrice
		spotAcc.bar.High = tick.UnderlyingPrice
		spotAcc.bar.Low = tick.UnderlyingPrice
		spotAcc.bar.Close = tick.UnderlyingPrice
		if spotAcc.bar.PriceSource == "" {
			spotAcc.bar.PriceSource = tick.UnderlyingIndex
		}
	} else {
		if tick.Timestamp.Before(spotAcc.FirstTimestamp) {
			spotAcc.FirstTimestamp = tick.Timestamp
			spotAcc.bar.Open = tick.UnderlyingPrice
			if tick.UnderlyingIndex != "" {
				spotAcc.bar.PriceSource = tick.UnderlyingIndex
			}
		}

		spotAcc.bar.High = maxf32(spotAcc.bar.High, tick.UnderlyingPrice)
		spotAcc.bar.Low = minf32NonZero(spotAcc.bar.Low, tick.UnderlyingPrice)
		if spotAcc.bar.PriceSource == "" && tick.UnderlyingIndex != "" {
			spotAcc.bar.PriceSource = tick.UnderlyingIndex
		}
	}

	if shouldReplaceClose(spotAcc.LastTimestamp, tick.Timestamp) {
		spotAcc.LastTimestamp = tick.Timestamp
		spotAcc.bar.Close = tick.UnderlyingPrice
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
