package api

import (
	"net/http"
	"strings"

	"github.com/Cyvadra/toktik/internal/config"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// Deps bundles every dependency the API layer needs. Optional providers
// may be left nil; the corresponding routes will return 501.
type Deps struct {
	Config            config.Runtime
	CryptoOptions     CryptoOptionsQuerier
	USStocks          USStocksQuerier
	USOptions         USOptionsQuerier
	Infra             InfraProvider
	DataBrowser       DataBrowserProvider
	Features          FeatureProvider
	Indicators        IndicatorSeriesProvider
	StrategyBacktests StrategyBacktestProvider
	CryptoSpot        CryptoSpotQuerier
	Forex             ForexQuerier
	Screener          ScreenerProvider
	StrategyCatalog   StrategyCatalogProvider
	Factors           FactorProvider
	Fundamentals      FundamentalsProvider
	Macro             MacroProvider
	Polygon           PolygonProvider // optional

	// Stop is closed when the server shuts down. Long-lived middleware
	// goroutines watch it to exit cleanly. May be nil; in that case
	// background goroutines run for the process lifetime.
	Stop <-chan struct{}
}

// NewRouterFromDeps is the canonical router constructor. It builds a
// gin engine wired with the supplied dependencies and middleware stack.
func NewRouterFromDeps(d Deps) *gin.Engine {
	r := gin.New()
	if proxies := d.Config.API.TrustedProxies; len(proxies) > 0 {
		_ = r.SetTrustedProxies(proxies)
	} else {
		// Default-deny: do not trust X-Forwarded-For unless explicitly
		// configured. Prevents IP-spoofed rate-limit bypass.
		_ = r.SetTrustedProxies(nil)
	}

	r.Use(SlogRecoveryMiddleware())
	r.Use(SlogRequestLogger())
	r.Use(SecurityHeadersMiddleware())
	r.Use(CORSMiddleware(d.Config.API))

	// Per-request timeout. Streaming endpoints (SSE, HTML reports) opt
	// out so they are not killed mid-stream.
	r.Use(RequestTimeoutMiddleware(d.Config.APIRequestTimeout(), isStreamingPath))

	// Auth + rate limiting. Both are no-ops when not configured.
	r.Use(APIKeyAuth(d.Config.API))
	r.Use(RateLimitMiddleware(d.Config.API, d.Stop))

	h := NewHandler(d)

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/ready", h.GetReadiness)

	v1 := r.Group("/api/v1")
	registerRoutes(v1, h)
	return r
}

// NewRouter is kept for backward compatibility with existing tests and
// call sites. New code should use NewRouterFromDeps.
//
// Deprecated: use NewRouterFromDeps with a populated Deps struct.
func NewRouter(
	cos CryptoOptionsQuerier,
	usStocks USStocksQuerier,
	usOptions USOptionsQuerier,
	infra InfraProvider,
	features FeatureProvider,
	indicators IndicatorSeriesProvider,
	strategyBacktests StrategyBacktestProvider,
	cryptoSpot CryptoSpotQuerier,
	screener ScreenerProvider,
	strategyCatalog StrategyCatalogProvider,
	factors FactorProvider,
	polygon ...PolygonProvider,
) *gin.Engine {
	d := Deps{
		Config:            config.DefaultRuntime(),
		CryptoOptions:     cos,
		USStocks:          usStocks,
		USOptions:         usOptions,
		Infra:             infra,
		DataBrowser:       nil,
		Features:          features,
		Indicators:        indicators,
		StrategyBacktests: strategyBacktests,
		CryptoSpot:        cryptoSpot,
		Screener:          screener,
		StrategyCatalog:   strategyCatalog,
		Factors:           factors,
	}
	if len(polygon) > 0 {
		d.Polygon = polygon[0]
	}
	return NewRouterFromDeps(d)
}

// registerRoutes wires the v1 route table. Kept as a separate function
// so tests and any future binary that wants a different prefix can call
// it directly.
func registerRoutes(v1 *gin.RouterGroup, h *Handler) {
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

	browserGroup := v1.Group("/browser")
	browserGroup.GET("/presets", h.ListBrowserPresets)
	browserDatasets := browserGroup.Group("/datasets/:dataset")
	browserDatasets.GET("/schema", h.GetBrowserDatasetSchema)
	browserDatasets.GET("/preview", h.GetBrowserDatasetPreview)
	browserDatasets.GET("/coverage", h.GetBrowserDatasetCoverage)
	browserDatasets.GET("/field-profile", h.GetBrowserFieldProfile)
	browserDatasets.GET("/valid-count", h.GetBrowserValidCount)
	browserDatasets.GET("/symbols", h.GetBrowserDatasetValues)

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

	marketForex := markets.Group("/forex")
	marketForex.GET("/bars", h.GetForexBars)
	marketForex.GET("/symbols", h.GetForexSymbols)

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

	fundamentalsGroup := v1.Group("/fundamentals")
	fundamentalsGroup.GET("/factors", h.ListFundamentalFactors)
	fundamentalsGroup.GET("/series", h.GetFundamentalSeries)
	fundamentalsGroup.GET("/snapshot", h.GetFundamentalSnapshot)
	fundamentalsGroup.GET("/panel", h.GetFundamentalPanel)
	fundamentalsGroup.GET("/freshness", h.GetFundamentalFreshness)

	macroGroup := v1.Group("/macro")
	macroGroup.GET("/factors", h.ListMacroFactors)
	macroGroup.GET("/series", h.GetMacroSeries)

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

// isStreamingPath identifies endpoints that must not have a per-request
// timeout applied (SSE streams and HTML report downloads that may be
// arbitrarily large).
func isStreamingPath(c *gin.Context) bool {
	p := c.Request.URL.Path
	if strings.HasSuffix(p, "/events") {
		return true
	}
	if strings.Contains(p, "/backtests/runs/") &&
		(strings.HasSuffix(p, "/report") || strings.Contains(p, "/reports/")) {
		return true
	}
	return false
}
