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
	bar          SpotBar1m
	observations []spotObservation
}

type spotObservation struct {
	timestamp       time.Time
	underlyingIndex string
	price           float32
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
	selectedSources := nearestUnderlyingIndices(s.observations, s.bar.Timestamp, 3)
	if len(selectedSources) == 0 {
		return
	}
	selectedSet := make(map[string]struct{}, len(selectedSources))
	for _, source := range selectedSources {
		selectedSet[source] = struct{}{}
	}

	allPrices := make([]float32, 0, len(s.observations))
	openWindow := make([]float32, 0, len(s.observations))
	closeWindow := make([]float32, 0, len(s.observations))
	firstPrices := make([]float32, 0, 4)
	lastPrices := make([]float32, 0, 4)
	var (
		firstTimestamp time.Time
		lastTimestamp  time.Time
		hasFirst       bool
	)
	minuteEnd := s.bar.Timestamp.Add(time.Minute)
	openWindowEnd := s.bar.Timestamp.Add(500 * time.Millisecond)
	closeWindowStart := minuteEnd.Add(-500 * time.Millisecond)

	for _, observation := range s.observations {
		if _, ok := selectedSet[observation.underlyingIndex]; !ok {
			continue
		}

		allPrices = append(allPrices, observation.price)
		if !observation.timestamp.Before(s.bar.Timestamp) && observation.timestamp.Before(openWindowEnd) {
			openWindow = append(openWindow, observation.price)
		}
		if !observation.timestamp.Before(closeWindowStart) && observation.timestamp.Before(minuteEnd) {
			closeWindow = append(closeWindow, observation.price)
		}

		if !hasFirst {
			firstTimestamp = observation.timestamp
			lastTimestamp = observation.timestamp
			hasFirst = true
			firstPrices = append(firstPrices, observation.price)
			lastPrices = append(lastPrices, observation.price)
			continue
		}

		if observation.timestamp.Before(firstTimestamp) {
			firstTimestamp = observation.timestamp
			firstPrices = firstPrices[:0]
			firstPrices = append(firstPrices, observation.price)
		} else if observation.timestamp.Equal(firstTimestamp) {
			firstPrices = append(firstPrices, observation.price)
		}

		if observation.timestamp.After(lastTimestamp) {
			lastTimestamp = observation.timestamp
			lastPrices = lastPrices[:0]
			lastPrices = append(lastPrices, observation.price)
		} else if observation.timestamp.Equal(lastTimestamp) {
			lastPrices = append(lastPrices, observation.price)
		}
	}

	if len(allPrices) == 0 {
		return
	}

	s.bar.TickCount = uint32(len(allPrices))
	s.bar.Volume = float64(s.bar.TickCount)
	if len(selectedSources) == 1 {
		s.bar.PriceSource = selectedSources[0]
	} else {
		s.bar.PriceSource = "mixed"
	}

	sorted := append([]float32(nil), allPrices...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	s.bar.High = percentileSortedF32(sorted, 0.95)
	s.bar.Low = percentileSortedF32(sorted, 0.05)

	if len(openWindow) > 0 {
		s.bar.Open = medianF32(openWindow)
	} else {
		s.bar.Open = medianF32(firstPrices)
	}

	if len(closeWindow) > 0 {
		s.bar.Close = medianF32(closeWindow)
	} else {
		s.bar.Close = medianF32(lastPrices)
	}
}

func nearestUnderlyingIndices(observations []spotObservation, minuteTS time.Time, limit int) []string {
	if limit < 1 || len(observations) == 0 {
		return nil
	}

	type candidate struct {
		source   string
		parsed   bool
		expiry   time.Time
		distance time.Duration
	}

	bySource := make(map[string]candidate)
	for _, observation := range observations {
		source := strings.TrimSpace(observation.underlyingIndex)
		if source == "" {
			continue
		}
		if _, exists := bySource[source]; exists {
			continue
		}
		expiry, ok := parseUnderlyingIndexDate(source)
		candidate := candidate{source: source, parsed: ok, expiry: expiry}
		if ok {
			candidate.distance = absDuration(expiry.Sub(minuteTS))
		}
		bySource[source] = candidate
	}

	parsed := make([]candidate, 0, len(bySource))
	unparsed := make([]candidate, 0, len(bySource))
	for _, candidate := range bySource {
		if candidate.parsed {
			parsed = append(parsed, candidate)
		} else {
			unparsed = append(unparsed, candidate)
		}
	}

	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].distance != parsed[j].distance {
			return parsed[i].distance < parsed[j].distance
		}
		if !parsed[i].expiry.Equal(parsed[j].expiry) {
			return parsed[i].expiry.Before(parsed[j].expiry)
		}
		return parsed[i].source < parsed[j].source
	})
	sort.Slice(unparsed, func(i, j int) bool {
		return unparsed[i].source < unparsed[j].source
	})

	selected := make([]string, 0, min(limit, len(bySource)))
	for _, candidate := range parsed {
		selected = append(selected, candidate.source)
		if len(selected) == limit {
			return selected
		}
	}
	for _, candidate := range unparsed {
		selected = append(selected, candidate.source)
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func parseUnderlyingIndexDate(underlyingIndex string) (time.Time, bool) {
	underlyingIndex = strings.TrimSpace(underlyingIndex)
	if underlyingIndex == "" {
		return time.Time{}, false
	}
	parts := strings.Split(underlyingIndex, "-")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	expiry, err := ParseExpiryDate(strings.ToUpper(parts[len(parts)-1]))
	if err != nil {
		return time.Time{}, false
	}
	return expiry, true
}

func absDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return -duration
	}
	return duration
}

func (a *Aggregator) Add(tick TickRow) {
	minuteTS := tick.Timestamp.UTC().Truncate(time.Minute)
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
	acc.bar.Volume = float64(acc.bar.TickCount)

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
		a.spotAccumulators[spotKey] = spotAcc
	}
	spotAcc.observations = append(spotAcc.observations, spotObservation{
		timestamp:       tick.Timestamp,
		underlyingIndex: strings.TrimSpace(tick.UnderlyingIndex),
		price:           tick.UnderlyingPrice,
	})
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
