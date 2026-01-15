package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// GetPlatformModifier returns the appropriate modifier mask for the current OS.
// On macOS, it returns ModMeta (Cmd key), on other platforms it returns ModCtrl.
func GetPlatformModifier() tcell.ModMask {
	if runtime.GOOS == "darwin" {
		return tcell.ModMeta
	}
	return tcell.ModCtrl
}

// FormatShortcut returns a human-readable string for a shortcut.
// On macOS it uses ⌘+, on other platforms it uses Ctrl+.
func FormatShortcut(r rune) string {
	if r == 0 {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("⌘ + %s", strings.ToUpper(string(r)))
	}
	return fmt.Sprintf("Ctrl + %s", strings.ToUpper(string(r)))
}

// Command represents a command that can be executed from the palette.
type Command struct {
	ID           string
	Title        string
	Keywords     []string
	ShortcutRune rune // The rune for the keyboard shortcut (e.g., 'r' for Cmd+R/Ctrl+R)
	Run          func(a *App)
}

// CommandContext provides context for command execution.
type CommandContext struct {
	SelectedIssue *linearapi.Issue
}

// DefaultCommands returns the default set of commands for the palette.
func DefaultCommands(app *App) []Command {
	return []Command{
		{
			ID:           "refresh",
			Title:        "Refresh issues",
			Keywords:     []string{"refresh", "reload", "r"},
			ShortcutRune: 'r',
			Run: func(a *App) {
				go a.refreshIssues()
			},
		},
		{
			ID:       "search",
			Title:    "Search issues",
			Keywords: []string{"search", "find", "s", "/"},
			// No shortcut - use '/' key directly instead (⌘+F conflicts with terminal find)
			Run: func(a *App) {
				a.openSearchPalette()
			},
		},
		{
			ID:           "clear_search",
			Title:        "Clear search",
			Keywords:     []string{"clear", "reset"},
			ShortcutRune: 'k',
			Run: func(a *App) {
				a.setSearchQuery("")
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
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil || issue.URL == "" {
					return
				}
				_ = openURL(issue.URL)
			},
		},
		{
			ID:           "copy_id",
			Title:        "Copy issue ID",
			Keywords:     []string{"copy", "id", "c", "identifier"},
			ShortcutRune: 'y',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					return
				}
				_ = copyToClipboard(issue.Identifier)
			},
		},
		{
			ID:       "copy_url",
			Title:    "Copy issue URL",
			Keywords: []string{"copy", "url", "link"},
			// No shortcut - ⌘+W conflicts with close tab
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil || issue.URL == "" {
					return
				}
				_ = copyToClipboard(issue.URL)
			},
		},
		{
			ID:           "assign_me",
			Title:        "Assign to me",
			Keywords:     []string{"assign", "me", "self", "take"},
			ShortcutRune: 'm',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				user := a.GetCurrentUser()
				if issue == nil || user == nil {
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
							logger.ErrorWithErr(err, "Failed to assign issue %s to user %s", issue.Identifier, user.DisplayName)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("Assigned issue %s to %s", issue.Identifier, user.DisplayName)
						go a.refreshIssues(issue.ID)
					})
				}()
			},
		},
		{
			ID:           "unassign",
			Title:        "Unassign issue",
			Keywords:     []string{"unassign", "remove", "clear assignee"},
			ShortcutRune: 'u',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
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
							logger.ErrorWithErr(err, "Failed to unassign issue %s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("Unassigned issue %s", issue.Identifier)
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
				issue := a.GetSelectedIssue()
				if issue == nil {
					return
				}
				go func() {
					ctx := context.Background()
					err := a.GetAPI().ArchiveIssue(ctx, issue.ID)
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "Failed to archive issue %s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("Archived issue %s", issue.Identifier)
						// After archiving, the issue won't be in the list, so just refresh without ID
						go a.refreshIssues()
					})
				}()
			},
		},
		{
			ID:           "change_status",
			Title:        "Change status",
			Keywords:     []string{"status", "state", "workflow", "todo", "progress", "done"},
			ShortcutRune: 's',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					return
				}
				a.ShowStatusPicker(func(stateID string) {
					go func() {
						ctx := context.Background()
						_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
							ID:      issue.ID,
							StateID: &stateID,
						})
						a.QueueUpdateDraw(func() {
							if err != nil {
								logger.ErrorWithErr(err, "Failed to change status for issue %s", issue.Identifier)
								a.updateStatusBarWithError(err)
								return
							}
							logger.Info("Changed status for issue %s", issue.Identifier)
							go a.refreshIssues(issue.ID)
						})
					}()
				})
			},
		},
		{
			ID:           "assign_user",
			Title:        "Assign to user",
			Keywords:     []string{"assign", "user", "team", "member"},
			ShortcutRune: 'a',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					return
				}
				a.ShowUserPicker(func(userID string) {
					go func() {
						ctx := context.Background()
						_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
							ID:         issue.ID,
							AssigneeID: &userID,
						})
						a.QueueUpdateDraw(func() {
							if err != nil {
								logger.ErrorWithErr(err, "Failed to assign issue %s to user", issue.Identifier)
								a.updateStatusBarWithError(err)
								return
							}
							logger.Info("Assigned issue %s to user", issue.Identifier)
							go a.refreshIssues(issue.ID)
						})
					}()
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
			ID:           "edit_title",
			Title:        "Edit issue title",
			Keywords:     []string{"edit", "title", "rename"},
			ShortcutRune: 'e',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
					return
				}
				a.ShowEditTitleModal()
			},
		},
		{
			ID:           "edit_labels",
			Title:        "Edit issue labels",
			Keywords:     []string{"labels", "label", "tag", "tags", "l"},
			ShortcutRune: 'l',
			Run: func(a *App) {
				issue := a.GetSelectedIssue()
				if issue == nil {
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
				if issue == nil || issue.Parent == nil {
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
				ExpandAll(a.expandedState, a.issues)
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

				// Render both tables, preserving selection
				var selectedMyIssueID, selectedOtherIssueID string
				if a.selectedIssue != nil {
					if _, ok := a.myIDToIssue[a.selectedIssue.ID]; ok {
						selectedMyIssueID = a.selectedIssue.ID
						a.activeIssuesSection = IssuesSectionMy
					} else if _, ok := a.otherIDToIssue[a.selectedIssue.ID]; ok {
						selectedOtherIssueID = a.selectedIssue.ID
						a.activeIssuesSection = IssuesSectionOther
					}
				}

				renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID)
				renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID)
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

				// Render both tables, preserving selection
				var selectedMyIssueID, selectedOtherIssueID string
				if a.selectedIssue != nil {
					if _, ok := a.myIDToIssue[a.selectedIssue.ID]; ok {
						selectedMyIssueID = a.selectedIssue.ID
						a.activeIssuesSection = IssuesSectionMy
					} else if _, ok := a.otherIDToIssue[a.selectedIssue.ID]; ok {
						selectedOtherIssueID = a.selectedIssue.ID
						a.activeIssuesSection = IssuesSectionOther
					}
				}

				renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, selectedMyIssueID)
				renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, selectedOtherIssueID)
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
					return
				}
				// Cannot set parent if this issue has children
				if len(issue.Children) > 0 {
					logger.Warning("Cannot set parent on issue %s that has sub-issues", issue.Identifier)
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
								logger.ErrorWithErr(err, "Failed to set parent for issue %s", issue.Identifier)
								a.updateStatusBarWithError(err)
								return
							}
							logger.Info("Set parent for issue %s", issue.Identifier)
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
				if issue == nil || issue.Parent == nil {
					return
				}
				emptyParent := ""
				go func() {
					ctx := context.Background()
					_, err := a.GetAPI().UpdateIssue(ctx, linearapi.UpdateIssueInput{
						ID:       issue.ID,
						ParentID: &emptyParent,
					})
					a.QueueUpdateDraw(func() {
						if err != nil {
							logger.ErrorWithErr(err, "Failed to remove parent for issue %s", issue.Identifier)
							a.updateStatusBarWithError(err)
							return
						}
						logger.Info("Removed parent for issue %s", issue.Identifier)
						go a.refreshIssues(issue.ID)
					})
				}()
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
					return
				}
				a.createCommentModal.Show(issue.ID, a.handleCreateComment)
			},
		},
	}
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
		logger.Warning("Unsupported OS for opening URLs: %s", runtime.GOOS)
		return nil
	}

	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "Failed to open URL: %s", url)
		return err
	}

	logger.Debug("Opened URL in browser: %s", url)
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
		logger.Warning("Unsupported OS for clipboard operations: %s", runtime.GOOS)
		return nil
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.ErrorWithErr(err, "Failed to get stdin pipe for clipboard command")
		return err
	}

	if err := cmd.Start(); err != nil {
		logger.ErrorWithErr(err, "Failed to start clipboard command")
		return err
	}

	_, err = stdin.Write([]byte(text))
	if err != nil {
		logger.ErrorWithErr(err, "Failed to write to clipboard")
		return err
	}
	_ = stdin.Close()

	if err := cmd.Wait(); err != nil {
		logger.ErrorWithErr(err, "Clipboard command failed")
		return err
	}

	logger.Debug("Copied to clipboard: %s", text)
	return nil
}
