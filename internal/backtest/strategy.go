package backtest

// Strategy is the user-facing interface for implementing trading strategies.
// Comparable to a Pine Script strategy body.
type Strategy interface {
	// Name returns a human-readable strategy identifier.
	Name() string

	// Init is called once before the backtest begins.
	// Use it to register indicators, request additional securities, and set parameters.
	Init(ctx *SetupContext) error

	// OnBar is called for each bar during the replay loop.
	// All indicators and cross-symbol data are already computed and aligned.
	OnBar(ctx *BarContext)
}
