package usmarket

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// SessionInfo describes a single trading day's session boundaries in UTC.
type SessionInfo struct {
	MarketDate         time.Time
	RegularOpenUTC     time.Time
	RegularCloseUTC    time.Time
	PremarketOpenUTC   time.Time
	PostmarketCloseUTC time.Time
	IsHoliday          bool
	IsEarlyClose       bool
}

// SessionMap maps "YYYY-MM-DD" → *SessionInfo for quick lookup.
type SessionMap map[string]*SessionInfo

// Lookup returns the session for the given calendar date (midnight UTC).
func (m SessionMap) Lookup(d time.Time) *SessionInfo {
	return m[d.Format("2006-01-02")]
}

// ClassifyTimestamp determines which session a UTC timestamp belongs to.
// Returns market_date, session_kind, is_regular_session, session_open (UTC), and session_seq.
func (m SessionMap) ClassifyTimestamp(ts time.Time) (marketDate time.Time, sessionKind string, isRegular uint8, sessionOpen time.Time, sessionSeq uint16) {
	nyTime := ts.In(newYorkLocation)
	calendarDate := time.Date(nyTime.Year(), nyTime.Month(), nyTime.Day(), 0, 0, 0, 0, time.UTC)

	session := m[calendarDate.Format("2006-01-02")]
	if session == nil {
		return calendarDate, "closed", 0, time.Time{}, 0
	}
	if session.IsHoliday {
		return calendarDate, "closed", 0, session.RegularOpenUTC, 0
	}

	switch {
	case ts.Before(session.PremarketOpenUTC):
		return calendarDate, "closed", 0, session.RegularOpenUTC, 0
	case ts.Before(session.RegularOpenUTC):
		return calendarDate, "premarket", 0, session.RegularOpenUTC, 0
	case ts.Before(session.RegularCloseUTC):
		seq := uint16(ts.Sub(session.RegularOpenUTC).Seconds() / 60)
		return calendarDate, "regular", 1, session.RegularOpenUTC, seq
	case ts.Before(session.PostmarketCloseUTC):
		return calendarDate, "postmarket", 0, session.RegularOpenUTC, 0
	default:
		return calendarDate, "closed", 0, session.RegularOpenUTC, 0
	}
}

// ---------------------------------------------------------------------------
// Calendar generation
// ---------------------------------------------------------------------------

// GenerateSessionCalendar builds session entries for every weekday in [startYear, endYear].
func GenerateSessionCalendar(startYear, endYear int) []*SessionInfo {
	var sessions []*SessionInfo
	for year := startYear; year <= endYear; year++ {
		holidays := usMarketHolidays(year)
		earlyDays := usMarketEarlyCloseDays(year, holidays)

		start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
		for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
			wd := d.Weekday()
			if wd == time.Saturday || wd == time.Sunday {
				continue
			}
			key := d.Format("2006-01-02")
			isHoliday := holidays[key]
			isEarly := earlyDays[key]

			regOpen := time.Date(d.Year(), d.Month(), d.Day(), 9, 30, 0, 0, newYorkLocation).UTC()
			preOpen := time.Date(d.Year(), d.Month(), d.Day(), 4, 0, 0, 0, newYorkLocation).UTC()
			postClose := time.Date(d.Year(), d.Month(), d.Day(), 20, 0, 0, 0, newYorkLocation).UTC()

			regClose := time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, newYorkLocation).UTC()
			if isEarly {
				regClose = time.Date(d.Year(), d.Month(), d.Day(), 13, 0, 0, 0, newYorkLocation).UTC()
			}

			sessions = append(sessions, &SessionInfo{
				MarketDate:         d,
				RegularOpenUTC:     regOpen,
				RegularCloseUTC:    regClose,
				PremarketOpenUTC:   preOpen,
				PostmarketCloseUTC: postClose,
				IsHoliday:          isHoliday,
				IsEarlyClose:       isEarly,
			})
		}
	}
	return sessions
}

// ---------------------------------------------------------------------------
// US market holiday logic
// ---------------------------------------------------------------------------

// usMarketHolidays returns observed holiday dates for a given year.
func usMarketHolidays(year int) map[string]bool {
	h := make(map[string]bool)
	add := func(d time.Time) { h[d.Format("2006-01-02")] = true }

	// New Year's Day
	add(observedDate(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)))
	// MLK Day — 3rd Monday of January
	add(nthWeekday(year, time.January, time.Monday, 3))
	// Presidents' Day — 3rd Monday of February
	add(nthWeekday(year, time.February, time.Monday, 3))
	// Good Friday
	add(goodFriday(year))
	// Memorial Day — last Monday of May
	add(lastWeekday(year, time.May, time.Monday))
	// Juneteenth (observed since 2022)
	if year >= 2022 {
		add(observedDate(time.Date(year, 6, 19, 0, 0, 0, 0, time.UTC)))
	}
	// Independence Day
	add(observedDate(time.Date(year, 7, 4, 0, 0, 0, 0, time.UTC)))
	// Labor Day — 1st Monday of September
	add(nthWeekday(year, time.September, time.Monday, 1))
	// Thanksgiving — 4th Thursday of November
	add(nthWeekday(year, time.November, time.Thursday, 4))
	// Christmas
	add(observedDate(time.Date(year, 12, 25, 0, 0, 0, 0, time.UTC)))

	// National Day of Mourning for President Jimmy Carter.
	// NYSE/Nasdaq and listed options markets were closed for the state funeral.
	if year == 2025 {
		add(time.Date(2025, 1, 9, 0, 0, 0, 0, time.UTC))
	}

	return h
}

// usMarketEarlyCloseDays returns dates that close at 13:00 ET.
func usMarketEarlyCloseDays(year int, holidays map[string]bool) map[string]bool {
	early := make(map[string]bool)
	tryAdd := func(d time.Time) {
		key := d.Format("2006-01-02")
		wd := d.Weekday()
		if wd >= time.Monday && wd <= time.Friday && !holidays[key] {
			early[key] = true
		}
	}

	// Day before Independence Day (observed)
	jul4 := observedDate(time.Date(year, 7, 4, 0, 0, 0, 0, time.UTC))
	tryAdd(jul4.AddDate(0, 0, -1))

	// Black Friday — day after Thanksgiving
	thanksgiving := nthWeekday(year, time.November, time.Thursday, 4)
	tryAdd(thanksgiving.AddDate(0, 0, 1))

	// Christmas Eve (observed)
	dec25 := observedDate(time.Date(year, 12, 25, 0, 0, 0, 0, time.UTC))
	tryAdd(dec25.AddDate(0, 0, -1))

	return early
}

// ---------------------------------------------------------------------------
// Date arithmetic helpers
// ---------------------------------------------------------------------------

func observedDate(d time.Time) time.Time {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1) // Friday
	case time.Sunday:
		return d.AddDate(0, 0, 1) // Monday
	default:
		return d
	}
}

func nthWeekday(year int, month time.Month, weekday time.Weekday, n int) time.Time {
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	offset := int(weekday - first.Weekday())
	if offset < 0 {
		offset += 7
	}
	day := 1 + offset + (n-1)*7
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func lastWeekday(year int, month time.Month, weekday time.Weekday) time.Time {
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC) // last day of month
	offset := int(last.Weekday() - weekday)
	if offset < 0 {
		offset += 7
	}
	return last.AddDate(0, 0, -offset)
}

// easterDate computes Easter Sunday using the Anonymous Gregorian algorithm.
func easterDate(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := ((h + l - 7*m + 114) % 31) + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func goodFriday(year int) time.Time {
	return easterDate(year).AddDate(0, 0, -2)
}

// ---------------------------------------------------------------------------
// ClickHouse operations for session calendar
// ---------------------------------------------------------------------------

const sessionCalendarDDL = `CREATE TABLE IF NOT EXISTS us_equity_sessions
(
    market_date          Date,
    regular_open_utc     DateTime('UTC'),
    regular_close_utc    DateTime('UTC'),
    premarket_open_utc   DateTime('UTC'),
    postmarket_close_utc DateTime('UTC'),
    is_holiday           UInt8 DEFAULT 0,
    is_early_close       UInt8 DEFAULT 0
)
ENGINE = ReplacingMergeTree()
ORDER BY market_date
SETTINGS index_granularity = 8192`

func InitSessionSchema(ctx context.Context, conn driver.Conn) error {
	return conn.Exec(ctx, sessionCalendarDDL)
}

// InitSessionCalendar generates sessions for [startYear, endYear] and inserts them.
func InitSessionCalendar(ctx context.Context, conn driver.Conn, startYear, endYear int) error {
	if err := InitSessionSchema(ctx, conn); err != nil {
		return fmt.Errorf("init session schema: %w", err)
	}
	sessions := GenerateSessionCalendar(startYear, endYear)
	if err := insertSessions(ctx, conn, sessions); err != nil {
		return fmt.Errorf("insert sessions: %w", err)
	}
	log.Printf("[session-calendar] upserted %d sessions for %d-%d", len(sessions), startYear, endYear)
	return nil
}

func insertSessions(ctx context.Context, conn driver.Conn, sessions []*SessionInfo) error {
	batch, err := conn.PrepareBatch(ctx, `INSERT INTO us_equity_sessions (
		market_date, regular_open_utc, regular_close_utc,
		premarket_open_utc, postmarket_close_utc,
		is_holiday, is_early_close
	)`)
	if err != nil {
		return fmt.Errorf("prepare session batch: %w", err)
	}
	for _, s := range sessions {
		var holiday, early uint8
		if s.IsHoliday {
			holiday = 1
		}
		if s.IsEarlyClose {
			early = 1
		}
		if err := batch.Append(
			s.MarketDate,
			s.RegularOpenUTC,
			s.RegularCloseUTC,
			s.PremarketOpenUTC,
			s.PostmarketCloseUTC,
			holiday,
			early,
		); err != nil {
			return fmt.Errorf("append session: %w", err)
		}
	}
	return batch.Send()
}

// LoadSessionMap reads the full session calendar from ClickHouse into memory.
func LoadSessionMap(ctx context.Context, conn driver.Conn) (SessionMap, error) {
	rows, err := conn.Query(ctx, `SELECT
		market_date, regular_open_utc, regular_close_utc,
		premarket_open_utc, postmarket_close_utc,
		is_holiday, is_early_close
	FROM us_equity_sessions FINAL`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	m := make(SessionMap)
	for rows.Next() {
		var (
			md                      time.Time
			regOpen, regClose       time.Time
			preOpen, postClose      time.Time
			isHoliday, isEarlyClose uint8
		)
		if err := rows.Scan(&md, &regOpen, &regClose, &preOpen, &postClose, &isHoliday, &isEarlyClose); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		s := &SessionInfo{
			MarketDate:         md,
			RegularOpenUTC:     regOpen,
			RegularCloseUTC:    regClose,
			PremarketOpenUTC:   preOpen,
			PostmarketCloseUTC: postClose,
			IsHoliday:          isHoliday == 1,
			IsEarlyClose:       isEarlyClose == 1,
		}
		m[md.Format("2006-01-02")] = s
	}
	return m, rows.Err()
}

// ---------------------------------------------------------------------------
// Bar enrichment — add session metadata to parsed bars
// ---------------------------------------------------------------------------

// EnrichStockBarsWithSession annotates each bar with session classification.
func EnrichStockBarsWithSession(bars <-chan StockBar1m, sessions SessionMap) <-chan StockBar1m {
	out := make(chan StockBar1m, 8192)
	go func() {
		defer close(out)
		for bar := range bars {
			bar.MarketDate, bar.SessionKind, bar.IsRegularSession,
				bar.SessionOpen, bar.SessionSeq = sessions.ClassifyTimestamp(bar.Timestamp)
			out <- bar
		}
	}()
	return out
}

// EnrichOptionBarsWithSession annotates each bar with session classification.
func EnrichOptionBarsWithSession(bars <-chan OptionBar1m, sessions SessionMap) <-chan OptionBar1m {
	out := make(chan OptionBar1m, 8192)
	go func() {
		defer close(out)
		for bar := range bars {
			bar.MarketDate, bar.SessionKind, bar.IsRegularSession,
				bar.SessionOpen, bar.SessionSeq = sessions.ClassifyTimestamp(bar.Timestamp)
			out <- bar
		}
	}()
	return out
}

// EnsureSessionColumns adds session columns to existing 1m tables via ALTER TABLE.
func EnsureSessionColumns(ctx context.Context, conn driver.Conn) error {
	tables := []string{"us_stocks_bar_1m", "us_options_bar_1m"}
	for _, table := range tables {
		stmts := []string{
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS market_date Date AFTER transactions`, table),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS session_kind Enum8('premarket' = 1, 'regular' = 2, 'postmarket' = 3, 'closed' = 4) DEFAULT 'closed' AFTER market_date`, table),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS is_regular_session UInt8 DEFAULT 0 AFTER session_kind`, table),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS session_open DateTime('UTC') AFTER is_regular_session`, table),
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS session_seq UInt16 DEFAULT 0 AFTER session_open`, table),
		}
		for _, stmt := range stmts {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("ensure session columns on %s: %w", table, err)
			}
		}
	}
	return nil
}
