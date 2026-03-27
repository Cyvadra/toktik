package report

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
)

func TestBuildHTMLViewIncludesHoverColumns(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":                {60000, 60100},
			"high":                {60200, 60300},
			"low":                 {59900, 60050},
			"close":               {60150, 60250},
			"htf_dc20_upper_prev": {62000, 62100},
			"htf_atr20_prev":      {850, 860},
		},
		ReportColumns: []backtest.ReportColumn{
			{Source: "htf_dc20_upper_prev", Label: "Donchian Upper", Decimals: 2},
			{Source: "htf_atr20_prev", Label: "ATR", Decimals: 2},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasHoverColumns {
		t.Fatal("view.HasHoverColumns = false, want true")
	}

	var payload []hoverColumnPayload
	if err := json.Unmarshal([]byte(view.HoverColumnsData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(HoverColumnsData) error = %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("len(payload) = %d, want 2", len(payload))
	}
	if payload[0].Overlay {
		t.Fatalf("payload[0].Overlay = true, want false")
	}
	if payload[0].Label != "Donchian Upper" || len(payload[0].Values) != 2 {
		t.Fatalf("unexpected first hover column payload: %#v", payload[0])
	}
	if payload[1].Label != "ATR" || payload[1].Values[1].Value != 860 {
		t.Fatalf("unexpected second hover column payload: %#v", payload[1])
	}
}

func TestBuildHTMLViewIncludesOverlayColumns(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":     {60000, 60100},
			"high":     {60200, 60300},
			"low":      {59900, 60050},
			"close":    {60150, 60250},
			"ema_fast": {60100, 60200},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "ema_fast",
			Label:    "EMA 20",
			Decimals: 2,
			Overlay:  true,
		}},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasHoverColumns {
		t.Fatal("view.HasHoverColumns = false, want true")
	}
	if view.HasFeatureColumns {
		t.Fatal("view.HasFeatureColumns = true, want false")
	}

	var payload []hoverColumnPayload
	if err := json.Unmarshal([]byte(view.HoverColumnsData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(HoverColumnsData) error = %v", err)
	}
	if len(payload) != 1 || !payload[0].Overlay {
		t.Fatalf("unexpected overlay payload: %#v", payload)
	}
}

func TestBuildHTMLViewIncludesUnderlyingVolumeHistogram(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":   {60000, 60200},
			"high":   {60300, 60400},
			"low":    {59900, 60100},
			"close":  {60200, 60150},
			"volume": {1234, 4567},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if !view.HasUnderlyingVolume {
		t.Fatal("view.HasUnderlyingVolume = false, want true")
	}
	if view.UnderlyingVolumeLabel != "成交量" {
		t.Fatalf("view.UnderlyingVolumeLabel = %q, want %q", view.UnderlyingVolumeLabel, "成交量")
	}

	var payload []chartHistogramPoint
	if err := json.Unmarshal([]byte(view.UnderlyingVolumeData), &payload); err != nil {
		t.Fatalf("json.Unmarshal(UnderlyingVolumeData) error = %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("len(payload) = %d, want 2", len(payload))
	}
	if payload[0].Value != 1234 || payload[0].Color != "rgba(34,197,94,0.52)" {
		t.Fatalf("unexpected first volume bar payload: %#v", payload[0])
	}
	if payload[1].Value != 4567 || payload[1].Color != "rgba(249,115,22,0.52)" {
		t.Fatalf("unexpected second volume bar payload: %#v", payload[1])
	}
}

func TestBuildHTMLViewNotesCompatibilityFallbackAndMissingVolume(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":            {60000, 60100},
			"high":            {60200, 60300},
			"low":             {59900, 60050},
			"close":           {60150, 60250},
			"compat_fallback": {1, 1},
			"volume":          {math.NaN(), math.NaN()},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	if len(view.Notes) < 2 {
		t.Fatalf("len(view.Notes) = %d, want at least 2", len(view.Notes))
	}

	joined := strings.Join(view.Notes, "\n")
	if !strings.Contains(joined, "兼容性回退市场数据源") {
		t.Fatalf("expected compatibility fallback note, got %q", joined)
	}
	if !strings.Contains(joined, "没有可用的原生成交量序列") {
		t.Fatalf("expected missing volume note, got %q", joined)
	}
}

func TestBuildHTMLViewSkipsMissingVolumeNoteWhenVolumeExists(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":   {60000, 60100},
			"high":   {60200, 60300},
			"low":    {59900, 60050},
			"close":  {60150, 60250},
			"volume": {123, 456},
		},
	}

	view := buildHTMLView(result, HTMLMeta{})
	joined := strings.Join(view.Notes, "\n")
	if strings.Contains(joined, "没有可用的原生成交量序列") {
		t.Fatalf("did not expect missing volume note, got %q", joined)
	}
}

func TestWriteBacktestHTMLUsesUTCChartFormatting(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.March, 31, 12, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.March, 31, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.March, 31, 12, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "formatUTCDateTime") {
		t.Fatalf("expected generated html to include UTC time formatter")
	}
	if !strings.Contains(html, "return formatted + ' UTC';") {
		t.Fatalf("expected generated html to format chart timestamps in UTC")
	}
	if !strings.Contains(html, "tickMarkFormatter: function(timeValue) { return formatUTCTickLabel(timeValue); }") {
		t.Fatalf("expected generated html to override tick mark formatting")
	}
	if !strings.Contains(html, " UTC") {
		t.Fatalf("expected GeneratedAt or other timestamps to include UTC label in html")
	}
}

func TestWriteBacktestHTMLIncludesHoverColumnSubplotControls(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":            {60000, 60100},
			"high":            {60200, 60300},
			"low":             {59900, 60050},
			"close":           {60150, 60250},
			"signal_strength": {0.25, 0.75},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "signal_strength",
			Label:    "Signal Strength",
			Decimals: 2,
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "underlying-feature-panel") {
		t.Fatalf("expected generated html to include hover column subplot panel")
	}
	if !strings.Contains(html, "data-hover-source") {
		t.Fatalf("expected generated html to include clickable hover column cards")
	}
	if !strings.Contains(html, "selectedHoverColumnSources") {
		t.Fatalf("expected generated html to include multi-select hover column subplot state")
	}
	if !strings.Contains(html, "preserveVisibleRanges") {
		t.Fatalf("expected generated html to preserve x-axis range during hover column updates")
	}
	if !strings.Contains(html, "priceScaleId: 'volume'") {
		t.Fatalf("expected generated html to merge volume histogram into the underlying chart")
	}
	if !strings.Contains(html, "feature-legend-value") {
		t.Fatalf("expected generated html to include live feature legend values")
	}
	if !strings.Contains(html, "featureChart.subscribeCrosshairMove") {
		t.Fatalf("expected generated html to sync subplot hover with the shared data window")
	}
}

func TestWriteBacktestHTMLIncludesOverlaySeriesSupport(t *testing.T) {
	result := &backtest.Result{
		StrategyName:   "test",
		StartTime:      time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		EndTime:        time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    101,
		EquityCurve:    []float64{100, 101},
		Timestamps: []time.Time{
			time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, time.January, 1, 1, 0, 0, 0, time.UTC),
		},
		Series: map[string][]float64{
			"open":     {60000, 60100},
			"high":     {60200, 60300},
			"low":      {59900, 60050},
			"close":    {60150, 60250},
			"ema_fast": {60100, 60200},
		},
		ReportColumns: []backtest.ReportColumn{{
			Source:   "ema_fast",
			Label:    "EMA 20",
			Decimals: 2,
			Overlay:  true,
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "var overlayPlots = new Map();") {
		t.Fatalf("expected generated html to include overlay plot state")
	}
	if !strings.Contains(html, "function renderOverlayPlots()") {
		t.Fatalf("expected generated html to include overlay rendering function")
	}
	if !strings.Contains(html, "column.overlay === true") {
		t.Fatalf("expected generated html to recognize overlay columns in payload")
	}
	if !strings.Contains(html, "叠加</div>") {
		t.Fatalf("expected generated html to label overlay cards in the data window")
	}
	if strings.Contains(html, "<div id=\"underlying-feature-panel\"") {
		t.Fatalf("did not expect subplot panel when all report columns are overlays")
	}
}

func TestWriteBacktestHTMLPlacesSpreadOpenTimeBesideHeaderStatus(t *testing.T) {
	openTime := time.Date(2024, time.January, 2, 3, 4, 0, 0, time.UTC)
	closeTime := time.Date(2024, time.January, 3, 5, 6, 0, 0, time.UTC)
	result := &backtest.Result{
		StrategyName:   "spread-test",
		StartTime:      openTime,
		EndTime:        closeTime,
		BarsCount:      2,
		InitialCapital: 100,
		FinalEquity:    102,
		EquityCurve:    []float64{100, 102},
		Timestamps:     []time.Time{openTime, closeTime},
		Series: map[string][]float64{
			"open":  {70000, 70100},
			"high":  {70200, 70300},
			"low":   {69900, 70050},
			"close": {70150, 70250},
		},
		SpreadPositions: []backtest.SpreadPositionReport{{
			ID:          1,
			Tag:         "short call spread",
			Status:      "closed",
			OpenTime:    openTime,
			CloseTime:   &closeTime,
			DaysHeld:    1.08,
			RealizedPnL: 12.34,
			Legs: []backtest.SpreadLegReport{{
				Symbol:      "BTC-20240105-50000-C",
				Side:        "sell",
				Type:        backtest.Call,
				StrikePrice: 50000,
				Expiration:  time.Date(2024, time.January, 5, 0, 0, 0, 0, time.UTC),
				Delta:       0.25,
				Qty:         1,
				EntryPrice:  10,
				EntryTime:   openTime,
				Closed:      true,
				ClosePrice:  4,
				CloseTime:   &closeTime,
				CloseReason: "tp",
				RealizedPnL: 6,
			}},
		}},
	}

	outputPath := filepath.Join(t.TempDir(), "report.html")
	if err := WriteBacktestHTML(outputPath, result, HTMLMeta{}); err != nil {
		t.Fatalf("WriteBacktestHTML() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	html := string(htmlBytes)

	if !strings.Contains(html, "下单 2024-01-02 03:04 UTC") {
		t.Fatalf("expected generated html to place spread open time in the card header")
	}
	if !strings.Contains(html, ">平仓 2024-01-03 05:06 UTC<") {
		t.Fatalf("expected generated html to keep the close event time in the header meta area")
	}
}
