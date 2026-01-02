//go:build wasip1

package main

import (
	"context"
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
	azureDevOpsAPIVersion = "7.0"
)

// AzureDevOpsClient handles API communication
type AzureDevOpsClient struct {
	PAT          string
	Organization string
	HTTPClient   *http.Client
}

// NewAzureDevOpsClient creates a new Azure DevOps API client using environment variables
func NewAzureDevOpsClient() *AzureDevOpsClient {
	return &AzureDevOpsClient{
		PAT:          os.Getenv("AZURE_DEVOPS_PAT"),
		Organization: os.Getenv("AZURE_DEVOPS_ORG"),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func main() {
	server := mcp.NewServer("Azure DevOps MCP Server", "1.0.0", nil)

	// Project tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_projects",
		Description: "List all projects in the Azure DevOps organization",
	}, listProjectsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_project",
		Description: "Get details about a specific project",
	}, getProjectHandler)

	// Repository tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_repos",
		Description: "List repositories in a project",
	}, listReposHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_repo",
		Description: "Get details about a specific repository",
	}, getRepoHandler)

	// Work Item tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_work_items",
		Description: "Query work items using WIQL (Work Item Query Language)",
	}, listWorkItemsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_work_item",
		Description: "Get details about a specific work item",
	}, getWorkItemHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_create_work_item",
		Description: "Create a new work item (Bug, Task, User Story, etc.)",
	}, createWorkItemHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_update_work_item",
		Description: "Update an existing work item",
	}, updateWorkItemHandler)

	// Pull Request tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_pull_requests",
		Description: "List pull requests in a repository",
	}, listPullRequestsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_pull_request",
		Description: "Get details about a specific pull request",
	}, getPullRequestHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_create_pull_request",
		Description: "Create a new pull request",
	}, createPullRequestHandler)

	// Pipeline tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_pipelines",
		Description: "List pipelines in a project",
	}, listPipelinesHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_pipeline",
		Description: "Get details about a specific pipeline",
	}, getPipelineHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_pipeline_runs",
		Description: "List runs for a pipeline",
	}, listPipelineRunsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_run_pipeline",
		Description: "Trigger a pipeline run",
	}, runPipelineHandler)

	// Build tools
	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_list_builds",
		Description: "List builds in a project",
	}, listBuildsHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "azdo_get_build",
		Description: "Get details about a specific build",
	}, getBuildHandler)

	log.Println("Starting Azure DevOps MCP Server...")
	ctx := context.Background()
	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		log.Fatalf("Failed to run MCP server: %v", err)
	}
}

// Parameter types

type EmptyParams struct{}

type ProjectParams struct {
	Project string `json:"project"`
}

type ListReposParams struct {
	Project string `json:"project"`
}

type GetRepoParams struct {
	Project    string `json:"project"`
	Repository string `json:"repository"`
}

type ListWorkItemsParams struct {
	Project string `json:"project"`
	Query   string `json:"query,omitempty"` // WIQL query
	Type    string `json:"type,omitempty"`  // Bug, Task, User Story, etc.
	State   string `json:"state,omitempty"` // New, Active, Resolved, Closed
	Top     int    `json:"top,omitempty"`
}

type GetWorkItemParams struct {
	ID int `json:"id"`
}

type CreateWorkItemParams struct {
	Project     string `json:"project"`
	Type        string `json:"type"`                   // Bug, Task, User Story, Feature, Epic
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`
	State       string `json:"state,omitempty"`
	Priority    int    `json:"priority,omitempty"` // 1-4
	Tags        string `json:"tags,omitempty"`     // Comma-separated
}

type UpdateWorkItemParams struct {
	ID          int    `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	AssignedTo  string `json:"assigned_to,omitempty"`
	State       string `json:"state,omitempty"`
	Priority    int    `json:"priority,omitempty"`
	Tags        string `json:"tags,omitempty"`
}

type ListPullRequestsParams struct {
	Project    string `json:"project"`
	Repository string `json:"repository"`
	Status     string `json:"status,omitempty"` // active, completed, abandoned, all
	Top        int    `json:"top,omitempty"`
}

type GetPullRequestParams struct {
	Project       string `json:"project"`
	Repository    string `json:"repository"`
	PullRequestID int    `json:"pull_request_id"`
}

type CreatePullRequestParams struct {
	Project      string `json:"project"`
	Repository   string `json:"repository"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
}

type ListPipelinesParams struct {
	Project string `json:"project"`
	Top     int    `json:"top,omitempty"`
}

type GetPipelineParams struct {
	Project    string `json:"project"`
	PipelineID int    `json:"pipeline_id"`
}

type ListPipelineRunsParams struct {
	Project    string `json:"project"`
	PipelineID int    `json:"pipeline_id"`
	Top        int    `json:"top,omitempty"`
}

type RunPipelineParams struct {
	Project    string `json:"project"`
	PipelineID int    `json:"pipeline_id"`
	Branch     string `json:"branch,omitempty"` // defaults to default branch
}

type ListBuildsParams struct {
	Project string `json:"project"`
	Top     int    `json:"top,omitempty"`
	Status  string `json:"status,omitempty"` // inProgress, completed, cancelling, postponed, notStarted, all
}

type GetBuildParams struct {
	Project string `json:"project"`
	BuildID int    `json:"build_id"`
}

// Handlers

func listProjectsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[EmptyParams]) (*mcp.CallToolResultFor[any], error) {
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	endpoint := client.baseURL() + "/_apis/projects?api-version=" + azureDevOpsAPIVersion

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list projects: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			State       string `json:"state"`
			URL         string `json:"url"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d projects:\n\n", result.Count))

	for i, project := range result.Value {
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, project.Name))
		if project.Description != "" {
			output.WriteString(fmt.Sprintf("   %s\n", truncate(project.Description, 80)))
		}
		output.WriteString(fmt.Sprintf("   State: %s\n\n", project.State))
	}

	return successResult(output.String()), nil
}

func getProjectHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ProjectParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" {
		return errorResult("project is required"), nil
	}

	endpoint := fmt.Sprintf("%s/_apis/projects/%s?api-version=%s", client.baseURL(), url.PathEscape(args.Project), azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get project: " + err.Error()), nil
	}

	var project struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		State       string `json:"state"`
		Visibility  string `json:"visibility"`
		URL         string `json:"url"`
	}

	if err := json.Unmarshal(resp, &project); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Project: %s\n\n", project.Name))
	output.WriteString(fmt.Sprintf("ID: %s\n", project.ID))
	if project.Description != "" {
		output.WriteString(fmt.Sprintf("Description: %s\n", project.Description))
	}
	output.WriteString(fmt.Sprintf("State: %s\n", project.State))
	output.WriteString(fmt.Sprintf("Visibility: %s\n", project.Visibility))

	return successResult(output.String()), nil
}

func listReposHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListReposParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" {
		return errorResult("project is required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list repos: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			DefaultBranch string `json:"defaultBranch"`
			Size          int64  `json:"size"`
			RemoteURL     string `json:"remoteUrl"`
			WebURL        string `json:"webUrl"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d repositories in %s:\n\n", result.Count, args.Project))

	for i, repo := range result.Value {
		output.WriteString(fmt.Sprintf("%d. %s\n", i+1, repo.Name))
		if repo.DefaultBranch != "" {
			output.WriteString(fmt.Sprintf("   Default Branch: %s\n", strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")))
		}
		output.WriteString(fmt.Sprintf("   Size: %s\n", formatBytes(repo.Size)))
		output.WriteString(fmt.Sprintf("   %s\n\n", repo.WebURL))
	}

	return successResult(output.String()), nil
}

func getRepoHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetRepoParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.Repository == "" {
		return errorResult("project and repository are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), url.PathEscape(args.Repository), azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get repo: " + err.Error()), nil
	}

	var repo struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		DefaultBranch string `json:"defaultBranch"`
		Size          int64  `json:"size"`
		RemoteURL     string `json:"remoteUrl"`
		WebURL        string `json:"webUrl"`
		Project       struct {
			Name string `json:"name"`
		} `json:"project"`
	}

	if err := json.Unmarshal(resp, &repo); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Repository: %s\n\n", repo.Name))
	output.WriteString(fmt.Sprintf("ID: %s\n", repo.ID))
	output.WriteString(fmt.Sprintf("Project: %s\n", repo.Project.Name))
	if repo.DefaultBranch != "" {
		output.WriteString(fmt.Sprintf("Default Branch: %s\n", strings.TrimPrefix(repo.DefaultBranch, "refs/heads/")))
	}
	output.WriteString(fmt.Sprintf("Size: %s\n", formatBytes(repo.Size)))
	output.WriteString(fmt.Sprintf("Clone URL: %s\n", repo.RemoteURL))
	output.WriteString(fmt.Sprintf("Web URL: %s\n", repo.WebURL))

	return successResult(output.String()), nil
}

func listWorkItemsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListWorkItemsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" {
		return errorResult("project is required"), nil
	}

	// Build WIQL query
	var query string
	if args.Query != "" {
		query = args.Query
	} else {
		query = fmt.Sprintf("SELECT [System.Id], [System.Title], [System.State], [System.WorkItemType], [System.AssignedTo] FROM WorkItems WHERE [System.TeamProject] = '%s'", args.Project)
		if args.Type != "" {
			query += fmt.Sprintf(" AND [System.WorkItemType] = '%s'", args.Type)
		}
		if args.State != "" {
			query += fmt.Sprintf(" AND [System.State] = '%s'", args.State)
		}
		query += " ORDER BY [System.ChangedDate] DESC"
	}

	top := args.Top
	if top <= 0 {
		top = 20
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/wit/wiql?api-version=%s&$top=%d",
		client.baseURL(), url.PathEscape(args.Project), azureDevOpsAPIVersion, top)

	payload := map[string]string{"query": query}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := client.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return errorResult("Failed to query work items: " + err.Error()), nil
	}

	var wiqlResult struct {
		WorkItems []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"workItems"`
	}

	if err := json.Unmarshal(resp, &wiqlResult); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	if len(wiqlResult.WorkItems) == 0 {
		return successResult("No work items found."), nil
	}

	// Get work item details
	var ids []string
	for _, wi := range wiqlResult.WorkItems {
		ids = append(ids, fmt.Sprintf("%d", wi.ID))
	}

	detailsEndpoint := fmt.Sprintf("%s/_apis/wit/workitems?ids=%s&api-version=%s",
		client.baseURL(), strings.Join(ids, ","), azureDevOpsAPIVersion)

	detailsResp, err := client.makeRequest("GET", detailsEndpoint, nil)
	if err != nil {
		return errorResult("Failed to get work item details: " + err.Error()), nil
	}

	var detailsResult struct {
		Count int `json:"count"`
		Value []struct {
			ID     int `json:"id"`
			Fields struct {
				Title        string `json:"System.Title"`
				State        string `json:"System.State"`
				WorkItemType string `json:"System.WorkItemType"`
				AssignedTo   struct {
					DisplayName string `json:"displayName"`
				} `json:"System.AssignedTo"`
				Priority int `json:"Microsoft.VSTS.Common.Priority"`
			} `json:"fields"`
			URL string `json:"url"`
		} `json:"value"`
	}

	if err := json.Unmarshal(detailsResp, &detailsResult); err != nil {
		return errorResult("Failed to parse work items: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d work items:\n\n", detailsResult.Count))

	for _, wi := range detailsResult.Value {
		stateIcon := getStateIcon(wi.Fields.State)
		output.WriteString(fmt.Sprintf("%s #%d [%s]: %s\n", stateIcon, wi.ID, wi.Fields.WorkItemType, wi.Fields.Title))
		if wi.Fields.AssignedTo.DisplayName != "" {
			output.WriteString(fmt.Sprintf("   Assigned to: %s\n", wi.Fields.AssignedTo.DisplayName))
		}
		if wi.Fields.Priority > 0 {
			output.WriteString(fmt.Sprintf("   Priority: %d\n", wi.Fields.Priority))
		}
		output.WriteString("\n")
	}

	return successResult(output.String()), nil
}

func getWorkItemHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetWorkItemParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.ID == 0 {
		return errorResult("id is required"), nil
	}

	endpoint := fmt.Sprintf("%s/_apis/wit/workitems/%d?api-version=%s&$expand=all",
		client.baseURL(), args.ID, azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get work item: " + err.Error()), nil
	}

	var wi struct {
		ID     int `json:"id"`
		Rev    int `json:"rev"`
		Fields struct {
			Title        string `json:"System.Title"`
			State        string `json:"System.State"`
			WorkItemType string `json:"System.WorkItemType"`
			Description  string `json:"System.Description"`
			AssignedTo   struct {
				DisplayName string `json:"displayName"`
			} `json:"System.AssignedTo"`
			CreatedBy struct {
				DisplayName string `json:"displayName"`
			} `json:"System.CreatedBy"`
			CreatedDate string `json:"System.CreatedDate"`
			ChangedDate string `json:"System.ChangedDate"`
			Priority    int    `json:"Microsoft.VSTS.Common.Priority"`
			Tags        string `json:"System.Tags"`
			AreaPath    string `json:"System.AreaPath"`
		} `json:"fields"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(resp, &wi); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	stateIcon := getStateIcon(wi.Fields.State)
	output.WriteString(fmt.Sprintf("%s Work Item #%d: %s\n\n", stateIcon, wi.ID, wi.Fields.Title))
	output.WriteString(fmt.Sprintf("Type: %s\n", wi.Fields.WorkItemType))
	output.WriteString(fmt.Sprintf("State: %s\n", wi.Fields.State))
	if wi.Fields.AssignedTo.DisplayName != "" {
		output.WriteString(fmt.Sprintf("Assigned To: %s\n", wi.Fields.AssignedTo.DisplayName))
	}
	if wi.Fields.Priority > 0 {
		output.WriteString(fmt.Sprintf("Priority: %d\n", wi.Fields.Priority))
	}
	output.WriteString(fmt.Sprintf("Area: %s\n", wi.Fields.AreaPath))
	if wi.Fields.Tags != "" {
		output.WriteString(fmt.Sprintf("Tags: %s\n", wi.Fields.Tags))
	}
	output.WriteString(fmt.Sprintf("\nCreated By: %s on %s\n", wi.Fields.CreatedBy.DisplayName, wi.Fields.CreatedDate))
	output.WriteString(fmt.Sprintf("Last Changed: %s\n", wi.Fields.ChangedDate))

	if wi.Fields.Description != "" {
		output.WriteString("\n--- Description ---\n")
		output.WriteString(stripHTML(wi.Fields.Description))
	}

	return successResult(output.String()), nil
}

func createWorkItemHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CreateWorkItemParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.Type == "" || args.Title == "" {
		return errorResult("project, type, and title are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/wit/workitems/$%s?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), url.PathEscape(args.Type), azureDevOpsAPIVersion)

	// Build JSON Patch document
	var patches []map[string]interface{}

	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/fields/System.Title",
		"value": args.Title,
	})

	if args.Description != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.Description",
			"value": args.Description,
		})
	}

	if args.AssignedTo != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.AssignedTo",
			"value": args.AssignedTo,
		})
	}

	if args.State != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.State",
			"value": args.State,
		})
	}

	if args.Priority > 0 {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/Microsoft.VSTS.Common.Priority",
			"value": args.Priority,
		})
	}

	if args.Tags != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.Tags",
			"value": args.Tags,
		})
	}

	payloadBytes, _ := json.Marshal(patches)

	resp, err := client.makeRequestWithContentType("POST", endpoint, strings.NewReader(string(payloadBytes)), "application/json-patch+json")
	if err != nil {
		return errorResult("Failed to create work item: " + err.Error()), nil
	}

	var wi struct {
		ID     int `json:"id"`
		Fields struct {
			Title string `json:"System.Title"`
		} `json:"fields"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(resp, &wi); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Work item #%d created successfully!\nTitle: %s", wi.ID, wi.Fields.Title)), nil
}

func updateWorkItemHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[UpdateWorkItemParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.ID == 0 {
		return errorResult("id is required"), nil
	}

	endpoint := fmt.Sprintf("%s/_apis/wit/workitems/%d?api-version=%s",
		client.baseURL(), args.ID, azureDevOpsAPIVersion)

	var patches []map[string]interface{}

	if args.Title != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.Title",
			"value": args.Title,
		})
	}

	if args.Description != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.Description",
			"value": args.Description,
		})
	}

	if args.AssignedTo != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.AssignedTo",
			"value": args.AssignedTo,
		})
	}

	if args.State != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.State",
			"value": args.State,
		})
	}

	if args.Priority > 0 {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/Microsoft.VSTS.Common.Priority",
			"value": args.Priority,
		})
	}

	if args.Tags != "" {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/fields/System.Tags",
			"value": args.Tags,
		})
	}

	if len(patches) == 0 {
		return errorResult("At least one field to update is required"), nil
	}

	payloadBytes, _ := json.Marshal(patches)

	resp, err := client.makeRequestWithContentType("PATCH", endpoint, strings.NewReader(string(payloadBytes)), "application/json-patch+json")
	if err != nil {
		return errorResult("Failed to update work item: " + err.Error()), nil
	}

	var wi struct {
		ID  int `json:"id"`
		Rev int `json:"rev"`
	}

	if err := json.Unmarshal(resp, &wi); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Work item #%d updated successfully (revision %d)", wi.ID, wi.Rev)), nil
}

func listPullRequestsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListPullRequestsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.Repository == "" {
		return errorResult("project and repository are required"), nil
	}

	top := args.Top
	if top <= 0 {
		top = 20
	}

	queryParams := url.Values{}
	queryParams.Set("api-version", azureDevOpsAPIVersion)
	queryParams.Set("$top", fmt.Sprintf("%d", top))
	if args.Status != "" {
		queryParams.Set("searchCriteria.status", args.Status)
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullrequests?%s",
		client.baseURL(), url.PathEscape(args.Project), url.PathEscape(args.Repository), queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list pull requests: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			PullRequestID int    `json:"pullRequestId"`
			Title         string `json:"title"`
			Status        string `json:"status"`
			CreatedBy     struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
			SourceRefName string `json:"sourceRefName"`
			TargetRefName string `json:"targetRefName"`
			CreationDate  string `json:"creationDate"`
			IsDraft       bool   `json:"isDraft"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Pull Requests in %s/%s:\n\n", args.Project, args.Repository))

	for _, pr := range result.Value {
		statusIcon := "🟢"
		switch pr.Status {
		case "completed":
			statusIcon = "🟣"
		case "abandoned":
			statusIcon = "🔴"
		}
		if pr.IsDraft {
			statusIcon = "📝"
		}

		sourceBranch := strings.TrimPrefix(pr.SourceRefName, "refs/heads/")
		targetBranch := strings.TrimPrefix(pr.TargetRefName, "refs/heads/")

		output.WriteString(fmt.Sprintf("%s #%d: %s\n", statusIcon, pr.PullRequestID, pr.Title))
		output.WriteString(fmt.Sprintf("   %s → %s | By %s\n", sourceBranch, targetBranch, pr.CreatedBy.DisplayName))
		output.WriteString(fmt.Sprintf("   Created: %s\n\n", pr.CreationDate))
	}

	return successResult(output.String()), nil
}

func getPullRequestHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetPullRequestParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.Repository == "" || args.PullRequestID == 0 {
		return errorResult("project, repository, and pull_request_id are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), url.PathEscape(args.Repository), args.PullRequestID, azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get pull request: " + err.Error()), nil
	}

	var pr struct {
		PullRequestID int    `json:"pullRequestId"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		Status        string `json:"status"`
		IsDraft       bool   `json:"isDraft"`
		CreatedBy     struct {
			DisplayName string `json:"displayName"`
		} `json:"createdBy"`
		SourceRefName string `json:"sourceRefName"`
		TargetRefName string `json:"targetRefName"`
		CreationDate  string `json:"creationDate"`
		ClosedDate    string `json:"closedDate"`
		MergeStatus   string `json:"mergeStatus"`
		Reviewers     []struct {
			DisplayName string `json:"displayName"`
			Vote        int    `json:"vote"`
		} `json:"reviewers"`
	}

	if err := json.Unmarshal(resp, &pr); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder

	status := pr.Status
	if pr.IsDraft {
		status = "draft"
	}

	sourceBranch := strings.TrimPrefix(pr.SourceRefName, "refs/heads/")
	targetBranch := strings.TrimPrefix(pr.TargetRefName, "refs/heads/")

	output.WriteString(fmt.Sprintf("PR #%d: %s\n\n", pr.PullRequestID, pr.Title))
	output.WriteString(fmt.Sprintf("Status: %s\n", status))
	output.WriteString(fmt.Sprintf("Author: %s\n", pr.CreatedBy.DisplayName))
	output.WriteString(fmt.Sprintf("Branch: %s → %s\n", sourceBranch, targetBranch))
	output.WriteString(fmt.Sprintf("Merge Status: %s\n", pr.MergeStatus))
	output.WriteString(fmt.Sprintf("Created: %s\n", pr.CreationDate))
	if pr.ClosedDate != "" {
		output.WriteString(fmt.Sprintf("Closed: %s\n", pr.ClosedDate))
	}

	if len(pr.Reviewers) > 0 {
		output.WriteString("\nReviewers:\n")
		for _, r := range pr.Reviewers {
			voteIcon := "⏳"
			switch r.Vote {
			case 10:
				voteIcon = "✅"
			case 5:
				voteIcon = "👍"
			case -5:
				voteIcon = "⏸️"
			case -10:
				voteIcon = "❌"
			}
			output.WriteString(fmt.Sprintf("  %s %s\n", voteIcon, r.DisplayName))
		}
	}

	if pr.Description != "" {
		output.WriteString("\n--- Description ---\n")
		output.WriteString(pr.Description)
	}

	return successResult(output.String()), nil
}

func createPullRequestHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CreatePullRequestParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.Repository == "" || args.SourceBranch == "" || args.TargetBranch == "" || args.Title == "" {
		return errorResult("project, repository, source_branch, target_branch, and title are required"), nil
	}

	// Ensure branches have refs/heads/ prefix
	sourceBranch := args.SourceBranch
	if !strings.HasPrefix(sourceBranch, "refs/heads/") {
		sourceBranch = "refs/heads/" + sourceBranch
	}
	targetBranch := args.TargetBranch
	if !strings.HasPrefix(targetBranch, "refs/heads/") {
		targetBranch = "refs/heads/" + targetBranch
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullrequests?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), url.PathEscape(args.Repository), azureDevOpsAPIVersion)

	payload := map[string]interface{}{
		"sourceRefName": sourceBranch,
		"targetRefName": targetBranch,
		"title":         args.Title,
	}
	if args.Description != "" {
		payload["description"] = args.Description
	}

	payloadBytes, _ := json.Marshal(payload)

	resp, err := client.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return errorResult("Failed to create pull request: " + err.Error()), nil
	}

	var pr struct {
		PullRequestID int    `json:"pullRequestId"`
		Title         string `json:"title"`
	}

	if err := json.Unmarshal(resp, &pr); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Pull request #%d created successfully!\nTitle: %s", pr.PullRequestID, pr.Title)), nil
}

func listPipelinesHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListPipelinesParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" {
		return errorResult("project is required"), nil
	}

	top := args.Top
	if top <= 0 {
		top = 20
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines?api-version=%s&$top=%d",
		client.baseURL(), url.PathEscape(args.Project), azureDevOpsAPIVersion, top)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list pipelines: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Folder string `json:"folder"`
			URL    string `json:"url"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d pipelines in %s:\n\n", result.Count, args.Project))

	for i, pipeline := range result.Value {
		output.WriteString(fmt.Sprintf("%d. [%d] %s\n", i+1, pipeline.ID, pipeline.Name))
		if pipeline.Folder != "" && pipeline.Folder != "\\" {
			output.WriteString(fmt.Sprintf("   Folder: %s\n", pipeline.Folder))
		}
		output.WriteString("\n")
	}

	return successResult(output.String()), nil
}

func getPipelineHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetPipelineParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.PipelineID == 0 {
		return errorResult("project and pipeline_id are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%d?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), args.PipelineID, azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get pipeline: " + err.Error()), nil
	}

	var pipeline struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		Folder        string `json:"folder"`
		Revision      int    `json:"revision"`
		Configuration struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"configuration"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(resp, &pipeline); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Pipeline: %s\n\n", pipeline.Name))
	output.WriteString(fmt.Sprintf("ID: %d\n", pipeline.ID))
	if pipeline.Folder != "" && pipeline.Folder != "\\" {
		output.WriteString(fmt.Sprintf("Folder: %s\n", pipeline.Folder))
	}
	output.WriteString(fmt.Sprintf("Revision: %d\n", pipeline.Revision))
	if pipeline.Configuration.Type != "" {
		output.WriteString(fmt.Sprintf("Type: %s\n", pipeline.Configuration.Type))
	}
	if pipeline.Configuration.Path != "" {
		output.WriteString(fmt.Sprintf("Path: %s\n", pipeline.Configuration.Path))
	}

	return successResult(output.String()), nil
}

func listPipelineRunsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListPipelineRunsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.PipelineID == 0 {
		return errorResult("project and pipeline_id are required"), nil
	}

	top := args.Top
	if top <= 0 {
		top = 10
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%d/runs?api-version=%s&$top=%d",
		client.baseURL(), url.PathEscape(args.Project), args.PipelineID, azureDevOpsAPIVersion, top)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list pipeline runs: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			State        string `json:"state"`
			Result       string `json:"result"`
			CreatedDate  string `json:"createdDate"`
			FinishedDate string `json:"finishedDate"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Pipeline runs for pipeline %d:\n\n", args.PipelineID))

	for _, run := range result.Value {
		resultIcon := "⏳"
		switch run.Result {
		case "succeeded":
			resultIcon = "✅"
		case "failed":
			resultIcon = "❌"
		case "canceled":
			resultIcon = "⏹️"
		}
		if run.State == "inProgress" {
			resultIcon = "🔄"
		}

		output.WriteString(fmt.Sprintf("%s Run #%d: %s\n", resultIcon, run.ID, run.Name))
		output.WriteString(fmt.Sprintf("   State: %s", run.State))
		if run.Result != "" {
			output.WriteString(fmt.Sprintf(" | Result: %s", run.Result))
		}
		output.WriteString("\n")
		output.WriteString(fmt.Sprintf("   Started: %s\n\n", run.CreatedDate))
	}

	return successResult(output.String()), nil
}

func runPipelineHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[RunPipelineParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.PipelineID == 0 {
		return errorResult("project and pipeline_id are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/pipelines/%d/runs?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), args.PipelineID, azureDevOpsAPIVersion)

	payload := map[string]interface{}{}
	if args.Branch != "" {
		branch := args.Branch
		if !strings.HasPrefix(branch, "refs/heads/") {
			branch = "refs/heads/" + branch
		}
		payload["resources"] = map[string]interface{}{
			"repositories": map[string]interface{}{
				"self": map[string]interface{}{
					"refName": branch,
				},
			},
		}
	}

	payloadBytes, _ := json.Marshal(payload)

	resp, err := client.makeRequest("POST", endpoint, strings.NewReader(string(payloadBytes)))
	if err != nil {
		return errorResult("Failed to run pipeline: " + err.Error()), nil
	}

	var run struct {
		ID    int    `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
		URL   string `json:"url"`
	}

	if err := json.Unmarshal(resp, &run); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	return successResult(fmt.Sprintf("Pipeline run #%d started!\nName: %s\nState: %s", run.ID, run.Name, run.State)), nil
}

func listBuildsHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListBuildsParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" {
		return errorResult("project is required"), nil
	}

	top := args.Top
	if top <= 0 {
		top = 20
	}

	queryParams := url.Values{}
	queryParams.Set("api-version", azureDevOpsAPIVersion)
	queryParams.Set("$top", fmt.Sprintf("%d", top))
	if args.Status != "" {
		queryParams.Set("statusFilter", args.Status)
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/build/builds?%s",
		client.baseURL(), url.PathEscape(args.Project), queryParams.Encode())

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to list builds: " + err.Error()), nil
	}

	var result struct {
		Count int `json:"count"`
		Value []struct {
			ID         int    `json:"id"`
			BuildNumber string `json:"buildNumber"`
			Status     string `json:"status"`
			Result     string `json:"result"`
			Definition struct {
				Name string `json:"name"`
			} `json:"definition"`
			SourceBranch string `json:"sourceBranch"`
			StartTime    string `json:"startTime"`
			FinishTime   string `json:"finishTime"`
			RequestedBy  struct {
				DisplayName string `json:"displayName"`
			} `json:"requestedBy"`
		} `json:"value"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Found %d builds in %s:\n\n", result.Count, args.Project))

	for _, build := range result.Value {
		resultIcon := "⏳"
		switch build.Result {
		case "succeeded":
			resultIcon = "✅"
		case "failed":
			resultIcon = "❌"
		case "canceled":
			resultIcon = "⏹️"
		case "partiallySucceeded":
			resultIcon = "⚠️"
		}
		if build.Status == "inProgress" {
			resultIcon = "🔄"
		}

		branch := strings.TrimPrefix(build.SourceBranch, "refs/heads/")
		output.WriteString(fmt.Sprintf("%s #%d %s\n", resultIcon, build.ID, build.BuildNumber))
		output.WriteString(fmt.Sprintf("   Pipeline: %s | Branch: %s\n", build.Definition.Name, branch))
		output.WriteString(fmt.Sprintf("   By: %s | Started: %s\n\n", build.RequestedBy.DisplayName, build.StartTime))
	}

	return successResult(output.String()), nil
}

func getBuildHandler(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[GetBuildParams]) (*mcp.CallToolResultFor[any], error) {
	args := params.Arguments
	client := NewAzureDevOpsClient()

	if err := client.validate(); err != nil {
		return errorResult(err.Error()), nil
	}

	if args.Project == "" || args.BuildID == 0 {
		return errorResult("project and build_id are required"), nil
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/build/builds/%d?api-version=%s",
		client.baseURL(), url.PathEscape(args.Project), args.BuildID, azureDevOpsAPIVersion)

	resp, err := client.makeRequest("GET", endpoint, nil)
	if err != nil {
		return errorResult("Failed to get build: " + err.Error()), nil
	}

	var build struct {
		ID          int    `json:"id"`
		BuildNumber string `json:"buildNumber"`
		Status      string `json:"status"`
		Result      string `json:"result"`
		Definition  struct {
			Name string `json:"name"`
		} `json:"definition"`
		SourceBranch  string `json:"sourceBranch"`
		SourceVersion string `json:"sourceVersion"`
		StartTime     string `json:"startTime"`
		FinishTime    string `json:"finishTime"`
		RequestedBy   struct {
			DisplayName string `json:"displayName"`
		} `json:"requestedBy"`
		RequestedFor struct {
			DisplayName string `json:"displayName"`
		} `json:"requestedFor"`
		Reason string `json:"reason"`
		URL    string `json:"url"`
	}

	if err := json.Unmarshal(resp, &build); err != nil {
		return errorResult("Failed to parse response: " + err.Error()), nil
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Build #%d: %s\n\n", build.ID, build.BuildNumber))
	output.WriteString(fmt.Sprintf("Pipeline: %s\n", build.Definition.Name))
	output.WriteString(fmt.Sprintf("Status: %s\n", build.Status))
	if build.Result != "" {
		output.WriteString(fmt.Sprintf("Result: %s\n", build.Result))
	}
	output.WriteString(fmt.Sprintf("Reason: %s\n", build.Reason))

	branch := strings.TrimPrefix(build.SourceBranch, "refs/heads/")
	output.WriteString(fmt.Sprintf("\nBranch: %s\n", branch))
	if build.SourceVersion != "" {
		output.WriteString(fmt.Sprintf("Commit: %s\n", build.SourceVersion[:min(7, len(build.SourceVersion))]))
	}

	output.WriteString(fmt.Sprintf("\nRequested By: %s\n", build.RequestedBy.DisplayName))
	if build.RequestedFor.DisplayName != "" && build.RequestedFor.DisplayName != build.RequestedBy.DisplayName {
		output.WriteString(fmt.Sprintf("Requested For: %s\n", build.RequestedFor.DisplayName))
	}
	output.WriteString(fmt.Sprintf("Started: %s\n", build.StartTime))
	if build.FinishTime != "" {
		output.WriteString(fmt.Sprintf("Finished: %s\n", build.FinishTime))
	}

	return successResult(output.String()), nil
}

// Helper functions

func (c *AzureDevOpsClient) validate() error {
	if c.PAT == "" {
		return fmt.Errorf("AZURE_DEVOPS_PAT environment variable is required")
	}
	if c.Organization == "" {
		return fmt.Errorf("AZURE_DEVOPS_ORG environment variable is required")
	}
	return nil
}

func (c *AzureDevOpsClient) baseURL() string {
	return fmt.Sprintf("https://dev.azure.com/%s", c.Organization)
}

func (c *AzureDevOpsClient) makeRequest(method, endpoint string, body io.Reader) ([]byte, error) {
	return c.makeRequestWithContentType(method, endpoint, body, "application/json")
}

func (c *AzureDevOpsClient) makeRequestWithContentType(method, endpoint string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}

	// Azure DevOps uses Basic auth with PAT (username can be empty)
	auth := base64.StdEncoding.EncodeToString([]byte(":" + c.PAT))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

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

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func getStateIcon(state string) string {
	switch strings.ToLower(state) {
	case "new":
		return "🆕"
	case "active", "in progress":
		return "🔄"
	case "resolved":
		return "✅"
	case "closed", "done":
		return "🔴"
	case "removed":
		return "🗑️"
	default:
		return "📋"
	}
}

func stripHTML(s string) string {
	// Simple HTML stripping - removes tags
	result := s
	for {
		start := strings.Index(result, "<")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	return strings.TrimSpace(result)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
