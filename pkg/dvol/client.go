package dvol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client wraps Deribit public API methods for DVOL history.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a DVOL client.
func NewClient(baseURL string) *Client {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		baseURL: base,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SupportsCurrency probes whether a currency is accepted by Deribit DVOL endpoint.
// A currency can be accepted even if current window returns zero rows.
func (c *Client) SupportsCurrency(ctx context.Context, currency, resolution string) (bool, error) {
	cur := normalizeCurrency(currency)
	res, err := normalizeResolution(resolution)
	if err != nil {
		return false, err
	}

	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	_, _, err = c.fetchPage(ctx, cur, res, start.UnixMilli(), end.UnixMilli())
	if err == nil {
		return true, nil
	}

	if strings.Contains(strings.ToLower(err.Error()), "invalid currency") {
		return false, nil
	}
	return false, err
}

// GetHistory fetches DVOL OHLC bars for one currency and resolution in [start, end].
// It transparently follows Deribit continuation pagination and de-duplicates rows.
func (c *Client) GetHistory(ctx context.Context, currency, resolution string, start, end time.Time) ([]Bar, error) {
	cur := normalizeCurrency(currency)
	if cur == "" {
		return nil, fmt.Errorf("currency is required")
	}
	res, err := normalizeResolution(resolution)
	if err != nil {
		return nil, err
	}

	startMs := start.UTC().UnixMilli()
	endMs := end.UTC().UnixMilli()
	if endMs < startMs {
		return nil, fmt.Errorf("end must be >= start")
	}

	rowsByTS := make(map[int64]Bar)
	currentEnd := endMs

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		chunk, continuation, err := c.fetchPage(ctx, cur, res, startMs, currentEnd)
		if err != nil {
			return nil, err
		}

		for _, b := range chunk {
			ts := b.Timestamp.UnixMilli()
			if ts < startMs || ts > endMs {
				continue
			}
			rowsByTS[ts] = b
		}

		if continuation == nil || *continuation <= startMs {
			break
		}
		if *continuation >= currentEnd {
			break
		}
		currentEnd = *continuation
	}

	out := make([]Bar, 0, len(rowsByTS))
	for _, row := range rowsByTS {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})

	return out, nil
}

func (c *Client) fetchPage(ctx context.Context, currency, resolution string, startMs, endMs int64) ([]Bar, *int64, error) {
	q := url.Values{}
	q.Set("currency", currency)
	q.Set("index_name", currency+"-DVOL")
	q.Set("resolution", resolution)
	q.Set("start_timestamp", strconv.FormatInt(startMs, 10))
	q.Set("end_timestamp", strconv.FormatInt(endMs, 10))

	u := c.baseURL + "/api/v2/public/get_volatility_index_data?" + q.Encode()
	body, err := c.getWithRetry(ctx, u)
	if err != nil {
		return nil, nil, err
	}

	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, fmt.Errorf("decode json: %w", err)
	}

	if parsed.Error != nil {
		reason := ""
		if parsed.Error.Data != nil && parsed.Error.Data.Reason != "" {
			reason = " (" + parsed.Error.Data.Reason + ")"
		}
		return nil, nil, fmt.Errorf("Deribit error code=%d message=%s%s", parsed.Error.Code, parsed.Error.Message, reason)
	}
	if parsed.Result == nil {
		return nil, nil, fmt.Errorf("Deribit response missing result")
	}

	bars := make([]Bar, 0, len(parsed.Result.Data))
	for _, row := range parsed.Result.Data {
		if len(row) < 5 {
			continue
		}
		ts := int64(row[0])
		bars = append(bars, Bar{
			Currency:   currency,
			IndexName:  currency + "-DVOL",
			Resolution: resolution,
			Timestamp:  time.UnixMilli(ts).UTC(),
			Open:       row[1],
			High:       row[2],
			Low:        row[3],
			Close:      row[4],
		})
	}

	return bars, parsed.Result.Continuation, nil
}

func (c *Client) getWithRetry(ctx context.Context, url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request Deribit: %w", err)
			if isTemporaryNetErr(err) {
				continue
			}
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}

		if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	if lastErr == nil {
		lastErr = errors.New("unknown request error")
	}
	return nil, lastErr
}

func isTemporaryNetErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "tempor") || strings.Contains(msg, "eof")
}

func normalizeCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

func normalizeResolution(resolution string) (string, error) {
	raw := strings.TrimSpace(resolution)
	if raw == "" {
		return "60", nil
	}

	switch strings.ToLower(raw) {
	case "1", "1s":
		return "1", nil
	case "60", "1m":
		return "60", nil
	case "3600", "1h":
		return "3600", nil
	case "43200", "12h":
		return "43200", nil
	case "86400", "1d", "1day", "1D":
		return "86400", nil
	default:
		if raw == "1D" {
			return "86400", nil
		}
	}

	return "", fmt.Errorf("unsupported resolution %q (supported: 1, 60, 3600, 43200, 86400 or aliases 1s/1m/1h/12h/1d)", resolution)
}
