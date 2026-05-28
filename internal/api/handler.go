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
	forex             ForexQuerier
	screener          ScreenerProvider
	strategyCatalog   StrategyCatalogProvider
	factors           FactorProvider
	fundamentals      FundamentalsProvider
	macro             MacroProvider
	financeCalendar   FinanceCalendarProvider
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
		forex:             d.Forex,
		screener:          d.Screener,
		strategyCatalog:   d.StrategyCatalog,
		factors:           d.Factors,
		fundamentals:      d.Fundamentals,
		macro:             d.Macro,
		financeCalendar:   d.FinanceCalendar,
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

// GetUSStockBars handles GET /api/v1/markets/us-stocks/bars.
//
//	@Summary		Get US stock bars
//	@Description	Returns OHLCV bars for a US stock symbol, optionally enriched with point-in-time fundamentals aligned to each bar and cached company profile metadata when available.
//	@Tags			USStocks
//	@Produce		json
//	@Param			symbol		query		string		true	"Stock ticker symbol"
//	@Param			interval	query		string		true	"Bar interval"
//	@Param			from		query		string		true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string		true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			factor		query		[]string	false	"Optional fundamentals to align onto each bar (repeat or comma-separated, e.g. pe,pb). PE/PB are recomputed from each bar close using the latest known filing-derived denominator."
//	@Param			limit		query		int			false	"Max rows (default 1000)"
//	@Param			cursor		query		string		false	"Pagination cursor"
//	@Success		200			{object}	dto.USStockBarResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/us-stocks/bars [get]

// GetUSStockSymbols handles GET /api/v1/markets/us-stocks/symbols.
//
//	@Summary		List US stock symbols
//	@Description	Returns available US stock ticker symbols, optionally including cached company profile metadata on each symbol row when available.
//	@Tags			USStocks
//	@Produce		json
//	@Param			search	query		string	false	"Substring match filter"
//	@Param			limit	query		int		false	"Max rows (default 100)"
//	@Param			cursor	query		string	false	"Pagination cursor"
//	@Success		200		{object}	dto.USStockSymbolResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/markets/us-stocks/symbols [get]

// GetUSOptionBars handles GET /api/v1/markets/us-options/bars.
//
//	@Summary		Get US option bars
//	@Description	Returns OHLCV bars for a US listed option contract.
//	@Tags			USOptions
//	@Produce		json
//	@Param			symbol		query		string	true	"Option contract symbol (Polygon OPRA ticker or raw OCC payload without O: prefix)"
//	@Param			interval	query		string	true	"Bar interval"
//	@Param			from		query		string	true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			session		query		string	false	"Session filter (1m only: regular, all, extended)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.USOptionBarResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/us-options/bars [get]

// GetUSOptionSymbols handles GET /api/v1/markets/us-options/symbols.
//
//	@Summary		List US option symbols
//	@Description	Returns available US listed option contract symbols.
//	@Tags			USOptions
//	@Produce		json
//	@Param			underlying	query		string	false	"Filter by underlying ticker symbol"
//	@Param			root		query		string	false	"Legacy alias for underlying"
//	@Param			search		query		string	false	"Substring match filter"
//	@Param			limit		query		int		false	"Max rows (default 100)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.USOptionSymbolResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/us-options/symbols [get]

// GetUSOptionGreeks handles GET /api/v1/markets/us-options/greeks.
//
//	@Summary		Get US option Greeks time-series
//	@Description	Returns Greeks snapshots over time for a US listed option contract.
//	@Tags			USOptions
//	@Produce		json
//	@Param			symbol		query		string	true	"Option contract symbol (Polygon OPRA ticker or raw OCC payload without O: prefix)"
//	@Param			interval	query		string	false	"Bar interval (default 1h)"
//	@Param			from		query		string	true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			session		query		string	false	"Session filter (1m only: regular, all, extended)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.USOptionGreeksResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/us-options/greeks [get]

// GetUSOptionChain handles GET /api/v1/markets/us-options/chain.
//
//	@Summary		Get US option chain
//	@Description	Returns option chain snapshots for a US underlying. If from/to are omitted, the latest available snapshot is returned.
//	@Tags			USOptions
//	@Produce		json
//	@Param			underlying	query		string	true	"Underlying ticker symbol"
//	@Param			expiration	query		string	false	"Filter contracts by expiration date (YYYY-MM-DD)"
//	@Param			from		query		string	false	"Snapshot window start (RFC3339 or YYYY-MM-DD); defaults to latest available snapshot"
//	@Param			to			query		string	false	"Snapshot window end (RFC3339 or YYYY-MM-DD); defaults to latest available snapshot"
//	@Param			interval	query		string	false	"Chain interval (default 1d)"	Enums(5m,15m,30m,1h,2h,4h,1d)
//	@Param			limit		query		int		false	"Max contracts (default 100)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.USOptionChainResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/us-options/chain [get]

// --- Crypto Spot handlers ---

// GetCryptoSpotBars handles GET /api/v1/markets/crypto-spot/bars.
//
//	@Summary		Get crypto spot bars
//	@Description	Returns OHLCV bars for a crypto spot pair.
//	@Tags			CryptoSpot
//	@Produce		json
//	@Param			symbol		query		string	true	"Spot pair symbol (e.g. BTCUSDT)"
//	@Param			interval	query		string	true	"Bar interval (15m, 1h, 4h, 1d)"
//	@Param			from		query		string	true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.CryptoSpotBarResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/crypto-spot/bars [get]

// GetCryptoSpotSymbols handles GET /api/v1/markets/crypto-spot/symbols.
//
//	@Summary		List crypto spot symbols
//	@Description	Returns available crypto spot pair symbols.
//	@Tags			CryptoSpot
//	@Produce		json
//	@Param			search	query		string	false	"Substring match filter"
//	@Param			limit	query		int		false	"Max rows (default 100)"
//	@Param			cursor	query		string	false	"Pagination cursor"
//	@Success		200		{object}	dto.CryptoSpotSymbolResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/markets/crypto-spot/symbols [get]

// GetForexBars handles GET /api/v1/markets/forex/bars.
//
//	@Summary		Get forex bars
//	@Description	Returns OHLCV bars for a forex or metal-linked FX symbol.
//	@Tags			Forex
//	@Produce		json
//	@Param			symbol		query		string	true	"Forex symbol (e.g. EURUSD, USDJPY, XAUUSD)"
//	@Param			interval	query		string	true	"Bar interval"
//	@Param			from		query		string	true	"Start time (RFC3339 or YYYY-MM-DD)"
//	@Param			to			query		string	true	"End time (RFC3339 or YYYY-MM-DD)"
//	@Param			limit		query		int		false	"Max rows (default 1000)"
//	@Param			cursor		query		string	false	"Pagination cursor"
//	@Success		200			{object}	dto.ForexBarResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/markets/forex/bars [get]

// GetForexSymbols handles GET /api/v1/markets/forex/symbols.
//
//	@Summary		List forex symbols
//	@Description	Returns available forex and metal-linked FX symbols.
//	@Tags			Forex
//	@Produce		json
//	@Param			search	query		string	false	"Substring match filter"
//	@Param			limit	query		int		false	"Max rows (default 100)"
//	@Param			cursor	query		string	false	"Pagination cursor"
//	@Success		200		{object}	dto.ForexSymbolResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/markets/forex/symbols [get]

// --- Feature history handlers ---
