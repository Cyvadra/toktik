package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

// USStocksService provides low-level US stock market-data queries.
type USStocksService struct {
	repo         *chrepo.Repo
	fundamentals usStockFundamentalsQuerier
	companyInfo  usStockCompanyProfileProvider
}

type usStockFundamentalsQuerier interface {
	QuerySeries(ctx context.Context, req dto.FundamentalSeriesRequest) (*dto.FundamentalSeriesResponse, error)
	QuerySnapshot(ctx context.Context, req dto.FundamentalSnapshotRequest) (*dto.FundamentalSnapshotResponse, error)
}

func NewUSStocksService(repo *chrepo.Repo, fundamentals ...usStockFundamentalsQuerier) *USStocksService {
	svc := &USStocksService{repo: repo}
	if len(fundamentals) > 0 {
		svc.fundamentals = fundamentals[0]
	}
	return svc
}

func (s *USStocksService) WithCompanyProfileProvider(provider usStockCompanyProfileProvider) *USStocksService {
	if s == nil {
		return nil
	}
	s.companyInfo = provider
	return s
}

func (s *USStocksService) QuerySymbols(ctx context.Context, req dto.USStockSymbolRequest) (*dto.USStockSymbolResponse, error) {
	limit := clamp(req.Limit, defaultSymbolLimit, maxSymbolLimit)
	query := `SELECT symbol
FROM us_stocks_bar_1m
WHERE 1 = 1`
	if req.Search != "" {
		query += fmt.Sprintf(` AND symbol ILIKE %s`, clickhouseStringLiteral("%"+req.Search+"%"))
	}
	if req.Cursor != "" {
		cursorSymbol, err := decodeCursorString(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		query += fmt.Sprintf(` AND symbol > %s`, clickhouseStringLiteral(cursorSymbol))
	}

	query += fmt.Sprintf(`
GROUP BY symbol
ORDER BY symbol
LIMIT %s`, clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query US stock symbols: %w", err)
	}
	defer rows.Close()

	symbols := make([]dto.USStockSymbolRow, 0, limit)
	for rows.Next() {
		var row dto.USStockSymbolRow
		if err := rows.Scan(&row.Symbol); err != nil {
			return nil, fmt.Errorf("scan US stock symbol row: %w", err)
		}
		symbols = append(symbols, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock symbol rows: %w", err)
	}

	resp := &dto.USStockSymbolResponse{Data: make([]dto.USStockSymbolRow, 0)}
	resp.Data, resp.NextCursor = applySymbolCursorPagination(symbols, limit, func(r dto.USStockSymbolRow) string {
		return encodeCursorString(r.Symbol)
	})
	s.attachCompanyProfilesToSymbols(ctx, resp.Data)
	return resp, nil
}

func (s *USStocksService) QueryBars(ctx context.Context, req dto.USStockBarRequest) (*dto.USStockBarResponse, error) {
	fromT, toT, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}

	tableName, err := resolveUSBarTable(req.Interval, chquery.USStockIntervals, "US stock")
	if err != nil {
		return nil, err
	}
	session, err := normalizeUSSession(req.Session, req.Interval)
	if err != nil {
		return nil, err
	}
	limit := usBarLimit(req.Limit)

	if req.Cursor != "" {
		cursorTime, err := decodeCursor(req.Cursor)
		if err != nil {
			return nil, invalidCursorError(err)
		}
		if cursorTime.After(fromT) {
			fromT = cursorTime
		}
	}

	query := fmt.Sprintf(`SELECT
    timestamp,
    symbol,
    open,
    high,
    low,
    close,
	toFloat64(volume) AS volume,
    toUInt64(transactions) AS transactions
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')%s
ORDER BY timestamp
LIMIT %s`, tableName, clickhouseStringLiteral(req.Symbol), clickhouseDateTimeLiteral(fromT), clickhouseDateTimeLiteral(toT), usSessionCondition(session), clickhouseUInt32Literal(limit+1))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query US stock bars: %w", err)
	}
	defer rows.Close()

	bars := make([]dto.USStockBarRow, 0, limit)
	for rows.Next() {
		var row dto.USStockBarRow
		if err := rows.Scan(
			&row.Timestamp,
			&row.Symbol,
			&row.Open,
			&row.High,
			&row.Low,
			&row.Close,
			&row.Volume,
			&row.Transactions,
		); err != nil {
			return nil, fmt.Errorf("scan US stock bar row: %w", err)
		}
		bars = append(bars, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock bar rows: %w", err)
	}

	resp := &dto.USStockBarResponse{Data: make([]dto.USStockBarRow, 0)}
	resp.Data, resp.NextCursor = applyTimeCursorPagination(bars, limit, func(r dto.USStockBarRow) string {
		return encodeCursor(r.Timestamp)
	})
	if err := s.attachFundamentals(ctx, req.Symbol, req.Factors, resp.Data); err != nil {
		return nil, err
	}
	s.attachCompanyProfile(ctx, req.Symbol, resp)
	return resp, nil
}

func (s *USStocksService) attachCompanyProfile(ctx context.Context, symbol string, resp *dto.USStockBarResponse) {
	if s == nil || s.companyInfo == nil || resp == nil {
		return
	}
	profile, err := s.companyInfo.CompanyProfile(ctx, symbol)
	if err != nil || profile == nil {
		return
	}
	if resp.Meta == nil {
		resp.Meta = &dto.USStockBarMeta{}
	}
	resp.Meta.Profile = profile
}

func (s *USStocksService) attachCompanyProfilesToSymbols(ctx context.Context, rows []dto.USStockSymbolRow) {
	if s == nil || s.companyInfo == nil || len(rows) == 0 {
		return
	}
	for i := range rows {
		profile, err := s.companyInfo.CompanyProfile(ctx, rows[i].Symbol)
		if err != nil || profile == nil {
			continue
		}
		rows[i].Profile = profile
	}
}

func (s *USStocksService) attachFundamentals(ctx context.Context, symbol string, requestedFactors []string, bars []dto.USStockBarRow) error {
	factors := normalizeRequestedFactors(requestedFactors)
	if len(factors) == 0 || len(bars) == 0 {
		return nil
	}
	if s.fundamentals == nil {
		return fmt.Errorf("US stock fundamentals enrichment requires fundamentals provider")
	}

	startTS := bars[0].Timestamp.UTC()
	endTS := bars[len(bars)-1].Timestamp.UTC().Add(time.Nanosecond)

	snapshot, err := s.fundamentals.QuerySnapshot(ctx, dto.FundamentalSnapshotRequest{
		Market:  "us-stocks",
		Symbol:  symbol,
		Factors: factors,
		AsOf:    startTS.Format(time.RFC3339Nano),
	})
	if err != nil {
		return fmt.Errorf("query US stock fundamentals snapshot: %w", err)
	}

	currentByFactor := make(map[string]dto.USStockBarFundamentalValue, len(factors))
	for _, entry := range snapshot.Data {
		currentByFactor[entry.Factor] = dto.USStockBarFundamentalValue{
			EventTS: entry.EventTS,
			KnownAt: entry.KnownAt,
			Value:   entry.Value,
			Source:  entry.Source,
			Filled:  !entry.EventTS.Equal(startTS),
		}
	}

	eventsByFactor := make(map[string][]dto.FundamentalSeriesPoint, len(factors))
	for _, factor := range factors {
		series, err := s.fundamentals.QuerySeries(ctx, dto.FundamentalSeriesRequest{
			Market: "us-stocks",
			Symbol: symbol,
			Factor: factor,
			From:   startTS.Format(time.RFC3339Nano),
			To:     endTS.Format(time.RFC3339Nano),
			Mode:   "event",
			AsOf:   endTS.Format(time.RFC3339Nano),
		})
		if err != nil {
			return fmt.Errorf("query US stock fundamentals series for factor %q: %w", factor, err)
		}
		events := append([]dto.FundamentalSeriesPoint(nil), series.Data...)
		sort.Slice(events, func(i, j int) bool {
			if !events[i].KnownAt.Equal(events[j].KnownAt) {
				return events[i].KnownAt.Before(events[j].KnownAt)
			}
			if events[i].Revision != events[j].Revision {
				return events[i].Revision < events[j].Revision
			}
			return events[i].EventTS.Before(events[j].EventTS)
		})
		eventsByFactor[factor] = events
	}
	denominatorByKey, err := s.loadPriceDerivedFundamentalDenominators(ctx, symbol, currentByFactor, eventsByFactor)
	if err != nil {
		return err
	}

	nextByFactor := make(map[string]int, len(factors))
	for barIndex := range bars {
		barTS := bars[barIndex].Timestamp.UTC()
		barFundamentals := map[string]dto.USStockBarFundamentalValue{}
		for _, factor := range factors {
			events := eventsByFactor[factor]
			for nextByFactor[factor] < len(events) {
				candidate := events[nextByFactor[factor]]
				if candidate.KnownAt.After(barTS) || candidate.EventTS.After(barTS) {
					break
				}
				currentByFactor[factor] = dto.USStockBarFundamentalValue{
					EventTS: candidate.EventTS,
					KnownAt: candidate.KnownAt,
					Value:   candidate.Value,
					Source:  candidate.Source,
					Filled:  !candidate.EventTS.Equal(barTS),
				}
				nextByFactor[factor]++
			}
			if value, ok := currentByFactor[factor]; ok {
				value.Value = priceDerivedFundamentalValue(factor, float64(bars[barIndex].Close), value, denominatorByKey)
				value.Filled = !value.EventTS.Equal(barTS)
				barFundamentals[factor] = value
			}
		}
		if len(barFundamentals) > 0 {
			bars[barIndex].Fundamentals = barFundamentals
		}
	}
	return nil
}

func (s *USStocksService) loadPriceDerivedFundamentalDenominators(ctx context.Context, symbol string, snapshot map[string]dto.USStockBarFundamentalValue, eventsByFactor map[string][]dto.FundamentalSeriesPoint) (map[string]float64, error) {
	denominators := map[string]float64{}
	if s.repo == nil {
		return denominators, nil
	}

	eventDates := map[time.Time]struct{}{}
	for factor, value := range snapshot {
		if isPriceDerivedFundamentalFactor(factor) && value.Value != 0 {
			eventDates[value.EventTS.UTC()] = struct{}{}
		}
	}
	for factor, events := range eventsByFactor {
		if !isPriceDerivedFundamentalFactor(factor) {
			continue
		}
		for _, event := range events {
			if event.Value == 0 {
				continue
			}
			eventDates[event.EventTS.UTC()] = struct{}{}
		}
	}
	if len(eventDates) == 0 {
		return denominators, nil
	}

	orderedDates := make([]time.Time, 0, len(eventDates))
	for eventTS := range eventDates {
		orderedDates = append(orderedDates, eventTS)
	}
	sort.Slice(orderedDates, func(i, j int) bool { return orderedDates[i].Before(orderedDates[j]) })

	priceSeries, err := s.loadUSStockDailyCloses(ctx, symbol, orderedDates[0].AddDate(0, 0, -14), orderedDates[len(orderedDates)-1].AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}
	for factor, value := range snapshot {
		if !isPriceDerivedFundamentalFactor(factor) || value.Value == 0 {
			continue
		}
		if closePrice, ok := priceSeries.closeOnOrBefore(value.EventTS); ok && closePrice != 0 {
			denominators[fundamentalObservationKey(factor, value.EventTS)] = closePrice / value.Value
		}
	}
	for factor, events := range eventsByFactor {
		if !isPriceDerivedFundamentalFactor(factor) {
			continue
		}
		for _, event := range events {
			if event.Value == 0 {
				continue
			}
			if closePrice, ok := priceSeries.closeOnOrBefore(event.EventTS); ok && closePrice != 0 {
				denominators[fundamentalObservationKey(factor, event.EventTS)] = closePrice / event.Value
			}
		}
	}
	return denominators, nil
}

type usStockDailyClose struct {
	Timestamp time.Time
	Close     float64
}

type usStockDailyCloseSeries []usStockDailyClose

func (s *USStocksService) loadUSStockDailyCloses(ctx context.Context, symbol string, from, to time.Time) (usStockDailyCloseSeries, error) {
	query := fmt.Sprintf(`SELECT
    timestamp,
    toFloat64(close) AS close
FROM %s
WHERE symbol = %s
  AND timestamp >= toDateTime(%s, 'UTC')
  AND timestamp < toDateTime(%s, 'UTC')
ORDER BY timestamp`, chquery.USStockIntervals["1d"], clickhouseStringLiteral(symbol), clickhouseDateTimeLiteral(from), clickhouseDateTimeLiteral(to))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query US stock daily closes for fundamentals: %w", err)
	}
	defer rows.Close()

	series := make(usStockDailyCloseSeries, 0, 64)
	for rows.Next() {
		var point usStockDailyClose
		if err := rows.Scan(&point.Timestamp, &point.Close); err != nil {
			return nil, fmt.Errorf("scan US stock daily close for fundamentals: %w", err)
		}
		series = append(series, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US stock daily closes for fundamentals: %w", err)
	}
	return series, nil
}

func (s usStockDailyCloseSeries) closeOnOrBefore(ts time.Time) (float64, bool) {
	if len(s) == 0 {
		return 0, false
	}
	idx := sort.Search(len(s), func(i int) bool { return s[i].Timestamp.After(ts.UTC()) }) - 1
	if idx < 0 {
		return 0, false
	}
	return s[idx].Close, true
}

func priceDerivedFundamentalValue(factor string, barClose float64, observation dto.USStockBarFundamentalValue, denominatorByKey map[string]float64) float64 {
	if !isPriceDerivedFundamentalFactor(factor) {
		return observation.Value
	}
	denominator, ok := denominatorByKey[fundamentalObservationKey(factor, observation.EventTS)]
	if !ok || denominator == 0 {
		return observation.Value
	}
	return barClose / denominator
}

func isPriceDerivedFundamentalFactor(factor string) bool {
	switch factor {
	case "pe", "pb":
		return true
	default:
		return false
	}
}

func fundamentalObservationKey(factor string, eventTS time.Time) string {
	return factor + "|" + eventTS.UTC().Format(time.RFC3339Nano)
}

func normalizeRequestedFactors(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		for _, part := range strings.Split(raw, ",") {
			factor := strings.TrimSpace(part)
			if factor == "" {
				continue
			}
			if _, exists := seen[factor]; exists {
				continue
			}
			seen[factor] = struct{}{}
			out = append(out, factor)
		}
	}
	return out
}
