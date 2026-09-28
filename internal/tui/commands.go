package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// FormatShortcut returns a human-readable string for a shortcut.
func FormatShortcut(r rune) string {
	if r == 0 {
		return ""
	}
	return strings.ToUpper(string(r))
}

// Command represents a command that can be executed from the palette.
type Command struct {
	ID              string
	Title           string
	Keywords        []string
	ShortcutRune    rune   // The rune for the keyboard shortcut (e.g., 'r' for refresh)
	ShortcutDisplay string // Custom display text for shortcut (e.g., "/" or "Esc"), overrides ShortcutRune display
	Run             func(a *App)
}

// CommandContext provides context for command execution.
type CommandContext struct {
	SelectedIssue *linearapi.Issue
}

// handleAskAgent handles the ask agent command.
func handleAskAgent(a *App) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.updateStatusBarWithError(fmt.Errorf("no issue selected"))
		return
	}

	if a.agentPromptModal == nil {
		a.agentPromptModal = NewAgentPromptModal(a)
	}
	if a.agentOutputModal == nil {
		a.agentOutputModal = NewAgentOutputModal(a)
	}
	if a.agentRunner == nil {
		a.agentRunner = agents.NewRunner()
	}

	issueID := issue.ID
	a.agentPromptModal.Show(func(prompt string, workspace string) {
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return
		}
		workspace = strings.TrimSpace(workspace)

		go func() {
			fetchIssue := a.fetchIssueByID
			if fetchIssue == nil {
				fetchIssue = a.api.FetchIssueByID
			}

			fullIssue, err := fetchIssue(context.Background(), issueID)
			if err != nil {
				logger.ErrorWithErr(err, "tui.commands: failed to fetch issue for agent issue_id=%s", issueID)
				a.QueueUpdateDraw(func() {
					a.updateStatusBarWithError(err)
				})
				return
			}

			issueContext := agents.BuildIssueContext(fullIssue)
			runner := a.agentRunner

			selected, err := agents.ProviderForKey(a.config.AgentProvider, runner.LookPath)
			if err != nil {
				logger.Error("tui.commands: invalid agent provider provider=%s", a.config.AgentProvider)
				a.QueueUpdateDraw(func() {
					a.updateStatusBarWithError(err)
				})
				return
			}

			if _, ok := selected.ResolveBinary(); !ok {
				logger.Error("tui.commands: agent binary not found provider=%s", selected.Name())
				a.QueueUpdateDraw(func() {
					a.updateStatusBarWithError(fmt.Errorf("agent binary not found for %s", selected.Name()))
				})
				return
			}

			options := agents.AgentRunOptions{
				Workspace: workspace,
				Model:     strings.TrimSpace(a.config.AgentModel),
				Sandbox:   strings.TrimSpace(a.config.AgentSandbox),
			}

			ctx, cancel := context.WithCancel(context.Background())
			a.QueueUpdateDraw(func() {
				title := fmt.Sprintf(" %s Output ", selected.Name())
				a.agentOutputModal.Show(title, cancel)
				a.agentOutputModal.AppendLine(fmt.Sprintf("Starting %s agent run...", selected.Name()))
			})

			runErr := runner.Run(ctx, selected, prompt, issueContext, options, func(event agents.AgentEvent) {
				a.agentOutputModal.AppendEvent(event)
			}, func(line string) {
				a.agentOutputModal.AppendRawLine(line)
			}, func(runErr error) {
				a.agentOutputModal.AppendLine(fmt.Sprintf("error: %v", runErr))
			})

			if runErr != nil {
				a.QueueUpdateDraw(func() {
					a.agentOutputModal.AppendLine(fmt.Sprintf("error: %v", runErr))
					a.agentOutputModal.FailRun(runErr)
				})
				return
			}

			a.agentOutputModal.StopSpinner()
			a.agentOutputModal.AppendLine("Agent run completed.")
		}()
	})
}

// effectiveIssueTargetIDs snapshots the current selection for a command. A
// mark set takes precedence over the cursor; otherwise the selected/cursor
// issue is the sole target.
func (a *App) effectiveIssueTargetIDs() []string {
	cursorID := ""
	selectedIssue := a.GetSelectedIssue()
	if selectedIssue != nil {
		cursorID = selectedIssue.ID
	}
	if cursorID == "" {
		_, section := a.activeIssuesTable()
		cursorID = a.cursorIssueID(section)
	}
	targetIDs := a.markedIssueSelection.EffectiveTargets(cursorID)
	resolved := a.resolveIssueTargetIDs(targetIDs)
	// A selected issue may be a freshly fetched detail that is not yet in the
	// table's loaded slice. Preserve that cursor target for single-issue
	// commands; marked targets still require reconciliation and are ignored when
	// stale.
	if len(resolved) == 0 && a.markedIssueSelection.Count() == 0 && selectedIssue != nil && selectedIssue.ID == cursorID {
		return []string{selectedIssue.ID}
	}
	return resolved
}

func navigationMutationKey(node *NavigationNode) string {
	if node == nil {
		return ""
	}
	return strings.Join([]string{
		node.ID,
		node.TeamID,
		node.CustomViewID,
		node.StateType,
		node.StateID,
		node.CycleID,
	}, "\x00")
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (a *App) resolveCommandTargets(issueIDs []string) []linearapi.Issue {
	targets := a.resolveIssueTargets(issueIDs)
	if len(targets) == 0 && a.markedIssueSelection.Count() == 0 && len(issueIDs) == 1 {
		if issue := a.GetSelectedIssue(); issue != nil && issue.ID == issueIDs[0] {
			return []linearapi.Issue{*issue}
		}
	}
	return targets
}

// runBulkIssueAction executes a mutation off the UI goroutine, emits progress
// through the status bar, and performs exactly one refresh when all targets
// have completed. Successful marks are removed on partial failure while failed
// marks remain available for retry.
func (a *App) runBulkIssueAction(action string, issueIDs []string, mutate func(context.Context, string) error) {
	issueIDs = a.resolveIssueTargetIDs(issueIDs)
	if len(issueIDs) == 0 && a.markedIssueSelection.Count() == 0 {
		if issue := a.GetSelectedIssue(); issue != nil {
			issueIDs = []string{issue.ID}
		}
	}
	if len(issueIDs) == 0 {
		a.flashStatus("No issue selected")
		return
	}
	ids := append([]string(nil), issueIDs...)
	snapshot := a.bulkIssueMutationSnapshot(ids)
	runner := a.bulkIssueActionRunner
	if runner == nil {
		runner = NewAsyncViewActionRunner(a.QueueUpdateDraw, AsyncViewActionHooks{
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
		a.bulkIssueActionRunner = runner
	}
	if runner.InFlight() {
		a.updateStatusBarWithError(fmt.Errorf("another bulk issue action is already in progress"))
		return
	}
	a.statusMessage = fmt.Sprintf("%s: 0/%d", action, len(ids))
	a.updateStatusBar()
	var summary BulkActionSummary
	var summaryMu sync.Mutex
	accepted := runner.RunResult("Bulk "+action, func(ctx context.Context) error {
		result := RunBulkAction(ctx, ids, 4, mutate, func(progress BulkActionProgress) {
			runner.Progress(fmt.Sprintf("%s: %d/%d", action, progress.Completed, progress.Total))
		})
		summaryMu.Lock()
		summary = result
		summaryMu.Unlock()
		return nil
	}, func(result AsyncViewActionResult) {
		if result.Err != nil || !a.bulkIssueMutationContextCurrent(snapshot) {
			return
		}
		summaryMu.Lock()
		final := summary
		summaryMu.Unlock()
		a.finishBulkIssueAction(action, ids, snapshot, final)
	})
	if !accepted && a.pages != nil {
		a.updateStatusBarWithError(fmt.Errorf("another bulk issue action is already in progress"))
	}
}

type bulkIssueMutationSnapshot struct {
	targetIDs           []string
	markedIDs           []string
	navigationKey       string
	selectionGeneration int64
}

func (a *App) bulkIssueMutationSnapshot(targetIDs []string) bulkIssueMutationSnapshot {
	snapshot := bulkIssueMutationSnapshot{
		targetIDs:           append([]string(nil), targetIDs...),
		markedIDs:           a.markedIssueSelection.IDs(),
		navigationKey:       navigationMutationKey(a.selectedNavigation),
		selectionGeneration: a.issueSelectionGeneration.Load(),
	}
	return snapshot
}

func (a *App) bulkIssueMutationContextCurrent(snapshot bulkIssueMutationSnapshot) bool {
	if a == nil || navigationMutationKey(a.selectedNavigation) != snapshot.navigationKey {
		return false
	}
	if !sameStringSlices(a.markedIssueSelection.IDs(), snapshot.markedIDs) {
		return false
	}
	if a.issueSelectionGeneration.Load() != snapshot.selectionGeneration {
		return false
	}
	currentTargets := a.effectiveIssueTargetIDs()
	if len(currentTargets) == 0 {
		// A successful refresh can temporarily clear the lightweight selected
		// issue before the next page is applied. The explicit selection
		// generation above still distinguishes a user selection change, so an
		// empty transient target set is not itself stale.
		return true
	}
	return sameStringSlices(currentTargets, snapshot.targetIDs)
}

func (a *App) finishBulkIssueAction(action string, targetIDs []string, snapshot bulkIssueMutationSnapshot, summary BulkActionSummary) {
	if !a.bulkIssueMutationContextCurrent(snapshot) {
		return
	}
	if summary.Failed == 0 {
		a.markedIssueSelection.Clear()
		if len(targetIDs) == 1 {
			if issue := a.issueForLoadedID(targetIDs[0]); issue != nil {
				message := ""
				switch action {
				case "Archive":
					message = fmt.Sprintf("Archived %s", issue.Identifier)
				case "Change status":
					message = fmt.Sprintf("Changed status for %s", issue.Identifier)
				case "Assign user":
					message = fmt.Sprintf("Assigned %s", issue.Identifier)
				case "Set cycle":
					message = fmt.Sprintf("Set cycle for %s", issue.Identifier)
				}
				if message != "" {
					a.flashStatus(message)
				} else {
					a.flashStatus(fmt.Sprintf("%s: %d succeeded", action, summary.Succeeded))
				}
			} else {
				a.flashStatus(fmt.Sprintf("%s: %d succeeded", action, summary.Succeeded))
			}
		} else {
			a.flashStatus(fmt.Sprintf("%s: %d succeeded", action, summary.Succeeded))
		}
	} else {
		failedIDs := make(map[string]struct{}, summary.Failed)
		for _, result := range summary.Results {
			if result.Err != nil {
				failedIDs[result.IssueID] = struct{}{}
				if issue := a.issueForLoadedID(result.IssueID); issue != nil {
					logger.ErrorWithErr(result.Err, "tui.commands: bulk %s failed issue=%s", action, issue.Identifier)
				} else {
					logger.ErrorWithErr(result.Err, "tui.commands: bulk %s failed issue_id=%s", action, result.IssueID)
				}
			}
		}
		for _, issueID := range targetIDs {
			if _, failed := failedIDs[issueID]; failed {
				if !a.markedIssueSelection.IsMarked(issueID) {
					a.markedIssueSelection.Toggle(issueID)
				}
				continue
			}
			if a.markedIssueSelection.IsMarked(issueID) {
				a.markedIssueSelection.Toggle(issueID)
			}
		}
		firstFailure := ""
		for _, result := range summary.Results {
			if result.Err != nil {
				firstFailure = result.Err.Error()
				break
			}
		}
		a.flashStatus(fmt.Sprintf("%s: %d succeeded, %d failed (first failure: %s)", action, summary.Succeeded, summary.Failed, firstFailure))
	}
	a.refreshIssueTableMarks()
	// Refresh only after the complete batch. Individual mutations never invoke
	// refreshIssues, which keeps network/UI work bounded to one final refresh.
	go a.refreshIssues()
}

func (a *App) issueForLoadedID(issueID string) *linearapi.Issue {
	if issueID == "" {
		return nil
	}
	a.issuesMu.RLock()
	defer a.issuesMu.RUnlock()
	for i := range a.issues {
		if a.issues[i].ID == issueID {
			issue := a.issues[i]
			return &issue
		}
	}
	return nil
}

func handleOpenBrowserCommand(a *App) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	if issue.URL == "" {
		a.flashStatus(fmt.Sprintf("No URL for %s", issue.Identifier))
		return
	}
	openFn := a.openURLFunc
	if openFn == nil {
		openFn = openURL
	}
	if err := openFn(issue.URL); err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.flashStatus(fmt.Sprintf("Opened %s: %s", issue.Identifier, issue.URL))
}

func handleCopyIssueIDCommand(a *App) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	copyFn := a.copyToClipboardFunc
	if copyFn == nil {
		copyFn = copyToClipboard
	}
	if err := copyFn(issue.Identifier); err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.flashStatus(fmt.Sprintf("Copied issue ID: %s", issue.Identifier))
}

func handleCopyIssueURLCommand(a *App) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	if issue.URL == "" {
		a.flashStatus(fmt.Sprintf("No URL for %s", issue.Identifier))
		return
	}
	copyFn := a.copyToClipboardFunc
	if copyFn == nil {
		copyFn = copyToClipboard
	}
	if err := copyFn(issue.URL); err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.flashStatus(fmt.Sprintf("Copied issue URL: %s", issue.Identifier))
}

func handleAssignMeCommand(a *App) {
	issue := a.GetSelectedIssue()
	user := a.GetCurrentUser()
	if issue == nil || user == nil {
		a.flashStatus("No issue or current user selected")
		return
	}
	go func() {
		ctx := context.Background()
		_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
			ID:         issue.ID,
			AssigneeID: &user.ID,
		})
		a.QueueUpdateDraw(func() {
			if err != nil {
				logger.ErrorWithErr(err, "tui.commands: failed to assign issue issue=%s user=%s", issue.Identifier, user.DisplayName)
				a.updateStatusBarWithError(err)
				return
			}
			logger.Info("tui.commands: assigned issue issue=%s user=%s", issue.Identifier, user.DisplayName)
			a.flashStatus(fmt.Sprintf("Assigned %s to %s", issue.Identifier, user.DisplayName))
			go a.refreshIssues(issue.ID)
		})
	}()
}

// DefaultCommands returns the default set of commands for the palette.
func DefaultCommands(app *App) []Command {
	lookPath := exec.LookPath
	if app != nil && app.agentRunner != nil && app.agentRunner.LookPath != nil {
		lookPath = app.agentRunner.LookPath
	}
	availableProviders := agents.AvailableProviderKeys(lookPath)

	commands := []Command{
		{
			ID:       "open_triage",
			Title:    "Open triage",
			Keywords: []string{"triage", "notifications", "workflow"},
			Run: func(a *App) {
				a.runKeySequenceCommand("navigate_triage")
			},
		},
		{
			ID:       "open_favorites",
			Title:    "Open favorites",
			Keywords: []string{"favorites", "starred", "sidebar"},
			Run: func(a *App) {
				a.runKeySequenceCommand("navigate_favorites")
			},
		},
		{
			ID:       "open_saved_views",
			Title:    "Manage saved views",
			Keywords: []string{"saved", "views", "custom", "view", "filter"},
			Run: func(a *App) {
				a.openSavedViews()
			},
		},
		{
			ID:       "open_inbox",
			Title:    "Open inbox",
			Keywords: []string{"inbox", "notifications", "mentions", "activity"},
			Run: func(a *App) {
				a.openInbox()
			},
		},
		{
			ID:       "open_comments",
			Title:    "Open issue comments/activity",
			Keywords: []string{"comments", "activity", "thread", "replies"},
			Run: func(a *App) {
				a.openCommentsModal()
			},
		},
		{
			ID:       "open_roadmap",
			Title:    "Open initiatives roadmap",
			Keywords: []string{"roadmap", "initiatives", "projects", "updates"},
			Run: func(a *App) {
				a.openRoadmap()
			},
		},
		{
			ID:           "refresh",
			Title:        "Refresh issues",
			Keywords:     []string{"refresh", "reload", "r"},
			ShortcutRune: 'r',
			Run: func(a *App) {
				a.flashStatus("Refreshing issues...")
				go a.refreshIssues()
			},
		},
		{
			ID:              "search",
			Title:           "Search issues",
			Keywords:        []string{"search", "find", "s", "/"},
			ShortcutDisplay: "/", // Handled globally, not via ShortcutRune
			Run: func(a *App) {
				a.openSearchPalette()
			},
		},
		{
			ID:              "clear_search",
			Title:           "Clear search",
			Keywords:        []string{"clear", "reset"},
			ShortcutDisplay: "Esc", // Handled globally via Escape key
			Run: func(a *App) {
				a.setSearchQuery("")
			},
		},
		{
			ID:       "settings",
			Title:    "Settings",
			Keywords: []string{"settings", "config", "preferences"},
			Run: func(a *App) {
				a.ShowSettingsModal()
			},
		},
		{
			ID:       "edit_prompt_templates",
			Title:    "Edit agent prompt templates",
			Keywords: []string{"agent", "prompt", "prompts", "template", "templates"},
			Run: func(a *App) {
				a.ShowPromptTemplatesModal()
			},
		},
		{
			ID:       "sort_updated",
			Title:    "Sort by updated",
			Keywords: []string{"sort", "updated", "recent"},
			// No shortcut - ⌘+1/2/3 conflicts with terminal tab switching
			Run: func(a *App) {
				a.setSortField(SortByUpdatedAt)
			},
		},
		{
			ID:       "sort_created",
			Title:    "Sort by created",
			Keywords: []string{"sort", "created", "new"},
			// No shortcut - ⌘+1/2/3 conflicts with terminal tab switching
			Run: func(a *App) {
				a.setSortField(SortByCreatedAt)
			},
		},
		{
			ID:       "sort_priority",
			Title:    "Sort by priority",
			Keywords: []string{"sort", "priority", "urgent"},
			// No shortcut - ⌘+1/2/3 conflicts with terminal tab switching
			Run: func(a *App) {
				a.setSortField(SortByPriority)
			},
		},
		{
			ID:           "open_browser",
			Title:        "Open in browser",
			Keywords:     []string{"open", "browser", "o", "web"},
			ShortcutRune: 'o',
			Run:          handleOpenBrowserCommand,
		},
		{
			ID:           "copy_id",
			Title:        "Copy issue ID",
			Keywords:     []string{"copy", "id", "c", "identifier"},
			ShortcutRune: 'y',
			Run:          handleCopyIssueIDCommand,
		},
		{
			ID:           "copy_url",
			Title:        "Copy issue URL",
			Keywords:     []string{"copy", "url", "link"},
			ShortcutRune: 'w', // 'w' for web URL
			Run:          handleCopyIssueURLCommand,
		},
		{
			ID:       "ask_agent",
			Title:    "Ask agent about selected issue",
			Keywords: []string{"agent", "ai", "claude", "cursor", "assistant"},
			Run:      handleAskAgent,
		},
		{
			ID:       "set_due_date",
			Title:    "Set due date",
			Keywords: []string{"due", "date", "deadline", "set"},
			Run: func(a *App) {
				a.showSetDueDateModal()
			},
		},
		{
			ID:       "clear_due_date",
			Title:    "Clear due date",
			Keywords: []string{"due", "date", "deadline", "clear", "remove"},
			Run: func(a *App) {
				a.clearDueDateForSelectedIssue()
			},
		},
		{
			ID:       "edit_estimate",
			Title:    "Edit estimate",
			Keywords: []string{"estimate", "points", "edit"},
			Run: func(a *App) {
				a.showEditEstimateModal()
			},
		},
		{
			ID:       "clear_estimate",
			Title:    "Clear estimate",
			Keywords: []string{"estimate", "points", "clear", "remove"},
			Run: func(a *App) {
				a.clearEstimateForSelectedIssue()
			},
		},
		{
			ID:       "list_project_milestones",
			Title:    "List project milestones",
			Keywords: []string{"project", "milestone", "list"},
			Run: func(a *App) {
				a.listProjectMilestonesForSelectedIssue()
			},
		},
		{
			ID:       "set_milestone",
			Title:    "Set milestone",
			Keywords: []string{"project", "milestone", "set"},
			Run: func(a *App) {
				a.showSetMilestonePicker()
			},
		},
		{
			ID:       "clear_milestone",
			Title:    "Clear milestone",
			Keywords: []string{"project", "milestone", "clear", "remove"},
			Run: func(a *App) {
				a.clearMilestoneForSelectedIssue()
			},
		},
		{
			ID:       "filter_issues",
			Title:    "Filter issues",
			Keywords: []string{"filter", "issues", "query"},
			Run: func(a *App) {
				a.showFilterIssuesPicker()
			},
		},
		{
			ID:       "clear_filters",
			Title:    "Clear filters",
			Keywords: []string{"filter", "clear", "reset"},
			Run: func(a *App) {
				a.clearFilters()
			},
		},
		{
			ID:       "filter_assignee",
			Title:    "Filter by assignee",
			Keywords: []string{"filter", "assignee", "user"},
			Run: func(a *App) {
				a.showAssigneeFilter()
			},
		},
		{
			ID:       "filter_labels",
			Title:    "Filter by labels",
			Keywords: []string{"filter", "labels", "tags"},
			Run: func(a *App) {
				a.showLabelFilter()
			},
		},
		{
			ID:       "filter_status",
			Title:    "Filter by status",
			Keywords: []string{"filter", "status", "state"},
			Run: func(a *App) {
				a.showStatusFilter()
			},
		},
		{
			ID:       "filter_project",
			Title:    "Filter by project",
			Keywords: []string{"filter", "project"},
			Run: func(a *App) {
				a.showProjectFilter()
			},
		},
		{
			ID:       "filter_cycle",
			Title:    "Filter by cycle",
			Keywords: []string{"filter", "cycle", "sprint"},
			Run: func(a *App) {
				a.showCycleFilter()
			},
		},
		{
			ID:       "filter_due_date",
			Title:    "Filter by due date",
			Keywords: []string{"filter", "due", "date"},
			Run: func(a *App) {
				a.showDueDateFilter()
			},
		},
		{
			ID:       "filter_estimate",
			Title:    "Filter by estimate",
			Keywords: []string{"filter", "estimate", "points"},
			Run: func(a *App) {
				a.showEstimateFilter()
			},
		},
		{
			ID:       "filter_text",
			Title:    "Filter by text search",
			Keywords: []string{"filter", "text", "search"},
			Run: func(a *App) {
				a.showTextFilter()
			},
		},
		{
			ID:       "add_issue_relation",
			Title:    "Add issue relation",
			Keywords: []string{"relation", "dependency", "blocking", "blocked", "related", "duplicate", "similar"},
			Run: func(a *App) {
				a.showAddIssueRelationPicker()
			},
		},
		{
			ID:       "remove_issue_relation",
			Title:    "Remove issue relation",
			Keywords: []string{"relation", "dependency", "remove", "unlink"},
			Run: func(a *App) {
				a.showRemoveIssueRelationPicker()
			},
		},
		{
			ID:       "subscribe_issue",
			Title:    "Subscribe",
			Keywords: []string{"subscribe", "watch", "subscriber"},
			Run: func(a *App) {
				a.subscribeSelectedIssue()
			},
		},
		{
			ID:       "unsubscribe_issue",
			Title:    "Unsubscribe",
			Keywords: []string{"unsubscribe", "watch", "subscriber"},
			Run: func(a *App) {
				a.unsubscribeSelectedIssue()
			},
		},
		{
			ID:       "open_attachment",
			Title:    "Open attachment",
			Keywords: []string{"attachment", "link", "open", "github", "jira", "slack", "url"},
			Run: func(a *App) {
				a.openSelectedAttachment()
			},
		},
		{
			ID:       "copy_attachment_url",
			Title:    "Copy attachment URL",
			Keywords: []string{"attachment", "link", "copy", "url"},
			Run: func(a *App) {
				a.copySelectedAttachmentURL()
			},
		},
		{
			ID:           "assign_me",
			Title:        "Assign to me",
			Keywords:     []string{"assign", "me", "self", "take"},
			ShortcutRune: 'm',
			Run:          handleAssignMeCommand,
		},
		{
			ID:           "unassign",
			Title:        "Unassign issue",
			Keywords:     []string{"unassign", "remove", "clear assignee"},
			ShortcutRune: 'u',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				emptyAssignee := ""
				go func() {
					ctx := context.Background()
					_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
						ID:         issue.ID,
						AssigneeID: &emptyAssignee,
					})
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "tui.commands: failed to unassign issue issue=%s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("tui.commands: unassigned issue issue=%s", issue.Identifier)
						a.flashStatus(fmt.Sprintf("Unassigned %s", issue.Identifier))
						go a.refreshIssues(issue.ID)
					})
				}()
			},
		},
		{
			ID:           "archive",
			Title:        "Archive issue",
			Keywords:     []string{"archive", "delete", "remove"},
			ShortcutRune: 'x',
			Run: func(a *App) {
				targetIDs := a.effectiveIssueTargetIDs()
				targets := a.resolveCommandTargets(targetIDs)
				if len(targets) == 0 {
					a.flashStatus("No issue selected")
					return
				}
				message := ""
				if len(targets) == 1 {
					message = fmt.Sprintf("Archive %s - %s?", targets[0].Identifier, targets[0].Title)
				} else {
					message = fmt.Sprintf("Archive %d marked issues?", len(targets))
				}
				a.confirmationModal.Show(
					"Archive Issue",
					message,
					"Archive",
					func() {
						archiveIssue := a.archiveIssueFunc
						if archiveIssue == nil {
							archiveIssue = a.GetAPI().ArchiveIssue
						}
						a.runBulkIssueAction("Archive", targetIDs, archiveIssue)
					},
				)
			},
		},
		{
			ID:           "change_status",
			Title:        "Change status",
			Keywords:     []string{"status", "state", "workflow", "todo", "progress", "done"},
			ShortcutRune: 's',
			Run: func(a *App) {
				targetIDs := a.effectiveIssueTargetIDs()
				if len(targetIDs) == 0 {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowStatusPicker(func(stateID string) {
					updateIssue := a.updateIssueFunc
					if updateIssue == nil {
						updateIssue = a.GetAPI().UpdateIssue
					}
					a.runBulkIssueAction("Change status", targetIDs, func(ctx context.Context, issueID string) error {
						_, err := updateIssue(ctx, linearapi.UpdateIssueInput{ID: issueID, StateID: &stateID})
						return err
					})
				})
			},
		},
		{
			ID:           "set_cycle",
			Title:        "Set cycle",
			Keywords:     []string{"cycle", "sprint", "iteration", "set"},
			ShortcutRune: 'c',
			Run: func(a *App) {
				targetIDs := a.effectiveIssueTargetIDs()
				if len(targetIDs) == 0 {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowCyclePicker(func(cycleID string) {
					updateIssue := a.updateIssueFunc
					if updateIssue == nil {
						updateIssue = a.GetAPI().UpdateIssue
					}
					a.runBulkIssueAction("Set cycle", targetIDs, func(ctx context.Context, issueID string) error {
						_, err := updateIssue(ctx, linearapi.UpdateIssueInput{ID: issueID, CycleID: &cycleID})
						return err
					})
				})
			},
		},
		{
			ID:       "clear_cycle",
			Title:    "Clear cycle",
			Keywords: []string{"cycle", "clear", "remove", "unset"},
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				if issue.Cycle == nil {
					a.flashStatus("No cycle assigned")
					return
				}
				emptyCycleID := ""
				go func() {
					ctx := context.Background()
					_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
						ID:      issue.ID,
						CycleID: &emptyCycleID,
					})
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "tui.commands: failed to clear cycle issue=%s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("tui.commands: cleared cycle issue=%s", issue.Identifier)
						a.flashStatus(fmt.Sprintf("Cleared cycle for %s", issue.Identifier))
						go a.refreshIssues(issue.ID)
					})
				}()
			},
		},
		{
			ID:           "assign_user",
			Title:        "Assign to user",
			Keywords:     []string{"assign", "user", "team", "member"},
			ShortcutRune: 'a',
			Run: func(a *App) {
				targetIDs := a.effectiveIssueTargetIDs()
				if len(targetIDs) == 0 {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowUserPicker(func(userID string) {
					updateIssue := a.updateIssueFunc
					if updateIssue == nil {
						updateIssue = a.GetAPI().UpdateIssue
					}
					a.runBulkIssueAction("Assign user", targetIDs, func(ctx context.Context, issueID string) error {
						_, err := updateIssue(ctx, linearapi.UpdateIssueInput{ID: issueID, AssigneeID: &userID})
						return err
					})
				})
			},
		},
		{
			ID:           "create_issue",
			Title:        "Create new issue",
			Keywords:     []string{"create", "new", "add", "issue"},
			ShortcutRune: 'n',
			Run: func(a *App) {
				teamID := a.GetSelectedTeamID()
				if teamID == "" {
					a.updateStatusBarWithError(fmt.Errorf("please select a team first"))
					return
				}
				a.ShowCreateIssueModal()
			},
		},
		{
			ID:       "edit_issue",
			Title:    "Edit issue",
			Keywords: []string{"edit", "issue", "title", "description", "status", "assignee", "priority", "project", "cycle", "labels"},
			Run: func(a *App) {
				if a.GetSelectedIssue() == nil {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowIssueEditorModal()
			},
		},
		{
			ID:           "edit_title",
			Title:        "Edit issue title",
			Keywords:     []string{"edit", "title", "rename"},
			ShortcutRune: 'e',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowEditTitleModal()
			},
		},
		{
			ID:       "edit_labels",
			Title:    "Edit issue labels",
			Keywords: []string{"labels", "label", "tag", "tags"},
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				a.ShowEditLabelsModal()
			},
		},
		{
			ID:       "toggle_sub_issues",
			Title:    "Toggle sub-issues",
			Keywords: []string{"toggle", "expand", "collapse", "sub", "children"},
			// No shortcut - ⌘+T conflicts with new tab. Use Space key in table instead.
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				a.toggleIssueExpanded(issue.ID)
			},
		},
		{
			ID:           "view_parent",
			Title:        "View parent issue",
			Keywords:     []string{"parent", "up", "back"},
			ShortcutRune: 'p',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				if issue.Parent == nil {
					a.flashStatus("No parent issue")
					return
				}
				// Try to navigate to parent in the table
				parentRow := a.getRowForIssue(issue.Parent.ID)
				if parentRow > 0 {
					a.issuesTable.Select(parentRow, 0)
					if parent := a.getIssueFromRow(parentRow); parent != nil {
						a.onIssueSelected(*parent)
					}
				}
			},
		},
		{
			ID:           "expand_all",
			Title:        "Expand all sub-issues",
			Keywords:     []string{"expand", "all", "open"},
			ShortcutRune: ']',
			Run: func(a *App) {
				a.issuesMu.RLock()
				issues := a.issues
				a.issuesMu.RUnlock()
				ExpandAll(a.expandedState, issues)
				// Rebuild rows for both sections
				currentUserID := ""
				if a.currentUser != nil {
					currentUserID = a.currentUser.ID
				}
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

				// Render both tables, preserving selection
				var selectedMyIssueID, selectedOtherIssueID string
				a.issuesMu.RLock()
				selectedIssue := a.selectedIssue
				a.issuesMu.RUnlock()
				if selectedIssue != nil {
					if _, ok := a.myIDToIssue[selectedIssue.ID]; ok {
						selectedMyIssueID = selectedIssue.ID
						a.activeIssuesSection = IssuesSectionMy
					} else if _, ok := a.otherIDToIssue[selectedIssue.ID]; ok {
						selectedOtherIssueID = selectedIssue.ID
						a.activeIssuesSection = IssuesSectionOther
					}
				}

				renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme, &a.markedIssueSelection)
				renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme, &a.markedIssueSelection)
			},
		},
		{
			ID:           "collapse_all",
			Title:        "Collapse all sub-issues",
			Keywords:     []string{"collapse", "all", "close"},
			ShortcutRune: '[',
			Run: func(a *App) {
				CollapseAll(a.expandedState)
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

				// Render both tables, preserving selection
				var selectedMyIssueID, selectedOtherIssueID string
				a.issuesMu.RLock()
				selectedIssue := a.selectedIssue
				a.issuesMu.RUnlock()
				if selectedIssue != nil {
					if _, ok := a.myIDToIssue[selectedIssue.ID]; ok {
						selectedMyIssueID = selectedIssue.ID
						a.activeIssuesSection = IssuesSectionMy
					} else if _, ok := a.otherIDToIssue[selectedIssue.ID]; ok {
						selectedOtherIssueID = selectedIssue.ID
						a.activeIssuesSection = IssuesSectionOther
					}
				}

				renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID, a.theme, &a.markedIssueSelection)
				renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID, a.theme, &a.markedIssueSelection)
			},
		},
		{
			ID:           "create_sub_issue",
			Title:        "Create sub-issue",
			Keywords:     []string{"create", "sub", "child", "new"},
			ShortcutRune: 'b',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				// Create sub-issue with current issue as parent
				a.ShowCreateSubIssueModal(issue.ID)
			},
		},
		{
			ID:           "set_parent",
			Title:        "Set parent issue",
			Keywords:     []string{"set", "parent", "link"},
			ShortcutRune: 'i',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				// Cannot set parent if this issue has children
				if len(issue.Children) > 0 {
					logger.Warning("tui.commands: cannot set parent on issue with sub-issues issue=%s", issue.Identifier)
					a.flashStatus("Cannot set parent on issue with sub-issues")
					return
				}
				a.ShowParentIssuePicker(func(parentID string) {
					go func() {
						ctx := context.Background()
						_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
							ID:       issue.ID,
							ParentID: &parentID,
						})
						a.QueueUpdateDraw(func() {
							if err != nil {
								logger.ErrorWithErr(err, "tui.commands: failed to set parent issue=%s", issue.Identifier)
								a.updateStatusBarWithError(err)
								return
							}
							logger.Info("tui.commands: set parent issue=%s", issue.Identifier)
							a.flashStatus(fmt.Sprintf("Set parent for %s", issue.Identifier))
							go a.refreshIssues(issue.ID)
						})
					}()
				})
			},
		},
		{
			ID:           "remove_parent",
			Title:        "Remove parent",
			Keywords:     []string{"remove", "parent", "unlink", "top"},
			ShortcutRune: 'd',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				if issue.Parent == nil {
					a.flashStatus("No parent issue")
					return
				}
				a.confirmationModal.Show(
					"Remove Parent",
					fmt.Sprintf("Remove parent from %s?", issue.Identifier),
					"Remove",
					func() {
						emptyParent := ""
						go func() {
							ctx := context.Background()
							_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
								ID:       issue.ID,
								ParentID: &emptyParent,
							})
							a.QueueUpdateDraw(func() {
								if err != nil {
									logger.ErrorWithErr(err, "tui.commands: failed to remove parent issue=%s", issue.Identifier)
									a.updateStatusBarWithError(err)
									return
								}
								logger.Info("tui.commands: removed parent issue=%s", issue.Identifier)
								a.flashStatus(fmt.Sprintf("Removed parent from %s", issue.Identifier))
								go a.refreshIssues(issue.ID)
							})
						}()
					},
				)
			},
		},
		{
			ID:           "add_comment",
			Title:        "Add comment",
			Keywords:     []string{"add", "comment", "reply", "t"},
			ShortcutRune: 't',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					a.flashStatus("No issue selected")
					return
				}
				a.createCommentModal.Show(issue.ID, a.handleCreateComment)
			},
		},
	}
	if len(availableProviders) == 0 {
		filtered := make([]Command, 0, len(commands))
		for _, command := range commands {
			if command.ID == "ask_agent" {
				continue
			}
			filtered = append(filtered, command)
		}
		commands = filtered
	}
	if app != nil && app.isTriageContext() {
		commands = append(commands,
			Command{
				ID:       "triage_accept",
				Title:    "Accept triage issue",
				Keywords: []string{"triage", "accept", "status", "workflow"},
				Run: func(a *App) {
					a.runTriageAcceptCommand()
				},
			},
			Command{
				ID:       "triage_duplicate",
				Title:    "Mark triage issue as duplicate",
				Keywords: []string{"triage", "duplicate", "relation", "mark"},
				Run: func(a *App) {
					a.runTriageDuplicateCommand()
				},
			},
			Command{
				ID:       "triage_decline",
				Title:    "Decline triage issue",
				Keywords: []string{"triage", "decline", "archive", "dismiss"},
				Run: func(a *App) {
					a.runTriageDeclineCommand()
				},
			},
			Command{
				ID:       "triage_snooze",
				Title:    "Snooze triage notification",
				Keywords: []string{"triage", "snooze", "notification", "later"},
				Run: func(a *App) {
					a.runTriageSnoozeCommand()
				},
			},
		)
	}

	if app != nil {
		applyCommandKeybindings(commands, app.config.Keybindings)
	}
	return commands
}

// openURL opens a URL in the default browser.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		logger.Warning("tui.commands: unsupported OS for opening URLs os=%s", runtime.GOOS)
		return nil
	}

	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "tui.commands: failed to open URL url=%s", url)
		return err
	}

	logger.Debug("tui.commands: opened URL in browser url=%s", url)
	return nil
}

// copyToClipboard copies text to the system clipboard.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case "windows":
		cmd = exec.Command("clip")
	default:
		logger.Warning("tui.commands: unsupported OS for clipboard operations os=%s", runtime.GOOS)
		return nil
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.ErrorWithErr(err, "tui.commands: failed to get stdin pipe for clipboard command")
		return err
	}

	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "tui.commands: failed to start clipboard command")
		return err
	}

	_, err = stdin.Write([]byte(text))
	if err != nil {
		logger.ErrorWithErr(err, "tui.commands: failed to write to clipboard")
		return err
	}
	_ = stdin.Close()

	if err := cmd.Wait(); err != nil {
		logger.ErrorWithErr(err, "tui.commands: clipboard command failed")
		return err
	}

	logger.Debug("tui.commands: copied to clipboard text_length=%d", len(text))
	return nil
}
