package signals

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SignalAction describes the intent of a signal event.
type SignalAction string

const (
	ActionInit  SignalAction = "init"
	ActionAdd   SignalAction = "add"
	ActionClose SignalAction = "close"
	ActionRoll  SignalAction = "roll"
	ActionOther SignalAction = ""
)

// SignalDirection describes the directional intent.
type SignalDirection string

const (
	DirectionLong  SignalDirection = "long"
	DirectionShort SignalDirection = "short"
	DirectionFlat  SignalDirection = "flat"
	DirectionNone  SignalDirection = ""
)

// SignalEvent is a structured signal entry carrying metadata beyond a timestamp.
type SignalEvent struct {
	// ID is a unique identifier generated from (Time + Source + Action) hash.
	// It is populated automatically by LoadEvents if not set.
	ID string

	// Time is the signal timestamp in UTC.
	Time time.Time

	// Name is the human-readable signal/order name.
	Name string

	// Direction is the positional intent: "long", "short", "flat", or "".
	Direction SignalDirection

	// Action is the order intent: "init", "add", "close", "roll", or "".
	Action SignalAction

	// Qty is an optional quantity or notional amount.
	Qty float64

	// Remarks is a free-form human-readable annotation.
	Remarks string

	// Source identifies where this signal came from (file path, system name, etc.).
	Source string

	// Tags is a set of arbitrary labels for filtering/grouping.
	Tags []string

	// GroupRef is an optional group identifier for linking orders.
	GroupRef string

	// Ref is an optional unique reference for order tracking.
	Ref string

	// Meta carries arbitrary key-value metadata for future extensions.
	Meta map[string]string

	// Priority is for ordering multiple signals on the same bar.
	Priority int

	// reserved fields for forward compatibility
	_ [2]string
}

// GenerateID computes a deterministic ID from the event's Time, Source, Action, and Name.
func (e *SignalEvent) GenerateID() string {
	payload := fmt.Sprintf("%d|%s|%s|%s", e.Time.UnixNano(), e.Source, e.Action, e.Name)
	h := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", h[:8])
}

// LoadEvents loads structured signal events from the given paths.
// It extends the LoadTimes functionality by parsing additional metadata columns.
func LoadEvents(cfg Config) ([]SignalEvent, error) {
	if len(cfg.Paths) == 0 {
		return nil, nil
	}
	var result []SignalEvent
	for _, rawPath := range cfg.Paths {
		path := trimPath(rawPath)
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
		events, err := loadOneEvents(resolved, cfg)
		if err != nil {
			return nil, fmt.Errorf("load events %s: %w", resolved, err)
		}
		result = append(result, events...)
	}
	for i := range result {
		if result[i].ID == "" {
			result[i].ID = result[i].GenerateID()
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Time.Equal(result[j].Time) {
			return result[i].Priority < result[j].Priority
		}
		return result[i].Time.Before(result[j].Time)
	})
	return result, nil
}

// EventsToTimes converts a slice of SignalEvent to the legacy map[int64]struct{} format.
func EventsToTimes(events []SignalEvent) map[int64]struct{} {
	m := make(map[int64]struct{}, len(events))
	for _, e := range events {
		m[e.Time.UTC().Unix()] = struct{}{}
	}
	return m
}

// BuildEventSeries constructs multi-column series from events aligned to bar timestamps.
// Returns a map of column names to float64 slices.
//
// Columns produced:
//
//	<prefix>           — 1.0 at signal bar, 0.0 otherwise (binary trigger)
//	<prefix>_direction — 1.0 for long, -1.0 for short, 0.0 for flat/none
//	<prefix>_action    — 1.0 for init, 2.0 for add, 3.0 for close, 4.0 for roll, 0.0 otherwise
//	<prefix>_qty       — quantity field (NaN if not set)
//	<prefix>_priority  — priority field (NaN if not set)
func BuildEventSeries(timestamps []time.Time, events []SignalEvent, prefix string) map[string][]float64 {
	n := len(timestamps)
	trigger := make([]float64, n)
	direction := make([]float64, n)
	action := make([]float64, n)
	qty := make([]float64, n)
	priority := make([]float64, n)
	for i := range qty {
		qty[i] = math.NaN()
		priority[i] = math.NaN()
	}

	// Index events by unix second for O(1) lookup.
	type eventGroup struct {
		events []SignalEvent
	}
	eventIndex := make(map[int64]*eventGroup, len(events))
	for _, e := range events {
		key := e.Time.UTC().Unix()
		eg, ok := eventIndex[key]
		if !ok {
			eg = &eventGroup{}
			eventIndex[key] = eg
		}
		eg.events = append(eg.events, e)
	}

	for i, ts := range timestamps {
		key := ts.UTC().Unix()
		eg, ok := eventIndex[key]
		if !ok {
			continue
		}
		// Use the first (highest priority) event for the main columns.
		e := eg.events[0]
		trigger[i] = float64(len(eg.events))
		direction[i] = directionToFloat(e.Direction)
		action[i] = actionToFloat(e.Action)
		if e.Qty != 0 {
			qty[i] = e.Qty
		}
		if e.Priority != 0 {
			priority[i] = float64(e.Priority)
		}
	}

	return map[string][]float64{
		prefix:                trigger,
		prefix + "_direction": direction,
		prefix + "_action":    action,
		prefix + "_qty":       qty,
		prefix + "_priority":  priority,
	}
}

// EventsAtTime returns all events matching a given bar timestamp.
func EventsAtTime(events []SignalEvent, t time.Time) []SignalEvent {
	target := t.UTC().Unix()
	var result []SignalEvent
	for _, e := range events {
		if e.Time.UTC().Unix() == target {
			result = append(result, e)
		}
	}
	return result
}

func directionToFloat(d SignalDirection) float64 {
	switch d {
	case DirectionLong:
		return 1
	case DirectionShort:
		return -1
	case DirectionFlat:
		return 0
	default:
		return 0
	}
}

func actionToFloat(a SignalAction) float64 {
	switch a {
	case ActionInit:
		return 1
	case ActionAdd:
		return 2
	case ActionClose:
		return 3
	case ActionRoll:
		return 4
	default:
		return 0
	}
}

func trimPath(p string) string {
	for _, c := range []byte{' ', '\t', '\n', '\r'} {
		for len(p) > 0 && p[0] == c {
			p = p[1:]
		}
		for len(p) > 0 && p[len(p)-1] == c {
			p = p[:len(p)-1]
		}
	}
	return p
}

// loadOneEvents loads events from a single file (CSV or text).
func loadOneEvents(path string, cfg Config) ([]SignalEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err == nil && len(records) > 0 {
		if events, ok, csvErr := loadCSVEvents(path, records, cfg); ok {
			return events, csvErr
		}
	}
	return loadTextEvents(path, cfg)
}

// loadCSVEvents parses structured events from CSV records.
func loadCSVEvents(path string, records [][]string, cfg Config) ([]SignalEvent, bool, error) {
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

	// Resolve optional rich columns.
	nameIdx, hasName := colIdx(columns, cfg.NameColumn)
	dirIdx, hasDir := colIdx(columns, cfg.DirectionColumn)
	actIdx, hasAct := colIdx(columns, cfg.ActionColumn)
	remIdx, hasRem := colIdx(columns, cfg.RemarksColumn)
	qtyIdx, hasQty := colIdx(columns, cfg.QtyColumn)
	refIdx, hasRef := colIdx(columns, cfg.RefColumn)
	grpIdx, hasGrp := colIdx(columns, cfg.GroupRefColumn)
	metaIdxs := make(map[string]int)
	for _, mc := range cfg.MetaColumns {
		if idx, ok := colIdx(columns, mc); ok {
			metaIdxs[mc] = idx
		}
	}

	var out []SignalEvent
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

		ev := SignalEvent{
			Time:   ts.UTC(),
			Source: cfg.SourceLabel,
		}
		if ev.Source == "" {
			ev.Source = path
		}
		if hasName && nameIdx < len(record) {
			ev.Name = strings.TrimSpace(record[nameIdx])
		}
		if hasDir && dirIdx < len(record) {
			ev.Direction = parseDirection(record[dirIdx])
		}
		if hasAct && actIdx < len(record) {
			ev.Action = parseAction(record[actIdx])
		} else if hasType && typeIndex < len(record) {
			ev.Action = parseAction(record[typeIndex])
		}
		if hasRem && remIdx < len(record) {
			ev.Remarks = strings.TrimSpace(record[remIdx])
		}
		if hasQty && qtyIdx < len(record) {
			if q, convErr := strconv.ParseFloat(strings.TrimSpace(record[qtyIdx]), 64); convErr == nil {
				ev.Qty = q
			}
		}
		if hasRef && refIdx < len(record) {
			ev.Ref = strings.TrimSpace(record[refIdx])
		}
		if hasGrp && grpIdx < len(record) {
			ev.GroupRef = strings.TrimSpace(record[grpIdx])
		}
		if len(metaIdxs) > 0 {
			ev.Meta = make(map[string]string, len(metaIdxs))
			for col, idx := range metaIdxs {
				if idx < len(record) {
					ev.Meta[col] = strings.TrimSpace(record[idx])
				}
			}
		}
		out = append(out, ev)
	}
	return out, true, nil
}

// loadTextEvents parses events from a text file (one timestamp per line).
func loadTextEvents(path string, cfg Config) ([]SignalEvent, error) {
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

	var out []SignalEvent
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
		ev := SignalEvent{
			Time:   ts.UTC(),
			Source: cfg.SourceLabel,
		}
		if ev.Source == "" {
			ev.Source = path
		}
		out = append(out, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

func colIdx(columns map[string]int, name string) (int, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	idx, ok := columns[name]
	return idx, ok
}

func parseDirection(raw string) SignalDirection {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "long", "buy", "多", "做多":
		return DirectionLong
	case "short", "sell", "空", "做空":
		return DirectionShort
	case "flat", "neutral", "平":
		return DirectionFlat
	default:
		return DirectionNone
	}
}

func parseAction(raw string) SignalAction {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(v, "init") || strings.Contains(v, "首仓") || strings.Contains(v, "爆发"):
		return ActionInit
	case strings.Contains(v, "add") || strings.Contains(v, "加仓"):
		return ActionAdd
	case strings.Contains(v, "close") || strings.Contains(v, "exit") || strings.Contains(v, "平仓") || strings.Contains(v, "出场"):
		return ActionClose
	case strings.Contains(v, "roll") || strings.Contains(v, "换仓"):
		return ActionRoll
	default:
		return ActionOther
	}
}
