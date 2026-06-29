package dto

import "time"

// ReadinessResponse describes low-level API readiness.
type ReadinessResponse struct {
	Status string `json:"status"`
}

type AppDataRefreshResponse struct {
	Status            string    `json:"status"`
	TriggeredAt       time.Time `json:"triggered_at"`
	AlreadyRunning    bool      `json:"already_running,omitempty"`
	PreviousTriggerAt time.Time `json:"previous_trigger_at,omitempty"`
}

// MarketDescriptor describes one market domain exposed by the infra layer.
type MarketDescriptor struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

// MarketCatalogResponse wraps all advertised market domains.
type MarketCatalogResponse struct {
	Markets []MarketDescriptor `json:"markets"`
}

// DatasetDescriptor describes one low-level market dataset and its freshness state.
type DatasetDescriptor struct {
	Name          string     `json:"name"`
	Market        string     `json:"market"`
	Relation      string     `json:"relation"`
	Status        string     `json:"status"`
	Freshness     string     `json:"freshness"`
	RowCount      uint64     `json:"row_count"`
	AgeSeconds    *int64     `json:"age_seconds,omitempty"`
	LastTimestamp *time.Time `json:"last_timestamp,omitempty"`
}

// DatasetMarketSummary aggregates dataset health by market.
type DatasetMarketSummary struct {
	Market  string `json:"market"`
	Total   int    `json:"total"`
	Ready   int    `json:"ready"`
	Stale   int    `json:"stale"`
	Missing int    `json:"missing"`
	Empty   int    `json:"empty"`
}

// DatasetSummary aggregates dataset health counts for the current result set.
type DatasetSummary struct {
	Total   int                    `json:"total"`
	Ready   int                    `json:"ready"`
	Stale   int                    `json:"stale"`
	Missing int                    `json:"missing"`
	Empty   int                    `json:"empty"`
	Markets []DatasetMarketSummary `json:"markets,omitempty"`
}

// DatasetCatalogResponse wraps all dataset descriptors.
type DatasetCatalogResponse struct {
	Summary  DatasetSummary      `json:"summary"`
	Datasets []DatasetDescriptor `json:"datasets"`
}

// DatasetQueryRequest is the query parameters for the infra datasets endpoint.
type DatasetQueryRequest struct {
	Market string `form:"market" binding:"omitempty"`
	Status string `form:"status" binding:"omitempty"`
}
