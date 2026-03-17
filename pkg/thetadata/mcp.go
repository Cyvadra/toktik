package thetadata

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MCPClient communicates with a Theta Data MCP server using SSE transport.
// It maintains a persistent SSE connection for receiving responses and
// sends JSON-RPC requests via POST. Calls are serialized per client instance.
type MCPClient struct {
	baseURL     string
	sessionPath string
	sseReader   *bufio.Reader
	sseResp     *http.Response
	httpClient  *http.Client
	nextID      atomic.Int64
	mu          sync.Mutex
	closed      bool
}

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type mcpToolResult struct {
	Content           []mcpContent    `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// NewMCPClient creates a new MCP client targeting the given server URL.
func NewMCPClient(baseURL string) *MCPClient {
	return &MCPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 0, // SSE needs no timeout; per-request timeouts are handled separately
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     0,
				DisableKeepAlives:   false,
				MaxIdleConnsPerHost: 10,
			},
		},
	}
}

// Connect establishes the SSE connection and initializes the MCP session.
func (c *MCPClient) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/mcp/sse", nil)
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect SSE: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return fmt.Errorf("SSE connection returned status %d", resp.StatusCode)
	}

	c.sseResp = resp
	c.sseReader = bufio.NewReaderSize(resp.Body, 10*1024*1024) // 10MB buffer for large responses

	// Read the first event (must be "endpoint")
	event, data, err := c.readSSEEvent()
	if err != nil {
		resp.Body.Close()
		return fmt.Errorf("read endpoint event: %w", err)
	}
	if event != "endpoint" {
		resp.Body.Close()
		return fmt.Errorf("expected endpoint event, got %q", event)
	}
	c.sessionPath = data

	// Initialize MCP session
	initResult, err := c.sendRPC(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "thetadata-sync",
			"version": "1.0.0",
		},
	})
	if err != nil {
		resp.Body.Close()
		return fmt.Errorf("initialize MCP: %w", err)
	}
	_ = initResult

	// Send initialized notification (no ID, no response expected)
	notif := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	body, _ := json.Marshal(notif)
	postReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+c.sessionPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create initialized notification: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/json")
	postResp, err := c.httpClient.Do(postReq)
	if err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}
	postResp.Body.Close()

	return nil
}

// CallTool invokes an MCP tool and returns the raw text result.
// Calls are serialized (one at a time per MCPClient instance).
func (c *MCPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return "", fmt.Errorf("client is closed")
	}

	result, err := c.sendRPC(ctx, "tools/call", mcpToolCallParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return "", err
	}

	// Parse MCP tool result wrapper
	var toolResult mcpToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return "", fmt.Errorf("parse tool result: %w", err)
	}

	if toolResult.IsError {
		if len(toolResult.Content) > 0 {
			return "", fmt.Errorf("tool error: %s", toolResult.Content[0].Text)
		}
		return "", fmt.Errorf("tool returned error with no content")
	}

	if len(toolResult.Content) == 0 {
		if len(toolResult.StructuredContent) == 0 {
			return "", fmt.Errorf("tool returned empty content")
		}
	}

	if len(toolResult.StructuredContent) > 0 {
		return string(toolResult.StructuredContent), nil
	}

	var text strings.Builder
	for _, item := range toolResult.Content {
		if item.Type != "text" {
			continue
		}
		text.WriteString(item.Text)
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("tool returned no text content")
	}

	return text.String(), nil
}

// sendRPC sends a JSON-RPC request and reads the response from the SSE stream.
func (c *MCPClient) sendRPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	postReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+c.sessionPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create POST request: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/json")

	postResp, err := c.httpClient.Do(postReq)
	if err != nil {
		return nil, fmt.Errorf("POST request: %w", err)
	}
	postResp.Body.Close()

	// Read SSE events until we get a matching response
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		event, data, err := c.readSSEEvent()
		if err != nil {
			return nil, fmt.Errorf("read SSE response: %w", err)
		}
		if event != "message" {
			continue
		}

		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			continue // skip malformed messages
		}
		if resp.ID != id {
			continue // not our response
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}

	return nil, fmt.Errorf("timeout waiting for response to request %d", id)
}

// readSSEEvent reads the next SSE event from the stream.
// Returns (event_type, data, error).
func (c *MCPClient) readSSEEvent() (string, string, error) {
	var eventType string
	var dataLines []string

	for {
		line, err := c.readLine()
		if err != nil {
			return "", "", err
		}

		if line == "" {
			// Empty line = end of event
			if eventType != "" || len(dataLines) > 0 {
				if eventType == "" {
					eventType = "message" // default event type
				}
				return eventType, strings.Join(dataLines, "\n"), nil
			}
			continue // skip empty lines between events
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		} else if strings.HasPrefix(line, ":") {
			// Comment line, ignore
		}
	}
}

// readLine reads a single line from the SSE stream, handling large lines.
func (c *MCPClient) readLine() (string, error) {
	var result []byte
	for {
		line, isPrefix, err := c.sseReader.ReadLine()
		if err != nil {
			if err == io.EOF {
				return "", fmt.Errorf("SSE connection closed")
			}
			return "", err
		}
		result = append(result, line...)
		if !isPrefix {
			return string(result), nil
		}
	}
}

// Close terminates the SSE connection.
func (c *MCPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.sseResp != nil {
		c.sseResp.Body.Close()
	}
}
