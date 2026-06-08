package calendarrepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EventType string

const (
	EventTypeEconomic            EventType = "economic"
	EventTypeEarnings            EventType = "earnings"
	EventTypeDividend            EventType = "dividend"
	EventTypeIPO                 EventType = "ipo"
	EventTypeSplit               EventType = "split"
	EventTypeFinancialReportDate EventType = "financial_report_date"
)

type CalendarEvent struct {
	ID               uint64     `gorm:"primaryKey"`
	EventType        string     `gorm:"size:40;not null;index:idx_calendar_identity,unique,priority:1;index:idx_calendar_query,priority:1"`
	Symbol           string     `gorm:"size:32;not null;default:'';index:idx_calendar_identity,unique,priority:2;index:idx_calendar_symbol_date,priority:1"`
	EventDate        string     `gorm:"size:32;not null;index:idx_calendar_identity,unique,priority:3"`
	EventAt          *time.Time `gorm:"index:idx_calendar_query,priority:2;index:idx_calendar_symbol_date,priority:2"`
	Title            string     `gorm:"size:255;not null;default:'';index:idx_calendar_identity,unique,priority:4"`
	Country          string     `gorm:"size:16"`
	Currency         string     `gorm:"size:16"`
	Impact           string     `gorm:"size:32"`
	Unit             string     `gorm:"size:32"`
	Previous         *float64
	Estimate         *float64
	Actual           *float64
	Change           *float64
	ChangePercentage *float64
	EPSActual        *float64
	EPSEstimated     *float64
	RevenueActual    *int64
	RevenueEstimated *int64
	Dividend         *float64
	AdjDividend      *float64
	Yield            *float64
	Frequency        string `gorm:"size:32"`
	RecordDate       string `gorm:"size:32"`
	PaymentDate      string `gorm:"size:32"`
	DeclarationDate  string `gorm:"size:32"`
	Company          string `gorm:"size:255"`
	Exchange         string `gorm:"size:64"`
	Action           string `gorm:"size:64"`
	Shares           *float64
	PriceRange       string `gorm:"size:64"`
	MarketCap        *float64
	Numerator        *float64
	Denominator      *float64
	SplitType        string `gorm:"size:64"`
	FiscalYear       *int
	Period           string `gorm:"size:16"`
	LinkJSON         string `gorm:"size:512"`
	LinkXLSX         string `gorm:"size:512"`
	RawJSON          string `gorm:"type:json"`
	Source           string `gorm:"size:32;not null;default:'fmp'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Repo struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) AutoMigrate(ctx context.Context) error {
	return r.db.WithContext(ctx).AutoMigrate(&CalendarEvent{})
}

func (r *Repo) UpsertEvents(ctx context.Context, events []CalendarEvent) error {
	if len(events) == 0 {
		return nil
	}
	// MySQL limits prepared statements to 65 535 placeholders. With ~43 columns per
	// row, a safe batch ceiling is 500 rows (500 × 43 = 21 500 placeholders).
	const batchSize = 500
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "event_type"},
			{Name: "symbol"},
			{Name: "event_date"},
			{Name: "title"},
		},
		UpdateAll: true,
	}).CreateInBatches(&events, batchSize).Error
}

func (r *Repo) ListEconomicEvents(ctx context.Context, from, to time.Time) ([]CalendarEvent, error) {
	var events []CalendarEvent
	err := r.db.WithContext(ctx).
		Where("event_type = ? AND event_at >= ? AND event_at <= ?", EventTypeEconomic, from, to).
		Order("event_at ASC").
		Find(&events).Error
	return events, err
}

func (r *Repo) ListStockEvents(ctx context.Context, symbols []string, from, to time.Time) ([]CalendarEvent, error) {
	return r.ListStockEventsByTypes(ctx, symbols, from, to, nil)
}

func (r *Repo) ListStockEventsByTypes(ctx context.Context, symbols []string, from, to time.Time, eventTypes []string) ([]CalendarEvent, error) {
	var events []CalendarEvent
	query := r.db.WithContext(ctx).
		Where("symbol IN ?", symbols).
		Where("event_at IS NULL OR (event_at >= ? AND event_at <= ?)", from, to)
	if len(eventTypes) > 0 {
		query = query.Where("event_type IN ?", eventTypes)
	}
	err := query.Order("event_at IS NULL ASC").
		Order("event_at ASC").
		Order("event_type ASC").
		Find(&events).Error
	return events, err
}
