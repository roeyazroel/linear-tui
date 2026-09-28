package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func configurePaletteRemapForTest(app *App, key string) {
	app.config.Keybindings = map[string]string{"open_palette": key}
	app.rebuildKeySequenceDispatcher()
	app.reapplyKeybindings()
}

func assertConfiguredPaletteOpens(t *testing.T, app *App, event *tcell.EventKey) {
	t.Helper()
	if got := app.app.GetInputCapture()(event); got != nil {
		t.Fatalf("configured palette key returned event %v", got)
	}
	if app.focusedPane != FocusPalette || !app.pages.HasPage("palette") {
		t.Fatalf("configured palette key did not open palette: focus=%v page=%v", app.focusedPane, app.pages.HasPage("palette"))
	}
	app.closePalette()
}

func TestConfiguredPaletteRemapWorksAcrossWorkspacePages(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	configurePaletteRemapForTest(app, ";")
	if !strings.Contains(app.statusBar.GetText(true), ";: palette") {
		t.Fatalf("status bar = %q, want configured palette shortcut", app.statusBar.GetText(true))
	}

	// Main workspace uses the configured action while Ctrl-K remains the alias.
	assertConfiguredPaletteOpens(t, app, tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone))
	if got := app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)); got != nil {
		t.Fatalf("Ctrl-K returned event %v", got)
	}
	if app.focusedPane != FocusPalette {
		t.Fatal("Ctrl-K did not remain a palette alias")
	}
	app.closePalette()

	app.inboxView.Show(InboxViewOptions{}, InboxViewCallbacks{})
	assertConfiguredPaletteOpens(t, app, tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone))
	app.pages.RemovePage(inboxViewPageName)

	app.issuesMu.Lock()
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "ENG-1"}
	app.issuesMu.Unlock()
	app.commentsModal.Show(CommentsModalOptions{IssueID: "issue-1"}, CommentsModalCallbacks{})
	assertConfiguredPaletteOpens(t, app, tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone))
	app.pages.RemovePage(commentsModalPageName)

	app.roadmapView.Show(RoadmapViewOptions{})
	assertConfiguredPaletteOpens(t, app, tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone))
	app.pages.RemovePage(roadmapPageName)

	app.savedViewsModal.Show(SavedViewsModalOptions{})
	assertConfiguredPaletteOpens(t, app, tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone))
}

func TestConfiguredPaletteRemapDoesNotStealEditableInput(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	configurePaletteRemapForTest(app, ";")
	app.textInputModal.Show("Text", "Value: ", "", nil)
	defer app.textInputModal.Hide()

	if got := app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, ';', tcell.ModNone)); got == nil || got.Rune() != ';' {
		t.Fatalf("editable palette key event = %v, want reprocessed rune", got)
	}
	if app.focusedPane == FocusPalette {
		t.Fatal("configured palette key opened palette while editable input was focused")
	}
	if front, _ := app.pages.GetFrontPage(); front == "palette" {
		t.Fatal("configured palette key moved palette page to front while editable input was focused")
	}
}
