package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxAPIResponseBytes = 8 << 20

const apiRequestTimeout = 900 * time.Second

type backtestRequest struct {
	Market     string         `json:"market,omitempty"`
	Instrument string         `json:"instrument,omitempty"`
	Asset      string         `json:"asset,omitempty"`
	Symbols    []string       `json:"symbols,omitempty"`
	Interval   string         `json:"interval,omitempty"`
	From       string         `json:"from"`
	To         string         `json:"to"`
	Capital    float64        `json:"capital"`
	DSL        string         `json:"dsl"`
	DSLParams  map[string]any `json:"dsl_params,omitempty"`
	DSLProfile *dslProfile    `json:"dsl_profile,omitempty"`
	Preflight  *bool          `json:"preflight,omitempty"`
}

type dslProfile struct {
	UsesOptions  *bool  `json:"uses_options,omitempty"`
	RegularTrade string `json:"regular_trade,omitempty"`
}

type dslParam struct {
	Name    string   `json:"name"`
	Title   string   `json:"title,omitempty"`
	Type    string   `json:"type"`
	Default any      `json:"default,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Options []string `json:"options,omitempty"`
}

type validationItem struct {
	DisplayName    string       `json:"display_name"`
	UsesOptions    bool         `json:"uses_options"`
	RegularTrade   string       `json:"regular_trade,omitempty"`
	DSLParams      []dslParam   `json:"dsl_params,omitempty"`
	DSLDiagnostics []diagnostic `json:"dsl_diagnostics,omitempty"`
	Warnings       []string     `json:"warnings,omitempty"`
}

type diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

type validationResponse struct {
	StrategyLabel string           `json:"strategy_label,omitempty"`
	Strategies    []validationItem `json:"strategies"`
}

type runAccepted struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	ReportURL string `json:"report_url,omitempty"`
}

type runProgress struct {
	Phase     string  `json:"phase,omitempty"`
	Percent   float64 `json:"percent"`
	Message   string  `json:"message,omitempty"`
	Completed bool    `json:"completed"`
}

type runStatus struct {
	RunID     string       `json:"run_id"`
	Status    string       `json:"status"`
	Progress  *runProgress `json:"progress,omitempty"`
	Error     string       `json:"error,omitempty"`
	ReportURL string       `json:"report_url,omitempty"`
}

type apiClient struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
}

func newAPIClient(baseURL *url.URL, apiKey string) *apiClient {
	return &apiClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: apiRequestTimeout,
		},
	}
}

func (c *apiClient) validate(ctx context.Context, payload backtestRequest) (*validationResponse, error) {
	preflight := false
	payload.Preflight = &preflight
	var response validationResponse
	if err := c.jsonRequest(ctx, http.MethodPost, "/api/v1/backtests/validate", payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *apiClient) start(ctx context.Context, payload backtestRequest) (*runAccepted, error) {
	var response runAccepted
	if err := c.jsonRequest(ctx, http.MethodPost, "/api/v1/backtests/runs", payload, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *apiClient) status(ctx context.Context, runID string) (*runStatus, error) {
	var response runStatus
	path := "/api/v1/backtests/runs/" + url.PathEscape(runID)
	if err := c.jsonRequest(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *apiClient) report(ctx context.Context, runID, reportPath string) (*http.Response, error) {
	allowedPrefix := "/api/v1/backtests/runs/" + url.PathEscape(runID)
	if reportPath != allowedPrefix+"/report" && !strings.HasPrefix(reportPath, allowedPrefix+"/reports/") {
		return nil, fmt.Errorf("report path is outside run %s", runID)
	}

	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + reportPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create report request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch report: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("Toktik API report returned %s", resp.Status)
	}
	return resp, nil
}

func (c *apiClient) jsonRequest(ctx context.Context, method, path string, payload, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode API request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Toktik API request failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Toktik API response: %w", err)
	}
	if len(responseBody) > maxAPIResponseBytes {
		return fmt.Errorf("Toktik API response exceeds %d bytes", maxAPIResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiError struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(responseBody, &apiError) == nil && apiError.Error != "" {
			return fmt.Errorf("Toktik API returned %s: %s", resp.Status, apiError.Error)
		}
		return fmt.Errorf("Toktik API returned %s", resp.Status)
	}
	if err := json.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("decode Toktik API response: %w", err)
	}
	return nil
}
