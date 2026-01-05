//go:build wasip1

package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/breml/rootcerts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "github.com/stealthrocket/net/http"
	_ "github.com/stealthrocket/net/wasip1"
)

// GmailClient handles API communication through the MCPer proxy
type GmailClient struct {
	ProxyURL   string // MCPER_PROXY_URL - the proxy endpoint for token injection
	AuthToken  string // MCPER_AUTH_TOKEN - auth token for the proxy
	HTTPClient *http.Client
}

// NewGmailClient creates a new Gmail API client using MCPer proxy for token injection
func NewGmailClient() *GmailClient {
	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}

	return &GmailClient{
		ProxyURL:  os.Getenv("MCPER_PROXY_URL"),
		AuthToken: os.Getenv("MCPER_AUTH_TOKEN"),
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// getGmailBaseURL returns the Gmail API base URL through the proxy
func (c *GmailClient) getGmailBaseURL() string {
	if c.ProxyURL != "" {
		// Use proxy: MCPER_PROXY_URL/gmail.googleapis.com/gmail/v1/users/me
		return strings.TrimSuffix(c.ProxyURL, "/") + "/gmail.googleapis.com/gmail/v1/users/me"
	}
	// Fallback to direct API (requires GMAIL_ACCESS_TOKEN to be set manually)
	return "https://gmail.googleapis.com/gmail/v1/users/me"
}

// isConfigured returns true if the client has proxy configuration
func (c *GmailClient) isConfigured() bool {
	return c.ProxyURL != "" && c.AuthToken != ""
}

func main() {
	// Create a new MCP server
	server := mcp.NewServer("Gmail MCP Server", "1.0.0", nil)

	// Add Gmail tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_list_messages",
		Description: "List emails from Gmail inbox with optional filters",
	}, listMessagesHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_get_message",
		Description: "Get the full content of a specific email by ID",
	}, getMessageHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_search",
		Description: "Search emails using Gmail search syntax (e.g., 'from:user@example.com', 'subject:hello', 'is:unread')",
	}, searchMessagesHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_send",
		Description: "Send an email",
	}, sendMessageHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_reply",
		Description: "Reply to an existing email thread",
	}, replyMessageHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_list_labels",
		Description: "List all Gmail labels (folders)",
	}, listLabelsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "gmail_modify_labels",
		Description: "Add or remove labels from a message (e.g., mark as read, archive)",
	}, modifyLabelsHandler)

	// Start the server
	log.Println("Starting Gmail MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// ListMessagesParams defines parameters for listing messages
type ListMessagesParams struct {
	MaxResults int    `json:"max_results,omitempty"`
	LabelIds   string `json:"label_ids,omitempty"` // Comma-separated: INBOX,UNREAD
	Query      string `json:"query,omitempty"`     // Gmail search query
}

// GetMessageParams defines parameters for getting a message
type GetMessageParams struct {
	MessageID string `json:"message_id"`
	Format    string `json:"format,omitempty"` // full, metadata, minimal
}

// SearchParams defines parameters for searching messages
type SearchParams struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// SendMessageParams defines parameters for sending a message
type SendMessageParams struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	CC      string `json:"cc,omitempty"`
	BCC     string `json:"bcc,omitempty"`
}

// ReplyMessageParams defines parameters for replying to a message
type ReplyMessageParams struct {
	MessageID string `json:"message_id"`
	Body      string `json:"body"`
}

// ModifyLabelsParams defines parameters for modifying labels
type ModifyLabelsParams struct {
	MessageID    string `json:"message_id"`
	AddLabels    string `json:"add_labels,omitempty"`    // Comma-separated
	RemoveLabels string `json:"remove_labels,omitempty"` // Comma-separated
}

// listMessagesHandler handles listing Gmail messages
func listMessagesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListMessagesParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	result, err := client.listMessages(args.LabelIds, args.Query, maxResults)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to list messages: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// getMessageHandler handles getting a specific message
func getMessageHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetMessageParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	if args.MessageID == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "message_id is required"}},
		}, nil
	}

	format := args.Format
	if format == "" {
		format = "full"
	}

	result, err := client.getMessage(args.MessageID, format)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to get message: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// searchMessagesHandler handles searching messages
func searchMessagesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	if args.Query == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "query is required (e.g., 'from:user@example.com', 'subject:hello', 'is:unread')"}},
		}, nil
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	result, err := client.listMessages("", args.Query, maxResults)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Search failed: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// sendMessageHandler handles sending a new message
func sendMessageHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SendMessageParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	if args.To == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "to (recipient email) is required"}},
		}, nil
	}

	if args.Subject == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "subject is required"}},
		}, nil
	}

	result, err := client.sendMessage(args.To, args.Subject, args.Body, args.CC, args.BCC)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to send message: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// replyMessageHandler handles replying to a message
func replyMessageHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ReplyMessageParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	if args.MessageID == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "message_id is required"}},
		}, nil
	}

	if args.Body == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "body is required"}},
		}, nil
	}

	result, err := client.replyToMessage(args.MessageID, args.Body)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to reply: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// EmptyParams for tools with no parameters
type EmptyParams struct{}

// listLabelsHandler handles listing Gmail labels
func listLabelsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[EmptyParams]) (*mcp.CallToolResultFor[any], error) {
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	result, err := client.listLabels()
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to list labels: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// modifyLabelsHandler handles modifying message labels
func modifyLabelsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ModifyLabelsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGmailClient()

	if !client.isConfigured() {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Gmail not configured. Please authenticate with Google through MCPer."}},
		}, nil
	}

	if args.MessageID == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "message_id is required"}},
		}, nil
	}

	if args.AddLabels == "" && args.RemoveLabels == "" {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "At least one of add_labels or remove_labels is required"}},
		}, nil
	}

	result, err := client.modifyLabels(args.MessageID, args.AddLabels, args.RemoveLabels)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Failed to modify labels: %v", err)}},
		}, nil
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: result}},
	}, nil
}

// Gmail API methods

func (c *GmailClient) listMessages(labelIds, query string, maxResults int) (string, error) {
	params := url.Values{}
	params.Set("maxResults", fmt.Sprintf("%d", maxResults))

	if labelIds != "" {
		for _, label := range strings.Split(labelIds, ",") {
			params.Add("labelIds", strings.TrimSpace(label))
		}
	}

	if query != "" {
		params.Set("q", query)
	}

	endpoint := fmt.Sprintf("%s/messages?%s", c.getGmailBaseURL(), params.Encode())

	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	if len(result.Messages) == 0 {
		return "No messages found.", nil
	}

	// Fetch details for each message
	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d messages:\n\n", len(result.Messages)))

	for i, msg := range result.Messages {
		details, err := c.getMessageMetadata(msg.ID)
		if err != nil {
			output.WriteString(fmt.Sprintf("%d. [Error fetching message %s]\n", i+1, msg.ID))
			continue
		}
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, details))
	}

	return output.String(), nil
}

func (c *GmailClient) getMessageMetadata(messageID string) (string, error) {
	endpoint := fmt.Sprintf("%s/messages/%s?format=metadata&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date",
		c.getGmailBaseURL(), messageID)

	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var msg struct {
		ID      string `json:"id"`
		Snippet string `json:"snippet"`
		Payload struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(resp, &msg); err != nil {
		return "", err
	}

	var from, subject, date string
	for _, h := range msg.Payload.Headers {
		switch h.Name {
		case "From":
			from = h.Value
		case "Subject":
			subject = h.Value
		case "Date":
			date = h.Value
		}
	}

	return fmt.Sprintf("ID: %s\n   From: %s\n   Subject: %s\n   Date: %s\n   Preview: %s",
		msg.ID, from, subject, date, truncate(msg.Snippet, 80)), nil
}

func (c *GmailClient) getMessage(messageID, format string) (string, error) {
	endpoint := fmt.Sprintf("%s/messages/%s?format=%s", c.getGmailBaseURL(), messageID, format)

	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var msg struct {
		ID       string   `json:"id"`
		ThreadID string   `json:"threadId"`
		LabelIDs []string `json:"labelIds"`
		Snippet  string   `json:"snippet"`
		Payload  struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
			Body struct {
				Data string `json:"data"`
			} `json:"body"`
			Parts []struct {
				MimeType string `json:"mimeType"`
				Body     struct {
					Data string `json:"data"`
				} `json:"body"`
			} `json:"parts"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(resp, &msg); err != nil {
		return "", err
	}

	var output strings.Builder
	output.WriteString("Email Details:\n\n")
	output.WriteString(fmt.Sprintf("ID: %s\n", msg.ID))
	output.WriteString(fmt.Sprintf("Thread ID: %s\n", msg.ThreadID))
	output.WriteString(fmt.Sprintf("Labels: %s\n\n", strings.Join(msg.LabelIDs, ", ")))

	for _, h := range msg.Payload.Headers {
		switch h.Name {
		case "From", "To", "Subject", "Date", "Cc", "Bcc":
			output.WriteString(fmt.Sprintf("%s: %s\n", h.Name, h.Value))
		}
	}

	output.WriteString("\n--- Body ---\n")

	// Try to get body from parts first (multipart messages)
	body := ""
	for _, part := range msg.Payload.Parts {
		if part.MimeType == "text/plain" && part.Body.Data != "" {
			decoded, err := base64.URLEncoding.DecodeString(part.Body.Data)
			if err == nil {
				body = string(decoded)
				break
			}
		}
	}

	// Fallback to direct body
	if body == "" && msg.Payload.Body.Data != "" {
		decoded, err := base64.URLEncoding.DecodeString(msg.Payload.Body.Data)
		if err == nil {
			body = string(decoded)
		}
	}

	if body == "" {
		body = msg.Snippet
	}

	output.WriteString(body)

	return output.String(), nil
}

func (c *GmailClient) sendMessage(to, subject, body, cc, bcc string) (string, error) {
	// Build RFC 2822 message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	if cc != "" {
		msg.WriteString(fmt.Sprintf("Cc: %s\r\n", cc))
	}
	if bcc != "" {
		msg.WriteString(fmt.Sprintf("Bcc: %s\r\n", bcc))
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// Base64 URL encode the message
	encoded := base64.URLEncoding.EncodeToString([]byte(msg.String()))

	payload := map[string]string{"raw": encoded}
	payloadBytes, _ := json.Marshal(payload)

	endpoint := fmt.Sprintf("%s/messages/send", c.getGmailBaseURL())
	resp, err := c.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", err
	}

	var result struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	return fmt.Sprintf("Message sent successfully!\nMessage ID: %s\nThread ID: %s", result.ID, result.ThreadID), nil
}

func (c *GmailClient) replyToMessage(messageID, body string) (string, error) {
	// Get original message to extract headers
	origResp, err := c.makeRequest("GET", fmt.Sprintf("%s/messages/%s?format=metadata", c.getGmailBaseURL(), messageID), nil)
	if err != nil {
		return "", fmt.Errorf("failed to get original message: %v", err)
	}

	var origMsg struct {
		ThreadID string `json:"threadId"`
		Payload  struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(origResp, &origMsg); err != nil {
		return "", err
	}

	var from, subject, msgId string
	for _, h := range origMsg.Payload.Headers {
		switch h.Name {
		case "From":
			from = h.Value
		case "Subject":
			subject = h.Value
		case "Message-ID":
			msgId = h.Value
		}
	}

	// Build reply message
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("To: %s\r\n", from))
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	if msgId != "" {
		msg.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", msgId))
		msg.WriteString(fmt.Sprintf("References: %s\r\n", msgId))
	}
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	encoded := base64.URLEncoding.EncodeToString([]byte(msg.String()))

	payload := map[string]string{
		"raw":      encoded,
		"threadId": origMsg.ThreadID,
	}
	payloadBytes, _ := json.Marshal(payload)

	endpoint := fmt.Sprintf("%s/messages/send", c.getGmailBaseURL())
	resp, err := c.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", err
	}

	var result struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	return fmt.Sprintf("Reply sent successfully!\nMessage ID: %s\nThread ID: %s", result.ID, result.ThreadID), nil
}

func (c *GmailClient) listLabels() (string, error) {
	endpoint := fmt.Sprintf("%s/labels", c.getGmailBaseURL())

	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Labels []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"labels"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return "", err
	}

	var output strings.Builder
	output.WriteString("Gmail Labels:\n\n")

	output.WriteString("System Labels:\n")
	for _, label := range result.Labels {
		if label.Type == "system" {
			output.WriteString(fmt.Sprintf("  - %s (ID: %s)\n", label.Name, label.ID))
		}
	}

	output.WriteString("\nUser Labels:\n")
	for _, label := range result.Labels {
		if label.Type == "user" {
			output.WriteString(fmt.Sprintf("  - %s (ID: %s)\n", label.Name, label.ID))
		}
	}

	return output.String(), nil
}

func (c *GmailClient) modifyLabels(messageID, addLabels, removeLabels string) (string, error) {
	payload := make(map[string][]string)

	if addLabels != "" {
		labels := strings.Split(addLabels, ",")
		for i := range labels {
			labels[i] = strings.TrimSpace(labels[i])
		}
		payload["addLabelIds"] = labels
	}

	if removeLabels != "" {
		labels := strings.Split(removeLabels, ",")
		for i := range labels {
			labels[i] = strings.TrimSpace(labels[i])
		}
		payload["removeLabelIds"] = labels
	}

	payloadBytes, _ := json.Marshal(payload)

	endpoint := fmt.Sprintf("%s/messages/%s/modify", c.getGmailBaseURL(), messageID)
	_, err := c.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return "", err
	}

	var actions []string
	if addLabels != "" {
		actions = append(actions, fmt.Sprintf("added labels: %s", addLabels))
	}
	if removeLabels != "" {
		actions = append(actions, fmt.Sprintf("removed labels: %s", removeLabels))
	}

	return fmt.Sprintf("Successfully %s for message %s", strings.Join(actions, " and "), messageID), nil
}

func (c *GmailClient) makeRequest(method, endpoint string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}

	// Authenticate with the MCPer proxy using the auth token
	// The proxy will inject the actual Google OAuth token
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
