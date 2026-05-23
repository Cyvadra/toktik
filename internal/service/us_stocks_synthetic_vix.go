package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

var syntheticVIXProxySymbols = []string{"VXX", "UVXY", "SVXY", "SVIX", "UVIX", "VIXY", "VIXM", "VXZ"}

const syntheticVIXDefaultIntercept = 1.8440597565739756

var syntheticVIXDefaultWeights = []float64{
	0.052353322743221596,
	0.16069138845108125,
	-0.001736387659850588,
	-0.2562511526964353,
	0.07196676003259866,
	-0.08179678276671708,
	0.39965159680959056,
	0.0406842402699849,
}

type syntheticVIXModel struct {
	Intercept float64
	Symbols   []string
	Weights   []float64
}

func isSyntheticVIXSymbol(symbol string) bool {
	return strings.EqualFold(strings.TrimSpace(symbol), "VIX")
}

func syntheticVIXFetchLimit(limit int) int {
	if limit <= 0 {
		return defaultBarLimit*4 + 1
	}
	return limit*4 + 1
}

func (s *USStocksService) mergeSyntheticVIXBars(ctx context.Context, tableName string, fromT, toT time.Time, session string, limit int, actual []dto.USStockBarRow) ([]dto.USStockBarRow, error) {
	model, err := s.loadSyntheticVIXModel(ctx)
	if err != nil {
		return nil, err
	}
	model = resolveSyntheticVIXModel(model)

	proxySeries, err := s.loadSyntheticVIXProxySeries(ctx, tableName, fromT, toT, session, limit, model.Symbols)
	if err != nil {
		return nil, err
	}
	if len(proxySeries) == 0 {
		return actual, nil
	}

	actualByTS := make(map[time.Time]dto.USStockBarRow, len(actual))
	merged := make([]dto.USStockBarRow, 0, len(actual)+len(proxySeries))
	for _, row := range actual {
		actualByTS[row.Timestamp] = row
	}

	for _, ts := range sortedSyntheticVIXTimestamps(proxySeries) {
		if row, ok := actualByTS[ts]; ok {
			merged = append(merged, row)
			continue
		}
		proxyRows, ok := proxySeries[ts]
		if !ok {
			continue
		}
		row, ok := buildSyntheticVIXBar(ts, model, proxyRows)
		if !ok {
			continue
		}
		merged = append(merged, row)
	}

	for _, row := range actual {
		if _, ok := proxySeries[row.Timestamp]; ok {
			continue
		}
		merged = append(merged, row)
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Timestamp.Before(merged[j].Timestamp)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (s *USStocksService) loadSyntheticVIXModel(ctx context.Context) (*syntheticVIXModel, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	query := `SELECT
	v.timestamp,
	toFloat64(v.close) AS vix,
	toFloat64(anyIf(p.close, p.symbol = 'VXX')) AS vxx,
	toFloat64(anyIf(p.close, p.symbol = 'UVXY')) AS uvxy,
	toFloat64(anyIf(p.close, p.symbol = 'SVXY')) AS svxy,
	toFloat64(anyIf(p.close, p.symbol = 'SVIX')) AS svix,
	toFloat64(anyIf(p.close, p.symbol = 'UVIX')) AS uvix,
	toFloat64(anyIf(p.close, p.symbol = 'VIXY')) AS vixy,
	toFloat64(anyIf(p.close, p.symbol = 'VIXM')) AS vixm,
	toFloat64(anyIf(p.close, p.symbol = 'VXZ')) AS vxz
FROM us_stocks_bar_1d v
INNER JOIN us_stocks_bar_1d p ON v.timestamp = p.timestamp
WHERE v.symbol = 'VIX'
	AND p.symbol IN ('VXX', 'UVXY', 'SVXY', 'SVIX', 'UVIX', 'VIXY', 'VIXM', 'VXZ')
GROUP BY v.timestamp, v.close
ORDER BY v.timestamp`
	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query synthetic VIX training rows: %w", err)
	}
	defer rows.Close()

	design := make([][]float64, 0, 128)
	response := make([]float64, 0, 128)
	for rows.Next() {
		var ts time.Time
		var vix float64
		proxies := make([]float64, len(syntheticVIXProxySymbols))
		scans := []any{&ts, &vix}
		for i := range proxies {
			scans = append(scans, &proxies[i])
		}
		if err := rows.Scan(scans...); err != nil {
			return nil, fmt.Errorf("scan synthetic VIX training row: %w", err)
		}
		if vix <= 0 {
			continue
		}
		row := make([]float64, 0, len(syntheticVIXProxySymbols)+1)
		row = append(row, 1)
		valid := true
		for _, proxy := range proxies {
			if proxy <= 0 || math.IsNaN(proxy) || math.IsInf(proxy, 0) {
				valid = false
				break
			}
			row = append(row, math.Log(proxy))
		}
		if !valid {
			continue
		}
		design = append(design, row)
		response = append(response, math.Log(vix))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate synthetic VIX training rows: %w", err)
	}
	if len(response) < len(syntheticVIXProxySymbols)+4 {
		return nil, nil
	}
	weights, ok := fitLogLinearModel(design, response)
	if !ok || len(weights) != len(syntheticVIXProxySymbols)+1 {
		return nil, nil
	}
	return &syntheticVIXModel{Intercept: weights[0], Symbols: append([]string(nil), syntheticVIXProxySymbols...), Weights: append([]float64(nil), weights[1:]...)}, nil
}

func resolveSyntheticVIXModel(model *syntheticVIXModel) *syntheticVIXModel {
	if model != nil && len(model.Symbols) > 0 && len(model.Symbols) == len(model.Weights) {
		return model
	}
	return defaultSyntheticVIXModel()
}

func defaultSyntheticVIXModel() *syntheticVIXModel {
	return &syntheticVIXModel{
		Intercept: syntheticVIXDefaultIntercept,
		Symbols:   append([]string(nil), syntheticVIXProxySymbols...),
		Weights:   append([]float64(nil), syntheticVIXDefaultWeights...),
	}
}

func (s *USStocksService) loadSyntheticVIXProxySeries(ctx context.Context, tableName string, fromT, toT time.Time, session string, limit int, symbols []string) (map[time.Time]map[string]dto.USStockBarRow, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	series := make(map[time.Time]map[string]dto.USStockBarRow)
	for _, symbol := range symbols {
		rows, err := s.queryBarRows(ctx, tableName, symbol, fromT, toT, session, limit)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			bucket := series[row.Timestamp]
			if bucket == nil {
				bucket = make(map[string]dto.USStockBarRow, len(symbols))
				series[row.Timestamp] = bucket
			}
			bucket[symbol] = row
		}
	}
	return series, nil
}

func sortedSyntheticVIXTimestamps(series map[time.Time]map[string]dto.USStockBarRow) []time.Time {
	keys := make([]time.Time, 0, len(series))
	for ts := range series {
		keys = append(keys, ts)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Before(keys[j])
	})
	return keys
}

func buildSyntheticVIXBar(ts time.Time, model *syntheticVIXModel, proxyRows map[string]dto.USStockBarRow) (dto.USStockBarRow, bool) {
	if model == nil {
		return dto.USStockBarRow{}, false
	}
	valuesOpen := make([]float64, 0, len(model.Symbols))
	valuesHigh := make([]float64, 0, len(model.Symbols))
	valuesLow := make([]float64, 0, len(model.Symbols))
	valuesClose := make([]float64, 0, len(model.Symbols))
	for _, symbol := range model.Symbols {
		row, ok := proxyRows[symbol]
		if !ok {
			return dto.USStockBarRow{}, false
		}
		if !positiveFloat32(row.Open) || !positiveFloat32(row.High) || !positiveFloat32(row.Low) || !positiveFloat32(row.Close) {
			return dto.USStockBarRow{}, false
		}
		valuesOpen = append(valuesOpen, float64(row.Open))
		valuesHigh = append(valuesHigh, float64(row.High))
		valuesLow = append(valuesLow, float64(row.Low))
		valuesClose = append(valuesClose, float64(row.Close))
	}
	open := predictSyntheticVIXValue(model, valuesOpen)
	high := predictSyntheticVIXValue(model, valuesHigh)
	low := predictSyntheticVIXValue(model, valuesLow)
	close := predictSyntheticVIXValue(model, valuesClose)
	if !(open > 0 && high > 0 && low > 0 && close > 0) {
		return dto.USStockBarRow{}, false
	}
	barHigh := maxFloat64(open, high, low, close)
	barLow := minFloat64(open, high, low, close)
	return dto.USStockBarRow{
		Timestamp:    ts,
		Symbol:       "VIX",
		Open:         float32(open),
		High:         float32(barHigh),
		Low:          float32(barLow),
		Close:        float32(close),
		Volume:       0,
		Transactions: 0,
	}, true
}

func predictSyntheticVIXValue(model *syntheticVIXModel, proxyValues []float64) float64 {
	if model == nil || len(proxyValues) != len(model.Weights) {
		return 0
	}
	total := model.Intercept
	for i, value := range proxyValues {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0
		}
		total += model.Weights[i] * math.Log(value)
	}
	predicted := math.Exp(total)
	if math.IsNaN(predicted) || math.IsInf(predicted, 0) || predicted <= 0 {
		return 0
	}
	return predicted
}

func fitLogLinearModel(design [][]float64, response []float64) ([]float64, bool) {
	if len(design) == 0 || len(design) != len(response) || len(design[0]) == 0 {
		return nil, false
	}
	cols := len(design[0])
	gram := make([][]float64, cols)
	for i := range gram {
		gram[i] = make([]float64, cols)
	}
	rhs := make([]float64, cols)
	for rowIdx, row := range design {
		if len(row) != cols {
			return nil, false
		}
		for i := 0; i < cols; i++ {
			rhs[i] += row[i] * response[rowIdx]
			for j := 0; j < cols; j++ {
				gram[i][j] += row[i] * row[j]
			}
		}
	}
	return solveLinearSystem(gram, rhs)
}

func solveLinearSystem(matrix [][]float64, rhs []float64) ([]float64, bool) {
	n := len(rhs)
	for col := 0; col < n; col++ {
		pivot := col
		for row := col + 1; row < n; row++ {
			if math.Abs(matrix[row][col]) > math.Abs(matrix[pivot][col]) {
				pivot = row
			}
		}
		if math.Abs(matrix[pivot][col]) < 1e-12 {
			return nil, false
		}
		matrix[col], matrix[pivot] = matrix[pivot], matrix[col]
		rhs[col], rhs[pivot] = rhs[pivot], rhs[col]
		pivotValue := matrix[col][col]
		for j := col; j < n; j++ {
			matrix[col][j] /= pivotValue
		}
		rhs[col] /= pivotValue
		for row := 0; row < n; row++ {
			if row == col {
				continue
			}
			factor := matrix[row][col]
			for j := col; j < n; j++ {
				matrix[row][j] -= factor * matrix[col][j]
			}
			rhs[row] -= factor * rhs[col]
		}
	}
	return rhs, true
}

func positiveFloat32(value float32) bool {
	v := float64(value)
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func maxFloat64(values ...float64) float64 {
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}

func minFloat64(values ...float64) float64 {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}
