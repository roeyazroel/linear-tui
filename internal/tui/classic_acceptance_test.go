package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestClassicShellDrawsThreePanesAndPaletteScrolls(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{Theme: config.ThemeCatppuccinMocha}, nil)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	app.app.SetScreen(screen).SetRoot(app.pages, true)
	screen.SetSize(120, 30)
	app.issuesMu.Lock()
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "LIN-1", Title: "Classic details"}
	app.issuesMu.Unlock()
	app.updateDetailsView()
	app.mainLayout.SetRect(0, 0, 120, 29)
	app.mainLayout.Draw(screen)
	if drawn := simulationScreenText(screen, 120, 30); !strings.Contains(drawn, "Navigation") || !strings.Contains(drawn, "Issues") || !strings.Contains(drawn, "Details") {
		t.Fatalf("classic shell did not draw all panes: %q", drawn)
	}
	_, _, navWidth, navHeight := app.navigationTree.GetRect()
	_, _, issuesWidth, issuesHeight := app.issuesColumn.GetRect()
	_, _, detailsWidth, detailsHeight := app.detailsView.GetRect()
	if navWidth != 24 || issuesWidth != 60 || detailsWidth != 36 || navHeight == 0 || issuesHeight == 0 || detailsHeight == 0 {
		t.Fatalf("classic pane widths = %d/%d/%d", navWidth, issuesWidth, detailsWidth)
	}
	if app.theme.Background != CatppuccinMochaTheme.Background {
		t.Fatal("Catppuccin theme was not applied")
	}
	commands := make([]Command, 30)
	for i := range commands {
		commands[i] = Command{ID: string(rune('a' + i)), Title: "command"}
	}
	app.paletteCtrl.commands = commands
	app.openPalette()
	app.paletteCtrl.SetCursor(len(commands) - 1)
	app.updatePaletteList()
	app.app.ForceDraw()
	if offset, _ := app.paletteList.GetOffset(); offset == 0 {
		t.Fatal("last palette command did not scroll into view")
	}
}

func simulationScreenText(screen tcell.SimulationScreen, width, height int) string {
	var text strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			main, _, _ := screen.Get(x, y)
			text.WriteString(main)
		}
		text.WriteByte('\n')
	}
	return text.String()
}

func TestClassicShortcutContract(t *testing.T) {
	app := newBulkSelectionTestApp()
	app.currentUser = &linearapi.User{ID: "me"}
	app.updateIssuesData(bulkSelectionIssues())
	app.focusedPane = FocusIssues
	app.activeIssuesSection = IssuesSectionOther
	app.otherIssuesTable.Select(2, 0)
	if findCommandByID(DefaultCommands(app), "archive").ShortcutRune != 'x' || findCommandByID(DefaultCommands(app), "edit_title").ShortcutRune != 'e' || findCommandByID(DefaultCommands(app), "edit_issue").ShortcutRune != 0 {
		t.Fatal("shortcut contract changed")
	}
	globalInput := app.app.GetInputCapture()
	if globalInput == nil {
		t.Fatal("application input capture is nil")
	}
	if event := globalInput(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone)); event == nil {
		t.Fatal("g was consumed before the classic issue table could handle top navigation")
	} else if input := app.otherIssuesTable.GetInputCapture(); input == nil {
		t.Fatal("issue table input capture is nil")
	} else {
		input(event)
	}
	if row, _ := app.otherIssuesTable.GetSelection(); row != 1 {
		t.Fatalf("g row = %d, want top row 1", row)
	}
	if app.handleIssuesKey(tcell.NewEventKey(tcell.KeyRune, 'v', tcell.ModNone)) != nil || !app.markedIssueSelection.IsMarked("issue-2") {
		t.Fatal("v did not mark the selected issue")
	}
	if app.handleIssuesKey(tcell.NewEventKey(tcell.KeyRune, 'V', tcell.ModNone)) != nil {
		t.Fatal("V was not consumed")
	}
	if app.handleIssuesKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone)) != nil || !app.pages.HasPage("confirmation") {
		t.Fatal("x did not open the archive confirmation")
	}

	settings := config.DefaultSettings()
	settings.Keybindings = map[string]string{"edit_title": "e"}
	configured, err := config.ConfigFromSettings("test-key", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}
	titleApp := NewApp(&linearapi.Client{}, configured, nil)
	titleApp.queueUpdateDraw = func(f func()) { f() }
	titleApp.fetchIssueByID = func(_ context.Context, _ string) (linearapi.Issue, error) {
		return linearapi.Issue{}, errors.New("details fetch intentionally skipped for shortcut test")
	}
	titleApp.currentUser = &linearapi.User{ID: "me"}
	titleApp.updateIssuesData(bulkSelectionIssues())
	titleApp.focusedPane = FocusIssues
	titleApp.activeIssuesSection = IssuesSectionOther
	titleApp.otherIssuesTable.Select(1, 0)
	if titleApp.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone)) != nil || !titleApp.pages.HasPage("edit_title") {
		t.Fatal("e did not open the title editor")
	}
	if findCommandByID(DefaultCommands(titleApp), "open_triage") == nil || findCommandByID(DefaultCommands(titleApp), "open_favorites") == nil {
		t.Fatal("palette destinations are unavailable without configured chords")
	}
}
