//go:build wasip1

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	_ "github.com/breml/rootcerts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "github.com/stealthrocket/net/http"
	_ "github.com/stealthrocket/net/wasip1"
)

const apiBaseURL = "https://api.frankfurter.app"

type CurrencyClient struct {
	HTTPClient *http.Client
}

func NewCurrencyClient() *CurrencyClient {
	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	// Clone default transport to preserve stealthrocket/net WASM patches
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		NextProtos: []string{"http/1.1"},
	}
	return &CurrencyClient{
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func main() {
	server := mcp.NewServer("Currency MCP Server", "1.0.0", nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "currency_convert",
		Description: "Convert an amount from one currency to another using real-time exchange rates",
	}, convertHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "currency_rate",
		Description: "Get the current exchange rate between two currencies",
	}, rateHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "currency_list",
		Description: "List all available currency codes and their names",
	}, listHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "currency_historical",
		Description: "Get the exchange rate for a specific historical date",
	}, historicalHandler)

	log.Println("Starting Currency MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

type ConvertParams struct {
	Amount float64 `json:"amount"`
	From   string  `json:"from"`
	To     string  `json:"to"`
}

type RateParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ListParams struct{}

type HistoricalParams struct {
	Date string `json:"date"` // YYYY-MM-DD format
	From string `json:"from"`
	To   string `json:"to"`
}

type ConversionResponse struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

func convertHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ConvertParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewCurrencyClient()

	if args.Amount <= 0 {
		return errorResult("amount must be positive"), nil
	}
	if args.From == "" || args.To == "" {
		return errorResult("from and to currency codes are required"), nil
	}

	from := strings.ToUpper(args.From)
	to := strings.ToUpper(args.To)

	endpoint := fmt.Sprintf("%s/latest?amount=%.2f&from=%s&to=%s", apiBaseURL, args.Amount, from, to)

	resp, err := client.makeRequest("GET", endpoint)
	if err != nil {
		return errorResult("Failed to fetch exchange rate: " + err.Error()), nil
	}

	var data ConversionResponse
	if err := json.Unmarshal(resp, &data); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	converted := data.Rates[to]
	rate := converted / args.Amount

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Currency Conversion\n\n"))
	output.WriteString(fmt.Sprintf("%.2f %s = %.2f %s\n\n", args.Amount, from, converted, to))
	output.WriteString(fmt.Sprintf("Exchange Rate: 1 %s = %.6f %s\n", from, rate, to))
	output.WriteString(fmt.Sprintf("Date: %s\n", data.Date))

	return successResult(output.String()), nil
}

func rateHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[RateParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewCurrencyClient()

	if args.From == "" || args.To == "" {
		return errorResult("from and to currency codes are required"), nil
	}

	from := strings.ToUpper(args.From)
	to := strings.ToUpper(args.To)

	endpoint := fmt.Sprintf("%s/latest?from=%s&to=%s", apiBaseURL, from, to)

	resp, err := client.makeRequest("GET", endpoint)
	if err != nil {
		return errorResult("Failed to fetch exchange rate: " + err.Error()), nil
	}

	var data ConversionResponse
	if err := json.Unmarshal(resp, &data); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	rate := data.Rates[to]

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Exchange Rate\n\n"))
	output.WriteString(fmt.Sprintf("1 %s = %.6f %s\n\n", from, rate, to))
	output.WriteString(fmt.Sprintf("Date: %s\n", data.Date))

	return successResult(output.String()), nil
}

func listHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListParams]) (*mcp.CallToolResultFor[any], error) {
	client := NewCurrencyClient()

	endpoint := fmt.Sprintf("%s/currencies", apiBaseURL)

	resp, err := client.makeRequest("GET", endpoint)
	if err != nil {
		return errorResult("Failed to fetch currencies: " + err.Error()), nil
	}

	var currencies map[string]string
	if err := json.Unmarshal(resp, &currencies); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Available Currencies (%d total)\n\n", len(currencies)))

	for code, name := range currencies {
		output.WriteString(fmt.Sprintf("%s - %s\n", code, name))
	}

	return successResult(output.String()), nil
}

func historicalHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[HistoricalParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewCurrencyClient()

	if args.Date == "" {
		return errorResult("date is required (YYYY-MM-DD format)"), nil
	}
	if args.From == "" || args.To == "" {
		return errorResult("from and to currency codes are required"), nil
	}

	from := strings.ToUpper(args.From)
	to := strings.ToUpper(args.To)

	endpoint := fmt.Sprintf("%s/%s?from=%s&to=%s", apiBaseURL, url.PathEscape(args.Date), from, to)

	resp, err := client.makeRequest("GET", endpoint)
	if err != nil {
		return errorResult("Failed to fetch historical rate: " + err.Error()), nil
	}

	var data ConversionResponse
	if err := json.Unmarshal(resp, &data); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	rate := data.Rates[to]

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Historical Exchange Rate\n\n"))
	output.WriteString(fmt.Sprintf("Date: %s\n\n", data.Date))
	output.WriteString(fmt.Sprintf("1 %s = %.6f %s\n", from, rate, to))

	return successResult(output.String()), nil
}

func (c *CurrencyClient) makeRequest(method, endpoint string) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mcper-currency-mcp/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func errorResult(msg string) *mcp.CallToolResultFor[any] {
	return &mcp.CallToolResultFor[any]{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func successResult(msg string) *mcp.CallToolResultFor[any] {
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}
