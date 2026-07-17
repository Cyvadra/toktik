package service

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
)

func TestExecuteIndicatorDSLPlotsSeries(t *testing.T) {
	bars := []indicatorBar{
		{Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 1, High: 2, Low: 0.5, Close: 1, Volume: 10},
		{Timestamp: time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), Open: 2, High: 3, Low: 1.5, Close: 2, Volume: 11},
		{Timestamp: time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC), Open: 3, High: 4, Low: 2.5, Close: 3, Volume: 12},
		{Timestamp: time.Date(2024, 1, 1, 3, 0, 0, 0, time.UTC), Open: 4, High: 5, Low: 3.5, Close: 4, Volume: 13},
	}

	result, err := executeIndicatorDSL(`strategy("Indicator API")
plot(close, title="Close")
plot(ta.sma(close, 3), title="SMA 3", precision=2)
plot(ta.rsi(close, 2), title="RSI 2", precision=2)
`, bars, nil, nil)
	if err != nil {
		t.Fatalf("executeIndicatorDSL returned error: %v", err)
	}
	if len(result.columns) != 3 {
		t.Fatalf("len(result.columns) = %d, want 3", len(result.columns))
	}

	closeSeries := result.series[result.columns[0].Source]
	if len(closeSeries) != len(bars) {
		t.Fatalf("len(closeSeries) = %d, want %d", len(closeSeries), len(bars))
	}
	if closeSeries[3] != 4 {
		t.Fatalf("closeSeries[3] = %v, want 4", closeSeries[3])
	}

	smaSeries := result.series[result.columns[1].Source]
	if !math.IsNaN(smaSeries[0]) || !math.IsNaN(smaSeries[1]) {
		t.Fatalf("unexpected SMA warmup values: %v", smaSeries[:2])
	}
	if smaSeries[2] != 2 || smaSeries[3] != 3 {
		t.Fatalf("unexpected SMA values: %v", smaSeries)
	}

	rsiSeries := result.series[result.columns[2].Source]
	if !math.IsNaN(rsiSeries[0]) || !math.IsNaN(rsiSeries[1]) {
		t.Fatalf("unexpected RSI warmup values: %v", rsiSeries[:2])
	}
	if rsiSeries[2] != 100 || rsiSeries[3] != 100 {
		t.Fatalf("unexpected RSI values: %v", rsiSeries)
	}
}

func TestBuildIndicatorDSLSourceFromIndicators(t *testing.T) {
	source, err := buildIndicatorDSLSource(dto.IndicatorSeriesRequest{Indicators: []string{"ta.sma(close,5)", " ta.rsi(close, 10) "}})
	if err != nil {
		t.Fatalf("buildIndicatorDSLSource returned error: %v", err)
	}
	if !strings.Contains(source, `plot(ta.sma(close,5), title="ta.sma(close,5)")`) {
		t.Fatalf("unexpected source: %s", source)
	}
	if !strings.Contains(source, `plot(ta.rsi(close, 10), title="ta.rsi(close, 10)")`) {
		t.Fatalf("unexpected source: %s", source)
	}
}

func TestBuildIndicatorDSLSourceFromPresets(t *testing.T) {
	source, err := buildIndicatorDSLSource(dto.IndicatorSeriesRequest{Presets: []string{"classic-volatility"}})
	if err != nil {
		t.Fatalf("buildIndicatorDSLSource returned error: %v", err)
	}
	if !strings.Contains(source, `plot(ta.atr(14), title="atr_14")`) {
		t.Fatalf("unexpected source: %s", source)
	}
	if !strings.Contains(source, `plot(ta.bb_upper(close,20,2.0), title="bb_upper_20_2")`) {
		t.Fatalf("unexpected source: %s", source)
	}
}

func TestBuildIndicatorDSLSourceRejectsUnknownPreset(t *testing.T) {
	_, err := buildIndicatorDSLSource(dto.IndicatorSeriesRequest{Presets: []string{"missing"}})
	if err == nil || !strings.Contains(err.Error(), "unknown indicator preset") {
		t.Fatalf("expected unknown preset error, got %v", err)
	}
}

func TestBuildIndicatorDSLSourceRejectsUnknownTAFunction(t *testing.T) {
	_, err := buildIndicatorDSLSource(dto.IndicatorSeriesRequest{Indicators: []string{"ta.not_a_real_function(close,5)"}})
	if err == nil || !strings.Contains(err.Error(), "unknown indicator function") {
		t.Fatalf("expected unknown indicator function error, got %v", err)
	}
}

func TestListIndicatorPresets(t *testing.T) {
	svc := &IndicatorService{}
	resp, err := svc.ListIndicatorPresets(t.Context())
	if err != nil {
		t.Fatalf("ListIndicatorPresets returned error: %v", err)
	}
	if len(resp.Presets) == 0 {
		t.Fatal("expected presets in response")
	}
	if resp.Presets[0].ID == "" || len(resp.Presets[0].Indicators) == 0 {
		t.Fatalf("unexpected preset response: %+v", resp.Presets[0])
	}
	if resp.Presets[0].Indicators[0].Key == "" || resp.Presets[0].Indicators[0].Expression == "" {
		t.Fatalf("unexpected preset indicator: %+v", resp.Presets[0].Indicators[0])
	}
}

func TestExecuteIndicatorDSLPlotsCCI(t *testing.T) {
	bars := []indicatorBar{
		{Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), High: 12, Low: 10, Close: 11},
		{Timestamp: time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC), High: 13, Low: 11, Close: 12},
		{Timestamp: time.Date(2024, 1, 1, 2, 0, 0, 0, time.UTC), High: 14, Low: 12, Close: 13},
	}

	result, err := executeIndicatorDSL(`plot(ta.cci(3), title="CCI 3")`, bars, nil, nil)
	if err != nil {
		t.Fatalf("executeIndicatorDSL returned error: %v", err)
	}
	cciSeries := result.series[result.columns[0].Source]
	if len(cciSeries) != len(bars) {
		t.Fatalf("len(cciSeries) = %d, want %d", len(cciSeries), len(bars))
	}
	if math.IsNaN(cciSeries[2]) {
		t.Fatalf("expected CCI value on latest bar, got %v", cciSeries)
	}
	if math.Abs(cciSeries[2]-100) > 1e-9 {
		t.Fatalf("cciSeries[2] = %v, want 100", cciSeries[2])
	}
}

func TestExecuteIndicatorDSLSupportsPineStyleATRAndCCIAndNestedPercentRank(t *testing.T) {
	bars := make([]indicatorBar, 0, 40)
	for i := 0; i < 40; i++ {
		close := float64(100 + i)
		bars = append(bars, indicatorBar{
			Timestamp: time.Date(2024, 1, 1, i, 0, 0, 0, time.UTC),
			Open:      close - 0.5,
			High:      close + 1,
			Low:       close - 1,
			Close:     close,
			Volume:    float64(1000 + i),
		})
	}

	result, err := executeIndicatorDSL(`
plot(ta.atr(high, low, close, 14), title="ATR Pine")
plot(ta.cci(high, low, close, 20), title="CCI Pine")
plot(ta.cci(hlc3,20), title="CCI HLC3")
plot(ta.percentrank(ta.rsi(close,14),20), title="RSI Rank")
`, bars, nil, nil)
	if err != nil {
		t.Fatalf("executeIndicatorDSL returned error: %v", err)
	}
	seriesByTitle := make(map[string][]float64, len(result.columns))
	for _, column := range result.columns {
		seriesByTitle[column.Title] = result.series[column.Source]
	}
	for _, key := range []string{"ATR Pine", "CCI Pine", "CCI HLC3", "RSI Rank"} {
		series := seriesByTitle[key]
		if len(series) != len(bars) {
			t.Fatalf("len(%s) = %d, want %d", key, len(series), len(bars))
		}
		if math.IsNaN(series[len(series)-1]) {
			t.Fatalf("expected final %s value, got %v", key, series)
		}
	}
}

func TestNormalizeIndicatorParams(t *testing.T) {
	numeric, stringsOut, err := normalizeIndicatorParams(map[string]interface{}{
		"Length":  14.0,
		"Enabled": true,
		"Mode":    "ema",
	})
	if err != nil {
		t.Fatalf("normalizeIndicatorParams returned error: %v", err)
	}
	if numeric["Length"] != 14 {
		t.Fatalf("numeric Length = %v, want 14", numeric["Length"])
	}
	if numeric["Enabled"] != 1 {
		t.Fatalf("numeric Enabled = %v, want 1", numeric["Enabled"])
	}
	if stringsOut["Mode"] != "ema" {
		t.Fatalf("stringsOut Mode = %q, want ema", stringsOut["Mode"])
	}
}

func TestEmptyIndicatorSeriesResponseUsesEmptySlices(t *testing.T) {
	resp := emptyIndicatorSeriesResponse("crypto-spot", "BTC", "1h")
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Market != "crypto-spot" || resp.Symbol != "BTC" || resp.Interval != "1h" {
		t.Fatalf("unexpected response metadata: %+v", resp)
	}
	if resp.Timestamps == nil || len(resp.Timestamps) != 0 {
		t.Fatalf("expected empty timestamps slice, got %+v", resp.Timestamps)
	}
	if resp.Series == nil || len(resp.Series) != 0 {
		t.Fatalf("expected empty series map, got %+v", resp.Series)
	}
}

func TestEncodeIndicatorValuesNaNBecomesNull(t *testing.T) {
	precision := 2
	values := encodeIndicatorValues([]float64{math.NaN(), 1.234, 1.235}, &precision)
	if len(values) != 3 {
		t.Fatalf("len(values) = %d, want 3", len(values))
	}
	if values[0] != nil {
		t.Fatalf("expected first value nil, got %+v", values[0])
	}
	if values[1] == nil || *values[1] != 1.23 {
		t.Fatalf("expected second value 1.23, got %+v", values[1])
	}
	if values[2] == nil || *values[2] != 1.24 {
		t.Fatalf("expected third value 1.24, got %+v", values[2])
	}
}
