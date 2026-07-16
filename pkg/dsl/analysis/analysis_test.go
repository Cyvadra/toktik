package analysis

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestAnalyzeDetectsDynamicOptionChain(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Dynamic Chain")
symbols = portfolio.symbols()
for symbol in symbols {
  chain = options.chain("us", symbol)
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if !manifest.UsesOptions {
		t.Fatal("expected UsesOptions")
	}
	if !manifest.HasDynamicOptionChainRequest() {
		t.Fatal("expected dynamic option chain request")
	}
	if len(manifest.OptionChainRequests()) != 0 {
		t.Fatalf("expected no literal option chain requests, got %+v", manifest.OptionChainRequests())
	}
	if len(manifest.Diagnostics) == 0 || manifest.Diagnostics[0].Code != "dsl.dynamic_option_chain" {
		t.Fatalf("unexpected diagnostics: %+v", manifest.Diagnostics)
	}
}

func TestAnalyzeResolvesStringAliasOptionChain(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Static Alias Chain")
symbol = input.string("TSLA", title="Target")
chain = options.chain("us", symbol)
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if manifest.HasDynamicOptionChainRequest() {
		t.Fatalf("expected aliased input string option chain to be static: %+v", manifest.Requests)
	}
	requests := manifest.OptionChainRequests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 option chain request, got %+v", requests)
	}
	if requests[0].Market != "us" || requests[0].Symbol != "TSLA" || requests[0].Key != "us|TSLA" {
		t.Fatalf("unexpected option chain request: %+v", requests[0])
	}
	for _, diagnostic := range manifest.Diagnostics {
		if diagnostic.Code == "dsl.dynamic_option_chain" {
			t.Fatalf("unexpected dynamic chain diagnostic: %+v", manifest.Diagnostics)
		}
	}
}

func TestAnalyzeResolvesConfigStringUniverse(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Static Alias Universe")
code = config.string("universe_code", "strong_momentum")
symbols = universe.symbols(code)
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if manifest.HasDynamicUniverseRequest() {
		t.Fatalf("expected aliased config string universe to be static: %+v", manifest.Requests)
	}
	requests := manifest.UniverseRequests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 universe request, got %+v", requests)
	}
	if requests[0].Code != "strong_momentum" || requests[0].Key != "strong_momentum" {
		t.Fatalf("unexpected universe request: %+v", requests[0])
	}
}

func TestAnalyzeDetectsFundamentalRequests(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Fundamental Request")
pe = request.fundamental("us-stocks", "AAPL", "pe")
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if len(manifest.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(manifest.Requests))
	}
	req := manifest.Requests[0]
	if req.Kind != "fundamental" {
		t.Fatalf("expected fundamental request, got %+v", req)
	}
	if req.Name != "pe" || req.Market != "us-stocks" || req.Symbol != "AAPL" {
		t.Fatalf("unexpected fundamental request fields: %+v", req)
	}
	if req.Mode != "filled" {
		t.Fatalf("expected default filled mode, got %+v", req)
	}
	if req.Interval != "primary" {
		t.Fatalf("expected fundamental requests to use primary interval placeholder, got %+v", req)
	}
}

func TestAnalyzeDetectsNamedFundamentalRequests(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Named Fundamental Request")
pe = request.fundamental(symbol="AAPL", factor="pe", market="us-stocks")
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if len(manifest.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(manifest.Requests))
	}
	req := manifest.Requests[0]
	if req.Dynamic {
		t.Fatalf("expected named literal fundamental request to be static: %+v", req)
	}
	if req.Kind != "fundamental" || req.Name != "pe" || req.Market != "us-stocks" || req.Symbol != "AAPL" || req.Mode != "filled" {
		t.Fatalf("unexpected named fundamental request: %+v", req)
	}
}

func TestAnalyzeDetectsUniverseRequests(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Universe Request")
symbols = universe.symbols("strong_momentum")
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	requests := manifest.UniverseRequests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 universe request, got %d", len(requests))
	}
	if requests[0].Code != "strong_momentum" || requests[0].Key != "strong_momentum" {
		t.Fatalf("unexpected universe request: %+v", requests[0])
	}
}

func TestAnalyzeDetectsDynamicUniverseRequests(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Dynamic Universe")
code = close > open ? "strong_momentum" : "value_allocation"
symbols = universe.symbols(code)
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if !manifest.HasDynamicUniverseRequest() {
		t.Fatal("expected dynamic universe request")
	}
	found := false
	for _, diagnostic := range manifest.Diagnostics {
		if diagnostic.Code == "dsl.dynamic_universe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unexpected diagnostics: %+v", manifest.Diagnostics)
	}
}

func TestAnalyzeExtractsUniverseBoundRequestTemplates(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Universe Templates")
symbols = universe.symbols("strong_momentum")
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
  pe = request.fundamental("us-stocks", symbol, "pe", "percentile")
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	templates := manifest.UniverseRequestTemplatesForPreload()
	if len(templates) != 2 {
		t.Fatalf("template count = %d, want 2: %+v", len(templates), templates)
	}
	security, fundamental := templates[0], templates[1]
	if security.Kind != "security" || security.Market != "us-stocks" || security.Interval != "1d" || security.Field != "close" || security.UniverseCode != "strong_momentum" || security.Tier != RequestTierUniverseExpand {
		t.Fatalf("unexpected security template: %+v", security)
	}
	if fundamental.Kind != "fundamental" || fundamental.Market != "us-stocks" || fundamental.Name != "pe" || fundamental.Mode != "percentile" || fundamental.UniverseCode != "strong_momentum" || fundamental.Tier != RequestTierUniverseExpand {
		t.Fatalf("unexpected fundamental template: %+v", fundamental)
	}
}

func TestAnalyzeDoesNotExtractPortfolioLoopRequestTemplates(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Portfolio Loop")
symbols = portfolio.symbols()
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if templates := manifest.UniverseRequestTemplatesForPreload(); len(templates) != 0 {
		t.Fatalf("unexpected universe templates: %+v", templates)
	}
}

func TestAnalyzeExtractsNestedUniverseLoopRequestTemplates(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Nested Universe")
symbols = universe.symbols("strong_momentum")
if bar_index > 0 {
  for symbol in symbols {
    external_close = request.security("us-stocks", symbol, "1d", "close")
  }
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	templates := manifest.UniverseRequestTemplatesForPreload()
	if len(templates) != 1 {
		t.Fatalf("template count = %d, want 1: %+v", len(templates), templates)
	}
	if templates[0].Kind != "security" || templates[0].UniverseCode != "strong_momentum" || templates[0].Tier != RequestTierUniverseExpand {
		t.Fatalf("unexpected nested template: %+v", templates[0])
	}
}

func TestAnalyzeUniverseBoundRequestsAreNotReportedRuntimeDynamic(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Universe Not Dynamic")
symbols = universe.symbols("strong_momentum")
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	for _, diagnostic := range manifest.Diagnostics {
		if diagnostic.Code == "dsl.runtime_dynamic_request" {
			t.Fatalf("universe-bound request should not emit runtime-dynamic diagnostic: %+v", manifest.Diagnostics)
		}
	}
	var securityRequests int
	for _, request := range manifest.Requests {
		if request.Kind != "security" {
			continue
		}
		securityRequests++
		if request.Dynamic || request.Tier != RequestTierUniverseExpand {
			t.Fatalf("universe-bound security request classified as runtime-dynamic: %+v", request)
		}
	}
	if securityRequests != 1 {
		t.Fatalf("security request count = %d, want 1: %+v", securityRequests, manifest.Requests)
	}
}

func TestAnalyzeShadowedLoopVariableDropsUniverseScope(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Shadowed Loop")
symbols = universe.symbols("strong_momentum")
others = ["A", "B"]
for symbol in symbols {
  for symbol in others {
    external_close = request.security("us-stocks", symbol, "1d", "close")
  }
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if templates := manifest.UniverseRequestTemplatesForPreload(); len(templates) != 0 {
		t.Fatalf("shadowed loop variable must not yield universe templates: %+v", templates)
	}
}

func TestAnalyzeWithParamsUsesUniverseOverride(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Parameterized Universe")
code = input.string("strong_momentum", title="Universe")
symbols = universe.symbols(code)
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := AnalyzeWithParams(prog, map[string]interface{}{"Universe": "value_allocation"})
	requests := manifest.UniverseRequests()
	if len(requests) != 1 || requests[0].Code != "value_allocation" {
		t.Fatalf("universe requests = %+v, want value_allocation", requests)
	}
	templates := manifest.UniverseRequestTemplatesForPreload()
	if len(templates) != 1 || templates[0].UniverseCode != "value_allocation" {
		t.Fatalf("templates = %+v, want value_allocation", templates)
	}
}

func TestAnalyzeReassignedUniverseCollectionIsRuntimeDynamic(t *testing.T) {
	prog, errs := parser.Parse(`strategy("Reassigned Universe")
symbols = universe.symbols("strong_momentum")
symbols := universe.symbols("value_allocation")
for symbol in symbols {
  external_close = request.security("us-stocks", symbol, "1d", "close")
}
`)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	manifest := Analyze(prog)
	if templates := manifest.UniverseRequestTemplatesForPreload(); len(templates) != 0 {
		t.Fatalf("reassigned collection produced unsafe templates: %+v", templates)
	}
}
