package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/calendarrepo"
	"github.com/Cyvadra/toktik/internal/syncpipeline"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

type FMPStockEarningsCalendarBackfillConfig struct {
	APIKey            string
	FMPCacheDir       string
	MySQLDSN          string
	ChunkDays         int
	RepairFromUTC     time.Time
	RepairToUTC       time.Time
	ColdStartFloorUTC time.Time
}

type fmpStockEarningsCalendarBackfill struct {
	cfg FMPStockEarningsCalendarBackfillConfig
}

var newFMPClient = fmp.New

func NewFMPStockEarningsCalendarBackfill(cfg FMPStockEarningsCalendarBackfillConfig) (syncpipeline.Syncer, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("fmp_stock_earnings_calendar_backfill: APIKey is required")
	}
	if strings.TrimSpace(cfg.MySQLDSN) == "" {
		return nil, fmt.Errorf("fmp_stock_earnings_calendar_backfill: MySQLDSN is required")
	}
	if cfg.ChunkDays <= 0 {
		cfg.ChunkDays = 1
	}
	if cfg.ColdStartFloorUTC.IsZero() {
		cfg.ColdStartFloorUTC = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &fmpStockEarningsCalendarBackfill{cfg: cfg}, nil
}

func (s *fmpStockEarningsCalendarBackfill) Name() string {
	return "fmp_stock_earnings_calendar_backfill"
}

func (s *fmpStockEarningsCalendarBackfill) SourceKeys(context.Context, driver.Conn) ([]string, error) {
	if s.cfg.RepairFromUTC.IsZero() && s.cfg.RepairToUTC.IsZero() {
		return []string{syncpipeline.SingletonSourceKey}, nil
	}
	from, to := s.repairRange()
	chunks := calendarDateChunks(from, to, s.cfg.ChunkDays)
	keys := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		keys = append(keys, chunk.String())
	}
	return keys, nil
}

func (s *fmpStockEarningsCalendarBackfill) ResolveCursor(_ context.Context, _ driver.Conn, sourceKey string) (time.Time, bool, error) {
	if chunk, ok := parseCalendarDateChunkKey(sourceKey); ok {
		return chunk.from, true, nil
	}
	gormDB, closeFn, err := openFinanceCalendarDB(s.cfg.MySQLDSN)
	if err != nil {
		return time.Time{}, false, err
	}
	defer closeFn()
	var latest string
	err = gormDB.Model(&calendarrepo.CalendarEvent{}).
		Where("event_type = ? AND event_at IS NOT NULL", calendarrepo.EventTypeEarnings).
		Select("DATE_FORMAT(MAX(event_at), '%Y-%m-%d')").
		Scan(&latest).Error
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query latest earnings calendar date: %w", err)
	}
	if strings.TrimSpace(latest) == "" {
		return time.Time{}, false, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", latest, time.UTC)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parse latest earnings calendar date %q: %w", latest, err)
	}
	return parsed, true, nil
}

func (s *fmpStockEarningsCalendarBackfill) ColdStartFloor(string) time.Time {
	return s.cfg.ColdStartFloorUTC
}

func (s *fmpStockEarningsCalendarBackfill) Sync(ctx context.Context, _ driver.Conn, req syncpipeline.SyncRequest) (syncpipeline.SyncResult, error) {
	options := []fmp.Option{}
	if strings.TrimSpace(s.cfg.FMPCacheDir) != "" {
		options = append(options, fmp.WithCacheDir(strings.TrimSpace(s.cfg.FMPCacheDir)))
	}
	client := newFMPClient(s.cfg.APIKey, options...)
	from, to := req.From, req.To
	chunks := calendarDateChunks(from, to, s.cfg.ChunkDays)
	if parsed, ok := parseCalendarDateChunkKey(req.SourceKey); ok {
		chunks = []calendarDateChunk{parsed}
		from, to = parsed.from, parsed.to
	}
	resultNotes := func(fetchedEvents, validEvents int, capHit bool) []string {
		notes := earningsCalendarBackfillNotes(fetchedEvents, validEvents, capHit, req.DryRun)
		notes = append(notes, fmt.Sprintf("sync_window=%s..%s", formatCalendarDate(from), formatCalendarDate(to)), fmt.Sprintf("chunks=%d", len(chunks)))
		return notes
	}
	events := []calendarrepo.CalendarEvent{}
	fetchedEvents := 0
	capHit := false
	for _, chunk := range chunks {
		rows, err := client.EarningsCalendar(ctx, formatCalendarDate(chunk.from), formatCalendarDate(chunk.to))
		fetchedEvents += len(rows)
		if len(rows) >= 4000 {
			capHit = true
		}
		if err != nil {
			return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: from, To: to, Notes: resultNotes(fetchedEvents, len(events), capHit)}, fmt.Errorf("fetch FMP earnings calendar %s: %w", chunk.String(), err)
		}
		for _, row := range rows {
			event, ok := earningsCalendarEventToModel(row)
			if ok {
				events = append(events, event)
			}
		}
	}
	if req.DryRun || len(events) == 0 {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: from, To: to, Notes: resultNotes(fetchedEvents, len(events), capHit)}, nil
	}
	gormDB, closeFn, err := openFinanceCalendarDB(s.cfg.MySQLDSN)
	if err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: from, To: to, Notes: resultNotes(fetchedEvents, len(events), capHit)}, err
	}
	defer closeFn()
	repo := calendarrepo.New(gormDB)
	if err := repo.UpsertEvents(ctx, events); err != nil {
		return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: from, To: to, Notes: resultNotes(fetchedEvents, len(events), capHit)}, fmt.Errorf("store earnings calendar backfill: %w", err)
	}
	return syncpipeline.SyncResult{SourceKey: req.SourceKey, From: from, To: to, RowsInserted: int64(len(events)), Notes: resultNotes(fetchedEvents, len(events), capHit)}, nil
}

func (s *fmpStockEarningsCalendarBackfill) AuditTargets(string) []syncpipeline.AuditTarget {
	return nil
}

func (s *fmpStockEarningsCalendarBackfill) MaxConcurrency() int { return 1 }

func (s *fmpStockEarningsCalendarBackfill) repairRange() (time.Time, time.Time) {
	from := s.cfg.RepairFromUTC
	to := s.cfg.RepairToUTC
	if from.IsZero() {
		from = s.cfg.ColdStartFloorUTC
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	return dateOnlyUTC(from), dateOnlyUTC(to)
}

func earningsCalendarBackfillNotes(fetchedEvents, validEvents int, capHit bool, dryRun bool) []string {
	notes := []string{fmt.Sprintf("fetched_events=%d", fetchedEvents), fmt.Sprintf("valid_events=%d", validEvents)}
	if capHit {
		notes = append(notes, "possible_fmp_cap=true; reduce calendar_chunk_days and rerun this window")
	}
	if dryRun || validEvents == 0 {
		notes = append(notes, "dry-run or no rows; MySQL calendar_events not mutated")
	} else {
		notes = append(notes, "MySQL calendar_events upsert attempted; rows reports net-new inserts only when the storage layer can report them")
	}
	return notes
}

type calendarDateChunk struct {
	from time.Time
	to   time.Time
}

func (c calendarDateChunk) String() string {
	return formatCalendarDate(c.from) + ".." + formatCalendarDate(c.to)
}

func parseCalendarDateChunkKey(value string) (calendarDateChunk, bool) {
	parts := strings.Split(strings.TrimSpace(value), "..")
	if len(parts) != 2 {
		return calendarDateChunk{}, false
	}
	from, err := time.ParseInLocation("2006-01-02", parts[0], time.UTC)
	if err != nil {
		return calendarDateChunk{}, false
	}
	to, err := time.ParseInLocation("2006-01-02", parts[1], time.UTC)
	if err != nil || to.Before(from) {
		return calendarDateChunk{}, false
	}
	return calendarDateChunk{from: from, to: to}, true
}

func calendarDateChunks(from, to time.Time, chunkDays int) []calendarDateChunk {
	from = dateOnlyUTC(from)
	to = dateOnlyUTC(to)
	if chunkDays <= 0 {
		chunkDays = 365
	}
	chunks := []calendarDateChunk{}
	for cursor := from; !cursor.After(to); cursor = cursor.AddDate(0, 0, chunkDays) {
		chunkTo := cursor.AddDate(0, 0, chunkDays-1)
		if chunkTo.After(to) {
			chunkTo = to
		}
		chunks = append(chunks, calendarDateChunk{from: cursor, to: chunkTo})
	}
	return chunks
}

func earningsCalendarEventToModel(row fmp.EarningsCalendarEvent) (calendarrepo.CalendarEvent, bool) {
	symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
	date := strings.TrimSpace(row.Date)
	if symbol == "" || date == "" {
		return calendarrepo.CalendarEvent{}, false
	}
	eventAt, err := time.ParseInLocation("2006-01-02", date, time.UTC)
	if err != nil {
		return calendarrepo.CalendarEvent{}, false
	}
	return calendarrepo.CalendarEvent{
		EventType:        string(calendarrepo.EventTypeEarnings),
		Symbol:           symbol,
		EventDate:        date,
		EventAt:          &eventAt,
		Title:            symbol + " earnings",
		EPSActual:        row.EPSActual,
		EPSEstimated:     row.EPSEstimated,
		RevenueActual:    row.RevenueActual,
		RevenueEstimated: row.RevenueEstimated,
		RawJSON:          calendarRawJSON(row),
		Source:           "fmp",
	}, true
}

func calendarRawJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}
