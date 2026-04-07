package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// Handler holds references to service layer dependencies.
type Handler struct {
	cryptoOptions CryptoOptionsQuerier
	usMarket      USMarketQuerier
}

func NewHandler(cos CryptoOptionsQuerier) *Handler {
	return &Handler{cryptoOptions: cos}
}

// NewHandlerWithUSMarket creates a Handler wired with both crypto-options and
// US market service implementations.
func NewHandlerWithUSMarket(cos CryptoOptionsQuerier, usm USMarketQuerier) *Handler {
	return &Handler{cryptoOptions: cos, usMarket: usm}
}

// handleServiceError maps service-level errors to appropriate HTTP responses.
func handleServiceError(c *gin.Context, err error) {
	var ve *dto.ValidationError
	if errors.As(err, &ve) {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		c.JSON(http.StatusGatewayTimeout, dto.ErrorResponse{Error: "request timeout"})
		return
	}
	slog.Error("internal error", "error", err)
	c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "internal server error"})
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
		handleServiceError(c, err)
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
		handleServiceError(c, err)
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
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// RunBacktest handles POST /api/v1/crypto-options/backtest
func (h *Handler) RunBacktest(c *gin.Context) {
	var req dto.BacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.RunBacktest(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSStockBars handles GET /api/v1/us-stocks/bars
func (h *Handler) GetUSStockBars(c *gin.Context) {
	if h.usMarket == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "US market service not configured"})
		return
	}

	var req dto.USStockBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.usMarket.QueryUSStockBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionBars handles GET /api/v1/us-options/bars
func (h *Handler) GetUSOptionBars(c *gin.Context) {
	if h.usMarket == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "US market service not configured"})
		return
	}

	var req dto.USOptionBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.usMarket.QueryUSOptionBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionChain handles GET /api/v1/us-options/chain
func (h *Handler) GetUSOptionChain(c *gin.Context) {
	if h.usMarket == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "US market service not configured"})
		return
	}

	var req dto.USOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.usMarket.QueryUSOptionChain(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
