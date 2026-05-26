package vix

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHistoryURL  = "https://cdn.cboe.com/api/global/us_indices/daily_prices/VIX_History.csv"
	defaultHTTPTimeout = 2 * time.Minute
	dateLayout         = "01/02/2006"
)

type Bar struct {
	Date  time.Time
	Open  float64
	High  float64
	Low   float64
	Close float64
}

type Client struct {
	historyURL string
	httpClient *http.Client
}

type Option func(*Client)

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithHistoryURL(historyURL string) Option {
	return func(c *Client) {
		trimmed := strings.TrimSpace(historyURL)
		if trimmed != "" {
			c.historyURL = trimmed
		}
	}
}

func New(opts ...Option) *Client {
	client := &Client{
		historyURL: DefaultHistoryURL,
		httpClient: &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: http.DefaultTransport,
		},
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

func (c *Client) FetchHistory(ctx context.Context) ([]Bar, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.historyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cboe vix: build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cboe vix: fetch history: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("cboe vix: upstream status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	rows, err := ParseCSV(resp.Body)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func ParseCSV(r io.Reader) ([]Bar, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("cboe vix: read csv: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("cboe vix: empty csv")
	}
	if err := validateHeader(records[0]); err != nil {
		return nil, err
	}
	rows := make([]Bar, 0, max(0, len(records)-1))
	for index, record := range records[1:] {
		if len(record) == 0 {
			continue
		}
		bar, err := parseRecord(record)
		if err != nil {
			return nil, fmt.Errorf("cboe vix: parse row %d: %w", index+2, err)
		}
		rows = append(rows, bar)
	}
	return rows, nil
}

func validateHeader(header []string) error {
	expected := []string{"DATE", "OPEN", "HIGH", "LOW", "CLOSE"}
	if len(header) < len(expected) {
		return fmt.Errorf("cboe vix: invalid header %q", header)
	}
	for index, column := range expected {
		if strings.TrimSpace(strings.ToUpper(header[index])) != column {
			return fmt.Errorf("cboe vix: invalid header %q", header)
		}
	}
	return nil
}

func parseRecord(record []string) (Bar, error) {
	if len(record) < 5 {
		return Bar{}, fmt.Errorf("expected 5 columns, got %d", len(record))
	}
	date, err := time.Parse(dateLayout, strings.TrimSpace(record[0]))
	if err != nil {
		return Bar{}, fmt.Errorf("parse date: %w", err)
	}
	open, err := strconv.ParseFloat(strings.TrimSpace(record[1]), 64)
	if err != nil {
		return Bar{}, fmt.Errorf("parse open: %w", err)
	}
	high, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
	if err != nil {
		return Bar{}, fmt.Errorf("parse high: %w", err)
	}
	low, err := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
	if err != nil {
		return Bar{}, fmt.Errorf("parse low: %w", err)
	}
	close, err := strconv.ParseFloat(strings.TrimSpace(record[4]), 64)
	if err != nil {
		return Bar{}, fmt.Errorf("parse close: %w", err)
	}
	return Bar{Date: date.UTC(), Open: open, High: high, Low: low, Close: close}, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
