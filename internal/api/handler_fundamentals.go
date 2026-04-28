package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// ListFundamentalFactors handles GET /api/v1/fundamentals/factors.
//
// @Summary      List fundamental factor catalog
// @Description  Returns active symbol-bound fundamental factors and their metadata.
// @Tags         Fundamentals
// @Produce      json
// @Param        market  query     string  false  "Market filter (us-stocks | crypto-spot)"
// @Success      200     {object}  dto.FundamentalFactorCatalogResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /fundamentals/factors [get]
func (h *Handler) ListFundamentalFactors(c *gin.Context) {
	var req dto.FundamentalFactorCatalogRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.fundamentals == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "fundamentals provider not configured"})
		return
	}
	resp, err := h.fundamentals.ListFactors(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetFundamentalSeries handles GET /api/v1/fundamentals/series.
//
// @Summary      Get fundamental factor series
// @Description  Returns event/as_of/filled series for one (market, symbol, factor). Point-in-time enforced via known_at.
// @Tags         Fundamentals
// @Produce      json
// @Param        market  query     string  true   "Market (us-stocks | crypto-spot)"
// @Param        symbol  query     string  true   "Symbol"
// @Param        factor  query     string  true   "Factor code (e.g., pe, pb)"
// @Param        from    query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to      query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        mode    query     string  false  "event | as_of | filled (default filled)"
// @Param        as_of   query     string  false  "Point-in-time cutoff (defaults to to)"
// @Success      200     {object}  dto.FundamentalSeriesResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /fundamentals/series [get]
func (h *Handler) GetFundamentalSeries(c *gin.Context) {
	var req dto.FundamentalSeriesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.fundamentals == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "fundamentals provider not configured"})
		return
	}
	resp, err := h.fundamentals.QuerySeries(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetFundamentalSnapshot handles GET /api/v1/fundamentals/snapshot.
//
// @Summary      Get fundamental snapshot for one symbol
// @Description  Returns latest known value per factor for one (market, symbol) at as_of.
// @Tags         Fundamentals
// @Produce      json
// @Param        market  query     string    true   "Market (us-stocks | crypto-spot)"
// @Param        symbol  query     string    true   "Symbol"
// @Param        factor  query     []string  false  "Factor codes (repeat or comma-separated)"
// @Param        as_of   query     string    false  "Point-in-time cutoff (defaults to now UTC)"
// @Success      200     {object}  dto.FundamentalSnapshotResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /fundamentals/snapshot [get]
func (h *Handler) GetFundamentalSnapshot(c *gin.Context) {
	var req dto.FundamentalSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.fundamentals == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "fundamentals provider not configured"})
		return
	}
	resp, err := h.fundamentals.QuerySnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetFundamentalPanel handles GET /api/v1/fundamentals/panel.
//
// @Summary      Get fundamental panel across symbols
// @Description  Returns latest known values per (symbol, factor) at as_of.
// @Tags         Fundamentals
// @Produce      json
// @Param        market  query     string    true   "Market (us-stocks | crypto-spot)"
// @Param        symbol  query     []string  true   "Symbols (repeat or comma-separated)"
// @Param        factor  query     []string  false  "Factor codes (repeat or comma-separated)"
// @Param        as_of   query     string    false  "Point-in-time cutoff (defaults to now UTC)"
// @Success      200     {object}  dto.FundamentalPanelResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /fundamentals/panel [get]
func (h *Handler) GetFundamentalPanel(c *gin.Context) {
	var req dto.FundamentalPanelRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.fundamentals == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "fundamentals provider not configured"})
		return
	}
	resp, err := h.fundamentals.QueryPanel(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetFundamentalFreshness handles GET /api/v1/fundamentals/freshness.
//
// @Summary      Get fundamental dataset freshness
// @Description  Returns latest known_at per factor and (when SLA configured) staleness flags.
// @Tags         Fundamentals
// @Produce      json
// @Param        market  query     string  false  "Market filter"
// @Param        factor  query     string  false  "Factor code filter"
// @Success      200     {object}  dto.FundamentalFreshnessResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /fundamentals/freshness [get]
func (h *Handler) GetFundamentalFreshness(c *gin.Context) {
	var req dto.FundamentalFreshnessRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.fundamentals == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "fundamentals provider not configured"})
		return
	}
	resp, err := h.fundamentals.QueryFreshness(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
