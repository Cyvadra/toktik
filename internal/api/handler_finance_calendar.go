package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetEconomicCalendar handles GET /api/v1/calendar/economic.
//
//	@Summary		Get economic calendar
//	@Description	Returns the persisted macro economic calendar from MySQL for the requested window, or the default window (today-7d to today+30d) when omitted. Refresh is handled by the fmp_economic_calendar data-sync-pipeline job.
//	@Tags			Calendar
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD)"
//	@Param			to		query		string	false	"End date (YYYY-MM-DD)"
//	@Success		200		{object}	dto.EconomicCalendarResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/calendar/economic [get]
func (h *Handler) GetEconomicCalendar(c *gin.Context) {
	if h.financeCalendar == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "finance calendar provider not configured"})
		return
	}
	var req dto.EconomicCalendarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	resp, err := h.financeCalendar.QueryEconomicCalendar(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetStockCalendar handles POST /api/v1/calendar/stocks.
//
//	@Summary		Get stock calendar
//	@Description	Returns persisted calendar events for an observed US stock pool from MySQL. The request body accepts symbols, optional from/to date window, optional event types, and earnings_only for Finnhub-replacement earnings calendar use cases. Refresh is handled by the fmp_observed_stock_calendar data-sync-pipeline job.
//	@Tags			Calendar
//	@Accept			json
//	@Produce		json
//	@Param			request	body		dto.StockCalendarRequest	true	"Stock calendar request"
//	@Success		200		{object}	dto.StockCalendarResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/calendar/stocks [post]
func (h *Handler) GetStockCalendar(c *gin.Context) {
	if h.financeCalendar == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "finance calendar provider not configured"})
		return
	}
	var req dto.StockCalendarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	resp, err := h.financeCalendar.QueryStockCalendar(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
