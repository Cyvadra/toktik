package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetVolatilitySnapshot handles GET /api/v1/features/volatility-snapshot.
//
//	@Summary		Get volatility snapshot
//	@Description	Returns current HV and IV regime metrics for an underlying. When the latest precomputed feature row has empty volatility fields, the server scans up to the prior 7 calendar days and uses the nearest valid values.
//	@Tags			Features
//	@Produce		json
//	@Param			market			query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying		query		string	true	"Underlying asset symbol"
//	@Param			lookback_days	query		int		false	"IV percentile lookback window (default 252)"
//	@Success		200				{object}	dto.FeatureVolatilitySnapshotResponse
//	@Failure		400				{object}	dto.ErrorResponse
//	@Failure		500				{object}	dto.ErrorResponse
//	@Router			/features/volatility-snapshot [get]
func (h *Handler) GetVolatilitySnapshot(c *gin.Context) {
	var req dto.FeatureVolatilitySnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryVolatilitySnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetVolatilityHistory handles GET /api/v1/features/volatility-history.
//
//	@Summary		Get volatility history
//	@Description	Returns a range of daily HV and IV metrics for an underlying. Precomputed feature queries scan up to 7 calendar days before the requested start date so empty volatility fields can be backfilled from the nearest earlier valid row while keeping the response clipped to the requested range.
//	@Tags			Features
//	@Produce		json
//	@Param			market			query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying		query		string	true	"Underlying asset symbol"
//	@Param			from			query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to				query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Param			lookback_days	query		int		false	"IV percentile lookback window (default 252)"
//	@Success		200				{object}	dto.FeatureVolatilityHistoryResponse
//	@Failure		400				{object}	dto.ErrorResponse
//	@Failure		500				{object}	dto.ErrorResponse
//	@Router			/features/volatility-history [get]
func (h *Handler) GetVolatilityHistory(c *gin.Context) {
	var req dto.FeatureVolatilityHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryVolatilityHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetTermStructureSnapshot handles GET /api/v1/features/term-structure-snapshot.
//
//	@Summary		Get IV term structure snapshot
//	@Description	Returns the current ATM IV term structure by expiration for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market				query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying			query		string	true	"Underlying asset symbol"
//	@Param			min_days_to_expiry	query		int		false	"Min DTE filter"
//	@Param			max_days_to_expiry	query		int		false	"Max DTE filter"
//	@Success		200					{object}	dto.FeatureTermStructureSnapshotResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		500					{object}	dto.ErrorResponse
//	@Router			/features/term-structure-snapshot [get]
func (h *Handler) GetTermStructureSnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryTermStructureSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetTermStructureHistory handles GET /api/v1/features/term-structure-history.
//
//	@Summary		Get term structure history
//	@Description	Returns historical IV term structure data for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market		query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying	query		string	true	"Underlying asset symbol"
//	@Param			from		query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.FeatureTermStructureHistoryResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/features/term-structure-history [get]
func (h *Handler) GetTermStructureHistory(c *gin.Context) {
	var req dto.FeatureTermStructureHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryTermStructureHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSkewSnapshot handles GET /api/v1/features/skew-snapshot.
//
//	@Summary		Get put-call skew snapshot
//	@Description	Returns the current OTM put-call IV skew by expiration for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market				query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying			query		string	true	"Underlying asset symbol"
//	@Param			min_days_to_expiry	query		int		false	"Min DTE filter"
//	@Param			max_days_to_expiry	query		int		false	"Max DTE filter"
//	@Success		200					{object}	dto.FeatureSkewSnapshotResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		500					{object}	dto.ErrorResponse
//	@Router			/features/skew-snapshot [get]
func (h *Handler) GetSkewSnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QuerySkewSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSkewHistory handles GET /api/v1/features/skew-history.
//
//	@Summary		Get skew history
//	@Description	Returns historical put-call skew data for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market		query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying	query		string	true	"Underlying asset symbol"
//	@Param			from		query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.FeatureSkewHistoryResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/features/skew-history [get]
func (h *Handler) GetSkewHistory(c *gin.Context) {
	var req dto.FeatureSkewHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QuerySkewHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetLiquiditySnapshot handles GET /api/v1/features/liquidity-snapshot.
//
//	@Summary		Get liquidity snapshot
//	@Description	Returns option liquidity metrics by expiration bucket for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market				query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying			query		string	true	"Underlying asset symbol"
//	@Param			min_days_to_expiry	query		int		false	"Min DTE filter"
//	@Param			max_days_to_expiry	query		int		false	"Max DTE filter"
//	@Success		200					{object}	dto.FeatureLiquiditySnapshotResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		500					{object}	dto.ErrorResponse
//	@Router			/features/liquidity-snapshot [get]
func (h *Handler) GetLiquiditySnapshot(c *gin.Context) {
	var req dto.FeatureSurfaceSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryLiquiditySnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetLiquidityHistory handles GET /api/v1/features/liquidity-history.
//
//	@Summary		Get liquidity history
//	@Description	Returns a range of daily liquidity metrics for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market				query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying			query		string	true	"Underlying asset symbol"
//	@Param			from				query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to					query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Param			min_days_to_expiry	query		int		false	"Min DTE filter"
//	@Param			max_days_to_expiry	query		int		false	"Max DTE filter"
//	@Success		200					{object}	dto.FeatureLiquidityHistoryResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		500					{object}	dto.ErrorResponse
//	@Router			/features/liquidity-history [get]
func (h *Handler) GetLiquidityHistory(c *gin.Context) {
	var req dto.FeatureLiquidityHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryLiquidityHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetEventWindowSnapshot handles GET /api/v1/features/event-window-snapshot.
//
//	@Summary		Get event window snapshot
//	@Description	Returns market-session proximity flags (holidays, early close) for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market		query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying	query		string	true	"Underlying asset symbol"
//	@Success		200			{object}	dto.FeatureEventWindowSnapshotResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/features/event-window-snapshot [get]
func (h *Handler) GetEventWindowSnapshot(c *gin.Context) {
	var req dto.FeatureUnderlyingSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryEventWindowSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetEventWindowHistory handles GET /api/v1/features/event-window-history.
//
//	@Summary		Get event window history
//	@Description	Returns a range of daily event-window flags for an underlying.
//	@Tags			Features
//	@Produce		json
//	@Param			market		query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying	query		string	true	"Underlying asset symbol"
//	@Param			from		query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Success		200			{object}	dto.FeatureEventWindowHistoryResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/features/event-window-history [get]
func (h *Handler) GetEventWindowHistory(c *gin.Context) {
	var req dto.FeatureUnderlyingHistoryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryEventWindowHistory(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDailyFeaturePanel handles GET /api/v1/features/daily-feature-panel.
//
//	@Summary		Get daily feature panel
//	@Description	Returns a consolidated daily panel with volatility, term structure, liquidity, and event features. Precomputed panel queries scan up to 7 calendar days before the requested start date so empty feature fields can be backfilled from the nearest earlier valid row while keeping the response clipped to the requested range.
//	@Tags			Features
//	@Produce		json
//	@Param			market				query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying			query		string	true	"Underlying asset symbol"
//	@Param			from				query		string	true	"Start date (RFC3339 or YYYY-MM-DD)"
//	@Param			to					query		string	true	"End date (RFC3339 or YYYY-MM-DD)"
//	@Param			lookback_days		query		int		false	"IV percentile lookback window (default 252)"
//	@Param			min_days_to_expiry	query		int		false	"Min DTE filter"
//	@Param			max_days_to_expiry	query		int		false	"Max DTE filter"
//	@Success		200					{object}	dto.FeatureDailyPanelResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		500					{object}	dto.ErrorResponse
//	@Router			/features/daily-feature-panel [get]
func (h *Handler) GetDailyFeaturePanel(c *gin.Context) {
	var req dto.FeatureDailyPanelRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.features == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "feature provider not configured"})
		return
	}

	resp, err := h.features.QueryDailyFeaturePanel(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
