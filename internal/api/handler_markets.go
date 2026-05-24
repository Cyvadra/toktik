package api

import (
	"net/http"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetBars handles GET /api/v1/markets/crypto-options/bars
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
// @Router       /markets/crypto-options/bars [get]
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

// GetSymbols handles GET /api/v1/markets/crypto-options/symbols
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
// @Router       /markets/crypto-options/symbols [get]
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

// GetGreeks handles GET /api/v1/markets/crypto-options/greeks
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
// @Router       /markets/crypto-options/greeks [get]
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

// GetUSStockBars handles GET /api/v1/markets/us-stocks/bars.
//
// @Summary      Get US stock bars
// @Description  Returns OHLCV bars for a US stock symbol, optionally enriched with point-in-time fundamentals aligned to each bar and cached company profile metadata when available.
// @Tags         USStocks
// @Produce      json
// @Param        symbol    query     string  true   "Stock ticker symbol"
// @Param        interval  query     string  true   "Bar interval"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        factor    query     []string  false  "Optional fundamentals to align onto each bar (repeat or comma-separated, e.g. pe,pb). PE/PB are recomputed from each bar close using the latest known filing-derived denominator."
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
// @Description  Returns available US stock ticker symbols, optionally including cached company profile metadata on each symbol row when available.
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

// GetForexBars handles GET /api/v1/markets/forex/bars.
//
// @Summary      Get forex bars
// @Description  Returns OHLCV bars for a forex or metal-linked FX symbol.
// @Tags         Forex
// @Produce      json
// @Param        symbol    query     string  true   "Forex symbol (e.g. EURUSD, USDJPY, XAUUSD)"
// @Param        interval  query     string  true   "Bar interval"
// @Param        from      query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to        query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        limit     query     int     false  "Max rows (default 1000)"
// @Param        cursor    query     string  false  "Pagination cursor"
// @Success      200       {object}  dto.ForexBarResponse
// @Failure      400       {object}  dto.ErrorResponse
// @Failure      500       {object}  dto.ErrorResponse
// @Router       /markets/forex/bars [get]
func (h *Handler) GetForexBars(c *gin.Context) {
	var req dto.ForexBarRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.forex == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "forex provider not configured"})
		return
	}

	resp, err := h.forex.QueryBars(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetForexSymbols handles GET /api/v1/markets/forex/symbols.
//
// @Summary      List forex symbols
// @Description  Returns available forex and metal-linked FX symbols.
// @Tags         Forex
// @Produce      json
// @Param        search  query     string  false  "Substring match filter"
// @Param        limit   query     int     false  "Max rows (default 100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Success      200     {object}  dto.ForexSymbolResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /markets/forex/symbols [get]
func (h *Handler) GetForexSymbols(c *gin.Context) {
	var req dto.ForexSymbolRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.forex == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "forex provider not configured"})
		return
	}

	resp, err := h.forex.QuerySymbols(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

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
