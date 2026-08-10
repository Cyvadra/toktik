package report

import (
	"encoding/json"
	"html/template"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

type htmlReportView struct {
	Title                        string
	StrategyName                 string
	Asset                        string
	Interval                     string
	DisplayIsDaily               bool
	Period                       string
	GeneratedAt                  string
	CapitalMode                  string
	CapitalProfile               string
	CapitalNote                  string
	InitialCapital               string
	FinalEquity                  string
	NetPnL                       string
	TotalReturn                  string
	AnnualizedReturn             string
	AnnualizedVolatility         string
	SharpeRatio                  string
	CalmarRatio                  string
	MaxDrawdown                  string
	StrategyPerformance          performanceMetricCardView
	AssetPerformance             performanceMetricCardView
	HasAssetPerformance          bool
	TotalFees                    string
	BarsCount                    int
	TradesCount                  int
	SpreadsCount                 int
	TradeMarkerCount             int
	SpreadEventCount             int
	EquityMin                    string
	EquityMax                    string
	DrawdownMax                  string
	HasUnderlyingChart           bool
	HasUnderlyingVolume          bool
	UnderlyingPriceMin           string
	UnderlyingPriceMax           string
	UnderlyingChartNote          string
	UnderlyingChartLabel         string
	UnderlyingChartSource        string
	UnderlyingChartOverride      bool
	UnderlyingVolumeLabel        string
	UnderlyingCandleData         template.JS
	UnderlyingVolumeData         template.JS
	UnderlyingMarkerData         template.JS
	HoverColumnsData             template.JS
	HasHoverColumns              bool
	HasFeatureColumns            bool
	EquitySeriesData             template.JS
	SettledEquitySeriesData      template.JS
	SettledFloatingProfitData    template.JS
	SettledFloatingLossData      template.JS
	SettledExposureData          template.JS
	QuoteNetValueSeriesData      template.JS
	HasQuoteNetValue             bool
	QuoteNetValueMin             string
	QuoteNetValueMax             string
	QuoteNetValueNote            string
	QuotePerformance             performanceMetricCardView
	BuyHoldPerformance           performanceMetricCardView
	HasBuyHoldPerformance        bool
	DailyQuoteNetValueSeriesData template.JS
	DailyBuyHoldSeriesData       template.JS
	HasDailyQuoteNetValue        bool
	DailyQuoteNetValueNote       string
	DailyAssetPnLSeriesData      template.JS
	HasDailyAssetPnL             bool
	DailyAssetPnLNote            string
	BuyHoldSeriesData            template.JS
	HasBuyHoldBenchmark          bool
	BuyHoldMin                   string
	BuyHoldMax                   string
	BuyHoldNote                  string
	CompressedChartPayload       template.JS
	DrawdownSeriesData           template.JS
	ActiveTimeData               template.JS
	EquityAnalysis               equityAnalysisView
	TradeOverview                tradeOverviewView
	MarketMix                    marketMixView
	SecurityMix                  []securityMixRowView
	PortfolioAttribution         portfolioAttributionView
	SpreadSummary                *spreadSummaryView
	SpreadGroups                 []spreadGroupView
	TopDrawdownGroups            []spreadGroupDrawdownView
	UngroupedSpreads             []spreadRowView
	Trades                       []tradeRowView
	Spreads                      []spreadRowView
	CompressedTradeRowsHTML      template.JS
	CompressedSpreadSectionsHTML template.JS
	NoTradeRows                  bool
	NoSpreadRows                 bool
	Notes                        []string
}

type tradeOverviewView struct {
	RawFills             string
	RoundTrips           string
	LongFills            string
	ShortFills           string
	TotalNotional        string
	GrossProfit          string
	GrossLoss            string
	NetPnL               string
	AvgPnLPerRoundTrip   string
	AvgCommissionPerFill string
}

type marketMixView struct {
	SecurityCount       string
	MarketCount         string
	OptionLegCount      string
	RegularTradeCount   string
	HasOptions          bool
	HasRegularTrades    bool
	HasMixedInstruments bool
	Description         string
}

type securityMixRowView struct {
	Security string
	Trades   string
	Notional string
	NetCash  string
}

type portfolioAttributionView struct {
	Description       string
	RegularNetCash    string
	OptionRealizedPnL string
	InstrumentRows    []portfolioAttributionRowView
	UnderlyingRows    []portfolioAttributionRowView
}

type portfolioAttributionRowView struct {
	Name     string
	Family   string
	Events   string
	Notional string
	PnL      string
	Details  string
	PnLClass string
}

type portfolioAttributionStats struct {
	RegularFills      int
	OptionLegs        int
	OptionSpreads     int
	SecurityCount     int
	UnderlyingCount   int
	RegularNetCash    float64
	OptionRealizedPnL float64
	HasRegular        bool
	HasOptions        bool
}

type equityAnalysisView struct {
	PeakEquity              string
	PeakTime                string
	LowestEquity            string
	LowestTime              string
	BestBarReturn           string
	WorstBarReturn          string
	BarReturnVolatility     string
	PositiveBars            string
	NegativeBars            string
	FlatBars                string
	MaxDrawdownDurationBars string
	MaxDrawdownDuration     string
}

type performanceMetricCardView struct {
	Name                 string
	ValueUnit            string
	AnnualizedReturn     string
	AnnualizedVolatility string
	MaxDrawdown          string
	SharpeRatio          string
	CalmarRatio          string
}

type spreadSummaryView struct {
	TotalSpreads   string
	ClosedSpreads  string
	OpenSpreads    string
	WinningSpreads string
	LosingSpreads  string
	WinRate        string
	TotalPnL       string
}

type tradeRowView struct {
	Timestamp  string
	Security   string
	Side       string
	Reason     string
	Qty        string
	FillPrice  string
	Commission string
	Slippage   string
	NetAmount  string
	SideClass  string
}

type spreadRowView struct {
	ID              int
	Tag             string
	GroupID         int
	AnchorID        string
	EventType       string
	EventClass      string
	EventTime       string
	HeaderTimeLabel string
	HeaderTime      string
	UnderlyingPrice string
	RelatedLink     string
	RelatedText     string
	EventUnix       int64
	WindowStartUnix int64
	WindowEndUnix   int64
	eventUnix       int64
	Status          string
	OpenTime        string
	CloseTime       string
	DaysHeld        string
	RealizedPnL     string
	StatusClass     string
	ReportMetrics   []spreadReportMetricView
	Legs            []spreadLegRowView
}

type spreadGroupView struct {
	ID            int
	Tag           string
	AnchorID      string
	Status        string
	StatusClass   string
	OpenTime      string
	CloseTime     string
	InitAmount    string
	HighestEquity string
	LowestEquity  string
	MaxDrawdown   string
	DecayFactor   string
	RollCount     string
	TotalPnL      string
	SpreadCount   int
	EventCount    int
	eventUnix     int64
	Spreads       []spreadRowView
}

type spreadGroupDrawdownView struct {
	ID            int
	Tag           string
	AnchorID      string
	Status        string
	StatusClass   string
	MaxDrawdown   string
	HighestEquity string
	LowestEquity  string
	TotalPnL      string
}

type spreadLegRowView struct {
	Symbol         string
	Side           string
	Type           string
	StrikePrice    string
	Expiration     string
	OpenSelect     string
	Qty            string
	EntryPrice     string
	EntryAmount    string
	EntryTime      string
	ClosePrice     string
	CloseTimeLabel string
	CloseTime      string
	CloseReason    string
	RealizedPnL    string
	SideClass      string
}

type spreadReportMetricView struct {
	Label     string
	Source    string
	Value     string
	KindLabel string
	KindClass string
}

type chartCandlePoint struct {
	Time  int64   `json:"time"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

type chartLinePoint struct {
	Time  int64    `json:"time"`
	Value *float64 `json:"value,omitempty"`
}

type chartHistogramPoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
	Color string  `json:"color,omitempty"`
}

type chartMarker struct {
	Time     int64  `json:"time"`
	Position string `json:"position"`
	Color    string `json:"color"`
	Shape    string `json:"shape"`
	Text     string `json:"text"`
}

func (p chartCandlePoint) MarshalJSON() ([]byte, error) {
	type payload struct {
		Time  int64   `json:"time"`
		Open  float64 `json:"open"`
		High  float64 `json:"high"`
		Low   float64 `json:"low"`
		Close float64 `json:"close"`
	}
	return json.Marshal(payload{
		Time:  p.Time,
		Open:  roundChartFloat(p.Open),
		High:  roundChartFloat(p.High),
		Low:   roundChartFloat(p.Low),
		Close: roundChartFloat(p.Close),
	})
}

func (p chartLinePoint) MarshalJSON() ([]byte, error) {
	type payload struct {
		Time  int64    `json:"time"`
		Value *float64 `json:"value,omitempty"`
	}
	var value *float64
	if p.Value != nil {
		rounded := roundChartFloat(*p.Value)
		value = &rounded
	}
	return json.Marshal(payload{Time: p.Time, Value: value})
}

func (p chartHistogramPoint) MarshalJSON() ([]byte, error) {
	type payload struct {
		Time  int64   `json:"time"`
		Value float64 `json:"value"`
		Color string  `json:"color,omitempty"`
	}
	return json.Marshal(payload{
		Time:  p.Time,
		Value: roundChartFloat(p.Value),
		Color: p.Color,
	})
}

type hoverColumnPayload struct {
	Source   string           `json:"source"`
	Label    string           `json:"label"`
	Decimals int              `json:"decimals"`
	Overlay  bool             `json:"overlay,omitempty"`
	Values   []chartLinePoint `json:"values"`
}

type markerKey struct {
	Time     int64
	Position string
	Color    string
	Shape    string
}

type settledEquityData struct {
	Series         []chartLinePoint
	FloatingProfit []chartHistogramPoint
	FloatingLoss   []chartHistogramPoint
	Exposure       []chartLinePoint
}

type regularPositionState struct {
	qty            float64
	avgEntryPrice  float64
	costBasis      float64
	openCommission float64
}

type spreadMetricResolver struct {
	timestamps []time.Time
	columns    []backtest.ReportColumn
	series     map[string][]float64
}

type underlyingPriceResolver struct {
	timestamps []time.Time
	series     map[string][]float64
	source     chartSeriesSource
}

type chartSeriesSource struct {
	Prefix     string
	Label      string
	SourceText string
	Override   bool
}
