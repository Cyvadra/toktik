package main

import "testing"

func TestParseExampleMarket(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantMarket    string
		wantSymbol    string
		wantErrSubstr string
	}{
		{name: "default", input: "", wantMarket: "crypto-options", wantSymbol: "BTC-3JAN25-100000-C"},
		{name: "crypto", input: "crypto", wantMarket: "crypto", wantSymbol: "BTC"},
		{name: "us", input: "us", wantMarket: "us", wantSymbol: "AAPL"},
		{name: "forex", input: "forex", wantMarket: "forex", wantSymbol: "EURUSD"},
		{name: "invalid", input: "macro", wantErrSubstr: "unsupported --market"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMarket, gotSymbol, err := parseExampleMarket(tt.input)
			if tt.wantErrSubstr != "" {
				if err == nil || err.Error() == "" || gotMarket != "" || gotSymbol != "" {
					t.Fatalf("expected error for input %q, got market=%q symbol=%q err=%v", tt.input, gotMarket, gotSymbol, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseExampleMarket returned error: %v", err)
			}
			if gotMarket != tt.wantMarket || gotSymbol != tt.wantSymbol {
				t.Fatalf("parseExampleMarket(%q) = (%q, %q), want (%q, %q)", tt.input, gotMarket, gotSymbol, tt.wantMarket, tt.wantSymbol)
			}
		})
	}
}
