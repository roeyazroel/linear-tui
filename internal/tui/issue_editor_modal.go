package tui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
)

// IssueEditorOption is one selectable value in an issue editor dropdown or
// label picker.
type IssueEditorOption struct {
	ID    string
	Label string
}

// IssueEditorValues contains the editable issue fields. IDs are kept as
// strings because Linear identifiers are opaque values rather than numbers.
type IssueEditorValues struct {
	IssueID, Title, Description, StateID, AssigneeID string
	Priority                                         int
	ProjectID, CycleID                               string
	LabelIDs                                         []string
}

// IssueEditorOptions supplies initial values and selectable metadata to an
// IssueEditorModal.
type IssueEditorOptions struct {
	Values    IssueEditorValues
	Statuses  []IssueEditorOption
	Assignees []IssueEditorOption
	Projects  []IssueEditorOption
	Cycles    []IssueEditorOption
	Labels    []IssueEditorOption
}

// IssueEditorModal is a reusable, presentation-only issue editor. It owns no
// Linear API operations; callers receive the values through onSave.
type IssueEditorModal struct {
	app   *App
	modal *tview.Flex
	form  *tview.Form

	// Exported field handles make the component straightforward to embed and
	// test while the lowercase aliases retain the conventions of older modals.
	Form        *tview.Form
	Title       *tview.InputField
	Description *tview.TextArea
	Status      *tview.DropDown
	Assignee    *tview.DropDown
	Priority    *tview.DropDown
	Project     *tview.DropDown
	Cycle       *tview.DropDown

	titleField       *tview.InputField
	descriptionField *tview.TextArea
	statusField      *tview.DropDown
	assigneeField    *tview.DropDown
	priorityField    *tview.DropDown
	projectField     *tview.DropDown
	cycleField       *tview.DropDown

	LabelsSummary *tview.TextView
	labelsSummary *tview.TextView

	values         IssueEditorValues
	statuses       []IssueEditorOption
	assignees      []IssueEditorOption
	projects       []IssueEditorOption
	cycles         []IssueEditorOption
	labels         []IssueEditorOption
	onSave         func(IssueEditorValues)
	labelsOpen     bool
	keepOpenOnSave bool
	saving         bool
}

// NewIssueEditorModal creates a reusable issue editor using the app's current
// theme, density, and interface mode.
func NewIssueEditorModal(app *App) *IssueEditorModal {
	em := &IssueEditorModal{app: app}
	theme := LinearTheme
	density := ResolveDensity(config.DefaultDensity)
	if app != nil {
		theme = app.theme
		density = app.density
	}

	em.Title = tview.NewInputField().
		SetLabel("Title").
		SetFieldWidth(70).
		SetFieldBackgroundColor(theme.InputBg).
		SetFieldTextColor(theme.Foreground).
		SetLabelColor(theme.Foreground)
	em.Description = tview.NewTextArea().
		SetLabel("Description").
		SetSize(4, 70)
	// Keep the textarea as a normal form item while allowing line breaks.
	em.Description.SetWrap(true).SetWordWrap(true)

	em.Status = newIssueEditorDropDown("Status", theme)
	em.Assignee = newIssueEditorDropDown("Assignee", theme)
	em.Priority = newIssueEditorDropDown("Priority", theme)
	em.Project = newIssueEditorDropDown("Project", theme)
	em.Cycle = newIssueEditorDropDown("Cycle", theme)

	em.titleField = em.Title
	em.descriptionField = em.Description
	em.statusField = em.Status
	em.assigneeField = em.Assignee
	em.priorityField = em.Priority
	em.projectField = em.Project
	em.cycleField = em.Cycle

	em.form = tview.NewForm()
	em.form.SetBackgroundColor(theme.HeaderBg)
	em.form.SetFieldBackgroundColor(theme.InputBg).
		SetFieldTextColor(theme.Foreground).
		SetButtonBackgroundColor(theme.Accent).
		SetButtonTextColor(theme.SelectionText).
		SetLabelColor(theme.Foreground).
		SetItemPadding(0)
	em.Form = em.form
	em.form.AddFormItem(em.Title).
		AddFormItem(em.Description).
		AddFormItem(em.Status).
		AddFormItem(em.Assignee).
		AddFormItem(em.Priority).
		AddFormItem(em.Project).
		AddFormItem(em.Cycle)

	em.form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			em.Hide()
			return nil
		case tcell.KeyEnter:
			if modifiers := event.Modifiers(); modifiers&tcell.ModCtrl != 0 || modifiers&tcell.ModMeta != 0 {
				em.save()
				return nil
			}
		}
		return event
	})

	// Dropdown callbacks update the local draft only. The caller receives a
	// complete snapshot once Save is accepted.
	em.Status.SetSelectedFunc(func(_ string, index int) {
		if index >= 0 && index < len(em.statuses) {
			em.values.StateID = em.statuses[index].ID
		}
	})
	em.Assignee.SetSelectedFunc(func(_ string, index int) {
		em.values.AssigneeID = issueEditorIDAt(em.assignees, index)
	})
	em.Priority.SetSelectedFunc(func(_ string, index int) {
		if index >= 0 && index < len(issueEditorPriorityLabels) {
			em.values.Priority = index
		}
	})
	em.Project.SetSelectedFunc(func(_ string, index int) {
		em.values.ProjectID = issueEditorIDAt(em.projects, index)
	})
	em.Cycle.SetSelectedFunc(func(_ string, index int) {
		em.values.CycleID = issueEditorIDAt(em.cycles, index)
	})

	em.labelsSummary = tview.NewTextView()
	em.labelsSummary.SetTextColor(theme.SecondaryText)
	em.labelsSummary.SetBackgroundColor(theme.HeaderBg)
	em.labelsSummary.SetWrap(false).SetWordWrap(false)
	em.LabelsSummary = em.labelsSummary

	// Labels is intentionally a picker button rather than a dropdown: it keeps
	// the editor compact while the existing reusable multi-select handles the
	// interaction details.
	em.form.AddButton("Labels", em.openLabels)
	em.form.AddButton("Save", em.save)
	em.form.AddButton("Cancel", em.Hide)

	// Construct the first shell now; Show rebuilds the shell so a live settings
	// mode switch is reflected before the modal is shown again.
	em.buildModal(theme, density)
	return em
}

var issueEditorPriorityLabels = []string{"No priority", "Urgent", "High", "Normal", "Low"}

func newIssueEditorDropDown(label string, theme Theme) *tview.DropDown {
	return tview.NewDropDown().
		SetLabel(label).
		SetFieldWidth(50).
		SetListStyles(
			tcell.StyleDefault.Background(theme.HeaderBg).Foreground(theme.Foreground),
			tcell.StyleDefault.Background(theme.Accent).Foreground(theme.SelectionText),
		)
}

func issueEditorIDAt(options []IssueEditorOption, index int) string {
	if index <= 0 || index >= len(options) {
		return ""
	}
	return options[index].ID
}

func cloneIssueEditorOptions(options []IssueEditorOption) []IssueEditorOption {
	if len(options) == 0 {
		return nil
	}
	return append([]IssueEditorOption(nil), options...)
}

func cloneIssueEditorIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	return append([]string(nil), ids...)
}

func cloneIssueEditorValues(values IssueEditorValues) IssueEditorValues {
	values.LabelIDs = cloneIssueEditorIDs(values.LabelIDs)
	return values
}

func issueEditorOptionLabels(options []IssueEditorOption) []string {
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.Label
		if labels[i] == "" {
			labels[i] = option.ID
		}
	}
	return labels
}

func issueEditorSelectedIndex(options []IssueEditorOption, id string, fallback int) int {
	for i, option := range options {
		if option.ID == id {
			return i
		}
	}
	if fallback >= 0 && fallback < len(options) {
		return fallback
	}
	return -1
}

func issueEditorClearOptions(clearLabel string, options []IssueEditorOption) []IssueEditorOption {
	result := make([]IssueEditorOption, 0, len(options)+1)
	result = append(result, IssueEditorOption{Label: clearLabel})
	for _, option := range options {
		// An explicit empty ID is already represented by the clear option.
		if option.ID == "" {
			continue
		}
		result = append(result, option)
	}
	return result
}

// Show resets every draft field, option cache, and callback from options, then
// presents the modal. Reusing an instance never carries stale unsaved state.
func (em *IssueEditorModal) Show(options IssueEditorOptions, onSave func(IssueEditorValues)) {
	if em == nil {
		return
	}
	em.values = cloneIssueEditorValues(options.Values)
	em.statuses = cloneIssueEditorOptions(options.Statuses)
	em.assignees = issueEditorClearOptions("Unassigned", cloneIssueEditorOptions(options.Assignees))
	em.projects = issueEditorClearOptions("No project", cloneIssueEditorOptions(options.Projects))
	em.cycles = issueEditorClearOptions("No cycle", cloneIssueEditorOptions(options.Cycles))
	em.labels = cloneIssueEditorOptions(options.Labels)
	em.onSave = onSave
	em.labelsOpen = false
	em.keepOpenOnSave = false
	em.saving = false

	em.Title.SetText(options.Values.Title)
	em.Description.SetText(options.Values.Description, false)
	em.Status.SetOptions(issueEditorOptionLabels(em.statuses), nil)
	em.Status.SetSelectedFunc(func(_ string, index int) {
		if index >= 0 && index < len(em.statuses) {
			em.values.StateID = em.statuses[index].ID
		}
	})
	statusIndex := issueEditorSelectedIndex(em.statuses, options.Values.StateID, 0)
	if statusIndex >= 0 {
		em.Status.SetCurrentOption(statusIndex)
	} else {
		em.Status.SetCurrentOption(-1)
		em.values.StateID = options.Values.StateID
	}

	em.Assignee.SetOptions(issueEditorOptionLabels(em.assignees), nil)
	em.Assignee.SetSelectedFunc(func(_ string, index int) {
		em.values.AssigneeID = issueEditorIDAt(em.assignees, index)
	})
	em.Assignee.SetCurrentOption(issueEditorSelectedIndex(em.assignees, options.Values.AssigneeID, 0))

	em.Priority.SetOptions(issueEditorPriorityLabels, nil)
	em.Priority.SetSelectedFunc(func(_ string, index int) {
		if index >= 0 && index < len(issueEditorPriorityLabels) {
			em.values.Priority = index
		}
	})
	priority := options.Values.Priority
	if priority < 0 || priority >= len(issueEditorPriorityLabels) {
		priority = 0
	}
	em.Priority.SetCurrentOption(priority)

	em.Project.SetOptions(issueEditorOptionLabels(em.projects), nil)
	em.Project.SetSelectedFunc(func(_ string, index int) {
		em.values.ProjectID = issueEditorIDAt(em.projects, index)
	})
	em.Project.SetCurrentOption(issueEditorSelectedIndex(em.projects, options.Values.ProjectID, 0))

	em.Cycle.SetOptions(issueEditorOptionLabels(em.cycles), nil)
	em.Cycle.SetSelectedFunc(func(_ string, index int) {
		em.values.CycleID = issueEditorIDAt(em.cycles, index)
	})
	em.Cycle.SetCurrentOption(issueEditorSelectedIndex(em.cycles, options.Values.CycleID, 0))

	em.updateLabelsSummary()
	if em.app != nil {
		em.buildModal(em.app.theme, em.app.density)
		if em.app.pages != nil {
			em.app.pages.AddPage("issue_editor", em.modal, true, true)
			em.app.pages.SendToFront("issue_editor")
		}
	}
	em.focusEditor()
}

// Hide removes the editor page and drops the callback so cancellation can
// never invoke a stale save handler.
func (em *IssueEditorModal) Hide() {
	if em == nil {
		return
	}
	em.labelsOpen = false
	em.onSave = nil
	em.keepOpenOnSave = false
	em.saving = false
	if em.app == nil {
		return
	}
	em.app.invalidateIssueEditorSaveContext()
	if em.app.pages != nil {
		em.app.pages.RemovePage("issue_editor")
	}
	if em.app.app != nil && em.app.navigationTree != nil && em.app.myIssuesTable != nil && em.app.otherIssuesTable != nil && em.app.detailsDescriptionView != nil && em.app.detailsCommentsView != nil {
		em.app.updateFocus()
	}
}

// HandleKey handles modal-level keys. Returning the event leaves ordinary
// field navigation to tview.
func (em *IssueEditorModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if em == nil || event == nil {
		return event
	}
	// This path also makes direct callers robust when the labels picker is
	// routed through the editor rather than the app's global dispatcher.
	if em.labelsOpen && em.app != nil && em.app.multiSelectModal != nil && em.app.pages != nil && em.app.pages.HasPage("multi_select") {
		result := em.app.multiSelectModal.HandleKey(event)
		if !em.app.pages.HasPage("multi_select") {
			em.labelsOpen = false
			em.focusEditor()
		}
		return result
	}
	switch event.Key() {
	case tcell.KeyEscape:
		em.Hide()
		return nil
	case tcell.KeyEnter:
		modifiers := event.Modifiers()
		if modifiers&tcell.ModCtrl != 0 || modifiers&tcell.ModMeta != 0 {
			em.save()
			return nil
		}
	}
	return event
}

// GetModal returns the shell primitive for adding to pages.
func (em *IssueEditorModal) GetModal() *tview.Flex {
	if em == nil {
		return nil
	}
	return em.modal
}

func (em *IssueEditorModal) save() {
	if em == nil {
		return
	}
	if em.saving {
		return
	}
	title := em.Title.GetText()
	if strings.TrimSpace(title) == "" {
		em.reportValidationError("Title cannot be blank")
		return
	}

	values := em.values
	values.Title = title
	values.Description = em.Description.GetText()
	values.StateID = em.selectedID(em.Status, em.statuses, values.StateID)
	values.AssigneeID = em.selectedID(em.Assignee, em.assignees, "")
	values.Priority = em.selectedPriority()
	values.ProjectID = em.selectedID(em.Project, em.projects, "")
	values.CycleID = em.selectedID(em.Cycle, em.cycles, "")
	values.LabelIDs = cloneIssueEditorIDs(em.values.LabelIDs)

	callback := em.onSave
	if em.keepOpenOnSave {
		em.saving = true
		if callback != nil {
			callback(values)
		}
		return
	}
	em.onSave = nil
	em.Hide()
	if callback != nil {
		callback(values)
	}
}

func (em *IssueEditorModal) setKeepOpenOnSave(keepOpen bool) {
	if em == nil {
		return
	}
	em.keepOpenOnSave = keepOpen
}

func (em *IssueEditorModal) setSaving(saving bool) {
	if em == nil {
		return
	}
	em.saving = saving
}

func (em *IssueEditorModal) selectedID(dropdown *tview.DropDown, options []IssueEditorOption, fallback string) string {
	if dropdown == nil {
		return fallback
	}
	index, _ := dropdown.GetCurrentOption()
	if index <= 0 || index >= len(options) {
		return fallback
	}
	return options[index].ID
}

func (em *IssueEditorModal) selectedPriority() int {
	if em.Priority == nil {
		return em.values.Priority
	}
	index, _ := em.Priority.GetCurrentOption()
	if index < 0 || index >= len(issueEditorPriorityLabels) {
		return em.values.Priority
	}
	return index
}

func (em *IssueEditorModal) reportValidationError(message string) {
	if em.app == nil {
		return
	}
	em.app.statusMessage = message
	if em.app.statusBar != nil {
		em.app.updateStatusBarWithError(fmt.Errorf("%s", message))
	}
}

func (em *IssueEditorModal) openLabels() {
	if em == nil || em.app == nil {
		return
	}
	if em.app.multiSelectModal == nil {
		em.app.multiSelectModal = NewMultiSelectModal(em.app)
	}
	items := make([]MultiSelectItem, 0, len(em.labels))
	for _, label := range em.labels {
		items = append(items, MultiSelectItem{ID: label.ID, Label: issueEditorOptionLabel(label)})
	}
	em.labelsOpen = true
	em.app.multiSelectModal.Show("Select labels", items, cloneIssueEditorIDs(em.values.LabelIDs), func(ids []string) {
		em.values.LabelIDs = cloneIssueEditorIDs(ids)
		em.updateLabelsSummary()
		em.labelsOpen = false
		em.focusEditor()
	})
}

func issueEditorOptionLabel(option IssueEditorOption) string {
	if option.Label != "" {
		return option.Label
	}
	return option.ID
}

func (em *IssueEditorModal) updateLabelsSummary() {
	if em.labelsSummary == nil {
		return
	}
	labelsByID := make(map[string]string, len(em.labels))
	for _, label := range em.labels {
		labelsByID[label.ID] = issueEditorOptionLabel(label)
	}
	selected := make([]string, 0, len(em.values.LabelIDs))
	for _, id := range em.values.LabelIDs {
		label := labelsByID[id]
		if label == "" {
			label = id
		}
		if label != "" {
			selected = append(selected, label)
		}
	}
	text := "Labels: None"
	if len(selected) > 0 {
		text = "Labels: " + strings.Join(selected, ", ")
	}
	em.labelsSummary.SetText(text)
}

func (em *IssueEditorModal) focusEditor() {
	if em == nil || em.app == nil || em.form == nil || em.app.app == nil {
		return
	}
	em.form.SetFocus(0)
	em.app.app.SetFocus(em.form)
}

func (em *IssueEditorModal) buildModal(theme Theme, density DensityProfile) {
	if em == nil || em.form == nil {
		return
	}
	header := tview.NewTextView()
	header.SetTextColor(theme.Accent)
	header.SetBackgroundColor(theme.HeaderBg)
	header.SetWrap(false).SetWordWrap(false)
	header.SetText("Edit Issue")
	help := tview.NewTextView()
	help.SetText("Tab: next field | Enter: select | Ctrl+Enter: save | Esc: cancel")
	help.SetTextColor(theme.SecondaryText)
	help.SetBackgroundColor(theme.HeaderBg)
	help.SetTextAlign(tview.AlignCenter).SetWrap(false).SetWordWrap(false)

	content := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(em.labelsSummary, 1, 0, false).
		AddItem(em.form, 0, 1, true).
		AddItem(help, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(theme.Accent).
		SetTitle(" Edit Issue ").
		SetTitleColor(theme.Foreground)
	content.SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)

	const composerWidth = 88
	const composerHeight = 23
	centered := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, composerHeight, 0, true).
		AddItem(nil, 0, 1, false)
	em.modal = newResponsiveWorkspaceModal(theme.Background, centered, composerWidth, em.resizeFields)
}

func (em *IssueEditorModal) resizeFields(modalWidth int) {
	fieldWidth := modalWidth - 18
	if fieldWidth < 1 {
		fieldWidth = 1
	}
	if fieldWidth > 70 {
		fieldWidth = 70
	}
	em.Title.SetFieldWidth(fieldWidth)
	em.Description.SetSize(4, fieldWidth)
	if fieldWidth > 50 {
		fieldWidth = 50
	}
	for _, field := range []*tview.DropDown{em.Status, em.Assignee, em.Priority, em.Project, em.Cycle} {
		field.SetFieldWidth(fieldWidth)
	}
}
