package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type swaggerDoc struct {
	Info     swaggerInfo                     `json:"info"`
	BasePath string                          `json:"basePath"`
	Paths    map[string]map[string]operation `json:"paths"`
}

type swaggerInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type operation struct {
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Consumes    []string            `json:"consumes"`
	Produces    []string            `json:"produces"`
	Deprecated  bool                `json:"deprecated"`
	Parameters  []parameter         `json:"parameters"`
	Responses   map[string]response `json:"responses"`
}

type parameter struct {
	Name        string     `json:"name"`
	In          string     `json:"in"`
	Description string     `json:"description"`
	Required    bool       `json:"required"`
	Type        string     `json:"type"`
	Schema      *schemaRef `json:"schema"`
	Items       *schemaRef `json:"items"`
}

type response struct {
	Description string     `json:"description"`
	Schema      *schemaRef `json:"schema"`
}

type schemaRef struct {
	Ref   string     `json:"$ref"`
	Type  string     `json:"type"`
	Items *schemaRef `json:"items"`
}

type endpointSpec struct {
	Method string
	Path   string
	Label  string
}

type sectionSpec struct {
	Title     string
	Endpoints []endpointSpec
}

var sections = []sectionSpec{
	{
		Title: "Technical Indicators",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/indicators/presets", Label: "Indicator preset catalog"},
			{Method: "POST", Path: "/indicators/series", Label: "DSL indicator series query"},
			{Method: "GET", Path: "/factors", Label: "Factor catalog"},
			{Method: "GET", Path: "/factors/bars", Label: "Factor time series"},
		},
	},
	{
		Title: "Feature Store Analytics",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/features/volatility-snapshot", Label: "Volatility snapshot"},
			{Method: "GET", Path: "/features/volatility-history", Label: "Volatility history"},
			{Method: "GET", Path: "/features/term-structure-snapshot", Label: "Term-structure snapshot"},
			{Method: "GET", Path: "/features/term-structure-history", Label: "Term-structure history"},
			{Method: "GET", Path: "/features/skew-snapshot", Label: "Skew snapshot"},
			{Method: "GET", Path: "/features/skew-history", Label: "Skew history"},
			{Method: "GET", Path: "/features/liquidity-snapshot", Label: "Liquidity snapshot"},
			{Method: "GET", Path: "/features/liquidity-history", Label: "Liquidity history"},
			{Method: "GET", Path: "/features/event-window-snapshot", Label: "Event-window snapshot"},
			{Method: "GET", Path: "/features/event-window-history", Label: "Event-window history"},
			{Method: "GET", Path: "/features/daily-feature-panel", Label: "Daily feature panel"},
		},
	},
	{
		Title: "Crypto Options Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/crypto-options/bars", Label: "Crypto option bars"},
			{Method: "GET", Path: "/crypto-options/symbols", Label: "Crypto option symbols"},
			{Method: "GET", Path: "/crypto-options/greeks", Label: "Crypto option greeks"},
			{Method: "GET", Path: "/markets/crypto-options/chain", Label: "Crypto option chain"},
		},
	},
	{
		Title: "Crypto Spot Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/markets/crypto-spot/bars", Label: "Crypto spot bars"},
			{Method: "GET", Path: "/markets/crypto-spot/symbols", Label: "Crypto spot symbols"},
		},
	},
	{
		Title: "US Stocks Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/markets/us-stocks/bars", Label: "US stock bars"},
			{Method: "GET", Path: "/markets/us-stocks/symbols", Label: "US stock symbols"},
		},
	},
	{
		Title: "US Options Market Data",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/markets/us-options/bars", Label: "US option bars"},
			{Method: "GET", Path: "/markets/us-options/symbols", Label: "US option symbols"},
			{Method: "GET", Path: "/markets/us-options/greeks", Label: "US option greeks"},
			{Method: "GET", Path: "/markets/us-options/chain", Label: "US option chain"},
		},
	},
	{
		Title: "Screeners",
		Endpoints: []endpointSpec{
			{Method: "GET", Path: "/screener/underlyings", Label: "Underlying screener"},
			{Method: "GET", Path: "/screener/options", Label: "Option screener"},
		},
	},
}

func main() {
	input := flag.String("input", "docs/swagger.json", "Path to Swagger JSON")
	output := flag.String("output", "docs/db-market-indicator-api.md", "Path to output Markdown")
	title := flag.String("title", "Database Market Data & Indicator API", "Markdown title")
	flag.Parse()

	doc, err := loadSwagger(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load swagger: %v\n", err)
		os.Exit(1)
	}

	content, err := renderMarkdown(doc, *input, *title)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render markdown: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*output, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write markdown: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "wrote %s\n", *output)
}

func loadSwagger(path string) (*swaggerDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc swaggerDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func renderMarkdown(doc *swaggerDoc, inputPath string, title string) (string, error) {
	var builder strings.Builder

	builder.WriteString("<!-- Code generated by cmd/api-docs-markdown; DO NOT EDIT. -->\n\n")
	builder.WriteString("> Note: This Markdown is generated by `go run ./cmd/api-docs-markdown`. Do not edit this file directly. Update the Swagger source or the generator, then rerun the command.\n\n")
	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString("- Source Swagger: `")
	builder.WriteString(filepath.ToSlash(inputPath))
	builder.WriteString("`\n")
	builder.WriteString("- API title: `")
	builder.WriteString(doc.Info.Title)
	builder.WriteString("`\n")
	builder.WriteString("- API version: `")
	builder.WriteString(doc.Info.Version)
	builder.WriteString("`\n")
	builder.WriteString("- Generated at: `")
	builder.WriteString(time.Now().UTC().Format(time.RFC3339))
	builder.WriteString("`\n\n")
	builder.WriteString("## Scope\n\n")
	builder.WriteString("This document exports the database-backed market data, technical indicator, feature-store analytics, and screener APIs. It intentionally excludes external proxy endpoints such as Polygon, and also excludes backtest and other non-query operational endpoints.\n\n")
	builder.WriteString("## Contents\n\n")
	for _, section := range sections {
		builder.WriteString("- [")
		builder.WriteString(section.Title)
		builder.WriteString("](#")
		builder.WriteString(slug(section.Title))
		builder.WriteString(")\n")
	}
	builder.WriteString("\n")

	for _, section := range sections {
		builder.WriteString("## ")
		builder.WriteString(section.Title)
		builder.WriteString("\n\n")
		for _, endpoint := range section.Endpoints {
			op, err := findOperation(doc, endpoint.Method, endpoint.Path)
			if err != nil {
				return "", err
			}
			writeEndpoint(&builder, doc.BasePath, endpoint, op)
			if endpoint.Path == "/indicators/series" {
				writeIndicatorExamples(&builder)
			}
		}
	}

	return builder.String(), nil
}

func findOperation(doc *swaggerDoc, method string, path string) (*operation, error) {
	pathItem, ok := doc.Paths[path]
	if !ok {
		return nil, fmt.Errorf("path %s not found in swagger", path)
	}
	op, ok := pathItem[strings.ToLower(method)]
	if !ok {
		return nil, fmt.Errorf("method %s %s not found in swagger", method, path)
	}
	return &op, nil
}

func writeEndpoint(builder *strings.Builder, basePath string, spec endpointSpec, op *operation) {
	builder.WriteString("### ")
	builder.WriteString(spec.Label)
	builder.WriteString("\n\n")
	builder.WriteString("- Endpoint: `")
	builder.WriteString(strings.ToUpper(spec.Method))
	builder.WriteString(" ")
	builder.WriteString(joinBasePath(basePath, spec.Path))
	builder.WriteString("`\n")
	if len(op.Tags) > 0 {
		builder.WriteString("- Tags: `")
		builder.WriteString(strings.Join(op.Tags, "`, `"))
		builder.WriteString("`\n")
	}
	if op.Deprecated {
		builder.WriteString("- Deprecated: `true`\n")
	}
	if len(op.Consumes) > 0 {
		builder.WriteString("- Consumes: `")
		builder.WriteString(strings.Join(op.Consumes, "`, `"))
		builder.WriteString("`\n")
	}
	if len(op.Produces) > 0 {
		builder.WriteString("- Produces: `")
		builder.WriteString(strings.Join(op.Produces, "`, `"))
		builder.WriteString("`\n")
	}
	builder.WriteString("- Summary: ")
	builder.WriteString(op.Summary)
	builder.WriteString("\n")
	if strings.TrimSpace(op.Description) != "" {
		builder.WriteString("- Description: ")
		builder.WriteString(op.Description)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")

	builder.WriteString("#### Parameters\n\n")
	if len(op.Parameters) == 0 {
		builder.WriteString("No parameters.\n\n")
	} else {
		builder.WriteString("| Name | In | Type | Required | Description |\n")
		builder.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, param := range op.Parameters {
			builder.WriteString("| ")
			builder.WriteString(escapePipes(param.Name))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(param.In))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(parameterType(param)))
			builder.WriteString(" | ")
			builder.WriteString(boolWord(param.Required))
			builder.WriteString(" | ")
			builder.WriteString(escapePipes(strings.TrimSpace(param.Description)))
			builder.WriteString(" |\n")
		}
		builder.WriteString("\n")
	}

	builder.WriteString("#### Responses\n\n")
	builder.WriteString("| Status | Schema | Description |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, code := range sortedResponseCodes(op.Responses) {
		resp := op.Responses[code]
		builder.WriteString("| ")
		builder.WriteString(code)
		builder.WriteString(" | ")
		builder.WriteString(escapePipes(schemaType(resp.Schema)))
		builder.WriteString(" | ")
		builder.WriteString(escapePipes(strings.TrimSpace(resp.Description)))
		builder.WriteString(" |\n")
	}
	builder.WriteString("\n")
}

func writeIndicatorExamples(builder *strings.Builder) {
	builder.WriteString("#### curl Example: Preset catalog\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS \"http://192.168.1.9:9010/api/v1/indicators/presets\" | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Built-in presets[]\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"presets\": [\"classic-volatility\", \"classic-momentum\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Simplified indicators[]\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"indicators\": [\"ta.sma(close,5)\", \"ta.rsi(close,10)\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Presets + custom expressions\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"crypto-spot\",\n")
	builder.WriteString("    \"symbol\": \"BTCUSDT\",\n")
	builder.WriteString("    \"interval\": \"4h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-03-01\",\n")
	builder.WriteString("    \"presets\": [\"classic-moving-averages\"],\n")
	builder.WriteString("    \"indicators\": [\"ta.percentrank(ta.rsi(close,14),20)\", \"ta.change(volume,5)\"],\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
	builder.WriteString("#### curl Example: Legacy DSL\n\n")
	builder.WriteString("```bash\n")
	builder.WriteString("curl -sS -X POST \"http://192.168.1.9:9010/api/v1/indicators/series\" \\\n")
	builder.WriteString("  -H \"Content-Type: application/json\" \\\n")
	builder.WriteString("  --data '{\n")
	builder.WriteString("    \"market\": \"us-stocks\",\n")
	builder.WriteString("    \"symbol\": \"AAPL\",\n")
	builder.WriteString("    \"interval\": \"1h\",\n")
	builder.WriteString("    \"from\": \"2024-01-01\",\n")
	builder.WriteString("    \"to\": \"2024-02-01\",\n")
	builder.WriteString("    \"dsl\": \"plot(close, title=\\\"Close\\\")\\nplot(ta.sma(close,5), title=\\\"SMA\\\")\",\n")
	builder.WriteString("    \"precision\": 2\n")
	builder.WriteString("  }' | jq\n")
	builder.WriteString("```\n\n")
}

func joinBasePath(basePath string, path string) string {
	base := strings.TrimSuffix(basePath, "/")
	if base == "" {
		return path
	}
	return base + path
}

func parameterType(param parameter) string {
	if param.Schema != nil {
		return schemaType(param.Schema)
	}
	if param.Type != "" {
		if param.Type == "array" && param.Items != nil {
			return "array<" + schemaType(param.Items) + ">"
		}
		return param.Type
	}
	return "object"
}

func schemaType(schema *schemaRef) string {
	if schema == nil {
		return "-"
	}
	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")
		return parts[len(parts)-1]
	}
	if schema.Type == "array" && schema.Items != nil {
		return "array<" + schemaType(schema.Items) + ">"
	}
	if schema.Type != "" {
		return schema.Type
	}
	return "object"
}

func sortedResponseCodes(responses map[string]response) []string {
	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool {
		return codes[i] < codes[j]
	})
	return codes
}

func boolWord(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func escapePipes(value string) string {
	if value == "" {
		return "-"
	}
	return strings.ReplaceAll(value, "|", "\\|")
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "&", "and")
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}
