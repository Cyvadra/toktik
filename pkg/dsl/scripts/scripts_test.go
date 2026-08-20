package dslscripts

import (
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestReadStrategyValidatesName(t *testing.T) {
	tests := []struct {
		name      string
		wantError bool
	}{
		{name: " golden-cross.toktik ", wantError: false},
		{name: "", wantError: true},
		{name: "../golden-cross.toktik", wantError: true},
		{name: "nested/golden-cross.toktik", wantError: true},
		{name: "/golden-cross.toktik", wantError: true},
		{name: "golden-cross.txt", wantError: true},
		{name: "Golden-Cross.toktik", wantError: true},
		{name: "golden-\ncross.toktik", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := ReadStrategy(tt.name)
			if (err != nil) != tt.wantError {
				t.Fatalf("ReadStrategy(%q) error = %v, wantError = %v", tt.name, err, tt.wantError)
			}
			if !tt.wantError && content == "" {
				t.Fatal("ReadStrategy returned empty content")
			}
		})
	}
}

func TestCryptoIVSmileProbeParses(t *testing.T) {
	source, err := ReadStrategy("crypto-iv-smile-probe.toktik")
	if err != nil {
		t.Fatal(err)
	}
	if _, errors := parser.Parse(source); len(errors) > 0 {
		t.Fatalf("parse IV smile probe: %v", errors)
	}
}

func TestUSOptionMinIVStrikePercentilesParses(t *testing.T) {
	source, err := ReadStrategy("us-option-min-iv-strike-percentiles.toktik")
	if err != nil {
		t.Fatal(err)
	}
	if _, errors := parser.Parse(source); len(errors) > 0 {
		t.Fatalf("parse US option minimum IV strike percentiles: %v", errors)
	}
}

func TestVrpDynamicParses(t *testing.T) {
	source, err := ReadStrategy("vrp-dynamic.toktik")
	if err != nil {
		t.Fatal(err)
	}
	if _, errors := parser.Parse(source); len(errors) > 0 {
		t.Fatalf("parse VRP dynamic: %v", errors)
	}
}
