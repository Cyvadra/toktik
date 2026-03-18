package optimization

import (
	"fmt"
	"testing"
)

func TestParamSpecValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ParamSpec
		wantErr bool
	}{
		{"valid int", ParamSpec{Name: "x", Type: ParamInt, Min: 1, Max: 10}, false},
		{"valid float", ParamSpec{Name: "x", Type: ParamFloat, Min: 0.1, Max: 1.0}, false},
		{"valid choice", ParamSpec{Name: "x", Type: ParamChoice, Choices: []string{"a", "b"}}, false},
		{"valid bool", ParamSpec{Name: "x", Type: ParamBool}, false},
		{"min > max", ParamSpec{Name: "x", Type: ParamInt, Min: 10, Max: 1}, true},
		{"empty choices", ParamSpec{Name: "x", Type: ParamChoice}, true},
		{"negative step", ParamSpec{Name: "x", Type: ParamInt, Min: 1, Max: 10, Step: -1}, true},
		{"unknown type", ParamSpec{Name: "x", Type: "unknown"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestParamSpecGridValues(t *testing.T) {
	p := ParamSpec{Name: "x", Type: ParamInt, Min: 1, Max: 5}
	vals := p.GridValues()
	if len(vals) != 5 {
		t.Errorf("expected 5 int values, got %d: %v", len(vals), vals)
	}

	p = ParamSpec{Name: "x", Type: ParamInt, Min: 0, Max: 10, Step: 3}
	vals = p.GridValues()
	if len(vals) != 4 {
		t.Errorf("expected 4 values (0,3,6,9), got %d: %v", len(vals), vals)
	}

	p = ParamSpec{Name: "x", Type: ParamFloat, Min: 0, Max: 1}
	vals = p.GridValues()
	if len(vals) != 11 {
		t.Errorf("expected 11 float values, got %d", len(vals))
	}

	p = ParamSpec{Name: "x", Type: ParamChoice, Choices: []string{"a", "b", "c"}}
	vals = p.GridValues()
	if len(vals) != 3 {
		t.Errorf("expected 3 choice values, got %d", len(vals))
	}

	p = ParamSpec{Name: "x", Type: ParamBool}
	vals = p.GridValues()
	if len(vals) != 2 {
		t.Errorf("expected 2 bool values, got %d", len(vals))
	}
}

func TestStrategySpecGridCombinations(t *testing.T) {
	spec := StrategySpec{
		Params: []ParamSpec{
			{Name: "a", Type: ParamInt, Min: 1, Max: 3},
			{Name: "b", Type: ParamChoice, Choices: []string{"x", "y"}},
		},
	}

	combos := spec.GridCombinations(0)
	if len(combos) != 6 {
		t.Fatalf("expected 6 combinations, got %d", len(combos))
	}

	seen := make(map[string]bool)
	for _, m := range combos {
		key := fmt.Sprintf("%v-%v", m["a"], m["b"])
		seen[key] = true
	}
	for _, e := range []string{"1-x", "1-y", "2-x", "2-y", "3-x", "3-y"} {
		if !seen[e] {
			t.Errorf("missing combination %q", e)
		}
	}
}

func TestStrategySpecGridCombinationsExceedsMax(t *testing.T) {
	spec := StrategySpec{
		Params: []ParamSpec{
			{Name: "a", Type: ParamInt, Min: 1, Max: 100},
			{Name: "b", Type: ParamInt, Min: 1, Max: 100},
		},
	}
	if combos := spec.GridCombinations(50); combos != nil {
		t.Error("expected nil when combinations exceed max")
	}
}

func TestStrategySpecRandomCombinations(t *testing.T) {
	spec := StrategySpec{
		Params: []ParamSpec{
			{Name: "a", Type: ParamInt, Min: 1, Max: 100},
			{Name: "b", Type: ParamFloat, Min: 0, Max: 1},
			{Name: "c", Type: ParamChoice, Choices: []string{"x", "y", "z"}},
		},
	}

	combos := spec.RandomCombinations(10, 42)
	if len(combos) != 10 {
		t.Fatalf("expected 10 combinations, got %d", len(combos))
	}

	for i, m := range combos {
		a, ok := m["a"].(int)
		if !ok || a < 1 || a > 100 {
			t.Errorf("combo[%d]: a=%v out of range or wrong type", i, m["a"])
		}
		b, ok := m["b"].(float64)
		if !ok || b < 0 || b > 1 {
			t.Errorf("combo[%d]: b=%v out of range or wrong type", i, m["b"])
		}
		c, ok := m["c"].(string)
		if !ok || (c != "x" && c != "y" && c != "z") {
			t.Errorf("combo[%d]: c=%v invalid", i, m["c"])
		}
	}

	// Deterministic with same seed
	combos2 := spec.RandomCombinations(10, 42)
	for i := range combos {
		if fmt.Sprintf("%v", combos[i]) != fmt.Sprintf("%v", combos2[i]) {
			t.Errorf("determinism broken at combo[%d]", i)
		}
	}
}

func TestStrategySpecValidate(t *testing.T) {
	dup := StrategySpec{
		Params: []ParamSpec{
			{Name: "x", Type: ParamInt, Min: 1, Max: 10},
			{Name: "x", Type: ParamFloat, Min: 0, Max: 1},
		},
	}
	if err := dup.Validate(); err == nil {
		t.Error("expected error for duplicate param names")
	}

	valid := StrategySpec{
		Params: []ParamSpec{
			{Name: "a", Type: ParamInt, Min: 1, Max: 10},
			{Name: "b", Type: ParamFloat, Min: 0, Max: 1},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultParams(t *testing.T) {
	spec := StrategySpec{
		Params: []ParamSpec{
			{Name: "n", Type: ParamInt, Default: 10},
			{Name: "rate", Type: ParamFloat, Default: 0.05},
			{Name: "mode", Type: ParamChoice, Choices: []string{"fast", "slow"}},
			{Name: "debug", Type: ParamBool, Default: 1},
		},
	}

	defaults := spec.DefaultParams()
	if v := defaults["n"].(int); v != 10 {
		t.Errorf("n=%v, want 10", v)
	}
	if v := defaults["rate"].(float64); v != 0.05 {
		t.Errorf("rate=%v, want 0.05", v)
	}
	if v := defaults["mode"].(string); v != "fast" {
		t.Errorf("mode=%v, want fast", v)
	}
	if v := defaults["debug"].(bool); v != true {
		t.Errorf("debug=%v, want true", v)
	}
}
