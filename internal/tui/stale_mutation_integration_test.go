package tui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// drainQueuedUpdates executes callbacks captured from the UI queue. It keeps
// stale-completion tests deterministic without sleeping for goroutine timing.
func drainQueuedUpdates(queueMu *sync.Mutex, queued *[]func()) {
	for {
		queueMu.Lock()
		if len(*queued) == 0 {
			queueMu.Unlock()
			return
		}
		callback := (*queued)[0]
		*queued = (*queued)[1:]
		queueMu.Unlock()
		callback()
	}
}

func TestSavedViewsMutationIsSingleFlightAndDropsClosedCompletion(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.savedViewsModal.Show(app.savedViewsOptions([]linearapi.CustomView{{ID: "view-1", Name: "Open"}}, false, nil))

	var queueMu sync.Mutex
	var queued []func()
	app.queueUpdateDraw = func(callback func()) {
		queueMu.Lock()
		queued = append(queued, callback)
		queueMu.Unlock()
	}

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	secondStarted := make(chan struct{})
	var createCalls atomic.Int32
	var reloadCalls atomic.Int32
	app.listCustomViewsFunc = func(context.Context) ([]linearapi.CustomView, error) {
		reloadCalls.Add(1)
		return []linearapi.CustomView{{ID: "fresh", Name: "Fresh"}}, nil
	}
	app.createCustomViewFunc = func(context.Context, linearapi.CreateCustomViewInput) (linearapi.CustomView, error) {
		call := createCalls.Add(1)
		if call == 1 {
			close(started)
			<-release
			close(finished)
		} else {
			close(secondStarted)
		}
		return linearapi.CustomView{ID: "created"}, nil
	}

	values := SavedViewEditorValues{Name: "Created", FilterJSON: `{}`}
	done := make(chan struct{})
	go func() {
		app.createSavedView(values)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("saved-view create blocked on mutation")
	}
	<-started

	// A second mutation must not overlap the first one.
	app.createSavedView(SavedViewEditorValues{Name: "Overlapping", FilterJSON: `{}`})
	select {
	case <-secondStarted:
		t.Fatal("overlapping saved-view mutation was accepted")
	case <-time.After(50 * time.Millisecond):
	}

	// Reopening the manager creates a newer view context. The old completion
	// must not reload or alter the reopened manager.
	app.savedViewsModal.Hide()
	app.savedViewsModal.Show(app.savedViewsOptions(nil, false, nil))
	close(release)
	<-finished
	drainQueuedUpdates(&queueMu, &queued)
	if got := reloadCalls.Load(); got != 0 {
		t.Fatalf("stale saved-view completion reloads = %d, want 0", got)
	}
}

func TestBulkIssueActionDropsStaleSelectionAndRejectsOverlap(t *testing.T) {
	app := newBulkSelectionTestApp()
	var queueMu sync.Mutex
	var queued []func()
	app.queueUpdateDraw = func(callback func()) {
		queueMu.Lock()
		queued = append(queued, callback)
		queueMu.Unlock()
	}
	app.updateIssuesData(bulkSelectionIssues())
	app.markedIssueSelection.Toggle("issue-1")
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	var refreshCalls atomic.Int32
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	mutate := func(context.Context, string) error {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
			close(finished)
		} else {
			close(secondStarted)
		}
		return nil
	}

	app.runBulkIssueAction("Archive", []string{"issue-1"}, mutate)
	<-started
	app.runBulkIssueAction("Archive", []string{"issue-1"}, mutate)
	select {
	case <-secondStarted:
		t.Fatal("overlapping bulk mutation was accepted")
	case <-time.After(50 * time.Millisecond):
	}

	// Simulate selecting another issue while the first operation is active.
	app.onIssueSelected(linearapi.Issue{ID: "issue-2", Identifier: "LIN-2"})
	app.markedIssueSelection.Clear()
	app.markedIssueSelection.Toggle("issue-2")
	close(release)
	<-finished
	drainQueuedUpdates(&queueMu, &queued)
	if !app.markedIssueSelection.IsMarked("issue-2") {
		t.Fatal("stale bulk completion cleared a newer selection mark")
	}
	if got := refreshCalls.Load(); got != 0 {
		t.Fatalf("stale bulk completion refreshes = %d, want 0", got)
	}
}

func TestTriageBatchDropsStaleNavigationCompletion(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "LIN-1"}
	app.markedIssueSelection.Toggle("issue-1")

	var queueMu sync.Mutex
	var queued []func()
	app.queueUpdateDraw = func(callback func()) {
		queueMu.Lock()
		queued = append(queued, callback)
		queueMu.Unlock()
	}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	navigationRefreshStarted := make(chan struct{})
	var refreshCalls atomic.Int32
	app.updateIssueFunc = func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		close(started)
		<-release
		close(finished)
		return linearapi.Issue{}, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		if refreshCalls.Add(1) == 1 {
			close(navigationRefreshStarted)
		}
		return linearapi.IssuePage{Issues: []linearapi.Issue{{ID: "issue-2"}}}, nil
	}

	app.runTriageBatch(TriageBatchInput{
		Action:               TriageActionAccept,
		IssueIDs:             []string{"issue-1"},
		DestinationStateID:   "state-started",
		DestinationStateType: "started",
	})
	<-started
	app.onNavigationSelected(&NavigationNode{ID: "all", Text: "All Issues"})
	select {
	case <-navigationRefreshStarted:
	case <-time.After(time.Second):
		t.Fatal("navigation refresh did not start")
	}
	app.markedIssueSelection.Clear()
	app.markedIssueSelection.Toggle("issue-2")
	close(release)
	<-finished
	drainQueuedUpdates(&queueMu, &queued)
	if app.markedIssueSelection.IsMarked("issue-1") {
		t.Fatal("stale triage completion changed the old selection marks")
	}
	if !app.markedIssueSelection.IsMarked("issue-2") {
		t.Fatal("stale triage completion cleared a newer selection mark")
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refreshes after stale triage completion = %d, want navigation refresh only (1)", got)
	}
}

func TestRoadmapProjectUpdateMutationDropsClosedCompletion(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var queueMu sync.Mutex
	var queued []func()
	callbackQueued := make(chan struct{}, 1)
	app.queueUpdateDraw = func(callback func()) {
		queueMu.Lock()
		queued = append(queued, callback)
		queueMu.Unlock()
		callbackQueued <- struct{}{}
	}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	reloadStarted := make(chan struct{})
	var reloadCalls atomic.Int32
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		close(reloadStarted)
		return nil, nil
	}
	app.createProjectUpdateFunc = func(context.Context, linearapi.CreateProjectUpdateInput) (linearapi.ProjectUpdate, error) {
		close(started)
		<-release
		close(finished)
		return linearapi.ProjectUpdate{ID: "update-1"}, nil
	}

	app.showRoadmapCreate("project-1")
	done := make(chan struct{})
	go func() {
		app.textInputModal.onSubmit("Status")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("project-update create blocked on mutation")
	}
	<-started
	app.roadmapView.Hide()
	close(release)
	<-finished
	select {
	case <-callbackQueued:
		drainQueuedUpdates(&queueMu, &queued)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-reloadStarted:
		t.Fatal("closed project-update completion started a roadmap reload")
	case <-time.After(50 * time.Millisecond):
	}
	if got := reloadCalls.Load(); got != 0 {
		t.Fatalf("closed project-update completion reloads = %d, want 0", got)
	}
}
