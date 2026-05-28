package api

import (
	"net/http"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// RunIndicatorSeries handles POST /api/v1/indicators/series.
//
//	@Summary		Run indicator series query
//	@Description	Evaluates either a full DSL script, one or more built-in presets[], or a simplified indicators[] expression list over market bars and returns aligned series arrays.
//	@Tags			Indicators
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.IndicatorSeriesRequest	true	"Indicator query"
//	@Success		200		{object}	dto.IndicatorSeriesResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/indicators/series [post]
func (h *Handler) RunIndicatorSeries(c *gin.Context) {
	var req dto.IndicatorSeriesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.indicators == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "indicator provider not configured"})
		return
	}

	resp, err := h.indicators.QueryIndicatorSeries(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ListIndicatorPresets handles GET /api/v1/indicators/presets.
//
//	@Summary		List indicator presets
//	@Description	Returns the built-in indicator preset bundles and the plotted expressions each preset expands to.
//	@Tags			Indicators
//	@Produce		json
//	@Success		200	{object}	dto.IndicatorPresetCatalogResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/indicators/presets [get]
func (h *Handler) ListIndicatorPresets(c *gin.Context) {
	if h.indicators == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "indicator provider not configured"})
		return
	}

	resp, err := h.indicators.ListIndicatorPresets(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// RunBacktest handles POST /api/v1/markets/crypto-options/backtest
//
//	@Summary		Run crypto options backtest (legacy, sync)
//	@Description	Runs a synchronous backtest on a crypto options strategy. Deprecated: use POST /backtests/runs instead.
//	@Tags			CryptoOptions
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.BacktestRequest	true	"Backtest configuration"
//	@Success		200		{object}	object
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Deprecated
//	@Router	/markets/crypto-options/backtest [post]
func (h *Handler) RunBacktest(c *gin.Context) {
	var req dto.BacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.RunBacktest(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ValidateStrategyBacktest handles POST /api/v1/backtests/validate.
//	@Summary		Validate a strategy backtest request
//	@Description	Validates strategy or DSL backtest inputs, resolves strategy metadata, and performs a prepare-time preflight check without starting an async run.
//	@Tags			Backtests
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.StrategyBacktestRunRequest	true	"Backtest validation configuration"
//	@Success		200		{object}	dto.StrategyBacktestValidationResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/backtests/validate [post]
func (h *Handler) ValidateStrategyBacktest(c *gin.Context) {
	var req dto.StrategyBacktestRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}

	resp, err := h.strategyBacktests.ValidateStrategyBacktest(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// StartStrategyBacktest handles POST /api/v1/backtests/runs.
//
//	@Summary		Start an async strategy backtest
//	@Description	Submits a strategy backtest run that executes asynchronously. Poll the run status via GET /backtests/runs/:runID.
//	@Tags			Backtests
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dto.StrategyBacktestRunRequest	true	"Backtest run configuration"
//	@Success		202		{object}	dto.StrategyBacktestRunAccepted
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/backtests/runs [post]
func (h *Handler) StartStrategyBacktest(c *gin.Context) {
	var req dto.StrategyBacktestRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}

	resp, err := h.strategyBacktests.StartStrategyBacktest(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusAccepted, decorateAcceptedBacktestRun(resp))
}

// GetStrategyBacktestRun handles GET /api/v1/backtests/runs/:runID.
//
//	@Summary		Get backtest run status
//	@Description	Returns the current status and results of a strategy backtest run.
//	@Tags			Backtests
//	@Produce		json
//	@Param			runID	path		string	true	"Backtest run ID"
//	@Success		200		{object}	dto.StrategyBacktestRunStatus
//	@Failure		404		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/backtests/runs/{runID} [get]
func (h *Handler) GetStrategyBacktestRun(c *gin.Context) {
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}

	resp, err := h.strategyBacktests.GetStrategyBacktestRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, decorateBacktestRunStatus(resp))
}

// GetStrategyBacktestReport handles GET /api/v1/backtests/runs/:runID/report.
//
//	@Summary		Get primary backtest report
//	@Description	Returns the reserved HTML report for a backtest run. Before completion, it returns 202 with the current run status.
//	@Tags			Backtests
//	@Produce		html
//	@Produce		json
//	@Param			runID	path		string	true	"Backtest run ID"
//	@Success		200		{string}	string
//	@Success		202		{object}	dto.StrategyBacktestRunStatus
//	@Failure		404		{object}	dto.ErrorResponse
//	@Failure		409		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/backtests/runs/{runID}/report [get]
func (h *Handler) GetStrategyBacktestReport(c *gin.Context) {
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}
	status, err := h.strategyBacktests.GetStrategyBacktestRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	writeBacktestReportResponse(c, h, status, "")
}

// GetStrategyBacktestNamedReport handles GET /api/v1/backtests/runs/:runID/reports/:reportID.
//
//	@Summary		Get a named backtest report
//	@Description	Returns an HTML report variant for a backtest run. Use reportID=overview for the overview page or 1..N for per-strategy detail pages.
//	@Tags			Backtests
//	@Produce		html
//	@Produce		json
//	@Param			runID		path		string	true	"Backtest run ID"
//	@Param			reportID	path		string	true	"Report selector: overview or 1..N"
//	@Success		200			{string}	string
//	@Success		202			{object}	dto.StrategyBacktestRunStatus
//	@Failure		404			{object}	dto.ErrorResponse
//	@Failure		409			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/backtests/runs/{runID}/reports/{reportID} [get]
func (h *Handler) GetStrategyBacktestNamedReport(c *gin.Context) {
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}
	status, err := h.strategyBacktests.GetStrategyBacktestRun(c.Request.Context(), c.Param("runID"))
	if err != nil {
		handleServiceError(c, err)
		return
	}
	writeBacktestReportResponse(c, h, status, c.Param("reportID"))
}

// StreamStrategyBacktestEvents handles GET /api/v1/backtests/runs/:runID/events.
//
//	@Summary		Stream backtest run events (SSE)
//	@Description	Returns a server-sent events stream for real-time backtest progress updates.
//	@Tags			Backtests
//	@Produce		text/event-stream
//	@Param			runID	path	string	true	"Backtest run ID"
//	@Success		200		"SSE stream of backtest events"
//	@Failure		404		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/backtests/runs/{runID}/events [get]
func (h *Handler) StreamStrategyBacktestEvents(c *gin.Context) {
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}

	runID := c.Param("runID")

	stream, unsubscribe, err := h.strategyBacktests.SubscribeStrategyBacktest(c.Request.Context(), runID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	defer unsubscribe()

	status, err := h.strategyBacktests.GetStrategyBacktestRun(c.Request.Context(), runID)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	if status.Status != "completed" && status.Status != "failed" {
		if err := writeSSEEvent(c, "status", status); err != nil {
			return
		}
	}

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-keepalive.C:
			if _, err := c.Writer.WriteString(": keepalive\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case event, ok := <-stream:
			if !ok {
				return
			}
			if event.Status == nil {
				continue
			}
			if err := writeSSEEvent(c, event.Event, event.Status); err != nil {
				return
			}
		}
	}
}

// ScreenUnderlyings handles GET /api/v1/screener/underlyings.
//
//	@Summary		Screen underlyings
//	@Description	Filters and ranks underlying assets by IV, volume, and other criteria.
//	@Tags			Screener
//	@Produce		json
//	@Param			market	query		string	true	"Market (crypto-options, us-options)"
//	@Param			sort_by	query		string	false	"Sort field"
//	@Param			order	query		string	false	"Sort order (asc, desc)"
//	@Param			limit	query		int		false	"Max rows (default 50)"
//	@Param			cursor	query		string	false	"Pagination cursor"
//	@Success		200		{object}	dto.ScreenUnderlyingResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/screener/underlyings [get]
func (h *Handler) ScreenUnderlyings(c *gin.Context) {
	var req dto.ScreenUnderlyingRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.screener == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "screener provider not configured"})
		return
	}

	resp, err := h.screener.ScreenUnderlyings(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ScreenUSTurnoverIntersection handles GET /api/v1/screener/us-underlyings/turnover-intersection.
//
//	@Summary		Screen US underlyings by shared turnover
//	@Description	Intersects the top US stocks and US options underlyings by trailing turnover, then returns the most liquid shared names.
//	@Tags			Screener
//	@Produce		json
//	@Param			limit			query		int		false	"Max rows to return (default 100)"
//	@Param			lookback_days	query		int		false	"Trailing trading days to aggregate (default 20)"
//	@Param			non_etf_only	query		bool	false	"Restrict to stock underlyings with PE/PB fundamentals coverage, then exclude ETF/fund classifications from cached company profiles"
//	@Success		200				{object}	dto.ScreenUSTurnoverIntersectionResponse
//	@Failure		400				{object}	dto.ErrorResponse
//	@Failure		500				{object}	dto.ErrorResponse
//	@Router			/screener/us-underlyings/turnover-intersection [get]
func (h *Handler) ScreenUSTurnoverIntersection(c *gin.Context) {
	var req dto.ScreenUSTurnoverIntersectionRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.screener == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "screener provider not configured"})
		return
	}

	resp, err := h.screener.ScreenUSTurnoverIntersection(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// ScreenOptions handles GET /api/v1/screener/options.
//
//	@Summary		Screen option contracts
//	@Description	Filters and ranks individual option contracts by Greeks, volume, open interest, and other criteria.
//	@Tags			Screener
//	@Produce		json
//	@Param			market		query		string	true	"Market (crypto-options, us-options)"
//	@Param			underlying	query		string	false	"Filter by underlying"
//	@Param			type		query		string	false	"Option type (call, put)"
//	@Param			min_dte		query		int		false	"Minimum days to expiry"
//	@Param			max_dte		query		int		false	"Maximum days to expiry"
//	@Param			sort_by		query		string	false	"Sort field"
//	@Param			order		query		string	false	"Sort order (asc, desc)"
//	@Param			limit		query		int		false	"Max rows (default 50)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.ScreenOptionResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/screener/options [get]
func (h *Handler) ScreenOptions(c *gin.Context) {
	var req dto.ScreenOptionRequest
	if err := bindScreenOptionRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.screener == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "screener provider not configured"})
		return
	}

	resp, err := h.screener.ScreenOptions(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
