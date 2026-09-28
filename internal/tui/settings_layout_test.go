package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestSettingsFormDoesNotPaintPastModalBorder(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "terminal", width: 167, height: 47},
		{name: "narrow terminal", width: 104, height: 28},
		{name: "compact terminal", width: 77, height: 40},
	} {
		t.Run(size.name, func(t *testing.T) {
			app := NewApp(&linearapi.Client{}, config.Config{}, nil)
			modal := NewSettingsModal(app)
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			defer screen.Fini()
			screen.SetSize(size.width, size.height)
			app.pages.SetRect(0, 0, size.width, size.height)
			modal.Show()
			for _, field := range []*tview.InputField{modal.endpointField, modal.logFileField, modal.agentWorkspaceField, modal.defaultTeamField, modal.defaultProjectField} {
				field.SetText(strings.Repeat("x", 80))
			}
			modal.modal.SetRect(0, 0, size.width, size.height)
			modal.modal.Draw(screen)

			x, y, width, height := modal.modalContent.GetRect()
			if x < 0 || y < 0 || x+width > size.width || y+height > size.height {
				t.Fatalf("modal border rect = (%d,%d %dx%d), screen = %dx%d", x, y, width, height, size.width, size.height)
			}
			for row := y + 1; row < y+height-1; row++ {
				for column := x + width; column < size.width; column++ {
					text, _, _ := screen.Get(column, row)
					if text != "" && text != " " && text != "\x00" {
						t.Fatalf("settings painted %q outside right border at (%d,%d); border=(%d,%d %dx%d)", text, column, row, x, y, width, height)
					}
				}
			}
		})
	}
}
