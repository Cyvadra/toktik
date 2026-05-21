package service

import (
	"math"
	"testing"
	"time"

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
