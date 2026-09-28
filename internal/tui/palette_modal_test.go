package tui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestPaletteModalHeightClampsCompactAndPageBounds(t *testing.T) {
	tests := []struct {
		name            string
		commandCount    int
		spacerLines     int
		availableHeight int
		want            int
	}{
		{name: "minimum", commandCount: 0, spacerLines: 1, want: paletteModalMinHeight},
		{name: "compact maximum", commandCount: 100, spacerLines: 1, want: paletteModalMaxHeight},
		{name: "page height", commandCount: 100, spacerLines: 1, availableHeight: 11, want: 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paletteModalHeight(tt.commandCount, tt.spacerLines, tt.availableHeight); got != tt.want {
				t.Fatalf("paletteModalHeight(%d, %d, %d) = %d, want %d", tt.commandCount, tt.spacerLines, tt.availableHeight, got, tt.want)
			}
		})
	}
}

func TestUpdatePaletteListUsesCompactScrollableViewport(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{}, nil)
	commands := make([]Command, 30)
	for i := range commands {
		commands[i] = Command{ID: fmt.Sprintf("command-%d", i), Title: fmt.Sprintf("Command %d", i)}
	}
	app.paletteCtrl.commands = commands
	app.paletteCtrl.Reset()
	app.focusedPane = FocusPalette

	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(120, 40)
	app.app.SetScreen(screen).SetRoot(app.pages, true)
	app.updatePaletteList()
	app.pages.ShowPage("palette")
	app.pages.SendToFront("palette")
	app.app.ForceDraw()

	x, y, width, modalHeight := app.paletteModalContent.GetRect()
	_ = x
	_ = y
	_ = width
	if modalHeight > 15 {
		t.Fatalf("palette modal height = %d, want compact height <= 15", modalHeight)
	}

	last := len(commands) - 1
	app.paletteCtrl.SetCursor(last)
	app.updatePaletteList()
	app.app.ForceDraw()
	x, y, width, listHeight := app.paletteList.GetRect()
	_ = x
	_ = y
	_ = width
	if listHeight >= len(commands) {
		t.Fatalf("palette list viewport height = %d for %d commands, want a bounded viewport", listHeight, len(commands))
	}
	if got, _ := app.paletteList.GetOffset(); got == 0 {
		t.Fatalf("palette list offset = 0 after selecting item %d in bounded viewport", last)
	}
}

func TestOpenSearchPaletteClampsToShortScreen(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{}, nil)
	screen := tcell.NewSimulationScreen("UTF-8")
	app.app.SetScreen(screen).SetRoot(app.pages, true)
	screen.SetSize(120, 6)
	app.app.ForceDraw()

	app.openSearchPalette()
	app.app.ForceDraw()

	x, y, width, modalHeight := app.paletteModalContent.GetRect()
	_ = x
	_ = y
	_ = width
	if modalHeight > 6 {
		t.Fatalf("search palette height = %d, want short-screen clamp <= 6", modalHeight)
	}
}

func TestUpdatePaletteListPreservesPaletteModeTitle(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{}, nil)
	app.focusedPane = FocusPalette
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(120, 40)
	app.app.SetScreen(screen).SetRoot(app.pages, true)
	app.updatePaletteList()
	app.app.ForceDraw()

	app.paletteCtrl.SetSearchMode(true)
	app.updatePaletteList()
	app.app.ForceDraw()
	if got := app.paletteModalContent.GetTitle(); got != " Search Issues " {
		t.Fatalf("search palette title = %q, want %q", got, " Search Issues ")
	}

	app.paletteCtrl.SetSearchMode(false)
	app.updatePaletteList()
	if got := app.paletteModalContent.GetTitle(); got != " Commands " {
		t.Fatalf("command palette title = %q, want %q", got, " Commands ")
	}
}
