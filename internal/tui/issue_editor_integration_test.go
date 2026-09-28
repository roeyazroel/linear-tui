package tui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func issueEditorIntegrationApp(t *testing.T, mode string, issue linearapi.Issue) *App {
	t.Helper()
	app := NewApp(&linearapi.Client{}, config.Config{
		Theme:         config.DefaultTheme,
		Density:       config.DefaultDensity,
		PageSize:      1,
		CacheTTL:      time.Minute,
		LinearAPIKey:  "test-key",
		APIEndpoint:   config.DefaultAPIEndpoint,
		LogLevel:      config.DefaultLogLevel,
		AgentProvider: config.DefaultAgentProvider,
		AgentSandbox:  config.DefaultAgentSandbox,
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.selectedNavigation = &NavigationNode{ID: issue.TeamID, TeamID: issue.TeamID, IsTeam: true, Text: "Engineering"}
	app.issuesMu.Lock()
	app.selectedIssue = &issue
	app.issues = []linearapi.Issue{issue}
	app.issuesMu.Unlock()
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) { return issue, nil }
	app.teamUsers = []linearapi.User{{ID: "user-1", DisplayName: "Ada"}, {ID: "user-2", Name: "Grace"}}
	app.teamProjects = []linearapi.Project{{ID: "project-1", Name: "CLI", TeamID: issue.TeamID}}
	app.workflowStates = []linearapi.WorkflowState{{ID: "state-1", Name: "Todo"}, {ID: "state-2", Name: "Done"}}
	app.teamCycles = []linearapi.Cycle{{ID: "cycle-1", Name: "Launch", Number: 12}}
	app.teamMetadataMu.Lock()
	app.teamMetadataTeamID = issue.TeamID
	app.teamMetadataMu.Unlock()
	app.fetchIssueLabelsFunc = func(context.Context, string) ([]linearapi.IssueLabel, error) {
		return []linearapi.IssueLabel{{ID: "label-1", Name: "bug"}, {ID: "label-2", Name: "urgent"}}, nil
	}
	return app
}

func TestIssueEditorIntegrationCommandUsesCompleteEditor(t *testing.T) {
	for _, mode := range []string{"", ""} {
		t.Run(mode, func(t *testing.T) {
			issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1", AssigneeID: "user-1", ProjectID: "project-1", Cycle: &linearapi.CycleRef{ID: "cycle-1", Name: "Launch", Number: 12}, Labels: []linearapi.IssueLabel{{ID: "label-2", Name: "urgent"}}}
			app := issueEditorIntegrationApp(t, mode, issue)
			command := findCommandByID(DefaultCommands(app), "edit_issue")
			if command == nil {
				t.Fatal("edit_issue command missing")
			}
			if command.ShortcutRune != 0 || command.Title != "Edit issue" {
				t.Fatalf("edit_issue command = %+v, want palette-only Edit issue", *command)
			}
			legacy := findCommandByID(DefaultCommands(app), "edit_title")
			if legacy == nil || legacy.ShortcutRune != 'e' {
				t.Fatalf("edit_title command = %+v, want e shortcut", legacy)
			}

			command.Run(app)
			waitForCondition(t, time.Second, func() bool {
				var visible bool
				workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
				return visible
			})
			workspaceUIRead(app, func() {
				if app.issueEditorModal.Title.GetText() != "Title" {
					t.Fatalf("editor title = %q, want Title", app.issueEditorModal.Title.GetText())
				}
				if got := app.issueEditorModal.LabelsSummary.GetText(true); got == "" || !containsText(got, "urgent") {
					t.Fatalf("editor labels summary = %q, want urgent", got)
				}
			})
		})
	}
}

func TestIssueEditorIntegrationFetchesMetadataForCacheTeamMismatch(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-b", Title: "Title", StateID: "state-b"}
	app := issueEditorIntegrationApp(t, "", issue)
	// Simulate a cache snapshot from the previously selected team.
	app.teamUsers = []linearapi.User{{ID: "team-a-user", Name: "Team A"}}
	app.teamProjects = []linearapi.Project{{ID: "team-a-project", Name: "Team A", TeamID: "team-a"}}
	app.workflowStates = []linearapi.WorkflowState{{ID: "state-a", Name: "Team A"}}
	app.teamCycles = []linearapi.Cycle{{ID: "team-a-cycle", Name: "Team A"}}
	app.teamMetadataMu.Lock()
	app.teamMetadataTeamID = "team-a"
	app.teamMetadataMu.Unlock()

	var userCalls, projectCalls, stateCalls, cycleCalls atomic.Int32
	app.fetchUsersFunc = func(context.Context, string) ([]linearapi.User, error) {
		userCalls.Add(1)
		return []linearapi.User{{ID: "team-b-user", Name: "Team B"}}, nil
	}
	app.fetchProjectsFunc = func(context.Context, string) ([]linearapi.Project, error) {
		projectCalls.Add(1)
		return []linearapi.Project{{ID: "team-b-project", Name: "Team B", TeamID: "team-b"}}, nil
	}
	app.fetchWorkflowStatesFunc = func(context.Context, string) ([]linearapi.WorkflowState, error) {
		stateCalls.Add(1)
		return []linearapi.WorkflowState{{ID: "state-b", Name: "Team B"}}, nil
	}
	app.fetchCyclesFunc = func(context.Context, string) ([]linearapi.Cycle, error) {
		cycleCalls.Add(1)
		return []linearapi.Cycle{{ID: "team-b-cycle", Name: "Team B"}}, nil
	}

	app.ShowIssueEditorModal()
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return visible
	})
	if userCalls.Load() != 1 || projectCalls.Load() != 1 || stateCalls.Load() != 1 || cycleCalls.Load() != 1 {
		t.Fatalf("metadata fetch calls for mismatched team = users:%d projects:%d states:%d cycles:%d, want one each", userCalls.Load(), projectCalls.Load(), stateCalls.Load(), cycleCalls.Load())
	}
}

func TestIssueEditorIntegrationUsesCachedMetadataForSameTeam(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	var userCalls, projectCalls, stateCalls, cycleCalls atomic.Int32
	app.fetchUsersFunc = func(context.Context, string) ([]linearapi.User, error) {
		userCalls.Add(1)
		return nil, nil
	}
	app.fetchProjectsFunc = func(context.Context, string) ([]linearapi.Project, error) {
		projectCalls.Add(1)
		return nil, nil
	}
	app.fetchWorkflowStatesFunc = func(context.Context, string) ([]linearapi.WorkflowState, error) {
		stateCalls.Add(1)
		return nil, nil
	}
	app.fetchCyclesFunc = func(context.Context, string) ([]linearapi.Cycle, error) {
		cycleCalls.Add(1)
		return nil, nil
	}

	app.ShowIssueEditorModal()
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return visible
	})
	if userCalls.Load() != 0 || projectCalls.Load() != 0 || stateCalls.Load() != 0 || cycleCalls.Load() != 0 {
		t.Fatalf("same-team cached metadata was refetched: users:%d projects:%d states:%d cycles:%d", userCalls.Load(), projectCalls.Load(), stateCalls.Load(), cycleCalls.Load())
	}
}

func TestIssueEditorIntegrationDropsSameIssueStaleTeamMetadata(t *testing.T) {
	issueA := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-a", Title: "Team A", StateID: "state-a"}
	app := issueEditorIntegrationApp(t, "", issueA)
	app.clearIssueEditorTeamMetadata()
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	var queueEvents atomic.Int32
	app.queueUpdateDraw = func(f func()) {
		f()
		queueEvents.Add(1)
	}
	app.fetchUsersFunc = func(_ context.Context, teamID string) ([]linearapi.User, error) {
		if teamID == "team-a" {
			select {
			case <-startedA:
			default:
				close(startedA)
			}
			<-releaseA
			return []linearapi.User{{ID: "team-a-user", Name: "Team A"}}, nil
		}
		return []linearapi.User{{ID: "team-b-user", Name: "Team B"}}, nil
	}
	app.fetchProjectsFunc = func(_ context.Context, teamID string) ([]linearapi.Project, error) {
		return []linearapi.Project{{ID: teamID + "-project", Name: teamID, TeamID: teamID}}, nil
	}
	app.fetchWorkflowStatesFunc = func(_ context.Context, teamID string) ([]linearapi.WorkflowState, error) {
		return []linearapi.WorkflowState{{ID: "state-" + teamID[len(teamID)-1:], Name: teamID}}, nil
	}
	app.fetchCyclesFunc = func(_ context.Context, teamID string) ([]linearapi.Cycle, error) {
		return []linearapi.Cycle{{ID: teamID + "-cycle", Name: teamID}}, nil
	}
	app.fetchIssueLabelsFunc = func(_ context.Context, teamID string) ([]linearapi.IssueLabel, error) {
		return []linearapi.IssueLabel{{ID: teamID + "-label", Name: teamID}}, nil
	}

	app.ShowIssueEditorModal()
	select {
	case <-startedA:
	case <-time.After(time.Second):
		t.Fatal("team A metadata fetch did not start")
	}

	issueB := issueA
	issueB.TeamID = "team-b"
	issueB.Title = "Team B"
	issueB.StateID = "state-b"
	workspaceUIRead(app, func() {
		app.selectedNavigation = &NavigationNode{ID: "team-b", TeamID: "team-b", IsTeam: true, Text: "Team B"}
		app.issuesMu.Lock()
		app.selectedIssue = &issueB
		app.issuesMu.Unlock()
	})
	app.ShowIssueEditorModal()
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return visible
	})
	beforeRelease := queueEvents.Load()
	close(releaseA)
	waitForCondition(t, time.Second, func() bool { return queueEvents.Load() > beforeRelease })
	workspaceUIRead(app, func() {
		if app.issueEditorModal.values.IssueID != issueB.ID || app.issueEditorModal.values.StateID != issueB.StateID {
			t.Fatalf("stale team A metadata replaced team B editor values: %#v", app.issueEditorModal.values)
		}
	})
}

func TestIssueEditorTeamMetadataDropsStalePreload(t *testing.T) {
	app := issueEditorIntegrationApp(t, "", linearapi.Issue{ID: "issue-1", TeamID: "team-b", Title: "Title"})
	generationA := app.teamMetadataGeneration.Add(1)
	generationB := app.teamMetadataGeneration.Add(1)
	if !app.publishIssueEditorTeamMetadata("team-b", generationB,
		[]linearapi.User{{ID: "team-b-user"}},
		[]linearapi.Project{{ID: "team-b-project", TeamID: "team-b"}},
		[]linearapi.WorkflowState{{ID: "state-b"}},
		[]linearapi.Cycle{{ID: "team-b-cycle"}},
	) {
		t.Fatal("team B metadata publish was rejected")
	}
	if app.publishIssueEditorTeamMetadata("team-a", generationA,
		[]linearapi.User{{ID: "team-a-user"}},
		[]linearapi.Project{{ID: "team-a-project", TeamID: "team-a"}},
		[]linearapi.WorkflowState{{ID: "state-a"}},
		[]linearapi.Cycle{{ID: "team-a-cycle"}},
	) {
		t.Fatal("stale team A metadata publish was accepted")
	}
	metadata := app.issueEditorTeamMetadataFor("team-b")
	if len(metadata.users) != 1 || metadata.users[0].ID != "team-b-user" || metadata.teamID != "team-b" {
		t.Fatalf("stale preload replaced team B metadata: %#v", metadata)
	}
}

func TestIssueEditorIntegrationSaveUpdatesAllFieldsAndRefreshesOnce(t *testing.T) {
	for _, mode := range []string{"", ""} {
		t.Run(mode, func(t *testing.T) {
			issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", Description: "Body", StateID: "state-1", AssigneeID: "user-1", Priority: 1, ProjectID: "project-1", Cycle: &linearapi.CycleRef{ID: "cycle-1", Name: "Launch", Number: 12}, Labels: []linearapi.IssueLabel{{ID: "label-1", Name: "bug"}}}
			app := issueEditorIntegrationApp(t, mode, issue)
			var updateCalls atomic.Int32
			var refreshCalls atomic.Int32
			var got linearapi.UpdateIssueInput
			var gotMu sync.Mutex
			app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
				updateCalls.Add(1)
				gotMu.Lock()
				got = input
				gotMu.Unlock()
				return issue, nil
			}
			app.fetchIssuesPage = func(_ context.Context, _ linearapi.FetchIssuesParams, _ *string) (linearapi.IssuePage, error) {
				refreshCalls.Add(1)
				return linearapi.IssuePage{Issues: []linearapi.Issue{issue}}, nil
			}
			refreshDone := installRefreshCompletionHook(app)

			app.ShowIssueEditorModal()
			waitForCondition(t, time.Second, func() bool {
				var visible bool
				workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
				return visible
			})
			workspaceUIRead(app, func() {
				editor := app.issueEditorModal
				editor.Title.SetText("Updated title")
				editor.Description.SetText("Updated body", false)
				editor.Status.SetCurrentOption(1)
				editor.Assignee.SetCurrentOption(0)
				editor.Priority.SetCurrentOption(4)
				editor.Project.SetCurrentOption(0)
				editor.Cycle.SetCurrentOption(0)
				editor.values.LabelIDs = nil
				editor.save()
			})

			waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 1 })
			waitForRefreshCompletion(t, refreshDone)
			if gotCount := updateCalls.Load(); gotCount != 1 {
				t.Fatalf("update calls = %d, want 1", gotCount)
			}
			if gotCount := refreshCalls.Load(); gotCount != 1 {
				t.Fatalf("refresh fetch calls = %d, want 1", gotCount)
			}
			gotMu.Lock()
			input := got
			gotMu.Unlock()
			if input.ID != issue.ID || input.Title == nil || *input.Title != "Updated title" || input.Description == nil || *input.Description != "Updated body" || input.StateID == nil || *input.StateID != "state-2" || input.AssigneeID == nil || *input.AssigneeID != "" || input.Priority == nil || *input.Priority != 4 || input.ProjectID == nil || *input.ProjectID != "" || input.CycleID == nil || *input.CycleID != "" {
				t.Fatalf("all-field update input = %+v, want complete values", input)
			}
			if input.LabelIDs == nil || len(*input.LabelIDs) != 0 {
				t.Fatalf("LabelIDs = %#v, want non-nil empty slice", input.LabelIDs)
			}
		})
	}
}

func TestIssueEditorIntegrationUpdateErrorDoesNotRefresh(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("update failed")
	}
	var refreshCalls atomic.Int32
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	app.ShowIssueEditorModal()
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return visible
	})
	workspaceUIRead(app, func() { app.issueEditorModal.save() })
	waitForCondition(t, time.Second, func() bool {
		var status string
		workspaceUIRead(app, func() { status = app.statusBar.GetText(true) })
		return containsText(status, "update failed")
	})
	if refreshCalls.Load() != 0 {
		t.Fatalf("refresh fetch calls = %d, want 0 after update error", refreshCalls.Load())
	}
}

func TestIssueEditorIntegrationDropsStaleSelection(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	started := make(chan struct{})
	release := make(chan struct{})
	app.fetchIssueLabelsFunc = func(context.Context, string) ([]linearapi.IssueLabel, error) {
		close(started)
		<-release
		return nil, nil
	}
	app.ShowIssueEditorModal()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("label fetch did not start")
	}
	other := issue
	other.ID = "issue-2"
	app.issuesMu.Lock()
	app.selectedIssue = &other
	app.issuesMu.Unlock()
	close(release)
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return !visible
	})
}

func containsText(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
