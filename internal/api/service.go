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
	QueryChain(ctx context.Context, req dto.CryptoOptionChainRequest) (*dto.CryptoOptionChainResponse, error)
}

// USStocksQuerier defines the operations needed for low-level US stock endpoints.
type USStocksQuerier interface {
	QueryBars(ctx context.Context, req dto.USStockBarRequest) (*dto.USStockBarResponse, error)
	QuerySymbols(ctx context.Context, req dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error)
	QuerySplits(ctx context.Context, req dto.USStockSplitRequest) (*dto.USStockSplitResponse, error)
	QueryProfiles(ctx context.Context, req dto.USStockProfileRequest) (*dto.USStockProfileResponse, error)
	QueryFundamentalMetrics(ctx context.Context, req dto.USStockFundamentalMetricsRequest) (*dto.USStockFundamentalMetricsResponse, error)
}

// USOptionsQuerier defines the operations needed for low-level US option endpoints.
type USOptionsQuerier interface {
	QueryBars(ctx context.Context, req dto.USOptionBarRequest) (*dto.USOptionBarResponse, error)
	QuerySymbols(ctx context.Context, req dto.USOptionSymbolRequest) (*dto.USOptionSymbolResponse, error)
	QueryGreeks(ctx context.Context, req dto.USOptionGreeksRequest) (*dto.USOptionGreeksResponse, error)
	QueryChain(ctx context.Context, req dto.USOptionChainRequest) (*dto.USOptionChainResponse, error)
	QueryOptionWall(ctx context.Context, req dto.USOptionWallRequest) (*dto.USOptionWallResponse, error)
}

// ForexQuerier defines the operations needed for low-level forex endpoints.
type ForexQuerier interface {
	QueryBars(ctx context.Context, req dto.ForexBarRequest) (*dto.ForexBarResponse, error)
	QuerySymbols(ctx context.Context, req dto.ForexSymbolRequest) (*dto.ForexSymbolResponse, error)
}

// InfraProvider describes non-business infrastructure endpoints exposed by the API.
type InfraProvider interface {
	Readiness(ctx context.Context) (*dto.ReadinessResponse, error)
	ListMarkets(ctx context.Context) (*dto.MarketCatalogResponse, error)
	ListDatasets(ctx context.Context, req dto.DatasetQueryRequest) (*dto.DatasetCatalogResponse, error)
}

// DataBrowserProvider exposes server-approved database inspection queries.
type DataBrowserProvider interface {
	ListBrowserPresets(ctx context.Context) (*dto.BrowserPresetResponse, error)
	QueryDatasetSchema(ctx context.Context, req dto.BrowserSchemaRequest) (*dto.BrowserSchemaResponse, error)
	QueryDatasetPreview(ctx context.Context, req dto.BrowserPreviewRequest) (*dto.BrowserPreviewResponse, error)
	QueryDatasetCoverage(ctx context.Context, req dto.BrowserCoverageRequest) (*dto.BrowserCoverageResponse, error)
	QueryFieldProfile(ctx context.Context, req dto.BrowserFieldProfileRequest) (*dto.BrowserFieldProfileResponse, error)
	QueryValidCount(ctx context.Context, req dto.BrowserValidCountRequest) (*dto.BrowserValidCountResponse, error)
	QueryDatasetValues(ctx context.Context, req dto.BrowserValueListRequest) (*dto.BrowserValueListResponse, error)
}

// FeatureProvider describes derived infra feature endpoints.
type FeatureProvider interface {
	QueryVolatilitySnapshot(ctx context.Context, req dto.FeatureVolatilitySnapshotRequest) (*dto.FeatureVolatilitySnapshotResponse, error)
	QueryVolatilityHistory(ctx context.Context, req dto.FeatureVolatilityHistoryRequest) (*dto.FeatureVolatilityHistoryResponse, error)
	QueryTermStructureSnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureTermStructureSnapshotResponse, error)
	QueryTermStructureHistory(ctx context.Context, req dto.FeatureTermStructureHistoryRequest) (*dto.FeatureTermStructureHistoryResponse, error)
	QuerySkewSnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureSkewSnapshotResponse, error)
	QuerySkewHistory(ctx context.Context, req dto.FeatureSkewHistoryRequest) (*dto.FeatureSkewHistoryResponse, error)
	QueryLiquiditySnapshot(ctx context.Context, req dto.FeatureSurfaceSnapshotRequest) (*dto.FeatureLiquiditySnapshotResponse, error)
	QueryLiquidityHistory(ctx context.Context, req dto.FeatureLiquidityHistoryRequest) (*dto.FeatureLiquidityHistoryResponse, error)
	QueryEventWindowSnapshot(ctx context.Context, req dto.FeatureUnderlyingSnapshotRequest) (*dto.FeatureEventWindowSnapshotResponse, error)
	QueryEventWindowHistory(ctx context.Context, req dto.FeatureUnderlyingHistoryRequest) (*dto.FeatureEventWindowHistoryResponse, error)
	QueryDailyFeaturePanel(ctx context.Context, req dto.FeatureDailyPanelRequest) (*dto.FeatureDailyPanelResponse, error)
}

// IndicatorSeriesProvider describes DSL-driven indicator sequence evaluation.
type IndicatorSeriesProvider interface {
	QueryIndicatorSeries(ctx context.Context, req dto.IndicatorSeriesRequest) (*dto.IndicatorSeriesResponse, error)
	ListIndicatorPresets(ctx context.Context) (*dto.IndicatorPresetCatalogResponse, error)
}

type StrategyBacktestProvider interface {
	ValidateStrategyBacktest(ctx context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestValidationResponse, error)
	StartStrategyBacktest(ctx context.Context, req dto.StrategyBacktestRunRequest) (*dto.StrategyBacktestRunAccepted, error)
	GetStrategyBacktestRun(ctx context.Context, runID string) (*dto.StrategyBacktestRunStatus, error)
	CancelStrategyBacktest(ctx context.Context, runID string) (*dto.StrategyBacktestRunStatus, error)
	SubscribeStrategyBacktest(ctx context.Context, runID string) (<-chan dto.StrategyBacktestSSEvent, func(), error)
}

// CryptoSpotQuerier defines operations for crypto spot market data.
type CryptoSpotQuerier interface {
	QueryBars(ctx context.Context, req dto.CryptoSpotBarRequest) (*dto.CryptoSpotBarResponse, error)
	QuerySymbols(ctx context.Context, req dto.CryptoSpotSymbolRequest) (*dto.CryptoSpotSymbolResponse, error)
}

// ScreenerProvider defines operations for underlying and options screening.
type ScreenerProvider interface {
	ScreenUnderlyings(ctx context.Context, req dto.ScreenUnderlyingRequest) (*dto.ScreenUnderlyingResponse, error)
	ScreenUSTurnoverIntersection(ctx context.Context, req dto.ScreenUSTurnoverIntersectionRequest) (*dto.ScreenUSTurnoverIntersectionResponse, error)
	ScreenOptions(ctx context.Context, req dto.ScreenOptionRequest) (*dto.ScreenOptionResponse, error)
}

type UniverseProvider interface {
	Members(ctx context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error)
	MemberIntervals(ctx context.Context, req dto.UniverseMembersRequest) (*dto.UniverseMembersResponse, error)
	Rebuild(ctx context.Context, req dto.UniverseRebuildRequest) (*dto.UniverseRebuildResponse, error)
}

// StrategyCatalogProvider defines operations for listing registered strategies.
type StrategyCatalogProvider interface {
	ListStrategies(ctx context.Context, req dto.StrategyCatalogListRequest) (*dto.StrategyCatalogResponse, error)
}

// FactorProvider defines operations for factor feed catalog and data queries.
type FactorProvider interface {
	ListFactors(ctx context.Context) (*dto.FactorCatalogResponse, error)
	QueryFactorBars(ctx context.Context, req dto.FactorBarRequest) (*dto.FactorBarResponse, error)
}

// FundamentalsProvider exposes symbol-bound fundamental factor queries
// (catalog, point-in-time series, snapshots, panels, freshness).
type FundamentalsProvider interface {
	ListFactors(ctx context.Context, req dto.FundamentalFactorCatalogRequest) (*dto.FundamentalFactorCatalogResponse, error)
	QuerySeries(ctx context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error)
	QuerySnapshot(ctx context.Context, req dto.FundamentalSnapshotRequest) (*dto.FundamentalSnapshotResponse, error)
	QueryPanel(ctx context.Context, req dto.FundamentalPanelRequest) (*dto.FundamentalPanelResponse, error)
	QueryFreshness(ctx context.Context, req dto.FundamentalFreshnessRequest) (*dto.FundamentalFreshnessResponse, error)
}

type MacroProvider interface {
	ListFactors(ctx context.Context, req dto.MacroFactorCatalogRequest) (*dto.MacroFactorCatalogResponse, error)
	QuerySeries(ctx context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error)
}

type FinanceCalendarProvider interface {
	QueryEconomicCalendar(ctx context.Context, req dto.EconomicCalendarRequest) (*dto.EconomicCalendarResponse, error)
	QueryStockCalendar(ctx context.Context, req dto.StockCalendarRequest) (*dto.StockCalendarResponse, error)
}

type PolygonProvider interface {
	QueryStockSnapshot(ctx context.Context, req dto.PolygonStockSnapshotRequest) (*dto.PolygonStockSnapshotResponse, error)
	QueryStockAggregates(ctx context.Context, req dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error)
	QueryStockQuotes(ctx context.Context, req dto.PolygonStockQuotesRequest) (*dto.PolygonQuoteResponse, error)
	QueryStockTrades(ctx context.Context, req dto.PolygonStockTradesRequest) (*dto.PolygonTradeResponse, error)
	QueryOptionContract(ctx context.Context, req dto.PolygonOptionContractRequest) (*dto.PolygonOptionContractResponse, error)
	QueryOptionChain(ctx context.Context, req dto.PolygonOptionChainRequest) (*dto.PolygonOptionChainResponse, error)
	QueryOptionAggregates(ctx context.Context, req dto.PolygonAggregateRequest) (*dto.PolygonAggregateResponse, error)
	QueryOptionQuotes(ctx context.Context, req dto.PolygonOptionQuotesRequest) (*dto.PolygonQuoteResponse, error)
	QueryOptionTrades(ctx context.Context, req dto.PolygonOptionTradesRequest) (*dto.PolygonTradeResponse, error)
}
