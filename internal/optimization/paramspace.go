package optimization

import (
	"fmt"
	"math"
	"math/rand"
)

// ParamType identifies parameter value domains.
type ParamType string

const (
	ParamInt    ParamType = "int"
	ParamFloat  ParamType = "float"
	ParamChoice ParamType = "choice"
	ParamBool   ParamType = "bool"
)

// ParamSpec describes one tunable parameter's valid range.
type ParamSpec struct {
	Name    string    `json:"name"`
	Type    ParamType `json:"type"`
	Min     float64   `json:"min,omitempty"`
	Max     float64   `json:"max,omitempty"`
	Step    float64   `json:"step,omitempty"`
	Default float64   `json:"default,omitempty"`
	Choices []string  `json:"choices,omitempty"`
}

// Validate checks that the spec is internally consistent.
func (p ParamSpec) Validate() error {
	switch p.Type {
	case ParamInt, ParamFloat:
		if p.Min > p.Max {
			return fmt.Errorf("param %q: min (%v) > max (%v)", p.Name, p.Min, p.Max)
		}
		if p.Step < 0 {
			return fmt.Errorf("param %q: step must be >= 0", p.Name)
		}
	case ParamChoice:
		if len(p.Choices) == 0 {
			return fmt.Errorf("param %q: choice type requires at least one choice", p.Name)
		}
	case ParamBool:
		// no extra validation needed
	default:
		return fmt.Errorf("param %q: unknown type %q", p.Name, p.Type)
	}
	return nil
}

// GridValues returns all discrete values this parameter takes in a grid search.
func (p ParamSpec) GridValues() []interface{} {
	switch p.Type {
	case ParamInt:
		step := p.Step
		if step <= 0 {
			step = 1
		}
		var vals []interface{}
		for v := p.Min; v <= p.Max+1e-9; v += step {
			vals = append(vals, int(math.Round(v)))
		}
		return vals
	case ParamFloat:
		step := p.Step
		if step <= 0 {
			step = (p.Max - p.Min) / 10
			if step <= 0 {
				return []interface{}{p.Min}
			}
		}
		var vals []interface{}
		for v := p.Min; v <= p.Max+step*1e-9; v += step {
			vals = append(vals, v)
		}
		return vals
	case ParamChoice:
		vals := make([]interface{}, len(p.Choices))
		for i, c := range p.Choices {
			vals[i] = c
		}
		return vals
	case ParamBool:
		return []interface{}{false, true}
	}
	return nil
}

// RandomValue returns a uniformly random value from this parameter's domain.
func (p ParamSpec) RandomValue(rng *rand.Rand) interface{} {
	switch p.Type {
	case ParamInt:
		lo := int(math.Round(p.Min))
		hi := int(math.Round(p.Max))
		if hi <= lo {
			return lo
		}
		return lo + rng.Intn(hi-lo+1)
	case ParamFloat:
		return p.Min + rng.Float64()*(p.Max-p.Min)
	case ParamChoice:
		return p.Choices[rng.Intn(len(p.Choices))]
	case ParamBool:
		return rng.Intn(2) == 1
	}
	return nil
}

// StrategySpec describes the full parameter space for a strategy.
type StrategySpec struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Params  []ParamSpec `json:"params"`
}

// Validate checks all parameter specs.
func (s StrategySpec) Validate() error {
	seen := make(map[string]bool, len(s.Params))
	for _, p := range s.Params {
		if seen[p.Name] {
			return fmt.Errorf("duplicate param name %q", p.Name)
		}
		seen[p.Name] = true
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// DefaultParams returns a map of parameter names to their default values.
func (s StrategySpec) DefaultParams() map[string]interface{} {
	m := make(map[string]interface{}, len(s.Params))
	for _, p := range s.Params {
		switch p.Type {
		case ParamInt:
			m[p.Name] = int(math.Round(p.Default))
		case ParamFloat:
			m[p.Name] = p.Default
		case ParamChoice:
			if len(p.Choices) > 0 {
				m[p.Name] = p.Choices[0]
			}
		case ParamBool:
			m[p.Name] = p.Default != 0
		}
	}
	return m
}

// GridCombinations returns all parameter combinations for grid search.
// Returns nil if the total count exceeds maxCombinations (0 = unlimited).
func (s StrategySpec) GridCombinations(maxCombinations int) []map[string]interface{} {
	if len(s.Params) == 0 {
		return []map[string]interface{}{{}}
	}

	paramValues := make([][]interface{}, len(s.Params))
	total := 1
	for i, p := range s.Params {
		vals := p.GridValues()
		paramValues[i] = vals
		total *= len(vals)
		if maxCombinations > 0 && total > maxCombinations {
			return nil
		}
	}

	combos := make([]map[string]interface{}, 0, total)
	indices := make([]int, len(s.Params))

	for {
		m := make(map[string]interface{}, len(s.Params))
		for i, p := range s.Params {
			m[p.Name] = paramValues[i][indices[i]]
		}
		combos = append(combos, m)

		carry := true
		for i := len(indices) - 1; i >= 0 && carry; i-- {
			indices[i]++
			if indices[i] < len(paramValues[i]) {
				carry = false
			} else {
				indices[i] = 0
			}
		}
		if carry {
			break
		}
	}

	return combos
}

// RandomCombinations returns n random parameter sets sampled uniformly.
func (s StrategySpec) RandomCombinations(n int, seed int64) []map[string]interface{} {
	rng := rand.New(rand.NewSource(seed))
	combos := make([]map[string]interface{}, n)
	for i := range combos {
		m := make(map[string]interface{}, len(s.Params))
		for _, p := range s.Params {
			m[p.Name] = p.RandomValue(rng)
		}
		combos[i] = m
	}
	return combos
}
