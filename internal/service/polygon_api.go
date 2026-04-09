package service

import (
	"context"

	"github.com/Cyvadra/toktik/internal/dto"
)

func (s *PolygonService) QueryStockSnapshot(ctx context.Context, req dto.PolygonStockSnapshotRequest) (*dto.PolygonStockSnapshotResponse, error) {
	data, err := s.StockSnapshot(ctx, req.Symbol)
	if err != nil {
		return nil, err
	}
	return &dto.PolygonStockSnapshotResponse{Data: data}, nil
}

func (s *PolygonService) QueryStockAggregates(ctx context.Context, req dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error) {
	data, err := s.StockAggregates(ctx, req.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonAggregateResponse{Data: data}, nil
}

func (s *PolygonService) QueryStockQuotes(ctx context.Context, req dto.PolygonStockQuotesRequest) (*dto.PolygonQuoteResponse, error) {
	data, err := s.StockQuotes(ctx, req.Symbol, req.PolygonQuoteRequest.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonQuoteResponse{Data: data}, nil
}

func (s *PolygonService) QueryStockTrades(ctx context.Context, req dto.PolygonStockTradesRequest) (*dto.PolygonTradeResponse, error) {
	data, err := s.StockTrades(ctx, req.Symbol, req.PolygonTradeRequest.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonTradeResponse{Data: data}, nil
}

func (s *PolygonService) QueryOptionContract(ctx context.Context, req dto.PolygonOptionContractRequest) (*dto.PolygonOptionContractResponse, error) {
	data, err := s.OptionContract(ctx, req.Ticker)
	if err != nil {
		return nil, err
	}
	return &dto.PolygonOptionContractResponse{Data: data}, nil
}

func (s *PolygonService) QueryOptionChain(ctx context.Context, req dto.PolygonOptionChainRequest) (*dto.PolygonOptionChainResponse, error) {
	data, err := s.OptionChain(ctx, req.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonOptionChainResponse{Data: data}, nil
}

func (s *PolygonService) QueryOptionAggregates(ctx context.Context, req dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error) {
	data, err := s.OptionAggregates(ctx, req.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonAggregateResponse{Data: data}, nil
}

func (s *PolygonService) QueryOptionQuotes(ctx context.Context, req dto.PolygonOptionQuotesRequest) (*dto.PolygonQuoteResponse, error) {
	data, err := s.OptionQuotes(ctx, req.Ticker, req.PolygonQuoteRequest.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonQuoteResponse{Data: data}, nil
}

func (s *PolygonService) QueryOptionTrades(ctx context.Context, req dto.PolygonOptionTradesRequest) (*dto.PolygonTradeResponse, error) {
	data, err := s.OptionTrades(ctx, req.Ticker, req.PolygonTradeRequest.ToPolygon())
	if err != nil {
		return nil, err
	}
	return &dto.PolygonTradeResponse{Data: data}, nil
}
