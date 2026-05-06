package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	polygonpkg "github.com/Cyvadra/toktik/pkg/polygon"
	"github.com/gin-gonic/gin"
)

// sseKeepaliveInterval is how often a comment heartbeat is written to
// keep idle SSE connections from being reaped by reverse proxies.
const sseKeepaliveInterval = 25 * time.Second

// Handler holds references to service layer dependencies.
type Handler struct {
	cryptoOptions     CryptoOptionsQuerier
	usStocks          USStocksQuerier
	usOptions         USOptionsQuerier
	infra             InfraProvider
	dataBrowser       DataBrowserProvider
	features          FeatureProvider
	indicators        IndicatorSeriesProvider
	strategyBacktests StrategyBacktestProvider
	cryptoSpot        CryptoSpotQuerier
	screener          ScreenerProvider
	strategyCatalog   StrategyCatalogProvider
	factors           FactorProvider
	fundamentals      FundamentalsProvider
	polygon           PolygonProvider

	// reportsRoot is the directory on disk under which all backtest
	// HTML reports must live. Any path outside this root is rejected
	// by resolveStrategyBacktestReportPath. Must be an absolute path
	// once the handler is constructed.
	reportsRoot string
}

// NewHandler builds a Handler from a populated Deps struct.
func NewHandler(d Deps) *Handler {
	root := strings.TrimSpace(d.Config.Paths.ReportsRoot)
	if root == "" {
		root = "reports/backtests"
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &Handler{
		cryptoOptions:     d.CryptoOptions,
		usStocks:          d.USStocks,
		usOptions:         d.USOptions,
		infra:             d.Infra,
		dataBrowser:       d.DataBrowser,
		features:          d.Features,
		indicators:        d.Indicators,
		strategyBacktests: d.StrategyBacktests,
		cryptoSpot:        d.CryptoSpot,
		screener:          d.Screener,
		strategyCatalog:   d.StrategyCatalog,
		factors:           d.Factors,
		fundamentals:      d.Fundamentals,
		polygon:           d.Polygon,
		reportsRoot:       root,
	}
}

// handleServiceError maps service-level errors to appropriate HTTP responses.
func handleServiceError(c *gin.Context, err error) {
	var ve *dto.ValidationError
	if errors.As(err, &ve) {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	var ne *dto.NotFoundError
	if errors.As(err, &ne) {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		c.JSON(http.StatusGatewayTimeout, dto.ErrorResponse{Error: "request timeout"})
		return
	}
	var polygonErr *polygonpkg.HTTPStatusError
	if errors.As(err, &polygonErr) {
		status := polygonErr.StatusCode
		if status < http.StatusBadRequest || status > 599 {
			status = http.StatusBadGateway
		}
		c.JSON(status, dto.ErrorResponse{Error: polygonErrorMessage(polygonErr)})
		return
	}
	slog.Error("internal error", "error", err)
	c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "internal server error"})
}

func polygonErrorMessage(err *polygonpkg.HTTPStatusError) string {
	if err == nil {
		return "polygon upstream error"
	}
	body := strings.TrimSpace(err.Body)
	if body == "" {
		status := strings.TrimSpace(err.Status)
		if status == "" {
			return "polygon upstream error"
		}
		return status
	}
	var payload struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Status    string `json:"status"`
	}
	if json.Unmarshal([]byte(body), &payload) == nil {
		parts := make([]string, 0, 3)
		if message := strings.TrimSpace(payload.Error); message != "" {
			parts = append(parts, message)
		} else if message := strings.TrimSpace(payload.Message); message != "" {
			parts = append(parts, message)
		}
		if requestID := strings.TrimSpace(payload.RequestID); requestID != "" {
			parts = append(parts, "request_id="+requestID)
		}
		if status := strings.TrimSpace(payload.Status); status != "" && !strings.EqualFold(status, "error") {
			parts = append(parts, "status="+status)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " | ")
		}
	}
	return body
}

func bindUSOptionSymbolRequest(c *gin.Context, req *dto.USOptionSymbolRequest) error {
	if err := c.ShouldBindQuery(req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Underlying) == "" && strings.TrimSpace(req.Root) != "" {
		req.Underlying = req.Root
	}
	return nil
}

func bindScreenOptionRequest(c *gin.Context, req *dto.ScreenOptionRequest) error {
	if err := c.ShouldBindQuery(req); err != nil {
		return err
	}
	req.NormalizeAliases()
	return nil
}

func writeSSEEvent(c *gin.Context, event string, payload any) error {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := c.Writer.WriteString("event: " + event + "\n"); err != nil {
		return err
	}
	if _, err := c.Writer.WriteString("data: " + string(data) + "\n\n"); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func strategyBacktestPrimaryReportURL(runID string) string {
	return "/api/v1/backtests/runs/" + strings.TrimSpace(runID) + "/report"
}

func strategyBacktestNamedReportURL(runID, reportID string) string {
	return "/api/v1/backtests/runs/" + strings.TrimSpace(runID) + "/reports/" + strings.TrimSpace(reportID)
}

func decorateAcceptedBacktestRun(resp *dto.StrategyBacktestRunAccepted) *dto.StrategyBacktestRunAccepted {
	if resp == nil {
		return nil
	}
	copy := *resp
	copy.ReportURL = strategyBacktestPrimaryReportURL(resp.RunID)
	return &copy
}

func decorateBacktestRunStatus(resp *dto.StrategyBacktestRunStatus) *dto.StrategyBacktestRunStatus {
	if resp == nil {
		return nil
	}
	copy := *resp
	copy.ReportURL = strategyBacktestPrimaryReportURL(resp.RunID)
	if resp.Result == nil {
		return &copy
	}
	resultCopy := *resp.Result
	resultCopy.ReportURL = strategyBacktestPrimaryReportURL(resp.RunID)
	if strings.TrimSpace(resp.Result.OverviewHTMLPath) != "" {
		resultCopy.OverviewReportURL = strategyBacktestNamedReportURL(resp.RunID, "overview")
	}
	if len(resp.Result.Summaries) > 0 {
		resultCopy.Summaries = make([]dto.StrategyBacktestSummary, len(resp.Result.Summaries))
		for index, summary := range resp.Result.Summaries {
			summaryCopy := summary
			summaryCopy.ReportURL = strategyBacktestNamedReportURL(resp.RunID, strconv.Itoa(index+1))
			resultCopy.Summaries[index] = summaryCopy
		}
	}
	copy.Result = &resultCopy
	return &copy
}

func (h *Handler) resolveStrategyBacktestReportPath(status *dto.StrategyBacktestRunStatus, reportID string) (string, error) {
	if status == nil {
		return "", dto.NewNotFoundError("backtest run not found")
	}
	if status.Result == nil {
		return "", dto.NewNotFoundError("backtest report is not ready")
	}
	trimmed := strings.TrimSpace(reportID)
	var candidate string
	switch {
	case trimmed == "":
		if strings.TrimSpace(status.Result.OverviewHTMLPath) != "" {
			candidate = status.Result.OverviewHTMLPath
		} else if len(status.Result.Summaries) > 0 {
			candidate = strings.TrimSpace(status.Result.Summaries[0].HTMLPath)
		}
		if candidate == "" {
			return "", dto.NewNotFoundError("backtest report is not ready")
		}
	case strings.EqualFold(trimmed, "overview"):
		candidate = strings.TrimSpace(status.Result.OverviewHTMLPath)
		if candidate == "" {
			return "", dto.NewNotFoundError("overview report is not available")
		}
	default:
		index, err := strconv.Atoi(trimmed)
		if err != nil || index < 1 || index > len(status.Result.Summaries) {
			return "", dto.NewNotFoundError("backtest report %q not found", reportID)
		}
		candidate = strings.TrimSpace(status.Result.Summaries[index-1].HTMLPath)
		if candidate == "" {
			return "", dto.NewNotFoundError("backtest report %q not found", reportID)
		}
	}
	return h.containReportPath(candidate)
}

// containReportPath returns an absolute path that is guaranteed to live
// inside the configured reports root, defending against path traversal
// in service-layer-supplied report locations.
func (h *Handler) containReportPath(candidate string) (string, error) {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", dto.NewNotFoundError("backtest report not found")
	}
	absCandidate = filepath.Clean(absCandidate)
	root := h.reportsRoot
	if root == "" {
		// No root configured - refuse rather than risk traversal.
		return "", dto.NewNotFoundError("backtest report not found")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", dto.NewNotFoundError("backtest report not found")
	}
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, absCandidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		slog.Warn("rejecting backtest report path outside reports root",
			"path", absCandidate,
			"root", rootAbs)
		return "", dto.NewNotFoundError("backtest report not found")
	}
	return absCandidate, nil
}

func writeBacktestReportResponse(c *gin.Context, h *Handler, status *dto.StrategyBacktestRunStatus, reportID string) {
	decorated := decorateBacktestRunStatus(status)
	if decorated == nil {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{Error: "backtest run not found"})
		return
	}
	if decorated.Status == "queued" || decorated.Status == "running" {
		c.JSON(http.StatusAccepted, decorated)
		return
	}
	if decorated.Status == "failed" {
		message := strings.TrimSpace(decorated.Error)
		if message == "" {
			message = "backtest run failed"
		}
		c.JSON(http.StatusConflict, dto.ErrorResponse{Error: message})
		return
	}
	reportPath, err := h.resolveStrategyBacktestReportPath(status, reportID)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	body, err := os.ReadFile(reportPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			handleServiceError(c, dto.NewNotFoundError("backtest report file not found"))
			return
		}
		handleServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", body)
}

// GetReadiness handles GET /ready.
//
// @Summary      Check API readiness
// @Description  Returns the readiness status of the API server and its backend dependencies.
// @Tags         Infrastructure
// @Produce      json
// @Success      200  {object}  dto.ReadinessResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /ready [get]
func (h *Handler) GetReadiness(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.Readiness(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetMarkets handles GET /api/v1/infra/markets.
//
// @Summary      List supported markets
// @Description  Returns all market domains (crypto-options, us-options, etc.) with their capabilities.
// @Tags         Infrastructure
// @Produce      json
// @Success      200  {object}  dto.MarketCatalogResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /infra/markets [get]
func (h *Handler) GetMarkets(c *gin.Context) {
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.ListMarkets(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetDatasets handles GET /api/v1/infra/datasets.
//
// @Summary      List datasets and freshness
// @Description  Returns all dataset descriptors with freshness and row count metadata. Filter by market or status.
// @Tags         Infrastructure
// @Produce      json
// @Param        market  query     string  false  "Filter by market"
// @Param        status  query     string  false  "Filter by status (ready, stale, missing, empty)"
// @Success      200     {object}  dto.DatasetCatalogResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /infra/datasets [get]
func (h *Handler) GetDatasets(c *gin.Context) {
	var req dto.DatasetQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.infra == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "infra provider not configured"})
		return
	}

	resp, err := h.infra.ListDatasets(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetVolatilitySnapshot handles GET /api/v1/features/volatility-snapshot.
//
// @Summary      Get volatility snapshot
// @Description  Returns current HV and IV regime metrics for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market         query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying     query     string  true   "Underlying asset symbol"
// @Param        lookback_days  query     int     false  "IV percentile lookback window (default 252)"
// @Success      200            {object}  dto.FeatureVolatilitySnapshotResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /features/volatility-snapshot [get]
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
// @Summary      Get volatility history
// @Description  Returns a range of daily HV and IV metrics for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market         query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying     query     string  true   "Underlying asset symbol"
// @Param        from           query     string  true   "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to             query     string  true   "End date (RFC3339 or YYYY-MM-DD)"
// @Param        lookback_days  query     int     false  "IV percentile lookback window (default 252)"
// @Success      200            {object}  dto.FeatureVolatilityHistoryResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /features/volatility-history [get]
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
// @Summary      Get IV term structure snapshot
// @Description  Returns the current ATM IV term structure by expiration for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market               query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying           query     string  true   "Underlying asset symbol"
// @Param        min_days_to_expiry   query     int     false  "Min DTE filter"
// @Param        max_days_to_expiry   query     int     false  "Max DTE filter"
// @Success      200                  {object}  dto.FeatureTermStructureSnapshotResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /features/term-structure-snapshot [get]
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

// GetSkewSnapshot handles GET /api/v1/features/skew-snapshot.
//
// @Summary      Get put-call skew snapshot
// @Description  Returns the current OTM put-call IV skew by expiration for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market               query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying           query     string  true   "Underlying asset symbol"
// @Param        min_days_to_expiry   query     int     false  "Min DTE filter"
// @Param        max_days_to_expiry   query     int     false  "Max DTE filter"
// @Success      200                  {object}  dto.FeatureSkewSnapshotResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /features/skew-snapshot [get]
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

// GetLiquiditySnapshot handles GET /api/v1/features/liquidity-snapshot.
//
// @Summary      Get liquidity snapshot
// @Description  Returns option liquidity metrics by expiration bucket for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market               query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying           query     string  true   "Underlying asset symbol"
// @Param        min_days_to_expiry   query     int     false  "Min DTE filter"
// @Param        max_days_to_expiry   query     int     false  "Max DTE filter"
// @Success      200                  {object}  dto.FeatureLiquiditySnapshotResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /features/liquidity-snapshot [get]
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
// @Summary      Get liquidity history
// @Description  Returns a range of daily liquidity metrics for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market               query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying           query     string  true   "Underlying asset symbol"
// @Param        from                 query     string  true   "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to                   query     string  true   "End date (RFC3339 or YYYY-MM-DD)"
// @Param        min_days_to_expiry   query     int     false  "Min DTE filter"
// @Param        max_days_to_expiry   query     int     false  "Max DTE filter"
// @Success      200                  {object}  dto.FeatureLiquidityHistoryResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /features/liquidity-history [get]
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
// @Summary      Get event window snapshot
// @Description  Returns market-session proximity flags (holidays, early close) for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market      query     string  true  "Market (crypto-options, us-options)"
// @Param        underlying  query     string  true  "Underlying asset symbol"
// @Success      200         {object}  dto.FeatureEventWindowSnapshotResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /features/event-window-snapshot [get]
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
// @Summary      Get event window history
// @Description  Returns a range of daily event-window flags for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market      query     string  true  "Market (crypto-options, us-options)"
// @Param        underlying  query     string  true  "Underlying asset symbol"
// @Param        from        query     string  true  "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true  "End date (RFC3339 or YYYY-MM-DD)"
// @Success      200         {object}  dto.FeatureEventWindowHistoryResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /features/event-window-history [get]
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
// @Summary      Get daily feature panel
// @Description  Returns a consolidated daily panel with volatility, term structure, liquidity, and event features.
// @Tags         Features
// @Produce      json
// @Param        market               query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying           query     string  true   "Underlying asset symbol"
// @Param        from                 query     string  true   "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to                   query     string  true   "End date (RFC3339 or YYYY-MM-DD)"
// @Param        lookback_days        query     int     false  "IV percentile lookback window (default 252)"
// @Param        min_days_to_expiry   query     int     false  "Min DTE filter"
// @Param        max_days_to_expiry   query     int     false  "Max DTE filter"
// @Success      200                  {object}  dto.FeatureDailyPanelResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /features/daily-feature-panel [get]
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

// GetBars handles GET /api/v1/crypto-options/bars
//
// @Summary      Get crypto option bars
// @Description  Returns OHLCV bars with Greeks and IV for a crypto option symbol.
// @Tags         CryptoOptions
// @Produce      json
// @Param        symbol    query     string  true   "Option symbol"
// @Param        interval  query     string  true   "Bar interval (1m,5m,15m,30m,1h,2h,4h,1d)"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query     int     false  "Max rows (default 1000, max 10000)"
// @Param        cursor    query     string  false  "Opaque pagination cursor"
// @Success      200       {object}  dto.BarResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /crypto-options/bars [get]
func (h *Handler) GetBars(c *gin.Context) {
	var req dto.BarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetSymbols handles GET /api/v1/crypto-options/symbols
//
// @Summary      List crypto option symbols
// @Description  Returns available crypto option contract symbols with metadata.
// @Tags         CryptoOptions
// @Produce      json
// @Param        search      query     string  false  "Substring match filter"
// @Param        base_asset  query     string  false  "Filter by base asset"
// @Param        limit       query     int     false  "Max rows (default 100, max 1000)"
// @Param        cursor      query     string  false  "Opaque pagination cursor"
// @Success      200         {object}  dto.SymbolResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /crypto-options/symbols [get]
func (h *Handler) GetSymbols(c *gin.Context) {
	var req dto.SymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// RunIndicatorSeries handles POST /api/v1/indicators/series.
//
// @Summary      Run indicator series query
// @Description  Evaluates either a full DSL script, one or more built-in presets[], or a simplified indicators[] expression list over market bars and returns aligned series arrays.
// @Tags         Indicators
// @Accept       json
// @Produce      json
// @Param        body  body      dto.IndicatorSeriesRequest  true  "Indicator query"
// @Success      200   {object}  dto.IndicatorSeriesResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /indicators/series [post]
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
// @Summary      List indicator presets
// @Description  Returns the built-in indicator preset bundles and the plotted expressions each preset expands to.
// @Tags         Indicators
// @Produce      json
// @Success      200  {object}  dto.IndicatorPresetCatalogResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /indicators/presets [get]
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

// GetGreeks handles GET /api/v1/crypto-options/greeks
//
// @Summary      Get crypto option Greeks time-series
// @Description  Returns Greeks snapshots over time for a crypto option symbol.
// @Tags         CryptoOptions
// @Produce      json
// @Param        symbol    query     string  true   "Option symbol"
// @Param        interval  query     string  false  "Bar interval (default 1m)"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query     int     false  "Max rows (default 1000, max 10000)"
// @Param        cursor    query     string  false  "Opaque pagination cursor"
// @Success      200       {object}  dto.GreeksResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /crypto-options/greeks [get]
func (h *Handler) GetGreeks(c *gin.Context) {
	var req dto.GreeksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QueryGreeks(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// RunBacktest handles POST /api/v1/crypto-options/backtest
//
// @Summary      Run crypto options backtest (legacy, sync)
// @Description  Runs a synchronous backtest on a crypto options strategy. Deprecated: use POST /backtests/runs instead.
// @Tags         CryptoOptions
// @Accept       json
// @Produce      json
// @Param        body  body      dto.BacktestRequest  true  "Backtest configuration"
// @Success      200   {object}  object
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Deprecated
// @Router       /crypto-options/backtest [post]
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

// StartStrategyBacktest handles POST /api/v1/backtests/runs.

// ValidateStrategyBacktest handles POST /api/v1/backtests/validate.
// @Summary      Validate a strategy backtest request
// @Description  Validates strategy or DSL backtest inputs, resolves strategy metadata, and performs a prepare-time preflight check without starting an async run.
// @Tags         Backtests
// @Accept       json
// @Produce      json
// @Param        body  body      dto.StrategyBacktestRunRequest  true  "Backtest validation configuration"
// @Success      200   {object}  dto.StrategyBacktestValidationResponse
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /backtests/validate [post]
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

// @Summary      Start an async strategy backtest
// @Description  Submits a strategy backtest run that executes asynchronously. Poll the run status via GET /backtests/runs/:runID.
// @Tags         Backtests
// @Accept       json
// @Produce      json
// @Param        body  body      dto.StrategyBacktestRunRequest  true  "Backtest run configuration"
// @Success      202   {object}  dto.StrategyBacktestRunAccepted
// @Failure      400   {object}  dto.ErrorResponse
// @Failure      500   {object}  dto.ErrorResponse
// @Router       /backtests/runs [post]
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
// @Summary      Get backtest run status
// @Description  Returns the current status and results of a strategy backtest run.
// @Tags         Backtests
// @Produce      json
// @Param        runID  path      string  true  "Backtest run ID"
// @Success      200    {object}  dto.StrategyBacktestRunStatus
// @Failure      404    {object}  dto.ErrorResponse
// @Failure      500    {object}  dto.ErrorResponse
// @Router       /backtests/runs/{runID} [get]
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
// @Summary      Get primary backtest report
// @Description  Returns the reserved HTML report for a backtest run. Before completion, it returns 202 with the current run status.
// @Tags         Backtests
// @Produce      html
// @Produce      json
// @Param        runID  path      string  true  "Backtest run ID"
// @Success      200    {string}  string
// @Success      202    {object}  dto.StrategyBacktestRunStatus
// @Failure      404    {object}  dto.ErrorResponse
// @Failure      409    {object}  dto.ErrorResponse
// @Failure      500    {object}  dto.ErrorResponse
// @Router       /backtests/runs/{runID}/report [get]
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
// @Summary      Get a named backtest report
// @Description  Returns an HTML report variant for a backtest run. Use reportID=overview for the overview page or 1..N for per-strategy detail pages.
// @Tags         Backtests
// @Produce      html
// @Produce      json
// @Param        runID     path      string  true  "Backtest run ID"
// @Param        reportID  path      string  true  "Report selector: overview or 1..N"
// @Success      200       {string}  string
// @Success      202       {object}  dto.StrategyBacktestRunStatus
// @Failure      404       {object}  dto.ErrorResponse
// @Failure      409       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /backtests/runs/{runID}/reports/{reportID} [get]
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
// @Summary      Stream backtest run events (SSE)
// @Description  Returns a server-sent events stream for real-time backtest progress updates.
// @Tags         Backtests
// @Produce      text/event-stream
// @Param        runID  path  string  true  "Backtest run ID"
// @Success      200    "SSE stream of backtest events"
// @Failure      404    {object}  dto.ErrorResponse
// @Failure      500    {object}  dto.ErrorResponse
// @Router       /backtests/runs/{runID}/events [get]
func (h *Handler) StreamStrategyBacktestEvents(c *gin.Context) {
	if h.strategyBacktests == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy backtest provider not configured"})
		return
	}

	runID := c.Param("runID")

	// Subscribe BEFORE fetching the snapshot status, so we cannot lose
	// any event published in the gap between the two calls.
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

// GetUSStockBars handles GET /api/v1/markets/us-stocks/bars.
//
// @Summary      Get US stock bars
// @Description  Returns OHLCV bars for a US stock symbol.
// @Tags         USStocks
// @Produce      json
// @Param        symbol    query     string  true   "Stock ticker symbol"
// @Param        interval  query     string  true   "Bar interval"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query     int     false  "Max rows (default 1000)"
// @Param        cursor    query     string  false  "Pagination cursor"
// @Success      200       {object}  dto.USStockBarResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /markets/us-stocks/bars [get]
func (h *Handler) GetUSStockBars(c *gin.Context) {
	var req dto.USStockBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usStocks == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-stocks provider not configured"})
		return
	}

	resp, err := h.usStocks.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSStockSymbols handles GET /api/v1/markets/us-stocks/symbols.
//
// @Summary      List US stock symbols
// @Description  Returns available US stock ticker symbols.
// @Tags         USStocks
// @Produce      json
// @Param        search  query     string  false  "Substring match filter"
// @Param        limit   query     int     false  "Max rows (default 100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Success      200     {object}  dto.USStockSymbolResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /markets/us-stocks/symbols [get]
func (h *Handler) GetUSStockSymbols(c *gin.Context) {
	var req dto.USStockSymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usStocks == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-stocks provider not configured"})
		return
	}

	resp, err := h.usStocks.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionBars handles GET /api/v1/markets/us-options/bars.
//
// @Summary      Get US option bars
// @Description  Returns OHLCV bars for a US listed option contract.
// @Tags         USOptions
// @Produce      json
// @Param        symbol    query     string  true   "Option contract symbol (Polygon OPRA ticker or raw OCC payload without O: prefix)"
// @Param        interval  query     string  true   "Bar interval"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        session   query     string  false  "Session filter (1m only: regular, all, extended)"
// @Param        limit     query     int     false  "Max rows (default 1000)"
// @Param        cursor    query     string  false  "Pagination cursor"
// @Success      200       {object}  dto.USOptionBarResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /markets/us-options/bars [get]
func (h *Handler) GetUSOptionBars(c *gin.Context) {
	var req dto.USOptionBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		if strings.TrimSpace(c.Query("underlying")) != "" && strings.TrimSpace(req.Symbol) == "" {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "symbol is required; underlying is not supported on this endpoint"})
			return
		}
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionSymbols handles GET /api/v1/markets/us-options/symbols.
//
// @Summary      List US option symbols
// @Description  Returns available US listed option contract symbols.
// @Tags         USOptions
// @Produce      json
// @Param        underlying  query     string  false  "Filter by underlying ticker symbol"
// @Param        root        query     string  false  "Legacy alias for underlying"
// @Param        search      query     string  false  "Substring match filter"
// @Param        limit       query     int     false  "Max rows (default 100)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Success      200     {object}  dto.USOptionSymbolResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /markets/us-options/symbols [get]
func (h *Handler) GetUSOptionSymbols(c *gin.Context) {
	var req dto.USOptionSymbolRequest
	if err := bindUSOptionSymbolRequest(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionGreeks handles GET /api/v1/markets/us-options/greeks.
//
// @Summary      Get US option Greeks time-series
// @Description  Returns Greeks snapshots over time for a US listed option contract.
// @Tags         USOptions
// @Produce      json
// @Param        symbol    query     string  true   "Option contract symbol (Polygon OPRA ticker or raw OCC payload without O: prefix)"
// @Param        interval  query     string  false  "Bar interval (default 1h)"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        session   query     string  false  "Session filter (1m only: regular, all, extended)"
// @Param        limit     query     int     false  "Max rows (default 1000)"
// @Param        cursor    query     string  false  "Pagination cursor"
// @Success      200       {object}  dto.USOptionGreeksResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /markets/us-options/greeks [get]
func (h *Handler) GetUSOptionGreeks(c *gin.Context) {
	var req dto.USOptionGreeksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		if strings.TrimSpace(c.Query("underlying")) != "" && strings.TrimSpace(req.Symbol) == "" {
			c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "symbol is required; underlying is not supported on this endpoint"})
			return
		}
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryGreeks(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetUSOptionChain handles GET /api/v1/markets/us-options/chain.
//
// @Summary      Get US option chain
// @Description  Returns option chain snapshots for a US underlying. If from/to are omitted, the latest available snapshot is returned.
// @Tags         USOptions
// @Produce      json
// @Param        underlying  query     string  true   "Underlying ticker symbol"
// @Param        expiration  query     string  false  "Filter contracts by expiration date (YYYY-MM-DD)"
// @Param        from        query     string  false  "Snapshot window start (RFC3339 or YYYY-MM-DD); defaults to latest available snapshot"
// @Param        to          query     string  false  "Snapshot window end (RFC3339 or YYYY-MM-DD); defaults to latest available snapshot"
// @Param        interval    query     string  false  "Chain interval (default 1d)"  Enums(5m,15m,30m,1h,2h,4h,1d)
// @Param        limit       query     int     false  "Max contracts (default 100)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Success      200         {object}  dto.USOptionChainResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /markets/us-options/chain [get]
func (h *Handler) GetUSOptionChain(c *gin.Context) {
	var req dto.USOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.usOptions == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "us-options provider not configured"})
		return
	}

	resp, err := h.usOptions.QueryChain(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	if resp == nil {
		resp = &dto.USOptionChainResponse{Data: make([]dto.USOptionChainSnapshot, 0)}
	}

	c.JSON(http.StatusOK, resp)
}

// --- Crypto Spot handlers ---

// GetCryptoSpotBars handles GET /api/v1/markets/crypto-spot/bars.
//
// @Summary      Get crypto spot bars
// @Description  Returns OHLCV bars for a crypto spot pair.
// @Tags         CryptoSpot
// @Produce      json
// @Param        symbol    query     string  true   "Spot pair symbol (e.g. BTCUSDT)"
// @Param        interval  query     string  true   "Bar interval (15m, 1h, 4h, 1d)"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query     int     false  "Max rows (default 1000)"
// @Param        cursor    query     string  false  "Pagination cursor"
// @Success      200       {object}  dto.CryptoSpotBarResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /markets/crypto-spot/bars [get]
func (h *Handler) GetCryptoSpotBars(c *gin.Context) {
	var req dto.CryptoSpotBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.cryptoSpot == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "crypto-spot provider not configured"})
		return
	}

	resp, err := h.cryptoSpot.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetCryptoSpotSymbols handles GET /api/v1/markets/crypto-spot/symbols.
//
// @Summary      List crypto spot symbols
// @Description  Returns available crypto spot pair symbols.
// @Tags         CryptoSpot
// @Produce      json
// @Param        search  query     string  false  "Substring match filter"
// @Param        limit   query     int     false  "Max rows (default 100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Success      200     {object}  dto.CryptoSpotSymbolResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /markets/crypto-spot/symbols [get]
func (h *Handler) GetCryptoSpotSymbols(c *gin.Context) {
	var req dto.CryptoSpotSymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.cryptoSpot == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "crypto-spot provider not configured"})
		return
	}

	resp, err := h.cryptoSpot.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// --- Feature history handlers ---

// GetTermStructureHistory handles GET /api/v1/features/term-structure-history.
//
// @Summary      Get term structure history
// @Description  Returns historical IV term structure data for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market      query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying  query     string  true   "Underlying asset symbol"
// @Param        from        query     string  true   "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true   "End date (RFC3339 or YYYY-MM-DD)"
// @Param        limit       query     int     false  "Max rows (default 1000)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Success      200         {object}  dto.FeatureTermStructureHistoryResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /features/term-structure-history [get]
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

// GetSkewHistory handles GET /api/v1/features/skew-history.
//
// @Summary      Get skew history
// @Description  Returns historical put-call skew data for an underlying.
// @Tags         Features
// @Produce      json
// @Param        market      query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying  query     string  true   "Underlying asset symbol"
// @Param        from        query     string  true   "Start date (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true   "End date (RFC3339 or YYYY-MM-DD)"
// @Param        limit       query     int     false  "Max rows (default 1000)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Success      200         {object}  dto.FeatureSkewHistoryResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /features/skew-history [get]
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

// --- Screener handlers ---

// ScreenUnderlyings handles GET /api/v1/screener/underlyings.
//
// @Summary      Screen underlyings
// @Description  Filters and ranks underlying assets by IV, volume, and other criteria.
// @Tags         Screener
// @Produce      json
// @Param        market     query     string   true   "Market (crypto-options, us-options)"
// @Param        sort_by    query     string   false  "Sort field"
// @Param        order      query     string   false  "Sort order (asc, desc)"
// @Param        limit      query     int      false  "Max rows (default 50)"
// @Param        cursor     query     string   false  "Pagination cursor"
// @Success      200        {object}  dto.ScreenUnderlyingResponse
// @Failure      400        {object}  dto.ErrorResponse
// @Failure      500        {object}  dto.ErrorResponse
// @Router       /screener/underlyings [get]
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

// ScreenOptions handles GET /api/v1/screener/options.
//
// @Summary      Screen option contracts
// @Description  Filters and ranks individual option contracts by Greeks, volume, open interest, and other criteria.
// @Tags         Screener
// @Produce      json
// @Param        market      query     string  true   "Market (crypto-options, us-options)"
// @Param        underlying  query     string  false  "Filter by underlying"
// @Param        type        query     string  false  "Option type (call, put)"
// @Param        min_dte     query     int     false  "Minimum days to expiry"
// @Param        max_dte     query     int     false  "Maximum days to expiry"
// @Param        sort_by     query     string  false  "Sort field"
// @Param        order       query     string  false  "Sort order (asc, desc)"
// @Param        limit       query     int     false  "Max rows (default 50)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Success      200         {object}  dto.ScreenOptionResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /screener/options [get]
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

// --- Strategy Catalog handlers ---

// ListStrategies handles GET /api/v1/strategies.
//
// @Summary      List registered strategies
// @Description  Returns all available strategy templates with metadata and group classification.
// @Tags         Strategies
// @Produce      json
// @Param        group  query     string  false  "Filter by strategy group"
// @Success      200    {object}  dto.StrategyCatalogResponse
// @Failure      400    {object}  dto.ErrorResponse
// @Failure      500    {object}  dto.ErrorResponse
// @Router       /strategies [get]
func (h *Handler) ListStrategies(c *gin.Context) {
	var req dto.StrategyCatalogListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.strategyCatalog == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "strategy catalog provider not configured"})
		return
	}

	resp, err := h.strategyCatalog.ListStrategies(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// --- Crypto Option Chain handler ---

// GetCryptoOptionChain handles GET /api/v1/markets/crypto-options/chain.
//
// @Summary      Get crypto option chain snapshots
// @Description  Returns option chain snapshots for a crypto base asset, grouped by timestamp.
// @Tags         CryptoOptions
// @Produce      json
// @Param        base_asset  query     string  true   "Base asset (e.g. BTC, ETH)"
// @Param        from        query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        interval    query     string  false  "Chain interval (default 1d)"  Enums(5m,15m,30m,1h,2h,3h,4h,6h,8h,12h,1d)
// @Param        limit       query     int     false  "Max rows (default 1000, max 10000)"
// @Param        cursor      query     string  false  "Opaque pagination cursor"
// @Success      200         {object}  dto.CryptoOptionChainResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /markets/crypto-options/chain [get]
func (h *Handler) GetCryptoOptionChain(c *gin.Context) {
	var req dto.CryptoOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}

	resp, err := h.cryptoOptions.QueryChain(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// --- Factor handlers ---

// ListFactors handles GET /api/v1/factors.
//
// @Summary      List available factor feeds
// @Description  Returns metadata for all registered factor feeds, including supported symbols, windows, and fields.
// @Tags         Factors
// @Produce      json
// @Success      200  {object}  dto.FactorCatalogResponse
// @Failure      500  {object}  dto.ErrorResponse
// @Router       /factors [get]
func (h *Handler) ListFactors(c *gin.Context) {
	if h.factors == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "factor provider not configured"})
		return
	}

	resp, err := h.factors.ListFactors(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetFactorBars handles GET /api/v1/factors/bars.
//
// @Summary      Query factor time-series data
// @Description  Returns OHLC bars for a specific factor feed, symbol, and time window.
// @Tags         Factors
// @Produce      json
// @Param        name    query     string  true   "Factor feed name (e.g. dvol)"
// @Param        symbol  query     string  true   "Symbol (e.g. BTC)"
// @Param        window  query     string  true   "Time window (e.g. 1h, 1d)"
// @Param        from    query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to      query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit   query     int     false  "Max rows (default 1000, max 10000)"
// @Param        cursor  query     string  false  "Opaque pagination cursor"
// @Success      200     {object}  dto.FactorBarResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /factors/bars [get]
func (h *Handler) GetFactorBars(c *gin.Context) {
	var req dto.FactorBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.factors == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "factor provider not configured"})
		return
	}

	resp, err := h.factors.QueryFactorBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
