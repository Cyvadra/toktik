package api

import (
	"net/http"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// GetDeribitOptionChain handles GET /api/v1/deribit/options/chain.
//
//	@Summary		Get realtime crypto option chain snapshot via Deribit
//	@Description	Returns a realtime Deribit option chain. Option premium prices use the native premium currency; implied volatility is returned as a decimal.
//	@Tags			Deribit
//	@Produce		json
//	@Param			underlying			query		string	true	"Underlying currency (for example BTC or ETH)"
//	@Param			date				query		string	false	"Historical UTC date (YYYY-MM-DD); returns the final available local snapshot for that date"
//	@Param			expiration_date		query		string	false	"Exact expiration date (YYYY-MM-DD)"
//	@Param			expiration_date_gte	query		string	false	"Minimum expiration date"
//	@Param			expiration_date_gt	query		string	false	"Expiration date greater than"
//	@Param			expiration_date_lte	query		string	false	"Maximum expiration date"
//	@Param			expiration_date_lt	query		string	false	"Expiration date less than"
//	@Param			contract_type		query		string	false	"call or put"
//	@Param			strike_price		query		number	false	"Exact strike price"
//	@Param			strike_price_gte	query		number	false	"Minimum strike price"
//	@Param			strike_price_gt		query		number	false	"Strike price greater than"
//	@Param			strike_price_lte	query		number	false	"Maximum strike price"
//	@Param			strike_price_lt		query		number	false	"Strike price less than"
//	@Param			order				query		string	false	"Sort direction (asc or desc)"
//	@Param			sort				query		string	false	"Sort field"
//	@Param			limit				query		int		false	"Maximum contracts (max 1000; zero returns all)"
//	@Success		200					{object}	dto.DeribitOptionChainResponse
//	@Failure		400					{object}	dto.ErrorResponse
//	@Failure		501					{object}	dto.ErrorResponse
//	@Failure		502					{object}	dto.ErrorResponse
//	@Failure		504					{object}	dto.ErrorResponse
//	@Router			/deribit/options/chain [get]
func (h *Handler) GetDeribitOptionChain(c *gin.Context) {
	var req dto.DeribitOptionChainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if h.deribit == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "deribit provider not configured"})
		return
	}
	resp, err := h.deribit.QueryOptionChain(c.Request.Context(), req)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
