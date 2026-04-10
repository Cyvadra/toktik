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
