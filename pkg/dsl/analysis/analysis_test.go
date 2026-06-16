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
