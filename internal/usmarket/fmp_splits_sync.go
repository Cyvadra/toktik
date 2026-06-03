package usmarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/pkg/fmp"
)

type FMPStockSplitsSyncConfig struct {
	APIKey    string
	Targets   []FMPStockSyncTarget
	BatchSize int
	DryRun    bool
}

type FMPStockSplitsSyncResult struct {
	SymbolsProcessed int64
	RowsInserted     int64
}

const stockSplitMaxFutureDays = 3

func SyncFMPStockSplits(ctx context.Context, conn driver.Conn, cfg FMPStockSplitsSyncConfig) (FMPStockSplitsSyncResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return FMPStockSplitsSyncResult{}, fmt.Errorf("FMP API key is required")
	}
	targets := normalizeStockSplitTargets(cfg.Targets)
	if len(targets) == 0 {
		return FMPStockSplitsSyncResult{}, fmt.Errorf("targets are required")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 1000
	}
	client := fmp.New(cfg.APIKey)
	now := time.Now().UTC()
	result := FMPStockSplitsSyncResult{}
	for _, target := range targets {
		events, err := client.Splits(ctx, target.FetchSymbol)
		if err != nil {
			if shouldSkipFMPStockSplitSymbol(err) {
				log.Printf("[fmp-stock-splits] skip unsupported symbol %s (fetch=%s): %v", target.StoreSymbol, target.FetchSymbol, err)
				continue
			}
			return result, fmt.Errorf("fetch FMP stock splits for %s (fetch=%s): %w", target.StoreSymbol, target.FetchSymbol, err)
		}
		splits, err := convertFMPStockSplits(target.StoreSymbol, events, now)
		if err != nil {
			return result, err
		}
		result.SymbolsProcessed++
		if cfg.DryRun {
			log.Printf("[fmp-stock-splits] dry-run: %s (fetch=%s) would insert %d split rows", target.StoreSymbol, target.FetchSymbol, len(splits))
			result.RowsInserted += int64(len(splits))
			continue
		}
		inserted, err := InsertStockSplits(ctx, conn, splits, cfg.BatchSize)
		if err != nil {
			return result, fmt.Errorf("insert stock splits for %s: %w", target.StoreSymbol, err)
		}
		result.RowsInserted += inserted
		log.Printf("[fmp-stock-splits] %s (fetch=%s) inserted %d split rows", target.StoreSymbol, target.FetchSymbol, inserted)
	}
	return result, nil
}

func convertFMPStockSplits(requestSymbol string, events []fmp.StockSplit, updatedAt time.Time) ([]StockSplit, error) {
	storeSymbol := strings.ToUpper(strings.TrimSpace(requestSymbol))
	out := make([]StockSplit, 0, len(events))
	for _, event := range events {
		symbol := strings.ToUpper(strings.TrimSpace(event.Symbol))
		if symbol == "" {
			symbol = storeSymbol
		}
		if symbol == "" {
			continue
		}
		if event.Numerator <= 0 || event.Denominator <= 0 {
			return nil, fmt.Errorf("invalid FMP split ratio for %s on %s: numerator=%v denominator=%v", symbol, event.Date, event.Numerator, event.Denominator)
		}
		splitDate, err := time.Parse("2006-01-02", strings.TrimSpace(event.Date))
		if err != nil {
			return nil, fmt.Errorf("parse FMP split date for %s %q: %w", symbol, event.Date, err)
		}
		if splitDate.UTC().After(updatedAt.UTC().AddDate(0, 0, stockSplitMaxFutureDays)) {
			continue
		}
		out = append(out, StockSplit{
			Symbol:      symbol,
			SplitDate:   splitDate.UTC(),
			Numerator:   event.Numerator,
			Denominator: event.Denominator,
			SplitType:   strings.TrimSpace(event.SplitType),
			Source:      "fmp",
			SourceHash:  stockSplitSourceHash(event),
			UpdatedAt:   updatedAt,
		})
	}
	return out, nil
}

func stockSplitSourceHash(event fmp.StockSplit) string {
	data, _ := json.Marshal(event)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeStockSplitSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := map[string]struct{}{}
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		out = append(out, symbol)
	}
	return out
}

func normalizeStockSplitTargets(targets []FMPStockSyncTarget) []FMPStockSyncTarget {
	return NormalizeUSStockSyncTargets(targets)
}

func shouldSkipFMPStockSplitSymbol(err error) bool {
	var httpErr *fmp.HTTPStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	if httpErr.StatusCode != 402 {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(httpErr.Body))
	if body == "" {
		return false
	}
	return strings.Contains(body, "premium query parameter") ||
		strings.Contains(body, "not available under your current subscription") ||
		strings.Contains(body, "special endpoint")
}
