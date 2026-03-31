package emaatrspot

import (
	"math"
	"testing"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

func TestPositionPctOrDefault(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{name: "default", in: 0, want: defaultPositionPct},
		{name: "keep fraction", in: 0.4, want: 0.4},
		{name: "cap at one", in: 1.8, want: 1},
	}

	for _, tt := range tests {
		if got := positionPctOrDefault(tt.in); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestInitRegistersIndicators(t *testing.T) {
	s := &strategy{
		fastPeriod:        defaultFastPeriod,
		slowPeriod:        defaultSlowPeriod,
		atrPeriod:         defaultATRPeriod,
		atrMultiplier:     defaultATRMultiplier,
		volumePeriod:      defaultVolumePeriod,
		volumeRatioMin:    defaultVolumeRatio,
		positionPct:       defaultPositionPct,
		highestSinceEntry: math.NaN(),
	}
	ctx := backtest.NewSetupContext("spot", "BTCUSDT", "1h")
	if err := s.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
}

func TestShouldEnterLong(t *testing.T) {
	tests := []struct {
		name         string
		crossover    float64
		openPrice    float64
		closePrice   float64
		emaFast      float64
		emaSlow      float64
		volumeRatio  float64
		minVolRatio  float64
		wantDecision bool
	}{
		{name: "enter on breakout with volume", crossover: 1, openPrice: 100, closePrice: 105, emaFast: 103, emaSlow: 101, volumeRatio: 1.5, minVolRatio: 1.2, wantDecision: true},
		{name: "reject weak volume", crossover: 1, openPrice: 100, closePrice: 105, emaFast: 103, emaSlow: 101, volumeRatio: 1.05, minVolRatio: 1.2, wantDecision: false},
		{name: "reject bearish candle", crossover: 1, openPrice: 106, closePrice: 105, emaFast: 103, emaSlow: 101, volumeRatio: 1.5, minVolRatio: 1.2, wantDecision: false},
	}

	for _, tt := range tests {
		if got := shouldEnterLong(tt.crossover, tt.openPrice, tt.closePrice, tt.emaFast, tt.emaSlow, tt.volumeRatio, tt.minVolRatio); got != tt.wantDecision {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.wantDecision)
		}
	}
}

func TestPositionSizeFromBudget(t *testing.T) {
	tests := []struct {
		name        string
		cash        float64
		equity      float64
		price       float64
		positionPct float64
		want        float64
	}{
		{name: "use configured budget", cash: 1000, equity: 1000, price: 100, positionPct: 0.5, want: 5},
		{name: "cap by cash", cash: 300, equity: 1000, price: 100, positionPct: 0.5, want: 3},
		{name: "invalid price", cash: 1000, equity: 1000, price: 0, positionPct: 0.5, want: 0},
	}

	for _, tt := range tests {
		if got := positionSizeFromBudget(tt.cash, tt.equity, tt.price, tt.positionPct); got != tt.want {
			t.Fatalf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
}

func TestResolveUsesStrategySpecificDefaults(t *testing.T) {
	built, err := catalog.Resolve(defaultStrategyName, catalog.DefaultConfig())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(built) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(built))
	}

	resolved, ok := built[0].(*strategy)
	if !ok {
		t.Fatalf("resolved strategy type = %T, want *strategy", built[0])
	}
	if resolved.fastPeriod != defaultFastPeriod {
		t.Fatalf("fastPeriod = %d, want %d", resolved.fastPeriod, defaultFastPeriod)
	}
	if resolved.slowPeriod != defaultSlowPeriod {
		t.Fatalf("slowPeriod = %d, want %d", resolved.slowPeriod, defaultSlowPeriod)
	}
	if resolved.volumePeriod != defaultVolumePeriod {
		t.Fatalf("volumePeriod = %d, want %d", resolved.volumePeriod, defaultVolumePeriod)
	}
	if resolved.volumeRatioMin != defaultVolumeRatio {
		t.Fatalf("volumeRatioMin = %v, want %v", resolved.volumeRatioMin, defaultVolumeRatio)
	}
}
