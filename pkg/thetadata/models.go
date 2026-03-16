package thetadata

import (
	"fmt"
	"hash/crc32"
	"time"
)

type Contract struct {
	Root       string
	Expiration time.Time
	Strike     float64
	Right      string
}

func (c Contract) Symbol() string {
	return fmt.Sprintf("%s-%s-%.2f-%s",
		c.Root, c.Expiration.Format("20060102"), c.Strike, c.Right)
}

func (c Contract) SymbolID() uint32 {
	return crc32.ChecksumIEEE([]byte(c.Symbol()))
}

type QuoteBar struct {
	Timestamp time.Time
	Bid       float64
	BidSize   int
	Ask       float64
	AskSize   int
}

type OHLCBar struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int
	Count     int
}

type GreeksEOD struct {
	Date            time.Time
	UnderlyingPrice float64
	ImpliedVol      float64
	Delta           float64
	Gamma           float64
	Vega            float64
	Theta           float64
	Rho             float64
	Close           float64
	Bid             float64
	Ask             float64
	Volume          int
	OpenInterest    int
}

type OpenInterestData struct {
	Date         time.Time
	OpenInterest float64
}

type GreeksResult struct {
	IV    float64
	Delta float64
	Gamma float64
	Vega  float64
	Theta float64
	Rho   float64
}

type ForwardInfo struct {
	Forward        float64
	DiscountFactor float64
	Rate           float64
}

type DateTask struct {
	Root string
	Date time.Time
}

type SyncConfig struct {
	Roots       []string
	StartDate   time.Time
	EndDate     time.Time
	MCPURL      string
	CHDSN       string
	Workers     int
	ProgressDir string
	MinVolume   int
	RateLimit   float64
	SchemaFile  string
}
