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

const (
	defaultGitHubAPIBaseURL = "https://api.github.com"
)

// getAPIBaseURL returns the API base URL, checking for proxy configuration
// If MCPER_PROXY_URL is set, it uses the proxy for token injection (e.g., https://mcper.io/api/forward/api.github.com)
// Otherwise uses the default GitHub API URL
func getAPIBaseURL() string {
	if proxyURL := os.Getenv("MCPER_PROXY_URL"); proxyURL != "" {
		// MCPER_PROXY_URL should be like "https://mcper.io/api/forward"
		// We append the target host to form the full proxy URL
		return proxyURL + "/api.github.com"
	}
	return defaultGitHubAPIBaseURL
}

// GitHubClient handles API communication
type GitHubClient struct {
	Token      string
	ProxyAuth  string // Auth token for the mcper proxy
	BaseURL    string
	UseProxy   bool
	HTTPClient *http.Client
}

// NewGitHubClient creates a new GitHub API client
// Uses GITHUB_TOKEN from environment if available
// Uses MCPER_PROXY_URL for token injection when running via mcper-cloud
func NewGitHubClient() *GitHubClient {
	proxyURL := os.Getenv("MCPER_PROXY_URL")
	useProxy := proxyURL != ""

	// Force HTTP/1.1 - WASM runtime doesn't support HTTP/2 parsing
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}

	return &GitHubClient{
		Token:     os.Getenv("GITHUB_TOKEN"),
		ProxyAuth: os.Getenv("MCPER_AUTH_TOKEN"), // Auth token for proxy requests
		BaseURL:   getAPIBaseURL(),
		UseProxy:  useProxy,
		HTTPClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func main() {
	server := mcp.NewServer("GitHub MCP Server", "1.0.0", nil)

	// Repository tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_repos",
		Description: "List repositories for the authenticated user or a specific user/org",
	}, listReposHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_repo",
		Description: "Get details about a specific repository",
	}, getRepoHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_search_repos",
		Description: "Search for repositories on GitHub",
	}, searchReposHandler)

	// Issue tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_issues",
		Description: "List issues for a repository",
	}, listIssuesHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_issue",
		Description: "Get details about a specific issue",
	}, getIssueHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_create_issue",
		Description: "Create a new issue in a repository",
	}, createIssueHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_add_issue_comment",
		Description: "Add a comment to an issue or pull request",
	}, addIssueCommentHandler)

	// Pull Request tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_prs",
		Description: "List pull requests for a repository",
	}, listPRsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_pr",
		Description: "Get details about a specific pull request",
	}, getPRHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_pr_diff",
		Description: "Get the diff for a pull request",
	}, getPRDiffHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_pr_files",
		Description: "List files changed in a pull request",
	}, listPRFilesHandler)

	// Code & Content tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_file",
		Description: "Get contents of a file from a repository",
	}, getFileHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_search_code",
		Description: "Search for code across GitHub repositories",
	}, searchCodeHandler)

	// User tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_user",
		Description: "Get information about a GitHub user",
	}, getUserHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_commits",
		Description: "List commits for a repository",
	}, listCommitsHandler)

	log.Println("Starting GitHub MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// Parameter types (token is always from GITHUB_TOKEN environment variable)

type ListReposParams struct {
	Username string `json:"username,omitempty"` // If empty, lists authenticated user's repos
	Org      string `json:"org,omitempty"`      // List org repos instead
	Type     string `json:"type,omitempty"`     // all, owner, member (default: all)
	Sort     string `json:"sort,omitempty"`     // created, updated, pushed, full_name
	PerPage  int    `json:"per_page,omitempty"`
}

type GetRepoParams struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

type SearchReposParams struct {
	Query   string `json:"query"`
	Sort    string `json:"sort,omitempty"` // stars, forks, updated
	PerPage int    `json:"per_page,omitempty"`
}

type ListIssuesParams struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	State   string `json:"state,omitempty"` // open, closed, all
	Labels  string `json:"labels,omitempty"`
	Sort    string `json:"sort,omitempty"` // created, updated, comments
	PerPage int    `json:"per_page,omitempty"`
}

type GetIssueParams struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"`
}

type CreateIssueParams struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Labels string `json:"labels,omitempty"` // Comma-separated
}

type AddCommentParams struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	IssueNumber int    `json:"issue_number"` // Works for PRs too
	Body        string `json:"body"`
}

type ListPRsParams struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	State   string `json:"state,omitempty"` // open, closed, all
	Sort    string `json:"sort,omitempty"`  // created, updated, popularity
	PerPage int    `json:"per_page,omitempty"`
}

type GetPRParams struct {
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	PRNumber int    `json:"pr_number"`
}

type GetFileParams struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref,omitempty"` // Branch, tag, or commit SHA
}

type SearchCodeParams struct {
	Query   string `json:"query"`
	PerPage int    `json:"per_page,omitempty"`
}

type GetUserParams struct {
	Username string `json:"username"`
}

type ListCommitsParams struct {
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
	SHA     string `json:"sha,omitempty"`    // Branch or commit SHA
	Path    string `json:"path,omitempty"`   // Only commits for this path
	Author  string `json:"author,omitempty"` // GitHub username or email
	PerPage int    `json:"per_page,omitempty"`
}

// Handlers

func listReposHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListReposParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	var endpoint string
	if args.Org != "" {
		endpoint = fmt.Sprintf("%s/orgs/%s/repos", client.BaseURL, args.Org)
	} else if args.Username != "" {
		endpoint = fmt.Sprintf("%s/users/%s/repos", client.BaseURL, args.Username)
	} else {
		endpoint = fmt.Sprintf("%s/user/repos", client.BaseURL)
	}

	queryParams := url.Values{}
	if args.Type != "" {
		queryParams.Set("type", args.Type)
	}
	if args.Sort != "" {
		queryParams.Set("sort", args.Sort)
	}
	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))

	if len(queryParams) > 0 {
		endpoint += "?" + queryParams.Encode()
	}

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list repos: " + err.Error()), nil
	}

	var repos []struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
		Stars       int    `json:"stargazers_count"`
		Forks       int    `json:"forks_count"`
		Language    string `json:"language"`
		UpdatedAt   string `json:"updated_at"`
		HTMLURL     string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &repos); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d repositories:\n\n", len(repos)))

	for i, repo := range repos {
		visibility := "public"
		if repo.Private {
			visibility = "private"
		}
		output.WriteString(fmt.Sprintf("%d. %s (%s)\n", i+1, repo.FullName, visibility))
		if repo.Description != "" {
			output.WriteString(fmt.Sprintf("   %s\n", truncate(repo.Description, 80)))
		}
		output.WriteString(fmt.Sprintf("   ⭐ %d | 🍴 %d | %s\n", repo.Stars, repo.Forks, repo.Language))
		output.WriteString(fmt.Sprintf("   %s\n\n", repo.HTMLURL))
	}

	return successResult(output.String()), nil
}

func getRepoHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetRepoParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" {
		return errorResult("owner and repo are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s", client.BaseURL, args.Owner, args.Repo)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get repo: " + err.Error()), nil
	}

	var repo struct {
		FullName      string   `json:"full_name"`
		Description   string   `json:"description"`
		Private       bool     `json:"private"`
		Stars         int      `json:"stargazers_count"`
		Forks         int      `json:"forks_count"`
		OpenIssues    int      `json:"open_issues_count"`
		Language      string   `json:"language"`
		DefaultBranch string   `json:"default_branch"`
		CreatedAt     string   `json:"created_at"`
		UpdatedAt     string   `json:"updated_at"`
		HTMLURL       string   `json:"html_url"`
		CloneURL      string   `json:"clone_url"`
		Topics        []string `json:"topics"`
		License       *struct {
			Name string `json:"name"`
		} `json:"license"`
	}

	if err := json.Unmarshal(resp, &repo); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Repository: %s\n\n", repo.FullName))
	if repo.Description != "" {
		output.WriteString(fmt.Sprintf("Description: %s\n", repo.Description))
	}
	output.WriteString(fmt.Sprintf("URL: %s\n", repo.HTMLURL))
	output.WriteString(fmt.Sprintf("Clone: %s\n", repo.CloneURL))
	output.WriteString(fmt.Sprintf("Default Branch: %s\n", repo.DefaultBranch))
	output.WriteString(fmt.Sprintf("Language: %s\n", repo.Language))
	if repo.License != nil {
		output.WriteString(fmt.Sprintf("License: %s\n", repo.License.Name))
	}
	output.WriteString(fmt.Sprintf("\nStats: ⭐ %d stars | 🍴 %d forks | 📋 %d open issues\n",
		repo.Stars, repo.Forks, repo.OpenIssues))
	if len(repo.Topics) > 0 {
		output.WriteString(fmt.Sprintf("Topics: %s\n", strings.Join(repo.Topics, ", ")))
	}
	output.WriteString(fmt.Sprintf("\nCreated: %s\nUpdated: %s\n", repo.CreatedAt, repo.UpdatedAt))

	return successResult(output.String()), nil
}

func searchReposHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchReposParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Query == "" {
		return errorResult("query is required"), nil
	}

	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}

	queryParams := url.Values{}
	queryParams.Set("q", args.Query)
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))
	if args.Sort != "" {
		queryParams.Set("sort", args.Sort)
	}

	endpoint := fmt.Sprintf("%s/search/repositories?%s", client.BaseURL, queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Search failed: " + err.Error()), nil
	}

	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			Stars       int    `json:"stargazers_count"`
			Language    string `json:"language"`
			HTMLURL     string `json:"html_url"`
		} `json:"items"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d repositories (showing %d):\n\n", result.TotalCount, len(result.Items)))

	for i, repo := range result.Items {
		output.WriteString(fmt.Sprintf("%d. %s (⭐ %d)\n", i+1, repo.FullName, repo.Stars))
		if repo.Description != "" {
			output.WriteString(fmt.Sprintf("   %s\n", truncate(repo.Description, 80)))
		}
		output.WriteString(fmt.Sprintf("   Language: %s | %s\n\n", repo.Language, repo.HTMLURL))
	}

	return successResult(output.String()), nil
}

func listIssuesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListIssuesParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" {
		return errorResult("owner and repo are required"), nil
	}

	queryParams := url.Values{}
	if args.State != "" {
		queryParams.Set("state", args.State)
	}
	if args.Labels != "" {
		queryParams.Set("labels", args.Labels)
	}
	if args.Sort != "" {
		queryParams.Set("sort", args.Sort)
	}
	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))

	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues?%s", client.BaseURL, args.Owner, args.Repo, queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list issues: " + err.Error()), nil
	}

	var issues []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		CreatedAt   string    `json:"created_at"`
		HTMLURL     string    `json:"html_url"`
		PullRequest *struct{} `json:"pull_request"` // Present if it's a PR
	}

	if err := json.Unmarshal(resp, &issues); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Issues for %s/%s:\n\n", args.Owner, args.Repo))

	for _, issue := range issues {
		if issue.PullRequest != nil {
			continue // Skip PRs
		}
		stateIcon := "🟢"
		if issue.State == "closed" {
			stateIcon = "🔴"
		}
		output.WriteString(fmt.Sprintf("%s #%d: %s\n", stateIcon, issue.Number, issue.Title))
		output.WriteString(fmt.Sprintf("   By @%s | %s\n", issue.User.Login, issue.CreatedAt))
		if len(issue.Labels) > 0 {
			var labelNames []string
			for _, l := range issue.Labels {
				labelNames = append(labelNames, l.Name)
			}
			output.WriteString(fmt.Sprintf("   Labels: %s\n", strings.Join(labelNames, ", ")))
		}
		output.WriteString(fmt.Sprintf("   %s\n\n", issue.HTMLURL))
	}

	return successResult(output.String()), nil
}

func getIssueHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetIssueParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" || args.IssueNumber == 0 {
		return errorResult("owner, repo, and issue_number are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d", client.BaseURL, args.Owner, args.Repo, args.IssueNumber)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get issue: " + err.Error()), nil
	}

	var issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		Comments  int    `json:"comments"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
		ClosedAt  string `json:"closed_at"`
		HTMLURL   string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &issue); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	stateIcon := "🟢 Open"
	if issue.State == "closed" {
		stateIcon = "🔴 Closed"
	}
	output.WriteString(fmt.Sprintf("Issue #%d: %s\n", issue.Number, issue.Title))
	output.WriteString(fmt.Sprintf("Status: %s\n", stateIcon))
	output.WriteString(fmt.Sprintf("Author: @%s\n", issue.User.Login))
	output.WriteString(fmt.Sprintf("URL: %s\n", issue.HTMLURL))

	if len(issue.Labels) > 0 {
		var labelNames []string
		for _, l := range issue.Labels {
			labelNames = append(labelNames, l.Name)
		}
		output.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(labelNames, ", ")))
	}

	if len(issue.Assignees) > 0 {
		var assigneeNames []string
		for _, a := range issue.Assignees {
			assigneeNames = append(assigneeNames, "@"+a.Login)
		}
		output.WriteString(fmt.Sprintf("Assignees: %s\n", strings.Join(assigneeNames, ", ")))
	}

	output.WriteString(fmt.Sprintf("Comments: %d\n", issue.Comments))
	output.WriteString(fmt.Sprintf("Created: %s\n", issue.CreatedAt))
	if issue.ClosedAt != "" {
		output.WriteString(fmt.Sprintf("Closed: %s\n", issue.ClosedAt))
	}

	output.WriteString("\n--- Body ---\n")
	if issue.Body != "" {
		output.WriteString(issue.Body)
	} else {
		output.WriteString("(No description provided)")
	}

	return successResult(output.String()), nil
}

func createIssueHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CreateIssueParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if client.Token == "" {
		return errorResult("Token required to create issues"), nil
	}

	if args.Owner == "" || args.Repo == "" || args.Title == "" {
		return errorResult("owner, repo, and title are required"), nil
	}

	payload := map[string]interface{}{
		"title": args.Title,
	}
	if args.Body != "" {
		payload["body"] = args.Body
	}
	if args.Labels != "" {
		payload["labels"] = strings.Split(args.Labels, ",")
	}

	payloadBytes, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues", client.BaseURL, args.Owner, args.Repo)

	resp, err := client.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return errorResult("Failed to create issue: " + err.Error()), nil
	}

	var issue struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &issue); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Issue #%d created successfully!\n%s", issue.Number, issue.HTMLURL)), nil
}

func addIssueCommentHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[AddCommentParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if client.Token == "" {
		return errorResult("Token required to add comments"), nil
	}

	if args.Owner == "" || args.Repo == "" || args.IssueNumber == 0 || args.Body == "" {
		return errorResult("owner, repo, issue_number, and body are required"), nil
	}

	payload := map[string]string{"body": args.Body}
	payloadBytes, _ := json.Marshal(payload)

	endpoint := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", client.BaseURL, args.Owner, args.Repo, args.IssueNumber)

	resp, err := client.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return errorResult("Failed to add comment: " + err.Error()), nil
	}

	var comment struct {
		ID      int    `json:"id"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &comment); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Comment added successfully!\n%s", comment.HTMLURL)), nil
}

func listPRsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListPRsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" {
		return errorResult("owner and repo are required"), nil
	}

	queryParams := url.Values{}
	if args.State != "" {
		queryParams.Set("state", args.State)
	}
	if args.Sort != "" {
		queryParams.Set("sort", args.Sort)
	}
	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))

	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls?%s", client.BaseURL, args.Owner, args.Repo, queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list PRs: " + err.Error()), nil
	}

	var prs []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &prs); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Pull Requests for %s/%s:\n\n", args.Owner, args.Repo))

	for _, pr := range prs {
		stateIcon := "🟢"
		if pr.State == "closed" {
			stateIcon = "🟣"
		}
		if pr.Draft {
			stateIcon = "📝"
		}
		output.WriteString(fmt.Sprintf("%s #%d: %s\n", stateIcon, pr.Number, pr.Title))
		output.WriteString(fmt.Sprintf("   %s → %s | By @%s\n", pr.Head.Ref, pr.Base.Ref, pr.User.Login))
		output.WriteString(fmt.Sprintf("   %s\n\n", pr.HTMLURL))
	}

	return successResult(output.String()), nil
}

func getPRHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetPRParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" || args.PRNumber == 0 {
		return errorResult("owner, repo, and pr_number are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", client.BaseURL, args.Owner, args.Repo, args.PRNumber)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get PR: " + err.Error()), nil
	}

	var pr struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		Merged bool   `json:"merged"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Commits      int    `json:"commits"`
		Additions    int    `json:"additions"`
		Deletions    int    `json:"deletions"`
		ChangedFiles int    `json:"changed_files"`
		Mergeable    *bool  `json:"mergeable"`
		CreatedAt    string `json:"created_at"`
		MergedAt     string `json:"merged_at"`
		HTMLURL      string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &pr); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	status := "🟢 Open"
	if pr.Merged {
		status = "🟣 Merged"
	} else if pr.State == "closed" {
		status = "🔴 Closed"
	}
	if pr.Draft {
		status = "📝 Draft"
	}

	output.WriteString(fmt.Sprintf("PR #%d: %s\n", pr.Number, pr.Title))
	output.WriteString(fmt.Sprintf("Status: %s\n", status))
	output.WriteString(fmt.Sprintf("Author: @%s\n", pr.User.Login))
	output.WriteString(fmt.Sprintf("Branch: %s → %s\n", pr.Head.Ref, pr.Base.Ref))
	output.WriteString(fmt.Sprintf("URL: %s\n\n", pr.HTMLURL))
	output.WriteString(fmt.Sprintf("Changes: %d commits | +%d -%d | %d files changed\n",
		pr.Commits, pr.Additions, pr.Deletions, pr.ChangedFiles))
	if pr.Mergeable != nil {
		mergeable := "Yes"
		if !*pr.Mergeable {
			mergeable = "No (conflicts)"
		}
		output.WriteString(fmt.Sprintf("Mergeable: %s\n", mergeable))
	}
	output.WriteString(fmt.Sprintf("Created: %s\n", pr.CreatedAt))
	if pr.MergedAt != "" {
		output.WriteString(fmt.Sprintf("Merged: %s\n", pr.MergedAt))
	}

	output.WriteString("\n--- Description ---\n")
	if pr.Body != "" {
		output.WriteString(pr.Body)
	} else {
		output.WriteString("(No description provided)")
	}

	return successResult(output.String()), nil
}

func getPRDiffHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetPRParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" || args.PRNumber == 0 {
		return errorResult("owner, repo, and pr_number are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", client.BaseURL, args.Owner, args.Repo, args.PRNumber)

	resp, err := client.makeRequestWithAccept("GET", endpoint, nil, "application/vnd.github.v3.diff")
	if err != nil {
		return errorResult("Failed to get PR diff: " + err.Error()), nil
	}

	return successResult(string(resp)), nil
}

func listPRFilesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetPRParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" || args.PRNumber == 0 {
		return errorResult("owner, repo, and pr_number are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files", client.BaseURL, args.Owner, args.Repo, args.PRNumber)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list PR files: " + err.Error()), nil
	}

	var files []struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Changes   int    `json:"changes"`
	}

	if err := json.Unmarshal(resp, &files); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Files changed in PR #%d:\n\n", args.PRNumber))

	for _, f := range files {
		statusIcon := "M"
		switch f.Status {
		case "added":
			statusIcon = "A"
		case "removed":
			statusIcon = "D"
		case "renamed":
			statusIcon = "R"
		}
		output.WriteString(fmt.Sprintf("[%s] %s (+%d -%d)\n", statusIcon, f.Filename, f.Additions, f.Deletions))
	}

	return successResult(output.String()), nil
}

func getFileHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetFileParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" || args.Path == "" {
		return errorResult("owner, repo, and path are required"), nil
	}

	endpoint := fmt.Sprintf("%s/repos/%s/%s/contents/%s", client.BaseURL, args.Owner, args.Repo, args.Path)
	if args.Ref != "" {
		endpoint += "?ref=" + url.QueryEscape(args.Ref)
	}

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get file: " + err.Error()), nil
	}

	var file struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Size     int    `json:"size"`
		Type     string `json:"type"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		HTMLURL  string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &file); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	if file.Type != "file" {
		return errorResult("Path is a directory, not a file"), nil
	}

	var content string
	if file.Encoding == "base64" {
		decoded, err := decodeBase64(file.Content)
		if err != nil {
			return errorResult("Failed to decode file content: " + err.Error()), nil
		}
		content = decoded
	} else {
		content = file.Content
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", file.Path))
	output.WriteString(fmt.Sprintf("Size: %d bytes\n", file.Size))
	output.WriteString(fmt.Sprintf("URL: %s\n\n", file.HTMLURL))
	output.WriteString("--- Content ---\n")
	output.WriteString(content)

	return successResult(output.String()), nil
}

func searchCodeHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchCodeParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Query == "" {
		return errorResult("query is required (e.g., 'addClass repo:jquery/jquery')"), nil
	}

	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}

	queryParams := url.Values{}
	queryParams.Set("q", args.Query)
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))

	endpoint := fmt.Sprintf("%s/search/code?%s", client.BaseURL, queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Search failed: " + err.Error()), nil
	}

	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			HTMLURL string `json:"html_url"`
		} `json:"items"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d code results (showing %d):\n\n", result.TotalCount, len(result.Items)))

	for i, item := range result.Items {
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, item.Path))
		output.WriteString(fmt.Sprintf("   Repo: %s\n", item.Repository.FullName))
		output.WriteString(fmt.Sprintf("   %s\n\n", item.HTMLURL))
	}

	return successResult(output.String()), nil
}

func getUserHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetUserParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Username == "" {
		return errorResult("username is required"), nil
	}

	endpoint := fmt.Sprintf("%s/users/%s", client.BaseURL, args.Username)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get user: " + err.Error()), nil
	}

	var user struct {
		Login       string `json:"login"`
		Name        string `json:"name"`
		Bio         string `json:"bio"`
		Company     string `json:"company"`
		Location    string `json:"location"`
		Email       string `json:"email"`
		Blog        string `json:"blog"`
		PublicRepos int    `json:"public_repos"`
		Followers   int    `json:"followers"`
		Following   int    `json:"following"`
		CreatedAt   string `json:"created_at"`
		HTMLURL     string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &user); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("GitHub User: @%s\n\n", user.Login))
	if user.Name != "" {
		output.WriteString(fmt.Sprintf("Name: %s\n", user.Name))
	}
	if user.Bio != "" {
		output.WriteString(fmt.Sprintf("Bio: %s\n", user.Bio))
	}
	if user.Company != "" {
		output.WriteString(fmt.Sprintf("Company: %s\n", user.Company))
	}
	if user.Location != "" {
		output.WriteString(fmt.Sprintf("Location: %s\n", user.Location))
	}
	if user.Email != "" {
		output.WriteString(fmt.Sprintf("Email: %s\n", user.Email))
	}
	if user.Blog != "" {
		output.WriteString(fmt.Sprintf("Blog: %s\n", user.Blog))
	}
	output.WriteString(fmt.Sprintf("\nPublic Repos: %d\n", user.PublicRepos))
	output.WriteString(fmt.Sprintf("Followers: %d | Following: %d\n", user.Followers, user.Following))
	output.WriteString(fmt.Sprintf("Member since: %s\n", user.CreatedAt))
	output.WriteString(fmt.Sprintf("URL: %s\n", user.HTMLURL))

	return successResult(output.String()), nil
}

func listCommitsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListCommitsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewGitHubClient()

	if args.Owner == "" || args.Repo == "" {
		return errorResult("owner and repo are required"), nil
	}

	queryParams := url.Values{}
	if args.SHA != "" {
		queryParams.Set("sha", args.SHA)
	}
	if args.Path != "" {
		queryParams.Set("path", args.Path)
	}
	if args.Author != "" {
		queryParams.Set("author", args.Author)
	}
	perPage := args.PerPage
	if perPage <= 0 {
		perPage = 10
	}
	queryParams.Set("per_page", fmt.Sprintf("%d", perPage))

	endpoint := fmt.Sprintf("%s/repos/%s/%s/commits?%s", client.BaseURL, args.Owner, args.Repo, queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list commits: " + err.Error()), nil
	}

	var commits []struct {
		SHA    string `json:"sha"`
		Commit struct {
			Author struct {
				Name string `json:"name"`
				Date string `json:"date"`
			} `json:"author"`
			Message string `json:"message"`
		} `json:"commit"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.Unmarshal(resp, &commits); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Commits for %s/%s:\n\n", args.Owner, args.Repo))

	for _, c := range commits {
		shortSHA := c.SHA[:7]
		message := strings.Split(c.Commit.Message, "\n")[0] // First line only
		output.WriteString(fmt.Sprintf("%s %s\n", shortSHA, truncate(message, 70)))
		output.WriteString(fmt.Sprintf("       By %s on %s\n\n", c.Commit.Author.Name, c.Commit.Author.Date))
	}

	return successResult(output.String()), nil
}

// Helper functions

func (c *GitHubClient) makeRequest(method, endpoint string, body io.Reader) ([]byte, error) {
	return c.makeRequestWithAccept(method, endpoint, body, "application/vnd.github.v3+json")
}

func (c *GitHubClient) makeRequestWithAccept(method, endpoint string, body io.Reader, accept string) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}

	if c.UseProxy && c.ProxyAuth != "" {
		// When using mcper proxy, pass API key for auth - proxy injects OAuth token
		req.Header.Set("Authorization", "Bearer "+c.ProxyAuth)
	} else if c.Token != "" {
		// Direct API call with local GitHub token
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "mcper-github-mcp/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func decodeBase64(s string) (string, error) {
	// GitHub returns base64 with newlines
	s = strings.ReplaceAll(s, "\n", "")
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
