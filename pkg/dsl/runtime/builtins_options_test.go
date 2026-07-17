package runtime

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/Cyvadra/toktik/pkg/dsl/diagnostics"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
)

func TestSelectValueOptionStrategies(t *testing.T) {
	tests := []struct {
		name string
		ctx  MarketContext
		want []string
	}{
		{
			name: "undervalued high iv",
			ctx:  MarketContext{ValuationState: "undervalued", IVState: "high"},
			want: []string{OptionStrategySellPut, OptionStrategyCoveredCall},
		},
		{
			name: "undervalued low iv high hv",
			ctx:  MarketContext{ValuationState: "undervalued", IVState: "low", HVState: "high"},
			want: []string{OptionStrategyBuySkewedStraddle},
		},
		{
			name: "overvalued high iv",
			ctx:  MarketContext{ValuationState: "overvalued", IVState: "high"},
			want: []string{OptionStrategySellCall, OptionStrategyBearCallSpread},
		},
		{
			name: "fair high iv",
			ctx:  MarketContext{ValuationState: "fair", IVState: "high"},
			want: []string{OptionStrategyShortStrangle, OptionStrategyIronCondor},
		},
		{
			name: "mid iv no value match",
			ctx:  MarketContext{ValuationState: "fair", IVState: "mid"},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectOptionStrategies(tt.ctx, "value")
			if !sameStrings(got, tt.want) {
				t.Fatalf("strategies = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSelectIndexOptionStrategies(t *testing.T) {
	tests := []struct {
		name string
		ctx  MarketContext
		want []string
	}{
		{
			name: "range high hv",
			ctx:  MarketContext{TrendState: "range", HVState: "high"},
			want: []string{OptionStrategyIronCondor, OptionStrategyShortStrangle},
		},
		{
			name: "range low hv",
			ctx:  MarketContext{TrendState: "range", HVState: "low"},
			want: []string{OptionStrategyBuyStraddle, OptionStrategyCalendarSpread},
		},
		{
			name: "down",
			ctx:  MarketContext{TrendState: "down", HVState: "mid"},
			want: []string{OptionStrategyBuyPut, OptionStrategyBearCallSpread},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectOptionStrategies(tt.ctx, "index")
			if !sameStrings(got, tt.want) {
				t.Fatalf("strategies = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildExpandedOptionStrategyLegs(t *testing.T) {
	bridge := newTestOptionsBridge()
	tests := []struct {
		name  string
		want  int
		sides []string
	}{
		{name: OptionStrategyBuyPut, want: 1, sides: []string{"buy"}},
		{name: OptionStrategySellCall, want: 1, sides: []string{"sell"}},
		{name: OptionStrategyCoveredCall, want: 1, sides: []string{"sell"}},
		{name: OptionStrategyBearCallSpread, want: 2, sides: []string{"buy", "sell"}},
		{name: OptionStrategyShortStrangle, want: 2, sides: []string{"sell", "sell"}},
		{name: OptionStrategyIronCondor, want: 4, sides: []string{"buy", "sell", "sell", "buy"}},
		{name: OptionStrategyBuyStraddle, want: 2, sides: []string{"buy", "buy"}},
		{name: OptionStrategyBuySkewedStraddle, want: 2, sides: []string{"buy", "buy"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legs := buildOptionStrategyLegs(bridge, bridge.OptionsChain(), tt.name, 2, 0.35)
			if len(legs) != tt.want {
				t.Fatalf("leg count = %d, want %d: %#v", len(legs), tt.want, legs)
			}
			for i, leg := range legs {
				arr := leg.Array()
				if len(arr) != 3 {
					t.Fatalf("leg %d = %#v, want [contract side qty]", i, leg)
				}
				if got := arr[1].Str(); got != tt.sides[i] {
					t.Fatalf("leg %d side = %q, want %q", i, got, tt.sides[i])
				}
				if got := arr[2].Float(); got != 2 {
					t.Fatalf("leg %d qty = %g, want 2", i, got)
				}
			}
		})
	}
}

func TestBuildOptionStrategyLegsRejectsMixedExpiryVertical(t *testing.T) {
	bridge := &testOptionsBridge{chain: []testOptionContract{
		{symbol: "C105", underlying: "SPY", market: "us-stocks", right: "call", strike: 105, dte: 30, delta: 0.35, mark: 3},
		{symbol: "C110B", underlying: "SPY", market: "us-stocks", right: "call", strike: 110, dte: 60, delta: 0.18, mark: 2},
	}}
	legs := buildOptionStrategyLegs(bridge, bridge.OptionsChain(), OptionStrategyBearCallSpread, 1, 0.35)
	if len(legs) != 0 {
		t.Fatalf("expected mixed-expiry vertical to be rejected, got %#v", legs)
	}
}

func TestBuildOptionStrategyLegsBuildsCalendarAcrossExpiries(t *testing.T) {
	bridge := newTestOptionsBridge()
	legs := buildOptionStrategyLegs(bridge, bridge.OptionsChain(), OptionStrategyCalendarSpread, 1, 0.35)
	if len(legs) != 2 {
		t.Fatalf("expected calendar spread legs, got %#v", legs)
	}
	if got := legs[0].Array()[1].Str(); got != "sell" {
		t.Fatalf("front calendar leg side = %q, want sell", got)
	}
	if got := legs[1].Array()[1].Str(); got != "buy" {
		t.Fatalf("back calendar leg side = %q, want buy", got)
	}
}

func TestParseLegInputsRejectsMalformedLegs(t *testing.T) {
	contract := testOptionContract{symbol: "C105", underlying: "SPY", market: "us-stocks", right: "call", strike: 105, dte: 30, delta: 0.35, mark: 3}
	legs := parseLegInputs([]Value{
		ArrayVal([]Value{ObjVal(contract), StringVal("sell"), FloatVal(1)}),
		ArrayVal([]Value{ObjVal(contract), StringVal("hold"), FloatVal(1)}),
		ArrayVal([]Value{ObjVal(contract), StringVal("buy"), FloatVal(0)}),
		ArrayVal([]Value{ObjVal(nil), StringVal("buy"), FloatVal(1)}),
	})
	if len(legs) != 1 {
		t.Fatalf("valid leg count = %d, want 1: %#v", len(legs), legs)
	}
	if legs[0].Side != "sell" || legs[0].Qty != 1 || legs[0].Contract == nil {
		t.Fatalf("unexpected parsed leg: %#v", legs[0])
	}
}

// TestSpreadOpenFailureReportsDiagnosticAndSentinel guards against silent
// na results from failed spread/group creation builtins: a failed
// spread.open must (1) return the -1 sentinel rather than na/undefined-int,
// and (2) surface a dsl.builtin_call_failed diagnostic instead of vanishing.
func TestSpreadOpenFailureReportsDiagnosticAndSentinel(t *testing.T) {
	src := `id = spread.open([], "tag")`
	prog, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	ip := NewInterpreter(prog)
	// No ip.Bridge assigned: spread.open has no OptionsBridge to call.
	RegisterOptionsBuiltins(ip)
	var collected diagnostics.List
	ip.Diagnostics = &collected
	ip.Init()
	ip.OnBar()

	id, ok := ip.Global.Get("id")
	if !ok {
		t.Fatal("id not found")
	}
	if id.Float() != -1 {
		t.Fatalf("id = %v, want -1 sentinel for a failed spread.open", id.Float())
	}
	if !collected.HasErrors() {
		t.Fatal("expected a diagnostic for the failed spread.open call")
	}
	if collected[0].Code != "dsl.builtin_call_failed" || collected[0].Function != "spread.open" {
		t.Fatalf("unexpected diagnostic: %+v", collected[0])
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type testOptionContract struct {
	symbol     string
	underlying string
	market     string
	right      string
	strike     float64
	dte        float64
	delta      float64
	mark       float64
}

type testOptionsBridge struct {
	chain []testOptionContract
}

func newTestOptionsBridge() *testOptionsBridge {
	return &testOptionsBridge{chain: []testOptionContract{
		{symbol: "C100", underlying: "SPY", market: "us-stocks", right: "call", strike: 100, dte: 30, delta: 0.50, mark: 5},
		{symbol: "C105", underlying: "SPY", market: "us-stocks", right: "call", strike: 105, dte: 30, delta: 0.35, mark: 3},
		{symbol: "C110", underlying: "SPY", market: "us-stocks", right: "call", strike: 110, dte: 30, delta: 0.18, mark: 1.5},
		{symbol: "C105B", underlying: "SPY", market: "us-stocks", right: "call", strike: 105, dte: 60, delta: 0.34, mark: 4},
		{symbol: "P100", underlying: "SPY", market: "us-stocks", right: "put", strike: 100, dte: 30, delta: -0.50, mark: 5},
		{symbol: "P95", underlying: "SPY", market: "us-stocks", right: "put", strike: 95, dte: 30, delta: -0.35, mark: 3},
		{symbol: "P90", underlying: "SPY", market: "us-stocks", right: "put", strike: 90, dte: 30, delta: -0.18, mark: 1.5},
	}}
}

func (b *testOptionsBridge) OptionsChain() interface{} { return b.chain }
func (b *testOptionsBridge) OptionsChainFor(market, underlying string) interface{} {
	return b.chain
}
func (b *testOptionsBridge) ChainCalls(chain interface{}) interface{} {
	return filterTestOptions(chain, func(contract testOptionContract) bool { return contract.right == "call" })
}
func (b *testOptionsBridge) ChainPuts(chain interface{}) interface{} {
	return filterTestOptions(chain, func(contract testOptionContract) bool { return contract.right == "put" })
}
func (b *testOptionsBridge) ChainExpiryNearest(chain interface{}, targetDays int) interface{} {
	return chain
}
func (b *testOptionsBridge) ChainExpiryRange(chain interface{}, minDays, maxDays int) interface{} {
	return chain
}
func (b *testOptionsBridge) ChainExpiryMin(chain interface{}, minDays int) interface{} { return chain }
func (b *testOptionsBridge) ChainExpiryMax(chain interface{}, maxDays int) interface{} { return chain }
func (b *testOptionsBridge) ChainDeltaRange(chain interface{}, minDelta, maxDelta float64) interface{} {
	return filterTestOptions(chain, func(contract testOptionContract) bool {
		return contract.delta >= minDelta && contract.delta <= maxDelta
	})
}
func (b *testOptionsBridge) ChainMinPremium(chain interface{}, minBid float64) interface{} {
	return chain
}
func (b *testOptionsBridge) ChainStrikeRange(chain interface{}, min, max float64) interface{} {
	return filterTestOptions(chain, func(contract testOptionContract) bool {
		return contract.strike >= min && contract.strike <= max
	})
}
func (b *testOptionsBridge) ChainLen(chain interface{}) int { return len(asTestOptions(chain)) }
func (b *testOptionsBridge) ChainBestSpread(chain interface{}) interface{} {
	contracts := asTestOptions(chain)
	if len(contracts) == 0 {
		return nil
	}
	return contracts[0]
}
func (b *testOptionsBridge) ChainSortByDelta(chain interface{}, targetDelta float64) []interface{} {
	contracts := asTestOptions(chain)
	sort.SliceStable(contracts, func(i, j int) bool {
		return math.Abs(contracts[i].delta-targetDelta) < math.Abs(contracts[j].delta-targetDelta)
	})
	out := make([]interface{}, len(contracts))
	for i := range contracts {
		out[i] = contracts[i]
	}
	return out
}

func (b *testOptionsBridge) ContractSymbol(c interface{}) string { return asTestOption(c).symbol }
func (b *testOptionsBridge) ContractUnderlying(c interface{}) string {
	return asTestOption(c).underlying
}
func (b *testOptionsBridge) ContractMarket(c interface{}) string  { return asTestOption(c).market }
func (b *testOptionsBridge) ContractType(c interface{}) string    { return asTestOption(c).right }
func (b *testOptionsBridge) ContractStrike(c interface{}) float64 { return asTestOption(c).strike }
func (b *testOptionsBridge) ContractExpiry(c interface{}) float64 { return asTestOption(c).dte }
func (b *testOptionsBridge) ContractDTE(c interface{}) float64    { return asTestOption(c).dte }
func (b *testOptionsBridge) ContractDelta(c interface{}) float64  { return asTestOption(c).delta }
func (b *testOptionsBridge) ContractGamma(c interface{}) float64  { return 0 }
func (b *testOptionsBridge) ContractVega(c interface{}) float64   { return 0 }
func (b *testOptionsBridge) ContractTheta(c interface{}) float64  { return 0 }
func (b *testOptionsBridge) ContractIV(c interface{}) float64     { return 0 }
func (b *testOptionsBridge) ContractBid(c interface{}) float64    { return asTestOption(c).mark }
func (b *testOptionsBridge) ContractAsk(c interface{}) float64    { return asTestOption(c).mark }
func (b *testOptionsBridge) ContractMark(c interface{}) float64   { return asTestOption(c).mark }
func (b *testOptionsBridge) ContractVolume(c interface{}) float64 { return 100 }
func (b *testOptionsBridge) ContractOI(c interface{}) float64     { return 100 }

func (b *testOptionsBridge) OpenSpread(legs []SpreadLegInput, tag string) int { return 1 }
func (b *testOptionsBridge) OpenSpreadInGroup(legs []SpreadLegInput, tag string, groupID int) int {
	return 1
}
func (b *testOptionsBridge) CloseSpread(spreadID int)                          {}
func (b *testOptionsBridge) CloseSpreadWithReason(spreadID int, reason string) {}
func (b *testOptionsBridge) CloseSpreadLeg(spreadID, legIndex int, closePrice float64) bool {
	return false
}
func (b *testOptionsBridge) SpreadGet(spreadID int) SpreadInfo { return SpreadInfo{} }
func (b *testOptionsBridge) OpenSpreads() []int                { return nil }
func (b *testOptionsBridge) SpreadPnL(spreadID int) float64    { return 0 }
func (b *testOptionsBridge) SpreadLegContract(spreadID, legIndex int) interface{} {
	return nil
}
func (b *testOptionsBridge) SpreadLegEntryPrice(spreadID, legIndex int) float64 { return 0 }
func (b *testOptionsBridge) SpreadLegQty(spreadID, legIndex int) float64        { return 0 }
func (b *testOptionsBridge) SpreadLegSide(spreadID, legIndex int) string        { return "" }
func (b *testOptionsBridge) SpreadLegIsOpen(spreadID, legIndex int) bool        { return false }
func (b *testOptionsBridge) GroupOpen(tag string, initAmount, decayFactor float64) int {
	return 1
}
func (b *testOptionsBridge) GroupClose(groupID int) {}
func (b *testOptionsBridge) GroupGet(groupID int) GroupInfo {
	return GroupInfo{}
}
func (b *testOptionsBridge) GroupAddSpread(groupID, spreadID int)                   {}
func (b *testOptionsBridge) GroupIncrementRoll(groupID int)                         {}
func (b *testOptionsBridge) OpenGroups() []int                                      { return nil }
func (b *testOptionsBridge) ScheduleCloseSpread(triggerBarOffset int, spreadID int) {}
func (b *testOptionsBridge) ScheduleCloseSpreadWithReason(triggerBarOffset int, spreadID int, reason string) {
}
func (b *testOptionsBridge) ScheduleCloseLeg(triggerBarOffset int, spreadID, legIndex int) {}
func (b *testOptionsBridge) ScheduleCloseGroup(triggerBarOffset int, groupID int)          {}

func filterTestOptions(chain interface{}, keep func(testOptionContract) bool) []testOptionContract {
	contracts := asTestOptions(chain)
	out := make([]testOptionContract, 0, len(contracts))
	for _, contract := range contracts {
		if keep(contract) {
			out = append(out, contract)
		}
	}
	return out
}

func asTestOptions(chain interface{}) []testOptionContract {
	switch typed := chain.(type) {
	case []testOptionContract:
		return append([]testOptionContract(nil), typed...)
	case []interface{}:
		out := make([]testOptionContract, 0, len(typed))
		for _, item := range typed {
			out = append(out, asTestOption(item))
		}
		return out
	default:
		return nil
	}
}

func asTestOption(contract interface{}) testOptionContract {
	switch typed := contract.(type) {
	case testOptionContract:
		return typed
	case *testOptionContract:
		return *typed
	default:
		return testOptionContract{symbol: strings.TrimSpace("UNKNOWN")}
	}
}
