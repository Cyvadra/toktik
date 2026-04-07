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

// ReportColumn declares a primary-series column that should be exposed by
// backtest outputs for UI display such as the HTML report data window.
type ReportColumn struct {
	Source   string `json:"source"`
	Label    string `json:"label"`
	Decimals int    `json:"decimals,omitempty"`
	Overlay  bool   `json:"overlay,omitempty"`
}

// ReportColumnProvider is an optional strategy extension point for exposing
// strategy-specific per-bar values in reports.
type ReportColumnProvider interface {
	ReportColumns() []ReportColumn
}

// ReportSeriesProvider is an optional strategy extension point for exposing
// strategy-generated per-bar series that are not part of the prepared market
// dataset, such as DSL plot() outputs.
type ReportSeriesProvider interface {
	ReportSeries() map[string][]float64
}

// StrategyPreloader is an optional extension point for one-time precomputation.
//
// If implemented, Preload is called during Engine.Prepare after indicators are
// resolved and before replay starts. Strategies can use this hook to create
// additional derived columns once (instead of recomputing during OnBar).
type StrategyPreloader interface {
	Preload(ctx *PreloadContext) error
}
