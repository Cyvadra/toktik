package polymarket

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type EventType string

const (
	EventBook           EventType = "book"
	EventPriceChange    EventType = "price_change"
	EventLastTradePrice EventType = "last_trade_price"
	EventTickSizeChange EventType = "tick_size_change"
)

type Side int8

const (
	SideUnknown Side = 0
	SideBid     Side = 1
	SideAsk     Side = -1
)

const (
	PriceScale int64 = 10_000
	SizeScale  int64 = 1_000_000
)

var ErrBookNotInitialized = errors.New("order book has no base snapshot")

type Level struct {
	Price int64
	Size  int64
}

type Event struct {
	Type        EventType
	Side        Side
	Price       int64
	Size        int64
	BidsJSON    string
	AsksJSON    string
	BestBid     int64
	BestAsk     int64
	HasBestBid  bool
	HasBestAsk  bool
	NewTickSize int64
}

type Book struct {
	bids        map[int64]int64
	asks        map[int64]int64
	initialized bool
	tickSize    int64
}

func NewBook() *Book {
	return &Book{
		bids: make(map[int64]int64),
		asks: make(map[int64]int64),
	}
}

func (book *Book) Apply(event Event) error {
	switch event.Type {
	case EventBook:
		bids, err := parseLevels(event.BidsJSON)
		if err != nil {
			return fmt.Errorf("parse bids: %w", err)
		}
		asks, err := parseLevels(event.AsksJSON)
		if err != nil {
			return fmt.Errorf("parse asks: %w", err)
		}
		book.bids = bids
		book.asks = asks
		book.initialized = true
	case EventPriceChange:
		if !book.initialized {
			return ErrBookNotInitialized
		}
		if event.Price <= 0 || event.Size < 0 {
			return fmt.Errorf("invalid price change price=%d size=%d", event.Price, event.Size)
		}
		levels, err := book.levelsForSide(event.Side)
		if err != nil {
			return err
		}
		if event.Size == 0 {
			delete(levels, event.Price)
		} else {
			levels[event.Price] = event.Size
		}
	case EventTickSizeChange:
		if event.NewTickSize <= 0 {
			return fmt.Errorf("invalid tick size %d", event.NewTickSize)
		}
		book.tickSize = event.NewTickSize
	case EventLastTradePrice:
		// Trades are observations. The price_change stream owns L2 mutations.
	default:
		return fmt.Errorf("unsupported event type %q", event.Type)
	}

	return nil
}

func (book *Book) Initialized() bool { return book.initialized }

func (book *Book) TickSize() int64 { return book.tickSize }

func (book *Book) BestBid() (Level, bool) {
	return bestLevel(book.bids, true)
}

func (book *Book) BestAsk() (Level, bool) {
	return bestLevel(book.asks, false)
}

func (book *Book) Levels(side Side) []Level {
	levels, err := book.levelsForSide(side)
	if err != nil {
		return nil
	}
	out := make([]Level, 0, len(levels))
	for price, size := range levels {
		out = append(out, Level{Price: price, Size: size})
	}
	sort.Slice(out, func(i, j int) bool {
		if side == SideBid {
			return out[i].Price > out[j].Price
		}
		return out[i].Price < out[j].Price
	})
	return out
}

func (book *Book) SnapshotEvent() (Event, error) {
	if !book.initialized {
		return Event{}, ErrBookNotInitialized
	}
	bids, err := encodeLevels(book.Levels(SideBid))
	if err != nil {
		return Event{}, fmt.Errorf("encode bids: %w", err)
	}
	asks, err := encodeLevels(book.Levels(SideAsk))
	if err != nil {
		return Event{}, fmt.Errorf("encode asks: %w", err)
	}
	return Event{Type: EventBook, BidsJSON: bids, AsksJSON: asks, NewTickSize: book.tickSize}, nil
}

func encodeLevels(levels []Level) (string, error) {
	values := make([][2]string, 0, len(levels))
	for _, level := range levels {
		values = append(values, [2]string{formatFixed(level.Price, PriceScale), formatFixed(level.Size, SizeScale)})
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func formatFixed(value, scale int64) string {
	whole := value / scale
	fraction := value % scale
	if fraction < 0 {
		fraction = -fraction
	}
	digits := len(strconv.FormatInt(scale, 10)) - 1
	formatted := fmt.Sprintf("%d.%0*d", whole, digits, fraction)
	return strings.TrimRight(strings.TrimRight(formatted, "0"), ".")
}

func (book *Book) levelsForSide(side Side) (map[int64]int64, error) {
	switch side {
	case SideBid:
		return book.bids, nil
	case SideAsk:
		return book.asks, nil
	default:
		return nil, fmt.Errorf("unknown book side %d", side)
	}
}

// ValidatePublishedQuotes compares a reconstructed state with source quotes at
// a known batch or checkpoint boundary. PMXT flattens websocket price-change
// batches, so callers must not invoke this after every individual row.
func (book *Book) ValidatePublishedQuotes(bestBid int64, hasBestBid bool, bestAsk int64, hasBestAsk bool) error {
	if hasBestBid {
		best, ok := book.BestBid()
		if !ok || best.Price != bestBid {
			return fmt.Errorf("published best bid %d does not match reconstructed book", bestBid)
		}
	}
	if hasBestAsk {
		best, ok := book.BestAsk()
		if !ok || best.Price != bestAsk {
			return fmt.Errorf("published best ask %d does not match reconstructed book", bestAsk)
		}
	}
	return nil
}

func bestLevel(levels map[int64]int64, highest bool) (Level, bool) {
	var best Level
	found := false
	for price, size := range levels {
		if !found || highest && price > best.Price || !highest && price < best.Price {
			best = Level{Price: price, Size: size}
			found = true
		}
	}
	return best, found
}

func parseLevels(raw string) (map[int64]int64, error) {
	var values [][]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	levels := make(map[int64]int64, len(values))
	for index, value := range values {
		if len(value) != 2 {
			return nil, fmt.Errorf("level %d has %d values", index, len(value))
		}
		price, err := ParseFixed(value[0], PriceScale)
		if err != nil {
			return nil, fmt.Errorf("level %d price: %w", index, err)
		}
		size, err := ParseFixed(value[1], SizeScale)
		if err != nil {
			return nil, fmt.Errorf("level %d size: %w", index, err)
		}
		if price <= 0 || size < 0 {
			return nil, fmt.Errorf("level %d has invalid price=%d size=%d", index, price, size)
		}
		if size > 0 {
			levels[price] = size
		}
	}
	return levels, nil
}

func ParseFixed(value string, scale int64) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || scale <= 0 {
		return 0, fmt.Errorf("invalid fixed-point value %q", value)
	}
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimPrefix(value, "-")
	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid decimal %q", value)
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	digits := len(strconv.FormatInt(scale, 10)) - 1
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > digits {
		return 0, fmt.Errorf("decimal %q exceeds scale %d", value, scale)
	}
	fraction += strings.Repeat("0", digits-len(fraction))
	fractionValue := int64(0)
	if fraction != "" {
		fractionValue, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	result := whole*scale + fractionValue
	if negative {
		result = -result
	}
	return result, nil
}
