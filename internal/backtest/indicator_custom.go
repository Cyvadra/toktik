package backtest

// Custom creates an indicator from an arbitrary function.
func Custom(deps []string, fn func(inputs map[string][]float64) []float64) Indicator {
	return &customIndicator{deps: deps, fn: fn}
}

type customIndicator struct {
	deps []string
	fn   func(inputs map[string][]float64) []float64
}

func (c *customIndicator) Deps() []string { return c.deps }

func (c *customIndicator) Compute(inputs map[string][]float64) []float64 {
	return c.fn(inputs)
}

// CustomOptional creates an indicator with both required and optional deps.
// Required deps cause an error if absent; optional deps are injected as
// all-NaN slices when absent, allowing the compute function to degrade
// gracefully.
func CustomOptional(required, optional []string, fn func(inputs map[string][]float64) []float64) Indicator {
	return &customOptionalIndicator{required: required, optional: optional, fn: fn}
}

type customOptionalIndicator struct {
	required []string
	optional []string
	fn       func(inputs map[string][]float64) []float64
}

func (c *customOptionalIndicator) Deps() []string         { return c.required }
func (c *customOptionalIndicator) OptionalDeps() []string { return c.optional }

func (c *customOptionalIndicator) Compute(inputs map[string][]float64) []float64 {
	return c.fn(inputs)
}
