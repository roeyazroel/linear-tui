package tui

import (
	"sync/atomic"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func sendPaletteKeys(t *testing.T, app *App, query string) {
	t.Helper()
	capture := app.app.GetInputCapture()
	if capture == nil {
		t.Fatal("application input capture is nil")
	}
	for _, key := range query {
		if got := capture(tcell.NewEventKey(tcell.KeyRune, key, tcell.ModNone)); got != nil {
			t.Fatalf("palette key %q returned %v, want consumed", key, got)
		}
	}
}

func TestPaletteInputRetainsGlobalShortcutRunes(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	var quits atomic.Int32
	app.quitFunc = func() { quits.Add(1) }
	capture := app.app.GetInputCapture()
	if got := capture(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)); got != nil {
		t.Fatalf("Ctrl-K returned %v, want consumed", got)
	}
	query := "comments/activity:q:"
	sendPaletteKeys(t, app, query)
	if app.focusedPane != FocusPalette || app.paletteCtrl.IsSearchMode() {
		t.Fatalf("palette state = focus:%v search:%v, want command palette", app.focusedPane, app.paletteCtrl.IsSearchMode())
	}
	if got := app.paletteCtrl.Query(); got != query || app.paletteInput.GetText() != query {
		t.Fatalf("palette query = %q input = %q, want %q", got, app.paletteInput.GetText(), query)
	}
	if got := quits.Load(); got != 0 {
		t.Fatalf("quit calls = %d, want 0 while typing palette text", got)
	}
}

func TestPaletteInputRetainsRemappedGlobalShortcutRunesOverWorkspaceViews(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.config.Keybindings = map[string]string{"quit": "z", "search": ";"}
	app.rebuildKeySequenceDispatcher()
	app.reapplyKeybindings()
	var quits atomic.Int32
	app.quitFunc = func() { quits.Add(1) }

	views := []struct {
		name string
		show func()
		hide func()
	}{
		{name: "inbox", show: func() { app.inboxView.Show(InboxViewOptions{}, InboxViewCallbacks{}) }, hide: func() { app.pages.RemovePage(inboxViewPageName) }},
		{name: "saved views", show: func() { app.savedViewsModal.Show(SavedViewsModalOptions{}) }, hide: func() { app.pages.RemovePage(savedViewsPageName) }},
		{name: "comments", show: func() { app.commentsModal.Show(CommentsModalOptions{IssueID: "issue-1"}, CommentsModalCallbacks{}) }, hide: func() { app.pages.RemovePage(commentsModalPageName) }},
		{name: "roadmap", show: func() { app.roadmapView.Show(RoadmapViewOptions{}) }, hide: func() { app.pages.RemovePage(roadmapPageName) }},
	}
	for _, view := range views {
		t.Run(view.name, func(t *testing.T) {
			view.show()
			capture := app.app.GetInputCapture()
			capture(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl))
			query := "comments/activity:z;:"
			sendPaletteKeys(t, app, query)
			if app.focusedPane != FocusPalette || app.paletteCtrl.IsSearchMode() || app.paletteCtrl.Query() != query {
				t.Fatalf("palette state = focus:%v search:%v query:%q", app.focusedPane, app.paletteCtrl.IsSearchMode(), app.paletteCtrl.Query())
			}
			if quits.Load() != 0 {
				t.Fatal("remapped quit ran while typing palette text")
			}
			app.closePalette()
			view.hide()
		})
	}
}
