package runtime

type Profile string

const (
	ProfileIndicator Profile = "indicator"
	ProfileAlert     Profile = "alert"
	ProfileBacktest  Profile = "backtest"
)

func RegisterProfile(ip *Interpreter, profile Profile) {
	cfg := profileConfigFor(profile)
	registerProfile(ip, cfg)
}

func RegisterBacktestProfile(ip *Interpreter, requestSecurity func(args []Value) Value, requestFactor func(args []Value) Value, requestFundamental func(args []Value) Value) {
	cfg := profileConfigFor(ProfileBacktest)
	cfg.requestSecurity = requestSecurity
	cfg.requestFactor = requestFactor
	cfg.requestFundamental = requestFundamental
	registerProfile(ip, cfg)
}

type profileConfig struct {
	requestSecurity    func(args []Value) Value
	requestFactor      func(args []Value) Value
	requestFundamental func(args []Value) Value
	request            bool
	options            bool
	alpha              bool
	signal             bool
	event              bool
	order              bool
	config             bool
	portfolio          bool
	universe           bool
	market             bool
	ref                bool
}

func profileConfigFor(profile Profile) profileConfig {
	switch profile {
	case ProfileIndicator:
		return profileConfig{}
	case ProfileAlert:
		return profileConfig{
			request: true,
			alpha:   true,
			signal:  true,
			event:   true,
			order:   true,
			config:  true,
			ref:     true,
		}
	default:
		return profileConfig{
			request:   true,
			options:   true,
			alpha:     true,
			signal:    true,
			event:     true,
			order:     true,
			config:    true,
			portfolio: true,
			universe:  true,
			market:    true,
			ref:       true,
		}
	}
}

func registerProfile(ip *Interpreter, cfg profileConfig) {
	RegisterTABuiltins(ip)
	RegisterMathBuiltins(ip)
	RegisterStrBuiltins(ip)
	RegisterStrategyBuiltins(ip)
	RegisterInputBuiltins(ip)
	if cfg.request {
		RegisterRequestBuiltins(ip, cfg.requestSecurity, cfg.requestFactor, cfg.requestFundamental)
	}
	if cfg.options {
		RegisterOptionsBuiltins(ip)
	}
	if cfg.alpha {
		RegisterAlphaBuiltins(ip)
	}
	if cfg.signal {
		RegisterSignalBuiltins(ip)
	}
	if cfg.event {
		RegisterEventBuiltins(ip)
	}
	if cfg.order {
		RegisterOrderBuiltins(ip)
	}
	if cfg.config {
		RegisterConfigBuiltins(ip)
	}
	if cfg.portfolio {
		RegisterPortfolioBuiltins(ip)
	}
	if cfg.universe {
		RegisterUniverseBuiltins(ip)
	}
	if cfg.market {
		RegisterMarketBuiltins(ip)
	}
	if cfg.ref {
		RegisterRefBuiltins(ip)
	}
	RegisterTraceBuiltins(ip)
}
