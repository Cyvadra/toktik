package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetReadiness handles GET /ready.
//
//	@Summary		Check API readiness
//	@Description	Returns the readiness status of the API server and its backend dependencies.
//	@Tags			Infrastructure
//	@Produce		json
//	@Success		200	{object}	dto.ReadinessResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/ready [get]
func (h *Handler) GetReadiness(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.Readiness(c.Request.Context())
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMarkets handles GET /api/v1/infra/markets.
//
//	@Summary		List supported markets
//	@Description	Returns all market domains (crypto-options, us-options, etc.) with their capabilities.
//	@Tags			Infrastructure
//	@Produce		json
//	@Success		200	{object}	dto.MarketCatalogResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/infra/markets [get]
func (h *Handler) GetMarkets(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.ListMarkets(c.Request.Context())
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDatasets handles GET /api/v1/infra/datasets.
//
//	@Summary		List datasets and freshness
//	@Description	Returns all dataset descriptors with freshness and row count metadata. Filter by market or status.
//	@Tags			Infrastructure
//	@Produce		json
//	@Param			market	query		string	false	"Filter by market"
//	@Param			status	query		string	false	"Filter by status (ready, stale, missing, empty)"
//	@Success		200		{object}	dto.DatasetCatalogResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/infra/datasets [get]
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
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListStrategies handles GET /api/v1/strategies.
//
//	@Summary		List registered strategies
//	@Description	Returns all available strategy templates with metadata and group classification.
//	@Tags			Strategies
//	@Produce		json
//	@Param			group	query		string	false	"Filter by strategy group"
//	@Success		200		{object}	dto.StrategyCatalogResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/strategies [get]
func (h *Handler) ListStrategies(c *gin.Context) {
	var req dto.StrategyCatalogListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.strategyCatalog == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy catalog provider not configured"})
		return
	}

	resp, err := h.strategyCatalog.ListStrategies(c.Request.Context(), req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListFactors handles GET /api/v1/factors.
//
//	@Summary		List available factor feeds
//	@Description	Returns metadata for all registered factor feeds, including supported symbols, windows, and fields.
//	@Tags			Factors
//	@Produce		json
//	@Success		200	{object}	dto.FactorCatalogResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/factors [get]
func (h *Handler) ListFactors(c *gin.Context) {
	if h.factors == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "factor provider not configured"})
		return
	}

	resp, err := h.factors.ListFactors(c.Request.Context())
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetFactorBars handles GET /api/v1/factors/bars.
//
//	@Summary		Query factor time-series data
//	@Description	Returns OHLC bars for a specific factor feed, symbol, and time window.
//	@Tags			Factors
//	@Produce		json
//	@Param			name	query		string	true	"Factor feed name (e.g. dvol)"
//	@Param			symbol	query		string	true	"Symbol (e.g. BTC)"
//	@Param			window	query		string	true	"Time window (e.g. 1h, 1d)"
//	@Param			from	query		string	true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to		query		string	true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			limit	query		int		false	"Max rows (default 1000, max 10000)"
//	@Param			cursor	query		string	false	"Opaque pagination cursor"
//	@Success		200		{object}	dto.FactorBarResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/factors/bars [get]
func (h *Handler) GetFactorBars(c *gin.Context) {
	var req dto.FactorBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.factors == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "factor provider not configured"})
		return
	}

	resp, err := h.factors.QueryFactorBars(c.Request.Context(), req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
