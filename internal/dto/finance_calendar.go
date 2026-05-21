package dto

type CalendarEventDTO struct {
	Type             string   `json:"type"`
	Symbol           string   `json:"symbol,omitempty"`
	Date             string   `json:"date"`
	Time             string   `json:"time,omitempty"`
	Title            string   `json:"title"`
	Country          string   `json:"country,omitempty"`
	Currency         string   `json:"currency,omitempty"`
	Impact           string   `json:"impact,omitempty"`
	Unit             string   `json:"unit,omitempty"`
	Previous         *float64 `json:"previous,omitempty"`
	Estimate         *float64 `json:"estimate,omitempty"`
	Actual           *float64 `json:"actual,omitempty"`
	Change           *float64 `json:"change,omitempty"`
	ChangePercentage *float64 `json:"change_percentage,omitempty"`
	EPSActual        *float64 `json:"eps_actual,omitempty"`
	EPSEstimated     *float64 `json:"eps_estimated,omitempty"`
	RevenueActual    *int64   `json:"revenue_actual,omitempty"`
	RevenueEstimated *int64   `json:"revenue_estimated,omitempty"`
	Dividend         *float64 `json:"dividend,omitempty"`
	AdjDividend      *float64 `json:"adj_dividend,omitempty"`
	Yield            *float64 `json:"yield,omitempty"`
	Frequency        string   `json:"frequency,omitempty"`
	RecordDate       string   `json:"record_date,omitempty"`
	PaymentDate      string   `json:"payment_date,omitempty"`
	DeclarationDate  string   `json:"declaration_date,omitempty"`
	Company          string   `json:"company,omitempty"`
	Exchange         string   `json:"exchange,omitempty"`
	Action           string   `json:"action,omitempty"`
	Shares           *float64 `json:"shares,omitempty"`
	PriceRange       string   `json:"price_range,omitempty"`
	MarketCap        *float64 `json:"market_cap,omitempty"`
	Numerator        *float64 `json:"numerator,omitempty"`
	Denominator      *float64 `json:"denominator,omitempty"`
	SplitType        string   `json:"split_type,omitempty"`
	FiscalYear       *int     `json:"fiscal_year,omitempty"`
	Period           string   `json:"period,omitempty"`
	LinkJSON         string   `json:"link_json,omitempty"`
	LinkXLSX         string   `json:"link_xlsx,omitempty"`
}

type EconomicCalendarResponse struct {
	Data []CalendarEventDTO `json:"data"`
}

type StockCalendarRequest struct {
	Symbols []string `json:"symbols" binding:"required"`
}

type StockCalendarResponse struct {
	Symbols []string           `json:"symbols"`
	Data    []CalendarEventDTO `json:"data"`
}
