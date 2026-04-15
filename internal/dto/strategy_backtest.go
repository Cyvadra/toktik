package dto

import (
	"fmt"
	"time"
)

type StrategyBacktestRunRequest struct {
	Market                   string                      `json:"market,omitempty"`
	Instrument               string                      `json:"instrument,omitempty"`
	Asset                    string                      `json:"asset" binding:"required"`
	Interval                 string                      `json:"interval,omitempty"`
	From                     string                      `json:"from" binding:"required"`
	To                       string                      `json:"to" binding:"required"`
	Capital                  float64                     `json:"capital" binding:"required"`
	Strategy                 string                      `json:"strategy,omitempty"`
	DSL                      string                      `json:"dsl,omitempty"`
	DSLParams                map[string]interface{}      `json:"dsl_params,omitempty"`
	DSLProfile               *StrategyBacktestDSLProfile `json:"dsl_profile,omitempty"`
	SignalSource             string                      `json:"signal_source,omitempty"`
	CommissionModel          string                      `json:"commission_model,omitempty"`
	CommissionValue          float64                     `json:"commission_value,omitempty"`
	SlippagePct              float64                     `json:"slippage_pct,omitempty"`
	HTMLOutput               string                      `json:"html_output,omitempty"`
	PositionSize             float64                     `json:"position_size,omitempty"`
	MaxHoldHours             float64                     `json:"max_hold_hours,omitempty"`
	TargetExpiryDays         int                         `json:"target_expiry_days,omitempty"`
	MinExpiryDays            int                         `json:"min_expiry_days,omitempty"`
	MinPremium               float64                     `json:"min_premium,omitempty"`
	ShortDeltaMin            float64                     `json:"short_delta_min,omitempty"`
	ShortDeltaMax            float64                     `json:"short_delta_max,omitempty"`
	LongDeltaMin             float64                     `json:"long_delta_min,omitempty"`
	LongDeltaMax             float64                     `json:"long_delta_max,omitempty"`
	SpreadEntryPriceMode     string                      `json:"spread_entry_price_mode,omitempty"`
	SpreadExitPriceMode      string                      `json:"spread_exit_price_mode,omitempty"`
	SpreadValuationPriceMode string                      `json:"spread_valuation_price_mode,omitempty"`
	MAPeriod                 int                         `json:"ma_period,omitempty"`
	PThreshold               float64                     `json:"p_threshold,omitempty"`
	Direction                string                      `json:"direction,omitempty"`
}

type StrategyBacktestDSLProfile struct {
	UsesOptions  *bool  `json:"uses_options,omitempty"`
	RegularTrade string `json:"regular_trade,omitempty"`
}

type StrategyBacktestDSLParam struct {
	Name    string      `json:"name"`
	Title   string      `json:"title,omitempty"`
	Type    string      `json:"type"`
	Default interface{} `json:"default,omitempty"`
	Min     *float64    `json:"min,omitempty"`
	Max     *float64    `json:"max,omitempty"`
	Step    *float64    `json:"step,omitempty"`
	Options []string    `json:"options,omitempty"`
}

type StrategyBacktestValidationRuntime struct {
	Market               string `json:"market,omitempty"`
	Instrument           string `json:"instrument,omitempty"`
	CapitalMode          string `json:"capital_mode,omitempty"`
	CapitalUnit          string `json:"capital_unit,omitempty"`
	CapitalExplanation   string `json:"capital_explanation,omitempty"`
	OptionsChainRequired bool   `json:"options_chain_required"`
	OptionsUnit          string `json:"options_unit,omitempty"`
	RegularTradeSummary  string `json:"regular_trade_summary,omitempty"`
}

type StrategyBacktestValidationItem struct {
	CanonicalName string                             `json:"canonical_name,omitempty"`
	DisplayName   string                             `json:"display_name"`
	ProfileLabel  string                             `json:"profile_label,omitempty"`
	ProfileSource string                             `json:"profile_source,omitempty"`
	UsesOptions   bool                               `json:"uses_options"`
	RegularTrade  string                             `json:"regular_trade,omitempty"`
	Runtime       *StrategyBacktestValidationRuntime `json:"runtime,omitempty"`
	DSLParams     []StrategyBacktestDSLParam         `json:"dsl_params,omitempty"`
	Warnings      []string                           `json:"warnings,omitempty"`
}

type StrategyBacktestValidationResponse struct {
	StrategyLabel string                           `json:"strategy_label,omitempty"`
	StrategyCount int                              `json:"strategy_count"`
	Strategies    []StrategyBacktestValidationItem `json:"strategies"`
}

type StrategyBacktestRunAccepted struct {
	RunID     string    `json:"run_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	StatusURL string    `json:"status_url"`
	EventsURL string    `json:"events_url"`
	ReportURL string    `json:"report_url,omitempty"`
}

type StrategyBacktestProgress struct {
	Phase     string    `json:"phase,omitempty"`
	Current   int       `json:"current"`
	Total     int       `json:"total"`
	Percent   float64   `json:"percent"`
	Message   string    `json:"message,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Completed bool      `json:"completed"`
}

type StrategyBacktestSpreadSummary struct {
	TotalSpreads   int     `json:"total_spreads"`
	ClosedSpreads  int     `json:"closed_spreads"`
	OpenSpreads    int     `json:"open_spreads"`
	TotalPnL       float64 `json:"total_pnl"`
	WinningSpreads int     `json:"winning_spreads"`
	LosingSpreads  int     `json:"losing_spreads"`
	WinRate        float64 `json:"win_rate"`
}

type StrategyBacktestSummary struct {
	StrategyName     string                         `json:"strategy_name"`
	StartTime        time.Time                      `json:"start_time"`
	EndTime          time.Time                      `json:"end_time"`
	BarsCount        int                            `json:"bars_count"`
	InitialCapital   float64                        `json:"initial_capital"`
	FinalEquity      float64                        `json:"final_equity"`
	AccountUnit      string                         `json:"account_unit,omitempty"`
	CapitalMode      string                         `json:"capital_mode,omitempty"`
	CapitalProfile   string                         `json:"capital_profile,omitempty"`
	CapitalNote      string                         `json:"capital_note,omitempty"`
	TotalReturn      float64                        `json:"total_return"`
	AnnualizedReturn float64                        `json:"annualized_return"`
	SharpeRatio      float64                        `json:"sharpe_ratio"`
	CalmarRatio      float64                        `json:"calmar_ratio"`
	MaxDrawdown      float64                        `json:"max_drawdown"`
	TotalTrades      int                            `json:"total_trades"`
	WinningTrades    int                            `json:"winning_trades"`
	LosingTrades     int                            `json:"losing_trades"`
	WinRate          float64                        `json:"win_rate"`
	ProfitFactor     float64                        `json:"profit_factor"`
	AvgWin           float64                        `json:"avg_win"`
	AvgLoss          float64                        `json:"avg_loss"`
	TotalFees        float64                        `json:"total_fees"`
	HTMLPath         string                         `json:"html_path,omitempty"`
	ReportURL        string                         `json:"report_url,omitempty"`
	SpreadSummary    *StrategyBacktestSpreadSummary `json:"spread_summary,omitempty"`
}

type StrategyBacktestRunResult struct {
	Summaries         []StrategyBacktestSummary `json:"summaries"`
	OverviewHTMLPath  string                    `json:"overview_html_path,omitempty"`
	ReportURL         string                    `json:"report_url,omitempty"`
	OverviewReportURL string                    `json:"overview_report_url,omitempty"`
}

type StrategyBacktestRunStatus struct {
	RunID       string                     `json:"run_id"`
	Status      string                     `json:"status"`
	Request     StrategyBacktestRunRequest `json:"request"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
	StartedAt   *time.Time                 `json:"started_at,omitempty"`
	CompletedAt *time.Time                 `json:"completed_at,omitempty"`
	Progress    *StrategyBacktestProgress  `json:"progress,omitempty"`
	Result      *StrategyBacktestRunResult `json:"result,omitempty"`
	Error       string                     `json:"error,omitempty"`
	ReportURL   string                     `json:"report_url,omitempty"`
}

type StrategyBacktestSSEvent struct {
	Event  string                     `json:"event"`
	Status *StrategyBacktestRunStatus `json:"status,omitempty"`
}

type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

func NewNotFoundError(format string, a ...interface{}) *NotFoundError {
	return &NotFoundError{Message: fmt.Sprintf(format, a...)}
}
