package api

import (
	"context"

	"github.com/Cyvadra/toktik/internal/backtest"
	"github.com/Cyvadra/toktik/internal/dto"
)

// CryptoOptionsQuerier defines the operations the API handler requires from
// the service layer. Accepting this interface instead of a concrete service
// allows handlers to be unit-tested with mocks.
type CryptoOptionsQuerier interface {
	QueryBars(ctx context.Context, req dto.BarRequest) (*dto.BarResponse, error)
	QuerySymbols(ctx context.Context, req dto.SymbolRequest) (*dto.SymbolResponse, error)
	QueryGreeks(ctx context.Context, req dto.GreeksRequest) (*dto.GreeksResponse, error)
	RunBacktest(ctx context.Context, req dto.BacktestRequest) (*backtest.Result, error)
}
