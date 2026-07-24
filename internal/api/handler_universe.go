package api

import (
	"net/http"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetUniverseMembers handles GET /api/v1/universes/{code}/members.
//
// @Summary      Get named universe members
// @Description  Returns point-in-time members when as_of is supplied, or every membership interval overlapping the half-open [from, to) range. Supply either as_of or a range; dates accept YYYY-MM-DD or RFC3339.
// @Tags         Universes
// @Produce      json
// @Param        code    path   string  true   "Named universe code"
// @Param        market  query  string  false  "Market; defaults to us-stocks"
// @Param        as_of   query  string  false  "Point-in-time date (YYYY-MM-DD or RFC3339); cannot be combined with a range"
// @Param        from    query  string  false  "Inclusive range start (YYYY-MM-DD or RFC3339)"
// @Param        to      query  string  false  "Exclusive range end (YYYY-MM-DD or RFC3339)"
// @Param        limit   query  int     false  "Maximum intervals returned; defaults to 5000 and is capped at 500000"
// @Success      200  {object}  dto.UniverseMembersResponse
// @Failure      400  {object}  dto.ErrorResponse
// @Failure      401  {object}  dto.ErrorResponse
// @Failure      404  {object}  dto.ErrorResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Failure      501  {object}  dto.ErrorResponse
// @Router       /universes/{code}/members [get]
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

// RebuildUniverse handles POST /api/v1/universes/rebuild.
//
// @Summary      Rebuild named universe membership
// @Description  Rebuilds membership over a server-derived half-open [from, to) range. The end is the latest date shared by SPY stock and option daily data; force_refresh=true (the default) starts from the configured rebuild history, while false resumes from existing membership. source_type defaults to turnover_intersection_union, which derives a daily union of liquid US stock/option underlyings across requested turnover lookbacks. preset_symbols and provider_holdings require symbols or members. Set dry_run=true to calculate the result without changing stored membership or recording a run. A configured API key authenticator is required even when local-client auth bypass is enabled.
// @Tags         Universes
// @Accept       json
// @Produce      json
// @Param        body  body      dto.UniverseRebuildRequest  true  "Universe rebuild configuration"
// @Success      200   {object}  dto.UniverseRebuildResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      401   {object}  dto.ErrorResponse
// @Failure      403   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Failure      501   {object}  dto.ErrorResponse
// @Router       /universes/rebuild [post]
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
