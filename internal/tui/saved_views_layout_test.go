package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestSavedViewsEditorFitsModalAndShowsActions(t *testing.T) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{name: "terminal", width: 167, height: 47},
		{name: "narrow terminal", width: 104, height: 28},
	} {
		t.Run(size.name, func(t *testing.T) {
			app := newSavedViewsModalTestApp("")
			modal := NewSavedViewsModal(app)
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			defer screen.Fini()
			app.app.SetScreen(screen).SetRoot(app.pages, true)
			screen.SetSize(size.width, size.height)

			modal.OpenCreate()
			app.app.SetRoot(modal.editorModal, true)
			modal.EditorName.SetText(strings.Repeat("n", 80))
			modal.EditorDescription.SetText(strings.Repeat("d", 80), true)
			modal.EditorColor.SetText(strings.Repeat("c", 80))
			modal.EditorFilter.SetText(strings.Repeat("f", 80), true)
			app.app.ForceDraw()

			border := savedViewsEditorBorder(t, modal)
			x, y, width, height := border.GetRect()
			if x < 0 || y < 0 || x+width > size.width || y+height > size.height {
				t.Fatalf("editor border rect = (%d,%d %dx%d), screen = %dx%d", x, y, width, height, size.width, size.height)
			}
			for row := y + 1; row < y+height-1; row++ {
				for column := x + width; column < size.width; column++ {
					text, _, _ := screen.Get(column, row)
					if text != "" && text != " " && text != "\x00" {
						t.Fatalf("editor painted %q outside right border at (%d,%d); border=(%d,%d %dx%d)", text, column, row, x, y, width, height)
					}
				}
			}
			text := savedViewsScreenText(screen, size.width, size.height)
			for _, want := range []string{"Save", "Cancel"} {
				if !strings.Contains(text, want) {
					t.Fatalf("editor screen omits %q:\n%s", want, text)
				}
			}

			modal.EditorName.SetText("")
			modal.SaveEditor()
			app.app.ForceDraw()
			if !strings.Contains(savedViewsScreenText(screen, size.width, size.height), "Name is required") {
				t.Fatal("editor validation message is not visible")
			}
		})
	}
}

func savedViewsEditorBorder(t *testing.T, modal *SavedViewsModal) *tview.Flex {
	t.Helper()
	vertical, ok := modal.editorModal.GetItem(1).(*tview.Flex)
	if !ok {
		t.Fatal("editor modal center is not a vertical flex")
	}
	border, ok := vertical.GetItem(1).(*tview.Flex)
	if !ok {
		t.Fatal("editor content is not a flex")
	}
	return border
}

func savedViewsScreenText(screen tcell.SimulationScreen, width, height int) string {
	var text strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell, _, _ := screen.Get(x, y)
			text.WriteString(cell)
		}
		text.WriteByte('\n')
	}
	return text.String()
}
