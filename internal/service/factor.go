package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	usmacro "github.com/Cyvadra/toktik/internal/usmarket/macro"
	"github.com/Cyvadra/toktik/pkg/feeds"
)

const defaultFactorBarLimit = 1000
const maxFactorBarLimit = 10000

// FactorService exposes registered factor feeds for catalog and time-series queries.
type FactorService struct {
	store *feeds.Store
	macro *MacroService
}

func NewFactorService(store *feeds.Store) *FactorService {
	return &FactorService{store: store}
}

func (s *FactorService) WithMacroService(macro *MacroService) *FactorService {
	s.macro = macro
	return s
}

func (s *FactorService) ListFactors(_ context.Context) (*dto.FactorCatalogResponse, error) {
	all := feeds.All()
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)

	data := make([]dto.FactorInfo, 0, len(names))
	for _, name := range names {
		f := all[name]
		windows := make([]string, 0, len(f.SourceWindows()))
		for _, w := range f.SourceWindows() {
			windows = append(windows, w.Label)
		}
		data = append(data, dto.FactorInfo{
			Name:          f.Name(),
			Symbols:       f.Symbols(),
			SourceWindows: windows,
			Fields:        f.Fields(),
		})
	}

	return &dto.FactorCatalogResponse{Data: data}, nil
}

func (s *FactorService) QueryFactorBars(ctx context.Context, req dto.FactorBarRequest) (*dto.FactorBarResponse, error) {
	f := feeds.Get(req.Name)
	if f == nil {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("unknown factor feed %q", req.Name)}
	}

	// Validate window
	var window feeds.Window
	found := false
	for _, w := range f.SourceWindows() {
		if w.Label == req.Window {
			window = w
			found = true
			break
		}
	}
	if !found {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("unsupported window %q for feed %q", req.Window, req.Name)}
	}

	// Validate symbol
	symbolValid := false
	for _, sym := range f.Symbols() {
		if sym == req.Symbol {
			symbolValid = true
			break
		}
	}
	if !symbolValid {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("unsupported symbol %q for feed %q", req.Symbol, req.Name)}
	}

	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("invalid time range: %v", err)}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultFactorBarLimit
	}
	if limit > maxFactorBarLimit {
		limit = maxFactorBarLimit
	}

	// Apply cursor (timestamp-based)
	if req.Cursor != "" {
		cursorTime, cerr := decodeCursor(req.Cursor)
		if cerr != nil {
			return nil, invalidCursorError(cerr)
		}
		from = cursorTime.Add(time.Nanosecond)
	}

	if strings.EqualFold(req.Name, "dvol") && s.macro != nil {
		return s.queryDVOLMacroBars(ctx, req, from, to, limit)
	}

	bars, err := s.store.QueryBars(ctx, req.Name, window, req.Symbol, from, to)
	if err != nil {
		return nil, fmt.Errorf("query factor bars: %w", err)
	}

	data := make([]dto.FactorBarRow, 0, len(bars))
	for _, b := range bars {
		data = append(data, dto.FactorBarRow{
			Timestamp: b.Timestamp,
			Symbol:    b.Symbol,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
		})
	}

	resp := &dto.FactorBarResponse{Data: make([]dto.FactorBarRow, 0)}
	if len(data) > limit {
		resp.Data = data[:limit]
		resp.NextCursor = encodeCursor(data[limit-1].Timestamp)
	} else {
		resp.Data = data
	}
	return resp, nil
}

func (s *FactorService) queryDVOLMacroBars(ctx context.Context, req dto.FactorBarRequest, from, to time.Time, limit int) (*dto.FactorBarResponse, error) {
	dataset, ok := usmacro.DeribitDVOLDatasetForSymbol(req.Symbol)
	if !ok {
		return nil, &dto.ValidationError{Message: fmt.Sprintf("unsupported symbol %q for feed %q", req.Symbol, req.Name)}
	}
	resp, err := s.macro.QuerySeries(ctx, dto.MacroSeriesRequest{
		Dataset:  dataset,
		Factors:  []string{"open", "high", "low", "close"},
		From:     from.UTC().Format(time.RFC3339Nano),
		To:       to.UTC().Format(time.RFC3339Nano),
		AsOf:     to.UTC().Format(time.RFC3339Nano),
		Interval: req.Window,
		Limit:    maxMacroSeriesLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("query DVOL macro bars: %w", err)
	}

	data := macroSeriesFactorBars(req.Symbol, resp.Data)
	out := &dto.FactorBarResponse{Data: make([]dto.FactorBarRow, 0)}
	if len(data) > limit {
		out.Data = data[:limit]
		out.NextCursor = encodeCursor(data[limit-1].Timestamp)
	} else {
		out.Data = data
	}
	return out, nil
}

func macroSeriesFactorBars(symbol string, points []dto.MacroSeriesPoint) []dto.FactorBarRow {
	byTimestamp := make(map[time.Time]*dto.FactorBarRow)
	for _, point := range points {
		timestamp := point.Timestamp.UTC()
		if timestamp.IsZero() {
			timestamp = point.EventTS.UTC()
		}
		row := byTimestamp[timestamp]
		if row == nil {
			row = &dto.FactorBarRow{Timestamp: timestamp, Symbol: strings.ToUpper(strings.TrimSpace(symbol))}
			byTimestamp[timestamp] = row
		}
		switch point.Factor {
		case "open":
			row.Open = point.Value
		case "high":
			row.High = point.Value
		case "low":
			row.Low = point.Value
		case "close":
			row.Close = point.Value
		}
	}

	timestamps := make([]time.Time, 0, len(byTimestamp))
	for timestamp := range byTimestamp {
		timestamps = append(timestamps, timestamp)
	}
	sort.Slice(timestamps, func(i, j int) bool { return timestamps[i].Before(timestamps[j]) })
	out := make([]dto.FactorBarRow, 0, len(timestamps))
	for _, timestamp := range timestamps {
		out = append(out, *byTimestamp[timestamp])
	}
	return out
}
