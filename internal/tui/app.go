package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/cache"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// SortField represents a field to sort issues by.
type SortField string

const (
	SortByUpdatedAt SortField = "updatedAt"
	SortByCreatedAt SortField = "createdAt"
	SortByPriority  SortField = "priority"
)

// IssueFilters contains structured filters applied in addition to navigation.
type IssueFilters struct {
	AssigneeID   string
	AssigneeName string
	LabelIDs     []string
	LabelNames   []string
	StateID      string
	StateName    string
	ProjectID    string
	ProjectName  string
	CycleID      string
	CycleName    string
	DueDate      linearapi.DateFilter
	Estimate     linearapi.NumberFilter
}

func (f IssueFilters) Empty() bool {
	return f.AssigneeID == "" &&
		len(f.LabelIDs) == 0 &&
		f.StateID == "" &&
		f.ProjectID == "" &&
		f.CycleID == "" &&
		f.DueDate.Empty() &&
		f.Estimate.Empty()
}

func (f IssueFilters) Summary() string {
	parts := make([]string, 0, 8)
	if f.AssigneeID != "" {
		label := f.AssigneeName
		if label == "" {
			label = f.AssigneeID
		}
		parts = append(parts, "assignee="+label)
	}
	if len(f.LabelIDs) > 0 {
		labels := f.LabelNames
		if len(labels) == 0 {
			labels = f.LabelIDs
		}
		parts = append(parts, "labels="+strings.Join(labels, ","))
	}
	if f.StateID != "" {
		label := f.StateName
		if label == "" {
			label = f.StateID
		}
		parts = append(parts, "status="+label)
	}
	if f.ProjectID != "" {
		label := f.ProjectName
		if label == "" {
			label = f.ProjectID
		}
		parts = append(parts, "project="+label)
	}
	if f.CycleID != "" {
		label := f.CycleName
		if label == "" {
			label = f.CycleID
		}
		parts = append(parts, "cycle="+label)
	}
	if !f.DueDate.Empty() {
		parts = append(parts, "due="+formatDateFilterSummary(f.DueDate))
	}
	if !f.Estimate.Empty() {
		parts = append(parts, "estimate="+formatNumberFilterSummary(f.Estimate))
	}
	return strings.Join(parts, ", ")
}

func formatDateFilterSummary(filter linearapi.DateFilter) string {
	switch {
	case filter.Eq != "":
		return filter.Eq
	case filter.GTE != "":
		return ">=" + filter.GTE
	case filter.GT != "":
		return ">" + filter.GT
	case filter.LTE != "":
		return "<=" + filter.LTE
	case filter.LT != "":
		return "<" + filter.LT
	case filter.Null != nil && *filter.Null:
		return "none"
	case filter.Null != nil:
		return "set"
	default:
		return ""
	}
}

func formatNumberFilterSummary(filter linearapi.NumberFilter) string {
	switch {
	case filter.Eq != nil:
		return formatEstimate(filter.Eq)
	case filter.GTE != nil:
		return ">=" + formatEstimate(filter.GTE)
	case filter.GT != nil:
		return ">" + formatEstimate(filter.GT)
	case filter.LTE != nil:
		return "<=" + formatEstimate(filter.LTE)
	case filter.LT != nil:
		return "<" + formatEstimate(filter.LT)
	case filter.Null != nil && *filter.Null:
		return "none"
	case filter.Null != nil:
		return "set"
	default:
		return ""
	}
}

type triageNotificationLookupState struct {
	done          chan struct{}
	generation    int64
	notifications map[string]string
	err           error
}

const roadmapEditorPageName = "roadmap_editor"

type roadmapEditorKind uint8

const (
	roadmapEditorInitiative roadmapEditorKind = iota + 1
	roadmapEditorProject
)

type roadmapEditorValues struct {
	kind               roadmapEditorKind
	id                 string
	initiativeID       string
	name               string
	description        string
	descriptionChanged bool
	status             linearapi.InitiativeStatus
	targetDate         string
	teamID             string
	teamIDChanged      bool
}

// roadmapEditor is an App-owned form shell for the roadmap resource editors.
// It deliberately contains only presentation state; mutation callbacks stay
// on App so every network operation can share the roadmap action runner.
type roadmapEditor struct {
	app                *App
	modal              *tview.Flex
	form               *tview.Form
	name               *tview.InputField
	description        *tview.TextArea
	status             *tview.DropDown
	targetDate         *tview.InputField
	teamID             *tview.InputField
	kind               roadmapEditorKind
	id                 string
	initiativeID       string
	title              string
	descriptionInitial string
	teamIDInitial      string
	onSave             func(roadmapEditorValues)
}

func newRoadmapEditor(app *App) *roadmapEditor {
	editor := &roadmapEditor{app: app, title: "Roadmap"}
	editor.name = tview.NewInputField().SetLabel("Name: ")
	editor.description = tview.NewTextArea().SetLabel("Description: ").SetWrap(true).SetWordWrap(true).SetSize(3, 0)
	editor.status = tview.NewDropDown().SetLabel("Status: ")
	editor.targetDate = tview.NewInputField().SetLabel("Target date (YYYY-MM-DD): ")
	editor.teamID = tview.NewInputField().SetLabel("Team ID (blank clears): ")
	editor.form = tview.NewForm().SetItemPadding(0)
	editor.form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event == nil {
			return event
		}
		if event.Key() == tcell.KeyEscape {
			editor.Hide()
			return nil
		}
		if event.Key() == tcell.KeyEnter && event.Modifiers()&(tcell.ModCtrl|tcell.ModMeta) != 0 {
			editor.save()
			return nil
		}
		return event
	})
	editor.buildModal()
	return editor
}

func (editor *roadmapEditor) buildModal() {
	if editor == nil || editor.form == nil {
		return
	}
	theme := LinearTheme
	density := ResolveDensity(config.DefaultDensity)
	if editor.app != nil {
		theme = editor.app.theme
		density = editor.app.density
	}
	editor.form.SetBackgroundColor(theme.HeaderBg)
	editor.form.SetFieldBackgroundColor(theme.InputBg).
		SetFieldTextColor(theme.Foreground).
		SetButtonBackgroundColor(theme.Accent).
		SetButtonTextColor(theme.SelectionText).
		SetLabelColor(theme.Foreground)
	editor.name.SetFieldBackgroundColor(theme.InputBg)
	editor.name.SetFieldTextColor(theme.Foreground)
	editor.name.SetLabelColor(theme.Foreground)
	editor.description.SetBackgroundColor(theme.InputBg)
	editor.targetDate.SetFieldBackgroundColor(theme.InputBg)
	editor.targetDate.SetFieldTextColor(theme.Foreground)
	editor.targetDate.SetLabelColor(theme.Foreground)
	editor.teamID.SetFieldBackgroundColor(theme.InputBg)
	editor.teamID.SetFieldTextColor(theme.Foreground)
	editor.teamID.SetLabelColor(theme.Foreground)
	editor.status.SetFieldBackgroundColor(theme.InputBg)
	editor.status.SetFieldTextColor(theme.Foreground)
	editor.status.SetLabelColor(theme.Foreground)
	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(editor.form, 0, 1, true)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).SetBorder(true).SetBorderColor(theme.Accent).
		SetTitle(" "+editor.title+" ").SetTitleColor(theme.Foreground).
		SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)
	editor.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(content, 0, 2, true).
		AddItem(nil, 0, 1, false)
	editor.modal.SetBackgroundColor(theme.Background)
}

func (editor *roadmapEditor) show(values roadmapEditorValues) {
	if editor == nil {
		return
	}
	editor.kind = values.kind
	editor.id = strings.TrimSpace(values.id)
	editor.initiativeID = strings.TrimSpace(values.initiativeID)
	editor.descriptionInitial = values.description
	editor.teamIDInitial = strings.TrimSpace(values.teamID)
	if values.kind == roadmapEditorInitiative {
		editor.title = "Create initiative"
		if editor.id != "" {
			editor.title = "Edit initiative"
		}
	} else {
		editor.title = "Create project"
		if editor.id != "" {
			editor.title = "Edit project"
		}
	}
	editor.onSave = editor.app.applyRoadmapEditorValues
	editor.form.Clear(true)
	editor.form.AddFormItem(editor.name).AddFormItem(editor.description)
	if values.kind == roadmapEditorInitiative {
		editor.form.AddFormItem(editor.status).AddFormItem(editor.targetDate)
	} else {
		editor.form.AddFormItem(editor.teamID)
	}
	editor.form.AddButton("Save", editor.save).AddButton("Cancel", editor.Hide)
	editor.name.SetText(values.name)
	editor.description.SetText(values.description, false)
	editor.targetDate.SetText(values.targetDate)
	editor.teamID.SetText(values.teamID)
	statusValues := []string{"Proposed", "Planned", "Active", "Completed", "Canceled"}
	editor.status.SetOptions(statusValues, nil)
	status := values.status
	if status == "" {
		status = linearapi.InitiativeStatusProposed
	}
	statusIndex := 0
	for index, option := range statusValues {
		if strings.EqualFold(option, string(status)) {
			statusIndex = index
			break
		}
	}
	editor.status.SetCurrentOption(statusIndex)
	editor.buildModal()
	if editor.app != nil && editor.app.pages != nil {
		editor.app.pages.AddPage(roadmapEditorPageName, editor.modal, true, true)
		editor.app.pages.SendToFront(roadmapEditorPageName)
		if editor.app.app != nil {
			editor.app.app.SetFocus(editor.form)
		}
	}
}

func (editor *roadmapEditor) save() {
	if editor == nil {
		return
	}
	name := strings.TrimSpace(editor.name.GetText())
	if name == "" {
		if editor.app != nil {
			editor.app.updateStatusBarWithError(fmt.Errorf("roadmap name cannot be blank"))
		}
		return
	}
	if editor.kind == roadmapEditorProject && editor.id == "" && strings.TrimSpace(editor.teamID.GetText()) == "" {
		if editor.app != nil {
			editor.app.updateStatusBarWithError(fmt.Errorf("project creation requires a team"))
		}
		return
	}
	statusIndex, _ := editor.status.GetCurrentOption()
	statusValues := []linearapi.InitiativeStatus{
		linearapi.InitiativeStatusProposed,
		linearapi.InitiativeStatusPlanned,
		linearapi.InitiativeStatusActive,
		linearapi.InitiativeStatusCompleted,
		linearapi.InitiativeStatusCanceled,
	}
	status := linearapi.InitiativeStatusProposed
	if statusIndex >= 0 && statusIndex < len(statusValues) {
		status = statusValues[statusIndex]
	}
	description := editor.description.GetText()
	values := roadmapEditorValues{
		kind:               editor.kind,
		id:                 editor.id,
		initiativeID:       editor.initiativeID,
		name:               name,
		description:        description,
		descriptionChanged: editor.id == "" || description != editor.descriptionInitial,
		status:             status,
		targetDate:         strings.TrimSpace(editor.targetDate.GetText()),
		teamID:             strings.TrimSpace(editor.teamID.GetText()),
		teamIDChanged:      editor.id == "" || strings.TrimSpace(editor.teamID.GetText()) != editor.teamIDInitial,
	}
	onSave := editor.onSave
	editor.onSave = nil
	editor.Hide()
	if onSave != nil {
		onSave(values)
	}
}

func (editor *roadmapEditor) Hide() {
	if editor == nil {
		return
	}
	editor.onSave = nil
	if editor.app != nil && editor.app.pages != nil {
		editor.app.pages.RemovePage(roadmapEditorPageName)
		if editor.app.app != nil {
			editor.app.updateFocus()
		}
	}
}

func (editor *roadmapEditor) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if editor == nil || event == nil {
		return event
	}
	if event.Key() == tcell.KeyEscape {
		editor.Hide()
		return nil
	}
	if event.Key() == tcell.KeyEnter && event.Modifiers()&(tcell.ModCtrl|tcell.ModMeta) != 0 {
		editor.save()
		return nil
	}
	return event
}

// App is the main application controller that manages all UI components.
type App struct {
	app       *tview.Application
	api       *linearapi.Client
	cache     *cache.TeamCache
	config    config.Config
	theme     Theme
	themeTags ThemeTags
	density   DensityProfile

	// UI components
	pages                  *tview.Pages
	mainLayout             *tview.Flex
	navigationTree         *tview.TreeView
	issuesTable            *tview.Table // Legacy - kept for backward compatibility during migration
	myIssuesTable          *tview.Table
	otherIssuesTable       *tview.Table
	issuesColumn           *tview.Flex     // Vertical flex containing My/Other tables
	detailsView            *tview.Flex     // Flex container for details (description + comments)
	detailsDescriptionView *tview.TextView // Scrollable description/metadata view
	detailsCommentsView    *tview.TextView // Scrollable comments view
	statusBar              *tview.TextView
	paletteModal           *tview.Flex
	paletteInput           *tview.InputField
	paletteList            *tview.List
	paletteModalContent    *tview.Flex
	paletteCtrl            *PaletteController
	pickerModal            *PickerModal
	createIssueModal       *CreateIssueModal
	createCommentModal     *CreateCommentModal
	editTitleModal         *EditTitleModal
	issueEditorModal       *IssueEditorModal
	editLabelsModal        *EditLabelsModal
	textInputModal         *TextInputModal
	multiSelectModal       *MultiSelectModal
	settingsModal          *SettingsModal
	promptTemplatesModal   *AgentPromptTemplatesModal
	agentPromptModal       *AgentPromptModal
	agentOutputModal       *AgentOutputModal
	confirmationModal      *ConfirmationModal
	inboxView              *InboxView
	commentsModal          *CommentsModal
	roadmapView            *RoadmapView
	roadmapEditor          *roadmapEditor
	savedViewsModal        *SavedViewsModal
	triageActions          *TriageActions
	keyHelpModal           *tview.Flex
	agentRunner            *agents.Runner
	agentPromptTemplates   []config.AgentPromptTemplate

	// App state (protected by issuesMu)
	issuesMu             sync.RWMutex
	selectedIssue        *linearapi.Issue
	selectedNavigation   *NavigationNode
	issues               []linearapi.Issue
	markedIssueSelection MarkedIssueSelection
	focusedPane          FocusTarget
	activeIssuesSection  IssuesSection // Tracks which issues section (My/Other) is currently active

	// Issue tree state (for sub-issue hierarchy)
	// Legacy fields - kept for backward compatibility during migration
	issueRows []IssueRow                  // Flattened rows for table rendering
	idToIssue map[string]*linearapi.Issue // Quick lookup by issue ID
	// Per-section issue tree state
	myIssueRows    []IssueRow                  // Flattened rows for "My Issues" table
	myIDToIssue    map[string]*linearapi.Issue // Quick lookup by issue ID for "My Issues"
	otherIssueRows []IssueRow                  // Flattened rows for "Other Issues" table
	otherIDToIssue map[string]*linearapi.Issue // Quick lookup by issue ID for "Other Issues"
	expandedState  map[string]bool             // Expanded state for parent issues (shared across sections)

	// Filter/sort state
	searchQuery   string
	richFilters   IssueFilters
	sortField     SortField
	statusMessage string
	startupNotice string

	searchDebounceTimer      *time.Timer
	searchDebounceMu         sync.Mutex
	searchDebounceGeneration atomic.Int64

	// Cached metadata for currently selected team
	currentUser              *linearapi.User
	teamUsers                []linearapi.User
	teamProjects             []linearapi.Project
	workflowStates           []linearapi.WorkflowState
	teamCycles               []linearapi.Cycle
	teamMetadataMu           sync.RWMutex
	teamMetadataTeamID       string
	teamMetadataGeneration   atomic.Int64
	issueEditorGeneration    atomic.Int64
	issueSelectionGeneration atomic.Int64
	issueEditorSaveRunner    *AsyncViewActionRunner

	// Loading state
	isLoading                      bool
	pendingRefresh                 bool
	pendingRefreshIssueID          string
	pendingRefreshAllowFocusChange bool
	refreshStateMu                 sync.Mutex
	pickerActive                   bool
	refreshGeneration              atomic.Int64

	// Lazy loading helpers (overridable in tests)
	fetchIssuesPage         func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error)
	fetchIssueByID          func(context.Context, string) (linearapi.Issue, error)
	queueUpdateDraw         func(func())
	updateIssueFunc         func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error)
	archiveIssueFunc        func(context.Context, string) error
	createIssueRelationFunc func(context.Context, linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error)
	deleteIssueRelationFunc func(context.Context, string) error
	subscribeIssueFunc      func(context.Context, string) (linearapi.Issue, error)
	unsubscribeIssueFunc    func(context.Context, string) (linearapi.Issue, error)
	openURLFunc             func(string) error
	copyToClipboardFunc     func(string) error
	refreshCompleted        func()
	fetchProjectsFunc       func(context.Context, string) ([]linearapi.Project, error)
	fetchUsersFunc          func(context.Context, string) ([]linearapi.User, error)
	fetchWorkflowStatesFunc func(context.Context, string) ([]linearapi.WorkflowState, error)
	fetchCyclesFunc         func(context.Context, string) ([]linearapi.Cycle, error)
	fetchIssueLabelsFunc    func(context.Context, string) ([]linearapi.IssueLabel, error)
	preloadTeamMetadataFunc func(string)

	// Workspace-view API seams are kept on App so tests and embedders can use
	// deterministic snapshots without constructing a GraphQL server.
	listNotificationsFunc         func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error)
	markNotificationReadFunc      func(context.Context, string, time.Time) (linearapi.Notification, error)
	markNotificationUnreadFunc    func(context.Context, string) (linearapi.Notification, error)
	snoozeNotificationFunc        func(context.Context, string, time.Time) (linearapi.Notification, error)
	unsnoozeNotificationFunc      func(context.Context, string) (linearapi.Notification, error)
	archiveNotificationFunc       func(context.Context, string) error
	listInitiativesFunc           func(context.Context) ([]linearapi.Initiative, error)
	listProjectUpdatesFunc        func(context.Context, string) ([]linearapi.ProjectUpdate, error)
	createInitiativeFunc          func(context.Context, linearapi.CreateInitiativeInput) (linearapi.Initiative, error)
	createInitiativeToProjectFunc func(context.Context, linearapi.CreateInitiativeToProjectInput) error
	updateInitiativeFunc          func(context.Context, linearapi.UpdateInitiativeInput) (linearapi.Initiative, error)
	archiveInitiativeFunc         func(context.Context, string) error
	deleteInitiativeFunc          func(context.Context, string) error
	createProjectFunc             func(context.Context, linearapi.CreateProjectInput) (linearapi.Project, error)
	updateProjectFunc             func(context.Context, linearapi.UpdateProjectInput) (linearapi.Project, error)
	deleteProjectFunc             func(context.Context, string) error
	createProjectUpdateFunc       func(context.Context, linearapi.CreateProjectUpdateInput) (linearapi.ProjectUpdate, error)
	updateProjectUpdateFunc       func(context.Context, linearapi.UpdateProjectUpdateInput) (linearapi.ProjectUpdate, error)
	archiveProjectUpdateFunc      func(context.Context, string) error
	updateCommentFunc             func(context.Context, string, string) (linearapi.Comment, error)
	deleteCommentFunc             func(context.Context, string) error
	addCommentReactionFunc        func(context.Context, string, string) (linearapi.Reaction, error)
	removeCommentReactionFunc     func(context.Context, string) error
	listCustomViewsFunc           func(context.Context) ([]linearapi.CustomView, error)
	createCustomViewFunc          func(context.Context, linearapi.CreateCustomViewInput) (linearapi.CustomView, error)
	updateCustomViewFunc          func(context.Context, string, linearapi.UpdateCustomViewInput) (linearapi.CustomView, error)
	deleteCustomViewFunc          func(context.Context, string) error
	inboxLoadGeneration           atomic.Int64
	commentsLoadGeneration        atomic.Int64
	roadmapLoadGeneration         atomic.Int64
	roadmapActionRunner           *AsyncViewActionRunner
	savedViewsLoadGeneration      atomic.Int64
	savedViewsMutationRunner      *AsyncViewActionRunner
	bulkIssueActionRunner         *AsyncViewActionRunner
	triageActionRunner            *AsyncViewActionRunner
	roadmapInitiatives            []linearapi.Initiative
	roadmapProjectUpdates         map[string][]linearapi.ProjectUpdate
	triageNotificationMu          sync.Mutex
	triageNotificationIndex       map[string]string
	triageNotificationLookup      *triageNotificationLookupState
	triageNotificationGeneration  atomic.Int64
	triageActionMu                sync.Mutex
	triagePendingBatches          []TriageBatchInput

	// UI update mutex (for test safety when queueUpdateDraw executes immediately)
	uiUpdateMu sync.Mutex

	// Race-safety for issue detail fetching
	fetchingIssueID string // Tracks which issue ID we're currently fetching

	// Details pane sub-view focus
	focusedDetailsView     bool // false = description, true = comments
	detailsCommentsVisible bool // Tracks whether comments view is shown

	// Key-sequence navigation state. The dispatcher is pure; App owns clock,
	// status/help presentation, and destination execution.
	keySequenceDispatcher   *KeySequenceDispatcher
	keySequenceClock        func() time.Time
	keySequenceHint         string
	keySequenceConfigError  error
	navigateDestinationFunc func(KeySequenceDestination) bool
	quitFunc                func()
	startupUpdateCheck      func(context.Context) string
	startupUpdateOnce       sync.Once
}

// FocusTarget indicates which pane has focus.
type FocusTarget int

const (
	FocusNavigation FocusTarget = iota
	FocusIssues
	FocusDetails
	FocusPalette
)

// NewApp creates a new application instance.
func NewApp(api *linearapi.Client, cfg config.Config, templates []config.AgentPromptTemplate) *App {
	if len(templates) == 0 {
		templates = config.DefaultAgentPromptTemplates()
	}
	theme := ResolveTheme(cfg.Theme)
	density := ResolveDensity(cfg.Density)

	app := &App{
		app:                     tview.NewApplication(),
		api:                     api,
		cache:                   cache.NewTeamCache(api, cfg.CacheTTL),
		config:                  cfg,
		theme:                   theme,
		themeTags:               NewThemeTags(theme),
		density:                 density,
		pages:                   tview.NewPages(),
		focusedPane:             FocusNavigation,
		sortField:               SortByUpdatedAt,
		expandedState:           make(map[string]bool),
		idToIssue:               make(map[string]*linearapi.Issue),
		myIDToIssue:             make(map[string]*linearapi.Issue),
		otherIDToIssue:          make(map[string]*linearapi.Issue),
		activeIssuesSection:     IssuesSectionOther, // Default to Other section
		agentPromptTemplates:    templates,
		triageNotificationIndex: make(map[string]string),
	}

	app.paletteCtrl = NewPaletteController(DefaultCommands(app))
	app.fetchIssuesPage = api.FetchIssuesPage
	app.fetchIssueByID = api.FetchIssueByID
	app.updateIssueFunc = api.UpdateIssue
	app.archiveIssueFunc = api.ArchiveIssue
	app.createIssueRelationFunc = api.CreateIssueRelation
	app.deleteIssueRelationFunc = api.DeleteIssueRelation
	app.subscribeIssueFunc = api.SubscribeToIssue
	app.unsubscribeIssueFunc = api.UnsubscribeFromIssue
	app.openURLFunc = openURL
	app.copyToClipboardFunc = copyToClipboard
	app.keySequenceClock = time.Now
	app.quitFunc = app.app.Stop
	app.navigateDestinationFunc = app.navigateKeySequenceDestination
	app.keySequenceDispatcher, app.keySequenceConfigError = buildKeySequenceDispatcher(cfg.Keybindings)
	app.fetchProjectsFunc = app.cache.GetProjects
	app.fetchUsersFunc = app.cache.GetUsers
	app.fetchWorkflowStatesFunc = app.cache.GetWorkflowStates
	app.fetchCyclesFunc = app.cache.GetCycles
	app.fetchIssueLabelsFunc = app.cache.GetIssueLabels
	app.preloadTeamMetadataFunc = app.preloadTeamMetadata
	if api != nil {
		app.listCustomViewsFunc = api.ListCustomViews
		app.createCustomViewFunc = api.CreateCustomView
		app.updateCustomViewFunc = api.UpdateCustomView
		app.deleteCustomViewFunc = api.DeleteCustomView
	}
	if strings.TrimSpace(cfg.LinearAPIKey) != "" {
		app.listNotificationsFunc = api.ListNotifications
		app.markNotificationReadFunc = api.MarkNotificationRead
		app.markNotificationUnreadFunc = api.MarkNotificationUnread
		app.snoozeNotificationFunc = api.SnoozeNotification
		app.unsnoozeNotificationFunc = api.UnsnoozeNotification
		app.archiveNotificationFunc = api.ArchiveNotification
		app.listInitiativesFunc = api.ListInitiatives
		app.listProjectUpdatesFunc = api.ListProjectUpdates
		app.createInitiativeFunc = api.CreateInitiative
		app.createInitiativeToProjectFunc = api.CreateInitiativeToProject
		app.updateInitiativeFunc = api.UpdateInitiative
		app.archiveInitiativeFunc = api.ArchiveInitiative
		app.deleteInitiativeFunc = api.DeleteInitiative
		app.createProjectFunc = api.CreateProject
		app.updateProjectFunc = api.UpdateProject
		app.deleteProjectFunc = api.DeleteProject
		app.createProjectUpdateFunc = api.CreateProjectUpdate
		app.updateProjectUpdateFunc = api.UpdateProjectUpdate
		app.archiveProjectUpdateFunc = api.ArchiveProjectUpdate
		app.updateCommentFunc = api.UpdateComment
		app.deleteCommentFunc = api.DeleteComment
		app.addCommentReactionFunc = api.AddCommentReaction
		app.removeCommentReactionFunc = api.RemoveCommentReaction
	}
	app.queueUpdateDraw = func(f func()) {
		app.app.QueueUpdateDraw(f)
	}

	app.applyThemeStyles()

	app.buildLayout()
	app.bindGlobalKeys()
	return app
}

// Run starts the application and blocks until it exits.
func (a *App) Run() error {
	a.app.SetRoot(a.pages, true).EnableMouse(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load initial data asynchronously
	a.loadInitialData()
	a.startupUpdateOnce.Do(func() {
		if a.startupUpdateCheck == nil {
			return
		}
		notices := make(chan string, 1)
		go func(check func(context.Context) string) {
			checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
			defer checkCancel()
			if notice := strings.TrimSpace(check(checkCtx)); notice != "" {
				select {
				case notices <- notice:
					a.app.QueueEvent(tcell.NewEventResize(0, 0))
				case <-ctx.Done():
				}
			}
		}(a.startupUpdateCheck)
		a.app.SetBeforeDrawFunc(func(tcell.Screen) bool {
			select {
			case notice := <-notices:
				a.startupNotice = notice
				a.statusBar.SetText(fmt.Sprintf("%s%s[-]", a.themeTags.Accent, notice))
			default:
			}
			return false
		})
	})

	// Start the application event loop
	return a.app.Run()
}

// SetStartupUpdateCheck installs an optional once-per-process launch check.
func (a *App) SetStartupUpdateCheck(check func(context.Context) string) {
	if a != nil {
		a.startupUpdateCheck = check
	}
}

// loadInitialData fetches user, navigation, and issues in a background goroutine.
func (a *App) loadInitialData() {
	go func() {
		ctx := context.Background()

		// Fetch current user first
		user, err := a.cache.GetCurrentUser(ctx)
		if err == nil {
			a.currentUser = &user
			logger.Debug("tui.app: current user loaded user=%s", user.DisplayName)
		} else {
			logger.Warning("tui.app: failed to load current user error=%v", err)
		}

		// Fetch teams and build navigation. Default navigation triggers its own
		// refresh after applying the configured selection.
		if !a.loadNavigationData(ctx) {
			a.refreshIssues()
		}
	}()
}

// applySettings updates runtime dependencies to match a new configuration.
func (a *App) applySettings(newCfg config.Config) {
	oldCfg := a.config
	themeDensityChanged := oldCfg.Theme != newCfg.Theme || oldCfg.Density != newCfg.Density
	keybindingsChanged := !equalKeybindings(oldCfg.Keybindings, newCfg.Keybindings)
	loggerChanged := oldCfg.LogFile != newCfg.LogFile || oldCfg.LogLevel != newCfg.LogLevel
	dataReload := settingsRequireDataReload(oldCfg, newCfg)

	// Store the full config before applying UI hooks so command construction and
	// action-key lookups observe the new values. None of the UI-only branches
	// replace the API client/cache or clear loaded state.
	a.config = newCfg
	if loggerChanged {
		logLevel := parseLogLevel(newCfg.LogLevel)
		if err := logger.Reinit(newCfg.LogFile, logLevel); err != nil {
			logger.ErrorWithErr(err, "tui.app: failed to reinitialize logger")
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
		}
		logger.Debug("tui.app: logger settings applied log_file=%s log_level=%s", newCfg.LogFile, newCfg.LogLevel)
	}

	if themeDensityChanged {
		a.applyThemeAndDensity()
	}
	if keybindingsChanged {
		a.reapplyKeybindings()
		a.rebuildKeySequenceDispatcher()
	}
	if !themeDensityChanged && !keybindingsChanged {
		a.updateStatusBar()
	}

	if !dataReload {
		return
	}

	a.api = linearapi.NewClient(linearapi.ClientConfig{
		Token:    newCfg.LinearAPIKey,
		Endpoint: newCfg.APIEndpoint,
		Timeout:  newCfg.Timeout,
	})
	a.cache = cache.NewTeamCache(a.api, newCfg.CacheTTL)
	a.fetchIssuesPage = a.api.FetchIssuesPage
	a.fetchIssueByID = a.api.FetchIssueByID
	a.updateIssueFunc = a.api.UpdateIssue
	a.archiveIssueFunc = a.api.ArchiveIssue
	a.createIssueRelationFunc = a.api.CreateIssueRelation
	a.deleteIssueRelationFunc = a.api.DeleteIssueRelation
	a.subscribeIssueFunc = a.api.SubscribeToIssue
	a.unsubscribeIssueFunc = a.api.UnsubscribeFromIssue
	a.fetchProjectsFunc = a.cache.GetProjects
	a.fetchUsersFunc = a.cache.GetUsers
	a.fetchWorkflowStatesFunc = a.cache.GetWorkflowStates
	a.fetchCyclesFunc = a.cache.GetCycles
	a.fetchIssueLabelsFunc = a.cache.GetIssueLabels
	a.listCustomViewsFunc = a.api.ListCustomViews
	a.createCustomViewFunc = a.api.CreateCustomView
	a.updateCustomViewFunc = a.api.UpdateCustomView
	a.deleteCustomViewFunc = a.api.DeleteCustomView
	if strings.TrimSpace(newCfg.LinearAPIKey) != "" {
		a.listNotificationsFunc = a.api.ListNotifications
		a.markNotificationReadFunc = a.api.MarkNotificationRead
		a.markNotificationUnreadFunc = a.api.MarkNotificationUnread
		a.snoozeNotificationFunc = a.api.SnoozeNotification
		a.unsnoozeNotificationFunc = a.api.UnsnoozeNotification
		a.archiveNotificationFunc = a.api.ArchiveNotification
		a.listInitiativesFunc = a.api.ListInitiatives
		a.listProjectUpdatesFunc = a.api.ListProjectUpdates
		a.createInitiativeFunc = a.api.CreateInitiative
		a.createInitiativeToProjectFunc = a.api.CreateInitiativeToProject
		a.updateInitiativeFunc = a.api.UpdateInitiative
		a.archiveInitiativeFunc = a.api.ArchiveInitiative
		a.deleteInitiativeFunc = a.api.DeleteInitiative
		a.createProjectFunc = a.api.CreateProject
		a.updateProjectFunc = a.api.UpdateProject
		a.deleteProjectFunc = a.api.DeleteProject
		a.createProjectUpdateFunc = a.api.CreateProjectUpdate
		a.updateProjectUpdateFunc = a.api.UpdateProjectUpdate
		a.archiveProjectUpdateFunc = a.api.ArchiveProjectUpdate
		a.updateCommentFunc = a.api.UpdateComment
		a.deleteCommentFunc = a.api.DeleteComment
		a.addCommentReactionFunc = a.api.AddCommentReaction
		a.removeCommentReactionFunc = a.api.RemoveCommentReaction
	} else {
		a.listNotificationsFunc = nil
		a.markNotificationReadFunc = nil
		a.markNotificationUnreadFunc = nil
		a.snoozeNotificationFunc = nil
		a.unsnoozeNotificationFunc = nil
		a.archiveNotificationFunc = nil
		a.listInitiativesFunc = nil
		a.listProjectUpdatesFunc = nil
		a.createInitiativeFunc = nil
		a.createInitiativeToProjectFunc = nil
		a.updateInitiativeFunc = nil
		a.archiveInitiativeFunc = nil
		a.deleteInitiativeFunc = nil
		a.createProjectFunc = nil
		a.updateProjectFunc = nil
		a.deleteProjectFunc = nil
		a.createProjectUpdateFunc = nil
		a.updateProjectUpdateFunc = nil
		a.archiveProjectUpdateFunc = nil
		a.updateCommentFunc = nil
		a.deleteCommentFunc = nil
		a.addCommentReactionFunc = nil
		a.removeCommentReactionFunc = nil
	}

	logger.Debug("tui.app: resetting cached state after data-affecting settings change")
	a.resetCachedState()
	a.loadInitialData()
}

// settingsRequireDataReload identifies configuration changes that invalidate
// API/cache-backed state. UI composition, visual preferences, keybindings,
// search timing, and agent preferences deliberately stay on the live path.
func settingsRequireDataReload(oldCfg, newCfg config.Config) bool {
	return oldCfg.LinearAPIKey != newCfg.LinearAPIKey ||
		oldCfg.APIEndpoint != newCfg.APIEndpoint ||
		oldCfg.Timeout != newCfg.Timeout ||
		oldCfg.PageSize != newCfg.PageSize ||
		oldCfg.CacheTTL != newCfg.CacheTTL ||
		oldCfg.DefaultTeam != newCfg.DefaultTeam ||
		oldCfg.DefaultProject != newCfg.DefaultProject
}

func equalKeybindings(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

// reapplyKeybindings refreshes command shortcuts in the existing palette
// controller and redraws its list without recreating the modal primitive.
func (a *App) reapplyKeybindings() {
	if a.paletteCtrl == nil {
		return
	}
	query := a.paletteCtrl.Query()
	cursor := a.paletteCtrl.Cursor()
	searchMode := a.paletteCtrl.IsSearchMode()
	issueContext := a.paletteCtrl.issueContext
	a.paletteCtrl.commands = DefaultCommands(a)
	a.paletteCtrl.issueContext = issueContext
	a.paletteCtrl.SetSearchMode(searchMode)
	a.paletteCtrl.SetQuery(query)
	a.paletteCtrl.SetCursor(cursor)
	if a.paletteList != nil {
		a.refreshPaletteItemsInPlace()
	}
}

func (a *App) rebuildKeySequenceDispatcher() {
	dispatcher, err := buildKeySequenceDispatcher(a.config.Keybindings)
	a.keySequenceDispatcher = dispatcher
	a.keySequenceConfigError = err
	if err != nil {
		logger.Warning("tui.app: key sequence bindings disabled error=%v", err)
	}
	if a.statusBar != nil {
		a.updateStatusBar()
	}
}

// refreshPaletteItemsInPlace redraws the current command rows without
// rebuilding the palette modal container. This keeps a settings save that
// changes keybindings from replacing the palette primitive or its modal page.
func (a *App) refreshPaletteItemsInPlace() {
	if a.paletteList == nil || a.paletteCtrl == nil {
		return
	}
	a.paletteList.Clear()
	for _, cmd := range a.paletteCtrl.Filtered() {
		shortcutHint := cmd.ShortcutDisplay
		if shortcutHint == "" && cmd.ShortcutRune != 0 {
			shortcutHint = FormatShortcut(cmd.ShortcutRune)
		}
		displayText := fmt.Sprintf("%s%8s[-]  %s", a.themeTags.SecondaryText, shortcutHint, cmd.Title)
		a.paletteList.AddItem(displayText, "", 0, nil)
	}
	if count := len(a.paletteCtrl.Filtered()); count > 0 {
		cursor := a.paletteCtrl.Cursor()
		if cursor >= count {
			cursor = count - 1
		}
		if cursor < 0 {
			cursor = 0
		}
		a.paletteList.SetCurrentItem(cursor)
	}
}

func (a *App) applyThemeAndDensity() {
	a.theme = ResolveTheme(a.config.Theme)
	a.themeTags = NewThemeTags(a.theme)
	a.density = ResolveDensity(a.config.Density)

	a.applyThemeStyles()
	a.applyThemeToComponents()
	a.applyDensityToComponents()
	a.rebuildModals()
	a.rebuildWorkspaceViewShells()
	a.updateStatusBar()
	a.updateDetailsView()
	a.updatePaletteList()
}

func (a *App) applyThemeStyles() {
	tview.Styles.PrimitiveBackgroundColor = a.theme.Background
	tview.Styles.ContrastBackgroundColor = a.theme.Background
	tview.Styles.MoreContrastBackgroundColor = a.theme.HeaderBg
	tview.Styles.BorderColor = a.theme.Border
	tview.Styles.TitleColor = a.theme.Foreground
	tview.Styles.GraphicsColor = a.theme.Border
	tview.Styles.PrimaryTextColor = a.theme.Foreground
	tview.Styles.SecondaryTextColor = a.theme.SecondaryText
	tview.Styles.TertiaryTextColor = a.theme.SecondaryText
	tview.Styles.InverseTextColor = a.theme.Background
	tview.Styles.ContrastSecondaryTextColor = a.theme.SecondaryText
}

func (a *App) applyThemeToComponents() {
	if a.navigationTree != nil {
		a.navigationTree.SetBackgroundColor(a.theme.Background).
			SetBorderColor(a.theme.Border).
			SetTitleColor(a.theme.Foreground)
		a.recolorNavigationTree()
	}

	if a.myIssuesTable != nil {
		a.applyIssuesTableTheme(a.myIssuesTable)
		renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, a.selectedIssueID(IssuesSectionMy), a.theme, &a.markedIssueSelection)
	}
	if a.otherIssuesTable != nil {
		a.applyIssuesTableTheme(a.otherIssuesTable)
		renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, a.selectedIssueID(IssuesSectionOther), a.theme, &a.markedIssueSelection)
	}

	if a.detailsDescriptionView != nil {
		a.detailsDescriptionView.SetTitleColor(a.theme.Foreground).
			SetBorderColor(a.theme.Border).
			SetBackgroundColor(a.theme.Background)
	}
	if a.detailsCommentsView != nil {
		a.detailsCommentsView.SetTitleColor(a.theme.Foreground).
			SetBorderColor(a.theme.Border).
			SetBackgroundColor(a.theme.Background)
	}

	if a.statusBar != nil {
		a.statusBar.SetBackgroundColor(a.theme.HeaderBg)
	}
}

func (a *App) applyDensityToComponents() {
	if a.detailsDescriptionView != nil {
		padding := a.density.DetailsPadding
		a.detailsDescriptionView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.detailsCommentsView != nil {
		padding := a.density.DetailsPadding
		a.detailsCommentsView.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.statusBar != nil {
		padding := a.density.StatusBarPadding
		a.statusBar.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
	if a.agentOutputModal != nil {
		a.agentOutputModal.ApplyDensity(a.density)
	}
}

func (a *App) rebuildModals() {
	a.invalidateIssueEditorSaveContext()
	if a.pages != nil {
		a.pages.RemovePage("palette")
		a.pages.RemovePage("issue_editor")
	}
	a.paletteModal = a.buildPaletteModal()
	if a.pages != nil {
		a.pages.AddPage("palette", a.paletteModal, true, false)
	}

	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
	a.issueEditorModal = NewIssueEditorModal(a)
	a.editLabelsModal = NewEditLabelsModal(a)
	a.textInputModal = NewTextInputModal(a)
	a.multiSelectModal = NewMultiSelectModal(a)
	a.settingsModal = NewSettingsModal(a)
	a.promptTemplatesModal = NewAgentPromptTemplatesModal(a)
	a.agentPromptModal = NewAgentPromptModal(a)
	if a.pages == nil || !a.pages.HasPage("agent_output") {
		a.agentOutputModal = NewAgentOutputModal(a)
	} else {
		a.agentOutputModal.ApplyTheme(a.theme)
		a.agentOutputModal.ApplyDensity(a.density)
	}
	a.confirmationModal = NewConfirmationModal(a)
}

type issueEditorSaveSnapshot struct {
	issueID             string
	navigationKey       string
	selectionGeneration int64
	editorGeneration    int64
}

func (a *App) issueEditorSaveContextCurrent(snapshot issueEditorSaveSnapshot) bool {
	if a == nil || a.issueEditorGeneration.Load() != snapshot.editorGeneration {
		return false
	}
	if a.pages == nil || !a.pages.HasPage("issue_editor") || a.issueEditorModal == nil {
		return false
	}
	if navigationMutationKey(a.selectedNavigation) != snapshot.navigationKey {
		return false
	}
	if a.issueSelectionGeneration.Load() != snapshot.selectionGeneration {
		return false
	}
	issue := a.GetSelectedIssue()
	return issue != nil && issue.ID == snapshot.issueID
}

func (a *App) invalidateIssueEditorSaveContext() {
	if a == nil {
		return
	}
	a.issueEditorGeneration.Add(1)
	if a.issueEditorSaveRunner != nil {
		a.issueEditorSaveRunner.Invalidate()
	}
	if a.issueEditorModal != nil {
		a.issueEditorModal.setSaving(false)
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.statusMessage)), "saving issue") {
		a.statusMessage = ""
		if a.statusBar != nil {
			a.updateStatusBar()
		}
	}
}

func (a *App) runIssueEditorSave(snapshot issueEditorSaveSnapshot, identifier string, input linearapi.UpdateIssueInput, updateIssue func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error)) {
	if a == nil || updateIssue == nil || !a.issueEditorSaveContextCurrent(snapshot) {
		if a != nil && a.issueEditorModal != nil {
			a.issueEditorModal.setSaving(false)
		}
		return
	}
	runner := a.issueEditorSaveRunner
	if runner == nil {
		runner = NewAsyncViewActionRunner(a.QueueUpdateDraw)
		a.issueEditorSaveRunner = runner
	}
	if runner.InFlight() {
		a.issueEditorModal.setSaving(false)
		a.updateStatusBarWithError(fmt.Errorf("another issue save is already in progress"))
		return
	}
	a.statusMessage = fmt.Sprintf("Saving issue %s...", identifier)
	a.updateStatusBar()
	accepted := runner.RunResult("Update issue", func(ctx context.Context) error {
		_, err := updateIssue(ctx, input)
		return err
	}, func(result AsyncViewActionResult) {
		if !a.issueEditorSaveContextCurrent(snapshot) {
			return
		}
		if result.Err != nil {
			a.issueEditorModal.setSaving(false)
			logger.ErrorWithErr(result.Err, "tui.app: failed to update issue issue=%s", identifier)
			a.updateStatusBarWithError(result.Err)
			return
		}
		a.issueEditorModal.setSaving(false)
		a.issueEditorModal.Hide()
		a.flashStatus(fmt.Sprintf("Updated issue %s", identifier))
		go a.refreshIssues(snapshot.issueID)
	})
	if !accepted && a.issueEditorSaveContextCurrent(snapshot) {
		a.issueEditorModal.setSaving(false)
		a.updateStatusBarWithError(fmt.Errorf("another issue save is already in progress"))
	}
}

func (a *App) applyIssuesTableTheme(table *tview.Table) {
	if table == nil {
		return
	}
	table.SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border).
		SetBackgroundColor(a.theme.Background)
	table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(a.theme.SelectionText).
		Background(a.theme.SelectionBg).
		Bold(true))
}

func (a *App) recolorNavigationTree() {
	if a.navigationTree == nil {
		return
	}
	root := a.navigationTree.GetRoot()
	if root == nil {
		return
	}
	a.applyNavigationNodeColors(root)
}

func (a *App) applyNavigationNodeColors(node *tview.TreeNode) {
	if node == nil {
		return
	}
	ref := node.GetReference()
	if ref == nil {
		node.SetColor(a.theme.Accent)
	} else if navNode, ok := ref.(*NavigationNode); ok {
		if navNode.IsProject || navNode.IsStatus {
			node.SetColor(a.theme.SecondaryText)
		} else {
			node.SetColor(a.theme.Foreground)
		}
	}
	for _, child := range node.GetChildren() {
		a.applyNavigationNodeColors(child)
	}
}

func (a *App) selectedIssueID(section IssuesSection) string {
	var table *tview.Table
	switch section {
	case IssuesSectionMy:
		table = a.myIssuesTable
	case IssuesSectionOther:
		table = a.otherIssuesTable
	}
	if table == nil {
		return ""
	}
	row, _ := table.GetSelection()
	if row <= 0 {
		return ""
	}
	issue := a.getIssueFromRowForSection(row, section)
	if issue == nil {
		return ""
	}
	return issue.ID
}

type issueEditorTeamMetadata struct {
	teamID         string
	users          []linearapi.User
	projects       []linearapi.Project
	workflowStates []linearapi.WorkflowState
	cycles         []linearapi.Cycle
}

func cloneTeamUsers(users []linearapi.User) []linearapi.User {
	return append([]linearapi.User(nil), users...)
}

func cloneTeamProjects(projects []linearapi.Project) []linearapi.Project {
	return append([]linearapi.Project(nil), projects...)
}

func cloneWorkflowStates(states []linearapi.WorkflowState) []linearapi.WorkflowState {
	return append([]linearapi.WorkflowState(nil), states...)
}

func cloneTeamCycles(cycles []linearapi.Cycle) []linearapi.Cycle {
	return append([]linearapi.Cycle(nil), cycles...)
}

func (a *App) issueEditorTeamMetadataFor(teamID string) issueEditorTeamMetadata {
	if a == nil {
		return issueEditorTeamMetadata{}
	}
	a.teamMetadataMu.RLock()
	defer a.teamMetadataMu.RUnlock()
	if strings.TrimSpace(teamID) == "" || a.teamMetadataTeamID != teamID {
		return issueEditorTeamMetadata{teamID: teamID}
	}
	return issueEditorTeamMetadata{
		teamID:         a.teamMetadataTeamID,
		users:          cloneTeamUsers(a.teamUsers),
		projects:       cloneTeamProjects(a.teamProjects),
		workflowStates: cloneWorkflowStates(a.workflowStates),
		cycles:         cloneTeamCycles(a.teamCycles),
	}
}

func (a *App) publishIssueEditorTeamMetadata(teamID string, generation int64, users []linearapi.User, projects []linearapi.Project, states []linearapi.WorkflowState, cycles []linearapi.Cycle) bool {
	if a == nil || strings.TrimSpace(teamID) == "" || generation != a.teamMetadataGeneration.Load() {
		return false
	}
	a.teamMetadataMu.Lock()
	if generation != a.teamMetadataGeneration.Load() {
		a.teamMetadataMu.Unlock()
		return false
	}
	a.teamMetadataTeamID = teamID
	a.teamUsers = cloneTeamUsers(users)
	a.teamProjects = cloneTeamProjects(projects)
	a.workflowStates = cloneWorkflowStates(states)
	a.teamCycles = cloneTeamCycles(cycles)
	a.teamMetadataMu.Unlock()
	return true
}

func (a *App) clearIssueEditorTeamMetadata() {
	if a == nil {
		return
	}
	a.teamMetadataGeneration.Add(1)
	a.teamMetadataMu.Lock()
	a.teamMetadataTeamID = ""
	a.teamUsers = nil
	a.teamProjects = nil
	a.workflowStates = nil
	a.teamCycles = nil
	a.teamMetadataMu.Unlock()
}

// resetCachedState clears cached user and issue data after config changes.
func (a *App) resetCachedState() {
	a.invalidateTriageNotificationLookup()
	a.invalidateIssueMutationContexts()
	a.invalidateIssueEditorSaveContext()
	a.issuesMu.Lock()
	a.selectedIssue = nil
	a.issues = nil
	a.issueRows = nil
	a.idToIssue = make(map[string]*linearapi.Issue)
	a.myIssueRows = nil
	a.myIDToIssue = make(map[string]*linearapi.Issue)
	a.otherIssueRows = nil
	a.otherIDToIssue = make(map[string]*linearapi.Issue)
	a.issuesMu.Unlock()

	a.selectedNavigation = nil
	a.currentUser = nil
	a.clearIssueEditorTeamMetadata()
	a.issueSelectionGeneration.Add(1)
	a.richFilters = IssueFilters{}
	a.searchQuery = ""
	a.cancelSearchDebounce()
	a.activeIssuesSection = IssuesSectionOther
	a.expandedState = make(map[string]bool)
	a.roadmapInitiatives = nil
	a.roadmapProjectUpdates = make(map[string][]linearapi.ProjectUpdate)
	a.roadmapLoadGeneration.Add(1)
	if a.roadmapActionRunner != nil {
		a.roadmapActionRunner.Invalidate()
	}

	a.refreshStateMu.Lock()
	a.isLoading = false
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	a.pendingRefreshAllowFocusChange = true
	a.refreshStateMu.Unlock()
	// Bump generation to prevent in-flight refreshes from updating UI.
	a.refreshGeneration.Add(1)
	a.issuesMu.Lock()
	a.fetchingIssueID = ""
	a.issuesMu.Unlock()
}

// parseLogLevel converts a string log level to a logger.LogLevel.
func parseLogLevel(level string) logger.LogLevel {
	switch level {
	case "debug":
		return logger.LevelDebug
	case "info":
		return logger.LevelInfo
	case "warning":
		return logger.LevelWarning
	case "error":
		return logger.LevelError
	default:
		return logger.LevelWarning
	}
}

// loadNavigationData fetches teams and projects from the API and updates the
// navigation tree. It reports whether default navigation started the initial
// issue refresh.
func (a *App) loadNavigationData(ctx context.Context) bool {
	var teams []linearapi.Team
	var favorites []linearapi.Favorite
	var teamsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		teams, teamsErr = a.cache.GetTeams(ctx)
	}()
	go func() {
		defer wg.Done()
		fetched, err := a.api.ListFavorites(ctx)
		if err != nil {
			// Favorites only enhance the tree; render it without them on failure.
			logger.ErrorWithErr(err, "tui.app: failed to load favorites")
			return
		}
		favorites = fetched
	}()
	wg.Wait()

	if teamsErr != nil {
		logger.ErrorWithErr(teamsErr, "tui.app: failed to load teams")
		a.app.QueueUpdateDraw(func() {
			a.updateStatusBarWithError(teamsErr)
		})
		return false
	}

	logger.Debug("tui.app: loaded teams count=%d favorites_count=%d", len(teams), len(favorites))
	a.app.QueueUpdateDraw(func() {
		a.rebuildNavigationTree(teams, favorites)
	})
	return a.applyDefaultNavigation(ctx, teams)
}

// rebuildNavigationTree rebuilds the navigation tree with real data.
func (a *App) rebuildNavigationTree(teams []linearapi.Team, favorites []linearapi.Favorite) {
	root := tview.NewTreeNode("Linear").
		SetColor(a.theme.Accent).
		SetSelectable(false)

	// Add "All Issues" at the top
	allIssues := tview.NewTreeNode("All Issues").
		SetColor(a.theme.Foreground).
		SetReference(&NavigationNode{ID: "all", Text: "All Issues"}).
		SetExpanded(true)
	root.AddChild(allIssues)

	a.appendFavoritesSection(root, favorites)

	// Add teams
	for _, team := range teams {
		teamNode := tview.NewTreeNode(team.Name).
			SetColor(a.theme.Foreground).
			SetReference(&NavigationNode{
				ID:     team.ID,
				Text:   team.Name,
				IsTeam: true,
				TeamID: team.ID,
			}).
			SetExpanded(false)

		// Note: Team selection is handled by the tree's SetSelectedFunc in buildNavigationTree()
		// Do NOT set SetSelectedFunc here as it causes duplicate callbacks

		root.AddChild(teamNode)
	}

	a.navigationTree.SetRoot(root)
	a.navigationTree.SetCurrentNode(allIssues)
	a.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
}

// onTeamExpanded loads projects for a team when it's expanded.
func (a *App) onTeamExpanded(teamID string, teamNode *tview.TreeNode) {
	// If already has children (projects loaded), just toggle expand
	if len(teamNode.GetChildren()) > 0 {
		teamNode.SetExpanded(!teamNode.IsExpanded())
		return
	}

	// Load projects, workflow states, and cycles asynchronously.
	go func() {
		logger.Debug("tui.app: loading navigation children team_id=%s", teamID)
		ctx := context.Background()
		var projects []linearapi.Project
		var states []linearapi.WorkflowState
		var cycles []linearapi.Cycle
		var projectsErr, statesErr, cyclesErr error
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			projects, projectsErr = a.cache.GetProjects(ctx, teamID)
		}()
		go func() {
			defer wg.Done()
			states, statesErr = a.cache.GetWorkflowStates(ctx, teamID)
		}()
		go func() {
			defer wg.Done()
			cycles, cyclesErr = a.cache.GetCycles(ctx, teamID)
		}()
		wg.Wait()
		if projectsErr != nil {
			logger.ErrorWithErr(projectsErr, "tui.app: failed to load projects team_id=%s", teamID)
			a.app.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(projectsErr)
			})
			return
		}
		if statesErr != nil {
			logger.ErrorWithErr(statesErr, "tui.app: failed to load workflow states team_id=%s", teamID)
			a.app.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(statesErr)
			})
			return
		}
		if cyclesErr != nil {
			logger.ErrorWithErr(cyclesErr, "tui.app: failed to load cycles team_id=%s", teamID)
			a.app.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(cyclesErr)
			})
			return
		}
		logger.Debug("tui.app: loaded navigation children team_id=%s projects=%d states=%d cycles=%d", teamID, len(projects), len(states), len(cycles))

		a.app.QueueUpdateDraw(func() {
			// Double-check children haven't been added by another goroutine
			if len(teamNode.GetChildren()) > 0 {
				teamNode.SetExpanded(true)
				return
			}
			a.populateTeamNodeChildren(teamNode, teamID, projects, states, cycles)
			teamNode.SetExpanded(true)
		})
	}()
}

// populateTeamNodeChildren renders cycle, status, and project child nodes under a team node.
func (a *App) populateTeamNodeChildren(teamNode *tview.TreeNode, teamID string, projects []linearapi.Project, states []linearapi.WorkflowState, cycles []linearapi.Cycle) {
	if len(cycles) > 0 {
		sortCyclesForNavigation(cycles)
		cyclesGroup := tview.NewTreeNode("  Cycles").
			SetColor(a.theme.SecondaryText).
			SetSelectable(false).
			SetReference(&NavigationNode{
				ID:      fmt.Sprintf("%s-cycles", teamID),
				Text:    "Cycles",
				TeamID:  teamID,
				IsCycle: true,
			})
		for _, cycle := range cycles {
			label := cycle.DisplayName()
			switch {
			case cycle.IsActive:
				label += " (active)"
			case cycle.IsNext:
				label += " (next)"
			case cycle.IsPrevious:
				label += " (previous)"
			}
			cycleNode := tview.NewTreeNode("    " + label).
				SetColor(a.theme.SecondaryText).
				SetReference(&NavigationNode{
					ID:        cycle.ID,
					Text:      label,
					TeamID:    teamID,
					IsCycle:   true,
					CycleID:   cycle.ID,
					CycleName: cycle.DisplayName(),
				})
			cyclesGroup.AddChild(cycleNode)
		}
		teamNode.AddChild(cyclesGroup)
	}
	if len(states) > 0 {
		sort.Slice(states, func(i, j int) bool {
			return states[i].Position < states[j].Position
		})
		statusGroup := tview.NewTreeNode("  Status").
			SetColor(a.theme.SecondaryText).
			SetSelectable(false).
			SetReference(&NavigationNode{
				ID:       fmt.Sprintf("%s-status", teamID),
				Text:     "Status",
				TeamID:   teamID,
				IsStatus: true,
			})
		for _, state := range states {
			stateNode := tview.NewTreeNode("    " + state.Name).
				SetColor(a.theme.SecondaryText).
				SetReference(&NavigationNode{
					ID:        state.ID,
					Text:      state.Name,
					TeamID:    teamID,
					IsStatus:  true,
					StateID:   state.ID,
					StateName: state.Name,
					StateType: state.Type,
				})
			statusGroup.AddChild(stateNode)
		}
		teamNode.AddChild(statusGroup)
	}
	for _, proj := range projects {
		projNode := tview.NewTreeNode("  " + proj.Name).
			SetColor(a.theme.SecondaryText).
			SetReference(&NavigationNode{
				ID:        proj.ID,
				Text:      proj.Name,
				IsProject: true,
				TeamID:    teamID,
			})
		teamNode.AddChild(projNode)
	}
}

func sortCyclesForNavigation(cycles []linearapi.Cycle) {
	sort.SliceStable(cycles, func(i, j int) bool {
		leftRank := cycleNavigationRank(cycles[i])
		rightRank := cycleNavigationRank(cycles[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if cycles[i].IsFuture || cycles[i].IsNext {
			return cycles[i].StartsAt.Before(cycles[j].StartsAt)
		}
		return cycles[i].StartsAt.After(cycles[j].StartsAt)
	})
}

func cycleNavigationRank(cycle linearapi.Cycle) int {
	switch {
	case cycle.IsActive:
		return 0
	case cycle.IsNext:
		return 1
	case cycle.IsFuture:
		return 2
	case cycle.IsPrevious:
		return 3
	case cycle.IsPast:
		return 4
	default:
		return 5
	}
}

// buildLayout constructs the main UI layout.
func (a *App) buildLayout() {
	// Build all panes
	a.navigationTree = a.buildNavigationTree()
	// Build My Issues and Other Issues tables
	a.myIssuesTable = a.buildIssuesTable(" My Issues ", IssuesSectionMy)
	a.otherIssuesTable = a.buildIssuesTable(" Other Issues ", IssuesSectionOther)
	// Create vertical flex for issues column
	a.issuesColumn = tview.NewFlex().SetDirection(tview.FlexRow)
	// Initially show only Other Issues table (My Issues will be added when issues are loaded)
	a.issuesColumn.AddItem(a.otherIssuesTable, 0, 1, false)
	// Legacy table for backward compatibility (will be removed after migration)
	a.issuesTable = a.otherIssuesTable
	a.detailsView = a.buildDetailsView()
	a.statusBar = a.buildStatusBar()
	a.mainLayout = tview.NewFlex().SetDirection(tview.FlexRow)

	// Build palette modal
	a.paletteModal = a.buildPaletteModal()

	// Build picker and create issue modals
	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
	a.issueEditorModal = NewIssueEditorModal(a)
	a.editLabelsModal = NewEditLabelsModal(a)
	a.textInputModal = NewTextInputModal(a)
	a.multiSelectModal = NewMultiSelectModal(a)
	a.settingsModal = NewSettingsModal(a)
	a.promptTemplatesModal = NewAgentPromptTemplatesModal(a)
	a.agentPromptModal = NewAgentPromptModal(a)
	a.agentOutputModal = NewAgentOutputModal(a)
	a.confirmationModal = NewConfirmationModal(a)
	a.inboxView = NewInboxView(a)
	a.commentsModal = NewCommentsModal(a)
	a.roadmapView = NewRoadmapView(a)
	a.roadmapEditor = newRoadmapEditor(a)
	a.roadmapActionRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw, AsyncViewActionHooks{
		OnStart: func(name string) {
			a.flashStatus(name + "…")
		},
		OnProgress: func(_, message string) {
			if strings.TrimSpace(message) != "" {
				a.flashStatus(message)
			}
		},
		OnError: func(_ string, err error) {
			if err != nil {
				a.updateStatusBarWithError(err)
			}
		},
	})
	a.savedViewsMutationRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw)
	a.bulkIssueActionRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw, AsyncViewActionHooks{
		OnProgress: func(_, message string) {
			if strings.TrimSpace(message) == "" {
				return
			}
			a.statusMessage = message
			a.updateStatusBar()
		},
		OnError: func(_ string, err error) {
			if err != nil {
				a.updateStatusBarWithError(err)
			}
		},
	})
	a.triageActionRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw, AsyncViewActionHooks{
		OnProgress: func(_, message string) {
			if !a.isTriageContext() || strings.TrimSpace(message) == "" {
				return
			}
			a.statusMessage = message
			a.updateStatusBar()
		},
		OnError: func(_ string, err error) {
			if err != nil && a.isTriageContext() {
				a.updateStatusBarWithError(err)
			}
		},
	})
	a.issueEditorSaveRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw)
	a.savedViewsModal = NewSavedViewsModal(a)
	a.agentRunner = agents.NewRunner()

	// Add main layout to pages
	a.pages.AddPage("main", a.mainLayout, true, true)
	a.pages.AddPage("palette", a.paletteModal, true, false)
	a.composeShell()

	// Set initial focus
	a.updateFocus()
}

// rebuildWorkspaceViewShells recomposes only the page primitives for the
// workspace views. Their snapshots, expansion state, and stable selections
// remain owned by the view instances, so a mode/theme change never reloads
// network data.
func (a *App) rebuildWorkspaceViewShells() {
	if a == nil || a.pages == nil {
		return
	}
	if a.inboxView != nil {
		visible := a.pages.HasPage(inboxViewPageName)
		a.inboxView.buildModal()
		if visible {
			a.pages.AddPage(inboxViewPageName, a.inboxView.modal, true, true)
			a.pages.SendToFront(inboxViewPageName)
		}
	}
	if a.commentsModal != nil {
		visible := a.pages.HasPage(commentsModalPageName)
		a.commentsModal.buildModal()
		if visible {
			a.pages.AddPage(commentsModalPageName, a.commentsModal.modal, true, true)
			a.pages.SendToFront(commentsModalPageName)
		}
	}
	if a.roadmapView != nil {
		visible := a.pages.HasPage(roadmapPageName)
		a.roadmapView.buildModal(a.theme, a.density)
		a.roadmapView.refreshRows()
		if visible {
			a.pages.AddPage(roadmapPageName, a.roadmapView.modal, true, true)
			a.pages.SendToFront(roadmapPageName)
		}
	}
	if a.roadmapEditor != nil {
		visible := a.pages.HasPage(roadmapEditorPageName)
		a.roadmapEditor.buildModal()
		if visible {
			a.pages.AddPage(roadmapEditorPageName, a.roadmapEditor.modal, true, true)
			a.pages.SendToFront(roadmapEditorPageName)
		}
	}
	if a.savedViewsModal != nil {
		visible := a.pages.HasPage(savedViewsPageName)
		selectedID := a.savedViewsModal.SelectedID()
		a.savedViewsModal.buildModal(a.theme, a.density)
		a.savedViewsModal.selectedID = selectedID
		a.savedViewsModal.refreshList()
		if visible {
			a.pages.AddPage(savedViewsPageName, a.savedViewsModal.modal, true, true)
			a.pages.SendToFront(savedViewsPageName)
		}
	}
}

func (a *App) inboxCallbacks() InboxViewCallbacks {
	return InboxViewCallbacks{
		OnOpenIssue: func(issueID string) {
			if a.inboxView != nil {
				a.inboxView.Hide()
			}
			a.openIssueByID(issueID)
		},
		OnToggleReadContext: func(ctx context.Context, notificationID string, read bool) error {
			if read {
				callback := a.markNotificationReadFunc
				if callback == nil && a.api != nil {
					callback = a.api.MarkNotificationRead
				}
				if callback == nil {
					return fmt.Errorf("mark notification read unavailable")
				}
				_, err := callback(ctx, notificationID, time.Now())
				return err
			}
			callback := a.markNotificationUnreadFunc
			if callback == nil && a.api != nil {
				callback = a.api.MarkNotificationUnread
			}
			if callback == nil {
				return fmt.Errorf("mark notification unread unavailable")
			}
			_, err := callback(ctx, notificationID)
			return err
		},
		OnChooseSnooze: func(string) (time.Time, error) {
			return time.Now().Add(24 * time.Hour), nil
		},
		OnSnoozeContext: func(ctx context.Context, notificationID string, until time.Time) error {
			callback := a.snoozeNotificationFunc
			if callback == nil && a.api != nil {
				callback = a.api.SnoozeNotification
			}
			if callback == nil {
				return fmt.Errorf("snooze notification unavailable")
			}
			_, err := callback(ctx, notificationID, until)
			return err
		},
		OnUnsnoozeContext: func(ctx context.Context, notificationID string) error {
			callback := a.unsnoozeNotificationFunc
			if callback == nil && a.api != nil {
				callback = a.api.UnsnoozeNotification
			}
			if callback == nil {
				return fmt.Errorf("unsnooze notification unavailable")
			}
			_, err := callback(ctx, notificationID)
			return err
		},
		OnArchiveContext: func(ctx context.Context, notificationID string) error {
			callback := a.archiveNotificationFunc
			if callback == nil && a.api != nil {
				callback = a.api.ArchiveNotification
			}
			if callback == nil {
				return fmt.Errorf("archive notification unavailable")
			}
			return callback(ctx, notificationID)
		},
		OnRefreshContext: func(context.Context) error {
			return nil
		},
		OnActionComplete: func(_ string, _ string) {
			if a.pages != nil && a.pages.HasPage(inboxViewPageName) {
				a.reloadInbox()
			}
		},
		OnClose: func() {
			a.inboxLoadGeneration.Add(1)
		},
	}
}

func (a *App) openInbox() {
	a.closeWorkspacePagesExcept(inboxViewPageName)
	if a.inboxView == nil {
		a.inboxView = NewInboxView(a)
	}
	a.inboxView.Show(InboxViewOptions{
		Notifications: a.inboxView.VisibleNotifications(),
		SelectedID:    a.inboxView.SelectedNotificationID(),
	}, a.inboxCallbacks())
	a.inboxView.SetLoading(true)
	a.reloadInbox()
}

func (a *App) reloadInbox() {
	if a == nil || a.inboxView == nil {
		return
	}
	generation := a.inboxLoadGeneration.Add(1)
	a.inboxView.SetLoading(true)
	list := a.listNotificationsFunc
	if list == nil && strings.TrimSpace(a.config.LinearAPIKey) != "" && a.api != nil {
		list = a.api.ListNotifications
	}
	if list == nil {
		a.inboxView.SetError("Inbox loading unavailable")
		return
	}
	go func() {
		notifications, err := list(context.Background(), linearapi.NotificationListOptions{IncludeArchived: false})
		a.QueueUpdateDraw(func() {
			if generation != a.inboxLoadGeneration.Load() || a.pages == nil || !a.pages.HasPage(inboxViewPageName) {
				return
			}
			if err != nil {
				a.inboxView.SetError(err.Error())
				return
			}
			a.inboxView.Reload(notifications)
			a.inboxView.SetLoading(false)
		})
	}()
}

func (a *App) openIssueByID(issueID string) {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		a.flashStatus("Related issue unavailable")
		return
	}
	fetch := a.fetchIssueByID
	if fetch == nil && a.api != nil {
		fetch = a.api.FetchIssueByID
	}
	if fetch == nil {
		a.flashStatus("Related issue unavailable")
		return
	}
	// onIssueSelected owns the single full-issue fetch and its stale selection
	// guard. Seed only the related ID here so the inbox closes immediately.
	a.onIssueSelected(linearapi.Issue{ID: issueID})
}

func (a *App) commentsCallbacks(issueID string) CommentsModalCallbacks {
	return CommentsModalCallbacks{
		OnEditContext: func(ctx context.Context, commentID, body string) error {
			callback := a.updateCommentFunc
			if callback == nil && a.api != nil {
				callback = a.api.UpdateComment
			}
			if callback == nil {
				return fmt.Errorf("comment edit unavailable")
			}
			_, err := callback(ctx, commentID, body)
			return err
		},
		OnDeleteContext: func(ctx context.Context, commentID string) error {
			callback := a.deleteCommentFunc
			if callback == nil && a.api != nil {
				callback = a.api.DeleteComment
			}
			if callback == nil {
				return fmt.Errorf("comment delete unavailable")
			}
			return callback(ctx, commentID)
		},
		OnAddReactionContext: func(ctx context.Context, commentID, emoji string) error {
			callback := a.addCommentReactionFunc
			if callback == nil && a.api != nil {
				callback = a.api.AddCommentReaction
			}
			if callback == nil {
				return fmt.Errorf("comment reaction unavailable")
			}
			_, err := callback(ctx, commentID, emoji)
			return err
		},
		OnRemoveReactionContext: func(ctx context.Context, reactionID string) error {
			callback := a.removeCommentReactionFunc
			if callback == nil && a.api != nil {
				callback = a.api.RemoveCommentReaction
			}
			if callback == nil {
				return fmt.Errorf("comment reaction removal unavailable")
			}
			return callback(ctx, reactionID)
		},
		OnActionComplete: func(name, _ string) {
			switch name {
			case "edit comment", "delete comment", "add reaction", "remove reaction":
				a.refreshSelectedIssueComments(issueID)
			}
		},
		OnClose: func() {
			a.commentsLoadGeneration.Add(1)
		},
	}
}

func (a *App) openCommentsModal() {
	issue := a.GetSelectedIssue()
	if issue == nil || strings.TrimSpace(issue.ID) == "" {
		a.flashStatus("No issue selected")
		return
	}
	a.closeWorkspacePagesExcept(commentsModalPageName)
	if a.commentsModal == nil {
		a.commentsModal = NewCommentsModal(a)
	}
	comments := BuildCommentThreads(issue.Comments)
	currentUserID := ""
	if user := a.GetCurrentUser(); user != nil {
		currentUserID = user.ID
	}
	a.commentsModal.Show(CommentsModalOptions{
		IssueID:       issue.ID,
		CurrentUserID: currentUserID,
		Comments:      comments,
		SelectedID:    a.commentsModal.SelectedCommentID(),
	}, a.commentsCallbacks(issue.ID))
}

func (a *App) refreshSelectedIssueComments(issueID string) {
	generation := a.commentsLoadGeneration.Add(1)
	fetch := a.fetchIssueByID
	if fetch == nil && a.api != nil {
		fetch = a.api.FetchIssueByID
	}
	if fetch == nil {
		return
	}
	go func() {
		issue, err := fetch(context.Background(), issueID)
		a.QueueUpdateDraw(func() {
			if generation != a.commentsLoadGeneration.Load() {
				return
			}
			if err != nil {
				if a.commentsModal != nil {
					a.commentsModal.SetError(err.Error())
				}
				return
			}
			selected := a.GetSelectedIssue()
			if selected == nil || selected.ID != issueID {
				return
			}
			a.issuesMu.Lock()
			a.selectedIssue = &issue
			a.issuesMu.Unlock()
			a.updateDetailsView()
			if a.commentsModal != nil && a.pages.HasPage(commentsModalPageName) {
				threads := BuildCommentThreads(issue.Comments)
				a.commentsModal.Reload(threads)
			}
		})
	}()
}

func (a *App) roadmapActions() RoadmapViewActions {
	return RoadmapViewActions{
		OnCreateInitiative: func() {
			a.showRoadmapCreateInitiative()
		},
		OnEditInitiative: func(initiative linearapi.Initiative) {
			a.showRoadmapEditInitiative(initiative)
		},
		OnArchiveInitiative: func(initiative linearapi.Initiative) {
			a.archiveRoadmapInitiative(initiative)
		},
		OnDeleteInitiative: func(initiative linearapi.Initiative) {
			a.deleteRoadmapInitiative(initiative)
		},
		OnCreateProject: func(initiativeID string) {
			a.showRoadmapCreateProject(initiativeID)
		},
		OnEditProject: func(project linearapi.Project) {
			a.showRoadmapEditProject(project)
		},
		OnDeleteProject: func(project linearapi.Project) {
			a.deleteRoadmapProject(project)
		},
		OnCreateProjectUpdate: func(projectID string) { a.showRoadmapCreate(projectID) },
		OnEditProjectUpdate:   func(update linearapi.ProjectUpdate) { a.showRoadmapEdit(update) },
		OnArchiveProjectUpdate: func(update linearapi.ProjectUpdate) {
			a.archiveRoadmapUpdate(update)
		},
	}
}

func (a *App) showRoadmapCreateInitiative() {
	if a == nil || a.roadmapEditor == nil {
		return
	}
	a.roadmapEditor.show(roadmapEditorValues{
		kind:   roadmapEditorInitiative,
		status: linearapi.InitiativeStatusProposed,
	})
}

func (a *App) showRoadmapEditInitiative(initiative linearapi.Initiative) {
	if a == nil || a.roadmapEditor == nil {
		return
	}
	a.roadmapEditor.show(roadmapEditorValues{
		kind:        roadmapEditorInitiative,
		id:          initiative.ID,
		name:        initiative.Name,
		description: initiative.Description,
		status:      linearapi.InitiativeStatus(initiative.Status),
		targetDate:  initiative.TargetDate,
	})
}

func (a *App) showRoadmapCreateProject(initiativeID string) {
	if a == nil || a.roadmapEditor == nil {
		return
	}
	a.roadmapEditor.show(roadmapEditorValues{
		kind:         roadmapEditorProject,
		initiativeID: initiativeID,
		teamID:       a.GetSelectedTeamID(),
	})
}

func (a *App) showRoadmapEditProject(project linearapi.Project) {
	if a == nil || a.roadmapEditor == nil {
		return
	}
	a.roadmapEditor.show(roadmapEditorValues{
		kind:        roadmapEditorProject,
		id:          project.ID,
		name:        project.Name,
		description: project.Description,
		teamID:      project.TeamID,
	})
}

func (a *App) applyRoadmapEditorValues(values roadmapEditorValues) {
	if a == nil {
		return
	}
	switch values.kind {
	case roadmapEditorInitiative:
		if values.id == "" {
			input := linearapi.CreateInitiativeInput{
				Name:        values.name,
				Description: values.description,
				Status:      values.status,
				TargetDate:  values.targetDate,
			}
			create := a.createInitiativeFunc
			if create == nil && a.api != nil {
				create = a.api.CreateInitiative
			}
			if create == nil {
				a.updateStatusBarWithError(fmt.Errorf("initiative creation unavailable"))
				return
			}
			a.runRoadmapMutation("Create initiative", func(ctx context.Context) error {
				_, err := create(ctx, input)
				return err
			})
			return
		}
		name, description, targetDate := values.name, values.description, values.targetDate
		status := values.status
		update := a.updateInitiativeFunc
		if update == nil && a.api != nil {
			update = a.api.UpdateInitiative
		}
		if update == nil {
			a.updateStatusBarWithError(fmt.Errorf("initiative editing unavailable"))
			return
		}
		input := linearapi.UpdateInitiativeInput{
			ID:          values.id,
			Name:        &name,
			Description: &description,
			Status:      &status,
			TargetDate:  &targetDate,
		}
		a.runRoadmapMutation("Update initiative", func(ctx context.Context) error {
			_, err := update(ctx, input)
			return err
		})
	case roadmapEditorProject:
		if values.id == "" {
			teamID := strings.TrimSpace(values.teamID)
			if teamID == "" {
				a.updateStatusBarWithError(fmt.Errorf("project creation requires a team"))
				return
			}
			create := a.createProjectFunc
			if create == nil && a.api != nil {
				create = a.api.CreateProject
			}
			if create == nil {
				a.updateStatusBarWithError(fmt.Errorf("project creation unavailable"))
				return
			}
			input := linearapi.CreateProjectInput{Name: values.name, TeamID: teamID, Description: values.description}
			a.runRoadmapMutation("Create project", func(ctx context.Context) error {
				project, err := create(ctx, input)
				if err != nil {
					return err
				}
				initiativeID := strings.TrimSpace(values.initiativeID)
				if initiativeID == "" {
					return nil
				}
				associate := a.createInitiativeToProjectFunc
				if associate == nil && a.api != nil {
					associate = a.api.CreateInitiativeToProject
				}
				projectID := strings.TrimSpace(project.ID)
				if projectID == "" {
					return fmt.Errorf("project created, but association with initiative %s failed: create returned no project ID; project remains available for manual association", initiativeID)
				}
				if associate == nil {
					return fmt.Errorf("project %s created, but association with initiative %s failed: association unavailable; project remains available for manual association", projectID, initiativeID)
				}
				if err := associate(ctx, linearapi.CreateInitiativeToProjectInput{
					InitiativeID: initiativeID,
					ProjectID:    projectID,
				}); err != nil {
					return fmt.Errorf("project %s created, but association with initiative %s failed: %w; project remains available for manual association", projectID, initiativeID, err)
				}
				return nil
			})
			return
		}
		name := values.name
		update := a.updateProjectFunc
		if update == nil && a.api != nil {
			update = a.api.UpdateProject
		}
		if update == nil {
			a.updateStatusBarWithError(fmt.Errorf("project editing unavailable"))
			return
		}
		input := linearapi.UpdateProjectInput{ID: values.id, Name: &name}
		if values.descriptionChanged {
			description := values.description
			input.Description = &description
		}
		if values.teamIDChanged {
			teamID := strings.TrimSpace(values.teamID)
			teamIDs := []string{}
			if teamID != "" {
				teamIDs = []string{teamID}
			}
			input.TeamIDs = &teamIDs
		}
		a.runRoadmapMutation("Update project", func(ctx context.Context) error {
			_, err := update(ctx, input)
			return err
		})
	}
}

func (a *App) runRoadmapMutation(name string, operation func(context.Context) error, successMessage ...string) {
	if a == nil || operation == nil {
		return
	}
	if a.roadmapActionRunner == nil {
		a.roadmapActionRunner = NewAsyncViewActionRunner(a.QueueUpdateDraw)
	}
	accepted := a.roadmapActionRunner.Run(name, operation, func(err error) {
		if err != nil || a.pages == nil || !a.pages.HasPage(roadmapPageName) {
			return
		}
		message := name + " complete"
		if len(successMessage) > 0 && strings.TrimSpace(successMessage[0]) != "" {
			message = successMessage[0]
		}
		a.flashStatus(message)
		a.reloadRoadmap()
	})
	if !accepted && a.pages != nil && a.pages.HasPage(roadmapPageName) {
		a.updateStatusBarWithError(fmt.Errorf("another roadmap action is already in progress"))
	}
}

func (a *App) archiveRoadmapInitiative(initiative linearapi.Initiative) {
	archive := a.archiveInitiativeFunc
	if archive == nil && a.api != nil {
		archive = a.api.ArchiveInitiative
	}
	if archive == nil {
		a.updateStatusBarWithError(fmt.Errorf("initiative archive unavailable"))
		return
	}
	a.runRoadmapMutation("Archive initiative", func(ctx context.Context) error {
		return archive(ctx, initiative.ID)
	})
}

func (a *App) deleteRoadmapInitiative(initiative linearapi.Initiative) {
	remove := a.deleteInitiativeFunc
	if remove == nil && a.api != nil {
		remove = a.api.DeleteInitiative
	}
	if remove == nil {
		a.updateStatusBarWithError(fmt.Errorf("initiative deletion unavailable"))
		return
	}
	a.runRoadmapMutation("Delete initiative", func(ctx context.Context) error {
		return remove(ctx, initiative.ID)
	})
}

func (a *App) deleteRoadmapProject(project linearapi.Project) {
	remove := a.deleteProjectFunc
	if remove == nil && a.api != nil {
		remove = a.api.DeleteProject
	}
	if remove == nil {
		a.updateStatusBarWithError(fmt.Errorf("project deletion unavailable"))
		return
	}
	a.runRoadmapMutation("Delete project", func(ctx context.Context) error {
		return remove(ctx, project.ID)
	})
}

func (a *App) roadmapOptions() RoadmapViewOptions {
	return RoadmapViewOptions{
		Initiatives:    cloneInitiatives(a.roadmapInitiatives),
		ProjectUpdates: cloneProjectUpdates(a.roadmapProjectUpdates),
		Actions:        a.roadmapActions(),
	}
}

func (a *App) openRoadmap() {
	a.closeWorkspacePagesExcept(roadmapPageName)
	if a.roadmapActionRunner != nil {
		a.roadmapActionRunner.Invalidate()
	}
	if a.roadmapView == nil {
		a.roadmapView = NewRoadmapView(a)
	}
	options := a.roadmapOptions()
	options.Loading = true
	a.roadmapView.Show(options)
	a.reloadRoadmap()
}

// savedViewsOptions binds the reusable manager to the App-owned API seams.
// The modal remains presentation-only: every callback below schedules exactly
// one mutation and, after success, one list reload.
func (a *App) savedViewsOptions(views []linearapi.CustomView, loading bool, loadErr error) SavedViewsModalOptions {
	return SavedViewsModalOptions{
		Views:   append([]linearapi.CustomView(nil), views...),
		Loading: loading,
		Error:   loadErr,
		OnApply: func(view linearapi.CustomView) {
			a.applySavedView(view)
		},
		OnCreate: func(values SavedViewEditorValues) {
			a.createSavedView(values)
		},
		OnEdit: func(values SavedViewEditorValues) {
			a.updateSavedView(values)
		},
		OnDelete: func(view linearapi.CustomView) {
			a.deleteSavedView(view)
		},
		OnReload: func() {
			a.reloadSavedViews()
		},
	}
}

// openSavedViews opens the manager in either UI mode and starts an async list
// request. It deliberately does not alter issue state or trigger an issue
// refresh until the user applies a view.
func (a *App) openSavedViews() {
	if a == nil || a.pages == nil {
		return
	}
	a.closeWorkspacePagesExcept(savedViewsPageName)
	if a.savedViewsMutationRunner != nil {
		a.savedViewsMutationRunner.Invalidate()
	}
	if a.savedViewsModal == nil {
		a.savedViewsModal = NewSavedViewsModal(a)
	}
	a.savedViewsModal.Show(a.savedViewsOptions(nil, true, nil))
	a.reloadSavedViews()
}

// reloadSavedViews loads the manager list with a generation guard. Closing or
// reopening the page invalidates older completions, so a slow response can
// never replace a newer list.
func (a *App) reloadSavedViews() {
	if a == nil || a.savedViewsModal == nil || a.pages == nil || !a.pages.HasPage(savedViewsPageName) {
		return
	}
	generation := a.savedViewsLoadGeneration.Add(1)
	modal := a.savedViewsModal
	modal.options.Loading = true
	modal.options.Error = nil
	modal.refreshList()
	list := a.listCustomViewsFunc
	if list == nil && a.api != nil {
		list = a.api.ListCustomViews
	}
	if list == nil {
		err := fmt.Errorf("saved views loading unavailable")
		modal.options.Loading = false
		modal.options.Error = err
		modal.refreshList()
		modal.setStatus(err.Error())
		return
	}
	go func() {
		views, err := list(context.Background())
		a.QueueUpdateDraw(func() {
			if generation != a.savedViewsLoadGeneration.Load() || a.pages == nil || !a.pages.HasPage(savedViewsPageName) {
				return
			}
			modal.options.Loading = false
			modal.options.Error = err
			if err != nil {
				modal.refreshList()
				modal.setStatus(err.Error())
				return
			}
			modal.options.Error = nil
			modal.SetViews(views)
			modal.setStatus("")
		})
	}()
}

func (a *App) createSavedView(values SavedViewEditorValues) {
	create := a.createCustomViewFunc
	if create == nil && a.api != nil {
		create = a.api.CreateCustomView
	}
	if create == nil {
		a.setSavedViewsMutationError(fmt.Errorf("create saved view unavailable"))
		return
	}
	input := linearapi.CreateCustomViewInput{
		Name:        strings.TrimSpace(values.Name),
		Description: values.Description,
		Color:       values.Color,
		TeamID:      a.GetSelectedTeamID(),
		FilterJSON:  values.FilterJSON,
	}
	a.runSavedViewsMutation("Create saved view", "Creating saved view...", func(ctx context.Context) error {
		_, err := create(ctx, input)
		return err
	}, "Saved view created")
}

func (a *App) updateSavedView(values SavedViewEditorValues) {
	update := a.updateCustomViewFunc
	if update == nil && a.api != nil {
		update = a.api.UpdateCustomView
	}
	if update == nil {
		a.setSavedViewsMutationError(fmt.Errorf("update saved view unavailable"))
		return
	}
	input := linearapi.UpdateCustomViewInput{
		Name:        strings.TrimSpace(values.Name),
		Description: values.Description,
		Color:       values.Color,
		FilterJSON:  values.FilterJSON,
	}
	a.runSavedViewsMutation("Update saved view", "Updating saved view...", func(ctx context.Context) error {
		_, err := update(ctx, strings.TrimSpace(values.ID), input)
		return err
	}, "Saved view updated")
}

func (a *App) deleteSavedView(view linearapi.CustomView) {
	remove := a.deleteCustomViewFunc
	if remove == nil && a.api != nil {
		remove = a.api.DeleteCustomView
	}
	if remove == nil {
		a.setSavedViewsMutationError(fmt.Errorf("delete saved view unavailable"))
		return
	}
	a.runSavedViewsMutation("Delete saved view", "Deleting saved view...", func(ctx context.Context) error {
		return remove(ctx, strings.TrimSpace(view.ID))
	}, "Saved view deleted")
}

func (a *App) runSavedViewsMutation(name, pendingMessage string, operation func(context.Context) error, successMessage string) {
	if a == nil || operation == nil {
		return
	}
	runner := a.savedViewsMutationRunner
	if runner == nil {
		runner = NewAsyncViewActionRunner(a.QueueUpdateDraw)
		a.savedViewsMutationRunner = runner
	}
	if a.pages == nil || !a.pages.HasPage(savedViewsPageName) {
		return
	}
	if runner.InFlight() {
		a.setSavedViewsMutationError(fmt.Errorf("another saved views action is already in progress"))
		return
	}
	a.savedViewsMutationStatus(pendingMessage)
	accepted := runner.Run(name, operation, func(err error) {
		if a.pages == nil || !a.pages.HasPage(savedViewsPageName) {
			return
		}
		if err != nil {
			a.setSavedViewsMutationError(err)
			return
		}
		a.flashStatus(successMessage)
		a.reloadSavedViews()
	})
	if !accepted && a.pages != nil && a.pages.HasPage(savedViewsPageName) {
		a.setSavedViewsMutationError(fmt.Errorf("another saved views action is already in progress"))
	}
}

func (a *App) savedViewsMutationStatus(message string) {
	if a.savedViewsModal != nil {
		a.savedViewsModal.setStatus(message)
	}
}

func (a *App) setSavedViewsMutationError(err error) {
	if err == nil {
		return
	}
	if a.savedViewsModal != nil {
		a.savedViewsModal.setStatus(err.Error())
	}
	a.updateStatusBarWithError(err)
}

// applySavedView selects the existing navigation node when possible, or
// creates a stable in-memory node for a view that is not in Favorites. The
// custom-view issue fetch is initiated exactly once here.
func (a *App) applySavedView(view linearapi.CustomView) {
	viewID := strings.TrimSpace(view.ID)
	if viewID == "" {
		a.flashStatus("Saved view is missing an ID")
		return
	}
	a.transitionToIssueListDestination()
	node := a.findNavigationNodeByCustomViewID(viewID)
	if node == nil {
		node = &NavigationNode{ID: viewID, Text: view.Name, CustomViewID: viewID, TeamID: view.TeamID}
	}
	if strings.TrimSpace(view.Name) != "" {
		node.Text = view.Name
	}
	if a.navigationTree != nil {
		if treeNode := a.findNavigationTreeNode(func(candidate *NavigationNode) bool { return candidate.CustomViewID == viewID }); treeNode != nil {
			a.navigationTree.SetCurrentNode(treeNode)
		}
	}
	a.selectedNavigation = node
	a.richFilters = IssueFilters{}
	a.focusedPane = FocusIssues
	a.updateFocus()
	a.flashStatus(fmt.Sprintf("Applied saved view: %s", view.Name))
	go a.refreshIssuesWithFocusChange(false)
}

func (a *App) findNavigationNodeByCustomViewID(viewID string) *NavigationNode {
	if strings.TrimSpace(viewID) == "" || a.navigationTree == nil {
		return nil
	}
	node := a.findNavigationTreeNode(func(candidate *NavigationNode) bool { return candidate.CustomViewID == viewID })
	if node == nil {
		return nil
	}
	ref := node.GetReference()
	nav, _ := ref.(*NavigationNode)
	return nav
}

// isTriageContext is the single gate for triage actions. A command can remain
// in a stale palette snapshot after navigation changes, so every action checks
// this predicate again at execution time.
func (a *App) isTriageContext() bool {
	return a != nil && a.selectedNavigation != nil && strings.EqualFold(strings.TrimSpace(a.selectedNavigation.StateType), "triage")
}

func (a *App) requireTriageContext() bool {
	if a.isTriageContext() {
		return true
	}
	a.flashStatus("Triage action unavailable outside Triage")
	return false
}

func (a *App) triageOperations() TriageOperations {
	updateIssue := a.updateIssueFunc
	if updateIssue == nil && a.api != nil && strings.TrimSpace(a.config.LinearAPIKey) != "" {
		updateIssue = a.api.UpdateIssue
	}
	createRelation := a.createIssueRelationFunc
	if createRelation == nil && a.api != nil && strings.TrimSpace(a.config.LinearAPIKey) != "" {
		createRelation = a.api.CreateIssueRelation
	}
	archiveIssue := a.archiveIssueFunc
	if archiveIssue == nil && a.api != nil && strings.TrimSpace(a.config.LinearAPIKey) != "" {
		archiveIssue = a.api.ArchiveIssue
	}
	snoozeNotification := a.snoozeNotificationFunc
	if snoozeNotification == nil && a.api != nil && strings.TrimSpace(a.config.LinearAPIKey) != "" {
		snoozeNotification = a.api.SnoozeNotification
	}
	return TriageOperations{
		UpdateIssue:         updateIssue,
		CreateIssueRelation: createRelation,
		ArchiveIssue:        archiveIssue,
		SnoozeNotification:  snoozeNotification,
	}
}

func (a *App) triageController() *TriageActions {
	if a.triageActions != nil {
		return a.triageActions
	}
	return NewTriageActions(a.triageOperations())
}

func (a *App) triageOperationAvailable(action TriageAction) bool {
	controller := a.triageController()
	if controller == nil {
		return false
	}
	ops := controller.operations
	switch action {
	case TriageActionAccept:
		return ops.UpdateIssue != nil
	case TriageActionDuplicate:
		return ops.CreateIssueRelation != nil
	case TriageActionDecline:
		return ops.ArchiveIssue != nil
	case TriageActionSnooze:
		return ops.SnoozeNotification != nil
	default:
		return false
	}
}

func (a *App) selectedTriageTargets() []string {
	if !a.requireTriageContext() {
		return nil
	}
	ids := a.effectiveIssueTargetIDs()
	if len(ids) == 0 {
		a.flashStatus("No triage issue selected")
	}
	return ids
}

func (a *App) runTriageAcceptCommand() {
	targets := a.selectedTriageTargets()
	if len(targets) == 0 {
		return
	}
	if !a.triageOperationAvailable(TriageActionAccept) {
		a.flashStatus("Triage accept unavailable")
		return
	}
	a.showTriageStatePicker(func(state linearapi.WorkflowState) {
		a.runTriageBatch(TriageBatchInput{
			Action:               TriageActionAccept,
			IssueIDs:             targets,
			DestinationStateID:   state.ID,
			DestinationStateType: state.Type,
		})
	})
}

func (a *App) showTriageStatePicker(onSelect func(linearapi.WorkflowState)) {
	if onSelect == nil {
		return
	}
	show := func(states []linearapi.WorkflowState) {
		items := make([]PickerItem, 0, len(states))
		for _, state := range states {
			if strings.EqualFold(strings.TrimSpace(state.Type), "triage") || strings.TrimSpace(state.Type) == "" {
				continue
			}
			items = append(items, PickerItem{ID: state.ID, Label: state.Name})
		}
		if len(items) == 0 {
			a.flashStatus("No non-triage workflow state available")
			return
		}
		statesByID := make(map[string]linearapi.WorkflowState, len(states))
		for _, state := range states {
			statesByID[state.ID] = state
		}
		a.pickerActive = true
		a.pickerModal.Show("Accept to Status", items, func(item PickerItem) {
			a.pickerActive = false
			state, ok := statesByID[item.ID]
			if !ok || strings.EqualFold(strings.TrimSpace(state.Type), "triage") || strings.TrimSpace(state.Type) == "" {
				a.flashStatus("Selected workflow state is unavailable")
				return
			}
			onSelect(state)
		})
	}
	if len(a.workflowStates) > 0 {
		show(append([]linearapi.WorkflowState(nil), a.workflowStates...))
		return
	}
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.flashStatus("Workflow states unavailable: select a team")
		return
	}
	go func() {
		states, err := a.cache.GetWorkflowStates(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.workflowStates = states
			show(states)
		})
	}()
}

func (a *App) runTriageDuplicateCommand() {
	targets := a.selectedTriageTargets()
	if len(targets) == 0 {
		return
	}
	if !a.triageOperationAvailable(TriageActionDuplicate) {
		a.flashStatus("Triage duplicate unavailable")
		return
	}
	a.textInputModal.Show("Mark Duplicate", "Target issue ID: ", "", func(targetID string) {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			a.flashStatus("Duplicate target issue ID is required")
			return
		}
		for _, issueID := range targets {
			if issueID == targetID {
				a.flashStatus("An issue cannot be a duplicate of itself")
				return
			}
		}
		a.runTriageBatch(TriageBatchInput{
			Action:            TriageActionDuplicate,
			IssueIDs:          targets,
			DuplicateTargetID: targetID,
		})
	})
}

func (a *App) runTriageDeclineCommand() {
	targets := a.selectedTriageTargets()
	if len(targets) == 0 {
		return
	}
	if !a.triageOperationAvailable(TriageActionDecline) {
		a.flashStatus("Triage decline unavailable")
		return
	}
	message := "Archive this triage issue?"
	if len(targets) > 1 {
		message = fmt.Sprintf("Archive %d triage issues?", len(targets))
	}
	a.confirmationModal.Show("Decline Triage", message, "Archive", func() {
		a.runTriageBatch(TriageBatchInput{Action: TriageActionDecline, IssueIDs: targets, Confirmed: true})
	})
}

func (a *App) runTriageSnoozeCommand() {
	targets := a.selectedTriageTargets()
	if len(targets) == 0 {
		return
	}
	if !a.triageOperationAvailable(TriageActionSnooze) {
		a.flashStatus("Triage snooze unavailable")
		return
	}
	lookupGeneration := a.triageNotificationGeneration.Load()
	knownNotificationIDs := make(map[string]string, len(targets))
	missingIssueIDs := make([]string, 0, len(targets))
	for _, issueID := range targets {
		if notificationID := a.relatedTriageNotificationID(issueID); notificationID != "" {
			knownNotificationIDs[issueID] = notificationID
		} else {
			missingIssueIDs = append(missingIssueIDs, issueID)
		}
	}
	finish := func(notificationIDs map[string]string, err error) {
		if lookupGeneration != a.triageNotificationGeneration.Load() || !a.isTriageContext() || !a.triageTargetsStillCurrent(targets) {
			return
		}
		if notificationIDs == nil {
			notificationIDs = make(map[string]string, len(targets))
		}
		for issueID, notificationID := range knownNotificationIDs {
			notificationIDs[issueID] = notificationID
		}
		if err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		for _, issueID := range targets {
			if strings.TrimSpace(notificationIDs[issueID]) == "" {
				a.flashStatus("Triage snooze unavailable: no related notification")
				return
			}
		}
		a.runTriageBatch(TriageBatchInput{
			Action:          TriageActionSnooze,
			IssueIDs:        targets,
			NotificationIDs: notificationIDs,
			SnoozeUntil:     time.Now().Add(24 * time.Hour),
		})
	}
	if len(missingIssueIDs) == 0 {
		finish(nil, nil)
		return
	}
	a.lookupTriageNotificationIDs(missingIssueIDs, finish)
}

func (a *App) relatedTriageNotificationID(issueID string) string {
	if a == nil || strings.TrimSpace(issueID) == "" {
		return ""
	}
	a.triageNotificationMu.Lock()
	if notificationID := a.triageNotificationIndex[issueID]; strings.TrimSpace(notificationID) != "" {
		a.triageNotificationMu.Unlock()
		return strings.TrimSpace(notificationID)
	}
	a.triageNotificationMu.Unlock()
	if a.inboxView == nil {
		return ""
	}
	for _, notification := range a.inboxView.VisibleNotifications() {
		if notification.Issue != nil && notification.Issue.ID == issueID && strings.TrimSpace(notification.ID) != "" {
			return strings.TrimSpace(notification.ID)
		}
	}
	return ""
}

// invalidateTriageNotificationLookup drops the independent issue-to-notification
// snapshot and makes any in-flight completion stale. Inbox visibility is not
// touched; this cache belongs to triage command resolution only.
func (a *App) invalidateTriageNotificationLookup() {
	if a == nil {
		return
	}
	a.triageNotificationGeneration.Add(1)
	a.triageNotificationMu.Lock()
	a.triageNotificationIndex = make(map[string]string)
	a.triageNotificationLookup = nil
	a.triageNotificationMu.Unlock()
}

func (a *App) triageTargetsStillCurrent(targets []string) bool {
	if a == nil || !a.isTriageContext() {
		return false
	}
	current := a.effectiveIssueTargetIDs()
	if len(current) != len(targets) {
		return false
	}
	for index := range targets {
		if current[index] != targets[index] {
			return false
		}
	}
	return true
}

func (a *App) lookupTriageNotificationIDs(issueIDs []string, callback func(map[string]string, error)) {
	if a == nil || callback == nil {
		return
	}
	requested := make([]string, 0, len(issueIDs))
	seen := make(map[string]struct{}, len(issueIDs))
	for _, issueID := range issueIDs {
		issueID = strings.TrimSpace(issueID)
		if issueID == "" {
			continue
		}
		if _, exists := seen[issueID]; exists {
			continue
		}
		seen[issueID] = struct{}{}
		requested = append(requested, issueID)
	}
	if len(requested) == 0 {
		a.QueueUpdateDraw(func() { callback(nil, nil) })
		return
	}

	generation := a.triageNotificationGeneration.Load()
	a.triageNotificationMu.Lock()
	cached := make(map[string]string, len(requested))
	missing := false
	for _, issueID := range requested {
		if notificationID := a.triageNotificationIndex[issueID]; strings.TrimSpace(notificationID) != "" {
			cached[issueID] = notificationID
		} else {
			missing = true
		}
	}
	if !missing {
		a.triageNotificationMu.Unlock()
		a.QueueUpdateDraw(func() { callback(cached, nil) })
		return
	}
	if lookup := a.triageNotificationLookup; lookup != nil {
		a.triageNotificationMu.Unlock()
		go func() {
			<-lookup.done
			a.deliverTriageNotificationLookup(lookup, requested, callback)
		}()
		return
	}
	lookup := &triageNotificationLookupState{done: make(chan struct{}), generation: generation}
	a.triageNotificationLookup = lookup
	a.triageNotificationMu.Unlock()

	go func() {
		list := a.listNotificationsFunc
		if list == nil && strings.TrimSpace(a.config.LinearAPIKey) != "" && a.api != nil {
			list = a.api.ListNotifications
		}
		if list == nil {
			lookup.err = fmt.Errorf("triage snooze notification lookup unavailable")
		} else {
			var notifications []linearapi.Notification
			notifications, lookup.err = list(context.Background(), linearapi.NotificationListOptions{IncludeArchived: false})
			if lookup.err == nil {
				lookup.notifications = make(map[string]string)
				for _, notification := range notifications {
					if notification.Issue == nil || strings.TrimSpace(notification.Issue.ID) == "" || strings.TrimSpace(notification.ID) == "" {
						continue
					}
					if _, exists := lookup.notifications[notification.Issue.ID]; !exists {
						lookup.notifications[notification.Issue.ID] = notification.ID
					}
				}
			}
		}

		a.triageNotificationMu.Lock()
		if a.triageNotificationGeneration.Load() == lookup.generation && lookup.err == nil {
			for issueID, notificationID := range lookup.notifications {
				a.triageNotificationIndex[issueID] = notificationID
			}
		}
		if a.triageNotificationLookup == lookup {
			a.triageNotificationLookup = nil
		}
		close(lookup.done)
		a.triageNotificationMu.Unlock()
		a.deliverTriageNotificationLookup(lookup, requested, callback)
	}()
}

func (a *App) deliverTriageNotificationLookup(lookup *triageNotificationLookupState, requested []string, callback func(map[string]string, error)) {
	if a == nil || lookup == nil || callback == nil {
		return
	}
	a.QueueUpdateDraw(func() {
		if a.triageNotificationGeneration.Load() != lookup.generation {
			return
		}
		result := make(map[string]string, len(requested))
		a.triageNotificationMu.Lock()
		for _, issueID := range requested {
			if notificationID := a.triageNotificationIndex[issueID]; strings.TrimSpace(notificationID) != "" {
				result[issueID] = notificationID
			} else if lookup.notifications != nil {
				result[issueID] = lookup.notifications[issueID]
			}
		}
		err := lookup.err
		a.triageNotificationMu.Unlock()
		callback(result, err)
	})
}

func (a *App) runTriageBatch(input TriageBatchInput) {
	if !a.requireTriageContext() {
		return
	}
	if len(input.IssueIDs) == 0 {
		a.flashStatus("No triage issue selected")
		return
	}
	if !a.triageOperationAvailable(input.Action) {
		a.flashStatus(fmt.Sprintf("Triage %s unavailable", input.Action))
		return
	}
	if input.Concurrency <= 0 {
		input.Concurrency = 4
	}
	snapshot := a.bulkIssueMutationSnapshot(input.IssueIDs)
	marked := len(snapshot.markedIDs) > 0
	runner := a.triageActionRunner
	if runner == nil {
		runner = NewAsyncViewActionRunner(a.QueueUpdateDraw, AsyncViewActionHooks{
			OnProgress: func(_, message string) {
				if !a.isTriageContext() || strings.TrimSpace(message) == "" {
					return
				}
				a.statusMessage = message
				a.updateStatusBar()
			},
			OnError: func(_ string, err error) {
				if err != nil && a.isTriageContext() {
					a.updateStatusBarWithError(err)
				}
			},
		})
		a.triageActionRunner = runner
	}
	if runner.InFlight() {
		a.enqueueTriageBatch(runner, input)
		return
	}
	a.statusMessage = fmt.Sprintf("Triage %s: 0/%d", input.Action, len(input.IssueIDs))
	a.updateStatusBar()
	controller := a.triageController()
	var summary TriageActionSummary
	var summaryMu sync.Mutex
	accepted := runner.RunResult("Triage "+string(input.Action), func(ctx context.Context) error {
		result := controller.RunBatch(ctx, input, func(progress TriageActionProgress) {
			runner.Progress(fmt.Sprintf("Triage %s: %d/%d", progress.Action, progress.Completed, progress.Total))
		})
		summaryMu.Lock()
		summary = result
		summaryMu.Unlock()
		return result.Err
	}, func(result AsyncViewActionResult) {
		current := result.Err == nil && a.isTriageContext() && a.bulkIssueMutationContextCurrent(snapshot)
		if current {
			summaryMu.Lock()
			final := summary
			summaryMu.Unlock()
			a.finishTriageBatch(input, marked, snapshot, final)
		}
		if !current {
			a.clearPendingTriageBatches()
			return
		}
		a.runNextPendingTriageBatch()
	})
	if !accepted && a.isTriageContext() {
		a.enqueueTriageBatch(runner, input)
	}
}

func (a *App) enqueueTriageBatch(runner *AsyncViewActionRunner, input TriageBatchInput) {
	if a == nil || runner == nil || !a.isTriageContext() {
		return
	}
	a.triageActionMu.Lock()
	a.triagePendingBatches = append(a.triagePendingBatches, input)
	a.triageActionMu.Unlock()
	a.flashStatus("Triage action queued")
	// The completion callback and this enqueue can race at the exact point
	// where the runner transitions to idle. Re-check after appending so a
	// batch cannot remain stranded after the completion observed an empty
	// queue.
	if !runner.InFlight() {
		a.runNextPendingTriageBatch()
	}
}

func (a *App) clearPendingTriageBatches() {
	if a == nil {
		return
	}
	a.triageActionMu.Lock()
	a.triagePendingBatches = nil
	a.triageActionMu.Unlock()
}

func (a *App) runNextPendingTriageBatch() {
	if a == nil || !a.isTriageContext() {
		a.clearPendingTriageBatches()
		return
	}
	a.triageActionMu.Lock()
	if len(a.triagePendingBatches) == 0 {
		a.triageActionMu.Unlock()
		return
	}
	next := a.triagePendingBatches[0]
	a.triagePendingBatches = a.triagePendingBatches[1:]
	a.triageActionMu.Unlock()
	a.runTriageBatch(next)
}

func (a *App) finishTriageBatch(input TriageBatchInput, marked bool, snapshot bulkIssueMutationSnapshot, summary TriageActionSummary) {
	if !a.bulkIssueMutationContextCurrent(snapshot) || !a.isTriageContext() {
		return
	}
	if summary.Err != nil {
		a.updateStatusBarWithError(summary.Err)
		return
	}
	failed := make(map[string]struct{}, summary.Failed)
	for _, result := range summary.Results {
		if result.Err != nil {
			failed[result.IssueID] = struct{}{}
		}
	}
	if marked {
		for _, issueID := range input.IssueIDs {
			if _, failedTarget := failed[issueID]; failedTarget {
				if !a.markedIssueSelection.IsMarked(issueID) {
					a.markedIssueSelection.Toggle(issueID)
				}
				continue
			}
			if a.markedIssueSelection.IsMarked(issueID) {
				a.markedIssueSelection.Toggle(issueID)
			}
		}
		a.refreshIssueTableMarks()
	}
	if summary.Failed > 0 {
		a.flashStatus(fmt.Sprintf("Triage %s: %d succeeded, %d failed", input.Action, summary.Succeeded, summary.Failed))
	} else {
		a.flashStatus(fmt.Sprintf("Triage %s: %d succeeded", input.Action, summary.Succeeded))
	}
	// The finish callback itself runs through QueueUpdateDraw. Defer the
	// refresh until that UI callback has returned so the refresh's own queued
	// updates cannot re-enter the serialized queue (the synchronous test queue
	// and a real UI queue both rely on that non-reentrancy).
	go a.refreshIssues()
}

func (a *App) closeWorkspacePagesExcept(pageName string) {
	if a == nil || a.pages == nil {
		return
	}
	if pageName != inboxViewPageName && a.pages.HasPage(inboxViewPageName) {
		if a.inboxView != nil && a.inboxView.actionRunner != nil {
			a.inboxView.actionRunner.Invalidate()
		}
		a.pages.RemovePage(inboxViewPageName)
		a.inboxLoadGeneration.Add(1)
	}
	if pageName != commentsModalPageName && a.pages.HasPage(commentsModalPageName) {
		if a.commentsModal != nil && a.commentsModal.actionRunner != nil {
			a.commentsModal.actionRunner.Invalidate()
		}
		a.pages.RemovePage(commentsModalPageName)
		a.commentsLoadGeneration.Add(1)
	}
	if pageName != roadmapPageName && a.pages.HasPage(roadmapPageName) {
		if a.roadmapActionRunner != nil {
			a.roadmapActionRunner.Invalidate()
		}
		a.pages.RemovePage(roadmapPageName)
		a.roadmapLoadGeneration.Add(1)
	}
	if pageName != roadmapEditorPageName && a.pages.HasPage(roadmapEditorPageName) {
		if a.roadmapEditor != nil {
			a.roadmapEditor.Hide()
		} else {
			a.pages.RemovePage(roadmapEditorPageName)
		}
	}
	if pageName != savedViewsPageName && a.pages.HasPage(savedViewsPageName) {
		if a.savedViewsMutationRunner != nil {
			a.savedViewsMutationRunner.Invalidate()
		}
		a.pages.RemovePage(savedViewsPageName)
		a.savedViewsLoadGeneration.Add(1)
	}
}

func (a *App) reloadRoadmap() {
	if a == nil || a.roadmapView == nil {
		return
	}
	generation := a.roadmapLoadGeneration.Add(1)
	options := a.roadmapOptions()
	options.Loading = true
	a.roadmapView.SetData(options)
	listInitiatives := a.listInitiativesFunc
	if listInitiatives == nil && strings.TrimSpace(a.config.LinearAPIKey) != "" && a.api != nil {
		listInitiatives = a.api.ListInitiatives
	}
	if listInitiatives == nil {
		a.roadmapView.SetData(RoadmapViewOptions{Error: fmt.Errorf("roadmap loading unavailable"), Actions: a.roadmapActions()})
		return
	}
	go func() {
		initiatives, err := listInitiatives(context.Background())
		if err == nil {
			updates := make(map[string][]linearapi.ProjectUpdate)
			listUpdates := a.listProjectUpdatesFunc
			if listUpdates == nil && strings.TrimSpace(a.config.LinearAPIKey) != "" && a.api != nil {
				listUpdates = a.api.ListProjectUpdates
			}
			if listUpdates != nil {
				for _, initiative := range initiatives {
					for _, project := range initiative.Projects {
						projectUpdates, updateErr := listUpdates(context.Background(), project.ID)
						if updateErr != nil {
							err = updateErr
							break
						}
						updates[project.ID] = projectUpdates
					}
					if err != nil {
						break
					}
				}
			}
			a.QueueUpdateDraw(func() {
				if generation != a.roadmapLoadGeneration.Load() || !a.pages.HasPage(roadmapPageName) {
					return
				}
				if err != nil {
					a.roadmapView.SetData(RoadmapViewOptions{Error: err, Actions: a.roadmapActions()})
					return
				}
				a.roadmapInitiatives = cloneInitiatives(initiatives)
				a.roadmapProjectUpdates = cloneProjectUpdates(updates)
				options := a.roadmapOptions()
				a.roadmapView.SetData(options)
			})
			return
		}
		a.QueueUpdateDraw(func() {
			if generation != a.roadmapLoadGeneration.Load() || !a.pages.HasPage(roadmapPageName) {
				return
			}
			a.roadmapView.SetData(RoadmapViewOptions{Error: err, Actions: a.roadmapActions()})
		})
	}()
}

func (a *App) showRoadmapCreate(projectID string) {
	if a.textInputModal == nil {
		return
	}
	a.textInputModal.Show("Create project update", "Body: ", "", func(body string) {
		if strings.TrimSpace(body) == "" {
			a.flashStatus("Project update body cannot be blank")
			return
		}
		create := a.createProjectUpdateFunc
		if create == nil && a.api != nil {
			create = a.api.CreateProjectUpdate
		}
		if create == nil {
			a.updateStatusBarWithError(fmt.Errorf("project update creation unavailable"))
			return
		}
		a.runRoadmapMutation("Create project update", func(ctx context.Context) error {
			_, err := create(ctx, linearapi.CreateProjectUpdateInput{ProjectID: projectID, Body: body})
			return err
		}, "Project update created")
	})
}

func (a *App) showRoadmapEdit(update linearapi.ProjectUpdate) {
	if a.textInputModal == nil {
		return
	}
	a.textInputModal.Show("Edit project update", "Body: ", update.Body, func(body string) {
		if strings.TrimSpace(body) == "" {
			a.flashStatus("Project update body cannot be blank")
			return
		}
		edit := a.updateProjectUpdateFunc
		if edit == nil && a.api != nil {
			edit = a.api.UpdateProjectUpdate
		}
		if edit == nil {
			a.updateStatusBarWithError(fmt.Errorf("project update editing unavailable"))
			return
		}
		a.runRoadmapMutation("Update project update", func(ctx context.Context) error {
			_, err := edit(ctx, linearapi.UpdateProjectUpdateInput{ID: update.ID, Body: body})
			return err
		}, "Project update saved")
	})
}

func (a *App) archiveRoadmapUpdate(update linearapi.ProjectUpdate) {
	archive := a.archiveProjectUpdateFunc
	if archive == nil && a.api != nil {
		archive = a.api.ArchiveProjectUpdate
	}
	if archive == nil {
		a.updateStatusBarWithError(fmt.Errorf("project update archive unavailable"))
		return
	}
	a.runRoadmapMutation("Archive project update", func(ctx context.Context) error {
		return archive(ctx, update.ID)
	}, "Project update archived")
}

// isPaletteKeyEvent recognizes the configured palette action and the stable
// Ctrl-K alias. Editable modal branches call their handlers before this check
// so text input remains authoritative.
func (a *App) isPaletteKeyEvent(event *tcell.EventKey) bool {
	if a == nil || event == nil {
		return false
	}
	if event.Key() == tcell.KeyCtrlK {
		return true
	}
	return event.Key() == tcell.KeyRune && event.Modifiers() == tcell.ModNone && event.Rune() == a.actionKey("open_palette", ':')
}

// quit stops the application through an injectable seam used by routing
// integration tests. Production wiring points the seam at tview's Stop.
func (a *App) quit() {
	if a == nil {
		return
	}
	if a.quitFunc != nil {
		a.quitFunc()
		return
	}
	if a.app != nil {
		a.app.Stop()
	}
}

// transitionToIssueListDestination closes every workspace overlay before a
// navigation route refreshes the main issue list. Keeping this teardown in one
// helper prevents a new route from forgetting a view runner or load generation.
func (a *App) transitionToIssueListDestination() {
	if a == nil {
		return
	}
	a.closeWorkspacePagesExcept("")
	a.invalidateIssueMutationContexts()
}

// prepareGlobalSearchContext tears down workspace overlays and leaves a saved
// or custom view before search becomes editable. Search is intentionally
// global: the issue list context is made explicit instead of relying on the
// API client to ignore a stale CustomViewID.
func (a *App) prepareGlobalSearchContext() {
	if a == nil {
		return
	}
	a.transitionToIssueListDestination()
	if a.selectedNavigation == nil || strings.TrimSpace(a.selectedNavigation.CustomViewID) == "" {
		return
	}
	if node := a.findNavigationTreeNode(func(nav *NavigationNode) bool { return nav.ID == "all" && !nav.IsIssue }); node != nil && a.navigationTree != nil {
		a.navigationTree.SetCurrentNode(node)
	}
	a.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	a.richFilters = IssueFilters{}
	a.reapplyKeybindings()
}

// handleGlobalWorkspaceBinding routes configured global actions before
// non-editable workspace handlers. Editable/form modal branches return before
// this function, so printable text is never consumed by shell shortcuts.
func (a *App) handleGlobalWorkspaceBinding(event *tcell.EventKey) (bool, *tcell.EventKey) {
	if a == nil || event == nil || event.Key() != tcell.KeyRune || event.Modifiers() != tcell.ModNone {
		if a != nil && a.isPaletteKeyEvent(event) {
			a.openPalette()
			return true, nil
		}
		return false, event
	}
	switch event.Rune() {
	case a.actionKey("quit", 'q'):
		a.quit()
		return true, nil
	case a.actionKey("open_palette", ':'):
		a.openPalette()
		return true, nil
	case a.actionKey("search", '/'):
		a.openSearchPalette()
		return true, nil
	default:
		return false, event
	}
}

func (a *App) activeModalInput(event *tcell.EventKey) (bool, *tcell.EventKey) {
	if a.pages.HasPage("key_help") {
		if event.Key() == tcell.KeyEscape {
			a.hideKeyHelp()
		}
		return true, nil
	}
	switch {
	case a.pages.HasPage("confirmation") && a.confirmationModal != nil:
		return true, a.confirmationModal.HandleKey(event)
	case a.pickerActive:
		return true, a.pickerModal.HandleKey(event)
	case a.pages.HasPage("create_issue") && a.createIssueModal != nil:
		return true, a.createIssueModal.HandleKey(event)
	case a.pages.HasPage("create_comment") && a.createCommentModal != nil:
		return true, a.createCommentModal.HandleKey(event)
	case a.pages.HasPage("edit_title") && a.editTitleModal != nil:
		return true, a.editTitleModal.HandleKey(event)
	case a.pages.HasPage("edit_labels") && a.editLabelsModal != nil:
		return true, a.editLabelsModal.HandleKey(event)
	case a.pages.HasPage("text_input") && a.textInputModal != nil:
		return true, a.textInputModal.HandleKey(event)
	case a.pages.HasPage("multi_select") && a.multiSelectModal != nil:
		return true, a.multiSelectModal.HandleKey(event)
	case a.pages.HasPage("issue_editor") && a.issueEditorModal != nil:
		return true, a.issueEditorModal.HandleKey(event)
	case a.pages.HasPage(roadmapEditorPageName) && a.roadmapEditor != nil:
		return true, a.roadmapEditor.HandleKey(event)
	case a.pages.HasPage("settings") && a.settingsModal != nil:
		return true, a.settingsModal.HandleKey(event)
	case a.pages.HasPage("prompt_templates") && a.promptTemplatesModal != nil:
		return true, a.promptTemplatesModal.HandleKey(event)
	case a.pages.HasPage("agent_prompt") && a.agentPromptModal != nil:
		return true, a.agentPromptModal.HandleKey(event)
	case a.pages.HasPage("agent_output") && a.agentOutputModal != nil:
		return true, a.agentOutputModal.HandleKey(event)
	case a.pages.HasPage("saved_views_editor") && a.savedViewsModal != nil:
		return true, a.savedViewsModal.HandleKey(event)
	case a.pages.HasPage(commentsModalPageName) && a.commentsModal != nil &&
		(a.commentsModal.IsEditing() || a.commentsModal.IsReactionPickerOpen() || a.commentsModal.IsDeleteConfirmationOpen()):
		return true, a.commentsModal.HandleKey(event)
	default:
		return false, event
	}
}

// bindGlobalKeys sets up global keyboard shortcuts.
func (a *App) bindGlobalKeys() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if handled, result := a.activeModalInput(event); handled {
			return result
		}
		// The palette owns every printable key while its input is active. In
		// particular, '/' and configured global bindings are query text here,
		// not new workspace actions.
		if a.focusedPane == FocusPalette {
			return a.handlePaletteKey(event)
		}

		if handled, result := a.handleGlobalWorkspaceBinding(event); handled {
			return result
		}

		if a.pages.HasPage(savedViewsPageName) && a.savedViewsModal != nil {
			if a.dispatchKeySequence(event) {
				return nil
			}
			if a.isPaletteKeyEvent(event) {
				a.openPalette()
				return nil
			}
			if event.Key() == tcell.KeyRune && event.Rune() == '?' {
				a.showKeyHelp()
				return nil
			}
			return a.savedViewsModal.HandleKey(event)
		}

		// Workspace pages own their navigation/action keys. Palette and the
		// editable comment body retain priority so global chords never steal
		// text input.
		if a.focusedPane != FocusPalette {
			if handled, result := a.handleWorkspaceViewKey(event); handled {
				return result
			}
		}

		// Chords are evaluated before ordinary pane shortcuts. Editable modal
		// branches above intentionally return first so text input never starts a
		// pending navigation prefix.
		if a.dispatchKeySequence(event) {
			return nil
		}

		// Global shortcuts (only when not in palette)
		switch event.Key() {
		case tcell.KeyEscape:
			// Clear marks first. A second Escape then performs the existing
			// search-clearing behavior, keeping marking keyboard-first.
			if a.markedIssueSelection.Count() > 0 {
				a.markedIssueSelection.Clear()
				a.refreshIssueTableMarks()
				return nil
			}
			// Clear search if active (when not in modals/palette)
			if a.searchQuery != "" {
				a.setSearchQuery("")
				return nil
			}
		case tcell.KeyCtrlC:
			a.quit()
			return nil
		case tcell.KeyCtrlK:
			a.openPalette()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			// Tab cycles forward through panes (Navigation -> Issues -> Details)
			// When in Details pane, first cycle between description and comments
			// Only cycle when not in palette or modals
			isBackward := event.Key() == tcell.KeyBacktab || event.Modifiers()&tcell.ModShift != 0
			if a.focusedPane != FocusPalette {
				if a.focusedPane == FocusDetails {
					if !a.detailsCommentsVisible {
						if isBackward {
							a.cyclePanesBackward()
						} else {
							a.cyclePanesForward()
						}
						return nil
					}
					// Cycle between description and comments within details pane
					if !isBackward {
						// Tab: description -> comments -> next pane
						if a.focusedDetailsView {
							// Currently on comments, move to next pane
							a.focusedDetailsView = false // Reset for next time
							a.cyclePanesForward()
						} else {
							// Currently on description, move to comments
							a.focusedDetailsView = true
							a.updateFocus()
						}
					} else {
						// Shift+Tab: comments -> description -> previous pane
						if a.focusedDetailsView {
							// Currently on comments, move to description
							a.focusedDetailsView = false
							a.updateFocus()
						} else {
							// Currently on description, move to previous pane
							a.cyclePanesBackward()
						}
					}
				} else {
					if isBackward {
						// Shift+Tab cycles backward
						a.cyclePanesBackward()
					} else {
						a.cyclePanesForward()
					}
				}
			}
			return nil
		case tcell.KeyRune:
			switch event.Rune() {
			case '?':
				a.showKeyHelp()
				return nil
			case a.actionKey("quit", 'q'):
				a.quit()
				return nil
			case a.actionKey("open_palette", ':'):
				a.openPalette()
				return nil
			case a.actionKey("search", '/'):
				a.openSearchPalette()
				return nil
			}
		}

		// Pane-specific shortcuts
		switch a.focusedPane {
		case FocusNavigation:
			return a.handleNavigationKey(event)
		case FocusIssues:
			return a.handleIssuesKey(event)
		case FocusDetails:
			return a.handleDetailsKey(event)
		}

		return event
	})
}

func (a *App) handleWorkspaceViewKey(event *tcell.EventKey) (bool, *tcell.EventKey) {
	if a == nil || a.pages == nil || event == nil {
		return false, event
	}
	if a.pages.HasPage(inboxViewPageName) && a.inboxView != nil {
		if a.dispatchKeySequence(event) {
			return true, nil
		}
		if a.isPaletteKeyEvent(event) {
			a.openPalette()
			return true, nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '?' {
			a.showKeyHelp()
			return true, nil
		}
		return true, a.inboxView.HandleKey(event)
	}
	if a.pages.HasPage(commentsModalPageName) && a.commentsModal != nil {
		if a.commentsModal.IsEditing() || a.commentsModal.IsReactionPickerOpen() || a.commentsModal.IsDeleteConfirmationOpen() {
			return true, a.commentsModal.HandleKey(event)
		}
		if a.dispatchKeySequence(event) {
			return true, nil
		}
		if a.isPaletteKeyEvent(event) {
			a.openPalette()
			return true, nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '?' {
			a.showKeyHelp()
			return true, nil
		}
		return true, a.commentsModal.HandleKey(event)
	}
	if a.pages.HasPage(roadmapPageName) && a.roadmapView != nil {
		if a.dispatchKeySequence(event) {
			return true, nil
		}
		if a.isPaletteKeyEvent(event) {
			a.openPalette()
			return true, nil
		}
		if event.Key() == tcell.KeyRune && event.Rune() == '?' {
			a.showKeyHelp()
			return true, nil
		}
		return true, a.roadmapView.HandleKey(event)
	}
	return false, event
}

func (a *App) keySequenceContext() string {
	switch a.focusedPane {
	case FocusNavigation:
		return "navigation"
	case FocusIssues:
		return "issues"
	case FocusDetails:
		return "details"
	default:
		return "global"
	}
}

func (a *App) dispatchKeySequence(event *tcell.EventKey) bool {
	if a.keySequenceDispatcher == nil || event == nil {
		return false
	}
	if event.Key() != tcell.KeyRune || event.Modifiers() != tcell.ModNone {
		if a.keySequenceDispatcher.Pending() {
			a.keySequenceDispatcher.Reset()
			a.keySequenceHint = ""
			a.updateStatusBar()
		}
		return false
	}
	now := time.Now()
	if a.keySequenceClock != nil {
		now = a.keySequenceClock()
	}
	result := a.keySequenceDispatcher.Feed(a.keySequenceContext(), event.Rune(), now)
	switch result.State {
	case KeySequenceStatePending:
		hint := result.Hint
		if completions := a.keySequenceCompletionHint(result.Prefix); completions != "" {
			hint = completions
		}
		a.keySequenceHint = fmt.Sprintf("%s: %s", result.Prefix, hint)
		a.updateStatusBar()
		return true
	case KeySequenceStateComplete:
		a.keySequenceHint = ""
		a.updateStatusBar()
		a.runKeySequenceCommand(result.CommandID)
		return true
	case KeySequenceStateInvalid, KeySequenceStateTimedOut:
		// The dispatcher has reset itself. Returning false deliberately lets the
		// same key continue through ordinary command/navigation handling.
		a.keySequenceHint = ""
		a.updateStatusBar()
	}
	return false
}

func (a *App) keySequenceCompletionHint(prefix string) string {
	bindings, err := keySequenceBindingsForConfig(a.config.Keybindings)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(bindings))
	seen := make(map[string]bool)
	for _, binding := range bindings {
		fields := strings.Fields(binding.Sequence)
		prefixFields := strings.Fields(prefix)
		if len(fields) <= len(prefixFields) || len(prefixFields) == 0 {
			continue
		}
		matches := true
		for index := range prefixFields {
			if fields[index] != prefixFields[index] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		key := fields[len(prefixFields)]
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, fmt.Sprintf("%s %s", key, binding.Hint))
	}
	return strings.Join(parts, " | ")
}

func (a *App) runKeySequenceCommand(commandID string) {
	var destination KeySequenceDestination
	switch commandID {
	case "navigate_all", "go_all", "all":
		destination = KeySequenceDestinationAll
	case "navigate_mine", "go_mine", "mine":
		destination = KeySequenceDestinationMine
	case "navigate_inbox", "go_inbox", "inbox":
		destination = KeySequenceDestinationInbox
	case "navigate_triage", "go_triage", "triage":
		destination = KeySequenceDestinationTriage
	case "navigate_views", "go_views", "views":
		destination = KeySequenceDestinationViews
	case "navigate_favorites", "go_favorites", "favorites":
		destination = KeySequenceDestinationFavorites
	case "navigate_projects", "go_projects", "projects":
		destination = KeySequenceDestinationProjects
	case "navigate_initiatives", "go_initiatives", "initiatives":
		destination = KeySequenceDestinationInitiatives
	case "navigate_cycles", "go_cycles", "cycles":
		destination = KeySequenceDestinationCycles
	default:
		logger.Warning("tui.app: unknown key sequence command command_id=%s", commandID)
		return
	}
	navigate := a.navigateDestinationFunc
	if navigate == nil {
		navigate = a.navigateKeySequenceDestination
	}
	if !navigate(destination) {
		a.flashStatus(fmt.Sprintf("%s navigation unavailable", keySequenceDestinationLabel(destination)))
	}
}

func keySequenceDestinationLabel(destination KeySequenceDestination) string {
	switch destination {
	case KeySequenceDestinationAll:
		return "All Issues"
	case KeySequenceDestinationMine:
		return "Mine"
	case KeySequenceDestinationInbox:
		return "Inbox"
	case KeySequenceDestinationTriage:
		return "Triage"
	case KeySequenceDestinationViews:
		return "Views"
	case KeySequenceDestinationFavorites:
		return "Favorites"
	case KeySequenceDestinationProjects:
		return "Projects"
	case KeySequenceDestinationInitiatives:
		return "Initiatives"
	case KeySequenceDestinationCycles:
		return "Cycles"
	default:
		return string(destination)
	}
}

func (a *App) navigateKeySequenceDestination(destination KeySequenceDestination) bool {
	// Issue-list destinations are the boundary between workspace overlays and
	// the main issue list. Tear down every overlay before selecting a tree node
	// or starting its refresh so stale runners cannot repaint the new route.
	switch destination {
	case KeySequenceDestinationAll,
		KeySequenceDestinationMine,
		KeySequenceDestinationTriage,
		KeySequenceDestinationFavorites,
		KeySequenceDestinationProjects,
		KeySequenceDestinationCycles:
		a.transitionToIssueListDestination()
	default:
		a.invalidateIssueMutationContexts()
	}
	switch destination {
	case KeySequenceDestinationAll:
		if node := a.findNavigationTreeNode(func(nav *NavigationNode) bool { return nav.ID == "all" && !nav.IsIssue }); node != nil {
			a.selectKeySequenceTreeNode(node)
			return true
		}
		a.richFilters = IssueFilters{}
		a.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
		a.reapplyKeybindings()
		a.focusedPane = FocusIssues
		a.updateFocus()
		go a.refreshIssuesWithFocusChange(false)
		return true
	case KeySequenceDestinationMine:
		user := a.currentUser
		if user == nil {
			for index := range a.teamUsers {
				if a.teamUsers[index].IsMe {
					user = &a.teamUsers[index]
					break
				}
			}
		}
		if user == nil || user.ID == "" {
			return false
		}
		a.richFilters.AssigneeID = user.ID
		a.richFilters.AssigneeName = formatUserDisplayName(*user)
		a.selectedNavigation = &NavigationNode{ID: "mine", Text: "Mine"}
		a.focusedPane = FocusIssues
		a.updateFocus()
		go a.refreshIssuesWithFocusChange(false)
		return true
	case KeySequenceDestinationTriage:
		if node := a.findNavigationTreeNode(func(nav *NavigationNode) bool { return nav.StateType == "triage" }); node != nil {
			a.selectKeySequenceTreeNode(node)
			return true
		}
		a.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", TeamID: a.GetSelectedTeamID(), StateType: "triage"}
		a.reapplyKeybindings()
		a.focusedPane = FocusIssues
		a.updateFocus()
		go a.refreshIssuesWithFocusChange(false)
		return true
	case KeySequenceDestinationFavorites:
		if node := a.findTreeNodeByText("Favorites"); node != nil {
			if favorite := firstSelectableTreeDescendant(node); favorite != nil {
				a.selectKeySequenceTreeNode(favorite)
				return true
			}
		}
		return false
	case KeySequenceDestinationViews:
		a.openSavedViews()
		return true
	case KeySequenceDestinationProjects:
		if node := a.findNavigationTreeNode(func(nav *NavigationNode) bool { return nav.IsProject }); node != nil {
			a.selectKeySequenceTreeNode(node)
			return true
		}
		return false
	case KeySequenceDestinationCycles:
		if node := a.findNavigationTreeNode(func(nav *NavigationNode) bool { return nav.IsCycle && nav.CycleID != "" }); node != nil {
			a.selectKeySequenceTreeNode(node)
			return true
		}
		return false
	case KeySequenceDestinationInbox:
		if a.listNotificationsFunc == nil && strings.TrimSpace(a.config.LinearAPIKey) == "" {
			return false
		}
		a.openInbox()
		return true
	case KeySequenceDestinationInitiatives:
		if a.listInitiativesFunc == nil && strings.TrimSpace(a.config.LinearAPIKey) == "" {
			return false
		}
		a.openRoadmap()
		return true
	default:
		return false
	}
}

func (a *App) selectKeySequenceTreeNode(node *tview.TreeNode) {
	if node == nil || a.navigationTree == nil {
		return
	}
	a.navigationTree.SetCurrentNode(node)
	// Commit focus before onNavigationSelected starts its asynchronous refresh.
	// The test queue is synchronous and the real UI queue serializes updates;
	// either way this ordering prevents the refresh from repainting panes while
	// focus titles are still being changed.
	a.focusedPane = FocusIssues
	a.updateFocus()
	if nav, ok := node.GetReference().(*NavigationNode); ok {
		a.onNavigationSelected(nav)
	}
}

// firstSelectableTreeDescendant finds the first concrete navigation target
// under a grouping node such as Favorites. Selecting the group itself would
// leave the issue list without a meaningful filter or refresh target.
func firstSelectableTreeDescendant(node *tview.TreeNode) *tview.TreeNode {
	if node == nil {
		return nil
	}
	for _, child := range node.GetChildren() {
		if nav, ok := child.GetReference().(*NavigationNode); ok && !nav.IsFolder {
			return child
		}
		if favorite := firstSelectableTreeDescendant(child); favorite != nil {
			return favorite
		}
	}
	return nil
}

func (a *App) findNavigationTreeNode(match func(*NavigationNode) bool) *tview.TreeNode {
	if a.navigationTree == nil || match == nil {
		return nil
	}
	var visit func(*tview.TreeNode) *tview.TreeNode
	visit = func(node *tview.TreeNode) *tview.TreeNode {
		if node == nil {
			return nil
		}
		if nav, ok := node.GetReference().(*NavigationNode); ok && match(nav) {
			return node
		}
		for _, child := range node.GetChildren() {
			if found := visit(child); found != nil {
				return found
			}
		}
		return nil
	}
	return visit(a.navigationTree.GetRoot())
}

func (a *App) findTreeNodeByText(text string) *tview.TreeNode {
	if a.navigationTree == nil {
		return nil
	}
	var visit func(*tview.TreeNode) *tview.TreeNode
	visit = func(node *tview.TreeNode) *tview.TreeNode {
		if node == nil {
			return nil
		}
		if strings.TrimSpace(node.GetText()) == strings.TrimSpace(text) {
			return node
		}
		for _, child := range node.GetChildren() {
			if found := visit(child); found != nil {
				return found
			}
		}
		return nil
	}
	return visit(a.navigationTree.GetRoot())
}

func (a *App) showKeyHelp() {
	if a.pages == nil {
		return
	}
	if a.pages.HasPage("key_help") {
		return
	}
	lines := []string{"Keyboard help", "", "Single keys:"}
	if a.paletteCtrl != nil {
		for _, command := range a.paletteCtrl.commands {
			shortcut := command.ShortcutDisplay
			if shortcut == "" && command.ShortcutRune != 0 {
				shortcut = FormatShortcut(command.ShortcutRune)
			}
			if shortcut != "" {
				lines = append(lines, fmt.Sprintf("  %-8s %s", shortcut, command.Title))
			}
		}
	}
	lines = append(lines, "", "Chords:")
	bindings, bindingErr := keySequenceBindingsForConfig(a.config.Keybindings)
	if bindingErr != nil {
		bindings = defaultKeySequenceBindings()
	}
	for _, binding := range bindings {
		lines = append(lines, fmt.Sprintf("  %-8s %s", binding.Sequence, binding.Hint))
	}
	if a.pages.HasPage(inboxViewPageName) && a.inboxView != nil {
		lines = append(lines, "", "Inbox:", "  "+a.inboxView.HelpText())
	}
	if a.pages.HasPage(commentsModalPageName) && a.commentsModal != nil {
		lines = append(lines, "", "Comments / Activity:", "  "+a.commentsModal.HelpText())
	}
	if a.pages.HasPage(roadmapPageName) && a.roadmapView != nil {
		lines = append(lines, "", "Roadmap:", "  j/k or arrows: navigate | Enter/Space: expand | n: new initiative | c: create | e: edit | a: archive/delete | d: delete | Esc: close")
	}
	lines = append(lines, "", "Esc: close")
	help := tview.NewTextView().SetDynamicColors(false).SetWrap(true).SetWordWrap(true)
	help.SetText(strings.Join(lines, "\n"))
	help.SetTextColor(a.theme.Foreground).SetBackgroundColor(a.theme.HeaderBg)
	content := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(help, 0, 1, true)
	content.SetBorder(true).SetBorderColor(a.theme.Accent).SetTitle(" Help ").SetTitleColor(a.theme.Foreground)
	content.SetBackgroundColor(a.theme.HeaderBg)
	a.keyHelpModal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(content, 70, 0, true).
		AddItem(nil, 0, 1, false)
	a.keyHelpModal.SetBackgroundColor(a.theme.Background)
	a.pages.AddPage("key_help", a.keyHelpModal, true, true)
	a.app.SetFocus(help)
}

func (a *App) hideKeyHelp() {
	if a.pages != nil {
		a.pages.RemovePage("key_help")
	}
	a.keyHelpModal = nil
	a.updateFocus()
}

// runCommandShortcut fires the palette command bound to the rune, if any.
func (a *App) runCommandShortcut(r rune) bool {
	for _, cmd := range a.paletteCtrl.commands {
		if cmd.ShortcutRune != 0 && cmd.ShortcutRune == r {
			cmd.Run(a)
			return true
		}
	}
	return false
}

// handleNavigationKey handles keyboard input when navigation pane is focused.
func (a *App) handleNavigationKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRight:
		a.focusedPane = FocusIssues
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		switch r := event.Rune(); r {
		case 'l':
			a.focusedPane = FocusIssues
			a.updateFocus()
			return nil
		case 'j', 'k', 'g', 'G', 'h':
			// Tree movement keys stay with the tree.
		default:
			// Command shortcuts work from the navigation pane too.
			if a.runCommandShortcut(r) {
				return nil
			}
		}
	}
	return event
}

// handleIssuesKey handles keyboard input when issues pane is focused.
func (a *App) handleIssuesKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyCtrlA {
		a.selectAllVisibleIssues()
		return nil
	}
	switch event.Key() {
	case tcell.KeyLeft:
		a.focusedPane = FocusNavigation
		a.updateFocus()
		return nil
	case tcell.KeyRight:
		a.focusedPane = FocusDetails
		a.focusedDetailsView = false // Start with description
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		r := event.Rune()
		if r == 'v' {
			a.toggleMarkedCursorIssue()
			return nil
		}
		if r == 'V' {
			a.extendMarkedCursorIssue()
			return nil
		}
		// Handle vim-style navigation first
		switch r {
		case 'h':
			a.focusedPane = FocusNavigation
			a.updateFocus()
			return nil
		case 'l':
			a.focusedPane = FocusDetails
			a.focusedDetailsView = false // Start with description
			a.updateFocus()
			return nil
		}
		// Handle command shortcuts (plain letters) - skip navigation keys
		if r != 'j' && r != 'k' { // j/k are handled by table for up/down
			if a.runCommandShortcut(r) {
				return nil
			}
		}
	}
	return event
}

// handleDetailsKey handles keyboard input when details pane is focused.
func (a *App) handleDetailsKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyLeft:
		a.focusedPane = FocusIssues
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		if event.Rune() == 'h' {
			a.focusedPane = FocusIssues
			a.updateFocus()
			return nil
		}
	}
	return event
}

// handlePaletteKey handles keyboard input when palette is open.
func (a *App) handlePaletteKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		if a.paletteCtrl.IsSearchMode() {
			// In search mode, clear search and close palette
			a.cancelSearchDebounce()
			a.closePaletteUI()
			a.setSearchQuery("")
			return nil
		}
		a.closePalette()
		return nil
	case tcell.KeyEnter:
		if a.paletteCtrl.IsSearchMode() {
			// In search mode, submit the search query
			query := a.paletteCtrl.Query()
			a.cancelSearchDebounce()
			a.prepareGlobalSearchContext()
			a.closePaletteUI()      // Close UI without changing focus
			a.setSearchQuery(query) // This will set focus to issues pane
			return nil
		}
		// In command mode, execute the selected command
		if cmd, ok := a.paletteCtrl.Selected(); ok {
			a.closePalette()
			cmd.Run(a)
			return nil
		}
		return nil
	case tcell.KeyUp:
		if !a.paletteCtrl.IsSearchMode() {
			a.paletteCtrl.MoveCursorUp()
			a.updatePaletteList()
		}
		return nil
	case tcell.KeyDown:
		if !a.paletteCtrl.IsSearchMode() {
			a.paletteCtrl.MoveCursorDown()
			a.updatePaletteList()
		}
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		query := a.paletteCtrl.Query()
		if len(query) > 0 {
			a.paletteCtrl.SetQuery(query[:len(query)-1])
			a.paletteInput.SetText(a.paletteCtrl.Query())
			if a.paletteCtrl.IsSearchMode() {
				a.scheduleSearchDebounce(a.paletteCtrl.Query())
			} else {
				a.updatePaletteList()
			}
		}
		return nil
	case tcell.KeyRune:
		query := a.paletteCtrl.Query() + string(event.Rune())
		a.paletteCtrl.SetQuery(query)
		a.paletteInput.SetText(query)
		if a.paletteCtrl.IsSearchMode() {
			a.scheduleSearchDebounce(query)
		} else {
			a.updatePaletteList()
		}
		return nil
	}
	return event
}

// cyclePanesForward cycles focus forward through panes.
// When in Issues pane, cycles: My Issues -> Other Issues -> Details
// Otherwise cycles: Navigation -> Issues -> Details -> Navigation
func (a *App) cyclePanesForward() {
	switch a.focusedPane {
	case FocusNavigation:
		a.focusedPane = FocusIssues
		// Set to My Issues if available, otherwise Other Issues
		if len(a.myIssueRows) > 0 {
			a.activeIssuesSection = IssuesSectionMy
		} else {
			a.activeIssuesSection = IssuesSectionOther
		}
	case FocusIssues:
		// If both My and Other issues exist, switch between them
		if len(a.myIssueRows) > 0 && len(a.otherIssueRows) > 0 {
			if a.activeIssuesSection == IssuesSectionMy {
				// Switch from My Issues to Other Issues
				a.activeIssuesSection = IssuesSectionOther
			} else {
				// Switch from Other Issues to Details pane
				a.focusedPane = FocusDetails
				a.focusedDetailsView = false // Start with description
			}
		} else {
			// Only one section exists, move to Details
			a.focusedPane = FocusDetails
			a.focusedDetailsView = false // Start with description
		}
	case FocusDetails:
		a.focusedPane = FocusNavigation
		// FocusPalette is excluded from cycling
	}
	a.updateFocus()
}

// cyclePanesBackward cycles focus backward through panes.
// When in Issues pane, cycles: Other Issues -> My Issues -> Navigation
// Otherwise cycles: Details -> Issues (My Issues preferred) -> Navigation -> Details
func (a *App) cyclePanesBackward() {
	switch a.focusedPane {
	case FocusNavigation:
		a.focusedPane = FocusDetails
		a.focusedDetailsView = false // Start with description
	case FocusIssues:
		// If both My and Other issues exist, switch between them
		if len(a.myIssueRows) > 0 && len(a.otherIssueRows) > 0 {
			if a.activeIssuesSection == IssuesSectionOther {
				// Switch from Other Issues to My Issues
				a.activeIssuesSection = IssuesSectionMy
			} else {
				// Switch from My Issues to Navigation pane
				a.focusedPane = FocusNavigation
			}
		} else {
			// Only one section exists, move to Navigation
			a.focusedPane = FocusNavigation
		}
	case FocusDetails:
		a.focusedPane = FocusIssues
		// Set to My Issues if available, otherwise Other Issues (consistent with forward cycle)
		if len(a.myIssueRows) > 0 {
			a.activeIssuesSection = IssuesSectionMy
		} else {
			a.activeIssuesSection = IssuesSectionOther
		}
		// FocusPalette is excluded from cycling
	}
	a.updateFocus()
}

// updateFocus updates the focus state of all panes.
func (a *App) updateFocus() {
	switch a.focusedPane {
	case FocusNavigation:
		a.app.SetFocus(a.navigationTree)
		a.navigationTree.SetBorderColor(a.theme.BorderFocus)
		a.myIssuesTable.SetBorderColor(a.theme.Border)
		a.otherIssuesTable.SetBorderColor(a.theme.Border)
		a.detailsDescriptionView.SetBorderColor(a.theme.Border)
		a.detailsCommentsView.SetBorderColor(a.theme.Border)
		// Update all pane titles
		a.updateAllPaneTitles()
	case FocusIssues:
		// Focus the active issues section
		if a.activeIssuesSection == IssuesSectionMy && len(a.myIssueRows) > 0 {
			a.app.SetFocus(a.myIssuesTable)
			a.myIssuesTable.SetBorderColor(a.theme.BorderFocus)
			a.otherIssuesTable.SetBorderColor(a.theme.Border)
		} else {
			a.app.SetFocus(a.otherIssuesTable)
			a.myIssuesTable.SetBorderColor(a.theme.Border)
			a.otherIssuesTable.SetBorderColor(a.theme.BorderFocus)
			a.activeIssuesSection = IssuesSectionOther
		}
		// Update all pane titles
		a.updateAllPaneTitles()
		a.navigationTree.SetBorderColor(a.theme.Border)
		a.detailsDescriptionView.SetBorderColor(a.theme.Border)
		a.detailsCommentsView.SetBorderColor(a.theme.Border)
	case FocusDetails:
		// Focus the appropriate sub-view based on state
		if !a.detailsCommentsVisible {
			a.focusedDetailsView = false
		}
		if a.focusedDetailsView && a.detailsCommentsVisible {
			a.app.SetFocus(a.detailsCommentsView)
			a.detailsDescriptionView.SetBorderColor(a.theme.Border)
			a.detailsCommentsView.SetBorderColor(a.theme.BorderFocus)
		} else {
			a.app.SetFocus(a.detailsDescriptionView)
			a.detailsDescriptionView.SetBorderColor(a.theme.BorderFocus)
			a.detailsCommentsView.SetBorderColor(a.theme.Border)
		}
		a.navigationTree.SetBorderColor(a.theme.Border)
		a.myIssuesTable.SetBorderColor(a.theme.Border)
		a.otherIssuesTable.SetBorderColor(a.theme.Border)
		// Update all pane titles
		a.updateAllPaneTitles()
	case FocusPalette:
		a.app.SetFocus(a.paletteInput)
		a.navigationTree.SetBorderColor(a.theme.Border)
		a.myIssuesTable.SetBorderColor(a.theme.Border)
		a.otherIssuesTable.SetBorderColor(a.theme.Border)
		a.detailsDescriptionView.SetBorderColor(a.theme.Border)
		a.detailsCommentsView.SetBorderColor(a.theme.Border)
		// Update all pane titles
		a.updateAllPaneTitles()
	}
	a.updateStatusBar()
}

// updateAllPaneTitles updates all pane titles with visual indicators for the active pane.
func (a *App) updateAllPaneTitles() {
	// Update Navigation pane title
	if a.focusedPane == FocusNavigation {
		a.navigationTree.SetTitle(" ▶ Navigation ")
		a.navigationTree.SetTitleColor(a.theme.Accent)
	} else {
		a.navigationTree.SetTitle(" Navigation ")
		a.navigationTree.SetTitleColor(a.theme.Foreground)
	}

	// Update Issues pane titles
	isIssuesFocused := a.focusedPane == FocusIssues

	// Update My Issues table title
	if len(a.myIssueRows) > 0 {
		if isIssuesFocused && a.activeIssuesSection == IssuesSectionMy {
			// Active section: add visual indicator and accent color
			a.myIssuesTable.SetTitle(" ▶ My Issues ")
			a.myIssuesTable.SetTitleColor(a.theme.Accent)
		} else {
			// Inactive section: normal title
			a.myIssuesTable.SetTitle(" My Issues ")
			a.myIssuesTable.SetTitleColor(a.theme.Foreground)
		}
	} else {
		// No issues in this section
		a.myIssuesTable.SetTitle(" My Issues ")
		a.myIssuesTable.SetTitleColor(a.theme.Foreground)
	}

	// Update Other Issues table title
	if len(a.otherIssueRows) > 0 {
		if isIssuesFocused && a.activeIssuesSection == IssuesSectionOther {
			// Active section: add visual indicator and accent color
			a.otherIssuesTable.SetTitle(" ▶ Other Issues ")
			a.otherIssuesTable.SetTitleColor(a.theme.Accent)
		} else {
			// Inactive section: normal title
			a.otherIssuesTable.SetTitle(" Other Issues ")
			a.otherIssuesTable.SetTitleColor(a.theme.Foreground)
		}
	} else {
		// No issues in this section
		a.otherIssuesTable.SetTitle(" Other Issues ")
		a.otherIssuesTable.SetTitleColor(a.theme.Foreground)
	}

	// Update Details pane titles
	isDetailsFocused := a.focusedPane == FocusDetails
	if a.detailsDescriptionView != nil {
		if isDetailsFocused {
			// Details pane is focused - show indicator on active sub-view
			if a.focusedDetailsView && a.detailsCommentsVisible && a.detailsCommentsView != nil {
				// Comments view is active
				a.detailsDescriptionView.SetTitle(" Details ")
				a.detailsDescriptionView.SetTitleColor(a.theme.Foreground)
				a.detailsCommentsView.SetTitle(" ▶ Comments ")
				a.detailsCommentsView.SetTitleColor(a.theme.Accent)
			} else {
				// Description view is active
				a.detailsDescriptionView.SetTitle(" ▶ Details ")
				a.detailsDescriptionView.SetTitleColor(a.theme.Accent)
				if a.detailsCommentsVisible && a.detailsCommentsView != nil {
					a.detailsCommentsView.SetTitle(" Comments ")
					a.detailsCommentsView.SetTitleColor(a.theme.Foreground)
				}
			}
		} else {
			// Details pane is not focused - reset both titles
			a.detailsDescriptionView.SetTitle(" Details ")
			a.detailsDescriptionView.SetTitleColor(a.theme.Foreground)
			if a.detailsCommentsView != nil {
				a.detailsCommentsView.SetTitle(" Comments ")
				a.detailsCommentsView.SetTitleColor(a.theme.Foreground)
			}
		}
	}
}

// openPalette opens the command palette overlay.
func (a *App) openPalette() {
	a.paletteCtrl.SetIssueContext(a.focusedPane == FocusIssues || a.focusedPane == FocusDetails)
	a.paletteCtrl.Reset()
	a.paletteInput.SetText("")
	a.paletteInput.SetLabel("> ")
	a.paletteInput.SetPlaceholder("Type to filter commands...")
	a.updatePaletteList()
	a.pages.ShowPage("palette")
	a.pages.SendToFront("palette")
	a.focusedPane = FocusPalette
	a.updateFocus()
}

// openSearchPalette opens the palette in search mode.
func (a *App) openSearchPalette() {
	a.prepareGlobalSearchContext()
	a.paletteCtrl.SetSearchMode(true)
	a.paletteCtrl.SetQuery(a.searchQuery)
	a.paletteInput.SetText(a.searchQuery)
	a.paletteInput.SetLabel("/ ")
	a.paletteInput.SetPlaceholder("Type to search issues...")
	a.updatePaletteList()
	a.pages.ShowPage("palette")
	a.pages.SendToFront("palette")
	a.focusedPane = FocusPalette
	a.updateFocus()
}

// closePalette closes the command palette overlay.
func (a *App) closePalette() {
	a.cancelSearchDebounce()
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
	a.focusedPane = FocusNavigation
	a.updateFocus()
}

// closePaletteUI closes the palette UI without changing focus.
// This is used when focus will be set by the caller (e.g., after search).
func (a *App) closePaletteUI() {
	a.cancelSearchDebounce()
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
}

func (a *App) searchDebounceDelay() time.Duration {
	if a.config.SearchDebounce > 0 {
		return a.config.SearchDebounce
	}
	return config.DefaultSearchDebounce
}

func (a *App) scheduleSearchDebounce(query string) {
	delay := a.searchDebounceDelay()
	generation := a.searchDebounceGeneration.Add(1)

	a.searchDebounceMu.Lock()
	if a.searchDebounceTimer != nil {
		a.searchDebounceTimer.Stop()
	}
	a.searchDebounceTimer = time.AfterFunc(delay, func() {
		if generation != a.searchDebounceGeneration.Load() {
			return
		}
		a.QueueUpdateDraw(func() {
			if generation != a.searchDebounceGeneration.Load() || !a.paletteCtrl.IsSearchMode() {
				return
			}
			a.setSearchQueryWithFocusChange(query, false)
		})
	})
	a.searchDebounceMu.Unlock()
}

func (a *App) cancelSearchDebounce() {
	a.searchDebounceGeneration.Add(1)

	a.searchDebounceMu.Lock()
	if a.searchDebounceTimer != nil {
		a.searchDebounceTimer.Stop()
		a.searchDebounceTimer = nil
	}
	a.searchDebounceMu.Unlock()
}

func (a *App) queueIssuesRefreshLocked(allowFocusChange bool, issueID ...string) {
	logger.Debug("tui.app: queueing issues refresh issue_id=%v", issueID)
	a.pendingRefresh = true
	a.pendingRefreshAllowFocusChange = allowFocusChange
	a.refreshGeneration.Add(1)
	if len(issueID) > 0 {
		a.pendingRefreshIssueID = issueID[0]
		return
	}
	a.pendingRefreshIssueID = ""
}

// runQueuedIssuesRefresh triggers any queued refresh after a fetch completes.
func (a *App) runQueuedIssuesRefresh() {
	a.refreshStateMu.Lock()
	defer a.refreshStateMu.Unlock()
	if !a.pendingRefresh {
		return
	}
	issueID := a.pendingRefreshIssueID
	allowFocusChange := a.pendingRefreshAllowFocusChange
	logger.Debug("tui.app: running queued refresh issue_id=%s", issueID)
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	a.pendingRefreshAllowFocusChange = true
	if issueID != "" {
		go a.refreshIssuesWithFocusChange(allowFocusChange, issueID)
		return
	}
	go a.refreshIssuesWithFocusChange(allowFocusChange)
}

func (a *App) notifyRefreshCompleted() {
	if a.refreshCompleted != nil {
		a.refreshCompleted()
	}
}

// refreshIssues fetches issues from the API and updates the UI.
// If issueID is provided, that issue will be selected after refresh.
func (a *App) refreshIssues(issueID ...string) {
	a.refreshIssuesWithFocusChange(true, issueID...)
}

// refreshIssuesWithFocusChange fetches issues and optionally shifts focus to the issues pane.
func (a *App) refreshIssuesWithFocusChange(allowFocusChange bool, issueID ...string) {
	a.refreshStateMu.Lock()
	if a.isLoading {
		a.queueIssuesRefreshLocked(allowFocusChange, issueID...)
		a.refreshStateMu.Unlock()
		return
	}
	a.isLoading = true
	a.refreshStateMu.Unlock()

	targetID := ""
	if len(issueID) > 0 {
		targetID = issueID[0]
	}
	logger.Debug("tui.app: starting issues refresh target_issue_id=%s", targetID)
	generation := a.refreshGeneration.Add(1)
	var targetIssueID string
	if len(issueID) > 0 {
		targetIssueID = issueID[0]
	}

	allowFocus := allowFocusChange
	go func() {
		ctx := context.Background()

		params := linearapi.FetchIssuesParams{
			First:   a.config.PageSize,
			Search:  a.searchQuery,
			OrderBy: string(a.sortField),
		}
		a.applyRichFiltersToParams(&params)

		// Apply team/project/state filter based on navigation selection
		if a.selectedNavigation != nil {
			switch {
			case a.selectedNavigation.CustomViewID != "":
				params.CustomViewID = a.selectedNavigation.CustomViewID
			case a.selectedNavigation.StateType != "":
				params.TeamID = a.selectedNavigation.TeamID
				params.StateType = a.selectedNavigation.StateType
			case a.selectedNavigation.IsStatus:
				params.TeamID = a.selectedNavigation.TeamID
				params.StateID = a.selectedNavigation.StateID
			case a.selectedNavigation.IsCycle:
				params.TeamID = a.selectedNavigation.TeamID
				params.CycleID = a.selectedNavigation.CycleID
			case a.selectedNavigation.IsTeam:
				params.TeamID = a.selectedNavigation.TeamID
			case a.selectedNavigation.IsProject:
				params.TeamID = a.selectedNavigation.TeamID
				params.ProjectID = a.selectedNavigation.ID
			}
			// If "All Issues", no team/project filter
		}

		fetchPage := a.fetchIssuesPage
		if fetchPage == nil {
			fetchPage = a.api.FetchIssuesPage
		}

		pageCount := 0
		fetchedCount := 0
		logger.Debug("tui.app: refreshing issues team_id=%s project_id=%s state_id=%s cycle_id=%s assignee_id=%s labels=%d search=%s", params.TeamID, params.ProjectID, params.StateID, params.CycleID, params.AssigneeID, len(params.LabelIDs), params.Search)
		page, err := fetchPage(ctx, params, nil)
		if err != nil {
			a.QueueUpdateDraw(func() {
				a.refreshStateMu.Lock()
				a.isLoading = false
				a.refreshStateMu.Unlock()
				logger.ErrorWithErr(err, "tui.app: failed to fetch issues")
				a.updateStatusBarWithError(err)
				a.notifyRefreshCompleted()
				a.runQueuedIssuesRefresh()
			})
			return
		}
		if generation != a.refreshGeneration.Load() {
			a.QueueUpdateDraw(func() {
				a.refreshStateMu.Lock()
				a.isLoading = false
				a.refreshStateMu.Unlock()
				a.notifyRefreshCompleted()
				a.runQueuedIssuesRefresh()
			})
			return
		}

		pageCount++
		fetchedCount += len(page.Issues)
		a.QueueUpdateDraw(func() {
			logger.Debug("tui.app: fetched issues page=%d count=%d", pageCount, len(page.Issues))
			a.updateIssuesData(page.Issues, targetIssueID)
			if allowFocus {
				// Ensure focus is on issues table after initial load
				a.focusedPane = FocusIssues
				a.updateFocus()
			}
			if page.HasNext {
				a.statusBar.SetText(fmt.Sprintf("%sLoading more (page %d, fetched %d)...[-]", a.themeTags.Warning, pageCount, fetchedCount))
			}
		})

		after := page.EndCursor
		for page.HasNext {
			if generation != a.refreshGeneration.Load() {
				break
			}
			nextPage, err := fetchPage(ctx, params, after)
			if err != nil {
				a.QueueUpdateDraw(func() {
					logger.ErrorWithErr(err, "tui.app: failed to fetch more issues page=%d", pageCount+1)
					a.updateStatusBarWithError(err)
				})
				break
			}
			if generation != a.refreshGeneration.Load() {
				break
			}

			page = nextPage
			after = page.EndCursor
			pageCount++
			fetchedCount += len(page.Issues)
			a.QueueUpdateDraw(func() {
				a.appendIssuesData(page.Issues)
				if page.HasNext {
					a.statusBar.SetText(fmt.Sprintf("%sLoading more (page %d, fetched %d)...[-]", a.themeTags.Warning, pageCount, fetchedCount))
				}
			})
		}

		a.QueueUpdateDraw(func() {
			a.refreshStateMu.Lock()
			a.isLoading = false
			a.refreshStateMu.Unlock()
			logger.Debug("tui.app: refresh completed pages=%d total_fetched=%d", pageCount, fetchedCount)
			a.updateStatusBar()
			a.notifyRefreshCompleted()
			a.runQueuedIssuesRefresh()
		})
	}()

	// Show loading indicator
	a.QueueUpdateDraw(func() {
		a.statusBar.SetText(fmt.Sprintf("%sLoading...[-]", a.themeTags.Warning))
	})
}

func (a *App) applyRichFiltersToParams(params *linearapi.FetchIssuesParams) {
	if params == nil {
		return
	}
	filters := a.richFilters
	if filters.AssigneeID != "" {
		params.AssigneeID = filters.AssigneeID
	}
	if len(filters.LabelIDs) > 0 {
		params.LabelIDs = append([]string(nil), filters.LabelIDs...)
	}
	if filters.StateID != "" {
		params.StateID = filters.StateID
	}
	if filters.ProjectID != "" {
		params.ProjectID = filters.ProjectID
	}
	if filters.CycleID != "" {
		params.CycleID = filters.CycleID
	}
	if !filters.DueDate.Empty() {
		params.DueDate = filters.DueDate
	}
	if !filters.Estimate.Empty() {
		params.Estimate = filters.Estimate
	}
}

// updateIssuesColumnLayout updates the issues column flex to show/hide My Issues table.
func (a *App) updateIssuesColumnLayout() {
	a.issuesColumn.Clear()

	// Add My Issues table if there are any
	if len(a.myIssueRows) > 0 {
		a.issuesColumn.AddItem(a.myIssuesTable, 0, 1, false)
	}

	// Always add Other Issues table
	a.issuesColumn.AddItem(a.otherIssuesTable, 0, 1, false)

	// Update all pane titles to reflect current state
	a.updateAllPaneTitles()
}

// updateIssuesData updates the UI with new issues data.
// If issueID is provided, that issue will be selected if found in the list.
func (a *App) updateIssuesData(issues []linearapi.Issue, issueID ...string) {
	a.issuesMu.Lock()
	a.issues = issues
	if a.sortField == SortByPriority {
		sortIssuesByPriority(a.issues)
	}

	// Determine target issue ID
	var targetIssueID string
	if len(issueID) > 0 && issueID[0] != "" {
		targetIssueID = issueID[0]
	} else if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}
	a.issuesMu.Unlock()

	selectedIssue := a.rebuildIssuesTables(targetIssueID)
	if selectedIssue != nil {
		a.onIssueSelected(*selectedIssue)
	} else {
		a.issuesMu.Lock()
		a.selectedIssue = nil
		a.issuesMu.Unlock()
		a.updateDetailsView()
	}
	a.updateStatusBar()
}

// rebuildIssuesTables rebuilds issue rows and renders tables, returning the selected issue.
func (a *App) rebuildIssuesTables(targetIssueID string) *linearapi.Issue {
	// Split issues by assignee.
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()

	// Marks belong to the complete loaded issue set, not merely the currently
	// visible flattened rows. A collapsed child remains actionable until it is
	// actually absent from the loaded data.
	validIDs := make([]string, 0, len(issues))
	for i := range issues {
		if issues[i].ID != "" {
			validIDs = append(validIDs, issues[i].ID)
		}
	}
	a.markedIssueSelection.Reconcile(validIDs)

	currentUserID := ""
	if a.currentUser != nil {
		currentUserID = a.currentUser.ID
	}
	myIssues, otherIssues := splitIssuesByAssignee(issues, currentUserID)

	// Build hierarchical tree rows for each section.
	a.myIssueRows, a.myIDToIssue = BuildIssueRows(myIssues, a.expandedState)
	a.otherIssueRows, a.otherIDToIssue = BuildIssueRows(otherIssues, a.expandedState)

	// Legacy: keep old fields for backward compatibility during migration.
	a.issueRows = make([]IssueRow, 0, len(a.myIssueRows)+len(a.otherIssueRows))
	a.issueRows = append(a.issueRows, a.myIssueRows...)
	a.issueRows = append(a.issueRows, a.otherIssueRows...)
	a.idToIssue = make(map[string]*linearapi.Issue)
	for k, v := range a.myIDToIssue {
		a.idToIssue[k] = v
	}
	for k, v := range a.otherIDToIssue {
		a.idToIssue[k] = v
	}

	// Update layout to show/hide My Issues section.
	a.updateIssuesColumnLayout()

	// Render both tables.
	var selectedMyIssueID, selectedOtherIssueID string
	if targetIssueID != "" {
		// Check which section contains the target issue.
		if _, ok := a.myIDToIssue[targetIssueID]; ok {
			selectedMyIssueID = targetIssueID
			a.activeIssuesSection = IssuesSectionMy
		} else if _, ok := a.otherIDToIssue[targetIssueID]; ok {
			selectedOtherIssueID = targetIssueID
			a.activeIssuesSection = IssuesSectionOther
		}
	}

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme, &a.markedIssueSelection)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme, &a.markedIssueSelection)

	// Select issue and update details.
	var selectedIssue *linearapi.Issue
	if targetIssueID != "" {
		if issue, ok := a.myIDToIssue[targetIssueID]; ok {
			selectedIssue = issue
		} else if issue, ok := a.otherIDToIssue[targetIssueID]; ok {
			selectedIssue = issue
		}
	}

	// If no target issue, default to first available.
	if selectedIssue == nil {
		if len(a.myIssueRows) > 0 {
			if issue, ok := a.myIDToIssue[a.myIssueRows[0].IssueID]; ok {
				selectedIssue = issue
				a.activeIssuesSection = IssuesSectionMy
			}
		} else if len(a.otherIssueRows) > 0 {
			if issue, ok := a.otherIDToIssue[a.otherIssueRows[0].IssueID]; ok {
				selectedIssue = issue
				a.activeIssuesSection = IssuesSectionOther
			}
		}
	}

	return selectedIssue
}

// resolveIssueTargets resolves IDs against the complete loaded issue set in
// caller order. Duplicate and stale IDs are ignored so a refresh cannot cause
// a bulk command to mutate an issue that is no longer present.
func (a *App) resolveIssueTargets(issueIDs []string) []linearapi.Issue {
	if len(issueIDs) == 0 {
		return nil
	}
	a.issuesMu.RLock()
	defer a.issuesMu.RUnlock()

	byID := make(map[string]linearapi.Issue, len(a.issues))
	for _, issue := range a.issues {
		if issue.ID != "" {
			byID[issue.ID] = issue
		}
	}
	seen := make(map[string]struct{}, len(issueIDs))
	targets := make([]linearapi.Issue, 0, len(issueIDs))
	for _, issueID := range issueIDs {
		if issueID == "" {
			continue
		}
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		issue, ok := byID[issueID]
		if !ok {
			continue
		}
		targets = append(targets, issue)
	}
	return targets
}

// issueIDsForLoadedTargets returns IDs from resolveIssueTargets while keeping
// the selection model's deterministic order.
func (a *App) resolveIssueTargetIDs(issueIDs []string) []string {
	targets := a.resolveIssueTargets(issueIDs)
	ids := make([]string, 0, len(targets))
	for _, issue := range targets {
		ids = append(ids, issue.ID)
	}
	return ids
}

// appendIssuesData merges additional issues and updates rendered tables.
func (a *App) appendIssuesData(newIssues []linearapi.Issue) {
	if len(newIssues) == 0 {
		return
	}

	a.issuesMu.Lock()
	existing := make(map[string]bool, len(a.issues))
	for _, issue := range a.issues {
		existing[issue.ID] = true
	}
	for _, issue := range newIssues {
		if existing[issue.ID] {
			continue
		}
		a.issues = append(a.issues, issue)
		existing[issue.ID] = true
	}

	if a.sortField == SortByPriority {
		sortIssuesByPriority(a.issues)
	}

	targetIssueID := ""
	if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}
	a.issuesMu.Unlock()

	selectedIssue := a.rebuildIssuesTables(targetIssueID)
	a.issuesMu.Lock()
	if selectedIssue != nil {
		a.selectedIssue = selectedIssue
	} else {
		a.selectedIssue = nil
	}
	a.issuesMu.Unlock()
	a.updateDetailsView()
	a.updateStatusBar()
}

// sortIssuesByPriority sorts issues by priority using Linear's priority semantics.
func sortIssuesByPriority(issues []linearapi.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		pi, pj := issues[i].Priority, issues[j].Priority
		// Map 0 (no priority) to a high value so it sorts last.
		if pi == 0 {
			pi = 5
		}
		if pj == 0 {
			pj = 5
		}
		return pi < pj
	})
}

// onIssueSelected handles when an issue is selected.
func (a *App) onIssueSelected(issue linearapi.Issue) {
	logger.Debug("tui.app: issue selected issue=%s", issue.Identifier)
	// Set selected issue immediately for quick UI feedback
	a.issuesMu.Lock()
	previousIssueID := ""
	if a.selectedIssue != nil {
		previousIssueID = a.selectedIssue.ID
	}
	a.selectedIssue = &issue
	a.issuesMu.Unlock()
	if previousIssueID != issue.ID {
		a.issueSelectionGeneration.Add(1)
		a.invalidateIssueEditorSaveContext()
		a.invalidateIssueMutationContexts()
		if a.commentsModal != nil && a.pages != nil && a.pages.HasPage(commentsModalPageName) {
			if a.commentsModal.actionRunner != nil {
				a.commentsModal.actionRunner.Invalidate()
			}
			a.commentsLoadGeneration.Add(1)
		}
	}
	a.updateDetailsView()

	// Fetch full issue details (including comments) in background
	issueID := issue.ID
	a.issuesMu.Lock()
	a.fetchingIssueID = issueID
	a.issuesMu.Unlock()

	go func() {
		logger.Debug("tui.app: fetching full issue details issue=%s", issue.Identifier)
		ctx := context.Background()
		fetchIssue := a.fetchIssueByID
		if fetchIssue == nil {
			fetchIssue = a.api.FetchIssueByID
		}
		fullIssue, err := fetchIssue(ctx, issueID)

		a.QueueUpdateDraw(func() {
			// Race-safety: only apply if this is still the issue we're fetching.
			a.issuesMu.RLock()
			isCurrent := a.fetchingIssueID == issueID
			a.issuesMu.RUnlock()
			if isCurrent {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to fetch full issue details issue=%s", issue.Identifier)
					// Keep the partial issue data we already have
					return
				}
				a.issuesMu.Lock()
				a.selectedIssue = &fullIssue
				a.issuesMu.Unlock()
				a.updateDetailsView()
			}
		})
	}()
}

// toggleIssueExpanded toggles the expand/collapse state of a parent issue.
func (a *App) toggleIssueExpanded(issueID string) {
	// Check both sections for the issue
	var issue *linearapi.Issue
	var ok bool
	if issue, ok = a.myIDToIssue[issueID]; !ok {
		if issue, ok = a.otherIDToIssue[issueID]; !ok {
			logger.Debug("tui.app: issue not found for toggle issue_id=%s", issueID)
			return
		}
	}

	if issue == nil {
		return
	}

	// Only toggle if this issue has children
	if len(issue.Children) == 0 {
		return
	}

	wasExpanded := a.expandedState[issueID]
	logger.Debug("tui.app: toggling issue expanded issue=%s was_expanded=%v", issue.Identifier, wasExpanded)

	ToggleExpanded(a.expandedState, issueID)

	// Rebuild rows for both sections
	currentUserID := ""
	if a.currentUser != nil {
		currentUserID = a.currentUser.ID
	}
	a.issuesMu.RLock()
	issues := a.issues
	a.issuesMu.RUnlock()
	myIssues, otherIssues := splitIssuesByAssignee(issues, currentUserID)
	a.myIssueRows, a.myIDToIssue = BuildIssueRows(myIssues, a.expandedState)
	a.otherIssueRows, a.otherIDToIssue = BuildIssueRows(otherIssues, a.expandedState)

	// Legacy: keep old fields for backward compatibility
	a.issueRows = make([]IssueRow, 0, len(a.myIssueRows)+len(a.otherIssueRows))
	a.issueRows = append(a.issueRows, a.myIssueRows...)
	a.issueRows = append(a.issueRows, a.otherIssueRows...)
	a.idToIssue = make(map[string]*linearapi.Issue)
	for k, v := range a.myIDToIssue {
		a.idToIssue[k] = v
	}
	for k, v := range a.otherIDToIssue {
		a.idToIssue[k] = v
	}

	// Update layout
	a.updateIssuesColumnLayout()

	// Render both tables, selecting the toggled issue
	var selectedMyIssueID, selectedOtherIssueID string
	if _, ok := a.myIDToIssue[issueID]; ok {
		selectedMyIssueID = issueID
		a.activeIssuesSection = IssuesSectionMy
	} else if _, ok := a.otherIDToIssue[issueID]; ok {
		selectedOtherIssueID = issueID
		a.activeIssuesSection = IssuesSectionOther
	}

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme, &a.markedIssueSelection)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme, &a.markedIssueSelection)
}

// onNavigationSelected handles when a navigation item is selected.
func (a *App) onNavigationSelected(node *NavigationNode) {
	a.invalidateIssueEditorSaveContext()
	a.invalidateIssueMutationContexts()
	logger.Debug("tui.app: navigation selected node_id=%s node_text=%s is_team=%v is_project=%v is_cycle=%v is_issue=%v", node.ID, node.Text, node.IsTeam, node.IsProject, node.IsCycle, node.IsIssue)

	// A favorited issue is not a filter of its own: scope to its team and ask
	// the refresh to land on the issue via the target-issue plumbing.
	if node.IsIssue {
		a.selectedNavigation = &NavigationNode{
			ID:     node.TeamID,
			Text:   node.Text,
			TeamID: node.TeamID,
			IsTeam: true,
		}
		a.reapplyKeybindings()
		if node.TeamID != "" {
			go a.preloadTeamMetadataFunc(node.TeamID)
		}
		go a.refreshIssuesWithFocusChange(false, node.IssueID)
		return
	}

	a.selectedNavigation = node
	// Triage commands are contextual. Refresh the existing palette in place so
	// entering or leaving the Triage navigation cannot leave stale actions
	// enabled.
	a.reapplyKeybindings()

	// Update selected team metadata for commands and create-issue defaults.
	if node.TeamID != "" {
		go a.preloadTeamMetadataFunc(node.TeamID)
	}

	// Refresh issues for the new selection - run in goroutine to avoid blocking
	// the tview callback (QueueUpdateDraw deadlocks if called from within a callback)
	go a.refreshIssuesWithFocusChange(false)
}

// invalidateIssueMutationContexts makes issue-selection-bound mutations stale
// before a navigation or selection change can expose a different target set.
// The underlying API calls may still finish, but their progress/completions are
// intentionally discarded by the corresponding runners.
func (a *App) invalidateIssueMutationContexts() {
	if a == nil {
		return
	}
	if a.bulkIssueActionRunner != nil {
		a.bulkIssueActionRunner.Invalidate()
	}
	if a.triageActionRunner != nil {
		a.triageActionRunner.Invalidate()
	}
	a.clearPendingTriageBatches()
}

// preloadTeamMetadata warms team-scoped metadata caches for commands and create-issue defaults.
func (a *App) preloadTeamMetadata(teamID string) {
	logger.Debug("tui.app: preloading team metadata team_id=%s", teamID)
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return
	}
	generation := a.teamMetadataGeneration.Add(1)
	ctx := context.Background()
	_ = a.cache.PreloadTeamMetadata(ctx, teamID)

	users, _ := a.cache.GetUsers(ctx, teamID)
	projects, _ := a.cache.GetProjects(ctx, teamID)
	states, _ := a.cache.GetWorkflowStates(ctx, teamID)
	cycles, _ := a.cache.GetCycles(ctx, teamID)

	logger.Debug("tui.app: loaded team metadata team_id=%s users_count=%d projects_count=%d states_count=%d cycles_count=%d", teamID, len(users), len(projects), len(states), len(cycles))
	a.QueueUpdateDraw(func() {
		if generation != a.teamMetadataGeneration.Load() || a.GetSelectedTeamID() != teamID {
			return
		}
		a.publishIssueEditorTeamMetadata(teamID, generation, users, projects, states, cycles)
	})
}

// setSearchQuery sets the search query and refreshes issues.
func (a *App) setSearchQuery(query string) {
	a.cancelSearchDebounce()
	a.setSearchQueryWithFocusChange(query, true)
}

func (a *App) setSearchQueryWithFocusChange(query string, allowFocusChange bool) {
	trimmedQuery := strings.TrimSpace(query)
	logger.Debug("tui.app: setting search query query=%s", trimmedQuery)
	a.searchQuery = trimmedQuery
	// Set focus to issues pane when searching
	if allowFocusChange {
		a.focusedPane = FocusIssues
	}
	a.updateFocus()
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssuesWithFocusChange(allowFocusChange)
}

// setSortField sets the sort field and refreshes issues.
func (a *App) setSortField(field SortField) {
	logger.Debug("tui.app: setting sort field field=%s", field)
	a.sortField = field
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssues()
}

// updateStatusBar updates the status bar with current information.
func (a *App) updateStatusBar() {
	var helpText string
	keyColor := a.themeTags.SecondaryText
	paletteHint := fmt.Sprintf("%c: palette", a.actionKey("open_palette", ':'))

	switch a.focusedPane {
	case FocusNavigation:
		helpText = fmt.Sprintf("%s↑↓: navigate | Enter: select | Tab/→/l: next pane | Shift+Tab/←/h: prev pane | %s | Ctrl-K: palette | ?: help | /: search | q: quit[-]", keyColor, paletteHint)
	case FocusIssues:
		helpText = fmt.Sprintf("%sj/k: navigate | Enter: select | Tab/→/l: next pane | Shift+Tab/←/h: prev pane | %s | Ctrl-K: palette | ?: help | /: search | q: quit[-]", keyColor, paletteHint)
	case FocusDetails:
		helpText = fmt.Sprintf("%sj/k: scroll | Tab: switch description/comments | →/l: next pane | Shift+Tab/←/h: prev pane | %s | Ctrl-K: palette | ?: help | /: search | q: quit[-]", keyColor, paletteHint)
	case FocusPalette:
		helpText = fmt.Sprintf("%s↑↓: navigate | Enter: execute | Esc: close[-]", keyColor)
	default:
		helpText = fmt.Sprintf("%sj/k: navigate | Tab: next pane | Shift+Tab: prev pane | %s | /: search | q: quit[-]", keyColor, paletteHint)
	}

	navText := ""
	if a.selectedNavigation != nil {
		label := a.selectedNavigation.Text
		if a.selectedNavigation.IsStatus {
			if a.selectedNavigation.StateName != "" {
				label = fmt.Sprintf("Status: %s", a.selectedNavigation.StateName)
			} else {
				label = "Status"
			}
		} else if a.selectedNavigation.IsCycle {
			if a.selectedNavigation.CycleName != "" {
				label = fmt.Sprintf("Cycle: %s", a.selectedNavigation.CycleName)
			} else {
				label = "Cycle"
			}
		}
		navText = fmt.Sprintf("%s%s[-]", a.themeTags.Accent, label)
	}

	searchText := ""
	if a.searchQuery != "" {
		searchText = fmt.Sprintf("%s🔍 %s[-]", a.themeTags.Warning, a.searchQuery)
	}
	filterText := ""
	if !a.richFilters.Empty() {
		filterText = fmt.Sprintf("%sFilters: %s[-]", a.themeTags.Warning, a.richFilters.Summary())
	}

	a.issuesMu.RLock()
	issuesLen := len(a.issues)
	a.issuesMu.RUnlock()
	statusText := fmt.Sprintf("%s%d issues[-]", a.themeTags.Accent, issuesLen)
	if issuesLen == 0 {
		statusText = fmt.Sprintf("%sNo issues[-]", a.themeTags.SecondaryText)
	}

	sep := fmt.Sprintf("%s | [-]", a.themeTags.Border)

	parts := make([]string, 0, 9)
	if a.statusMessage != "" {
		parts = append(parts, fmt.Sprintf("%s%s[-]", a.themeTags.Accent, a.statusMessage))
	}
	if a.keySequenceHint != "" {
		parts = append(parts, fmt.Sprintf("%s%s[-]", a.themeTags.Warning, a.keySequenceHint))
	}
	if a.keySequenceConfigError != nil {
		parts = append(parts, fmt.Sprintf("%sKey sequence disabled: %v[-]", a.themeTags.Error, a.keySequenceConfigError))
	}
	if marked := a.markedIssueSelection.Count(); marked > 0 {
		parts = append(parts, fmt.Sprintf("%s%d marked[-]", a.themeTags.Accent, marked))
	}
	if a.startupNotice != "" {
		parts = append(parts, fmt.Sprintf("%s%s[-]", a.themeTags.Accent, a.startupNotice))
	}
	if navText != "" {
		parts = append(parts, navText)
	}
	if searchText != "" {
		parts = append(parts, searchText)
	}
	if filterText != "" {
		parts = append(parts, filterText)
	}
	parts = append(parts, statusText)
	parts = append(parts, helpText)

	text := parts[0]
	for i := 1; i < len(parts); i++ {
		text += sep + parts[i]
	}

	a.statusBar.SetText(text)
}

// updateStatusBarWithError updates the status bar with an error message.
func (a *App) updateStatusBarWithError(err error) {
	a.statusBar.SetText(fmt.Sprintf("%sError: %v[-]", a.themeTags.Error, err))
}

func (a *App) flashStatus(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	a.statusMessage = message
	a.statusBar.SetText(fmt.Sprintf("%s%s[-]", a.themeTags.Accent, message))
}

// GetAPI returns the Linear API client (used by commands).
func (a *App) GetAPI() *linearapi.Client {
	return a.api
}

// GetCache returns the team cache (used by commands).
func (a *App) GetCache() *cache.TeamCache {
	return a.cache
}

// GetSelectedIssue returns the currently selected issue.
func (a *App) GetSelectedIssue() *linearapi.Issue {
	a.issuesMu.RLock()
	defer a.issuesMu.RUnlock()
	return a.selectedIssue
}

// GetSelectedTeamID returns the currently selected team ID, if any.
func (a *App) GetSelectedTeamID() string {
	if a.selectedNavigation != nil && a.selectedNavigation.TeamID != "" {
		return a.selectedNavigation.TeamID
	}
	// If we have a selected issue, use its team
	a.issuesMu.RLock()
	selectedIssue := a.selectedIssue
	a.issuesMu.RUnlock()
	if selectedIssue != nil {
		return selectedIssue.TeamID
	}
	return ""
}

// GetCurrentUser returns the current authenticated user.
func (a *App) GetCurrentUser() *linearapi.User {
	return a.currentUser
}

// GetTeamUsers returns the users for the currently selected team.
func (a *App) GetTeamUsers() []linearapi.User {
	return a.teamUsers
}

// FetchTeamUsers fetches users for a specific team from the API.
func (a *App) FetchTeamUsers(teamID string) ([]linearapi.User, error) {
	ctx := context.Background()
	users, err := a.cache.GetUsers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	a.teamUsers = users
	return users, nil
}

// GetTeamProjects returns the projects for the currently selected team.
func (a *App) GetTeamProjects() []linearapi.Project {
	return a.teamProjects
}

// FetchTeamProjects fetches projects for a specific team from the API.
func (a *App) FetchTeamProjects(teamID string) ([]linearapi.Project, error) {
	ctx := context.Background()
	projects, err := a.cache.GetProjects(ctx, teamID)
	if err != nil {
		return nil, err
	}
	a.teamProjects = projects
	return projects, nil
}

// GetTeamCycles returns the cycles for the currently selected team.
func (a *App) GetTeamCycles() []linearapi.Cycle {
	return a.teamCycles
}

// FetchTeamCycles fetches cycles for a specific team from the API.
func (a *App) FetchTeamCycles(teamID string) ([]linearapi.Cycle, error) {
	ctx := context.Background()
	cycles, err := a.cache.GetCycles(ctx, teamID)
	if err != nil {
		return nil, err
	}
	sortCyclesForNavigation(cycles)
	a.teamCycles = cycles
	return cycles, nil
}

// GetWorkflowStates returns the workflow states for the currently selected team.
func (a *App) GetWorkflowStates() []linearapi.WorkflowState {
	return a.workflowStates
}

// QueueUpdateDraw queues a UI update function to be run in the main thread.
func (a *App) QueueUpdateDraw(f func()) {
	if a.queueUpdateDraw != nil {
		// Serialize UI updates when test overrides queueUpdateDraw to execute immediately
		a.uiUpdateMu.Lock()
		defer a.uiUpdateMu.Unlock()
		a.queueUpdateDraw(f)
		return
	}
	a.app.QueueUpdateDraw(f)
}

// loadPickerData loads picker data asynchronously if not already cached.
func (a *App) loadPickerData(
	resourceName string,
	hasData func() bool,
	loadData func(ctx context.Context, teamID string) error,
	onLoaded func(),
) {
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		logger.Warning("tui.app: cannot show %s picker, no team selected", resourceName)
		return
	}
	go func() {
		logger.Debug("tui.app: loading %s team_id=%s", resourceName, teamID)
		ctx := context.Background()
		if err := loadData(ctx, teamID); err != nil {
			logger.ErrorWithErr(err, "tui.app: failed to load %s team_id=%s", resourceName, teamID)
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}
		logger.Debug("tui.app: loaded %s team_id=%s", resourceName, teamID)
		a.QueueUpdateDraw(onLoaded)
	}()
}

// ShowStatusPicker shows a picker for workflow states.
func (a *App) ShowStatusPicker(onSelect func(stateID string)) {
	logger.Debug("tui.app: showing status picker")
	states := a.workflowStates
	if len(states) == 0 {
		a.loadPickerData(
			"workflow states",
			func() bool { return len(a.workflowStates) > 0 },
			func(ctx context.Context, teamID string) error {
				loadedStates, err := a.cache.GetWorkflowStates(ctx, teamID)
				if err != nil {
					return err
				}
				a.workflowStates = loadedStates
				return nil
			},
			func() {
				a.showStatusPickerWithStates(a.workflowStates, onSelect)
			},
		)
		return
	}
	a.showStatusPickerWithStates(states, onSelect)
}

func (a *App) showStatusPickerWithStates(states []linearapi.WorkflowState, onSelect func(stateID string)) {
	items := make([]PickerItem, 0, len(states))
	for _, state := range states {
		items = append(items, PickerItem{
			ID:    state.ID,
			Label: state.Name,
		})
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Status", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowUserPicker shows a picker for team users.
func (a *App) ShowUserPicker(onSelect func(userID string)) {
	logger.Debug("tui.app: showing user picker")
	users := a.teamUsers
	if len(users) == 0 {
		a.loadPickerData(
			"users for picker",
			func() bool { return len(a.teamUsers) > 0 },
			func(ctx context.Context, teamID string) error {
				loadedUsers, err := a.cache.GetUsers(ctx, teamID)
				if err != nil {
					return err
				}
				a.teamUsers = loadedUsers
				return nil
			},
			func() {
				a.showUserPickerWithUsers(a.teamUsers, onSelect)
			},
		)
		return
	}
	a.showUserPickerWithUsers(users, onSelect)
}

func (a *App) showUserPickerWithUsers(users []linearapi.User, onSelect func(userID string)) {
	items := make([]PickerItem, 0, len(users))
	for _, user := range users {
		label := user.Name
		if user.IsMe {
			label += " (me)"
		}
		items = append(items, PickerItem{
			ID:    user.ID,
			Label: label,
		})
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Assignee", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowCyclePicker shows a picker for team cycles.
func (a *App) ShowCyclePicker(onSelect func(cycleID string)) {
	logger.Debug("tui.app: showing cycle picker")
	cycles := a.teamCycles
	if len(cycles) == 0 {
		a.loadPickerData(
			"cycles for picker",
			func() bool { return len(a.teamCycles) > 0 },
			func(ctx context.Context, teamID string) error {
				loadedCycles, err := a.cache.GetCycles(ctx, teamID)
				if err != nil {
					return err
				}
				sortCyclesForNavigation(loadedCycles)
				a.teamCycles = loadedCycles
				return nil
			},
			func() {
				a.showCyclePickerWithCycles(a.teamCycles, onSelect)
			},
		)
		return
	}
	a.showCyclePickerWithCycles(cycles, onSelect)
}

func (a *App) showCyclePickerWithCycles(cycles []linearapi.Cycle, onSelect func(cycleID string)) {
	items := make([]PickerItem, 0, len(cycles))
	for _, cycle := range cycles {
		label := cycle.DisplayName()
		switch {
		case cycle.IsActive:
			label += " (active)"
		case cycle.IsNext:
			label += " (next)"
		case cycle.IsPrevious:
			label += " (previous)"
		}
		items = append(items, PickerItem{
			ID:    cycle.ID,
			Label: label,
		})
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Cycle", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowParentIssuePicker shows a picker for selecting a parent issue.
// It lists all top-level issues (issues without a parent) from the current list.
func (a *App) ShowParentIssuePicker(onSelect func(parentID string)) {
	// Filter to only show issues that could be parents (no parent themselves)
	a.issuesMu.RLock()
	issues := a.issues
	selectedIssue := a.selectedIssue
	a.issuesMu.RUnlock()
	excludedIDs := excludedParentCandidateIDs(selectedIssue, issues)
	items := make([]PickerItem, 0)
	for _, issue := range issues {
		if issue.Parent == nil && !excludedIDs[issue.ID] {
			items = append(items, PickerItem{
				ID:    issue.ID,
				Label: issue.Identifier + " - " + issue.Title,
			})
		}
	}

	if len(items) == 0 {
		logger.Warning("tui.app: no parent issues available for picker")
		a.updateStatusBarWithError(fmt.Errorf("no parent issues available"))
		return
	}
	logger.Debug("tui.app: parent issue picker items count=%d", len(items))

	a.pickerActive = true
	a.pickerModal.Show("Select Parent Issue", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

func excludedParentCandidateIDs(selected *linearapi.Issue, issues []linearapi.Issue) map[string]bool {
	excluded := make(map[string]bool)
	if selected == nil {
		return excluded
	}
	excluded[selected.ID] = true
	byID := make(map[string]linearapi.Issue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	var visit func(issue linearapi.Issue)
	visit = func(issue linearapi.Issue) {
		for _, child := range issue.Children {
			if excluded[child.ID] {
				continue
			}
			excluded[child.ID] = true
			if fullChild, ok := byID[child.ID]; ok {
				visit(fullChild)
			}
		}
	}
	visit(*selected)
	return excluded
}

// ShowCreateIssueModal shows the create issue modal.
func (a *App) ShowCreateIssueModal() {
	a.showCreateIssueModalWithParent("", nil)
}

// ShowCreateSubIssueModal shows the create issue modal with a parent issue pre-set.
func (a *App) ShowCreateSubIssueModal(parentID string) {
	a.showCreateIssueModalWithParent(parentID, a.issueRefForID(parentID))
}

// showCreateIssueModalWithParent shows the create issue modal, optionally with a parent.
func (a *App) showCreateIssueModalWithParent(parentID string, parentRef *linearapi.IssueRef) {
	teamID := a.GetSelectedTeamID()
	projectID := ""
	if a.selectedNavigation != nil && a.selectedNavigation.IsProject {
		projectID = a.selectedNavigation.ID
	}
	cycleID := ""
	if a.selectedNavigation != nil && a.selectedNavigation.IsCycle {
		cycleID = a.selectedNavigation.CycleID
	}

	a.createIssueModal.ShowWithOptions(CreateIssueModalOptions{
		TeamID:    teamID,
		ProjectID: projectID,
		Parent:    parentRef,
		CycleID:   cycleID,
	}, func(title, description, tID, pID, assigneeID, cID string, priority int) {
		if title == "" {
			return
		}
		go func() {
			ctx := context.Background()
			input := linearapi.CreateIssueInput{
				TeamID:      tID,
				Title:       title,
				Description: description,
			}
			if pID != "" {
				input.ProjectID = pID
			}
			if assigneeID != "" {
				input.AssigneeID = assigneeID
			}
			if cID != "" {
				input.CycleID = cID
			}
			if priority > 0 {
				input.Priority = priority
			}
			if parentID != "" {
				input.ParentID = parentID
			}
			issue, err := a.api.CreateIssue(ctx, input)
			a.QueueUpdateDraw(func() {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to create issue title=%s", title)
					a.updateStatusBarWithError(err)
					return
				}
				if parentID != "" {
					logger.Info("tui.app: created sub-issue issue=%s title=%s", issue.Identifier, title)
					a.flashStatus(fmt.Sprintf("Created sub-issue %s", issue.Identifier))
				} else {
					logger.Info("tui.app: created issue issue=%s title=%s", issue.Identifier, title)
					a.flashStatus(fmt.Sprintf("Created issue %s", issue.Identifier))
				}
				go a.refreshIssues(issue.ID)
			})
		}()
	})
}

func (a *App) issueRefForID(issueID string) *linearapi.IssueRef {
	if issueID == "" {
		return nil
	}
	a.issuesMu.RLock()
	defer a.issuesMu.RUnlock()
	if a.selectedIssue != nil && a.selectedIssue.ID == issueID {
		return &linearapi.IssueRef{ID: a.selectedIssue.ID, Identifier: a.selectedIssue.Identifier, Title: a.selectedIssue.Title}
	}
	for _, issue := range a.issues {
		if issue.ID == issueID {
			return &linearapi.IssueRef{ID: issue.ID, Identifier: issue.Identifier, Title: issue.Title}
		}
	}
	return nil
}

// ShowEditTitleModal shows the edit title modal.
func (a *App) ShowEditTitleModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		return
	}

	a.editTitleModal.Show(issue.ID, issue.Title, func(issueID, title string) {
		go func() {
			ctx := context.Background()
			_, err := a.api.UpdateIssue(ctx, linearapi.UpdateIssueInput{
				ID:    issueID,
				Title: &title,
			})
			a.QueueUpdateDraw(func() {
				if err != nil {
					logger.ErrorWithErr(err, "tui.app: failed to update issue title issue=%s", issue.Identifier)
					a.updateStatusBarWithError(err)
					return
				}
				logger.Info("tui.app: updated issue title issue=%s", issue.Identifier)
				a.flashStatus(fmt.Sprintf("Updated title for %s", issue.Identifier))
				go a.refreshIssues(issueID)
			})
		}()
	})
}

// ShowIssueEditorModal loads the metadata required by the complete issue
// editor and presents it only if the originally selected issue is still
// selected when loading finishes.
func (a *App) ShowIssueEditorModal() {
	a.invalidateIssueEditorSaveContext()
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}

	issueSnapshot := *issue
	issueID := issueSnapshot.ID
	teamID := issueSnapshot.TeamID
	if teamID == "" {
		teamID = a.GetSelectedTeamID()
	}
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("cannot edit issue: no team context"))
		return
	}
	requestGeneration := a.issueEditorGeneration.Load()
	metadataGeneration := a.teamMetadataGeneration.Load()

	// Use already loaded metadata immediately and fetch only missing team
	// resources. Labels are fetched for every editor invocation so the picker
	// reflects the current workspace/team label set.
	metadata := a.issueEditorTeamMetadataFor(teamID)
	statuses := metadata.workflowStates
	users := metadata.users
	projects := metadata.projects
	cycles := metadata.cycles

	fetchUsers := a.fetchUsersFunc
	if fetchUsers == nil && a.cache != nil {
		fetchUsers = a.cache.GetUsers
	}
	fetchProjects := a.fetchProjectsFunc
	if fetchProjects == nil && a.cache != nil {
		fetchProjects = a.cache.GetProjects
	}
	fetchStates := a.fetchWorkflowStatesFunc
	if fetchStates == nil && a.cache != nil {
		fetchStates = a.cache.GetWorkflowStates
	}
	fetchCycles := a.fetchCyclesFunc
	if fetchCycles == nil && a.cache != nil {
		fetchCycles = a.cache.GetCycles
	}
	fetchLabels := a.fetchIssueLabelsFunc
	if fetchLabels == nil && a.cache != nil {
		fetchLabels = a.cache.GetIssueLabels
	}

	go func() {
		ctx := context.Background()
		var wg sync.WaitGroup
		var fetchErrMu sync.Mutex
		var fetchErr error
		setFetchErr := func(err error) {
			if err == nil {
				return
			}
			fetchErrMu.Lock()
			if fetchErr == nil {
				fetchErr = err
			}
			fetchErrMu.Unlock()
		}

		if len(users) == 0 && fetchUsers != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaded, err := fetchUsers(ctx, teamID)
				if err != nil {
					setFetchErr(err)
					return
				}
				users = loaded
			}()
		}
		if len(projects) == 0 && fetchProjects != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaded, err := fetchProjects(ctx, teamID)
				if err != nil {
					setFetchErr(err)
					return
				}
				projects = loaded
			}()
		}
		if len(statuses) == 0 && fetchStates != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaded, err := fetchStates(ctx, teamID)
				if err != nil {
					setFetchErr(err)
					return
				}
				statuses = loaded
			}()
		}
		if len(cycles) == 0 && fetchCycles != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaded, err := fetchCycles(ctx, teamID)
				if err != nil {
					setFetchErr(err)
					return
				}
				cycles = loaded
			}()
		}

		var labels []linearapi.IssueLabel
		if fetchLabels != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				loaded, err := fetchLabels(ctx, teamID)
				if err != nil {
					setFetchErr(err)
					return
				}
				labels = loaded
			}()
		}
		wg.Wait()

		fetchErrMu.Lock()
		err := fetchErr
		fetchErrMu.Unlock()
		a.QueueUpdateDraw(func() {
			if requestGeneration != a.issueEditorGeneration.Load() || metadataGeneration != a.teamMetadataGeneration.Load() {
				logger.Debug("tui.app: dropping stale issue editor metadata request issue=%s team_id=%s", issueID, teamID)
				return
			}
			if err != nil {
				logger.ErrorWithErr(err, "tui.app: failed to load issue editor metadata issue=%s team_id=%s", issueSnapshot.Identifier, teamID)
				a.updateStatusBarWithError(err)
				return
			}
			current := a.GetSelectedIssue()
			currentTeamID := ""
			if current != nil {
				currentTeamID = strings.TrimSpace(current.TeamID)
				if currentTeamID == "" {
					currentTeamID = a.GetSelectedTeamID()
				}
			}
			if current == nil || current.ID != issueID || currentTeamID != teamID || a.GetSelectedTeamID() != teamID {
				logger.Debug("tui.app: dropping stale issue editor metadata issue=%s team_id=%s", issueID, teamID)
				return
			}

			// Publish fetched metadata for the existing picker/modal conventions.
			if !a.publishIssueEditorTeamMetadata(teamID, metadataGeneration, users, projects, statuses, cycles) {
				return
			}

			if a.issueEditorModal == nil {
				a.issueEditorModal = NewIssueEditorModal(a)
			}
			options := buildIssueEditorOptions(issueSnapshot, statuses, users, projects, cycles, labels)
			a.issueEditorModal.Show(options, func(values IssueEditorValues) {
				title := values.Title
				description := values.Description
				stateID := values.StateID
				assigneeID := values.AssigneeID
				projectID := values.ProjectID
				cycleID := values.CycleID
				priority := values.Priority
				labelIDs := make([]string, len(values.LabelIDs))
				copy(labelIDs, values.LabelIDs)
				input := linearapi.UpdateIssueInput{
					ID:          issueID,
					Title:       &title,
					Description: &description,
					StateID:     &stateID,
					AssigneeID:  &assigneeID,
					Priority:    &priority,
					ProjectID:   &projectID,
					CycleID:     &cycleID,
					LabelIDs:    &labelIDs,
				}
				updateIssue := a.updateIssueFunc
				if updateIssue == nil && a.api != nil {
					updateIssue = a.api.UpdateIssue
				}
				snapshot := issueEditorSaveSnapshot{
					issueID:             issueID,
					navigationKey:       navigationMutationKey(a.selectedNavigation),
					selectionGeneration: a.issueSelectionGeneration.Load(),
					editorGeneration:    requestGeneration,
				}
				if updateIssue == nil {
					if a.issueEditorSaveContextCurrent(snapshot) {
						a.issueEditorModal.setSaving(false)
						a.updateStatusBarWithError(fmt.Errorf("cannot update issue: API unavailable"))
					}
					return
				}
				a.runIssueEditorSave(snapshot, issueSnapshot.Identifier, input, updateIssue)
			})
			a.issueEditorModal.setKeepOpenOnSave(true)
		})
	}()
}

func buildIssueEditorOptions(issue linearapi.Issue, statuses []linearapi.WorkflowState, users []linearapi.User, projects []linearapi.Project, cycles []linearapi.Cycle, labels []linearapi.IssueLabel) IssueEditorOptions {
	stateOptions := make([]IssueEditorOption, 0, len(statuses)+1)
	for _, state := range statuses {
		stateOptions = append(stateOptions, IssueEditorOption{ID: state.ID, Label: firstNonEmpty(state.Name, state.ID)})
	}
	appendIssueEditorOption(&stateOptions, issue.StateID, issue.State)

	assigneeOptions := make([]IssueEditorOption, 0, len(users)+1)
	for _, user := range users {
		assigneeOptions = append(assigneeOptions, IssueEditorOption{ID: user.ID, Label: formatUserDisplayName(user)})
	}
	appendIssueEditorOption(&assigneeOptions, issue.AssigneeID, issue.Assignee)

	projectOptions := make([]IssueEditorOption, 0, len(projects)+1)
	for _, project := range projects {
		projectOptions = append(projectOptions, IssueEditorOption{ID: project.ID, Label: firstNonEmpty(project.Name, project.ID)})
	}
	appendIssueEditorOption(&projectOptions, issue.ProjectID, issue.ProjectID)

	cycleOptions := make([]IssueEditorOption, 0, len(cycles)+1)
	for _, cycle := range cycles {
		cycleOptions = append(cycleOptions, IssueEditorOption{ID: cycle.ID, Label: cycle.DisplayName()})
	}
	currentCycleID := ""
	currentCycleLabel := ""
	if issue.Cycle != nil {
		currentCycleID = issue.Cycle.ID
		currentCycleLabel = issue.Cycle.DisplayName()
	}
	appendIssueEditorOption(&cycleOptions, currentCycleID, currentCycleLabel)

	labelOptions := make([]IssueEditorOption, 0, len(labels)+len(issue.Labels))
	for _, label := range labels {
		labelOptions = append(labelOptions, IssueEditorOption{ID: label.ID, Label: firstNonEmpty(label.Name, label.ID)})
	}
	for _, label := range issue.Labels {
		appendIssueEditorOption(&labelOptions, label.ID, label.Name)
	}
	labelIDs := make([]string, len(issue.Labels))
	for i, label := range issue.Labels {
		labelIDs[i] = label.ID
	}

	return IssueEditorOptions{
		Values: IssueEditorValues{
			IssueID:     issue.ID,
			Title:       issue.Title,
			Description: issue.Description,
			StateID:     issue.StateID,
			AssigneeID:  issue.AssigneeID,
			Priority:    issue.Priority,
			ProjectID:   issue.ProjectID,
			CycleID:     currentCycleID,
			LabelIDs:    labelIDs,
		},
		Statuses:  stateOptions,
		Assignees: assigneeOptions,
		Projects:  projectOptions,
		Cycles:    cycleOptions,
		Labels:    labelOptions,
	}
}

func appendIssueEditorOption(options *[]IssueEditorOption, id, label string) {
	if options == nil || id == "" {
		return
	}
	for _, option := range *options {
		if option.ID == id {
			return
		}
	}
	*options = append(*options, IssueEditorOption{ID: id, Label: firstNonEmpty(label, id)})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ShowEditLabelsModal shows the edit labels modal for the selected issue.
func (a *App) ShowEditLabelsModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		return
	}

	teamID := issue.TeamID
	if teamID == "" {
		teamID = a.GetSelectedTeamID()
	}
	if teamID == "" {
		logger.Warning("tui.app: cannot edit labels, no team context issue=%s", issue.Identifier)
		a.updateStatusBarWithError(fmt.Errorf("cannot edit labels: no team context"))
		return
	}

	// Get current label IDs from the issue
	currentLabelIDs := make([]string, len(issue.Labels))
	for i, lbl := range issue.Labels {
		currentLabelIDs[i] = lbl.ID
	}

	// Load available labels asynchronously
	go func() {
		logger.Debug("tui.app: loading labels for edit modal issue=%s team_id=%s", issue.Identifier, teamID)
		ctx := context.Background()
		availableLabels, err := a.cache.GetIssueLabels(ctx, teamID)
		if err != nil {
			logger.ErrorWithErr(err, "tui.app: failed to load labels issue=%s team_id=%s", issue.Identifier, teamID)
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}
		logger.Debug("tui.app: loaded labels issue=%s count=%d", issue.Identifier, len(availableLabels))

		a.QueueUpdateDraw(func() {
			a.editLabelsModal.Show(issue.ID, currentLabelIDs, availableLabels, func(issueID string, labelIDs []string) {
				go func() {
					ctx := context.Background()
					_, err := a.api.UpdateIssue(ctx, linearapi.UpdateIssueInput{
						ID:       issueID,
						LabelIDs: &labelIDs,
					})
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "tui.app: failed to update labels issue=%s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("tui.app: updated labels issue=%s", issue.Identifier)
						a.flashStatus(fmt.Sprintf("Updated labels for %s", issue.Identifier))
						go a.refreshIssues(issueID)
					})
				}()
			})
		})
	}()
}

// ShowSettingsModal shows the settings modal.
func (a *App) ShowSettingsModal() {
	if a.settingsModal == nil {
		return
	}

	a.settingsModal.Show()
}

// ShowPromptTemplatesModal shows the prompt templates modal.
func (a *App) ShowPromptTemplatesModal() {
	if a.promptTemplatesModal == nil {
		return
	}

	promptsPath, err := config.PromptTemplatesFilePath()
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}

	templates, err := config.EnsurePromptTemplatesFile(promptsPath)
	if err != nil {
		a.updateStatusBarWithError(err)
		templates = a.agentPromptTemplates
		if len(templates) == 0 {
			templates = config.DefaultAgentPromptTemplates()
		}
	} else {
		a.agentPromptTemplates = templates
	}

	a.promptTemplatesModal.Show(templates, func(updated []config.AgentPromptTemplate) error {
		if err := config.SavePromptTemplates(promptsPath, updated); err != nil {
			return err
		}
		a.agentPromptTemplates = updated
		a.agentPromptModal = NewAgentPromptModal(a)
		return nil
	})
}
