package linearapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func roadmapRequest(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode GraphQL request: %v", err)
	}
	return request
}

func roadmapVariables(request map[string]interface{}) map[string]interface{} {
	variables, _ := request["variables"].(map[string]interface{})
	return variables
}

func roadmapNodeJSON(id, body, health, projectID, userID string) string {
	return `{"id":` + strconvQuote(id) +
		`,"body":` + strconvQuote(body) +
		`,"health":` + strconvQuote(health) +
		`,"createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z"` +
		`,"project":{"id":` + strconvQuote(projectID) + `,"name":"Roadmap"}` +
		`,"user":{"id":` + strconvQuote(userID) + `,"name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}`
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestListInitiatives(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := roadmapRequest(t, r)
		requests = append(requests, request)
		query, _ := request["query"].(string)
		variables := roadmapVariables(request)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.Contains(query, "initiative(id:"):
			_, _ = w.Write([]byte(`{"data":{"initiative":{"projects":{"nodes":[{"id":"project-2","name":"Second","teams":{"nodes":[{"id":"team-2"}]}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`))
		case variables["after"] == nil:
			_, _ = w.Write([]byte(`{"data":{"initiatives":{"nodes":[{"id":"initiative-1","name":"Launch","description":"First initiative","status":"Active","targetDate":"2026-12-31","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z","projects":{"nodes":[{"id":"project-1","name":"First","description":"Project details","teams":{"nodes":[{"id":"team-1"}]}}],"pageInfo":{"hasNextPage":true,"endCursor":"project-cursor"}}}],"pageInfo":{"hasNextPage":true,"endCursor":"initiative-cursor"}}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"initiatives":{"nodes":[{"id":"initiative-2","name":"Ship","description":null,"status":"Planned","targetDate":null,"createdAt":"2025-02-01T00:00:00Z","updatedAt":"2025-02-02T00:00:00Z","projects":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`))
		}
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	initiatives, err := client.ListInitiatives(context.Background())
	if err != nil {
		t.Fatalf("ListInitiatives() error: %v", err)
	}
	want := []Initiative{
		{
			ID:          "initiative-1",
			Name:        "Launch",
			Description: "First initiative",
			Status:      "Active",
			TargetDate:  "2026-12-31",
			CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			Projects: []Project{
				{ID: "project-1", Name: "First", Description: "Project details", TeamID: "team-1"},
				{ID: "project-2", Name: "Second", TeamID: "team-2"},
			},
		},
		{
			ID:          "initiative-2",
			Name:        "Ship",
			Status:      "Planned",
			CreatedAt:   time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2025, 2, 2, 0, 0, 0, 0, time.UTC),
			TargetDate:  "",
			Description: "",
			Projects:    []Project{},
		},
	}
	if !reflect.DeepEqual(initiatives, want) {
		t.Fatalf("initiatives = %#v, want %#v", initiatives, want)
	}
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3 (initiative pages plus nested project page)", len(requests))
	}
	if query, _ := requests[0]["query"].(string); !strings.Contains(query, "initiatives") || !strings.Contains(query, "projects") {
		t.Fatalf("initial query = %q, want initiatives and projects", query)
	}
	if query, _ := requests[0]["query"].(string); !strings.Contains(query, "teams") {
		t.Fatalf("initial query = %q, want project team identity", query)
	}
	if query, _ := requests[0]["query"].(string); !strings.Contains(query, "teams(first: $teamsFirst)") {
		t.Fatalf("initial query = %q, want explicitly paginated teams connection", query)
	}
	if teamsFirst := roadmapVariables(requests[0])["teamsFirst"]; teamsFirst != float64(1) {
		t.Fatalf("initial teamsFirst = %#v, want 1", teamsFirst)
	}
	if query, _ := requests[1]["query"].(string); !strings.Contains(query, "teams(first: $teamsFirst)") {
		t.Fatalf("nested query = %q, want explicitly paginated teams connection", query)
	}
	if teamsFirst := roadmapVariables(requests[1])["teamsFirst"]; teamsFirst != float64(1) {
		t.Fatalf("nested teamsFirst = %#v, want 1", teamsFirst)
	}
}

func TestListProjectUpdates(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := roadmapRequest(t, r)
		requests = append(requests, request)
		variables := roadmapVariables(request)
		w.Header().Set("Content-Type", "application/json")
		if variables["after"] == nil {
			_, _ = w.Write([]byte(`{"data":{"projectUpdates":{"nodes":[` + roadmapNodeJSON("update-1", "Initial update", "onTrack", "project-1", "user-1") + `],"pageInfo":{"hasNextPage":true,"endCursor":"update-cursor"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"projectUpdates":{"nodes":[` + roadmapNodeJSON("update-2", "Follow-up", "atRisk", "project-1", "user-2") + `],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	updates, err := client.ListProjectUpdates(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("ListProjectUpdates() error: %v", err)
	}
	if len(updates) != 2 || updates[0].ID != "update-1" || updates[1].ID != "update-2" {
		t.Fatalf("updates = %#v, want two mapped updates", updates)
	}
	if updates[0].ProjectID != "project-1" || updates[0].Author.ID != "user-1" || updates[0].Author.DisplayName != "Ada Lovelace" {
		t.Fatalf("first update mapping = %#v, want project and author", updates[0])
	}
	if updates[0].Health != "onTrack" || updates[1].Health != "atRisk" {
		t.Fatalf("health mapping = %q, %q", updates[0].Health, updates[1].Health)
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2 pages", len(requests))
	}
	query, _ := requests[0]["query"].(string)
	if !strings.Contains(query, "projectUpdates") || !strings.Contains(query, "filter") {
		t.Fatalf("query = %q, want projectUpdates filter", query)
	}
	input := roadmapVariables(requests[0])["filter"]
	if !strings.Contains(string(mustJSON(input)), "project") || !strings.Contains(string(mustJSON(input)), "project-1") {
		t.Fatalf("filter variable = %#v, want project id filter", input)
	}
}

func TestRoadmapMutationOperations(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateCreate":{"success":true,"projectUpdate":` + roadmapNodeJSON("update-1", "Created", "offTrack", "project-1", "user-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		update, err := client.CreateProjectUpdate(context.Background(), CreateProjectUpdateInput{
			ProjectID: "project-1",
			Body:      "Created",
			Health:    ProjectUpdateHealthOffTrack,
		})
		if err != nil {
			t.Fatalf("CreateProjectUpdate() error: %v", err)
		}
		if update.ID != "update-1" || update.ProjectID != "project-1" || update.Health != "offTrack" {
			t.Fatalf("update = %#v, want mapped created update", update)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectUpdateCreate") || !strings.Contains(query, "ProjectUpdateCreateInput") {
			t.Fatalf("query = %q, want create operation and input type", query)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		if input["projectId"] != "project-1" || input["body"] != "Created" || input["health"] != "offTrack" {
			t.Fatalf("input = %#v, want schema-confirmed fields", input)
		}
	})

	t.Run("update", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateUpdate":{"success":true,"projectUpdate":` + roadmapNodeJSON("update-1", "Updated", "atRisk", "project-1", "user-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		update, err := client.UpdateProjectUpdate(context.Background(), UpdateProjectUpdateInput{
			ID:     "update-1",
			Body:   "Updated",
			Health: ProjectUpdateHealthAtRisk,
		})
		if err != nil {
			t.Fatalf("UpdateProjectUpdate() error: %v", err)
		}
		if update.Body != "Updated" || update.Health != "atRisk" {
			t.Fatalf("update = %#v, want mapped updated update", update)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectUpdateUpdate") || !strings.Contains(query, "ProjectUpdateUpdateInput") {
			t.Fatalf("query = %q, want update operation and input type", query)
		}
		variables := roadmapVariables(request)
		if variables["id"] != "update-1" {
			t.Fatalf("id variable = %#v, want update-1", variables["id"])
		}
		input, _ := variables["input"].(map[string]interface{})
		if input["body"] != "Updated" || input["health"] != "atRisk" {
			t.Fatalf("input = %#v, want schema-confirmed fields", input)
		}
	})

	t.Run("archive", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateArchive":{"success":true,"entity":null}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		if err := client.ArchiveProjectUpdate(context.Background(), "update-1"); err != nil {
			t.Fatalf("ArchiveProjectUpdate() error: %v", err)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectUpdateArchive") {
			t.Fatalf("query = %q, want archive operation", query)
		}
		if roadmapVariables(request)["id"] != "update-1" {
			t.Fatalf("id variable = %#v, want update-1", roadmapVariables(request)["id"])
		}
	})
}

func TestRoadmapValidationDoesNotReachNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})

	tests := []struct {
		name string
		call func() error
	}{
		{name: "list updates empty project", call: func() error {
			_, err := client.ListProjectUpdates(context.Background(), " ")
			return err
		}},
		{name: "create empty project", call: func() error {
			_, err := client.CreateProjectUpdate(context.Background(), CreateProjectUpdateInput{Body: "body"})
			return err
		}},
		{name: "create empty body", call: func() error {
			_, err := client.CreateProjectUpdate(context.Background(), CreateProjectUpdateInput{ProjectID: "project-1"})
			return err
		}},
		{name: "create invalid health", call: func() error {
			_, err := client.CreateProjectUpdate(context.Background(), CreateProjectUpdateInput{ProjectID: "project-1", Body: "body", Health: "unknown"})
			return err
		}},
		{name: "update empty id", call: func() error {
			_, err := client.UpdateProjectUpdate(context.Background(), UpdateProjectUpdateInput{Body: "body"})
			return err
		}},
		{name: "update empty body", call: func() error {
			_, err := client.UpdateProjectUpdate(context.Background(), UpdateProjectUpdateInput{ID: "update-1"})
			return err
		}},
		{name: "update invalid health", call: func() error {
			_, err := client.UpdateProjectUpdate(context.Background(), UpdateProjectUpdateInput{ID: "update-1", Body: "body", Health: "unknown"})
			return err
		}},
		{name: "archive empty id", call: func() error {
			return client.ArchiveProjectUpdate(context.Background(), " ")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want zero for validation errors", calls)
	}
}

func TestRoadmapGraphQLErrorsAndOperationFailures(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errors":[{"message":"rate limited"}]}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		_, err := client.ListProjectUpdates(context.Background(), "project-1")
		if err == nil || !strings.Contains(err.Error(), "list project updates") || !strings.Contains(err.Error(), "rate limited") {
			t.Fatalf("error = %v, want wrapped API error", err)
		}
	})

	t.Run("create success false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateCreate":{"success":false,"projectUpdate":` + roadmapNodeJSON("update-1", "body", "onTrack", "project-1", "user-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		_, err := client.CreateProjectUpdate(context.Background(), CreateProjectUpdateInput{ProjectID: "project-1", Body: "body"})
		if err == nil || !strings.Contains(err.Error(), "operation failed") {
			t.Fatalf("error = %v, want operation failed", err)
		}
	})

	t.Run("update success false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateUpdate":{"success":false,"projectUpdate":` + roadmapNodeJSON("update-1", "body", "onTrack", "project-1", "user-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		_, err := client.UpdateProjectUpdate(context.Background(), UpdateProjectUpdateInput{ID: "update-1", Body: "body"})
		if err == nil || !strings.Contains(err.Error(), "operation failed") {
			t.Fatalf("error = %v, want operation failed", err)
		}
	})

	t.Run("archive success false", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdateArchive":{"success":false,"entity":null}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		err := client.ArchiveProjectUpdate(context.Background(), "update-1")
		if err == nil || !strings.Contains(err.Error(), "operation failed") {
			t.Fatalf("error = %v, want operation failed", err)
		}
	})
}

func roadmapProjectMutationJSON(id, name, teamID string) string {
	return `{"id":` + strconvQuote(id) + `,"name":` + strconvQuote(name) + `,"description":"Project details","teams":{"nodes":[{"id":` + strconvQuote(teamID) + `}]}}`
}

func roadmapInitiativeMutationJSON(id, name, description, status, targetDate string) string {
	return `{"id":` + strconvQuote(id) + `,"name":` + strconvQuote(name) + `,"description":` + strconvQuote(description) + `,"status":` + strconvQuote(status) + `,"targetDate":` + strconvQuote(targetDate) + `}`
}

func TestRoadmapResourceMutations(t *testing.T) {
	t.Run("project create", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectCreate":{"success":true,"project":` + roadmapProjectMutationJSON("project-1", "CLI", "team-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		project, err := client.CreateProject(context.Background(), CreateProjectInput{Name: "CLI", TeamID: "team-1", Description: "Command line"})
		if err != nil {
			t.Fatalf("CreateProject() error: %v", err)
		}
		if project.ID != "project-1" || project.Name != "CLI" || project.Description != "Project details" || project.TeamID != "team-1" {
			t.Fatalf("project = %#v, want mapped project", project)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectCreate") || !strings.Contains(query, "ProjectCreateInput") {
			t.Fatalf("query = %q, want project create and exact input type", query)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		if input["name"] != "CLI" || !reflect.DeepEqual(input["teamIds"], []interface{}{"team-1"}) || input["description"] != "Command line" {
			t.Fatalf("input = %#v, want schema-confirmed project fields", input)
		}
	})

	t.Run("project update clear and set", func(t *testing.T) {
		var request map[string]interface{}
		description := ""
		name := "CLI 2"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdate":{"success":true,"project":` + roadmapProjectMutationJSON("project-1", "CLI 2", "team-1") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		project, err := client.UpdateProject(context.Background(), UpdateProjectInput{ID: "project-1", Name: &name, Description: &description})
		if err != nil {
			t.Fatalf("UpdateProject() error: %v", err)
		}
		if project.ID != "project-1" || project.Name != "CLI 2" {
			t.Fatalf("project = %#v, want mapped project", project)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectUpdate") || !strings.Contains(query, "ProjectUpdateInput") {
			t.Fatalf("query = %q, want project update and exact input type", query)
		}
		variables := roadmapVariables(request)
		if variables["id"] != "project-1" {
			t.Fatalf("id variable = %#v, want project-1", variables["id"])
		}
		input, _ := variables["input"].(map[string]interface{})
		if input["name"] != "CLI 2" || input["description"] != "" {
			t.Fatalf("input = %#v, want set and explicit clear fields", input)
		}
		if _, ok := input["teamIds"]; ok {
			t.Fatalf("input = %#v, ordinary rename must omit teamIds", input)
		}
	})

	t.Run("project update explicit team clear", func(t *testing.T) {
		var request map[string]interface{}
		name := "CLI 2"
		teamIDs := []string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdate":{"success":true,"project":` + roadmapProjectMutationJSON("project-1", "CLI 2", "") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		if _, err := client.UpdateProject(context.Background(), UpdateProjectInput{ID: "project-1", Name: &name, TeamIDs: &teamIDs}); err != nil {
			t.Fatalf("UpdateProject() error: %v", err)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		teamIDsValue, ok := input["teamIds"]
		if !ok || !reflect.DeepEqual(teamIDsValue, []interface{}{}) {
			t.Fatalf("input teamIds = %#v (present=%v), want explicit empty list", teamIDsValue, ok)
		}
	})

	t.Run("project update explicit team set", func(t *testing.T) {
		var request map[string]interface{}
		teamIDs := []string{"team-2"}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectUpdate":{"success":true,"project":` + roadmapProjectMutationJSON("project-1", "CLI 2", "team-2") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		if _, err := client.UpdateProject(context.Background(), UpdateProjectInput{ID: "project-1", TeamIDs: &teamIDs}); err != nil {
			t.Fatalf("UpdateProject() error: %v", err)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		if !reflect.DeepEqual(input["teamIds"], []interface{}{"team-2"}) {
			t.Fatalf("input teamIds = %#v, want explicit team set", input["teamIds"])
		}
	})

	t.Run("project delete", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"projectDelete":{"success":true,"entity":null}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		if err := client.DeleteProject(context.Background(), "project-1"); err != nil {
			t.Fatalf("DeleteProject() error: %v", err)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "projectDelete") {
			t.Fatalf("query = %q, want project delete", query)
		}
		if roadmapVariables(request)["id"] != "project-1" {
			t.Fatalf("id variable = %#v, want project-1", roadmapVariables(request)["id"])
		}
	})

	t.Run("initiative create", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"initiativeCreate":{"success":true,"initiative":` + roadmapInitiativeMutationJSON("initiative-1", "Launch", "Ship it", "Active", "2026-12-31") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		initiative, err := client.CreateInitiative(context.Background(), CreateInitiativeInput{Name: "Launch", Description: "Ship it", Status: InitiativeStatusActive, TargetDate: "2026-12-31"})
		if err != nil {
			t.Fatalf("CreateInitiative() error: %v", err)
		}
		if initiative.ID != "initiative-1" || initiative.Name != "Launch" || initiative.Status != "Active" || initiative.TargetDate != "2026-12-31" {
			t.Fatalf("initiative = %#v, want mapped initiative", initiative)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "initiativeCreate") || !strings.Contains(query, "InitiativeCreateInput") {
			t.Fatalf("query = %q, want initiative create and exact input type", query)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		if input["name"] != "Launch" || input["description"] != "Ship it" || input["status"] != "Active" || input["targetDate"] != "2026-12-31" {
			t.Fatalf("input = %#v, want schema-confirmed initiative fields", input)
		}
	})

	t.Run("initiative update clear and set", func(t *testing.T) {
		var request map[string]interface{}
		description := ""
		name := "Launch 2"
		status := InitiativeStatusCompleted
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = roadmapRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"initiativeUpdate":{"success":true,"initiative":` + roadmapInitiativeMutationJSON("initiative-1", "Launch 2", "", "Completed", "") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		initiative, err := client.UpdateInitiative(context.Background(), UpdateInitiativeInput{ID: "initiative-1", Name: &name, Description: &description, Status: &status})
		if err != nil {
			t.Fatalf("UpdateInitiative() error: %v", err)
		}
		if initiative.ID != "initiative-1" || initiative.Name != "Launch 2" || initiative.Status != "Completed" {
			t.Fatalf("initiative = %#v, want mapped initiative", initiative)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "initiativeUpdate") || !strings.Contains(query, "InitiativeUpdateInput") {
			t.Fatalf("query = %q, want initiative update and exact input type", query)
		}
		input, _ := roadmapVariables(request)["input"].(map[string]interface{})
		if input["name"] != "Launch 2" || input["description"] != "" || input["status"] != "Completed" {
			t.Fatalf("input = %#v, want set and explicit clear fields", input)
		}
	})

	for _, operation := range []struct {
		name string
		call func(*Client) error
		want string
	}{
		{name: "initiative archive", call: func(client *Client) error { return client.ArchiveInitiative(context.Background(), "initiative-1") }, want: "initiativeArchive"},
		{name: "initiative delete", call: func(client *Client) error { return client.DeleteInitiative(context.Background(), "initiative-1") }, want: "initiativeDelete"},
	} {
		t.Run(operation.name, func(t *testing.T) {
			var request map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request = roadmapRequest(t, r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"` + operation.want + `":{"success":true,"entity":null,"entityId":"initiative-1"}}}`))
			}))
			defer server.Close()
			client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
			if err := operation.call(client); err != nil {
				t.Fatalf("%s() error: %v", operation.name, err)
			}
			query, _ := request["query"].(string)
			if !strings.Contains(query, operation.want) {
				t.Fatalf("query = %q, want %s", query, operation.want)
			}
			if roadmapVariables(request)["id"] != "initiative-1" {
				t.Fatalf("id variable = %#v, want initiative-1", roadmapVariables(request)["id"])
			}
		})
	}
}

func TestCreateInitiativeToProject(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request = roadmapRequest(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"initiativeToProjectCreate":{"success":true}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	if err := client.CreateInitiativeToProject(context.Background(), CreateInitiativeToProjectInput{
		InitiativeID: "initiative-1",
		ProjectID:    "project-1",
	}); err != nil {
		t.Fatalf("CreateInitiativeToProject() error: %v", err)
	}

	query, _ := request["query"].(string)
	if !strings.Contains(query, "initiativeToProjectCreate") || !strings.Contains(query, "InitiativeToProjectCreateInput") {
		t.Fatalf("query = %q, want exact association operation and input type", query)
	}
	input, _ := roadmapVariables(request)["input"].(map[string]interface{})
	if input["initiativeId"] != "initiative-1" || input["projectId"] != "project-1" {
		t.Fatalf("input = %#v, want initiativeId/projectId", input)
	}
	if _, ok := input["sortOrder"]; ok {
		t.Fatalf("input = %#v, nil sort order must be omitted", input)
	}
}

func TestRoadmapResourceMutationValidationAndFailures(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"should not be reached"}]}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	name := "name"
	tests := []struct {
		name string
		call func() error
	}{
		{name: "create project empty name", call: func() error {
			_, err := client.CreateProject(context.Background(), CreateProjectInput{TeamID: "team-1"})
			return err
		}},
		{name: "create project missing team", call: func() error {
			_, err := client.CreateProject(context.Background(), CreateProjectInput{Name: "name"})
			return err
		}},
		{name: "update project empty id", call: func() error {
			_, err := client.UpdateProject(context.Background(), UpdateProjectInput{Name: &name})
			return err
		}},
		{name: "update project no fields", call: func() error {
			_, err := client.UpdateProject(context.Background(), UpdateProjectInput{ID: "project-1"})
			return err
		}},
		{name: "associate initiative empty initiative", call: func() error {
			return client.CreateInitiativeToProject(context.Background(), CreateInitiativeToProjectInput{ProjectID: "project-1"})
		}},
		{name: "associate initiative empty project", call: func() error {
			return client.CreateInitiativeToProject(context.Background(), CreateInitiativeToProjectInput{InitiativeID: "initiative-1"})
		}},
		{name: "delete project empty id", call: func() error { return client.DeleteProject(context.Background(), " ") }},
		{name: "create initiative empty name", call: func() error {
			_, err := client.CreateInitiative(context.Background(), CreateInitiativeInput{})
			return err
		}},
		{name: "create initiative invalid status", call: func() error {
			_, err := client.CreateInitiative(context.Background(), CreateInitiativeInput{Name: "name", Status: "Unknown"})
			return err
		}},
		{name: "update initiative empty id", call: func() error {
			_, err := client.UpdateInitiative(context.Background(), UpdateInitiativeInput{})
			return err
		}},
		{name: "update initiative no fields", call: func() error {
			_, err := client.UpdateInitiative(context.Background(), UpdateInitiativeInput{ID: "initiative-1"})
			return err
		}},
		{name: "update initiative invalid status", call: func() error {
			invalid := InitiativeStatus("Unknown")
			_, err := client.UpdateInitiative(context.Background(), UpdateInitiativeInput{ID: "initiative-1", Status: &invalid})
			return err
		}},
		{name: "archive initiative empty id", call: func() error { return client.ArchiveInitiative(context.Background(), " ") }},
		{name: "delete initiative empty id", call: func() error { return client.DeleteInitiative(context.Background(), " ") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want zero for validation errors", calls)
	}

	for _, operation := range []struct {
		name string
		call func(*Client) error
	}{
		{name: "project delete", call: func(client *Client) error { return client.DeleteProject(context.Background(), "project-1") }},
		{name: "initiative archive", call: func(client *Client) error { return client.ArchiveInitiative(context.Background(), "initiative-1") }},
		{name: "initiative delete", call: func(client *Client) error { return client.DeleteInitiative(context.Background(), "initiative-1") }},
		{name: "initiative to project create", call: func(client *Client) error {
			return client.CreateInitiativeToProject(context.Background(), CreateInitiativeToProjectInput{InitiativeID: "initiative-1", ProjectID: "project-1"})
		}},
	} {
		t.Run(operation.name+" API error", func(t *testing.T) {
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"errors":[{"message":"rate limited"}]}`))
			}))
			defer apiServer.Close()
			apiClient := NewClient(ClientConfig{Token: "test-token", Endpoint: apiServer.URL})
			if err := operation.call(apiClient); err == nil || !strings.Contains(err.Error(), "rate limited") {
				t.Fatalf("error = %v, want wrapped API error", err)
			}
		})
	}

	for _, operation := range []struct {
		name string
		body string
		call func(*Client) error
	}{
		{name: "project delete", body: `{"data":{"projectDelete":{"success":false,"entity":null}}}`, call: func(client *Client) error { return client.DeleteProject(context.Background(), "project-1") }},
		{name: "initiative archive", body: `{"data":{"initiativeArchive":{"success":false,"entity":null}}}`, call: func(client *Client) error { return client.ArchiveInitiative(context.Background(), "initiative-1") }},
		{name: "initiative delete", body: `{"data":{"initiativeDelete":{"success":false,"entity":null,"entityId":"initiative-1"}}}`, call: func(client *Client) error { return client.DeleteInitiative(context.Background(), "initiative-1") }},
		{name: "initiative to project create", body: `{"data":{"initiativeToProjectCreate":{"success":false}}}`, call: func(client *Client) error {
			return client.CreateInitiativeToProject(context.Background(), CreateInitiativeToProjectInput{InitiativeID: "initiative-1", ProjectID: "project-1"})
		}},
	} {
		t.Run(operation.name+" operation failure", func(t *testing.T) {
			failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(operation.body))
			}))
			defer failureServer.Close()
			failureClient := NewClient(ClientConfig{Token: "test-token", Endpoint: failureServer.URL})
			if err := operation.call(failureClient); err == nil || !strings.Contains(err.Error(), "operation failed") {
				t.Fatalf("error = %v, want operation failed", err)
			}
		})
	}
}

func mustJSON(value interface{}) []byte {
	b, _ := json.Marshal(value)
	return b
}
