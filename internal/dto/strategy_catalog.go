package dto

// StrategyCatalogListRequest defines query parameters for listing strategies.
type StrategyCatalogListRequest struct {
	Group string `form:"group" binding:"omitempty"`
}

// StrategyCatalogEntry describes one registered strategy.
type StrategyCatalogEntry struct {
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases,omitempty"`
	Groups       []string `json:"groups,omitempty"`
	UsesOptions  bool     `json:"uses_options"`
	RegularTrade string   `json:"regular_trade"`
	ProfileLabel string   `json:"profile_label"`
}

// StrategyCatalogResponse wraps the strategy catalog list.
type StrategyCatalogResponse struct {
	Data []StrategyCatalogEntry `json:"data"`
}
