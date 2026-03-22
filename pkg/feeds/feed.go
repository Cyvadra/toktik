package feeds

import (
	"context"
	"time"
)

// Feed is the interface that external data sources must implement.
// Each feed registers itself via Register() in its init() function.
type Feed interface {
	// Name returns the unique, lowercase identifier for this feed (e.g., "dvol").
	// Used in table names: feed_<name>_<window>.
	Name() string

	// Fields returns the field names this feed provides.
	// Every feed implicitly provides ["open", "high", "low", "close"];
	// additional fields are returned here (e.g., ["volume"]).
	Fields() []string

	// Symbols returns the known symbols this feed supports (e.g., ["BTC", "ETH"]).
	Symbols() []string

	// SourceWindows returns the native time windows this feed can fetch
	// from its external API. The platform aggregates to other windows automatically.
	SourceWindows() []Window

	// Fetch retrieves raw bars from the external source.
	Fetch(ctx context.Context, req FetchRequest) ([]Bar, error)
}

// FetchRequest describes a data fetch against an external source.
type FetchRequest struct {
	Symbol string
	Window Window
	Start  time.Time
	End    time.Time
}

// Bar is a single OHLC data point from a feed.
type Bar struct {
	Symbol    string
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
}
