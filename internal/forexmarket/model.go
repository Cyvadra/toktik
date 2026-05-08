package forexmarket

import "time"

// Bar1m is a 1-minute bar for a forex pair or metal-linked FX cross.
// The schema mirrors usmarket.StockBar1m so higher layers can share patterns.
type Bar1m struct {
	Timestamp    time.Time
	Symbol       string
	Open         float32
	High         float32
	Low          float32
	Close        float32
	Volume       float64
	Transactions uint32
	MarketDate   time.Time
	SessionKind  string
	IsRegular    uint8
	SessionOpen  time.Time
	SessionSeq   uint16
}
