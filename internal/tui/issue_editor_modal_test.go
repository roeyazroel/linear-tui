package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newIssueEditorModalTestApp(mode string) *App {
	return NewApp(&linearapi.Client{}, config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		LinearAPIKey:   "test-key",
		APIEndpoint:    config.DefaultAPIEndpoint,
		LogLevel:       config.DefaultLogLevel,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
	}, nil)
}

func issueEditorModalOptions() IssueEditorOptions {
	return IssueEditorOptions{
		Values: IssueEditorValues{
			IssueID:     "issue-1",
			Title:       "Initial title",
			Description: "Initial description",
			StateID:     "state-2",
			AssigneeID:  "user-2",
			Priority:    2,
			ProjectID:   "project-1",
			CycleID:     "cycle-2",
			LabelIDs:    []string{"label-2"},
		},
		Statuses:  []IssueEditorOption{{ID: "state-1", Label: "Todo"}, {ID: "state-2", Label: "In progress"}},
		Assignees: []IssueEditorOption{{ID: "user-1", Label: "Ada"}, {ID: "user-2", Label: "Grace"}},
		Projects:  []IssueEditorOption{{ID: "project-1", Label: "CLI"}},
		Cycles:    []IssueEditorOption{{ID: "cycle-1", Label: "Current"}, {ID: "cycle-2", Label: "Next"}},
		Labels:    []IssueEditorOption{{ID: "label-1", Label: "bug"}, {ID: "label-2", Label: "urgent"}},
	}
}

func TestIssueEditorModalInitialValuesAndOptions(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	editor.Show(issueEditorModalOptions(), nil)

	if got := editor.Title.GetText(); got != "Initial title" {
		t.Fatalf("title = %q, want initial title", got)
	}
	if got := editor.Description.GetText(); got != "Initial description" {
		t.Fatalf("description = %q, want initial description", got)
	}
	if idx, _ := editor.Status.GetCurrentOption(); idx != 1 {
		t.Fatalf("status index = %d, want 1", idx)
	}
	if idx, _ := editor.Assignee.GetCurrentOption(); idx != 2 {
		t.Fatalf("assignee index = %d, want 2 (clear option first)", idx)
	}
	if idx, _ := editor.Priority.GetCurrentOption(); idx != 2 {
		t.Fatalf("priority index = %d, want 2", idx)
	}
	if idx, _ := editor.Project.GetCurrentOption(); idx != 1 {
		t.Fatalf("project index = %d, want 1 (clear option first)", idx)
	}
	if idx, _ := editor.Cycle.GetCurrentOption(); idx != 2 {
		t.Fatalf("cycle index = %d, want 2 (clear option first)", idx)
	}
	if got := editor.LabelsSummary.GetText(true); !strings.Contains(got, "urgent") {
		t.Fatalf("labels summary = %q, want selected label", got)
	}
	if !app.pages.HasPage("issue_editor") {
		t.Fatal("Show did not add issue_editor page")
	}
}

func TestIssueEditorModalClearOptionsAndPriorityMapping(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	editor.Show(issueEditorModalOptions(), nil)

	if _, got := editor.Assignee.GetCurrentOption(); got == "" {
		t.Fatal("assignee should expose a visible clear option")
	}
	if _, got := editor.Project.GetCurrentOption(); got == "" {
		t.Fatal("project should expose a visible clear option")
	}
	if _, got := editor.Cycle.GetCurrentOption(); got == "" {
		t.Fatal("cycle should expose a visible clear option")
	}
	if got := editor.Priority.GetOptionCount(); got != 5 {
		t.Fatalf("priority option count = %d, want 5", got)
	}
	want := []string{"No priority", "Urgent", "High", "Normal", "Low"}
	for i, expected := range want {
		editor.Priority.SetCurrentOption(i)
		if _, got := editor.Priority.GetCurrentOption(); got != expected {
			t.Errorf("priority[%d] = %q, want %q", i, got, expected)
		}
		if editor.values.Priority != i {
			t.Errorf("priority value after option %d = %d, want %d", i, editor.values.Priority, i)
		}
	}
	editor.Assignee.SetCurrentOption(0)
	editor.Project.SetCurrentOption(0)
	editor.Cycle.SetCurrentOption(0)
	if editor.values.AssigneeID != "" || editor.values.ProjectID != "" || editor.values.CycleID != "" {
		t.Fatalf("clear options did not clear values: %#v", editor.values)
	}
}

func TestIssueEditorModalSaveOutputCopiesLabelsAndHides(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	options := issueEditorModalOptions()
	var saved IssueEditorValues
	var calls int
	editor.Show(options, func(values IssueEditorValues) {
		calls++
		saved = values
	})
	editor.Title.SetText("Saved title")
	editor.Description.SetText("Saved description", true)
	editor.Status.SetCurrentOption(0)
	editor.Priority.SetCurrentOption(4)
	editor.Assignee.SetCurrentOption(0)
	editor.Project.SetCurrentOption(0)
	editor.Cycle.SetCurrentOption(0)
	editor.values.LabelIDs = []string{"label-1", "label-2"}
	editor.save()

	if calls != 1 {
		t.Fatalf("save callback calls = %d, want 1", calls)
	}
	if app.pages.HasPage("issue_editor") {
		t.Fatal("valid save left issue_editor page visible")
	}
	want := IssueEditorValues{
		IssueID:     "issue-1",
		Title:       "Saved title",
		Description: "Saved description",
		StateID:     "state-1",
		Priority:    4,
		LabelIDs:    []string{"label-1", "label-2"},
	}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved values = %#v, want %#v", saved, want)
	}
	saved.LabelIDs[0] = "mutated"
	if editor.values.LabelIDs[0] == "mutated" {
		t.Fatal("saved label IDs alias editor state")
	}
}

func TestIssueEditorModalInvalidTitleKeepsOpenAndReportsError(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	editor.Show(issueEditorModalOptions(), nil)
	editor.Title.SetText("  ")
	editor.save()

	if !app.pages.HasPage("issue_editor") {
		t.Fatal("invalid title closed issue_editor page")
	}
	if !strings.Contains(strings.ToLower(app.statusMessage), "title") {
		t.Fatalf("statusMessage = %q, want title validation error", app.statusMessage)
	}
	if !strings.Contains(strings.ToLower(app.statusBar.GetText(true)), "title") {
		t.Fatalf("status bar = %q, want title validation error", app.statusBar.GetText(true))
	}
}

func TestIssueEditorModalCancelAndRepeatedShowReset(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	calls := 0
	editor.Show(issueEditorModalOptions(), func(IssueEditorValues) { calls++ })
	editor.Title.SetText("unsaved")
	editor.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if app.pages.HasPage("issue_editor") {
		t.Fatal("Esc did not hide issue editor")
	}
	if calls != 0 {
		t.Fatalf("cancel callback calls = %d, want 0", calls)
	}

	next := IssueEditorOptions{
		Values:   IssueEditorValues{IssueID: "issue-2", Title: "Next", Description: "Body", Priority: 0},
		Statuses: []IssueEditorOption{{ID: "state-new", Label: "New"}},
	}
	editor.Show(next, nil)
	if editor.Title.GetText() != "Next" || editor.Description.GetText() != "Body" {
		t.Fatalf("repeated Show retained stale text: title=%q description=%q", editor.Title.GetText(), editor.Description.GetText())
	}
	if editor.values.IssueID != "issue-2" || editor.values.StateID != "state-new" {
		t.Fatalf("repeated Show retained stale values: %#v", editor.values)
	}
	if editor.Assignee.GetOptionCount() != 1 || editor.Project.GetOptionCount() != 1 || editor.Cycle.GetOptionCount() != 1 {
		t.Fatalf("repeated Show retained stale clear options: assignee=%d project=%d cycle=%d", editor.Assignee.GetOptionCount(), editor.Project.GetOptionCount(), editor.Cycle.GetOptionCount())
	}
}

func TestIssueEditorModalLabelsRoundTripRestoresFocus(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	app.queueUpdateDraw = func(f func()) { f() }
	editor := NewIssueEditorModal(app)
	editor.Show(issueEditorModalOptions(), nil)
	editor.openLabels()
	if !app.pages.HasPage("multi_select") {
		t.Fatal("openLabels did not show multi-select page")
	}
	if editor.Title.GetText() != "Initial title" {
		t.Fatal("opening labels changed unsaved editor fields")
	}
	// Simulate applying the current selection through the reusable picker.
	app.multiSelectModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if app.pages.HasPage("multi_select") {
		t.Fatal("label selection page remained visible after apply")
	}
	if !app.pages.HasPage("issue_editor") {
		t.Fatal("returning from labels removed issue editor page")
	}
	if app.app.GetFocus() != editor.Title {
		t.Fatalf("focus after labels round-trip = %T, want editor title field", app.app.GetFocus())
	}
	if !reflect.DeepEqual(editor.values.LabelIDs, []string{"label-2"}) {
		t.Fatalf("label IDs after round-trip = %#v, want label-2", editor.values.LabelIDs)
	}
}

func TestIssueEditorModalPageLifecycle(t *testing.T) {
	app := newIssueEditorModalTestApp("")
	editor := NewIssueEditorModal(app)
	if editor.GetModal() == nil {
		t.Fatal("GetModal returned nil")
	}
	if editor.GetModal().GetItemCount() != 3 {
		t.Fatalf("modal outer item count = %d, want 3", editor.GetModal().GetItemCount())
	}
	editor.Show(IssueEditorOptions{Values: IssueEditorValues{Title: "x"}}, nil)
	editor.Hide()
	if app.pages.HasPage("issue_editor") {
		t.Fatal("Hide did not remove issue_editor page")
	}
}
