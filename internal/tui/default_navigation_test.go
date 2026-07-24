package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestFindTeamByKeyOrName(t *testing.T) {
	teams := []linearapi.Team{
		{ID: "team-1", Key: "ENG", Name: "Engineering"},
		{ID: "team-2", Key: "NEX", Name: "Nexa"},
	}

	tests := []struct {
		name   string
		query  string
		wantID string
	}{
		{name: "matches key", query: "NEX", wantID: "team-2"},
		{name: "matches key case-insensitively", query: "nex", wantID: "team-2"},
		{name: "matches name", query: "Engineering", wantID: "team-1"},
		{name: "matches name case-insensitively", query: "engineering", wantID: "team-1"},
		{name: "trims whitespace", query: "  NEX  ", wantID: "team-2"},
		{name: "no match returns nil", query: "MISSING", wantID: ""},
		{name: "empty query returns nil", query: "", wantID: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			team := findTeamByKeyOrName(teams, tc.query)
			if tc.wantID == "" {
				if team != nil {
					t.Fatalf("findTeamByKeyOrName(%q) = %+v, want nil", tc.query, team)
				}
				return
			}
			if team == nil {
				t.Fatalf("findTeamByKeyOrName(%q) = nil, want team %s", tc.query, tc.wantID)
			}
			if team.ID != tc.wantID {
				t.Errorf("findTeamByKeyOrName(%q).ID = %s, want %s", tc.query, team.ID, tc.wantID)
			}
		})
	}
}

// defaultNavTeams returns the team fixtures used by default-navigation tests.
func defaultNavTeams() []linearapi.Team {
	return []linearapi.Team{
		{ID: "team-1", Key: "ENG", Name: "Engineering"},
		{ID: "team-2", Key: "NEX", Name: "Nexa"},
	}
}

// newDefaultNavTestApp builds an App with seams stubbed for default-navigation tests.
func newDefaultNavTestApp(cfg config.Config) *App {
	cfg.CacheTTL = time.Minute
	cfg.PageSize = 10
	app := NewApp(&linearapi.Client{}, cfg, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.preloadTeamMetadataFunc = func(string) {}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{}, nil
	}
	app.fetchProjectsFunc = func(_ context.Context, teamID string) ([]linearapi.Project, error) {
		return []linearapi.Project{
			{ID: "proj-1", Name: "Website", TeamID: teamID},
			{ID: "proj-2", Name: "Mobile App", TeamID: teamID},
		}, nil
	}
	app.fetchWorkflowStatesFunc = func(context.Context, string) ([]linearapi.WorkflowState, error) {
		return nil, nil
	}
	app.fetchCyclesFunc = func(context.Context, string) ([]linearapi.Cycle, error) {
		return nil, nil
	}
	return app
}

// currentNavigationNode returns the NavigationNode referenced by the tree's current node.
func currentNavigationNode(t *testing.T, app *App) *NavigationNode {
	t.Helper()
	node := app.navigationTree.GetCurrentNode()
	if node == nil {
		t.Fatal("navigation tree has no current node")
	}
	nav, ok := node.GetReference().(*NavigationNode)
	if !ok {
		t.Fatalf("current node reference is %T, want *NavigationNode", node.GetReference())
	}
	return nav
}

func TestApplyDefaultNavigationSelectsTeam(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "nex"})
	refreshDone := installRefreshCompletionHook(app)
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)
	waitForRefreshCompletion(t, refreshDone)

	nav := currentNavigationNode(t, app)
	if !nav.IsTeam || nav.TeamID != "team-2" {
		t.Fatalf("current node = %+v, want team node for team-2", nav)
	}
	if app.selectedNavigation == nil || app.selectedNavigation.TeamID != "team-2" {
		t.Fatalf("selectedNavigation = %+v, want team-2", app.selectedNavigation)
	}
	teamNode := app.navigationTree.GetCurrentNode()
	if !teamNode.IsExpanded() {
		t.Error("team node should be expanded")
	}
	if len(teamNode.GetChildren()) == 0 {
		t.Error("team node should have project children")
	}
}

func TestApplyDefaultNavigationSelectsProject(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "NEX", DefaultProject: "website"})
	refreshDone := installRefreshCompletionHook(app)
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)
	waitForRefreshCompletion(t, refreshDone)

	nav := currentNavigationNode(t, app)
	if !nav.IsProject || nav.ID != "proj-1" {
		t.Fatalf("current node = %+v, want project node proj-1", nav)
	}
	if app.selectedNavigation == nil || !app.selectedNavigation.IsProject {
		t.Fatalf("selectedNavigation = %+v, want project selection", app.selectedNavigation)
	}
}

func TestApplyDefaultNavigationUnknownTeamKeepsAllIssues(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "MISSING"})
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)

	nav := currentNavigationNode(t, app)
	if nav.ID != "all" {
		t.Fatalf("current node = %+v, want All Issues", nav)
	}
	if !strings.Contains(app.statusMessage, "MISSING") {
		t.Errorf("statusMessage = %q, want mention of missing team", app.statusMessage)
	}
}

func TestApplyDefaultNavigationUnknownProjectSelectsTeam(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "NEX", DefaultProject: "Missing"})
	refreshDone := installRefreshCompletionHook(app)
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)
	waitForRefreshCompletion(t, refreshDone)

	nav := currentNavigationNode(t, app)
	if !nav.IsTeam || nav.TeamID != "team-2" {
		t.Fatalf("current node = %+v, want team node for team-2", nav)
	}
	if !strings.Contains(app.statusMessage, "Missing") {
		t.Errorf("statusMessage = %q, want mention of missing project", app.statusMessage)
	}
}

func TestApplyDefaultNavigationProjectFetchFailureIsNotReportedAsMissing(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "NEX", DefaultProject: "Website"})
	app.fetchProjectsFunc = func(context.Context, string) ([]linearapi.Project, error) {
		return nil, errors.New("temporary projects failure")
	}
	refreshDone := installRefreshCompletionHook(app)
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)
	waitForRefreshCompletion(t, refreshDone)

	if strings.Contains(strings.ToLower(app.statusMessage), "not found") {
		t.Fatalf("statusMessage = %q, should report a load failure rather than a missing project", app.statusMessage)
	}
	if !strings.Contains(strings.ToLower(app.statusMessage), "load") {
		t.Fatalf("statusMessage = %q, want project load failure", app.statusMessage)
	}
}

func TestApplyDefaultNavigationWarnsWhenProjectCannotBeAppliedAfterPartialLoadFailure(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{DefaultTeam: "NEX", DefaultProject: "Website"})
	app.fetchWorkflowStatesFunc = func(context.Context, string) ([]linearapi.WorkflowState, error) {
		return nil, errors.New("temporary states failure")
	}
	refreshDone := installRefreshCompletionHook(app)
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)
	waitForRefreshCompletion(t, refreshDone)

	nav := currentNavigationNode(t, app)
	if !nav.IsTeam || nav.TeamID != "team-2" {
		t.Fatalf("current node = %+v, want fallback team node for team-2", nav)
	}
	if !strings.Contains(strings.ToLower(app.statusMessage), "default project") ||
		!strings.Contains(strings.ToLower(app.statusMessage), "load") {
		t.Fatalf("statusMessage = %q, want warning that the default project could not be loaded", app.statusMessage)
	}
}

func TestApplyDefaultNavigationNoDefaultsIsNoop(t *testing.T) {
	app := newDefaultNavTestApp(config.Config{})
	teams := defaultNavTeams()
	app.rebuildNavigationTree(teams)

	app.applyDefaultNavigation(context.Background(), teams)

	nav := currentNavigationNode(t, app)
	if nav.ID != "all" {
		t.Fatalf("current node = %+v, want All Issues", nav)
	}
}

func TestFindProjectByName(t *testing.T) {
	projects := []linearapi.Project{
		{ID: "proj-1", Name: "Website", TeamID: "team-1"},
		{ID: "proj-2", Name: "Mobile App", TeamID: "team-1"},
	}

	tests := []struct {
		name   string
		query  string
		wantID string
	}{
		{name: "matches name", query: "Website", wantID: "proj-1"},
		{name: "matches case-insensitively", query: "mobile app", wantID: "proj-2"},
		{name: "trims whitespace", query: " Website ", wantID: "proj-1"},
		{name: "no match returns nil", query: "Missing", wantID: ""},
		{name: "empty query returns nil", query: "", wantID: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := findProjectByName(projects, tc.query)
			if tc.wantID == "" {
				if project != nil {
					t.Fatalf("findProjectByName(%q) = %+v, want nil", tc.query, project)
				}
				return
			}
			if project == nil {
				t.Fatalf("findProjectByName(%q) = nil, want project %s", tc.query, tc.wantID)
			}
			if project.ID != tc.wantID {
				t.Errorf("findProjectByName(%q).ID = %s, want %s", tc.query, project.ID, tc.wantID)
			}
		})
	}
}
