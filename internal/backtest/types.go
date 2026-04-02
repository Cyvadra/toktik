package backtest

// This file defines domain-specific type aliases for improved code clarity
// and future extensibility. These types document the semantic meaning of
// numeric values used throughout the backtest engine.
//
// While these are currently type aliases (no runtime overhead), they can
// be promoted to distinct types with validation if needed.

// BarIdx represents a 0-based bar index in a time series.
// The first bar is index 0.
type BarIdx = int

// SpreadID is a unique identifier for a spread position.
// Valid IDs are > 0; 0 indicates an invalid/unassigned spread.
type SpreadID = int

// LegIdx is the 0-based index of a leg within a spread.
type LegIdx = int

// GroupID is a unique identifier for a spread group.
// 0 indicates an ungrouped spread.
type GroupID = int

// OrderID is a unique identifier for an order.
// Valid IDs are > 0.
type OrderID = int

// TradeID is a unique identifier for a filled trade.
// Valid IDs are > 0.
type TradeID = int

// Qty represents a quantity (number of contracts or shares).
// Should be positive for normal orders.
type Qty = float64

// Price represents a price per unit (e.g., option premium, stock price).
// Should be positive for valid prices.
type Price = float64

// Premium is an alias for Price specifically for option premiums.
type Premium = Price

// Notional represents a dollar/base-currency amount.
type Notional = float64

// Fraction represents a value in the range [0, 1], such as percentages
// expressed as decimals (e.g., 0.05 for 5%).
type Fraction = float64

// DeltaValue represents an option delta in the range [-1, 1].
type DeltaValue = float64

// DTE represents Days To Expiration as an integer.
type DTE = int
