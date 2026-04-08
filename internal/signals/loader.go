package signals

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Paths             []string
	TimestampColumns  []string
	TypeColumns       []string
	SignalColumns     []string
	TimeLayouts       []string
	Location          *time.Location
	TextLocation      *time.Location
	EntryMatchers     []string
	SkipMissing       bool
	TextOptionalIndex bool

	// Rich event column mappings (used by LoadEvents).
	NameColumn      string
	DirectionColumn string
	ActionColumn    string
	RemarksColumn   string
	QtyColumn       string
	RefColumn       string
	GroupRefColumn  string
	MetaColumns     []string // Additional columns captured into Meta map.
	SourceLabel     string   // Populates SignalEvent.Source if set.
}

func LoadTimes(cfg Config) (map[int64]struct{}, error) {
	if len(cfg.Paths) == 0 {
		return map[int64]struct{}{}, nil
	}
	result := make(map[int64]struct{})
	for _, rawPath := range cfg.Paths {
		path := strings.TrimSpace(rawPath)
		if path == "" {
			continue
		}
		resolved, found, err := ResolvePath(path)
		if err != nil {
			return nil, err
		}
		if !found {
			if cfg.SkipMissing {
				continue
			}
			return nil, fmt.Errorf("signal file not found: %s", path)
		}
		times, err := loadOne(resolved, cfg)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", resolved, err)
		}
		for ts := range times {
			result[ts] = struct{}{}
		}
	}
	return result, nil
}

func BuildBinarySeries(timestamps []time.Time, times map[int64]struct{}) []float64 {
	out := make([]float64, len(timestamps))
	for i, ts := range timestamps {
		if _, ok := times[ts.UTC().Unix()]; ok {
			out[i] = 1
		}
	}
	return out
}

func ResolvePath(path string) (string, bool, error) {
	if _, err := os.Stat(path); err == nil {
		return path, true, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory for signal file %s: %w", path, err)
	}
	resolved := filepath.Join(wd, path)
	if _, err := os.Stat(resolved); err == nil {
		return resolved, true, nil
	}
	return "", false, nil
}

func loadOne(path string, cfg Config) (map[int64]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err == nil && len(records) > 0 {
		if times, ok, csvErr := loadCSVRecords(path, records, cfg); ok {
			return times, csvErr
		}
	}
	return loadTextFile(path, cfg)
}

func loadCSVRecords(path string, records [][]string, cfg Config) (map[int64]struct{}, bool, error) {
	columns := csvColumnIndex(records[0])
	timeIndex, hasTime := firstColumnIndex(columns, cfg.TimestampColumns)
	if !hasTime {
		return nil, false, nil
	}

	typeIndex, hasType := firstColumnIndex(columns, cfg.TypeColumns)
	signalIndex, hasSignal := firstColumnIndex(columns, cfg.SignalColumns)
	if len(cfg.EntryMatchers) > 0 && !hasType && !hasSignal {
		return nil, true, fmt.Errorf("signal csv %s missing type/signal columns", path)
	}

	location := cfg.Location
	if location == nil {
		location = time.UTC
	}

	out := make(map[int64]struct{})
	for rowIndex, record := range records[1:] {
		if timeIndex >= len(record) {
			continue
		}
		if !isEntryRecord(record, hasType, typeIndex, hasSignal, signalIndex, cfg.EntryMatchers) {
			continue
		}
		raw := strings.TrimSpace(record[timeIndex])
		if raw == "" {
			continue
		}
		ts, parseErr := parseWithLayouts(raw, cfg.TimeLayouts, location)
		if parseErr != nil {
			return nil, true, fmt.Errorf("parse signal row %d (%q): %w", rowIndex+2, raw, parseErr)
		}
		out[ts.UTC().Unix()] = struct{}{}
	}
	return out, true, nil
}

func loadTextFile(path string, cfg Config) (map[int64]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	location := cfg.TextLocation
	if location == nil {
		location = cfg.Location
	}
	if location == nil {
		location = time.UTC
	}

	out := make(map[int64]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if cfg.TextOptionalIndex {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if _, convErr := strconv.Atoi(fields[0]); convErr == nil {
					line = strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
				}
			}
		}
		ts, parseErr := parseWithLayouts(line, cfg.TimeLayouts, location)
		if parseErr != nil {
			continue
		}
		out[ts.UTC().Unix()] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

func parseWithLayouts(raw string, layouts []string, location *time.Location) (time.Time, error) {
	if len(layouts) == 0 {
		layouts = []string{time.RFC3339}
	}
	var lastErr error
	for _, layout := range layouts {
		layout = strings.TrimSpace(layout)
		if layout == "" {
			continue
		}
		ts, err := time.ParseInLocation(layout, raw, location)
		if err == nil {
			return ts, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no time layouts configured")
	}
	return time.Time{}, lastErr
}

func csvColumnIndex(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, name := range header {
		normalized := strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))
		columns[normalized] = index
	}
	return columns
}

func firstColumnIndex(columns map[string]int, names []string) (int, bool) {
	for _, name := range names {
		if idx, ok := columns[strings.TrimSpace(name)]; ok {
			return idx, true
		}
	}
	return 0, false
}

func isEntryRecord(record []string, hasType bool, typeIndex int, hasSignal bool, signalIndex int, matchers []string) bool {
	if len(matchers) == 0 {
		return true
	}
	if hasType && typeIndex < len(record) {
		if matchesAny(record[typeIndex], []string{"出场", "平仓", "exit", "close"}) {
			return false
		}
		if matchesAny(record[typeIndex], matchers) {
			return true
		}
	}
	if hasSignal && signalIndex < len(record) {
		return matchesAny(record[signalIndex], matchers)
	}
	return false
}

func matchesAny(raw string, matchers []string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))
	for _, matcher := range matchers {
		matcher = strings.ToLower(strings.TrimSpace(matcher))
		if matcher != "" && strings.Contains(value, matcher) {
			return true
		}
	}
	return false
}
