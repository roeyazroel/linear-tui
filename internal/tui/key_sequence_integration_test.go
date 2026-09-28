package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newKeySequenceIntegrationApp(t *testing.T) *App {
	t.Helper()
	app := NewApp(&linearapi.Client{}, config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
		Keybindings:    map[string]string{"navigate_projects": "g p"},
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.preloadTeamMetadataFunc = func(string) {}
	app.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	app.focusedPane = FocusNavigation
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{}, nil
	}
	return app
}

func TestKeySequenceIntegrationNavigatesConfiguredChord(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	refreshDone := installRefreshCompletionHook(app)
	app.currentUser = &linearapi.User{ID: "user-1", DisplayName: "Ada"}
	app.rebuildNavigationTree([]linearapi.Team{{ID: "team-1", Key: "ENG", Name: "Engineering"}}, nil)
	team := app.findTeamTreeNode("team-1")
	app.populateTeamNodeChildren(team, "team-1", []linearapi.Project{{ID: "project-1", Name: "CLI"}}, nil, []linearapi.Cycle{{ID: "cycle-1", Name: "Launch", Number: 1}})

	capture := app.app.GetInputCapture()
	if capture == nil {
		t.Fatal("app input capture is nil")
	}
	var chordHint string
	workspaceUIRead(app, func() {
		if got := capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got != nil {
			t.Fatalf("first chord key returned %v, want consumed", got)
		}
		chordHint = app.statusBar.GetText(true)
	})
	if !strings.Contains(chordHint, "g") {
		t.Fatalf("pending chord hint missing from status bar: %q", chordHint)
	}
	workspaceUIRead(app, func() {
		if got := capture(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModNone)); got != nil {
			t.Fatalf("completed chord returned %v, want consumed", got)
		}
		if app.selectedNavigation == nil || !app.selectedNavigation.IsProject || app.selectedNavigation.ID != "project-1" {
			t.Fatalf("project destination = %+v, want project-1", app.selectedNavigation)
		}
	})
	waitForRefreshCompletion(t, refreshDone)
}

func TestKeySequenceIntegrationInvalidReprocessesSingleKey(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	var refreshes atomic.Int32
	app.paletteCtrl.commands = append(app.paletteCtrl.commands, Command{ID: "test_refresh", ShortcutRune: '~', Run: func(*App) { refreshes.Add(1) }})
	app.keySequenceDispatcher = mustNewTestKeySequenceDispatcher(t)
	capture := app.app.GetInputCapture()
	capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	if got := capture(tcell.NewEventKey(tcell.KeyRune, '~', tcell.ModNone)); got != nil {
		t.Fatalf("invalid continuation returned %v", got)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("invalid continuation refresh count = %d, want 1", refreshes.Load())
	}
}

func TestKeySequenceIntegrationDoesNotInterceptEditableInput(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	app.textInputModal.Show("Text", "Value: ", "", nil)
	capture := app.app.GetInputCapture()
	if got := capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); got == nil || got.Rune() != 'g' {
		t.Fatalf("editable input g event = %v, want reprocessed rune", got)
	}
	if app.keySequenceDispatcher.Pending() {
		t.Fatal("editable input started a pending chord")
	}
	app.textInputModal.Hide()
}

func TestKeySequenceIntegrationHelpAndPaletteAliases(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	capture := app.app.GetInputCapture()
	if got := capture(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone)); got != nil {
		t.Fatalf("help key returned %v", got)
	}
	if !app.pages.HasPage("key_help") {
		t.Fatal("? did not open contextual help page")
	}
	if got := capture(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatalf("help escape returned %v", got)
	}
	if app.pages.HasPage("key_help") {
		t.Fatal("Esc did not close contextual help page")
	}
	if got := capture(tcell.NewEventKey(tcell.KeyCtrlK, 0, tcell.ModCtrl)); got != nil {
		t.Fatalf("Ctrl-K returned %v", got)
	}
	if app.focusedPane != FocusPalette || !app.pages.HasPage("palette") {
		t.Fatalf("Ctrl-K palette state focused=%v page=%v", app.focusedPane, app.pages.HasPage("palette"))
	}
}

func TestKeySequenceIntegrationUnavailableDestinationReportsStatus(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	app.keySequenceDispatcher = mustNewTestKeySequenceDispatcher(t)
	capture := app.app.GetInputCapture()
	capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	capture(tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone))
	if !strings.Contains(strings.ToLower(app.statusBar.GetText(true)), "inbox") || !strings.Contains(strings.ToLower(app.statusBar.GetText(true)), "unavailable") {
		t.Fatalf("unavailable inbox status = %q", app.statusBar.GetText(true))
	}
}

func TestKeySequenceIntegrationTimeoutReprocessesCurrentKey(t *testing.T) {
	app := newKeySequenceIntegrationApp(t)
	clock := time.Unix(100, 0)
	app.keySequenceClock = func() time.Time { return clock }
	var calls atomic.Int32
	app.paletteCtrl.commands = []Command{{ID: "test", ShortcutRune: 'x', Run: func(*App) { calls.Add(1) }}}
	capture := app.app.GetInputCapture()
	capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
	clock = clock.Add(2 * time.Second)
	if got := capture(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)); got != nil {
		t.Fatalf("timed-out continuation returned %v", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("timed-out continuation calls = %d, want 1", calls.Load())
	}
	if app.keySequenceDispatcher.Pending() {
		t.Fatal("timed-out continuation left dispatcher pending")
	}
}

func TestKeySequenceBindingConfigurationReportsConflicts(t *testing.T) {
	if _, err := buildKeySequenceDispatcher(map[string]string{"open_palette": "g"}); err != nil {
		t.Fatalf("configured global palette key should be reachable: %v", err)
	}
	if _, err := buildKeySequenceDispatcher(map[string]string{"navigate_all": "g a", "navigate_mine": "g a"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Fatalf("sequence duplicate error = %v, want duplicate report", err)
	}
}

func mustNewTestKeySequenceDispatcher(t *testing.T) *KeySequenceDispatcher {
	t.Helper()
	dispatcher, err := NewKeySequenceDispatcher(defaultKeySequenceBindings(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher
}
