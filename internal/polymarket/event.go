package polymarket

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type ReplayClock string

const (
	ReplayClockReceived ReplayClock = "received"
	ReplayClockExchange ReplayClock = "exchange"
)

type EventKey struct {
	ExchangeTime time.Time
	ReceivedTime time.Time
	SourceFile   string
	SourceRow    uint64
}

func (key EventKey) EventID() string {
	source := filepath.Base(strings.TrimSpace(key.SourceFile))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", source, key.SourceRow)))
	return hex.EncodeToString(sum[:16])
}

func (key EventKey) ReplayTime(clock ReplayClock) (time.Time, error) {
	switch clock {
	case ReplayClockReceived:
		return key.ReceivedTime, nil
	case ReplayClockExchange:
		return key.ExchangeTime, nil
	default:
		return time.Time{}, fmt.Errorf("unsupported replay clock %q", clock)
	}
}

func CompareEventKeys(left, right EventKey, clock ReplayClock) (int, error) {
	leftPrimary, err := left.ReplayTime(clock)
	if err != nil {
		return 0, err
	}
	rightPrimary, err := right.ReplayTime(clock)
	if err != nil {
		return 0, err
	}
	if cmp := leftPrimary.Compare(rightPrimary); cmp != 0 {
		return cmp, nil
	}

	leftSecondary := left.ExchangeTime
	rightSecondary := right.ExchangeTime
	if clock == ReplayClockExchange {
		leftSecondary = left.ReceivedTime
		rightSecondary = right.ReceivedTime
	}
	if cmp := leftSecondary.Compare(rightSecondary); cmp != 0 {
		return cmp, nil
	}
	if left.SourceFile < right.SourceFile {
		return -1, nil
	}
	if left.SourceFile > right.SourceFile {
		return 1, nil
	}
	if left.SourceRow < right.SourceRow {
		return -1, nil
	}
	if left.SourceRow > right.SourceRow {
		return 1, nil
	}
	return 0, nil
}
