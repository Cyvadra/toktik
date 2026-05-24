package usmarket

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Cyvadra/toktik/internal/chquery"
)

// FMPStockSyncTarget defines how one logical symbol should be fetched from FMP
// and stored in us_stocks_bar_1m.
type FMPStockSyncTarget struct {
	StoreSymbol string
	FetchSymbol string
	Source      string
}

type USStockSyncTargetProvider string

const (
	USStockSyncTargetProviderFMP USStockSyncTargetProvider = "fmp"
)

type USStockSyncTargetResolverOptions struct {
	IncludeOptionGaps bool
	FetchOverrides    map[string]string
	Provider          USStockSyncTargetProvider
}

func NormalizeUSStockSyncTargets(targets []FMPStockSyncTarget) []FMPStockSyncTarget {
	seen := make(map[string]struct{}, len(targets))
	out := make([]FMPStockSyncTarget, 0, len(targets))
	for _, target := range targets {
		storeSymbol := strings.ToUpper(strings.TrimSpace(target.StoreSymbol))
		if storeSymbol == "" {
			continue
		}
		if _, ok := seen[storeSymbol]; ok {
			continue
		}
		seen[storeSymbol] = struct{}{}
		fetchSymbol := strings.ToUpper(strings.TrimSpace(target.FetchSymbol))
		if fetchSymbol == "" {
			fetchSymbol = storeSymbol
		}
		out = append(out, FMPStockSyncTarget{
			StoreSymbol: storeSymbol,
			FetchSymbol: fetchSymbol,
			Source:      strings.TrimSpace(target.Source),
		})
	}
	return out
}

var fmpDirectGapUnderlyings = map[string]struct{}{
	"ABBNY": {},
	"ACHHY": {},
	"ADAPY": {},
	"AFIIQ": {},
	"ALLGF": {},
	"ATEST": {},
	"BIGGQ": {},
	"CORZQ": {},
	"CRRCF": {},
	"DIDIY": {},
	"DSHK":  {},
	"DTCB":  {},
	"EBIXQ": {},
	"ELMSQ": {},
	"EMWPF": {},
	"ENDPQ": {},
	"ENIAY": {},
	"EQOSQ": {},
	"FRCB":  {},
	"FTCHF": {},
	"IBOT":  {},
	"IOGPQ": {},
	"IRBTQ": {},
	"JTKWY": {},
	"KLDO":  {},
	"LAZRQ": {},
	"LNWO":  {},
	"LOVLQ": {},
	"MODVQ": {},
	"NINEQ": {},
	"ODTC":  {},
	"ORANY": {},
	"ORPHY": {},
	"RADCQ": {},
	"REVRQ": {},
	"RIDEQ": {},
	"RVIC":  {},
	"SDCCQ": {},
	"SICP":  {},
	"SIVBQ": {},
	"SNPTY": {},
	"SONDQ": {},
	"TPICQ": {},
	"TRIRF": {},
	"TTCFQ": {},
	"VQSSF": {},
	"VRAYQ": {},
	"WEWKQ": {},
	"YELLQ": {},
	"ZJZZT": {},
	"ZVZZT": {},
	"ZYXIQ": {},
}

var fmpIndexGapAliases = map[string]string{
	"DJX":   "^DJI",
	"MRUT":  "^RUT",
	"NDX":   "^IXIC",
	"NDXP":  "^IXIC",
	"NQX":   "^IXIC",
	"RUT":   "^RUT",
	"RUTW":  "^RUT",
	"SPIKE": "^VIX",
	"SPX":   "^GSPC",
	"SPXW":  "^GSPC",
	"VIX":   "^VIX",
	"VIXW":  "^VIX",
	"VOLQ":  "^VIX",
	"XND":   "^IXIC",
	"XSP":   "^GSPC",
}

// ResolveUSStockSyncTargets returns the default FMP stock sync target set.
// It starts from symbols already stored in us_stocks_bar_1m, then appends a
// deterministic, code-reviewed subset of option underlyings missing from the
// stock table. Those extra targets are stored using the option underlying name
// but can fetch via a code-reviewed index alias when needed.
func ResolveUSStockSyncTargets(ctx context.Context, conn driver.Conn, symbols []string, limit int, includeOptionGaps bool) ([]FMPStockSyncTarget, error) {
	return ResolveUSStockSyncTargetsWithOptions(ctx, conn, symbols, limit, USStockSyncTargetResolverOptions{IncludeOptionGaps: includeOptionGaps, Provider: USStockSyncTargetProviderFMP})
}

func ResolveUSStockSyncTargetsWithOptions(ctx context.Context, conn driver.Conn, symbols []string, limit int, opts USStockSyncTargetResolverOptions) ([]FMPStockSyncTarget, error) {
	opts = normalizeUSStockSyncTargetResolverOptions(opts)
	if len(symbols) > 0 {
		return applyUSStockSyncTargetFetchOverrides(normalizeExplicitSyncTargets(symbols, opts.Provider), opts.FetchOverrides), nil
	}

	stockSymbols, err := resolveStoredUSStockSymbols(ctx, conn, nil, limit)
	if err != nil {
		return nil, err
	}
	stockSet := make(map[string]struct{}, len(stockSymbols))
	targets := make([]FMPStockSyncTarget, 0, len(stockSymbols))
	for _, symbol := range stockSymbols {
		stockSet[symbol] = struct{}{}
		targets = append(targets, buildStoredStockSyncTarget(symbol, opts.Provider))
	}
	if !opts.IncludeOptionGaps {
		return applyUSStockSyncTargetFetchOverrides(targets, opts.FetchOverrides), nil
	}

	optionUnderlyings, err := listUSOptionUnderlyings(ctx, conn)
	if err != nil {
		return nil, err
	}
	for _, underlying := range optionUnderlyings {
		if _, ok := stockSet[underlying]; ok {
			continue
		}
		target, ok := resolveMissingUnderlyingSyncTarget(underlying, stockSet, opts.Provider)
		if !ok {
			continue
		}
		targets = append(targets, target)
	}
	ordered, err := PrioritizeUSStockSyncTargets(ctx, conn, targets)
	if err != nil {
		return nil, err
	}
	return applyUSStockSyncTargetFetchOverrides(ordered, opts.FetchOverrides), nil
}

func normalizeUSStockSyncTargetResolverOptions(opts USStockSyncTargetResolverOptions) USStockSyncTargetResolverOptions {
	if opts.Provider == "" {
		opts.Provider = USStockSyncTargetProviderFMP
	}
	if len(opts.FetchOverrides) > 0 {
		normalized := make(map[string]string, len(opts.FetchOverrides))
		for rawKey, rawValue := range opts.FetchOverrides {
			key := strings.ToUpper(strings.TrimSpace(rawKey))
			value := strings.ToUpper(strings.TrimSpace(rawValue))
			if key == "" || value == "" {
				continue
			}
			normalized[key] = value
		}
		opts.FetchOverrides = normalized
	}
	return opts
}

func buildStoredStockSyncTarget(symbol string, provider USStockSyncTargetProvider) FMPStockSyncTarget {
	storeSymbol := strings.ToUpper(strings.TrimSpace(symbol))
	fetchSymbol := storeSymbol
	source := "stored-stock"
	if alias, ok := resolveUSStockIndexAlias(storeSymbol, provider); ok {
		fetchSymbol = alias
		source = "stored-stock-index-alias"
	}
	return FMPStockSyncTarget{
		StoreSymbol: storeSymbol,
		FetchSymbol: fetchSymbol,
		Source:      source,
	}
}

func normalizeExplicitSyncTargets(symbols []string, provider USStockSyncTargetProvider) []FMPStockSyncTarget {
	seen := make(map[string]struct{}, len(symbols))
	targets := make([]FMPStockSyncTarget, 0, len(symbols))
	for _, symbol := range symbols {
		storeSymbol := strings.ToUpper(strings.TrimSpace(symbol))
		if storeSymbol == "" {
			continue
		}
		if _, ok := seen[storeSymbol]; ok {
			continue
		}
		seen[storeSymbol] = struct{}{}
		fetchSymbol := storeSymbol
		source := "explicit"
		if alias, ok := resolveUSStockIndexAlias(storeSymbol, provider); ok {
			fetchSymbol = alias
			source = "explicit-index-alias"
		}
		targets = append(targets, FMPStockSyncTarget{
			StoreSymbol: storeSymbol,
			FetchSymbol: fetchSymbol,
			Source:      source,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].StoreSymbol < targets[j].StoreSymbol
	})
	return targets
}

func storeSymbolsFromSyncTargets(targets []FMPStockSyncTarget) []string {
	seen := make(map[string]struct{}, len(targets))
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		storeSymbol := strings.ToUpper(strings.TrimSpace(target.StoreSymbol))
		if storeSymbol == "" {
			continue
		}
		if _, ok := seen[storeSymbol]; ok {
			continue
		}
		seen[storeSymbol] = struct{}{}
		out = append(out, storeSymbol)
	}
	return out
}

func FetchSymbolsFromSyncTargets(targets []FMPStockSyncTarget) []string {
	seen := make(map[string]struct{}, len(targets))
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		fetchSymbol := strings.ToUpper(strings.TrimSpace(target.FetchSymbol))
		if fetchSymbol == "" {
			continue
		}
		if _, ok := seen[fetchSymbol]; ok {
			continue
		}
		seen[fetchSymbol] = struct{}{}
		out = append(out, fetchSymbol)
	}
	return out
}

func resolveMissingUnderlyingSyncTarget(underlying string, stockSet map[string]struct{}, provider USStockSyncTargetProvider) (FMPStockSyncTarget, bool) {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if underlying == "" {
		return FMPStockSyncTarget{}, false
	}
	if alias, ok := resolveUSStockIndexAlias(underlying, provider); ok {
		return FMPStockSyncTarget{StoreSymbol: underlying, FetchSymbol: alias, Source: "option-gap-index-alias"}, true
	}
	if supportsUSStockDirectGapUnderlyings(provider) {
		if _, ok := fmpDirectGapUnderlyings[underlying]; ok {
			return FMPStockSyncTarget{StoreSymbol: underlying, FetchSymbol: underlying, Source: "option-gap-direct"}, true
		}
	}
	return FMPStockSyncTarget{}, false
}

func resolveUSStockIndexAlias(symbol string, provider USStockSyncTargetProvider) (string, bool) {
	switch provider {
	case USStockSyncTargetProviderFMP:
		alias, ok := fmpIndexGapAliases[symbol]
		return alias, ok
	default:
		return "", false
	}
}

func supportsUSStockDirectGapUnderlyings(provider USStockSyncTargetProvider) bool {
	switch provider {
	case USStockSyncTargetProviderFMP:
		return true
	default:
		return false
	}
}

func applyUSStockSyncTargetFetchOverrides(targets []FMPStockSyncTarget, overrides map[string]string) []FMPStockSyncTarget {
	if len(overrides) == 0 {
		return targets
	}
	adjusted := append([]FMPStockSyncTarget(nil), targets...)
	for index, target := range adjusted {
		mapped := strings.ToUpper(strings.TrimSpace(overrides[target.StoreSymbol]))
		if mapped == "" {
			continue
		}
		adjusted[index].FetchSymbol = mapped
		if adjusted[index].Source == "" {
			adjusted[index].Source = "fetch-override"
			continue
		}
		adjusted[index].Source += "-fetch-override"
	}
	return adjusted
}

func listUSOptionUnderlyings(ctx context.Context, conn driver.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, chquery.USOptionsListUnderlyings)
	if err != nil {
		return nil, fmt.Errorf("query US option underlyings: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var underlying string
		if err := rows.Scan(&underlying); err != nil {
			return nil, fmt.Errorf("scan US option underlying: %w", err)
		}
		underlying = strings.ToUpper(strings.TrimSpace(underlying))
		if underlying != "" {
			out = append(out, underlying)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate US option underlyings: %w", err)
	}
	return out, nil
}
