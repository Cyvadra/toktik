package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// ListMacroFactors handles GET /api/v1/macro/factors.
//
// @Summary      List macro factor catalog
// @Description  Returns active macro/weak-binding factor definitions and metadata.
// @Tags         Macro
// @Produce      json
// @Param        dataset  query     string  false  "Dataset filter (e.g. gurufocus-shiller)"
// @Success      200      {object}  dto.MacroFactorCatalogResponse
// @Failure      400      {object}  dto.ErrorResponse
// @Failure      500      {object}  dto.ErrorResponse
// @Router       /macro/factors [get]
func (h *Handler) ListMacroFactors(c *gin.Context) {
	var req dto.MacroFactorCatalogRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.macro == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "macro provider not configured"})
		return
	}
	resp, err := h.macro.ListFactors(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetMacroSeries handles GET /api/v1/macro/series.
//
// @Summary      Get macro factor series
// @Description  Returns sparse monthly observations or reference-bar-expanded realtime series for weakly-bound macro factors. For dataset=gurufocus-shiller, supported factor codes are: sp500, dividend, earnings, CPI, rate_GS10, real_sp, real_div, real_earnings, pe10, ractual, rexpect, pe_reg, ir10, excess_cape_yield, real_excess_annualized_returns_10y, pe10_live, pe_reg_live, cape_earnings_yield_live, regression_earnings_yield_live.
// @Tags         Macro
// @Produce      json
// @Param        dataset           query     string    true   "Dataset (e.g. gurufocus-shiller)"
// @Param        factor            query     []string  true   "Factor codes (repeat or comma-separated). For gurufocus-shiller: sp500, dividend, earnings, CPI, rate_GS10, real_sp, real_div, real_earnings, pe10, ractual, rexpect, pe_reg, ir10, excess_cape_yield, real_excess_annualized_returns_10y, pe10_live, pe_reg_live, cape_earnings_yield_live, regression_earnings_yield_live"
// @Param        from              query     string    true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to                query     string    true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        as_of             query     string    false  "Point-in-time cutoff (defaults to to)"
// @Param        interval          query     string    false  "event or a US stock interval such as 1m/5m/1h/1d"
// @Param        reference_market  query     string    false  "Reference market for expanded realtime queries (currently us-stocks)"
// @Param        reference_symbol  query     string    false  "Reference symbol for expanded realtime queries (for example SPY)"
// @Param        limit             query     int       false  "Maximum returned rows"
// @Success      200               {object}  dto.MacroSeriesResponse
// @Failure      400               {object}  dto.ErrorResponse
// @Failure      500               {object}  dto.ErrorResponse
// @Router       /macro/series [get]
func (h *Handler) GetMacroSeries(c *gin.Context) {
	var req dto.MacroSeriesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.macro == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "macro provider not configured"})
		return
	}
	resp, err := h.macro.QuerySeries(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
