package tui

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newUXTestApp() *App {
	app := NewApp(&linearapi.Client{}, config.Config{
		PageSize: 1,
		CacheTTL: time.Minute,
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.teamUsers = []linearapi.User{{ID: "user-1", Name: "Test User"}}
	app.teamCycles = []linearapi.Cycle{{ID: "cycle-1", Name: "Test Cycle", Number: 1}}
	return app
}

func TestPaletteController_FilterCommandsMatchesAllQueryTokens(t *testing.T) {
	commands := []Command{
		{ID: "sort_updated", Title: "Sort by updated", Keywords: []string{"sort", "updated", "recent"}},
		{ID: "sort_priority", Title: "Sort by priority", Keywords: []string{"sort", "priority", "urgent"}},
	}
	pc := NewPaletteController(commands)

	pc.SetQuery("sort priority")

	filtered := pc.Filtered()
	if len(filtered) != 1 {
		t.Fatalf("Filtered() length = %d, want 1", len(filtered))
	}
	if filtered[0].ID != "sort_priority" {
		t.Fatalf("Filtered()[0].ID = %q, want sort_priority", filtered[0].ID)
	}
}

func TestOpenSearchPaletteUsesSearchSpecificChrome(t *testing.T) {
	app := newUXTestApp()

	app.openSearchPalette()

	if got := app.paletteModalContent.GetTitle(); got != " Search Issues " {
		t.Fatalf("palette title = %q, want %q", got, " Search Issues ")
	}
	if got := app.paletteInput.GetLabel(); got != "/ " {
		t.Fatalf("palette input label = %q, want %q", got, "/ ")
	}
}

func TestSettingsModalShowsAndBuildsSearchDebounceSetting(t *testing.T) {
	app := newUXTestApp()
	app.config.SearchDebounce = 450 * time.Millisecond
	modal := app.settingsModal

	modal.Show()
	defer modal.Hide()

	if got := modal.searchDebounceField.GetText(); got != "450ms" {
		t.Fatalf("search debounce field = %q, want 450ms", got)
	}

	modal.searchDebounceField.SetText("600ms")
	settings, err := modal.settingsFromForm()
	if err != nil {
		t.Fatalf("settingsFromForm() error: %v", err)
	}
	if settings.SearchDebounce != "600ms" {
		t.Fatalf("SearchDebounce = %q, want 600ms", settings.SearchDebounce)
	}
}

func TestSettingsModalShowsAndBuildsDefaultNavigationSettings(t *testing.T) {
	app := newUXTestApp()
	app.config.DefaultTeam = "NEX"
	app.config.DefaultProject = "Website"
	modal := app.settingsModal

	modal.Show()
	defer modal.Hide()

	if got := modal.defaultTeamField.GetText(); got != "NEX" {
		t.Fatalf("default team field = %q, want NEX", got)
	}
	if got := modal.defaultProjectField.GetText(); got != "Website" {
		t.Fatalf("default project field = %q, want Website", got)
	}

	modal.defaultTeamField.SetText("ENG")
	modal.defaultProjectField.SetText("Mobile App")
	settings, err := modal.settingsFromForm()
	if err != nil {
		t.Fatalf("settingsFromForm() error: %v", err)
	}
	if settings.DefaultTeam != "ENG" {
		t.Fatalf("DefaultTeam = %q, want ENG", settings.DefaultTeam)
	}
	if settings.DefaultProject != "Mobile App" {
		t.Fatalf("DefaultProject = %q, want Mobile App", settings.DefaultProject)
	}
}

func TestCreateIssueModalShowWithOptionsResetsFocusAndShowsParentContext(t *testing.T) {
	app := newUXTestApp()
	modal := app.createIssueModal
	modal.form.SetFocus(2)

	parent := &linearapi.IssueRef{
		ID:         "parent-1",
		Identifier: "LTUI-1",
		Title:      "Parent issue",
	}
	modal.ShowWithOptions(CreateIssueModalOptions{
		TeamID: "team-1",
		Parent: parent,
	}, func(title, description, teamID, projectID, assigneeID, cycleID string, priority int) {})

	focusedItem, _ := modal.form.GetFocusedItemIndex()
	if focusedItem != 0 {
		t.Fatalf("focused form item = %d, want 0 for Title", focusedItem)
	}
	if got := modal.headerView.GetText(true); got != "Create Sub-Issue" {
		t.Fatalf("header text = %q, want Create Sub-Issue", got)
	}
	if got := modal.parentView.GetText(true); !strings.Contains(got, "Parent: LTUI-1 - Parent issue") {
		t.Fatalf("parent context = %q, want parent identifier and title", got)
	}
}

func TestCreateIssueModalEscapeClosesOpenDropdownBeforeModal(t *testing.T) {
	app := newUXTestApp()
	modal := app.createIssueModal
	modal.Show("team-1", "", func(title, description, teamID, projectID, assigneeID, cycleID string, priority int) {})
	modal.form.SetFocus(2)
	openDropdownForTest(t, modal.assigneeField)

	modal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if modal.assigneeField.IsOpen() {
		t.Fatal("assignee dropdown is still open after Escape")
	}
	if !app.pages.HasPage("create_issue") {
		t.Fatal("create issue modal closed; expected Escape to close only the open dropdown")
	}
}

func TestEditLabelsModalShowsFocusAndTogglesWithSpaceAndT(t *testing.T) {
	app := newUXTestApp()
	modal := app.editLabelsModal
	var saved []string
	labels := []linearapi.IssueLabel{
		{ID: "bug", Name: "Bug"},
		{ID: "feature", Name: "Feature"},
	}
	modal.Show("issue-1", nil, labels, func(_ string, labelIDs []string) {
		saved = append([]string(nil), labelIDs...)
	})

	first, _ := modal.list.GetItemText(0)
	if first != "> ( ) Bug" {
		t.Fatalf("initial first row = %q, want focused unchecked row", first)
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone))
	first, _ = modal.list.GetItemText(0)
	if first != "> (x) Bug" {
		t.Fatalf("after space first row = %q, want focused checked row", first)
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	first, _ = modal.list.GetItemText(0)
	second, _ := modal.list.GetItemText(1)
	if first != "  (x) Bug" || second != "> ( ) Feature" {
		t.Fatalf("rows after moving focus = %q / %q, want focus marker on second row", first, second)
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 't', tcell.ModNone))
	second, _ = modal.list.GetItemText(1)
	if second != "> (x) Feature" {
		t.Fatalf("after t second row = %q, want focused checked row", second)
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	sort.Strings(saved)
	if !reflect.DeepEqual(saved, []string{"bug", "feature"}) {
		t.Fatalf("saved label IDs = %#v, want bug and feature", saved)
	}
}

func TestShowParentIssuePickerExcludesSelectedIssueAndDescendants(t *testing.T) {
	app := newUXTestApp()
	selected := linearapi.Issue{
		ID:         "selected",
		Identifier: "LTUI-1",
		Title:      "Selected",
		Children: []linearapi.IssueChildRef{
			{ID: "child", Identifier: "LTUI-2", Title: "Child"},
		},
	}
	app.issuesMu.Lock()
	app.selectedIssue = &selected
	app.issues = []linearapi.Issue{
		selected,
		{ID: "child", Identifier: "LTUI-2", Title: "Child"},
		{ID: "sibling", Identifier: "LTUI-3", Title: "Sibling"},
	}
	app.issuesMu.Unlock()

	app.ShowParentIssuePicker(func(parentID string) {})

	if len(app.pickerModal.items) != 1 {
		t.Fatalf("picker item count = %d, want 1", len(app.pickerModal.items))
	}
	if got := app.pickerModal.items[0].ID; got != "sibling" {
		t.Fatalf("picker item ID = %q, want sibling", got)
	}
}

func TestDestructiveCommandsOpenConfirmationBeforeMutation(t *testing.T) {
	app := newUXTestApp()
	parent := &linearapi.IssueRef{ID: "parent-1", Identifier: "LTUI-1", Title: "Parent"}
	issue := linearapi.Issue{ID: "issue-1", Identifier: "LTUI-2", Title: "Child", Parent: parent}
	app.issuesMu.Lock()
	app.selectedIssue = &issue
	app.issuesMu.Unlock()

	archive := findCommandByID(DefaultCommands(app), "archive")
	if archive == nil {
		t.Fatal("archive command not found")
	}
	archive.Run(app)
	if !app.pages.HasPage("confirmation") {
		t.Fatal("archive command did not open confirmation modal")
	}

	app.confirmationModal.Hide()
	removeParent := findCommandByID(DefaultCommands(app), "remove_parent")
	if removeParent == nil {
		t.Fatal("remove_parent command not found")
	}
	removeParent.Run(app)
	if !app.pages.HasPage("confirmation") {
		t.Fatal("remove parent command did not open confirmation modal")
	}
}

func TestNoOpCommandsShowStatusFeedback(t *testing.T) {
	app := newUXTestApp()

	openBrowser := findCommandByID(DefaultCommands(app), "open_browser")
	if openBrowser == nil {
		t.Fatal("open_browser command not found")
	}
	openBrowser.Run(app)
	if got := app.statusBar.GetText(true); !strings.Contains(got, "No issue selected") {
		t.Fatalf("status after open_browser without issue = %q, want no issue feedback", got)
	}

	app.issuesMu.Lock()
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "LTUI-1", Title: "No parent"}
	app.issuesMu.Unlock()
	viewParent := findCommandByID(DefaultCommands(app), "view_parent")
	if viewParent == nil {
		t.Fatal("view_parent command not found")
	}
	viewParent.Run(app)
	if got := app.statusBar.GetText(true); !strings.Contains(got, "No parent issue") {
		t.Fatalf("status after view_parent without parent = %q, want no parent feedback", got)
	}
}

func TestAgentOutputModalFailureSetsErrorStatusAndFinalSummary(t *testing.T) {
	app := newUXTestApp()
	modal := app.agentOutputModal
	modal.Show(" Cursor Output ", func() {})

	modal.FailRun(fmt.Errorf("agent exited: exit status 1: invalid model"))

	if got := modal.statusView.GetText(true); !strings.Contains(got, "Status: Error") {
		t.Fatalf("status = %q, want error status", got)
	}
	final := modal.finalView.GetText(true)
	if !strings.Contains(final, "Agent run failed") {
		t.Fatalf("final output = %q, want failure summary", final)
	}
	if !strings.Contains(final, "Check agent provider/model settings") {
		t.Fatalf("final output = %q, want model/provider guidance", final)
	}
}

func openDropdownForTest(t *testing.T, dropdown interface {
	InputHandler() func(*tcell.EventKey, func(tview.Primitive))
	IsOpen() bool
}) {
	t.Helper()
	handler := dropdown.InputHandler()
	if handler == nil {
		t.Fatal("dropdown input handler is nil")
	}
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
	if !dropdown.IsOpen() {
		t.Fatal("dropdown did not open")
	}
}
