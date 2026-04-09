package dslcatalog

import (
	"strings"
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
		"buy-flash-low-dsl",
		"lvol-scalper-dsl",
		"covered-call-0330-tvsig-dsl",
		"dual-spreads-btc-volatility-dsl",
		"retracement-ratio-protective-spread-dsl",
		"ma-deviation-spread-outer-source-dsl",
		"turtle-trend-simp-dsl",
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

func TestRetracementDSLSignalSourceDefaultResolvesPaths(t *testing.T) {
	source, err := retracementDSLSignalSource(catalog.Config{})
	if err != nil {
		t.Fatalf("retracementDSLSignalSource() failed: %v", err)
	}
	if !strings.Contains(source, "data/signals/retracement_ratio_protective_spread/12h_short.csv") {
		t.Fatalf("default signal source missing short path: %q", source)
	}
	if !strings.Contains(source, "data/signals/retracement_ratio_protective_spread/12h_long.csv") {
		t.Fatalf("default signal source missing long path: %q", source)
	}
}
