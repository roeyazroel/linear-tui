package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestIssueEditorSaveIsNonblockingAndSingleFlight(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var updateCalls atomic.Int32
	var refreshCalls atomic.Int32
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		updateCalls.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return issue, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{Issues: []linearapi.Issue{issue}}, nil
	}
	refreshDone := installRefreshCompletionHook(app)

	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() {
		app.issueEditorModal.Title.SetText("Saving draft")
		app.issueEditorModal.save()
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("issue update did not start")
	}

	workspaceUIRead(app, func() {
		if !app.pages.HasPage("issue_editor") {
			t.Fatal("save submission closed issue editor before completion")
		}
		if !strings.Contains(strings.ToLower(app.statusBar.GetText(true)), "saving") {
			t.Fatalf("saving status = %q, want saving indicator", app.statusBar.GetText(true))
		}
		app.issueEditorModal.save()
	})
	if got := updateCalls.Load(); got != 1 {
		t.Fatalf("duplicate save update calls = %d, want 1", got)
	}

	close(release)
	waitForRefreshCompletion(t, refreshDone)
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("successful save refresh calls = %d, want 1", got)
	}
	workspaceUIRead(app, func() {
		if app.pages.HasPage("issue_editor") {
			t.Fatal("successful save left issue editor visible")
		}
	})
}

func TestIssueEditorSaveErrorPreservesDraftAndRetry(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", Description: "Body", StateID: "state-1", AssigneeID: "user-1", Priority: 1, ProjectID: "project-1", Cycle: &linearapi.CycleRef{ID: "cycle-1", Name: "Launch", Number: 12}, Labels: []linearapi.IssueLabel{{ID: "label-1", Name: "bug"}}}
	app := issueEditorIntegrationApp(t, "", issue)
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	var calls atomic.Int32
	var refreshCalls atomic.Int32
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			<-firstRelease
			return linearapi.Issue{}, errors.New("update failed")
		}
		return issue, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{Issues: []linearapi.Issue{issue}}, nil
	}
	refreshDone := installRefreshCompletionHook(app)

	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() {
		editor := app.issueEditorModal
		editor.Title.SetText("Edited title")
		editor.Description.SetText("Edited description", false)
		editor.Status.SetCurrentOption(1)
		editor.Assignee.SetCurrentOption(0)
		editor.Priority.SetCurrentOption(4)
		editor.Project.SetCurrentOption(0)
		editor.Cycle.SetCurrentOption(0)
		editor.values.LabelIDs = []string{"label-1", "label-2"}
		editor.updateLabelsSummary()
		editor.save()
	})
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first issue update did not start")
	}
	close(firstRelease)
	waitForCondition(t, time.Second, func() bool {
		var status string
		workspaceUIRead(app, func() { status = app.statusBar.GetText(true) })
		return containsText(status, "update failed")
	})

	workspaceUIRead(app, func() {
		editor := app.issueEditorModal
		if !app.pages.HasPage("issue_editor") {
			t.Fatal("error closed issue editor")
		}
		if editor.Title.GetText() != "Edited title" || editor.Description.GetText() != "Edited description" {
			t.Fatalf("draft text after error = (%q, %q)", editor.Title.GetText(), editor.Description.GetText())
		}
		if index, _ := editor.Status.GetCurrentOption(); index != 1 {
			t.Fatalf("status selection after error = %d, want 1", index)
		}
		if index, _ := editor.Assignee.GetCurrentOption(); index != 0 {
			t.Fatalf("assignee selection after error = %d, want clear option", index)
		}
		if index, _ := editor.Priority.GetCurrentOption(); index != 4 {
			t.Fatalf("priority selection after error = %d, want 4", index)
		}
		if index, _ := editor.Project.GetCurrentOption(); index != 0 {
			t.Fatalf("project selection after error = %d, want clear option", index)
		}
		if index, _ := editor.Cycle.GetCurrentOption(); index != 0 {
			t.Fatalf("cycle selection after error = %d, want clear option", index)
		}
		if got := editor.values.LabelIDs; len(got) != 2 || got[0] != "label-1" || got[1] != "label-2" {
			t.Fatalf("labels after error = %#v, want both draft labels", got)
		}
		editor.save()
	})
	waitForCondition(t, time.Second, func() bool { return calls.Load() == 2 })
	waitForRefreshCompletion(t, refreshDone)
	if got := calls.Load(); got != 2 {
		t.Fatalf("retry update calls = %d, want 2", got)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("retry refresh calls = %d, want 1", got)
	}
}

func TestIssueEditorSaveDropsStaleNavigationCompletion(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	app.preloadTeamMetadataFunc = func(string) {}
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	var refreshCalls atomic.Int32
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		close(started)
		<-release
		close(completed)
		return issue, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	refreshDone := installRefreshCompletionHook(app)
	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() { app.issueEditorModal.save() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("issue update did not start")
	}
	workspaceUIRead(app, func() {
		app.onNavigationSelected(&NavigationNode{ID: "team-2", TeamID: "team-2", IsTeam: true, Text: "Other"})
	})
	waitForRefreshCompletion(t, refreshDone)
	refreshesAfterNavigation := refreshCalls.Load()
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("stale navigation update did not complete")
	}
	assertNoIssueEditorSuccessStatus(t, app)
	if got := refreshCalls.Load(); got != refreshesAfterNavigation {
		t.Fatalf("stale navigation completion refresh calls = %d, want %d", got, refreshesAfterNavigation)
	}
}

func TestIssueEditorSaveDropsStaleSelectionCompletion(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	other := issue
	other.ID = "issue-2"
	other.Identifier = "ENG-2"
	other.Title = "Other issue"
	app := issueEditorIntegrationApp(t, "", issue)
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	var refreshCalls atomic.Int32
	app.fetchIssueByID = func(_ context.Context, id string) (linearapi.Issue, error) {
		if id == other.ID {
			return other, nil
		}
		return issue, nil
	}
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		close(started)
		<-release
		close(completed)
		return issue, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() { app.issueEditorModal.save() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("issue update did not start")
	}
	workspaceUIRead(app, func() { app.onIssueSelected(other) })
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("stale selection update did not complete")
	}
	assertNoIssueEditorSuccessStatus(t, app)
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("stale selection completion refresh calls = %d, want 0", got)
	}
	workspaceUIRead(app, func() {
		if selected := app.GetSelectedIssue(); selected == nil || selected.ID != other.ID {
			t.Fatalf("stale selection completion reselected old issue: %#v", selected)
		}
	})
}

func TestIssueEditorSaveDropsStaleCloseAndNewSessionCompletion(t *testing.T) {
	issue := linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", TeamID: "team-1", Title: "Title", StateID: "state-1"}
	app := issueEditorIntegrationApp(t, "", issue)
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	var refreshCalls atomic.Int32
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		close(started)
		<-release
		close(completed)
		return issue, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() { app.issueEditorModal.save() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("issue update did not start")
	}
	workspaceUIRead(app, func() { app.issueEditorModal.Hide() })
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("stale closed-editor update did not complete")
	}
	assertNoIssueEditorSuccessStatus(t, app)
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("stale closed-editor completion refresh calls = %d, want 0", got)
	}

	// Start a fresh session while the old request is still blocked and verify
	// the old completion cannot overwrite it.
	started = make(chan struct{})
	release = make(chan struct{})
	completed = make(chan struct{})
	var oldCall atomic.Bool
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		if !oldCall.Swap(true) {
			close(started)
			<-release
			close(completed)
		}
		return issue, nil
	}
	app.ShowIssueEditorModal()
	waitForIssueEditorVisible(t, app)
	workspaceUIRead(app, func() { app.issueEditorModal.save() })
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("new-session predecessor update did not start")
	}
	other := issue
	other.ID = "issue-2"
	other.Identifier = "ENG-2"
	other.Title = "New session"
	workspaceUIRead(app, func() {
		app.issuesMu.Lock()
		app.selectedIssue = &other
		app.issuesMu.Unlock()
	})
	app.ShowIssueEditorModal()
	waitForCondition(t, time.Second, func() bool {
		var currentID string
		workspaceUIRead(app, func() {
			if app.issueEditorModal != nil {
				currentID = app.issueEditorModal.values.IssueID
			}
		})
		return currentID == other.ID
	})
	close(release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("stale new-session update did not complete")
	}
	assertNoIssueEditorSuccessStatus(t, app)
	workspaceUIRead(app, func() {
		if app.issueEditorModal.values.IssueID != other.ID || app.issueEditorModal.Title.GetText() != other.Title {
			t.Fatalf("old save replaced new editor session: %#v title=%q", app.issueEditorModal.values, app.issueEditorModal.Title.GetText())
		}
	})
}

func assertNoIssueEditorSuccessStatus(t *testing.T, app *App) {
	t.Helper()
	workspaceUIRead(app, func() {
		if strings.Contains(strings.ToLower(app.statusMessage), "updated issue") {
			t.Fatalf("stale issue save changed status to %q", app.statusMessage)
		}
	})
}

func waitForIssueEditorVisible(t *testing.T, app *App) {
	t.Helper()
	waitForCondition(t, time.Second, func() bool {
		var visible bool
		workspaceUIRead(app, func() { visible = app.pages.HasPage("issue_editor") })
		return visible
	})
}
