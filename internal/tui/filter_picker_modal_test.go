package tui

import (
	"context"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newScopeTestApp(t *testing.T) *App {
	t.Helper()
	app := NewApp(&linearapi.Client{}, config.Config{PageSize: 1, CacheTTL: time.Minute}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.fetchIssuesPage = func(ctx context.Context, params linearapi.FetchIssuesParams, after *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{Issues: []linearapi.Issue{}, HasNext: false}, nil
	}
	// Avoid real network calls when a scope sets team context.
	app.loadTeamMetadataFunc = func(string) {}
	return app
}

func TestFilterPickerModal_Refilter(t *testing.T) {
	app := newScopeTestApp(t)
	fp := app.filterPickerModal

	items := []PickerItem{
		{ID: "1", Label: "Telehealth"},
		{ID: "2", Label: "User Inboxes"},
		{ID: "3", Label: "Skills"},
	}
	fp.Show("Select Project", items, func(PickerItem) {})

	if len(fp.filtered) != 3 {
		t.Fatalf("expected 3 items before filtering, got %d", len(fp.filtered))
	}

	// Case-insensitive substring filter.
	fp.query = "IN"
	fp.refilter()
	if len(fp.filtered) != 1 || fp.filtered[0].ID != "2" {
		t.Fatalf("expected only 'User Inboxes' to match 'IN', got %+v", fp.filtered)
	}

	// Empty query restores all items.
	fp.query = ""
	fp.refilter()
	if len(fp.filtered) != 3 {
		t.Fatalf("expected 3 items after clearing query, got %d", len(fp.filtered))
	}
}

func TestSetProjectScope(t *testing.T) {
	app := newScopeTestApp(t)

	// Scope to a specific project (no team => no metadata load).
	app.setProjectScope("proj-1", "Telehealth", "")
	if app.richFilters.ProjectID != "proj-1" {
		t.Fatalf("ProjectID = %q, want %q", app.richFilters.ProjectID, "proj-1")
	}
	if app.richFilters.ProjectName != "Telehealth" {
		t.Fatalf("ProjectName = %q, want %q", app.richFilters.ProjectName, "Telehealth")
	}

	// "All Projects" clears the project scope.
	app.setProjectScope("", "", "")
	if app.richFilters.ProjectID != "" || app.richFilters.ProjectName != "" {
		t.Fatalf("expected cleared project scope, got id=%q name=%q", app.richFilters.ProjectID, app.richFilters.ProjectName)
	}
}

func TestRefreshIssues_DefaultsToMyFavorites(t *testing.T) {
	app := newScopeTestApp(t)
	app.currentUser = &linearapi.User{ID: "me"}
	app.favoriteProjects = []linearapi.Project{{ID: "p1"}, {ID: "p2"}}

	called := make(chan linearapi.FetchIssuesParams, 1)
	app.fetchIssuesPage = func(ctx context.Context, params linearapi.FetchIssuesParams, after *string) (linearapi.IssuePage, error) {
		select {
		case called <- params:
		default:
		}
		return linearapi.IssuePage{Issues: []linearapi.Issue{}, HasNext: false}, nil
	}

	app.refreshIssues()

	select {
	case params := <-called:
		if params.AssigneeID != "me" {
			t.Fatalf("AssigneeID = %q, want %q", params.AssigneeID, "me")
		}
		if len(params.ProjectIDs) != 2 || params.ProjectIDs[0] != "p1" || params.ProjectIDs[1] != "p2" {
			t.Fatalf("ProjectIDs = %v, want [p1 p2]", params.ProjectIDs)
		}
	case <-time.After(time.Second):
		t.Fatal("fetchIssuesPage was not called")
	}
}

func TestRefreshIssues_ToggleAllAssignees(t *testing.T) {
	app := newScopeTestApp(t)
	app.currentUser = &linearapi.User{ID: "me"}

	called := make(chan linearapi.FetchIssuesParams, 1)
	app.fetchIssuesPage = func(ctx context.Context, params linearapi.FetchIssuesParams, after *string) (linearapi.IssuePage, error) {
		select {
		case called <- params:
		default:
		}
		return linearapi.IssuePage{Issues: []linearapi.Issue{}, HasNext: false}, nil
	}

	app.myIssuesOnly = false
	app.refreshIssues()

	select {
	case params := <-called:
		if params.AssigneeID != "" {
			t.Fatalf("AssigneeID = %q, want empty (all assignees)", params.AssigneeID)
		}
	case <-time.After(time.Second):
		t.Fatal("fetchIssuesPage was not called")
	}
}

func TestSetProjectScope_SetsTeamContext(t *testing.T) {
	app := newScopeTestApp(t)
	app.setProjectScope("proj-1", "Telehealth", "team-9")
	if app.selectedProjectTeamID != "team-9" {
		t.Fatalf("selectedProjectTeamID = %q, want %q", app.selectedProjectTeamID, "team-9")
	}
	if got := app.GetSelectedTeamID(); got != "team-9" {
		t.Fatalf("GetSelectedTeamID() = %q, want %q", got, "team-9")
	}
}
