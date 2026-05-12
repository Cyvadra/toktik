//go:build !tigerapi

package tigerapi

import (
	"encoding/json"
	"fmt"
	"time"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
)

const disabledMessage = "tigerapi is isolated from default builds; rebuild with -tags tigerapi to enable it"

type Environment string

const (
	EnvironmentProd    Environment = "PROD"
	EnvironmentSandbox Environment = "SANDBOX"
)

type Config struct {
	TigerID             string
	PrivateKey          string
	Account             string
	License             string
	Environment         Environment
	Language            string
	Timezone            string
	Timeout             time.Duration
	EnableDynamicDomain bool
	Token               string
	TokenFile           string
	ServerURL           string
	DeviceID            string
}

type Client struct{}

type MarketState struct {
	Market string `json:"market"`
	Status string `json:"status"`
}

type StockQuote struct {
	Symbol      string  `json:"symbol"`
	LatestPrice float64 `json:"latestPrice"`
	Open        float64 `json:"open"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	PrevClose   float64 `json:"prevClose"`
	BidPrice    float64 `json:"bidPrice"`
	AskPrice    float64 `json:"askPrice"`
	Volume      int64   `json:"volume"`
	Timestamp   int64   `json:"timestamp"`
}

type KlineBar struct {
	Symbol       string         `json:"symbol"`
	Time         int64          `json:"time"`
	Open         float64        `json:"open"`
	High         float64        `json:"high"`
	Low          float64        `json:"low"`
	Close        float64        `json:"close"`
	Volume       float64        `json:"volume"`
	Amount       float64        `json:"amount"`
	Fundamentals map[string]any `json:"-"`
}

func (b *KlineBar) UnmarshalJSON(data []byte) error {
	type base KlineBar
	var decoded base
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*b = KlineBar(decoded)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	delete(raw, "symbol")
	delete(raw, "time")
	delete(raw, "open")
	delete(raw, "high")
	delete(raw, "low")
	delete(raw, "close")
	delete(raw, "volume")
	delete(raw, "amount")
	if len(raw) == 0 {
		b.Fundamentals = nil
		return nil
	}
	b.Fundamentals = make(map[string]any, len(raw))
	for key, value := range raw {
		var decodedValue any
		if err := json.Unmarshal(value, &decodedValue); err != nil {
			return fmt.Errorf("decode tiger kline field %s: %w", key, err)
		}
		b.Fundamentals[key] = decodedValue
	}
	return nil
}

func (b KlineBar) MarshalJSON() ([]byte, error) {
	encoded := map[string]any{
		"symbol": b.Symbol,
		"time":   b.Time,
		"open":   b.Open,
		"high":   b.High,
		"low":    b.Low,
		"close":  b.Close,
		"volume": b.Volume,
		"amount": b.Amount,
	}
	for key, value := range b.Fundamentals {
		encoded[key] = value
	}
	return json.Marshal(encoded)
}

type TimelinePoint struct {
	Symbol   string  `json:"symbol"`
	Time     int64   `json:"time"`
	Price    float64 `json:"price"`
	AvgPrice float64 `json:"avgPrice"`
	Volume   float64 `json:"volume"`
}

type TradeTick struct {
	Symbol    string  `json:"symbol"`
	Time      int64   `json:"time"`
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Direction string  `json:"direction"`
}

type QuoteDepth struct {
	Symbol string      `json:"symbol"`
	Bids   [][]float64 `json:"bids"`
	Asks   [][]float64 `json:"asks"`
}

type OptionContract struct {
	Identifier string  `json:"identifier"`
	Symbol     string  `json:"symbol"`
	Expiry     string  `json:"expiry"`
	Strike     float64 `json:"strike"`
	PutCall    string  `json:"putCall"`
	Multiplier int64   `json:"multiplier"`
}

type OptionQuote struct {
	Symbol          string  `json:"symbol"`
	Identifier      string  `json:"identifier"`
	LatestPrice     float64 `json:"latestPrice"`
	BidPrice        float64 `json:"bidPrice"`
	AskPrice        float64 `json:"askPrice"`
	Volume          int64   `json:"volume"`
	OpenInterest    int64   `json:"openInterest"`
	UnderlyingPrice float64 `json:"underlyingPrice"`
	Delta           float64 `json:"delta"`
	Gamma           float64 `json:"gamma"`
	Theta           float64 `json:"theta"`
	Vega            float64 `json:"vega"`
	Rho             float64 `json:"rho"`
	Volatility      string  `json:"volatility"`
	Timestamp       int64   `json:"timestamp"`
}

type StockKlineRequest struct {
	Symbol          string
	Period          string
	BeginTime       string
	EndTime         string
	Limit           int
	PageToken       string
	WithFundamental bool
}

type KlinePage struct {
	Bars          []KlineBar
	NextPageToken string
}

type OptionKlineRequest struct {
	Identifier string
	Period     string
}

func LoadConfigFromEnv() (Config, error) {
	return Config{}, fmt.Errorf(disabledMessage)
}

func LoadConfigFromRuntime(runtimeconfig.Runtime) (Config, error) {
	return Config{}, fmt.Errorf(disabledMessage)
}

func NewFromEnv() (*Client, error) {
	return nil, fmt.Errorf(disabledMessage)
}

func NewFromRuntime(runtimeconfig.Runtime) (*Client, error) {
	return nil, fmt.Errorf(disabledMessage)
}

func New(Config) (*Client, error) {
	return nil, fmt.Errorf(disabledMessage)
}

func (c Config) Validate() error {
	return fmt.Errorf(disabledMessage)
}

func (c *Client) StockKlinesPage(StockKlineRequest) (KlinePage, error) {
	return KlinePage{}, fmt.Errorf(disabledMessage)
}
