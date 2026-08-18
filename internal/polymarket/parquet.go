package polymarket

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

const pmxtColumnCount = 16

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
		meta := ConditionMeta{
			ConditionID:  record.ConditionID,
			EventID:      record.EventID,
			MarketID:     record.MarketID,
			Slug:         record.Slug,
			Underlying:   strings.ToUpper(record.Asset),
			Interval:     strings.ToLower(record.Period),
			WindowStart:  time.Unix(record.WindowStart, 0).UTC(),
			Closed:       record.Closed,
			Outcomes:     append([]string(nil), record.Outcomes...),
			OutcomeAsset: append([]string(nil), record.ClobTokenIDs...),
		}
		if meta.StartDate, err = parseOptionalTime(record.StartDate); err != nil {
			return nil, fmt.Errorf("condition map line %d start_date: %w", lineNumber, err)
		}
		if meta.EndDate, err = parseOptionalTime(record.EndDate); err != nil {
			return nil, fmt.Errorf("condition map line %d end_date: %w", lineNumber, err)
		}
		windowDuration, err := polymarketIntervalDuration(meta.Interval)
		if err != nil {
			return nil, fmt.Errorf("condition map line %d interval: %w", lineNumber, err)
		}
		meta.WindowEnd = meta.WindowStart.Add(windowDuration)
		if existing, ok := catalog.Conditions[meta.ConditionID]; ok && existing.Slug != meta.Slug {
			return nil, fmt.Errorf("condition %s maps to multiple slugs", meta.ConditionID)
		}
		catalog.Conditions[meta.ConditionID] = meta
		for _, assetID := range meta.OutcomeAsset {
			if conditionID, ok := catalog.Assets[assetID]; ok && conditionID != meta.ConditionID {
				return nil, fmt.Errorf("asset %s maps to multiple conditions", assetID)
			}
			catalog.Assets[assetID] = meta.ConditionID
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan condition map %s: %w", path, err)
	}
	return catalog, nil
}

func StreamSelectedEvents(ctx context.Context, path string, allowedConditions map[string]ConditionMeta, consume func(RawEvent) error) (uint64, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, fmt.Errorf("open PMXT parquet %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat PMXT parquet %s: %w", path, err)
	}
	parquetFile, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		return 0, fmt.Errorf("open PMXT parquet metadata %s: %w", path, err)
	}
	if parquetFile.Schema().String() == "" || len(parquetFile.RowGroups()) == 0 {
		return 0, nil
	}

	sourceFile := filepath.Base(path)
	rowBuffer := make([]parquet.Row, 2048)
	var sourceRow uint64
	var selected uint64
	for _, rowGroup := range parquetFile.RowGroups() {
		rows := rowGroup.Rows()
		for {
			n, readErr := rows.ReadRows(rowBuffer)
			for index := 0; index < n; index++ {
				if err := ctx.Err(); err != nil {
					rows.Close()
					return selected, err
				}
				row := rowBuffer[index]
				if len(row) != pmxtColumnCount {
					rows.Close()
					return selected, fmt.Errorf("PMXT row %d has %d columns, want %d", sourceRow, len(row), pmxtColumnCount)
				}
				conditionID := string(row[2].Bytes())
				if _, ok := allowedConditions[conditionID]; ok {
					event, err := decodePMXTRow(row, sourceFile, sourceRow)
					if err != nil {
						rows.Close()
						return selected, fmt.Errorf("decode PMXT row %d: %w", sourceRow, err)
					}
					if err := consume(event); err != nil {
						rows.Close()
						return selected, err
					}
					selected++
				}
				sourceRow++
				rowBuffer[index] = row[:0]
			}
			if readErr != nil {
				if closeErr := rows.Close(); closeErr != nil && readErr == io.EOF {
					return selected, fmt.Errorf("close PMXT row group: %w", closeErr)
				}
				if readErr == io.EOF {
					break
				}
				return selected, fmt.Errorf("read PMXT parquet %s: %w", path, readErr)
			}
		}
	}
	return selected, nil
}

func decodePMXTRow(row parquet.Row, sourceFile string, sourceRow uint64) (RawEvent, error) {
	if len(row) != pmxtColumnCount {
		return RawEvent{}, fmt.Errorf("unexpected column count %d", len(row))
	}
	event := RawEvent{
		Key: EventKey{
			ReceivedTime: time.UnixMilli(row[0].Int64()).UTC(),
			ExchangeTime: time.UnixMilli(row[1].Int64()).UTC(),
			SourceFile:   sourceFile,
			SourceRow:    sourceRow,
		},
		ConditionID:     string(row[2].Bytes()),
		Type:            EventType(row[3].String()),
		AssetID:         row[4].String(),
		BidsJSON:        nullableString(row[5]),
		AsksJSON:        nullableString(row[6]),
		PriceE4:         nullableDecimal(row[7]),
		SizeE6:          nullableDecimal(row[8]),
		Side:            nullableString(row[9]),
		BestBidE4:       nullableDecimal(row[10]),
		BestAskE4:       nullableDecimal(row[11]),
		FeeRateBPS:      nullableUint16(row[12]),
		TransactionHash: nullableString(row[13]),
		OldTickSizeE4:   nullableDecimal(row[14]),
		NewTickSizeE4:   nullableDecimal(row[15]),
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

func nullableDecimal(value parquet.Value) NullableInt64 {
	if value.IsNull() {
		return NullableInt64{}
	}
	return NullableInt64{Value: signedBigEndian(value.Bytes()), Valid: true}
}

func nullableUint16(value parquet.Value) NullableUint16 {
	if value.IsNull() {
		return NullableUint16{}
	}
	return NullableUint16{Value: uint16(value.Uint32()), Valid: true}
}

func signedBigEndian(value []byte) int64 {
	if len(value) == 0 {
		return 0
	}
	var padded [8]byte
	if value[0]&0x80 != 0 {
		for index := range padded {
			padded[index] = 0xff
		}
	}
	if len(value) > len(padded) {
		value = value[len(value)-len(padded):]
	}
	copy(padded[len(padded)-len(value):], value)
	return int64(binary.BigEndian.Uint64(padded[:]))
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
