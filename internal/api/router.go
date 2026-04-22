package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// NewRouter builds the Gin engine with all API routes registered.
func NewRouter(cos CryptoOptionsQuerier, usStocks USStocksQuerier, usOptions USOptionsQuerier, infra InfraProvider, features FeatureProvider, indicators IndicatorSeriesProvider, strategyBacktests StrategyBacktestProvider, cryptoSpot CryptoSpotQuerier, screener ScreenerProvider, strategyCatalog StrategyCatalogProvider, factors FactorProvider, polygon ...PolygonProvider) *gin.Engine {
	r := gin.Default()
	var polygonProvider PolygonProvider
	if len(polygon) > 0 {
		polygonProvider = polygon[0]
	}
	h := NewHandler(cos, usStocks, usOptions, infra, features, indicators, strategyBacktests, cryptoSpot, screener, strategyCatalog, factors, polygonProvider)

	// Apply middleware
	r.Use(CORSMiddleware())
	// TODO: Apply APIKeyAuth() middleware once API keys are configured in production.
	// TODO: Apply RateLimitMiddleware() to protect against abuse.
	// r.Use(APIKeyAuth())
	// r.Use(RateLimitMiddleware())

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/ready", h.GetReadiness)

	v1 := r.Group("/api/v1")
	{
		backtestsGroup := v1.Group("/backtests")
		backtestsGroup.POST("/validate", h.ValidateStrategyBacktest)
		backtestsGroup.POST("/runs", h.StartStrategyBacktest)
		backtestsGroup.GET("/runs/:runID", h.GetStrategyBacktestRun)
		backtestsGroup.GET("/runs/:runID/events", h.StreamStrategyBacktestEvents)
		backtestsGroup.GET("/runs/:runID/report", h.GetStrategyBacktestReport)
		backtestsGroup.GET("/runs/:runID/reports/:reportID", h.GetStrategyBacktestNamedReport)

		infraGroup := v1.Group("/infra")
		infraGroup.GET("/markets", h.GetMarkets)
		infraGroup.GET("/datasets", h.GetDatasets)

		featuresGroup := v1.Group("/features")
		featuresGroup.GET("/volatility-snapshot", h.GetVolatilitySnapshot)
		featuresGroup.GET("/volatility-history", h.GetVolatilityHistory)
		featuresGroup.GET("/term-structure-snapshot", h.GetTermStructureSnapshot)
		featuresGroup.GET("/term-structure-history", h.GetTermStructureHistory)
		featuresGroup.GET("/skew-snapshot", h.GetSkewSnapshot)
		featuresGroup.GET("/skew-history", h.GetSkewHistory)
		featuresGroup.GET("/liquidity-snapshot", h.GetLiquiditySnapshot)
		featuresGroup.GET("/liquidity-history", h.GetLiquidityHistory)
		featuresGroup.GET("/event-window-snapshot", h.GetEventWindowSnapshot)
		featuresGroup.GET("/event-window-history", h.GetEventWindowHistory)
		featuresGroup.GET("/daily-feature-panel", h.GetDailyFeaturePanel)

		indicatorsGroup := v1.Group("/indicators")
		indicatorsGroup.GET("/presets", h.ListIndicatorPresets)
		indicatorsGroup.POST("/series", h.RunIndicatorSeries)

		co := v1.Group("/crypto-options")
		co.GET("/bars", h.GetBars)
		co.GET("/symbols", h.GetSymbols)
		co.GET("/greeks", h.GetGreeks)
		co.GET("/chain", h.GetCryptoOptionChain)
		co.POST("/backtest", h.RunBacktest)

		markets := v1.Group("/markets")
		marketCryptoOptions := markets.Group("/crypto-options")
		marketCryptoOptions.GET("/bars", h.GetBars)
		marketCryptoOptions.GET("/symbols", h.GetSymbols)
		marketCryptoOptions.GET("/greeks", h.GetGreeks)
		marketCryptoOptions.GET("/chain", h.GetCryptoOptionChain)
		marketCryptoOptions.POST("/backtest", h.RunBacktest)

		marketCryptoSpot := markets.Group("/crypto-spot")
		marketCryptoSpot.GET("/bars", h.GetCryptoSpotBars)
		marketCryptoSpot.GET("/symbols", h.GetCryptoSpotSymbols)

		marketUSStocks := markets.Group("/us-stocks")
		marketUSStocks.GET("/bars", h.GetUSStockBars)
		marketUSStocks.GET("/symbols", h.GetUSStockSymbols)

		marketUSOptions := markets.Group("/us-options")
		marketUSOptions.GET("/bars", h.GetUSOptionBars)
		marketUSOptions.GET("/symbols", h.GetUSOptionSymbols)
		marketUSOptions.GET("/greeks", h.GetUSOptionGreeks)
		marketUSOptions.GET("/chain", h.GetUSOptionChain)

		screenerGroup := v1.Group("/screener")
		screenerGroup.GET("/underlyings", h.ScreenUnderlyings)
		screenerGroup.GET("/options", h.ScreenOptions)

		factorsGroup := v1.Group("/factors")
		factorsGroup.GET("", h.ListFactors)
		factorsGroup.GET("/bars", h.GetFactorBars)

		polygonGroup := v1.Group("/polygon")
		polygonStocks := polygonGroup.Group("/stocks")
		polygonStocks.GET("/snapshot", h.GetPolygonStockSnapshot)
		polygonStocks.GET("/aggregates", h.GetPolygonStockAggregates)
		polygonStocks.GET("/quotes", h.GetPolygonStockQuotes)
		polygonStocks.GET("/trades", h.GetPolygonStockTrades)

		polygonOptions := polygonGroup.Group("/options")
		polygonOptions.GET("/contract", h.GetPolygonOptionContract)
		polygonOptions.GET("/chain", h.GetPolygonOptionChain)
		polygonOptions.GET("/aggregates", h.GetPolygonOptionAggregates)
		polygonOptions.GET("/quotes", h.GetPolygonOptionQuotes)
		polygonOptions.GET("/trades", h.GetPolygonOptionTrades)

		v1.GET("/strategies", h.ListStrategies)
	}

	return r
}
