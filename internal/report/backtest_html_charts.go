package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func resolveChartSeriesSource(result *backtest.Result, meta HTMLMeta) chartSeriesSource {
	defaultLabel := strings.TrimSpace(meta.Asset)
	if defaultLabel == "" && result != nil {
		defaultLabel = strings.TrimSpace(result.UnderlyingUnit)
	}
	if defaultLabel == "" {
		defaultLabel = "primary"
	}
	if strings.TrimSpace(meta.Interval) != "" {
		defaultLabel = defaultLabel + " / " + strings.TrimSpace(meta.Interval)
	}

	source := chartSeriesSource{Label: defaultLabel, SourceText: "primary OHLC series"}
	if result == nil || len(result.Series) == 0 {
		return source
	}
	prefix := normalizeSeriesPrefix(meta.ChartSeriesPrefix)
	if prefix == "" {
		return source
	}
	if !seriesHasFiniteValue(seriesBySource(result.Series, prefix, "close")) {
		source.SourceText = fmt.Sprintf("primary OHLC series; requested chart source %q was not available", prefix)
		return source
	}
	label := strings.TrimSpace(meta.ChartSelectionLabel)
	if label == "" {
		label = inferChartSourceLabel(prefix)
	}
	return chartSeriesSource{
		Prefix:     prefix,
		Label:      fallbackText(label, prefix),
		SourceText: prefix,
		Override:   true,
	}
}

func normalizeSeriesPrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), ".")
}

func inferChartSourceLabel(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if strings.HasPrefix(trimmed, "request.security.") {
		key := strings.TrimPrefix(trimmed, "request.security.")
		parts := strings.Split(key, "|")
		if len(parts) == 3 {
			return fmt.Sprintf("%s / %s / %s", parts[1], parts[0], parts[2])
		}
	}
	return trimmed
}

func seriesBySource(series map[string][]float64, prefix, field string) []float64 {
	if len(series) == 0 {
		return nil
	}
	if prefix != "" {
		if values := series[prefix+"."+field]; len(values) > 0 {
			return values
		}
	}
	return series[field]
}

func buildUnderlyingCandles(result *backtest.Result, source chartSeriesSource) ([]chartCandlePoint, bool) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, false
	}
	closeSeries := seriesBySource(result.Series, source.Prefix, "close")
	if len(closeSeries) == 0 {
		return nil, false
	}
	openSeries := seriesBySource(result.Series, source.Prefix, "open")
	highSeries := seriesBySource(result.Series, source.Prefix, "high")
	lowSeries := seriesBySource(result.Series, source.Prefix, "low")
	n := minInt(len(result.Timestamps), len(closeSeries))
	if n == 0 {
		return nil, false
	}

	candles := make([]chartCandlePoint, 0, n)
	usedFallback := false
	prevClose := closeSeries[0]
	for i := 0; i < n; i++ {
		closeValue := closeSeries[i]
		if !chartValueValid(closeValue) {
			continue
		}

		openValue := closeValue
		if i < len(openSeries) && chartValueValid(openSeries[i]) {
			openValue = openSeries[i]
		} else {
			usedFallback = true
			if i > 0 && chartValueValid(prevClose) {
				openValue = prevClose
			}
		}

		highValue := math.Max(openValue, closeValue)
		if i < len(highSeries) && chartValueValid(highSeries[i]) {
			highValue = math.Max(highValue, highSeries[i])
		} else {
			usedFallback = true
		}

		lowValue := math.Min(openValue, closeValue)
		if i < len(lowSeries) && chartValueValid(lowSeries[i]) {
			lowValue = math.Min(lowValue, lowSeries[i])
		} else {
			usedFallback = true
		}

		candles = append(candles, chartCandlePoint{
			Time:  result.Timestamps[i].Unix(),
			Open:  openValue,
			High:  highValue,
			Low:   lowValue,
			Close: closeValue,
		})
		prevClose = closeValue
	}
	return candles, usedFallback
}

func buildUnderlyingVolume(result *backtest.Result, source chartSeriesSource) ([]chartHistogramPoint, string) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, ""
	}

	seriesName := ""
	label := ""
	switch {
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "volume")):
		seriesName = "volume"
		label = "成交量"
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "tick_count")):
		seriesName = "tick_count"
		label = "成交笔数"
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "vol_norm")):
		seriesName = "vol_norm"
		label = "成交量"
	default:
		return nil, ""
	}

	values := seriesBySource(result.Series, source.Prefix, seriesName)
	openSeries := seriesBySource(result.Series, source.Prefix, "open")
	closeSeries := seriesBySource(result.Series, source.Prefix, "close")
	n := minInt(len(result.Timestamps), len(values))
	if n == 0 {
		return nil, ""
	}

	points := make([]chartHistogramPoint, 0, n)
	prevClose := math.NaN()
	if len(closeSeries) > 0 {
		prevClose = closeSeries[0]
	}
	for i := 0; i < n; i++ {
		value := values[i]
		if !chartValueValid(value) {
			continue
		}

		color := "rgba(34,197,94,0.52)"
		closeValue := math.NaN()
		if i < len(closeSeries) && chartValueValid(closeSeries[i]) {
			closeValue = closeSeries[i]
		}
		openValue := math.NaN()
		if i < len(openSeries) && chartValueValid(openSeries[i]) {
			openValue = openSeries[i]
		} else if chartValueValid(prevClose) {
			openValue = prevClose
		}
		if chartValueValid(openValue) && chartValueValid(closeValue) && closeValue < openValue {
			color = "rgba(249,115,22,0.52)"
		}

		points = append(points, chartHistogramPoint{
			Time:  result.Timestamps[i].Unix(),
			Value: value,
			Color: color,
		})
		if chartValueValid(closeValue) {
			prevClose = closeValue
		}
	}
	return points, label
}

func buildHoverColumns(result *backtest.Result) []hoverColumnPayload {
	if result == nil || len(result.ReportColumns) == 0 || len(result.Timestamps) == 0 || result.Series == nil {
		return []hoverColumnPayload{}
	}
	payload := make([]hoverColumnPayload, 0, len(result.ReportColumns))
	for _, column := range result.ReportColumns {
		values := result.Series[column.Source]
		if len(values) == 0 {
			continue
		}
		payload = append(payload, hoverColumnPayload{
			Source:   column.Source,
			Label:    column.Label,
			Decimals: column.Decimals,
			Overlay:  column.Overlay,
			Values:   buildTimeAlignedLineSeries(result.Timestamps, values),
		})
	}
	if len(payload) == 0 {
		return []hoverColumnPayload{}
	}
	return payload
}

func buildLineSeries(times []time.Time, values []float64) []chartLinePoint {
	n := minInt(len(times), len(values))
	if n == 0 {
		return []chartLinePoint{}
	}
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		if !chartValueValid(values[i]) {
			continue
		}
		value := values[i]
		points = append(points, chartLinePoint{
			Time:  times[i].Unix(),
			Value: &value,
		})
	}
	return points
}

func buildQuoteNetValueSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 {
		return nil
	}
	closeSeries := []float64(nil)
	if result.Series != nil {
		closeSeries = result.Series["close"]
	}
	if len(closeSeries) == 0 {
		if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
			return buildLineSeries(result.Timestamps, result.EquityCurve)
		}
		return nil
	}
	n := minInt(len(result.Timestamps), minInt(len(result.EquityCurve), len(closeSeries)))
	if n == 0 {
		return nil
	}
	values := make([]float64, 0, n)
	times := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		equityValue := result.EquityCurve[i]
		if !chartValueValid(equityValue) {
			continue
		}
		value := equityValue
		if !strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") && strings.TrimSpace(result.AccountUnit) != "" {
			closeValue := closeSeries[i]
			if !chartValueValid(closeValue) || closeValue <= 0 {
				continue
			}
			value = equityValue * closeValue
		}
		values = append(values, value)
		times = append(times, result.Timestamps[i])
	}
	return buildLineSeries(times, values)
}

func buildBuyHoldSeries(result *backtest.Result) ([]chartLinePoint, float64) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, 0
	}
	closeSeries := result.Series["close"]
	n := minInt(len(result.Timestamps), len(closeSeries))
	if n == 0 {
		return nil, 0
	}

	entryIndex := -1
	entryClose := 0.0
	for i := 0; i < n; i++ {
		if chartValueValid(closeSeries[i]) && closeSeries[i] > 0 {
			entryIndex = i
			entryClose = closeSeries[i]
			break
		}
	}
	if entryIndex < 0 {
		return nil, 0
	}

	initialUSD := result.InitialCapital
	trimmedUnit := strings.TrimSpace(result.AccountUnit)
	if trimmedUnit != "" && !strings.EqualFold(trimmedUnit, "USD") {
		initialUSD = result.InitialCapital * entryClose
	}
	if !chartValueValid(initialUSD) || initialUSD <= 0 {
		return nil, 0
	}

	points := make([]chartLinePoint, 0, n-entryIndex)
	for i := entryIndex; i < n; i++ {
		closeValue := closeSeries[i]
		if !chartValueValid(closeValue) || closeValue <= 0 {
			continue
		}
		value := initialUSD * (closeValue / entryClose)
		points = append(points, chartLinePoint{
			Time:  result.Timestamps[i].Unix(),
			Value: &value,
		})
	}
	if len(points) == 0 {
		return nil, 0
	}
	return points, initialUSD
}

func buildAssetPnLSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return nil
	}

	n := minInt(len(result.Timestamps), len(result.EquityCurve))
	closeSeries := []float64(nil)
	if result.Series != nil {
		closeSeries = result.Series["close"]
	}

	trimmedUnit := strings.TrimSpace(result.AccountUnit)
	assetInitial := 0.0
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		equityValue := result.EquityCurve[i]
		if !chartValueValid(equityValue) {
			continue
		}

		assetEquity := equityValue
		if isUSDLikeUnit(trimmedUnit) || trimmedUnit == "" {
			if i >= len(closeSeries) {
				continue
			}
			closeValue := closeSeries[i]
			if !chartValueValid(closeValue) || closeValue <= 0 {
				continue
			}
			assetEquity = equityValue / closeValue
			if assetInitial == 0 {
				assetInitial = result.InitialCapital / closeValue
			}
		} else if assetInitial == 0 {
			assetInitial = result.InitialCapital
		}

		pnlValue := assetEquity - assetInitial
		points = append(points, chartLinePoint{Time: result.Timestamps[i].Unix(), Value: floatPtr(pnlValue)})
	}

	if len(points) == 0 {
		return nil
	}
	return points
}

func linePointValues(series []chartLinePoint) []float64 {
	values := make([]float64, 0, len(series))
	for _, point := range series {
		if point.Value != nil {
			values = append(values, *point.Value)
		}
	}
	return values
}

func hasSubDailyInterval(timestamps []time.Time) bool {
	if len(timestamps) < 2 {
		return false
	}
	minGap := time.Duration(1<<63 - 1)
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap > 0 && gap < minGap {
			minGap = gap
		}
	}
	if minGap == time.Duration(1<<63-1) {
		return false
	}
	return minGap < 24*time.Hour
}

func isUSDLikeUnit(unit string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(unit))
	switch trimmed {
	case "", "USD", "USDT", "USDC", "BUSD", "FDUSD":
		return true
	default:
		return false
	}
}

func quoteMetricUnitLabel(result *backtest.Result) string {
	if result == nil {
		return "U"
	}
	if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
		return "USD"
	}
	return "U"
}

func buildTimeAlignedLineSeries(times []time.Time, values []float64) []chartLinePoint {
	n := minInt(len(times), len(values))
	if n == 0 {
		return []chartLinePoint{}
	}
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		point := chartLinePoint{Time: times[i].Unix()}
		if chartValueValid(values[i]) {
			value := values[i]
			point.Value = &value
		}
		points = append(points, point)
	}
	return points
}

func buildUnderlyingMarkers(result *backtest.Result) ([]chartMarker, int, int) {
	if result == nil {
		return []chartMarker{}, 0, 0
	}
	aggregated := make(map[markerKey]*chartMarker)
	tradeMarkerCount := 0
	spreadEventCount := 0

	for _, trade := range result.Trades {
		tradeMarkerCount++
		shape := "arrowUp"
		position := "belowBar"
		color := "#2dd4bf"
		label := fmt.Sprintf("买入 %s", decimal(trade.Qty))
		if trade.Side == backtest.Sell {
			shape = "arrowDown"
			position = "aboveBar"
			color = "#f59e0b"
			label = fmt.Sprintf("卖出 %s", decimal(trade.Qty))
		}
		appendMarker(aggregated, chartMarker{
			Time:     trade.Timestamp.Unix(),
			Position: position,
			Color:    color,
			Shape:    shape,
			Text:     label,
		})
	}

	for _, spread := range result.SpreadPositions {
		spreadEventCount++
		appendMarker(aggregated, chartMarker{
			Time:     spread.OpenTime.Unix(),
			Position: "belowBar",
			Color:    "#60a5fa",
			Shape:    "circle",
			Text:     fmt.Sprintf("开仓 #%d", spread.ID),
		})
		if spread.CloseTime != nil {
			spreadEventCount++
			appendMarker(aggregated, chartMarker{
				Time:     spread.CloseTime.Unix(),
				Position: "aboveBar",
				Color:    "#fb7185",
				Shape:    "square",
				Text:     fmt.Sprintf("平仓 #%d", spread.ID),
			})
		}
	}

	markers := make([]chartMarker, 0, len(aggregated))
	for _, marker := range aggregated {
		markers = append(markers, *marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].Time != markers[j].Time {
			return markers[i].Time < markers[j].Time
		}
		if markers[i].Position != markers[j].Position {
			return markers[i].Position < markers[j].Position
		}
		return markers[i].Text < markers[j].Text
	})
	return markers, tradeMarkerCount, spreadEventCount
}

func buildActiveTimes(result *backtest.Result) []int64 {
	if result == nil || len(result.Timestamps) == 0 {
		return []int64{}
	}
	n := len(result.Timestamps)
	active := make([]bool, n)

	for _, spread := range result.SpreadPositions {
		for i, ts := range result.Timestamps {
			if ts.Before(spread.OpenTime) {
				continue
			}
			if spread.CloseTime != nil && ts.After(*spread.CloseTime) {
				continue
			}
			active[i] = true
		}
	}

	trades := append([]backtest.Trade(nil), result.Trades...)
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Timestamp.Equal(trades[j].Timestamp) {
			if trades[i].Security.Symbol != trades[j].Security.Symbol {
				return trades[i].Security.Symbol < trades[j].Security.Symbol
			}
			return trades[i].ID < trades[j].ID
		}
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	positionBySecurity := make(map[string]float64)
	activeSecurities := 0
	tradeIdx := 0
	for i, ts := range result.Timestamps {
		hadTrade := false
		for tradeIdx < len(trades) && !trades[tradeIdx].Timestamp.After(ts) {
			trade := trades[tradeIdx]
			key := trade.Security.Market + "|" + trade.Security.Symbol + "|" + trade.Security.Interval
			prevQty := positionBySecurity[key]
			nextQty := prevQty
			if trade.Side == backtest.Buy {
				nextQty += trade.Qty
			} else {
				nextQty -= trade.Qty
			}
			if math.Abs(prevQty) <= 1e-9 && math.Abs(nextQty) > 1e-9 {
				activeSecurities++
			} else if math.Abs(prevQty) > 1e-9 && math.Abs(nextQty) <= 1e-9 {
				activeSecurities--
			}
			if math.Abs(nextQty) <= 1e-9 {
				delete(positionBySecurity, key)
			} else {
				positionBySecurity[key] = nextQty
			}
			hadTrade = true
			tradeIdx++
		}
		if hadTrade || activeSecurities > 0 {
			active[i] = true
		}
	}

	activeTimes := make([]int64, 0, n)
	for i, ok := range active {
		if ok {
			activeTimes = append(activeTimes, result.Timestamps[i].Unix())
		}
	}
	return activeTimes
}

func buildSettledEquityData(result *backtest.Result) settledEquityData {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return settledEquityData{}
	}

	eventDeltas := buildSettlementEventDeltas(result)
	series := []chartLinePoint{{Time: result.Timestamps[0].Unix(), Value: floatPtr(result.InitialCapital)}}
	profitBars := make([]chartHistogramPoint, 0, len(eventDeltas))
	lossBars := make([]chartHistogramPoint, 0, len(eventDeltas))
	exposure := buildExposureSeries(result)

	currentSettled := result.InitialCapital
	segmentMaxFloat := 0.0
	segmentMinFloat := 0.0

	for index, ts := range result.Timestamps {
		unix := ts.Unix()
		if delta, ok := eventDeltas[unix]; ok && math.Abs(delta) > 1e-9 {
			if segmentMaxFloat > 1e-9 {
				profitBars = append(profitBars, chartHistogramPoint{Time: unix, Value: segmentMaxFloat, Color: "rgba(45, 212, 191, 0.35)"})
			}
			if segmentMinFloat < -1e-9 {
				lossBars = append(lossBars, chartHistogramPoint{Time: unix, Value: segmentMinFloat, Color: "rgba(248, 113, 113, 0.28)"})
			}
			currentSettled += delta
			appendOrReplaceLinePoint(&series, chartLinePoint{Time: unix, Value: floatPtr(currentSettled)})
			segmentMaxFloat = 0
			segmentMinFloat = 0
		}

		if index >= len(result.EquityCurve) {
			continue
		}
		floating := result.EquityCurve[index] - currentSettled
		if floating > segmentMaxFloat {
			segmentMaxFloat = floating
		}
		if floating < segmentMinFloat {
			segmentMinFloat = floating
		}
	}

	return settledEquityData{
		Series:         series,
		FloatingProfit: profitBars,
		FloatingLoss:   lossBars,
		Exposure:       exposure,
	}
}

func buildSettlementEventDeltas(result *backtest.Result) map[int64]float64 {
	deltas := buildRegularTradeSettlementDeltas(result.Trades)
	for eventTime, delta := range buildSpreadSettlementDeltas(result.SpreadPositions) {
		deltas[eventTime] += delta
	}
	return deltas
}

func buildRegularTradeSettlementDeltas(trades []backtest.Trade) map[int64]float64 {
	if len(trades) == 0 {
		return map[int64]float64{}
	}

	ordered := append([]backtest.Trade(nil), trades...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Timestamp.Equal(ordered[j].Timestamp) {
			if ordered[i].Security.Symbol != ordered[j].Security.Symbol {
				return ordered[i].Security.Symbol < ordered[j].Security.Symbol
			}
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})

	states := make(map[backtest.SecurityRef]*regularPositionState)
	deltas := make(map[int64]float64)
	for _, trade := range ordered {
		state, ok := states[trade.Security]
		if !ok {
			state = &regularPositionState{}
			states[trade.Security] = state
		}
		realized := applyRegularTradeSettlement(state, trade)
		if math.Abs(realized) > 1e-9 {
			deltas[trade.Timestamp.Unix()] += realized
		}
	}
	return deltas
}

func applyRegularTradeSettlement(state *regularPositionState, trade backtest.Trade) float64 {
	fillQty := trade.Qty
	if trade.Side == backtest.Sell {
		fillQty = -fillQty
	}
	fillAbs := math.Abs(fillQty)
	positionAbs := math.Abs(state.qty)

	if state.qty == 0 {
		state.qty = fillQty
		state.avgEntryPrice = trade.FillPrice
		state.costBasis = trade.FillPrice * fillAbs
		state.openCommission = trade.Commission
		return 0
	}

	if (state.qty > 0 && fillQty > 0) || (state.qty < 0 && fillQty < 0) {
		state.costBasis += trade.FillPrice * fillAbs
		state.qty += fillQty
		state.avgEntryPrice = state.costBasis / math.Abs(state.qty)
		state.openCommission += trade.Commission
		return 0
	}

	closeQty := fillAbs
	if closeQty > positionAbs {
		closeQty = positionAbs
	}

	realized := 0.0
	if state.qty > 0 {
		realized = closeQty * (trade.FillPrice - state.avgEntryPrice)
	} else {
		realized = closeQty * (state.avgEntryPrice - trade.FillPrice)
	}

	openCommissionAllocated := 0.0
	if positionAbs > 1e-9 && state.openCommission != 0 {
		openCommissionAllocated = state.openCommission * (closeQty / positionAbs)
	}
	exitCommissionAllocated := 0.0
	if fillAbs > 1e-9 && trade.Commission != 0 {
		exitCommissionAllocated = trade.Commission * (closeQty / fillAbs)
	}
	realized -= openCommissionAllocated + exitCommissionAllocated

	remainingOld := positionAbs - closeQty
	newQty := fillAbs - closeQty
	if remainingOld > 1e-9 {
		if state.qty > 0 {
			state.qty = remainingOld
		} else {
			state.qty = -remainingOld
		}
		state.costBasis = state.avgEntryPrice * remainingOld
		state.openCommission -= openCommissionAllocated
		return realized
	}

	if newQty > 1e-9 {
		if fillQty > 0 {
			state.qty = newQty
		} else {
			state.qty = -newQty
		}
		state.avgEntryPrice = trade.FillPrice
		state.costBasis = trade.FillPrice * newQty
		state.openCommission = trade.Commission - exitCommissionAllocated
		return realized
	}

	state.qty = 0
	state.avgEntryPrice = 0
	state.costBasis = 0
	state.openCommission = 0
	return realized
}

func buildSpreadSettlementDeltas(spreads []backtest.SpreadPositionReport) map[int64]float64 {
	deltas := make(map[int64]float64)
	for _, spread := range spreads {
		settledAt := spread.CloseTime
		if settledAt == nil {
			settledAt = spread.CloseTriggerTime
		}
		if settledAt == nil {
			continue
		}
		deltas[settledAt.Unix()] += spread.RealizedPnL
	}
	return deltas
}

func buildExposureSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 {
		return nil
	}

	counts := make([]float64, len(result.Timestamps))
	for _, spread := range result.SpreadPositions {
		for index, ts := range result.Timestamps {
			if ts.Before(spread.OpenTime) {
				continue
			}
			if spread.CloseTime != nil && ts.After(*spread.CloseTime) {
				continue
			}
			counts[index]++
		}
	}

	trades := append([]backtest.Trade(nil), result.Trades...)
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Timestamp.Equal(trades[j].Timestamp) {
			if trades[i].Security.Symbol != trades[j].Security.Symbol {
				return trades[i].Security.Symbol < trades[j].Security.Symbol
			}
			return trades[i].ID < trades[j].ID
		}
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	positionBySecurity := make(map[string]float64)
	activeSecurities := 0
	tradeIndex := 0
	for index, ts := range result.Timestamps {
		for tradeIndex < len(trades) && !trades[tradeIndex].Timestamp.After(ts) {
			trade := trades[tradeIndex]
			key := trade.Security.Market + "|" + trade.Security.Symbol + "|" + trade.Security.Interval
			prevQty := positionBySecurity[key]
			nextQty := prevQty
			if trade.Side == backtest.Buy {
				nextQty += trade.Qty
			} else {
				nextQty -= trade.Qty
			}
			if math.Abs(prevQty) <= 1e-9 && math.Abs(nextQty) > 1e-9 {
				activeSecurities++
			} else if math.Abs(prevQty) > 1e-9 && math.Abs(nextQty) <= 1e-9 {
				activeSecurities--
			}
			if math.Abs(nextQty) <= 1e-9 {
				delete(positionBySecurity, key)
			} else {
				positionBySecurity[key] = nextQty
			}
			tradeIndex++
		}
		counts[index] += float64(activeSecurities)
	}

	return buildTimeAlignedLineSeries(result.Timestamps, counts)
}

func appendOrReplaceLinePoint(points *[]chartLinePoint, point chartLinePoint) {
	if len(*points) == 0 {
		*points = append(*points, point)
		return
	}
	last := &(*points)[len(*points)-1]
	if last.Time == point.Time {
		last.Value = point.Value
		return
	}
	*points = append(*points, point)
}

func floatPtr(value float64) *float64 {
	return &value
}

func appendMarker(markers map[markerKey]*chartMarker, marker chartMarker) {
	key := markerKey{
		Time:     marker.Time,
		Position: marker.Position,
		Color:    marker.Color,
		Shape:    marker.Shape,
	}
	if existing, ok := markers[key]; ok {
		if !strings.Contains(existing.Text, marker.Text) {
			existing.Text += " · " + marker.Text
		}
		return
	}
	copyMarker := marker
	markers[key] = &copyMarker
}

func candleRange(candles []chartCandlePoint) (float64, float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	minValue := candles[0].Low
	maxValue := candles[0].High
	for _, candle := range candles[1:] {
		if candle.Low < minValue {
			minValue = candle.Low
		}
		if candle.High > maxValue {
			maxValue = candle.High
		}
	}
	return minValue, maxValue
}

func linePath(values []float64, width, height float64) string {
	if len(values) == 0 {
		return ""
	}
	minValue, maxValue := minMax(values)
	if maxValue-minValue == 0 {
		maxValue += 1
		minValue -= 1
	}
	var builder strings.Builder
	for i, value := range values {
		x := 0.0
		if len(values) > 1 {
			x = width * float64(i) / float64(len(values)-1)
		}
		y := height - ((value-minValue)/(maxValue-minValue))*height
		if i == 0 {
			builder.WriteString(fmt.Sprintf("M %.2f %.2f", x, y))
		} else {
			builder.WriteString(fmt.Sprintf(" L %.2f %.2f", x, y))
		}
	}
	return builder.String()
}

func drawdownSeries(equity []float64) []float64 {
	if len(equity) == 0 {
		return nil
	}
	series := make([]float64, len(equity))
	peak := equity[0]
	for i, eq := range equity {
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			series[i] = (peak - eq) / peak
		}
	}
	return series
}

func minMax(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	minValue, maxValue := values[0], values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}

func maxValue(values []float64) float64 {
	maxVal := 0.0
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}
