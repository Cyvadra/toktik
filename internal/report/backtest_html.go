package report

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

// HTMLMeta carries extra presentation metadata for a generated backtest report.
type HTMLMeta struct {
	Asset               string
	Interval            string
	GeneratedAt         time.Time
	ChartMarket         string
	ChartSymbol         string
	ChartInterval       string
	ChartSeriesPrefix   string
	ChartSelectionLabel string
}

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

type combinedHTMLReportView struct {
	Title       string
	Asset       string
	Interval    string
	Period      string
	GeneratedAt string
	Strategies  []combinedHTMLStrategyView
}

type combinedHTMLStrategyView struct {
	AnchorID string
	Report   htmlReportView
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

var (
	_ = combinedHTMLReportView{}
	_ = combinedHTMLStrategyView{}
	_ = buildCombinedHTMLView
	_ = slugToken
	_ = currency4
	_ = signedCurrency
	_ = nullableCurrency4
	_ = combinedHTMLTemplate
)

// WriteBacktestHTML renders a self-contained static HTML report for a backtest result.
func WriteBacktestHTML(path string, result *backtest.Result, meta HTMLMeta) error {
	view := buildHTMLView(result, meta)
	tmpl, err := template.New("backtest-report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("parse report template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return fmt.Errorf("render report template: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write report html: %w", err)
	}
	return nil
}

// WriteCombinedBacktestHTML renders multiple backtest results into one self-contained HTML report.
func WriteCombinedBacktestHTML(path string, results []*backtest.Result, meta HTMLMeta) error {
	if len(results) == 0 {
		return nil
	}
	merged := mergeBacktestResults(results)
	if merged == nil {
		return nil
	}
	return WriteBacktestHTML(path, merged, meta)
}

func mergeBacktestResults(results []*backtest.Result) *backtest.Result {
	valid := make([]*backtest.Result, 0, len(results))
	for _, result := range results {
		if result != nil {
			valid = append(valid, result)
		}
	}
	if len(valid) == 0 {
		return nil
	}

	merged := &backtest.Result{
		StrategyName: "Combined",
		Series:       make(map[string][]float64),
	}

	strategyNames := make([]string, 0, len(valid))
	for idx, result := range valid {
		name := strings.TrimSpace(result.StrategyName)
		if name == "" {
			name = fmt.Sprintf("strategy-%d", idx+1)
		}
		strategyNames = append(strategyNames, name)

		if merged.StartTime.IsZero() || (!result.StartTime.IsZero() && result.StartTime.Before(merged.StartTime)) {
			merged.StartTime = result.StartTime
		}
		if merged.EndTime.IsZero() || result.EndTime.After(merged.EndTime) {
			merged.EndTime = result.EndTime
		}
		if merged.AccountUnit == "" {
			merged.AccountUnit = result.AccountUnit
		} else if result.AccountUnit != "" && merged.AccountUnit != result.AccountUnit {
			merged.AccountUnit = ""
		}

		merged.InitialCapital += result.InitialCapital
		merged.FinalEquity += result.FinalEquity
		merged.TotalFees += result.TotalFees
		if result.BarsCount > merged.BarsCount {
			merged.BarsCount = result.BarsCount
		}

		merged.Trades = append(merged.Trades, result.Trades...)

		for _, spread := range result.SpreadPositions {
			spreadCopy := spread
			merged.SpreadPositions = append(merged.SpreadPositions, spreadCopy)
		}

		if len(merged.Series) == 0 {
			for key, series := range result.Series {
				copied := make([]float64, len(series))
				copy(copied, series)
				merged.Series[key] = copied
			}
		}
	}

	merged.StrategyName = "Combined: " + strings.Join(strategyNames, " + ")

	sort.Slice(merged.Trades, func(i, j int) bool {
		if merged.Trades[i].Timestamp.Equal(merged.Trades[j].Timestamp) {
			if merged.Trades[i].Security.Symbol != merged.Trades[j].Security.Symbol {
				return merged.Trades[i].Security.Symbol < merged.Trades[j].Security.Symbol
			}
			return merged.Trades[i].ID < merged.Trades[j].ID
		}
		return merged.Trades[i].Timestamp.Before(merged.Trades[j].Timestamp)
	})

	sort.Slice(merged.SpreadPositions, func(i, j int) bool {
		if merged.SpreadPositions[i].OpenTime.Equal(merged.SpreadPositions[j].OpenTime) {
			return merged.SpreadPositions[i].ID < merged.SpreadPositions[j].ID
		}
		return merged.SpreadPositions[i].OpenTime.Before(merged.SpreadPositions[j].OpenTime)
	})
	for i := range merged.SpreadPositions {
		merged.SpreadPositions[i].ID = i + 1
	}

	referenceTimes := selectReferenceTimeline(valid)
	merged.Timestamps, merged.EquityCurve = mergeEquityCurve(valid, referenceTimes)
	if len(merged.Timestamps) > 0 {
		merged.StartTime = merged.Timestamps[0]
		merged.EndTime = merged.Timestamps[len(merged.Timestamps)-1]
		merged.FinalEquity = merged.EquityCurve[len(merged.EquityCurve)-1]
		merged.BarsCount = len(merged.Timestamps)
	}

	if merged.InitialCapital > 0 {
		merged.TotalReturn = (merged.FinalEquity - merged.InitialCapital) / merged.InitialCapital
		years := merged.EndTime.Sub(merged.StartTime).Hours() / (365 * 24)
		if years > 0 && merged.FinalEquity > 0 {
			merged.AnnualizedReturn = math.Pow(merged.FinalEquity/merged.InitialCapital, 1.0/years) - 1
		}
	}

	backtest.ApplyDerivedPerformance(merged)

	merged.TradeOverview = backtest.ComputeTradeOverview(merged.Trades)
	merged.EquityAnalysis = backtest.ComputeEquityAnalysis(merged.EquityCurve, merged.Timestamps)
	backtest.ApplyTradeSummary(merged)
	merged.SpreadSummary = computeSpreadSummaryFromReports(merged.SpreadPositions)

	return merged
}

func selectReferenceTimeline(results []*backtest.Result) []time.Time {
	for _, result := range results {
		if result == nil || len(result.Timestamps) == 0 {
			continue
		}
		times := make([]time.Time, len(result.Timestamps))
		copy(times, result.Timestamps)
		sort.Slice(times, func(i, j int) bool {
			return times[i].Before(times[j])
		})
		return times
	}
	return nil
}

func mergeEquityCurve(results []*backtest.Result, referenceTimes []time.Time) ([]time.Time, []float64) {
	if len(referenceTimes) == 0 {
		return nil, nil
	}

	type equityPoint struct {
		time  time.Time
		value float64
	}

	pointsByResult := make([][]equityPoint, 0, len(results))
	for _, result := range results {
		n := minInt(len(result.Timestamps), len(result.EquityCurve))
		points := make([]equityPoint, 0, n)
		for i := 0; i < n; i++ {
			if !chartValueValid(result.EquityCurve[i]) {
				continue
			}
			points = append(points, equityPoint{time: result.Timestamps[i], value: result.EquityCurve[i]})
		}
		sort.Slice(points, func(i, j int) bool {
			return points[i].time.Before(points[j].time)
		})
		pointsByResult = append(pointsByResult, points)
	}

	indices := make([]int, len(pointsByResult))
	last := make([]float64, len(pointsByResult))
	for i, result := range results {
		last[i] = result.InitialCapital
	}

	times := make([]time.Time, 0, len(referenceTimes))
	equity := make([]float64, 0, len(referenceTimes))
	for _, ts := range referenceTimes {
		total := 0.0
		for i := range pointsByResult {
			points := pointsByResult[i]
			for indices[i] < len(points) && !points[indices[i]].time.After(ts) {
				last[i] = points[indices[i]].value
				indices[i]++
			}
			total += last[i]
		}
		times = append(times, ts)
		equity = append(equity, total)
	}

	return times, equity
}

func computeSpreadSummaryFromReports(spreads []backtest.SpreadPositionReport) *backtest.SpreadSummary {
	if len(spreads) == 0 {
		return nil
	}

	summary := &backtest.SpreadSummary{TotalSpreads: len(spreads)}
	for _, spread := range spreads {
		if spread.CloseTime != nil {
			summary.ClosedSpreads++
			summary.TotalPnL += spread.RealizedPnL
			if spread.RealizedPnL > 0 {
				summary.WinningSpreads++
			} else if spread.RealizedPnL < 0 {
				summary.LosingSpreads++
			}
		} else {
			summary.OpenSpreads++
		}
	}
	if summary.ClosedSpreads > 0 {
		summary.WinRate = float64(summary.WinningSpreads) / float64(summary.ClosedSpreads)
	}
	return summary
}

func buildHTMLView(result *backtest.Result, meta HTMLMeta) htmlReportView {
	if meta.GeneratedAt.IsZero() {
		meta.GeneratedAt = time.Now()
	}
	chartSource := resolveChartSeriesSource(result, meta)

	drawdown := drawdownSeries(result.EquityCurve)
	displayIsDaily := hasSubDailyInterval(result.Timestamps)
	equitySeries := buildLineSeries(result.Timestamps, result.EquityCurve)
	drawdownLine := buildLineSeries(result.Timestamps, drawdown)
	if displayIsDaily {
		equitySeries = compressLineSeriesDailyEOD(equitySeries)
		drawdownLine = compressLineSeriesDailyEOD(drawdownLine)
	}
	view := htmlReportView{
		Title:                        fmt.Sprintf("%s 回测报告", result.StrategyName),
		StrategyName:                 result.StrategyName,
		Asset:                        meta.Asset,
		Interval:                     meta.Interval,
		DisplayIsDaily:               displayIsDaily,
		Period:                       fmt.Sprintf("%s 至 %s", formatDate(result.StartTime), formatDate(result.EndTime)),
		GeneratedAt:                  meta.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		CapitalMode:                  fallbackText(strings.TrimSpace(result.CapitalMode), strings.TrimSpace(result.AccountUnit)),
		CapitalProfile:               fallbackText(strings.TrimSpace(result.CapitalProfile), "未标注"),
		CapitalNote:                  fallbackText(strings.TrimSpace(result.CapitalNote), "-capital 按账户单位解释。"),
		InitialCapital:               amount(result.InitialCapital, result.AccountUnit),
		FinalEquity:                  amount(result.FinalEquity, result.AccountUnit),
		NetPnL:                       signedAmount(result.FinalEquity-result.InitialCapital, result.AccountUnit),
		TotalReturn:                  pct(result.TotalReturn),
		AnnualizedReturn:             pct(result.AnnualizedReturn),
		AnnualizedVolatility:         pct(result.AnnualizedVolatility),
		SharpeRatio:                  decimal(result.SharpeRatio),
		CalmarRatio:                  ratio(result.CalmarRatio),
		MaxDrawdown:                  pct(result.MaxDrawdown),
		StrategyPerformance:          buildPerformanceMetricCard("策略 U 本位", quoteMetricUnitLabel(result), result.QuotePerformance),
		TotalFees:                    amount(result.TotalFees, result.AccountUnit),
		BarsCount:                    result.BarsCount,
		TradesCount:                  len(result.Trades),
		SpreadsCount:                 len(result.SpreadPositions),
		NoTradeRows:                  len(result.Trades) == 0,
		NoSpreadRows:                 len(result.SpreadPositions) == 0,
		UnderlyingCandleData:         template.JS("[]"),
		UnderlyingVolumeData:         template.JS("[]"),
		UnderlyingMarkerData:         template.JS("[]"),
		HoverColumnsData:             template.JS("[]"),
		ActiveTimeData:               template.JS("[]"),
		EquitySeriesData:             marshalJS(equitySeries),
		SettledEquitySeriesData:      template.JS("[]"),
		SettledFloatingProfitData:    template.JS("[]"),
		SettledFloatingLossData:      template.JS("[]"),
		SettledExposureData:          template.JS("[]"),
		QuoteNetValueSeriesData:      template.JS("[]"),
		DailyQuoteNetValueSeriesData: template.JS("[]"),
		DailyBuyHoldSeriesData:       template.JS("[]"),
		DailyAssetPnLSeriesData:      template.JS("[]"),
		BuyHoldSeriesData:            template.JS("[]"),
		CompressedChartPayload:       template.JS("{}"),
		CompressedTradeRowsHTML:      template.JS(`""`),
		CompressedSpreadSectionsHTML: template.JS(`""`),
		DrawdownSeriesData:           marshalJS(drawdownLine),
	}
	if result.QuotePerformance == nil {
		view.StrategyPerformance = buildPerformanceMetricCard("策略账户本位", strings.TrimSpace(result.AccountUnit), result.AccountPerformance)
	}
	if result.BuyHoldPerformance != nil {
		view.HasAssetPerformance = true
		view.AssetPerformance = buildPerformanceMetricCard("Buy & Hold", quoteMetricUnitLabel(result), result.BuyHoldPerformance)
	}

	settledData := buildSettledEquityData(result)
	if displayIsDaily {
		settledData = buildSettledEquityDataDailyEOD(result)
	}
	view.SettledEquitySeriesData = marshalJS(settledData.Series)
	view.SettledFloatingProfitData = marshalJS(settledData.FloatingProfit)
	view.SettledFloatingLossData = marshalJS(settledData.FloatingLoss)
	view.SettledExposureData = marshalJS(settledData.Exposure)

	minEq, maxEq := minMax(result.EquityCurve)
	view.EquityMin = amount(minEq, result.AccountUnit)
	view.EquityMax = amount(maxEq, result.AccountUnit)
	view.DrawdownMax = pct(maxValue(drawdown))

	quoteNetValueSeries := buildQuoteNetValueSeries(result)
	if displayIsDaily {
		quoteNetValueSeries = compressLineSeriesDailyEOD(quoteNetValueSeries)
	}
	if len(quoteNetValueSeries) > 0 {
		view.HasQuoteNetValue = true
		view.QuoteNetValueSeriesData = marshalJS(quoteNetValueSeries)
		quoteValues := linePointValues(quoteNetValueSeries)
		minQuote, maxQuote := minMax(quoteValues)
		view.QuoteNetValueMin = currency(minQuote)
		view.QuoteNetValueMax = currency(maxQuote)
		view.QuotePerformance = buildPerformanceMetricCard("策略 U 本位", quoteMetricUnitLabel(result), result.QuotePerformance)
		if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
			view.QuoteNetValueNote = "该曲线以 U / USD 报价货币计价；账户本身已经是美元口径。无风险利率按 0%，年化按 365 天。"
		} else {
			view.QuoteNetValueNote = "该曲线将账户净值按标的收盘价换算为 U / USD 报价货币。无风险利率按 0%，年化按 365 天。"
		}
	}

	buyHoldSeries, buyHoldInitialUSD := buildBuyHoldSeries(result)
	if displayIsDaily {
		buyHoldSeries = compressLineSeriesDailyEOD(buyHoldSeries)
	}
	if len(buyHoldSeries) > 0 {
		view.HasBuyHoldBenchmark = true
		view.BuyHoldSeriesData = marshalJS(buyHoldSeries)
		buyHoldValues := make([]float64, 0, len(buyHoldSeries))
		for _, point := range buyHoldSeries {
			if point.Value != nil {
				buyHoldValues = append(buyHoldValues, *point.Value)
			}
		}
		minBuyHold, maxBuyHold := minMax(buyHoldValues)
		view.BuyHoldMin = currency(minBuyHold)
		view.BuyHoldMax = currency(maxBuyHold)
		if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
			view.BuyHoldNote = fmt.Sprintf("Buy&Hold 参考线按 USD 计价，假设用初始资金 %s 在首个有效收盘价一次性买入并持有。", currency(buyHoldInitialUSD))
		} else {
			view.BuyHoldNote = fmt.Sprintf("Buy&Hold 参考线始终按 USD 计价；此处先将初始资金 %s 按首个有效收盘价换算为 %s，再一次性买入并持有。", amount(result.InitialCapital, result.AccountUnit), currency(buyHoldInitialUSD))
		}
		view.HasBuyHoldPerformance = result.BuyHoldPerformance != nil
		view.BuyHoldPerformance = buildPerformanceMetricCard("Buy & Hold", quoteMetricUnitLabel(result), result.BuyHoldPerformance)
	}

	if displayIsDaily && len(quoteNetValueSeries) > 0 {
		dailyQuoteSeries := quoteNetValueSeries
		if len(dailyQuoteSeries) > 0 {
			view.HasDailyQuoteNetValue = true
			view.DailyQuoteNetValueSeriesData = marshalJS(dailyQuoteSeries)
			view.DailyBuyHoldSeriesData = marshalJS(buyHoldSeries)
			view.DailyQuoteNetValueNote = "该图保留每个 UTC 日的最后一个净值点，便于查看全周期收益演化。"
		}
	}

	if dailyAssetPnLSeries := compressLineSeriesDailyEOD(buildAssetPnLSeries(result)); len(dailyAssetPnLSeries) > 0 {
		assetUnit := fallbackText(strings.TrimSpace(result.UnderlyingUnit), fallbackText(strings.TrimSpace(meta.Asset), "asset"))
		view.HasDailyAssetPnL = true
		view.DailyAssetPnLSeriesData = marshalJS(dailyAssetPnLSeries)
		if isUSDLikeUnit(result.AccountUnit) || strings.TrimSpace(result.AccountUnit) == "" {
			view.DailyAssetPnLNote = fmt.Sprintf("该图先将账户净值按标的收盘价换算为 %s 本位，再减去初始 %s 资本；每个 UTC 日仅保留最后一个点，展示的是 PnL 而非 equity。", assetUnit, assetUnit)
		} else {
			view.DailyAssetPnLNote = fmt.Sprintf("该图直接以 %s 本位净值减去初始 %s 资本；每个 UTC 日仅保留最后一个点，展示的是 PnL 而非 equity。", assetUnit, assetUnit)
		}
	}

	view.TradeOverview = buildTradeOverviewView(result.TradeOverview, result.AccountUnit)
	view.EquityAnalysis = buildEquityAnalysisView(result.EquityAnalysis, result.AccountUnit)
	view.MarketMix, view.SecurityMix = buildMarketMixView(result, result.AccountUnit)
	view.PortfolioAttribution = buildPortfolioAttributionView(result, result.AccountUnit)
	view.Trades = buildTradeRows(result.Trades, result.AccountUnit)
	metricResolver := newSpreadMetricResolver(result)
	priceResolver := newUnderlyingPriceResolver(result, chartSource)
	view.Spreads = buildSpreadRows(result.SpreadPositions, result.AccountUnit, metricResolver, priceResolver)
	view.SpreadGroups, view.UngroupedSpreads = buildSpreadGroupViews(result.SpreadGroups, result.SpreadPositions, result.AccountUnit, metricResolver, priceResolver)
	view.TopDrawdownGroups = buildTopSpreadGroupDrawdownViews(result.SpreadGroups, result.AccountUnit)

	if result.SpreadSummary != nil {
		s := result.SpreadSummary
		view.SpreadSummary = &spreadSummaryView{
			TotalSpreads:   integer(s.TotalSpreads),
			ClosedSpreads:  integer(s.ClosedSpreads),
			OpenSpreads:    integer(s.OpenSpreads),
			WinningSpreads: integer(s.WinningSpreads),
			LosingSpreads:  integer(s.LosingSpreads),
			WinRate:        pct(s.WinRate),
			TotalPnL:       signedAmount(s.TotalPnL, result.AccountUnit),
		}
	}

	candles, candleFallback := buildUnderlyingCandles(result, chartSource)
	if displayIsDaily {
		candles = compressCandlesDailyEOD(candles)
	}
	if len(candles) > 0 {
		view.HasUnderlyingChart = true
		view.UnderlyingChartLabel = chartSource.Label
		view.UnderlyingChartSource = chartSource.SourceText
		view.UnderlyingChartOverride = chartSource.Override
		view.UnderlyingCandleData = marshalJS(candles)
		minUnderlying, maxUnderlying := candleRange(candles)
		view.UnderlyingPriceMin = currency(minUnderlying)
		view.UnderlyingPriceMax = currency(maxUnderlying)
		if candleFallback {
			view.UnderlyingChartNote = "OHLC was not fully available in the result series, so candle bodies were reconstructed from close data for visualization."
		}
	}

	volumePoints, volumeLabel := buildUnderlyingVolume(result, chartSource)
	if displayIsDaily {
		volumePoints = compressHistogramSeriesDaily(volumePoints, histogramAggregateSum)
	}
	if len(volumePoints) > 0 {
		view.HasUnderlyingVolume = true
		view.UnderlyingVolumeLabel = volumeLabel
		view.UnderlyingVolumeData = marshalJS(volumePoints)
	}

	markers, tradeMarkerCount, spreadEventCount := buildUnderlyingMarkers(result)
	if displayIsDaily {
		markers = compressMarkersDaily(markers)
	}
	view.UnderlyingMarkerData = marshalJS(markers)
	hoverColumns := buildHoverColumns(result)
	if displayIsDaily {
		hoverColumns = compressHoverColumnsDailyEOD(hoverColumns)
	}
	view.HoverColumnsData = marshalJS(hoverColumns)
	view.HasHoverColumns = len(hoverColumns) > 0
	view.HasFeatureColumns = hasFeatureColumns(hoverColumns)
	activeTimes := buildActiveTimes(result)
	if displayIsDaily {
		activeTimes = compressActiveTimesDaily(activeTimes, result.Timestamps)
	}
	view.ActiveTimeData = marshalJS(activeTimes)
	view.TradeMarkerCount = tradeMarkerCount
	view.SpreadEventCount = spreadEventCount
	view.Notes = buildNotes(result, candleFallback)
	if displayIsDaily {
		view.Notes = append(view.Notes, "为保持所有图表的缩放与交互一致，日内周期结果在 HTML 中统一按每个 UTC 日的最后一个可用点展示。")
	}
	view.CompressedChartPayload = buildCompressedChartPayload(view)
	view.CompressedTradeRowsHTML = marshalJS(compressTextLiteral(renderTradeRowsHTML(view.Trades)))
	view.CompressedSpreadSectionsHTML = marshalJS(compressTextLiteral(renderSpreadSectionsHTML(view.SpreadGroups, view.UngroupedSpreads)))

	return view
}

func hasFeatureColumns(columns []hoverColumnPayload) bool {
	for _, column := range columns {
		if !column.Overlay {
			return true
		}
	}
	return false
}

func buildCombinedHTMLView(results []*backtest.Result, meta HTMLMeta) combinedHTMLReportView {
	if meta.GeneratedAt.IsZero() {
		meta.GeneratedAt = time.Now()
	}
	periodStart := results[0].StartTime
	periodEnd := results[0].EndTime
	strategies := make([]combinedHTMLStrategyView, 0, len(results))
	for index, result := range results {
		if result.StartTime.Before(periodStart) {
			periodStart = result.StartTime
		}
		if result.EndTime.After(periodEnd) {
			periodEnd = result.EndTime
		}
		strategies = append(strategies, combinedHTMLStrategyView{
			AnchorID: fmt.Sprintf("strategy-%d-%s", index+1, slugToken(result.StrategyName)),
			Report:   buildHTMLView(result, meta),
		})
	}
	return combinedHTMLReportView{
		Title:       fmt.Sprintf("%s 回测报告", meta.Asset),
		Asset:       meta.Asset,
		Interval:    meta.Interval,
		Period:      fmt.Sprintf("%s 至 %s", formatDate(periodStart), formatDate(periodEnd)),
		GeneratedAt: meta.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		Strategies:  strategies,
	}
}

func buildTradeOverviewView(overview *backtest.TradeOverview, unit string) tradeOverviewView {
	if overview == nil {
		return tradeOverviewView{}
	}
	return tradeOverviewView{
		RawFills:             integer(overview.RawFills),
		RoundTrips:           integer(overview.RoundTrips),
		LongFills:            integer(overview.LongFills),
		ShortFills:           integer(overview.ShortFills),
		TotalNotional:        amount(overview.TotalNotional, unit),
		GrossProfit:          amount(overview.GrossProfit, unit),
		GrossLoss:            amount(overview.GrossLoss, unit),
		NetPnL:               signedAmount(overview.NetPnL, unit),
		AvgPnLPerRoundTrip:   signedAmount(overview.AvgPnLPerRoundTrip, unit),
		AvgCommissionPerFill: amount(overview.AvgCommissionPerFill, unit),
	}
}

func buildEquityAnalysisView(analysis *backtest.EquityAnalysis, unit string) equityAnalysisView {
	if analysis == nil {
		return equityAnalysisView{}
	}
	return equityAnalysisView{
		PeakEquity:              amount(analysis.PeakEquity, unit),
		PeakTime:                formatDateTime(analysis.PeakTime),
		LowestEquity:            amount(analysis.LowestEquity, unit),
		LowestTime:              formatDateTime(analysis.LowestTime),
		BestBarReturn:           pct(analysis.BestBarReturn),
		WorstBarReturn:          pct(analysis.WorstBarReturn),
		BarReturnVolatility:     pct(analysis.BarReturnVolatility),
		PositiveBars:            integer(analysis.PositiveBars),
		NegativeBars:            integer(analysis.NegativeBars),
		FlatBars:                integer(analysis.FlatBars),
		MaxDrawdownDurationBars: integer(analysis.MaxDrawdownDurationBars),
		MaxDrawdownDuration:     fmt.Sprintf("%.1f 小时", analysis.MaxDrawdownDuration),
	}
}

func buildPerformanceMetricCard(name, unit string, snapshot *backtest.PerformanceSnapshot) performanceMetricCardView {
	view := performanceMetricCardView{
		Name:      name,
		ValueUnit: fallbackText(strings.TrimSpace(unit), "-"),
	}
	if snapshot == nil {
		return view
	}
	view.AnnualizedReturn = pct(snapshot.AnnualizedReturn)
	view.AnnualizedVolatility = pct(snapshot.AnnualizedVolatility)
	view.MaxDrawdown = pct(snapshot.MaxDrawdown)
	view.SharpeRatio = decimal(snapshot.SharpeRatio)
	view.CalmarRatio = ratio(snapshot.CalmarRatio)
	return view
}

func buildTradeRows(trades []backtest.Trade, unit string) []tradeRowView {
	rows := make([]tradeRowView, 0, len(trades))
	for _, trade := range trades {
		side := trade.Side.String()
		rows = append(rows, tradeRowView{
			Timestamp:  formatDateTime(trade.Timestamp),
			Security:   fmt.Sprintf("%s / %s / %s", trade.Security.Market, trade.Security.Symbol, trade.Security.Interval),
			Side:       translateSide(side),
			Reason:     fallbackText(strings.TrimSpace(trade.Note), "-"),
			Qty:        decimal(trade.Qty),
			FillPrice:  amount(trade.FillPrice, unit),
			Commission: amount(trade.Commission, unit),
			Slippage:   amount(trade.Slippage, unit),
			NetAmount:  signedAmount(trade.NetAmount(), unit),
			SideClass:  sideClass(side),
		})
	}
	return rows
}

func buildMarketMixView(result *backtest.Result, unit string) (marketMixView, []securityMixRowView) {
	if result == nil {
		return marketMixView{Description: "无交易标的记录。"}, nil
	}
	type aggregate struct {
		trades   int
		notional float64
		netCash  float64
	}
	markets := make(map[string]struct{})
	securities := make(map[string]*aggregate)
	regularTrades := 0
	optionLegs := 0

	addMarket := func(market string) {
		market = strings.TrimSpace(market)
		if market != "" {
			markets[market] = struct{}{}
		}
	}
	securityKey := func(market, symbol, interval string) string {
		parts := []string{strings.TrimSpace(market), strings.TrimSpace(symbol)}
		if strings.TrimSpace(interval) != "" {
			parts = append(parts, strings.TrimSpace(interval))
		}
		return strings.Join(parts, " / ")
	}

	for _, trade := range result.Trades {
		regularTrades++
		addMarket(trade.Security.Market)
		key := securityKey(trade.Security.Market, trade.Security.Symbol, trade.Security.Interval)
		if key == " / " || strings.TrimSpace(key) == "" {
			key = "regular"
		}
		agg := securities[key]
		if agg == nil {
			agg = &aggregate{}
			securities[key] = agg
		}
		agg.trades++
		agg.notional += math.Abs(trade.Qty * trade.FillPrice)
		agg.netCash += trade.NetAmount()
	}
	for _, spread := range result.SpreadPositions {
		for _, leg := range spread.Legs {
			optionLegs++
			key := strings.TrimSpace(leg.Symbol)
			if key == "" {
				key = fmt.Sprintf("option spread #%d", spread.ID)
			}
			agg := securities[key]
			if agg == nil {
				agg = &aggregate{}
				securities[key] = agg
			}
			agg.trades++
			agg.notional += math.Abs(leg.Qty * leg.EntryPrice)
			if leg.Closed {
				agg.netCash += leg.RealizedPnL
			}
		}
	}

	keys := make([]string, 0, len(securities))
	for key := range securities {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := securities[keys[i]]
		right := securities[keys[j]]
		if left.trades != right.trades {
			return left.trades > right.trades
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 8 {
		keys = keys[:8]
	}
	rows := make([]securityMixRowView, 0, len(keys))
	for _, key := range keys {
		agg := securities[key]
		rows = append(rows, securityMixRowView{
			Security: key,
			Trades:   integer(agg.trades),
			Notional: amount4(agg.notional, unit),
			NetCash:  signedAmount(agg.netCash, unit),
		})
	}

	view := marketMixView{
		SecurityCount:       integer(len(securities)),
		MarketCount:         integer(len(markets)),
		OptionLegCount:      integer(optionLegs),
		RegularTradeCount:   integer(regularTrades),
		HasOptions:          optionLegs > 0,
		HasRegularTrades:    regularTrades > 0,
		HasMixedInstruments: optionLegs > 0 && regularTrades > 0,
	}
	switch {
	case view.HasMixedInstruments:
		view.Description = "本报告包含普通股票/现货成交与期权价差腿，价格图仅代表被声明的参考品种，收益与风险以组合权益为准。"
	case view.HasOptions:
		view.Description = "本报告主要由期权合约或价差生命周期构成，价格图用于显示所选底层参考品种。"
	case view.HasRegularTrades:
		view.Description = "本报告由普通标的成交构成；多品种策略的价格图仅显示所选参考品种。"
	default:
		view.Description = "本次运行未记录成交或期权腿，图表仅展示可用的行情与权益序列。"
	}
	return view, rows
}

func buildPortfolioAttributionView(result *backtest.Result, unit string) portfolioAttributionView {
	view := portfolioAttributionView{
		Description: "组合归因按报告已记录事件派生：普通成交展示成交净现金流，期权展示价差已实现盈亏；账户权益仍是最终组合口径。",
	}
	if result == nil {
		return view
	}
	type aggregate struct {
		family   string
		events   int
		notional float64
		pnl      float64
		details  map[string]struct{}
	}
	newAgg := func(family string) *aggregate { return &aggregate{family: family, details: map[string]struct{}{}} }
	underlyings := map[string]*aggregate{}
	regularNotional := 0.0
	regularNetCash := 0.0
	optionNotional := 0.0
	optionRealizedPnL := 0.0
	optionLegs := 0
	closedSpreads := 0
	openSpreads := 0
	groups := map[int]struct{}{}

	for _, trade := range result.Trades {
		notional := math.Abs(trade.Qty * trade.FillPrice)
		netCash := trade.NetAmount()
		regularNotional += notional
		regularNetCash += netCash
		name := normalizeAttributionName(trade.Security.Symbol, "regular")
		agg := underlyings[name]
		if agg == nil {
			agg = newAgg("普通成交")
			underlyings[name] = agg
		}
		agg.events++
		agg.notional += notional
		agg.pnl += netCash
		if market := strings.TrimSpace(trade.Security.Market); market != "" {
			agg.details[market] = struct{}{}
		}
	}
	for _, spread := range result.SpreadPositions {
		if strings.EqualFold(strings.TrimSpace(spread.Status), "closed") {
			closedSpreads++
		} else {
			openSpreads++
		}
		if spread.GroupID != 0 {
			groups[spread.GroupID] = struct{}{}
		}
		underlying := spreadUnderlying(spread)
		if underlying == "" {
			underlying = fmt.Sprintf("spread #%d", spread.ID)
		}
		agg := underlyings[underlying]
		if agg == nil {
			agg = newAgg("期权价差")
			underlyings[underlying] = agg
		} else if agg.family != "期权价差" {
			agg.family = "混合"
		}
		agg.pnl += spread.RealizedPnL
		optionRealizedPnL += spread.RealizedPnL
		for _, leg := range spread.Legs {
			optionLegs++
			agg.events++
			notional := math.Abs(leg.Qty * leg.EntryPrice)
			agg.notional += notional
			optionNotional += notional
			if tag := strings.TrimSpace(spread.Tag); tag != "" {
				agg.details[tag] = struct{}{}
			}
		}
	}

	if len(result.Trades) > 0 {
		view.InstrumentRows = append(view.InstrumentRows, portfolioAttributionRowView{
			Name:     "普通股票/现货成交",
			Family:   "Regular",
			Events:   integer(len(result.Trades)),
			Notional: amount4(regularNotional, unit),
			PnL:      signedAmount(regularNetCash, unit),
			Details:  fmt.Sprintf("%s 个成交标的", integer(countRegularSecurities(result.Trades))),
			PnLClass: signedClass(regularNetCash),
		})
	}
	if len(result.SpreadPositions) > 0 {
		details := []string{fmt.Sprintf("%s 条期权腿", integer(optionLegs)), fmt.Sprintf("%s 已平仓", integer(closedSpreads)), fmt.Sprintf("%s 未平仓", integer(openSpreads))}
		if len(groups) > 0 {
			details = append(details, fmt.Sprintf("%s 个订单组", integer(len(groups))))
		}
		view.InstrumentRows = append(view.InstrumentRows, portfolioAttributionRowView{
			Name:     "期权价差/合约生命周期",
			Family:   "Options",
			Events:   integer(len(result.SpreadPositions)),
			Notional: amount4(optionNotional, unit),
			PnL:      signedAmount(optionRealizedPnL, unit),
			Details:  strings.Join(details, " · "),
			PnLClass: signedClass(optionRealizedPnL),
		})
	}

	keys := make([]string, 0, len(underlyings))
	for key := range underlyings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := underlyings[keys[i]]
		right := underlyings[keys[j]]
		if math.Abs(left.pnl) != math.Abs(right.pnl) {
			return math.Abs(left.pnl) > math.Abs(right.pnl)
		}
		if left.events != right.events {
			return left.events > right.events
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	for _, key := range keys {
		agg := underlyings[key]
		view.UnderlyingRows = append(view.UnderlyingRows, portfolioAttributionRowView{
			Name:     key,
			Family:   agg.family,
			Events:   integer(agg.events),
			Notional: amount4(agg.notional, unit),
			PnL:      signedAmount(agg.pnl, unit),
			Details:  joinTopDetails(agg.details, 3),
			PnLClass: signedClass(agg.pnl),
		})
	}
	view.RegularNetCash = signedAmount(regularNetCash, unit)
	view.OptionRealizedPnL = signedAmount(optionRealizedPnL, unit)
	return view
}

func buildPortfolioAttributionStats(result *backtest.Result) portfolioAttributionStats {
	if result == nil {
		return portfolioAttributionStats{}
	}
	securities := map[string]struct{}{}
	underlyings := map[string]struct{}{}
	stats := portfolioAttributionStats{RegularFills: len(result.Trades), OptionSpreads: len(result.SpreadPositions), HasRegular: len(result.Trades) > 0, HasOptions: len(result.SpreadPositions) > 0}
	for _, trade := range result.Trades {
		key := normalizeAttributionName(strings.Join([]string{trade.Security.Market, trade.Security.Symbol, trade.Security.Interval}, "/"), "regular")
		securities[key] = struct{}{}
		underlyings[normalizeAttributionName(trade.Security.Symbol, "regular")] = struct{}{}
		stats.RegularNetCash += trade.NetAmount()
	}
	for _, spread := range result.SpreadPositions {
		underlying := spreadUnderlying(spread)
		if underlying != "" {
			underlyings[underlying] = struct{}{}
		}
		stats.OptionRealizedPnL += spread.RealizedPnL
		for _, leg := range spread.Legs {
			stats.OptionLegs++
			securities[normalizeAttributionName(leg.Symbol, fmt.Sprintf("spread-%d", spread.ID))] = struct{}{}
		}
	}
	stats.SecurityCount = len(securities)
	stats.UnderlyingCount = len(underlyings)
	return stats
}

func countRegularSecurities(trades []backtest.Trade) int {
	seen := map[string]struct{}{}
	for _, trade := range trades {
		key := strings.Join([]string{trade.Security.Market, trade.Security.Symbol, trade.Security.Interval}, "/")
		seen[normalizeAttributionName(key, "regular")] = struct{}{}
	}
	return len(seen)
}

func spreadUnderlying(spread backtest.SpreadPositionReport) string {
	for _, leg := range spread.Legs {
		if underlying := optionUnderlyingFromSymbol(leg.Symbol); underlying != "" {
			return underlying
		}
	}
	return normalizeAttributionName(stripExecDeltaTagSuffix(spread.Tag), "")
}

func optionUnderlyingFromSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return ""
	}
	for index := 0; index+6 < len(symbol); index++ {
		chunk := symbol[index : index+6]
		if isDigits(chunk) && index+6 < len(symbol) {
			right := symbol[index+6]
			if right == 'C' || right == 'P' {
				return strings.TrimRight(symbol[:index], " _-")
			}
		}
	}
	if cut := strings.IndexAny(symbol, "-_ "); cut > 0 {
		return symbol[:cut]
	}
	return symbol
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func normalizeAttributionName(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func joinTopDetails(details map[string]struct{}, limit int) string {
	if len(details) == 0 {
		return "-"
	}
	values := make([]string, 0, len(details))
	for value := range details {
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	if limit > 0 && len(values) > limit {
		values = append(values[:limit], fmt.Sprintf("+%d", len(values)-limit))
	}
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, " · ")
}

func signedClass(value float64) string {
	switch {
	case value > 0:
		return "text-up"
	case value < 0:
		return "text-down"
	default:
		return "text-slate-300"
	}
}

func buildSpreadRows(spreads []backtest.SpreadPositionReport, unit string, metricResolver spreadMetricResolver, priceResolver underlyingPriceResolver) []spreadRowView {
	rows := make([]spreadRowView, 0, len(spreads)*2)
	for _, spread := range spreads {
		displayTag := stripExecDeltaTagSuffix(spread.Tag)
		displayCloseNote := stripExecDeltaTagSuffix(spread.CloseNote)
		openMetrics := metricResolver.valuesAt(spread.OpenTime)
		openUnderlyingPrice := priceResolver.valueAt(spread.OpenTime)
		legs := make([]spreadLegRowView, 0, len(spread.Legs))
		for _, leg := range spread.Legs {
			expiryOpenDays := leg.Expiration.Sub(leg.EntryTime).Hours() / 24
			entryDelta := leg.Delta
			if leg.EntryDelta != nil {
				entryDelta = *leg.EntryDelta
			}
			closeTimeLabel := "平仓时间"
			legView := spreadLegRowView{
				Symbol:         leg.Symbol,
				Side:           translateSide(leg.Side),
				Type:           translateOptionType(string(leg.Type)),
				StrikePrice:    currency(leg.StrikePrice),
				Expiration:     formatDate(leg.Expiration),
				OpenSelect:     expiryOpenDelta(expiryOpenDays, entryDelta),
				Qty:            decimal(leg.Qty),
				EntryPrice:     amount4(leg.EntryPrice, unit),
				EntryAmount:    amount4(leg.Qty*leg.EntryPrice, unit),
				EntryTime:      formatDateTime(leg.EntryTime),
				ClosePrice:     nullableAmount4(leg.ClosePrice, leg.Closed, unit),
				CloseTimeLabel: closeTimeLabel,
				CloseTime:      "-",
				CloseReason:    fallbackText(strings.TrimSpace(leg.CloseReason), "-"),
				RealizedPnL:    signedAmount(leg.RealizedPnL, unit),
				SideClass:      sideClass(leg.Side),
			}
			if leg.CloseTriggerTime != nil {
				legView.CloseTimeLabel = "平仓触发时间"
				legView.CloseTime = formatDateTime(*leg.CloseTriggerTime)
			} else if leg.CloseTime != nil {
				legView.CloseTime = formatDateTime(*leg.CloseTime)
			}
			legs = append(legs, legView)
		}

		openAnchor := fmt.Sprintf("spread-%d-open", spread.ID)
		closeAnchor := fmt.Sprintf("spread-%d-close", spread.ID)

		openRow := spreadRowView{
			ID:              spread.ID,
			Tag:             displayTag,
			GroupID:         spread.GroupID,
			AnchorID:        openAnchor,
			EventType:       "OPEN",
			EventClass:      "bg-sky-500/15 text-sky-200 ring-sky-400/40",
			EventTime:       formatDateTime(spread.OpenTime),
			HeaderTimeLabel: "下单",
			HeaderTime:      formatDateTime(spread.OpenTime),
			UnderlyingPrice: openUnderlyingPrice,
			EventUnix:       spread.OpenTime.Unix(),
			WindowStartUnix: spread.OpenTime.Unix(),
			WindowEndUnix:   spread.OpenTime.Unix(),
			Status:          "已开仓",
			eventUnix:       spread.OpenTime.Unix(),
			OpenTime:        formatDateTime(spread.OpenTime),
			CloseTime:       "-",
			DaysHeld:        "-",
			RealizedPnL:     "-",
			StatusClass:     statusClass("open"),
			ReportMetrics:   openMetrics,
			Legs:            legs,
		}
		if spread.CloseTime != nil {
			openRow.WindowEndUnix = spread.CloseTime.Unix()
			openRow.RelatedLink = closeAnchor
			openRow.RelatedText = "跳转到平仓"
		}
		rows = append(rows, openRow)

		if spread.CloseTime != nil {
			closeTag := displayTag
			closeEventTime := spread.CloseTime
			closeHeaderLabel := "平仓时间"
			closeHeaderTime := formatDateTime(*spread.CloseTime)
			if spread.CloseTriggerTime != nil {
				closeEventTime = spread.CloseTriggerTime
				closeHeaderLabel = "平仓触发"
				closeHeaderTime = formatDateTime(*spread.CloseTriggerTime)
			}
			if strings.TrimSpace(displayCloseNote) != "" {
				closeTag = displayCloseNote
			}
			closeMetrics := metricResolver.valuesAt(*closeEventTime)
			closeUnderlyingPrice := priceResolver.valueAt(*closeEventTime)
			rows = append(rows, spreadRowView{
				ID:              spread.ID,
				Tag:             closeTag,
				GroupID:         spread.GroupID,
				AnchorID:        closeAnchor,
				EventType:       "CLOSE",
				EventClass:      "bg-rose-500/15 text-rose-200 ring-rose-400/40",
				EventTime:       formatDateTime(*closeEventTime),
				HeaderTimeLabel: closeHeaderLabel,
				HeaderTime:      closeHeaderTime,
				UnderlyingPrice: closeUnderlyingPrice,
				RelatedLink:     openAnchor,
				RelatedText:     "跳转到开仓",
				EventUnix:       closeEventTime.Unix(),
				WindowStartUnix: spread.OpenTime.Unix(),
				WindowEndUnix:   closeEventTime.Unix(),
				Status:          translateSpreadStatus(spread.Status),
				eventUnix:       closeEventTime.Unix(),
				OpenTime:        formatDateTime(spread.OpenTime),
				CloseTime:       closeHeaderTime,
				DaysHeld:        fmt.Sprintf("%.2f 天", spread.DaysHeld),
				RealizedPnL:     signedAmount(spread.RealizedPnL, unit),
				StatusClass:     statusClass(spread.Status),
				ReportMetrics:   closeMetrics,
				Legs:            legs,
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].eventUnix != rows[j].eventUnix {
			return rows[i].eventUnix < rows[j].eventUnix
		}
		if rows[i].ID != rows[j].ID {
			return rows[i].ID < rows[j].ID
		}
		if rows[i].EventType != rows[j].EventType {
			return rows[i].EventType == "OPEN"
		}
		return rows[i].Tag < rows[j].Tag
	})

	return rows
}

func buildSpreadGroupViews(groups []backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport, unit string, metricResolver spreadMetricResolver, priceResolver underlyingPriceResolver) ([]spreadGroupView, []spreadRowView) {
	if len(spreads) == 0 {
		return nil, nil
	}

	spreadMap := make(map[int]backtest.SpreadPositionReport, len(spreads))
	groupSpreads := make(map[int][]backtest.SpreadPositionReport)
	ungrouped := make([]backtest.SpreadPositionReport, 0, len(spreads))
	for _, spread := range spreads {
		spreadMap[spread.ID] = spread
		if spread.GroupID > 0 {
			groupSpreads[spread.GroupID] = append(groupSpreads[spread.GroupID], spread)
			continue
		}
		ungrouped = append(ungrouped, spread)
	}

	groupReports := make(map[int]backtest.SpreadGroupReport, len(groups))
	for _, group := range groups {
		groupReports[group.ID] = group
		if _, exists := groupSpreads[group.ID]; !exists {
			groupSpreads[group.ID] = nil
		}
	}

	views := make([]spreadGroupView, 0, len(groupSpreads))
	for groupID, groupedSpreads := range groupSpreads {
		if groupID <= 0 {
			continue
		}

		report, hasReport := groupReports[groupID]
		orderedSpreads := orderedGroupedSpreads(report, groupedSpreads, spreadMap)
		if len(orderedSpreads) == 0 {
			continue
		}
		rows := buildSpreadRows(orderedSpreads, unit, metricResolver, priceResolver)

		openTime := earliestSpreadOpenTime(orderedSpreads)
		if hasReport && !report.OpenTime.IsZero() {
			openTime = report.OpenTime
		}
		closeTime := latestSpreadCloseTimeReport(orderedSpreads)
		if hasReport && report.CloseTime != nil {
			closeTime = report.CloseTime
		}
		totalPnL := totalGroupedSpreadPnL(orderedSpreads)
		status := groupedSpreadStatus(report, orderedSpreads)
		view := spreadGroupView{
			ID:            groupID,
			Tag:           groupedSpreadTag(report, groupID),
			AnchorID:      fmt.Sprintf("spread-group-%d", groupID),
			Status:        translateSpreadStatus(status),
			StatusClass:   statusClass(status),
			OpenTime:      formatDateTime(openTime),
			CloseTime:     nullableTime(closeTime),
			InitAmount:    groupedInitAmount(report, unit),
			HighestEquity: amount(report.HighestEquity, unit),
			LowestEquity:  amount(report.LowestEquity, unit),
			MaxDrawdown:   pct(report.MaxDrawdown),
			DecayFactor:   groupedDecayFactor(report),
			RollCount:     groupedRollCount(report),
			TotalPnL:      signedAmount(totalPnL, unit),
			SpreadCount:   len(orderedSpreads),
			EventCount:    len(rows),
			Spreads:       rows,
		}
		if !openTime.IsZero() {
			view.eventUnix = openTime.Unix()
		}
		views = append(views, view)
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].eventUnix != views[j].eventUnix {
			return views[i].eventUnix < views[j].eventUnix
		}
		return views[i].ID < views[j].ID
	})

	return views, buildSpreadRows(ungrouped, unit, metricResolver, priceResolver)
}

func buildTopSpreadGroupDrawdownViews(groups []backtest.SpreadGroupReport, unit string) []spreadGroupDrawdownView {
	if len(groups) == 0 {
		return nil
	}
	ordered := append([]backtest.SpreadGroupReport(nil), groups...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].MaxDrawdown != ordered[j].MaxDrawdown {
			return ordered[i].MaxDrawdown > ordered[j].MaxDrawdown
		}
		return ordered[i].ID < ordered[j].ID
	})
	if len(ordered) > 5 {
		ordered = ordered[:5]
	}
	views := make([]spreadGroupDrawdownView, 0, len(ordered))
	for _, group := range ordered {
		status := strings.TrimSpace(group.Status)
		if status == "" {
			status = "open"
		}
		views = append(views, spreadGroupDrawdownView{
			ID:            group.ID,
			Tag:           groupedSpreadTag(group, group.ID),
			AnchorID:      fmt.Sprintf("spread-group-%d", group.ID),
			Status:        translateSpreadStatus(status),
			StatusClass:   statusClass(status),
			MaxDrawdown:   pct(group.MaxDrawdown),
			HighestEquity: amount(group.HighestEquity, unit),
			LowestEquity:  amount(group.LowestEquity, unit),
			TotalPnL:      signedAmount(group.TotalPnL, unit),
		})
	}
	return views
}

func orderedGroupedSpreads(report backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport, spreadMap map[int]backtest.SpreadPositionReport) []backtest.SpreadPositionReport {
	if len(spreads) == 0 {
		return nil
	}

	ordered := make([]backtest.SpreadPositionReport, 0, len(spreads))
	seen := make(map[int]struct{}, len(spreads))
	for _, spreadID := range report.SpreadIDs {
		spread, ok := spreadMap[spreadID]
		if !ok || spread.GroupID != report.ID {
			continue
		}
		ordered = append(ordered, spread)
		seen[spread.ID] = struct{}{}
	}
	for _, spread := range spreads {
		if _, ok := seen[spread.ID]; ok {
			continue
		}
		ordered = append(ordered, spread)
	}

	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].OpenTime.Equal(ordered[j].OpenTime) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].OpenTime.Before(ordered[j].OpenTime)
	})
	return ordered
}

func earliestSpreadOpenTime(spreads []backtest.SpreadPositionReport) time.Time {
	var earliest time.Time
	for _, spread := range spreads {
		if earliest.IsZero() || spread.OpenTime.Before(earliest) {
			earliest = spread.OpenTime
		}
	}
	return earliest
}

func latestSpreadCloseTimeReport(spreads []backtest.SpreadPositionReport) *time.Time {
	var latest time.Time
	found := false
	for _, spread := range spreads {
		if spread.CloseTime != nil && (!found || spread.CloseTime.After(latest)) {
			latest = *spread.CloseTime
			found = true
		}
	}
	if !found {
		return nil
	}
	return &latest
}

func totalGroupedSpreadPnL(spreads []backtest.SpreadPositionReport) float64 {
	total := 0.0
	for _, spread := range spreads {
		total += spread.RealizedPnL
	}
	return total
}

func groupedSpreadStatus(report backtest.SpreadGroupReport, spreads []backtest.SpreadPositionReport) string {
	if strings.TrimSpace(report.Status) != "" {
		return report.Status
	}
	if len(spreads) == 0 {
		return "open"
	}
	for _, spread := range spreads {
		if !strings.EqualFold(spread.Status, "closed") {
			return "open"
		}
	}
	return "closed"
}

func groupedSpreadTag(report backtest.SpreadGroupReport, groupID int) string {
	if strings.TrimSpace(report.Tag) != "" {
		return report.Tag
	}
	return fmt.Sprintf("spread-group-%d", groupID)
}

func groupedInitAmount(report backtest.SpreadGroupReport, unit string) string {
	if report.InitAmount == 0 {
		return "-"
	}
	return amount4(report.InitAmount, unit)
}

func groupedDecayFactor(report backtest.SpreadGroupReport) string {
	if report.DecayFactor == 0 {
		return "-"
	}
	return decimal(report.DecayFactor)
}

func groupedRollCount(report backtest.SpreadGroupReport) string {
	return integer(report.RollCount)
}

func nullableTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return formatDateTime(*value)
}

func stripExecDeltaTagSuffix(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	parts := strings.Split(tag, " | ")
	filtered := parts[:0]
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, "exec_Delta=") {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return tag
	}
	return strings.Join(filtered, " | ")
}

func buildNotes(result *backtest.Result, candleFallback bool) []string {
	notes := make([]string, 0, 6)
	if len(result.Trades) == 0 && len(result.SpreadPositions) > 0 {
		notes = append(notes, "该策略通过价差跟踪腿执行，因此原始经纪商成交表为空，但价差生命周期事件仍会标注在价格图上。")
	}
	if result.EquityAnalysis != nil && result.EquityAnalysis.MaxDrawdownDurationBars > 0 {
		notes = append(notes, fmt.Sprintf("最长回撤持续了 %d 根 bar（%.1f 小时）。", result.EquityAnalysis.MaxDrawdownDurationBars, result.EquityAnalysis.MaxDrawdownDuration))
	}
	if result.TradeOverview != nil && result.TradeOverview.RoundTrips == 0 && len(result.SpreadPositions) == 0 {
		notes = append(notes, "本次运行未记录到已完成的往返交易。")
	}
	if reportUsesCompatFallback(result) {
		notes = append(notes, "该报告使用了兼容性回退市场数据源。价格 bar 仍可使用，但部分辅助字段可能缺失或为重建值。")
	}
	if !reportHasUsableVolume(result) {
		notes = append(notes, "本次运行没有可用的原生成交量序列。基于成交量的指标和悬停窗口中的成交量字段将显示为 n/a；若使用报告图表覆盖，请确认覆盖源同时暴露 volume 或 tick_count。")
	}
	if candleFallback {
		notes = append(notes, "由于导出结果中缺少完整 OHLC 序列，参考价格图已根据收盘价数据重建。")
	}
	return notes
}

func reportUsesCompatFallback(result *backtest.Result) bool {
	if result == nil || result.Series == nil {
		return false
	}
	return seriesHasTruthyFlag(result.Series["compat_fallback"])
}

func reportHasUsableVolume(result *backtest.Result) bool {
	if result == nil || result.Series == nil {
		return false
	}
	if seriesHasFiniteValue(result.Series["volume"]) {
		return true
	}
	if seriesHasFiniteValue(result.Series["tick_count"]) {
		return true
	}
	return seriesHasFiniteValue(result.Series["vol_norm"])
}

func seriesHasTruthyFlag(values []float64) bool {
	for _, value := range values {
		if chartValueValid(value) && value != 0 {
			return true
		}
	}
	return false
}

func seriesHasFiniteValue(values []float64) bool {
	for _, value := range values {
		if chartValueValid(value) {
			return true
		}
	}
	return false
}

func resolveChartSeriesSource(result *backtest.Result, meta HTMLMeta) chartSeriesSource {
	defaultLabel := strings.TrimSpace(meta.Asset)
	if defaultLabel == "" && result != nil {
		defaultLabel = strings.TrimSpace(result.UnderlyingUnit)
	}
	if defaultLabel == "" {
		defaultLabel = "primary"
	}
	if strings.TrimSpace(meta.Interval) != "" {
		defaultLabel = defaultLabel + " / " + strings.TrimSpace(meta.Interval)
	}

	source := chartSeriesSource{Label: defaultLabel, SourceText: "primary OHLC series"}
	if result == nil || len(result.Series) == 0 {
		return source
	}
	prefix := normalizeSeriesPrefix(meta.ChartSeriesPrefix)
	if prefix == "" {
		return source
	}
	if !seriesHasFiniteValue(seriesBySource(result.Series, prefix, "close")) {
		source.SourceText = fmt.Sprintf("primary OHLC series; requested chart source %q was not available", prefix)
		return source
	}
	label := strings.TrimSpace(meta.ChartSelectionLabel)
	if label == "" {
		label = inferChartSourceLabel(prefix)
	}
	return chartSeriesSource{
		Prefix:     prefix,
		Label:      fallbackText(label, prefix),
		SourceText: prefix,
		Override:   true,
	}
}

func normalizeSeriesPrefix(prefix string) string {
	return strings.Trim(strings.TrimSpace(prefix), ".")
}

func inferChartSourceLabel(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if strings.HasPrefix(trimmed, "request.security.") {
		key := strings.TrimPrefix(trimmed, "request.security.")
		parts := strings.Split(key, "|")
		if len(parts) == 3 {
			return fmt.Sprintf("%s / %s / %s", parts[1], parts[0], parts[2])
		}
	}
	return trimmed
}

func seriesBySource(series map[string][]float64, prefix, field string) []float64 {
	if len(series) == 0 {
		return nil
	}
	if prefix != "" {
		if values := series[prefix+"."+field]; len(values) > 0 {
			return values
		}
	}
	return series[field]
}

func buildUnderlyingCandles(result *backtest.Result, source chartSeriesSource) ([]chartCandlePoint, bool) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, false
	}
	closeSeries := seriesBySource(result.Series, source.Prefix, "close")
	if len(closeSeries) == 0 {
		return nil, false
	}
	openSeries := seriesBySource(result.Series, source.Prefix, "open")
	highSeries := seriesBySource(result.Series, source.Prefix, "high")
	lowSeries := seriesBySource(result.Series, source.Prefix, "low")
	n := minInt(len(result.Timestamps), len(closeSeries))
	if n == 0 {
		return nil, false
	}

	candles := make([]chartCandlePoint, 0, n)
	usedFallback := false
	prevClose := closeSeries[0]
	for i := 0; i < n; i++ {
		closeValue := closeSeries[i]
		if !chartValueValid(closeValue) {
			continue
		}

		openValue := closeValue
		if i < len(openSeries) && chartValueValid(openSeries[i]) {
			openValue = openSeries[i]
		} else {
			usedFallback = true
			if i > 0 && chartValueValid(prevClose) {
				openValue = prevClose
			}
		}

		highValue := math.Max(openValue, closeValue)
		if i < len(highSeries) && chartValueValid(highSeries[i]) {
			highValue = math.Max(highValue, highSeries[i])
		} else {
			usedFallback = true
		}

		lowValue := math.Min(openValue, closeValue)
		if i < len(lowSeries) && chartValueValid(lowSeries[i]) {
			lowValue = math.Min(lowValue, lowSeries[i])
		} else {
			usedFallback = true
		}

		candles = append(candles, chartCandlePoint{
			Time:  result.Timestamps[i].Unix(),
			Open:  openValue,
			High:  highValue,
			Low:   lowValue,
			Close: closeValue,
		})
		prevClose = closeValue
	}
	return candles, usedFallback
}

func buildUnderlyingVolume(result *backtest.Result, source chartSeriesSource) ([]chartHistogramPoint, string) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, ""
	}

	seriesName := ""
	label := ""
	switch {
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "volume")):
		seriesName = "volume"
		label = "成交量"
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "tick_count")):
		seriesName = "tick_count"
		label = "成交笔数"
	case seriesHasFiniteValue(seriesBySource(result.Series, source.Prefix, "vol_norm")):
		seriesName = "vol_norm"
		label = "成交量"
	default:
		return nil, ""
	}

	values := seriesBySource(result.Series, source.Prefix, seriesName)
	openSeries := seriesBySource(result.Series, source.Prefix, "open")
	closeSeries := seriesBySource(result.Series, source.Prefix, "close")
	n := minInt(len(result.Timestamps), len(values))
	if n == 0 {
		return nil, ""
	}

	points := make([]chartHistogramPoint, 0, n)
	prevClose := math.NaN()
	if len(closeSeries) > 0 {
		prevClose = closeSeries[0]
	}
	for i := 0; i < n; i++ {
		value := values[i]
		if !chartValueValid(value) {
			continue
		}

		color := "rgba(34,197,94,0.52)"
		closeValue := math.NaN()
		if i < len(closeSeries) && chartValueValid(closeSeries[i]) {
			closeValue = closeSeries[i]
		}
		openValue := math.NaN()
		if i < len(openSeries) && chartValueValid(openSeries[i]) {
			openValue = openSeries[i]
		} else if chartValueValid(prevClose) {
			openValue = prevClose
		}
		if chartValueValid(openValue) && chartValueValid(closeValue) && closeValue < openValue {
			color = "rgba(249,115,22,0.52)"
		}

		points = append(points, chartHistogramPoint{
			Time:  result.Timestamps[i].Unix(),
			Value: value,
			Color: color,
		})
		if chartValueValid(closeValue) {
			prevClose = closeValue
		}
	}
	return points, label
}

func buildHoverColumns(result *backtest.Result) []hoverColumnPayload {
	if result == nil || len(result.ReportColumns) == 0 || len(result.Timestamps) == 0 || result.Series == nil {
		return []hoverColumnPayload{}
	}
	payload := make([]hoverColumnPayload, 0, len(result.ReportColumns))
	for _, column := range result.ReportColumns {
		values := result.Series[column.Source]
		if len(values) == 0 {
			continue
		}
		payload = append(payload, hoverColumnPayload{
			Source:   column.Source,
			Label:    column.Label,
			Decimals: column.Decimals,
			Overlay:  column.Overlay,
			Values:   buildTimeAlignedLineSeries(result.Timestamps, values),
		})
	}
	if len(payload) == 0 {
		return []hoverColumnPayload{}
	}
	return payload
}

func newSpreadMetricResolver(result *backtest.Result) spreadMetricResolver {
	if result == nil || len(result.ReportColumns) == 0 || len(result.Timestamps) == 0 || len(result.Series) == 0 {
		return spreadMetricResolver{}
	}
	return spreadMetricResolver{
		timestamps: result.Timestamps,
		columns:    result.ReportColumns,
		series:     result.Series,
	}
}

func newUnderlyingPriceResolver(result *backtest.Result, source chartSeriesSource) underlyingPriceResolver {
	if result == nil || len(result.Timestamps) == 0 || len(result.Series) == 0 {
		return underlyingPriceResolver{}
	}
	return underlyingPriceResolver{
		timestamps: result.Timestamps,
		series:     result.Series,
		source:     source,
	}
}

func (resolver underlyingPriceResolver) valueAt(eventTime time.Time) string {
	if eventTime.IsZero() || len(resolver.timestamps) == 0 || len(resolver.series) == 0 {
		return ""
	}
	index := reportColumnIndexAtOrBefore(resolver.timestamps, eventTime)
	if index < 0 {
		return ""
	}
	for _, key := range []string{"close", "open", "high", "low"} {
		values := seriesBySource(resolver.series, resolver.source.Prefix, key)
		if index >= len(values) {
			continue
		}
		value := values[index]
		if chartValueValid(value) {
			return currency(value)
		}
	}
	return ""
}

func (resolver spreadMetricResolver) valuesAt(eventTime time.Time) []spreadReportMetricView {
	if eventTime.IsZero() || len(resolver.timestamps) == 0 || len(resolver.columns) == 0 || len(resolver.series) == 0 {
		return nil
	}
	index := reportColumnIndexAtOrBefore(resolver.timestamps, eventTime)
	if index < 0 {
		return nil
	}
	metrics := make([]spreadReportMetricView, 0, len(resolver.columns))
	for _, column := range resolver.columns {
		values := resolver.series[column.Source]
		if index >= len(values) {
			continue
		}
		value := values[index]
		if !chartValueValid(value) {
			continue
		}
		kindLabel := "子图"
		kindClass := "bg-teal-500/10 text-teal-200 ring-1 ring-teal-400/20"
		if column.Overlay {
			kindLabel = "叠加"
			kindClass = "bg-sky-500/10 text-sky-200 ring-1 ring-sky-400/20"
		}
		metrics = append(metrics, spreadReportMetricView{
			Label:     fallbackText(strings.TrimSpace(column.Label), column.Source),
			Source:    column.Source,
			Value:     formatReportMetricValue(value, column.Decimals),
			KindLabel: kindLabel,
			KindClass: kindClass,
		})
	}
	if len(metrics) == 0 {
		return nil
	}
	return metrics
}

func reportColumnIndexAtOrBefore(timestamps []time.Time, eventTime time.Time) int {
	if len(timestamps) == 0 || eventTime.IsZero() {
		return -1
	}
	idx := sort.Search(len(timestamps), func(i int) bool {
		return timestamps[i].After(eventTime)
	})
	if idx == 0 {
		if timestamps[0].After(eventTime) {
			return -1
		}
		return 0
	}
	if idx >= len(timestamps) {
		return len(timestamps) - 1
	}
	return idx - 1
}

func buildLineSeries(times []time.Time, values []float64) []chartLinePoint {
	n := minInt(len(times), len(values))
	if n == 0 {
		return []chartLinePoint{}
	}
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		if !chartValueValid(values[i]) {
			continue
		}
		value := values[i]
		points = append(points, chartLinePoint{
			Time:  times[i].Unix(),
			Value: &value,
		})
	}
	return points
}

func buildQuoteNetValueSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 {
		return nil
	}
	closeSeries := []float64(nil)
	if result.Series != nil {
		closeSeries = result.Series["close"]
	}
	if len(closeSeries) == 0 {
		if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
			return buildLineSeries(result.Timestamps, result.EquityCurve)
		}
		return nil
	}
	n := minInt(len(result.Timestamps), minInt(len(result.EquityCurve), len(closeSeries)))
	if n == 0 {
		return nil
	}
	values := make([]float64, 0, n)
	times := make([]time.Time, 0, n)
	for i := 0; i < n; i++ {
		equityValue := result.EquityCurve[i]
		if !chartValueValid(equityValue) {
			continue
		}
		value := equityValue
		if !strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") && strings.TrimSpace(result.AccountUnit) != "" {
			closeValue := closeSeries[i]
			if !chartValueValid(closeValue) || closeValue <= 0 {
				continue
			}
			value = equityValue * closeValue
		}
		values = append(values, value)
		times = append(times, result.Timestamps[i])
	}
	return buildLineSeries(times, values)
}

func buildBuyHoldSeries(result *backtest.Result) ([]chartLinePoint, float64) {
	if result == nil || len(result.Timestamps) == 0 || result.Series == nil {
		return nil, 0
	}
	closeSeries := result.Series["close"]
	n := minInt(len(result.Timestamps), len(closeSeries))
	if n == 0 {
		return nil, 0
	}

	entryIndex := -1
	entryClose := 0.0
	for i := 0; i < n; i++ {
		if chartValueValid(closeSeries[i]) && closeSeries[i] > 0 {
			entryIndex = i
			entryClose = closeSeries[i]
			break
		}
	}
	if entryIndex < 0 {
		return nil, 0
	}

	initialUSD := result.InitialCapital
	trimmedUnit := strings.TrimSpace(result.AccountUnit)
	if trimmedUnit != "" && !strings.EqualFold(trimmedUnit, "USD") {
		initialUSD = result.InitialCapital * entryClose
	}
	if !chartValueValid(initialUSD) || initialUSD <= 0 {
		return nil, 0
	}

	points := make([]chartLinePoint, 0, n-entryIndex)
	for i := entryIndex; i < n; i++ {
		closeValue := closeSeries[i]
		if !chartValueValid(closeValue) || closeValue <= 0 {
			continue
		}
		value := initialUSD * (closeValue / entryClose)
		points = append(points, chartLinePoint{
			Time:  result.Timestamps[i].Unix(),
			Value: &value,
		})
	}
	if len(points) == 0 {
		return nil, 0
	}
	return points, initialUSD
}

func buildAssetPnLSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return nil
	}

	n := minInt(len(result.Timestamps), len(result.EquityCurve))
	closeSeries := []float64(nil)
	if result.Series != nil {
		closeSeries = result.Series["close"]
	}

	trimmedUnit := strings.TrimSpace(result.AccountUnit)
	assetInitial := 0.0
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		equityValue := result.EquityCurve[i]
		if !chartValueValid(equityValue) {
			continue
		}

		assetEquity := equityValue
		if isUSDLikeUnit(trimmedUnit) || trimmedUnit == "" {
			if i >= len(closeSeries) {
				continue
			}
			closeValue := closeSeries[i]
			if !chartValueValid(closeValue) || closeValue <= 0 {
				continue
			}
			assetEquity = equityValue / closeValue
			if assetInitial == 0 {
				assetInitial = result.InitialCapital / closeValue
			}
		} else if assetInitial == 0 {
			assetInitial = result.InitialCapital
		}

		pnlValue := assetEquity - assetInitial
		points = append(points, chartLinePoint{Time: result.Timestamps[i].Unix(), Value: floatPtr(pnlValue)})
	}

	if len(points) == 0 {
		return nil
	}
	return points
}

func compressLineSeriesDailyEOD(series []chartLinePoint) []chartLinePoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartLinePoint, 0, len(series))
	currentDay := ""
	var current chartLinePoint
	for _, point := range series {
		if point.Value == nil {
			continue
		}
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		current = point
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressTimeAlignedLineSeriesDailyEOD(series []chartLinePoint) []chartLinePoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartLinePoint, 0, len(series))
	currentDay := ""
	var current chartLinePoint
	for _, point := range series {
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		current = point
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressCandlesDailyEOD(candles []chartCandlePoint) []chartCandlePoint {
	if len(candles) == 0 {
		return nil
	}
	compressed := make([]chartCandlePoint, 0, len(candles))
	currentDay := ""
	var current chartCandlePoint
	for _, candle := range candles {
		day := time.Unix(candle.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = candle
			continue
		}
		current.High = math.Max(current.High, candle.High)
		current.Low = math.Min(current.Low, candle.Low)
		current.Close = candle.Close
		current.Time = candle.Time
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

type histogramAggregateMode int

const (
	histogramAggregateLast histogramAggregateMode = iota
	histogramAggregateSum
)

func compressHistogramSeriesDaily(series []chartHistogramPoint, mode histogramAggregateMode) []chartHistogramPoint {
	if len(series) == 0 {
		return nil
	}
	compressed := make([]chartHistogramPoint, 0, len(series))
	currentDay := ""
	var current chartHistogramPoint
	for _, point := range series {
		day := time.Unix(point.Time, 0).UTC().Format("2006-01-02")
		if day != currentDay {
			if currentDay != "" {
				compressed = append(compressed, current)
			}
			currentDay = day
			current = point
			continue
		}
		if mode == histogramAggregateSum {
			current.Value += point.Value
		} else {
			current.Value = point.Value
		}
		current.Time = point.Time
		if strings.TrimSpace(point.Color) != "" {
			current.Color = point.Color
		}
	}
	if currentDay != "" {
		compressed = append(compressed, current)
	}
	return compressed
}

func compressHoverColumnsDailyEOD(columns []hoverColumnPayload) []hoverColumnPayload {
	if len(columns) == 0 {
		return nil
	}
	compressed := make([]hoverColumnPayload, 0, len(columns))
	for _, column := range columns {
		column.Values = compressTimeAlignedLineSeriesDailyEOD(column.Values)
		compressed = append(compressed, column)
	}
	return compressed
}

func compressActiveTimesDaily(activeTimes []int64, timeline []time.Time) []int64 {
	if len(activeTimes) == 0 || len(timeline) == 0 {
		return nil
	}
	activeSet := make(map[int64]struct{}, len(activeTimes))
	for _, ts := range activeTimes {
		activeSet[ts] = struct{}{}
	}
	lastByDay := make(map[string]int64)
	hasActiveByDay := make(map[string]bool)
	orderedDays := make([]string, 0, len(timeline))
	seenDay := make(map[string]struct{}, len(timeline))
	for _, ts := range timeline {
		day := ts.UTC().Format("2006-01-02")
		if _, ok := seenDay[day]; !ok {
			seenDay[day] = struct{}{}
			orderedDays = append(orderedDays, day)
		}
		unix := ts.Unix()
		lastByDay[day] = unix
		if _, ok := activeSet[unix]; ok {
			hasActiveByDay[day] = true
		}
	}
	compressed := make([]int64, 0, len(orderedDays))
	for _, day := range orderedDays {
		if hasActiveByDay[day] {
			compressed = append(compressed, lastByDay[day])
		}
	}
	return compressed
}

func buildSettledEquityDataDailyEOD(result *backtest.Result) settledEquityData {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return settledEquityData{}
	}

	eventDeltas := buildSettlementEventDeltas(result)
	currentSettled := result.InitialCapital
	series := make([]chartLinePoint, 0, len(result.Timestamps))
	profitBars := make([]chartHistogramPoint, 0, len(result.Timestamps))
	lossBars := make([]chartHistogramPoint, 0, len(result.Timestamps))

	currentDay := ""
	dayLastTime := int64(0)
	daySettled := currentSettled
	dayMaxFloat := 0.0
	dayMinFloat := 0.0
	flushDay := func() {
		if currentDay == "" {
			return
		}
		series = append(series, chartLinePoint{Time: dayLastTime, Value: floatPtr(daySettled)})
		if dayMaxFloat > 1e-9 {
			profitBars = append(profitBars, chartHistogramPoint{Time: dayLastTime, Value: dayMaxFloat, Color: "rgba(45, 212, 191, 0.35)"})
		}
		if dayMinFloat < -1e-9 {
			lossBars = append(lossBars, chartHistogramPoint{Time: dayLastTime, Value: dayMinFloat, Color: "rgba(248, 113, 113, 0.28)"})
		}
	}

	for index, ts := range result.Timestamps {
		day := ts.UTC().Format("2006-01-02")
		if day != currentDay {
			flushDay()
			currentDay = day
			dayMaxFloat = 0
			dayMinFloat = 0
		}
		unix := ts.Unix()
		if delta, ok := eventDeltas[unix]; ok && math.Abs(delta) > 1e-9 {
			currentSettled += delta
		}
		if index >= len(result.EquityCurve) {
			continue
		}
		floating := result.EquityCurve[index] - currentSettled
		if floating > dayMaxFloat {
			dayMaxFloat = floating
		}
		if floating < dayMinFloat {
			dayMinFloat = floating
		}
		daySettled = currentSettled
		dayLastTime = unix
	}
	flushDay()

	return settledEquityData{
		Series:         series,
		FloatingProfit: profitBars,
		FloatingLoss:   lossBars,
		Exposure:       compressLineSeriesDailyEOD(buildExposureSeries(result)),
	}
}

func compressMarkersDaily(markers []chartMarker) []chartMarker {
	if len(markers) == 0 {
		return nil
	}
	type groupedMarker struct {
		marker chartMarker
		texts  []string
		seen   map[string]struct{}
	}
	grouped := make(map[string]*groupedMarker)
	for _, marker := range markers {
		day := time.Unix(marker.Time, 0).UTC().Format("2006-01-02")
		groupKey := strings.Join([]string{day, marker.Position, marker.Color, marker.Shape}, "|")
		group, ok := grouped[groupKey]
		if !ok {
			group = &groupedMarker{
				marker: chartMarker{
					Time:     marker.Time,
					Position: marker.Position,
					Color:    marker.Color,
					Shape:    marker.Shape,
				},
				seen: make(map[string]struct{}),
			}
			grouped[groupKey] = group
		}
		group.marker.Time = marker.Time
		text := strings.TrimSpace(marker.Text)
		if text == "" {
			continue
		}
		if _, exists := group.seen[text]; exists {
			continue
		}
		group.seen[text] = struct{}{}
		group.texts = append(group.texts, text)
	}
	compressed := make([]chartMarker, 0, len(grouped))
	for _, group := range grouped {
		text := strings.Join(group.texts, " · ")
		if len(group.texts) > 3 {
			text = strings.Join(group.texts[:3], " · ") + fmt.Sprintf(" · +%d", len(group.texts)-3)
		}
		group.marker.Text = text
		compressed = append(compressed, group.marker)
	}
	sort.Slice(compressed, func(i, j int) bool {
		if compressed[i].Time != compressed[j].Time {
			return compressed[i].Time < compressed[j].Time
		}
		if compressed[i].Position != compressed[j].Position {
			return compressed[i].Position < compressed[j].Position
		}
		return compressed[i].Text < compressed[j].Text
	})
	return compressed
}

func linePointValues(series []chartLinePoint) []float64 {
	values := make([]float64, 0, len(series))
	for _, point := range series {
		if point.Value != nil {
			values = append(values, *point.Value)
		}
	}
	return values
}

func hasSubDailyInterval(timestamps []time.Time) bool {
	if len(timestamps) < 2 {
		return false
	}
	minGap := time.Duration(1<<63 - 1)
	for i := 1; i < len(timestamps); i++ {
		gap := timestamps[i].Sub(timestamps[i-1])
		if gap > 0 && gap < minGap {
			minGap = gap
		}
	}
	if minGap == time.Duration(1<<63-1) {
		return false
	}
	return minGap < 24*time.Hour
}

func isUSDLikeUnit(unit string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(unit))
	switch trimmed {
	case "", "USD", "USDT", "USDC", "BUSD", "FDUSD":
		return true
	default:
		return false
	}
}

func quoteMetricUnitLabel(result *backtest.Result) string {
	if result == nil {
		return "U"
	}
	if strings.EqualFold(strings.TrimSpace(result.AccountUnit), "USD") || strings.TrimSpace(result.AccountUnit) == "" {
		return "USD"
	}
	return "U"
}

func buildTimeAlignedLineSeries(times []time.Time, values []float64) []chartLinePoint {
	n := minInt(len(times), len(values))
	if n == 0 {
		return []chartLinePoint{}
	}
	points := make([]chartLinePoint, 0, n)
	for i := 0; i < n; i++ {
		point := chartLinePoint{Time: times[i].Unix()}
		if chartValueValid(values[i]) {
			value := values[i]
			point.Value = &value
		}
		points = append(points, point)
	}
	return points
}

func buildUnderlyingMarkers(result *backtest.Result) ([]chartMarker, int, int) {
	if result == nil {
		return []chartMarker{}, 0, 0
	}
	aggregated := make(map[markerKey]*chartMarker)
	tradeMarkerCount := 0
	spreadEventCount := 0

	for _, trade := range result.Trades {
		tradeMarkerCount++
		shape := "arrowUp"
		position := "belowBar"
		color := "#2dd4bf"
		label := fmt.Sprintf("买入 %s", decimal(trade.Qty))
		if trade.Side == backtest.Sell {
			shape = "arrowDown"
			position = "aboveBar"
			color = "#f59e0b"
			label = fmt.Sprintf("卖出 %s", decimal(trade.Qty))
		}
		appendMarker(aggregated, chartMarker{
			Time:     trade.Timestamp.Unix(),
			Position: position,
			Color:    color,
			Shape:    shape,
			Text:     label,
		})
	}

	for _, spread := range result.SpreadPositions {
		spreadEventCount++
		appendMarker(aggregated, chartMarker{
			Time:     spread.OpenTime.Unix(),
			Position: "belowBar",
			Color:    "#60a5fa",
			Shape:    "circle",
			Text:     fmt.Sprintf("开仓 #%d", spread.ID),
		})
		if spread.CloseTime != nil {
			spreadEventCount++
			appendMarker(aggregated, chartMarker{
				Time:     spread.CloseTime.Unix(),
				Position: "aboveBar",
				Color:    "#fb7185",
				Shape:    "square",
				Text:     fmt.Sprintf("平仓 #%d", spread.ID),
			})
		}
	}

	markers := make([]chartMarker, 0, len(aggregated))
	for _, marker := range aggregated {
		markers = append(markers, *marker)
	}
	sort.Slice(markers, func(i, j int) bool {
		if markers[i].Time != markers[j].Time {
			return markers[i].Time < markers[j].Time
		}
		if markers[i].Position != markers[j].Position {
			return markers[i].Position < markers[j].Position
		}
		return markers[i].Text < markers[j].Text
	})
	return markers, tradeMarkerCount, spreadEventCount
}

func buildActiveTimes(result *backtest.Result) []int64 {
	if result == nil || len(result.Timestamps) == 0 {
		return []int64{}
	}
	n := len(result.Timestamps)
	active := make([]bool, n)

	for _, spread := range result.SpreadPositions {
		for i, ts := range result.Timestamps {
			if ts.Before(spread.OpenTime) {
				continue
			}
			if spread.CloseTime != nil && ts.After(*spread.CloseTime) {
				continue
			}
			active[i] = true
		}
	}

	trades := append([]backtest.Trade(nil), result.Trades...)
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Timestamp.Equal(trades[j].Timestamp) {
			if trades[i].Security.Symbol != trades[j].Security.Symbol {
				return trades[i].Security.Symbol < trades[j].Security.Symbol
			}
			return trades[i].ID < trades[j].ID
		}
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	positionBySecurity := make(map[string]float64)
	activeSecurities := 0
	tradeIdx := 0
	for i, ts := range result.Timestamps {
		hadTrade := false
		for tradeIdx < len(trades) && !trades[tradeIdx].Timestamp.After(ts) {
			trade := trades[tradeIdx]
			key := trade.Security.Market + "|" + trade.Security.Symbol + "|" + trade.Security.Interval
			prevQty := positionBySecurity[key]
			nextQty := prevQty
			if trade.Side == backtest.Buy {
				nextQty += trade.Qty
			} else {
				nextQty -= trade.Qty
			}
			if math.Abs(prevQty) <= 1e-9 && math.Abs(nextQty) > 1e-9 {
				activeSecurities++
			} else if math.Abs(prevQty) > 1e-9 && math.Abs(nextQty) <= 1e-9 {
				activeSecurities--
			}
			if math.Abs(nextQty) <= 1e-9 {
				delete(positionBySecurity, key)
			} else {
				positionBySecurity[key] = nextQty
			}
			hadTrade = true
			tradeIdx++
		}
		if hadTrade || activeSecurities > 0 {
			active[i] = true
		}
	}

	activeTimes := make([]int64, 0, n)
	for i, ok := range active {
		if ok {
			activeTimes = append(activeTimes, result.Timestamps[i].Unix())
		}
	}
	return activeTimes
}

func buildSettledEquityData(result *backtest.Result) settledEquityData {
	if result == nil || len(result.Timestamps) == 0 || len(result.EquityCurve) == 0 {
		return settledEquityData{}
	}

	eventDeltas := buildSettlementEventDeltas(result)
	series := []chartLinePoint{{Time: result.Timestamps[0].Unix(), Value: floatPtr(result.InitialCapital)}}
	profitBars := make([]chartHistogramPoint, 0, len(eventDeltas))
	lossBars := make([]chartHistogramPoint, 0, len(eventDeltas))
	exposure := buildExposureSeries(result)

	currentSettled := result.InitialCapital
	segmentMaxFloat := 0.0
	segmentMinFloat := 0.0

	for index, ts := range result.Timestamps {
		unix := ts.Unix()
		if delta, ok := eventDeltas[unix]; ok && math.Abs(delta) > 1e-9 {
			if segmentMaxFloat > 1e-9 {
				profitBars = append(profitBars, chartHistogramPoint{Time: unix, Value: segmentMaxFloat, Color: "rgba(45, 212, 191, 0.35)"})
			}
			if segmentMinFloat < -1e-9 {
				lossBars = append(lossBars, chartHistogramPoint{Time: unix, Value: segmentMinFloat, Color: "rgba(248, 113, 113, 0.28)"})
			}
			currentSettled += delta
			appendOrReplaceLinePoint(&series, chartLinePoint{Time: unix, Value: floatPtr(currentSettled)})
			segmentMaxFloat = 0
			segmentMinFloat = 0
		}

		if index >= len(result.EquityCurve) {
			continue
		}
		floating := result.EquityCurve[index] - currentSettled
		if floating > segmentMaxFloat {
			segmentMaxFloat = floating
		}
		if floating < segmentMinFloat {
			segmentMinFloat = floating
		}
	}

	return settledEquityData{
		Series:         series,
		FloatingProfit: profitBars,
		FloatingLoss:   lossBars,
		Exposure:       exposure,
	}
}

func buildSettlementEventDeltas(result *backtest.Result) map[int64]float64 {
	deltas := buildRegularTradeSettlementDeltas(result.Trades)
	for eventTime, delta := range buildSpreadSettlementDeltas(result.SpreadPositions) {
		deltas[eventTime] += delta
	}
	return deltas
}

func buildRegularTradeSettlementDeltas(trades []backtest.Trade) map[int64]float64 {
	if len(trades) == 0 {
		return map[int64]float64{}
	}

	ordered := append([]backtest.Trade(nil), trades...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Timestamp.Equal(ordered[j].Timestamp) {
			if ordered[i].Security.Symbol != ordered[j].Security.Symbol {
				return ordered[i].Security.Symbol < ordered[j].Security.Symbol
			}
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Timestamp.Before(ordered[j].Timestamp)
	})

	states := make(map[backtest.SecurityRef]*regularPositionState)
	deltas := make(map[int64]float64)
	for _, trade := range ordered {
		state, ok := states[trade.Security]
		if !ok {
			state = &regularPositionState{}
			states[trade.Security] = state
		}
		realized := applyRegularTradeSettlement(state, trade)
		if math.Abs(realized) > 1e-9 {
			deltas[trade.Timestamp.Unix()] += realized
		}
	}
	return deltas
}

func applyRegularTradeSettlement(state *regularPositionState, trade backtest.Trade) float64 {
	fillQty := trade.Qty
	if trade.Side == backtest.Sell {
		fillQty = -fillQty
	}
	fillAbs := math.Abs(fillQty)
	positionAbs := math.Abs(state.qty)

	if state.qty == 0 {
		state.qty = fillQty
		state.avgEntryPrice = trade.FillPrice
		state.costBasis = trade.FillPrice * fillAbs
		state.openCommission = trade.Commission
		return 0
	}

	if (state.qty > 0 && fillQty > 0) || (state.qty < 0 && fillQty < 0) {
		state.costBasis += trade.FillPrice * fillAbs
		state.qty += fillQty
		state.avgEntryPrice = state.costBasis / math.Abs(state.qty)
		state.openCommission += trade.Commission
		return 0
	}

	closeQty := fillAbs
	if closeQty > positionAbs {
		closeQty = positionAbs
	}

	realized := 0.0
	if state.qty > 0 {
		realized = closeQty * (trade.FillPrice - state.avgEntryPrice)
	} else {
		realized = closeQty * (state.avgEntryPrice - trade.FillPrice)
	}

	openCommissionAllocated := 0.0
	if positionAbs > 1e-9 && state.openCommission != 0 {
		openCommissionAllocated = state.openCommission * (closeQty / positionAbs)
	}
	exitCommissionAllocated := 0.0
	if fillAbs > 1e-9 && trade.Commission != 0 {
		exitCommissionAllocated = trade.Commission * (closeQty / fillAbs)
	}
	realized -= openCommissionAllocated + exitCommissionAllocated

	remainingOld := positionAbs - closeQty
	newQty := fillAbs - closeQty
	if remainingOld > 1e-9 {
		if state.qty > 0 {
			state.qty = remainingOld
		} else {
			state.qty = -remainingOld
		}
		state.costBasis = state.avgEntryPrice * remainingOld
		state.openCommission -= openCommissionAllocated
		return realized
	}

	if newQty > 1e-9 {
		if fillQty > 0 {
			state.qty = newQty
		} else {
			state.qty = -newQty
		}
		state.avgEntryPrice = trade.FillPrice
		state.costBasis = trade.FillPrice * newQty
		state.openCommission = trade.Commission - exitCommissionAllocated
		return realized
	}

	state.qty = 0
	state.avgEntryPrice = 0
	state.costBasis = 0
	state.openCommission = 0
	return realized
}

func buildSpreadSettlementDeltas(spreads []backtest.SpreadPositionReport) map[int64]float64 {
	deltas := make(map[int64]float64)
	for _, spread := range spreads {
		settledAt := spread.CloseTime
		if settledAt == nil {
			settledAt = spread.CloseTriggerTime
		}
		if settledAt == nil {
			continue
		}
		deltas[settledAt.Unix()] += spread.RealizedPnL
	}
	return deltas
}

func buildExposureSeries(result *backtest.Result) []chartLinePoint {
	if result == nil || len(result.Timestamps) == 0 {
		return nil
	}

	counts := make([]float64, len(result.Timestamps))
	for _, spread := range result.SpreadPositions {
		for index, ts := range result.Timestamps {
			if ts.Before(spread.OpenTime) {
				continue
			}
			if spread.CloseTime != nil && ts.After(*spread.CloseTime) {
				continue
			}
			counts[index]++
		}
	}

	trades := append([]backtest.Trade(nil), result.Trades...)
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].Timestamp.Equal(trades[j].Timestamp) {
			if trades[i].Security.Symbol != trades[j].Security.Symbol {
				return trades[i].Security.Symbol < trades[j].Security.Symbol
			}
			return trades[i].ID < trades[j].ID
		}
		return trades[i].Timestamp.Before(trades[j].Timestamp)
	})

	positionBySecurity := make(map[string]float64)
	activeSecurities := 0
	tradeIndex := 0
	for index, ts := range result.Timestamps {
		for tradeIndex < len(trades) && !trades[tradeIndex].Timestamp.After(ts) {
			trade := trades[tradeIndex]
			key := trade.Security.Market + "|" + trade.Security.Symbol + "|" + trade.Security.Interval
			prevQty := positionBySecurity[key]
			nextQty := prevQty
			if trade.Side == backtest.Buy {
				nextQty += trade.Qty
			} else {
				nextQty -= trade.Qty
			}
			if math.Abs(prevQty) <= 1e-9 && math.Abs(nextQty) > 1e-9 {
				activeSecurities++
			} else if math.Abs(prevQty) > 1e-9 && math.Abs(nextQty) <= 1e-9 {
				activeSecurities--
			}
			if math.Abs(nextQty) <= 1e-9 {
				delete(positionBySecurity, key)
			} else {
				positionBySecurity[key] = nextQty
			}
			tradeIndex++
		}
		counts[index] += float64(activeSecurities)
	}

	return buildTimeAlignedLineSeries(result.Timestamps, counts)
}

func appendOrReplaceLinePoint(points *[]chartLinePoint, point chartLinePoint) {
	if len(*points) == 0 {
		*points = append(*points, point)
		return
	}
	last := &(*points)[len(*points)-1]
	if last.Time == point.Time {
		last.Value = point.Value
		return
	}
	*points = append(*points, point)
}

func floatPtr(value float64) *float64 {
	return &value
}

func appendMarker(markers map[markerKey]*chartMarker, marker chartMarker) {
	key := markerKey{
		Time:     marker.Time,
		Position: marker.Position,
		Color:    marker.Color,
		Shape:    marker.Shape,
	}
	if existing, ok := markers[key]; ok {
		if !strings.Contains(existing.Text, marker.Text) {
			existing.Text += " · " + marker.Text
		}
		return
	}
	copyMarker := marker
	markers[key] = &copyMarker
}

func candleRange(candles []chartCandlePoint) (float64, float64) {
	if len(candles) == 0 {
		return 0, 0
	}
	minValue := candles[0].Low
	maxValue := candles[0].High
	for _, candle := range candles[1:] {
		if candle.Low < minValue {
			minValue = candle.Low
		}
		if candle.High > maxValue {
			maxValue = candle.High
		}
	}
	return minValue, maxValue
}

func marshalJS(value any) template.JS {
	encoded, err := json.Marshal(value)
	if err != nil {
		return template.JS("[]")
	}
	return template.JS(encoded)
}

func roundChartFloat(value float64) float64 {
	if !chartValueValid(value) {
		return value
	}
	const precision = 10000
	return math.Round(value*precision) / precision
}

func buildCompressedChartPayload(view htmlReportView) template.JS {
	payload := map[string]string{
		"underlyingCandles":      compressJSONLiteral(string(view.UnderlyingCandleData)),
		"underlyingVolumeSeries": compressJSONLiteral(string(view.UnderlyingVolumeData)),
		"underlyingMarkers":      compressJSONLiteral(string(view.UnderlyingMarkerData)),
		"hoverColumns":           compressJSONLiteral(string(view.HoverColumnsData)),
		"equitySeries":           compressJSONLiteral(string(view.EquitySeriesData)),
		"settledEquitySeries":    compressJSONLiteral(string(view.SettledEquitySeriesData)),
		"settledFloatingProfit":  compressJSONLiteral(string(view.SettledFloatingProfitData)),
		"settledFloatingLoss":    compressJSONLiteral(string(view.SettledFloatingLossData)),
		"settledExposure":        compressJSONLiteral(string(view.SettledExposureData)),
		"quoteNetValueSeries":    compressJSONLiteral(string(view.QuoteNetValueSeriesData)),
		"dailyQuoteNetValue":     compressJSONLiteral(string(view.DailyQuoteNetValueSeriesData)),
		"dailyBuyHold":           compressJSONLiteral(string(view.DailyBuyHoldSeriesData)),
		"dailyAssetPnL":          compressJSONLiteral(string(view.DailyAssetPnLSeriesData)),
		"buyHoldSeries":          compressJSONLiteral(string(view.BuyHoldSeriesData)),
		"drawdownSeries":         compressJSONLiteral(string(view.DrawdownSeriesData)),
		"activeTimes":            compressJSONLiteral(string(view.ActiveTimeData)),
	}
	return marshalJS(payload)
}

func compressTextLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write([]byte(trimmed)); err != nil {
		return ""
	}
	if err := writer.Close(); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func compressJSONLiteral(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "[]"
	}
	return compressTextLiteral(trimmed)
}

func renderTradeRowsHTML(rows []tradeRowView) string {
	if len(rows) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&builder, "<tr class=\"border-b border-white/5\"><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 text-slate-200\">%s</td><td class=\"px-4 py-2 font-semibold %s\">%s</td><td class=\"px-4 py-2 text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-slate-200\">%s</td><td class=\"px-4 py-2 mono text-slate-200\">%s</td><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-slate-300\">%s</td><td class=\"px-4 py-2 mono text-white\">%s</td></tr>",
			template.HTMLEscapeString(row.Timestamp),
			template.HTMLEscapeString(row.Security),
			row.SideClass,
			template.HTMLEscapeString(row.Side),
			template.HTMLEscapeString(row.Reason),
			template.HTMLEscapeString(row.Qty),
			template.HTMLEscapeString(row.FillPrice),
			template.HTMLEscapeString(row.Commission),
			template.HTMLEscapeString(row.Slippage),
			template.HTMLEscapeString(row.NetAmount),
		)
	}
	return builder.String()
}

func renderSpreadSectionsHTML(groups []spreadGroupView, ungrouped []spreadRowView) string {
	if len(groups) == 0 && len(ungrouped) == 0 {
		return ""
	}
	var builder strings.Builder
	if len(groups) > 0 {
		builder.WriteString("<div class=\"space-y-5 mb-5\">")
		for _, group := range groups {
			fmt.Fprintf(&builder, "<details id=\"%s\" class=\"border border-white/5 rounded-xl overflow-hidden bg-white/[0.02]\" data-spread-group open><summary class=\"spread-group-summary cursor-pointer px-4 py-3 bg-white/[0.03] transition hover:bg-white/[0.045] focus:outline-none focus-visible:ring-2 focus-visible:ring-teal-400/60\"><div class=\"flex flex-wrap items-center justify-between gap-3\"><div class=\"flex flex-wrap items-center gap-3\"><span class=\"mono text-xs text-slate-400 inline-flex items-center gap-2\"><span class=\"spread-group-chevron text-sm text-teal-300\">▸</span><span class=\"spread-group-state-open\">展开</span><span class=\"spread-group-state-closed\">收起</span></span><span class=\"font-medium text-slate-100\">组 #%d %s</span><span class=\"mono text-xs px-2 py-0.5 rounded %s\">%s</span><span class=\"mono text-xs text-slate-400\">开组 %s</span>",
				template.HTMLEscapeString(group.AnchorID), group.ID, template.HTMLEscapeString(group.Tag), group.StatusClass, template.HTMLEscapeString(group.Status), template.HTMLEscapeString(group.OpenTime))
			if group.CloseTime != "-" {
				fmt.Fprintf(&builder, "<span class=\"mono text-xs text-slate-400\">闭组 %s</span>", template.HTMLEscapeString(group.CloseTime))
			}
			fmt.Fprintf(&builder, "</div><div class=\"flex flex-wrap gap-5 text-xs text-slate-400\"><span>%d 个持仓</span><span>%d 个事件</span><span>滚仓 %s 次</span><span>初始资金 <span class=\"mono text-slate-300\">%s</span></span><span>Highest Equity <span class=\"mono text-slate-300\">%s</span></span><span>Lowest Equity <span class=\"mono text-slate-300\">%s</span></span><span>Max Drawdown <span class=\"mono text-rose-200\">%s</span></span><span>衰减 <span class=\"mono text-slate-300\">%s</span></span><span>组盈亏 <span class=\"mono text-slate-200\">%s</span></span></div></div></summary><div class=\"border-t border-white/5 p-4 space-y-4\">",
				group.SpreadCount,
				group.EventCount,
				template.HTMLEscapeString(group.RollCount),
				template.HTMLEscapeString(group.InitAmount),
				template.HTMLEscapeString(group.HighestEquity),
				template.HTMLEscapeString(group.LowestEquity),
				template.HTMLEscapeString(group.MaxDrawdown),
				template.HTMLEscapeString(group.DecayFactor),
				template.HTMLEscapeString(group.TotalPnL),
			)
			for _, spread := range group.Spreads {
				builder.WriteString(renderSpreadEventCardHTML(spread))
			}
			builder.WriteString("</div></details>")
		}
		builder.WriteString("</div>")
	}
	if len(ungrouped) > 0 {
		fmt.Fprintf(&builder, "<div><div class=\"flex items-center justify-between mb-3\"><h3 class=\"text-sm font-medium text-slate-200\">未分组持仓</h3><span class=\"mono text-xs text-slate-400\">%d 个事件</span></div>", len(ungrouped))
		for _, spread := range ungrouped {
			builder.WriteString(renderSpreadEventCardHTML(spread))
		}
		builder.WriteString("</div>")
	}
	return builder.String()
}

func renderSpreadEventCardHTML(row spreadRowView) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "<div id=\"%s\" class=\"mb-4 border border-white/5 rounded-lg overflow-hidden\"><div class=\"flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 bg-white/[0.02] border-b border-white/5\"><div class=\"flex items-center gap-3\"><span class=\"font-medium text-slate-200\">#%d %s</span>", template.HTMLEscapeString(row.AnchorID), row.ID, template.HTMLEscapeString(row.Tag))
	if row.GroupID > 0 {
		fmt.Fprintf(&builder, "<span class=\"mono text-xs px-2 py-0.5 rounded bg-violet-500/15 text-violet-300 ring-1 ring-violet-400/30\">组 #%d</span>", row.GroupID)
	}
	fmt.Fprintf(&builder, "<span class=\"mono text-xs px-2 py-0.5 rounded %s\">%s</span><span class=\"mono text-xs text-slate-400\">%s %s</span>", row.StatusClass, template.HTMLEscapeString(row.Status), template.HTMLEscapeString(row.HeaderTimeLabel), template.HTMLEscapeString(row.HeaderTime))
	if row.UnderlyingPrice != "" {
		fmt.Fprintf(&builder, "<span class=\"mono text-xs text-slate-400\">标的 %s</span>", template.HTMLEscapeString(row.UnderlyingPrice))
	}
	builder.WriteString("</div><div class=\"flex gap-5 text-xs text-slate-400\">")
	if row.EventUnix > 0 {
		fmt.Fprintf(&builder, "<button type=\"button\" class=\"rounded-md border border-white/10 px-2.5 py-1 text-[11px] text-slate-300 transition hover:border-sky-400/40 hover:text-sky-200\" data-chart-jump-time=\"%d\" data-chart-window-start=\"%d\" data-chart-window-end=\"%d\">定位到图表</button>", row.EventUnix, row.WindowStartUnix, row.WindowEndUnix)
	}
	if row.EventTime != row.HeaderTime {
		label := "平仓"
		if row.EventType == "OPEN" {
			label = "开仓"
		}
		fmt.Fprintf(&builder, "<span>%s %s</span>", label, template.HTMLEscapeString(row.EventTime))
	}
	fmt.Fprintf(&builder, "<span>盈亏 <span class=\"mono text-slate-300\">%s</span></span>", template.HTMLEscapeString(row.RealizedPnL))
	if row.RelatedLink != "" {
		fmt.Fprintf(&builder, "<a class=\"text-sky-300 hover:text-sky-200 underline underline-offset-2\" href=\"#%s\">%s</a>", template.HTMLEscapeString(row.RelatedLink), template.HTMLEscapeString(row.RelatedText))
	}
	builder.WriteString("</div></div><div class=\"px-4 py-3 space-y-3\">")
	if len(row.ReportMetrics) > 0 {
		builder.WriteString("<div class=\"grid gap-2 md:grid-cols-2 xl:grid-cols-4\">")
		for _, metric := range row.ReportMetrics {
			fmt.Fprintf(&builder, "<div class=\"spread-metric-card\"><div class=\"flex items-center justify-between gap-2\"><div class=\"text-xs text-slate-400\">%s</div><span class=\"mono text-[10px] px-2 py-0.5 rounded %s\">%s</span></div><div class=\"mt-2 mono text-sm text-slate-200\">%s</div><div class=\"mt-1 mono text-[11px] text-slate-500\">%s</div></div>", template.HTMLEscapeString(metric.Label), metric.KindClass, template.HTMLEscapeString(metric.KindLabel), template.HTMLEscapeString(metric.Value), template.HTMLEscapeString(metric.Source))
		}
		builder.WriteString("</div>")
	}
	fmt.Fprintf(&builder, "<div class=\"grid gap-3 md:grid-cols-2 xl:grid-cols-5 text-sm\"><div><div class=\"text-slate-400\">状态</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">开仓时间</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">平仓时间</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">持有天数</div><div class=\"mt-1 mono text-slate-200\">%s</div></div><div><div class=\"text-slate-400\">实现盈亏</div><div class=\"mt-1 mono text-slate-200\">%s</div></div></div>", template.HTMLEscapeString(row.Status), template.HTMLEscapeString(row.OpenTime), template.HTMLEscapeString(row.CloseTime), template.HTMLEscapeString(row.DaysHeld), template.HTMLEscapeString(row.RealizedPnL))
	builder.WriteString(renderSpreadLegsTableHTML(row))
	builder.WriteString("</div></div>")
	return builder.String()
}

func renderSpreadLegsTableHTML(row spreadRowView) string {
	if len(row.Legs) == 0 {
		return ""
	}
	var builder strings.Builder
	if row.EventType == "OPEN" {
		builder.WriteString("<div class=\"overflow-x-auto border border-white/8 rounded-lg\"><table class=\"min-w-full text-sm\"><thead class=\"bg-white/[0.02] border-b border-white/8\"><tr class=\"text-left text-slate-400\"><th class=\"px-4 py-2\">合约</th><th class=\"px-4 py-2\">事件</th><th class=\"px-4 py-2\">方向</th><th class=\"px-4 py-2\">类型</th><th class=\"px-4 py-2\">行权价</th><th class=\"px-4 py-2\">到期日</th><th class=\"px-4 py-2\">筛选</th><th class=\"px-4 py-2\">数量</th><th class=\"px-4 py-2\">入场价</th><th class=\"px-4 py-2\">入场额</th></tr></thead><tbody class=\"divide-y divide-white/5\">")
		for _, leg := range row.Legs {
			fmt.Fprintf(&builder, "<tr class=\"border-b border-white/[0.03]\"><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5\"><span class=\"mono text-xs px-2 py-0.5 rounded ring-1 %s\">开仓</span></td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td></tr>", template.HTMLEscapeString(leg.Symbol), row.EventClass, template.HTMLEscapeString(leg.Side), template.HTMLEscapeString(leg.Type), template.HTMLEscapeString(leg.StrikePrice), template.HTMLEscapeString(leg.Expiration), template.HTMLEscapeString(leg.OpenSelect), template.HTMLEscapeString(leg.Qty), template.HTMLEscapeString(leg.EntryPrice), template.HTMLEscapeString(leg.EntryAmount))
		}
		builder.WriteString("</tbody></table></div>")
		return builder.String()
	}
	closeTimeLabel := "平仓时间"
	if label := strings.TrimSpace(row.Legs[0].CloseTimeLabel); label != "" {
		closeTimeLabel = label
	}
	fmt.Fprintf(&builder, "<div class=\"overflow-x-auto border border-white/8 rounded-lg\"><table class=\"min-w-full text-sm\"><thead class=\"bg-white/[0.02] border-b border-white/8\"><tr class=\"text-left text-slate-400\"><th class=\"px-4 py-2\">合约</th><th class=\"px-4 py-2\">事件</th><th class=\"px-4 py-2\">方向</th><th class=\"px-4 py-2\">类型</th><th class=\"px-4 py-2\">行权价</th><th class=\"px-4 py-2\">到期日</th><th class=\"px-4 py-2\">数量</th><th class=\"px-4 py-2\">平仓价</th><th class=\"px-4 py-2\">%s</th><th class=\"px-4 py-2\">原因</th><th class=\"px-4 py-2\">实现盈亏</th></tr></thead><tbody class=\"divide-y divide-white/5\">", template.HTMLEscapeString(closeTimeLabel))
	for _, leg := range row.Legs {
		fmt.Fprintf(&builder, "<tr class=\"border-b border-white/[0.03]\"><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5\"><span class=\"mono text-xs px-2 py-0.5 rounded ring-1 %s\">平仓</span></td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 mono text-slate-400\">%s</td><td class=\"px-4 py-1.5 text-slate-300\">%s</td><td class=\"px-4 py-1.5 mono text-slate-300\">%s</td></tr>", template.HTMLEscapeString(leg.Symbol), row.EventClass, template.HTMLEscapeString(leg.Side), template.HTMLEscapeString(leg.Type), template.HTMLEscapeString(leg.StrikePrice), template.HTMLEscapeString(leg.Expiration), template.HTMLEscapeString(leg.Qty), template.HTMLEscapeString(leg.ClosePrice), template.HTMLEscapeString(leg.CloseTime), template.HTMLEscapeString(leg.CloseReason), template.HTMLEscapeString(leg.RealizedPnL))
	}
	builder.WriteString("</tbody></table></div>")
	return builder.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func chartValueValid(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func linePath(values []float64, width, height float64) string {
	if len(values) == 0 {
		return ""
	}
	minValue, maxValue := minMax(values)
	if maxValue-minValue == 0 {
		maxValue += 1
		minValue -= 1
	}
	var builder strings.Builder
	for i, value := range values {
		x := 0.0
		if len(values) > 1 {
			x = width * float64(i) / float64(len(values)-1)
		}
		y := height - ((value-minValue)/(maxValue-minValue))*height
		if i == 0 {
			builder.WriteString(fmt.Sprintf("M %.2f %.2f", x, y))
		} else {
			builder.WriteString(fmt.Sprintf(" L %.2f %.2f", x, y))
		}
	}
	return builder.String()
}

func drawdownSeries(equity []float64) []float64 {
	if len(equity) == 0 {
		return nil
	}
	series := make([]float64, len(equity))
	peak := equity[0]
	for i, eq := range equity {
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			series[i] = (peak - eq) / peak
		}
	}
	return series
}

func minMax(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	minValue, maxValue := values[0], values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}

func maxValue(values []float64) float64 {
	maxVal := 0.0
	for _, value := range values {
		if value > maxVal {
			maxVal = value
		}
	}
	return maxVal
}

func currency(value float64) string {
	return formatAmount(value, "", 2)
}

func amount(value float64, unit string) string {
	decimals := 2
	if strings.TrimSpace(unit) != "" {
		decimals = 4
	}
	return formatAmount(value, unit, decimals)
}

func amount4(value float64, unit string) string {
	return formatAmount(value, unit, 4)
}

func formatAmount(value float64, unit string, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	trimmedUnit := strings.TrimSpace(unit)
	if trimmedUnit == "" || strings.EqualFold(trimmedUnit, "USD") {
		if value < 0 {
			return "-$" + fmt.Sprintf("%.*f", decimals, -value)
		}
		return "$" + fmt.Sprintf("%.*f", decimals, value)
	}
	if value < 0 {
		return "-" + fmt.Sprintf("%.*f", decimals, -value) + " " + trimmedUnit
	}
	return fmt.Sprintf("%.*f", decimals, value) + " " + trimmedUnit
}

func currency4(value float64) string {
	return formatAmount(value, "", 4)
}

func signedCurrency(value float64) string {
	return signedAmount(value, "")
}

func signedAmount(value float64, unit string) string {
	if value > 0 {
		return "+" + amount(value, unit)
	}
	return amount(value, unit)
}

func decimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
}

func ratio(value float64) string {
	if math.IsNaN(value) {
		return "-"
	}
	if math.IsInf(value, 1) {
		return "∞"
	}
	if math.IsInf(value, -1) {
		return "-∞"
	}
	return fmt.Sprintf("%.2f", value)
}

func expiryOpenDelta(expiryOpenDays, delta float64) string {
	if math.IsNaN(expiryOpenDays) || math.IsInf(expiryOpenDays, 0) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return "-"
	}
	return fmt.Sprintf("%.2f 天 | Δ %.2f", expiryOpenDays, delta)
}

func integer(value int) string {
	return fmt.Sprintf("%d", value)
}

func pct(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	return backtest.FormatPercent(value)
}

func nullableCurrency4(value float64, ok bool) string {
	return nullableAmount4(value, ok, "")
}

func nullableAmount4(value float64, ok bool, unit string) string {
	if !ok {
		return "-"
	}
	return amount4(value, unit)
}

func formatReportMetricValue(value float64, decimals int) string {
	if !chartValueValid(value) {
		return "-"
	}
	if decimals < 0 {
		decimals = 0
	}
	return fmt.Sprintf("%.*f", decimals, value)
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02")
}

func formatDateTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format("2006-01-02 15:04 UTC")
}

func sideClass(side string) string {
	if strings.EqualFold(side, "buy") {
		return "text-emerald-300"
	}
	return "text-amber-300"
}

func translateSide(side string) string {
	switch {
	case strings.EqualFold(side, "buy"):
		return "买入"
	case strings.EqualFold(side, "sell"):
		return "卖出"
	default:
		return strings.ToUpper(side)
	}
}

func translateOptionType(optionType string) string {
	switch {
	case strings.EqualFold(optionType, "call"):
		return "看涨"
	case strings.EqualFold(optionType, "put"):
		return "看跌"
	default:
		return strings.ToUpper(optionType)
	}
}

func translateSpreadStatus(status string) string {
	switch {
	case strings.EqualFold(status, "open"):
		return "已开仓"
	case strings.EqualFold(status, "closed"):
		return "已平仓"
	case strings.EqualFold(status, "expired"):
		return "已到期"
	case strings.EqualFold(status, "exercised"):
		return "已行权"
	case strings.EqualFold(status, "assigned"):
		return "已指派"
	default:
		return strings.ToUpper(status)
	}
}

func statusClass(status string) string {
	if strings.EqualFold(status, "closed") {
		return "bg-emerald-500/15 text-emerald-200 ring-emerald-400/40"
	}
	return "bg-amber-500/15 text-amber-200 ring-amber-400/40"
}

func slugToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "_", "-", ".", "-", ":", "-", "#", "")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-")
	if value == "" {
		return "strategy"
	}
	return value
}

const htmlTemplate = `{{ define "classicSpreadEventCard" }}
<div id="{{ .AnchorID }}" class="mb-4 border border-white/5 rounded-lg overflow-hidden">
	<div class="flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 bg-white/[0.02] border-b border-white/5">
		<div class="flex items-center gap-3">
			<span class="font-medium text-slate-200">#{{ .ID }} {{ .Tag }}</span>
			{{ if gt .GroupID 0 }}<span class="mono text-xs px-2 py-0.5 rounded bg-violet-500/15 text-violet-300 ring-1 ring-violet-400/30">组 #{{ .GroupID }}</span>{{ end }}
			<span class="mono text-xs px-2 py-0.5 rounded {{ .StatusClass }}">{{ .Status }}</span>
			<span class="mono text-xs text-slate-400">{{ .HeaderTimeLabel }} {{ .HeaderTime }}</span>
			{{ if .UnderlyingPrice }}<span class="mono text-xs text-slate-400">标的 {{ .UnderlyingPrice }}</span>{{ end }}
		</div>
		<div class="flex gap-5 text-xs text-slate-400">
			{{ if gt .EventUnix 0 }}<button type="button" class="rounded-md border border-white/10 px-2.5 py-1 text-[11px] text-slate-300 transition hover:border-sky-400/40 hover:text-sky-200" data-chart-jump-time="{{ .EventUnix }}" data-chart-window-start="{{ .WindowStartUnix }}" data-chart-window-end="{{ .WindowEndUnix }}">定位到图表</button>{{ end }}
			{{ if ne .EventTime .HeaderTime }}<span>{{ if eq .EventType "OPEN" }}开仓{{ else }}平仓{{ end }} {{ .EventTime }}</span>{{ end }}
			<span>盈亏 <span class="mono text-slate-300">{{ .RealizedPnL }}</span></span>
			{{ if .RelatedLink }}<a class="text-sky-300 hover:text-sky-200 underline underline-offset-2" href="#{{ .RelatedLink }}">{{ .RelatedText }}</a>{{ end }}
		</div>
	</div>
	{{ if .ReportMetrics }}
	<div class="px-4 py-3 border-b border-white/5 bg-white/[0.02]">
		<div class="flex flex-wrap items-center gap-3 mb-3">
			<span class="mono text-[11px] uppercase tracking-[0.22em] text-slate-500">策略列快照</span>
			<span class="text-xs text-slate-500">取事件时点最近可用 bar 的 report columns 值</span>
		</div>
		<div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
			{{ range .ReportMetrics }}
			<div class="spread-metric-card">
				<div class="flex items-start justify-between gap-3">
					<div>
						<div class="text-[11px] mono uppercase tracking-[0.16em] text-slate-500">{{ .Label }}</div>
						<div class="mt-1 mono text-sm text-slate-100">{{ .Value }}</div>
					</div>
					<span class="mono text-[10px] px-2 py-0.5 rounded-full {{ .KindClass }}">{{ .KindLabel }}</span>
				</div>
				<div class="mt-3 mono text-[11px] text-slate-500">{{ .Source }}</div>
			</div>
			{{ end }}
		</div>
	</div>
	{{ end }}
	<div class="overflow-x-auto">
		<table class="w-full text-sm">
			<thead>
				<tr class="text-left text-slate-500 text-xs uppercase border-b border-white/5">
					<th class="px-4 py-2 font-medium">合约</th>
					<th class="px-4 py-2 font-medium">事件</th>
					<th class="px-4 py-2 font-medium">方向</th>
					<th class="px-4 py-2 font-medium">类型</th>
					<th class="px-4 py-2 font-medium">行权价</th>
					<th class="px-4 py-2 font-medium">到期日</th>
					{{ if eq .EventType "OPEN" }}
					<th class="px-4 py-2 font-medium">筛选条件</th>
					<th class="px-4 py-2 font-medium">数量</th>
					<th class="px-4 py-2 font-medium">入场价格</th>
					<th class="px-4 py-2 font-medium">入场金额</th>
					{{ else }}
					<th class="px-4 py-2 font-medium">数量</th>
					<th class="px-4 py-2 font-medium">平仓价格</th>
					<th class="px-4 py-2 font-medium">{{ if .Legs }}{{ (index .Legs 0).CloseTimeLabel }}{{ else }}平仓时间{{ end }}</th>
					<th class="px-4 py-2 font-medium">平仓原因</th>
					<th class="px-4 py-2 font-medium">单腿盈亏</th>
					{{ end }}
				</tr>
			</thead>
			<tbody>
				{{ $eventType := .EventType }}
				{{ $eventClass := .EventClass }}
				{{ range .Legs }}
				<tr class="border-b border-white/[0.03]">
					<td class="px-4 py-1.5 mono text-slate-300">{{ .Symbol }}</td>
					<td class="px-4 py-1.5"><span class="mono text-xs px-2 py-0.5 rounded ring-1 {{ $eventClass }}">{{ if eq $eventType "OPEN" }}开仓{{ else }}平仓{{ end }}</span></td>
					<td class="px-4 py-1.5 text-slate-300">{{ .Side }}</td>
					<td class="px-4 py-1.5 text-slate-400">{{ .Type }}</td>
					<td class="px-4 py-1.5 mono text-slate-300">{{ .StrikePrice }}</td>
					<td class="px-4 py-1.5 mono text-slate-400">{{ .Expiration }}</td>
					{{ if eq $eventType "OPEN" }}
					<td class="px-4 py-1.5 mono text-slate-300">{{ .OpenSelect }}</td>
					<td class="px-4 py-1.5 mono text-slate-300">{{ .Qty }}</td>
					<td class="px-4 py-1.5 mono text-slate-300">{{ .EntryPrice }}</td>
					<td class="px-4 py-1.5 mono text-slate-300">{{ .EntryAmount }}</td>
					{{ else }}
					<td class="px-4 py-1.5 mono text-slate-300">{{ .Qty }}</td>
					<td class="px-4 py-1.5 mono text-slate-400">{{ .ClosePrice }}</td>
					<td class="px-4 py-1.5 mono text-slate-400">{{ .CloseTime }}</td>
					<td class="px-4 py-1.5 text-slate-300">{{ .CloseReason }}</td>
					<td class="px-4 py-1.5 mono text-slate-300">{{ .RealizedPnL }}</td>
					{{ end }}
				</tr>
				{{ end }}
			</tbody>
		</table>
	</div>
</div>
{{ end }}<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{ .Title }}</title>
	<script>
		(function() {
			var theme = 'light';
			try {
				var stored = localStorage.getItem('toktik-report-theme');
				if (stored === 'light' || stored === 'dark') {
					theme = stored;
				} else if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
					theme = 'dark';
				}
			} catch (error) {
				if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {
					theme = 'dark';
				}
			}
			document.documentElement.setAttribute('data-theme', theme);
		})();
	</script>
	<script src="https://cdn.tailwindcss.com"></script>
	<script src="https://unpkg.com/lightweight-charts@4.2.3/dist/lightweight-charts.standalone.production.js"></script>
	<script src="https://cdn.jsdelivr.net/npm/pako@2.1.0/dist/pako.min.js"></script>
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=IBM+Plex+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
  <style>
		:root {
			--paper: #f3f6fb;
			--panel: #ffffff;
			--panel-alt: #f8fafc;
			--ink: #0f172a;
			--ink-soft: #334155;
			--muted: #64748b;
			--line: #d7dee8;
			--line-strong: #c5d0dd;
			--accent: #0f6c7b;
			--accent-soft: #e7f3f5;
			--gain: #107c10;
			--loss: #bc4b09;
			--alert: #b42318;
			--shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
			--page-bg: linear-gradient(180deg, #f7f9fc 0%, #eef3f8 100%);
		}
		html[data-theme="light"] {
			color-scheme: light;
		}
		html[data-theme="dark"] {
			color-scheme: dark;
			--paper: #071019;
			--panel: #0f1a24;
			--panel-alt: #132230;
			--ink: #f8fafc;
			--ink-soft: #cbd5e1;
			--muted: #94a3b8;
			--line: rgba(255,255,255,0.08);
			--line-strong: rgba(255,255,255,0.18);
			--accent: #3dd6c6;
			--accent-soft: rgba(61,214,198,0.12);
			--gain: #4ade80;
			--loss: #f59e0b;
			--alert: #fb7185;
			--shadow: 0 24px 64px rgba(0, 0, 0, 0.34);
			--page-bg:
				radial-gradient(circle at top left, rgba(61,214,198,.18), transparent 26%),
				radial-gradient(circle at top right, rgba(240,164,58,.14), transparent 22%),
				linear-gradient(180deg, #050d14 0%, #09131b 34%, #0d1823 100%);
		}
		body {
			background: var(--page-bg);
			font-family: 'IBM Plex Sans', system-ui, sans-serif;
			color: var(--ink-soft);
		}
		.mono { font-family: 'IBM Plex Mono', monospace; }
		.chart-box {
			background: var(--panel);
			border: 1px solid var(--line);
			border-radius: 4px;
			box-shadow: inset 0 1px 0 rgba(255,255,255,0.65);
		}
		table.perf td, table.perf th { padding: 7px 12px; white-space: nowrap; }
		table.perf th {
			color: var(--muted);
			font-weight: 600;
			text-align: left;
			font-size: 11px;
			text-transform: uppercase;
			letter-spacing: 0.08em;
		}
		table.perf td { font-family: 'IBM Plex Mono', monospace; color: var(--ink); font-size: 13px; }
		table.perf tr { border-bottom: 1px solid var(--line); }
		.section {
			background: var(--panel);
			border: 1px solid var(--line);
			border-radius: 4px;
			padding: 20px;
			margin-bottom: 14px;
			box-shadow: var(--shadow);
		}
		.section h2 {
			color: var(--ink);
			font-size: 16px;
			font-weight: 700;
			margin-bottom: 12px;
			letter-spacing: 0.01em;
		}
		.text-up { color: var(--gain); }
		.text-down { color: var(--loss); }
		.data-window-card {
			background: var(--panel-alt);
			border: 1px solid var(--line);
			border-radius: 4px;
			padding: 10px 12px;
		}
		.data-window-card-button { width: 100%; text-align: left; transition: border-color 120ms ease, background 120ms ease, transform 120ms ease; }
		.data-window-card-button:hover { border-color: #8bbbc4; background: var(--accent-soft); }
		.data-window-card-button.active { border-color: #77a9b4; background: var(--accent-soft); box-shadow: inset 0 0 0 1px rgba(15,108,123,0.12); }
		.feature-empty-state { border: 1px dashed var(--line-strong); border-radius: 4px; padding: 18px; text-align: center; color: var(--muted); font-size: 12px; background: var(--panel-alt); }
		.feature-legend-swatch { display: inline-block; width: 10px; height: 10px; border-radius: 999px; }
		.feature-legend-value { color: var(--ink); }
		.spread-metric-card { background: var(--panel-alt); border: 1px solid var(--line); border-radius: 4px; padding: 12px 14px; min-height: 84px; }
		.spread-group-summary { list-style: none; }
		.spread-group-summary::-webkit-details-marker { display: none; }
		.spread-group-chevron { transition: transform 140ms ease; }
		details[open] > .spread-group-summary .spread-group-chevron { transform: rotate(90deg); }
		.spread-group-state-open { display: inline; }
		.spread-group-state-closed { display: none; }
		details[open] > .spread-group-summary .spread-group-state-open { display: none; }
		details[open] > .spread-group-summary .spread-group-state-closed { display: inline; }
		.section:last-child { margin-bottom: 0; }
		.text-white, .text-slate-100, .text-slate-200 { color: var(--ink) !important; }
		.text-slate-300 { color: var(--ink-soft) !important; }
		.text-slate-400, .text-slate-500 { color: var(--muted) !important; }
		.text-teal-400, .text-teal-300 { color: var(--accent) !important; }
		.text-sky-300, .text-blue-400 { color: #185abd !important; }
		.text-amber-300 { color: var(--loss) !important; }
		.text-rose-200, .text-rose-400 { color: var(--alert) !important; }
		.border-white\/10, .border-white\/8, .border-white\/5 { border-color: var(--line) !important; }
		.bg-white, .bg-white\/\[0\.03\], .bg-white\/\[0\.02\], .bg-white\/\[0\.05\], .bg-white\/\[0\.045\] { background: var(--panel-alt) !important; }
		.bg-\[\#111922\] { background: var(--panel) !important; }
		.bg-transparent { background: transparent !important; }
		.rounded-2xl, .rounded-xl, .rounded-lg, .rounded-md, .rounded-\[2rem\], .rounded-\[1\.75rem\], .rounded-\[1\.5rem\] { border-radius: 4px !important; }
		.max-h-\[32rem\], .max-h-\[34rem\] { content-visibility: auto; contain-intrinsic-size: 1200px; }
		thead.sticky { box-shadow: 0 1px 0 var(--line); }
		select, input[type="checkbox"] { color: var(--ink); }
  </style>
</head>
<body class="text-slate-300 min-h-screen p-4 lg:p-6">
  <div class="max-w-[1600px] mx-auto">
    <header class="mb-6">
	<div class="text-xs mono uppercase tracking-widest text-teal-400 mb-1">回测报告</div>
      <h1 class="text-2xl font-bold text-white">{{ .StrategyName }}</h1>
	<p class="text-sm text-slate-400 mt-1">{{ .Asset }} · {{ .Interval }} · {{ .Period }} · 生成于 {{ .GeneratedAt }}</p>
    </header>

	<div class="section">
	  <div class="grid gap-3 lg:grid-cols-3">
		<div class="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
		  <div class="text-xs mono uppercase tracking-widest text-slate-400">资金计价</div>
		  <div class="mt-2 text-lg font-semibold text-white">{{ .CapitalMode }}</div>
		</div>
		<div class="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
		  <div class="text-xs mono uppercase tracking-widest text-slate-400">策略画像</div>
		  <div class="mt-2 text-lg font-semibold text-white">{{ .CapitalProfile }}</div>
		</div>
		<div class="rounded-xl border border-white/10 bg-white/[0.03] px-4 py-3">
		  <div class="text-xs mono uppercase tracking-widest text-slate-400">解释</div>
		  <div class="mt-2 text-sm text-slate-200">{{ .CapitalNote }}</div>
		</div>
	  </div>
	</div>

	<div class="section">
	  <div class="flex flex-wrap items-start justify-between gap-4 mb-4">
		<div>
		  <h2 class="!mb-1">交易范围</h2>
		  <p class="text-xs text-slate-400">{{ .MarketMix.Description }}</p>
		</div>
		<div class="grid grid-cols-2 gap-2 sm:grid-cols-4 text-xs">
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">标的/合约</div><div class="mt-1 mono text-slate-100">{{ .MarketMix.SecurityCount }}</div></div>
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">市场</div><div class="mt-1 mono text-slate-100">{{ .MarketMix.MarketCount }}</div></div>
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">普通成交</div><div class="mt-1 mono text-slate-100">{{ .MarketMix.RegularTradeCount }}</div></div>
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">期权腿</div><div class="mt-1 mono text-slate-100">{{ .MarketMix.OptionLegCount }}</div></div>
		</div>
	  </div>
	  {{ if .SecurityMix }}
	  <div class="overflow-x-auto border border-white/8 rounded-lg">
		<table class="w-full text-sm">
		  <thead><tr class="text-left text-slate-500 text-xs uppercase border-b border-white/5"><th class="px-4 py-2 font-medium">标的 / 合约</th><th class="px-4 py-2 font-medium">事件数</th><th class="px-4 py-2 font-medium">名义金额</th><th class="px-4 py-2 font-medium">净现金 / 已实现</th></tr></thead>
		  <tbody>
			{{ range .SecurityMix }}<tr class="border-b border-white/[0.03]"><td class="px-4 py-2 mono text-slate-200">{{ .Security }}</td><td class="px-4 py-2 mono text-slate-300">{{ .Trades }}</td><td class="px-4 py-2 mono text-slate-300">{{ .Notional }}</td><td class="px-4 py-2 mono text-slate-200">{{ .NetCash }}</td></tr>{{ end }}
		  </tbody>
		</table>
	  </div>
	  {{ end }}
	</div>

	{{ if .PortfolioAttribution.InstrumentRows }}
	<div class="section">
	  <div class="flex flex-wrap items-start justify-between gap-4 mb-4">
		<div>
		  <h2 class="!mb-1">组合归因</h2>
		  <p class="text-xs text-slate-400">{{ .PortfolioAttribution.Description }}</p>
		</div>
		<div class="grid grid-cols-1 gap-2 sm:grid-cols-2 text-xs">
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">普通成交净现金</div><div class="mt-1 mono text-slate-100">{{ .PortfolioAttribution.RegularNetCash }}</div></div>
		  <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2"><div class="text-slate-500">期权已实现盈亏</div><div class="mt-1 mono text-slate-100">{{ .PortfolioAttribution.OptionRealizedPnL }}</div></div>
		</div>
	  </div>
	  <div class="grid gap-4 xl:grid-cols-2">
		<div class="overflow-x-auto border border-white/8 rounded-lg">
		  <table class="w-full text-sm">
			<thead><tr class="text-left text-slate-500 text-xs uppercase border-b border-white/5"><th class="px-4 py-2 font-medium">工具族</th><th class="px-4 py-2 font-medium">事件</th><th class="px-4 py-2 font-medium">名义金额</th><th class="px-4 py-2 font-medium">归因</th><th class="px-4 py-2 font-medium">细节</th></tr></thead>
			<tbody>
			  {{ range .PortfolioAttribution.InstrumentRows }}<tr class="border-b border-white/[0.03]"><td class="px-4 py-2"><div class="font-medium text-slate-100">{{ .Name }}</div><div class="mt-0.5 mono text-xs text-slate-500">{{ .Family }}</div></td><td class="px-4 py-2 mono text-slate-300">{{ .Events }}</td><td class="px-4 py-2 mono text-slate-300">{{ .Notional }}</td><td class="px-4 py-2 mono {{ .PnLClass }}">{{ .PnL }}</td><td class="px-4 py-2 text-slate-400">{{ .Details }}</td></tr>{{ end }}
			</tbody>
		  </table>
		</div>
		{{ if .PortfolioAttribution.UnderlyingRows }}
		<div class="overflow-x-auto border border-white/8 rounded-lg">
		  <table class="w-full text-sm">
			<thead><tr class="text-left text-slate-500 text-xs uppercase border-b border-white/5"><th class="px-4 py-2 font-medium">Underlying / 标的</th><th class="px-4 py-2 font-medium">类型</th><th class="px-4 py-2 font-medium">事件</th><th class="px-4 py-2 font-medium">名义金额</th><th class="px-4 py-2 font-medium">归因</th></tr></thead>
			<tbody>
			  {{ range .PortfolioAttribution.UnderlyingRows }}<tr class="border-b border-white/[0.03]"><td class="px-4 py-2"><div class="mono text-slate-200">{{ .Name }}</div><div class="mt-0.5 text-xs text-slate-500">{{ .Details }}</div></td><td class="px-4 py-2 text-slate-400">{{ .Family }}</td><td class="px-4 py-2 mono text-slate-300">{{ .Events }}</td><td class="px-4 py-2 mono text-slate-300">{{ .Notional }}</td><td class="px-4 py-2 mono {{ .PnLClass }}">{{ .PnL }}</td></tr>{{ end }}
			</tbody>
		  </table>
		</div>
		{{ end }}
	  </div>
	</div>
	{{ end }}

	<div class="section">
	  <div class="flex flex-wrap items-center justify-between gap-3">
		<div>
		  <h2 class="!mb-1">图表时间轴</h2>
		  <p class="text-xs text-slate-400">忽略空闲时段以压缩无变化的平坦区间（应用于下方所有图表）。</p>
		  {{ if .DisplayIsDaily }}<p class="mt-1 text-xs text-slate-500">当前报告已统一聚合到 1 Day 日终粒度，便于全部图表同步缩放与浏览。</p>{{ end }}
		</div>
		<div class="flex flex-wrap items-center gap-3">
		  <label class="inline-flex items-center gap-2 rounded-md border border-white/10 px-3 py-2 text-sm text-slate-200 cursor-pointer select-none">
			<input id="toggle-ignore-idle" type="checkbox" class="accent-teal-400" />
			<span>忽略空闲时段</span>
		  </label>
		  <select id="theme-select" class="rounded-md border border-white/10 bg-transparent px-3 py-2 text-sm text-slate-200 cursor-pointer">
			<option value="light">亮色</option>
			<option value="dark">暗色</option>
		  </select>
		  <select id="timezone-select" class="rounded-md border border-white/10 bg-transparent px-3 py-2 text-sm text-slate-200 cursor-pointer">
			<option value="utc">UTC</option>
			<option value="local">本地时间</option>
		  </select>
		</div>
	  </div>
	</div>

	<div class="section">
	  <div class="grid gap-3 lg:grid-cols-2 mb-4">
		<div class="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
		  <div class="text-[11px] mono uppercase tracking-[0.22em] text-teal-400">{{ .StrategyPerformance.Name }}</div>
		  <p class="mt-1 text-xs text-slate-400">单位 {{ .StrategyPerformance.ValueUnit }} · 与下方 U 本位净值图口径一致 · 无风险利率 0% · 年化 365 天</p>
		  <div class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-5 text-sm">
			<div><div class="text-slate-500">年化收益</div><div class="mt-1 font-mono text-white">{{ .StrategyPerformance.AnnualizedReturn }}</div></div>
			<div><div class="text-slate-500">年化波动</div><div class="mt-1 font-mono text-white">{{ .StrategyPerformance.AnnualizedVolatility }}</div></div>
			<div><div class="text-slate-500">最大回撤</div><div class="mt-1 font-mono text-white">{{ .StrategyPerformance.MaxDrawdown }}</div></div>
			<div><div class="text-slate-500">夏普比率</div><div class="mt-1 font-mono text-white">{{ .StrategyPerformance.SharpeRatio }}</div></div>
			<div><div class="text-slate-500">卡玛比</div><div class="mt-1 font-mono text-white">{{ .StrategyPerformance.CalmarRatio }}</div></div>
		  </div>
		</div>
		{{ if .HasAssetPerformance }}
		<div class="rounded-2xl border border-sky-400/20 bg-sky-400/[0.06] p-4">
		  <div class="text-[11px] mono uppercase tracking-[0.22em] text-sky-300">{{ .AssetPerformance.Name }}</div>
		  <p class="mt-1 text-xs text-slate-400">单位 {{ .AssetPerformance.ValueUnit }} · 与下方 Buy&amp;Hold 基准曲线口径一致</p>
		  <div class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-5 text-sm">
			<div><div class="text-slate-500">年化收益</div><div class="mt-1 font-mono text-white">{{ .AssetPerformance.AnnualizedReturn }}</div></div>
			<div><div class="text-slate-500">年化波动</div><div class="mt-1 font-mono text-white">{{ .AssetPerformance.AnnualizedVolatility }}</div></div>
			<div><div class="text-slate-500">最大回撤</div><div class="mt-1 font-mono text-white">{{ .AssetPerformance.MaxDrawdown }}</div></div>
			<div><div class="text-slate-500">夏普比率</div><div class="mt-1 font-mono text-white">{{ .AssetPerformance.SharpeRatio }}</div></div>
			<div><div class="text-slate-500">卡玛比</div><div class="mt-1 font-mono text-white">{{ .AssetPerformance.CalmarRatio }}</div></div>
		  </div>
		</div>
		{{ end }}
	  </div>
			<h2>策略表现</h2>
      <div class="overflow-x-auto">
        <table class="perf w-full">
          <thead>
            <tr>
							<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">资金与收益</th>
							<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">风险</th>
							{{ if not .NoTradeRows }}<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">交易</th>{{ end }}
							<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">权益</th>
							{{ if .SpreadSummary }}<th colspan="2" class="pb-2 text-teal-400 border-b border-teal-400/20">价差</th>{{ end }}
            </tr>
          </thead>
          <tbody>
            <tr>
							<td class="text-slate-400 !font-sans">初始资金</td><td>{{ .InitialCapital }}</td>
							<td class="text-slate-400 !font-sans">最大回撤</td><td>{{ .MaxDrawdown }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">原始成交</td><td>{{ .TradeOverview.RawFills }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">权益峰值</td><td>{{ .EquityAnalysis.PeakEquity }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">总数</td><td>{{ .SpreadSummary.TotalSpreads }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">最终权益</td><td>{{ .FinalEquity }}</td>
							<td class="text-slate-400 !font-sans">夏普比率</td><td>{{ .SharpeRatio }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">往返交易</td><td>{{ .TradeOverview.RoundTrips }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">最低权益</td><td>{{ .EquityAnalysis.LowestEquity }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">已平仓</td><td>{{ .SpreadSummary.ClosedSpreads }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">净盈亏</td><td>{{ .NetPnL }}</td>
						<td class="text-slate-400 !font-sans">卡玛比</td><td>{{ .CalmarRatio }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">多头 / 空头</td><td>{{ .TradeOverview.LongFills }} / {{ .TradeOverview.ShortFills }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">峰值时间</td><td>{{ .EquityAnalysis.PeakTime }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">未平仓</td><td>{{ .SpreadSummary.OpenSpreads }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">总收益率</td><td>{{ .TotalReturn }}</td>
						<td class="text-slate-400 !font-sans">年化波动率</td><td>{{ .AnnualizedVolatility }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">毛利润</td><td>{{ .TradeOverview.GrossProfit }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">最低点时间</td><td>{{ .EquityAnalysis.LowestTime }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">盈利</td><td>{{ .SpreadSummary.WinningSpreads }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">年化收益</td><td>{{ .AnnualizedReturn }}</td>
						<td class="text-slate-400 !font-sans">Bar 波动率</td><td>{{ .EquityAnalysis.BarReturnVolatility }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">毛亏损</td><td>{{ .TradeOverview.GrossLoss }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">上涨 Bars</td><td>{{ .EquityAnalysis.PositiveBars }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">亏损</td><td>{{ .SpreadSummary.LosingSpreads }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">总手续费</td><td>{{ .TotalFees }}</td>
						<td class="text-slate-400 !font-sans">最差 Bar</td><td>{{ .EquityAnalysis.WorstBarReturn }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">交易净盈亏</td><td>{{ .TradeOverview.NetPnL }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">下跌 Bars</td><td>{{ .EquityAnalysis.NegativeBars }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">胜率</td><td>{{ .SpreadSummary.WinRate }}</td>{{ end }}
            </tr>
            <tr>
							<td class="text-slate-400 !font-sans">Bars 数</td><td>{{ .BarsCount }}</td>
						<td class="text-slate-400 !font-sans">回撤时长</td><td>{{ .EquityAnalysis.MaxDrawdownDuration }}</td>
							{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">平均往返盈亏</td><td>{{ .TradeOverview.AvgPnLPerRoundTrip }}</td>{{ end }}
							<td class="text-slate-400 !font-sans">平盘 Bars</td><td>{{ .EquityAnalysis.FlatBars }}</td>
							{{ if .SpreadSummary }}<td class="text-slate-400 !font-sans">价差盈亏</td><td>{{ .SpreadSummary.TotalPnL }}</td>{{ end }}
            </tr>
            <tr>
              <td></td><td></td>
              <td class="text-slate-400 !font-sans">回撤持续 Bars</td><td>{{ .EquityAnalysis.MaxDrawdownDurationBars }}</td>
						{{ if not .NoTradeRows }}<td class="text-slate-400 !font-sans">平均手续费</td><td>{{ .TradeOverview.AvgCommissionPerFill }}</td>{{ else }}<td></td><td></td>{{ end }}
              <td></td><td></td>
              {{ if .SpreadSummary }}<td></td><td></td>{{ end }}
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    {{ if .HasUnderlyingChart }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
				<div>
				  <h2 class="!mb-0">参考价格图</h2>
				  <p class="mt-1 text-xs text-slate-400">显示 {{ .UnderlyingChartLabel }} · 数据源 {{ .UnderlyingChartSource }}{{ if .UnderlyingChartOverride }} · 用户显式覆盖{{ end }}</p>
				</div>
        <div class="flex gap-2 text-xs mono">
					<span class="text-up">买入</span>
					<span class="text-down">卖出</span>
					<span class="text-blue-400">开仓</span>
					<span class="text-rose-400">平仓</span>
        </div>
      </div>
      {{ if .UnderlyingChartNote }}<p class="text-xs text-amber-300 mb-3">{{ .UnderlyingChartNote }}</p>{{ end }}
			<div id="underlying-data-window" class="mb-3 rounded-xl border border-white/8 bg-white/[0.02] p-3">
				<div class="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
					<div>
						<div class="text-[11px] mono uppercase tracking-[0.22em] text-teal-400">数据窗口</div>
						<p class="mt-1 text-xs text-slate-400">将鼠标悬停在蜡烛图上可查看 OHLC{{ if .HasUnderlyingVolume }}、{{ .UnderlyingVolumeLabel }}{{ end }}{{ if .HasHoverColumns }} 以及策略列{{ end }}{{ if .HasFeatureColumns }}。点击非叠加策略列卡片可在子图中切换显示{{ end }}。</p>
					</div>
					<div id="underlying-data-window-time" class="mono text-xs text-slate-400"></div>
				</div>
				<div id="underlying-data-window-grid" class="mt-3 grid gap-2 sm:grid-cols-2 xl:grid-cols-6"></div>
			</div>
      <div class="chart-box p-1">
        <div id="underlying-chart" style="width:100%;height:420px;"></div>
				{{ if .HasFeatureColumns }}
				<div id="underlying-feature-panel" class="mt-1 border-t border-white/8 pt-3 hidden">
					<div class="flex flex-col gap-3 px-3 pb-2 sm:flex-row sm:items-center sm:justify-between">
						<div>
							<div id="underlying-feature-title" class="text-[11px] mono uppercase tracking-[0.18em] text-sky-300">特征子图</div>
							<p id="underlying-feature-subtitle" class="mt-1 text-xs text-slate-400">点击数据窗口中的非叠加策略列，可在价格图下方绘制多条序列。</p>
						</div>
						<button id="underlying-feature-clear" type="button" class="hidden rounded-md border border-white/10 px-3 py-1.5 text-xs text-slate-300 transition hover:border-white/20 hover:text-white">清空全部</button>
					</div>
					<div id="underlying-feature-empty" class="mx-3 mb-3 feature-empty-state mono">未选择任何特征。</div>
					<div id="underlying-feature-legend" class="mx-3 mb-3 hidden flex flex-wrap gap-2"></div>
					<div id="underlying-feature-chart" class="hidden" style="width:100%;height:220px;"></div>
				</div>
				{{ end }}
      </div>
    </div>
    {{ end }}

    <div class="section">
	<div class="flex flex-wrap items-center justify-between gap-3 mb-3">
		<div>
			<h2 class="!mb-1">权益曲线</h2>
			<p class="text-xs text-slate-400">范围 {{ .EquityMin }} 至 {{ .EquityMax }} · 手续费 {{ .TotalFees }}</p>
			<p id="equity-mode-note" class="text-xs text-slate-500">{{ if .DisplayIsDaily }}当前图表已按 1 Day 聚合，展示每个 UTC 日的最后一个权益点；切换后仅保留已结算权益。{{ else }}默认展示逐 bar 净值；切换后仅保留已结算收益点，并在下方显示区间浮盈浮亏与持仓暴露。{{ end }}</p>
		</div>
		<label class="inline-flex items-center gap-2 rounded-md border border-white/10 px-3 py-2 text-sm text-slate-200 cursor-pointer select-none" data-equity-mode="settled">
			<input id="toggle-settled-equity" type="checkbox" class="accent-teal-400" />
			<span>仅显示已结算权益</span>
		</label>
	</div>
      <div class="chart-box p-1">
        <div id="equity-chart" style="width:100%;height:300px;"></div>
      </div>
	  <div id="settled-context-panel" class="mt-3 hidden">
		<div class="mb-2 flex flex-wrap items-center justify-between gap-3">
			<div>
				<div class="text-[11px] mono uppercase tracking-[0.22em] text-teal-400">已结算模式上下文</div>
				<p class="mt-1 text-xs text-slate-400">绿色 / 红色柱表示两次结算之间的最大浮盈 / 浮亏，白线表示持仓暴露强度。</p>
			</div>
		</div>
		<div class="chart-box p-1">
			<div id="settled-context-chart" style="width:100%;height:180px;"></div>
		</div>
	  </div>
    </div>

	{{ if .HasQuoteNetValue }}
    <div class="section">
	<h2>U 本位净值</h2>
	<p class="text-xs text-slate-400 mb-3">范围 {{ .QuoteNetValueMin }} 至 {{ .QuoteNetValueMax }} · {{ .QuoteNetValueNote }}</p>
      <div class="chart-box p-1">
		<div id="quote-net-value-chart" style="width:100%;height:300px;"></div>
      </div>
	  {{ if .HasBuyHoldBenchmark }}<p class="mt-3 text-xs text-slate-400">Buy&amp;Hold 范围 {{ .BuyHoldMin }} 至 {{ .BuyHoldMax }} · {{ .BuyHoldNote }}</p>{{ end }}
	  <div class="mt-4 grid gap-3 lg:grid-cols-2">
		<div class="rounded-2xl border border-white/10 bg-white/[0.03] p-4">
		  <div class="text-[11px] mono uppercase tracking-[0.22em] text-teal-400">{{ .QuotePerformance.Name }}</div>
		  <div class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-5 text-sm">
			<div><div class="text-slate-500">年化收益</div><div class="mt-1 font-mono text-white">{{ .QuotePerformance.AnnualizedReturn }}</div></div>
			<div><div class="text-slate-500">年化波动</div><div class="mt-1 font-mono text-white">{{ .QuotePerformance.AnnualizedVolatility }}</div></div>
			<div><div class="text-slate-500">最大回撤</div><div class="mt-1 font-mono text-white">{{ .QuotePerformance.MaxDrawdown }}</div></div>
			<div><div class="text-slate-500">夏普比率</div><div class="mt-1 font-mono text-white">{{ .QuotePerformance.SharpeRatio }}</div></div>
			<div><div class="text-slate-500">卡玛比</div><div class="mt-1 font-mono text-white">{{ .QuotePerformance.CalmarRatio }}</div></div>
		  </div>
		</div>
		{{ if .HasBuyHoldPerformance }}
		<div class="rounded-2xl border border-sky-400/20 bg-sky-400/[0.06] p-4">
		  <div class="text-[11px] mono uppercase tracking-[0.22em] text-sky-300">{{ .BuyHoldPerformance.Name }}</div>
		  <div class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-5 text-sm">
			<div><div class="text-slate-500">年化收益</div><div class="mt-1 font-mono text-white">{{ .BuyHoldPerformance.AnnualizedReturn }}</div></div>
			<div><div class="text-slate-500">年化波动</div><div class="mt-1 font-mono text-white">{{ .BuyHoldPerformance.AnnualizedVolatility }}</div></div>
			<div><div class="text-slate-500">最大回撤</div><div class="mt-1 font-mono text-white">{{ .BuyHoldPerformance.MaxDrawdown }}</div></div>
			<div><div class="text-slate-500">夏普比率</div><div class="mt-1 font-mono text-white">{{ .BuyHoldPerformance.SharpeRatio }}</div></div>
			<div><div class="text-slate-500">卡玛比</div><div class="mt-1 font-mono text-white">{{ .BuyHoldPerformance.CalmarRatio }}</div></div>
		  </div>
		</div>
		{{ end }}
	  </div>
    </div>
    {{ end }}

	{{ if .HasDailyQuoteNetValue }}
	<div class="section">
	  <h2>1 Day 日终净值</h2>
	  <p class="text-xs text-slate-400 mb-3">{{ .DailyQuoteNetValueNote }}</p>
	  <div class="chart-box p-1">
		<div id="quote-daily-chart" style="width:100%;height:280px;"></div>
	  </div>
	</div>
	{{ end }}

	{{ if .HasDailyAssetPnL }}
	<div class="section">
	  <h2>1 Day Asset 本位 PnL</h2>
	  <p class="text-xs text-slate-400 mb-3">{{ .DailyAssetPnLNote }}</p>
	  <div class="chart-box p-1">
		<div id="asset-daily-pnl-chart" style="width:100%;height:280px;"></div>
	  </div>
	</div>
	{{ end }}

    <div class="section">
	<h2>回撤</h2>
	<p class="text-xs text-slate-400 mb-3">最大回撤 {{ .DrawdownMax }}</p>
      <div class="chart-box p-1">
        <div id="drawdown-chart" style="width:100%;height:220px;"></div>
      </div>
    </div>

	{{ if .TopDrawdownGroups }}
	<div class="section">
		<div class="flex items-center justify-between mb-3 gap-3">
			<h2 class="!mb-0">最大回撤前 5 订单组</h2>
			<span class="mono text-xs text-slate-400">按组内最大回撤从高到低排序</span>
		</div>
		<div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
			{{ range .TopDrawdownGroups }}
			<a href="#{{ .AnchorID }}" class="block rounded-xl border border-white/8 bg-white/[0.03] p-4 transition hover:border-white/15 hover:bg-white/[0.05]">
				<div class="flex items-start justify-between gap-3">
					<div>
						<div class="font-medium text-slate-100">组 #{{ .ID }} {{ .Tag }}</div>
						<div class="mt-2 inline-flex rounded-full px-2 py-0.5 text-[11px] font-mono {{ .StatusClass }}">{{ .Status }}</div>
					</div>
					<div class="text-right">
						<div class="text-[11px] tracking-[0.2em] text-slate-500">最大回撤</div>
						<div class="mt-1 font-mono text-base text-rose-200">{{ .MaxDrawdown }}</div>
					</div>
				</div>
				<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
					<div><dt class="text-slate-400">最高权益</dt><dd class="mt-1 font-mono text-white">{{ .HighestEquity }}</dd></div>
					<div><dt class="text-slate-400">最低权益</dt><dd class="mt-1 font-mono text-white">{{ .LowestEquity }}</dd></div>
					<div class="col-span-2"><dt class="text-slate-400">组盈亏</dt><dd class="mt-1 font-mono text-white">{{ .TotalPnL }}</dd></div>
				</dl>
			</a>
			{{ end }}
		</div>
	</div>
	{{ end }}

    {{ if not .NoSpreadRows }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
				<h2 class="!mb-0">价差活动</h2>
				<span class="mono text-xs text-slate-400">{{ .SpreadsCount }} 个持仓 · {{ len .Spreads }} 个事件</span>
      </div>
			<div class="mb-4 flex flex-wrap items-center justify-between gap-3">
				<p class="text-xs text-slate-400">开仓和平仓会拆分为独立事件，并按时间排序。可使用跳转链接在同一价差的两个事件之间切换；分组支持折叠查看。</p>
				{{ if .SpreadGroups }}
				<div class="flex flex-wrap gap-2">
					<button type="button" data-spread-groups-toggle="collapse" class="rounded-md border border-white/10 px-3 py-1.5 text-xs text-slate-300 transition hover:border-white/20 hover:text-white">全部折叠</button>
					<button type="button" data-spread-groups-toggle="expand" class="rounded-md border border-white/10 px-3 py-1.5 text-xs text-slate-300 transition hover:border-white/20 hover:text-white">全部展开</button>
				</div>
				{{ end }}
			</div>
			<div id="spread-sections-container"></div>
    </div>
    {{ end }}

    {{ if not .NoTradeRows }}
    <div class="section">
      <div class="flex items-center justify-between mb-3">
		<h2 class="!mb-0">成交明细</h2>
		<span class="mono text-xs text-slate-400">{{ .TradesCount }} 笔成交</span>
      </div>
      <div class="overflow-x-auto border border-white/8 rounded-lg">
        <div class="max-h-[32rem] overflow-auto">
          <table class="w-full text-sm">
            <thead class="sticky top-0 bg-[#111922]">
              <tr class="text-left text-slate-400 text-xs uppercase border-b border-white/8">
								<th class="px-4 py-2">时间</th>
								<th class="px-4 py-2">标的</th>
								<th class="px-4 py-2">方向</th>
								<th class="px-4 py-2">原因</th>
								<th class="px-4 py-2">数量</th>
								<th class="px-4 py-2">成交价</th>
								<th class="px-4 py-2">手续费</th>
								<th class="px-4 py-2">滑点</th>
								<th class="px-4 py-2">净额</th>
              </tr>
            </thead>
				<tbody id="trades-rows-container"></tbody>
          </table>
        </div>
      </div>
    </div>
    {{ end }}

    {{ if .Notes }}
    <div class="section">
	<h2>说明</h2>
      {{ range .Notes }}
      <p class="text-sm text-slate-400 mb-1">{{ . }}</p>
      {{ end }}
    </div>
    {{ end }}
  </div>

  <script>
    const compressedChartPayload = {{ .CompressedChartPayload }};
		const compressedTradeRowsHTML = {{ .CompressedTradeRowsHTML }};
		const compressedSpreadSectionsHTML = {{ .CompressedSpreadSectionsHTML }};
		const underlyingVolumeLabel = {{ printf "%q" .UnderlyingVolumeLabel }};

		function decodeCompressedJSON(key, fallbackValue) {
			const encoded = compressedChartPayload[key];
			if (!encoded) {
				return fallbackValue;
			}
			if (!window.pako) {
				console.warn('pako unavailable; chart payload skipped for', key);
				return fallbackValue;
			}
			try {
				const binary = atob(encoded);
				const bytes = new Uint8Array(binary.length);
				for (let index = 0; index < binary.length; index++) {
					bytes[index] = binary.charCodeAt(index);
				}
				const decoded = window.pako.ungzip(bytes, { to: 'string' });
				return JSON.parse(decoded);
			} catch (error) {
				console.warn('failed to decode chart payload', key, error);
				return fallbackValue;
			}
		}

		function decodeCompressedText(encoded) {
			if (!encoded || !window.pako) {
				return '';
			}
			try {
				const binary = atob(encoded);
				const bytes = new Uint8Array(binary.length);
				for (let index = 0; index < binary.length; index++) {
					bytes[index] = binary.charCodeAt(index);
				}
				return window.pako.ungzip(bytes, { to: 'string' });
			} catch (error) {
				console.warn('failed to decode html payload', error);
				return '';
			}
		}

		function hydrateDeferredHTML() {
			const tradeRowsContainer = document.getElementById('trades-rows-container');
			if (tradeRowsContainer && compressedTradeRowsHTML) {
				tradeRowsContainer.innerHTML = decodeCompressedText(compressedTradeRowsHTML);
			}
			const spreadSectionsContainer = document.getElementById('spread-sections-container');
			if (spreadSectionsContainer && compressedSpreadSectionsHTML) {
				spreadSectionsContainer.innerHTML = decodeCompressedText(compressedSpreadSectionsHTML);
			}
		}

		const underlyingCandles = decodeCompressedJSON('underlyingCandles', []);
		const underlyingVolumeSeries = decodeCompressedJSON('underlyingVolumeSeries', []);
		const underlyingMarkers = decodeCompressedJSON('underlyingMarkers', []);
		const hoverColumns = decodeCompressedJSON('hoverColumns', []);
		const equitySeries = decodeCompressedJSON('equitySeries', []);
		const settledEquitySeries = decodeCompressedJSON('settledEquitySeries', []);
		const settledFloatingProfitSeries = decodeCompressedJSON('settledFloatingProfit', []);
		const settledFloatingLossSeries = decodeCompressedJSON('settledFloatingLoss', []);
		const settledExposureSeries = decodeCompressedJSON('settledExposure', []);
		const quoteNetValueSeries = decodeCompressedJSON('quoteNetValueSeries', []);
		const dailyQuoteNetValueSeries = decodeCompressedJSON('dailyQuoteNetValue', []);
		const dailyBuyHoldSeries = decodeCompressedJSON('dailyBuyHold', []);
		const dailyAssetPnLSeries = decodeCompressedJSON('dailyAssetPnL', []);
		const buyHoldSeries = decodeCompressedJSON('buyHoldSeries', []);
		const drawdownSeries = decodeCompressedJSON('drawdownSeries', []);
		const activeTimes = decodeCompressedJSON('activeTimes', []);
		const displayIsDaily = {{ if .DisplayIsDaily }}true{{ else }}false{{ end }};
		hydrateDeferredHTML();

	// Timezone mode utilities
	function detectDefaultTimeZoneMode() {
		try {
			var tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
			return (tz && tz !== 'UTC') ? 'local' : 'utc';
		} catch (e) {
			return 'utc';
		}
	}

	function formatDateTimeForMode(timeValue, mode) {
		var unixSeconds = normalizeUnixSeconds(timeValue);
		if (unixSeconds === null) return '';
		var date = new Date(unixSeconds * 1000);
		if (mode === 'local') {
			return date.getFullYear() + '-' +
				String(date.getMonth() + 1).padStart(2, '0') + '-' +
				String(date.getDate()).padStart(2, '0') + ' ' +
				String(date.getHours()).padStart(2, '0') + ':' +
				String(date.getMinutes()).padStart(2, '0');
		}
		return date.getUTCFullYear() + '-' +
			String(date.getUTCMonth() + 1).padStart(2, '0') + '-' +
			String(date.getUTCDate()).padStart(2, '0') + ' ' +
			String(date.getUTCHours()).padStart(2, '0') + ':' +
			String(date.getUTCMinutes()).padStart(2, '0') + ' UTC';
	}

	function rewriteVisibleDateText(mode) {
		var dateElements = document.querySelectorAll('[data-utc-time]');
		dateElements.forEach(function(el) {
			var utcTime = parseInt(el.getAttribute('data-utc-time'), 10);
			if (!isNaN(utcTime)) {
				el.textContent = formatDateTimeForMode(utcTime, mode);
			}
		});
	}

	function applyTimeZoneMode(mode) {
		var select = document.getElementById('timezone-select');
		if (select) select.value = mode;
		rewriteVisibleDateText(mode);
	}

		const themePalettes = {
			light: {
				background: '#ffffff',
				text: '#475569',
				grid: '#edf2f7',
				border: '#d7dee8',
				crosshairV: 'rgba(15,108,123,0.22)',
				crosshairVLabel: '#0f6c7b',
				crosshairH: 'rgba(188,75,9,0.18)',
				crosshairHLabel: '#bc4b09',
				settledLine: '#0f172a',
				settledMarker: '#0f172a',
				settledMarkerBg: '#ffffff',
				exposureLine: 'rgba(51,65,85,0.92)'
			},
			dark: {
				background: '#0f1a24',
				text: '#cbd5e1',
				grid: 'rgba(255,255,255,0.05)',
				border: 'rgba(255,255,255,0.08)',
				crosshairV: 'rgba(61,214,198,0.28)',
				crosshairVLabel: '#0f766e',
				crosshairH: 'rgba(240,164,58,0.24)',
				crosshairHLabel: '#a16207',
				settledLine: '#f8fafc',
				settledMarker: '#f8fafc',
				settledMarkerBg: '#0f1a24',
				exposureLine: 'rgba(226,232,240,0.9)'
			}
		};

		function currentThemeName() {
			var theme = document.documentElement.getAttribute('data-theme');
			return theme === 'dark' ? 'dark' : 'light';
		}

		function currentThemePalette() {
			return themePalettes[currentThemeName()] || themePalettes.light;
		}

		function buildChartTheme(palette) {
			return {
				layout: {
					background: { color: palette.background },
					textColor: palette.text,
					fontFamily: 'IBM Plex Mono, monospace'
				},
				grid: {
					vertLines: { color: palette.grid },
					horzLines: { color: palette.grid }
				},
				timeScale: {
					borderColor: palette.border,
					timeVisible: true,
					secondsVisible: false,
					minBarSpacing: 0.1,
					rightOffset: 0
				},
				rightPriceScale: {
					borderColor: palette.border
				},
				crosshair: {
					vertLine: { color: palette.crosshairV, labelBackgroundColor: palette.crosshairVLabel },
					horzLine: { color: palette.crosshairH, labelBackgroundColor: palette.crosshairHLabel }
				}
			};
		}

		const utcMonthNames = ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月'];

		function padUTC(value) {
			return String(value).padStart(2, '0');
		}

		function normalizeUnixSeconds(timeValue) {
			if (typeof timeValue === 'number' && isFinite(timeValue)) {
				return timeValue;
			}
			if (timeValue && typeof timeValue.timestamp === 'number' && isFinite(timeValue.timestamp)) {
				return timeValue.timestamp;
			}
			if (
				timeValue &&
				typeof timeValue.year === 'number' &&
				typeof timeValue.month === 'number' &&
				typeof timeValue.day === 'number'
			) {
				return Math.floor(Date.UTC(timeValue.year, timeValue.month - 1, timeValue.day) / 1000);
			}
			return null;
		}

		function formatUTCDateTime(timeValue, includeSeconds) {
			var unixSeconds = normalizeUnixSeconds(timeValue);
			if (unixSeconds === null) {
				return '';
			}
			var date = new Date(unixSeconds * 1000);
			var formatted =
				date.getUTCFullYear() + '-' +
				padUTC(date.getUTCMonth() + 1) + '-' +
				padUTC(date.getUTCDate()) + ' ' +
				padUTC(date.getUTCHours()) + ':' +
				padUTC(date.getUTCMinutes());
			if (includeSeconds) {
				formatted += ':' + padUTC(date.getUTCSeconds());
			}
			return formatted + ' UTC';
		}

		function formatUTCTickLabel(timeValue) {
			var unixSeconds = normalizeUnixSeconds(timeValue);
			if (unixSeconds === null) {
				return '';
			}
			var date = new Date(unixSeconds * 1000);
			if (date.getUTCHours() === 0 && date.getUTCMinutes() === 0 && date.getUTCSeconds() === 0) {
				return utcMonthNames[date.getUTCMonth()] + padUTC(date.getUTCDate()) + '日';
			}
			return padUTC(date.getUTCHours()) + ':' + padUTC(date.getUTCMinutes());
		}

		function createChart(id, height, extraOptions) {
      var el = document.getElementById(id);
      if (!el || typeof LightweightCharts === 'undefined') return null;
      var chart = LightweightCharts.createChart(el, Object.assign({
        width: el.clientWidth, height: height,
				handleScroll: true, handleScale: true,
				localization: {
					locale: 'zh-CN',
					timeFormatter: function(timeValue) { return formatUTCDateTime(timeValue, true); },
					tickMarkFormatter: function(timeValue) { return formatUTCTickLabel(timeValue); }
				}
			}, buildChartTheme(currentThemePalette()), extraOptions || {}));
      if (typeof ResizeObserver !== 'undefined') {
        new ResizeObserver(function() { chart.applyOptions({ width: el.clientWidth }); }).observe(el);
      }
      return chart;
    }

		var chartSyncState = { syncing: false, synced: new WeakSet() };

		function registerSyncedChart(chart) {
			if (!chart || chartSyncState.synced.has(chart)) return;
			chartSyncState.synced.add(chart);
			chart.timeScale().subscribeVisibleTimeRangeChange(function(range) {
				if (!range || chartSyncState.syncing) return;
				chartSyncState.syncing = true;
				syncedCharts.forEach(function(otherChart) {
					if (otherChart !== chart) otherChart.timeScale().setVisibleRange(range);
				});
				chartSyncState.syncing = false;
			});
		}

		function buildActiveTimeSet() {
			var set = new Set();
			if (!Array.isArray(activeTimes)) return set;
			activeTimes.forEach(function(t) {
				if (typeof t === 'number') set.add(t);
			});
			return set;
		}

		function filterLineSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}
		function filterCandleSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

		function filterMarkerSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

		function filterHistogramSeriesByTimes(series, activeSet, enabled) {
			if (!enabled || activeSet.size === 0) return series;
			return series.filter(function(point) {
				return activeSet.has(point.time);
			});
		}

		function buildBaseTimeline() {
			var seen = new Set();
			var times = [];
			function appendTimes(series) {
				(series || []).forEach(function(point) {
					if (!point || typeof point.time !== 'number' || seen.has(point.time)) return;
					seen.add(point.time);
					times.push(point.time);
				});
			}
			appendTimes(underlyingCandles);
			appendTimes(equitySeries);
			appendTimes(settledEquitySeries);
			appendTimes(settledFloatingProfitSeries);
			appendTimes(settledFloatingLossSeries);
			appendTimes(settledExposureSeries);
			appendTimes(drawdownSeries);
			appendTimes(quoteNetValueSeries);
			appendTimes(buyHoldSeries);
			appendTimes(dailyQuoteNetValueSeries);
			appendTimes(dailyBuyHoldSeries);
			times.sort(function(a, b) { return a - b; });
			return times;
		}

		function currentTimeline() {
			if (!currentIdleFilterEnabled || activeSet.size === 0) return baseTimeline.slice();
			return baseTimeline.filter(function(time) {
				return activeSet.has(time);
			});
		}

		function applyVisibleTimeRange(range) {
			if (!range || range.from === undefined || range.to === undefined) return;
			syncedCharts.forEach(function(chart) {
				if (chart) chart.timeScale().setVisibleRange(range);
			});
		}

		function resolveSeriesTimeRange(seriesCollections) {
			var collections = Array.isArray(seriesCollections) ? seriesCollections : [seriesCollections];
			var minTime = null;
			var maxTime = null;
			collections.forEach(function(series) {
				(series || []).forEach(function(point) {
					if (!point || typeof point.time !== 'number' || !isFinite(point.time)) return;
					if (minTime === null || point.time < minTime) minTime = point.time;
					if (maxTime === null || point.time > maxTime) maxTime = point.time;
				});
			});
			if (minTime === null || maxTime === null) return null;
			return { from: minTime, to: maxTime };
		}

		function applyChartFullRange(chart, seriesCollections) {
			if (!chart) return;
			var range = resolveSeriesTimeRange(seriesCollections);
			if (range) chart.timeScale().setVisibleRange(range);
		}

		function fitChartsToVisibleData() {
			var timeline = currentTimeline();
			if (!timeline.length) return;
			applyVisibleTimeRange({ from: timeline[0], to: timeline[timeline.length - 1] });
		}

		function nearestTimeIndex(timeline, targetTime) {
			if (!timeline.length) return -1;
			var left = 0;
			var right = timeline.length - 1;
			while (left <= right) {
				var middle = Math.floor((left + right) / 2);
				var candidate = timeline[middle];
				if (candidate === targetTime) return middle;
				if (candidate < targetTime) left = middle + 1;
				else right = middle - 1;
			}
			if (left >= timeline.length) return timeline.length - 1;
			if (right < 0) return 0;
			return Math.abs(timeline[left] - targetTime) < Math.abs(timeline[right] - targetTime) ? left : right;
		}

		function focusChartsAtEvent(targetTime, windowStart, windowEnd) {
			var timeline = currentTimeline();
			if (!timeline.length || typeof targetTime !== 'number' || !isFinite(targetTime)) return;
			var startIndex = nearestTimeIndex(timeline, targetTime);
			var endIndex = startIndex;
			if (typeof windowStart === 'number' && isFinite(windowStart) && typeof windowEnd === 'number' && isFinite(windowEnd)) {
				startIndex = nearestTimeIndex(timeline, Math.min(windowStart, windowEnd));
				endIndex = nearestTimeIndex(timeline, Math.max(windowStart, windowEnd));
			}
			if (startIndex < 0 || endIndex < 0) return;
			if (endIndex < startIndex) {
				var swap = startIndex;
				startIndex = endIndex;
				endIndex = swap;
			}
			var span = endIndex - startIndex;
			var padding = span > 0 ? Math.max(4, Math.round(span * 0.35)) : 12;
			startIndex = Math.max(0, startIndex - padding);
			endIndex = Math.min(timeline.length - 1, endIndex + padding);
			applyVisibleTimeRange({ from: timeline[startIndex], to: timeline[endIndex] });
			var chartHost = document.getElementById('underlying-chart') || document.getElementById('equity-chart');
			if (chartHost && typeof chartHost.scrollIntoView === 'function') {
				chartHost.scrollIntoView({ behavior: 'smooth', block: 'center' });
			}
		}

    function syncCharts(charts) {
			charts.forEach(registerSyncedChart);
    }

		var charts = [];
		var syncedCharts = [];
		var activeSet = buildActiveTimeSet();
		var baseTimeline = buildBaseTimeline();
		var hasActiveFilter = activeSet.size > 1;

		var underlyingChart = null;
		var underlyingSeries = null;
		var underlyingVolumePlot = null;
		var overlayPlots = new Map();
		var featureChart = null;
		var featurePlots = new Map();
		var featurePanel = document.getElementById('underlying-feature-panel');
		var featureChartEl = document.getElementById('underlying-feature-chart');
		var featureEmpty = document.getElementById('underlying-feature-empty');
		var featureLegend = document.getElementById('underlying-feature-legend');
		var featureTitle = document.getElementById('underlying-feature-title');
		var featureSubtitle = document.getElementById('underlying-feature-subtitle');
		var featureClear = document.getElementById('underlying-feature-clear');
		var equityChart = null;
		var equityPlot = null;
		var settledEquityPlot = null;
		var settledContextPanel = document.getElementById('settled-context-panel');
		var settledContextChart = null;
		var settledFloatingProfitPlot = null;
		var settledFloatingLossPlot = null;
		var settledExposurePlot = null;
		var settledModeEnabled = false;
		var equityModeNote = document.getElementById('equity-mode-note');
		var quoteNetValueChart = null;
		var quoteNetValuePlot = null;
		var quoteBuyHoldPlot = null;
		var quoteDailyChart = null;
		var quoteDailyPlot = null;
		var quoteDailyBuyHoldPlot = null;
		var assetDailyPnLChart = null;
		var assetDailyPnLPlot = null;
		var drawdownChart = null;
		var drawdownPlot = null;
		var dataWindowGrid = document.getElementById('underlying-data-window-grid');
		var dataWindowTime = document.getElementById('underlying-data-window-time');
		var candleByTime = new Map();
		var volumeByTime = new Map();
		var hoverColumnMaps = [];
		var selectedHoverColumnSources = new Set();
		var currentDataWindowTime = null;
		var currentIdleFilterEnabled = false;

		function addChart(chart, options) {
			if (!chart) return;
			charts.push(chart);
			if (options && options.sync === false) return;
			syncedCharts.push(chart);
			registerSyncedChart(chart);
		}

		underlyingCandles.forEach(function(point) {
			candleByTime.set(point.time, point);
		});
		underlyingVolumeSeries.forEach(function(point) {
			volumeByTime.set(point.time, point.value);
		});
		hoverColumnMaps = hoverColumns.map(function(column) {
			var values = new Map();
			(column.values || []).forEach(function(point) {
				values.set(point.time, point.value);
			});
			return {
				source: column.source,
				label: column.label || column.source,
				decimals: typeof column.decimals === 'number' ? column.decimals : 2,
				overlay: column.overlay === true,
				values: values,
				series: Array.isArray(column.values) ? column.values : []
			};
		});

		function selectedHoverColumns() {
			return hoverColumnMaps.filter(function(column) {
				return !column.overlay && selectedHoverColumnSources.has(column.source);
			});
		}

		function overlayHoverColumns() {
			return hoverColumnMaps.filter(function(column) {
				return column.overlay;
			});
		}

		function formatTimestamp(unixSeconds) {
			if (typeof unixSeconds !== 'number') {
				return '未悬停图表';
			}
			return formatUTCDateTime(unixSeconds, true);
		}

		function formatNumber(value, decimals) {
			if (typeof value !== 'number' || !isFinite(value)) {
				return 'n/a';
			}
			return value.toLocaleString('zh-CN', {
				minimumFractionDigits: decimals,
				maximumFractionDigits: decimals
			});
		}

		function escapeHTML(value) {
			return String(value)
				.replace(/&/g, '&amp;')
				.replace(/</g, '&lt;')
				.replace(/>/g, '&gt;')
				.replace(/\"/g, '&quot;')
				.replace(/'/g, '&#39;');
		}

		function featureColor(index) {
			var palette = ['#38bdf8', '#f472b6', '#f59e0b', '#a3e635', '#22d3ee', '#fb7185', '#c084fc', '#facc15'];
			return palette[index % palette.length];
		}

		function preserveVisibleRange(chart, callback) {
			if (!chart) {
				callback();
				return;
			}
			var visibleRange = chart.timeScale().getVisibleLogicalRange();
			callback();
			if (visibleRange) {
				chart.timeScale().setVisibleLogicalRange(visibleRange);
			}
		}

		function preserveVisibleRanges(targetCharts, callback) {
			var ranges = targetCharts.map(function(chart) {
				return chart ? chart.timeScale().getVisibleLogicalRange() : null;
			});
			callback();
			targetCharts.forEach(function(chart, index) {
				if (chart && ranges[index]) {
					chart.timeScale().setVisibleLogicalRange(ranges[index]);
				}
			});
		}

		function ensureSettledContextChart() {
			if (!settledContextPanel || settledContextChart) return;
			settledContextChart = createChart('settled-context-chart', 180);
			if (!settledContextChart) return;
			var palette = currentThemePalette();
			settledContextChart.applyOptions({
				leftPriceScale: {
					visible: true,
					borderColor: palette.border
				},
				rightPriceScale: {
					visible: true,
					borderColor: palette.border
				}
			});
			settledFloatingProfitPlot = settledContextChart.addHistogramSeries({
				priceScaleId: 'right',
				priceLineVisible: false,
				lastValueVisible: false,
				base: 0,
			});
			settledFloatingLossPlot = settledContextChart.addHistogramSeries({
				priceScaleId: 'right',
				priceLineVisible: false,
				lastValueVisible: false,
				base: 0,
			});
			settledExposurePlot = settledContextChart.addLineSeries({
				priceScaleId: 'left',
				color: palette.exposureLine,
				lineWidth: 2,
				priceLineVisible: false,
				lastValueVisible: true,
				crosshairMarkerRadius: 3,
				crosshairMarkerBorderColor: palette.exposureLine,
				crosshairMarkerBackgroundColor: palette.exposureLine,
			});
			charts.push(settledContextChart);
			registerSyncedChart(settledContextChart);
		}

		function renderSettledContext(showSettled) {
			if (!settledContextPanel) return;
			if (!showSettled) {
				settledContextPanel.classList.add('hidden');
				if (settledFloatingProfitPlot) settledFloatingProfitPlot.setData([]);
				if (settledFloatingLossPlot) settledFloatingLossPlot.setData([]);
				if (settledExposurePlot) settledExposurePlot.setData([]);
				return;
			}
			settledContextPanel.classList.remove('hidden');
			ensureSettledContextChart();
			if (!settledContextChart) return;
			settledFloatingProfitPlot.setData(filterHistogramSeriesByTimes(settledFloatingProfitSeries, activeSet, currentIdleFilterEnabled));
			settledFloatingLossPlot.setData(filterHistogramSeriesByTimes(settledFloatingLossSeries, activeSet, currentIdleFilterEnabled));
			settledExposurePlot.setData(filterLineSeriesByTimes(settledExposureSeries, activeSet, currentIdleFilterEnabled));
		}

		function renderEquitySeriesMode(showSettled, options) {
			if (!equityPlot || !settledEquityPlot) return;
			settledModeEnabled = showSettled === true;
			var shouldRefit = !options || options.refit !== false;
			equityPlot.setData(settledModeEnabled ? [] : filterLineSeriesByTimes(equitySeries, activeSet, currentIdleFilterEnabled));
			settledEquityPlot.setData(settledModeEnabled ? filterLineSeriesByTimes(settledEquitySeries, activeSet, currentIdleFilterEnabled) : []);
			if (equityModeNote) {
				equityModeNote.textContent = settledModeEnabled
					? (displayIsDaily ? '当前仅展示按 1 Day 聚合后的已结算权益；下方面板给出每个 UTC 日内的最大浮盈 / 浮亏与日终持仓暴露。' : '当前仅展示已结算权益点；下方面板会给出每次结算之间的最大浮盈 / 浮亏与持仓暴露。')
					: (displayIsDaily ? '当前图表已按 1 Day 聚合，展示每个 UTC 日的最后一个权益点；切换后仅保留已结算权益。' : '默认展示逐 bar 净值；切换后仅保留已结算收益点，并在下方显示区间浮盈浮亏与持仓暴露。');
			}
			renderSettledContext(settledModeEnabled);
			if (shouldRefit) fitChartsToVisibleData();
		}

		function appendDataWindowItem(items, label, value) {
			items.push(
				'<div class="data-window-card">' +
				'<div class="text-[11px] mono uppercase tracking-[0.18em] text-slate-500">' + escapeHTML(label) + '</div>' +
				'<div class="mt-1 mono text-sm text-slate-100">' + escapeHTML(value) + '</div>' +
				'</div>'
			);
		}

		function appendFeatureWindowItem(items, label, value, source, active, color) {
			items.push(
				'<button type="button" class="data-window-card data-window-card-button' + (active ? ' active' : '') + '" data-hover-source="' + escapeHTML(source) + '">' +
				'<div class="flex items-start justify-between gap-3">' +
				'<div>' +
				'<div class="text-[11px] mono uppercase tracking-[0.18em] text-slate-500">' + escapeHTML(label) + '</div>' +
				'<div class="mt-1 mono text-sm text-slate-100">' + escapeHTML(value) + '</div>' +
				'</div>' +
				'<div class="flex flex-col items-end gap-1">' +
				'<span class="feature-legend-swatch" style="background:' + escapeHTML(color) + '"></span>' +
				'<div class="text-[10px] mono uppercase tracking-[0.18em] ' + (active ? 'text-teal-300' : 'text-slate-500') + '">' + (active ? '已显示' : '添加') + '</div>' +
				'</div>' +
				'</div>' +
				'</button>'
			);
		}

		function appendOverlayWindowItem(items, label, value, color) {
			items.push(
				'<div class="data-window-card">' +
				'<div class="flex items-start justify-between gap-3">' +
				'<div>' +
				'<div class="text-[11px] mono uppercase tracking-[0.18em] text-slate-500">' + escapeHTML(label) + '</div>' +
				'<div class="mt-1 mono text-sm text-slate-100">' + escapeHTML(value) + '</div>' +
				'</div>' +
				'<div class="flex flex-col items-end gap-1">' +
				'<span class="feature-legend-swatch" style="background:' + escapeHTML(color) + '"></span>' +
				'<div class="text-[10px] mono uppercase tracking-[0.18em] text-sky-300">叠加</div>' +
				'</div>' +
				'</div>' +
				'</div>'
			);
		}

		function createFeatureChartIfNeeded() {
			if (!featureChartEl || featureChart) return;
			featureChart = createChart('underlying-feature-chart', 220);
			if (!featureChart) return;
			charts.push(featureChart);
			registerSyncedChart(featureChart);
			if (underlyingChart) {
				var visibleRange = underlyingChart.timeScale().getVisibleLogicalRange();
				if (visibleRange) featureChart.timeScale().setVisibleLogicalRange(visibleRange);
				else featureChart.timeScale().fitContent();
			}
		}

		function ensureFeaturePlot(source, color) {
			var plot = featurePlots.get(source);
			if (plot || !featureChart) {
				return plot;
			}
			plot = featureChart.addLineSeries({
				color: color,
				lineWidth: 2,
				priceLineVisible: false,
				lastValueVisible: true,
				crosshairMarkerRadius: 4,
				crosshairMarkerBorderColor: color,
				crosshairMarkerBackgroundColor: color
			});
			featurePlots.set(source, plot);
			return plot;
		}

		function ensureOverlayPlot(source, color) {
			var plot = overlayPlots.get(source);
			if (plot || !underlyingChart) {
				return plot;
			}
			plot = underlyingChart.addLineSeries({
				priceScaleId: 'right',
				color: color,
				lineWidth: 2,
				priceLineVisible: false,
				lastValueVisible: true,
				crosshairMarkerRadius: 4,
				crosshairMarkerBorderColor: color,
				crosshairMarkerBackgroundColor: color
			});
			overlayPlots.set(source, plot);
			return plot;
		}

		function renderOverlayPlots() {
			if (!underlyingChart) return;
			preserveVisibleRange(underlyingChart, function() {
				var overlayColumns = overlayHoverColumns();
				var activeSources = new Set(overlayColumns.map(function(column) { return column.source; }));
				overlayPlots.forEach(function(plot, source) {
					if (!activeSources.has(source)) {
						underlyingChart.removeSeries(plot);
						overlayPlots.delete(source);
					}
				});
				overlayColumns.forEach(function(column, index) {
					var color = featureColor(index);
					var plot = ensureOverlayPlot(column.source, color);
					if (!plot) return;
					plot.applyOptions({
						color: color,
						crosshairMarkerBorderColor: color,
						crosshairMarkerBackgroundColor: color
					});
					plot.setData(filterLineSeriesByTimes(column.series, activeSet, currentIdleFilterEnabled));
				});
			});
		}

		function renderFeatureLegend(columns, unixSeconds) {
			if (!featureLegend) return;
			if (!columns.length) {
				featureLegend.classList.add('hidden');
				featureLegend.innerHTML = '';
				return;
			}
			featureLegend.classList.remove('hidden');
			featureLegend.innerHTML = columns.map(function(column) {
				var value = typeof unixSeconds === 'number' ? column.values.get(unixSeconds) : undefined;
				var decimals = typeof column.decimals === 'number' ? column.decimals : 2;
				return '<div class="inline-flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.03] px-3 py-1 text-xs text-slate-300">' +
					'<span class="feature-legend-swatch" style="background:' + escapeHTML(featureColor(column.index)) + '"></span>' +
					'<span class="mono">' + escapeHTML(column.label) + '</span>' +
					'<span class="mono feature-legend-value">' + escapeHTML(formatNumber(value, decimals)) + '</span>' +
					'</div>';
			}).join('');
		}

		function renderFeatureChart() {
			if (!featurePanel) return;
			var selectedColumns = selectedHoverColumns();
			if (!selectedColumns.length) {
				featurePanel.classList.add('hidden');
				if (featureClear) featureClear.classList.add('hidden');
				if (featureEmpty) featureEmpty.classList.remove('hidden');
				if (featureLegend) featureLegend.classList.add('hidden');
				if (featureChartEl) featureChartEl.classList.add('hidden');
				featurePlots.forEach(function(plot) {
					if (featureChart) featureChart.removeSeries(plot);
				});
				featurePlots.clear();
				return;
			}

			featurePanel.classList.remove('hidden');
			createFeatureChartIfNeeded();
			if (!featureChart) return;

			preserveVisibleRange(featureChart, function() {
				var activeSources = new Set(selectedColumns.map(function(column) { return column.source; }));
				featurePlots.forEach(function(plot, source) {
					if (!activeSources.has(source)) {
						featureChart.removeSeries(plot);
						featurePlots.delete(source);
					}
				});
				selectedColumns.forEach(function(column, index) {
					var color = featureColor(index);
					var plot = ensureFeaturePlot(column.source, color);
					if (!plot) return;
					plot.applyOptions({
						color: color,
						crosshairMarkerBorderColor: color,
						crosshairMarkerBackgroundColor: color
					});
					plot.setData(filterLineSeriesByTimes(column.series, activeSet, currentIdleFilterEnabled));
				});
			});

			if (featureTitle) featureTitle.textContent = selectedColumns.length === 1 ? selectedColumns[0].label : '特征子图 · ' + selectedColumns.length + ' 条序列';
			if (featureSubtitle) featureSubtitle.textContent = selectedColumns.map(function(column) {
				return column.label + ' (' + column.source + ')';
			}).join(' · ');
			renderFeatureLegend(selectedColumns.map(function(column) {
				return {
					index: hoverColumnMaps.findIndex(function(candidate) { return candidate.source === column.source; }),
					label: column.label,
					decimals: column.decimals,
					values: column.values,
				};
			}), currentDataWindowTime);
			if (featureClear) featureClear.classList.remove('hidden');
			if (featureEmpty) featureEmpty.classList.add('hidden');
			if (featureChartEl) featureChartEl.classList.remove('hidden');

			if (featureChart && !featureChart.__toktikCrosshairBound) {
				featureChart.__toktikCrosshairBound = true;
				featureChart.subscribeCrosshairMove(function(param) {
					if (!param || param.time === undefined) return;
					renderDataWindow(Number(param.time));
				});
			}
		}

		function renderDataWindow(unixSeconds) {
			if (!dataWindowGrid) {
				return;
			}
			var resolvedTime = typeof unixSeconds === 'number' ? unixSeconds : null;
			if (resolvedTime === null && underlyingCandles.length > 0) {
				resolvedTime = underlyingCandles[underlyingCandles.length - 1].time;
			}
			if (dataWindowTime) {
				dataWindowTime.textContent = formatTimestamp(resolvedTime);
			}
			if (resolvedTime === null) {
				currentDataWindowTime = null;
				dataWindowGrid.innerHTML = '<div class="data-window-card mono text-sm text-slate-300">无可用图表数据。</div>';
				return;
			}
			currentDataWindowTime = resolvedTime;
			var candle = candleByTime.get(resolvedTime);
			var items = [];
			if (candle) {
				appendDataWindowItem(items, '开盘', formatNumber(candle.open, 2));
				appendDataWindowItem(items, '最高', formatNumber(candle.high, 2));
				appendDataWindowItem(items, '最低', formatNumber(candle.low, 2));
				appendDataWindowItem(items, '收盘', formatNumber(candle.close, 2));
			}
			if (volumeByTime.has(resolvedTime)) {
				appendDataWindowItem(items, underlyingVolumeLabel || '成交量', formatNumber(volumeByTime.get(resolvedTime), 0));
			}
			hoverColumnMaps.forEach(function(column, index) {
				if (column.overlay) {
					appendOverlayWindowItem(items, column.label, formatNumber(column.values.get(resolvedTime), column.decimals), featureColor(index));
					return;
				}
				appendFeatureWindowItem(items, column.label, formatNumber(column.values.get(resolvedTime), column.decimals), column.source, selectedHoverColumnSources.has(column.source), featureColor(index));
			});
			dataWindowGrid.innerHTML = items.join('');
			renderFeatureLegend(selectedHoverColumns().map(function(column) {
				return {
					index: hoverColumnMaps.findIndex(function(candidate) { return candidate.source === column.source; }),
					label: column.label,
					decimals: column.decimals,
					values: column.values,
				};
			}), resolvedTime);
		}

    if (underlyingCandles.length > 0) {
      var pc = createChart('underlying-chart', 420);
      if (pc) {
				var cs = pc.addCandlestickSeries({
          priceScaleId: 'right',
          upColor: '#22c55e', downColor: '#f97316',
          wickUpColor: '#22c55e', wickDownColor: '#f97316',
          borderUpColor: '#22c55e', borderDownColor: '#f97316'
        });
				cs.priceScale().applyOptions({
					scaleMargins: { top: 0.04, bottom: underlyingVolumeSeries.length > 0 ? 0.24 : 0.06 }
				});
				cs.setData(underlyingCandles);
				cs.setMarkers(underlyingMarkers);
				pc.subscribeCrosshairMove(function(param) {
					if (!param || param.time === undefined) {
						renderDataWindow(null);
						return;
					}
					renderDataWindow(Number(param.time));
				});
        pc.timeScale().fitContent();
				underlyingChart = pc;
				underlyingSeries = cs;
				addChart(pc);
				renderOverlayPlots();
      }
    }

		if (underlyingVolumeSeries.length > 0) {
			if (underlyingChart) {
				var pvs = underlyingChart.addHistogramSeries({
					priceScaleId: 'volume',
					priceFormat: { type: 'volume' },
					priceLineVisible: false,
					lastValueVisible: false,
					lastPriceAnimation: 0,
					base: 0
				});
				pvs.setData(underlyingVolumeSeries);
				underlyingChart.priceScale('volume').applyOptions({
					scaleMargins: { top: 0.8, bottom: 0.02 },
					borderVisible: false
				});
				underlyingVolumePlot = pvs;
			}
		}

		renderDataWindow(null);

    var ec = createChart('equity-chart', 300);
    if (ec) {
			var palette = currentThemePalette();
      var el = ec.addAreaSeries({
        lineColor: '#2dd4bf', topColor: 'rgba(45,212,191,0.28)',
        bottomColor: 'rgba(45,212,191,0.02)', lineWidth: 2
      });
			settledEquityPlot = ec.addLineSeries({
				color: palette.settledLine,
				lineWidth: 2,
				priceLineVisible: true,
				lastValueVisible: true,
				crosshairMarkerRadius: 5,
				crosshairMarkerBorderColor: palette.settledMarker,
				crosshairMarkerBackgroundColor: palette.settledMarkerBg
			});
      settledEquityPlot.setData([]);
      el.setData(equitySeries);
			equityChart = ec;
			equityPlot = el;
				addChart(ec);
    }

		if (quoteNetValueSeries.length > 0) {
		  var puc = createChart('quote-net-value-chart', 300);
      if (puc) {
			var palette = currentThemePalette();
			if (buyHoldSeries.length > 0) {
				puc.applyOptions({
					leftPriceScale: {
						visible: true,
						borderColor: palette.border
					},
					rightPriceScale: {
						visible: true,
						borderColor: palette.border
					}
				});
			}
        var pul = puc.addAreaSeries({
          lineColor: '#a78bfa', topColor: 'rgba(167,139,250,0.25)',
          bottomColor: 'rgba(167,139,250,0.02)', lineWidth: 2
        });
			pul.setData(quoteNetValueSeries);
			if (buyHoldSeries.length > 0) {
				quoteBuyHoldPlot = puc.addLineSeries({
					priceScaleId: 'left',
					color: '#60a5fa',
					lineWidth: 2,
					priceLineVisible: true,
					lastValueVisible: true,
					crosshairMarkerRadius: 4,
					crosshairMarkerBorderColor: '#60a5fa',
					crosshairMarkerBackgroundColor: '#60a5fa'
				});
				quoteBuyHoldPlot.setData(buyHoldSeries);
			}
        puc.timeScale().fitContent();
				quoteNetValueChart = puc;
				quoteNetValuePlot = pul;
				addChart(puc);
      }
    }

		if (dailyQuoteNetValueSeries.length > 0) {
		  var qdc = createChart('quote-daily-chart', 280);
		  if (qdc) {
			quoteDailyPlot = qdc.addAreaSeries({
			  lineColor: '#22d3ee', topColor: 'rgba(34,211,238,0.22)',
			  bottomColor: 'rgba(34,211,238,0.03)', lineWidth: 2
			});
			quoteDailyPlot.setData(dailyQuoteNetValueSeries);
			if (dailyBuyHoldSeries.length > 0) {
				quoteDailyBuyHoldPlot = qdc.addLineSeries({
					color: '#60a5fa',
					lineWidth: 2,
					priceLineVisible: true,
					lastValueVisible: true,
					crosshairMarkerRadius: 4,
					crosshairMarkerBorderColor: '#60a5fa',
					crosshairMarkerBackgroundColor: '#60a5fa'
				});
				quoteDailyBuyHoldPlot.setData(dailyBuyHoldSeries);
			}
			qdc.timeScale().fitContent();
			quoteDailyChart = qdc;
				addChart(qdc);
		  }
		}

		if (dailyAssetPnLSeries.length > 0) {
		  var apc = createChart('asset-daily-pnl-chart', 280, { handleScroll: false, handleScale: false });
		  if (apc) {
			assetDailyPnLPlot = apc.addLineSeries({
			  color: '#f59e0b',
			  lineWidth: 2,
			  priceLineVisible: true,
			  lastValueVisible: true,
			  crosshairMarkerRadius: 4,
			  crosshairMarkerBorderColor: '#f59e0b',
			  crosshairMarkerBackgroundColor: '#f59e0b'
			});
			assetDailyPnLPlot.setData(dailyAssetPnLSeries);
			assetDailyPnLChart = apc;
			addChart(apc, { sync: false });
			applyChartFullRange(assetDailyPnLChart, [dailyAssetPnLSeries]);
		  }
		}

    var dc = createChart('drawdown-chart', 220);
    if (dc) {
      var dl = dc.addAreaSeries({
        lineColor: '#fbbf24', topColor: 'rgba(251,191,36,0.25)',
        bottomColor: 'rgba(251,191,36,0.02)', lineWidth: 2
      });
      dl.setData(drawdownSeries);
      dc.timeScale().fitContent();
			drawdownChart = dc;
			drawdownPlot = dl;
			addChart(dc);
    }

		function applyIdleFilter(enabled) {
			var useFilter = enabled && hasActiveFilter;
			currentIdleFilterEnabled = useFilter;

			if (underlyingSeries) {
				underlyingSeries.setData(filterCandleSeriesByTimes(underlyingCandles, activeSet, useFilter));
				underlyingSeries.setMarkers(filterMarkerSeriesByTimes(underlyingMarkers, activeSet, useFilter));
			}
			if (underlyingVolumePlot) {
				underlyingVolumePlot.setData(filterHistogramSeriesByTimes(underlyingVolumeSeries, activeSet, useFilter));
			}
			renderOverlayPlots();
			renderEquitySeriesMode(settledModeEnabled, { refit: false });
			if (quoteNetValuePlot) {
				quoteNetValuePlot.setData(filterLineSeriesByTimes(quoteNetValueSeries, activeSet, useFilter));
			}
			if (quoteBuyHoldPlot) {
				quoteBuyHoldPlot.setData(filterLineSeriesByTimes(buyHoldSeries, activeSet, useFilter));
			}
			if (drawdownPlot) {
				drawdownPlot.setData(filterLineSeriesByTimes(drawdownSeries, activeSet, useFilter));
			}
			renderFeatureChart();
			renderDataWindow(currentDataWindowTime);
			fitChartsToVisibleData();
			applyChartFullRange(assetDailyPnLChart, [dailyAssetPnLSeries]);
		}

		if (syncedCharts.length > 1) syncCharts(syncedCharts);
		renderEquitySeriesMode(false, { refit: false });
		fitChartsToVisibleData();
		if (window.requestAnimationFrame) {
			window.requestAnimationFrame(function() {
				fitChartsToVisibleData();
			});
		}
		applyChartFullRange(assetDailyPnLChart, [dailyAssetPnLSeries]);

		if (dataWindowGrid) {
			dataWindowGrid.addEventListener('click', function(event) {
				var target = event.target;
				if (!target || typeof target.closest !== 'function') return;
				var featureButton = target.closest('[data-hover-source]');
				if (!featureButton) return;
				var source = featureButton.getAttribute('data-hover-source');
				if (selectedHoverColumnSources.has(source)) selectedHoverColumnSources.delete(source);
				else selectedHoverColumnSources.add(source);
				renderFeatureChart();
				renderDataWindow(currentDataWindowTime);
			});
		}

		if (featureClear) {
			featureClear.addEventListener('click', function() {
				selectedHoverColumnSources.clear();
				renderFeatureChart();
				renderDataWindow(currentDataWindowTime);
			});
		}

		var toggle = document.getElementById('toggle-ignore-idle');
		if (toggle) {
			toggle.disabled = !hasActiveFilter;
			if (!hasActiveFilter) {
				toggle.checked = false;
				toggle.title = '时间轴上未检测到活跃持仓或交易。';
			}
			toggle.addEventListener('change', function(e) {
				applyIdleFilter(e.target.checked);
			});
		}

		var spreadGroupButtons = document.querySelectorAll('[data-spread-groups-toggle]');
		if (spreadGroupButtons.length > 0) {
			spreadGroupButtons.forEach(function(button) {
				button.addEventListener('click', function() {
					var action = button.getAttribute('data-spread-groups-toggle');
					document.querySelectorAll('[data-spread-group]').forEach(function(group) {
						group.open = action === 'expand';
					});
				});
			});
		}

		document.addEventListener('click', function(event) {
			var target = event.target;
			if (!target || typeof target.closest !== 'function') return;
			var locateButton = target.closest('[data-chart-jump-time]');
			if (!locateButton) return;
			var jumpTime = parseInt(locateButton.getAttribute('data-chart-jump-time'), 10);
			var windowStart = parseInt(locateButton.getAttribute('data-chart-window-start'), 10);
			var windowEnd = parseInt(locateButton.getAttribute('data-chart-window-end'), 10);
			focusChartsAtEvent(
				isNaN(jumpTime) ? NaN : jumpTime,
				isNaN(windowStart) ? NaN : windowStart,
				isNaN(windowEnd) ? NaN : windowEnd
			);
		});

		// Initialize timezone selector
		var timezoneSelect = document.getElementById('timezone-select');
		if (timezoneSelect) {
			timezoneSelect.addEventListener('change', function(e) {
				applyTimeZoneMode(e.target.value);
			});
		}
		var themeSelect = document.getElementById('theme-select');
		if (themeSelect) {
			themeSelect.value = currentThemeName();
			themeSelect.addEventListener('change', function(e) {
				var nextTheme = e.target.value === 'dark' ? 'dark' : 'light';
				try {
					localStorage.setItem('toktik-report-theme', nextTheme);
				} catch (error) {
					console.warn('failed to persist report theme', error);
				}
				document.documentElement.setAttribute('data-theme', nextTheme);
				window.location.reload();
			});
		}
		applyTimeZoneMode(detectDefaultTimeZoneMode());

		// Initialize settled equity toggle
		var settledToggle = document.getElementById('toggle-settled-equity');
		if (settledToggle) {
			settledToggle.addEventListener('change', function(e) {
				renderEquitySeriesMode(e.target.checked);
			});
		}
  </script>
</body>
</html>`

const combinedHTMLTemplate = `{{ define "combinedSpreadEventCard" }}
<article id="{{ .AnchorID }}" class="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.03]">
	<div class="flex flex-col gap-3 border-b border-white/10 px-4 py-4 lg:flex-row lg:items-center lg:justify-between">
		<div>
			<div class="flex flex-wrap items-center gap-3">
				<span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .EventClass }}">{{ if eq .EventType "OPEN" }}开仓{{ else }}平仓{{ end }}</span>
				<h4 class="text-lg font-bold text-white">#{{ .ID }} {{ .Tag }}</h4>
				<span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .StatusClass }}">{{ .Status }}</span>
				{{ if .RelatedLink }}<a href="#{{ .RelatedLink }}" class="font-mono text-xs text-steel underline underline-offset-2 hover:text-white">{{ .RelatedText }}</a>{{ end }}
			</div>
			<p class="mt-2 text-sm text-slate-300">事件时间 {{ .EventTime }}{{ if .UnderlyingPrice }} · 标的 {{ .UnderlyingPrice }}{{ end }} · 开仓 {{ .OpenTime }} · 平仓 {{ .CloseTime }} · 持有 {{ .DaysHeld }}</p>
		</div>
		<div class="grid grid-cols-1 gap-4 text-sm lg:text-right">
			<div><div class="text-slate-400">已实现盈亏</div><div class="font-mono text-white">{{ .RealizedPnL }}</div></div>
		</div>
	</div>
	<div class="overflow-auto">
		<table class="min-w-full divide-y divide-white/10 text-sm">
			<thead class="bg-canvas/60 text-left text-slate-400">
				<tr>
					<th class="px-4 py-3 font-medium">合约</th>
					<th class="px-4 py-3 font-medium">方向</th>
					<th class="px-4 py-3 font-medium">类型</th>
					<th class="px-4 py-3 font-medium">行权价</th>
					<th class="px-4 py-3 font-medium">到期日</th>
					{{ if eq .EventType "OPEN" }}
					<th class="px-4 py-3 font-medium">筛选条件</th>
					<th class="px-4 py-3 font-medium">数量</th>
					<th class="px-4 py-3 font-medium">入场价格</th>
					<th class="px-4 py-3 font-medium">入场金额</th>
					{{ else }}
					<th class="px-4 py-3 font-medium">数量</th>
					<th class="px-4 py-3 font-medium">平仓价格</th>
					<th class="px-4 py-3 font-medium">{{ if .Legs }}{{ (index .Legs 0).CloseTimeLabel }}{{ else }}平仓时间{{ end }}</th>
					<th class="px-4 py-3 font-medium">平仓原因</th>
					<th class="px-4 py-3 font-medium">单腿盈亏</th>
					{{ end }}
				</tr>
			</thead>
			<tbody class="divide-y divide-white/5">
				{{ $eventType := .EventType }}
				{{ range .Legs }}
				<tr>
					<td class="px-4 py-3 font-mono text-slate-200">{{ .Symbol }}</td>
					<td class="px-4 py-3 font-semibold {{ .SideClass }}">{{ .Side }}</td>
					<td class="px-4 py-3 text-slate-200">{{ .Type }}</td>
					<td class="px-4 py-3 font-mono text-slate-200">{{ .StrikePrice }}</td>
					<td class="px-4 py-3 font-mono text-slate-300">{{ .Expiration }}</td>
					{{ if eq $eventType "OPEN" }}
					<td class="px-4 py-3 font-mono text-slate-300">{{ .OpenSelect }}</td>
					<td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
					<td class="px-4 py-3 font-mono text-slate-200">{{ .EntryPrice }}</td>
					<td class="px-4 py-3 font-mono text-slate-200">{{ .EntryAmount }}</td>
					{{ else }}
					<td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
					<td class="px-4 py-3 font-mono text-slate-300">{{ .ClosePrice }}</td>
					<td class="px-4 py-3 font-mono text-slate-300">{{ .CloseTime }}</td>
					<td class="px-4 py-3 text-slate-300">{{ .CloseReason }}</td>
					<td class="px-4 py-3 font-mono text-white">{{ .RealizedPnL }}</td>
					{{ end }}
				</tr>
				{{ end }}
			</tbody>
		</table>
	</div>
</article>
{{ end }}<!DOCTYPE html>
<html lang="zh-CN">
<head>
	<meta charset="UTF-8" />
	<meta name="viewport" content="width=device-width, initial-scale=1.0" />
	<title>{{ .Title }}</title>
	<script>
		tailwind = window.tailwind || {};
		tailwind.config = {
			theme: {
				extend: {
					colors: {
						canvas: '#071019',
						shell: '#0d1823',
						panel: '#122130',
						mist: '#dbe8e2',
						aqua: '#3dd6c6',
						ember: '#f0a43a',
						rose: '#fb7185',
						steel: '#7dd3fc'
					},
					fontFamily: {
						sans: ['Sora', 'system-ui', 'sans-serif'],
						mono: ['IBM Plex Mono', 'monospace']
					},
					boxShadow: {
						report: '0 30px 90px rgba(0,0,0,.34)'
					}
				}
			}
		}
	</script>
	<script src="https://cdn.tailwindcss.com"></script>
	<script src="https://unpkg.com/lightweight-charts@4.2.3/dist/lightweight-charts.standalone.production.js"></script>
	<link rel="preconnect" href="https://fonts.googleapis.com">
	<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
	<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500&family=Sora:wght@500;600;700;800&display=swap" rel="stylesheet">
	<style>
		:root {
			color-scheme: dark;
		}
		body {
			background:
				radial-gradient(circle at top left, rgba(61,214,198,.18), transparent 26%),
				radial-gradient(circle at top right, rgba(240,164,58,.14), transparent 22%),
				linear-gradient(180deg, #050d14 0%, #09131b 34%, #0d1823 100%);
		}
		.glass-panel {
			background: linear-gradient(180deg, rgba(255,255,255,.05) 0%, rgba(255,255,255,.02) 100%);
			backdrop-filter: blur(18px);
		}
		.chart-shell {
			background:
				linear-gradient(180deg, rgba(8,17,26,.94), rgba(8,17,26,.78)),
				radial-gradient(circle at top, rgba(61,214,198,.08), transparent 40%);
		}
		.chart-host {
			width: 100%;
			min-height: 18rem;
		}
		.chart-host.tall {
			min-height: 34rem;
		}
	</style>
</head>
<body class="min-h-screen font-sans text-mist">
	<main class="w-full px-3 py-4 sm:px-5 lg:px-8 lg:py-6">
		<section class="overflow-hidden rounded-[2rem] border border-white/10 bg-canvas/85 shadow-report">
			<div class="border-b border-white/10 px-5 py-7 sm:px-7 lg:px-10">
				<div class="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
					<div class="max-w-4xl">
						<p class="font-mono text-xs uppercase tracking-[0.34em] text-aqua/80">组合回测报告</p>
						<h1 class="mt-3 text-3xl font-extrabold tracking-tight text-white sm:text-4xl lg:text-5xl">{{ .Asset }} · {{ .Interval }}</h1>
						<p class="mt-4 max-w-3xl text-sm leading-7 text-slate-300 sm:text-base">{{ .Period }}。所有选中策略都会渲染到同一个 HTML 文件中，图表、成交记录和价差活动会按策略分组展示。</p>
					</div>
					<div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-2 xl:min-w-[24rem]">
						<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">生成时间</div>
							<div class="mt-2 text-sm text-white sm:text-base">{{ .GeneratedAt }}</div>
						</div>
						<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">策略数</div>
							<div class="mt-2 text-base text-white">{{ len .Strategies }}</div>
						</div>
					</div>
				</div>
				<div class="mt-6 flex flex-wrap gap-3">
					{{ range .Strategies }}
					<a href="#{{ .AnchorID }}" class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-200 transition hover:border-aqua/40 hover:text-white">{{ .Report.StrategyName }}</a>
					{{ end }}
				</div>
			</div>

			<div class="space-y-8 px-5 py-6 sm:px-7 lg:px-10 lg:py-8">
				{{ range .Strategies }}
				<section id="{{ .AnchorID }}" class="rounded-[1.75rem] border border-white/10 bg-panel/45 p-5 lg:p-6">
					<div class="flex flex-col gap-4 border-b border-white/10 pb-5 lg:flex-row lg:items-end lg:justify-between">
						<div>
							<p class="font-mono text-xs uppercase tracking-[0.28em] text-aqua/70">策略</p>
							<h2 class="mt-2 text-3xl font-bold text-white">{{ .Report.StrategyName }}</h2>
							<p class="mt-2 text-sm text-slate-300">{{ .Report.Asset }} · {{ .Report.Interval }} · {{ .Report.Period }}</p>
						</div>
						<div class="grid gap-3 text-sm text-slate-300 sm:grid-cols-3 xl:min-w-[32rem]">
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">最终权益</div>
								<div class="mt-2 text-base text-white">{{ .Report.FinalEquity }}</div>
							</div>
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">总收益率</div>
								<div class="mt-2 text-base text-white">{{ .Report.TotalReturn }}</div>
							</div>
							<div class="rounded-2xl border border-white/10 bg-white/5 px-4 py-3">
								<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">最大回撤</div>
								<div class="mt-2 text-base text-white">{{ .Report.MaxDrawdown }}</div>
							</div>
						</div>
					</div>

					<div class="mt-5 grid gap-4 sm:grid-cols-2 2xl:grid-cols-6">
						<article class="glass-panel rounded-3xl border border-white/10 p-5 2xl:col-span-2">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">净盈亏</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.NetPnL }}</div>
							<div class="mt-2 text-sm text-slate-300">初始资金 {{ .Report.InitialCapital }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">年化收益</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.AnnualizedReturn }}</div>
							<div class="mt-2 text-sm text-slate-300">夏普 {{ .Report.SharpeRatio }} · 卡玛 {{ .Report.CalmarRatio }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">Bars 数</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.BarsCount }}</div>
							<div class="mt-2 text-sm text-slate-300">手续费 {{ .Report.TotalFees }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">交易标记</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.TradeMarkerCount }}</div>
							<div class="mt-2 text-sm text-slate-300">价差事件 {{ .Report.SpreadEventCount }}</div>
						</article>
						<article class="glass-panel rounded-3xl border border-white/10 p-5">
							<div class="font-mono text-[11px] uppercase tracking-[0.24em] text-slate-400">活动</div>
							<div class="mt-3 text-3xl font-bold text-white">{{ .Report.TradesCount }}</div>
							<div class="mt-2 text-sm text-slate-300">价差持仓 {{ .Report.SpreadsCount }}</div>
						</article>
					</div>

					<div class="mt-6 space-y-6">
						<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
							<div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
								<div>
									<div class="flex flex-wrap items-center gap-3">
										<h3 class="text-2xl font-bold text-white">标的价格</h3>
										<span class="rounded-full border border-steel/30 bg-steel/10 px-3 py-1 font-mono text-[11px] uppercase tracking-[0.2em] text-steel">K 线 + 标记</span>
									</div>
										<p class="mt-2 text-sm text-slate-300">范围 {{ .Report.UnderlyingPriceMin }} 至 {{ .Report.UnderlyingPriceMax }}。买入和卖出成交会直接标注在价格 bar 上；如果有数据，价差开仓和平仓事件也会一并标注。{{ if .Report.HasUnderlyingVolume }}价格图下方会显示 {{ .Report.UnderlyingVolumeLabel }} 柱状图。{{ end }}</p>
								</div>
							</div>
							{{ if .Report.UnderlyingChartNote }}
							<div class="mt-4 rounded-2xl border border-amber-400/15 bg-amber-400/8 px-4 py-3 text-sm text-amber-100">{{ .Report.UnderlyingChartNote }}</div>
							{{ end }}
							<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
								{{ if .Report.HasUnderlyingChart }}
								<div id="{{ .AnchorID }}-underlying-chart" class="chart-host tall"></div>
								{{ if .Report.HasUnderlyingVolume }}
								<div class="mt-3 border-t border-white/10 pt-3">
									<div class="px-3 pb-1 font-mono text-[11px] uppercase tracking-[0.2em] text-slate-400">{{ .Report.UnderlyingVolumeLabel }}</div>
									<div id="{{ .AnchorID }}-underlying-volume-chart" class="chart-host"></div>
								</div>
								{{ end }}
								{{ else }}
								<div class="flex min-h-[20rem] items-center justify-center rounded-[1.25rem] border border-dashed border-white/10 bg-white/[0.03] px-6 text-center text-sm text-slate-300">回测结果中缺少标的 OHLC 数据，因此无法渲染蜡烛图。</div>
								{{ end }}
							</div>
						</section>

						<div class="space-y-6">
							<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
								<h3 class="text-2xl font-bold text-white">权益曲线</h3>
								<p class="mt-2 text-sm text-slate-300">组合权益路径范围为 {{ .Report.EquityMin }} 至 {{ .Report.EquityMax }}。</p>
								<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
									<div id="{{ .AnchorID }}-equity-chart" class="chart-host"></div>
								</div>
							</section>

							<section class="rounded-[1.75rem] border border-white/10 bg-panel/55 p-5 lg:p-6">
								<h3 class="text-2xl font-bold text-white">回撤轨迹</h3>
								<p class="mt-2 text-sm text-slate-300">从峰值到谷值的压力在最深处达到 {{ .Report.DrawdownMax }}。</p>
								<div class="chart-shell mt-5 overflow-hidden rounded-[1.5rem] border border-white/10 px-2 py-2 sm:px-3 sm:py-3">
									<div id="{{ .AnchorID }}-drawdown-chart" class="chart-host"></div>
								</div>
							</section>
						</div>

						{{ if .Report.Notes }}
						<div class="grid gap-3 xl:grid-cols-{{ if gt (len .Report.Notes) 1 }}2{{ else }}1{{ end }}">
							{{ range .Report.Notes }}
							<div class="rounded-2xl border border-white/10 bg-white/[0.04] px-4 py-3 text-sm text-slate-300">{{ . }}</div>
							{{ end }}
						</div>
						{{ end }}

						<div class="grid gap-6 xl:grid-cols-3">
							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">交易概览</h3>
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">原始成交</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.RawFills }}</dd></div>
									<div><dt class="text-slate-400">往返交易</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.RoundTrips }}</dd></div>
									<div><dt class="text-slate-400">多头成交</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.LongFills }}</dd></div>
									<div><dt class="text-slate-400">空头成交</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.ShortFills }}</dd></div>
									<div><dt class="text-slate-400">总名义金额</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.TotalNotional }}</dd></div>
									<div><dt class="text-slate-400">净盈亏</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.NetPnL }}</dd></div>
									<div><dt class="text-slate-400">毛利润</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.GrossProfit }}</dd></div>
									<div><dt class="text-slate-400">毛亏损</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.GrossLoss }}</dd></div>
									<div><dt class="text-slate-400">平均往返盈亏</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.AvgPnLPerRoundTrip }}</dd></div>
									<div><dt class="text-slate-400">平均手续费</dt><dd class="mt-1 font-mono text-white">{{ .Report.TradeOverview.AvgCommissionPerFill }}</dd></div>
								</dl>
							</section>

							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">权益分析</h3>
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">权益峰值</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PeakEquity }}</dd></div>
									<div><dt class="text-slate-400">最低权益</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.LowestEquity }}</dd></div>
									<div><dt class="text-slate-400">峰值时间</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PeakTime }}</dd></div>
									<div><dt class="text-slate-400">最低点时间</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.LowestTime }}</dd></div>
									<div><dt class="text-slate-400">最佳 Bar</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.BestBarReturn }}</dd></div>
									<div><dt class="text-slate-400">最差 Bar</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.WorstBarReturn }}</dd></div>
									<div><dt class="text-slate-400">波动率</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.BarReturnVolatility }}</dd></div>
									<div><dt class="text-slate-400">上涨 Bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.PositiveBars }}</dd></div>
									<div><dt class="text-slate-400">下跌 Bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.NegativeBars }}</dd></div>
									<div><dt class="text-slate-400">平盘 Bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.FlatBars }}</dd></div>
									<div><dt class="text-slate-400">回撤持续 Bars</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.MaxDrawdownDurationBars }}</dd></div>
									<div><dt class="text-slate-400">回撤时长</dt><dd class="mt-1 font-mono text-white">{{ .Report.EquityAnalysis.MaxDrawdownDuration }}</dd></div>
								</dl>
							</section>

							<section class="rounded-3xl border border-white/10 bg-panel/45 p-5">
								<h3 class="text-lg font-bold text-white">价差汇总</h3>
								{{ if .Report.SpreadSummary }}
								<dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
									<div><dt class="text-slate-400">价差总数</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.TotalSpreads }}</dd></div>
									<div><dt class="text-slate-400">已平仓</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.ClosedSpreads }}</dd></div>
									<div><dt class="text-slate-400">未平仓</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.OpenSpreads }}</dd></div>
									<div><dt class="text-slate-400">盈利</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.WinningSpreads }}</dd></div>
									<div><dt class="text-slate-400">亏损</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.LosingSpreads }}</dd></div>
									<div><dt class="text-slate-400">胜率</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.WinRate }}</dd></div>
									<div class="col-span-2"><dt class="text-slate-400">价差总盈亏</dt><dd class="mt-1 font-mono text-white">{{ .Report.SpreadSummary.TotalPnL }}</dd></div>
								</dl>
								{{ else }}
								<p class="mt-4 text-sm text-slate-300">本次运行未记录价差汇总。</p>
								{{ end }}
							</section>
						</div>

						<section class="rounded-[1.75rem] border border-white/10 bg-panel/40 p-5 lg:p-6">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
								<h3 class="text-2xl font-bold text-white">成交明细</h3>
								<span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .Report.TradesCount }} 笔成交</span>
							</div>
							{{ if .Report.NoTradeRows }}
							<div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">本次运行未记录原始经纪商成交。</div>
							{{ else }}
							<div class="mt-5 overflow-hidden rounded-2xl border border-white/10">
								<div class="max-h-[34rem] overflow-auto">
									<table class="min-w-full divide-y divide-white/10 text-sm">
										<thead class="sticky top-0 bg-canvas/95 backdrop-blur">
											<tr class="text-left text-slate-400">
												<th class="px-4 py-3 font-medium">时间</th>
												<th class="px-4 py-3 font-medium">标的</th>
												<th class="px-4 py-3 font-medium">方向</th>
												<th class="px-4 py-3 font-medium">原因</th>
												<th class="px-4 py-3 font-medium">数量</th>
												<th class="px-4 py-3 font-medium">成交价</th>
												<th class="px-4 py-3 font-medium">手续费</th>
												<th class="px-4 py-3 font-medium">滑点</th>
												<th class="px-4 py-3 font-medium">净额</th>
											</tr>
										</thead>
										<tbody class="divide-y divide-white/5 bg-white/[0.02]">
											{{ range .Report.Trades }}
											<tr>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Timestamp }}</td>
												<td class="px-4 py-3 text-slate-200">{{ .Security }}</td>
												<td class="px-4 py-3 font-semibold {{ .SideClass }}">{{ .Side }}</td>
												<td class="px-4 py-3 text-slate-300">{{ .Reason }}</td>
												<td class="px-4 py-3 font-mono text-slate-200">{{ .Qty }}</td>
												<td class="px-4 py-3 font-mono text-slate-200">{{ .FillPrice }}</td>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Commission }}</td>
												<td class="px-4 py-3 font-mono text-slate-300">{{ .Slippage }}</td>
												<td class="px-4 py-3 font-mono text-white">{{ .NetAmount }}</td>
											</tr>
											{{ end }}
										</tbody>
									</table>
								</div>
							</div>
							{{ end }}
						</section>

						<section class="rounded-[1.75rem] border border-white/10 bg-panel/40 p-5 lg:p-6">
							<div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
								<h3 class="text-2xl font-bold text-white">价差活动</h3>
								<span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ .Report.SpreadsCount }} 个持仓 · {{ len .Report.Spreads }} 个事件</span>
							</div>
							{{ if .Report.NoSpreadRows }}
							<div class="mt-5 rounded-2xl border border-dashed border-white/15 bg-white/5 px-4 py-6 text-sm text-slate-300">本次运行未记录价差持仓。</div>
							{{ else }}
							<p class="mt-3 text-sm text-slate-300">开仓和平仓会按时间顺序分别列出。可使用跳转链接在同一价差的两个事件之间切换。</p>
							{{ if .Report.TopDrawdownGroups }}
							<div class="mt-5 grid gap-3 xl:grid-cols-2">
								{{ range .Report.TopDrawdownGroups }}
								<a href="#{{ .AnchorID }}" class="block rounded-2xl border border-white/10 bg-white/[0.03] p-4 transition hover:border-white/15 hover:bg-white/[0.05]">
									<div class="flex items-start justify-between gap-3">
										<div>
											<div class="text-base font-bold text-white">组 #{{ .ID }} {{ .Tag }}</div>
											<div class="mt-2 inline-flex rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .StatusClass }}">{{ .Status }}</div>
										</div>
										<div class="text-right">
											<div class="text-[11px] tracking-[0.18em] text-slate-500">最大回撤</div>
											<div class="mt-1 font-mono text-lg text-rose-200">{{ .MaxDrawdown }}</div>
										</div>
									</div>
									<div class="mt-4 grid grid-cols-2 gap-3 text-sm">
										<div><div class="text-slate-400">最高权益</div><div class="mt-1 font-mono text-white">{{ .HighestEquity }}</div></div>
										<div><div class="text-slate-400">最低权益</div><div class="mt-1 font-mono text-white">{{ .LowestEquity }}</div></div>
										<div class="col-span-2"><div class="text-slate-400">组盈亏</div><div class="mt-1 font-mono text-white">{{ .TotalPnL }}</div></div>
									</div>
								</a>
								{{ end }}
							</div>
							{{ end }}
							<div class="mt-5 space-y-4">
								{{ if .Report.SpreadGroups }}
								{{ range .Report.SpreadGroups }}
								<section id="{{ .AnchorID }}" class="overflow-hidden rounded-2xl border border-white/10 bg-white/[0.03]">
									<div class="border-b border-white/10 px-4 py-4">
										<div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
											<div>
												<div class="flex flex-wrap items-center gap-3">
													<h4 class="text-lg font-bold text-white">组 #{{ .ID }} {{ .Tag }}</h4>
													<span class="rounded-full px-2.5 py-1 text-[11px] font-mono ring-1 {{ .StatusClass }}">{{ .Status }}</span>
												</div>
												<p class="mt-2 text-sm text-slate-300">开组 {{ .OpenTime }} · 闭组 {{ .CloseTime }} · {{ .SpreadCount }} 个持仓 · {{ .EventCount }} 个事件</p>
											</div>
											<div class="grid grid-cols-2 gap-4 text-sm lg:text-right">
												<div><div class="text-slate-400">初始资金</div><div class="font-mono text-white">{{ .InitAmount }}</div></div>
												<div><div class="text-slate-400">组盈亏</div><div class="font-mono text-white">{{ .TotalPnL }}</div></div>
												<div><div class="text-slate-400">Highest Equity</div><div class="font-mono text-white">{{ .HighestEquity }}</div></div>
												<div><div class="text-slate-400">Lowest Equity</div><div class="font-mono text-white">{{ .LowestEquity }}</div></div>
												<div><div class="text-slate-400">Max Drawdown</div><div class="font-mono text-rose-200">{{ .MaxDrawdown }}</div></div>
												<div><div class="text-slate-400">滚仓次数</div><div class="font-mono text-white">{{ .RollCount }}</div></div>
												<div><div class="text-slate-400">衰减系数</div><div class="font-mono text-white">{{ .DecayFactor }}</div></div>
											</div>
										</div>
									</div>
									<div class="space-y-4 p-4">
										{{ range .Spreads }}
										{{ template "combinedSpreadEventCard" . }}
										{{ end }}
									</div>
								</section>
								{{ end }}
								{{ end }}
								{{ if .Report.UngroupedSpreads }}
								<div>
									<div class="mb-3 flex items-center justify-between">
										<h4 class="text-base font-bold text-white">未分组持仓</h4>
										<span class="rounded-full border border-white/10 bg-white/5 px-3 py-1 font-mono text-xs text-slate-300">{{ len .Report.UngroupedSpreads }} 个事件</span>
									</div>
									<div class="space-y-4">
										{{ range .Report.UngroupedSpreads }}
										{{ template "combinedSpreadEventCard" . }}
										{{ end }}
									</div>
								</div>
								{{ end }}
							</div>
							{{ end }}
						</section>
					</div>
				</section>
				{{ end }}
			</div>
		</section>
	</main>

	<script>
		const strategyReports = [
			{{ range .Strategies }}
			{
				anchorId: {{ .AnchorID }},
				hasUnderlyingChart: {{ if .Report.HasUnderlyingChart }}true{{ else }}false{{ end }},
				underlyingCandles: {{ .Report.UnderlyingCandleData }},
				underlyingVolume: {{ .Report.UnderlyingVolumeData }},
				underlyingMarkers: {{ .Report.UnderlyingMarkerData }},
				equitySeries: {{ .Report.EquitySeriesData }},
				drawdownSeries: {{ .Report.DrawdownSeriesData }}
			},
			{{ end }}
		];

		const chartTheme = {
			layout: {
				background: { color: '#08111a' },
				textColor: '#b6c6d2',
				fontFamily: 'IBM Plex Mono, monospace'
			},
			grid: {
				vertLines: { color: 'rgba(255,255,255,0.05)' },
				horzLines: { color: 'rgba(255,255,255,0.05)' }
			},
			timeScale: {
				borderColor: 'rgba(255,255,255,0.08)',
				timeVisible: true,
				secondsVisible: false
			},
			rightPriceScale: {
				borderColor: 'rgba(255,255,255,0.08)'
			},
			crosshair: {
				vertLine: {
					color: 'rgba(61,214,198,0.35)',
					labelBackgroundColor: '#0f766e'
				},
				horzLine: {
					color: 'rgba(240,164,58,0.35)',
					labelBackgroundColor: '#a16207'
				}
			}
		};

		const utcMonthNames = ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月'];

		function padUTC(value) {
			return String(value).padStart(2, '0');
		}

		function normalizeUnixSeconds(timeValue) {
			if (typeof timeValue === 'number' && isFinite(timeValue)) {
				return timeValue;
			}
			if (timeValue && typeof timeValue.timestamp === 'number' && isFinite(timeValue.timestamp)) {
				return timeValue.timestamp;
			}
			if (
				timeValue &&
				typeof timeValue.year === 'number' &&
				typeof timeValue.month === 'number' &&
				typeof timeValue.day === 'number'
			) {
				return Math.floor(Date.UTC(timeValue.year, timeValue.month - 1, timeValue.day) / 1000);
			}
			return null;
		}

		function formatUTCDateTime(timeValue, includeSeconds) {
			const unixSeconds = normalizeUnixSeconds(timeValue);
			if (unixSeconds === null) {
				return '';
			}
			const date = new Date(unixSeconds * 1000);
			let formatted =
				date.getUTCFullYear() + '-' +
				padUTC(date.getUTCMonth() + 1) + '-' +
				padUTC(date.getUTCDate()) + ' ' +
				padUTC(date.getUTCHours()) + ':' +
				padUTC(date.getUTCMinutes());
			if (includeSeconds) {
				formatted += ':' + padUTC(date.getUTCSeconds());
			}
			return formatted + ' UTC';
		}

		function formatUTCTickLabel(timeValue) {
			const unixSeconds = normalizeUnixSeconds(timeValue);
			if (unixSeconds === null) {
				return '';
			}
			const date = new Date(unixSeconds * 1000);
			if (date.getUTCHours() === 0 && date.getUTCMinutes() === 0 && date.getUTCSeconds() === 0) {
				return utcMonthNames[date.getUTCMonth()] + padUTC(date.getUTCDate()) + '日';
			}
			return padUTC(date.getUTCHours()) + ':' + padUTC(date.getUTCMinutes());
		}

		function createResponsiveChart(containerId, height, extraOptions) {
			const container = document.getElementById(containerId);
			if (!container || typeof LightweightCharts === 'undefined') {
				return null;
			}
			const chart = LightweightCharts.createChart(
				container,
				Object.assign(
					{
						width: container.clientWidth,
						height: height,
						handleScroll: true,
						handleScale: true,
						localization: {
							locale: 'zh-CN',
							timeFormatter: function(timeValue) { return formatUTCDateTime(timeValue, true); },
							tickMarkFormatter: function(timeValue) { return formatUTCTickLabel(timeValue); }
						}
					},
					chartTheme,
					extraOptions || {}
				)
			);

			const resizeChart = function() {
				chart.applyOptions({ width: container.clientWidth });
			};

			if (typeof ResizeObserver !== 'undefined') {
				const observer = new ResizeObserver(function() {
					resizeChart();
				});
				observer.observe(container);
			} else {
				window.addEventListener('resize', resizeChart);
			}

			return chart;
		}

		function syncVisibleRanges(charts) {
			let syncing = false;
			charts.forEach(function(sourceChart) {
				sourceChart.timeScale().subscribeVisibleLogicalRangeChange(function(range) {
					if (!range || syncing) {
						return;
					}
					syncing = true;
					charts.forEach(function(targetChart) {
						if (targetChart !== sourceChart) {
							targetChart.timeScale().setVisibleLogicalRange(range);
						}
					});
					syncing = false;
				});
			});
		}

		strategyReports.forEach(function(report) {
			const charts = [];

			if (report.hasUnderlyingChart && report.underlyingCandles.length > 0) {
				const priceChart = createResponsiveChart(report.anchorId + '-underlying-chart', 560, {
					watermark: {
						visible: true,
						text: '标的',
						fontSize: 42,
						color: 'rgba(255,255,255,0.04)'
					}
				});
				if (priceChart) {
					const candleSeries = priceChart.addCandlestickSeries({
						upColor: '#22c55e',
						downColor: '#f97316',
						wickUpColor: '#22c55e',
						wickDownColor: '#f97316',
						borderUpColor: '#22c55e',
						borderDownColor: '#f97316',
						lastValueVisible: true,
						priceLineVisible: true
					});
					candleSeries.setData(report.underlyingCandles);
					candleSeries.setMarkers(report.underlyingMarkers);
					priceChart.timeScale().fitContent();
					charts.push(priceChart);
				}
			}

			if (report.underlyingVolume && report.underlyingVolume.length > 0) {
				const volumeChart = createResponsiveChart(report.anchorId + '-underlying-volume-chart', 180, {});
				if (volumeChart) {
					const volumeSeries = volumeChart.addHistogramSeries({
						priceFormat: { type: 'volume' },
						priceLineVisible: false,
						lastValueVisible: false,
						base: 0
					});
					volumeSeries.setData(report.underlyingVolume);
					volumeChart.timeScale().fitContent();
					charts.push(volumeChart);
				}
			}

			const equityChart = createResponsiveChart(report.anchorId + '-equity-chart', 320, {
				watermark: {
					visible: true,
						text: '权益',
					fontSize: 30,
					color: 'rgba(255,255,255,0.035)'
				}
			});
			if (equityChart) {
				const equityLine = equityChart.addAreaSeries({
					lineColor: '#3dd6c6',
					topColor: 'rgba(61,214,198,0.34)',
					bottomColor: 'rgba(61,214,198,0.04)',
					lineWidth: 3,
					priceLineVisible: true,
					lastValueVisible: true
				});
				equityLine.setData(report.equitySeries);
				equityChart.timeScale().fitContent();
				charts.push(equityChart);
			}

			const drawdownChart = createResponsiveChart(report.anchorId + '-drawdown-chart', 320, {
				watermark: {
					visible: true,
						text: '回撤',
					fontSize: 30,
					color: 'rgba(255,255,255,0.035)'
				}
			});
			if (drawdownChart) {
				const drawdownLine = drawdownChart.addAreaSeries({
					lineColor: '#f0a43a',
					topColor: 'rgba(240,164,58,0.32)',
					bottomColor: 'rgba(240,164,58,0.04)',
					lineWidth: 3,
					priceLineVisible: true,
					lastValueVisible: true
				});
				drawdownLine.setData(report.drawdownSeries);
				drawdownChart.timeScale().fitContent();
				charts.push(drawdownChart);
			}

			if (charts.length > 1) {
				syncVisibleRanges(charts);
			}
		});
	</script>
</body>
</html>`
