package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

func (h *Handler) TriggerAppDataRefresh(c *gin.Context) {
	if h.appDataRefresh == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "app data refresh provider not configured"})
		return
	}
	resp, err := h.appDataRefresh.TriggerAppDataRefresh(c.Request.Context())
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, resp)
}
