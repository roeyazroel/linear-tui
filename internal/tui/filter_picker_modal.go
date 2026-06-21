package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// FilterPickerModal is a picker overlay with type-to-filter search: an input
// field at the top narrows the list as the user types. It is intended for
// selecting a single item from a potentially large list (e.g. projects).
type FilterPickerModal struct {
	app       *App
	modal     *tview.Flex
	input     *tview.InputField
	list      *tview.List
	titleView *tview.TextView
	allItems  []PickerItem
	filtered  []PickerItem
	query     string
	onSelect  func(item PickerItem)
}

// NewFilterPickerModal creates a new filterable picker modal.
func NewFilterPickerModal(app *App) *FilterPickerModal {
	fp := &FilterPickerModal{app: app}

	fp.titleView = tview.NewTextView()
	fp.titleView.SetTextColor(app.theme.Accent)
	fp.titleView.SetBackgroundColor(app.theme.HeaderBg)

	fp.input = tview.NewInputField()
	fp.input.
		SetLabel("> ").
		SetLabelColor(app.theme.Accent).
		SetFieldWidth(0).
		SetPlaceholder("Type to filter...").
		SetPlaceholderTextColor(app.theme.SecondaryText).
		SetFieldBackgroundColor(app.theme.InputBg).
		SetFieldTextColor(app.theme.Foreground).
		SetBackgroundColor(app.theme.HeaderBg)

	fp.list = tview.NewList().
		ShowSecondaryText(false).
		SetMainTextColor(app.theme.Foreground).
		SetSelectedBackgroundColor(app.theme.Accent).
		SetSelectedTextColor(app.theme.SelectionText).
		SetHighlightFullLine(true)
	fp.list.SetBackgroundColor(app.theme.HeaderBg)

	helpText := tview.NewTextView()
	helpText.SetText("Type: filter | ↑↓: navigate | Enter: select | Esc: cancel")
	helpText.SetTextColor(app.theme.SecondaryText)
	helpText.SetBackgroundColor(app.theme.HeaderBg)

	modalContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(fp.titleView, 1, 0, false).
		AddItem(fp.input, 1, 0, true).
		AddItem(fp.list, 0, 1, false).
		AddItem(helpText, 1, 0, false)
	modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	fp.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modalContent, 20, 0, true).
			AddItem(nil, 0, 1, false), 60, 0, true).
		AddItem(nil, 0, 1, false)
	fp.modal.SetBackgroundColor(app.theme.Background)

	return fp
}

// Show displays the picker with the given title and items.
func (fp *FilterPickerModal) Show(title string, items []PickerItem, onSelect func(item PickerItem)) {
	fp.allItems = items
	fp.onSelect = onSelect
	fp.query = ""

	fp.titleView.SetText(title)
	fp.input.SetText("")
	fp.refilter()

	fp.app.filterPickerActive = true
	fp.app.pages.AddPage("filter_picker", fp.modal, true, true)
	fp.app.pages.SendToFront("filter_picker")
	fp.app.app.SetFocus(fp.input)
}

// Hide removes the picker overlay.
func (fp *FilterPickerModal) Hide() {
	fp.app.filterPickerActive = false
	fp.app.pages.RemovePage("filter_picker")
	fp.app.updateFocus()
}

// refilter recomputes the visible list from the current query.
func (fp *FilterPickerModal) refilter() {
	query := strings.ToLower(strings.TrimSpace(fp.query))
	fp.filtered = fp.filtered[:0]
	for _, item := range fp.allItems {
		if query == "" || strings.Contains(strings.ToLower(item.Label), query) {
			fp.filtered = append(fp.filtered, item)
		}
	}

	fp.list.Clear()
	for _, item := range fp.filtered {
		fp.list.AddItem(item.Label, "", 0, nil)
	}
	if len(fp.filtered) > 0 {
		fp.list.SetCurrentItem(0)
	}
}

// HandleKey handles keyboard input for the filter picker.
func (fp *FilterPickerModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		fp.Hide()
		return nil
	case tcell.KeyEnter:
		idx := fp.list.GetCurrentItem()
		if idx >= 0 && idx < len(fp.filtered) {
			item := fp.filtered[idx]
			fp.Hide()
			if fp.onSelect != nil {
				fp.onSelect(item)
			}
		}
		return nil
	case tcell.KeyUp, tcell.KeyCtrlP:
		idx := fp.list.GetCurrentItem()
		if idx > 0 {
			fp.list.SetCurrentItem(idx - 1)
		}
		return nil
	case tcell.KeyDown, tcell.KeyCtrlN:
		idx := fp.list.GetCurrentItem()
		if idx < fp.list.GetItemCount()-1 {
			fp.list.SetCurrentItem(idx + 1)
		}
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(fp.query) > 0 {
			fp.query = fp.query[:len(fp.query)-1]
			fp.input.SetText(fp.query)
			fp.refilter()
		}
		return nil
	case tcell.KeyRune:
		fp.query += string(event.Rune())
		fp.input.SetText(fp.query)
		fp.refilter()
		return nil
	}
	return event
}

// GetModal returns the modal flex for adding to pages.
func (fp *FilterPickerModal) GetModal() *tview.Flex {
	return fp.modal
}
