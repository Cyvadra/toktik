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
	usStocks      USStocksQuerier
	usOptions     USOptionsQuerier
	infra         InfraProvider
	features      FeatureProvider
}

func NewHandler(cos CryptoOptionsQuerier, usStocks USStocksQuerier, usOptions USOptionsQuerier, infra InfraProvider, features FeatureProvider) *Handler {
	return &Handler{cryptoOptions: cos, usStocks: usStocks, usOptions: usOptions, infra: infra, features: features}
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

// GetReadiness handles GET /ready.
func (h *Handler) GetReadiness(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.Readiness(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMarkets handles GET /api/v1/infra/markets.
func (h *Handler) GetMarkets(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.ListMarkets(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDatasets handles GET /api/v1/infra/datasets.
func (h *Handler) GetDatasets(c *gin.Context) {
	var req dto.DatasetQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.ListDatasets(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetVolatilitySnapshot handles GET /api/v1/features/volatility-snapshot.
func (h *Handler) GetVolatilitySnapshot(c *gin.Context) {
	var req dto.FeatureVolatilitySnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryVolatilitySnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetVolatilityHistory handles GET /api/v1/features/volatility-history.
func (h *Handler) GetVolatilityHistory(c *gin.Context) {
	var req dto.FeatureVolatilityHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryVolatilityHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetTermStructureSnapshot handles GET /api/v1/features/term-structure-snapshot.
func (h *Handler) GetTermStructureSnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryTermStructureSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSkewSnapshot handles GET /api/v1/features/skew-snapshot.
func (h *Handler) GetSkewSnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QuerySkewSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetLiquiditySnapshot handles GET /api/v1/features/liquidity-snapshot.
func (h *Handler) GetLiquiditySnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryLiquiditySnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetLiquidityHistory handles GET /api/v1/features/liquidity-history.
func (h *Handler) GetLiquidityHistory(c *gin.Context) {
	var req dto.FeatureLiquidityHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryLiquidityHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetEventWindowSnapshot handles GET /api/v1/features/event-window-snapshot.
func (h *Handler) GetEventWindowSnapshot(c *gin.Context) {
	var req dto.FeatureUnderlyingSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryEventWindowSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetEventWindowHistory handles GET /api/v1/features/event-window-history.
func (h *Handler) GetEventWindowHistory(c *gin.Context) {
	var req dto.FeatureUnderlyingHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryEventWindowHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDailyFeaturePanel handles GET /api/v1/features/daily-feature-panel.
func (h *Handler) GetDailyFeaturePanel(c *gin.Context) {
	var req dto.FeatureDailyPanelRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryDailyFeaturePanel(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
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

// GetUSStockBars handles GET /api/v1/markets/us-stocks/bars.
func (h *Handler) GetUSStockBars(c *gin.Context) {
	var req dto.USStockBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usStocks == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-stocks provider not configured"})
		return
	}

	resp, err := h.usStocks.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSStockSymbols handles GET /api/v1/markets/us-stocks/symbols.
func (h *Handler) GetUSStockSymbols(c *gin.Context) {
	var req dto.USStockSymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usStocks == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-stocks provider not configured"})
		return
	}

	resp, err := h.usStocks.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionBars handles GET /api/v1/markets/us-options/bars.
func (h *Handler) GetUSOptionBars(c *gin.Context) {
	var req dto.USOptionBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionSymbols handles GET /api/v1/markets/us-options/symbols.
func (h *Handler) GetUSOptionSymbols(c *gin.Context) {
	var req dto.USOptionSymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionGreeks handles GET /api/v1/markets/us-options/greeks.
func (h *Handler) GetUSOptionGreeks(c *gin.Context) {
	var req dto.USOptionGreeksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryGreeks(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionChain handles GET /api/v1/markets/us-options/chain.
func (h *Handler) GetUSOptionChain(c *gin.Context) {
	var req dto.USOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryChain(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
