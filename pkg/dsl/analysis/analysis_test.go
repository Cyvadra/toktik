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
