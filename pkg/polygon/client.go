package polygon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
	"github.com/massive-com/client-go/v3/rest"
	"github.com/massive-com/client-go/v3/rest/gen"
)

type Client struct {
	config     Config
	httpClient *http.Client
	sdk        *gen.ClientWithResponses
}

type AggregateRequest struct {
	Ticker     string
	Multiplier int
	Timespan   string
	From       string
	To         string
	Adjusted   *bool
	Sort       string
	Limit      int
}

type QuoteRequest struct {
	Timestamp    string
	TimestampGte string
	TimestampGt  string
	TimestampLte string
	TimestampLt  string
	Order        string
	Sort         string
	Limit        int
}

type TradeRequest struct {
	Timestamp    string
	TimestampGte string
	TimestampGt  string
	TimestampLte string
	TimestampLt  string
	Order        string
	Sort         string
	Limit        int
}

type OptionChainRequest struct {
	Underlying        string
	ExpirationDate    string
	ExpirationDateGte string
	ExpirationDateGt  string
	ExpirationDateLte string
	ExpirationDateLt  string
	ContractType      string
	StrikePrice       *float64
	StrikePriceGte    *float64
	StrikePriceGt     *float64
	StrikePriceLte    *float64
	StrikePriceLt     *float64
	Order             string
	Sort              string
	Limit             int
}

type AggregateBar struct {
	Ticker     string   `json:"ticker"`
	Timestamp  int64    `json:"timestamp"`
	Open       float64  `json:"open"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	Close      float64  `json:"close"`
	Volume     float64  `json:"volume"`
	VWAP       *float64 `json:"vwap,omitempty"`
	TradeCount *int     `json:"tradeCount,omitempty"`
	Adjusted   bool     `json:"adjusted"`
	OTC        *bool    `json:"otc,omitempty"`
}

type Quote struct {
	AskExchange          *int     `json:"askExchange,omitempty"`
	AskPrice             *float64 `json:"askPrice,omitempty"`
	AskSize              *float64 `json:"askSize,omitempty"`
	BidExchange          *int     `json:"bidExchange,omitempty"`
	BidPrice             *float64 `json:"bidPrice,omitempty"`
	BidSize              *float64 `json:"bidSize,omitempty"`
	Conditions           []int32  `json:"conditions,omitempty"`
	Indicators           []int32  `json:"indicators,omitempty"`
	ParticipantTimestamp *int64   `json:"participantTimestamp,omitempty"`
	SequenceNumber       int64    `json:"sequenceNumber"`
	SIPTimestamp         int64    `json:"sipTimestamp"`
	Tape                 *int32   `json:"tape,omitempty"`
	TRFTimestamp         *int64   `json:"trfTimestamp,omitempty"`
}

type Trade struct {
	Conditions           []int32 `json:"conditions,omitempty"`
	Correction           *int    `json:"correction,omitempty"`
	DecimalSize          string  `json:"decimalSize,omitempty"`
	Exchange             int     `json:"exchange"`
	ID                   string  `json:"id,omitempty"`
	ParticipantTimestamp *int64  `json:"participantTimestamp,omitempty"`
	Price                float64 `json:"price"`
	SequenceNumber       *int64  `json:"sequenceNumber,omitempty"`
	SIPTimestamp         int64   `json:"sipTimestamp"`
	Size                 float64 `json:"size"`
	Tape                 *int32  `json:"tape,omitempty"`
	TRFID                *int    `json:"trfId,omitempty"`
	TRFTimestamp         *int64  `json:"trfTimestamp,omitempty"`
}

type SnapshotBar struct {
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	VWAP   float64 `json:"vwap"`
}

type SnapshotMinuteBar struct {
	SnapshotBar
	AccumulatedVolume int   `json:"accumulatedVolume"`
	TradeCount        int   `json:"tradeCount"`
	Timestamp         int64 `json:"timestamp"`
}

type StockSnapshot struct {
	Ticker           string             `json:"ticker"`
	Updated          *int64             `json:"updated,omitempty"`
	TodaysChange     *float64           `json:"todaysChange,omitempty"`
	TodaysChangePerc *float64           `json:"todaysChangePerc,omitempty"`
	FairMarketValue  *float64           `json:"fairMarketValue,omitempty"`
	Day              *SnapshotBar       `json:"day,omitempty"`
	Minute           *SnapshotMinuteBar `json:"minute,omitempty"`
	PreviousDay      *SnapshotBar       `json:"previousDay,omitempty"`
	LastQuote        *Quote             `json:"lastQuote,omitempty"`
	LastTrade        *Trade             `json:"lastTrade,omitempty"`
}

type OptionContract struct {
	Ticker            string  `json:"ticker"`
	UnderlyingTicker  string  `json:"underlyingTicker"`
	ContractType      string  `json:"contractType"`
	ExerciseStyle     string  `json:"exerciseStyle"`
	ExpirationDate    string  `json:"expirationDate"`
	PrimaryExchange   string  `json:"primaryExchange"`
	SharesPerContract float64 `json:"sharesPerContract"`
	StrikePrice       float64 `json:"strikePrice"`
	Correction        *int    `json:"correction,omitempty"`
	CFI               string  `json:"cfi,omitempty"`
}

type OptionGreeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
}

type OptionDay struct {
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"changePercent"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	PreviousClose float64 `json:"previousClose"`
	Volume        float64 `json:"volume"`
	VWAP          float64 `json:"vwap"`
	LastUpdated   *int64  `json:"lastUpdated,omitempty"`
}

type UnderlyingAsset struct {
	Ticker            string   `json:"ticker"`
	Price             *float64 `json:"price,omitempty"`
	Value             *float64 `json:"value,omitempty"`
	ChangeToBreakEven float64  `json:"changeToBreakEven"`
	LastUpdated       *int64   `json:"lastUpdated,omitempty"`
	Timeframe         string   `json:"timeframe,omitempty"`
}

type OptionChainContract struct {
	BreakEvenPrice    float64         `json:"breakEvenPrice"`
	Contract          OptionContract  `json:"contract"`
	Day               OptionDay       `json:"day"`
	FairMarketValue   *float64        `json:"fairMarketValue,omitempty"`
	FairMarketUpdated *int64          `json:"fairMarketUpdated,omitempty"`
	Greeks            *OptionGreeks   `json:"greeks,omitempty"`
	ImpliedVolatility *float64        `json:"impliedVolatility,omitempty"`
	LastQuote         Quote           `json:"lastQuote"`
	LastTrade         *Trade          `json:"lastTrade,omitempty"`
	OpenInterest      float64         `json:"openInterest"`
	UnderlyingAsset   UnderlyingAsset `json:"underlyingAsset"`
}

type pagedResults[T any] struct {
	NextURL *string `json:"next_url,omitempty"`
	Results []T     `json:"results,omitempty"`
	Status  string  `json:"status"`
}

type aggregatePageItem struct {
	Open       float64  `json:"o"`
	High       float64  `json:"h"`
	Low        float64  `json:"l"`
	Close      float64  `json:"c"`
	TradeCount *int     `json:"n,omitempty"`
	Timestamp  int64    `json:"t"`
	Volume     float64  `json:"v"`
	VWAP       *float64 `json:"vw,omitempty"`
	OTC        *bool    `json:"otc,omitempty"`
}

type quotePageItem struct {
	AskExchange          *int     `json:"ask_exchange,omitempty"`
	AskPrice             *float64 `json:"ask_price,omitempty"`
	AskSize              *float64 `json:"ask_size,omitempty"`
	BidExchange          *int     `json:"bid_exchange,omitempty"`
	BidPrice             *float64 `json:"bid_price,omitempty"`
	BidSize              *float64 `json:"bid_size,omitempty"`
	Conditions           []int32  `json:"conditions,omitempty"`
	Indicators           []int32  `json:"indicators,omitempty"`
	ParticipantTimestamp *int64   `json:"participant_timestamp,omitempty"`
	SequenceNumber       int64    `json:"sequence_number"`
	SIPTimestamp         int64    `json:"sip_timestamp"`
	Tape                 *int32   `json:"tape,omitempty"`
	TRFTimestamp         *int64   `json:"trf_timestamp,omitempty"`
}

type tradePageItem struct {
	Conditions           []int32 `json:"conditions,omitempty"`
	Correction           *int    `json:"correction,omitempty"`
	DecimalSize          string  `json:"decimal_size,omitempty"`
	Exchange             int     `json:"exchange"`
	ID                   string  `json:"id,omitempty"`
	ParticipantTimestamp *int64  `json:"participant_timestamp,omitempty"`
	Price                float64 `json:"price"`
	SequenceNumber       *int64  `json:"sequence_number,omitempty"`
	SIPTimestamp         int64   `json:"sip_timestamp"`
	Size                 float64 `json:"size"`
	Tape                 *int32  `json:"tape,omitempty"`
	TRFID                *int    `json:"trf_id,omitempty"`
	TRFTimestamp         *int64  `json:"trf_timestamp,omitempty"`
}

type optionContractPageItem struct {
	BreakEvenPrice float64 `json:"break_even_price"`
	Day            struct {
		Change        float64 `json:"change"`
		ChangePercent float64 `json:"change_percent"`
		Close         float64 `json:"close"`
		High          float64 `json:"high"`
		LastUpdated   *int64  `json:"last_updated,omitempty"`
		Low           float64 `json:"low"`
		Open          float64 `json:"open"`
		PreviousClose float64 `json:"previous_close"`
		Volume        float64 `json:"volume"`
		VWAP          float64 `json:"vwap"`
	} `json:"day"`
	Details struct {
		ContractType      string  `json:"contract_type"`
		ExerciseStyle     string  `json:"exercise_style"`
		ExpirationDate    string  `json:"expiration_date"`
		SharesPerContract float64 `json:"shares_per_contract"`
		StrikePrice       float64 `json:"strike_price"`
		Ticker            string  `json:"ticker"`
	} `json:"details"`
	Fmv            *float64 `json:"fmv,omitempty"`
	FmvLastUpdated *int64   `json:"fmv_last_updated,omitempty"`
	Greeks         *struct {
		Delta float64 `json:"delta"`
		Gamma float64 `json:"gamma"`
		Theta float64 `json:"theta"`
		Vega  float64 `json:"vega"`
	} `json:"greeks,omitempty"`
	ImpliedVolatility *float64 `json:"implied_volatility,omitempty"`
	LastQuote         struct {
		Ask         float64 `json:"ask"`
		AskExchange *int32  `json:"ask_exchange,omitempty"`
		AskSize     float64 `json:"ask_size"`
		Bid         float64 `json:"bid"`
		BidExchange *int32  `json:"bid_exchange,omitempty"`
		BidSize     float64 `json:"bid_size"`
		LastUpdated *int64  `json:"last_updated,omitempty"`
		Midpoint    float64 `json:"midpoint"`
		Timeframe   *string `json:"timeframe,omitempty"`
	} `json:"last_quote"`
	LastTrade *struct {
		Conditions   []int32 `json:"conditions,omitempty"`
		Exchange     int     `json:"exchange"`
		Price        float64 `json:"price"`
		SipTimestamp int64   `json:"sip_timestamp"`
		Size         int32   `json:"size"`
		Timeframe    *string `json:"timeframe,omitempty"`
	} `json:"last_trade,omitempty"`
	OpenInterest    float64 `json:"open_interest"`
	UnderlyingAsset struct {
		ChangeToBreakEven float64  `json:"change_to_break_even"`
		LastUpdated       *int64   `json:"last_updated,omitempty"`
		Price             *float64 `json:"price,omitempty"`
		Ticker            string   `json:"ticker"`
		Timeframe         *string  `json:"timeframe,omitempty"`
		Value             *float64 `json:"value,omitempty"`
	} `json:"underlying_asset"`
}

func NewFromEnv() (*Client, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

func NewFromRuntime(runtimeCfg runtimeconfig.Runtime) (*Client, error) {
	cfg, err := LoadConfigFromRuntime(runtimeCfg)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}

func New(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	transport := http.DefaultTransport
	if cfg.Trace {
		transport = &debugTransport{base: http.DefaultTransport}
	}

	httpClient := &http.Client{
		Timeout:   cfg.normalizedTimeout(),
		Transport: transport,
	}

	client := &Client{
		config:     cfg,
		httpClient: httpClient,
	}

	sdk, err := gen.NewClientWithResponses(
		cfg.normalizedBaseURL(),
		gen.WithHTTPClient(httpClient),
		gen.WithRequestEditorFn(client.addHeaders),
	)
	if err != nil {
		return nil, fmt.Errorf("init massive client: %w", err)
	}
	client.sdk = sdk
	return client, nil
}

func (c *Client) Config() Config {
	return c.config
}

func (c *Client) SDKClient() *gen.ClientWithResponses {
	return c.sdk
}

func (c *Client) StockSnapshot(symbol string) (*StockSnapshot, error) {
	symbol = normalizeTicker(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	resp, err := c.sdk.GetStocksSnapshotTickerWithResponse(context.Background(), symbol)
	if err != nil {
		return nil, fmt.Errorf("query massive stock snapshot: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Ticker == nil {
		return nil, nil
	}
	item := resp.JSON200.Ticker

	snapshot := &StockSnapshot{
		Ticker:           derefString(item.Ticker),
		Updated:          intPtrToInt64Ptr(item.Updated),
		TodaysChange:     item.TodaysChange,
		TodaysChangePerc: item.TodaysChangePerc,
		FairMarketValue:  item.Fmv,
	}
	if item.Day != nil {
		snapshot.Day = &SnapshotBar{Open: item.Day.O, High: item.Day.H, Low: item.Day.L, Close: item.Day.C, Volume: item.Day.V, VWAP: item.Day.Vw}
	}
	if item.Min != nil {
		snapshot.Minute = &SnapshotMinuteBar{
			SnapshotBar:       SnapshotBar{Open: item.Min.O, High: item.Min.H, Low: item.Min.L, Close: item.Min.C, Volume: item.Min.V, VWAP: item.Min.Vw},
			AccumulatedVolume: item.Min.Av,
			TradeCount:        item.Min.N,
			Timestamp:         int64(item.Min.Timestamp),
		}
	}
	if item.PrevDay != nil {
		snapshot.PreviousDay = &SnapshotBar{Open: item.PrevDay.O, High: item.PrevDay.H, Low: item.PrevDay.L, Close: item.PrevDay.C, Volume: item.PrevDay.V, VWAP: item.PrevDay.Vw}
	}
	if item.LastQuote != nil {
		snapshot.LastQuote = &Quote{
			AskPrice:       float64Ptr(item.LastQuote.AskPrice),
			AskSize:        intToFloat64Ptr(item.LastQuote.AskSize),
			BidPrice:       float64Ptr(item.LastQuote.BidPrice),
			BidSize:        intToFloat64Ptr(item.LastQuote.BidSize),
			SIPTimestamp:   int64(item.LastQuote.Timestamp),
			AskExchange:    nil,
			BidExchange:    nil,
			SequenceNumber: 0,
		}
	}
	if item.LastTrade != nil {
		snapshot.LastTrade = &Trade{
			Conditions:   intsToInt32s(item.LastTrade.C),
			ID:           item.LastTrade.I,
			Exchange:     item.LastTrade.BidExchange,
			Price:        item.LastTrade.BidPrice,
			SIPTimestamp: int64(item.LastTrade.Timestamp),
			Size:         float64(item.LastTrade.BidSize),
		}
	}
	return snapshot, nil
}

func (c *Client) StockAggregates(req AggregateRequest) ([]AggregateBar, error) {
	if err := validateAggregateRequest(req); err != nil {
		return nil, fmt.Errorf("stock aggregates: %w", err)
	}
	params := &gen.GetStocksAggregatesParams{}
	if req.Adjusted != nil {
		params.Adjusted = req.Adjusted
	}
	sort := "asc"
	if strings.TrimSpace(req.Sort) != "" {
		sort = strings.ToLower(strings.TrimSpace(req.Sort))
	}
	params.Sort = sort
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}

	resp, err := c.sdk.GetStocksAggregatesWithResponse(
		context.Background(),
		normalizeTicker(req.Ticker),
		normalizedMultiplier(req.Multiplier),
		gen.GetStocksAggregatesParamsTimespan(strings.ToLower(strings.TrimSpace(req.Timespan))),
		strings.TrimSpace(req.From),
		strings.TrimSpace(req.To),
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("query massive stock aggregates: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, nil
	}

	bars := make([]AggregateBar, 0)
	if resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			bars = append(bars, AggregateBar{
				Ticker:     resp.JSON200.Ticker,
				Timestamp:  int64(item.Timestamp),
				Open:       item.O,
				High:       item.H,
				Low:        item.L,
				Close:      item.C,
				Volume:     item.V,
				VWAP:       item.Vw,
				TradeCount: item.N,
				Adjusted:   resp.JSON200.Adjusted,
				OTC:        item.Otc,
			})
		}
	}
	if c.config.Pagination && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[aggregatePageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			bars = append(bars, AggregateBar{
				Ticker:     resp.JSON200.Ticker,
				Timestamp:  item.Timestamp,
				Open:       item.Open,
				High:       item.High,
				Low:        item.Low,
				Close:      item.Close,
				Volume:     item.Volume,
				VWAP:       item.VWAP,
				TradeCount: item.TradeCount,
				Adjusted:   resp.JSON200.Adjusted,
				OTC:        item.OTC,
			})
		}
	}
	return bars, nil
}

func (c *Client) StockQuotes(symbol string, req QuoteRequest) ([]Quote, error) {
	symbol = normalizeTicker(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("stock quotes: symbol is required")
	}
	params := buildStockQuoteParams(req)
	resp, err := c.sdk.GetStocksQuotesWithResponse(context.Background(), symbol, params)
	if err != nil {
		return nil, fmt.Errorf("query massive stock quotes: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}

	quotes := make([]Quote, 0)
	if resp.JSON200 != nil && resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			var mapped quotePageItem
			if err := remarshal(item, &mapped); err != nil {
				return nil, fmt.Errorf("decode stock quote item: %w", err)
			}
			quotes = append(quotes, mapQuotePageItem(mapped))
		}
	}
	if c.config.Pagination && resp.JSON200 != nil && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[quotePageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			quotes = append(quotes, mapQuotePageItem(item))
		}
	}
	return quotes, nil
}

func (c *Client) StockTrades(symbol string, req TradeRequest) ([]Trade, error) {
	symbol = normalizeTicker(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("stock trades: symbol is required")
	}
	params := buildStockTradeParams(req)
	resp, err := c.sdk.GetStocksTradesWithResponse(context.Background(), symbol, params)
	if err != nil {
		return nil, fmt.Errorf("query massive stock trades: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}

	trades := make([]Trade, 0)
	if resp.JSON200 != nil && resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			var mapped tradePageItem
			if err := remarshal(item, &mapped); err != nil {
				return nil, fmt.Errorf("decode stock trade item: %w", err)
			}
			quotes := mapTradePageItem(mapped)
			trades = append(trades, quotes)
		}
	}
	if c.config.Pagination && resp.JSON200 != nil && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[tradePageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			trades = append(trades, mapTradePageItem(item))
		}
	}
	return trades, nil
}

func (c *Client) OptionContract(ticker string) (*OptionContract, error) {
	ticker = normalizeTicker(ticker)
	if ticker == "" {
		return nil, fmt.Errorf("option contract: ticker is required")
	}
	resp, err := c.sdk.GetOptionsContractWithResponse(context.Background(), ticker, nil)
	if err != nil {
		return nil, fmt.Errorf("query massive option contract: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil || resp.JSON200.Results == nil {
		return nil, nil
	}
	item := resp.JSON200.Results
	return &OptionContract{
		Ticker:            derefString(item.Ticker),
		UnderlyingTicker:  derefString(item.UnderlyingTicker),
		ContractType:      derefString(item.ContractType),
		ExerciseStyle:     formatMaybeEnum(item.ExerciseStyle),
		ExpirationDate:    derefString(item.ExpirationDate),
		PrimaryExchange:   derefString(item.PrimaryExchange),
		SharesPerContract: float32PtrValue(item.SharesPerContract),
		StrikePrice:       float32PtrValue(item.StrikePrice),
		Correction:        item.Correction,
		CFI:               derefString(item.Cfi),
	}, nil
}

func (c *Client) OptionChain(req OptionChainRequest) ([]OptionChainContract, error) {
	underlying := normalizeTicker(req.Underlying)
	if underlying == "" {
		return nil, fmt.Errorf("option chain: underlying is required")
	}
	params := &gen.GetOptionsChainParams{}
	if req.StrikePrice != nil {
		params.StrikePrice = float64ToFloat32Ptr(req.StrikePrice)
	}
	if req.StrikePriceGte != nil {
		params.StrikePriceGte = float64ToFloat32Ptr(req.StrikePriceGte)
	}
	if req.StrikePriceGt != nil {
		params.StrikePriceGt = float64ToFloat32Ptr(req.StrikePriceGt)
	}
	if req.StrikePriceLte != nil {
		params.StrikePriceLte = float64ToFloat32Ptr(req.StrikePriceLte)
	}
	if req.StrikePriceLt != nil {
		params.StrikePriceLt = float64ToFloat32Ptr(req.StrikePriceLt)
	}
	setStringIfPresent(&params.ExpirationDate, req.ExpirationDate)
	setStringIfPresent(&params.ExpirationDateGte, req.ExpirationDateGte)
	setStringIfPresent(&params.ExpirationDateGt, req.ExpirationDateGt)
	setStringIfPresent(&params.ExpirationDateLte, req.ExpirationDateLte)
	setStringIfPresent(&params.ExpirationDateLt, req.ExpirationDateLt)
	if strings.TrimSpace(req.ContractType) != "" {
		contractType := gen.GetOptionsChainParamsContractType(strings.ToLower(strings.TrimSpace(req.ContractType)))
		params.ContractType = &contractType
	}
	if strings.TrimSpace(req.Order) != "" {
		order := gen.GetOptionsChainParamsOrder(strings.ToLower(strings.TrimSpace(req.Order)))
		params.Order = &order
	}
	if strings.TrimSpace(req.Sort) != "" {
		sort := gen.GetOptionsChainParamsSort(strings.ToLower(strings.TrimSpace(req.Sort)))
		params.Sort = &sort
	}
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}

	resp, err := c.sdk.GetOptionsChainWithResponse(context.Background(), underlying, params)
	if err != nil {
		return nil, fmt.Errorf("query massive option chain: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}

	contracts := make([]OptionChainContract, 0)
	if resp.JSON200 != nil && resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			var mapped optionContractPageItem
			if err := remarshal(item, &mapped); err != nil {
				return nil, fmt.Errorf("decode option chain item: %w", err)
			}
			contracts = append(contracts, mapOptionChainItem(mapped))
		}
	}
	if c.config.Pagination && resp.JSON200 != nil && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[optionContractPageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			contracts = append(contracts, mapOptionChainItem(item))
		}
	}
	return contracts, nil
}

func (c *Client) OptionAggregates(req AggregateRequest) ([]AggregateBar, error) {
	if err := validateAggregateRequest(req); err != nil {
		return nil, fmt.Errorf("option aggregates: %w", err)
	}
	params := &gen.GetOptionsAggregatesParams{}
	if req.Adjusted != nil {
		params.Adjusted = req.Adjusted
	}
	sort := "asc"
	if strings.TrimSpace(req.Sort) != "" {
		sort = strings.ToLower(strings.TrimSpace(req.Sort))
	}
	params.Sort = sort
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}
	resp, err := c.sdk.GetOptionsAggregatesWithResponse(
		context.Background(),
		normalizeTicker(req.Ticker),
		normalizedMultiplier(req.Multiplier),
		gen.GetOptionsAggregatesParamsTimespan(strings.ToLower(strings.TrimSpace(req.Timespan))),
		strings.TrimSpace(req.From),
		strings.TrimSpace(req.To),
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("query massive option aggregates: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, nil
	}
	bars := make([]AggregateBar, 0)
	if resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			bars = append(bars, AggregateBar{
				Ticker:     resp.JSON200.Ticker,
				Timestamp:  int64(item.Timestamp),
				Open:       item.O,
				High:       item.H,
				Low:        item.L,
				Close:      item.C,
				Volume:     item.V,
				VWAP:       item.Vw,
				TradeCount: item.N,
				Adjusted:   resp.JSON200.Adjusted,
			})
		}
	}
	return bars, nil
}

func (c *Client) OptionQuotes(ticker string, req QuoteRequest) ([]Quote, error) {
	ticker = normalizeTicker(ticker)
	if ticker == "" {
		return nil, fmt.Errorf("option quotes: ticker is required")
	}
	params := buildOptionQuoteParams(req)
	resp, err := c.sdk.GetOptionsQuotesWithResponse(context.Background(), ticker, params)
	if err != nil {
		return nil, fmt.Errorf("query massive option quotes: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	quotes := make([]Quote, 0)
	if resp.JSON200 != nil && resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			var mapped quotePageItem
			if err := remarshal(item, &mapped); err != nil {
				return nil, fmt.Errorf("decode option quote item: %w", err)
			}
			quotes = append(quotes, mapQuotePageItem(mapped))
		}
	}
	if c.config.Pagination && resp.JSON200 != nil && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[quotePageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			quotes = append(quotes, mapQuotePageItem(item))
		}
	}
	return quotes, nil
}

func (c *Client) OptionTrades(ticker string, req TradeRequest) ([]Trade, error) {
	ticker = normalizeTicker(ticker)
	if ticker == "" {
		return nil, fmt.Errorf("option trades: ticker is required")
	}
	params := buildOptionTradeParams(req)
	resp, err := c.sdk.GetOptionsTradesWithResponse(context.Background(), ticker, params)
	if err != nil {
		return nil, fmt.Errorf("query massive option trades: %w", err)
	}
	if err := normalizeResponseError(resp, resp.HTTPResponse, resp.Body, rest.CheckResponse(resp)); err != nil {
		return nil, err
	}
	trades := make([]Trade, 0)
	if resp.JSON200 != nil && resp.JSON200.Results != nil {
		for _, item := range *resp.JSON200.Results {
			var mapped tradePageItem
			if err := remarshal(item, &mapped); err != nil {
				return nil, fmt.Errorf("decode option trade item: %w", err)
			}
			trades = append(trades, mapTradePageItem(mapped))
		}
	}
	if c.config.Pagination && resp.JSON200 != nil && resp.JSON200.NextUrl != nil {
		pages, err := fetchNextPages[tradePageItem](c, *resp.JSON200.NextUrl)
		if err != nil {
			return nil, err
		}
		for _, item := range pages {
			trades = append(trades, mapTradePageItem(item))
		}
	}
	return trades, nil
}

func (c *Client) addHeaders(_ context.Context, req *http.Request) error {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.config.APIKey))
	req.Header.Set("User-Agent", "toktik-polygon")
	return nil
}

func fetchNextPages[T any](c *Client, nextURL string) ([]T, error) {
	results := make([]T, 0)
	current := strings.TrimSpace(nextURL)
	for c.config.Pagination && current != "" {
		page, err := fetchPage[T](c, current)
		if err != nil {
			return nil, err
		}
		results = append(results, page.Results...)
		if page.NextURL == nil {
			break
		}
		current = strings.TrimSpace(*page.NextURL)
	}
	return results, nil
}

func fetchPage[T any](c *Client, nextURL string) (*pagedResults[T], error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, nextURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build next page request: %w", err)
	}
	if err := c.addHeaders(context.Background(), req); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch next page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pagination request failed: %s body=%s", resp.Status, strings.TrimSpace(string(body)))
	}
	var page pagedResults[T]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("decode next page: %w", err)
	}
	return &page, nil
}

func buildStockQuoteParams(req QuoteRequest) *gen.GetStocksQuotesParams {
	params := &gen.GetStocksQuotesParams{}
	setStringIfPresent(&params.Timestamp, req.Timestamp)
	setStringIfPresent(&params.TimestampGte, req.TimestampGte)
	setStringIfPresent(&params.TimestampGt, req.TimestampGt)
	setStringIfPresent(&params.TimestampLte, req.TimestampLte)
	setStringIfPresent(&params.TimestampLt, req.TimestampLt)
	if strings.TrimSpace(req.Order) != "" {
		order := gen.GetStocksQuotesParamsOrder(strings.ToLower(strings.TrimSpace(req.Order)))
		params.Order = &order
	}
	if strings.TrimSpace(req.Sort) != "" {
		sort := gen.GetStocksQuotesParamsSort(strings.ToLower(strings.TrimSpace(req.Sort)))
		params.Sort = &sort
	}
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}
	return params
}

func buildOptionQuoteParams(req QuoteRequest) *gen.GetOptionsQuotesParams {
	params := &gen.GetOptionsQuotesParams{}
	setStringIfPresent(&params.Timestamp, req.Timestamp)
	setStringIfPresent(&params.TimestampGte, req.TimestampGte)
	setStringIfPresent(&params.TimestampGt, req.TimestampGt)
	setStringIfPresent(&params.TimestampLte, req.TimestampLte)
	setStringIfPresent(&params.TimestampLt, req.TimestampLt)
	if strings.TrimSpace(req.Order) != "" {
		order := gen.GetOptionsQuotesParamsOrder(strings.ToLower(strings.TrimSpace(req.Order)))
		params.Order = &order
	}
	if strings.TrimSpace(req.Sort) != "" {
		sort := gen.GetOptionsQuotesParamsSort(strings.ToLower(strings.TrimSpace(req.Sort)))
		params.Sort = &sort
	}
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}
	return params
}

func buildStockTradeParams(req TradeRequest) *gen.GetStocksTradesParams {
	params := &gen.GetStocksTradesParams{}
	setStringIfPresent(&params.Timestamp, req.Timestamp)
	setStringIfPresent(&params.TimestampGte, req.TimestampGte)
	setStringIfPresent(&params.TimestampGt, req.TimestampGt)
	setStringIfPresent(&params.TimestampLte, req.TimestampLte)
	setStringIfPresent(&params.TimestampLt, req.TimestampLt)
	if strings.TrimSpace(req.Order) != "" {
		order := gen.GetStocksTradesParamsOrder(strings.ToLower(strings.TrimSpace(req.Order)))
		params.Order = &order
	}
	if strings.TrimSpace(req.Sort) != "" {
		sort := gen.GetStocksTradesParamsSort(strings.ToLower(strings.TrimSpace(req.Sort)))
		params.Sort = &sort
	}
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}
	return params
}

func buildOptionTradeParams(req TradeRequest) *gen.GetOptionsTradesParams {
	params := &gen.GetOptionsTradesParams{}
	setStringIfPresent(&params.Timestamp, req.Timestamp)
	setStringIfPresent(&params.TimestampGte, req.TimestampGte)
	setStringIfPresent(&params.TimestampGt, req.TimestampGt)
	setStringIfPresent(&params.TimestampLte, req.TimestampLte)
	setStringIfPresent(&params.TimestampLt, req.TimestampLt)
	if strings.TrimSpace(req.Order) != "" {
		order := gen.GetOptionsTradesParamsOrder(strings.ToLower(strings.TrimSpace(req.Order)))
		params.Order = &order
	}
	if strings.TrimSpace(req.Sort) != "" {
		sort := gen.GetOptionsTradesParamsSort(strings.ToLower(strings.TrimSpace(req.Sort)))
		params.Sort = &sort
	}
	if req.Limit > 0 {
		params.Limit = rest.Ptr(req.Limit)
	}
	return params
}

func validateAggregateRequest(req AggregateRequest) error {
	if normalizeTicker(req.Ticker) == "" {
		return fmt.Errorf("ticker is required")
	}
	if strings.TrimSpace(req.Timespan) == "" {
		return fmt.Errorf("timespan is required")
	}
	if strings.TrimSpace(req.From) == "" || strings.TrimSpace(req.To) == "" {
		return fmt.Errorf("from and to are required")
	}
	return nil
}

func normalizedMultiplier(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func normalizeTicker(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func mapQuotePageItem(item quotePageItem) Quote {
	return Quote{
		AskExchange:          item.AskExchange,
		AskPrice:             item.AskPrice,
		AskSize:              item.AskSize,
		BidExchange:          item.BidExchange,
		BidPrice:             item.BidPrice,
		BidSize:              item.BidSize,
		Conditions:           append([]int32(nil), item.Conditions...),
		Indicators:           append([]int32(nil), item.Indicators...),
		ParticipantTimestamp: item.ParticipantTimestamp,
		SequenceNumber:       item.SequenceNumber,
		SIPTimestamp:         item.SIPTimestamp,
		Tape:                 item.Tape,
		TRFTimestamp:         item.TRFTimestamp,
	}
}

func mapTradePageItem(item tradePageItem) Trade {
	return Trade{
		Conditions:           append([]int32(nil), item.Conditions...),
		Correction:           item.Correction,
		DecimalSize:          item.DecimalSize,
		Exchange:             item.Exchange,
		ID:                   item.ID,
		ParticipantTimestamp: item.ParticipantTimestamp,
		Price:                item.Price,
		SequenceNumber:       item.SequenceNumber,
		SIPTimestamp:         item.SIPTimestamp,
		Size:                 item.Size,
		Tape:                 item.Tape,
		TRFID:                item.TRFID,
		TRFTimestamp:         item.TRFTimestamp,
	}
}

func mapOptionChainItem(item optionContractPageItem) OptionChainContract {
	quote := Quote{
		AskExchange:    int32ToIntPtr(item.LastQuote.AskExchange),
		AskPrice:       float64Ptr(item.LastQuote.Ask),
		AskSize:        float64Ptr(item.LastQuote.AskSize),
		BidExchange:    int32ToIntPtr(item.LastQuote.BidExchange),
		BidPrice:       float64Ptr(item.LastQuote.Bid),
		BidSize:        float64Ptr(item.LastQuote.BidSize),
		SIPTimestamp:   0,
		SequenceNumber: 0,
	}
	if item.LastQuote.LastUpdated != nil {
		quote.SIPTimestamp = *item.LastQuote.LastUpdated
	}
	var trade *Trade
	if item.LastTrade != nil {
		trade = &Trade{
			Conditions:   append([]int32(nil), item.LastTrade.Conditions...),
			Exchange:     item.LastTrade.Exchange,
			Price:        item.LastTrade.Price,
			SIPTimestamp: item.LastTrade.SipTimestamp,
			Size:         float64(item.LastTrade.Size),
		}
	}
	var greeks *OptionGreeks
	if item.Greeks != nil {
		greeks = &OptionGreeks{Delta: item.Greeks.Delta, Gamma: item.Greeks.Gamma, Theta: item.Greeks.Theta, Vega: item.Greeks.Vega}
	}
	return OptionChainContract{
		BreakEvenPrice: item.BreakEvenPrice,
		Contract: OptionContract{
			Ticker:            item.Details.Ticker,
			UnderlyingTicker:  item.UnderlyingAsset.Ticker,
			ContractType:      item.Details.ContractType,
			ExerciseStyle:     item.Details.ExerciseStyle,
			ExpirationDate:    item.Details.ExpirationDate,
			SharesPerContract: item.Details.SharesPerContract,
			StrikePrice:       item.Details.StrikePrice,
		},
		Day: OptionDay{
			Change:        item.Day.Change,
			ChangePercent: item.Day.ChangePercent,
			Open:          item.Day.Open,
			High:          item.Day.High,
			Low:           item.Day.Low,
			Close:         item.Day.Close,
			PreviousClose: item.Day.PreviousClose,
			Volume:        item.Day.Volume,
			VWAP:          item.Day.VWAP,
			LastUpdated:   item.Day.LastUpdated,
		},
		FairMarketValue:   item.Fmv,
		FairMarketUpdated: item.FmvLastUpdated,
		Greeks:            greeks,
		ImpliedVolatility: item.ImpliedVolatility,
		LastQuote:         quote,
		LastTrade:         trade,
		OpenInterest:      item.OpenInterest,
		UnderlyingAsset: UnderlyingAsset{
			Ticker:            item.UnderlyingAsset.Ticker,
			Price:             item.UnderlyingAsset.Price,
			Value:             item.UnderlyingAsset.Value,
			ChangeToBreakEven: item.UnderlyingAsset.ChangeToBreakEven,
			LastUpdated:       item.UnderlyingAsset.LastUpdated,
			Timeframe:         derefString(item.UnderlyingAsset.Timeframe),
		},
	}
}

func setStringIfPresent(dst **string, value string) {
	if strings.TrimSpace(value) != "" {
		*dst = rest.String(strings.TrimSpace(value))
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func slicePtrOrEmpty(value *[]int32) []int32 {
	if value == nil {
		return nil
	}
	return append([]int32(nil), (*value)...)
}

func intPtrToInt64Ptr(value *int) *int64 {
	if value == nil {
		return nil
	}
	v := int64(*value)
	return &v
}

func int64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func intToFloat64Ptr(value int) *float64 {
	v := float64(value)
	return &v
}

func int32ToIntPtr(value *int32) *int {
	if value == nil {
		return nil
	}
	v := int(*value)
	return &v
}

func float64Ptr(value float64) *float64 {
	v := value
	return &v
}

func float32PtrValue(value *float32) float64 {
	if value == nil {
		return 0
	}
	return float64(*value)
}

func float32ToFloat64Ptr(value *float32) *float64 {
	if value == nil {
		return nil
	}
	v := float64(*value)
	return &v
}

func float64ToFloat32Ptr(value *float64) *float32 {
	if value == nil {
		return nil
	}
	v := float32(*value)
	return &v
}

func formatMaybeEnum[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func remarshal(src any, dst any) error {
	encoded, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, dst)
}

func intsToInt32s(values []int) []int32 {
	if len(values) == 0 {
		return nil
	}
	out := make([]int32, len(values))
	for i, value := range values {
		out[i] = int32(value)
	}
	return out
}

type debugTransport struct {
	base http.RoundTripper
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Printf("Request URL: %s\n", req.URL.String())
	h := req.Header.Clone()
	if h.Get("Authorization") != "" {
		h.Set("Authorization", "Bearer REDACTED")
	}
	fmt.Printf("Request Headers: %+v\n", h)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Response Headers: %+v\n", resp.Header)
	return resp, nil
}
