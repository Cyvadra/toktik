package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetEconomicCalendar handles GET /api/v1/calendar/economic.
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
