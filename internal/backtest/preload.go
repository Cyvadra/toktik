package backtest

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// PreloadContext provides mutable access to prepared datasets so strategies can
// precompute derived columns once during Engine.Prepare.
type PreloadContext struct {
	primaryRef SecurityRef
	securities []SecurityRef
	factors    []FactorRef
	byRef      map[SecurityRef]*PreloadSecurity
	byFactor   map[FactorRef]*PreloadSecurity
	params     map[string]interface{}
}

// PreloadSecurity exposes one security's prepared dataset.
type PreloadSecurity struct {
	ref      SecurityRef
	ds       *DataSet
	alignMap []int
}

func newPreloadContext(
	primaryRef SecurityRef,
	regs []securityRegistration,
	sets []*DataSet,
	alignMaps [][]int,
	factorRegs []factorRegistration,
	factorSets []*DataSet,
	factorAlignMaps [][]int,
	params map[string]interface{},
) *PreloadContext {
	refs := make([]SecurityRef, len(regs))
	byRef := make(map[SecurityRef]*PreloadSecurity, len(regs))
	for i, reg := range regs {
		refs[i] = reg.ref
		byRef[reg.ref] = &PreloadSecurity{ref: reg.ref, ds: sets[i], alignMap: alignMaps[i]}
	}

	factorRefs := make([]FactorRef, len(factorRegs))
	byFactor := make(map[FactorRef]*PreloadSecurity, len(factorRegs))
	for i, reg := range factorRegs {
		factorRefs[i] = reg.ref
		byFactor[reg.ref] = &PreloadSecurity{ds: factorSets[i], alignMap: factorAlignMaps[i]}
	}

	return &PreloadContext{
		primaryRef: primaryRef,
		securities: refs,
		factors:    factorRefs,
		byRef:      byRef,
		byFactor:   byFactor,
		params:     params,
	}
}

// PrimaryRef returns the primary security reference.
func (pc *PreloadContext) PrimaryRef() SecurityRef { return pc.primaryRef }

// SecurityRefs returns all registered security references in stable order.
func (pc *PreloadContext) SecurityRefs() []SecurityRef {
	out := make([]SecurityRef, len(pc.securities))
	copy(out, pc.securities)
	return out
}

// Primary returns preload access for the primary security.
func (pc *PreloadContext) Primary() *PreloadSecurity {
	return pc.byRef[pc.primaryRef]
}

// Security returns preload access for a specific security, or nil if missing.
func (pc *PreloadContext) Security(ref SecurityRef) *PreloadSecurity {
	return pc.byRef[ref]
}

// FactorRefs returns all registered external factor references in stable order.
func (pc *PreloadContext) FactorRefs() []FactorRef {
	out := make([]FactorRef, len(pc.factors))
	copy(out, pc.factors)
	return out
}

// Factor returns preload access for a specific external factor, or nil if missing.
func (pc *PreloadContext) Factor(ref FactorRef) *PreloadSecurity {
	return pc.byFactor[ref]
}

// Param returns a named parameter value.
func (pc *PreloadContext) Param(name string) interface{} {
	return pc.params[name]
}

// ColumnAlignedToPrimary returns a column aligned onto primary timestamps.
// For the primary security, this returns a copy of the original column.
// For secondary securities, values are mapped via alignMap and missing points are NaN.
func (pc *PreloadContext) ColumnAlignedToPrimary(ref SecurityRef, column string) ([]float64, error) {
	primary := pc.byRef[pc.primaryRef]
	if primary == nil || primary.ds == nil {
		return nil, fmt.Errorf("primary dataset unavailable")
	}

	sec := pc.byRef[ref]
	if sec == nil {
		return nil, fmt.Errorf("security %s/%s/%s not registered", ref.Market, ref.Symbol, ref.Interval)
	}

	col, err := sec.RequireColumn(column)
	if err != nil {
		return nil, err
	}

	if ref == pc.primaryRef || sec.alignMap == nil {
		out := make([]float64, len(col))
		copy(out, col)
		return out, nil
	}

	return alignColumn(col, sec.alignMap, primary.ds.Len), nil
}

// ColumnAlignedFactorToPrimary returns an external factor column aligned onto primary timestamps.
func (pc *PreloadContext) ColumnAlignedFactorToPrimary(ref FactorRef, column string) ([]float64, error) {
	primary := pc.byRef[pc.primaryRef]
	if primary == nil || primary.ds == nil {
		return nil, fmt.Errorf("primary dataset unavailable")
	}

	factor := pc.byFactor[ref]
	if factor == nil {
		return nil, fmt.Errorf("factor %s/%s not registered", ref.Name, ref.Interval)
	}

	col, err := factor.RequireColumn(column)
	if err != nil {
		return nil, err
	}

	if factor.alignMap == nil {
		out := make([]float64, len(col))
		copy(out, col)
		return out, nil
	}

	return alignColumn(col, factor.alignMap, primary.ds.Len), nil
}

func alignColumn(col []float64, alignMap []int, outLen int) []float64 {
	out := make([]float64, outLen)
	for i := 0; i < outLen; i++ {
		mapped := -1
		if i < len(alignMap) {
			mapped = alignMap[i]
		}
		if mapped < 0 || mapped >= len(col) {
			out[i] = math.NaN()
			continue
		}
		out[i] = col[mapped]
	}
	return out
}

// Ref returns the security reference.
func (ps *PreloadSecurity) Ref() SecurityRef { return ps.ref }

// Len returns number of bars in this dataset.
func (ps *PreloadSecurity) Len() int {
	if ps == nil || ps.ds == nil {
		return 0
	}
	return ps.ds.Len
}

// Timestamps returns the timestamp slice.
func (ps *PreloadSecurity) Timestamps() []time.Time {
	if ps == nil || ps.ds == nil {
		return nil
	}
	return ps.ds.Timestamps
}

// AlignMap returns primary-index to this-security-index mapping.
// Primary security returns nil.
func (ps *PreloadSecurity) AlignMap() []int {
	if ps == nil {
		return nil
	}
	return ps.alignMap
}

// Column returns a column by name or nil if missing.
func (ps *PreloadSecurity) Column(name string) []float64 {
	if ps == nil || ps.ds == nil {
		return nil
	}
	return ps.ds.Column(name)
}

// RequireColumn returns a column by name or an error if missing.
func (ps *PreloadSecurity) RequireColumn(name string) ([]float64, error) {
	col := ps.Column(name)
	if col == nil {
		return nil, fmt.Errorf("column %q not found for %s/%s/%s", name, ps.ref.Market, ps.ref.Symbol, ps.ref.Interval)
	}
	return col, nil
}

// SetColumn adds or replaces a derived column. Length must equal dataset length.
func (ps *PreloadSecurity) SetColumn(name string, values []float64) error {
	if ps == nil || ps.ds == nil {
		return fmt.Errorf("nil preload security")
	}
	if len(values) != ps.ds.Len {
		return fmt.Errorf("column %q length %d != dataset len %d", name, len(values), ps.ds.Len)
	}
	ps.ds.Columns[name] = values
	return nil
}

// MultiQuantile computes multiple rolling quantile columns for a source field.
// quantiles maps output column name -> quantile (q in [0,1]).
func (ps *PreloadSecurity) MultiQuantile(source string, period int, quantiles map[string]float64) error {
	if ps == nil || ps.ds == nil {
		return fmt.Errorf("nil preload security")
	}
	if period <= 0 {
		return fmt.Errorf("period must be > 0")
	}
	if len(quantiles) == 0 {
		return fmt.Errorf("quantiles cannot be empty")
	}

	sourceCol, err := ps.RequireColumn(source)
	if err != nil {
		return err
	}

	names := make([]string, 0, len(quantiles))
	qvals := make([]float64, 0, len(quantiles))
	for name, q := range quantiles {
		if name == "" {
			return fmt.Errorf("quantile output name cannot be empty")
		}
		names = append(names, name)
		qvals = append(qvals, q)
	}

	order := make([]int, len(names))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		return names[order[i]] < names[order[j]]
	})
	sortedNames := make([]string, len(names))
	sortedQs := make([]float64, len(qvals))
	for i, idx := range order {
		sortedNames[i] = names[idx]
		sortedQs[i] = qvals[idx]
	}

	series := computeRollingQuantiles(sourceCol, period, sortedQs)
	for i, name := range sortedNames {
		col := make([]float64, len(series[i]))
		copy(col, series[i])
		if err := ps.SetColumn(name, col); err != nil {
			return err
		}
	}
	return nil
}

// Quantile computes one rolling quantile column for a source field.
func (ps *PreloadSecurity) Quantile(outputName, source string, period int, q float64) error {
	return ps.MultiQuantile(source, period, map[string]float64{outputName: q})
}
