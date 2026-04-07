package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/feeds"
)

const defaultFactorBarLimit = 1000
const maxFactorBarLimit = 10000

// FactorService exposes registered factor feeds for catalog and time-series queries.
type FactorService struct {
	store *feeds.Store
}

func NewFactorService(store *feeds.Store) *FactorService {
	return &FactorService{store: store}
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

	resp := &dto.FactorBarResponse{}
	if len(data) > limit {
		resp.Data = data[:limit]
		resp.NextCursor = encodeCursor(data[limit-1].Timestamp)
	} else {
		resp.Data = data
	}
	return resp, nil
}
