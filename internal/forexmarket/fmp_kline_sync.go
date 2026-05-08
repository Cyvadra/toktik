package forexmarket

import (
	"context"
	"fmt"
	"log"
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
}

// FMPKlineSyncResult summarises a sync run.
type FMPKlineSyncResult struct {
	ProcessedSymbols int
	FailedSymbols    int
	FetchedBars      int64
	InsertedRows     int64
}

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

	for _, raw := range cfg.Symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			continue
		}

		bars, err := fetchFMPIntradayChunked(ctx, client, symbol, cfg.Interval, cfg.From, cfg.To)
		if err != nil {
			log.Printf("[ERROR] %s: fetch FMP intraday: %v", symbol, err)
			result.FailedSymbols++
			continue
		}
		result.FetchedBars += int64(len(bars))

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
					Volume:       bar.Volume,
					Transactions: 0,
					MarketDate:   marketDate,
					SessionKind:  sessionKind,
					IsRegular:    isRegular,
					SessionOpen:  sessionOpen,
					SessionSeq:   sessionSeq,
				}
			}
		}(symbol, bars)

		inserted, err := InsertBars(ctx, conn, ch, cfg.BatchSize)
		if err != nil {
			log.Printf("[ERROR] %s: insert forex bars: %v", symbol, err)
			result.FailedSymbols++
			continue
		}
		result.ProcessedSymbols++
		result.InsertedRows += inserted
		log.Printf("[OK] %s: fetched=%d inserted=%d", symbol, len(bars), inserted)
	}

	return result, nil
}

func fetchFMPIntradayChunked(ctx context.Context, client *fmp.Client, symbol string, interval fmp.IntradayInterval, from, to time.Time) ([]fmp.IntradayBar, error) {
	const chunkDays = 30
	var all []fmp.IntradayBar
	cursor := from
	for !cursor.After(to) {
		chunkEnd := cursor.AddDate(0, 0, chunkDays-1)
		if chunkEnd.After(to) {
			chunkEnd = to
		}
		bars, err := client.ForexIntradayPrices(ctx, symbol, interval,
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
	return all, nil
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
