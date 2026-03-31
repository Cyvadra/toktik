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

// AddColumn adds a named column. Returns an error if the data length does not
// match the DataSet length, which would indicate a programming mistake in the
// DataFeed implementation.
func (ds *DataSet) AddColumn(name string, data []float64) error {
	if ds.Len > 0 && len(data) != ds.Len {
		return fmt.Errorf("backtest.DataSet.AddColumn(%q): length %d != DataSet.Len %d", name, len(data), ds.Len)
	}
	ds.Columns[name] = data
	return nil
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

// Slice returns a new DataSet containing bars in [startBar, endBar).
// Returns an error if the range is out of bounds.
func (ds *DataSet) Slice(startBar, endBar int) (*DataSet, error) {
	if startBar < 0 || endBar > ds.Len || startBar >= endBar {
		return nil, fmt.Errorf("backtest.DataSet.Slice(%d, %d): out of bounds (Len=%d)", startBar, endBar, ds.Len)
	}
	n := endBar - startBar
	out := &DataSet{
		Timestamps: make([]time.Time, n),
		Columns:    make(map[string][]float64, len(ds.Columns)),
		Len:        n,
	}
	copy(out.Timestamps, ds.Timestamps[startBar:endBar])
	for name, col := range ds.Columns {
		sliced := make([]float64, n)
		copy(sliced, col[startBar:endBar])
		out.Columns[name] = sliced
	}
	return out, nil
}

// Clone returns a deep copy of the DataSet.
func (ds *DataSet) Clone() *DataSet {
	out := &DataSet{
		Timestamps: make([]time.Time, ds.Len),
		Columns:    make(map[string][]float64, len(ds.Columns)),
		Len:        ds.Len,
	}
	copy(out.Timestamps, ds.Timestamps)
	for name, col := range ds.Columns {
		dup := make([]float64, len(col))
		copy(dup, col)
		out.Columns[name] = dup
	}
	return out
}
