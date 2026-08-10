package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func compressLineSeriesDailyEOD(series []chartLinePoint) []chartLinePoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartLinePoint, 0, len(series))
	currentDay := ""
	var current chartLinePoint
	for _, point := range series {
		if point.Value == nil {
			continue
		}
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		current = point
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressTimeAlignedLineSeriesDailyEOD(series []chartLinePoint) []chartLinePoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartLinePoint, 0, len(series))
	currentDay := ""
	var current chartLinePoint
	for _, point := range series {
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		current = point
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressCandlesDailyEOD(candles []chartCandlePoint) []chartCandlePoint {
	if len(candles) == 0 {
		return nil
	}
	compressed := make([]chartCandlePoint, 0, len(candles))
	currentDay := ""
	var current chartCandlePoint
	for _, candle := range candles {
		day := time.Unix(candle.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = candle
			continue
		}
		current.High = math.Max(current.High, candle.High)
		current.Low = math.Min(current.Low, candle.Low)
		current.Close = candle.Close
		current.Time = candle.Time
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

type histogramAggregateMode int

const (
	histogramAggregateLast histogramAggregateMode = iota
	histogramAggregateSum
)

func compressHistogramSeriesDaily(series []chartHistogramPoint, mode histogramAggregateMode) []chartHistogramPoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartHistogramPoint, 0, len(series))
	currentDay := ""
	var current chartHistogramPoint
	for _, point := range series {
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		if mode == histogramAggregateSum {
			current.Value += point.Value
		} else {
			current.Value = point.Value
		}
		current.Time = point.Time
		if strings.TrimSpace(point.Color) != "" {
			current.Color = point.Color
		}
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressHoverColumnsDailyEOD(columns []hoverColumnPayload) []hoverColumnPayload {
	if len(columns) == 0 {
		return nil
	}
	compressed := make([]hoverColumnPayload, 0, len(columns))
	for _, column := range columns {
		column.Values = compressTimeAlignedLineSeriesDailyEOD(column.Values)
		compressed = append(compressed, column)
	}
	return compressed
}

func compressActiveTimesDaily(activeTimes []int64, timeline []time.Time) []int64 {
	if len(activeTimes) == 0 || len(timeline) == 0 {
		return nil
	}
	activeSet := make(map[int64]struct{}, len(activeTimes))
	for _, ts := range activeTimes {
		activeSet[ts] = struct{}{}
	}
	lastByDay := make(map[string]int64)
	hasActiveByDay := make(map[string]bool)
	orderedDays := make([]string, 0, len(timeline))
	seenDay := make(map[string]struct{}, len(timeline))
	for _, ts := range timeline {
		day := ts.UTC().Format("2006-01-02")
		if _, ok := seenDay[day]; !ok {
			seenDay[day] = struct{}{}
			orderedDays = append(orderedDays, day)
		}
		unix := ts.Unix()
		lastByDay[day] = unix
		if _, ok := activeSet[unix]; ok {
			hasActiveByDay[day] = true
		}
	}
	compressed := make([]int64, 0, len(orderedDays))
	for _, day := range orderedDays {
		if hasActiveByDay[day] {
			compressed = append(compressed, lastByDay[day])
		}
	}
	return compressed
}

func buildSettledEquityDataDailyEOD(result *backtest.Result) settledEquityData {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return settledEquityData{}
	}

	eventDeltas := buildSettlementEventDeltas(result)
	currentSettled := result.InitialCapital
	series := make([]chartLinePoint, 0, len(result.Timestamps))
	profitBars := make([]chartHistogramPoint, 0, len(result.Timestamps))
	lossBars := make([]chartHistogramPoint, 0, len(result.Timestamps))

	currentDay := ""
	dayLastTime := int64(0)
	daySettled := currentSettled
	dayMaxFloat := 0.0
	dayMinFloat := 0.0
	flushDay := func() {
		if currentDay == "" {
			return
		}
		series = append(series, chartLinePoint{Time: dayLastTime, Value: floatPtr(daySettled)})
		if dayMaxFloat > 1e-9 {
			profitBars = append(profitBars, chartHistogramPoint{Time: dayLastTime, Value: dayMaxFloat, Color: "rgba(45, 212, 191, 0.35)"})
		}
		if dayMinFloat < -1e-9 {
			lossBars = append(lossBars, chartHistogramPoint{Time: dayLastTime, Value: dayMinFloat, Color: "rgba(248, 113, 113, 0.28)"})
		}
	}

	for index, ts := range result.Timestamps {
		day := ts.UTC().Format("2006-01-02")
		if day != currentDay {
			flushDay()
			currentDay = day
			dayMaxFloat = 0
			dayMinFloat = 0
		}
		unix := ts.Unix()
		if delta, ok := eventDeltas[unix]; ok && math.Abs(delta) > 1e-9 {
			currentSettled += delta
		}
		if index >= len(result.EquityCurve) {
			continue
		}
		floating := result.EquityCurve[index] - currentSettled
		if floating > dayMaxFloat {
			dayMaxFloat = floating
		}
		if floating < dayMinFloat {
			dayMinFloat = floating
		}
		daySettled = currentSettled
		dayLastTime = unix
	}
	flushDay()

	return settledEquityData{
		Series:         series,
		FloatingProfit: profitBars,
		FloatingLoss:   lossBars,
		Exposure:       compressLineSeriesDailyEOD(buildExposureSeries(result)),
	}
}

func compressMarkersDaily(markers []chartMarker) []chartMarker {
	if len(markers) == 0 {
		return nil
	}
	type groupedMarker struct {
		marker chartMarker
		texts  []string
		seen   map[string]struct{}
	}
	grouped := make(map[string]*groupedMarker)
	for _, marker := range markers {
		day := time.Unix(marker.Time, 0).UTC().Format("2006-01-02")
		groupKey := strings.Join([]string{day, marker.Position, marker.Color, marker.Shape}, "|")
		group, ok := grouped[groupKey]
		if !ok {
			group = &groupedMarker{
				marker: chartMarker{
					Time:     marker.Time,
					Position: marker.Position,
					Color:    marker.Color,
					Shape:    marker.Shape,
				},
				seen: make(map[string]struct{}),
			}
			grouped[groupKey] = group
		}
		group.marker.Time = marker.Time
		text := strings.TrimSpace(marker.Text)
		if text == "" {
			continue
		}
		if _, exists := group.seen[text]; exists {
			continue
		}
		group.seen[text] = struct{}{}
		group.texts = append(group.texts, text)
	}
	compressed := make([]chartMarker, 0, len(grouped))
	for _, group := range grouped {
		text := strings.Join(group.texts, " · ")
		if len(group.texts) > 3 {
			text = strings.Join(group.texts[:3], " · ") + fmt.Sprintf(" · +%d", len(group.texts)-3)
		}
		group.marker.Text = text
		compressed = append(compressed, group.marker)
	}
	sort.Slice(compressed, func(i, j int) bool {
		if compressed[i].Time != compressed[j].Time {
			return compressed[i].Time < compressed[j].Time
		}
		if compressed[i].Position != compressed[j].Position {
			return compressed[i].Position < compressed[j].Position
		}
		return compressed[i].Text < compressed[j].Text
	})
	return compressed
}
