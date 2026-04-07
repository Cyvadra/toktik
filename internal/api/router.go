package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the Gin engine with all API routes registered.
func NewRouter(cos CryptoOptionsQuerier) *gin.Engine {
	return NewRouterWithUSMarket(cos, nil)
}

// NewRouterWithUSMarket builds the Gin engine with crypto-options and optional
// US market routes registered. Pass nil for usm to omit US market routes.
func NewRouterWithUSMarket(cos CryptoOptionsQuerier, usm USMarketQuerier) *gin.Engine {
	r := gin.Default()
	h := NewHandlerWithUSMarket(cos, usm)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	{
		co := v1.Group("/crypto-options")
		co.GET("/bars", h.GetBars)
		co.GET("/symbols", h.GetSymbols)
		co.GET("/greeks", h.GetGreeks)
		co.POST("/backtest", h.RunBacktest)
	}

	if usm != nil {
		{
			us := v1.Group("/us-stocks")
			us.GET("/bars", h.GetUSStockBars)
		}
		{
			uo := v1.Group("/us-options")
			uo.GET("/bars", h.GetUSOptionBars)
			uo.GET("/chain", h.GetUSOptionChain)
		}
	}

	return r
}
