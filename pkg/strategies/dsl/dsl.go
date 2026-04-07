// Package dsl provides a declarative Spec-based DSL for defining trading strategies.
// It is inspired by TradingView's Pine Script and allows strategies to be expressed
// as a combination of indicators, entry conditions, exit conditions, and sizing rules
// without manually implementing the backtest.Strategy interface.
//
// Usage — stateless strategy:
//
//	dsl.Spec{
//	    Name:   "my-strategy",
//	    Groups: []string{"trend"},
//	    Indicators: []dsl.IndicatorDef{
//	        {Name: "sma20", Indicator: backtest.SMA("close", 20)},
//	    },
//	    EntryLong: []dsl.Condition{
//	        func(ctx *backtest.BarContext) bool { return ctx.Close() > ctx.Ind("sma20") },
//	    },
//	    ExitLong: []dsl.Condition{
//	        func(ctx *backtest.BarContext) bool { return ctx.Close() < ctx.Ind("sma20") },
//	    },
//	    Sizing: dsl.QtyFromPctEquity(0.95),
//	}.Register()
//
// Usage — config-parameterised strategy:
//
//	dsl.Spec{
//	    Name:   "my-param-strategy",
//	    Groups: []string{"trend"},
//	}.RegisterWithConfig(func(cfg catalog.Config) dsl.Spec {
//	    period := catalog.IntOrDefault(cfg.FastPeriod, 20)
//	    return dsl.Spec{
//	        Indicators: []dsl.IndicatorDef{
//	            {Name: "sma", Indicator: backtest.SMA("close", period)},
//	        },
//	        EntryLong: []dsl.Condition{
//	            func(ctx *backtest.BarContext) bool { return ctx.Close() > ctx.Ind("sma") },
//	        },
//	        ExitLong: []dsl.Condition{
//	            func(ctx *backtest.BarContext) bool { return ctx.Close() < ctx.Ind("sma") },
//	        },
//	        Sizing: dsl.QtyFromPctEquity(0.95),
//	    }
//	})
package dsl

import (
	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// IndicatorDef binds a name to an indicator definition for registration.
type IndicatorDef struct {
	Name      string
	Indicator backtest.Indicator
}

// Condition is a predicate evaluated on each bar. It returns true when the
// condition is satisfied (e.g. an entry or exit signal fires).
type Condition func(ctx *backtest.BarContext) bool

// SizingFunc computes the order quantity for an entry on the given bar.
// It receives the BarContext so it can access price, cash, and equity.
type SizingFunc func(ctx *backtest.BarContext) float64

// Spec is the declarative description of a strategy body, analogous to a
// Pine Script strategy definition. It covers indicators, entry/exit conditions,
// and position sizing — the three core concerns of a trend-following or
// signal-based strategy.
//
// Fields left at their zero value are treated as "not configured":
//   - An empty Indicators slice means no indicators are registered.
//   - An empty EntryLong/ExitLong slice means the respective side never fires.
//   - A nil Sizing means no order is placed even when an entry fires.
//   - TWAPBars <= 1 (or 0) means immediate market execution.
type Spec struct {
	// Registration metadata — required by the outer Spec; may be left empty in
	// inner Specs returned by RegisterWithConfig factories (they inherit from outer).
	Name    string
	Aliases []string
	Groups  []string
	Profile catalog.StrategyProfile

	// Indicators to register on the primary security during Init.
	Indicators []IndicatorDef

	// EntryLong holds entry conditions for long positions.
	// The first condition that returns true triggers a buy order.
	EntryLong []Condition

	// ExitLong holds exit conditions for long positions.
	// The first condition that returns true closes the long position.
	ExitLong []Condition

	// Sizing computes the buy quantity when an entry fires.
	// If nil, no order is placed even when an entry condition is true.
	Sizing SizingFunc

	// TWAPBars sets the TWAP execution window for entries (>1 = sliced, <=1 = immediate).
	TWAPBars int
}

// Register registers the Spec as a stateless strategy in the global catalog.
// All parameters come from the Spec itself; Config values are ignored.
func (s Spec) Register() {
	inner := s
	displayName := s.Name
	catalog.Register(catalog.Registration{
		Name:    s.Name,
		Aliases: s.Aliases,
		Groups:  s.Groups,
		Profile: s.Profile,
		Factory: func(_ catalog.Config) (backtest.Strategy, error) {
			return &dslStrategy{spec: inner, name: displayName}, nil
		},
	})
}

// RegisterWithConfig registers the Spec with a factory function that produces
// an inner Spec from a catalog.Config. The outer Spec's Name, Aliases, Groups,
// and Profile are used for registration; the inner Spec returned by factory
// carries the actual indicators and conditions (which may depend on cfg).
//
// If the inner Spec's Name is empty it inherits the outer Spec's Name.
func (s Spec) RegisterWithConfig(factory func(catalog.Config) Spec) {
	outerName := s.Name
	catalog.Register(catalog.Registration{
		Name:    s.Name,
		Aliases: s.Aliases,
		Groups:  s.Groups,
		Profile: s.Profile,
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			inner := factory(cfg)
			name := inner.Name
			if name == "" {
				name = outerName
			}
			return &dslStrategy{spec: inner, name: name}, nil
		},
	})
}

// dslStrategy is the runtime strategy produced by a Spec. It implements
// backtest.Strategy by executing the Spec's indicators and conditions.
type dslStrategy struct {
	spec Spec
	name string
}

// Name returns the human-readable strategy identifier.
func (d *dslStrategy) Name() string { return d.name }

// Init registers all declared indicators on the primary security.
func (d *dslStrategy) Init(ctx *backtest.SetupContext) error {
	for _, ind := range d.spec.Indicators {
		ctx.Register(ind.Name, ind.Indicator)
	}
	return nil
}

// OnBar is called once per bar. It evaluates entry and exit conditions and
// submits orders accordingly.
//
// Entry logic: if flat, iterate EntryLong conditions; the first true condition
// triggers a sized buy order (TWAP if TWAPBars > 1).
//
// Exit logic: if long, iterate ExitLong conditions; the first true condition
// closes the position.
func (d *dslStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	if ctx.Position(primary) == 0 {
		for _, cond := range d.spec.EntryLong {
			if cond(ctx) {
				if d.spec.Sizing == nil {
					break
				}
				qty := d.spec.Sizing(ctx)
				if qty <= 0 {
					break
				}
				if d.spec.TWAPBars > 1 {
					ctx.Order(primary).Buy().Qty(qty).TWAP(d.spec.TWAPBars).Submit()
				} else {
					ctx.Order(primary).Buy().Qty(qty).Submit()
				}
				break
			}
		}
	}

	if ctx.Position(primary) > 0 {
		for _, cond := range d.spec.ExitLong {
			if cond(ctx) {
				ctx.ClosePosition(primary)
				break
			}
		}
	}
}
