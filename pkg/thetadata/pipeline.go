package thetadata

import (
	"context"
	"fmt"
	"log"
	"sort"
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

	// Create one MCP client for metadata queries
	metaClient, err := p.newClient(ctx)
	if err != nil {
		return fmt.Errorf("create meta client: %w", err)
	}
	defer metaClient.Close()
	defer metaClient.mcp.Close()

	// Phase 1: Enumerate the contract universe
	log.Printf("[%s] Phase 1: Enumerating contracts...", root)
	expirations, err := metaClient.ListExpirations(ctx, root)
	if err != nil {
		return fmt.Errorf("list expirations: %w", err)
	}

	// Filter expirations to those relevant for our date range
	var relevantExps []time.Time
	for _, exp := range expirations {
		// An expiration is relevant if it hasn't expired before our start date
		if !exp.Before(p.cfg.StartDate) {
			relevantExps = append(relevantExps, exp)
		}
	}
	log.Printf("[%s] Found %d total expirations, %d relevant for date range",
		root, len(expirations), len(relevantExps))

	// Build contract universe: exp → strikes
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
		if len(strikes) > 0 {
			universe = append(universe, expStrikes{Exp: exp, Strikes: strikes})
			phase1Progress.Add(1, true)
		} else {
			phase1Progress.Add(1, false)
		}
	}
	phase1Progress.Finish("complete")
	log.Printf("[%s] Contract universe: %d expirations with strikes", root, len(universe))

	// Phase 2: Fetch EOD Greeks for all contracts (bulk date range)
	// This gives us daily volume, underlying price, and Greeks for validation.
	log.Printf("[%s] Phase 2: Fetching EOD Greeks for volume filtering...", root)
	type contractEOD struct {
		Contract Contract
		EOD      []GreeksEOD
	}

	// Collect all contracts
	var allContracts []Contract
	for _, es := range universe {
		for _, strike := range es.Strikes {
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
	log.Printf("[%s] Total contracts: %d", root, len(allContracts))
	phase2Progress := newPhaseProgress(root, "Phase 2", "contracts", len(allContracts))
	phase2Progress.Start()

	// Fetch EOD data concurrently
	eodMap := make(map[string][]GreeksEOD) // key: contract.Symbol()
	var eodMu sync.Mutex

	err = p.parallelWork(ctx, allContracts, p.cfg.Workers, func(ctx context.Context, client *Client, c Contract) error {
		eod, err := client.GetGreeksEOD(ctx, c, p.cfg.StartDate, p.cfg.EndDate)
		if err != nil {
			// Non-fatal: contract may not have data in this range
			phase2Progress.Add(1, false)
			return nil
		}
		if len(eod) > 0 {
			eodMu.Lock()
			eodMap[c.Symbol()] = eod
			eodMu.Unlock()
			phase2Progress.Add(1, true)
			return nil
		}
		phase2Progress.Add(1, false)
		return nil
	})
	if err != nil {
		phase2Progress.Finish("aborted")
		return fmt.Errorf("fetch EOD data: %w", err)
	}
	phase2Progress.Finish("complete")
	log.Printf("[%s] EOD data fetched for %d contracts", root, len(eodMap))

	// Build daily volume index: date → set of contracts with volume >= minVolume
	type dateContracts struct {
		Date            time.Time
		Contracts       []Contract
		UnderlyingPrice float64 // from EOD
	}
	dateIndex := make(map[string]*dateContracts) // key: "YYYY-MM-DD"

	for sym, eods := range eodMap {
		for _, eod := range eods {
			if eod.Volume < p.cfg.MinVolume {
				continue
			}
			dateStr := eod.Date.Format("2006-01-02")
			dc, ok := dateIndex[dateStr]
			if !ok {
				dc = &dateContracts{Date: eod.Date}
				dateIndex[dateStr] = dc
			}
			// Recover contract from symbol
			c := symbolToContract(root, sym)
			if c != nil {
				dc.Contracts = append(dc.Contracts, *c)
			}
			if eod.UnderlyingPrice > 0 && dc.UnderlyingPrice == 0 {
				dc.UnderlyingPrice = eod.UnderlyingPrice
			}
		}
	}

	// Phase 3: Download 1m data for each date (reverse chronological)
	log.Printf("[%s] Phase 3: Downloading 1m data for %d dates with active contracts...",
		root, len(dateIndex))

	// Sort dates in reverse chronological order
	var dates []string
	for d := range dateIndex {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	for i, dateStr := range dates {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		date, _ := time.Parse("2006-01-02", dateStr)

		if p.progress.IsCompleted(root, date) {
			continue
		}

		wasInFlight := p.progress.IsInFlight(root, date)

		hasExistingData, err := p.store.HasDateData(ctx, root, date)
		if err != nil {
			return fmt.Errorf("check existing data for %s %s: %w", root, dateStr, err)
		}

		if err := p.progress.MarkStarted(root, date, len(dateIndex[dateStr].Contracts)); err != nil {
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

		dc := dateIndex[dateStr]
		log.Printf("[%s] [%d/%d] %s: %d contracts",
			root, i+1, len(dates), dateStr, len(dc.Contracts))

		stats, err := p.processDate(ctx, root, date, dc.Contracts, dc.UnderlyingPrice, eodMap)
		if err != nil {
			_ = p.progress.MarkFailed(root, date, "process_date", err, stats)
			log.Printf("[%s] Error processing %s: %v (skipping)", root, dateStr, err)
			continue
		}

		if err := p.progress.MarkCompleted(root, date, stats); err != nil {
			log.Printf("[%s] Warning: failed to mark progress for %s: %v", root, dateStr, err)
		}
	}

	log.Printf("[%s] Sync complete", root)
	return nil
}

// processDate downloads and processes all 1m data for a single date.
func (p *Pipeline) processDate(ctx context.Context, root string, date time.Time,
	contracts []Contract, eodUnderlyingPrice float64, eodMap map[string][]GreeksEOD) (DateSyncStats, error) {
	stats := DateSyncStats{ExpectedContracts: len(contracts)}
	dateLabel := date.Format("2006-01-02")
	log.Printf("[%s] [%s] Intraday download: %d contracts queued", root, dateLabel, len(contracts))

	// Download 1m quotes and OHLC for all contracts
	type contractData struct {
		Contract Contract
		Quotes   []QuoteBar
		OHLC     []OHLCBar
	}
	var allData []contractData
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
			allData = append(allData, contractData{Contract: c, Quotes: quotes, OHLC: ohlc})
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
				// Fallback to EOD underlying price
				if eodUnderlyingPrice > 0 {
					fwd = ForwardInfo{
						Forward:        eodUnderlyingPrice,
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

		// Get OI from EOD
		var dailyOI float32
		if eods, ok := eodMap[sym]; ok {
			for _, eod := range eods {
				if eod.Date.Format("2006-01-02") == date.Format("2006-01-02") {
					dailyOI = float32(eod.OpenInterest)
					break
				}
			}
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

				OpenInterest: dailyOI,
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
