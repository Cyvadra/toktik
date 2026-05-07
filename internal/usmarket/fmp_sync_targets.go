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
	if len(symbols) > 0 {
		return normalizeExplicitSyncTargets(symbols), nil
	}

	stockSymbols, err := ResolveUSStockSymbols(ctx, conn, nil, limit)
	if err != nil {
		return nil, err
	}
	stockSet := make(map[string]struct{}, len(stockSymbols))
	targets := make([]FMPStockSyncTarget, 0, len(stockSymbols))
	for _, symbol := range stockSymbols {
		stockSet[symbol] = struct{}{}
		targets = append(targets, FMPStockSyncTarget{
			StoreSymbol: symbol,
			FetchSymbol: symbol,
			Source:      "stored-stock",
		})
	}
	if !includeOptionGaps {
		return targets, nil
	}

	optionUnderlyings, err := listUSOptionUnderlyings(ctx, conn)
	if err != nil {
		return nil, err
	}
	for _, underlying := range optionUnderlyings {
		if _, ok := stockSet[underlying]; ok {
			continue
		}
		target, ok := resolveMissingUnderlyingSyncTarget(underlying, stockSet)
		if !ok {
			continue
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func normalizeExplicitSyncTargets(symbols []string) []FMPStockSyncTarget {
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
		if alias, ok := fmpIndexGapAliases[storeSymbol]; ok {
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

func resolveMissingUnderlyingSyncTarget(underlying string, stockSet map[string]struct{}) (FMPStockSyncTarget, bool) {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if underlying == "" {
		return FMPStockSyncTarget{}, false
	}
	if alias, ok := fmpIndexGapAliases[underlying]; ok {
		return FMPStockSyncTarget{StoreSymbol: underlying, FetchSymbol: alias, Source: "option-gap-index-alias"}, true
	}
	if _, ok := fmpDirectGapUnderlyings[underlying]; ok {
		return FMPStockSyncTarget{StoreSymbol: underlying, FetchSymbol: underlying, Source: "option-gap-direct"}, true
	}
	return FMPStockSyncTarget{}, false
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
