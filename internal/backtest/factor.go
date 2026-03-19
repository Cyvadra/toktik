package backtest

import (
	"context"
	"time"
)

// FactorRequest describes what external factor data to load.
type FactorRequest struct {
	Name     string
	Interval string
	From     time.Time
	To       time.Time
}

// FactorFeed loads external, non-security-bound factor series.
type FactorFeed interface {
	Load(ctx context.Context, req FactorRequest) (*DataSet, error)
	Fields() []string
}

// FactorRef is an opaque handle to a registered factor source used in a backtest.
type FactorRef struct {
	Name     string
	Interval string
	Index    int
}

// factorRegistration captures an external factor request from Strategy.Init.
type factorRegistration struct {
	ref  FactorRef
	inds map[string]Indicator
}
