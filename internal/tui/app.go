package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

// App is the main application controller that manages all UI components.
type App struct {
	app    *tview.Application
	api    *linearapi.Client
	cache  *cache.TeamCache
	config config.Config

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

	// App state
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
	searchQuery string
	sortField   SortField

	// Cached metadata for currently selected team
	currentUser    *linearapi.User
	teamUsers      []linearapi.User
	workflowStates []linearapi.WorkflowState

	// Loading state
	isLoading             bool
	pendingRefresh        bool
	pendingRefreshIssueID string
	pickerActive          bool

	// Race-safety for issue detail fetching
	fetchingIssueID string // Tracks which issue ID we're currently fetching

	// Details pane sub-view focus
	focusedDetailsView bool // false = description, true = comments
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
func NewApp(api *linearapi.Client, cfg config.Config) *App {
	app := &App{
		app:                 tview.NewApplication(),
		api:                 api,
		cache:               cache.NewTeamCache(api, cfg.CacheTTL),
		config:              cfg,
		pages:               tview.NewPages(),
		focusedPane:         FocusNavigation,
		sortField:           SortByUpdatedAt,
		expandedState:       make(map[string]bool),
		idToIssue:           make(map[string]*linearapi.Issue),
		myIDToIssue:         make(map[string]*linearapi.Issue),
		otherIDToIssue:      make(map[string]*linearapi.Issue),
		activeIssuesSection: IssuesSectionOther, // Default to Other section
	}

	app.paletteCtrl = NewPaletteController(DefaultCommands(app))

	// Apply global theme
	tview.Styles.PrimitiveBackgroundColor = LinearTheme.Background
	tview.Styles.ContrastBackgroundColor = LinearTheme.Background
	tview.Styles.MoreContrastBackgroundColor = LinearTheme.HeaderBg
	tview.Styles.BorderColor = LinearTheme.Border
	tview.Styles.TitleColor = LinearTheme.Foreground
	tview.Styles.GraphicsColor = LinearTheme.Border
	tview.Styles.PrimaryTextColor = LinearTheme.Foreground
	tview.Styles.SecondaryTextColor = LinearTheme.SecondaryText
	tview.Styles.TertiaryTextColor = LinearTheme.SecondaryText
	tview.Styles.InverseTextColor = LinearTheme.Background
	tview.Styles.ContrastSecondaryTextColor = LinearTheme.SecondaryText

	app.buildLayout()
	app.bindGlobalKeys()

	return app
}

// Run starts the application and blocks until it exits.
func (a *App) Run() error {
	a.app.SetRoot(a.pages, true).EnableMouse(true)

	// Load initial data asynchronously
	go func() {
		ctx := context.Background()

		// Fetch current user first
		user, err := a.cache.GetCurrentUser(ctx)
		if err == nil {
			a.currentUser = &user
			logger.Debug("Current user loaded: %s", user.DisplayName)
		} else {
			logger.Warning("Failed to load current user: %v", err)
		}

		// Fetch teams and build navigation
		a.loadNavigationData(ctx)

		// Load issues for initial view
		a.refreshIssues()
	}()

	// Start the application event loop
	return a.app.Run()
}

// loadNavigationData fetches teams and projects from the API and updates the navigation tree.
func (a *App) loadNavigationData(ctx context.Context) {
	teams, err := a.cache.GetTeams(ctx)
	if err != nil {
		logger.ErrorWithErr(err, "Failed to load teams")
		a.app.QueueUpdateDraw(func() {
			a.updateStatusBarWithError(err)
		})
		return
	}

	logger.Debug("Loaded %d teams", len(teams))
	a.app.QueueUpdateDraw(func() {
		a.rebuildNavigationTree(teams)
	})
}

// rebuildNavigationTree rebuilds the navigation tree with real data.
func (a *App) rebuildNavigationTree(teams []linearapi.Team) {
	root := tview.NewTreeNode("Linear").
		SetColor(LinearTheme.Accent).
		SetSelectable(false)

	// Add "All Issues" at the top
	allIssues := tview.NewTreeNode("All Issues").
		SetColor(LinearTheme.Foreground).
		SetReference(&NavigationNode{ID: "all", Text: "All Issues"}).
		SetExpanded(true)
	root.AddChild(allIssues)

	// Add teams
	for _, team := range teams {
		teamNode := tview.NewTreeNode(team.Name).
			SetColor(LinearTheme.Foreground).
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

	// Load projects asynchronously
	go func() {
		ctx := context.Background()
		projects, err := a.cache.GetProjects(ctx, teamID)
		if err != nil {
			a.app.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}

		a.app.QueueUpdateDraw(func() {
			// Double-check children haven't been added by another goroutine
			if len(teamNode.GetChildren()) > 0 {
				teamNode.SetExpanded(true)
				return
			}
			for _, proj := range projects {
				projNode := tview.NewTreeNode("  " + proj.Name).
					SetColor(LinearTheme.SecondaryText).
					SetReference(&NavigationNode{
						ID:        proj.ID,
						Text:      proj.Name,
						IsProject: true,
						TeamID:    teamID,
					})
				teamNode.AddChild(projNode)
			}
			teamNode.SetExpanded(true)
		})
	}()
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

	// Add main layout to pages
	a.pages.AddPage("main", a.mainLayout, true, true)
	a.pages.AddPage("palette", a.paletteModal, true, false)

	// Set initial focus
	a.updateFocus()
}

// bindGlobalKeys sets up global keyboard shortcuts.
func (a *App) bindGlobalKeys() {
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
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

		// Handle palette first if it's open
		if a.focusedPane == FocusPalette {
			return a.handlePaletteKey(event)
		}

		// Handle command shortcuts (Cmd/Ctrl + key) - only in main panes
		platformMod := GetPlatformModifier()
		if event.Modifiers()&platformMod != 0 && event.Key() == tcell.KeyRune {
			r := event.Rune()
			// Check all commands for matching shortcut
			for _, cmd := range a.paletteCtrl.commands {
				if cmd.ShortcutRune != 0 && cmd.ShortcutRune == r {
					cmd.Run(a)
					return nil
				}
			}
		}

		// Global shortcuts (only when not in palette)
		switch event.Key() {
		case tcell.KeyCtrlC:
			a.app.Stop()
			return nil
		case tcell.KeyTab:
			// Tab cycles forward through panes (Navigation -> Issues -> Details)
			// When in Details pane, first cycle between description and comments
			// Only cycle when not in palette or modals
			if a.focusedPane != FocusPalette {
				if a.focusedPane == FocusDetails {
					// Cycle between description and comments within details pane
					if event.Modifiers()&tcell.ModShift == 0 {
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
					if event.Modifiers()&tcell.ModShift == 0 {
						a.cyclePanesForward()
					} else {
						// Shift+Tab cycles backward
						a.cyclePanesBackward()
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
		switch event.Rune() {
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
		a.closePalette()
		return nil
	case tcell.KeyEnter:
		if a.paletteCtrl.IsSearchMode() {
			// In search mode, submit the search query
			query := a.paletteCtrl.Query()
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
			if !a.paletteCtrl.IsSearchMode() {
				a.updatePaletteList()
			}
		}
		return nil
	case tcell.KeyRune:
		query := a.paletteCtrl.Query() + string(event.Rune())
		a.paletteCtrl.SetQuery(query)
		a.paletteInput.SetText(query)
		if !a.paletteCtrl.IsSearchMode() {
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
// Otherwise cycles: Details -> Issues -> Navigation -> Details
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
		// Set to Other Issues if available, otherwise My Issues
		if len(a.otherIssueRows) > 0 {
			a.activeIssuesSection = IssuesSectionOther
		} else {
			a.activeIssuesSection = IssuesSectionMy
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
		a.navigationTree.SetBorderColor(LinearTheme.BorderFocus)
		a.myIssuesTable.SetBorderColor(LinearTheme.Border)
		a.otherIssuesTable.SetBorderColor(LinearTheme.Border)
		a.detailsDescriptionView.SetBorderColor(LinearTheme.Border)
		a.detailsCommentsView.SetBorderColor(LinearTheme.Border)
	case FocusIssues:
		// Focus the active issues section
		if a.activeIssuesSection == IssuesSectionMy && len(a.myIssueRows) > 0 {
			a.app.SetFocus(a.myIssuesTable)
			a.myIssuesTable.SetBorderColor(LinearTheme.BorderFocus)
			a.otherIssuesTable.SetBorderColor(LinearTheme.Border)
		} else {
			a.app.SetFocus(a.otherIssuesTable)
			a.myIssuesTable.SetBorderColor(LinearTheme.Border)
			a.otherIssuesTable.SetBorderColor(LinearTheme.BorderFocus)
			a.activeIssuesSection = IssuesSectionOther
		}
		a.navigationTree.SetBorderColor(LinearTheme.Border)
		a.detailsDescriptionView.SetBorderColor(LinearTheme.Border)
		a.detailsCommentsView.SetBorderColor(LinearTheme.Border)
	case FocusDetails:
		// Focus the appropriate sub-view based on state
		if a.focusedDetailsView {
			a.app.SetFocus(a.detailsCommentsView)
			a.detailsDescriptionView.SetBorderColor(LinearTheme.Border)
			a.detailsCommentsView.SetBorderColor(LinearTheme.BorderFocus)
		} else {
			a.app.SetFocus(a.detailsDescriptionView)
			a.detailsDescriptionView.SetBorderColor(LinearTheme.BorderFocus)
			a.detailsCommentsView.SetBorderColor(LinearTheme.Border)
		}
		a.navigationTree.SetBorderColor(LinearTheme.Border)
		a.myIssuesTable.SetBorderColor(LinearTheme.Border)
		a.otherIssuesTable.SetBorderColor(LinearTheme.Border)
	case FocusPalette:
		a.app.SetFocus(a.paletteInput)
		a.navigationTree.SetBorderColor(LinearTheme.Border)
		a.myIssuesTable.SetBorderColor(LinearTheme.Border)
		a.otherIssuesTable.SetBorderColor(LinearTheme.Border)
		a.detailsDescriptionView.SetBorderColor(LinearTheme.Border)
		a.detailsCommentsView.SetBorderColor(LinearTheme.Border)
	}
	a.updateStatusBar()
}

// openPalette opens the command palette overlay.
func (a *App) openPalette() {
	a.paletteCtrl.Reset()
	a.paletteInput.SetText("")
	a.paletteInput.SetLabel("> ")
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
	a.paletteList.Clear()
	a.pages.ShowPage("palette")
	a.pages.SendToFront("palette")
	a.focusedPane = FocusPalette
	a.updateFocus()
}

// closePalette closes the command palette overlay.
func (a *App) closePalette() {
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
	a.focusedPane = FocusNavigation
	a.updateFocus()
}

// closePaletteUI closes the palette UI without changing focus.
// This is used when focus will be set by the caller (e.g., after search).
func (a *App) closePaletteUI() {
	a.paletteCtrl.SetSearchMode(false)
	a.pages.HidePage("palette")
}

// queueIssuesRefresh records a refresh request while a fetch is in progress.
func (a *App) queueIssuesRefresh(issueID ...string) {
	a.pendingRefresh = true
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
	a.pendingRefresh = false
	a.pendingRefreshIssueID = ""
	if issueID != "" {
		go a.refreshIssues(issueID)
		return
	}
	go a.refreshIssues()
}

// refreshIssues fetches issues from the API and updates the UI.
// If issueID is provided, that issue will be selected after refresh.
func (a *App) refreshIssues(issueID ...string) {
	if a.isLoading {
		a.queueIssuesRefresh(issueID...)
		return
	}
	a.isLoading = true

	var targetIssueID string
	if len(issueID) > 0 {
		targetIssueID = issueID[0]
	}

	go func() {
		ctx := context.Background()

		params := linearapi.FetchIssuesParams{
			First:   a.config.PageSize,
			Search:  a.searchQuery,
			OrderBy: string(a.sortField),
		}
		params.OnProgress = func(progress linearapi.IssueFetchProgress) {
			a.app.QueueUpdateDraw(func() {
				a.statusBar.SetText(fmt.Sprintf("[yellow]Loading issues (page %d, fetched %d)...[-]", progress.Page, progress.Fetched))
			})
		}

		// Apply team/project filter based on navigation selection
		if a.selectedNavigation != nil {
			if a.selectedNavigation.IsTeam {
				params.TeamID = a.selectedNavigation.TeamID
			} else if a.selectedNavigation.IsProject {
				params.TeamID = a.selectedNavigation.TeamID
				params.ProjectID = a.selectedNavigation.ID
			}
			// If "All Issues", no team/project filter
		}

		issues, err := a.api.FetchIssues(ctx, params)

		a.app.QueueUpdateDraw(func() {
			a.isLoading = false
			if err != nil {
				logger.ErrorWithErr(err, "Failed to fetch issues")
				a.updateStatusBarWithError(err)
			} else {
				logger.Debug("Fetched %d issues", len(issues))
				a.updateIssuesData(issues, targetIssueID)
				// Ensure focus is on issues table after refresh
				a.focusedPane = FocusIssues
				a.updateFocus()
			}
			a.runQueuedIssuesRefresh()
		})
	}()

	// Show loading indicator
	a.app.QueueUpdateDraw(func() {
		a.statusBar.SetText("[yellow]Loading...[-]")
	})
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
}

// updateIssuesData updates the UI with new issues data.
// If issueID is provided, that issue will be selected if found in the list.
func (a *App) updateIssuesData(issues []linearapi.Issue, issueID ...string) {
	a.issues = issues

	// Determine target issue ID
	var targetIssueID string
	if len(issueID) > 0 && issueID[0] != "" {
		targetIssueID = issueID[0]
	} else if a.selectedIssue != nil {
		targetIssueID = a.selectedIssue.ID
	}

	// Split issues by assignee
	currentUserID := ""
	if a.currentUser != nil {
		currentUserID = a.currentUser.ID
	}
	myIssues, otherIssues := splitIssuesByAssignee(issues, currentUserID)

	// Build hierarchical tree rows for each section
	a.myIssueRows, a.myIDToIssue = BuildIssueRows(myIssues, a.expandedState)
	a.otherIssueRows, a.otherIDToIssue = BuildIssueRows(otherIssues, a.expandedState)

	// Legacy: keep old fields for backward compatibility during migration
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

	// Update layout to show/hide My Issues section
	a.updateIssuesColumnLayout()

	// Render both tables
	var selectedMyIssueID, selectedOtherIssueID string
	if targetIssueID != "" {
		// Check which section contains the target issue
		if _, ok := a.myIDToIssue[targetIssueID]; ok {
			selectedMyIssueID = targetIssueID
			a.activeIssuesSection = IssuesSectionMy
		} else if _, ok := a.otherIDToIssue[targetIssueID]; ok {
			selectedOtherIssueID = targetIssueID
			a.activeIssuesSection = IssuesSectionOther
		}
	}

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID)

	// Select issue and update details
	var selectedIssue *linearapi.Issue
	if targetIssueID != "" {
		if issue, ok := a.myIDToIssue[targetIssueID]; ok {
			selectedIssue = issue
		} else if issue, ok := a.otherIDToIssue[targetIssueID]; ok {
			selectedIssue = issue
		}
	}

	// If no target issue, default to first available
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

	if selectedIssue != nil {
		a.onIssueSelected(*selectedIssue)
	} else {
		a.selectedIssue = nil
		a.updateDetailsView()
	}
	a.updateStatusBar()
}

// onIssueSelected handles when an issue is selected.
func (a *App) onIssueSelected(issue linearapi.Issue) {
	// Set selected issue immediately for quick UI feedback
	a.selectedIssue = &issue
	a.updateDetailsView()

	// Fetch full issue details (including comments) in background
	issueID := issue.ID
	a.fetchingIssueID = issueID

	go func() {
		ctx := context.Background()
		fullIssue, err := a.api.FetchIssueByID(ctx, issueID)

		a.app.QueueUpdateDraw(func() {
			// Race-safety: only apply if this is still the issue we're fetching
			if a.fetchingIssueID == issueID {
				if err != nil {
					logger.ErrorWithErr(err, "Failed to fetch full issue details for %s", issue.Identifier)
					// Keep the partial issue data we already have
					return
				}
				a.selectedIssue = &fullIssue
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

	ToggleExpanded(a.expandedState, issueID)

	// Rebuild rows for both sections
	currentUserID := ""
	if a.currentUser != nil {
		currentUserID = a.currentUser.ID
	}
	myIssues, otherIssues := splitIssuesByAssignee(a.issues, currentUserID)
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

	renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID)
	renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID)
}

// onNavigationSelected handles when a navigation item is selected.
func (a *App) onNavigationSelected(node *NavigationNode) {
	a.selectedNavigation = node

	// Update selected team/project
	if node.IsTeam {
		// Load team metadata (users, workflow states) in background
		go func() {
			ctx := context.Background()
			_ = a.cache.PreloadTeamMetadata(ctx, node.TeamID)

			// Update team users and states for the selected team
			users, _ := a.cache.GetUsers(ctx, node.TeamID)
			states, _ := a.cache.GetWorkflowStates(ctx, node.TeamID)

			a.app.QueueUpdateDraw(func() {
				a.teamUsers = users
				a.workflowStates = states
			})
		}()
	}

	// Refresh issues for the new selection - run in goroutine to avoid blocking
	// the tview callback (QueueUpdateDraw deadlocks if called from within a callback)
	go a.refreshIssues()
}

// setSearchQuery sets the search query and refreshes issues.
func (a *App) setSearchQuery(query string) {
	trimmedQuery := strings.TrimSpace(query)
	a.searchQuery = trimmedQuery
	// Set focus to issues pane when searching
	a.focusedPane = FocusIssues
	a.updateFocus()
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssues()
}

// setSortField sets the sort field and refreshes issues.
func (a *App) setSortField(field SortField) {
	a.sortField = field
	// Run in goroutine to avoid deadlock when called from tview callbacks
	go a.refreshIssues()
}

// updateStatusBar updates the status bar with current information.
func (a *App) updateStatusBar() {
	var helpText string
	keyColor := "[#A0A0A0]"

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
		navText = fmt.Sprintf("[#5E6AD2]%s[-]", a.selectedNavigation.Text)
	}

	searchText := ""
	if a.searchQuery != "" {
		searchText = fmt.Sprintf("[#F2C94C]🔍 %s[-]", a.searchQuery)
	}

	statusText := fmt.Sprintf("[#5E6AD2]%d issues[-]", len(a.issues))
	if len(a.issues) == 0 {
		statusText = "[#787878]No issues[-]"
	}

	sep := "[#3C3C3C] | [-]"

	parts := []string{helpText}
	if navText != "" {
		parts = append(parts, navText)
	}
	if searchText != "" {
		parts = append(parts, searchText)
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
	a.statusBar.SetText(fmt.Sprintf("[red]Error: %v[-]", err))
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
	return a.selectedIssue
}

// GetSelectedTeamID returns the currently selected team ID, if any.
func (a *App) GetSelectedTeamID() string {
	if a.selectedNavigation != nil && a.selectedNavigation.TeamID != "" {
		return a.selectedNavigation.TeamID
	}
	// If we have a selected issue, use its team
	if a.selectedIssue != nil {
		return a.selectedIssue.TeamID
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

// GetWorkflowStates returns the workflow states for the currently selected team.
func (a *App) GetWorkflowStates() []linearapi.WorkflowState {
	return a.workflowStates
}

// QueueUpdateDraw queues a UI update function to be run in the main thread.
func (a *App) QueueUpdateDraw(f func()) {
	a.app.QueueUpdateDraw(f)
}

// ShowStatusPicker shows a picker for workflow states.
func (a *App) ShowStatusPicker(onSelect func(stateID string)) {
	states := a.workflowStates
	if len(states) == 0 {
		// Try to load states for current team
		teamID := a.GetSelectedTeamID()
		if teamID == "" {
			return
		}
		go func() {
			ctx := context.Background()
			loadedStates, err := a.cache.GetWorkflowStates(ctx, teamID)
			if err != nil {
				a.QueueUpdateDraw(func() {
					a.updateStatusBarWithError(err)
				})
				return
			}
			a.QueueUpdateDraw(func() {
				a.workflowStates = loadedStates
				a.showStatusPickerWithStates(loadedStates, onSelect)
			})
		}()
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
	users := a.teamUsers
	if len(users) == 0 {
		// Try to load users for current team
		teamID := a.GetSelectedTeamID()
		if teamID == "" {
			return
		}
		go func() {
			ctx := context.Background()
			loadedUsers, err := a.cache.GetUsers(ctx, teamID)
			if err != nil {
				a.QueueUpdateDraw(func() {
					a.updateStatusBarWithError(err)
				})
				return
			}
			a.QueueUpdateDraw(func() {
				a.teamUsers = loadedUsers
				a.showUserPickerWithUsers(loadedUsers, onSelect)
			})
		}()
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

// ShowParentIssuePicker shows a picker for selecting a parent issue.
// It lists all top-level issues (issues without a parent) from the current list.
func (a *App) ShowParentIssuePicker(onSelect func(parentID string)) {
	// Filter to only show issues that could be parents (no parent themselves)
	items := make([]PickerItem, 0)
	for _, issue := range a.issues {
		if issue.Parent == nil {
			items = append(items, PickerItem{
				ID:    issue.ID,
				Label: issue.Identifier + " - " + issue.Title,
			})
		}
	}

	if len(items) == 0 {
		a.updateStatusBarWithError(fmt.Errorf("no parent issues available"))
		return
	}

	a.pickerActive = true
	a.pickerModal.Show("Select Parent Issue", items, func(item PickerItem) {
		a.pickerActive = false
		onSelect(item.ID)
	})
}

// ShowCreateIssueModal shows the create issue modal.
func (a *App) ShowCreateIssueModal() {
	a.showCreateIssueModalWithParent("")
}

// ShowCreateSubIssueModal shows the create issue modal with a parent issue pre-set.
func (a *App) ShowCreateSubIssueModal(parentID string) {
	a.showCreateIssueModalWithParent(parentID)
}

// showCreateIssueModalWithParent shows the create issue modal, optionally with a parent.
func (a *App) showCreateIssueModalWithParent(parentID string) {
	teamID := a.GetSelectedTeamID()
	projectID := ""
	if a.selectedNavigation != nil && a.selectedNavigation.IsProject {
		projectID = a.selectedNavigation.ID
	}

	a.createIssueModal.Show(teamID, projectID, func(title, description, tID, pID, assigneeID string, priority int) {
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
			if priority > 0 {
				input.Priority = priority
			}
			if parentID != "" {
				input.ParentID = parentID
			}
			issue, err := a.api.CreateIssue(ctx, input)
			a.QueueUpdateDraw(func() {
				if err != nil {
					logger.ErrorWithErr(err, "Failed to create issue: %s", title)
					a.updateStatusBarWithError(err)
					return
				}
				if parentID != "" {
					logger.Info("Created sub-issue %s: %s", issue.Identifier, title)
				} else {
					logger.Info("Created issue %s: %s", issue.Identifier, title)
				}
				go a.refreshIssues(issue.ID)
			})
		}()
	})
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
					logger.ErrorWithErr(err, "Failed to update issue title for %s", issue.Identifier)
					a.updateStatusBarWithError(err)
					return
				}
				logger.Info("Updated title for issue %s", issue.Identifier)
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
		ctx := context.Background()
		availableLabels, err := a.cache.GetIssueLabels(ctx, teamID)
		if err != nil {
			a.QueueUpdateDraw(func() {
				a.updateStatusBarWithError(err)
			})
			return
		}

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
							logger.ErrorWithErr(err, "Failed to update labels for issue %s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("Updated labels for issue %s", issue.Identifier)
						go a.refreshIssues(issueID)
					})
				}()
			})
		})
	}()
}
