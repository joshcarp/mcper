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

const (
	linkedInAPIBaseURL = "https://api.linkedin.com/v2"
	// Placeholder token that will be replaced by the host
	placeholderToken = "PLACEHOLDER_OAUTH_TOKEN"
)

// LinkedInClient handles API communication
type LinkedInClient struct {
	AccessToken string
	HTTPClient  *http.Client
}

// NewLinkedInClient creates a new LinkedIn API client
func NewLinkedInClient(accessToken string) *LinkedInClient {
	// If no access token is provided, use the placeholder
	if accessToken == "" {
		accessToken = placeholderToken
	}

	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}

	return &LinkedInClient{
		AccessToken: accessToken,
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func main() {
	// Create a new MCP server
	server := mcp.NewServer("LinkedIn MCP Server", "1.0.0", nil)

	// Add LinkedIn search tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_people",
		Description: "Search for people on LinkedIn with various filters",
	}, linkedInSearchPeopleHandler)

	// Add LinkedIn profile tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_profile",
		Description: "Get detailed profile information for a LinkedIn user",
	}, linkedInGetProfileHandler)

	// Add LinkedIn company search tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_search_companies",
		Description: "Search for companies on LinkedIn",
	}, linkedInSearchCompaniesHandler)

	// Add LinkedIn connection tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linkedin_get_connections",
		Description: "Get user's LinkedIn connections",
	}, linkedInGetConnectionsHandler)

	// Start the server
	log.Println("Starting LinkedIn MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// LinkedInSearchParams defines the parameters for LinkedIn people search
type LinkedInSearchParams struct {
	AccessToken string `json:"access_token,omitempty"`
	Query       string `json:"query"`
	Location    string `json:"location,omitempty"`
	Industry    string `json:"industry,omitempty"`
	Company     string `json:"company,omitempty"`
	Title       string `json:"title,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// LinkedInProfileParams defines the parameters for getting a profile
type LinkedInProfileParams struct {
	AccessToken string `json:"access_token,omitempty"`
	ProfileID   string `json:"profile_id"`
}

// LinkedInCompanySearchParams defines the parameters for company search
type LinkedInCompanySearchParams struct {
	AccessToken string `json:"access_token,omitempty"`
	Query       string `json:"query"`
	Industry    string `json:"industry,omitempty"`
	Location    string `json:"location,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// LinkedInConnectionsParams defines the parameters for getting connections
type LinkedInConnectionsParams struct {
	AccessToken string `json:"access_token,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// linkedInSearchPeopleHandler handles LinkedIn people search
func linkedInSearchPeopleHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[LinkedInSearchParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments

	// Access token is now optional - will use placeholder if not provided
	if args.Query == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Search query is required"}},
		}, nil
	}

	client := NewLinkedInClient(args.AccessToken)

	// Build search query with filters
	searchQuery := buildPeopleSearchQuery(args)

	result, err := client.searchPeople(searchQuery, args.Limit)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("LinkedIn search failed: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// linkedInGetProfileHandler handles getting LinkedIn profile details
func linkedInGetProfileHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[LinkedInProfileParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments

	if args.ProfileID == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Profile ID is required"}},
		}, nil
	}

	client := NewLinkedInClient(args.AccessToken)

	result, err := client.getProfile(args.ProfileID)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to get profile: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// linkedInSearchCompaniesHandler handles LinkedIn company search
func linkedInSearchCompaniesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[LinkedInCompanySearchParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments

	if args.Query == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Search query is required"}},
		}, nil
	}

	client := NewLinkedInClient(args.AccessToken)

	result, err := client.searchCompanies(args.Query, args.Industry, args.Location, args.Limit)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Company search failed: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// linkedInGetConnectionsHandler handles getting user connections
func linkedInGetConnectionsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[LinkedInConnectionsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments

	client := NewLinkedInClient(args.AccessToken)

	result, err := client.getConnections(args.Limit)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to get connections: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// buildPeopleSearchQuery constructs the search query with filters
func buildPeopleSearchQuery(params LinkedInSearchParams) string {
	query := params.Query

	if params.Location != "" {
		query += fmt.Sprintf(" AND location:\"%s\"", params.Location)
	}

	if params.Industry != "" {
		query += fmt.Sprintf(" AND industry:\"%s\"", params.Industry)
	}

	if params.Company != "" {
		query += fmt.Sprintf(" AND company:\"%s\"", params.Company)
	}

	if params.Title != "" {
		query += fmt.Sprintf(" AND title:\"%s\"", params.Title)
	}

	return query
}

// searchPeople performs a LinkedIn people search
func (c *LinkedInClient) searchPeople(query string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}

	// LinkedIn People Search API endpoint
	endpoint := fmt.Sprintf("%s/peopleSearch?q=people&keywords=%s&count=%d",
		linkedInAPIBaseURL, url.QueryEscape(query), limit)

	resp, err := c.makeAuthenticatedRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("search request failed: %v", err)
	}

	// Parse and format the response
	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	return formatPeopleSearchResults(result), nil
}

// getProfile retrieves a LinkedIn profile
func (c *LinkedInClient) getProfile(profileID string) (string, error) {
	endpoint := fmt.Sprintf("%s/people/%s", linkedInAPIBaseURL, profileID)

	resp, err := c.makeAuthenticatedRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("profile request failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	return formatProfileResult(result), nil
}

// searchCompanies performs a LinkedIn company search
func (c *LinkedInClient) searchCompanies(query, industry, location string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}

	// Build search parameters
	params := url.Values{}
	params.Set("q", "companies")
	params.Set("keywords", query)
	params.Set("count", fmt.Sprintf("%d", limit))

	if industry != "" {
		params.Set("industry", industry)
	}

	if location != "" {
		params.Set("location", location)
	}

	endpoint := fmt.Sprintf("%s/companySearch?%s", linkedInAPIBaseURL, params.Encode())

	resp, err := c.makeAuthenticatedRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("company search request failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	return formatCompanySearchResults(result), nil
}

// getConnections retrieves user's LinkedIn connections
func (c *LinkedInClient) getConnections(limit int) (string, error) {
	if limit <= 0 {
		limit = 50
	}

	endpoint := fmt.Sprintf("%s/connections?count=%d", linkedInAPIBaseURL, limit)

	resp, err := c.makeAuthenticatedRequest("GET", endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("connections request failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	return formatConnectionsResult(result), nil
}

// makeAuthenticatedRequest makes an authenticated HTTP request to LinkedIn API
func (c *LinkedInClient) makeAuthenticatedRequest(method, endpoint string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}

	// Always set the Authorization header - will be replaced by host if needed
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}

// formatPeopleSearchResults formats the people search results
func formatPeopleSearchResults(data map[string]interface{}) string {
	var result strings.Builder
	result.WriteString("LinkedIn People Search Results:\n\n")

	if elements, ok := data["elements"].([]interface{}); ok {
		for i, element := range elements {
			if person, ok := element.(map[string]interface{}); ok {
				result.WriteString(fmt.Sprintf("%d. ", i+1))

				if firstName, ok := person["firstName"].(string); ok {
					result.WriteString(firstName + " ")
				}
				if lastName, ok := person["lastName"].(string); ok {
					result.WriteString(lastName)
				}
				result.WriteString("\n")

				if headline, ok := person["headline"].(string); ok {
					result.WriteString(fmt.Sprintf("   Headline: %s\n", headline))
				}

				if location, ok := person["location"].(map[string]interface{}); ok {
					if name, ok := location["name"].(string); ok {
						result.WriteString(fmt.Sprintf("   Location: %s\n", name))
					}
				}

				if id, ok := person["id"].(string); ok {
					result.WriteString(fmt.Sprintf("   Profile ID: %s\n", id))
				}

				result.WriteString("\n")
			}
		}
	}

	return result.String()
}

// formatProfileResult formats the profile result
func formatProfileResult(data map[string]interface{}) string {
	var result strings.Builder
	result.WriteString("LinkedIn Profile:\n\n")

	if firstName, ok := data["firstName"].(string); ok {
		result.WriteString(fmt.Sprintf("First Name: %s\n", firstName))
	}

	if lastName, ok := data["lastName"].(string); ok {
		result.WriteString(fmt.Sprintf("Last Name: %s\n", lastName))
	}

	if headline, ok := data["headline"].(string); ok {
		result.WriteString(fmt.Sprintf("Headline: %s\n", headline))
	}

	if summary, ok := data["summary"].(string); ok {
		result.WriteString(fmt.Sprintf("Summary: %s\n", summary))
	}

	if location, ok := data["location"].(map[string]interface{}); ok {
		if name, ok := location["name"].(string); ok {
			result.WriteString(fmt.Sprintf("Location: %s\n", name))
		}
	}

	if industry, ok := data["industry"].(string); ok {
		result.WriteString(fmt.Sprintf("Industry: %s\n", industry))
	}

	if id, ok := data["id"].(string); ok {
		result.WriteString(fmt.Sprintf("Profile ID: %s\n", id))
	}

	return result.String()
}

// formatCompanySearchResults formats the company search results
func formatCompanySearchResults(data map[string]interface{}) string {
	var result strings.Builder
	result.WriteString("LinkedIn Company Search Results:\n\n")

	if elements, ok := data["elements"].([]interface{}); ok {
		for i, element := range elements {
			if company, ok := element.(map[string]interface{}); ok {
				result.WriteString(fmt.Sprintf("%d. ", i+1))

				if name, ok := company["name"].(string); ok {
					result.WriteString(name)
				}
				result.WriteString("\n")

				if description, ok := company["description"].(string); ok {
					result.WriteString(fmt.Sprintf("   Description: %s\n", description))
				}

				if industry, ok := company["industry"].(string); ok {
					result.WriteString(fmt.Sprintf("   Industry: %s\n", industry))
				}

				if location, ok := company["location"].(map[string]interface{}); ok {
					if name, ok := location["name"].(string); ok {
						result.WriteString(fmt.Sprintf("   Location: %s\n", name))
					}
				}

				if id, ok := company["id"].(string); ok {
					result.WriteString(fmt.Sprintf("   Company ID: %s\n", id))
				}

				result.WriteString("\n")
			}
		}
	}

	return result.String()
}

// formatConnectionsResult formats the connections result
func formatConnectionsResult(data map[string]interface{}) string {
	var result strings.Builder
	result.WriteString("LinkedIn Connections:\n\n")

	if elements, ok := data["elements"].([]interface{}); ok {
		for i, element := range elements {
			if connection, ok := element.(map[string]interface{}); ok {
				result.WriteString(fmt.Sprintf("%d. ", i+1))

				if firstName, ok := connection["firstName"].(string); ok {
					result.WriteString(firstName + " ")
				}
				if lastName, ok := connection["lastName"].(string); ok {
					result.WriteString(lastName)
				}
				result.WriteString("\n")

				if headline, ok := connection["headline"].(string); ok {
					result.WriteString(fmt.Sprintf("   Headline: %s\n", headline))
				}

				if id, ok := connection["id"].(string); ok {
					result.WriteString(fmt.Sprintf("   Profile ID: %s\n", id))
				}

				result.WriteString("\n")
			}
		}
	}

	return result.String()
}
