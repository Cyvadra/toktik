package service

import (
	"encoding/base64"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/dto"
	"github.com/Cyvadra/toktik/internal/usmarket"
)

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

func normalizeUSOptionQuerySymbol(symbol string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(symbol))
	if value == "" {
		return "", dto.NewValidationError("symbol is required")
	}
	if !strings.HasPrefix(value, "O:") {
		value = "O:" + value
	}
	if _, _, _, _, err := usmarket.ParseOptionTicker(value); err != nil {
		return "", dto.NewValidationError("invalid option symbol %q: %v", symbol, err)
	}
	return value, nil
}

func defaultUSOptionChainWindow(latest time.Time) (string, string) {
	from := latest.UTC()
	to := from.Add(time.Second)
	return from.Format(time.RFC3339), to.Format(time.RFC3339)
}

func parseUSOptionExpirationDate(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	expiration, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return time.Time{}, dto.NewValidationError("invalid 'expiration': expected YYYY-MM-DD, got %q", value)
	}
	return expiration.UTC(), nil
}

func resolveUSOptionUnderlying(underlying, root string) string {
	value := strings.TrimSpace(underlying)
	if value == "" {
		value = strings.TrimSpace(root)
	}
	return strings.ToUpper(value)
}

func clickhouseStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func clickhouseDateTimeLiteral(value time.Time) string {
	return clickhouseStringLiteral(value.UTC().Format("2006-01-02 15:04:05"))
}

func clickhouseUInt32Literal(value int) string {
	if value < 0 {
		value = 0
	}
	return strconv.FormatUint(uint64(value), 10)
}

func sanitizeFloat32(value float32) float32 {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return 0
	}
	return value
}

func sanitizeFloat64(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
