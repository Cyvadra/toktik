package backtest

import (
	"context"
	"time"
)

// FactorRequest describes what external factor data to load.
//
// Market and Symbol are optional. When set, they identify a symbol-bound
// factor (e.g., a fundamental like PE for AAPL on us-stocks). Feeds that do
// not need symbol context may ignore them.
type FactorRequest struct {
	Name          string
	Interval      string
	Mode          string
	Market        string
	Symbol        string
	PrimaryMarket string
	PrimarySymbol string
	From          time.Time
	To            time.Time
}

// FactorFeed loads external factor series. Implementations may be
// symbol-agnostic (one global series per name) or symbol-aware (one series
// per market+symbol combination, sharing a single feed registration).
type FactorFeed interface {
	Load(ctx context.Context, req FactorRequest) (*DataSet, error)
	Fields() []string
}

// FactorRef is an opaque handle to a registered factor source used in a backtest.
//
// Market and Symbol are zero for symbol-agnostic factors and populated for
// symbol-bound ones (registered via SetupContext.AddSymbolFactor).
type FactorRef struct {
	Name     string
	Interval string
	Mode     string
	Market   string
	Symbol   string
	Index    int
}

// factorRegistration captures an external factor request from Strategy.Init.
type factorRegistration struct {
	ref  FactorRef
	inds map[string]Indicator
}
