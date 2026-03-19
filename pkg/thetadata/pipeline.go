package thetadata

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Cyvadra/toktik/internal/cryptooptions"
)

// Pipeline orchestrates the download of US equity options historical data
// from the Theta Data API, computes Greeks, and stores results in ClickHouse.
type Pipeline struct {
	cfg      SyncConfig
	store    *Store
	progress *Progress
}

type phaseProgress struct {
	root      string
	phase     string
	unit      string
	total     int
	startedAt time.Time
	processed atomic.Int64
	succeeded atomic.Int64
	stopCh    chan struct{}
	stopped   atomic.Bool
}

// NewPipeline creates a new download pipeline.
func NewPipeline(cfg SyncConfig, store *Store, progress *Progress) *Pipeline {
	return &Pipeline{
		cfg:      cfg,
		store:    store,
		progress: progress,
	}
}

func newPhaseProgress(root, phase, unit string, total int) *phaseProgress {
	return &phaseProgress{
		root:      root,
		phase:     phase,
		unit:      unit,
		total:     total,
		startedAt: time.Now(),
		stopCh:    make(chan struct{}),
	}
}

func (p *phaseProgress) Start() {
	if p.total <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.logStatus("in progress")
			case <-p.stopCh:
				return
			}
		}
	}()
}

func (p *phaseProgress) Add(processedDelta int, succeeded bool) {
	if processedDelta > 0 {
		p.processed.Add(int64(processedDelta))
	}
	if succeeded {
		p.succeeded.Add(1)
	}
}

func (p *phaseProgress) Finish(summary string) {
	if p.stopped.CompareAndSwap(false, true) {
		close(p.stopCh)
	}
	p.logStatus(summary)
}

func (p *phaseProgress) logStatus(summary string) {
	processed := int(p.processed.Load())
	succeeded := int(p.succeeded.Load())
	percent := 100.0
	if p.total > 0 {
		percent = float64(processed) * 100 / float64(p.total)
	}
	elapsed := time.Since(p.startedAt).Round(time.Second)
	remaining := "eta=n/a"
	if processed > 0 && p.total > 0 && processed < p.total {
		eta := time.Duration(float64(time.Since(p.startedAt)) * float64(p.total-processed) / float64(processed))
		remaining = fmt.Sprintf("eta=%s", eta.Round(time.Second))
	}
	log.Printf("[%s] %s progress: %d/%d %s (%.1f%%, kept=%d, elapsed=%s, %s) [%s]",
		p.root, p.phase, processed, p.total, p.unit, percent, succeeded, elapsed, remaining, summary)
}

// Run executes the full download pipeline for all configured root symbols.
func (p *Pipeline) Run(ctx context.Context) error {
	for _, root := range p.cfg.Roots {
		if err := p.syncRoot(ctx, root); err != nil {
			return fmt.Errorf("sync %s: %w", root, err)
		}
	}
	return nil
}

// syncRoot downloads all data for a single root symbol.
func (p *Pipeline) syncRoot(ctx context.Context, root string) error {
	log.Printf("[%s] Starting sync from %s to %s",
		root, p.cfg.EndDate.Format("2006-01-02"), p.cfg.StartDate.Format("2006-01-02"))

	metaClient, err := p.newClient(ctx)
	if err != nil {
		return fmt.Errorf("create meta client: %w", err)
	}
	defer metaClient.Close()
	defer metaClient.mcp.Close()

	log.Printf("[%s] Phase 1: Enumerating contracts...", root)
	expirations, err := metaClient.ListExpirations(ctx, root)
	if err != nil {
		return fmt.Errorf("list expirations: %w", err)
	}

	var relevantExps []time.Time
	for _, exp := range expirations {
		if !exp.Before(p.cfg.StartDate) {
			relevantExps = append(relevantExps, exp)
		}
	}
	log.Printf("[%s] Found %d total expirations, %d relevant for date range",
		root, len(expirations), len(relevantExps))

	type expStrikes struct {
		Exp     time.Time
		Strikes []float64
	}
	var universe []expStrikes
	phase1Progress := newPhaseProgress(root, "Phase 1", "expirations", len(relevantExps))
	phase1Progress.Start()
	for _, exp := range relevantExps {
		strikes, err := metaClient.ListStrikes(ctx, root, exp)
		if err != nil {
			log.Printf("[%s] Warning: failed to list strikes for exp %s: %v",
				root, exp.Format("2006-01-02"), err)
			phase1Progress.Add(1, false)
			continue
		}
		if len(strikes) == 0 {
			phase1Progress.Add(1, false)
			continue
		}
		universe = append(universe, expStrikes{Exp: exp, Strikes: strikes})
		phase1Progress.Add(1, true)
	}
	phase1Progress.Finish("complete")
	log.Printf("[%s] Contract universe: %d expirations with strikes", root, len(universe))

	var estimatedPrice float64
	if len(universe) > 0 {
		sampleExp := universe[len(universe)/2]
		midStrike := sampleExp.Strikes[len(sampleExp.Strikes)/2]
		sampleContract := Contract{
			Root:       root,
			Expiration: sampleExp.Exp,
			Strike:     midStrike,
			Right:      "C",
		}
		if sampleEOD, err := metaClient.GetGreeksEOD(ctx, sampleContract, p.cfg.StartDate, p.cfg.EndDate); err == nil && len(sampleEOD) > 0 {
			for i := len(sampleEOD) - 1; i >= 0; i-- {
				if sampleEOD[i].UnderlyingPrice > 0 {
					estimatedPrice = sampleEOD[i].UnderlyingPrice
					break
				}
			}
		}
		if estimatedPrice == 0 {
			sampleExp = universe[0]
			midStrike = sampleExp.Strikes[len(sampleExp.Strikes)/2]
			sampleContract = Contract{Root: root, Expiration: sampleExp.Exp, Strike: midStrike, Right: "P"}
			if sampleEOD, err := metaClient.GetGreeksEOD(ctx, sampleContract, p.cfg.StartDate, p.cfg.EndDate); err == nil && len(sampleEOD) > 0 {
				for i := len(sampleEOD) - 1; i >= 0; i-- {
					if sampleEOD[i].UnderlyingPrice > 0 {
						estimatedPrice = sampleEOD[i].UnderlyingPrice
						break
					}
				}
			}
		}
	}

	var allContracts []Contract
	var skippedByStrike int
	for _, es := range universe {
		for _, strike := range es.Strikes {
			if estimatedPrice > 0 {
				ratio := strike / estimatedPrice
				if ratio < 0.5 || ratio > 1.5 {
					skippedByStrike += 2
					continue
				}
			}
			for _, right := range []string{"C", "P"} {
				allContracts = append(allContracts, Contract{
					Root:       root,
					Expiration: es.Exp,
					Strike:     strike,
					Right:      right,
				})
			}
		}
	}
	if estimatedPrice > 0 {
		log.Printf("[%s] Estimated underlying price: %.2f, strike filter ±50%%: kept %d, skipped %d contracts",
			root, estimatedPrice, len(allContracts), skippedByStrike)
	}
	log.Printf("[%s] Total contracts: %d", root, len(allContracts))

	underlyingFallback := p.buildUnderlyingFallback(ctx, metaClient, root)
	weeks := buildWeekdayBatches(p.cfg.StartDate, p.cfg.EndDate, p.cfg.BatchDays)
	totalDates := countWeekBatchDates(weeks)
	log.Printf("[%s] Phase 2/3: OHLC-first weekly discovery across %d trading days in %d batches",
		root, totalDates, len(weeks))

	weekOffset := 0
	for wi, weekDates := range weeks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pendingDates := make([]time.Time, 0, len(weekDates))
		pendingDateSet := make(map[string]struct{}, len(weekDates))
		for _, date := range weekDates {
			if !p.progress.IsCompleted(root, date) {
				pendingDates = append(pendingDates, date)
				pendingDateSet[date.Format("2006-01-02")] = struct{}{}
			}
		}
		if len(pendingDates) == 0 {
			weekOffset += len(weekDates)
			continue
		}

		weekEnd := weekDates[0]
		weekStart := weekDates[len(weekDates)-1]
		var weekContracts []Contract
		for _, c := range allContracts {
			if !c.Expiration.Before(weekStart) {
				weekContracts = append(weekContracts, c)
			}
		}

		if len(weekContracts) == 0 {
			for _, date := range pendingDates {
				weekOffset++
				if err := p.progress.MarkCompleted(root, date, DateSyncStats{}); err != nil {
					log.Printf("[%s] Warning: failed to mark empty week date %s complete: %v", root, date.Format("2006-01-02"), err)
				}
			}
			continue
		}

		log.Printf("[%s] Phase 2 batch %d/%d: %s to %s, %d pending dates, %d candidate contracts",
			root, wi+1, len(weeks),
			weekStart.Format("2006-01-02"), weekEnd.Format("2006-01-02"),
			len(pendingDates), len(weekContracts))

		selectedByDate := make(map[string][]Contract, len(pendingDates))
		selectedContractsMap := make(map[string]Contract)
		ohlcCache := &sync.Map{}
		var selectionMu sync.Mutex
		type phase2Diagnostics struct {
			ohlcNonEmpty  atomic.Int64
			withVolume    atomic.Int64
			withCount     atomic.Int64
			barsOnly      atomic.Int64
			keptByVolume  atomic.Int64
			keptByCount   atomic.Int64
			keptByBars    atomic.Int64
			sampleMu      sync.Mutex
			sampleEntries []string
		}
		diagnostics := &phase2Diagnostics{}
		maxSamples := p.cfg.DebugSampleContracts
		if maxSamples <= 0 {
			maxSamples = 8
		}
		appendSample := func(entry string) {
			if !p.cfg.Debug {
				return
			}
			diagnostics.sampleMu.Lock()
			defer diagnostics.sampleMu.Unlock()
			if len(diagnostics.sampleEntries) >= maxSamples {
				return
			}
			diagnostics.sampleEntries = append(diagnostics.sampleEntries, entry)
		}

		phase2Progress := newPhaseProgress(root, fmt.Sprintf("Phase 2 batch %d/%d", wi+1, len(weeks)),
			"contracts", len(weekContracts))
		phase2Progress.Start()
		err = p.parallelWork(ctx, weekContracts, p.cfg.Workers, func(ctx context.Context, client *Client, c Contract) error {
			ohlcBars, err := client.GetOHLC1mRange(ctx, c, weekStart, weekEnd)
			if err != nil {
				if p.cfg.Debug {
					appendSample(fmt.Sprintf("%s ohlc_error=%v", c.Symbol(), err))
				}
				phase2Progress.Add(1, false)
				return nil
			}
			if len(ohlcBars) == 0 {
				if p.cfg.Debug {
					appendSample(fmt.Sprintf("%s empty_ohlc", c.Symbol()))
				}
				phase2Progress.Add(1, false)
				return nil
			}

			ohlcByDate := make(map[string][]OHLCBar)
			volumeByDate := make(map[string]int)
			countByDate := make(map[string]int)
			barsByDate := make(map[string]int)
			for _, bar := range ohlcBars {
				dateStr := bar.Timestamp.Format("2006-01-02")
				if _, ok := pendingDateSet[dateStr]; !ok {
					continue
				}
				ohlcByDate[dateStr] = append(ohlcByDate[dateStr], bar)
				volumeByDate[dateStr] += bar.Volume
				countByDate[dateStr] += bar.Count
				barsByDate[dateStr]++
			}
			if len(ohlcByDate) == 0 {
				if p.cfg.Debug {
					appendSample(fmt.Sprintf("%s ohlc_outside_pending bars=%d", c.Symbol(), len(ohlcBars)))
				}
				phase2Progress.Add(1, false)
				return nil
			}
			diagnostics.ohlcNonEmpty.Add(1)

			hasVolume := false
			hasCount := false
			hasBarsOnly := false
			for dateStr := range ohlcByDate {
				switch {
				case volumeByDate[dateStr] > 0:
					hasVolume = true
				case countByDate[dateStr] > 0:
					hasCount = true
				case barsByDate[dateStr] > 0:
					hasBarsOnly = true
				}
			}
			if hasVolume {
				diagnostics.withVolume.Add(1)
			}
			if hasCount {
				diagnostics.withCount.Add(1)
			}
			if hasBarsOnly {
				diagnostics.barsOnly.Add(1)
			}

			kept := false
			firstReason := "none"
			firstDate := ""
			firstActivity := 0
			firstVolume := 0
			firstCount := 0
			firstBars := 0
			selectionMu.Lock()
			for dateStr := range ohlcByDate {
				activity := volumeByDate[dateStr]
				reason := "volume"
				if activity == 0 {
					activity = countByDate[dateStr]
					reason = "count"
				}
				if activity == 0 {
					activity = barsByDate[dateStr]
					reason = "bars"
				}
				if firstDate == "" {
					firstDate = dateStr
					firstReason = reason
					firstActivity = activity
					firstVolume = volumeByDate[dateStr]
					firstCount = countByDate[dateStr]
					firstBars = barsByDate[dateStr]
				}
				if activity < p.cfg.MinVolume {
					continue
				}
				switch reason {
				case "volume":
					diagnostics.keptByVolume.Add(1)
				case "count":
					diagnostics.keptByCount.Add(1)
				case "bars":
					diagnostics.keptByBars.Add(1)
				}
				selectedByDate[dateStr] = append(selectedByDate[dateStr], c)
				selectedContractsMap[c.Symbol()] = c
				kept = true
			}
			if kept {
				ohlcCache.Store(c.Symbol(), &contractWeekData{Contract: c, OHLC: ohlcByDate})
			}
			selectionMu.Unlock()

			if p.cfg.Debug {
				appendSample(fmt.Sprintf("%s first_date=%s volume=%d count=%d bars=%d activity=%d reason=%s kept=%t",
					c.Symbol(), firstDate, firstVolume, firstCount, firstBars, firstActivity, firstReason, kept))
			}
			phase2Progress.Add(1, kept)
			return nil
		})
		if err != nil {
			phase2Progress.Finish("aborted")
			return fmt.Errorf("discover active week %d: %w", wi+1, err)
		}
		phase2Progress.Finish("complete")
		if p.cfg.Debug {
			log.Printf("[%s] DEBUG phase2 batch %d/%d summary: ohlc_nonempty=%d with_volume=%d with_count=%d bars_only=%d kept_contracts=%d kept_by_volume=%d kept_by_count=%d kept_by_bars=%d",
				root, wi+1, len(weeks),
				diagnostics.ohlcNonEmpty.Load(), diagnostics.withVolume.Load(), diagnostics.withCount.Load(), diagnostics.barsOnly.Load(),
				len(selectedContractsMap), diagnostics.keptByVolume.Load(), diagnostics.keptByCount.Load(), diagnostics.keptByBars.Load())
			diagnostics.sampleMu.Lock()
			sampleEntries := append([]string(nil), diagnostics.sampleEntries...)
			diagnostics.sampleMu.Unlock()
			for i, sample := range sampleEntries {
				log.Printf("[%s] DEBUG phase2 sample[%d]: %s", root, i+1, sample)
			}
		}

		var selectedContracts []Contract
		for _, c := range selectedContractsMap {
			selectedContracts = append(selectedContracts, c)
		}

		weekCache := &sync.Map{}
		for _, c := range selectedContracts {
			if cached, ok := ohlcCache.Load(c.Symbol()); ok {
				weekCache.Store(c.Symbol(), cached)
			}
		}

		phase3Progress := newPhaseProgress(root, fmt.Sprintf("Phase 3 batch %d/%d", wi+1, len(weeks)),
			"contracts", len(selectedContracts))
		phase3Progress.Start()
		err = p.parallelWork(ctx, selectedContracts, p.cfg.Workers, func(ctx context.Context, client *Client, c Contract) error {
			quotes, err := client.GetQuotes1mRange(ctx, c, weekStart, weekEnd)
			if err != nil {
				phase3Progress.Add(1, false)
				return nil
			}
			if len(quotes) == 0 {
				phase3Progress.Add(1, false)
				return nil
			}

			quotesByDate := make(map[string][]QuoteBar)
			for _, q := range quotes {
				dateStr := q.Timestamp.Format("2006-01-02")
				if _, ok := pendingDateSet[dateStr]; !ok {
					continue
				}
				quotesByDate[dateStr] = append(quotesByDate[dateStr], q)
			}
			cached, ok := weekCache.Load(c.Symbol())
			if !ok {
				phase3Progress.Add(1, false)
				return nil
			}
			cwd := cached.(*contractWeekData)
			weekCache.Store(c.Symbol(), &contractWeekData{
				Contract: c,
				Quotes:   quotesByDate,
				OHLC:     cwd.OHLC,
			})
			phase3Progress.Add(1, true)
			return nil
		})
		if err != nil {
			phase3Progress.Finish("aborted")
			return fmt.Errorf("download quotes for week %d: %w", wi+1, err)
		}
		phase3Progress.Finish("complete")

		for _, date := range pendingDates {
			dateStr := date.Format("2006-01-02")
			weekOffset++

			wasInFlight := p.progress.IsInFlight(root, date)
			hasExistingData, err := p.store.HasDateData(ctx, root, date)
			if err != nil {
				return fmt.Errorf("check existing data for %s %s: %w", root, dateStr, err)
			}

			contractsForDate := selectedByDate[dateStr]
			if err := p.progress.MarkStarted(root, date, len(contractsForDate)); err != nil {
				return fmt.Errorf("mark started for %s %s: %w", root, dateStr, err)
			}

			if hasExistingData {
				log.Printf("[%s] %s: found existing unfinished data, deleting before retry", root, dateStr)
				if err := p.store.DeleteDateData(ctx, root, date); err != nil {
					return fmt.Errorf("cleanup existing data for %s %s: %w", root, dateStr, err)
				}
			}

			if wasInFlight && !hasExistingData {
				log.Printf("[%s] %s: resuming previously interrupted date", root, dateStr)
			}

			log.Printf("[%s] [%d/%d] %s: %d contracts",
				root, weekOffset, totalDates, dateStr, len(contractsForDate))

			if len(contractsForDate) == 0 {
				if err := p.progress.MarkCompleted(root, date, DateSyncStats{}); err != nil {
					log.Printf("[%s] Warning: failed to mark empty date %s complete: %v", root, dateStr, err)
				}
				continue
			}

			stats, err := p.processDateFromCache(ctx, root, date, contractsForDate, underlyingFallback[dateStr], weekCache)
			if err != nil {
				_ = p.progress.MarkFailed(root, date, "process_date", err, stats)
				log.Printf("[%s] Error processing %s: %v (skipping)", root, dateStr, err)
				continue
			}

			if err := p.progress.MarkCompleted(root, date, stats); err != nil {
				log.Printf("[%s] Warning: failed to mark progress for %s: %v", root, dateStr, err)
			}
		}
	}

	log.Printf("[%s] Sync complete", root)
	return nil
}

// groupDatesIntoWeeks splits a sorted list of date strings into batches of up to batchSize.
func buildWeekdayBatches(startDate, endDate time.Time, batchSize int) [][]time.Time {
	if batchSize <= 0 {
		batchSize = 5
	}
	var dates []time.Time
	for date := normalizeDate(endDate); !date.Before(normalizeDate(startDate)); date = date.AddDate(0, 0, -1) {
		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			continue
		}
		dates = append(dates, date)
	}

	result := make([][]time.Time, 0, (len(dates)+batchSize-1)/batchSize)
	for i := 0; i < len(dates); i += batchSize {
		end := i + batchSize
		if end > len(dates) {
			end = len(dates)
		}
		result = append(result, dates[i:end])
	}
	return result
}

func countWeekBatchDates(weeks [][]time.Time) int {
	total := 0
	for _, week := range weeks {
		total += len(week)
	}
	return total
}

func normalizeDate(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (p *Pipeline) buildUnderlyingFallback(ctx context.Context, client *Client, root string) map[string]float64 {
	underlyingByDate := make(map[string]float64)
	stockEOD, err := client.GetStockEOD(ctx, root, p.cfg.StartDate, p.cfg.EndDate)
	if err != nil {
		log.Printf("[%s] Underlying fallback unavailable: %v", root, err)
		return underlyingByDate
	}
	for _, bar := range stockEOD {
		if bar.Close <= 0 {
			continue
		}
		underlyingByDate[bar.Timestamp.Format("2006-01-02")] = bar.Close
	}
	if len(underlyingByDate) > 0 {
		log.Printf("[%s] Loaded underlying fallback for %d dates", root, len(underlyingByDate))
	}
	return underlyingByDate
}

// processDateFromCache processes a single date using pre-downloaded data from the weekly cache.
func (p *Pipeline) processDateFromCache(ctx context.Context, root string, date time.Time,
	contracts []Contract, underlyingPrice float64,
	weekCache *sync.Map) (DateSyncStats, error) {

	stats := DateSyncStats{ExpectedContracts: len(contracts)}
	dateLabel := date.Format("2006-01-02")

	// Extract this date's data from the weekly cache
	var allData []contractDataItem

	for _, c := range contracts {
		if cached, ok := weekCache.Load(c.Symbol()); ok {
			cwd := cached.(*contractWeekData)
			quotes := cwd.Quotes[dateLabel]
			ohlc := cwd.OHLC[dateLabel]
			if len(quotes) > 0 {
				allData = append(allData, contractDataItem{Contract: c, Quotes: quotes, OHLC: ohlc})
			}
		}
	}

	if len(allData) == 0 {
		return stats, fmt.Errorf("no intraday data for %s", dateLabel)
	}
	stats.DownloadedContracts = len(allData)

	return p.assembleAndStore(ctx, root, date, allData, underlyingPrice, stats)
}

// processDate downloads and processes all 1m data for a single date (legacy single-date path).
func (p *Pipeline) processDate(ctx context.Context, root string, date time.Time,
	contracts []Contract, underlyingPrice float64) (DateSyncStats, error) {
	stats := DateSyncStats{ExpectedContracts: len(contracts)}
	dateLabel := date.Format("2006-01-02")
	log.Printf("[%s] [%s] Intraday download: %d contracts queued", root, dateLabel, len(contracts))

	// Download 1m quotes and OHLC for all contracts
	var allData []contractDataItem
	var dataMu sync.Mutex
	phase3Progress := newPhaseProgress(root, "Phase 3", "contracts", len(contracts))
	phase3Progress.Start()

	err := p.parallelWork(ctx, contracts, p.cfg.Workers, func(ctx context.Context, client *Client, c Contract) error {
		quotes, err := client.GetQuotes1m(ctx, c, date)
		if err != nil {
			phase3Progress.Add(1, false)
			return nil // Non-fatal
		}
		ohlc, err := client.GetOHLC1m(ctx, c, date)
		if err != nil {
			ohlc = nil // Non-fatal
		}
		if len(quotes) > 0 {
			dataMu.Lock()
			allData = append(allData, contractDataItem{Contract: c, Quotes: quotes, OHLC: ohlc})
			dataMu.Unlock()
			phase3Progress.Add(1, true)
			return nil
		}
		phase3Progress.Add(1, false)
		return nil
	})
	if err != nil {
		phase3Progress.Finish("aborted")
		return stats, err
	}
	phase3Progress.Finish("download complete")

	if len(allData) == 0 {
		return stats, fmt.Errorf("no intraday data downloaded")
	}
	stats.DownloadedContracts = len(allData)

	return p.assembleAndStore(ctx, root, date, allData, underlyingPrice, stats)
}

// contractDataItem holds quotes and OHLC for a single contract (used by both processDate and processDateFromCache).
type contractDataItem struct {
	Contract Contract
	Quotes   []QuoteBar
	OHLC     []OHLCBar
}

// contractWeekData holds a contract's downloaded data indexed by date for weekly batch processing.
type contractWeekData struct {
	Contract Contract
	Quotes   map[string][]QuoteBar // dateStr → bars
	OHLC     map[string][]OHLCBar  // dateStr → bars
}

// assembleAndStore computes Greeks, assembles Bar1m records, and stores to ClickHouse.
func (p *Pipeline) assembleAndStore(ctx context.Context, root string, date time.Time,
	allData []contractDataItem, underlyingPrice float64,
	stats DateSyncStats) (DateSyncStats, error) {

	dateLabel := date.Format("2006-01-02")

	// Build minute-level index: timestamp → contracts with quotes at that minute
	type minuteQuote struct {
		Contract Contract
		Quote    QuoteBar
		OHLC     *OHLCBar // may be nil
	}
	minuteIndex := make(map[int64][]minuteQuote) // key: unix timestamp

	ohlcIndex := make(map[string]map[int64]OHLCBar) // contract.Symbol() → timestamp → bar
	for i := range allData {
		cd := &allData[i]
		if cd.OHLC != nil {
			ohlcMap := make(map[int64]OHLCBar)
			for _, bar := range cd.OHLC {
				ohlcMap[bar.Timestamp.Unix()] = bar
			}
			ohlcIndex[cd.Contract.Symbol()] = ohlcMap
		}

		for _, q := range cd.Quotes {
			ts := q.Timestamp.Unix()
			var ohlcPtr *OHLCBar
			if ohlcMap, ok := ohlcIndex[cd.Contract.Symbol()]; ok {
				if bar, ok := ohlcMap[ts]; ok {
					ohlcPtr = &bar
				}
			}
			minuteIndex[ts] = append(minuteIndex[ts], minuteQuote{
				Contract: cd.Contract,
				Quote:    q,
				OHLC:     ohlcPtr,
			})
		}
	}

	// For each minute: compute forward price per expiration using put-call parity
	type forwardKey struct {
		Timestamp  int64
		Expiration string
	}
	forwardCache := make(map[forwardKey]ForwardInfo)

	for ts, quotes := range minuteIndex {
		// Group by expiration
		byExp := make(map[string][]minuteQuote)
		for _, mq := range quotes {
			expKey := mq.Contract.Expiration.Format("2006-01-02")
			byExp[expKey] = append(byExp[expKey], mq)
		}

		for expKey, expQuotes := range byExp {
			callMids := make(map[float64]float64)
			putMids := make(map[float64]float64)

			for _, mq := range expQuotes {
				mid := (mq.Quote.Bid + mq.Quote.Ask) / 2
				if mid <= 0 || mq.Quote.Bid <= 0 || mq.Quote.Ask <= 0 {
					continue
				}
				// Filter wide spreads: spread/mid > 50%
				spread := mq.Quote.Ask - mq.Quote.Bid
				if spread/mid > 0.5 {
					continue
				}

				if mq.Contract.Right == "C" {
					callMids[mq.Contract.Strike] = mid
				} else {
					putMids[mq.Contract.Strike] = mid
				}
			}

			exp, _ := time.Parse("2006-01-02", expKey)
			T := exp.Sub(time.Unix(ts, 0)).Hours() / (365.25 * 24)
			if T <= 0 {
				continue
			}

			fwd, err := ForwardFromParity(callMids, putMids, T)
			if err != nil {
				// Fallback to root-level underlying EOD when parity is unavailable.
				if underlyingPrice > 0 {
					fwd = ForwardInfo{
						Forward:        underlyingPrice,
						DiscountFactor: 1.0,
						Rate:           0.05,
					}
				} else {
					continue
				}
			}

			forwardCache[forwardKey{Timestamp: ts, Expiration: expKey}] = fwd
		}
	}

	// Assemble Bar1m records
	var bars []cryptooptions.Bar1m
	var symbols []cryptooptions.SymbolMeta
	symbolsSeen := make(map[uint32]bool)

	// Also collect underlying price bars (one per minute)
	spotBarMap := make(map[int64]float32) // ts → forward price (as spot proxy)

	for _, cd := range allData {
		c := cd.Contract
		symID := c.SymbolID()
		sym := c.Symbol()

		if !symbolsSeen[symID] {
			symbolsSeen[symID] = true
			symbols = append(symbols, cryptooptions.SymbolMeta{
				SymbolID:        symID,
				Symbol:          sym,
				BaseAsset:       c.Root,
				OptionType:      map[string]string{"C": "call", "P": "put"}[c.Right],
				StrikePrice:     float32(c.Strike),
				Expiration:      c.Expiration,
				UnderlyingIndex: c.Root,
			})
		}

		for _, q := range cd.Quotes {
			ts := q.Timestamp.Unix()
			expKey := c.Expiration.Format("2006-01-02")

			fwd, hasFwd := forwardCache[forwardKey{Timestamp: ts, Expiration: expKey}]

			// Mark price = mid of bid/ask
			markMid := float32((q.Bid + q.Ask) / 2)
			bidPrice := float32(q.Bid)
			askPrice := float32(q.Ask)

			// OHLC from trade data
			var lastOpen, lastHigh, lastLow, lastClose float32
			var tickCount uint16
			if ohlcMap, ok := ohlcIndex[sym]; ok {
				if bar, ok := ohlcMap[ts]; ok {
					lastOpen = float32(bar.Open)
					lastHigh = float32(bar.High)
					lastLow = float32(bar.Low)
					lastClose = float32(bar.Close)
					tickCount = uint16(bar.Count)
				}
			}

			// Compute Greeks if we have forward info
			var markIV, bidIV, askIV float32
			var delta, gamma, vega, theta, rho float32

			if hasFwd && fwd.Forward > 0 {
				T := c.Expiration.Sub(q.Timestamp).Hours() / (365.25 * 24)
				isCall := c.Right == "C"

				if T > 0 && markMid > 0 {
					if iv, err := ImpliedVol(float64(markMid), fwd.Forward, c.Strike, T, fwd.DiscountFactor, isCall); err == nil {
						markIV = float32(iv)
						g := ComputeGreeks(fwd.Forward, c.Strike, T, iv, fwd.DiscountFactor, fwd.Rate, isCall)
						delta = float32(g.Delta)
						gamma = float32(g.Gamma)
						vega = float32(g.Vega)
						theta = float32(g.Theta)
						rho = float32(g.Rho)
					}

					if bidPrice > 0 {
						if iv, err := ImpliedVol(float64(bidPrice), fwd.Forward, c.Strike, T, fwd.DiscountFactor, isCall); err == nil {
							bidIV = float32(iv)
						}
					}
					if askPrice > 0 {
						if iv, err := ImpliedVol(float64(askPrice), fwd.Forward, c.Strike, T, fwd.DiscountFactor, isCall); err == nil {
							askIV = float32(iv)
						}
					}
				}

				if _, exists := spotBarMap[ts]; !exists {
					spotBarMap[ts] = float32(fwd.Forward)
				}
			}

			bar := cryptooptions.Bar1m{
				Timestamp: q.Timestamp,
				SymbolID:  symID,
				BaseAsset: c.Root,

				MarkOpen: markMid, MarkHigh: markMid, MarkLow: markMid, MarkClose: markMid,
				LastOpen: lastOpen, LastHigh: lastHigh, LastLow: lastLow, LastClose: lastClose,
				BidOpen: bidPrice, BidHigh: bidPrice, BidLow: bidPrice, BidClose: bidPrice,
				AskOpen: askPrice, AskHigh: askPrice, AskLow: askPrice, AskClose: askPrice,

				MarkIVOpen: markIV, MarkIVClose: markIV,
				BidIVOpen: bidIV,
				AskIVOpen: askIV,

				Delta: delta, Gamma: gamma, Vega: vega, Theta: theta, Rho: rho,

				OpenInterest: 0,
				TickCount:    tickCount,
			}
			bars = append(bars, bar)
		}
	}
	stats.ExpectedBars = len(bars)
	if err := p.progress.MarkDownloadProgress(root, date, stats.DownloadedContracts, stats.ExpectedBars); err != nil {
		log.Printf("[%s] Warning: mark download progress for %s: %v", root, date.Format("2006-01-02"), err)
	}

	// Insert into ClickHouse
	if len(symbols) > 0 {
		if err := p.store.InsertSymbols(ctx, symbols); err != nil {
			log.Printf("[%s] Warning: insert symbols: %v", root, err)
		}
	}

	if len(bars) > 0 {
		// Insert in batches of 50000
		for i := 0; i < len(bars); i += 50000 {
			end := i + 50000
			if end > len(bars) {
				end = len(bars)
			}
			if err := p.store.InsertBars(ctx, bars[i:end]); err != nil {
				return stats, fmt.Errorf("insert bars batch: %w", err)
			}
		}
	}

	// Insert spot bars (underlying price proxy from forward)
	var spotBars []cryptooptions.SpotBar1m
	for ts, fwdPrice := range spotBarMap {
		spotBars = append(spotBars, cryptooptions.SpotBar1m{
			Timestamp:   time.Unix(ts, 0).UTC(),
			Symbol:      root,
			PriceSource: "parity_forward",
			Open:        fwdPrice,
			High:        fwdPrice,
			Low:         fwdPrice,
			Close:       fwdPrice,
			TickCount:   1,
		})
	}
	stats.ExpectedSpotBars = len(spotBars)
	if len(spotBars) > 0 {
		if err := p.store.InsertSpotBars(ctx, spotBars); err != nil {
			log.Printf("[%s] Warning: insert spot bars: %v", root, err)
		}
	}

	storedBars, storedSpotBars, err := p.store.CountDateData(ctx, root, date)
	if err != nil {
		return stats, fmt.Errorf("count stored date data: %w", err)
	}
	stats.StoredBars = storedBars
	stats.StoredSpotBars = storedSpotBars
	if stats.StoredBars != stats.ExpectedBars {
		return stats, fmt.Errorf("stored option bars mismatch: expected=%d actual=%d", stats.ExpectedBars, stats.StoredBars)
	}
	if stats.StoredSpotBars != stats.ExpectedSpotBars {
		return stats, fmt.Errorf("stored spot bars mismatch: expected=%d actual=%d", stats.ExpectedSpotBars, stats.StoredSpotBars)
	}

	log.Printf("[%s] [%s] Storage verification: option bars %d/%d, spot bars %d/%d",
		root, dateLabel, stats.StoredBars, stats.ExpectedBars, stats.StoredSpotBars, stats.ExpectedSpotBars)
	log.Printf("[%s] %s: inserted %d bars, %d symbols, %d spot bars",
		root, date.Format("2006-01-02"), len(bars), len(symbols), len(spotBars))

	return stats, nil
}

// parallelWork processes items concurrently with worker pool.
// Each worker gets its own MCP client connection.
func (p *Pipeline) parallelWork(ctx context.Context, contracts []Contract, workers int,
	fn func(ctx context.Context, client *Client, c Contract) error) error {

	if workers < 1 {
		workers = 1
	}
	if workers > len(contracts) {
		workers = len(contracts)
	}

	ch := make(chan Contract, len(contracts))
	for _, c := range contracts {
		ch <- c
	}
	close(ch)

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			client, err := p.newClient(ctx)
			if err != nil {
				errCh <- fmt.Errorf("worker %d: create client: %w", workerID, err)
				return
			}
			defer client.Close()
			defer client.mcp.Close()

			for c := range ch {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if err := fn(ctx, client, c); err != nil {
					log.Printf("Worker %d: error processing %s: %v", workerID, c.Symbol(), err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

// newClient creates a new Theta Data API client with its own MCP connection.
func (p *Pipeline) newClient(ctx context.Context) (*Client, error) {
	mcp := NewMCPClient(p.cfg.MCPURL)
	if err := mcp.Connect(ctx); err != nil {
		return nil, err
	}
	return NewClient(mcp, p.cfg.RateLimit), nil
}

// symbolToContract parses a symbol string back into a Contract.
// Format: ROOT-YYYYMMDD-STRIKE-RIGHT
func symbolToContract(root, sym string) *Contract {
	// Find the root prefix and strip it
	if len(sym) <= len(root)+1 {
		return nil
	}
	rest := sym[len(root)+1:] // skip "ROOT-"

	// Parse YYYYMMDD-STRIKE-RIGHT
	parts := splitFromRight(rest, "-", 2)
	if len(parts) != 3 {
		return nil
	}

	exp, err := time.Parse("20060102", parts[0])
	if err != nil {
		return nil
	}

	var strike float64
	if _, err := fmt.Sscanf(parts[1], "%f", &strike); err != nil {
		return nil
	}

	right := parts[2]
	if right != "C" && right != "P" {
		return nil
	}

	return &Contract{
		Root:       root,
		Expiration: exp,
		Strike:     strike,
		Right:      right,
	}
}

// splitFromRight splits string s by sep, taking the last n parts.
// Returns [prefix, part1, ..., partN].
func splitFromRight(s, sep string, n int) []string {
	var result []string
	for i := 0; i < n; i++ {
		idx := strings.LastIndex(s, sep)
		if idx < 0 {
			return nil
		}
		result = append([]string{s[idx+len(sep):]}, result...)
		s = s[:idx]
	}
	result = append([]string{s}, result...)
	return result
}
