package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

var usStockLogoSymbolPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,15}$`)

// GetUSStockLogo handles GET /utils/us-stocks/logos/{symbol}.{ext}.
//
//	@Summary		Get US stock logo image
//	@Description	Returns a US stock logo as an image-like URL. Use `/utils/us-stocks/logos/AAPL.png` directly in image tags or clients. The endpoint does not require an API key. If the logo is cached in MySQL it is returned immediately; otherwise the server tries FMP in real time, stores the logo as base64 in MySQL, and returns it. Symbols that failed FMP lookup in the last 24 hours return a generated default PNG icon.
//	@Tags			Utilities
//	@Produce		png
//	@Param			symbol	path		string	true	"Ticker plus image suffix, for example AAPL.png. Supported suffixes: .png, .jpg, .jpeg, .svg, .webp."
//	@Success		200		{file}		binary	"Logo image bytes"
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		501		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/utils/us-stocks/logos/{symbol} [get]
func (h *Handler) GetUSStockLogo(c *gin.Context) {
	if h.logos == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "logo provider not configured"})
		return
	}
	symbol, ok := trimImageSuffix(c.Param("symbol"))
	if !ok {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "logo URL must end with an image suffix"})
		return
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !usStockLogoSymbolPattern.MatchString(symbol) {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: "invalid stock symbol"})
		return
	}
	logo, err := h.logos.GetLogo(c.Request.Context(), symbol)
	if err != nil {
		h.handleServiceError(c, err)
		return
	}
	if logo.Default {
		c.Header("X-Toktik-Logo-Default", "true")
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, logo.ContentType, logo.Data)
}

func trimImageSuffix(value string) (string, bool) {
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".svg", ".webp"} {
		if strings.HasSuffix(strings.ToLower(value), suffix) {
			return strings.TrimSpace(value[:len(value)-len(suffix)]), true
		}
	}
	return "", false
}
