package cryptooptions

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/klauspost/compress/zstd"
)

// csvColumnIndex maps CSV header names to their column index.
type csvColumnIndex struct {
	exchange        int
	symbol          int
	timestamp       int
	localTimestamp  int
	optionType      int
	strikePrice     int
	expiration      int
	openInterest    int
	lastPrice       int
	bidPrice        int
	bidAmount       int
	bidIV           int
	askPrice        int
	askAmount       int
	askIV           int
	markPrice       int
	markIV          int
	underlyingIndex int
	underlyingPrice int
	delta           int
	gamma           int
	vega            int
	theta           int
	rho             int
}

func buildColumnIndex(header []string) (csvColumnIndex, error) {
	idx := csvColumnIndex{
		exchange: -1, symbol: -1, timestamp: -1, localTimestamp: -1,
		optionType: -1, strikePrice: -1, expiration: -1, openInterest: -1,
		lastPrice: -1, bidPrice: -1, bidAmount: -1, bidIV: -1,
		askPrice: -1, askAmount: -1, askIV: -1, markPrice: -1, markIV: -1,
		underlyingIndex: -1, underlyingPrice: -1,
		delta: -1, gamma: -1, vega: -1, theta: -1, rho: -1,
	}
	for i, col := range header {
		switch col {
		case "exchange":
			idx.exchange = i
		case "symbol":
			idx.symbol = i
		case "timestamp":
			idx.timestamp = i
		case "local_timestamp":
			idx.localTimestamp = i
		case "type":
			idx.optionType = i
		case "strike_price":
			idx.strikePrice = i
		case "expiration":
			idx.expiration = i
		case "open_interest":
			idx.openInterest = i
		case "last_price":
			idx.lastPrice = i
		case "bid_price":
			idx.bidPrice = i
		case "bid_amount":
			idx.bidAmount = i
		case "bid_iv":
			idx.bidIV = i
		case "ask_price":
			idx.askPrice = i
		case "ask_amount":
			idx.askAmount = i
		case "ask_iv":
			idx.askIV = i
		case "mark_price":
			idx.markPrice = i
		case "mark_iv":
			idx.markIV = i
		case "underlying_index":
			idx.underlyingIndex = i
		case "underlying_price":
			idx.underlyingPrice = i
		case "delta":
			idx.delta = i
		case "gamma":
			idx.gamma = i
		case "vega":
			idx.vega = i
		case "theta":
			idx.theta = i
		case "rho":
			idx.rho = i
		}
	}
	if idx.symbol < 0 || idx.timestamp < 0 {
		return idx, fmt.Errorf("CSV header missing required columns 'symbol' and/or 'timestamp'")
	}
	return idx, nil
}

func parseFloat32(s string) float32 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0
	}
	return float32(v)
}

func parseMicrosecondTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	us, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	sec := us / 1_000_000
	nsec := (us % 1_000_000) * 1000
	return time.Unix(sec, nsec).UTC(), nil
}

func safeGet(record []string, i int) string {
	if i < 0 || i >= len(record) {
		return ""
	}
	return record[i]
}

func parseRow(record []string, idx *csvColumnIndex) (TickRow, error) {
	ts, err := parseMicrosecondTimestamp(safeGet(record, idx.timestamp))
	if err != nil {
		return TickRow{}, fmt.Errorf("bad timestamp: %w", err)
	}

	var localTS time.Time
	if idx.localTimestamp >= 0 {
		localTS, _ = parseMicrosecondTimestamp(safeGet(record, idx.localTimestamp))
	}

	var expiration time.Time
	if idx.expiration >= 0 {
		expStr := safeGet(record, idx.expiration)
		if expStr != "" {
			expiration, _ = parseMicrosecondTimestamp(expStr)
		}
	}

	return TickRow{
		Exchange:        safeGet(record, idx.exchange),
		Symbol:          safeGet(record, idx.symbol),
		Timestamp:       ts,
		LocalTimestamp:  localTS,
		OptionType:      safeGet(record, idx.optionType),
		StrikePrice:     parseFloat32(safeGet(record, idx.strikePrice)),
		Expiration:      expiration,
		OpenInterest:    parseFloat32(safeGet(record, idx.openInterest)),
		LastPrice:       parseFloat32(safeGet(record, idx.lastPrice)),
		BidPrice:        parseFloat32(safeGet(record, idx.bidPrice)),
		BidAmount:       parseFloat32(safeGet(record, idx.bidAmount)),
		BidIV:           parseFloat32(safeGet(record, idx.bidIV)),
		AskPrice:        parseFloat32(safeGet(record, idx.askPrice)),
		AskAmount:       parseFloat32(safeGet(record, idx.askAmount)),
		AskIV:           parseFloat32(safeGet(record, idx.askIV)),
		MarkPrice:       parseFloat32(safeGet(record, idx.markPrice)),
		MarkIV:          parseFloat32(safeGet(record, idx.markIV)),
		UnderlyingIndex: safeGet(record, idx.underlyingIndex),
		UnderlyingPrice: parseFloat32(safeGet(record, idx.underlyingPrice)),
		Delta:           parseFloat32(safeGet(record, idx.delta)),
		Gamma:           parseFloat32(safeGet(record, idx.gamma)),
		Vega:            parseFloat32(safeGet(record, idx.vega)),
		Theta:           parseFloat32(safeGet(record, idx.theta)),
		Rho:             parseFloat32(safeGet(record, idx.rho)),
	}, nil
}

// ParseCSVFromZST opens a .zst file, streams decompression, and sends
// parsed TickRow values on the returned channel. The channel is closed
// when all rows have been read. Errors are logged but non-fatal rows
// are skipped.
func ParseCSVFromZST(path string) (<-chan TickRow, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}

	decoder, err := zstd.NewReader(f)
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("zstd decoder for %s: %w", path, err)
	}

	csvReader := csv.NewReader(decoder)
	csvReader.ReuseRecord = true
	csvReader.LazyQuotes = true

	header, err := csvReader.Read()
	if err != nil {
		decoder.Close()
		f.Close()
		return nil, nil, fmt.Errorf("read CSV header from %s: %w", path, err)
	}

	headerCopy := make([]string, len(header))
	copy(headerCopy, header)

	idx, err := buildColumnIndex(headerCopy)
	if err != nil {
		decoder.Close()
		f.Close()
		return nil, nil, fmt.Errorf("build column index for %s: %w", path, err)
	}

	ch := make(chan TickRow, 4096)
	closeFn := func() {
		decoder.Close()
		f.Close()
	}

	go func() {
		defer close(ch)
		lineNum := 1
		badLines := 0
		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break
			}
			lineNum++
			if err != nil {
				badLines++
				if badLines <= 10 {
					log.Printf("[csvparser] %s line %d: read error: %v", path, lineNum, err)
				}
				continue
			}
			row := make([]string, len(record))
			copy(row, record)
			tick, err := parseRow(row, &idx)
			if err != nil {
				badLines++
				if badLines <= 10 {
					log.Printf("[csvparser] %s line %d: parse error: %v", path, lineNum, err)
				}
				continue
			}
			ch <- tick
		}
		if badLines > 10 {
			log.Printf("[csvparser] %s: %d total bad lines (only first 10 logged)", path, badLines)
		}
	}()

	return ch, closeFn, nil
}
