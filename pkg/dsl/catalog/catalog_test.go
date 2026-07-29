package dslcatalog

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/bridge"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// TestDSLStrategiesParse verifies that every registered DSL strategy script
// can be parsed without errors by instantiating it through the catalog.
func TestDSLStrategiesParse(t *testing.T) {
	dslStrategies := []string{
		"golden-cross-dsl",
		"ema-atr-spot-dsl",
		"delta-filter-dsl",
		"strong-momentum-dsl",
		"daily-picks-dsl",
		"daily-picks-core-dsl",
		"value-allocation-dsl",
		"index-options-dsl",
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

func TestDailyPicksCoreDeclaresFixedSymbolDependencies(t *testing.T) {
	strategies, err := catalog.Resolve("daily-picks-core-dsl", catalog.Config{})
	if err != nil {
		t.Fatalf("Resolve daily-picks-core-dsl failed: %v", err)
	}
	strategy, ok := strategies[0].(*bridge.DslStrategy)
	if !ok {
		t.Fatalf("strategy type = %T, want *bridge.DslStrategy", strategies[0])
	}
	manifest := strategy.Manifest()
	for _, symbol := range []string{"SPY", "QQQ", "NVDA"} {
		for _, field := range []string{"hv30", "iv_percentile"} {
			found := false
			for _, request := range manifest.Requests {
				if request.Kind == "factor" && request.Symbol == symbol && request.Name == "volatility" && request.Field == field {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("factor requests = %+v, missing %s volatility.%s", manifest.Requests, symbol, field)
			}
		}
	}
	wantFundamentals := map[string]map[string]bool{
		"SPY":  {"pe10_live": true},
		"QQQ":  {"pe": true},
		"NVDA": {"pe": true, "pb": true},
	}
	for symbol, factors := range wantFundamentals {
		for factor := range factors {
			found := false
			for _, request := range manifest.Requests {
				if request.Kind == "fundamental" && request.Symbol == symbol && request.Name == factor {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("fundamental requests = %+v, missing %s %s", manifest.Requests, symbol, factor)
			}
		}
	}
	chains := manifest.OptionChainRequests()
	if len(chains) != 3 {
		t.Fatalf("option chain requests = %+v, want 3", chains)
	}
	for _, symbol := range []string{"SPY", "QQQ", "NVDA"} {
		found := false
		for _, chain := range chains {
			if chain.Symbol == symbol {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("option chain requests = %+v, missing %s", chains, symbol)
		}
	}
}

func TestRegisterDSLRejectsInvalidSource(t *testing.T) {
	if err := RegisterDSL("invalid-dsl-test", "x = @"); err == nil {
		t.Fatal("expected invalid DSL registration to fail")
	}
}

func TestStrategySamplesDeclareExpectedDataDependencies(t *testing.T) {
	tests := []struct {
		name         string
		universeCode string
		wantTemplate string
		wantSymbol   string
	}{
		{name: "strong-momentum-dsl", universeCode: "strong_momentum", wantTemplate: "factor"},
		{name: "daily-picks-dsl", universeCode: "daily_picks", wantTemplate: "fundamental"},
		{name: "value-allocation-dsl", universeCode: "value_allocation", wantTemplate: "fundamental"},
		{name: "index-options-dsl", wantSymbol: "SPY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategies, err := catalog.Resolve(tt.name, catalog.Config{})
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", tt.name, err)
			}
			strategy, ok := strategies[0].(*bridge.DslStrategy)
			if !ok {
				t.Fatalf("strategy type = %T, want *bridge.DslStrategy", strategies[0])
			}
			manifest := strategy.Manifest()
			if tt.universeCode != "" {
				requests := manifest.UniverseRequests()
				if len(requests) != 1 || requests[0].Code != tt.universeCode {
					t.Fatalf("universe requests = %+v, want %q", requests, tt.universeCode)
				}
				found := false
				for _, request := range manifest.UniverseRequestTemplatesForPreload() {
					if request.Kind == tt.wantTemplate {
						found = true
					}
				}
				if !found {
					t.Fatalf("universe request templates = %+v, want %s", manifest.UniverseRequestTemplatesForPreload(), tt.wantTemplate)
				}
			}
			if tt.wantSymbol != "" {
				found := false
				for _, request := range manifest.Requests {
					if request.Kind == "factor" && request.Symbol == tt.wantSymbol {
						found = true
					}
				}
				if !found {
					t.Fatalf("static factor requests = %+v, want %s", manifest.Requests, tt.wantSymbol)
				}
			}
		})
	}
}
