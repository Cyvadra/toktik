package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/cache"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

const (
	financeCalendarSource         = "fmp"
	financeCalendarDateLayout     = "2006-01-02"
	financeCalendarDateTimeLayout = "2006-01-02 15:04:05"
	financeCalendarSyncCacheTTL   = 12 * time.Hour
)

type FinanceCalendarService struct {
	repo  *calendarrepo.Repo
	fmp   *fmp.Client
	cache cache.Store
	now   func() time.Time
}

func NewFinanceCalendarService(repo *calendarrepo.Repo, fmpClient *fmp.Client, cacheStore ...cache.Store) *FinanceCalendarService {
	svc := &FinanceCalendarService{repo: repo, fmp: fmpClient, now: time.Now}
	if len(cacheStore) > 0 {
		svc.cache = cacheStore[0]
	}
	return svc
}

func (s *FinanceCalendarService) QueryEconomicCalendar(ctx context.Context) (*dto.EconomicCalendarResponse, error) {
	if s == nil || s.repo == nil || s.fmp == nil {
		return nil, fmt.Errorf("finance calendar service not configured")
	}
	from, to := s.economicWindow()
	if err := s.ensureEconomicCalendarSynced(ctx, from, to); err != nil {
		return nil, err
	}
	events, err := s.repo.ListEconomicEvents(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("query economic calendar: %w", err)
	}
	return &dto.EconomicCalendarResponse{Data: calendarEventsToDTO(events)}, nil
}

func (s *FinanceCalendarService) QueryStockCalendar(ctx context.Context, req dto.StockCalendarRequest) (*dto.StockCalendarResponse, error) {
	if s == nil || s.repo == nil || s.fmp == nil {
		return nil, fmt.Errorf("finance calendar service not configured")
	}
	symbols := normalizeFinanceCalendarSymbols(req.Symbols)
	if len(symbols) == 0 {
		return nil, dto.NewValidationError("symbols must be non-empty")
	}
	from, to := s.stockWindow()
	if err := s.ensureStockCalendarSynced(ctx, from, to, symbols); err != nil {
		return nil, err
	}
	events, err := s.repo.ListStockEvents(ctx, symbols, from, to)
	if err != nil {
		return nil, fmt.Errorf("query stock calendar: %w", err)
	}
	return &dto.StockCalendarResponse{Symbols: symbols, Data: calendarEventsToDTO(events)}, nil
}

func (s *FinanceCalendarService) economicWindow() (time.Time, time.Time) {
	now := dateOnly(s.now())
	return now.AddDate(0, 0, -7), now.AddDate(0, 0, 30)
}

func (s *FinanceCalendarService) stockWindow() (time.Time, time.Time) {
	now := dateOnly(s.now())
	return now.AddDate(0, 0, -30), now.AddDate(0, 0, 90)
}

func (s *FinanceCalendarService) SyncEconomicCalendar(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.fmp == nil {
		return 0, fmt.Errorf("finance calendar service not configured")
	}
	from, to := s.economicWindow()
	cacheKey := financeCalendarEconomicSyncCacheKey(from, to)
	if fresh, _ := s.loadSyncMarker(ctx, cacheKey); fresh {
		return 0, nil
	}
	rows, err := s.syncEconomicCalendar(ctx, from, to)
	if err != nil {
		return 0, err
	}
	_ = s.storeSyncMarker(ctx, cacheKey)
	return rows, nil
}

func (s *FinanceCalendarService) SyncStockCalendar(ctx context.Context, symbols []string) (int, error) {
	if s == nil || s.repo == nil || s.fmp == nil {
		return 0, fmt.Errorf("finance calendar service not configured")
	}
	normalized := normalizeFinanceCalendarSymbols(symbols)
	if len(normalized) == 0 {
		return 0, dto.NewValidationError("symbols must be non-empty")
	}
	from, to := s.stockWindow()
	cacheKey := financeCalendarStockSyncCacheKey(normalized, from, to)
	if fresh, _ := s.loadSyncMarker(ctx, cacheKey); fresh {
		return 0, nil
	}
	rows, err := s.syncStockCalendar(ctx, from, to, normalized)
	if err != nil {
		return 0, err
	}
	_ = s.storeSyncMarker(ctx, cacheKey)
	return rows, nil
}

func (s *FinanceCalendarService) ensureEconomicCalendarSynced(ctx context.Context, from, to time.Time) error {
	cacheKey := financeCalendarEconomicSyncCacheKey(from, to)
	if fresh, _ := s.loadSyncMarker(ctx, cacheKey); fresh {
		return nil
	}
	if _, err := s.syncEconomicCalendar(ctx, from, to); err != nil {
		return err
	}
	_ = s.storeSyncMarker(ctx, cacheKey)
	return nil
}

func (s *FinanceCalendarService) ensureStockCalendarSynced(ctx context.Context, from, to time.Time, symbols []string) error {
	cacheKey := financeCalendarStockSyncCacheKey(symbols, from, to)
	if fresh, _ := s.loadSyncMarker(ctx, cacheKey); fresh {
		return nil
	}
	if _, err := s.syncStockCalendar(ctx, from, to, symbols); err != nil {
		return err
	}
	_ = s.storeSyncMarker(ctx, cacheKey)
	return nil
}

func (s *FinanceCalendarService) syncEconomicCalendar(ctx context.Context, from, to time.Time) (int, error) {
	rows, err := s.fmp.EconomicCalendar(ctx, formatDate(from), formatDate(to), "")
	if err != nil {
		return 0, fmt.Errorf("fetch FMP economic calendar: %w", err)
	}
	events := make([]calendarrepo.CalendarEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, economicEventToModel(row))
	}
	if err := s.repo.UpsertEvents(ctx, events); err != nil {
		return 0, fmt.Errorf("store economic calendar: %w", err)
	}
	return len(events), nil
}

func (s *FinanceCalendarService) syncStockCalendar(ctx context.Context, from, to time.Time, symbols []string) (int, error) {
	fromDate := formatDate(from)
	toDate := formatDate(to)
	allowed := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		allowed[symbol] = struct{}{}
	}
	events := []calendarrepo.CalendarEvent{}

	earnings, err := s.fmp.EarningsCalendar(ctx, fromDate, toDate)
	if err != nil {
		return 0, fmt.Errorf("fetch FMP earnings calendar: %w", err)
	}
	for _, row := range earnings {
		if _, ok := allowed[normalizeSymbol(row.Symbol)]; ok {
			events = append(events, earningsEventToModel(row))
		}
	}

	dividends, err := s.fmp.DividendsCalendar(ctx, fromDate, toDate)
	if err != nil {
		return 0, fmt.Errorf("fetch FMP dividends calendar: %w", err)
	}
	for _, row := range dividends {
		if _, ok := allowed[normalizeSymbol(row.Symbol)]; ok {
			events = append(events, dividendEventToModel(row))
		}
	}

	splits, err := s.fmp.SplitsCalendar(ctx, fromDate, toDate)
	if err != nil {
		return 0, fmt.Errorf("fetch FMP splits calendar: %w", err)
	}
	for _, row := range splits {
		if _, ok := allowed[normalizeSymbol(row.Symbol)]; ok {
			events = append(events, splitEventToModel(row))
		}
	}

	ipos, err := s.fmp.IPOsCalendar(ctx, fromDate, toDate)
	if err != nil {
		return 0, fmt.Errorf("fetch FMP ipos calendar: %w", err)
	}
	for _, row := range ipos {
		if _, ok := allowed[normalizeSymbol(row.Symbol)]; ok {
			events = append(events, ipoEventToModel(row))
		}
	}

	for _, symbol := range symbols {
		reportDates, err := s.fmp.FinancialReportDates(ctx, symbol)
		if err != nil {
			return 0, fmt.Errorf("fetch FMP financial report dates for %s: %w", symbol, err)
		}
		for _, row := range reportDates {
			events = append(events, financialReportDateToModel(row))
		}
	}

	if err := s.repo.UpsertEvents(ctx, events); err != nil {
		return 0, fmt.Errorf("store stock calendar: %w", err)
	}
	return len(events), nil
}

func (s *FinanceCalendarService) loadSyncMarker(ctx context.Context, key string) (bool, error) {
	if s == nil || s.cache == nil {
		return false, nil
	}
	_, ok, err := s.cache.Get(ctx, key)
	return ok, err
}

func (s *FinanceCalendarService) storeSyncMarker(ctx context.Context, key string) error {
	if s == nil || s.cache == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	payload := []byte(s.now().UTC().Format(time.RFC3339))
	return s.cache.Set(ctx, key, payload, financeCalendarSyncCacheTTL)
}

func financeCalendarEconomicSyncCacheKey(from, to time.Time) string {
	return fmt.Sprintf("finance-calendar:sync:v1:economic:from=%s:to=%s", formatDate(from), formatDate(to))
}

func financeCalendarStockSyncCacheKey(symbols []string, from, to time.Time) string {
	canonical := append([]string(nil), symbols...)
	sort.Strings(canonical)
	return fmt.Sprintf("finance-calendar:sync:v1:stocks:from=%s:to=%s:symbols=%s", formatDate(from), formatDate(to), strings.Join(canonical, ","))
}

func economicEventToModel(row fmp.EconomicCalendarEvent) calendarrepo.CalendarEvent {
	eventAt, eventDate := parseFMPDateTime(row.Date)
	return calendarrepo.CalendarEvent{
		EventType:        string(calendarrepo.EventTypeEconomic),
		EventDate:        eventDate,
		EventAt:          eventAt,
		Title:            strings.TrimSpace(row.Event),
		Country:          strings.TrimSpace(row.Country),
		Currency:         strings.TrimSpace(row.Currency),
		Impact:           strings.TrimSpace(row.Impact),
		Unit:             strings.TrimSpace(row.Unit),
		Previous:         row.Previous,
		Estimate:         row.Estimate,
		Actual:           row.Actual,
		Change:           row.Change,
		ChangePercentage: financeFloatPtr(row.ChangePercentage),
		RawJSON:          rawJSON(row),
		Source:           financeCalendarSource,
	}
}

func earningsEventToModel(row fmp.EarningsCalendarEvent) calendarrepo.CalendarEvent {
	eventAt, eventDate := parseFMPDate(row.Date)
	symbol := normalizeSymbol(row.Symbol)
	return calendarrepo.CalendarEvent{
		EventType:        string(calendarrepo.EventTypeEarnings),
		Symbol:           symbol,
		EventDate:        eventDate,
		EventAt:          eventAt,
		Title:            symbol + " earnings",
		EPSActual:        row.EPSActual,
		EPSEstimated:     row.EPSEstimated,
		RevenueActual:    row.RevenueActual,
		RevenueEstimated: row.RevenueEstimated,
		RawJSON:          rawJSON(row),
		Source:           financeCalendarSource,
	}
}

func dividendEventToModel(row fmp.DividendsCalendarEvent) calendarrepo.CalendarEvent {
	eventAt, eventDate := parseFMPDate(row.Date)
	symbol := normalizeSymbol(row.Symbol)
	adjDividend := row.AdjDividend
	dividend := row.Dividend
	yield := row.Yield
	return calendarrepo.CalendarEvent{
		EventType:       string(calendarrepo.EventTypeDividend),
		Symbol:          symbol,
		EventDate:       eventDate,
		EventAt:         eventAt,
		Title:           symbol + " dividend",
		AdjDividend:     &adjDividend,
		Dividend:        &dividend,
		Yield:           &yield,
		Frequency:       strings.TrimSpace(row.Frequency),
		RecordDate:      strings.TrimSpace(row.RecordDate),
		PaymentDate:     strings.TrimSpace(row.PaymentDate),
		DeclarationDate: strings.TrimSpace(row.DeclarationDate),
		RawJSON:         rawJSON(row),
		Source:          financeCalendarSource,
	}
}

func splitEventToModel(row fmp.SplitsCalendarEvent) calendarrepo.CalendarEvent {
	eventAt, eventDate := parseFMPDate(row.Date)
	symbol := normalizeSymbol(row.Symbol)
	numerator := row.Numerator
	denominator := row.Denominator
	return calendarrepo.CalendarEvent{
		EventType:   string(calendarrepo.EventTypeSplit),
		Symbol:      symbol,
		EventDate:   eventDate,
		EventAt:     eventAt,
		Title:       symbol + " split",
		Numerator:   &numerator,
		Denominator: &denominator,
		SplitType:   strings.TrimSpace(row.SplitType),
		RawJSON:     rawJSON(row),
		Source:      financeCalendarSource,
	}
}

func ipoEventToModel(row fmp.IPOCalendarEvent) calendarrepo.CalendarEvent {
	eventAt, eventDate := parseFMPDate(row.Date)
	symbol := normalizeSymbol(row.Symbol)
	return calendarrepo.CalendarEvent{
		EventType:  string(calendarrepo.EventTypeIPO),
		Symbol:     symbol,
		EventDate:  eventDate,
		EventAt:    eventAt,
		Title:      strings.TrimSpace(row.Company),
		Company:    strings.TrimSpace(row.Company),
		Exchange:   strings.TrimSpace(row.Exchange),
		Action:     strings.TrimSpace(row.Actions),
		Shares:     row.Shares,
		PriceRange: strings.TrimSpace(row.PriceRange),
		MarketCap:  row.MarketCap,
		RawJSON:    rawJSON(row),
		Source:     financeCalendarSource,
	}
}

func financialReportDateToModel(row fmp.FinancialReportDate) calendarrepo.CalendarEvent {
	symbol := normalizeSymbol(row.Symbol)
	fiscalYear := row.FiscalYear
	period := strings.TrimSpace(row.Period)
	eventDate := fmt.Sprintf("%d-%s", fiscalYear, period)
	return calendarrepo.CalendarEvent{
		EventType:  string(calendarrepo.EventTypeFinancialReportDate),
		Symbol:     symbol,
		EventDate:  eventDate,
		Title:      symbol + " financial report " + period,
		FiscalYear: &fiscalYear,
		Period:     period,
		LinkJSON:   strings.TrimSpace(row.LinkJSON),
		LinkXLSX:   strings.TrimSpace(row.LinkXLSX),
		RawJSON:    rawJSON(row),
		Source:     financeCalendarSource,
	}
}

func calendarEventsToDTO(events []calendarrepo.CalendarEvent) []dto.CalendarEventDTO {
	out := make([]dto.CalendarEventDTO, 0, len(events))
	for _, event := range events {
		item := dto.CalendarEventDTO{
			Type:             event.EventType,
			Symbol:           event.Symbol,
			Date:             event.EventDate,
			Title:            event.Title,
			Country:          event.Country,
			Currency:         event.Currency,
			Impact:           event.Impact,
			Unit:             event.Unit,
			Previous:         event.Previous,
			Estimate:         event.Estimate,
			Actual:           event.Actual,
			Change:           event.Change,
			ChangePercentage: event.ChangePercentage,
			EPSActual:        event.EPSActual,
			EPSEstimated:     event.EPSEstimated,
			RevenueActual:    event.RevenueActual,
			RevenueEstimated: event.RevenueEstimated,
			Dividend:         event.Dividend,
			AdjDividend:      event.AdjDividend,
			Yield:            event.Yield,
			Frequency:        event.Frequency,
			RecordDate:       event.RecordDate,
			PaymentDate:      event.PaymentDate,
			DeclarationDate:  event.DeclarationDate,
			Company:          event.Company,
			Exchange:         event.Exchange,
			Action:           event.Action,
			Shares:           event.Shares,
			PriceRange:       event.PriceRange,
			MarketCap:        event.MarketCap,
			Numerator:        event.Numerator,
			Denominator:      event.Denominator,
			SplitType:        event.SplitType,
			FiscalYear:       event.FiscalYear,
			Period:           event.Period,
			LinkJSON:         event.LinkJSON,
			LinkXLSX:         event.LinkXLSX,
		}
		if event.EventAt != nil {
			item.Time = event.EventAt.Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return out
}

func normalizeFinanceCalendarSymbols(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			symbol := normalizeSymbol(part)
			if symbol == "" {
				continue
			}
			if _, ok := seen[symbol]; ok {
				continue
			}
			seen[symbol] = struct{}{}
			out = append(out, symbol)
		}
	}
	return out
}

func normalizeSymbol(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func parseFMPDate(value string) (*time.Time, string) {
	date := strings.TrimSpace(value)
	if date == "" {
		return nil, ""
	}
	parsed, err := time.ParseInLocation(financeCalendarDateLayout, date, time.UTC)
	if err != nil {
		return nil, date
	}
	return &parsed, date
}

func parseFMPDateTime(value string) (*time.Time, string) {
	dateTime := strings.TrimSpace(value)
	if dateTime == "" {
		return nil, ""
	}
	parsed, err := time.ParseInLocation(financeCalendarDateTimeLayout, dateTime, time.UTC)
	if err != nil {
		return parseFMPDate(dateTime)
	}
	return &parsed, parsed.Format(financeCalendarDateLayout)
}

func rawJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}

func financeFloatPtr(value float64) *float64 {
	return &value
}

func dateOnly(value time.Time) time.Time {
	date := value.In(time.UTC)
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
}

func formatDate(value time.Time) string {
	return value.Format(financeCalendarDateLayout)
}
