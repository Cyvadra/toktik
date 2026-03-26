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
		positionPct:       defaultPositionPct,
		highestSinceEntry: math.NaN(),
	}
	ctx := backtest.NewSetupContext("spot", "BTCUSDT", "1h")
	if err := s.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
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
}
