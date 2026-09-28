package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

const savedViewsPageName = "saved_views"

const (
	savedViewEditorFieldWidth = 48
	savedViewEditorHeight     = 22
	savedViewEditorHelp       = "Name required | Filter JSON must be an object | Ctrl+Enter: save | Esc: cancel"
)

// SavedViewEditorValues is the validated, presentation-level editor payload.
// FilterJSON remains text until the API layer validates and converts it to the
// schema's IssueFilter object.
type SavedViewEditorValues struct {
	ID          string
	Name        string
	Description string
	Color       string
	FilterJSON  string
}

// SavedViewsModalOptions supplies views, state, and injected actions.
type SavedViewsModalOptions struct {
	Views   []linearapi.CustomView
	Loading bool
	Error   error

	OnApply  func(view linearapi.CustomView)
	OnCreate func(values SavedViewEditorValues)
	OnEdit   func(values SavedViewEditorValues)
	OnDelete func(view linearapi.CustomView)
	OnReload func()
}

// SavedViewsOptions and SavedViewsModalCallbacks are naming aliases for
// embedders that keep data and callbacks in separate layers.
type SavedViewsOptions = SavedViewsModalOptions
type SavedViewsModalCallbacks = SavedViewsModalOptions

// SavedViewsModal is a reusable manager for durable Linear custom views.
// It performs no API calls and delegates all mutations to callbacks.
type SavedViewsModal struct {
	app   *App
	modal *tview.Flex

	List    *tview.List
	Details *tview.TextView
	Header  *tview.TextView
	Help    *tview.TextView
	Status  *tview.TextView

	EditorName        *tview.InputField
	EditorDescription *tview.TextArea
	EditorColor       *tview.InputField
	EditorFilter      *tview.TextArea

	options    SavedViewsModalOptions
	views      []linearapi.CustomView
	selectedID string

	editorModal               *tview.Flex
	editorForm                *tview.Form
	editorHelp                *tview.TextView
	editorEdit                bool
	editorOriginalDescription string
	editorOriginalColor       string
	editorOriginalFilter      string

	deletePending bool
	pendingDelete *linearapi.CustomView
	// deleteConfirmationPage records the exact shared confirmation primitive
	// created for this modal. The app has other confirmation users, so cleanup
	// must never remove a page that replaced this one.
	deleteConfirmationPage tview.Primitive
}

// NewSavedViewsModal creates a saved-views manager with shell styling derived
// from the current app theme and density.
func NewSavedViewsModal(app *App) *SavedViewsModal {
	sm := &SavedViewsModal{app: app}
	sm.buildWidgets()
	return sm
}

// GetModal returns the reusable shell so callers can mount it in their own
// page stack when they do not use Show directly.
func (sm *SavedViewsModal) GetModal() *tview.Flex {
	if sm == nil {
		return nil
	}
	return sm.modal
}

func (sm *SavedViewsModal) buildWidgets() {
	theme := LinearTheme
	if sm.app != nil {
		theme = sm.app.theme
	}
	sm.List = tview.NewList().
		ShowSecondaryText(false).
		SetMainTextColor(theme.Foreground).
		SetSelectedBackgroundColor(theme.Accent).
		SetSelectedTextColor(theme.SelectionText).
		SetHighlightFullLine(true)
	sm.List.SetBackgroundColor(theme.HeaderBg)
	sm.List.SetChangedFunc(func(index int, _ string, _ string, _ rune) { sm.syncSelection(index) })
	sm.Details = tview.NewTextView().SetWrap(true).SetWordWrap(true)
	sm.Details.SetBackgroundColor(theme.HeaderBg)
	sm.Details.SetTextColor(theme.Foreground)
	sm.Header = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	sm.Header.SetBackgroundColor(theme.HeaderBg)
	sm.Header.SetTextColor(theme.Accent)
	sm.Help = tview.NewTextView().SetWrap(false).SetWordWrap(false).SetTextAlign(tview.AlignCenter)
	sm.Help.SetBackgroundColor(theme.HeaderBg)
	sm.Help.SetTextColor(theme.SecondaryText)
	sm.Status = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	sm.Status.SetBackgroundColor(theme.HeaderBg)
	sm.Status.SetTextColor(theme.SecondaryText)
	sm.buildModal(theme, sm.density())
}

func (sm *SavedViewsModal) buildModal(theme Theme, density DensityProfile) {
	sm.Header.SetText("Saved Views")
	sm.updateHelp()
	body := tview.NewFlex().AddItem(sm.List, 0, 2, true).AddItem(sm.Details, 0, 3, false)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(sm.Header, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(sm.Status, 1, 0, false).
		AddItem(sm.Help, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).SetBorder(true).SetTitle(" Saved Views ").SetTitleColor(theme.Foreground)
	content.SetBorderColor(theme.Accent)
	content.SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)
	const width = 108
	const height = 22
	vertical := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, height, 0, true).
		AddItem(nil, 0, 1, false)
	sm.modal = newResponsiveWorkspaceModal(theme.Background, vertical, width, nil)
}

// Show displays the manager and resets deterministic loading/error/empty state.
func (sm *SavedViewsModal) Show(options SavedViewsModalOptions) {
	if sm == nil {
		return
	}
	// A repeated Show must close only a confirmation owned by this manager.
	// This also clears stale delete state before replacing the option callbacks.
	sm.CancelDelete()
	sm.options = options
	sm.views = cloneSavedViews(options.Views)
	sm.Status.SetText("")
	sm.buildModal(sm.theme(), sm.density())
	sm.refreshList()
	if sm.app != nil && sm.app.pages != nil {
		sm.app.pages.AddPage(savedViewsPageName, sm.modal, true, true)
		sm.app.pages.SendToFront(savedViewsPageName)
	}
	sm.focusList()
}

// SetViews refreshes the list while preserving the selected stable ID.
func (sm *SavedViewsModal) SetViews(views []linearapi.CustomView) {
	if sm == nil {
		return
	}
	sm.views = cloneSavedViews(views)
	sm.options.Views = cloneSavedViews(views)
	sm.refreshList()
}

func (sm *SavedViewsModal) refreshList() {
	previous := sm.selectedID
	sm.selectedID = ""
	sm.List.Clear()
	if sm.options.Loading {
		sm.List.AddItem("Loading saved views...", "", 0, nil)
		sm.Details.SetText("Loading saved views...")
		sm.updateHelp()
		return
	}
	if sm.options.Error != nil {
		sm.List.AddItem("Error: "+sm.options.Error.Error(), "", 0, nil)
		sm.Details.SetText(sm.options.Error.Error())
		sm.updateHelp()
		return
	}
	if len(sm.views) == 0 {
		sm.List.AddItem("No saved views", "", 0, nil)
		sm.Details.SetText("No saved views available.")
		sm.updateHelp()
		return
	}
	selectedIndex := 0
	for index, view := range sm.views {
		label := view.Name
		if view.Color != "" {
			label += "  " + view.Color
		}
		sm.List.AddItem(label, "", 0, nil)
		if view.ID == previous {
			selectedIndex = index
		}
	}
	sm.List.SetCurrentItem(selectedIndex)
	sm.syncSelection(selectedIndex)
	sm.updateHelp()
}

func (sm *SavedViewsModal) syncSelection(index int) {
	if !sm.dataReady() || index < 0 || index >= len(sm.views) {
		return
	}
	sm.selectedID = sm.views[index].ID
	view := sm.views[index]
	sm.Details.SetText(fmt.Sprintf("%s\n\n%s\n\nColor: %s\n\nFilter: %s", view.Name, view.Description, view.Color, view.FilterJSON))
}

// HandleKey handles list actions and delegates editor keys while editing.
func (sm *SavedViewsModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if sm == nil || event == nil {
		return event
	}
	if sm.EditorVisible() {
		if event.Key() == tcell.KeyEscape {
			sm.CancelEditor()
			return nil
		}
		if event.Key() == tcell.KeyEnter && event.Modifiers()&(tcell.ModCtrl|tcell.ModMeta) != 0 {
			sm.SaveEditor()
			return nil
		}
		return event
	}
	if sm.deletePending {
		if event.Key() == tcell.KeyEscape {
			sm.CancelDelete()
			return nil
		}
		return event
	}
	switch event.Key() {
	case tcell.KeyEscape:
		sm.Hide()
		return nil
	case tcell.KeyUp:
		sm.move(-1)
		return nil
	case tcell.KeyDown:
		sm.move(1)
		return nil
	case tcell.KeyEnter:
		sm.applyCurrent()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'j':
			sm.move(1)
			return nil
		case 'k':
			sm.move(-1)
			return nil
		case 'n':
			sm.OpenCreate()
			return nil
		case 'e':
			sm.OpenEdit()
			return nil
		case 'd':
			sm.requestDelete()
			return nil
		case 'r':
			if sm.options.OnReload != nil {
				sm.options.OnReload()
			}
			return nil
		}
	}
	return event
}

// Hide closes the manager and any local editor.
func (sm *SavedViewsModal) Hide() {
	if sm == nil {
		return
	}
	if sm.app != nil && sm.app.savedViewsMutationRunner != nil {
		sm.app.savedViewsMutationRunner.Invalidate()
	}
	sm.CancelEditor()
	sm.CancelDelete()
	if sm.app != nil && sm.app.pages != nil {
		sm.app.pages.RemovePage(savedViewsPageName)
		if sm.app.app != nil && sm.app.navigationTree != nil {
			sm.app.updateFocus()
		}
	}
}

func (sm *SavedViewsModal) move(delta int) {
	if !sm.dataReady() {
		return
	}
	index := sm.List.GetCurrentItem() + delta
	if index < 0 {
		index = 0
	}
	if index >= len(sm.views) {
		index = len(sm.views) - 1
	}
	sm.List.SetCurrentItem(index)
	sm.syncSelection(index)
}

func (sm *SavedViewsModal) applyCurrent() {
	if !sm.dataReady() {
		return
	}
	index := sm.List.GetCurrentItem()
	if index < 0 || index >= len(sm.views) {
		return
	}
	view := sm.views[index]
	onApply := sm.options.OnApply
	sm.Hide()
	if onApply != nil {
		onApply(view)
	}
}

// OpenCreate opens the editor with empty values.
func (sm *SavedViewsModal) OpenCreate() {
	sm.openEditor(SavedViewEditorValues{}, false)
}

// OpenEdit opens the editor for the selected view.
func (sm *SavedViewsModal) OpenEdit() {
	if !sm.dataReady() {
		return
	}
	index := sm.List.GetCurrentItem()
	if index < 0 || index >= len(sm.views) {
		return
	}
	view := sm.views[index]
	sm.openEditor(SavedViewEditorValues{ID: view.ID, Name: view.Name, Description: view.Description, Color: view.Color, FilterJSON: view.FilterJSON}, true)
}

func (sm *SavedViewsModal) openEditor(values SavedViewEditorValues, editing bool) {
	sm.editorEdit = editing
	sm.editorOriginalDescription = values.Description
	sm.editorOriginalColor = values.Color
	sm.editorOriginalFilter = values.FilterJSON
	theme := sm.theme()
	sm.EditorName = tview.NewInputField().SetLabel("Name").SetFieldWidth(savedViewEditorFieldWidth)
	sm.EditorDescription = tview.NewTextArea().SetLabel("Description").SetSize(3, savedViewEditorFieldWidth)
	sm.EditorColor = tview.NewInputField().SetLabel("Color").SetFieldWidth(savedViewEditorFieldWidth)
	sm.EditorFilter = tview.NewTextArea().SetLabel("Filter JSON").SetSize(7, savedViewEditorFieldWidth)
	sm.EditorName.SetText(values.Name)
	sm.EditorDescription.SetText(values.Description, false)
	sm.EditorColor.SetText(values.Color)
	sm.EditorFilter.SetText(values.FilterJSON, false)
	sm.editorForm = tview.NewForm()
	sm.editorForm.SetBackgroundColor(theme.HeaderBg)
	sm.editorForm.SetFieldBackgroundColor(theme.InputBg)
	sm.editorForm.SetFieldTextColor(theme.Foreground)
	sm.editorForm.SetButtonBackgroundColor(theme.Accent)
	sm.editorForm.SetButtonTextColor(theme.SelectionText)
	sm.editorForm.SetLabelColor(theme.Foreground)
	sm.editorForm.AddFormItem(sm.EditorName).AddFormItem(sm.EditorDescription).AddFormItem(sm.EditorColor).AddFormItem(sm.EditorFilter)
	sm.editorForm.AddButton("Save", sm.SaveEditor).AddButton("Cancel", sm.CancelEditor)
	title := "New Saved View"
	if editing {
		title = "Edit Saved View"
	}
	sm.editorHelp = tview.NewTextView().SetText(savedViewEditorHelp)
	sm.editorHelp.SetTextColor(theme.SecondaryText).SetBackgroundColor(theme.HeaderBg)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(sm.editorForm, 0, 1, true).AddItem(sm.editorHelp, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).SetBorder(true).SetTitle(" " + title + " ").SetTitleColor(theme.Foreground).SetBorderColor(theme.Accent)
	sm.editorModal = tview.NewFlex().AddItem(nil, 0, 1, false).AddItem(tview.NewFlex().SetDirection(tview.FlexRow).AddItem(nil, 0, 1, false).AddItem(content, savedViewEditorHeight, 0, true).AddItem(nil, 0, 1, false), 74, 0, true).AddItem(nil, 0, 1, false)
	sm.editorModal.SetBackgroundColor(theme.Background)
	if sm.app != nil && sm.app.pages != nil {
		sm.app.pages.AddPage("saved_views_editor", sm.editorModal, true, true)
		sm.app.pages.SendToFront("saved_views_editor")
	}
	if sm.app != nil && sm.app.app != nil {
		sm.app.app.SetFocus(sm.EditorName)
	}
}

// SaveEditor validates fields and invokes the create/edit callback once.
func (sm *SavedViewsModal) SaveEditor() {
	if sm == nil || !sm.EditorVisible() {
		return
	}
	name := strings.TrimSpace(sm.EditorName.GetText())
	if name == "" {
		sm.setStatus("Name is required")
		return
	}
	filterJSON := strings.TrimSpace(sm.EditorFilter.GetText())
	if filterJSON != "" {
		var parsed interface{}
		if err := json.Unmarshal([]byte(filterJSON), &parsed); err != nil {
			sm.setStatus("Filter JSON is invalid")
			return
		}
		if _, ok := parsed.(map[string]interface{}); !ok {
			sm.setStatus("Filter JSON must be an object")
			return
		}
	}
	description := sm.EditorDescription.GetText()
	color := strings.TrimSpace(sm.EditorColor.GetText())
	if sm.editorEdit {
		description = savedViewEditorFieldValue(description, sm.editorOriginalDescription)
		color = savedViewEditorFieldValue(color, strings.TrimSpace(sm.editorOriginalColor))
		filterJSON = savedViewEditorFieldValue(filterJSON, strings.TrimSpace(sm.editorOriginalFilter))
	}
	values := SavedViewEditorValues{ID: sm.EditorID(), Name: name, Description: description, Color: color, FilterJSON: filterJSON}
	sm.closeEditor()
	if sm.editorEdit {
		if sm.options.OnEdit != nil {
			sm.options.OnEdit(values)
		}
	} else if sm.options.OnCreate != nil {
		sm.options.OnCreate(values)
	}
}

func (sm *SavedViewsModal) EditorID() string {
	if !sm.editorEdit {
		return ""
	}
	index := sm.List.GetCurrentItem()
	if index < 0 || index >= len(sm.views) {
		return ""
	}
	return sm.views[index].ID
}

// CancelEditor closes the editor without invoking callbacks.
func (sm *SavedViewsModal) CancelEditor() {
	if sm == nil {
		return
	}
	sm.closeEditor()
}

func (sm *SavedViewsModal) closeEditor() {
	if sm.app != nil && sm.app.pages != nil {
		sm.app.pages.RemovePage("saved_views_editor")
	}
	sm.editorModal = nil
	sm.editorForm = nil
	sm.editorHelp = nil
	sm.EditorName = nil
	sm.EditorDescription = nil
	sm.EditorColor = nil
	sm.EditorFilter = nil
	sm.editorOriginalDescription = ""
	sm.editorOriginalColor = ""
	sm.editorOriginalFilter = ""
	sm.focusList()
}

func savedViewEditorFieldValue(current, original string) string {
	current = strings.TrimSpace(current)
	original = strings.TrimSpace(original)
	if current == original {
		return linearapi.CustomViewFieldUnchanged
	}
	if current == "" {
		return linearapi.CustomViewFieldClear
	}
	return current
}

// EditorVisible reports whether the editor overlay is active.
func (sm *SavedViewsModal) EditorVisible() bool {
	return sm != nil && sm.app != nil && sm.app.pages != nil && sm.app.pages.HasPage("saved_views_editor")
}

func (sm *SavedViewsModal) requestDelete() {
	if !sm.dataReady() {
		return
	}
	index := sm.List.GetCurrentItem()
	if index < 0 || index >= len(sm.views) {
		return
	}
	view := sm.views[index]
	sm.deletePending = true
	sm.pendingDelete = &view
	if sm.app != nil && sm.app.confirmationModal != nil && sm.app.pages != nil {
		sm.app.confirmationModal.Show("Delete saved view", "Delete this saved view?", "Delete", sm.ConfirmDelete, sm.CancelDelete)
		sm.deleteConfirmationPage = sm.app.confirmationModal.modal
	}
}

// DeletePending reports whether a delete awaits explicit confirmation.
func (sm *SavedViewsModal) DeletePending() bool { return sm != nil && sm.deletePending }

// ConfirmDelete invokes the injected delete callback after explicit approval.
func (sm *SavedViewsModal) ConfirmDelete() {
	if sm == nil || !sm.deletePending || sm.pendingDelete == nil {
		return
	}
	if !sm.dataReady() {
		sm.CancelDelete()
		return
	}
	view := *sm.pendingDelete
	sm.deletePending = false
	sm.pendingDelete = nil
	sm.removeOwnedConfirmationPage()
	if sm.options.OnDelete != nil {
		sm.options.OnDelete(view)
	}
	sm.focusList()
}

// CancelDelete cancels a pending delete without invoking callbacks.
func (sm *SavedViewsModal) CancelDelete() {
	if sm == nil {
		return
	}
	sm.deletePending = false
	sm.pendingDelete = nil
	sm.removeOwnedConfirmationPage()
	sm.focusList()
}

// removeOwnedConfirmationPage removes the shared confirmation page only when
// it still contains the exact primitive created by this modal. Another modal
// may have replaced the page while a saved-view delete was pending.
func (sm *SavedViewsModal) removeOwnedConfirmationPage() {
	if sm == nil {
		return
	}
	owned := sm.deleteConfirmationPage
	sm.deleteConfirmationPage = nil
	if owned == nil || sm.app == nil || sm.app.pages == nil {
		return
	}
	if sm.app.confirmationModal != nil {
		sm.app.confirmationModal.release(owned)
	}
	if sm.app.pages.GetPage("confirmation") == owned {
		sm.app.pages.RemovePage("confirmation")
	}
}

func (sm *SavedViewsModal) setStatus(message string) {
	sm.Status.SetText(message)
	if sm.editorHelp != nil {
		if message == "" {
			sm.editorHelp.SetText(savedViewEditorHelp)
		} else {
			sm.editorHelp.SetText(message)
		}
	}
	if sm.app != nil {
		sm.app.statusMessage = message
	}
}

func (sm *SavedViewsModal) StatusText() string {
	if sm == nil || sm.Status == nil {
		return ""
	}
	return sm.Status.GetText(true)
}

func (sm *SavedViewsModal) focusList() {
	if sm.app != nil && sm.app.app != nil && sm.List != nil {
		sm.app.app.SetFocus(sm.List)
	}
}

func (sm *SavedViewsModal) SelectedID() string {
	if sm == nil {
		return ""
	}
	return sm.selectedID
}

// dataReady reports whether the visible list represents an actionable saved
// view. Loading, error, and empty rows are status rows rather than views,
// even when an older snapshot remains attached to the modal options.
func (sm *SavedViewsModal) dataReady() bool {
	if sm == nil || sm.List == nil || sm.options.Loading || sm.options.Error != nil || len(sm.views) == 0 {
		return false
	}
	index := sm.List.GetCurrentItem()
	return index >= 0 && index < len(sm.views)
}

func (sm *SavedViewsModal) updateHelp() {
	if sm == nil || sm.Help == nil {
		return
	}
	if sm.dataReady() {
		sm.Help.SetText("j/k or arrows: navigate | Enter: apply | n: new | e: edit | d: delete | r: reload | Esc: close")
		return
	}
	sm.Help.SetText("n: new | r: reload | Esc: close")
}

func (sm *SavedViewsModal) theme() Theme {
	if sm.app != nil {
		return sm.app.theme
	}
	return LinearTheme
}

func (sm *SavedViewsModal) density() DensityProfile {
	if sm.app != nil {
		return sm.app.density
	}
	return ResolveDensity("comfortable")
}

func cloneSavedViews(views []linearapi.CustomView) []linearapi.CustomView {
	return append([]linearapi.CustomView(nil), views...)
}
