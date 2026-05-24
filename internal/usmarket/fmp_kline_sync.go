package usmarket

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

// FMPStockKlineSyncConfig configures sync of US-stock 1-minute bars from FMP
// into us_stocks_bar_1m. Note FMP intraday timestamps for US equities are
// reported in America/New_York (ET); this helper handles ET→UTC conversion and
// session classification using the existing SessionMap pipeline.
type FMPStockKlineSyncConfig struct {
	APIKey    string
	Targets   []FMPStockSyncTarget
	From      time.Time // inclusive UTC date
	To        time.Time // inclusive UTC date
	Interval  fmp.IntradayInterval
	BatchSize int
	DryRun    bool
	// Replace, when true, deletes existing us_stocks_bar_1m rows for each target
	// symbol within [From, To] before inserting fresh FMP data. This ensures
	// idempotent re-imports without accumulating duplicate 1m rows.
	Replace bool
}

// FMPStockKlineSyncResult summarises a sync run.
type FMPStockKlineSyncResult struct {
	ProcessedSymbols int
	FailedSymbols    int
	FetchedBars      int64
	InsertedRows     int64
	ThrottledSymbols []string
}

// SyncFMPStockKlines fetches FMP intraday bars per symbol and inserts them
// into us_stocks_bar_1m, computing session metadata locally via SessionMap.
// Symbols are processed sequentially because FMP's intraday endpoint is
// rate-limited per IP/key.
func SyncFMPStockKlines(ctx context.Context, conn driver.Conn, cfg FMPStockKlineSyncConfig) (FMPStockKlineSyncResult, error) {
	if cfg.APIKey == "" {
		return FMPStockKlineSyncResult{}, fmt.Errorf("FMP API key is required")
	}
	if len(cfg.Targets) == 0 {
		return FMPStockKlineSyncResult{}, fmt.Errorf("at least one target is required")
	}
	if cfg.From.IsZero() || cfg.To.IsZero() || cfg.To.Before(cfg.From) {
		return FMPStockKlineSyncResult{}, fmt.Errorf("from/to must be set and to >= from")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}

	// Build session map covering [from.year .. to.year]. Generated entirely
	// locally so we don't have to seed us_equity_sessions just to backfill.
	sessions := buildSessionMap(cfg.From.Year(), cfg.To.Year())

	client := fmp.New(cfg.APIKey)
	var result FMPStockKlineSyncResult
	throttled := make(map[string]struct{})
	targets := NormalizeUSStockSyncTargets(cfg.Targets)

	for _, target := range targets {
		storeSymbol := strings.ToUpper(strings.TrimSpace(target.StoreSymbol))
		fetchSymbol := strings.ToUpper(strings.TrimSpace(target.FetchSymbol))
		if storeSymbol == "" {
			continue
		}
		if fetchSymbol == "" {
			fetchSymbol = storeSymbol
		}
		if cfg.Replace && !cfg.DryRun {
			// to is the inclusive end date; make it exclusive for the delete scope
			exclusiveTo := cfg.To.AddDate(0, 0, 1)
			if err := DeleteStockBarsSymbolScope(ctx, conn, storeSymbol, cfg.From, exclusiveTo); err != nil {
				log.Printf("[ERROR] %s: delete existing 1m rows for replace: %v", storeSymbol, err)
				result.FailedSymbols++
				continue
			}
			log.Printf("[REPLACE] %s: cleared 1m rows for %s..%s", storeSymbol, cfg.From.Format("2006-01-02"), cfg.To.Format("2006-01-02"))
		}

		bars, err := fetchFMPIntradayChunkedStock(ctx, client, fetchSymbol, cfg.Interval, cfg.From, cfg.To)
		if err != nil {
			log.Printf("[ERROR] %s <= %s: fetch FMP intraday (%s): %v", storeSymbol, fetchSymbol, target.Source, err)
			if fmp.IsHTTPStatus(err, http.StatusTooManyRequests) {
				throttled[storeSymbol] = struct{}{}
			}
			result.FailedSymbols++
			continue
		}
		result.FetchedBars += int64(len(bars))

		if cfg.DryRun || len(bars) == 0 {
			result.ProcessedSymbols++
			log.Printf("[%s] %s <= %s: fetched=%d inserted=0 dry_run=%v source=%s", "DRYRUN-OR-EMPTY", storeSymbol, fetchSymbol, len(bars), cfg.DryRun, target.Source)
			continue
		}

		ch := make(chan StockBar1m, 1024)
		go func(bars []fmp.IntradayBar) {
			defer close(ch)
			for _, bar := range bars {
				tsUTC, ok := parseFMPStockIntradayTimestamp(bar.Date)
				if !ok {
					continue
				}
				stockBar := StockBar1m{
					Timestamp:    tsUTC,
					Symbol:       storeSymbol,
					Open:         float32(bar.Open),
					High:         float32(bar.High),
					Low:          float32(bar.Low),
					Close:        float32(bar.Close),
					Volume:       bar.Volume,
					Transactions: 0,
				}
				stockBar.MarketDate, stockBar.SessionKind, stockBar.IsRegularSession,
					stockBar.SessionOpen, stockBar.SessionSeq = sessions.ClassifyTimestamp(tsUTC)
				ch <- stockBar
			}
		}(bars)

		inserted, err := InsertStockBars(ctx, conn, ch, cfg.BatchSize)
		if err != nil {
			log.Printf("[ERROR] %s <= %s: insert stock bars (%s): %v", storeSymbol, fetchSymbol, target.Source, err)
			result.FailedSymbols++
			continue
		}
		result.ProcessedSymbols++
		result.InsertedRows += inserted
		log.Printf("[OK] %s <= %s: fetched=%d inserted=%d source=%s", storeSymbol, fetchSymbol, len(bars), inserted, target.Source)
	}
	if len(throttled) > 0 {
		result.ThrottledSymbols = make([]string, 0, len(throttled))
		for symbol := range throttled {
			result.ThrottledSymbols = append(result.ThrottledSymbols, symbol)
		}
		sort.Strings(result.ThrottledSymbols)
	}
	return result, nil
}

// buildSessionMap generates a SessionMap in-process from the embedded calendar
// rules; avoids depending on us_equity_sessions being seeded.
func buildSessionMap(startYear, endYear int) SessionMap {
	if endYear < startYear {
		endYear = startYear
	}
	sessions := GenerateSessionCalendar(startYear, endYear)
	m := make(SessionMap, len(sessions))
	for _, s := range sessions {
		m[s.MarketDate.Format("2006-01-02")] = s
	}
	return m
}

// fetchFMPIntradayChunkedStock walks the requested date range in 30-day
// chunks (FMP intraday endpoint window cap).
func fetchFMPIntradayChunkedStock(ctx context.Context, client *fmp.Client, symbol string, interval fmp.IntradayInterval, from, to time.Time) ([]fmp.IntradayBar, error) {
	const chunkDays = 30
	var all []fmp.IntradayBar
	cursor := from
	for !cursor.After(to) {
		chunkEnd := cursor.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		bars, err := client.IntradayPrices(ctx, symbol, interval,
			cursor.Format("2006-01-02"),
			chunkEnd.Format("2006-01-02"),
		)
		if err != nil {
			return nil, fmt.Errorf("FMP intraday %s [%s..%s]: %w", symbol,
				cursor.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), err)
		}
		all = append(all, bars...)
		cursor = chunkEnd.AddDate(0, 0, 1)
	}
	return all, nil
}

// parseFMPStockIntradayTimestamp parses FMP's "YYYY-MM-DD HH:MM:SS" (ET)
// stock-intraday timestamp and returns it converted to UTC.
func parseFMPStockIntradayTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, newYorkLocation)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
