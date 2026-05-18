package forexmarket

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

// FMPKlineSyncConfig configures sync of FMP forex 1-minute bars into
// forex_bar_1m. FMP returns forex intraday timestamps in UTC.
type FMPKlineSyncConfig struct {
	APIKey    string
	Symbols   []string
	From      time.Time
	To        time.Time
	Interval  fmp.IntradayInterval
	BatchSize int
	DryRun    bool
	Replace   bool
}

// FMPKlineSyncResult summarises a sync run.
type FMPKlineSyncResult struct {
	ProcessedSymbols int
	FailedSymbols    int
	FetchedBars      int64
	InsertedRows     int64
	ThrottledSymbols []string
}

var fetchFMPIntradayBars = fetchFMPIntradayChunked
var deleteBarsForSymbolScope = DeleteBarsSymbolScope
var insertForexBars = InsertBars

// SyncFMPKlines fetches FMP intraday bars per symbol and inserts them into
// forex_bar_1m. Symbols are processed sequentially to stay within provider
// rate limits.
func SyncFMPKlines(ctx context.Context, conn driver.Conn, cfg FMPKlineSyncConfig) (FMPKlineSyncResult, error) {
	if cfg.APIKey == "" {
		return FMPKlineSyncResult{}, fmt.Errorf("FMP API key is required")
	}
	if len(cfg.Symbols) == 0 {
		return FMPKlineSyncResult{}, fmt.Errorf("at least one symbol is required")
	}
	if cfg.From.IsZero() || cfg.To.IsZero() || cfg.To.Before(cfg.From) {
		return FMPKlineSyncResult{}, fmt.Errorf("from/to must be set and to >= from")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50000
	}
	if cfg.Interval == "" {
		cfg.Interval = fmp.Interval1Min
	}

	client := fmp.New(cfg.APIKey)
	var result FMPKlineSyncResult
	throttled := make(map[string]struct{})

	for _, raw := range cfg.Symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			continue
		}

		bars, err := fetchFMPIntradayBars(ctx, client, symbol, cfg.Interval, cfg.From, cfg.To)
		if err != nil {
			log.Printf("[ERROR] %s: fetch FMP intraday: %v", symbol, err)
			if fmp.IsHTTPStatus(err, http.StatusTooManyRequests) {
				throttled[symbol] = struct{}{}
			}
			result.FailedSymbols++
			continue
		}
		result.FetchedBars += int64(len(bars))

		if cfg.Replace && !cfg.DryRun {
			exclusiveTo := cfg.To.AddDate(0, 0, 1)
			if err := deleteBarsForSymbolScope(ctx, conn, symbol, cfg.From, exclusiveTo); err != nil {
				log.Printf("[ERROR] %s: delete existing forex rows for replace: %v", symbol, err)
				result.FailedSymbols++
				continue
			}
			log.Printf("[REPLACE] %s: cleared forex rows and aggregates for %s..%s", symbol, cfg.From.Format("2006-01-02"), cfg.To.Format("2006-01-02"))
		}

		if cfg.DryRun || len(bars) == 0 {
			result.ProcessedSymbols++
			log.Printf("[%s] %s: fetched=%d inserted=0 dry_run=%v", "DRYRUN-OR-EMPTY", symbol, len(bars), cfg.DryRun)
			continue
		}

		ch := make(chan Bar1m, 1024)
		go func(symbol string, bars []fmp.IntradayBar) {
			defer close(ch)
			for _, bar := range bars {
				ts, ok := parseFMPIntradayTimestamp(bar.Date)
				if !ok {
					continue
				}
				marketDate, sessionKind, isRegular, sessionOpen, sessionSeq := ClassifyTimestamp(ts)
				ch <- Bar1m{
					Timestamp:    ts,
					Symbol:       symbol,
					Open:         float32(bar.Open),
					High:         float32(bar.High),
					Low:          float32(bar.Low),
					Close:        float32(bar.Close),
					Volume:       0,
					Transactions: 0,
					MarketDate:   marketDate,
					SessionKind:  sessionKind,
					IsRegular:    isRegular,
					SessionOpen:  sessionOpen,
					SessionSeq:   sessionSeq,
				}
			}
		}(symbol, bars)

		inserted, err := insertForexBars(ctx, conn, ch, cfg.BatchSize)
		if err != nil {
			log.Printf("[ERROR] %s: insert forex bars: %v", symbol, err)
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

func fetchFMPIntradayChunked(ctx context.Context, client *fmp.Client, symbol string, interval fmp.IntradayInterval, from, to time.Time) ([]fmp.IntradayBar, error) {
	return fetchFMPIntradayChunkedFunc(ctx, func(ctx context.Context, symbol string, interval fmp.IntradayInterval, from, to string) ([]fmp.IntradayBar, error) {
		return client.ForexIntradayPrices(ctx, symbol, interval, from, to)
	}, symbol, interval, from, to)
}

func fetchFMPIntradayChunkedFunc(ctx context.Context, fetch func(context.Context, string, fmp.IntradayInterval, string, string) ([]fmp.IntradayBar, error), symbol string, interval fmp.IntradayInterval, from, to time.Time) ([]fmp.IntradayBar, error) {
	chunkDays := intradayChunkDays(interval)
	var all []fmp.IntradayBar
	cursor := from
	for !cursor.After(to) {
		chunkEnd := cursor.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		bars, err := fetch(ctx, symbol, interval,
			cursor.Format("2006-01-02"),
			chunkEnd.Format("2006-01-02"),
		)
		if err != nil {
			return nil, fmt.Errorf("FMP forex intraday %s [%s..%s]: %w", symbol,
				cursor.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), err)
		}
		all = append(all, bars...)
		cursor = chunkEnd.AddDate(0, 0, 1)
	}
	return normalizeIntradayBars(all), nil
}

func intradayChunkDays(interval fmp.IntradayInterval) int {
	switch interval {
	case fmp.Interval1Min:
		return 3
	case fmp.Interval5Min:
		return 15
	case fmp.Interval15Min:
		return 45
	case fmp.Interval30Min:
		return 90
	case fmp.Interval1Hour:
		return 180
	case fmp.Interval4Hour:
		return 365
	default:
		return 30
	}
}

func normalizeIntradayBars(bars []fmp.IntradayBar) []fmp.IntradayBar {
	if len(bars) <= 1 {
		return bars
	}
	type stampedBar struct {
		bar fmp.IntradayBar
		ts  time.Time
	}
	stamped := make([]stampedBar, 0, len(bars))
	seen := make(map[string]struct{}, len(bars))
	for _, bar := range bars {
		ts, ok := parseFMPIntradayTimestamp(bar.Date)
		if !ok {
			continue
		}
		key := ts.Format(time.RFC3339)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		stamped = append(stamped, stampedBar{bar: bar, ts: ts})
	}
	sort.Slice(stamped, func(i, j int) bool {
		return stamped[i].ts.Before(stamped[j].ts)
	})
	out := make([]fmp.IntradayBar, 0, len(stamped))
	for _, item := range stamped {
		out = append(out, item.bar)
	}
	return out
}

func parseFMPIntradayTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
