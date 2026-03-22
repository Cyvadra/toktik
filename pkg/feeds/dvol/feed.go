package dvol

import (
	"context"

	"github.com/Cyvadra/toktik/pkg/feeds"
)

const feedName = "dvol"

// dvolFeed implements feeds.Feed for Deribit Volatility Index data.
type dvolFeed struct {
	client *Client
}

func init() {
	feeds.Register(&dvolFeed{
		client: NewClient(DefaultBaseURL),
	})
}

func (f *dvolFeed) Name() string { return feedName }

func (f *dvolFeed) Fields() []string {
	return []string{"open", "high", "low", "close"}
}

func (f *dvolFeed) Symbols() []string {
	return AcceptedCurrencies
}

func (f *dvolFeed) SourceWindows() []feeds.Window {
	return []feeds.Window{
		{Label: "1m", Duration: 60_000_000_000},          // 1 minute
		{Label: "1h", Duration: 3_600_000_000_000},       // 1 hour
		{Label: "12h", Duration: 12 * 3_600_000_000_000}, // 12 hours
		{Label: "1d", Duration: 24 * 3_600_000_000_000},  // 1 day
	}
}

// Fetch retrieves DVOL bars from Deribit for the given symbol and window.
func (f *dvolFeed) Fetch(ctx context.Context, req feeds.FetchRequest) ([]feeds.Bar, error) {
	resolution := windowToResolution(req.Window)

	bars, err := f.client.GetHistory(ctx, req.Symbol, resolution, req.Start, req.End)
	if err != nil {
		return nil, err
	}

	out := make([]feeds.Bar, len(bars))
	for i, b := range bars {
		out[i] = feeds.Bar{
			Symbol:    b.Symbol,
			Timestamp: b.Timestamp,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
		}
	}
	return out, nil
}

// NewFeedWithClient creates a DVOL feed with a custom client (for sync commands).
func NewFeedWithClient(baseURL string) feeds.Feed {
	return &dvolFeed{client: NewClient(baseURL)}
}

func windowToResolution(w feeds.Window) string {
	switch w.Label {
	case "1m":
		return "60"
	case "1h":
		return "3600"
	case "12h":
		return "43200"
	case "1d":
		return "86400"
	default:
		return "60" // default to 1m
	}
}
