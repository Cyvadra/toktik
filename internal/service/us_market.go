package service

import (
	"encoding/base64"
	"strings"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

var usStockBarIntervals = map[string]string{
	"1m":  "us_stocks_bar_1m",
	"5m":  "us_stocks_bar_5m",
	"15m": "us_stocks_bar_15m",
	"30m": "us_stocks_bar_30m",
	"1h":  "us_stocks_bar_1h",
	"2h":  "us_stocks_bar_2h",
	"4h":  "us_stocks_bar_4h",
	"1d":  "us_stocks_bar_1d",
}

var usOptionBarIntervals = map[string]string{
	"1m":  "us_options_bar_1m",
	"5m":  "us_options_bar_5m",
	"15m": "us_options_bar_15m",
	"30m": "us_options_bar_30m",
	"1h":  "us_options_bar_1h",
	"2h":  "us_options_bar_2h",
	"4h":  "us_options_bar_4h",
	"1d":  "us_options_bar_1d",
}

func resolveUSBarTable(interval string, tables map[string]string, label string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(interval))
	table, ok := tables[key]
	if !ok {
		return "", dto.NewValidationError("unsupported %s interval %q", label, interval)
	}
	return table, nil
}

func normalizeUSSession(session string, interval string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(session))
	intervalKey := strings.ToLower(strings.TrimSpace(interval))
	if intervalKey != "1m" {
		switch value {
		case "", "regular", "all":
			// Pre-aggregated US bar views are already built from regular-session 1m data.
			return "all", nil
		case "extended":
			return "", dto.NewValidationError("session %q is only supported for 1m US market bars", value)
		default:
			return "", dto.NewValidationError("unsupported session %q", session)
		}
	}

	if value == "" {
		value = "regular"
	}
	switch value {
	case "regular", "all", "extended":
		return value, nil
	default:
		return "", dto.NewValidationError("unsupported session %q", session)
	}
}

func usSessionCondition(session string) string {
	switch session {
	case "all":
		return ""
	case "extended":
		return " AND session_kind IN ('premarket', 'regular', 'postmarket')"
	default:
		return " AND is_regular_session = 1"
	}
}

func usBarLimit(limit int) int {
	return clamp(limit, defaultBarLimit, maxBarLimit)
}

func invalidCursorError(err error) error {
	return dto.NewValidationError("invalid cursor: %v", err)
}

func encodeCursorString(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeCursorString(cursor string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normalizeUSOptionInterval(interval string) string {
	value := strings.ToLower(strings.TrimSpace(interval))
	if value == "" {
		return "1m"
	}
	return value
}

func normalizeUSChainInterval(interval string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(interval))
	if value == "" {
		value = "1d"
	}
	if _, ok := usmarket.ChainPrecomputedIntervals[value]; !ok {
		return "", dto.NewValidationError("unsupported us-options chain interval %q", interval)
	}
	return value, nil
}
