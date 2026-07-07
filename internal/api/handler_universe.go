package api

import (
	"net/http"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

func (h *Handler) GetUniverseMembers(c *gin.Context) {
	if h.universes == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "universe provider not configured"})
		return
	}
	req := dto.UniverseMembersRequest{
		Code:   strings.TrimSpace(c.Param("code")),
		Market: strings.TrimSpace(c.Query("market")),
	}
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if raw := strings.TrimSpace(c.Query("as_of")); raw != "" {
		asOf, err := dto.ParseUniverseDate(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid as_of date"})
			return
		}
		req.AsOf = asOf
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		from, err := dto.ParseUniverseDate(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid from date"})
			return
		}
		req.From = from
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		to, err := dto.ParseUniverseDate(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid to date"})
			return
		}
		req.To = to
	}
	var resp *dto.UniverseMembersResponse
	var err error
	if !req.From.IsZero() || !req.To.IsZero() {
		resp, err = h.universes.MemberIntervals(c.Request.Context(), req)
	} else {
		resp, err = h.universes.Members(c.Request.Context(), req)
	}
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) RebuildUniverse(c *gin.Context) {
	if h.universes == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "universe provider not configured"})
		return
	}
	if !h.requireAPIKey {
		c.JSON(http.StatusForbidden, dto.ErrorResponse{Error: "universe rebuild requires API key authentication"})
		return
	}
	var req dto.UniverseRebuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	resp, err := h.universes.Rebuild(c.Request.Context(), req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
