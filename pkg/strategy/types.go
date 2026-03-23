package strategy

import "github.com/Cyvadra/toktik/internal/backtest"

// Strategy aliases the core strategy contract for external package consumers.
type Strategy = backtest.Strategy

// ReportColumn aliases report column declarations for strategy outputs.
type ReportColumn = backtest.ReportColumn

// ReportColumnProvider aliases the optional report-column extension contract.
type ReportColumnProvider = backtest.ReportColumnProvider

// StrategyPreloader aliases the optional preload extension contract.
type StrategyPreloader = backtest.StrategyPreloader
