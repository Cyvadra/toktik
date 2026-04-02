// Package dsl provides a declarative strategy definition layer for the
// backtest engine.  It lets authors describe strategy logic without writing
// the registration + init boilerplate by hand.
//
// # Quick start
//
// A stateless single-security strategy (no mutable bar-by-bar state):
//
//	(&dsl.Spec{
//	    Name:   "my-strategy",
//	    Groups: []string{"trend"},
//	    Indicators: []dsl.IndicatorSpec{
//	        {Name: "sma_fast", Ind: backtest.SMA("close", 10)},
//	        {Name: "sma_slow", Ind: backtest.SMA("close", 50)},
//	        {Name: "buy_signal",  Ind: backtest.Crossover("sma_fast", "sma_slow")},
//	        {Name: "sell_signal", Ind: backtest.Crossunder("sma_fast", "sma_slow")},
//	    },
//	    OnBar: func(ctx *backtest.BarContext) {
//	        ref := ctx.PrimaryRef()
//	        if ctx.Ind("buy_signal") == 1 && ctx.Position(ref) == 0 {
//	            ctx.Buy(ref, dsl.QtyFromPctEquity(ctx, 0.95))
//	        }
//	        if ctx.Ind("sell_signal") == 1 && ctx.Position(ref) > 0 {
//	            ctx.ClosePosition(ref)
//	        }
//	    },
//	}).Register()
//
// A stateful strategy (mutable per-instance state) uses New instead of OnBar:
//
//	(&dsl.Spec{
//	    Name:   "trailing-stop",
//	    Groups: []string{"trend"},
//	    Indicators: []dsl.IndicatorSpec{
//	        {Name: "atr", Ind: backtest.ATR(14)},
//	    },
//	    New: func() *dsl.Instance {
//	        var highest float64 = math.NaN()
//	        return &dsl.Instance{
//	            OnBar: func(ctx *backtest.BarContext) {
//	                // use and update `highest`
//	            },
//	        }
//	    },
//	}).Register()
//
// A multi-timeframe strategy stores SecurityRef values in the Init hook:
//
//	(&dsl.Spec{
//	    Name: "mtf-example",
//	    Securities: []dsl.SecuritySpec{{
//	        Key: "htf", Market: "crypto-options", Symbol: "BTC-SPOT", Interval: "4h",
//	        Indicators: []dsl.IndicatorSpec{
//	            {Name: "sma200", Ind: backtest.SMA("close", 200)},
//	        },
//	    }},
//	    New: func() *dsl.Instance {
//	        var htfRef backtest.SecurityRef
//	        return &dsl.Instance{
//	            Init: func(_ *backtest.SetupContext, refs dsl.RefSet) error {
//	                htfRef = refs.MustSecurity("htf")
//	                return nil
//	            },
//	            OnBar: func(ctx *backtest.BarContext) {
//	                htf := ctx.Security(htfRef)
//	                _ = htf.Ind("sma200")
//	            },
//	        }
//	    },
//	}).Register()
package dsl

import (
	"fmt"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/pkg/strategies/catalog"
)

// IndicatorSpec declares a named indicator to be registered during Init.
type IndicatorSpec struct {
	// Name is the indicator key used in BarContext.Ind / SecurityAccessor.Ind.
	Name string
	// Ind is the indicator implementation.
	Ind backtest.Indicator
}

// SecuritySpec declares an additional market data series for cross-symbol or
// multi-timeframe access.  Each declared security is added via
// SetupContext.AddSecurity during Init, and the resulting SecurityRef is
// accessible in the Init and OnBar hooks via RefSet.
type SecuritySpec struct {
	// Key is an arbitrary string used to look up the SecurityRef via RefSet.
	Key string
	// Market, Symbol, Interval identify the data series (same semantics as
	// SetupContext.AddSecurity).
	Market   string
	Symbol   string
	Interval string
	// Indicators to register on this security.
	Indicators []IndicatorSpec
}

// FactorSpec declares an external factor series independent of any symbol.
type FactorSpec struct {
	// Key is an arbitrary string used to look up the FactorRef via RefSet.
	Key string
	// Name and Interval identify the factor series (same semantics as
	// SetupContext.AddFactor).
	Name     string
	Interval string
	// Indicators to register on this factor.
	Indicators []IndicatorSpec
}

// RefSet provides lookup of SecurityRef and FactorRef values by the Key
// declared in SecuritySpec / FactorSpec.  It is passed to Instance.Init.
type RefSet struct {
	secRefs map[string]backtest.SecurityRef
	facRefs map[string]backtest.FactorRef
}

// Security returns the SecurityRef for the given key, if present.
func (r RefSet) Security(key string) (backtest.SecurityRef, bool) {
	ref, ok := r.secRefs[key]
	return ref, ok
}

// MustSecurity returns the SecurityRef for the given key.
// Panics if the key was not declared in Spec.Securities.
func (r RefSet) MustSecurity(key string) backtest.SecurityRef {
	ref, ok := r.secRefs[key]
	if !ok {
		panic(fmt.Sprintf("dsl: unknown security key %q", key))
	}
	return ref
}

// Factor returns the FactorRef for the given key, if present.
func (r RefSet) Factor(key string) (backtest.FactorRef, bool) {
	ref, ok := r.facRefs[key]
	return ref, ok
}

// MustFactor returns the FactorRef for the given key.
// Panics if the key was not declared in Spec.Factors.
func (r RefSet) MustFactor(key string) backtest.FactorRef {
	ref, ok := r.facRefs[key]
	if !ok {
		panic(fmt.Sprintf("dsl: unknown factor key %q", key))
	}
	return ref
}

// Instance holds the per-strategy-instance callbacks produced by Spec.New.
// Use Instance when your OnBar logic requires mutable state that must be
// isolated across multiple strategy instantiations.
type Instance struct {
	// Init is called during strategy initialisation after all declarative
	// indicators and securities have been registered.  It may be nil.
	// Use it to capture SecurityRef / FactorRef values for later use in OnBar.
	Init func(ctx *backtest.SetupContext, refs RefSet) error

	// OnBar is called for every bar during replay.
	OnBar func(ctx *backtest.BarContext)
}

// Spec is a declarative strategy definition.
//
// Fill only the fields you need.  Either OnBar or New must be set.
type Spec struct {
	// --- Identity ---

	// Name is the canonical strategy name used for catalog registration.
	Name string
	// Aliases are additional names / CLI tokens that resolve to this strategy.
	Aliases []string
	// Groups are catalog group tags (e.g. "trend", "options", "spread").
	Groups []string
	// Profile describes whether the strategy trades options or regular assets.
	Profile catalog.StrategyProfile

	// --- Declarative indicators on the primary security ---

	// Indicators are registered on the primary security during Init.
	Indicators []IndicatorSpec

	// --- Additional data sources ---

	// Securities declares additional market data series for cross-symbol /
	// multi-timeframe access.
	Securities []SecuritySpec

	// Factors declares external factor series independent of any market symbol.
	Factors []FactorSpec

	// --- Strategy parameters ---

	// Params are named strategy parameters exposed via SetupContext.SetParam.
	Params map[string]interface{}

	// Warmup is additional historical data to request before the test window.
	Warmup time.Duration

	// --- Logic callbacks ---

	// New creates a fresh per-instance state bundle.  Called once each time the
	// catalog Factory instantiates the strategy.  Use this when OnBar requires
	// mutable state that must not be shared across runs.
	//
	// Either New or OnBar must be set; if both are set, New takes precedence.
	New func() *Instance

	// OnBar is the stateless variant.  Use when OnBar requires no mutable state
	// beyond what is already captured in indicators and context accessors.
	OnBar func(*backtest.BarContext)

	// ReportColumns declares per-bar chart column headers for HTML reports.
	// Implement this when strategy-specific series should appear in the data window.
	ReportColumns []backtest.ReportColumn
}

// Compile creates a new backtest.Strategy from the Spec.
// Each call returns an independent instance (safe to call multiple times).
func (s *Spec) Compile() backtest.Strategy {
	var inst *Instance
	if s.New != nil {
		inst = s.New()
	}
	return &dslStrategy{spec: s, inst: inst}
}

// Register compiles the Spec and registers it with the strategy catalog.
// Panics on errors (same semantics as catalog.Register).
func (s *Spec) Register() {
	spec := s // capture pointer for Factory closure
	catalog.Register(catalog.Registration{
		Name:    spec.Name,
		Aliases: spec.Aliases,
		Groups:  spec.Groups,
		Profile: spec.Profile,
		Factory: func(_ catalog.Config) (backtest.Strategy, error) {
			return spec.Compile(), nil
		},
	})
}

// RegisterWithConfig is like Register but the Factory receives the catalog.Config
// so the strategy can be parameterised from CLI / API inputs.
//
// The factory function may omit Name, Aliases, Groups, and Profile on the
// returned Spec; they are inherited from the outer Spec when empty.
func (s *Spec) RegisterWithConfig(factory func(cfg catalog.Config) (*Spec, error)) {
	name := s.Name
	aliases := s.Aliases
	groups := s.Groups
	profile := s.Profile
	catalog.Register(catalog.Registration{
		Name:    name,
		Aliases: aliases,
		Groups:  groups,
		Profile: profile,
		Factory: func(cfg catalog.Config) (backtest.Strategy, error) {
			spec, err := factory(cfg)
			if err != nil {
				return nil, err
			}
			// Inherit identity fields from the outer Spec when the factory
			// did not set them, so factory functions only need to declare
			// their logic-specific fields.
			if spec.Name == "" {
				spec.Name = name
			}
			if spec.Aliases == nil {
				spec.Aliases = aliases
			}
			if spec.Groups == nil {
				spec.Groups = groups
			}
			if spec.Profile == (catalog.StrategyProfile{}) {
				spec.Profile = profile
			}
			return spec.Compile(), nil
		},
	})
}

// --- internal implementation ---

type dslStrategy struct {
	spec *Spec
	inst *Instance // nil for stateless (OnBar-only) specs
}

func (d *dslStrategy) Name() string { return d.spec.Name }

func (d *dslStrategy) ReportColumns() []backtest.ReportColumn {
	if d.spec.ReportColumns == nil {
		return []backtest.ReportColumn{}
	}
	return d.spec.ReportColumns
}

func (d *dslStrategy) Init(ctx *backtest.SetupContext) error {
	// Register declared indicators on the primary security.
	for _, ind := range d.spec.Indicators {
		ctx.Register(ind.Name, ind.Ind)
	}

	// Add additional securities and their indicators.
	secRefs := make(map[string]backtest.SecurityRef, len(d.spec.Securities))
	for _, sec := range d.spec.Securities {
		ref := ctx.AddSecurity(sec.Market, sec.Symbol, sec.Interval)
		secRefs[sec.Key] = ref
		for _, ind := range sec.Indicators {
			ctx.RegisterOn(ref, ind.Name, ind.Ind)
		}
	}

	// Add external factor series and their indicators.
	facRefs := make(map[string]backtest.FactorRef, len(d.spec.Factors))
	for _, fac := range d.spec.Factors {
		ref := ctx.AddFactor(fac.Name, fac.Interval)
		facRefs[fac.Key] = ref
		for _, ind := range fac.Indicators {
			ctx.RegisterFactor(ref, ind.Name, ind.Ind)
		}
	}

	// Apply named parameters.
	for name, value := range d.spec.Params {
		ctx.SetParam(name, value)
	}

	// Apply warmup.
	if d.spec.Warmup > 0 {
		ctx.SetWarmup(d.spec.Warmup)
	}

	// Run the instance Init hook if present.
	if d.inst != nil && d.inst.Init != nil {
		refs := RefSet{secRefs: secRefs, facRefs: facRefs}
		return d.inst.Init(ctx, refs)
	}

	return nil
}

func (d *dslStrategy) OnBar(ctx *backtest.BarContext) {
	if d.inst != nil && d.inst.OnBar != nil {
		d.inst.OnBar(ctx)
		return
	}
	if d.spec.OnBar != nil {
		d.spec.OnBar(ctx)
	}
}
