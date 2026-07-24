package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newReviewGraphQLServer(t *testing.T, onQuery func(string)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if onQuery != nil {
			onQuery(request.Query)
		}

		var data any
		query := strings.ToLower(request.Query)
		switch {
		case strings.Contains(query, "viewer"):
			data = map[string]any{
				"viewer": map[string]any{
					"id":          "user-1",
					"name":        "Test User",
					"displayName": "Test User",
					"email":       "test@example.com",
				},
			}
		case strings.Contains(query, "teams"):
			data = map[string]any{
				"teams": map[string]any{
					"nodes": []any{
						map[string]any{"id": "team-2", "key": "NEX", "name": "Nexa"},
					},
				},
			}
		case strings.Contains(query, "projects"):
			data = map[string]any{
				"team": map[string]any{
					"projects": map[string]any{
						"nodes": []any{
							map[string]any{"id": "proj-1", "name": "Website"},
						},
					},
				},
			}
		case strings.Contains(query, "states"):
			data = map[string]any{
				"team": map[string]any{
					"states": map[string]any{"nodes": []any{}},
				},
			}
		case strings.Contains(query, "cycles"):
			data = map[string]any{
				"team": map[string]any{
					"cycles": map[string]any{
						"nodes": []any{},
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   "",
						},
					},
				},
			}
		case strings.Contains(query, "issues"):
			data = map[string]any{
				"issues": map[string]any{
					"nodes": []any{},
					"pageInfo": map[string]any{
						"hasNextPage": false,
						"endCursor":   "",
					},
				},
			}
		default:
			t.Errorf("unexpected GraphQL query: %s", request.Query)
			data = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
			t.Errorf("encode GraphQL response: %v", err)
		}
	}))
}

func startReviewTestApplication(t *testing.T, app *App) {
	t.Helper()

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(120, 40)
	app.app.SetScreen(screen).SetRoot(app.pages, true)

	runDone := make(chan error, 1)
	go func() {
		runDone <- app.app.Run()
	}()

	ready := make(chan struct{})
	go func() {
		app.app.QueueUpdate(func() {
			close(ready)
		})
	}()
	select {
	case <-ready:
	case err := <-runDone:
		t.Fatalf("test application stopped before becoming ready: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out starting test application")
	}

	t.Cleanup(func() {
		app.app.Stop()
		select {
		case err := <-runDone:
			if err != nil {
				t.Errorf("test application stopped with error: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("timed out stopping test application")
		}
	})
}

func TestApplySettingsRebindsDefaultNavigationFetchersToNewCache(t *testing.T) {
	var oldProjectQueries atomic.Int32
	oldServer := newReviewGraphQLServer(t, func(query string) {
		if strings.Contains(strings.ToLower(query), "projects") {
			oldProjectQueries.Add(1)
		}
	})
	defer oldServer.Close()

	var newProjectQueries atomic.Int32
	newServer := newReviewGraphQLServer(t, func(query string) {
		if strings.Contains(strings.ToLower(query), "projects") {
			newProjectQueries.Add(1)
		}
	})
	defer newServer.Close()

	cfg := config.Config{
		APIEndpoint: oldServer.URL,
		CacheTTL:    time.Minute,
		PageSize:    10,
	}
	client := linearapi.NewClient(linearapi.ClientConfig{Endpoint: oldServer.URL})
	app := NewApp(client, cfg, nil)
	startReviewTestApplication(t, app)
	refreshDone := installRefreshCompletionHook(app)

	newCfg := cfg
	newCfg.APIEndpoint = newServer.URL
	app.applySettings(newCfg)

	if _, err := app.fetchProjectsFunc(context.Background(), "team-2"); err != nil {
		t.Fatalf("fetchProjectsFunc() error: %v", err)
	}
	if got := newProjectQueries.Load(); got == 0 {
		t.Fatal("fetchProjectsFunc() did not use the replacement cache")
	}
	if got := oldProjectQueries.Load(); got != 0 {
		t.Fatalf("fetchProjectsFunc() used the abandoned cache %d time(s)", got)
	}
	waitForRefreshCompletion(t, refreshDone)
}

func TestLoadInitialDataRefreshesOnlyTheConfiguredDefaultView(t *testing.T) {
	server := newReviewGraphQLServer(t, nil)
	defer server.Close()

	cfg := config.Config{
		APIEndpoint:    server.URL,
		CacheTTL:       time.Minute,
		PageSize:       10,
		DefaultTeam:    "NEX",
		DefaultProject: "Website",
	}
	client := linearapi.NewClient(linearapi.ClientConfig{Endpoint: server.URL})
	app := NewApp(client, cfg, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.preloadTeamMetadataFunc = func(string) {}
	refreshDone := installRefreshCompletionHook(app)

	fetches := make(chan linearapi.FetchIssuesParams, 3)
	app.fetchIssuesPage = func(_ context.Context, params linearapi.FetchIssuesParams, _ *string) (linearapi.IssuePage, error) {
		fetches <- params
		return linearapi.IssuePage{}, nil
	}
	startReviewTestApplication(t, app)

	app.loadInitialData()

	select {
	case params := <-fetches:
		if params.TeamID != "team-2" || params.ProjectID != "proj-1" {
			t.Fatalf("initial issue fetch = team %q project %q, want team-2/proj-1", params.TeamID, params.ProjectID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial issue fetch")
	}

	select {
	case params := <-fetches:
		t.Fatalf("unexpected duplicate initial issue fetch for team %q project %q", params.TeamID, params.ProjectID)
	case <-time.After(250 * time.Millisecond):
	}
	waitForRefreshCompletion(t, refreshDone)
}
