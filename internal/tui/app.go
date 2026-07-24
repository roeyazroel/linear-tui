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
	editLabelsModal        *EditLabelsModal
	textInputModal         *TextInputModal
	multiSelectModal       *MultiSelectModal
	settingsModal          *SettingsModal
	promptTemplatesModal   *AgentPromptTemplatesModal
	agentPromptModal       *AgentPromptModal
	agentOutputModal       *AgentOutputModal
	confirmationModal      *ConfirmationModal
	agentRunner            *agents.Runner
	agentPromptTemplates   []config.AgentPromptTemplate

	// App state (protected by issuesMu)
	issuesMu            sync.RWMutex
	selectedIssue       *linearapi.Issue
	selectedNavigation  *NavigationNode
	issues              []linearapi.Issue
	focusedPane         FocusTarget
	activeIssuesSection IssuesSection // Tracks which issues section (My/Other) is currently active

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

	searchDebounceTimer      *time.Timer
	searchDebounceMu         sync.Mutex
	searchDebounceGeneration atomic.Int64

	// Cached metadata for currently selected team
	currentUser    *linearapi.User
	teamUsers      []linearapi.User
	teamProjects   []linearapi.Project
	workflowStates []linearapi.WorkflowState
	teamCycles     []linearapi.Cycle

	// Loading state
	isLoading                      bool
	pendingRefresh                 bool
	pendingRefreshIssueID          string
	pendingRefreshAllowFocusChange bool
	pickerActive                   bool
	refreshGeneration              atomic.Int64

	// Lazy loading helpers (overridable in tests)
	fetchIssuesPage         func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error)
	fetchIssueByID          func(context.Context, string) (linearapi.Issue, error)
	queueUpdateDraw         func(func())
	updateIssueFunc         func(context.Context, linearapi.UpdateIssueInput) (linearapi.Issue, error)
	createIssueRelationFunc func(context.Context, linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error)
	deleteIssueRelationFunc func(context.Context, string) error
	subscribeIssueFunc      func(context.Context, string) (linearapi.Issue, error)
	unsubscribeIssueFunc    func(context.Context, string) (linearapi.Issue, error)
	openURLFunc             func(string) error
	copyToClipboardFunc     func(string) error
	refreshCompleted        func()
	fetchProjectsFunc       func(context.Context, string) ([]linearapi.Project, error)
	fetchWorkflowStatesFunc func(context.Context, string) ([]linearapi.WorkflowState, error)
	fetchCyclesFunc         func(context.Context, string) ([]linearapi.Cycle, error)
	preloadTeamMetadataFunc func(string)

	// UI update mutex (for test safety when queueUpdateDraw executes immediately)
	uiUpdateMu sync.Mutex

	// Race-safety for issue detail fetching
	fetchingIssueID string // Tracks which issue ID we're currently fetching

	// Details pane sub-view focus
	focusedDetailsView     bool // false = description, true = comments
	detailsCommentsVisible bool // Tracks whether comments view is shown
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
		app:                  tview.NewApplication(),
		api:                  api,
		cache:                cache.NewTeamCache(api, cfg.CacheTTL),
		config:               cfg,
		theme:                theme,
		themeTags:            NewThemeTags(theme),
		density:              density,
		pages:                tview.NewPages(),
		focusedPane:          FocusNavigation,
		sortField:            SortByUpdatedAt,
		expandedState:        make(map[string]bool),
		idToIssue:            make(map[string]*linearapi.Issue),
		myIDToIssue:          make(map[string]*linearapi.Issue),
		otherIDToIssue:       make(map[string]*linearapi.Issue),
		activeIssuesSection:  IssuesSectionOther, // Default to Other section
		agentPromptTemplates: templates,
	}

	app.paletteCtrl = NewPaletteController(DefaultCommands(app))
	app.fetchIssuesPage = api.FetchIssuesPage
	app.fetchIssueByID = api.FetchIssueByID
	app.updateIssueFunc = api.UpdateIssue
	app.createIssueRelationFunc = api.CreateIssueRelation
	app.deleteIssueRelationFunc = api.DeleteIssueRelation
	app.subscribeIssueFunc = api.SubscribeToIssue
	app.unsubscribeIssueFunc = api.UnsubscribeFromIssue
	app.openURLFunc = openURL
	app.copyToClipboardFunc = copyToClipboard
	app.fetchProjectsFunc = app.cache.GetProjects
	app.fetchWorkflowStatesFunc = app.cache.GetWorkflowStates
	app.fetchCyclesFunc = app.cache.GetCycles
	app.preloadTeamMetadataFunc = app.preloadTeamMetadata
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

	// Load initial data asynchronously
	a.loadInitialData()

	// Start the application event loop
	return a.app.Run()
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
	a.config = newCfg
	a.applyThemeAndDensity()

	logLevel := parseLogLevel(newCfg.LogLevel)
	if err := logger.Reinit(newCfg.LogFile, logLevel); err != nil {
		logger.ErrorWithErr(err, "tui.app: failed to reinitialize logger")
		a.QueueUpdateDraw(func() {
			a.updateStatusBarWithError(err)
		})
		return
	}
	logger.Debug("tui.app: settings applied log_file=%s log_level=%s", newCfg.LogFile, newCfg.LogLevel)

	a.api = linearapi.NewClient(linearapi.ClientConfig{
		Token:    newCfg.LinearAPIKey,
		Endpoint: newCfg.APIEndpoint,
		Timeout:  newCfg.Timeout,
	})
	a.cache = cache.NewTeamCache(a.api, newCfg.CacheTTL)
	a.fetchIssuesPage = a.api.FetchIssuesPage
	a.fetchIssueByID = a.api.FetchIssueByID
	a.updateIssueFunc = a.api.UpdateIssue
	a.createIssueRelationFunc = a.api.CreateIssueRelation
	a.deleteIssueRelationFunc = a.api.DeleteIssueRelation
	a.subscribeIssueFunc = a.api.SubscribeToIssue
	a.unsubscribeIssueFunc = a.api.UnsubscribeFromIssue
	a.fetchProjectsFunc = a.cache.GetProjects
	a.fetchWorkflowStatesFunc = a.cache.GetWorkflowStates
	a.fetchCyclesFunc = a.cache.GetCycles

	logger.Debug("tui.app: resetting cached state after settings change")
	a.resetCachedState()
	a.loadInitialData()
}

func (a *App) applyThemeAndDensity() {
	a.theme = ResolveTheme(a.config.Theme)
	a.themeTags = NewThemeTags(a.theme)
	a.density = ResolveDensity(a.config.Density)

	a.applyThemeStyles()
	a.applyThemeToComponents()
	a.applyDensityToComponents()
	a.rebuildModals()
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
		renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, a.selectedIssueID(IssuesSectionMy), a.theme)
	}
	if a.otherIssuesTable != nil {
		a.applyIssuesTableTheme(a.otherIssuesTable)
		renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, a.selectedIssueID(IssuesSectionOther), a.theme)
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
	if a.pages != nil {
		a.pages.RemovePage("palette")
	}
	a.paletteModal = a.buildPaletteModal()
	if a.pages != nil {
		a.pages.AddPage("palette", a.paletteModal, true, false)
	}

	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
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

// resetCachedState clears cached user and issue data after config changes.
func (a *App) resetCachedState() {
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
	a.teamUsers = nil
	a.teamProjects = nil
	a.workflowStates = nil
	a.teamCycles = nil
	a.richFilters = IssueFilters{}
	a.searchQuery = ""
	a.cancelSearchDebounce()
	a.activeIssuesSection = IssuesSectionOther
	a.expandedState = make(map[string]bool)

	a.isLoading = false
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	a.pendingRefreshAllowFocusChange = true
	// Bump generation to prevent in-flight refreshes from updating UI.
	a.refreshGeneration.Add(1)
	a.fetchingIssueID = ""
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
	teams, err := a.cache.GetTeams(ctx)
	if err != nil {
		logger.ErrorWithErr(err, "tui.app: failed to load teams")
		a.app.QueueUpdateDraw(func() {
			a.updateStatusBarWithError(err)
		})
		return false
	}

	logger.Debug("tui.app: loaded teams count=%d", len(teams))
	a.app.QueueUpdateDraw(func() {
		a.rebuildNavigationTree(teams)
	})
	return a.applyDefaultNavigation(ctx, teams)
}

// rebuildNavigationTree rebuilds the navigation tree with real data.
func (a *App) rebuildNavigationTree(teams []linearapi.Team) {
	root := tview.NewTreeNode("Linear").
		SetColor(a.theme.Accent).
		SetSelectable(false)

	// Add "All Issues" at the top
	allIssues := tview.NewTreeNode("All Issues").
		SetColor(a.theme.Foreground).
		SetReference(&NavigationNode{ID: "all", Text: "All Issues"}).
		SetExpanded(true)
	root.AddChild(allIssues)

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

	// Create horizontal split: navigation (20%) | issues (50%) | details (30%)
	contentFlex := tview.NewFlex().
		AddItem(a.navigationTree, 0, 2, true).
		AddItem(a.issuesColumn, 0, 5, false).
		AddItem(a.detailsView, 0, 3, false)

	// Create vertical layout: content + status bar
	a.mainLayout = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(contentFlex, 0, 1, true).
		AddItem(a.statusBar, 1, 1, false)

	// Build palette modal
	a.paletteModal = a.buildPaletteModal()

	// Build picker and create issue modals
	a.pickerModal = NewPickerModal(a)
	a.createIssueModal = NewCreateIssueModal(a)
	a.createCommentModal = NewCreateCommentModal(a)
	a.editTitleModal = NewEditTitleModal(a)
	a.editLabelsModal = NewEditLabelsModal(a)
	a.textInputModal = NewTextInputModal(a)
	a.multiSelectModal = NewMultiSelectModal(a)
	a.settingsModal = NewSettingsModal(a)
	a.promptTemplatesModal = NewAgentPromptTemplatesModal(a)
	a.agentPromptModal = NewAgentPromptModal(a)
	a.agentOutputModal = NewAgentOutputModal(a)
	a.confirmationModal = NewConfirmationModal(a)
	a.agentRunner = agents.NewRunner()

	// Add main layout to pages
	a.pages.AddPage("main", a.mainLayout, true, true)
	a.pages.AddPage("palette", a.paletteModal, true, false)

	// Set initial focus
	a.updateFocus()
}

// bindGlobalKeys sets up global keyboard shortcuts.
func (a *App) bindGlobalKeys() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if a.pages.HasPage("confirmation") && a.confirmationModal != nil {
			return a.confirmationModal.HandleKey(event)
		}

		// Handle picker modal if active
		if a.pickerActive {
			return a.pickerModal.HandleKey(event)
		}

		// Check if create issue modal is visible and handle its keys
		if a.pages.HasPage("create_issue") && a.createIssueModal != nil {
			return a.createIssueModal.HandleKey(event)
		}

		// Check if create comment modal is visible and handle its keys
		if a.pages.HasPage("create_comment") && a.createCommentModal != nil {
			return a.createCommentModal.HandleKey(event)
		}

		// Check if edit title modal is visible and handle its keys
		if a.pages.HasPage("edit_title") && a.editTitleModal != nil {
			return a.editTitleModal.HandleKey(event)
		}

		// Check if edit labels modal is visible and handle its keys
		if a.pages.HasPage("edit_labels") && a.editLabelsModal != nil {
			return a.editLabelsModal.HandleKey(event)
		}

		if a.pages.HasPage("text_input") && a.textInputModal != nil {
			return a.textInputModal.HandleKey(event)
		}

		if a.pages.HasPage("multi_select") && a.multiSelectModal != nil {
			return a.multiSelectModal.HandleKey(event)
		}

		// Check if settings modal is visible and handle its keys
		if a.pages.HasPage("settings") && a.settingsModal != nil {
			return a.settingsModal.HandleKey(event)
		}

		// Check if prompt templates modal is visible and handle its keys
		if a.pages.HasPage("prompt_templates") && a.promptTemplatesModal != nil {
			return a.promptTemplatesModal.HandleKey(event)
		}

		// Check if agent prompt modal is visible and handle its keys
		if a.pages.HasPage("agent_prompt") && a.agentPromptModal != nil {
			return a.agentPromptModal.HandleKey(event)
		}

		// Check if agent output modal is visible and handle its keys
		if a.pages.HasPage("agent_output") && a.agentOutputModal != nil {
			return a.agentOutputModal.HandleKey(event)
		}

		// Handle palette first if it's open
		if a.focusedPane == FocusPalette {
			return a.handlePaletteKey(event)
		}

		// Global shortcuts (only when not in palette)
		switch event.Key() {
		case tcell.KeyEscape:
			// Clear search if active (when not in modals/palette)
			if a.searchQuery != "" {
				a.setSearchQuery("")
				return nil
			}
		case tcell.KeyCtrlC:
			a.app.Stop()
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
			case 'q':
				a.app.Stop()
				return nil
			case ':':
				a.openPalette()
				return nil
			case '/':
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

// handleNavigationKey handles keyboard input when navigation pane is focused.
func (a *App) handleNavigationKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyRight:
		a.focusedPane = FocusIssues
		a.updateFocus()
		return nil
	case tcell.KeyRune:
		if event.Rune() == 'l' {
			a.focusedPane = FocusIssues
			a.updateFocus()
			return nil
		}
	}
	return event
}

// handleIssuesKey handles keyboard input when issues pane is focused.
func (a *App) handleIssuesKey(event *tcell.EventKey) *tcell.EventKey {
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
			for _, cmd := range a.paletteCtrl.commands {
				if cmd.ShortcutRune != 0 && cmd.ShortcutRune == r {
					cmd.Run(a)
					return nil
				}
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
	a.paletteCtrl.SetSearchMode(true)
	a.paletteCtrl.SetQuery(a.searchQuery)
	a.paletteInput.SetText(a.searchQuery)
	a.paletteInput.SetLabel("/ ")
	a.paletteInput.SetPlaceholder("Type to search issues...")
	a.paletteList.Clear()
	a.paletteModalContent.SetTitle(" Search Issues ")
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

// queueIssuesRefresh records a refresh request while a fetch is in progress.
func (a *App) queueIssuesRefresh(allowFocusChange bool, issueID ...string) {
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
	if a.isLoading {
		a.queueIssuesRefresh(allowFocusChange, issueID...)
		return
	}
	a.isLoading = true

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
				a.isLoading = false
				logger.ErrorWithErr(err, "tui.app: failed to fetch issues")
				a.updateStatusBarWithError(err)
				a.notifyRefreshCompleted()
				a.runQueuedIssuesRefresh()
			})
			return
		}
		if generation != a.refreshGeneration.Load() {
			a.QueueUpdateDraw(func() {
				a.isLoading = false
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
			a.isLoading = false
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

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme)

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
	a.selectedIssue = &issue
	a.issuesMu.Unlock()
	a.updateDetailsView()

	// Fetch full issue details (including comments) in background
	issueID := issue.ID
	a.fetchingIssueID = issueID

	go func() {
		logger.Debug("tui.app: fetching full issue details issue=%s", issue.Identifier)
		ctx := context.Background()
		fetchIssue := a.fetchIssueByID
		if fetchIssue == nil {
			fetchIssue = a.api.FetchIssueByID
		}
		fullIssue, err := fetchIssue(ctx, issueID)

		a.QueueUpdateDraw(func() {
			// Race-safety: only apply if this is still the issue we're fetching
			if a.fetchingIssueID == issueID {
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

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme)
}

// onNavigationSelected handles when a navigation item is selected.
func (a *App) onNavigationSelected(node *NavigationNode) {
	logger.Debug("tui.app: navigation selected node_id=%s node_text=%s is_team=%v is_project=%v is_cycle=%v", node.ID, node.Text, node.IsTeam, node.IsProject, node.IsCycle)
	a.selectedNavigation = node

	// Update selected team metadata for commands and create-issue defaults.
	if node.TeamID != "" {
		go a.preloadTeamMetadataFunc(node.TeamID)
	}

	// Refresh issues for the new selection - run in goroutine to avoid blocking
	// the tview callback (QueueUpdateDraw deadlocks if called from within a callback)
	go a.refreshIssuesWithFocusChange(false)
}

// preloadTeamMetadata warms team-scoped metadata caches for commands and create-issue defaults.
func (a *App) preloadTeamMetadata(teamID string) {
	logger.Debug("tui.app: preloading team metadata team_id=%s", teamID)
	ctx := context.Background()
	_ = a.cache.PreloadTeamMetadata(ctx, teamID)

	users, _ := a.cache.GetUsers(ctx, teamID)
	projects, _ := a.cache.GetProjects(ctx, teamID)
	states, _ := a.cache.GetWorkflowStates(ctx, teamID)
	cycles, _ := a.cache.GetCycles(ctx, teamID)

	logger.Debug("tui.app: loaded team metadata team_id=%s users_count=%d projects_count=%d states_count=%d cycles_count=%d", teamID, len(users), len(projects), len(states), len(cycles))
	a.app.QueueUpdateDraw(func() {
		a.teamUsers = users
		a.teamProjects = projects
		a.workflowStates = states
		a.teamCycles = cycles
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

	switch a.focusedPane {
	case FocusNavigation:
		helpText = fmt.Sprintf("%s↑↓: navigate | Enter: select | Tab/→/l: next pane | Shift+Tab/←/h: prev pane | :: palette | /: search | q: quit[-]", keyColor)
	case FocusIssues:
		helpText = fmt.Sprintf("%sj/k: navigate | Enter: select | Tab/→/l: next pane | Shift+Tab/←/h: prev pane | :: palette | /: search | q: quit[-]", keyColor)
	case FocusDetails:
		helpText = fmt.Sprintf("%sj/k: scroll | Tab: switch description/comments | →/l: next pane | Shift+Tab/←/h: prev pane | :: palette | /: search | q: quit[-]", keyColor)
	case FocusPalette:
		helpText = fmt.Sprintf("%s↑↓: navigate | Enter: execute | Esc: close[-]", keyColor)
	default:
		helpText = fmt.Sprintf("%sj/k: navigate | Tab: next pane | Shift+Tab: prev pane | :: palette | /: search | q: quit[-]", keyColor)
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

	parts := []string{helpText}
	if navText != "" {
		parts = append(parts, navText)
	}
	if searchText != "" {
		parts = append(parts, searchText)
	}
	if filterText != "" {
		parts = append(parts, filterText)
	}
	if a.statusMessage != "" {
		parts = append(parts, fmt.Sprintf("%s%s[-]", a.themeTags.Accent, a.statusMessage))
	}
	parts = append(parts, statusText)

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
