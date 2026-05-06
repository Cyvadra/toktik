package dto

import "time"

// BrowserDatasetDescriptor describes one dataset exposed by the internal data browser.
type BrowserDatasetDescriptor struct {
	Name            string   `json:"name"`
	Market          string   `json:"market"`
	Relation        string   `json:"relation"`
	TimeField       string   `json:"time_field,omitempty"`
	SymbolField     string   `json:"symbol_field,omitempty"`
	UnderlyingField string   `json:"underlying_field,omitempty"`
	Fields          []string `json:"fields,omitempty"`
	Checks          []string `json:"checks,omitempty"`
}

// BrowserPresetResponse lists all server-approved browser datasets.
type BrowserPresetResponse struct {
	Datasets []BrowserDatasetDescriptor `json:"datasets"`
}

// BrowserDatasetPathRequest binds the dataset path parameter.
type BrowserDatasetPathRequest struct {
	Dataset string `uri:"dataset" binding:"required"`
}

// BrowserSchemaRequest requests schema metadata for a dataset.
type BrowserSchemaRequest struct {
	Dataset string `uri:"dataset" binding:"required"`
}

// BrowserColumn describes one ClickHouse column.
type BrowserColumn struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Position          uint64 `json:"position"`
	DefaultKind       string `json:"default_kind,omitempty"`
	DefaultExpression string `json:"default_expression,omitempty"`
	Comment           string `json:"comment,omitempty"`
	CodecExpression   string `json:"codec_expression,omitempty"`
	IsNullable        bool   `json:"is_nullable"`
}

// BrowserSchemaResponse returns schema metadata for a dataset relation.
type BrowserSchemaResponse struct {
	Dataset BrowserDatasetDescriptor `json:"dataset"`
	Columns []BrowserColumn          `json:"columns"`
}

// BrowserPreviewRequest requests a bounded sample of rows.
type BrowserPreviewRequest struct {
	Dataset    string `uri:"dataset" binding:"required"`
	Symbol     string `form:"symbol" binding:"omitempty"`
	Underlying string `form:"underlying" binding:"omitempty"`
	From       string `form:"from" binding:"omitempty"`
	To         string `form:"to" binding:"omitempty"`
	Columns    string `form:"columns" binding:"omitempty"`
	Limit      int    `form:"limit" binding:"omitempty"`
}

// BrowserPreviewResponse returns rows as generic JSON objects.
type BrowserPreviewResponse struct {
	Dataset BrowserDatasetDescriptor `json:"dataset"`
	Columns []string                 `json:"columns"`
	Data    []map[string]any         `json:"data"`
}

// BrowserCoverageRequest requests time coverage for one dataset, optionally scoped to a symbol.
type BrowserCoverageRequest struct {
	Dataset    string `uri:"dataset" binding:"required"`
	Symbol     string `form:"symbol" binding:"omitempty"`
	Underlying string `form:"underlying" binding:"omitempty"`
	From       string `form:"from" binding:"omitempty"`
	To         string `form:"to" binding:"omitempty"`
}

// BrowserDailyCoverage describes row coverage for one date.
type BrowserDailyCoverage struct {
	Date     time.Time `json:"date"`
	RowCount uint64    `json:"row_count"`
}

// BrowserCoverageResponse describes first/last timestamps and daily counts.
type BrowserCoverageResponse struct {
	Dataset        BrowserDatasetDescriptor `json:"dataset"`
	RowCount       uint64                   `json:"row_count"`
	FirstTimestamp *time.Time               `json:"first_timestamp,omitempty"`
	LastTimestamp  *time.Time               `json:"last_timestamp,omitempty"`
	Daily          []BrowserDailyCoverage   `json:"daily,omitempty"`
}

// BrowserFieldProfileRequest requests simple validity/profile stats for one field.
type BrowserFieldProfileRequest struct {
	Dataset string `uri:"dataset" binding:"required"`
	Field   string `form:"field" binding:"required"`
	From    string `form:"from" binding:"omitempty"`
	To      string `form:"to" binding:"omitempty"`
	Limit   int    `form:"limit" binding:"omitempty"`
}

// BrowserFieldProfileResponse returns generic profile stats for a field.
type BrowserFieldProfileResponse struct {
	Dataset       BrowserDatasetDescriptor `json:"dataset"`
	Field         string                   `json:"field"`
	Type          string                   `json:"type"`
	RowCount      uint64                   `json:"row_count"`
	NullCount     uint64                   `json:"null_count"`
	ZeroCount     *uint64                  `json:"zero_count,omitempty"`
	EmptyCount    *uint64                  `json:"empty_count,omitempty"`
	DistinctCount uint64                   `json:"distinct_count"`
	Min           any                      `json:"min,omitempty"`
	Max           any                      `json:"max,omitempty"`
}

// BrowserValidCountRequest requests a server-approved validity check count.
type BrowserValidCountRequest struct {
	Dataset string `uri:"dataset" binding:"required"`
	Check   string `form:"check" binding:"omitempty"`
	From    string `form:"from" binding:"omitempty"`
	To      string `form:"to" binding:"omitempty"`
}

// BrowserValidCountResponse returns valid and invalid counts for a check.
type BrowserValidCountResponse struct {
	Dataset      BrowserDatasetDescriptor `json:"dataset"`
	Check        string                   `json:"check"`
	RowCount     uint64                   `json:"row_count"`
	ValidCount   uint64                   `json:"valid_count"`
	InvalidCount uint64                   `json:"invalid_count"`
}

// BrowserValueListRequest requests distinct symbol/underlying values for a dataset.
type BrowserValueListRequest struct {
	Dataset string `uri:"dataset" binding:"required"`
	Search  string `form:"search" binding:"omitempty"`
	Limit   int    `form:"limit" binding:"omitempty"`
}

// BrowserValueItem describes one distinct dataset value.
type BrowserValueItem struct {
	Value         string     `json:"value"`
	RowCount      uint64     `json:"row_count"`
	LastTimestamp *time.Time `json:"last_timestamp,omitempty"`
}

// BrowserValueFieldList groups values by the backing dataset field.
type BrowserValueFieldList struct {
	Field  string             `json:"field"`
	Values []BrowserValueItem `json:"values"`
}

// BrowserValueListResponse returns cached distinct values for symbol-like fields.
type BrowserValueListResponse struct {
	Dataset BrowserDatasetDescriptor `json:"dataset"`
	Fields  []BrowserValueFieldList  `json:"fields"`
	Cached  bool                     `json:"cached"`
}
