package usmarket

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cyvadra/toktik/internal/csvutil"
)

func openCSVReader(path string) (io.ReadCloser, *csv.Reader, error) {
	return csvutil.OpenMaybeGzipCSV(path, 4*1024*1024)
}

// ParseOptionCSV reads a Polygon OPRA minute-agg CSV (optionally gzipped) and
// streams parsed OptionBar1m records into the returned channel.
// The caller must drain the channel. Any read error is stored in *readErr.
func ParseOptionCSV(path string) (<-chan OptionBar1m, *error, error) {
	reader, csvReader, err := openCSVReader(path)
	if err != nil {
		return nil, nil, err
	}

	// Read and validate header
	header, err := csvReader.Read()
	if err != nil {
		reader.Close()
		return nil, nil, fmt.Errorf("read header %s: %w", path, err)
	}
	colIdx := mapColumns(header)
	if err := requireColumns(colIdx, "ticker", "volume", "open", "close", "high", "low", "window_start", "transactions"); err != nil {
		reader.Close()
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}

	ch := make(chan OptionBar1m, 8192)
	var readErr error

	go func() {
		defer close(ch)
		defer reader.Close()

		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				readErr = fmt.Errorf("read csv row: %w", err)
				return
			}

			ticker := record[colIdx["ticker"]]
			underlying, expiration, optType, strike, err := ParseOptionTicker(ticker)
			if err != nil {
				// Skip tickers that don't match the expected format
				continue
			}

			ts := parseNanosTimestamp(record[colIdx["window_start"]])
			if ts.IsZero() {
				continue
			}

			bar := OptionBar1m{
				Timestamp:    ts,
				Symbol:       ticker,
				Underlying:   underlying,
				OptionType:   optType,
				Expiration:   expiration,
				Strike:       strike,
				Open:         parseFloat32(record[colIdx["open"]]),
				High:         parseFloat32(record[colIdx["high"]]),
				Low:          parseFloat32(record[colIdx["low"]]),
				Close:        parseFloat32(record[colIdx["close"]]),
				Volume:       parseFloat64(record[colIdx["volume"]]),
				Transactions: parseUint32(record[colIdx["transactions"]]),
			}
			ch <- bar
		}
	}()

	return ch, &readErr, nil
}

// ParseStockCSV reads a Polygon SIP minute-agg CSV (optionally gzipped) and
// streams parsed StockBar1m records into the returned channel.
func ParseStockCSV(path string) (<-chan StockBar1m, *error, error) {
	reader, csvReader, err := openCSVReader(path)
	if err != nil {
		return nil, nil, err
	}

	header, err := csvReader.Read()
	if err != nil {
		reader.Close()
		return nil, nil, fmt.Errorf("read header %s: %w", path, err)
	}
	colIdx := mapColumns(header)
	if err := requireColumns(colIdx, "ticker", "volume", "open", "close", "high", "low", "window_start", "transactions"); err != nil {
		reader.Close()
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}

	ch := make(chan StockBar1m, 8192)
	var readErr error

	go func() {
		defer close(ch)
		defer reader.Close()

		for {
			record, err := csvReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				readErr = fmt.Errorf("read csv row: %w", err)
				return
			}

			ts := parseNanosTimestamp(record[colIdx["window_start"]])
			if ts.IsZero() {
				continue
			}

			bar := StockBar1m{
				Timestamp:    ts,
				Symbol:       record[colIdx["ticker"]],
				Open:         parseFloat32(record[colIdx["open"]]),
				High:         parseFloat32(record[colIdx["high"]]),
				Low:          parseFloat32(record[colIdx["low"]]),
				Close:        parseFloat32(record[colIdx["close"]]),
				Volume:       parseFloat64(record[colIdx["volume"]]),
				Transactions: parseUint32(record[colIdx["transactions"]]),
			}
			ch <- bar
		}
	}()

	return ch, &readErr, nil
}

// CollectOptionUnderlyings returns the distinct underlyings referenced in an option CSV file.
func CollectOptionUnderlyings(path string) ([]string, error) {
	reader, csvReader, err := openCSVReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header %s: %w", path, err)
	}
	colIdx := mapColumns(header)
	if err := requireColumns(colIdx, "ticker"); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	unique := make(map[string]struct{})
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row: %w", err)
		}

		underlying, _, _, _, err := ParseOptionTicker(record[colIdx["ticker"]])
		if err != nil {
			continue
		}
		unique[underlying] = struct{}{}
	}

	underlyings := make([]string, 0, len(unique))
	for symbol := range unique {
		underlyings = append(underlyings, symbol)
	}
	sort.Strings(underlyings)
	return underlyings, nil
}

func mapColumns(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, col := range header {
		m[strings.TrimSpace(strings.ToLower(col))] = i
	}
	return m
}

func requireColumns(colIdx map[string]int, cols ...string) error {
	for _, col := range cols {
		if _, ok := colIdx[col]; !ok {
			return fmt.Errorf("missing required column %q", col)
		}
	}
	return nil
}

func parseNanosTimestamp(s string) time.Time {
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil || ns <= 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

func parseFloat32(s string) float32 {
	v, _ := strconv.ParseFloat(s, 32)
	return float32(v)
}

func parseFloat64(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	if v < 0 {
		return 0
	}
	return v
}

func parseUint32(s string) uint32 {
	v, _ := strconv.ParseFloat(s, 64)
	if v < 0 {
		return 0
	}
	return uint32(v)
}
