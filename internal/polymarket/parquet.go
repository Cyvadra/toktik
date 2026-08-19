package polymarket

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

const pmxtColumnCount = 16

var ErrPMXTFileRead = errors.New("read PMXT file")

type NullableInt64 struct {
	Value int64
	Valid bool
}

type NullableUint16 struct {
	Value uint16
	Valid bool
}

type NullableString struct {
	Value string
	Valid bool
}

type RawEvent struct {
	Key             EventKey
	ConditionID     string
	AssetID         string
	Type            EventType
	BidsJSON        NullableString
	AsksJSON        NullableString
	PriceE4         NullableInt64
	SizeE6          NullableInt64
	Side            NullableString
	BestBidE4       NullableInt64
	BestAskE4       NullableInt64
	FeeRateBPS      NullableUint16
	TransactionHash NullableString
	OldTickSizeE4   NullableInt64
	NewTickSizeE4   NullableInt64
}

func (event RawEvent) ReplayEvent() (Event, error) {
	replay := Event{
		Type:        event.Type,
		Price:       event.PriceE4.Value,
		Size:        event.SizeE6.Value,
		BidsJSON:    event.BidsJSON.Value,
		AsksJSON:    event.AsksJSON.Value,
		BestBid:     event.BestBidE4.Value,
		BestAsk:     event.BestAskE4.Value,
		HasBestBid:  event.BestBidE4.Valid,
		HasBestAsk:  event.BestAskE4.Valid,
		NewTickSize: event.NewTickSizeE4.Value,
	}
	if event.Side.Valid {
		switch event.Side.Value {
		case "BUY":
			replay.Side = SideBid
		case "SELL":
			replay.Side = SideAsk
		default:
			return Event{}, fmt.Errorf("unsupported Polymarket side %q", event.Side.Value)
		}
	}
	return replay, nil
}

type ConditionMeta struct {
	ConditionID  string
	EventID      string
	MarketID     string
	Slug         string
	Underlying   string
	Interval     string
	WindowStart  time.Time
	WindowEnd    time.Time
	StartDate    *time.Time
	EndDate      *time.Time
	Closed       bool
	Resolved     bool
	Winner       uint8
	Outcomes     []string
	OutcomeAsset []string
}

type ConditionCatalog struct {
	Conditions map[string]ConditionMeta
	Assets     map[string]string
}

type conditionMapRecord struct {
	Status       string   `json:"status"`
	EventID      string   `json:"event_id"`
	MarketID     string   `json:"market_id"`
	ConditionID  string   `json:"condition_id"`
	Slug         string   `json:"slug"`
	Asset        string   `json:"asset"`
	Period       string   `json:"period"`
	WindowStart  int64    `json:"window_start"`
	StartDate    string   `json:"start_date"`
	EndDate      string   `json:"end_date"`
	Closed       bool     `json:"closed"`
	Outcomes     []string `json:"outcomes"`
	ClobTokenIDs []string `json:"clob_token_ids"`
	TokenUp      string   `json:"token_up"`
	TokenDown    string   `json:"token_down"`
	Resolved     bool     `json:"resolved"`
	Winner       string   `json:"winner"`
}

func LoadConditionCatalog(path string) (*ConditionCatalog, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open condition map %s: %w", path, err)
	}
	defer file.Close()

	catalog := &ConditionCatalog{
		Conditions: make(map[string]ConditionMeta),
		Assets:     make(map[string]string),
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return loadConditionCatalogByCondition(file, path)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var record conditionMapRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode condition map line %d: %w", lineNumber, err)
		}
		if record.Status != "ok" {
			continue
		}
		if len(record.ConditionID) != 66 || len(record.Outcomes) != len(record.ClobTokenIDs) || len(record.Outcomes) == 0 {
			return nil, fmt.Errorf("invalid condition map line %d for %q", lineNumber, record.ConditionID)
		}
		meta, err := conditionMetaFromRecord(record)
		if err != nil {
			return nil, fmt.Errorf("condition map line %d: %w", lineNumber, err)
		}
		if meta.StartDate, err = parseOptionalTime(record.StartDate); err != nil {
			return nil, fmt.Errorf("condition map line %d start_date: %w", lineNumber, err)
		}
		if meta.EndDate, err = parseOptionalTime(record.EndDate); err != nil {
			return nil, fmt.Errorf("condition map line %d end_date: %w", lineNumber, err)
		}
		if err := addConditionMeta(catalog, meta); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan condition map %s: %w", path, err)
	}
	return catalog, nil
}

func loadConditionCatalogByCondition(file *os.File, path string) (*ConditionCatalog, error) {
	var records map[string]conditionMapRecord
	if err := json.NewDecoder(file).Decode(&records); err != nil {
		return nil, fmt.Errorf("decode condition map %s: %w", path, err)
	}
	catalog := &ConditionCatalog{
		Conditions: make(map[string]ConditionMeta),
		Assets:     make(map[string]string),
	}
	conditionIDs := make([]string, 0, len(records))
	for conditionID := range records {
		conditionIDs = append(conditionIDs, conditionID)
	}
	sort.Strings(conditionIDs)
	for _, conditionID := range conditionIDs {
		record := records[conditionID]
		record.ConditionID = conditionID
		if record.Status == "" {
			record.Status = "ok"
		}
		if record.Status != "ok" {
			continue
		}
		meta, err := conditionMetaFromRecord(record)
		if err != nil {
			return nil, fmt.Errorf("condition %s: %w", conditionID, err)
		}
		if err := addConditionMeta(catalog, meta); err != nil {
			return nil, err
		}
	}
	return catalog, nil
}

func conditionMetaFromRecord(record conditionMapRecord) (ConditionMeta, error) {
	outcomes := record.Outcomes
	outcomeAsset := record.ClobTokenIDs
	if len(outcomes) == 0 && record.TokenUp != "" && record.TokenDown != "" {
		outcomes = []string{"Up", "Down"}
		outcomeAsset = []string{record.TokenUp, record.TokenDown}
	}
	if len(record.ConditionID) != 66 || len(outcomes) != len(outcomeAsset) || len(outcomes) == 0 {
		return ConditionMeta{}, fmt.Errorf("invalid condition metadata for %q", record.ConditionID)
	}
	meta := ConditionMeta{
		ConditionID:  record.ConditionID,
		EventID:      record.EventID,
		MarketID:     record.MarketID,
		Slug:         record.Slug,
		Underlying:   strings.ToUpper(record.Asset),
		Interval:     strings.ToLower(record.Period),
		WindowStart:  time.Unix(record.WindowStart, 0).UTC(),
		Closed:       record.Closed,
		Resolved:     record.Resolved,
		Winner:       binaryWinner(record.Winner),
		Outcomes:     append([]string(nil), outcomes...),
		OutcomeAsset: append([]string(nil), outcomeAsset...),
	}
	windowDuration, err := polymarketIntervalDuration(meta.Interval)
	if err != nil {
		return ConditionMeta{}, fmt.Errorf("interval: %w", err)
	}
	meta.WindowEnd = meta.WindowStart.Add(windowDuration)
	return meta, nil
}

func addConditionMeta(catalog *ConditionCatalog, meta ConditionMeta) error {
	if existing, ok := catalog.Conditions[meta.ConditionID]; ok && existing.Slug != meta.Slug {
		return fmt.Errorf("condition %s maps to multiple slugs", meta.ConditionID)
	}
	catalog.Conditions[meta.ConditionID] = meta
	for _, assetID := range meta.OutcomeAsset {
		if conditionID, ok := catalog.Assets[assetID]; ok && conditionID != meta.ConditionID {
			return fmt.Errorf("asset %s maps to multiple conditions", assetID)
		}
		catalog.Assets[assetID] = meta.ConditionID
	}
	return nil
}

func binaryWinner(winner string) uint8 {
	switch strings.ToLower(strings.TrimSpace(winner)) {
	case "up":
		return 1
	case "down":
		return 2
	default:
		return 0
	}
}

func ScanRawEvents(ctx context.Context, path string, allowedConditions map[string]ConditionMeta, consume func(RawEvent) error) (uint64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("%w: open PMXT parquet %s: %w", ErrPMXTFileRead, path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("%w: stat PMXT parquet %s: %w", ErrPMXTFileRead, path, err)
	}
	parquetFile, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		return 0, fmt.Errorf("%w: open PMXT parquet metadata %s: %w", ErrPMXTFileRead, path, err)
	}
	if len(parquetFile.RowGroups()) == 0 {
		return 0, nil
	}

	sourceFile := filepath.Base(path)
	rowBuffer := make([]parquet.Row, 2048)
	var sourceRows uint64
	for _, rowGroup := range parquetFile.RowGroups() {
		rows := rowGroup.Rows()
		for {
			n, readErr := rows.ReadRows(rowBuffer)
			for index := 0; index < n; index++ {
				if err := ctx.Err(); err != nil {
					rows.Close()
					return sourceRows, err
				}
				row := rowBuffer[index]
				if len(row) != pmxtColumnCount {
					rows.Close()
					return sourceRows, fmt.Errorf("%w: PMXT row %d has %d columns, want %d", ErrPMXTFileRead, sourceRows, len(row), pmxtColumnCount)
				}
				conditionID := string(row[2].Bytes())
				if _, ok := allowedConditions[conditionID]; ok {
					event, err := decodePMXTRow(row, conditionID, sourceFile, sourceRows)
					if err != nil {
						rows.Close()
						return sourceRows, fmt.Errorf("%w: decode PMXT row %d: %w", ErrPMXTFileRead, sourceRows, err)
					}
					if err := consume(event); err != nil {
						rows.Close()
						return sourceRows, err
					}
				}
				sourceRows++
				rowBuffer[index] = row[:0]
			}
			if readErr != nil {
				if closeErr := rows.Close(); closeErr != nil && readErr == io.EOF {
					return sourceRows, fmt.Errorf("%w: close PMXT row group: %w", ErrPMXTFileRead, closeErr)
				}
				if readErr == io.EOF {
					break
				}
				return sourceRows, fmt.Errorf("%w: read PMXT parquet %s: %w", ErrPMXTFileRead, path, readErr)
			}
		}
	}
	return sourceRows, nil
}

func ParseArchiveFileHour(name string) (time.Time, error) {
	name = filepath.Base(name)
	const prefix = "polymarket_orderbook_"
	const suffix = ".parquet"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return time.Time{}, fmt.Errorf("invalid Polymarket archive filename %q", name)
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	timestamp, err := time.Parse("2006-01-02T15", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse Polymarket archive filename %q: %w", name, err)
	}
	return timestamp.UTC(), nil
}

func eventWithinConditionWindow(event RawEvent, meta ConditionMeta) bool {
	return !event.Key.ExchangeTime.Before(meta.WindowStart) && event.Key.ExchangeTime.Before(meta.WindowEnd)
}

func decodePMXTRow(row parquet.Row, conditionID, sourceFile string, sourceRow uint64) (RawEvent, error) {
	if len(row) != pmxtColumnCount {
		return RawEvent{}, fmt.Errorf("unexpected column count %d", len(row))
	}
	priceE4, err := nullableDecimal(row[7])
	if err != nil {
		return RawEvent{}, fmt.Errorf("price: %w", err)
	}
	sizeE6, err := nullableDecimal(row[8])
	if err != nil {
		return RawEvent{}, fmt.Errorf("size: %w", err)
	}
	bestBidE4, err := nullableDecimal(row[10])
	if err != nil {
		return RawEvent{}, fmt.Errorf("best bid: %w", err)
	}
	bestAskE4, err := nullableDecimal(row[11])
	if err != nil {
		return RawEvent{}, fmt.Errorf("best ask: %w", err)
	}
	oldTickSizeE4, err := nullableDecimal(row[14])
	if err != nil {
		return RawEvent{}, fmt.Errorf("old tick size: %w", err)
	}
	newTickSizeE4, err := nullableDecimal(row[15])
	if err != nil {
		return RawEvent{}, fmt.Errorf("new tick size: %w", err)
	}
	event := RawEvent{
		Key: EventKey{
			ReceivedTime: time.UnixMilli(row[0].Int64()).UTC(),
			ExchangeTime: time.UnixMilli(row[1].Int64()).UTC(),
			SourceFile:   sourceFile,
			SourceRow:    sourceRow,
		},
		ConditionID:     conditionID,
		Type:            EventType(row[3].String()),
		AssetID:         row[4].String(),
		BidsJSON:        nullableString(row[5]),
		AsksJSON:        nullableString(row[6]),
		PriceE4:         priceE4,
		SizeE6:          sizeE6,
		Side:            nullableString(row[9]),
		BestBidE4:       bestBidE4,
		BestAskE4:       bestAskE4,
		FeeRateBPS:      nullableUint16(row[12]),
		TransactionHash: nullableString(row[13]),
		OldTickSizeE4:   oldTickSizeE4,
		NewTickSizeE4:   newTickSizeE4,
	}
	if event.Side.Valid {
		event.Side.Value = strings.ToUpper(strings.TrimSpace(event.Side.Value))
	}
	switch event.Type {
	case EventBook, EventPriceChange, EventLastTradePrice, EventTickSizeChange:
	default:
		return RawEvent{}, fmt.Errorf("unsupported event type %q", event.Type)
	}
	return event, nil
}

func nullableString(value parquet.Value) NullableString {
	if value.IsNull() {
		return NullableString{}
	}
	return NullableString{Value: value.String(), Valid: true}
}

func nullableDecimal(value parquet.Value) (NullableInt64, error) {
	if value.IsNull() {
		return NullableInt64{}, nil
	}
	decoded, err := signedBigEndian(value.Bytes())
	if err != nil {
		return NullableInt64{}, err
	}
	return NullableInt64{Value: decoded, Valid: true}, nil
}

func nullableUint16(value parquet.Value) NullableUint16 {
	if value.IsNull() {
		return NullableUint16{}
	}
	return NullableUint16{Value: uint16(value.Uint32()), Valid: true}
}

func signedBigEndian(value []byte) (int64, error) {
	if len(value) == 0 {
		return 0, nil
	}
	negative := value[0]&0x80 != 0
	if len(value) > 8 {
		extension := byte(0)
		if negative {
			extension = 0xff
		}
		for _, leading := range value[:len(value)-8] {
			if leading != extension {
				return 0, fmt.Errorf("decimal exceeds int64 range")
			}
		}
		value = value[len(value)-8:]
		if negative != (value[0]&0x80 != 0) {
			return 0, fmt.Errorf("decimal exceeds int64 range")
		}
	}
	var padded [8]byte
	if negative {
		for index := range padded {
			padded[index] = 0xff
		}
	}
	copy(padded[len(padded)-len(value):], value)
	return int64(binary.BigEndian.Uint64(padded[:])), nil
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func polymarketIntervalDuration(interval string) (time.Duration, error) {
	switch interval {
	case "5m":
		return 5 * time.Minute, nil
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported Polymarket interval %q", interval)
	}
}
