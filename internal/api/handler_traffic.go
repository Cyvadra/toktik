package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetTrafficStats returns hourly API-server traffic and a period summary.
func (h *Handler) GetTrafficStats(c *gin.Context) {
	if h.trafficStats == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "traffic statistics unavailable"})
		return
	}
	var req dto.TrafficStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	response, err := h.trafficStats.QueryTrafficStats(c.Request.Context(), req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}
