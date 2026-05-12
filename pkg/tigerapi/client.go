//go:build tigerapi

package tigerapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	runtimeconfig "github.com/Cyvadra/toktik/internal/config"
	tigerclient "github.com/tigerfintech/openapi-go-sdk/client"
	tigerconfig "github.com/tigerfintech/openapi-go-sdk/config"
	"github.com/tigerfintech/openapi-go-sdk/quote"
)

type Client struct {
	config          Config
	sdkConfig       *tigerconfig.ClientConfig
	httpClient      *tigerclient.HttpClient
	quoteHTTPClient *tigerclient.HttpClient
	quoteClient     *quote.QuoteClient
}

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
	Symbol string  `json:"symbol"`
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
	Amount float64 `json:"amount"`
	// Fundamentals preserves extra bar-level fields such as turnover_rate or ttm_pe_rate
	// when Tiger returns them via with_fundamental.
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

type optionExpirationEnvelope struct {
	Symbol string   `json:"symbol"`
	Dates  []string `json:"dates"`
}

type klineEnvelope struct {
	Symbol        string     `json:"symbol"`
	NextPageToken string     `json:"nextPageToken"`
	Items         []KlineBar `json:"items"`
}

type optionChainEnvelope struct {
	Symbol string            `json:"symbol"`
	Expiry any               `json:"expiry"`
	Items  []optionChainPair `json:"items"`
}

type optionChainPair struct {
	Put  *optionChainNode `json:"put"`
	Call *optionChainNode `json:"call"`
}

type optionChainNode struct {
	Identifier string  `json:"identifier"`
	Strike     float64 `json:"strike,string"`
	Right      string  `json:"right"`
	Multiplier int64   `json:"multiplier"`
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

	sdkConfig, err := tigerconfig.NewClientConfig(cfg.toSDKOptions()...)
	if err != nil {
		return nil, fmt.Errorf("init tiger sdk config: %w", err)
	}
	if cfg.ServerURL != "" {
		sdkConfig.ServerURL = cfg.ServerURL
		sdkConfig.QuoteServerURL = cfg.ServerURL
	}

	httpClient := tigerclient.NewHttpClient(sdkConfig)
	quoteHTTPClient := tigerclient.NewQuoteHttpClient(sdkConfig)
	quoteClient, err := quote.NewQuoteClientWithPermissions(quoteHTTPClient)
	if err != nil {
		return nil, fmt.Errorf("init tiger quote permissions: %w", err)
	}
	return &Client{
		config:          cfg,
		sdkConfig:       sdkConfig,
		httpClient:      httpClient,
		quoteHTTPClient: quoteHTTPClient,
		quoteClient:     quoteClient,
	}, nil
}

func (c *Client) Config() Config {
	return c.config
}

func (c *Client) SDKConfig() *tigerconfig.ClientConfig {
	return c.sdkConfig
}

func (c *Client) USMarketState() ([]MarketState, error) {
	return c.MarketState("US")
}

func (c *Client) MarketState(market string) ([]MarketState, error) {
	market = strings.ToUpper(strings.TrimSpace(market))
	if market == "" {
		return nil, fmt.Errorf("market is required")
	}
	data, err := c.quoteClient.MarketState(market)
	if err != nil {
		return nil, fmt.Errorf("query tiger market_state: %w", err)
	}
	var out []MarketState
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger market_state response: %w", err)
	}
	return out, nil
}

func (c *Client) StockQuotes(symbols []string) ([]StockQuote, error) {
	normalized, err := normalizeSymbols(symbols)
	if err != nil {
		return nil, err
	}
	data, err := c.quoteClient.QuoteRealTime(normalized)
	if err != nil {
		return nil, fmt.Errorf("query tiger quote_real_time: %w", err)
	}
	var out []StockQuote
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger quote_real_time response: %w", err)
	}
	return out, nil
}

func (c *Client) StockKlines(req StockKlineRequest) ([]KlineBar, error) {
	page, err := c.StockKlinesPage(req)
	if err != nil {
		return nil, err
	}
	return page.Bars, nil
}

func (c *Client) StockKlinesPage(req StockKlineRequest) (KlinePage, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	period := strings.TrimSpace(req.Period)
	if symbol == "" {
		return KlinePage{}, fmt.Errorf("symbol is required")
	}
	if period == "" {
		return KlinePage{}, fmt.Errorf("period is required")
	}
	bizParams := map[string]any{
		"symbols": []string{symbol},
		"period":  period,
	}
	if beginTime := strings.TrimSpace(req.BeginTime); beginTime != "" {
		bizParams["begin_time"] = beginTime
	}
	if endTime := strings.TrimSpace(req.EndTime); endTime != "" {
		bizParams["end_time"] = endTime
	}
	if req.Limit > 0 {
		bizParams["limit"] = req.Limit
	}
	if pageToken := strings.TrimSpace(req.PageToken); pageToken != "" {
		bizParams["page_token"] = pageToken
	}
	if req.WithFundamental {
		bizParams["with_fundamental"] = true
	}
	data, err := c.execute("kline", bizParams)
	if err != nil {
		return KlinePage{}, fmt.Errorf("query tiger kline: %w", err)
	}
	out, err := decodeKlinePage(data)
	if err != nil {
		return KlinePage{}, fmt.Errorf("decode tiger kline response: %w", err)
	}
	return out, nil
}

func (c *Client) StockTimeline(symbols []string) ([]TimelinePoint, error) {
	normalized, err := normalizeSymbols(symbols)
	if err != nil {
		return nil, err
	}
	data, err := c.quoteClient.Timeline(normalized)
	if err != nil {
		return nil, fmt.Errorf("query tiger timeline: %w", err)
	}
	var out []TimelinePoint
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger timeline response: %w", err)
	}
	return out, nil
}

func (c *Client) StockTradeTicks(symbols []string) ([]TradeTick, error) {
	normalized, err := normalizeSymbols(symbols)
	if err != nil {
		return nil, err
	}
	data, err := c.quoteClient.TradeTick(normalized)
	if err != nil {
		return nil, fmt.Errorf("query tiger trade_tick: %w", err)
	}
	var out []TradeTick
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger trade_tick response: %w", err)
	}
	return out, nil
}

func (c *Client) StockDepth(symbol string) (*QuoteDepth, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	data, err := c.quoteClient.QuoteDepth(symbol)
	if err != nil {
		return nil, fmt.Errorf("query tiger quote_depth: %w", err)
	}
	var out QuoteDepth
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger quote_depth response: %w", err)
	}
	return &out, nil
}

func (c *Client) OptionExpirations(underlying string) ([]string, error) {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	if underlying == "" {
		return nil, fmt.Errorf("underlying symbol is required")
	}
	data, err := c.quoteClient.OptionExpiration(underlying)
	if err != nil {
		return nil, fmt.Errorf("query tiger option_expiration: %w", err)
	}
	out, err := decodeOptionExpirations(data)
	if err != nil {
		return nil, fmt.Errorf("decode tiger option_expiration response: %w", err)
	}
	return out, nil
}

func (c *Client) OptionChain(underlying string, expiry string) ([]OptionContract, error) {
	underlying = strings.ToUpper(strings.TrimSpace(underlying))
	expiry = strings.TrimSpace(expiry)
	if underlying == "" {
		return nil, fmt.Errorf("underlying symbol is required")
	}
	if expiry == "" {
		return nil, fmt.Errorf("expiry is required")
	}
	data, err := c.quoteClient.OptionChain(underlying, expiry)
	if err != nil {
		return nil, fmt.Errorf("query tiger option_chain: %w", err)
	}
	out, err := decodeOptionChainContracts(data)
	if err != nil {
		return nil, fmt.Errorf("decode tiger option_chain response: %w", err)
	}
	return out, nil
}

func (c *Client) OptionQuotes(identifiers []string) ([]OptionQuote, error) {
	normalized, err := normalizeIdentifiers(identifiers)
	if err != nil {
		return nil, err
	}
	data, err := c.quoteClient.OptionBrief(normalized)
	if err != nil {
		return nil, fmt.Errorf("query tiger option_brief: %w", err)
	}
	var out []OptionQuote
	if err := decodeJSON(data, &out); err != nil {
		return nil, fmt.Errorf("decode tiger option_brief response: %w", err)
	}
	return out, nil
}

func (c *Client) OptionKlines(req OptionKlineRequest) ([]KlineBar, error) {
	identifier := strings.TrimSpace(req.Identifier)
	period := strings.TrimSpace(req.Period)
	if identifier == "" {
		return nil, fmt.Errorf("identifier is required")
	}
	if period == "" {
		return nil, fmt.Errorf("period is required")
	}
	data, err := c.quoteClient.OptionKline(identifier, period)
	if err != nil {
		return nil, fmt.Errorf("query tiger option_kline: %w", err)
	}
	page, err := decodeKlinePage(data)
	if err != nil {
		return nil, fmt.Errorf("decode tiger option_kline response: %w", err)
	}
	return page.Bars, nil
}

func (c *Client) Execute(method string, bizParams any, out any) error {
	data, err := c.execute(method, bizParams)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := decodeJSON(data, out); err != nil {
		return fmt.Errorf("decode tiger %s response: %w", method, err)
	}
	return nil
}

func (c *Client) ExecuteRawResponse(method string, bizParams any) (string, error) {
	return c.ExecuteRawResponseVersioned(method, bizParams, "")
}

func (c *Client) ExecuteRawResponseVersioned(method string, bizParams any, version string) (string, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return "", fmt.Errorf("method is required")
	}
	bizContent, err := marshalBizParams(bizParams)
	if err != nil {
		return "", err
	}
	httpClient := c.httpClient
	if usesQuoteGateway(method) {
		httpClient = c.quoteHTTPClient
	}
	if version != "" {
		request, err := tigerclient.NewVersionedApiRequest(method, bizParams, version)
		if err != nil {
			return "", fmt.Errorf("encode tiger %s request: %w", method, err)
		}
		response, err := httpClient.Execute(request)
		if err != nil {
			return "", fmt.Errorf("query tiger %s: %w", method, err)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return "", fmt.Errorf("encode tiger %s raw response: %w", method, err)
		}
		return string(encoded), nil
	}

	response, err := httpClient.ExecuteRaw(method, bizContent)
	if err != nil {
		return "", fmt.Errorf("query tiger %s: %w", method, err)
	}
	return response, nil
}

func (c *Client) execute(method string, bizParams any) (json.RawMessage, error) {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil, fmt.Errorf("method is required")
	}
	request, err := tigerclient.NewApiRequest(method, bizParams)
	if err != nil {
		return nil, fmt.Errorf("encode tiger %s request: %w", method, err)
	}
	httpClient := c.httpClient
	if usesQuoteGateway(method) {
		httpClient = c.quoteHTTPClient
	}
	response, err := httpClient.Execute(request)
	if err != nil {
		return nil, fmt.Errorf("query tiger %s: %w", method, err)
	}
	return response.Data, nil
}

func decodeJSON(data json.RawMessage, out any) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return json.Unmarshal(data, out)
}

func decodeOptionExpirations(data json.RawMessage) ([]string, error) {
	var direct []string
	if err := decodeJSON(data, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}

	var envelopes []optionExpirationEnvelope
	if err := decodeJSON(data, &envelopes); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, envelope := range envelopes {
		for _, date := range envelope.Dates {
			date = strings.TrimSpace(date)
			if date == "" {
				continue
			}
			if _, ok := seen[date]; ok {
				continue
			}
			seen[date] = struct{}{}
			out = append(out, date)
		}
	}
	return out, nil
}

func decodeKlinePage(data json.RawMessage) (KlinePage, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.Contains(trimmed, `"items"`) {
		var envelopes []klineEnvelope
		if err := decodeJSON(data, &envelopes); err != nil {
			return KlinePage{}, err
		}
		out := make([]KlineBar, 0)
		nextPageToken := ""
		for _, envelope := range envelopes {
			if nextPageToken == "" {
				nextPageToken = strings.TrimSpace(envelope.NextPageToken)
			}
			for _, item := range envelope.Items {
				if item.Symbol == "" {
					item.Symbol = envelope.Symbol
				}
				out = append(out, item)
			}
		}
		return KlinePage{Bars: out, NextPageToken: nextPageToken}, nil
	}

	var direct []KlineBar
	if err := decodeJSON(data, &direct); err == nil && len(direct) > 0 {
		return KlinePage{Bars: direct}, nil
	}
	return KlinePage{}, nil
}

func decodeOptionChainContracts(data json.RawMessage) ([]OptionContract, error) {
	var direct []OptionContract
	if err := decodeJSON(data, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}

	var envelopes []optionChainEnvelope
	if err := decodeJSON(data, &envelopes); err != nil {
		return nil, err
	}

	contracts := make([]OptionContract, 0)
	for _, envelope := range envelopes {
		expiry := formatExpiry(envelope.Expiry)
		for _, item := range envelope.Items {
			if item.Call != nil {
				contracts = append(contracts, optionContractFromNode(envelope.Symbol, expiry, item.Call))
			}
			if item.Put != nil {
				contracts = append(contracts, optionContractFromNode(envelope.Symbol, expiry, item.Put))
			}
		}
	}
	return contracts, nil
}

func optionContractFromNode(symbol string, expiry string, node *optionChainNode) OptionContract {
	return OptionContract{
		Identifier: normalizeIdentifierSpacing(node.Identifier),
		Symbol:     symbol,
		Expiry:     expiry,
		Strike:     node.Strike,
		PutCall:    strings.ToUpper(strings.TrimSpace(node.Right)),
		Multiplier: node.Multiplier,
	}
}

func normalizeIdentifierSpacing(identifier string) string {
	return strings.Join(strings.Fields(identifier), " ")
}

func formatExpiry(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return unixMillisToDateString(int64(value))
	case int64:
		return unixMillisToDateString(value)
	default:
		return ""
	}
}

func unixMillisToDateString(milliseconds int64) string {
	if milliseconds <= 0 {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format("2006-01-02")
}

func marshalBizParams(bizParams any) (string, error) {
	request, err := tigerclient.NewApiRequest("placeholder", bizParams)
	if err != nil {
		return "", fmt.Errorf("encode tiger raw request: %w", err)
	}
	return request.BizContent, nil
}

func usesQuoteGateway(method string) bool {
	switch strings.TrimSpace(method) {
	case "grab_quote_permission", "market_state", "quote_real_time", "kline", "timeline", "trade_tick", "quote_depth",
		"option_expiration", "option_chain", "option_brief", "option_kline", "future_exchange", "future_contracts",
		"future_real_time_quote", "future_kline", "financial_daily", "financial_report", "corporate_action",
		"capital_flow", "capital_distribution", "market_scanner":
		return true
	default:
		return false
	}
}

func normalizeSymbols(values []string) ([]string, error) {
	return normalizeList(values, true, "symbols")
}

func normalizeIdentifiers(values []string) ([]string, error) {
	return normalizeList(values, false, "identifiers")
}

func normalizeList(values []string, upper bool, field string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if upper {
			normalized = strings.ToUpper(normalized)
		}
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	return out, nil
}
