package api

import (
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/gin-gonic/gin"
)

// NewRouter builds the Gin engine with all API routes registered.
func NewRouter(cos *service.CryptoOptionsService) *gin.Engine {
	r := gin.Default()
	h := NewHandler(cos)

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
