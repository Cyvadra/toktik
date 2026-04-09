package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetPolygonStockSnapshot handles GET /api/v1/polygon/stocks/snapshot.
//
// @Summary      Get realtime US stock snapshot via Polygon
// @Description  Proxies Polygon stock snapshot data. This endpoint bypasses the platform database and is intended for realtime client reads.
// @Tags         Polygon
// @Produce      json
// @Param        symbol  query     string  true  "Stock ticker symbol"
// @Success      200     {object}  dto.PolygonStockSnapshotResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /polygon/stocks/snapshot [get]
func (h *Handler) GetPolygonStockSnapshot(c *gin.Context) {
	var req dto.PolygonStockSnapshotRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryStockSnapshot(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonStockAggregates handles GET /api/v1/polygon/stocks/aggregates.
//
// @Summary      Get US stock aggregate bars via Polygon
// @Description  Proxies Polygon aggregate bars for historical or near-realtime stock data with short-TTL caching based on the requested time window.
// @Tags         Polygon
// @Produce      json
// @Param        ticker      query     string  true   "Stock ticker symbol"
// @Param        multiplier  query     int     false  "Aggregate multiplier"
// @Param        timespan    query     string  true   "Timespan (minute,hour,day,...)"
// @Param        from        query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        adjusted    query     bool    false  "Adjusted results"
// @Param        sort        query     string  false  "Sort direction"
// @Param        limit       query     int     false  "Page size"
// @Success      200         {object}  dto.PolygonAggregateResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /polygon/stocks/aggregates [get]
func (h *Handler) GetPolygonStockAggregates(c *gin.Context) {
	h.handlePolygonAggregate(c, false)
}

// GetPolygonStockQuotes handles GET /api/v1/polygon/stocks/quotes.
//
// @Summary      Get US stock quotes via Polygon
// @Description  Proxies Polygon quote history for a stock ticker. Use near-now windows for realtime quote polling.
// @Tags         Polygon
// @Produce      json
// @Param        symbol         query     string  true   "Stock ticker symbol"
// @Param        timestamp      query     string  false  "Exact timestamp"
// @Param        timestamp_gte  query     string  false  "Timestamp greater than or equal"
// @Param        timestamp_gt   query     string  false  "Timestamp greater than"
// @Param        timestamp_lte  query     string  false  "Timestamp less than or equal"
// @Param        timestamp_lt   query     string  false  "Timestamp less than"
// @Param        order          query     string  false  "Sort order"
// @Param        sort           query     string  false  "Sort field"
// @Param        limit          query     int     false  "Page size"
// @Success      200            {object}  dto.PolygonQuoteResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /polygon/stocks/quotes [get]
func (h *Handler) GetPolygonStockQuotes(c *gin.Context) {
	var req dto.PolygonStockQuotesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryStockQuotes(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonStockTrades handles GET /api/v1/polygon/stocks/trades.
//
// @Summary      Get US stock trades via Polygon
// @Description  Proxies Polygon trade history for a stock ticker. Use this when you need prints instead of NBBO quotes.
// @Tags         Polygon
// @Produce      json
// @Param        symbol         query     string  true   "Stock ticker symbol"
// @Param        timestamp      query     string  false  "Exact timestamp"
// @Param        timestamp_gte  query     string  false  "Timestamp greater than or equal"
// @Param        timestamp_gt   query     string  false  "Timestamp greater than"
// @Param        timestamp_lte  query     string  false  "Timestamp less than or equal"
// @Param        timestamp_lt   query     string  false  "Timestamp less than"
// @Param        order          query     string  false  "Sort order"
// @Param        sort           query     string  false  "Sort field"
// @Param        limit          query     int     false  "Page size"
// @Success      200            {object}  dto.PolygonTradeResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /polygon/stocks/trades [get]
func (h *Handler) GetPolygonStockTrades(c *gin.Context) {
	var req dto.PolygonStockTradesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryStockTrades(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonOptionContract handles GET /api/v1/polygon/options/contract.
//
// @Summary      Get US option contract metadata via Polygon
// @Description  Returns static contract metadata for a single OCC-style Polygon option ticker.
// @Tags         Polygon
// @Produce      json
// @Param        ticker  query     string  true  "Option ticker, e.g. O:SPY251219C00650000"
// @Success      200     {object}  dto.PolygonOptionContractResponse
// @Failure      400     {object}  dto.ErrorResponse
// @Failure      500     {object}  dto.ErrorResponse
// @Router       /polygon/options/contract [get]
func (h *Handler) GetPolygonOptionContract(c *gin.Context) {
	var req dto.PolygonOptionContractRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryOptionContract(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonOptionChain handles GET /api/v1/polygon/options/chain.
//
// @Summary      Get realtime US option chain snapshot via Polygon
// @Description  Proxies Polygon option chain snapshots for a US underlying. This is the recommended endpoint for realtime option surface reads.
// @Tags         Polygon
// @Produce      json
// @Param        underlying           query     string   true   "Underlying ticker symbol"
// @Param        expiration_date      query     string   false  "Exact expiration date"
// @Param        expiration_date_gte  query     string   false  "Minimum expiration date"
// @Param        expiration_date_gt   query     string   false  "Expiration date greater than"
// @Param        expiration_date_lte  query     string   false  "Maximum expiration date"
// @Param        expiration_date_lt   query     string   false  "Expiration date less than"
// @Param        contract_type        query     string   false  "call or put"
// @Param        strike_price         query     number   false  "Exact strike price"
// @Param        strike_price_gte     query     number   false  "Minimum strike price"
// @Param        strike_price_gt      query     number   false  "Strike price greater than"
// @Param        strike_price_lte     query     number   false  "Maximum strike price"
// @Param        strike_price_lt      query     number   false  "Strike price less than"
// @Param        order                query     string   false  "Sort direction"
// @Param        sort                 query     string   false  "Sort field"
// @Param        limit                query     int      false  "Page size"
// @Success      200                  {object}  dto.PolygonOptionChainResponse
// @Failure      400                  {object}  dto.ErrorResponse
// @Failure      500                  {object}  dto.ErrorResponse
// @Router       /polygon/options/chain [get]
func (h *Handler) GetPolygonOptionChain(c *gin.Context) {
	var req dto.PolygonOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryOptionChain(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonOptionAggregates handles GET /api/v1/polygon/options/aggregates.
//
// @Summary      Get US option aggregate bars via Polygon
// @Description  Proxies Polygon aggregate bars for a US option contract over a requested time window.
// @Tags         Polygon
// @Produce      json
// @Param        ticker      query     string  true   "Option ticker"
// @Param        multiplier  query     int     false  "Aggregate multiplier"
// @Param        timespan    query     string  true   "Timespan (minute,hour,day,...)"
// @Param        from        query     string  true   "Start time (RFC3339 or YYYY-MM-DD)"
// @Param        to          query     string  true   "End time (RFC3339 or YYYY-MM-DD)"
// @Param        adjusted    query     bool    false  "Adjusted results"
// @Param        sort        query     string  false  "Sort direction"
// @Param        limit       query     int     false  "Page size"
// @Success      200         {object}  dto.PolygonAggregateResponse
// @Failure      400         {object}  dto.ErrorResponse
// @Failure      500         {object}  dto.ErrorResponse
// @Router       /polygon/options/aggregates [get]
func (h *Handler) GetPolygonOptionAggregates(c *gin.Context) {
	h.handlePolygonAggregate(c, true)
}

// GetPolygonOptionQuotes handles GET /api/v1/polygon/options/quotes.
//
// @Summary      Get US option quotes via Polygon
// @Description  Proxies Polygon quote history for a US option contract.
// @Tags         Polygon
// @Produce      json
// @Param        ticker         query     string  true   "Option ticker"
// @Param        timestamp      query     string  false  "Exact timestamp"
// @Param        timestamp_gte  query     string  false  "Timestamp greater than or equal"
// @Param        timestamp_gt   query     string  false  "Timestamp greater than"
// @Param        timestamp_lte  query     string  false  "Timestamp less than or equal"
// @Param        timestamp_lt   query     string  false  "Timestamp less than"
// @Param        order          query     string  false  "Sort order"
// @Param        sort           query     string  false  "Sort field"
// @Param        limit          query     int     false  "Page size"
// @Success      200            {object}  dto.PolygonQuoteResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /polygon/options/quotes [get]
func (h *Handler) GetPolygonOptionQuotes(c *gin.Context) {
	var req dto.PolygonOptionQuotesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryOptionQuotes(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetPolygonOptionTrades handles GET /api/v1/polygon/options/trades.
//
// @Summary      Get US option trades via Polygon
// @Description  Proxies Polygon trade history for a US option contract.
// @Tags         Polygon
// @Produce      json
// @Param        ticker         query     string  true   "Option ticker"
// @Param        timestamp      query     string  false  "Exact timestamp"
// @Param        timestamp_gte  query     string  false  "Timestamp greater than or equal"
// @Param        timestamp_gt   query     string  false  "Timestamp greater than"
// @Param        timestamp_lte  query     string  false  "Timestamp less than or equal"
// @Param        timestamp_lt   query     string  false  "Timestamp less than"
// @Param        order          query     string  false  "Sort order"
// @Param        sort           query     string  false  "Sort field"
// @Param        limit          query     int     false  "Page size"
// @Success      200            {object}  dto.PolygonTradeResponse
// @Failure      400            {object}  dto.ErrorResponse
// @Failure      500            {object}  dto.ErrorResponse
// @Router       /polygon/options/trades [get]
func (h *Handler) GetPolygonOptionTrades(c *gin.Context) {
	var req dto.PolygonOptionTradesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	resp, err := h.polygon.QueryOptionTrades(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) handlePolygonAggregate(c *gin.Context, option bool) {
	var req dto.PolygonAggregateRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.polygon == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "polygon provider not configured"})
		return
	}
	var (
		resp *dto.PolygonAggregateResponse
		err  error
	)
	if option {
		resp, err = h.polygon.QueryOptionAggregates(c.Request.Context(), req)
	} else {
		resp, err = h.polygon.QueryStockAggregates(c.Request.Context(), req)
	}
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
