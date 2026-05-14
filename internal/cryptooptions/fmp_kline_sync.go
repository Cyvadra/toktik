package cryptooptions

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

// FMPSpotPriceSource is the value written to crypto_spot_bar_1m.price_source
// for bars sourced from FMP's intraday endpoint.
const FMPSpotPriceSource = "fmp-1min"

// FMPSpotSyncConfig configures a single-symbol FMP crypto spot sync.
//
// Symbol uses FMP's pair convention (e.g. "BTCUSD", "ETHUSD"). The base asset
// (without the USD quote) is what gets stored in crypto_spot_bar_1m.symbol so
// downstream queries match what other importers use.
type FMPSpotSyncConfig struct {
	APIKey      string
	Symbols     []string  // FMP pair symbols, e.g. ["BTCUSD","ETHUSD"]
	From        time.Time // inclusive UTC date
	To          time.Time // inclusive UTC date
	Interval    fmp.IntradayInterval
	BatchSize   int
	PriceSource string // optional override; defaults to FMPSpotPriceSource
	DryRun      bool
	Replace     bool
}

// FMPSpotSyncResult summarises the sync.
type FMPSpotSyncResult struct {
	ProcessedSymbols int
	FailedSymbols    int
	FetchedBars      int64
	InsertedRows     int64
	ThrottledSymbols []string
}

// SyncFMPSpotBars fetches FMP intraday bars for each requested crypto pair and
// inserts them into crypto_spot_bar_1m. The current implementation walks
// symbols sequentially (FMP free tier is single-stream rate-limited) and slices
// the date range into 30-day chunks because FMP caps a single intraday call to
// ~30 days.
func SyncFMPSpotBars(ctx context.Context, conn driver.Conn, cfg FMPSpotSyncConfig) (FMPSpotSyncResult, error) {
	if cfg.APIKey == "" {
		return FMPSpotSyncResult{}, fmt.Errorf("FMP API key is required")
	}
	if len(cfg.Symbols) == 0 {
		return FMPSpotSyncResult{}, fmt.Errorf("at least one symbol is required")
	}
	if cfg.From.IsZero() || cfg.To.IsZero() || cfg.To.Before(cfg.From) {
		return FMPSpotSyncResult{}, fmt.Errorf("from/to must be set and to >= from")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}
	priceSource := cfg.PriceSource
	if priceSource == "" {
		priceSource = FMPSpotPriceSource
	}

	client := fmp.New(cfg.APIKey)
	var result FMPSpotSyncResult
	throttled := make(map[string]struct{})

	for _, raw := range cfg.Symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			continue
		}
		baseAsset := strings.TrimSuffix(symbol, "USD")
		if cfg.Replace && !cfg.DryRun {
			exclusiveTo := cfg.To.AddDate(0, 0, 1)
			if err := DeleteSpotBarsScope(ctx, conn, baseAsset, cfg.From, exclusiveTo); err != nil {
				log.Printf("[ERROR] %s: delete existing spot rows for replace: %v", symbol, err)
				result.FailedSymbols++
				continue
			}
			log.Printf("[REPLACE] %s: cleared spot rows and aggregates for %s..%s", symbol, cfg.From.Format("2006-01-02"), cfg.To.Format("2006-01-02"))
		}

		bars, err := fetchFMPIntradayChunked(ctx, client, symbol, cfg.Interval, cfg.From, cfg.To)
		if err != nil {
			log.Printf("[ERROR] %s: fetch FMP intraday: %v", symbol, err)
			if fmp.IsHTTPStatus(err, http.StatusTooManyRequests) {
				throttled[symbol] = struct{}{}
			}
			result.FailedSymbols++
			continue
		}
		result.FetchedBars += int64(len(bars))

		if cfg.DryRun || len(bars) == 0 {
			result.ProcessedSymbols++
			log.Printf("[%s] %s: fetched=%d inserted=0 dry_run=%v", "DRYRUN-OR-EMPTY", symbol, len(bars), cfg.DryRun)
			continue
		}

		ch := make(chan SpotBar1m, 1024)
		go func(bars []fmp.IntradayBar) {
			defer close(ch)
			for _, bar := range bars {
				ts, ok := parseFMPIntradayTimestamp(bar.Date)
				if !ok {
					continue
				}
				ch <- SpotBar1m{
					Timestamp:   ts,
					Symbol:      baseAsset,
					PriceSource: priceSource,
					Open:        float32(bar.Open),
					High:        float32(bar.High),
					Low:         float32(bar.Low),
					Close:       float32(bar.Close),
					Volume:      bar.Volume,
					TickCount:   0,
					VolumeBase:  bar.Volume,
					VolumeQuote: 0,
					BarInterval: string(cfg.Interval),
				}
			}
		}(bars)

		inserted, err := InsertSpotBars(ctx, conn, ch, cfg.BatchSize)
		if err != nil {
			log.Printf("[ERROR] %s: insert spot bars: %v", symbol, err)
			result.FailedSymbols++
			continue
		}
		result.ProcessedSymbols++
		result.InsertedRows += inserted
		log.Printf("[OK] %s: fetched=%d inserted=%d", symbol, len(bars), inserted)
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

// fetchFMPIntradayChunked walks the requested date range in 30-day chunks
// because FMP caps a single intraday call window. Each chunk's bars are
// concatenated; the caller does not need to deduplicate because chunks are
// non-overlapping.
func fetchFMPIntradayChunked(ctx context.Context, client *fmp.Client, symbol string, interval fmp.IntradayInterval, from, to time.Time) ([]fmp.IntradayBar, error) {
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

// parseFMPIntradayTimestamp parses FMP's intraday "YYYY-MM-DD HH:MM:SS" format.
// FMP returns UTC for crypto and forex (and ET for US stocks; callers using
// this for US stocks must convert separately).
func parseFMPIntradayTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
