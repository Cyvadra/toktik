package dualspreadsvol

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/feeds"
)

func smoothingEnabled(raw any) bool {
	switch value := raw.(type) {
	case nil:
		return true
	case bool:
		return value
	case string:
		normalized := strings.TrimSpace(strings.ToLower(value))
		if normalized == "" {
			return true
		}
		switch normalized {
		case "0", "false", "off", "no":
			return false
		default:
			return true
		}
	case float64:
		return value != 0
	case int:
		return value != 0
	default:
		return true
	}
}

func (s *strategy) apply12HSmoothing(ctx *backtest.PreloadContext, primary, htf, dvol12h, dvolLive *backtest.PreloadSecurity) error {
	primaryClose, err := primary.RequireColumn("close")
	if err != nil {
		return err
	}
	dvolLiveClose, err := ctx.ColumnAlignedFactorToPrimary(s.dvolLiveRef, "close")
	if err != nil {
		return err
	}

	priceBuckets, priceWeights, err := buildHTFProgress(primary.Timestamps(), s.primaryTF, htf.Timestamps(), interval12h)
	if err != nil {
		return err
	}
	dvolBuckets, dvolWeights, err := buildHTFProgress(primary.Timestamps(), s.primaryTF, dvol12h.Timestamps(), interval12h)
	if err != nil {
		return err
	}

	confirmedVolStd, err := primary.RequireColumn(volStdValueColumn)
	if err != nil {
		return err
	}
	confirmedVolStdPR, err := primary.RequireColumn(volStdPercentileColumn)
	if err != nil {
		return err
	}
	confirmedVolStdQ, err := primary.RequireColumn(volStdThresholdColumn)
	if err != nil {
		return err
	}
	confirmedDVOL, err := primary.RequireColumn(dvolValueColumn)
	if err != nil {
		return err
	}
	confirmedIVPR, err := primary.RequireColumn(ivPercentileColumn)
	if err != nil {
		return err
	}
	confirmedIVQ, err := primary.RequireColumn(ivThresholdColumn)
	if err != nil {
		return err
	}

	htfClose, err := htf.RequireColumn("close")
	if err != nil {
		return err
	}
	htfStd, err := htf.RequireColumn(volStdBaseColumn)
	if err != nil {
		return err
	}
	htfRatio, err := htf.RequireColumn(volStdValueColumn)
	if err != nil {
		return err
	}
	dvol12hClose, err := dvol12h.RequireColumn("close")
	if err != nil {
		return err
	}

	volStdSmoothed := makeNaNSlice(primary.Len())
	volStdPRSmoothed := makeNaNSlice(primary.Len())
	volStdQSmoothed := makeNaNSlice(primary.Len())
	dvolSmoothed := makeNaNSlice(primary.Len())
	ivPRSmoothed := makeNaNSlice(primary.Len())
	ivQSmoothed := makeNaNSlice(primary.Len())

	for i := 0; i < primary.Len(); i++ {
		estVolStd := estimateVolStdRatio(priceBuckets[i], primaryClose[i], htfClose, htfStd)
		volStdSmoothed[i] = blendEstimatedValue(confirmedVolStd[i], estVolStd, priceWeights[i])

		estVolStdPR := percentileRankWithEstimatedCurrent(htfRatio, priceBuckets[i], volStdLookback, estVolStd)
		volStdPRSmoothed[i] = blendEstimatedValue(confirmedVolStdPR[i], estVolStdPR, priceWeights[i])

		estVolStdQ := rollingQuantileWithEstimatedCurrent(htfRatio, priceBuckets[i], volStdLookback, defaultVolThresholdPercentile/100, estVolStd)
		volStdQSmoothed[i] = blendEstimatedValue(confirmedVolStdQ[i], estVolStdQ, priceWeights[i])

		estDVOL := dvolLiveClose[i]
		dvolSmoothed[i] = blendEstimatedValue(confirmedDVOL[i], estDVOL, dvolWeights[i])

		estIVPR := percentileRankWithEstimatedCurrent(dvol12hClose, dvolBuckets[i], ivPercentileLookback, estDVOL)
		ivPRSmoothed[i] = blendEstimatedValue(confirmedIVPR[i], estIVPR, dvolWeights[i])

		estIVQ := rollingQuantileWithEstimatedCurrent(dvol12hClose, dvolBuckets[i], ivPercentileLookback, defaultVolThresholdPercentile/100, estDVOL)
		ivQSmoothed[i] = blendEstimatedValue(confirmedIVQ[i], estIVQ, dvolWeights[i])
	}

	if err := primary.SetColumn(volStdValueColumn, volStdSmoothed); err != nil {
		return err
	}
	if err := primary.SetColumn(volStdPercentileColumn, volStdPRSmoothed); err != nil {
		return err
	}
	if err := primary.SetColumn(volStdThresholdColumn, volStdQSmoothed); err != nil {
		return err
	}
	if err := primary.SetColumn(dvolValueColumn, dvolSmoothed); err != nil {
		return err
	}
	if err := primary.SetColumn(ivPercentileColumn, ivPRSmoothed); err != nil {
		return err
	}
	return primary.SetColumn(ivThresholdColumn, ivQSmoothed)
}

func buildHTFProgress(primaryTimes []time.Time, primaryInterval string, higherTimes []time.Time, higherInterval string) ([]int, []float64, error) {
	primaryDur, err := parseIntervalDuration(primaryInterval)
	if err != nil {
		return nil, nil, fmt.Errorf("parse primary interval %q: %w", primaryInterval, err)
	}
	higherDur, err := parseIntervalDuration(higherInterval)
	if err != nil {
		return nil, nil, fmt.Errorf("parse higher interval %q: %w", higherInterval, err)
	}

	indices := make([]int, len(primaryTimes))
	weights := make([]float64, len(primaryTimes))
	for i := range indices {
		indices[i] = -1
	}
	if len(higherTimes) == 0 {
		return indices, weights, nil
	}

	for i, ts := range primaryTimes {
		evalTime := ts.Add(primaryDur)
		idx := sort.Search(len(higherTimes), func(j int) bool {
			return !higherTimes[j].Before(evalTime)
		}) - 1
		if idx < 0 {
			continue
		}
		indices[i] = idx
		elapsed := evalTime.Sub(higherTimes[idx])
		weight := float64(elapsed) / float64(higherDur)
		if weight < 0 {
			weight = 0
		}
		if weight > 1 {
			weight = 1
		}
		weights[i] = weight
	}

	return indices, weights, nil
}

func parseIntervalDuration(interval string) (time.Duration, error) {
	normalized := strings.TrimSpace(strings.ToLower(interval))
	if normalized == "" {
		return 0, fmt.Errorf("empty interval")
	}
	if window, err := feeds.ParseWindow(normalized); err == nil {
		return window.Duration, nil
	}
	if minutes, err := strconv.Atoi(normalized); err == nil {
		return time.Duration(minutes) * time.Minute, nil
	}
	return 0, fmt.Errorf("unsupported interval %q", interval)
}

func estimateVolStdRatio(currentIndex int, currentClose float64, confirmedClose, confirmedStd []float64) float64 {
	estStd := rollingStdWithEstimatedCurrent(confirmedClose, currentIndex, currentClose, volStdPeriod)
	if math.IsNaN(estStd) {
		return math.NaN()
	}
	estStdSMA := rollingAverageWithEstimatedCurrent(confirmedStd, currentIndex, estStd, volStdSMAPeriod)
	if math.IsNaN(estStdSMA) || estStdSMA == 0 {
		return math.NaN()
	}
	return estStd / estStdSMA
}

func rollingStdWithEstimatedCurrent(confirmed []float64, currentIndex int, currentValue float64, period int) float64 {
	if period <= 1 || math.IsNaN(currentValue) || currentIndex < period-1 {
		return math.NaN()
	}
	start := currentIndex - period + 1
	sum := 0.0
	sumSq := 0.0
	for idx := start; idx < currentIndex; idx++ {
		value := confirmed[idx]
		if math.IsNaN(value) {
			return math.NaN()
		}
		sum += value
		sumSq += value * value
	}
	sum += currentValue
	sumSq += currentValue * currentValue
	variance := (sumSq - sum*sum/float64(period)) / float64(period-1)
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func rollingAverageWithEstimatedCurrent(confirmed []float64, currentIndex int, currentValue float64, period int) float64 {
	if period <= 0 || math.IsNaN(currentValue) || currentIndex < period-1 {
		return math.NaN()
	}
	start := currentIndex - period + 1
	sum := 0.0
	for idx := start; idx < currentIndex; idx++ {
		value := confirmed[idx]
		if math.IsNaN(value) {
			return math.NaN()
		}
		sum += value
	}
	return (sum + currentValue) / float64(period)
}

func percentileRankWithEstimatedCurrent(confirmed []float64, currentIndex, period int, currentValue float64) float64 {
	if math.IsNaN(currentValue) || currentIndex < period {
		return math.NaN()
	}
	start := currentIndex - period
	count := 0
	valid := 0
	for idx := start; idx < currentIndex; idx++ {
		value := confirmed[idx]
		if math.IsNaN(value) {
			continue
		}
		valid++
		if value < currentValue {
			count++
		}
	}
	if valid == 0 {
		return math.NaN()
	}
	return float64(count) / float64(valid) * 100
}

func rollingQuantileWithEstimatedCurrent(confirmed []float64, currentIndex, period int, q, currentValue float64) float64 {
	if math.IsNaN(currentValue) || currentIndex < period-1 {
		return math.NaN()
	}
	values := make([]float64, 0, period)
	start := currentIndex - period + 1
	for idx := start; idx < currentIndex; idx++ {
		value := confirmed[idx]
		if math.IsNaN(value) {
			return math.NaN()
		}
		values = append(values, value)
	}
	values = append(values, currentValue)
	sort.Float64s(values)
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	position := q * float64(period-1)
	lo := int(math.Floor(position))
	hi := int(math.Ceil(position))
	if lo == hi {
		return values[lo]
	}
	weight := position - float64(lo)
	return values[lo]*(1-weight) + values[hi]*weight
}

func blendEstimatedValue(confirmed, estimated, weight float64) float64 {
	if math.IsNaN(estimated) {
		return confirmed
	}
	if math.IsNaN(confirmed) {
		return estimated
	}
	if weight <= 0 {
		return confirmed
	}
	if weight >= 1 {
		return estimated
	}
	return confirmed*(1-weight) + estimated*weight
}

func makeNaNSlice(length int) []float64 {
	values := make([]float64, length)
	for i := range values {
		values[i] = math.NaN()
	}
	return values
}
