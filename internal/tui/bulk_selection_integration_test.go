package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newBulkSelectionTestApp() *App {
	app := NewApp(&linearapi.Client{}, config.Config{
		PageSize: 20,
		CacheTTL: time.Minute,
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.fetchIssueByID = func(_ context.Context, id string) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("details fetch intentionally skipped for selection tests: " + id)
	}
	return app
}

func bulkSelectionIssues() []linearapi.Issue {
	return []linearapi.Issue{
		{ID: "issue-1", Identifier: "LIN-1", Title: "First", State: "Todo", AssigneeID: "me", TeamID: "team-1"},
		{ID: "issue-2", Identifier: "LIN-2", Title: "Second", State: "Todo", AssigneeID: "other", TeamID: "team-1"},
		{ID: "issue-3", Identifier: "LIN-3", Title: "Third", State: "Todo", AssigneeID: "other", TeamID: "team-1"},
	}
}

func TestIssueMarkKeysRenderAndSelectAcrossSections(t *testing.T) {
	app := newBulkSelectionTestApp()
	app.currentUser = &linearapi.User{ID: "me"}
	app.updateIssuesData(bulkSelectionIssues())
	app.focusedPane = FocusIssues

	// Mark the first My Issues row, then extend across its visible section.
	app.activeIssuesSection = IssuesSectionMy
	app.myIssuesTable.Select(1, 0)
	if got := app.handleIssuesKey(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)); got != nil {
		t.Fatalf("v returned event %v, want nil", got)
	}
	if !app.markedIssueSelection.IsMarked("issue-1") {
		t.Fatal("v did not mark cursor issue")
	}
	if got := app.handleIssuesKey(tcell.NewEventKey(tcell.KeyRune, 'V', tcell.ModNone)); got != nil {
		t.Fatalf("V returned event %v, want nil", got)
	}
	if got, want := app.markedIssueSelection.IDs(), []string{"issue-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("V marks = %#v, want %#v for one-row My section", got, want)
	}

	// Ctrl+A covers both currently rendered sections, not just the active one.
	if got := app.handleIssuesKey(tcell.NewEventKey(tcell.KeyCtrlA, 0, tcell.ModNone)); got != nil {
		t.Fatalf("Ctrl+A returned event %v, want nil", got)
	}
	if got, want := app.markedIssueSelection.IDs(), []string{"issue-1", "issue-2", "issue-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Ctrl+A marks = %#v, want %#v", got, want)
	}

	if got := app.statusBar.GetText(true); !strings.Contains(got, "3 marked") {
		t.Fatalf("status bar = %q, want marked count", got)
	}

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(100, 8)
	app.myIssuesTable.SetRect(0, 0, 100, 8)
	app.myIssuesTable.Draw(screen)
	if drawn := simulationScreenText(screen, 100, 8); !strings.Contains(drawn, "(x) LIN-1") {
		t.Fatalf("marked issue indicator was not rendered: %q", drawn)
	}

	app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	screen.Clear()
	app.myIssuesTable.Draw(screen)
	if drawn := simulationScreenText(screen, 100, 8); strings.Contains(drawn, "(x)") {
		t.Fatalf("cleared issue indicator remained rendered: %q", drawn)
	}
}

func TestIssueEscapeClearsMarksBeforeExistingEscapeBehavior(t *testing.T) {
	app := newBulkSelectionTestApp()
	app.updateIssuesData(bulkSelectionIssues())
	app.focusedPane = FocusIssues
	app.searchQuery = "query"
	app.markedIssueSelection.Toggle("issue-1")

	capture := app.app.GetInputCapture()
	if capture == nil {
		t.Fatal("application input capture is nil")
	}
	if got := capture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatalf("Esc returned event %v, want nil", got)
	}
	if app.markedIssueSelection.Count() != 0 {
		t.Fatalf("Esc left marks: %#v", app.markedIssueSelection.IDs())
	}
	if app.searchQuery != "query" {
		t.Fatalf("Esc cleared search together with marks; search = %q", app.searchQuery)
	}
}

func TestEnterOpensParentDetailsAndSpaceExpands(t *testing.T) {
	app := newBulkSelectionTestApp()
	child := linearapi.Issue{ID: "child", Identifier: "LIN-2", Title: "Child", State: "Todo", TeamID: "team-1"}
	parent := linearapi.Issue{ID: "parent", Identifier: "LIN-1", Title: "Parent", State: "Todo", TeamID: "team-1", Children: []linearapi.IssueChildRef{{ID: child.ID, Identifier: child.Identifier, Title: child.Title}}}
	child.Parent = &linearapi.IssueRef{ID: parent.ID, Identifier: parent.Identifier, Title: parent.Title}
	app.updateIssuesData([]linearapi.Issue{parent, child})
	app.focusedPane = FocusIssues
	app.activeIssuesSection = IssuesSectionOther
	app.otherIssuesTable.Select(1, 0)

	inputCapture := app.otherIssuesTable.GetInputCapture()
	if inputCapture == nil {
		t.Fatal("other issue table input capture is nil")
	}
	inputCapture(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if app.focusedPane != FocusDetails {
		t.Fatalf("Enter focused pane = %v, want details", app.focusedPane)
	}
	if app.expandedState[parent.ID] {
		t.Fatal("Enter expanded parent; it must open details")
	}

	app.focusedPane = FocusIssues
	inputCapture(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	if !app.expandedState[parent.ID] {
		t.Fatal("Space did not expand parent")
	}
}

func TestMarkedIssuesSurviveCollapseAndReconcileOnlyStaleLoadedIDs(t *testing.T) {
	app := newBulkSelectionTestApp()
	child := linearapi.Issue{ID: "child", Identifier: "LIN-2", Title: "Child", State: "Todo", TeamID: "team-1"}
	parent := linearapi.Issue{ID: "parent", Identifier: "LIN-1", Title: "Parent", State: "Todo", TeamID: "team-1", Children: []linearapi.IssueChildRef{{ID: child.ID, Identifier: child.Identifier, Title: child.Title}}}
	child.Parent = &linearapi.IssueRef{ID: parent.ID, Identifier: parent.Identifier, Title: parent.Title}
	app.updateIssuesData([]linearapi.Issue{parent, child})
	app.markedIssueSelection.Toggle(child.ID)
	app.toggleIssueExpanded(parent.ID)
	if !app.markedIssueSelection.IsMarked(child.ID) {
		t.Fatal("collapsing parent dropped mark for still-loaded child")
	}
	app.updateIssuesData([]linearapi.Issue{parent})
	if app.markedIssueSelection.IsMarked(child.ID) {
		t.Fatal("reconcile retained stale child mark")
	}
}

func TestResolveIssueTargetsUsesLoadedOrderAndSkipsStaleIDs(t *testing.T) {
	app := newBulkSelectionTestApp()
	issues := bulkSelectionIssues()
	app.issues = issues
	got := app.resolveIssueTargets([]string{"issue-3", "stale", "issue-1", "issue-3"})
	ids := make([]string, 0, len(got))
	for _, issue := range got {
		ids = append(ids, issue.ID)
	}
	if want := []string{"issue-3", "issue-1"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("resolved IDs = %#v, want %#v", ids, want)
	}
}

func TestBulkChangeStatusUsesOnePickerAndOneFinalRefresh(t *testing.T) {
	app := newBulkSelectionTestApp()
	app.updateIssuesData(bulkSelectionIssues())
	app.issuesMu.Lock()
	app.selectedIssue = &app.issues[0]
	app.issuesMu.Unlock()
	app.markedIssueSelection.SelectAllVisible([]string{"issue-1", "issue-2", "issue-3"})

	var updatesMu sync.Mutex
	var updates []linearapi.UpdateIssueInput
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		updatesMu.Lock()
		updates = append(updates, input)
		updatesMu.Unlock()
		return linearapi.Issue{ID: input.ID}, nil
	}
	// Inject the picker seam through the existing cached states and select the
	// first item from the picker callback after the command opens it.
	app.workflowStates = []linearapi.WorkflowState{{ID: "state-done", Name: "Done"}}
	refreshDone := installRefreshCompletionHook(app)
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{Issues: bulkSelectionIssues()}, nil
	}

	command := findCommandByID(DefaultCommands(app), "change_status")
	if command == nil {
		t.Fatal("change_status command not found")
	}
	command.Run(app)
	if !app.pickerActive {
		t.Fatal("change_status did not open picker")
	}
	app.pickerModal.list.SetCurrentItem(0)
	app.pickerModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitForCondition(t, time.Second, func() bool {
		updatesMu.Lock()
		defer updatesMu.Unlock()
		return len(updates) == 3
	})
	waitForRefreshCompletion(t, refreshDone)
	updatesMu.Lock()
	defer updatesMu.Unlock()
	if len(updates) != 3 {
		t.Fatalf("updates = %d, want 3", len(updates))
	}
	for _, input := range updates {
		if input.StateID == nil || *input.StateID != "state-done" {
			t.Fatalf("status update = %#v, want state-done", input)
		}
	}
	if app.markedIssueSelection.Count() != 0 {
		t.Fatalf("successful bulk status left marks: %#v", app.markedIssueSelection.IDs())
	}
}

func TestBulkArchiveRetainsFailedTargetsAndReportsProgress(t *testing.T) {
	app := newBulkSelectionTestApp()
	app.updateIssuesData(bulkSelectionIssues())
	app.issuesMu.Lock()
	app.selectedIssue = &app.issues[0]
	app.issuesMu.Unlock()
	app.markedIssueSelection.SelectAllVisible([]string{"issue-1", "issue-2", "issue-3"})
	var calls atomic.Int32
	app.archiveIssueFunc = func(_ context.Context, issueID string) error {
		calls.Add(1)
		if issueID == "issue-2" {
			return errors.New("permission denied")
		}
		return nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{Issues: bulkSelectionIssues()}, nil
	}
	refreshDone := installRefreshCompletionHook(app)

	command := findCommandByID(DefaultCommands(app), "archive")
	if command == nil {
		t.Fatal("archive command not found")
	}
	if command.ShortcutRune != 'x' {
		t.Fatalf("archive shortcut = %q, want x", command.ShortcutRune)
	}
	command.Run(app)
	if !app.pages.HasPage("confirmation") {
		t.Fatal("archive did not open confirmation")
	}
	app.confirmationModal.Hide()
	app.confirmationModal.onConfirm()
	waitForCondition(t, time.Second, func() bool { return calls.Load() == 3 })
	waitForRefreshCompletion(t, refreshDone)
	if got, want := app.markedIssueSelection.IDs(), []string{"issue-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("failed archive marks = %#v, want %#v", got, want)
	}
	if got := app.statusBar.GetText(true); !strings.Contains(got, "1 failed") || !strings.Contains(got, "2 succeeded") {
		t.Fatalf("archive status = %q, want partial summary", got)
	}
}
