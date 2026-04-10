package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/dsl/parser"
	"github.com/Cyvadra/toktik/pkg/dsl/runtime"
)

const maxIndicatorBars = 200000

// IndicatorService evaluates DSL plot() expressions over historical bars.
type IndicatorService struct {
	cryptoOptions *CryptoOptionsService
	cryptoSpot    *CryptoSpotService
	usStocks      *USStocksService
	usOptions     *USOptionsService
}

func NewIndicatorService(conn driver.Conn) *IndicatorService {
	return &IndicatorService{
		cryptoOptions: NewCryptoOptionsService(conn),
		cryptoSpot:    NewCryptoSpotService(conn),
		usStocks:      NewUSStocksService(conn),
		usOptions:     NewUSOptionsService(conn),
	}
}

type indicatorBar struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Fields    map[string]float64
}

type indicatorSeriesResult struct {
	columns []runtime.PlotSpec
	series  map[string][]float64
}

func (s *IndicatorService) QueryIndicatorSeries(ctx context.Context, req dto.IndicatorSeriesRequest) (*dto.IndicatorSeriesResponse, error) {
	if err := validateIndicatorSeriesRequest(req); err != nil {
		return nil, err
	}
	_, _, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	bars, market, interval, err := s.loadBars(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return emptyIndicatorSeriesResponse(market, req.Symbol, interval), nil
	}
	if len(bars) > maxIndicatorBars {
		return nil, dto.NewValidationError("indicator request returned %d bars, exceeds max %d", len(bars), maxIndicatorBars)
	}

	params, stringParams, err := normalizeIndicatorParams(req.Params)
	if err != nil {
		return nil, err
	}
	source, err := buildIndicatorDSLSource(req)
	if err != nil {
		return nil, err
	}

	result, err := executeIndicatorDSL(source, bars, params, stringParams)
	if err != nil {
		return nil, err
	}
	if len(result.columns) == 0 {
		return nil, dto.NewValidationError("dsl produced no plot series; add plot(...) statements")
	}
	series, err := flattenIndicatorSeries(result, req.Precision)
	if err != nil {
		return nil, err
	}

	resp := &dto.IndicatorSeriesResponse{
		Market:     market,
		Symbol:     req.Symbol,
		Interval:   interval,
		Timestamps: make([]time.Time, len(bars)),
		Series:     series,
	}
	for i, bar := range bars {
		resp.Timestamps[i] = bar.Timestamp
	}

	return resp, nil
}

func emptyIndicatorSeriesResponse(market, symbol, interval string) *dto.IndicatorSeriesResponse {
	return &dto.IndicatorSeriesResponse{
		Market:     market,
		Symbol:     symbol,
		Interval:   interval,
		Timestamps: []time.Time{},
		Series:     map[string][]*float64{},
	}
}

func validateIndicatorSeriesRequest(req dto.IndicatorSeriesRequest) error {
	if req.Precision != nil && *req.Precision < 0 {
		return dto.NewValidationError("precision must be greater than or equal to 0")
	}
	if strings.TrimSpace(req.DSL) != "" {
		return nil
	}
	for _, indicator := range req.Indicators {
		if strings.TrimSpace(indicator) != "" {
			return nil
		}
	}
	return dto.NewValidationError("either dsl or indicators must be provided")
}

func buildIndicatorDSLSource(req dto.IndicatorSeriesRequest) (string, error) {
	parts := make([]string, 0, len(req.Indicators)+1)
	if dsl := strings.TrimSpace(req.DSL); dsl != "" {
		parts = append(parts, dsl)
	}
	for _, indicator := range req.Indicators {
		trimmed := strings.TrimSpace(indicator)
		if trimmed == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("plot(%s, title=\"%s\")", trimmed, escapeIndicatorTitle(trimmed)))
	}
	if len(parts) == 0 {
		return "", dto.NewValidationError("either dsl or indicators must be provided")
	}
	return strings.Join(parts, "\n"), nil
}

func escapeIndicatorTitle(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func flattenIndicatorSeries(result *indicatorSeriesResult, precision *int) (map[string][]*float64, error) {
	out := make(map[string][]*float64, len(result.columns))
	for _, column := range result.columns {
		key := indicatorSeriesKey(column)
		if _, exists := out[key]; exists {
			return nil, dto.NewValidationError("duplicate indicator output key %q", key)
		}
		out[key] = encodeIndicatorValues(result.series[column.Source], precision)
	}
	return out, nil
}

func indicatorSeriesKey(column runtime.PlotSpec) string {
	if strings.TrimSpace(column.Title) != "" {
		return column.Title
	}
	return column.Source
}

func (s *IndicatorService) loadBars(ctx context.Context, req dto.IndicatorSeriesRequest) ([]indicatorBar, string, string, error) {
	market := strings.ToLower(strings.TrimSpace(req.Market))
	interval := strings.TrimSpace(req.Interval)
	switch market {
	case "crypto-options":
		return s.loadCryptoOptionBars(ctx, req.Symbol, interval, req.From, req.To)
	case "crypto-spot":
		return s.loadCryptoSpotBars(ctx, req.Symbol, interval, req.From, req.To)
	case "us-stocks":
		return s.loadUSStockBars(ctx, req.Symbol, interval, req.From, req.To, req.Session)
	case "us-options":
		return s.loadUSOptionBars(ctx, req.Symbol, interval, req.From, req.To, req.Session)
	default:
		return nil, "", "", dto.NewValidationError("unsupported market %q", req.Market)
	}
}

func (s *IndicatorService) loadCryptoOptionBars(ctx context.Context, symbol, interval, from, to string) ([]indicatorBar, string, string, error) {
	resp, err := s.collectCryptoOptionBars(ctx, dto.BarRequest{Symbol: symbol, Interval: interval, From: from, To: to})
	if err != nil {
		return nil, "", "", err
	}
	bars := make([]indicatorBar, 0, len(resp))
	for _, row := range resp {
		bars = append(bars, indicatorBar{
			Timestamp: row.Timestamp,
			Open:      float64(row.MarkOpen),
			High:      float64(row.MarkHigh),
			Low:       float64(row.MarkLow),
			Close:     float64(row.MarkClose),
			Volume:    row.Volume,
			Fields: map[string]float64{
				"mark_open":              float64(row.MarkOpen),
				"mark_high":              float64(row.MarkHigh),
				"mark_low":               float64(row.MarkLow),
				"mark_close":             float64(row.MarkClose),
				"last_open":              float64(row.LastOpen),
				"last_high":              float64(row.LastHigh),
				"last_low":               float64(row.LastLow),
				"last_close":             float64(row.LastClose),
				"bid_open":               float64(row.BidOpen),
				"bid_high":               float64(row.BidHigh),
				"bid_low":                float64(row.BidLow),
				"bid_close":              float64(row.BidClose),
				"ask_open":               float64(row.AskOpen),
				"ask_high":               float64(row.AskHigh),
				"ask_low":                float64(row.AskLow),
				"ask_close":              float64(row.AskClose),
				"mark_iv_open":           float64(row.MarkIVOpen),
				"mark_iv_close":          float64(row.MarkIVClose),
				"bid_iv_open":            float64(row.BidIVOpen),
				"ask_iv_open":            float64(row.AskIVOpen),
				"delta":                  float64(row.Delta),
				"gamma":                  float64(row.Gamma),
				"vega":                   float64(row.Vega),
				"theta":                  float64(row.Theta),
				"rho":                    float64(row.Rho),
				"underlying_price_open":  float64(row.UnderlyingPriceOpen),
				"underlying_price_high":  float64(row.UnderlyingPriceHigh),
				"underlying_price_low":   float64(row.UnderlyingPriceLow),
				"underlying_price_close": float64(row.UnderlyingPriceClose),
				"open_interest":          float64(row.OpenInterest),
				"tick_count":             float64(row.TickCount),
			},
		})
	}
	return bars, "crypto-options", interval, nil
}

func (s *IndicatorService) loadCryptoSpotBars(ctx context.Context, symbol, interval, from, to string) ([]indicatorBar, string, string, error) {
	resp, err := s.collectCryptoSpotBars(ctx, dto.CryptoSpotBarRequest{Symbol: symbol, Interval: interval, From: from, To: to})
	if err != nil {
		return nil, "", "", err
	}
	bars := make([]indicatorBar, 0, len(resp))
	for _, row := range resp {
		bars = append(bars, indicatorBar{
			Timestamp: row.Timestamp,
			Open:      float64(row.Open),
			High:      float64(row.High),
			Low:       float64(row.Low),
			Close:     float64(row.Close),
			Volume:    row.Volume,
			Fields: map[string]float64{
				"tick_count": float64(row.TickCount),
			},
		})
	}
	return bars, "crypto-spot", interval, nil
}

func (s *IndicatorService) loadUSStockBars(ctx context.Context, symbol, interval, from, to, session string) ([]indicatorBar, string, string, error) {
	resp, err := s.collectUSStockBars(ctx, dto.USStockBarRequest{Symbol: symbol, Interval: interval, From: from, To: to, Session: session})
	if err != nil {
		return nil, "", "", err
	}
	bars := make([]indicatorBar, 0, len(resp))
	for _, row := range resp {
		bars = append(bars, indicatorBar{
			Timestamp: row.Timestamp,
			Open:      float64(row.Open),
			High:      float64(row.High),
			Low:       float64(row.Low),
			Close:     float64(row.Close),
			Volume:    row.Volume,
			Fields: map[string]float64{
				"transactions": float64(row.Transactions),
			},
		})
	}
	return bars, "us-stocks", interval, nil
}

func (s *IndicatorService) loadUSOptionBars(ctx context.Context, symbol, interval, from, to, session string) ([]indicatorBar, string, string, error) {
	resp, err := s.collectUSOptionBars(ctx, dto.USOptionBarRequest{Symbol: symbol, Interval: interval, From: from, To: to, Session: session})
	if err != nil {
		return nil, "", "", err
	}
	bars := make([]indicatorBar, 0, len(resp))
	for _, row := range resp {
		bars = append(bars, indicatorBar{
			Timestamp: row.Timestamp,
			Open:      float64(row.Open),
			High:      float64(row.High),
			Low:       float64(row.Low),
			Close:     float64(row.Close),
			Volume:    row.Volume,
			Fields: map[string]float64{
				"underlying_close":   float64(row.UnderlyingClose),
				"implied_volatility": float64(row.ImpliedVolatility),
				"delta":              float64(row.Delta),
				"gamma":              float64(row.Gamma),
				"vega":               float64(row.Vega),
				"theta":              float64(row.Theta),
				"rho":                float64(row.Rho),
				"transactions":       float64(row.Transactions),
			},
		})
	}
	return bars, "us-options", interval, nil
}

func (s *IndicatorService) collectCryptoOptionBars(ctx context.Context, req dto.BarRequest) ([]dto.BarRow, error) {
	current := req
	current.Limit = maxBarLimit
	out := make([]dto.BarRow, 0, maxBarLimit)
	for {
		resp, err := s.cryptoOptions.QueryBars(ctx, current)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Data...)
		if resp.NextCursor == "" {
			return out, nil
		}
		if len(out) > maxIndicatorBars {
			return nil, dto.NewValidationError("indicator request exceeded max bar count %d", maxIndicatorBars)
		}
		current.Cursor = resp.NextCursor
	}
}

func (s *IndicatorService) collectCryptoSpotBars(ctx context.Context, req dto.CryptoSpotBarRequest) ([]dto.CryptoSpotBarRow, error) {
	current := req
	current.Limit = maxBarLimit
	out := make([]dto.CryptoSpotBarRow, 0, maxBarLimit)
	for {
		resp, err := s.cryptoSpot.QueryBars(ctx, current)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Data...)
		if resp.NextCursor == "" {
			return out, nil
		}
		if len(out) > maxIndicatorBars {
			return nil, dto.NewValidationError("indicator request exceeded max bar count %d", maxIndicatorBars)
		}
		current.Cursor = resp.NextCursor
	}
}

func (s *IndicatorService) collectUSStockBars(ctx context.Context, req dto.USStockBarRequest) ([]dto.USStockBarRow, error) {
	current := req
	current.Limit = maxBarLimit
	out := make([]dto.USStockBarRow, 0, maxBarLimit)
	for {
		resp, err := s.usStocks.QueryBars(ctx, current)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Data...)
		if resp.NextCursor == "" {
			return out, nil
		}
		if len(out) > maxIndicatorBars {
			return nil, dto.NewValidationError("indicator request exceeded max bar count %d", maxIndicatorBars)
		}
		current.Cursor = resp.NextCursor
	}
}

func (s *IndicatorService) collectUSOptionBars(ctx context.Context, req dto.USOptionBarRequest) ([]dto.USOptionBarRow, error) {
	current := req
	current.Limit = maxBarLimit
	out := make([]dto.USOptionBarRow, 0, maxBarLimit)
	for {
		resp, err := s.usOptions.QueryBars(ctx, current)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Data...)
		if resp.NextCursor == "" {
			return out, nil
		}
		if len(out) > maxIndicatorBars {
			return nil, dto.NewValidationError("indicator request exceeded max bar count %d", maxIndicatorBars)
		}
		current.Cursor = resp.NextCursor
	}
}

func executeIndicatorDSL(source string, bars []indicatorBar, params map[string]float64, stringParams map[string]string) (*indicatorSeriesResult, error) {
	prog, errs := parser.Parse(source)
	if len(errs) > 0 {
		return nil, dto.NewValidationError("invalid dsl: %s", strings.Join(errs, "; "))
	}
	ip := runtime.NewInterpreter(prog)
	ip.Inputs = params
	ip.InputStrings = stringParams
	runtime.RegisterTABuiltins(ip)
	runtime.RegisterMathBuiltins(ip)
	runtime.RegisterStrBuiltins(ip)
	runtime.RegisterStrategyBuiltins(ip)
	runtime.RegisterInputBuiltins(ip)
	ip.Init()

	bridge := &indicatorBridge{bars: bars}
	ip.Bridge = bridge
	for idx, bar := range bars {
		bridge.index = idx
		for name, value := range bar.Fields {
			ip.SetNamedField(name, value)
		}
		ip.OnBar()
	}

	return &indicatorSeriesResult{columns: ip.PlotColumns(), series: ip.PlotSeries()}, nil
}

type indicatorBridge struct {
	bars  []indicatorBar
	index int
}

func (b *indicatorBridge) current() indicatorBar {
	if b.index < 0 || b.index >= len(b.bars) {
		return indicatorBar{}
	}
	return b.bars[b.index]
}

func (b *indicatorBridge) BarIndex() int              { return b.index }
func (b *indicatorBridge) Open() float64              { return b.current().Open }
func (b *indicatorBridge) High() float64              { return b.current().High }
func (b *indicatorBridge) Low() float64               { return b.current().Low }
func (b *indicatorBridge) Close() float64             { return b.current().Close }
func (b *indicatorBridge) Volume() float64            { return b.current().Volume }
func (b *indicatorBridge) Buy(float64)                {}
func (b *indicatorBridge) Sell(float64)               {}
func (b *indicatorBridge) EntryLong(string, float64)  {}
func (b *indicatorBridge) EntryShort(string, float64) {}
func (b *indicatorBridge) ExitLong(string)            {}
func (b *indicatorBridge) ExitShort(string)           {}
func (b *indicatorBridge) PositionSize() float64      { return 0 }
func (b *indicatorBridge) PositionAvgPrice() float64  { return 0 }
func (b *indicatorBridge) Equity() float64            { return 0 }
func (b *indicatorBridge) Cash() float64              { return 0 }
func (b *indicatorBridge) Ind(string) float64         { return math.NaN() }
func (b *indicatorBridge) IndAt(string, int) float64  { return math.NaN() }

func (b *indicatorBridge) Field(name string) float64 {
	return b.fieldAt(name, b.index)
}

func (b *indicatorBridge) FieldAt(name string, offset int) float64 {
	return b.fieldAt(name, b.index-offset)
}

func (b *indicatorBridge) fieldAt(name string, idx int) float64 {
	if idx < 0 || idx >= len(b.bars) {
		return math.NaN()
	}
	bar := b.bars[idx]
	switch name {
	case "open":
		return bar.Open
	case "high":
		return bar.High
	case "low":
		return bar.Low
	case "close":
		return bar.Close
	case "volume":
		return bar.Volume
	default:
		if value, ok := bar.Fields[name]; ok {
			return value
		}
		return math.NaN()
	}
}

func encodeIndicatorValues(values []float64, precision *int) []*float64 {
	out := make([]*float64, len(values))
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		v := roundIndicatorValue(value, precision)
		out[i] = &v
	}
	return out
}

func roundIndicatorValue(value float64, precision *int) float64 {
	if precision == nil {
		return value
	}
	factor := math.Pow10(*precision)
	rounded := math.Round(value*factor) / factor
	if rounded == 0 {
		return 0
	}
	return rounded
}

func normalizeIndicatorParams(params map[string]interface{}) (map[string]float64, map[string]string, error) {
	if len(params) == 0 {
		return nil, nil, nil
	}
	numeric := make(map[string]float64, len(params))
	stringsOut := make(map[string]string)
	for key, raw := range params {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		switch value := raw.(type) {
		case float64:
			numeric[trimmed] = value
		case float32:
			numeric[trimmed] = float64(value)
		case int:
			numeric[trimmed] = float64(value)
		case int32:
			numeric[trimmed] = float64(value)
		case int64:
			numeric[trimmed] = float64(value)
		case bool:
			if value {
				numeric[trimmed] = 1
			} else {
				numeric[trimmed] = 0
			}
		case string:
			stringsOut[trimmed] = value
		default:
			return nil, nil, dto.NewValidationError("unsupported param type for %q", trimmed)
		}
	}
	if len(numeric) == 0 {
		numeric = nil
	}
	if len(stringsOut) == 0 {
		stringsOut = nil
	}
	return numeric, stringsOut, nil
}
