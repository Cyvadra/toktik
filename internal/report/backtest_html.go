package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
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

// WriteBacktestHTML renders a static HTML report for a backtest result.
// Result data is embedded; chart and style assets are loaded from CDNs.
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

//go:embed templates/backtest.html
var htmlTemplate string
