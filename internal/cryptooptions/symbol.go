package cryptooptions

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"
)

const SymbolIDHashName = "xxHash64"

// SymbolID returns a deterministic uint64 ID for a symbol string
// using xxHash64.
func SymbolID(symbol string) uint64 {
	return xxhash.Sum64String(symbol)
}

// ParseSymbol parses a deribit option symbol string into its components.
//
// Format: <base_asset>-<expiry>-<strike>-<type>
// Examples:
//
// "ETH-3JAN25-4250-C"       -> base=ETH, expiry=3JAN25, strike=4250, type=call
// "SOL_USDC-31JAN25-205-P"  -> base=SOL_USDC, expiry=31JAN25, strike=205, type=put
// "BTC-28MAR25-100000-C"    -> base=BTC, expiry=28MAR25, strike=100000, type=call
//
// Splits from the right on '-' so base_asset can contain hyphens.
func ParseSymbol(symbol string) (SymbolMeta, error) {
	parts := strings.Split(symbol, "-")
	if len(parts) < 4 {
		return SymbolMeta{}, fmt.Errorf("invalid symbol format: %q (need at least 4 dash-separated parts)", symbol)
	}

	n := len(parts)
	typePart := strings.ToUpper(parts[n-1])
	strikePart := parts[n-2]
	expiryPart := strings.ToUpper(parts[n-3])
	baseAsset := strings.Join(parts[:n-3], "-")

	var optionType string
	switch typePart {
	case "C":
		optionType = "call"
	case "P":
		optionType = "put"
	default:
		return SymbolMeta{}, fmt.Errorf("invalid option type %q in symbol %q (expected C or P)", typePart, symbol)
	}

	strike64, err := strconv.ParseFloat(strikePart, 32)
	if err != nil {
		return SymbolMeta{}, fmt.Errorf("invalid strike price %q in symbol %q: %w", strikePart, symbol, err)
	}

	expiry, err := ParseExpiryDate(expiryPart)
	if err != nil {
		return SymbolMeta{}, fmt.Errorf("invalid expiry %q in symbol %q: %w", expiryPart, symbol, err)
	}

	return SymbolMeta{
		SymbolID:    SymbolID(symbol),
		Symbol:      symbol,
		BaseAsset:   baseAsset,
		OptionType:  optionType,
		StrikePrice: float32(strike64),
		Expiration:  expiry,
	}, nil
}

// ParseExpiryDate parses a deribit-style expiry date string.
// Format: "<day><MON><YY>" e.g. "3JAN25", "31JAN25", "28MAR25"
func ParseExpiryDate(s string) (time.Time, error) {
	t, err := time.Parse("2Jan06", s)
	if err != nil {
		return time.Time{}, err
	}
	t = t.UTC()
	return t, nil
}

// ExtractBaseAsset extracts the base asset from a symbol string
// without full parsing.
func ExtractBaseAsset(symbol string) string {
	parts := strings.Split(symbol, "-")
	if len(parts) < 4 {
		return symbol
	}
	return strings.Join(parts[:len(parts)-3], "-")
}
