package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Cyvadra/toktik/internal/chquery"
	"github.com/Cyvadra/toktik/internal/chrepo"
	"github.com/Cyvadra/toktik/internal/dto"
)

const (
	macroDatasetGurufocusShiller = "gurufocus-shiller"
	macroDatasetFMPSP500Shiller  = "fmp-sp500-shiller"
	macroDatasetFMPNDXShiller    = "fmp-nasdaq100-shiller"
	macroIntervalEvent           = "event"
	macroRealtimeForwardFill     = "forward_fill"
	macroRealtimePriceScaled     = "price_scaled"
	defaultMacroReferenceMarket  = "us-stocks"
	defaultMacroSeriesLimit      = 10000
	maxMacroSeriesLimit          = 200000
	macroVirtualFactorSource     = "derived"
)

var supportedMacroDatasets = map[string]struct{}{
	macroDatasetGurufocusShiller: {},
	macroDatasetFMPSP500Shiller:  {},
	macroDatasetFMPNDXShiller:    {},
}

type MacroService struct {
	repo     *chrepo.Repo
	virtuals *macroVirtualFactorProvider
}

type macroFactorMeta struct {
	Dataset         string
	FactorCode      string
	DisplayName     string
	Description     string
	ValueType       string
	Unit            string
	FillPolicy      string
	FillMaxDays     int
	ReferenceMarket string
	ReferenceSymbol string
	RealtimeMode    string
}

type macroObservation struct {
	FactorCode      string
	EventTS         time.Time
	KnownAt         time.Time
	Value           float64
	Source          string
	ReferenceMarket string
	ReferenceSymbol string
	AnchorValue     float64
	Revision        uint32
}

type macroReferenceBar struct {
	Timestamp time.Time
	Close     float64
}

func NewMacroService(repo *chrepo.Repo) *MacroService {
	return &MacroService{repo: repo, virtuals: newMacroVirtualFactorProvider()}
}

func (s *MacroService) ListFactors(ctx context.Context, req dto.MacroFactorCatalogRequest) (*dto.MacroFactorCatalogResponse, error) {
	dataset, err := normalizeMacroDataset(req.Dataset, false)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.Query(ctx, chquery.MacroFactorCatalogQuery(), clickhouse.Named("dataset", dataset))
	if err != nil {
		return nil, fmt.Errorf("query macro factor catalog: %w", err)
	}
	defer rows.Close()

	out := &dto.MacroFactorCatalogResponse{Data: []dto.MacroFactorCatalogEntry{}}
	for rows.Next() {
		var (
			entry       dto.MacroFactorCatalogEntry
			fillMaxDays uint16
			pointInTime uint8
			active      uint8
			slaHours    uint32
		)
		if err := rows.Scan(
			&entry.Dataset,
			&entry.FactorCode,
			&entry.DisplayName,
			&entry.Description,
			&entry.ValueType,
			&entry.Unit,
			&entry.PreferredFrequency,
			&entry.FillPolicy,
			&fillMaxDays,
			&pointInTime,
			&entry.Source,
			&entry.ReferenceMarket,
			&entry.ReferenceSymbol,
			&entry.RealtimeMode,
			&active,
			&slaHours,
			&entry.Metadata,
			&entry.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan macro factor catalog: %w", err)
		}
		entry.FillMaxDays = int(fillMaxDays)
		entry.PointInTime = pointInTime != 0
		entry.Active = active != 0
		entry.SLAHours = int(slaHours)
		out.Data = append(out.Data, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	virtualDataset := dataset
	if virtualDataset == "" {
		virtualDataset = macroDatasetGurufocusShiller
	}
	out.Data = s.virtuals.appendCatalogEntries(virtualDataset, out.Data)
	return out, rows.Err()
}

func (s *MacroService) QuerySeries(ctx context.Context, req dto.MacroSeriesRequest) (*dto.MacroSeriesResponse, error) {
	dataset, err := normalizeMacroDataset(req.Dataset, true)
	if err != nil {
		return nil, err
	}
	requestedFactors := normalizeStringList(req.Factors)
	if len(requestedFactors) == 0 {
		return nil, dto.NewValidationError("factor must be non-empty")
	}
	from, to, err := dto.ParseTimeRange(req.From, req.To)
	if err != nil {
		return nil, err
	}
	asOf, err := resolveAsOf(req.AsOf, to)
	if err != nil {
		return nil, err
	}
	interval := strings.ToLower(strings.TrimSpace(req.Interval))
	if interval == "" {
		interval = macroIntervalEvent
	}
	from, to = normalizeMacroExpandedRange(interval, req.From, req.To, from, to)
	limit := clamp(req.Limit, defaultMacroSeriesLimit, maxMacroSeriesLimit)

	metaByFactor, err := s.loadFactorMeta(ctx, dataset)
	if err != nil {
		return nil, err
	}
	virtualByFactor := s.virtuals.factorMap(dataset)
	sourceFactors := make([]string, 0, len(requestedFactors))
	seenSource := map[string]bool{}
	for _, factor := range requestedFactors {
		if virtual, ok := virtualByFactor[factor]; ok {
			if _, ok := metaByFactor[virtual.BaseFactor]; !ok {
				return nil, dto.NewValidationError("virtual macro factor %q requires base factor %q", factor, virtual.BaseFactor)
			}
			if !seenSource[virtual.BaseFactor] {
				seenSource[virtual.BaseFactor] = true
				sourceFactors = append(sourceFactors, virtual.BaseFactor)
			}
			continue
		}
		if _, ok := metaByFactor[factor]; !ok {
			return nil, dto.NewValidationError("unknown macro factor %q for dataset %q", factor, dataset)
		}
		if !seenSource[factor] {
			seenSource[factor] = true
			sourceFactors = append(sourceFactors, factor)
		}
	}

	observationFrom := from
	if interval != macroIntervalEvent {
		observationFrom = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	observations, err := s.queryMacroObservations(ctx, dataset, sourceFactors, observationFrom, to, asOf)
	if err != nil {
		return nil, err
	}

	resp := &dto.MacroSeriesResponse{
		Dataset:  dataset,
		Interval: interval,
		AsOf:     asOf,
		Data:     []dto.MacroSeriesPoint{},
	}

	if interval == macroIntervalEvent {
		resp.Data = buildMacroEventSeries(requestedFactors, observations, virtualByFactor, from, to)
		if len(resp.Data) > limit {
			resp.Data = resp.Data[:limit]
		}
		return resp, nil
	}

	referenceMarket, referenceSymbol, err := resolveMacroReference(req.ReferenceMarket, req.ReferenceSymbol, sourceFactors, metaByFactor)
	if err != nil {
		return nil, err
	}
	tableName, err := resolveUSBarTable(interval, chquery.USStockIntervals, "macro reference")
	if err != nil {
		return nil, err
	}
	anchorValues, err := s.queryReferenceAnchorValues(ctx, tableName, referenceSymbol, collectMacroAnchorEventTimestamps(observations, from, to), interval == "1d")
	if err != nil {
		return nil, err
	}
	bars, err := s.queryReferenceBars(ctx, tableName, referenceSymbol, from, to, limit, interval == "1d")
	if err != nil {
		return nil, err
	}
	resp.ReferenceMarket = referenceMarket
	resp.ReferenceSymbol = referenceSymbol
	if interval == "1d" {
		resp.Data = buildExpandedMacroDailySeries(requestedFactors, observations, metaByFactor, virtualByFactor, bars, anchorValues, referenceMarket, referenceSymbol, from, to)
	} else {
		resp.Data = buildExpandedMacroSeries(requestedFactors, observations, metaByFactor, virtualByFactor, bars, anchorValues, referenceMarket, referenceSymbol)
	}
	if len(resp.Data) > limit {
		resp.Data = resp.Data[:limit]
	}
	return resp, nil
}

func (s *MacroService) loadFactorMeta(ctx context.Context, dataset string) (map[string]macroFactorMeta, error) {
	rows, err := s.repo.Query(ctx, chquery.MacroFactorCatalogQuery(), clickhouse.Named("dataset", dataset))
	if err != nil {
		return nil, fmt.Errorf("query macro factor metadata: %w", err)
	}
	defer rows.Close()

	out := make(map[string]macroFactorMeta)
	for rows.Next() {
		var (
			entry       dto.MacroFactorCatalogEntry
			fillMaxDays uint16
			pointInTime uint8
			active      uint8
			slaHours    uint32
		)
		if err := rows.Scan(
			&entry.Dataset,
			&entry.FactorCode,
			&entry.DisplayName,
			&entry.Description,
			&entry.ValueType,
			&entry.Unit,
			&entry.PreferredFrequency,
			&entry.FillPolicy,
			&fillMaxDays,
			&pointInTime,
			&entry.Source,
			&entry.ReferenceMarket,
			&entry.ReferenceSymbol,
			&entry.RealtimeMode,
			&active,
			&slaHours,
			&entry.Metadata,
			&entry.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan macro factor metadata: %w", err)
		}
		out[entry.FactorCode] = macroFactorMeta{
			Dataset:         entry.Dataset,
			FactorCode:      entry.FactorCode,
			DisplayName:     entry.DisplayName,
			Description:     entry.Description,
			ValueType:       entry.ValueType,
			Unit:            entry.Unit,
			FillPolicy:      entry.FillPolicy,
			FillMaxDays:     int(fillMaxDays),
			ReferenceMarket: entry.ReferenceMarket,
			ReferenceSymbol: entry.ReferenceSymbol,
			RealtimeMode:    entry.RealtimeMode,
		}
	}
	for code, virtual := range s.virtuals.factorMap(dataset) {
		base := out[virtual.BaseFactor]
		out[code] = macroFactorMeta{
			Dataset:         dataset,
			FactorCode:      code,
			DisplayName:     virtual.DisplayName,
			Description:     virtual.Description,
			ValueType:       virtual.ValueType,
			Unit:            virtual.Unit,
			FillPolicy:      base.FillPolicy,
			FillMaxDays:     base.FillMaxDays,
			ReferenceMarket: base.ReferenceMarket,
			ReferenceSymbol: base.ReferenceSymbol,
			RealtimeMode:    base.RealtimeMode,
		}
	}
	return out, rows.Err()
}

func (s *MacroService) queryMacroObservations(ctx context.Context, dataset string, factors []string, from, to, asOf time.Time) (map[string][]macroObservation, error) {
	rows, err := s.repo.Query(ctx, chquery.MacroSeriesEventQuery(),
		clickhouse.Named("dataset", dataset),
		clickhouse.Named("factors", factors),
		clickhouse.Named("as_of", asOf.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("from", from.UTC().Format(time.RFC3339Nano)),
		clickhouse.Named("to", to.UTC().Format(time.RFC3339Nano)),
	)
	if err != nil {
		return nil, fmt.Errorf("query macro observations: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]macroObservation)
	for rows.Next() {
		var row macroObservation
		if err := rows.Scan(
			&row.FactorCode,
			&row.EventTS,
			&row.KnownAt,
			&row.Value,
			&row.Source,
			&row.ReferenceMarket,
			&row.ReferenceSymbol,
			&row.AnchorValue,
			&row.Revision,
		); err != nil {
			return nil, fmt.Errorf("scan macro observation: %w", err)
		}
		out[row.FactorCode] = append(out[row.FactorCode], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for factor := range out {
		series := out[factor]
		collapsed := make([]macroObservation, 0, len(series))
		for _, item := range series {
			if len(collapsed) == 0 {
				collapsed = append(collapsed, item)
				continue
			}
			last := collapsed[len(collapsed)-1]
			if last.EventTS.Equal(item.EventTS) {
				collapsed[len(collapsed)-1] = item
				continue
			}
			collapsed = append(collapsed, item)
		}
		out[factor] = collapsed
	}
	return out, nil
}

func (s *MacroService) queryReferenceBars(ctx context.Context, tableName, symbol string, from, to time.Time, limit int, includeSeed bool) ([]macroReferenceBar, error) {
	sessionCondition := ""
	if strings.HasSuffix(tableName, "_1m") {
		sessionCondition = "\n\t  AND is_regular_session = 1"
	}

	query := fmt.Sprintf(`SELECT
		timestamp,
		toFloat64(close) AS close
	FROM %s
	WHERE symbol = %s
	  AND timestamp >= toDateTime(%s, 'UTC')
	  AND timestamp < toDateTime(%s, 'UTC')
	  %s
	ORDER BY timestamp
	LIMIT %s`, tableName, clickhouseStringLiteral(symbol), clickhouseDateTimeLiteral(from), clickhouseDateTimeLiteral(to), sessionCondition, clickhouseUInt32Literal(limit))

	rows, err := s.repo.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query macro reference bars: %w", err)
	}
	defer rows.Close()

	out := make([]macroReferenceBar, 0, limit)
	if includeSeed {
		seedQuery := fmt.Sprintf(`SELECT
			timestamp,
			toFloat64(close) AS close
		FROM %s
		WHERE symbol = %s
		  AND timestamp < toDateTime(%s, 'UTC')
		ORDER BY timestamp DESC
		LIMIT 1`, tableName, clickhouseStringLiteral(symbol), clickhouseDateTimeLiteral(from))

		seedRows, err := s.repo.Query(ctx, seedQuery)
		if err != nil {
			return nil, fmt.Errorf("query macro reference bar seed: %w", err)
		}
		defer seedRows.Close()
		for seedRows.Next() {
			var seed macroReferenceBar
			if err := seedRows.Scan(&seed.Timestamp, &seed.Close); err != nil {
				return nil, fmt.Errorf("scan macro reference bar seed: %w", err)
			}
			out = append(out, seed)
		}
		if err := seedRows.Err(); err != nil {
			return nil, err
		}
	}
	for rows.Next() {
		var row macroReferenceBar
		if err := rows.Scan(&row.Timestamp, &row.Close); err != nil {
			return nil, fmt.Errorf("scan macro reference bar: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, dto.NewNotFoundError("no reference bars found for %s in requested range", symbol)
	}
	return out, nil
}

func flattenMacroObservationsInRange(observations map[string][]macroObservation, filled bool, from, to time.Time) []dto.MacroSeriesPoint {
	out := make([]dto.MacroSeriesPoint, 0)
	for factor, series := range observations {
		for _, item := range series {
			if item.EventTS.Before(from) || !item.EventTS.Before(to) {
				continue
			}
			out = append(out, dto.MacroSeriesPoint{
				Factor:          factor,
				Timestamp:       item.EventTS,
				EventTS:         item.EventTS,
				KnownAt:         item.KnownAt,
				Value:           item.Value,
				Source:          item.Source,
				Filled:          filled,
				ReferenceMarket: item.ReferenceMarket,
				ReferenceSymbol: item.ReferenceSymbol,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func buildMacroEventSeries(requestedFactors []string, observations map[string][]macroObservation, virtualByFactor map[string]macroVirtualFactor, from, to time.Time) []dto.MacroSeriesPoint {
	raw := flattenMacroObservationsInRange(observations, false, from, to)
	byFactor := groupMacroSeriesByFactor(raw)
	out := make([]dto.MacroSeriesPoint, 0, len(raw))
	for _, factor := range requestedFactors {
		if virtual, ok := virtualByFactor[factor]; ok {
			out = append(out, deriveVirtualMacroSeries(factor, byFactor[virtual.BaseFactor], virtual)...)
			continue
		}
		out = append(out, byFactor[factor]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func buildExpandedMacroSeries(requestedFactors []string, observations map[string][]macroObservation, metaByFactor map[string]macroFactorMeta, virtualByFactor map[string]macroVirtualFactor, bars []macroReferenceBar, anchorValues map[time.Time]float64, referenceMarket, referenceSymbol string) []dto.MacroSeriesPoint {
	baseExpanded := expandMacroObservations(observations, metaByFactor, bars, anchorValues, referenceMarket, referenceSymbol)
	byFactor := groupMacroSeriesByFactor(baseExpanded)
	out := make([]dto.MacroSeriesPoint, 0, len(baseExpanded))
	for _, factor := range requestedFactors {
		if virtual, ok := virtualByFactor[factor]; ok {
			out = append(out, deriveVirtualMacroSeries(factor, byFactor[virtual.BaseFactor], virtual)...)
			continue
		}
		out = append(out, byFactor[factor]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func buildExpandedMacroDailySeries(requestedFactors []string, observations map[string][]macroObservation, metaByFactor map[string]macroFactorMeta, virtualByFactor map[string]macroVirtualFactor, bars []macroReferenceBar, anchorValues map[time.Time]float64, referenceMarket, referenceSymbol string, from, to time.Time) []dto.MacroSeriesPoint {
	baseExpanded := expandMacroObservationsDaily(observations, metaByFactor, bars, anchorValues, referenceMarket, referenceSymbol, from, to)
	byFactor := groupMacroSeriesByFactor(baseExpanded)
	out := make([]dto.MacroSeriesPoint, 0, len(baseExpanded))
	for _, factor := range requestedFactors {
		if virtual, ok := virtualByFactor[factor]; ok {
			out = append(out, deriveVirtualMacroSeries(factor, byFactor[virtual.BaseFactor], virtual)...)
			continue
		}
		out = append(out, byFactor[factor]...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func groupMacroSeriesByFactor(points []dto.MacroSeriesPoint) map[string][]dto.MacroSeriesPoint {
	out := make(map[string][]dto.MacroSeriesPoint)
	for _, point := range points {
		out[point.Factor] = append(out[point.Factor], point)
	}
	return out
}

func deriveVirtualMacroSeries(code string, source []dto.MacroSeriesPoint, virtual macroVirtualFactor) []dto.MacroSeriesPoint {
	if len(source) == 0 {
		return nil
	}
	out := make([]dto.MacroSeriesPoint, 0, len(source))
	for _, point := range source {
		value, ok := virtual.Transform(point.Value)
		if !ok {
			continue
		}
		point.Factor = code
		point.Value = value
		out = append(out, point)
	}
	return out
}

func expandMacroObservations(observations map[string][]macroObservation, metaByFactor map[string]macroFactorMeta, bars []macroReferenceBar, anchorValues map[time.Time]float64, referenceMarket, referenceSymbol string) []dto.MacroSeriesPoint {
	out := make([]dto.MacroSeriesPoint, 0, len(bars)*max(1, len(observations)))
	for factor, series := range observations {
		if len(series) == 0 {
			continue
		}
		meta := metaByFactor[factor]
		seriesIndex := 0
		for _, bar := range bars {
			for seriesIndex+1 < len(series) && !series[seriesIndex+1].KnownAt.After(bar.Timestamp) {
				seriesIndex++
			}
			current := series[seriesIndex]
			if current.KnownAt.After(bar.Timestamp) {
				continue
			}
			value := current.Value
			realtime := false
			anchorValue := resolveMacroAnchorValue(current, anchorValues, referenceSymbol)
			if meta.RealtimeMode == macroRealtimePriceScaled && !math.IsNaN(anchorValue) && anchorValue != 0 {
				value = current.Value * (bar.Close / anchorValue)
				realtime = true
			}
			out = append(out, dto.MacroSeriesPoint{
				Factor:          factor,
				Timestamp:       bar.Timestamp,
				EventTS:         current.EventTS,
				KnownAt:         current.KnownAt,
				Value:           value,
				Source:          current.Source,
				Filled:          true,
				Realtime:        realtime,
				ReferenceMarket: referenceMarket,
				ReferenceSymbol: referenceSymbol,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func expandMacroObservationsDaily(observations map[string][]macroObservation, metaByFactor map[string]macroFactorMeta, bars []macroReferenceBar, anchorValues map[time.Time]float64, referenceMarket, referenceSymbol string, from, to time.Time) []dto.MacroSeriesPoint {
	if len(bars) == 0 {
		return nil
	}
	startDay := time.Date(from.UTC().Year(), from.UTC().Month(), from.UTC().Day(), 0, 0, 0, 0, time.UTC)
	out := make([]dto.MacroSeriesPoint, 0, len(observations)*max(1, int(to.Sub(startDay)/(24*time.Hour))))
	for factor, series := range observations {
		if len(series) == 0 {
			continue
		}
		meta := metaByFactor[factor]
		seriesIndex := 0
		barIndex := 0
		for day := startDay; day.Before(to); day = day.AddDate(0, 0, 1) {
			for barIndex+1 < len(bars) && !bars[barIndex+1].Timestamp.After(day) {
				barIndex++
			}
			currentBar := bars[barIndex]
			if currentBar.Timestamp.After(day) {
				continue
			}

			dayEnd := day.AddDate(0, 0, 1)
			for seriesIndex+1 < len(series) && series[seriesIndex+1].KnownAt.Before(dayEnd) {
				seriesIndex++
			}
			current := series[seriesIndex]
			if !current.KnownAt.Before(dayEnd) {
				continue
			}

			value := current.Value
			realtime := false
			anchorValue := resolveMacroAnchorValue(current, anchorValues, referenceSymbol)
			if meta.RealtimeMode == macroRealtimePriceScaled && !math.IsNaN(anchorValue) && anchorValue != 0 {
				value = current.Value * (currentBar.Close / anchorValue)
				realtime = true
			}
			out = append(out, dto.MacroSeriesPoint{
				Factor:          factor,
				Timestamp:       day,
				EventTS:         current.EventTS,
				KnownAt:         current.KnownAt,
				Value:           value,
				Source:          current.Source,
				Filled:          true,
				Realtime:        realtime,
				ReferenceMarket: referenceMarket,
				ReferenceSymbol: referenceSymbol,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Factor < out[j].Factor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func (s *MacroService) queryReferenceAnchorValues(ctx context.Context, tableName, symbol string, eventTimestamps []time.Time, includeSeed bool) (map[time.Time]float64, error) {
	if len(eventTimestamps) == 0 {
		return map[time.Time]float64{}, nil
	}
	sortedTargets := append([]time.Time(nil), eventTimestamps...)
	sort.Slice(sortedTargets, func(i, j int) bool { return sortedTargets[i].Before(sortedTargets[j]) })
	from := sortedTargets[0].AddDate(0, 0, -14)
	to := sortedTargets[len(sortedTargets)-1].Add(24 * time.Hour)
	bars, err := s.queryReferenceBars(ctx, tableName, symbol, from, to, maxMacroSeriesLimit, includeSeed)
	if err != nil {
		return nil, fmt.Errorf("query macro reference anchors: %w", err)
	}
	return mapMacroAnchorValues(sortedTargets, bars), nil
}

func collectMacroAnchorEventTimestamps(observations map[string][]macroObservation, from, to time.Time) []time.Time {
	seen := make(map[time.Time]struct{})
	out := make([]time.Time, 0)
	for _, series := range observations {
		if len(series) == 0 {
			continue
		}
		currentIndex := -1
		for index, item := range series {
			if !item.KnownAt.After(from) {
				currentIndex = index
				continue
			}
			break
		}
		startIndex := 0
		if currentIndex >= 0 {
			startIndex = currentIndex
		}
		for index := startIndex; index < len(series); index++ {
			item := series[index]
			if index > currentIndex && !item.KnownAt.Before(to) {
				break
			}
			if _, ok := seen[item.EventTS]; ok {
				continue
			}
			seen[item.EventTS] = struct{}{}
			out = append(out, item.EventTS)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

func mapMacroAnchorValues(targets []time.Time, bars []macroReferenceBar) map[time.Time]float64 {
	out := make(map[time.Time]float64, len(targets))
	if len(targets) == 0 || len(bars) == 0 {
		return out
	}
	barIndex := 0
	for _, target := range targets {
		for barIndex+1 < len(bars) && !bars[barIndex+1].Timestamp.After(target) {
			barIndex++
		}
		current := bars[barIndex]
		if current.Timestamp.After(target) {
			continue
		}
		out[target] = current.Close
	}
	return out
}

func resolveMacroAnchorValue(current macroObservation, anchorValues map[time.Time]float64, referenceSymbol string) float64 {
	if anchorValues != nil {
		if value, ok := anchorValues[current.EventTS]; ok && !math.IsNaN(value) && value != 0 {
			return value
		}
	}
	if strings.EqualFold(current.ReferenceSymbol, referenceSymbol) && !math.IsNaN(current.AnchorValue) && current.AnchorValue != 0 {
		return current.AnchorValue
	}
	return math.NaN()
}

func normalizeMacroDataset(dataset string, required bool) (string, error) {
	dataset = strings.TrimSpace(dataset)
	if dataset == "" {
		if required {
			return "", dto.NewValidationError("dataset is required")
		}
		return "", nil
	}
	if _, ok := supportedMacroDatasets[dataset]; !ok {
		return "", dto.NewValidationError("unsupported macro dataset %q", dataset)
	}
	return dataset, nil
}

func resolveMacroReference(requestMarket, requestSymbol string, factors []string, metaByFactor map[string]macroFactorMeta) (string, string, error) {
	market := strings.TrimSpace(requestMarket)
	symbol := strings.ToUpper(strings.TrimSpace(requestSymbol))
	if market != "" && market != defaultMacroReferenceMarket {
		return "", "", dto.NewValidationError("unsupported reference_market %q", requestMarket)
	}
	if symbol != "" {
		if market == "" {
			market = defaultMacroReferenceMarket
		}
		return market, symbol, nil
	}
	for _, factor := range factors {
		meta := metaByFactor[factor]
		if meta.ReferenceSymbol == "" {
			continue
		}
		if symbol == "" {
			symbol = meta.ReferenceSymbol
			market = meta.ReferenceMarket
			continue
		}
		if symbol != meta.ReferenceSymbol {
			return "", "", dto.NewValidationError("factors require different reference symbols; pass reference_symbol explicitly")
		}
	}
	if symbol == "" {
		return "", "", dto.NewValidationError("reference_symbol is required for expanded macro intervals")
	}
	if market == "" {
		market = defaultMacroReferenceMarket
	}
	return market, symbol, nil
}

func normalizeMacroExpandedRange(interval, rawFrom, rawTo string, from, to time.Time) (time.Time, time.Time) {
	if interval == "1d" && isDateOnlyTimeInput(rawFrom) && isDateOnlyTimeInput(rawTo) {
		return from, to.AddDate(0, 0, 1)
	}
	return from, to
}

func isDateOnlyTimeInput(value string) bool {
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "T") {
		return false
	}
	_, err := time.Parse("2006-01-02", trimmed)
	return err == nil
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
