package backtest

import (
	"strings"
	"testing"
)

type collisionMultiIndicator struct {
	outputs []string
}

func (indicator collisionMultiIndicator) Deps() []string { return []string{"close"} }

func (indicator collisionMultiIndicator) Compute(map[string][]float64) []float64 { return nil }

func (indicator collisionMultiIndicator) OutputNames(string) []string { return indicator.outputs }

func (indicator collisionMultiIndicator) ComputeMulti(string, map[string][]float64) map[string][]float64 {
	return map[string][]float64{"shared": {1}}
}

func TestResolveIndicatorsRejectsOutputCollision(t *testing.T) {
	registered := map[string]Indicator{
		"first":  collisionMultiIndicator{outputs: []string{"first", "shared"}},
		"second": collisionMultiIndicator{outputs: []string{"second", "shared"}},
	}
	err := resolveIndicators(registered, map[string][]float64{"close": {1}})
	if err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("resolveIndicators error = %v, want shared output collision", err)
	}
}

func TestResolveIndicatorsRejectsRawDataOverwrite(t *testing.T) {
	registered := map[string]Indicator{
		"derived": collisionMultiIndicator{outputs: []string{"close"}},
	}
	err := resolveIndicators(registered, map[string][]float64{"close": {1}})
	if err == nil || !strings.Contains(err.Error(), "raw data") {
		t.Fatalf("resolveIndicators error = %v, want raw data collision", err)
	}
}
