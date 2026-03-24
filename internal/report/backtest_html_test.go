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
	if payload[0].Label != "Donchian Upper" || len(payload[0].Values) != 2 {
		t.Fatalf("unexpected first hover column payload: %#v", payload[0])
	}
	if payload[1].Label != "ATR" || payload[1].Values[1].Value != 860 {
		t.Fatalf("unexpected second hover column payload: %#v", payload[1])
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
	if view.UnderlyingVolumeLabel != "Volume" {
		t.Fatalf("view.UnderlyingVolumeLabel = %q, want %q", view.UnderlyingVolumeLabel, "Volume")
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
	if !strings.Contains(joined, "compatibility fallback market-data source") {
		t.Fatalf("expected compatibility fallback note, got %q", joined)
	}
	if !strings.Contains(joined, "No native volume series was available") {
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
	if strings.Contains(joined, "No native volume series was available") {
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
