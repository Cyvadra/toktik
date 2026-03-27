package usmarket

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Polygon OPRA ticker format: O:<underlying><YYMMDD><C|P><strike*1000>
// The underlying can be 1-6 chars, date is 6 digits, type is C/P, strike is 8 digits.
var optionTickerRe = regexp.MustCompile(`^O:([A-Z]+)(\d{6})([CP])(\d{8})$`)

// ParseOptionTicker parses a Polygon OPRA option ticker into its components.
// Example: O:AAPL230120C00130000 -> underlying=AAPL, exp=2023-01-20, type=C, strike=130.0
func ParseOptionTicker(ticker string) (underlying string, expiration time.Time, optionType string, strike float64, err error) {
	m := optionTickerRe.FindStringSubmatch(ticker)
	if m == nil {
		return "", time.Time{}, "", 0, fmt.Errorf("invalid option ticker: %q", ticker)
	}

	underlying = m[1]

	exp, err := time.Parse("060102", m[2])
	if err != nil {
		return "", time.Time{}, "", 0, fmt.Errorf("parse expiration in %q: %w", ticker, err)
	}
	expiration = exp.UTC()

	optionType = m[3]

	strikeInt, err := strconv.ParseInt(m[4], 10, 64)
	if err != nil {
		return "", time.Time{}, "", 0, fmt.Errorf("parse strike in %q: %w", ticker, err)
	}
	strike = float64(strikeInt) / 1000.0

	return underlying, expiration, optionType, strike, nil
}
