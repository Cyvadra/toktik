package dslcatalog

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// TestDSLStrategiesParse verifies that every registered DSL strategy script
// can be parsed without errors by instantiating it through the catalog.
func TestDSLStrategiesParse(t *testing.T) {
	dslStrategies := []string{
		"golden-cross-dsl",
		"ema-atr-spot-dsl",
		"delta-filter-dsl",
		"lvol-scalper-dsl",
	}

	for _, name := range dslStrategies {
		t.Run(name, func(t *testing.T) {
			strats, err := catalog.Resolve(name, catalog.Config{})
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", name, err)
			}
			if len(strats) == 0 {
				t.Fatalf("Resolve(%q) returned 0 strategies", name)
			}
		})
	}
}
