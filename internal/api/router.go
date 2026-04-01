package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter builds the Gin engine with all API routes registered.
func NewRouter(cos CryptoOptionsQuerier) *gin.Engine {
	r := gin.Default()
	h := NewHandler(cos)

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

	return r
}
