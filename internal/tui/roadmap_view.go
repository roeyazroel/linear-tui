package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

const roadmapPageName = "roadmap"

// RoadmapViewActions contains presentation-level hooks for roadmap resource
// actions. The view never calls the Linear client itself; callers inject the
// editor/client workflow through these callbacks.
type RoadmapViewActions struct {
	OnCreateInitiative  func()
	OnEditInitiative    func(initiative linearapi.Initiative)
	OnArchiveInitiative func(initiative linearapi.Initiative)
	OnDeleteInitiative  func(initiative linearapi.Initiative)

	OnCreateProject func(initiativeID string)
	OnEditProject   func(project linearapi.Project)
	OnDeleteProject func(project linearapi.Project)

	OnCreateProjectUpdate  func(projectID string)
	OnEditProjectUpdate    func(update linearapi.ProjectUpdate)
	OnArchiveProjectUpdate func(update linearapi.ProjectUpdate)

	// Short aliases keep the component convenient for callers that already use
	// the action names without the resource prefix.
	CreateInitiative  func()
	EditInitiative    func(initiative linearapi.Initiative)
	ArchiveInitiative func(initiative linearapi.Initiative)
	DeleteInitiative  func(initiative linearapi.Initiative)

	CreateProject func(initiativeID string)
	EditProject   func(project linearapi.Project)
	DeleteProject func(project linearapi.Project)

	CreateProjectUpdate  func(projectID string)
	EditProjectUpdate    func(update linearapi.ProjectUpdate)
	ArchiveProjectUpdate func(update linearapi.ProjectUpdate)
}

// RoadmapViewCallbacks is an alias for callers that prefer callback naming.
type RoadmapViewCallbacks = RoadmapViewActions

// RoadmapViewOptions is the complete render input for a RoadmapView.
type RoadmapViewOptions struct {
	Initiatives    []linearapi.Initiative
	ProjectUpdates map[string][]linearapi.ProjectUpdate
	// Updates is accepted as a concise alias for ProjectUpdates.
	Updates map[string][]linearapi.ProjectUpdate
	Loading bool
	Error   error

	Actions   RoadmapViewActions
	Callbacks RoadmapViewActions

	OnCreateInitiative  func()
	OnEditInitiative    func(initiative linearapi.Initiative)
	OnArchiveInitiative func(initiative linearapi.Initiative)
	OnDeleteInitiative  func(initiative linearapi.Initiative)
	OnCreateProject     func(initiativeID string)
	OnEditProject       func(project linearapi.Project)
	OnDeleteProject     func(project linearapi.Project)

	OnCreateProjectUpdate  func(projectID string)
	OnEditProjectUpdate    func(update linearapi.ProjectUpdate)
	OnArchiveProjectUpdate func(update linearapi.ProjectUpdate)
}

// RoadmapViewData is an alias useful when data is refreshed independently of
// the initial Show call.
type RoadmapViewData = RoadmapViewOptions

// RoadmapHealthChoices returns the only health values accepted by Linear's
// project update workflow. A fresh slice is returned for caller safety.
func RoadmapHealthChoices() []linearapi.ProjectUpdateHealthType {
	return []linearapi.ProjectUpdateHealthType{
		linearapi.ProjectUpdateHealthOnTrack,
		linearapi.ProjectUpdateHealthAtRisk,
		linearapi.ProjectUpdateHealthOffTrack,
	}
}

type roadmapRowKind uint8

const (
	roadmapInitiativeRow roadmapRowKind = iota
	roadmapProjectRow
	roadmapUpdateRow
)

type roadmapRow struct {
	kind         roadmapRowKind
	id           string
	initiativeID string
	projectID    string
	initiative   *linearapi.Initiative
	project      *linearapi.Project
	update       *linearapi.ProjectUpdate
	depth        int
}

// RoadmapView is a reusable hierarchical roadmap browser. It renders
// initiatives, their projects, and project updates, while delegating all
// mutations to injected action callbacks.
type RoadmapView struct {
	app   *App
	modal *tview.Flex

	List    *tview.List
	Details *tview.TextView
	Header  *tview.TextView
	Help    *tview.TextView

	options     RoadmapViewOptions
	initiatives []linearapi.Initiative
	updates     map[string][]linearapi.ProjectUpdate
	rows        []roadmapRow
	expanded    map[string]bool
	selectedID  string

	archivePending bool
	pendingArchive *linearapi.ProjectUpdate
	pendingAction  roadmapPendingAction
	// archiveConfirmationPage records the exact shared confirmation primitive
	// owned by this view so cleanup cannot remove a replacement prompt.
	archiveConfirmationPage tview.Primitive
}

type roadmapPendingActionKind uint8

const (
	roadmapPendingNone roadmapPendingActionKind = iota
	roadmapPendingArchiveUpdate
	roadmapPendingArchiveInitiative
	roadmapPendingDeleteInitiative
	roadmapPendingDeleteProject
)

type roadmapPendingAction struct {
	kind       roadmapPendingActionKind
	initiative *linearapi.Initiative
	project    *linearapi.Project
	update     *linearapi.ProjectUpdate
}

// NewRoadmapView creates a roadmap view using the app's current theme and
// density. It is safe to create before App wiring registers its page.
func NewRoadmapView(app *App) *RoadmapView {
	rv := &RoadmapView{
		app:      app,
		expanded: make(map[string]bool),
		updates:  make(map[string][]linearapi.ProjectUpdate),
	}
	rv.buildWidgets()
	return rv
}

func (rv *RoadmapView) buildWidgets() {
	theme := LinearTheme
	density := ResolveDensity("comfortable")
	if rv.app != nil {
		theme = rv.app.theme
		density = rv.app.density
	}
	rv.List = tview.NewList().
		ShowSecondaryText(false).
		SetMainTextColor(theme.Foreground).
		SetSelectedBackgroundColor(theme.Accent).
		SetSelectedTextColor(theme.SelectionText).
		SetHighlightFullLine(true)
	rv.List.SetBackgroundColor(theme.HeaderBg)
	rv.List.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		rv.syncSelection(index)
	})
	rv.Details = tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetWordWrap(true)
	rv.Details.SetBackgroundColor(theme.HeaderBg)
	rv.Details.SetTextColor(theme.Foreground)
	rv.Header = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	rv.Header.SetBackgroundColor(theme.HeaderBg)
	rv.Header.SetTextColor(theme.Accent)
	rv.Help = tview.NewTextView().SetWrap(false).SetWordWrap(false).SetTextAlign(tview.AlignCenter)
	rv.Help.SetBackgroundColor(theme.HeaderBg)
	rv.Help.SetTextColor(theme.SecondaryText)

	rv.buildModal(theme, density)
}

func (rv *RoadmapView) buildModal(theme Theme, density DensityProfile) {
	help := "n: new initiative | Esc: close"
	rv.Header.SetText("Roadmap")
	rv.Help.SetText(help)
	body := tview.NewFlex().
		AddItem(rv.List, 0, 2, true).
		AddItem(rv.Details, 0, 3, false)
	content := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(rv.Header, 1, 0, false).
		AddItem(body, 0, 1, true).
		AddItem(rv.Help, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).SetBorder(true).SetTitle(" Roadmap ").SetTitleColor(theme.Foreground)
	content.SetBorderColor(theme.Accent)
	content.SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)

	const width = 108
	const height = 24
	vertical := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, height, 0, true).
		AddItem(nil, 0, 1, false)
	rv.modal = newResponsiveWorkspaceModal(theme.Background, vertical, width, nil)
}

// Show presents a complete roadmap snapshot.
func (rv *RoadmapView) Show(options RoadmapViewOptions) {
	if rv == nil {
		return
	}
	rv.CancelArchive()
	rv.options = options
	rv.initiatives = cloneInitiatives(options.Initiatives)
	rv.updates = cloneProjectUpdates(options.ProjectUpdates)
	if len(rv.updates) == 0 && len(options.Updates) > 0 {
		rv.updates = cloneProjectUpdates(options.Updates)
	}
	rv.archivePending = false
	rv.pendingArchive = nil
	rv.pendingAction = roadmapPendingAction{}
	rv.buildModal(rv.theme(), rv.density())
	rv.refreshRows()
	if rv.app != nil && rv.app.pages != nil {
		rv.app.pages.AddPage(roadmapPageName, rv.modal, true, true)
		rv.app.pages.SendToFront(roadmapPageName)
	}
	rv.focus()
}

// SetData replaces data while preserving expansion and selected IDs where
// those IDs still exist.
func (rv *RoadmapView) SetData(options RoadmapViewOptions) {
	if rv == nil {
		return
	}
	wasVisible := rv.app != nil && rv.app.pages != nil && rv.app.pages.HasPage(roadmapPageName)
	rv.options = options
	rv.initiatives = cloneInitiatives(options.Initiatives)
	rv.updates = cloneProjectUpdates(options.ProjectUpdates)
	if len(rv.updates) == 0 && len(options.Updates) > 0 {
		rv.updates = cloneProjectUpdates(options.Updates)
	}
	rv.refreshRows()
	if wasVisible {
		rv.focus()
	}
}

// SetInitiatives updates only the initiative/project hierarchy.
func (rv *RoadmapView) SetInitiatives(initiatives []linearapi.Initiative) {
	options := rv.options
	options.Initiatives = initiatives
	rv.SetData(options)
}

// SetProjectUpdates replaces updates for one project.
func (rv *RoadmapView) SetProjectUpdates(projectID string, updates []linearapi.ProjectUpdate) {
	if rv == nil {
		return
	}
	if rv.updates == nil {
		rv.updates = make(map[string][]linearapi.ProjectUpdate)
	}
	rv.updates[projectID] = append([]linearapi.ProjectUpdate(nil), updates...)
	rv.options.ProjectUpdates = cloneProjectUpdates(rv.updates)
	rv.refreshRows()
}

// Hide removes the roadmap page.
func (rv *RoadmapView) Hide() {
	if rv == nil {
		return
	}
	if rv.app != nil && rv.app.roadmapActionRunner != nil {
		rv.app.roadmapActionRunner.Invalidate()
	}
	rv.CancelArchive()
	if rv.app != nil && rv.app.pages != nil {
		rv.app.pages.RemovePage(roadmapPageName)
		if rv.app.app != nil && rv.app.navigationTree != nil {
			rv.app.updateFocus()
		}
	}
}

// HandleKey handles navigation and action shortcuts.
func (rv *RoadmapView) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if rv == nil || event == nil {
		return event
	}
	if rv.archivePending {
		if event.Key() == tcell.KeyEscape {
			rv.CancelArchive()
			return nil
		}
		return event
	}
	switch event.Key() {
	case tcell.KeyEscape:
		rv.Hide()
		return nil
	case tcell.KeyUp:
		rv.move(-1)
		return nil
	case tcell.KeyDown:
		rv.move(1)
		return nil
	case tcell.KeyEnter, tcell.KeyRune:
		if event.Key() == tcell.KeyRune {
			switch event.Rune() {
			case 'j':
				rv.move(1)
				return nil
			case 'k':
				rv.move(-1)
				return nil
			case ' ':
				rv.toggleCurrent()
				return nil
			case 'n':
				rv.createInitiative()
				return nil
			case 'c':
				rv.createCurrent()
				return nil
			case 'e':
				rv.editCurrent()
				return nil
			case 'a':
				rv.requestArchive()
				return nil
			case 'd':
				rv.requestDelete()
				return nil
			}
		} else {
			rv.toggleCurrent()
			return nil
		}
	}
	return event
}

// GetModal returns the shell primitive for page registration.
func (rv *RoadmapView) GetModal() *tview.Flex {
	if rv == nil {
		return nil
	}
	return rv.modal
}

// SelectedID returns the stable ID of the highlighted row.
func (rv *RoadmapView) SelectedID() string {
	if rv == nil {
		return ""
	}
	return rv.selectedID
}

// ArchivePending reports whether an archive/delete request is awaiting
// confirmation. The name is retained for compatibility with project-update
// callers.
func (rv *RoadmapView) ArchivePending() bool {
	return rv != nil && rv.archivePending
}

// ConfirmArchive explicitly confirms the pending archive/delete action. The
// method name is retained for compatibility with project-update callers.
func (rv *RoadmapView) ConfirmArchive() {
	if rv == nil || !rv.archivePending {
		return
	}
	if !rv.dataReady() {
		rv.CancelArchive()
		return
	}
	pending := rv.pendingAction
	rv.archivePending = false
	rv.pendingArchive = nil
	rv.pendingAction = roadmapPendingAction{}
	rv.removeOwnedConfirmationPage()
	switch pending.kind {
	case roadmapPendingArchiveUpdate:
		if pending.update != nil {
			if callback := rv.archiveCallback(); callback != nil {
				callback(*pending.update)
			}
		}
	case roadmapPendingArchiveInitiative:
		if pending.initiative != nil {
			if callback := rv.archiveInitiativeCallback(); callback != nil {
				callback(*pending.initiative)
			}
		}
	case roadmapPendingDeleteInitiative:
		if pending.initiative != nil {
			if callback := rv.deleteInitiativeCallback(); callback != nil {
				callback(*pending.initiative)
			}
		}
	case roadmapPendingDeleteProject:
		if pending.project != nil {
			if callback := rv.deleteProjectCallback(); callback != nil {
				callback(*pending.project)
			}
		}
	}
	rv.focus()
}

// CancelArchive cancels a pending archive request without invoking callbacks.
func (rv *RoadmapView) CancelArchive() {
	if rv == nil {
		return
	}
	rv.archivePending = false
	rv.pendingArchive = nil
	rv.pendingAction = roadmapPendingAction{}
	rv.removeOwnedConfirmationPage()
	rv.focus()
}

// removeOwnedConfirmationPage removes the shared confirmation page only when
// it still contains the exact primitive created for this view.
func (rv *RoadmapView) removeOwnedConfirmationPage() {
	if rv == nil {
		return
	}
	owned := rv.archiveConfirmationPage
	rv.archiveConfirmationPage = nil
	if owned == nil || rv.app == nil || rv.app.pages == nil {
		return
	}
	if rv.app.confirmationModal != nil {
		rv.app.confirmationModal.release(owned)
	}
	if rv.app.pages.GetPage("confirmation") == owned {
		rv.app.pages.RemovePage("confirmation")
	}
}

func (rv *RoadmapView) refreshRows() {
	previous := rv.selectedID
	rv.rows = nil
	rv.selectedID = ""
	if rv.options.Loading {
		rv.List.Clear()
		rv.List.AddItem("Loading roadmap...", "", 0, nil)
		rv.Details.SetText("Loading roadmap...")
		rv.updateHelp()
		return
	}
	if rv.options.Error != nil {
		rv.List.Clear()
		rv.List.AddItem("Error: "+rv.options.Error.Error(), "", 0, nil)
		rv.Details.SetText(rv.options.Error.Error())
		rv.updateHelp()
		return
	}
	for index := range rv.initiatives {
		initiative := &rv.initiatives[index]
		rv.rows = append(rv.rows, roadmapRow{kind: roadmapInitiativeRow, id: initiative.ID, initiativeID: initiative.ID, initiative: initiative})
		if !rv.expanded[initiative.ID] {
			continue
		}
		for projectIndex := range initiative.Projects {
			project := &initiative.Projects[projectIndex]
			rv.rows = append(rv.rows, roadmapRow{kind: roadmapProjectRow, id: project.ID, initiativeID: initiative.ID, projectID: project.ID, project: project, depth: 1})
			if !rv.expanded[project.ID] {
				continue
			}
			for updateIndex := range rv.updates[project.ID] {
				update := &rv.updates[project.ID][updateIndex]
				rv.rows = append(rv.rows, roadmapRow{kind: roadmapUpdateRow, id: update.ID, initiativeID: initiative.ID, projectID: project.ID, update: update, depth: 2})
			}
		}
	}
	rv.List.Clear()
	if len(rv.rows) == 0 {
		rv.List.AddItem("No initiatives", "", 0, nil)
		rv.Details.SetText("No roadmap initiatives available.")
		rv.updateHelp()
		return
	}
	selectedIndex := 0
	for index, row := range rv.rows {
		label := rv.rowLabel(row)
		rv.List.AddItem(label, "", 0, nil)
		if row.id == previous {
			selectedIndex = index
		}
	}
	rv.List.SetCurrentItem(selectedIndex)
	rv.syncSelection(selectedIndex)
	rv.updateHelp()
}

func (rv *RoadmapView) rowLabel(row roadmapRow) string {
	prefix := strings.Repeat("  ", row.depth)
	marker := " "
	if row.kind != roadmapUpdateRow {
		if rv.expanded[row.id] {
			marker = "-"
		} else {
			marker = "+"
		}
	}
	switch row.kind {
	case roadmapInitiativeRow:
		return fmt.Sprintf("%s%s %s", prefix, marker, row.initiative.Name)
	case roadmapProjectRow:
		return fmt.Sprintf("%s%s %s", prefix, marker, row.project.Name)
	case roadmapUpdateRow:
		return fmt.Sprintf("%s- %s", prefix, compactRoadmapBody(row.update.Body))
	default:
		return prefix + row.id
	}
}

func compactRoadmapBody(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if len([]rune(body)) > 72 {
		return string([]rune(body)[:69]) + "..."
	}
	return body
}

func (rv *RoadmapView) syncSelection(index int) {
	if index < 0 || index >= len(rv.rows) {
		return
	}
	rv.selectedID = rv.rows[index].id
	rv.renderDetails(rv.rows[index])
	rv.updateHelp()
}

func (rv *RoadmapView) renderDetails(row roadmapRow) {
	var text string
	switch row.kind {
	case roadmapInitiativeRow:
		initiative := row.initiative
		text = fmt.Sprintf("%s\n\nStatus: %s\nTarget: %s\n\n%s", initiative.Name, initiative.Status, initiative.TargetDate, initiative.Description)
	case roadmapProjectRow:
		text = fmt.Sprintf("%s\n\nProject ID: %s\n\nSelect to view project updates.", row.project.Name, row.project.ID)
	case roadmapUpdateRow:
		update := row.update
		author := update.Author.DisplayName
		if author == "" {
			author = update.Author.Name
		}
		text = fmt.Sprintf("Project update\n\nHealth: %s\nAuthor: %s\n\n%s", update.Health, author, update.Body)
	}
	rv.Details.SetText(strings.TrimSpace(text))
}

func (rv *RoadmapView) move(delta int) {
	if !rv.dataReady() {
		return
	}
	index := rv.List.GetCurrentItem() + delta
	if index < 0 {
		index = 0
	}
	if index >= len(rv.rows) {
		index = len(rv.rows) - 1
	}
	rv.List.SetCurrentItem(index)
	rv.syncSelection(index)
}

func (rv *RoadmapView) toggleCurrent() {
	index := rv.List.GetCurrentItem()
	if index < 0 || index >= len(rv.rows) {
		return
	}
	row := rv.rows[index]
	if row.kind == roadmapUpdateRow {
		return
	}
	rv.expanded[row.id] = !rv.expanded[row.id]
	rv.refreshRows()
	for index, candidate := range rv.rows {
		if candidate.id == row.id {
			rv.List.SetCurrentItem(index)
			rv.syncSelection(index)
			return
		}
	}
}

func (rv *RoadmapView) createCurrent() {
	row := rv.currentRow()
	if row == nil {
		return
	}
	if row.kind == roadmapInitiativeRow {
		if callback := rv.createProjectCallback(); callback != nil {
			callback(row.initiative.ID)
		}
		return
	}
	projectID := row.projectID
	if row.kind == roadmapProjectRow {
		projectID = row.project.ID
	}
	if row.kind == roadmapInitiativeRow || projectID == "" {
		return
	}
	if callback := rv.createCallback(); callback != nil {
		callback(projectID)
	}
}

func (rv *RoadmapView) createInitiative() {
	if callback := rv.createInitiativeCallback(); callback != nil {
		callback()
	}
}

func (rv *RoadmapView) editCurrent() {
	row := rv.currentRow()
	if row == nil {
		return
	}
	switch row.kind {
	case roadmapInitiativeRow:
		if row.initiative != nil {
			if callback := rv.editInitiativeCallback(); callback != nil {
				callback(*row.initiative)
			}
		}
	case roadmapProjectRow:
		if row.project != nil {
			if callback := rv.editProjectCallback(); callback != nil {
				callback(*row.project)
			}
		}
	case roadmapUpdateRow:
		if row.update != nil {
			if callback := rv.editCallback(); callback != nil {
				callback(*row.update)
			}
		}
	}
}

func (rv *RoadmapView) requestArchive() {
	row := rv.currentRow()
	if row == nil {
		return
	}
	switch row.kind {
	case roadmapInitiativeRow:
		if row.initiative == nil {
			return
		}
		initiative := *row.initiative
		rv.setPendingAction(roadmapPendingAction{kind: roadmapPendingArchiveInitiative, initiative: &initiative}, "Archive initiative", "Archive this initiative?", "Archive")
	case roadmapProjectRow:
		if row.project == nil {
			return
		}
		project := *row.project
		rv.setPendingAction(roadmapPendingAction{kind: roadmapPendingDeleteProject, project: &project}, "Delete project", "Delete this project?", "Delete")
	case roadmapUpdateRow:
		if row.update == nil {
			return
		}
		update := *row.update
		rv.setPendingAction(roadmapPendingAction{kind: roadmapPendingArchiveUpdate, update: &update}, "Archive project update", "Archive this project update?", "Archive")
	}
}

func (rv *RoadmapView) requestDelete() {
	row := rv.currentRow()
	if row == nil {
		return
	}
	switch row.kind {
	case roadmapInitiativeRow:
		if row.initiative == nil {
			return
		}
		initiative := *row.initiative
		rv.setPendingAction(roadmapPendingAction{kind: roadmapPendingDeleteInitiative, initiative: &initiative}, "Delete initiative", "Delete this initiative?", "Delete")
	case roadmapProjectRow:
		if row.project == nil {
			return
		}
		project := *row.project
		rv.setPendingAction(roadmapPendingAction{kind: roadmapPendingDeleteProject, project: &project}, "Delete project", "Delete this project?", "Delete")
	}
}

func (rv *RoadmapView) setPendingAction(action roadmapPendingAction, title, prompt, confirmLabel string) {
	rv.archivePending = true
	rv.pendingAction = action
	rv.pendingArchive = action.update
	if rv.app != nil && rv.app.confirmationModal != nil && rv.app.pages != nil {
		rv.app.confirmationModal.Show(title, prompt, confirmLabel, rv.ConfirmArchive, rv.CancelArchive)
		rv.archiveConfirmationPage = rv.app.confirmationModal.modal
	}
}

func (rv *RoadmapView) currentRow() *roadmapRow {
	if !rv.dataReady() {
		return nil
	}
	index := rv.List.GetCurrentItem()
	if index < 0 || index >= len(rv.rows) {
		return nil
	}
	return &rv.rows[index]
}

func (rv *RoadmapView) createCallback() func(string) {
	if rv.options.Actions.OnCreateProjectUpdate != nil {
		return rv.options.Actions.OnCreateProjectUpdate
	}
	if rv.options.Actions.CreateProjectUpdate != nil {
		return rv.options.Actions.CreateProjectUpdate
	}
	if rv.options.Callbacks.OnCreateProjectUpdate != nil {
		return rv.options.Callbacks.OnCreateProjectUpdate
	}
	if rv.options.OnCreateProjectUpdate != nil {
		return rv.options.OnCreateProjectUpdate
	}
	return nil
}

func (rv *RoadmapView) createInitiativeCallback() func() {
	if rv.options.Actions.OnCreateInitiative != nil {
		return rv.options.Actions.OnCreateInitiative
	}
	if rv.options.Actions.CreateInitiative != nil {
		return rv.options.Actions.CreateInitiative
	}
	if rv.options.Callbacks.OnCreateInitiative != nil {
		return rv.options.Callbacks.OnCreateInitiative
	}
	if rv.options.OnCreateInitiative != nil {
		return rv.options.OnCreateInitiative
	}
	return nil
}

func (rv *RoadmapView) createProjectCallback() func(string) {
	if rv.options.Actions.OnCreateProject != nil {
		return rv.options.Actions.OnCreateProject
	}
	if rv.options.Actions.CreateProject != nil {
		return rv.options.Actions.CreateProject
	}
	if rv.options.Callbacks.OnCreateProject != nil {
		return rv.options.Callbacks.OnCreateProject
	}
	if rv.options.OnCreateProject != nil {
		return rv.options.OnCreateProject
	}
	return nil
}

func (rv *RoadmapView) editCallback() func(linearapi.ProjectUpdate) {
	if rv.options.Actions.OnEditProjectUpdate != nil {
		return rv.options.Actions.OnEditProjectUpdate
	}
	if rv.options.Actions.EditProjectUpdate != nil {
		return rv.options.Actions.EditProjectUpdate
	}
	if rv.options.Callbacks.OnEditProjectUpdate != nil {
		return rv.options.Callbacks.OnEditProjectUpdate
	}
	if rv.options.OnEditProjectUpdate != nil {
		return rv.options.OnEditProjectUpdate
	}
	return nil
}

func (rv *RoadmapView) editInitiativeCallback() func(linearapi.Initiative) {
	if rv.options.Actions.OnEditInitiative != nil {
		return rv.options.Actions.OnEditInitiative
	}
	if rv.options.Actions.EditInitiative != nil {
		return rv.options.Actions.EditInitiative
	}
	if rv.options.Callbacks.OnEditInitiative != nil {
		return rv.options.Callbacks.OnEditInitiative
	}
	if rv.options.OnEditInitiative != nil {
		return rv.options.OnEditInitiative
	}
	return nil
}

func (rv *RoadmapView) editProjectCallback() func(linearapi.Project) {
	if rv.options.Actions.OnEditProject != nil {
		return rv.options.Actions.OnEditProject
	}
	if rv.options.Actions.EditProject != nil {
		return rv.options.Actions.EditProject
	}
	if rv.options.Callbacks.OnEditProject != nil {
		return rv.options.Callbacks.OnEditProject
	}
	if rv.options.OnEditProject != nil {
		return rv.options.OnEditProject
	}
	return nil
}

func (rv *RoadmapView) archiveCallback() func(linearapi.ProjectUpdate) {
	if rv.options.Actions.OnArchiveProjectUpdate != nil {
		return rv.options.Actions.OnArchiveProjectUpdate
	}
	if rv.options.Actions.ArchiveProjectUpdate != nil {
		return rv.options.Actions.ArchiveProjectUpdate
	}
	if rv.options.Callbacks.OnArchiveProjectUpdate != nil {
		return rv.options.Callbacks.OnArchiveProjectUpdate
	}
	if rv.options.OnArchiveProjectUpdate != nil {
		return rv.options.OnArchiveProjectUpdate
	}
	return nil
}

func (rv *RoadmapView) archiveInitiativeCallback() func(linearapi.Initiative) {
	if rv.options.Actions.OnArchiveInitiative != nil {
		return rv.options.Actions.OnArchiveInitiative
	}
	if rv.options.Actions.ArchiveInitiative != nil {
		return rv.options.Actions.ArchiveInitiative
	}
	if rv.options.Callbacks.OnArchiveInitiative != nil {
		return rv.options.Callbacks.OnArchiveInitiative
	}
	if rv.options.OnArchiveInitiative != nil {
		return rv.options.OnArchiveInitiative
	}
	return nil
}

func (rv *RoadmapView) deleteInitiativeCallback() func(linearapi.Initiative) {
	if rv.options.Actions.OnDeleteInitiative != nil {
		return rv.options.Actions.OnDeleteInitiative
	}
	if rv.options.Actions.DeleteInitiative != nil {
		return rv.options.Actions.DeleteInitiative
	}
	if rv.options.Callbacks.OnDeleteInitiative != nil {
		return rv.options.Callbacks.OnDeleteInitiative
	}
	if rv.options.OnDeleteInitiative != nil {
		return rv.options.OnDeleteInitiative
	}
	return nil
}

func (rv *RoadmapView) deleteProjectCallback() func(linearapi.Project) {
	if rv.options.Actions.OnDeleteProject != nil {
		return rv.options.Actions.OnDeleteProject
	}
	if rv.options.Actions.DeleteProject != nil {
		return rv.options.Actions.DeleteProject
	}
	if rv.options.Callbacks.OnDeleteProject != nil {
		return rv.options.Callbacks.OnDeleteProject
	}
	if rv.options.OnDeleteProject != nil {
		return rv.options.OnDeleteProject
	}
	return nil
}

func (rv *RoadmapView) focus() {
	if rv.app != nil && rv.app.app != nil && rv.List != nil {
		rv.app.app.SetFocus(rv.List)
	}
}

// dataReady reports whether the visible list contains real roadmap rows.
// Loading and error snapshots intentionally discard old rows so keyboard
// actions cannot mutate stale initiatives, projects, or updates.
func (rv *RoadmapView) dataReady() bool {
	return rv != nil && rv.List != nil && !rv.options.Loading && rv.options.Error == nil && len(rv.rows) > 0
}

func (rv *RoadmapView) updateHelp() {
	if rv == nil || rv.Help == nil {
		return
	}
	help := "n: new initiative | Esc: close"
	if rv.dataReady() {
		help = "j/k or arrows: navigate | n: new initiative | c: create | e: edit | a: archive"
		row := rv.currentRow()
		if row != nil {
			if row.kind != roadmapUpdateRow {
				help = "j/k or arrows: navigate | Enter/Space: expand | n: new initiative | c: create | e: edit | a: archive | d: delete"
			}
		}
	}
	rv.Help.SetText(help + " | Esc: close")
}

func (rv *RoadmapView) theme() Theme {
	if rv.app != nil {
		return rv.app.theme
	}
	return LinearTheme
}

func (rv *RoadmapView) density() DensityProfile {
	if rv.app != nil {
		return rv.app.density
	}
	return ResolveDensity("comfortable")
}

func cloneInitiatives(initiatives []linearapi.Initiative) []linearapi.Initiative {
	result := append([]linearapi.Initiative(nil), initiatives...)
	for index := range result {
		result[index].Projects = append([]linearapi.Project(nil), result[index].Projects...)
	}
	return result
}

func cloneProjectUpdates(updates map[string][]linearapi.ProjectUpdate) map[string][]linearapi.ProjectUpdate {
	if len(updates) == 0 {
		return make(map[string][]linearapi.ProjectUpdate)
	}
	result := make(map[string][]linearapi.ProjectUpdate, len(updates))
	keys := make([]string, 0, len(updates))
	for projectID := range updates {
		keys = append(keys, projectID)
	}
	sort.Strings(keys)
	for _, projectID := range keys {
		result[projectID] = append([]linearapi.ProjectUpdate(nil), updates[projectID]...)
	}
	return result
}
