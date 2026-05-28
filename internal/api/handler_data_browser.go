package api

import (
	"net/http"
	"reflect"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/gin-gonic/gin"
)

// ListBrowserPresets handles GET /api/v1/browser/presets.
//
//	@Summary		List data browser presets
//	@Description	Returns server-approved datasets and checks available to the internal data browser.
//	@Tags			DataBrowser
//	@Produce		json
//	@Success		200	{object}	dto.BrowserPresetResponse
//	@Failure		500	{object}	dto.ErrorResponse
//	@Router			/browser/presets [get]
func (h *Handler) ListBrowserPresets(c *gin.Context) {
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.ListBrowserPresets(c.Request.Context())
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserDatasetSchema handles GET /api/v1/browser/datasets/:dataset/schema.
//
//	@Summary		Get dataset schema
//	@Description	Returns ClickHouse column metadata for a server-approved browser dataset.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset	path		string	true	"Dataset preset name"
//	@Success		200		{object}	dto.BrowserSchemaResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/schema [get]
func (h *Handler) GetBrowserDatasetSchema(c *gin.Context) {
	var req dto.BrowserSchemaRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryDatasetSchema(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserDatasetPreview handles GET /api/v1/browser/datasets/:dataset/preview.
//
//	@Summary		Preview dataset rows
//	@Description	Returns a bounded row sample for a server-approved browser dataset.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset		path		string	true	"Dataset preset name"
//	@Param			symbol		query		string	false	"Symbol filter"
//	@Param			underlying	query		string	false	"Underlying filter"
//	@Param			from		query		string	false	"Start time"
//	@Param			to			query		string	false	"End time"
//	@Param			columns		query		string	false	"Comma-separated approved columns"
//	@Param			limit		query		int		false	"Max rows, capped at 1000"
//	@Success		200			{object}	dto.BrowserPreviewResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/preview [get]
func (h *Handler) GetBrowserDatasetPreview(c *gin.Context) {
	var req dto.BrowserPreviewRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryDatasetPreview(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserDatasetCoverage handles GET /api/v1/browser/datasets/:dataset/coverage.
//
//	@Summary		Get dataset time coverage
//	@Description	Returns first/last timestamps and daily row counts for a server-approved browser dataset.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset		path		string	true	"Dataset preset name"
//	@Param			symbol		query		string	false	"Symbol filter"
//	@Param			underlying	query		string	false	"Underlying filter"
//	@Param			from		query		string	false	"Start time"
//	@Param			to			query		string	false	"End time"
//	@Success		200			{object}	dto.BrowserCoverageResponse
//	@Failure		400			{object}	dto.ErrorResponse
//	@Failure		500			{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/coverage [get]
func (h *Handler) GetBrowserDatasetCoverage(c *gin.Context) {
	var req dto.BrowserCoverageRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryDatasetCoverage(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserFieldProfile handles GET /api/v1/browser/datasets/:dataset/field-profile.
//
//	@Summary		Profile a dataset field
//	@Description	Returns simple null/zero/empty/distinct/min/max stats for an approved field.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset	path		string	true	"Dataset preset name"
//	@Param			field	query		string	true	"Field name"
//	@Param			from	query		string	false	"Start time"
//	@Param			to		query		string	false	"End time"
//	@Success		200		{object}	dto.BrowserFieldProfileResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/field-profile [get]
func (h *Handler) GetBrowserFieldProfile(c *gin.Context) {
	var req dto.BrowserFieldProfileRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryFieldProfile(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserValidCount handles GET /api/v1/browser/datasets/:dataset/valid-count.
//
//	@Summary		Count valid rows
//	@Description	Counts valid and invalid rows using a server-approved validity check.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset	path		string	true	"Dataset preset name"
//	@Param			check	query		string	false	"Validity check name"
//	@Param			from	query		string	false	"Start time"
//	@Param			to		query		string	false	"End time"
//	@Success		200		{object}	dto.BrowserValidCountResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/valid-count [get]
func (h *Handler) GetBrowserValidCount(c *gin.Context) {
	var req dto.BrowserValidCountRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryValidCount(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetBrowserDatasetValues handles GET /api/v1/browser/datasets/:dataset/symbols.
//
//	@Summary		List dataset symbols and underlyings
//	@Description	Returns cached distinct values for the dataset's symbol and underlying fields.
//	@Tags			DataBrowser
//	@Produce		json
//	@Param			dataset	path		string	true	"Dataset preset name"
//	@Param			search	query		string	false	"Optional substring filter"
//	@Param			limit	query		int		false	"Max values per field, capped at 5000"
//	@Success		200		{object}	dto.BrowserValueListResponse
//	@Failure		400		{object}	dto.ErrorResponse
//	@Failure		500		{object}	dto.ErrorResponse
//	@Router			/browser/datasets/{dataset}/symbols [get]
func (h *Handler) GetBrowserDatasetValues(c *gin.Context) {
	var req dto.BrowserValueListRequest
	if !bindBrowserRequest(c, &req) {
		return
	}
	if h.dataBrowser == nil {
		c.JSON(http.StatusNotImplemented, dto.ErrorResponse{Error: "data browser provider not configured"})
		return
	}
	resp, err := h.dataBrowser.QueryDatasetValues(c.Request.Context(), req)
	if err != nil {
		handleServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func bindBrowserRequest(c *gin.Context, req any) bool {
	var pathReq dto.BrowserDatasetPathRequest
	if err := c.ShouldBindUri(&pathReq); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return false
	}
	if !setBrowserDataset(req, pathReq.Dataset) {
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse{Error: "browser request missing dataset field"})
		return false
	}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
		return false
	}
	return true
}

func setBrowserDataset(req any, dataset string) bool {
	value := reflect.ValueOf(req)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return false
	}
	elem := value.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}
	field := elem.FieldByName("Dataset")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return false
	}
	field.SetString(dataset)
	return true
}
