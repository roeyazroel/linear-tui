package linearapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

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
					"nodes": []
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

func TestFetchIssuesParams_Defaults(t *testing.T) {
	params := FetchIssuesParams{}
	if params.First != 0 {
		t.Errorf("Default First = %d, want 0 (will be set to 50 by client)", params.First)
	}
	if params.OrderBy != "" {
		t.Errorf("Default OrderBy = %q, want empty string (will default to updatedAt)", params.OrderBy)
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
			name:   "single term includes identifier",
			params: FetchIssuesParams{Search: "ABC-123"},
			want: IssueFilter{
				"or": []map[string]interface{}{
					{"title": map[string]interface{}{"containsIgnoreCase": "ABC-123"}},
					{"description": map[string]interface{}{"containsIgnoreCase": "ABC-123"}},
					{"identifier": map[string]interface{}{"containsIgnoreCase": "ABC-123"}},
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
							{"identifier": map[string]interface{}{"containsIgnoreCase": "login"}},
						},
					},
					{
						"or": []map[string]interface{}{
							{"title": map[string]interface{}{"containsIgnoreCase": "bug"}},
							{"description": map[string]interface{}{"containsIgnoreCase": "bug"}},
							{"identifier": map[string]interface{}{"containsIgnoreCase": "bug"}},
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
					{"identifier": map[string]interface{}{"containsIgnoreCase": "issue"}},
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
