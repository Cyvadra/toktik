package runtime

type Profile string

const (
	ProfileIndicator Profile = "indicator"
	ProfileBacktest  Profile = "backtest"
)

func RegisterProfile(ip *Interpreter, profile Profile) {
	RegisterTABuiltins(ip)
	RegisterMathBuiltins(ip)
	RegisterStrBuiltins(ip)
	RegisterStrategyBuiltins(ip)
	RegisterInputBuiltins(ip)
	if profile == ProfileIndicator {
		return
	}
	RegisterRequestBuiltins(ip, nil, nil)
	RegisterOptionsBuiltins(ip)
	RegisterAlphaBuiltins(ip)
	RegisterSignalBuiltins(ip)
	RegisterEventBuiltins(ip)
	RegisterOrderBuiltins(ip)
	RegisterConfigBuiltins(ip)
	RegisterPortfolioBuiltins(ip)
	RegisterRefBuiltins(ip)
}

func RegisterBacktestProfile(ip *Interpreter, requestSecurity func(args []Value) Value, requestFactor func(args []Value) Value) {
	RegisterTABuiltins(ip)
	RegisterMathBuiltins(ip)
	RegisterStrBuiltins(ip)
	RegisterStrategyBuiltins(ip)
	RegisterInputBuiltins(ip)
	RegisterRequestBuiltins(ip, requestSecurity, requestFactor)
	RegisterOptionsBuiltins(ip)
	RegisterAlphaBuiltins(ip)
	RegisterSignalBuiltins(ip)
	RegisterEventBuiltins(ip)
	RegisterOrderBuiltins(ip)
	RegisterConfigBuiltins(ip)
	RegisterPortfolioBuiltins(ip)
	RegisterRefBuiltins(ip)
}
