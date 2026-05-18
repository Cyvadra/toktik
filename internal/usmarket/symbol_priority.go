package usmarket

import (
	"context"
	"fmt"
	"sort"
	"strings"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const usSymbolPriorityDollarVolumeLookbackDays = 90

const (
	PriorityOrderNone      = "none"
	PriorityOrderUSDefault = "us-default"
)

var usPresetSymbolPriority = []string{
	"SPY", "QQQ", "IWM", "DIA", "GLD", "SLV", "TLT", "USO", "AAPL", "MSFT", "NVDA", "GOOGL", "AMZN", "META", "TSLA", "SOL", "IBIT", "ETHA", "COIN", "MSTR", "ASHR", "FXI", "MCHI", "KWEB", "EWH", "VIX",
}

type usSymbolPriorityInfo struct {
	Symbol              string
	PresetRank          int
	AverageDollarVolume float64
	HasDollarVolume     bool
}

// PrioritizeUSSymbols orders US-market symbols for long-running sync/backfill
// tasks. Preset symbols are always processed first; the remainder are ordered
// by 90-session average dollar volume descending, then symbol ascending.
func PrioritizeUSSymbols(ctx context.Context, conn driver.Conn, symbols []string) ([]string, error) {
	normalized := normalizeUSPrioritySymbols(symbols)
	if len(normalized) == 0 {
		return nil, nil
	}
	infos, err := resolveUSSymbolPriorityInfo(ctx, conn, normalized)
	if err != nil {
		return nil, err
	}
	return sortUSSymbolsByPriority(normalized, infos), nil
}

func NormalizeUSPriorityOrder(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return PriorityOrderUSDefault, nil
	}
	switch normalized {
	case PriorityOrderNone, PriorityOrderUSDefault:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported priority order %q", value)
	}
}

func MaybePrioritizeUSSymbols(ctx context.Context, conn driver.Conn, symbols []string, priorityOrder string) ([]string, error) {
	normalizedOrder, err := NormalizeUSPriorityOrder(priorityOrder)
	if err != nil {
		return nil, err
	}
	normalizedSymbols := normalizeUSPrioritySymbols(symbols)
	if normalizedOrder == PriorityOrderNone {
		sort.Strings(normalizedSymbols)
		return normalizedSymbols, nil
	}
	return PrioritizeUSSymbols(ctx, conn, normalizedSymbols)
}

func MaybePrioritizeMissingOptionGreeksTasks(ctx context.Context, conn driver.Conn, tasks []MissingOptionGreeksTask, priorityOrder string) ([]MissingOptionGreeksTask, error) {
	normalizedOrder, err := NormalizeUSPriorityOrder(priorityOrder)
	if err != nil {
		return nil, err
	}
	ordered := append([]MissingOptionGreeksTask(nil), tasks...)
	if normalizedOrder == PriorityOrderNone {
		sort.SliceStable(ordered, func(i, j int) bool {
			if !ordered[i].MarketDate.Equal(ordered[j].MarketDate) {
				return ordered[i].MarketDate.Before(ordered[j].MarketDate)
			}
			return ordered[i].Underlying < ordered[j].Underlying
		})
		return ordered, nil
	}
	uniqueSymbols := make([]string, 0, len(ordered))
	seen := make(map[string]struct{}, len(ordered))
	for _, task := range ordered {
		if _, ok := seen[task.Underlying]; ok {
			continue
		}
		seen[task.Underlying] = struct{}{}
		uniqueSymbols = append(uniqueSymbols, task.Underlying)
	}
	infos, err := resolveUSSymbolPriorityInfo(ctx, conn, normalizeUSPrioritySymbols(uniqueSymbols))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left := infos[ordered[i].Underlying]
		right := infos[ordered[j].Underlying]
		if compareUSSymbolPriority(left, right) {
			return true
		}
		if compareUSSymbolPriority(right, left) {
			return false
		}
		if !ordered[i].MarketDate.Equal(ordered[j].MarketDate) {
			return ordered[i].MarketDate.Before(ordered[j].MarketDate)
		}
		return ordered[i].Underlying < ordered[j].Underlying
	})
	return ordered, nil
}

func PrioritizeUSStockSyncTargets(ctx context.Context, conn driver.Conn, targets []FMPStockSyncTarget) ([]FMPStockSyncTarget, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	symbols := make([]string, 0, len(targets))
	for _, target := range targets {
		symbols = append(symbols, target.StoreSymbol)
	}
	infos, err := resolveUSSymbolPriorityInfo(ctx, conn, normalizeUSPrioritySymbols(symbols))
	if err != nil {
		return nil, err
	}
	ordered := append([]FMPStockSyncTarget(nil), targets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := infos[ordered[i].StoreSymbol]
		right := infos[ordered[j].StoreSymbol]
		return compareUSSymbolPriority(left, right)
	})
	return ordered, nil
}

func resolveUSSymbolPriorityInfo(ctx context.Context, conn driver.Conn, symbols []string) (map[string]usSymbolPriorityInfo, error) {
	infos := make(map[string]usSymbolPriorityInfo, len(symbols))
	presetRank := make(map[string]int, len(usPresetSymbolPriority))
	for idx, symbol := range usPresetSymbolPriority {
		presetRank[symbol] = idx
	}
	for _, symbol := range symbols {
		info := usSymbolPriorityInfo{Symbol: symbol, PresetRank: -1}
		if rank, ok := presetRank[symbol]; ok {
			info.PresetRank = rank
		}
		infos[symbol] = info
	}

	rows, err := conn.Query(ctx, `SELECT symbol, avg(dollar_volume) AS avg_dollar_volume
FROM (
	SELECT
		symbol,
		toFloat64(close) * toFloat64(volume) AS dollar_volume
	FROM us_stocks_bar_1d
	WHERE symbol IN ({symbols:Array(String)})
	ORDER BY symbol ASC, timestamp DESC
	LIMIT 90 BY symbol
)
GROUP BY symbol`, clickhouse.Named("symbols", symbols))
	if err != nil {
		return nil, fmt.Errorf("query US symbol priority dollar volume: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			symbol          string
			avgDollarVolume float64
		)
		if err := rows.Scan(&symbol, &avgDollarVolume); err != nil {
			return nil, fmt.Errorf("scan US symbol priority dollar volume: %w", err)
		}
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		info := infos[symbol]
		info.HasDollarVolume = avgDollarVolume > 0
		info.AverageDollarVolume = avgDollarVolume
		infos[symbol] = info
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US symbol priority dollar volume: %w", err)
	}
	return infos, nil
}

func sortUSSymbolsByPriority(symbols []string, infos map[string]usSymbolPriorityInfo) []string {
	ordered := append([]string(nil), symbols...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return compareUSSymbolPriority(infos[ordered[i]], infos[ordered[j]])
	})
	return ordered
}

func compareUSSymbolPriority(left, right usSymbolPriorityInfo) bool {
	leftPreset := left.PresetRank >= 0
	rightPreset := right.PresetRank >= 0
	if leftPreset != rightPreset {
		return leftPreset
	}
	if leftPreset && rightPreset && left.PresetRank != right.PresetRank {
		return left.PresetRank < right.PresetRank
	}
	if left.HasDollarVolume != right.HasDollarVolume {
		return left.HasDollarVolume
	}
	if left.HasDollarVolume && right.HasDollarVolume && left.AverageDollarVolume != right.AverageDollarVolume {
		return left.AverageDollarVolume > right.AverageDollarVolume
	}
	return left.Symbol < right.Symbol
}

func normalizeUSPrioritySymbols(symbols []string) []string {
	seen := make(map[string]struct{}, len(symbols))
	ordered := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := strings.ToUpper(strings.TrimSpace(symbol))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		ordered = append(ordered, normalized)
	}
	return ordered
}
