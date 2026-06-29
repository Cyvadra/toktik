package datafeed

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
	usmacro "github.com/Cyvadra/toktik/internal/usmarket/macro"
)

type MacroSeriesQuerier interface {
	QuerySeries(ctx context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error)
}

type DVOLMacroFactorFeed struct {
	svc MacroSeriesQuerier
}

func NewDVOLMacroFactorFeed(svc MacroSeriesQuerier) *DVOLMacroFactorFeed {
	return &DVOLMacroFactorFeed{svc: svc}
}

func (f *DVOLMacroFactorFeed) Load(ctx context.Context, req backtest.FactorRequest) (*backtest.DataSet, error) {
	symbol := resolveDVOLMacroSymbol(req.Name)
	dataset, ok := usmacro.DeribitDVOLDatasetForSymbol(symbol)
	if !ok {
		return nil, fmt.Errorf("unsupported DVOL macro symbol %q", symbol)
	}
	resp, err := f.svc.QuerySeries(ctx, dto.MacroSeriesRequest{
		Dataset:  dataset,
		Factors:  []string{"open", "high", "low", "close"},
		From:     req.From.UTC().Format(time.RFC3339Nano),
		To:       req.To.UTC().Format(time.RFC3339Nano),
		AsOf:     req.To.UTC().Format(time.RFC3339Nano),
		Interval: req.Interval,
		Limit:    200000,
	})
	if err != nil {
		return nil, fmt.Errorf("load DVOL macro %s/%s: %w", dataset, req.Interval, err)
	}
	return macroSeriesOHLCDataSet(resp.Data), nil
}

func (f *DVOLMacroFactorFeed) Fields() []string {
	return []string{"open", "high", "low", "close"}
}

func resolveDVOLMacroSymbol(factorName string) string {
	for i := range factorName {
		if factorName[i] == ':' {
			return strings.ToUpper(strings.TrimSpace(factorName[i+1:]))
		}
	}
	return "BTC"
}

func macroSeriesOHLCDataSet(points []dto.MacroSeriesPoint) *backtest.DataSet {
	byTimestamp := map[time.Time]map[string]float64{}
	for _, point := range points {
		timestamp := point.Timestamp.UTC()
		if timestamp.IsZero() {
			timestamp = point.EventTS.UTC()
		}
		columns := byTimestamp[timestamp]
		if columns == nil {
			columns = map[string]float64{}
			byTimestamp[timestamp] = columns
		}
		columns[point.Factor] = point.Value
	}
	timestamps := make([]time.Time, 0, len(byTimestamp))
	for timestamp := range byTimestamp {
		timestamps = append(timestamps, timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i].Before(timestamps[j]) })
	ds := backtest.NewDataSet(len(timestamps))
	ds.SetTimestamps(timestamps)
	columns := map[string][]float64{
		"open":  make([]float64, len(timestamps)),
		"high":  make([]float64, len(timestamps)),
		"low":   make([]float64, len(timestamps)),
		"close": make([]float64, len(timestamps)),
	}
	for i, timestamp := range timestamps {
		values := byTimestamp[timestamp]
		for name, column := range columns {
			column[i] = values[name]
		}
	}
	for _, name := range []string{"open", "high", "low", "close"} {
		ds.AddColumn(name, columns[name])
	}
	return ds
}
