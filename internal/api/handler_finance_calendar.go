package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetEconomicCalendar handles GET /api/v1/calendar/economic.
//
//	@Summary		Get economic calendar
//	@Description	Returns the macro economic calendar for the default window (today-7d to today+30d). The upstream FMP data is synced on demand with a 12h sync marker cache.
//	@Tags			Calendar
//	@Produce		json
//	@Success		200	{object}	dto.EconomicCalendarResponse
//	@Failure		400	{object}	dto.ErrorResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/calendar/economic [get]
func (h *Handler) GetEconomicCalendar(c *gin.Context) {
	if h.financeCalendar == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "finance calendar provider not configured"})
		return
	}
	resp, err := h.financeCalendar.QueryEconomicCalendar(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetStockCalendar handles POST /api/v1/calendar/stocks.
//
//	@Summary		Get stock calendar
//	@Description	Returns calendar events for an observed US stock pool. The request body accepts a list of symbols and the service syncs the default stock window (today-30d to today+90d) before reading from MySQL.
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
