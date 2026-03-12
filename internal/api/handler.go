package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/service"
	"github.com/gin-gonic/gin"
)

// Handler holds references to service layer dependencies.
type Handler struct {
	cryptoOptions *service.CryptoOptionsService
}

func NewHandler(cos *service.CryptoOptionsService) *Handler {
	return &Handler{cryptoOptions: cos}
}

// GetBars handles GET /api/v1/crypto-options/bars
func (h *Handler) GetBars(c *gin.Context) {
	var req dto.BarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QueryBars(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSymbols handles GET /api/v1/crypto-options/symbols
func (h *Handler) GetSymbols(c *gin.Context) {
	var req dto.SymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetGreeks handles GET /api/v1/crypto-options/greeks
func (h *Handler) GetGreeks(c *gin.Context) {
	var req dto.GreeksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QueryGreeks(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
