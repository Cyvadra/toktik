package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

func TestFitLogLinearModelRecoversKnownCoefficients(t *testing.T) {
	design := [][]float64{
		{1, math.Log(10), math.Log(20)},
		{1, math.Log(11), math.Log(19)},
		{1, math.Log(12), math.Log(18)},
		{1, math.Log(13), math.Log(17)},
	}
	response := make([]float64, len(design))
	for i, row := range design {
		response[i] = 1.5 + 0.25*row[1] - 0.5*row[2]
	}
	weights, ok := fitLogLinearModel(design, response)
	if !ok {
		t.Fatal("fitLogLinearModel returned !ok")
	}
	if len(weights) != 3 {
		t.Fatalf("unexpected weight count: %d", len(weights))
	}
	if math.Abs(weights[0]-1.5) > 1e-9 || math.Abs(weights[1]-0.25) > 1e-9 || math.Abs(weights[2]+0.5) > 1e-9 {
		t.Fatalf("unexpected weights: %#v", weights)
	}
}

func TestBuildSyntheticVIXBarUsesProxyModel(t *testing.T) {
	model := &syntheticVIXModel{
		Intercept: math.Log(2),
		Symbols:   []string{"VXX", "SVXY"},
		Weights:   []float64{1, -1},
	}
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	bar, ok := buildSyntheticVIXBar(ts, model, map[string]dto.USStockBarRow{
		"VXX":  {Open: 40, High: 44, Low: 38, Close: 42},
		"SVXY": {Open: 20, High: 22, Low: 19, Close: 21},
	})
	if !ok {
		t.Fatal("buildSyntheticVIXBar returned !ok")
	}
	if bar.Symbol != "VIX" || !bar.Timestamp.Equal(ts) {
		t.Fatalf("unexpected synthetic bar identity: %#v", bar)
	}
	if math.Abs(float64(bar.Open)-4) > 1e-6 {
		t.Fatalf("unexpected open: %#v", bar)
	}
	if math.Abs(float64(bar.Close)-4) > 1e-6 {
		t.Fatalf("unexpected close: %#v", bar)
	}
	if bar.High < bar.Open || bar.High < bar.Close || bar.Low > bar.Open || bar.Low > bar.Close {
		t.Fatalf("unexpected OHLC bounds: %#v", bar)
	}
	if bar.Volume != 0 || bar.Transactions != 0 {
		t.Fatalf("expected synthetic bar to zero out volume and transactions: %#v", bar)
	}
}

func TestResolveSyntheticVIXModelFallsBackToDefault(t *testing.T) {
	model := resolveSyntheticVIXModel(nil)
	if model == nil {
		t.Fatal("expected fallback model")
	}
	if model.Intercept != syntheticVIXDefaultIntercept {
		t.Fatalf("unexpected fallback intercept: %v", model.Intercept)
	}
	if len(model.Symbols) != len(syntheticVIXProxySymbols) || len(model.Weights) != len(syntheticVIXDefaultWeights) {
		t.Fatalf("unexpected fallback dimensions: %#v", model)
	}
	for i, symbol := range syntheticVIXProxySymbols {
		if model.Symbols[i] != symbol {
			t.Fatalf("unexpected fallback symbol order: %#v", model.Symbols)
		}
		if model.Weights[i] != syntheticVIXDefaultWeights[i] {
			t.Fatalf("unexpected fallback weights: %#v", model.Weights)
		}
	}
}

func TestResolveSyntheticVIXModelKeepsFittedModel(t *testing.T) {
	fitted := &syntheticVIXModel{
		Intercept: 0.5,
		Symbols:   []string{"VXX", "UVXY"},
		Weights:   []float64{1.25, -0.75},
	}
	if got := resolveSyntheticVIXModel(fitted); got != fitted {
		t.Fatalf("expected fitted model to be preserved, got %#v", got)
	}
}

func TestQueryBarsIncludeLatestVIXSynthesizesFromLatestProxyBars(t *testing.T) {
	first := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	conn := &fakeForexConn{rowSets: []driver.Rows{
		&fakeForexRows{data: [][]any{{first, "VIX", float32(17), float32(18), float32(16), float32(17.5), 0.0, uint64(0)}}},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{},
		&fakeForexRows{data: [][]any{{first, float32(16.8), float32(18.2), float32(16.1), float32(17.7)}}},
	}}
	latestCache := NewLatestUSMarketCache(cache.NewMemoryStore(), time.Hour)
	for _, symbol := range syntheticVIXProxySymbols {
		if err := latestCache.StoreStockBars(context.Background(), symbol, "fmp", true, []LatestUSStockDailyBar{{
			Timestamp: latest,
			Symbol:    symbol,
			Open:      10,
			High:      12,
			Low:       9,
			Close:     11,
		}}); err != nil {
			t.Fatalf("StoreStockBars(%s) failed: %v", symbol, err)
		}
	}
	svc := NewUSStocksService(chrepo.NewRepo(conn)).WithLatestMarketCache(latestCache)

	resp, err := svc.QueryBars(context.Background(), dto.USStockBarRequest{Symbol: "VIX", Interval: "1d", From: "2026-07-01", To: "2026-07-09", IncludeLatest: true})
	if err != nil {
		t.Fatalf("QueryBars returned error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected two VIX rows, got %#v", resp.Data)
	}
	if !resp.Data[0].Timestamp.Equal(first) || resp.Data[0].Close != float32(17.7) {
		t.Fatalf("expected CBOE macro base row, got %#v", resp.Data[0])
	}
	if !resp.Data[1].Timestamp.Equal(latest) || resp.Data[1].Close <= 0 || resp.Data[1].Volume != 0 {
		t.Fatalf("unexpected latest synthetic VIX bar: %#v", resp.Data[1])
	}
}
