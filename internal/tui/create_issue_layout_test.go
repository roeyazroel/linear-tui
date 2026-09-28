package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestCreateIssueModalFitsNarrowTerminalAndKeepsFormNavigation(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{Theme: config.DefaultTheme, Density: config.DefaultDensity}, nil)
	modal := app.createIssueModal
	title := modal.form.GetFormItemByLabel("Title").(*tview.InputField)
	description := modal.form.GetFormItemByLabel("Description").(*tview.TextArea)
	title.SetText(strings.Repeat("T", 80))
	description.SetText(strings.Repeat("D", 80), true)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(120, 30)
	modal.GetModal().SetRect(0, 0, 120, 30)
	modal.GetModal().Draw(screen)
	content := modal.GetModal().GetItem(1).(*tview.Flex).GetItem(1).(*tview.Flex)
	if _, _, width, _ := content.GetRect(); width != 75 {
		t.Fatalf("wide create issue modal width = %d, want preferred 75", width)
	}

	screen.SetSize(77, 30)
	screen.Clear()
	modal.GetModal().SetRect(0, 0, 77, 30)
	modal.GetModal().Draw(screen)

	if title.GetText() != strings.Repeat("T", 80) || description.GetText() != strings.Repeat("D", 80) {
		t.Fatal("resizing the modal changed form values")
	}
	content = modal.GetModal().GetItem(1).(*tview.Flex).GetItem(1).(*tview.Flex)
	x, y, width, height := content.GetRect()
	for row := y + 1; row < y+height-1; row++ {
		for column := x + width; column < 77; column++ {
			text, _, _ := screen.Get(column, row)
			if text != "" && text != " " && text != "\x00" {
				t.Fatalf("create issue modal painted %q outside right border at (%d,%d)", text, column, row)
			}
		}
	}

	modal.form.SetFocus(0)
	setFocus := func(p tview.Primitive) { app.app.SetFocus(p) }
	modal.form.Focus(setFocus)
	for want := 0; want <= modal.form.GetFormItemCount()+modal.form.GetButtonCount()-1; want++ {
		item, button := modal.form.GetFocusedItemIndex()
		if want < modal.form.GetFormItemCount() {
			if item != want || button != -1 {
				t.Fatalf("focused form element = (%d,%d), want item %d", item, button, want)
			}
		} else if item != -1 || button != want-modal.form.GetFormItemCount() {
			t.Fatalf("focused form element = (%d,%d), want button %d", item, button, want-modal.form.GetFormItemCount())
		}
		if want < modal.form.GetFormItemCount()+modal.form.GetButtonCount()-1 {
			if handler := modal.form.InputHandler(); handler != nil {
				handler(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone), setFocus)
			}
		}
	}
}
