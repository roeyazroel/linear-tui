package linearapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// issueNodeJSON returns a JSON object string for an issue node used in tests.
func issueNodeJSON(id, identifier, title string) string {
	return fmt.Sprintf(`{
		"id": %q,
		"identifier": %q,
		"title": %q,
		"state": {"id": "state-1", "name": "Todo"},
		"assignee": null,
		"priority": 1,
		"updatedAt": "2025-01-01T00:00:00Z",
		"createdAt": "2025-01-01T00:00:00Z",
		"description": null,
		"team": {"id": "team-1"},
		"project": null,
		"labels": {"nodes": []},
		"url": "https://linear.app/issue/%s",
		"archivedAt": null,
		"parent": null,
		"children": {"nodes": []}
	}`, id, identifier, title, identifier)
}

func issueNodeWithCycleJSON(id, identifier, title string) string {
	node := issueNodeJSON(id, identifier, title)
	return strings.Replace(node, `"children": {"nodes": []}`, `"cycle": {
			"id": "cycle-1",
			"name": "Launch",
			"number": 12,
			"startsAt": "2025-01-01T00:00:00Z",
			"endsAt": "2025-01-15T00:00:00Z",
			"isActive": true,
			"isFuture": false,
			"isPast": false,
			"isNext": false,
			"isPrevious": false
		},
		"children": {"nodes": []}`, 1)
}

func issueNodeWithPlanningFieldsJSON(id, identifier, title string) string {
	node := issueNodeWithCycleJSON(id, identifier, title)
	return strings.Replace(node, `"children": {"nodes": []}`, `"dueDate": "2026-06-15",
		"estimate": 5,
		"projectMilestone": {
			"id": "milestone-1",
			"name": "Beta",
			"targetDate": "2026-06-30",
			"status": "next",
			"project": {"id": "project-1"}
		},
		"children": {"nodes": []}`, 1)
}

// issuesPageResponse builds a GraphQL response with issue nodes and page info.
func issuesPageResponse(nodes []string, hasNextPage bool, endCursor string) string {
	return fmt.Sprintf(`{
		"data": {
			"issues": {
				"nodes": [%s],
				"pageInfo": {
					"hasNextPage": %t,
					"endCursor": %q
				}
			}
		}
	}`, strings.Join(nodes, ","), hasNextPage, endCursor)
}

func cyclesPageResponse(nodes []string, hasNextPage bool, endCursor string) string {
	return fmt.Sprintf(`{
		"data": {
			"team": {
				"cycles": {
					"nodes": [%s],
					"pageInfo": {
						"hasNextPage": %t,
						"endCursor": %q
					}
				}
			}
		}
	}`, strings.Join(nodes, ","), hasNextPage, endCursor)
}

func cycleNodeJSON(id, name string, number int, isActive bool) string {
	return fmt.Sprintf(`{
		"id": %q,
		"name": %q,
		"number": %d,
		"description": null,
		"startsAt": "2025-01-01T00:00:00Z",
		"endsAt": "2025-01-15T00:00:00Z",
		"isActive": %t,
		"isFuture": false,
		"isPast": false,
		"isNext": false,
		"isPrevious": false,
		"team": {"id": "team-1"}
	}`, id, name, number, isActive)
}

func projectMilestonesPageResponse(nodes []string, hasNextPage bool, endCursor string) string {
	return fmt.Sprintf(`{
		"data": {
			"projectMilestones": {
				"nodes": [%s],
				"pageInfo": {
					"hasNextPage": %t,
					"endCursor": %q
				}
			}
		}
	}`, strings.Join(nodes, ","), hasNextPage, endCursor)
}

func projectMilestoneNodeJSON(id, name, targetDate, status, projectID string) string {
	target := "null"
	if targetDate != "" {
		target = fmt.Sprintf("%q", targetDate)
	}
	return fmt.Sprintf(`{
		"id": %q,
		"name": %q,
		"targetDate": %s,
		"status": %q,
		"sortOrder": 10,
		"progress": 0.25,
		"project": {"id": %q}
	}`, id, name, target, status, projectID)
}

func mutationIssueResponse(root string) string {
	return fmt.Sprintf(`{
		"data": {
			"%s": {
				"success": true,
				"issue": {
					"id": "issue-1",
					"identifier": "ABC-1",
					"title": "Issue with cycle",
					"state": {"id": "state-1", "name": "Todo"},
					"assignee": null,
					"priority": 1,
					"updatedAt": "2025-01-01T00:00:00Z",
					"createdAt": "2025-01-01T00:00:00Z",
					"description": null,
					"team": {"id": "team-1"},
					"project": null,
					"cycle": {
						"id": "cycle-1",
						"name": "Launch",
						"number": 12,
						"startsAt": "2025-01-01T00:00:00Z",
						"endsAt": "2025-01-15T00:00:00Z",
						"isActive": true,
						"isFuture": false,
						"isPast": false,
						"isNext": false,
						"isPrevious": false
					},
					"labels": {"nodes": []},
					"url": "https://linear.app/issue/ABC-1"
				}
			}
		}
	}`, root)
}

func issueRelationMutationResponse(root string) string {
	return fmt.Sprintf(`{
		"data": {
			"%s": {
				"success": true,
				"issueRelation": {
					"id": "rel-1",
					"type": "blocks",
					"issue": {"id": "issue-1", "identifier": "ABC-1", "title": "Source"},
					"relatedIssue": {"id": "issue-2", "identifier": "ABC-2", "title": "Target"}
				}
			}
		}
	}`, root)
}

func TestNewClient(t *testing.T) {
	token := "test-token-123"
	client := NewClientWithToken(token)

	if client == nil {
		t.Fatal("NewClientWithToken() returned nil")
	}

	if client.token != token {
		t.Errorf("NewClientWithToken() token = %q, want %q", client.token, token)
	}

	if client.endpoint != DefaultEndpoint {
		t.Errorf("NewClientWithToken() endpoint = %q, want %q", client.endpoint, DefaultEndpoint)
	}

	if client.httpClient == nil {
		t.Error("NewClientWithToken() httpClient should not be nil")
	}

	if client.client == nil {
		t.Error("NewClientWithToken() client should not be nil")
	}
}

func TestNewClient_CustomConfig(t *testing.T) {
	customEndpoint := "http://localhost:8080/graphql"
	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: customEndpoint,
	})

	if client.endpoint != customEndpoint {
		t.Errorf("NewClient() endpoint = %q, want %q", client.endpoint, customEndpoint)
	}

	if client.Endpoint() != customEndpoint {
		t.Errorf("Endpoint() = %q, want %q", client.Endpoint(), customEndpoint)
	}
}

func TestNewClient_CustomHTTPClient(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": {"teams": {"nodes": []}}}`))
	}))
	defer server.Close()

	customHTTPClient := &http.Client{}
	client := NewClient(ClientConfig{
		Token:      "my-token",
		Endpoint:   server.URL,
		HTTPClient: customHTTPClient,
	})

	ctx := context.Background()
	_, err := client.ListTeams(ctx)
	// May fail due to GraphQL response format, but we can verify auth header was set
	_ = err

	if authHeader != "my-token" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "my-token")
	}
}

func TestAuthTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expected := "test-token"
		if auth != expected {
			t.Errorf("Authorization header = %q, want %q", auth, expected)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": {"issues": {"nodes": []}}}`))
	}))
	defer server.Close()

	transport := &authTransport{
		Token: "test-token",
		Base:  http.DefaultTransport,
	}

	req, err := http.NewRequest("POST", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func TestAuthTransportBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer oauth-token" {
			t.Errorf("Authorization header = %q, want Bearer oauth-token", auth)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &authTransport{
		Token:     "oauth-token",
		UseBearer: true,
		Base:      http.DefaultTransport,
	}
	req, err := http.NewRequest("POST", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestAuthTransportUnauthorizedRefreshRetry(t *testing.T) {
	var calls int
	var headers []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		headers = append(headers, r.Header.Get("Authorization"))
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"teams":{"nodes":[]}}}`))
	}))
	defer server.Close()

	refreshCalls := 0
	client := NewClient(ClientConfig{
		Token:     "old-token",
		UseBearer: true,
		Endpoint:  server.URL,
		OnUnauthorized: func(ctx context.Context) (string, error) {
			refreshCalls++
			return "new-token", nil
		},
	})

	_, err := client.ListTeams(context.Background())
	_ = err
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d", refreshCalls)
	}
	if calls < 2 {
		t.Fatalf("expected retry after 401, calls=%d headers=%v", calls, headers)
	}
	if len(headers) >= 2 && headers[1] != "Bearer new-token" {
		t.Fatalf("retry header = %q, headers=%v", headers[1], headers)
	}
}

func TestFetchIssues_RequestFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Authorization header format
		auth := r.Header.Get("Authorization")
		expected := "test-token"
		if auth != expected {
			t.Errorf("Authorization header = %q, want %q", auth, expected)
		}

		// Check Content-Type
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", contentType)
		}

		// Parse request body to verify GraphQL query structure
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Verify request has query field
		if _, ok := reqBody["query"]; !ok {
			t.Error("Request body missing 'query' field")
		}

		// Verify request has variables field
		if _, ok := reqBody["variables"]; !ok {
			t.Error("Request body missing 'variables' field")
		}

		// Send a valid GraphQL response
		response := `{
			"data": {
				"issues": {
					"nodes": [],
					"pageInfo": {
						"hasNextPage": false,
						"endCursor": ""
					}
				}
			}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	// Create client with test server URL using new config
	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	ctx := context.Background()
	_, err := client.FetchIssues(ctx, FetchIssuesParams{First: 10})
	if err != nil {
		// We expect this might fail due to GraphQL parsing, but we've verified
		// the request format is correct
		t.Logf("FetchIssues() error (expected for test): %v", err)
	}
}

// TestFetchIssues_PaginatesAllPages verifies that all pages are fetched and concatenated.
func TestFetchIssues_PaginatesAllPages(t *testing.T) {
	var afterValues []interface{}
	requestCount := 0

	pageOne := issuesPageResponse([]string{
		issueNodeJSON("issue-1", "ABC-1", "First issue"),
	}, true, "cursor-1")
	pageTwo := issuesPageResponse([]string{
		issueNodeJSON("issue-2", "ABC-2", "Second issue"),
		issueNodeJSON("issue-3", "ABC-3", "Third issue"),
	}, false, "cursor-2")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		afterValues = append(afterValues, variables["after"])

		w.Header().Set("Content-Type", "application/json")
		if requestCount == 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(pageOne))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(pageTwo))
		}
		requestCount++
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	issues, err := client.FetchIssues(context.Background(), FetchIssuesParams{First: 2})
	if err != nil {
		t.Fatalf("FetchIssues() error: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("Expected 2 requests, got %d", requestCount)
	}
	if len(afterValues) != 2 {
		t.Fatalf("Expected 2 after values, got %d", len(afterValues))
	}
	if afterValues[0] != nil {
		t.Errorf("First request after = %#v, want nil", afterValues[0])
	}
	if afterValues[1] != "cursor-1" {
		t.Errorf("Second request after = %#v, want %q", afterValues[1], "cursor-1")
	}

	if len(issues) != 3 {
		t.Fatalf("Fetched issues = %d, want 3", len(issues))
	}
	if issues[0].ID != "issue-1" || issues[1].ID != "issue-2" || issues[2].ID != "issue-3" {
		t.Errorf("Fetched issues order = [%s, %s, %s], want issue-1, issue-2, issue-3",
			issues[0].ID, issues[1].ID, issues[2].ID)
	}
}

func TestListCycles_PaginatesAndParses(t *testing.T) {
	var afterValues []interface{}
	requestCount := 0

	pageOne := cyclesPageResponse([]string{
		cycleNodeJSON("cycle-1", "Launch", 12, true),
	}, true, "cycle-cursor-1")
	pageTwo := cyclesPageResponse([]string{
		cycleNodeJSON("cycle-2", "", 13, false),
	}, false, "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		query, _ := reqBody["query"].(string)
		if !strings.Contains(query, "cycles") {
			t.Fatalf("query does not request cycles: %s", query)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		if variables["teamId"] != "team-1" {
			t.Fatalf("teamId = %#v, want team-1", variables["teamId"])
		}
		afterValues = append(afterValues, variables["after"])

		w.Header().Set("Content-Type", "application/json")
		if requestCount == 0 {
			_, _ = w.Write([]byte(pageOne))
		} else {
			_, _ = w.Write([]byte(pageTwo))
		}
		requestCount++
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	cycles, err := client.ListCycles(context.Background(), "team-1")
	if err != nil {
		t.Fatalf("ListCycles() error: %v", err)
	}

	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}
	if afterValues[0] != nil || afterValues[1] != "cycle-cursor-1" {
		t.Fatalf("afterValues = %#v, want [nil cycle-cursor-1]", afterValues)
	}
	if len(cycles) != 2 {
		t.Fatalf("cycles length = %d, want 2", len(cycles))
	}
	if cycles[0].ID != "cycle-1" || cycles[0].Name != "Launch" || cycles[0].Number != 12 || !cycles[0].IsActive {
		t.Fatalf("cycles[0] = %+v, want active Launch cycle 12", cycles[0])
	}
	if cycles[1].DisplayName() != "Cycle 13" {
		t.Fatalf("cycles[1].DisplayName() = %q, want Cycle 13", cycles[1].DisplayName())
	}
}

// TestFetchIssuesPage_Defaults verifies page defaults and pagination metadata.
func TestFetchIssuesPage_Defaults(t *testing.T) {
	var firstValue interface{}
	response := issuesPageResponse([]string{
		issueNodeJSON("issue-1", "ABC-1", "First issue"),
	}, true, "cursor-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		firstValue = variables["first"]

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	page, err := client.FetchIssuesPage(context.Background(), FetchIssuesParams{}, nil)
	if err != nil {
		t.Fatalf("FetchIssuesPage() error: %v", err)
	}

	if firstValue != float64(50) {
		t.Errorf("First default = %#v, want 50", firstValue)
	}
	if !page.HasNext {
		t.Error("HasNext = false, want true")
	}
	if page.EndCursor == nil || *page.EndCursor != "cursor-1" {
		t.Errorf("EndCursor = %#v, want cursor-1", page.EndCursor)
	}
	if len(page.Issues) != 1 || page.Issues[0].ID != "issue-1" {
		t.Errorf("Issues = %+v, want single issue-1", page.Issues)
	}
}

func TestFetchIssues_ParsesCycle(t *testing.T) {
	response := issuesPageResponse([]string{
		issueNodeWithCycleJSON("issue-1", "ABC-1", "Issue with cycle"),
	}, false, "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	issues, err := client.FetchIssues(context.Background(), FetchIssuesParams{First: 1})
	if err != nil {
		t.Fatalf("FetchIssues() error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues length = %d, want 1", len(issues))
	}
	if issues[0].Cycle == nil {
		t.Fatal("Cycle should be populated")
	}
	if issues[0].Cycle.ID != "cycle-1" || issues[0].Cycle.DisplayName() != "Launch" || !issues[0].Cycle.IsActive {
		t.Fatalf("Cycle = %+v, want active Launch cycle", issues[0].Cycle)
	}
}

func TestFetchIssues_ParsesPlanningFields(t *testing.T) {
	response := issuesPageResponse([]string{
		issueNodeWithPlanningFieldsJSON("issue-1", "ABC-1", "Planning issue"),
	}, false, "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	issues, err := client.FetchIssues(context.Background(), FetchIssuesParams{TeamID: "team-1"})
	if err != nil {
		t.Fatalf("FetchIssues() error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("FetchIssues() returned %d issues, want 1", len(issues))
	}
	if issues[0].DueDate == nil || *issues[0].DueDate != "2026-06-15" {
		t.Fatalf("DueDate = %#v, want 2026-06-15", issues[0].DueDate)
	}
	if issues[0].Estimate == nil || *issues[0].Estimate != 5 {
		t.Fatalf("Estimate = %#v, want 5", issues[0].Estimate)
	}
	if issues[0].ProjectMilestone == nil {
		t.Fatal("ProjectMilestone should be populated")
	}
	if issues[0].ProjectMilestone.ID != "milestone-1" || issues[0].ProjectMilestone.Name != "Beta" || issues[0].ProjectMilestone.ProjectID != "project-1" {
		t.Fatalf("ProjectMilestone = %+v, want milestone-1 Beta project-1", issues[0].ProjectMilestone)
	}
}

func TestFetchIssueByID_ParsesRelationsSubscribersAndAttachments(t *testing.T) {
	response := `{
		"data": {
			"issue": {
				"id": "issue-1",
				"identifier": "ABC-1",
				"title": "Full issue",
				"state": {"id": "state-1", "name": "Todo"},
				"assignee": null,
				"priority": 1,
				"updatedAt": "2025-01-01T00:00:00Z",
				"createdAt": "2025-01-01T00:00:00Z",
				"description": null,
				"team": {"id": "team-1"},
				"project": {"id": "project-1"},
				"cycle": null,
				"dueDate": "2026-06-15",
				"estimate": 3,
				"projectMilestone": {
					"id": "milestone-1",
					"name": "Beta",
					"targetDate": "2026-06-30",
					"status": "next",
					"project": {"id": "project-1"}
				},
				"labels": {"nodes": []},
				"url": "https://linear.app/issue/ABC-1",
				"archivedAt": null,
				"parent": null,
				"children": {"nodes": []},
				"relations": {"nodes": [{
					"id": "rel-1",
					"type": "blocks",
					"issue": {"id": "issue-1", "identifier": "ABC-1", "title": "Full issue"},
					"relatedIssue": {"id": "issue-2", "identifier": "ABC-2", "title": "Blocked target"}
				}]},
				"inverseRelations": {"nodes": [{
					"id": "rel-2",
					"type": "blocks",
					"issue": {"id": "issue-3", "identifier": "ABC-3", "title": "Blocking source"},
					"relatedIssue": {"id": "issue-1", "identifier": "ABC-1", "title": "Full issue"}
				}]},
				"subscribers": {"nodes": [{
					"id": "user-1",
					"name": "Ada",
					"displayName": "Ada Lovelace",
					"email": "ada@example.com",
					"isMe": true
				}]},
				"attachments": {"nodes": [{
					"id": "attachment-1",
					"title": "Pull request",
					"subtitle": "GitHub",
					"url": "https://github.com/acme/repo/pull/1",
					"sourceType": "github",
					"createdAt": "2025-01-02T00:00:00Z",
					"updatedAt": "2025-01-03T00:00:00Z"
				}]},
				"comments": {"nodes": []}
			}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	issue, err := client.FetchIssueByID(context.Background(), "issue-1")
	if err != nil {
		t.Fatalf("FetchIssueByID() error: %v", err)
	}
	if len(issue.Relations) != 2 {
		t.Fatalf("Relations length = %d, want 2", len(issue.Relations))
	}
	if issue.Relations[0].DisplayType() != "blocking" {
		t.Fatalf("Relations[0].DisplayType() = %q, want blocking", issue.Relations[0].DisplayType())
	}
	if issue.Relations[1].DisplayType() != "blocked by" {
		t.Fatalf("Relations[1].DisplayType() = %q, want blocked by", issue.Relations[1].DisplayType())
	}
	if len(issue.Subscribers) != 1 || !issue.Subscribers[0].IsMe {
		t.Fatalf("Subscribers = %+v, want one current user", issue.Subscribers)
	}
	if len(issue.Attachments) != 1 || issue.Attachments[0].SourceType != "github" || issue.Attachments[0].URL == "" {
		t.Fatalf("Attachments = %+v, want one GitHub attachment", issue.Attachments)
	}
}

func TestFetchIssueByID_ParsesCommentThreadsReactionsAndAllPages(t *testing.T) {
	responses := []string{
		`{"data":{"issue":{"id":"issue-1","identifier":"ABC-1","title":"Threaded","state":{"id":"state-1","name":"Todo"},"assignee":null,"priority":1,"updatedAt":"2025-01-01T00:00:00Z","createdAt":"2025-01-01T00:00:00Z","description":null,"team":{"id":"team-1"},"project":null,"labels":{"nodes":[]},"url":"https://linear.app/issue/ABC-1","archivedAt":null,"parent":null,"children":{"nodes":[]},"relations":{"nodes":[]},"inverseRelations":{"nodes":[]},"subscribers":{"nodes":[]},"attachments":{"nodes":[]},"comments":{"nodes":[{"id":"comment-1","body":"root","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z","parent":null,"user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true},"reactions":[{"id":"reaction-1","emoji":"👍","createdAt":"2025-01-01T00:01:00Z","updatedAt":"2025-01-01T00:01:00Z","comment":{"id":"comment-1","issue":{"id":"issue-1"}},"user":{"id":"user-2","name":"Grace","displayName":"Grace Hopper","email":"grace@example.com","isMe":false}}]},{"id":"comment-2","body":"reply","createdAt":"2025-01-01T00:02:00Z","updatedAt":"2025-01-01T00:02:00Z","parent":{"id":"comment-1"},"user":{"id":"user-2","name":"Grace","displayName":"Grace Hopper","email":"grace@example.com","isMe":false},"reactions":[]}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}`,
		`{"data":{"issue":{"id":"issue-1","identifier":"ABC-1","title":"Threaded","state":{"id":"state-1","name":"Todo"},"assignee":null,"priority":1,"updatedAt":"2025-01-01T00:00:00Z","createdAt":"2025-01-01T00:00:00Z","description":null,"team":{"id":"team-1"},"project":null,"labels":{"nodes":[]},"url":"https://linear.app/issue/ABC-1","archivedAt":null,"parent":null,"children":{"nodes":[]},"relations":{"nodes":[]},"inverseRelations":{"nodes":[]},"subscribers":{"nodes":[]},"attachments":{"nodes":[]},"comments":{"nodes":[{"id":"comment-3","body":"orphan","createdAt":"2025-01-01T00:03:00Z","updatedAt":"2025-01-01T00:03:00Z","parent":{"id":"missing-parent"},"user":{"id":"user-3","name":"Lin","displayName":"Lin Doe","email":"lin@example.com","isMe":false},"reactions":[{"id":"reaction-3","emoji":"🎉","createdAt":"2025-01-01T00:04:00Z","updatedAt":"2025-01-01T00:04:00Z","comment":{"id":"comment-3","issue":{"id":"issue-1"}},"user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}]}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor-2"}}}}}`,
	}
	requestQueries := make([]string, 0, len(responses))
	requestCursors := make([]interface{}, 0, len(responses))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requestQueries = append(requestQueries, request.Query)
		requestCursors = append(requestCursors, request.Variables["commentsAfter"])
		if len(requestQueries) > len(responses) {
			t.Fatalf("received %d requests, want %d", len(requestQueries), len(responses))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responses[len(requestQueries)-1]))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	issue, err := client.FetchIssueByID(context.Background(), "issue-1")
	if err != nil {
		t.Fatalf("FetchIssueByID() error: %v", err)
	}
	if len(issue.Comments) != 3 {
		t.Fatalf("comments length = %d, want 3", len(issue.Comments))
	}
	if issue.Comments[0].ParentID != "" || issue.Comments[0].Parent != nil {
		t.Fatalf("root parent = %#v, want nil", issue.Comments[0].Parent)
	}
	if issue.Comments[1].ParentID != "comment-1" || issue.Comments[1].Parent == nil || issue.Comments[1].Parent.ID != "comment-1" {
		t.Fatalf("reply parent = %#v, want comment-1", issue.Comments[1].Parent)
	}
	if len(issue.Comments[0].Reactions) != 1 || issue.Comments[0].Reactions[0].ID != "reaction-1" || issue.Comments[0].Reactions[0].CommentID != "comment-1" || issue.Comments[0].Reactions[0].User.ID != "user-2" {
		t.Fatalf("root reactions = %#v, want reaction-1/user-2", issue.Comments[0].Reactions)
	}
	if len(issue.Comments[2].Reactions) != 1 || issue.Comments[2].Reactions[0].ID != "reaction-3" || issue.Comments[2].Reactions[0].CommentID != "comment-3" {
		t.Fatalf("orphan reactions = %#v, want reaction-3/comment-3", issue.Comments[2].Reactions)
	}
	if len(requestQueries) != 2 || requestCursors[0] != nil || requestCursors[1] != "cursor-1" {
		t.Fatalf("request cursors = %#v, want [nil cursor-1]", requestCursors)
	}
	for _, query := range requestQueries {
		for _, field := range []string{"parent", "reactions", "pageInfo", "after: $commentsAfter"} {
			if !strings.Contains(query, field) {
				t.Fatalf("query = %q, want %q", query, field)
			}
		}
	}
}

func TestFetchIssueByIDRejectsMalformedCommentPagination(t *testing.T) {
	response := `{"data":{"issue":{"id":"issue-1","identifier":"ABC-1","title":"Malformed","state":{"id":"state-1","name":"Todo"},"assignee":null,"priority":1,"updatedAt":"2025-01-01T00:00:00Z","createdAt":"2025-01-01T00:00:00Z","description":null,"team":{"id":"team-1"},"project":null,"labels":{"nodes":[]},"url":"https://linear.app/issue/ABC-1","archivedAt":null,"parent":null,"children":{"nodes":[]},"relations":{"nodes":[]},"inverseRelations":{"nodes":[]},"subscribers":{"nodes":[]},"attachments":{"nodes":[]},"comments":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":null}}}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	_, err := client.FetchIssueByID(context.Background(), "issue-1")
	if err == nil || !strings.Contains(err.Error(), "no end cursor") {
		t.Fatalf("FetchIssueByID() error = %v, want missing end cursor", err)
	}
}

func TestCreateCommentPreservesParentAndReactions(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"commentCreate":{"success":true,"comment":{"id":"comment-2","body":"reply","createdAt":"2025-01-01T00:02:00Z","updatedAt":"2025-01-02T00:02:00Z","issueId":"issue-1","parent":{"id":"comment-1"},"user":{"id":"user-2","name":"Grace","displayName":"Grace Hopper","email":"grace@example.com","isMe":false},"reactions":[{"id":"reaction-2","emoji":"✅","createdAt":"2025-01-01T00:03:00Z","updatedAt":"2025-01-01T00:03:00Z","comment":{"id":"comment-2","issue":{"id":"issue-1"}},"user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}]}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	comment, err := client.CreateComment(context.Background(), CreateCommentInput{IssueID: "issue-1", Body: "reply"})
	if err != nil {
		t.Fatalf("CreateComment() error: %v", err)
	}
	if comment.ParentID != "comment-1" || comment.Parent == nil || comment.Parent.ID != "comment-1" {
		t.Fatalf("parent = %#v, want comment-1", comment.Parent)
	}
	if len(comment.Reactions) != 1 || comment.Reactions[0].ID != "reaction-2" || comment.Reactions[0].CommentID != "comment-2" || comment.Reactions[0].User.ID != "user-1" {
		t.Fatalf("reactions = %#v, want reaction-2/comment-2/user-1", comment.Reactions)
	}
	query, _ := request["query"].(string)
	for _, field := range []string{"parent", "reactions"} {
		if !strings.Contains(query, field) {
			t.Fatalf("query = %q, want %q", query, field)
		}
	}
}

// TestFetchIssuesPage_NoNextPage verifies end cursor is cleared when pagination ends.
func TestFetchIssuesPage_NoNextPage(t *testing.T) {
	response := issuesPageResponse([]string{}, false, "cursor-ignored")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	page, err := client.FetchIssuesPage(context.Background(), FetchIssuesParams{First: 1}, nil)
	if err != nil {
		t.Fatalf("FetchIssuesPage() error: %v", err)
	}

	if page.HasNext {
		t.Error("HasNext = true, want false")
	}
	if page.EndCursor != nil {
		t.Errorf("EndCursor = %#v, want nil", page.EndCursor)
	}
}

// TestFetchIssues_ProgressCallback verifies progress updates per page.
func TestFetchIssues_ProgressCallback(t *testing.T) {
	pageOne := issuesPageResponse([]string{
		issueNodeJSON("issue-1", "ABC-1", "First issue"),
	}, true, "cursor-1")
	pageTwo := issuesPageResponse([]string{
		issueNodeJSON("issue-2", "ABC-2", "Second issue"),
		issueNodeJSON("issue-3", "ABC-3", "Third issue"),
	}, false, "cursor-2")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 0 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(pageOne))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(pageTwo))
		}
		requestCount++
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	progressCalls := make([]IssueFetchProgress, 0)
	params := FetchIssuesParams{
		First: 2,
		OnProgress: func(progress IssueFetchProgress) {
			progressCalls = append(progressCalls, progress)
		},
	}

	_, err := client.FetchIssues(context.Background(), params)
	if err != nil {
		t.Fatalf("FetchIssues() error: %v", err)
	}

	if len(progressCalls) != 2 {
		t.Fatalf("Progress calls = %d, want 2", len(progressCalls))
	}
	if progressCalls[0].Page != 1 || progressCalls[0].Fetched != 1 {
		t.Errorf("First progress = %+v, want Page=1 Fetched=1", progressCalls[0])
	}
	if progressCalls[1].Page != 2 || progressCalls[1].Fetched != 3 {
		t.Errorf("Second progress = %+v, want Page=2 Fetched=3", progressCalls[1])
	}
}

// TestFetchIssues_StopsWhenNoNextPage verifies pagination stops at the last page.
func TestFetchIssues_StopsWhenNoNextPage(t *testing.T) {
	requestCount := 0
	response := issuesPageResponse([]string{
		issueNodeJSON("issue-1", "ABC-1", "First issue"),
	}, false, "cursor-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	_, err := client.FetchIssues(context.Background(), FetchIssuesParams{First: 1})
	if err != nil {
		t.Fatalf("FetchIssues() error: %v", err)
	}

	if requestCount != 1 {
		t.Fatalf("Expected 1 request, got %d", requestCount)
	}
}

func TestFetchIssuesParams_Defaults(t *testing.T) {
	params := FetchIssuesParams{}
	if params.First != 0 {
		t.Errorf("Default First = %d, want 0 (will be set to 50 by client)", params.First)
	}
	if params.OrderBy != "" {
		t.Errorf("Default OrderBy = %q, want empty string (will default to updatedAt)", params.OrderBy)
	}
}

func TestBuildBaseIssueFilter(t *testing.T) {
	tests := []struct {
		name   string
		params FetchIssuesParams
		want   IssueFilter
	}{
		{
			name:   "state only filter",
			params: FetchIssuesParams{StateID: "state-1"},
			want: IssueFilter{
				"state": map[string]interface{}{"id": map[string]interface{}{"eq": "state-1"}},
			},
		},
		{
			name: "team project state filters",
			params: FetchIssuesParams{
				TeamID:    "team-1",
				ProjectID: "project-1",
				StateID:   "state-2",
				CycleID:   "cycle-1",
			},
			want: IssueFilter{
				"team":    map[string]interface{}{"id": map[string]interface{}{"eq": "team-1"}},
				"project": map[string]interface{}{"id": map[string]interface{}{"eq": "project-1"}},
				"state":   map[string]interface{}{"id": map[string]interface{}{"eq": "state-2"}},
				"cycle":   map[string]interface{}{"id": map[string]interface{}{"eq": "cycle-1"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBaseIssueFilter(tt.params)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildBaseIssueFilter() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestBuildIssueFilter_SearchTerms verifies search term filtering behavior.
func TestBuildIssueFilter_SearchTerms(t *testing.T) {
	tests := []struct {
		name   string
		params FetchIssuesParams
		want   IssueFilter
	}{
		{
			name:   "single term searches title and description",
			params: FetchIssuesParams{Search: "ABC-123"},
			want: IssueFilter{
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "ABC-123"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "ABC-123"}},
				},
			},
		},
		{
			name:   "multiple terms require each term",
			params: FetchIssuesParams{Search: "login bug"},
			want: IssueFilter{
				"and": []map[string]interface{}{
					{
						"or": []map[string]interface{}{
							{"title": map[string]interface{}{"containsIgnoreCase": "login"}},
							{"description": map[string]interface{}{"containsIgnoreCase": "login"}},
						},
					},
					{
						"or": []map[string]interface{}{
							{"title": map[string]interface{}{"containsIgnoreCase": "bug"}},
							{"description": map[string]interface{}{"containsIgnoreCase": "bug"}},
						},
					},
				},
			},
		},
		{
			name:   "trims search and preserves team filters",
			params: FetchIssuesParams{TeamID: "team-1", ProjectID: "project-1", Search: "  issue  "},
			want: IssueFilter{
				"team":    map[string]interface{}{"id": map[string]interface{}{"eq": "team-1"}},
				"project": map[string]interface{}{"id": map[string]interface{}{"eq": "project-1"}},
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "issue"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "issue"}},
				},
			},
		},
		{
			name:   "state filter without search",
			params: FetchIssuesParams{StateID: "state-2"},
			want: IssueFilter{
				"state": map[string]interface{}{"id": map[string]interface{}{"eq": "state-2"}},
			},
		},
		{
			name: "state filter with search and team",
			params: FetchIssuesParams{
				TeamID:  "team-1",
				StateID: "state-3",
				Search:  "fix",
			},
			want: IssueFilter{
				"team":  map[string]interface{}{"id": map[string]interface{}{"eq": "team-1"}},
				"state": map[string]interface{}{"id": map[string]interface{}{"eq": "state-3"}},
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "fix"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "fix"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildIssueFilter(tt.params)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("buildIssueFilter() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCreateIssueInput(t *testing.T) {
	input := CreateIssueInput{
		TeamID:      "team-123",
		Title:       "Test Issue",
		Description: "Description",
	}

	if input.TeamID != "team-123" {
		t.Errorf("TeamID = %q, want %q", input.TeamID, "team-123")
	}
	if input.Title != "Test Issue" {
		t.Errorf("Title = %q, want %q", input.Title, "Test Issue")
	}
}

func TestUpdateIssueInput(t *testing.T) {
	title := "New Title"
	stateID := "state-456"
	input := UpdateIssueInput{
		ID:      "issue-123",
		Title:   &title,
		StateID: &stateID,
	}

	if input.ID != "issue-123" {
		t.Errorf("ID = %q, want %q", input.ID, "issue-123")
	}
	if *input.Title != "New Title" {
		t.Errorf("Title = %q, want %q", *input.Title, "New Title")
	}
	if *input.StateID != "state-456" {
		t.Errorf("StateID = %q, want %q", *input.StateID, "state-456")
	}
	if input.Description != nil {
		t.Error("Description should be nil when not set")
	}
}

func TestBuildIssueFilter_CombinesRichFilters(t *testing.T) {
	nullFalse := false
	estimate := 5.0
	got := buildIssueFilter(FetchIssuesParams{
		TeamID:             "team-1",
		ProjectID:          "project-1",
		StateID:            "state-1",
		CycleID:            "cycle-1",
		AssigneeID:         "user-1",
		LabelIDs:           []string{"label-1", "label-2"},
		ProjectMilestoneID: "milestone-1",
		DueDate: DateFilter{
			GTE: "2026-06-01",
			LTE: "2026-06-30",
		},
		Estimate: NumberFilter{
			Eq: &estimate,
		},
		Search: "login bug",
	})

	want := IssueFilter{
		"team":             map[string]interface{}{"id": map[string]interface{}{"eq": "team-1"}},
		"project":          map[string]interface{}{"id": map[string]interface{}{"eq": "project-1"}},
		"state":            map[string]interface{}{"id": map[string]interface{}{"eq": "state-1"}},
		"cycle":            map[string]interface{}{"id": map[string]interface{}{"eq": "cycle-1"}},
		"assignee":         map[string]interface{}{"id": map[string]interface{}{"eq": "user-1"}},
		"projectMilestone": map[string]interface{}{"id": map[string]interface{}{"eq": "milestone-1"}},
		"dueDate":          map[string]interface{}{"gte": "2026-06-01", "lte": "2026-06-30"},
		"estimate":         map[string]interface{}{"eq": estimate},
		"and": []map[string]interface{}{
			{"labels": map[string]interface{}{"some": map[string]interface{}{"id": map[string]interface{}{"eq": "label-1"}}}},
			{"labels": map[string]interface{}{"some": map[string]interface{}{"id": map[string]interface{}{"eq": "label-2"}}}},
			{
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "login"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "login"}},
				},
			},
			{
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "bug"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "bug"}},
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildIssueFilter() = %#v, want %#v", got, want)
	}

	got = buildStructuredIssueFilter(FetchIssuesParams{
		DueDate: DateFilter{Null: &nullFalse},
	})
	want = IssueFilter{
		"dueDate": map[string]interface{}{"null": false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildStructuredIssueFilter(null due date) = %#v, want %#v", got, want)
	}
}

func TestSearchIssuesPage_UsesStructuredFilterWithSearchTerm(t *testing.T) {
	var filterValue map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		var okFilter bool
		filterValue, okFilter = variables["filter"].(map[string]interface{})
		if !okFilter {
			t.Fatalf("variables.filter = %#v, want object", variables["filter"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"searchIssues":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	if _, err := client.FetchIssuesPage(context.Background(), FetchIssuesParams{
		Search:     "ABC-1",
		AssigneeID: "user-1",
		LabelIDs:   []string{"label-1"},
	}, nil); err != nil {
		t.Fatalf("FetchIssuesPage() error: %v", err)
	}

	if _, hasTextOr := filterValue["or"]; hasTextOr {
		t.Fatalf("searchIssues filter includes text OR filters: %#v", filterValue)
	}
	if _, ok := filterValue["assignee"]; !ok {
		t.Fatalf("searchIssues filter missing assignee: %#v", filterValue)
	}
	if andFilters, ok := filterValue["and"].([]interface{}); !ok || len(andFilters) != 1 {
		t.Fatalf("searchIssues filter and = %#v, want one label condition", filterValue["and"])
	}
}

func TestIssueLabel(t *testing.T) {
	label := IssueLabel{
		ID:    "label-123",
		Name:  "Bug",
		Color: "#ff0000",
	}

	if label.ID != "label-123" {
		t.Errorf("ID = %q, want %q", label.ID, "label-123")
	}
	if label.Name != "Bug" {
		t.Errorf("Name = %q, want %q", label.Name, "Bug")
	}
	if label.Color != "#ff0000" {
		t.Errorf("Color = %q, want %q", label.Color, "#ff0000")
	}
}

func TestIssueWithLabels(t *testing.T) {
	issue := Issue{
		ID:         "issue-123",
		Identifier: "LIN-123",
		Title:      "Test Issue",
		Labels: []IssueLabel{
			{ID: "lbl-1", Name: "Bug", Color: "#ff0000"},
			{ID: "lbl-2", Name: "Feature", Color: "#00ff00"},
		},
	}

	if len(issue.Labels) != 2 {
		t.Fatalf("Labels length = %d, want 2", len(issue.Labels))
	}
	if issue.Labels[0].Name != "Bug" {
		t.Errorf("Labels[0].Name = %q, want %q", issue.Labels[0].Name, "Bug")
	}
	if issue.Labels[1].Name != "Feature" {
		t.Errorf("Labels[1].Name = %q, want %q", issue.Labels[1].Name, "Feature")
	}
}

func TestUpdateIssueInput_LabelIDs(t *testing.T) {
	t.Run("nil LabelIDs means no change", func(t *testing.T) {
		input := UpdateIssueInput{
			ID:       "issue-123",
			LabelIDs: nil,
		}
		if input.LabelIDs != nil {
			t.Error("LabelIDs should be nil when not set")
		}
	})

	t.Run("empty slice clears all labels", func(t *testing.T) {
		emptyLabels := []string{}
		input := UpdateIssueInput{
			ID:       "issue-123",
			LabelIDs: &emptyLabels,
		}
		if input.LabelIDs == nil {
			t.Fatal("LabelIDs should not be nil")
		}
		if len(*input.LabelIDs) != 0 {
			t.Errorf("LabelIDs length = %d, want 0", len(*input.LabelIDs))
		}
	})

	t.Run("non-empty slice sets specific labels", func(t *testing.T) {
		labelIDs := []string{"lbl-1", "lbl-2", "lbl-3"}
		input := UpdateIssueInput{
			ID:       "issue-123",
			LabelIDs: &labelIDs,
		}
		if input.LabelIDs == nil {
			t.Fatal("LabelIDs should not be nil")
		}
		if len(*input.LabelIDs) != 3 {
			t.Errorf("LabelIDs length = %d, want 3", len(*input.LabelIDs))
		}
		if (*input.LabelIDs)[0] != "lbl-1" {
			t.Errorf("LabelIDs[0] = %q, want %q", (*input.LabelIDs)[0], "lbl-1")
		}
	})
}

func TestIssueRef(t *testing.T) {
	ref := IssueRef{
		ID:         "issue-123",
		Identifier: "LIN-123",
		Title:      "Parent Issue",
	}

	if ref.ID != "issue-123" {
		t.Errorf("ID = %q, want %q", ref.ID, "issue-123")
	}
	if ref.Identifier != "LIN-123" {
		t.Errorf("Identifier = %q, want %q", ref.Identifier, "LIN-123")
	}
	if ref.Title != "Parent Issue" {
		t.Errorf("Title = %q, want %q", ref.Title, "Parent Issue")
	}
}

func TestIssueChildRef(t *testing.T) {
	ref := IssueChildRef{
		ID:         "child-123",
		Identifier: "LIN-456",
		Title:      "Child Issue",
		State:      "In Progress",
		StateID:    "state-789",
	}

	if ref.ID != "child-123" {
		t.Errorf("ID = %q, want %q", ref.ID, "child-123")
	}
	if ref.Identifier != "LIN-456" {
		t.Errorf("Identifier = %q, want %q", ref.Identifier, "LIN-456")
	}
	if ref.Title != "Child Issue" {
		t.Errorf("Title = %q, want %q", ref.Title, "Child Issue")
	}
	if ref.State != "In Progress" {
		t.Errorf("State = %q, want %q", ref.State, "In Progress")
	}
	if ref.StateID != "state-789" {
		t.Errorf("StateID = %q, want %q", ref.StateID, "state-789")
	}
}

func TestIssueWithParentAndChildren(t *testing.T) {
	parent := &IssueRef{
		ID:         "parent-123",
		Identifier: "LIN-100",
		Title:      "Parent Issue",
	}
	children := []IssueChildRef{
		{ID: "child-1", Identifier: "LIN-201", Title: "Child 1", State: "Todo"},
		{ID: "child-2", Identifier: "LIN-202", Title: "Child 2", State: "Done"},
	}

	issue := Issue{
		ID:         "issue-123",
		Identifier: "LIN-123",
		Title:      "Test Issue",
		Parent:     parent,
		Children:   children,
	}

	// Test parent
	if issue.Parent == nil {
		t.Fatal("Parent should not be nil")
	}
	if issue.Parent.ID != "parent-123" {
		t.Errorf("Parent.ID = %q, want %q", issue.Parent.ID, "parent-123")
	}

	// Test children
	if len(issue.Children) != 2 {
		t.Fatalf("Children length = %d, want 2", len(issue.Children))
	}
	if issue.Children[0].Identifier != "LIN-201" {
		t.Errorf("Children[0].Identifier = %q, want %q", issue.Children[0].Identifier, "LIN-201")
	}
	if issue.Children[1].State != "Done" {
		t.Errorf("Children[1].State = %q, want %q", issue.Children[1].State, "Done")
	}
}

func TestIssueWithoutParentOrChildren(t *testing.T) {
	issue := Issue{
		ID:         "issue-123",
		Identifier: "LIN-123",
		Title:      "Standalone Issue",
		Parent:     nil,
		Children:   nil,
	}

	if issue.Parent != nil {
		t.Error("Parent should be nil for standalone issue")
	}
	if issue.Children != nil {
		t.Error("Children should be nil for standalone issue")
	}
}

func TestCreateIssueInput_ParentID(t *testing.T) {
	t.Run("without parent", func(t *testing.T) {
		input := CreateIssueInput{
			TeamID: "team-123",
			Title:  "New Issue",
		}
		if input.ParentID != "" {
			t.Errorf("ParentID = %q, want empty string", input.ParentID)
		}
	})

	t.Run("with parent", func(t *testing.T) {
		input := CreateIssueInput{
			TeamID:   "team-123",
			Title:    "Sub Issue",
			ParentID: "parent-456",
		}
		if input.ParentID != "parent-456" {
			t.Errorf("ParentID = %q, want %q", input.ParentID, "parent-456")
		}
	})
}

func TestCreateIssue_SendsCycleID(t *testing.T) {
	var input map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		input, ok = variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("variables.input = %#v, want object", variables["input"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mutationIssueResponse("issueCreate")))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	_, err := client.CreateIssue(context.Background(), CreateIssueInput{
		TeamID:  "team-1",
		Title:   "Issue in cycle",
		CycleID: "cycle-1",
	})
	if err != nil {
		t.Fatalf("CreateIssue() error: %v", err)
	}
	if input["cycleId"] != "cycle-1" {
		t.Fatalf("cycleId = %#v, want cycle-1", input["cycleId"])
	}
}

func TestUpdateIssueInput_ParentID(t *testing.T) {
	t.Run("nil ParentID means no change", func(t *testing.T) {
		input := UpdateIssueInput{
			ID:       "issue-123",
			ParentID: nil,
		}
		if input.ParentID != nil {
			t.Error("ParentID should be nil when not set")
		}
	})

	t.Run("empty string clears parent", func(t *testing.T) {
		emptyParent := ""
		input := UpdateIssueInput{
			ID:       "issue-123",
			ParentID: &emptyParent,
		}
		if input.ParentID == nil {
			t.Fatal("ParentID should not be nil")
		}
		if *input.ParentID != "" {
			t.Errorf("ParentID = %q, want empty string", *input.ParentID)
		}
	})

	t.Run("non-empty string sets parent", func(t *testing.T) {
		parentID := "parent-456"
		input := UpdateIssueInput{
			ID:       "issue-123",
			ParentID: &parentID,
		}
		if input.ParentID == nil {
			t.Fatal("ParentID should not be nil")
		}
		if *input.ParentID != "parent-456" {
			t.Errorf("ParentID = %q, want %q", *input.ParentID, "parent-456")
		}
	})
}

func TestUpdateIssue_SetsAndClearsCycleID(t *testing.T) {
	var inputs []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		input, ok := variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("variables.input = %#v, want object", variables["input"])
		}
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mutationIssueResponse("issueUpdate")))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	cycleID := "cycle-1"
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{ID: "issue-1", CycleID: &cycleID}); err != nil {
		t.Fatalf("UpdateIssue(set cycle) error: %v", err)
	}

	clearCycleID := ""
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{ID: "issue-1", CycleID: &clearCycleID}); err != nil {
		t.Fatalf("UpdateIssue(clear cycle) error: %v", err)
	}

	if len(inputs) != 2 {
		t.Fatalf("inputs length = %d, want 2", len(inputs))
	}
	if inputs[0]["cycleId"] != "cycle-1" {
		t.Fatalf("set cycleId = %#v, want cycle-1", inputs[0]["cycleId"])
	}
	if value, ok := inputs[1]["cycleId"]; !ok || value != nil {
		t.Fatalf("clear cycleId = %#v (present=%v), want present null", value, ok)
	}
}

func TestUpdateIssue_SetsClearsAndOmitsProjectID(t *testing.T) {
	var inputs []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		input, ok := variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("variables.input = %#v, want object", variables["input"])
		}
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mutationIssueResponse("issueUpdate")))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	projectID := "project-1"
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{ID: "issue-1", ProjectID: &projectID}); err != nil {
		t.Fatalf("UpdateIssue(set project) error: %v", err)
	}
	clearProjectID := ""
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{ID: "issue-1", ProjectID: &clearProjectID}); err != nil {
		t.Fatalf("UpdateIssue(clear project) error: %v", err)
	}
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{ID: "issue-1"}); err != nil {
		t.Fatalf("UpdateIssue(omit project) error: %v", err)
	}

	if len(inputs) != 3 {
		t.Fatalf("inputs length = %d, want 3", len(inputs))
	}
	if got := inputs[0]["projectId"]; got != "project-1" {
		t.Fatalf("set projectId = %#v, want project-1", got)
	}
	if got, ok := inputs[1]["projectId"]; !ok || got != nil {
		t.Fatalf("clear projectId = %#v (present=%v), want present null", got, ok)
	}
	if _, ok := inputs[2]["projectId"]; ok {
		t.Fatalf("no-change projectId unexpectedly present: %#v", inputs[2])
	}
}

func TestUpdateIssue_SetsAndClearsPlanningFields(t *testing.T) {
	var inputs []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables, ok := reqBody["variables"].(map[string]interface{})
		if !ok {
			t.Fatalf("Request body missing variables")
		}
		input, ok := variables["input"].(map[string]interface{})
		if !ok {
			t.Fatalf("variables.input = %#v, want object", variables["input"])
		}
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mutationIssueResponse("issueUpdate")))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	dueDate := "2026-06-15"
	estimate := 5.0
	milestoneID := "milestone-1"
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{
		ID:                 "issue-1",
		DueDate:            &dueDate,
		Estimate:           &estimate,
		ProjectMilestoneID: &milestoneID,
	}); err != nil {
		t.Fatalf("UpdateIssue(set planning fields) error: %v", err)
	}

	clearDueDate := ""
	clearMilestone := ""
	if _, err := client.UpdateIssue(context.Background(), UpdateIssueInput{
		ID:                 "issue-1",
		DueDate:            &clearDueDate,
		ClearEstimate:      true,
		ProjectMilestoneID: &clearMilestone,
	}); err != nil {
		t.Fatalf("UpdateIssue(clear planning fields) error: %v", err)
	}

	if len(inputs) != 2 {
		t.Fatalf("inputs length = %d, want 2", len(inputs))
	}
	if inputs[0]["dueDate"] != "2026-06-15" || inputs[0]["estimate"] != float64(5) || inputs[0]["projectMilestoneId"] != "milestone-1" {
		t.Fatalf("set input = %#v, want dueDate estimate projectMilestoneId", inputs[0])
	}
	if value, ok := inputs[1]["dueDate"]; !ok || value != nil {
		t.Fatalf("clear dueDate = %#v (present=%v), want present null", value, ok)
	}
	if value, ok := inputs[1]["estimate"]; !ok || value != nil {
		t.Fatalf("clear estimate = %#v (present=%v), want present null", value, ok)
	}
	if value, ok := inputs[1]["projectMilestoneId"]; !ok || value != nil {
		t.Fatalf("clear projectMilestoneId = %#v (present=%v), want present null", value, ok)
	}
}

func TestListProjectMilestones_PaginatesAndParses(t *testing.T) {
	requestCount := 0
	var afterValues []interface{}
	pageOne := projectMilestonesPageResponse([]string{
		projectMilestoneNodeJSON("milestone-1", "Beta", "2026-06-30", "next", "project-1"),
	}, true, "cursor-1")
	pageTwo := projectMilestonesPageResponse([]string{
		projectMilestoneNodeJSON("milestone-2", "GA", "", "unstarted", "project-1"),
	}, false, "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		variables := reqBody["variables"].(map[string]interface{})
		afterValues = append(afterValues, variables["after"])
		w.Header().Set("Content-Type", "application/json")
		if requestCount == 0 {
			_, _ = w.Write([]byte(pageOne))
		} else {
			_, _ = w.Write([]byte(pageTwo))
		}
		requestCount++
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	milestones, err := client.ListProjectMilestones(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("ListProjectMilestones() error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}
	if afterValues[0] != nil || afterValues[1] != "cursor-1" {
		t.Fatalf("afterValues = %#v, want [nil cursor-1]", afterValues)
	}
	if len(milestones) != 2 {
		t.Fatalf("milestones length = %d, want 2", len(milestones))
	}
	if milestones[0].ID != "milestone-1" || milestones[0].TargetDate == nil || *milestones[0].TargetDate != "2026-06-30" {
		t.Fatalf("milestones[0] = %+v, want Beta with target date", milestones[0])
	}
}

func TestIssueRelationMutationsAndSubscriptions(t *testing.T) {
	var operations []string
	var inputs []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		query, _ := reqBody["query"].(string)
		switch {
		case strings.Contains(query, "issueRelationCreate"):
			operations = append(operations, "issueRelationCreate")
			variables := reqBody["variables"].(map[string]interface{})
			inputs = append(inputs, variables["input"].(map[string]interface{}))
			_, _ = w.Write([]byte(issueRelationMutationResponse("issueRelationCreate")))
		case strings.Contains(query, "issueRelationDelete"):
			operations = append(operations, "issueRelationDelete")
			_, _ = w.Write([]byte(`{"data":{"issueRelationDelete":{"success":true}}}`))
		case strings.Contains(query, "issueSubscribe"):
			operations = append(operations, "issueSubscribe")
			_, _ = w.Write([]byte(mutationIssueResponse("issueSubscribe")))
		case strings.Contains(query, "issueUnsubscribe"):
			operations = append(operations, "issueUnsubscribe")
			_, _ = w.Write([]byte(mutationIssueResponse("issueUnsubscribe")))
		default:
			t.Fatalf("unexpected query: %s", query)
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		Token:    "test-token",
		Endpoint: server.URL,
	})

	relation, err := client.CreateIssueRelation(context.Background(), CreateIssueRelationInput{
		IssueID:        "issue-1",
		RelatedIssueID: "issue-2",
		Type:           IssueRelationBlocks,
	})
	if err != nil {
		t.Fatalf("CreateIssueRelation() error: %v", err)
	}
	if relation.ID != "rel-1" || relation.Type != string(IssueRelationBlocks) {
		t.Fatalf("relation = %+v, want rel-1 blocks", relation)
	}
	if err := client.DeleteIssueRelation(context.Background(), "rel-1"); err != nil {
		t.Fatalf("DeleteIssueRelation() error: %v", err)
	}
	if _, err := client.SubscribeToIssue(context.Background(), "issue-1"); err != nil {
		t.Fatalf("SubscribeToIssue() error: %v", err)
	}
	if _, err := client.UnsubscribeFromIssue(context.Background(), "issue-1"); err != nil {
		t.Fatalf("UnsubscribeFromIssue() error: %v", err)
	}

	wantOperations := []string{"issueRelationCreate", "issueRelationDelete", "issueSubscribe", "issueUnsubscribe"}
	if !reflect.DeepEqual(operations, wantOperations) {
		t.Fatalf("operations = %#v, want %#v", operations, wantOperations)
	}
	if len(inputs) != 1 || inputs[0]["issueId"] != "issue-1" || inputs[0]["relatedIssueId"] != "issue-2" || inputs[0]["type"] != "blocks" {
		t.Fatalf("issueRelationCreate input = %#v, want issue relation input", inputs)
	}
}
