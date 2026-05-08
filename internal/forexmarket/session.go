package forexmarket

import "time"

// ClassifyTimestamp assigns simple 24x5 forex session metadata.
// We use UTC calendar days as market_date/session_open so the stored schema
// stays aligned and deterministic across providers.
func ClassifyTimestamp(ts time.Time) (marketDate time.Time, sessionKind string, isRegular uint8, sessionOpen time.Time, sessionSeq uint16) {
	ts = ts.UTC()
	marketDate = time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
	sessionOpen = marketDate
	sessionSeq = uint16(ts.Hour()*60 + ts.Minute())

	switch ts.Weekday() {
	case time.Saturday:
		return marketDate, "closed", 0, sessionOpen, sessionSeq
	default:
		return marketDate, "regular", 1, sessionOpen, sessionSeq
	}
}
