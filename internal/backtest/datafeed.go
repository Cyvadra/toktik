package backtest

import (
	"context"
	"fmt"
	"time"
)

// DataRequest describes what data to load from a feed.
type DataRequest struct {
	Market   string    // e.g. "crypto-options", "crypto-perps", "us-stock-options"
	Symbol   string    // market-specific symbol string
	Interval string    // "1m", "5m", "1h", etc.
	From     time.Time // inclusive start
	To       time.Time // exclusive end
}

// DataFeed loads market data into columnar DataSets.
// Each market type (crypto options, perps, stocks, etc.) implements this interface.
type DataFeed interface {
	// Load fetches bars for the given request and returns a columnar DataSet.
	Load(ctx context.Context, req DataRequest) (*DataSet, error)

	// Fields returns the list of field names this feed provides,
	// e.g. ["open", "high", "low", "close", "volume", "delta", "gamma", ...].
	Fields() []string
}

// DataSet is a columnar store of time-series bar data.
// Each field is a contiguous []float64 slice for cache-friendly vectorized computation.
type DataSet struct {
	Timestamps []time.Time          // bar timestamps, sorted ascending
	Columns    map[string][]float64 // field name → values, all same length as Timestamps
	Len        int                  // number of bars
}

// NewDataSet creates a DataSet with the given capacity.
func NewDataSet(capacity int) *DataSet {
	return &DataSet{
		Timestamps: make([]time.Time, 0, capacity),
		Columns:    make(map[string][]float64),
		Len:        0,
	}
}

// AddColumn adds a named column. Panics if length does not match Len.
func (ds *DataSet) AddColumn(name string, data []float64) {
	if ds.Len > 0 && len(data) != ds.Len {
		panic(fmt.Sprintf("backtest.DataSet.AddColumn(%q): length %d != DataSet.Len %d", name, len(data), ds.Len))
	}
	ds.Columns[name] = data
}

// Column returns the named column or an empty slice if not found.
func (ds *DataSet) Column(name string) []float64 {
	if col, ok := ds.Columns[name]; ok {
		return col
	}
	return nil
}

// SetTimestamps sets timestamps and updates Len.
func (ds *DataSet) SetTimestamps(ts []time.Time) {
	ds.Timestamps = ts
	ds.Len = len(ts)
}
