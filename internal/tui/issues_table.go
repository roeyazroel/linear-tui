package tui

import (
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// Tree icons for expand/collapse indicators.
const (
	IconExpanded    = "▼"
	IconCollapsed   = "▶"
	IconChildPrefix = "└─"
)

// formatPriority formats a priority value into a display string with icon and label.
// Linear priority: 0 = No priority, 1 = Urgent, 2 = High, 3 = Normal, 4 = Low.
func formatPriority(priority int, theme Theme) (string, tcell.Color) {
	switch priority {
	case 1:
		return Icons.Priority + " Urgent", theme.StatusCanceled // Red for urgent
	case 2:
		return Icons.Priority + " High", theme.StatusInProgress // Yellow for high
	case 3:
		return Icons.Priority + " Normal", theme.Foreground // Default for normal
	case 4:
		return Icons.Priority + " Low", theme.SecondaryText // Gray for low
	default:
		return "-", theme.SecondaryText // No priority
	}
}

// getIssueFromRow returns the issue for a given table row (accounting for header).
// Returns nil if the row is invalid.
// This is a convenience wrapper that uses the current app's issueRows and idToIssue.
func (a *App) getIssueFromRow(row int) *linearapi.Issue {
	return getIssueFromRowModel(row, a.issueRows, a.idToIssue)
}

// getRowForIssue returns the table row for a given issue ID.
// Returns -1 if not found.
// This is a convenience wrapper that uses the current app's issueRows.
func (a *App) getRowForIssue(issueID string) int {
	return getRowForIssueModel(issueID, a.issueRows)
}

// getIssueFromRowModel returns the issue for a given table row using the provided model.
// Returns nil if the row is invalid.
func getIssueFromRowModel(row int, rows []IssueRow, idToIssue map[string]*linearapi.Issue) *linearapi.Issue {
	rowIndex := row - 1 // Account for header row
	if rowIndex < 0 || rowIndex >= len(rows) {
		return nil
	}
	issueID := rows[rowIndex].IssueID
	if issue, ok := idToIssue[issueID]; ok {
		return issue
	}
	return nil
}

// getRowForIssueModel returns the table row for a given issue ID using the provided model.
// Returns -1 if not found.
func getRowForIssueModel(issueID string, rows []IssueRow) int {
	for i, row := range rows {
		if row.IssueID == issueID {
			return i + 1 // +1 for header row
		}
	}
	return -1
}

// IssuesSection represents which issues section is active.
type IssuesSection int

const (
	IssuesSectionMy IssuesSection = iota
	IssuesSectionOther
)

// buildIssuesTable creates and configures an issues table widget with the given title.
// The table will use the provided getIssue and getRow functions for lookups.
func (a *App) buildIssuesTable(title string, section IssuesSection) *tview.Table {
	table := tview.NewTable()
	table.SetBorders(false). // Remove cell borders for cleaner look
					SetSelectable(true, false).
					SetBorder(true).
					SetTitle(title).
					SetTitleColor(a.theme.Foreground).
					SetBorderColor(a.theme.Border).
					SetBackgroundColor(a.theme.Background)

	table.SetSelectedStyle(tcell.StyleDefault.
		Foreground(a.theme.SelectionText).
		Background(a.theme.SelectionBg).
		Bold(true))

	// Set column headers with better styling
	headerStyle := tcell.StyleDefault.
		Foreground(a.theme.HeaderText).
		Background(a.theme.HeaderBg).
		Bold(true)

	table.SetCell(0, 0, tview.NewTableCell(" ID").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 1, tview.NewTableCell("State").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 2, tview.NewTableCell("Priority").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 3, tview.NewTableCell("Assignee").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 4, tview.NewTableCell("Cycle").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 5, tview.NewTableCell("Due").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 6, tview.NewTableCell("Est").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 7, tview.NewTableCell("Milestone").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 8, tview.NewTableCell("Title").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(6))

	// Set fixed column widths
	table.SetFixed(1, 0)

	// Handle selection (Enter to open details or toggle expand)
	table.SetSelectedFunc(func(row, _ int) {
		issue := a.getIssueFromRowForSection(row, section)
		if issue == nil {
			return
		}

		// Enter always opens/focuses details. Expansion is deliberately bound to
		// Space so parent rows do not have a surprising dual action.
		a.onIssueSelected(*issue)
		a.focusedPane = FocusDetails
		a.updateFocus()
	})

	// Set up keyboard navigation with cross-section support
	a.setupIssuesTableNavigation(table, section)

	return table
}

// setupIssuesTableNavigation sets up keyboard navigation for an issues table with cross-section support.
func (a *App) setupIssuesTableNavigation(table *tview.Table, section IssuesSection) {
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if a.handleIssueSelectionKey(table, section, event) {
			return nil
		}
		switch event.Key() {
		case tcell.KeyRune:
			switch event.Rune() {
			case 'j':
				row, _ := table.GetSelection()
				if row < table.GetRowCount()-1 {
					table.Select(row+1, 0)
					if issue := a.getIssueFromRowForSection(row+1, section); issue != nil {
						a.onIssueSelected(*issue)
						a.activeIssuesSection = section
					}
				} else if section == IssuesSectionMy && len(a.otherIssueRows) > 0 {
					// At bottom of this section - try to move to next section
					// Move to Other Issues table
					a.activeIssuesSection = IssuesSectionOther
					a.otherIssuesTable.Select(1, 0)
					if issue := a.getIssueFromRowForSection(1, IssuesSectionOther); issue != nil {
						a.onIssueSelected(*issue)
					}
					a.updateFocus()
				}
				return nil
			case 'k':
				row, _ := table.GetSelection()
				if row > 1 {
					table.Select(row-1, 0)
					if issue := a.getIssueFromRowForSection(row-1, section); issue != nil {
						a.onIssueSelected(*issue)
						a.activeIssuesSection = section
					}
				} else if section == IssuesSectionOther && len(a.myIssueRows) > 0 {
					// At top of this section - try to move to previous section
					// Move to My Issues table
					a.activeIssuesSection = IssuesSectionMy
					lastRow := len(a.myIssueRows)
					a.myIssuesTable.Select(lastRow, 0)
					if issue := a.getIssueFromRowForSection(lastRow, IssuesSectionMy); issue != nil {
						a.onIssueSelected(*issue)
					}
					a.updateFocus()
				}
				return nil
			case 'g':
				// Go to top of current section
				table.Select(1, 0)
				if issue := a.getIssueFromRowForSection(1, section); issue != nil {
					a.onIssueSelected(*issue)
					a.activeIssuesSection = section
				}
				return nil
			case 'G':
				// Go to bottom of current section
				var rows []IssueRow
				switch section {
				case IssuesSectionMy:
					rows = a.myIssueRows
				case IssuesSectionOther:
					rows = a.otherIssueRows
				}
				if len(rows) > 0 {
					lastRow := len(rows)
					table.Select(lastRow, 0)
					if issue := a.getIssueFromRowForSection(lastRow, section); issue != nil {
						a.onIssueSelected(*issue)
						a.activeIssuesSection = section
					}
				}
				return nil
			case 'l':
				// Expand current parent issue
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRowForSection(row, section); issue != nil {
					if len(issue.Children) > 0 && !a.expandedState[issue.ID] {
						a.toggleIssueExpanded(issue.ID)
						a.activeIssuesSection = section
					}
				}
				return nil
			case 'h':
				// Collapse current parent issue, or go to parent if on child
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRowForSection(row, section); issue != nil {
					if len(issue.Children) > 0 && a.expandedState[issue.ID] {
						// Collapse this parent
						a.toggleIssueExpanded(issue.ID)
						a.activeIssuesSection = section
					} else if issue.Parent != nil {
						// Navigate to parent - may be in different section
						parentRow := a.getRowForIssueInSection(issue.Parent.ID, IssuesSectionMy)
						if parentRow > 0 {
							a.activeIssuesSection = IssuesSectionMy
							a.myIssuesTable.Select(parentRow, 0)
							if parent := a.getIssueFromRowForSection(parentRow, IssuesSectionMy); parent != nil {
								a.onIssueSelected(*parent)
							}
							a.updateFocus()
						} else {
							parentRow = a.getRowForIssueInSection(issue.Parent.ID, IssuesSectionOther)
							if parentRow > 0 {
								a.activeIssuesSection = IssuesSectionOther
								a.otherIssuesTable.Select(parentRow, 0)
								if parent := a.getIssueFromRowForSection(parentRow, IssuesSectionOther); parent != nil {
									a.onIssueSelected(*parent)
								}
								a.updateFocus()
							}
						}
					}
				}
				return nil
			case ' ':
				// Space toggles expand/collapse
				row, _ := table.GetSelection()
				if issue := a.getIssueFromRowForSection(row, section); issue != nil {
					if len(issue.Children) > 0 {
						a.toggleIssueExpanded(issue.ID)
						a.activeIssuesSection = section
					}
				}
				return nil
			}
		case tcell.KeyEnter:
			row, _ := table.GetSelection()
			issue := a.getIssueFromRowForSection(row, section)
			if issue == nil {
				return nil
			}

			// Enter always opens/focuses details; Space handles expansion.
			a.onIssueSelected(*issue)
			a.focusedPane = FocusDetails
			a.updateFocus()
			return nil
		case tcell.KeyDown:
			row, _ := table.GetSelection()
			if row < table.GetRowCount()-1 {
				table.Select(row+1, 0)
				if issue := a.getIssueFromRowForSection(row+1, section); issue != nil {
					a.onIssueSelected(*issue)
					a.activeIssuesSection = section
				}
			} else if section == IssuesSectionMy && len(a.otherIssueRows) > 0 {
				// At bottom - try to move to next section
				a.activeIssuesSection = IssuesSectionOther
				a.otherIssuesTable.Select(1, 0)
				if issue := a.getIssueFromRowForSection(1, IssuesSectionOther); issue != nil {
					a.onIssueSelected(*issue)
				}
				a.updateFocus()
			}
			return nil
		case tcell.KeyUp:
			row, _ := table.GetSelection()
			if row > 1 {
				table.Select(row-1, 0)
				if issue := a.getIssueFromRowForSection(row-1, section); issue != nil {
					a.onIssueSelected(*issue)
					a.activeIssuesSection = section
				}
			} else if section == IssuesSectionOther && len(a.myIssueRows) > 0 {
				// At top - try to move to previous section
				a.activeIssuesSection = IssuesSectionMy
				lastRow := len(a.myIssueRows)
				a.myIssuesTable.Select(lastRow, 0)
				if issue := a.getIssueFromRowForSection(lastRow, IssuesSectionMy); issue != nil {
					a.onIssueSelected(*issue)
				}
				a.updateFocus()
			}
			return nil
		}
		return event
	})
}

func (a *App) handleIssueSelectionKey(table *tview.Table, section IssuesSection, event *tcell.EventKey) bool {
	if event.Key() == tcell.KeyCtrlA {
		a.selectAllVisibleIssues()
		return true
	}
	if event.Key() != tcell.KeyRune || (event.Rune() != 'v' && event.Rune() != 'V') {
		return false
	}
	row, _ := table.GetSelection()
	issue := a.getIssueFromRowForSection(row, section)
	if issue == nil {
		return true
	}
	if event.Rune() == 'v' {
		a.markedIssueSelection.Toggle(issue.ID)
	} else {
		a.markedIssueSelection.ExtendRange(a.visibleIssueIDs(section), issue.ID)
	}
	a.refreshIssueTableMarks()
	return true
}

// getIssueFromRowForSection returns the issue for a given table row in the specified section.
func (a *App) getIssueFromRowForSection(row int, section IssuesSection) *linearapi.Issue {
	var rows []IssueRow
	var idToIssue map[string]*linearapi.Issue
	switch section {
	case IssuesSectionMy:
		rows = a.myIssueRows
		idToIssue = a.myIDToIssue
	case IssuesSectionOther:
		rows = a.otherIssueRows
		idToIssue = a.otherIDToIssue
	}
	return getIssueFromRowModel(row, rows, idToIssue)
}

// getRowForIssueInSection returns the table row for a given issue ID in the specified section.
func (a *App) getRowForIssueInSection(issueID string, section IssuesSection) int {
	var rows []IssueRow
	switch section {
	case IssuesSectionMy:
		rows = a.myIssueRows
	case IssuesSectionOther:
		rows = a.otherIssueRows
	}
	return getRowForIssueModel(issueID, rows)
}

// activeIssuesTable returns the table and section receiving issue-pane keys.
func (a *App) activeIssuesTable() (*tview.Table, IssuesSection) {
	if a.activeIssuesSection == IssuesSectionMy && a.myIssuesTable != nil {
		return a.myIssuesTable, IssuesSectionMy
	}
	if a.otherIssuesTable != nil {
		return a.otherIssuesTable, IssuesSectionOther
	}
	if a.myIssuesTable != nil {
		return a.myIssuesTable, IssuesSectionMy
	}
	return nil, a.activeIssuesSection
}

func (a *App) cursorIssueID(section IssuesSection) string {
	table, _ := a.activeIssuesTable()
	if table == nil {
		return ""
	}
	row, _ := table.GetSelection()
	issue := a.getIssueFromRowForSection(row, section)
	if issue == nil {
		return ""
	}
	return issue.ID
}

func (a *App) visibleIssueIDs(section IssuesSection) []string {
	var rows []IssueRow
	switch section {
	case IssuesSectionMy:
		rows = a.myIssueRows
	case IssuesSectionOther:
		rows = a.otherIssueRows
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.IssueID != "" {
			ids = append(ids, row.IssueID)
		}
	}
	return ids
}

func (a *App) toggleMarkedCursorIssue() {
	_, section := a.activeIssuesTable()
	issueID := a.cursorIssueID(section)
	if issueID == "" {
		return
	}
	a.markedIssueSelection.Toggle(issueID)
	a.refreshIssueTableMarks()
}

func (a *App) extendMarkedCursorIssue() {
	_, section := a.activeIssuesTable()
	issueID := a.cursorIssueID(section)
	if issueID == "" {
		return
	}
	a.markedIssueSelection.ExtendRange(a.visibleIssueIDs(section), issueID)
	a.refreshIssueTableMarks()
}

func (a *App) selectAllVisibleIssues() {
	_, section := a.activeIssuesTable()
	ids := append([]string(nil), a.visibleIssueIDs(IssuesSectionMy)...)
	ids = append(ids, a.visibleIssueIDs(IssuesSectionOther)...)
	if len(ids) == 0 {
		return
	}
	// section is intentionally read above to keep activeIssuesTable's fallback
	// behavior consistent when both sections are empty.
	_ = section
	a.markedIssueSelection.SelectAllVisible(ids)
	a.refreshIssueTableMarks()
}

// refreshIssueTableMarks redraws both sections so a mark remains visible even
// when the cursor is moved between the two tables.
func (a *App) refreshIssueTableMarks() {
	if a.myIssuesTable != nil {
		renderIssuesTableModel(a.myIssuesTable, a.myIssueRows, a.myIDToIssue, a.selectedIssueID(IssuesSectionMy), a.theme, &a.markedIssueSelection)
	}
	if a.otherIssuesTable != nil {
		renderIssuesTableModel(a.otherIssuesTable, a.otherIssueRows, a.otherIDToIssue, a.selectedIssueID(IssuesSectionOther), a.theme, &a.markedIssueSelection)
	}
	a.updateStatusBar()
}

// renderIssuesTableModel renders a table with the given rows and issue lookup map.
func renderIssuesTableModel(table *tview.Table, rows []IssueRow, idToIssue map[string]*linearapi.Issue, selectedIssueID string, theme Theme, selections ...*MarkedIssueSelection) {
	table.Clear()
	var marked *MarkedIssueSelection
	if len(selections) > 0 {
		marked = selections[0]
	}

	// Set column headers with better styling
	headerStyle := tcell.StyleDefault.
		Foreground(theme.HeaderText).
		Background(theme.HeaderBg).
		Bold(true)

	table.SetCell(0, 0, tview.NewTableCell(" ID").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 1, tview.NewTableCell("State").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 2, tview.NewTableCell("Priority").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 3, tview.NewTableCell("Assignee").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 4, tview.NewTableCell("Cycle").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 5, tview.NewTableCell("Due").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 6, tview.NewTableCell("Est").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(1))
	table.SetCell(0, 7, tview.NewTableCell("Milestone").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(2))
	table.SetCell(0, 8, tview.NewTableCell("Title").
		SetStyle(headerStyle).
		SetAlign(tview.AlignLeft).
		SetSelectable(false).
		SetExpansion(6))

	// Add issue rows using the hierarchical structure
	for i, issueRow := range rows {
		row := i + 1

		issue, ok := idToIssue[issueRow.IssueID]
		if !ok || issue == nil {
			continue
		}

		// Build identifier with hierarchy indicator
		identifier := issue.Identifier
		identifierPrefix := " "

		if issueRow.Level > 0 {
			// Child issue - show indent prefix
			identifierPrefix = " " + IconChildPrefix + " "
		} else if issueRow.HasChildren {
			// Parent issue - show expand/collapse indicator
			if issueRow.IsExpanded {
				identifierPrefix = " " + IconExpanded + " "
			} else {
				identifierPrefix = " " + IconCollapsed + " "
			}
		}

		marker := "   "
		if marked != nil && marked.IsMarked(issue.ID) {
			marker = "(x)"
		}
		table.SetCell(row, 0, tview.NewTableCell(marker+identifierPrefix+identifier).
			SetTextColor(theme.SecondaryText).
			SetAlign(tview.AlignLeft))

		// State with color based on state
		state := issue.State
		var stateColor tcell.Color
		var stateIcon string

		// Color code states
		lowerState := strings.ToLower(state)
		switch {
		case strings.Contains(lowerState, "done") || strings.Contains(lowerState, "complete"):
			stateColor = theme.StatusDone
			stateIcon = Icons.Done
		case strings.Contains(lowerState, "progress"):
			stateColor = theme.StatusInProgress
			stateIcon = Icons.InProgress
		case strings.Contains(lowerState, "cancel"):
			stateColor = theme.StatusCanceled
			stateIcon = Icons.Done
		default:
			stateColor = theme.StatusTodo
			stateIcon = Icons.Todo
		}

		if len(state) > 12 {
			state = state[:12]
		}

		table.SetCell(row, 1, tview.NewTableCell(stateIcon+" "+state).
			SetTextColor(stateColor).
			SetAlign(tview.AlignLeft))

		// Priority
		priorityText, priorityColor := formatPriority(issue.Priority, theme)
		table.SetCell(row, 2, tview.NewTableCell(priorityText).
			SetTextColor(priorityColor).
			SetAlign(tview.AlignLeft))

		// Assignee
		assignee := issue.Assignee
		assigneeColor := theme.Foreground
		if assignee == "" {
			assignee = "Unassigned"
			assigneeColor = theme.SecondaryText
		}
		if len(assignee) > 15 {
			assignee = assignee[:15]
		}

		table.SetCell(row, 3, tview.NewTableCell(assignee).
			SetTextColor(assigneeColor).
			SetAlign(tview.AlignLeft))

		cycle := formatCycleName(issue.Cycle)
		cycleColor := theme.Foreground
		if cycle == "-" {
			cycleColor = theme.SecondaryText
		}
		if len(cycle) > 15 {
			cycle = cycle[:15]
		}
		table.SetCell(row, 4, tview.NewTableCell(cycle).
			SetTextColor(cycleColor).
			SetAlign(tview.AlignLeft))

		dueDate := formatDueDate(issue.DueDate)
		dueColor := theme.Foreground
		if dueDate == "-" {
			dueColor = theme.SecondaryText
		}
		table.SetCell(row, 5, tview.NewTableCell(dueDate).
			SetTextColor(dueColor).
			SetAlign(tview.AlignLeft))

		estimate := formatEstimate(issue.Estimate)
		estimateColor := theme.Foreground
		if estimate == "-" {
			estimateColor = theme.SecondaryText
		}
		table.SetCell(row, 6, tview.NewTableCell(estimate).
			SetTextColor(estimateColor).
			SetAlign(tview.AlignLeft))

		milestone := formatMilestoneName(issue.ProjectMilestone)
		milestoneColor := theme.Foreground
		if milestone == "-" {
			milestoneColor = theme.SecondaryText
		}
		if len(milestone) > 18 {
			milestone = milestone[:18]
		}
		table.SetCell(row, 7, tview.NewTableCell(milestone).
			SetTextColor(milestoneColor).
			SetAlign(tview.AlignLeft))

		// Title
		title := issue.Title
		table.SetCell(row, 8, tview.NewTableCell(title).
			SetTextColor(theme.Foreground).
			SetAlign(tview.AlignLeft))
	}

	// Select the specified issue or first row
	if len(rows) > 0 {
		selectedRow := 1 // Default to first issue (row 1, row 0 is header)
		if selectedIssueID != "" {
			// Find the row with matching issue ID
			for i, row := range rows {
				if row.IssueID == selectedIssueID {
					selectedRow = i + 1 // +1 because row 0 is header
					break
				}
			}
		}
		table.Select(selectedRow, 0)
	} else {
		// Show empty state message
		table.SetCell(1, 0, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 1, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 2, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 3, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 4, tview.NewTableCell("No issues").
			SetTextColor(theme.SecondaryText).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
		table.SetCell(1, 5, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 6, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 7, tview.NewTableCell("").SetSelectable(false))
		table.SetCell(1, 8, tview.NewTableCell("").SetSelectable(false))
	}
}

func formatCycleName(cycle *linearapi.CycleRef) string {
	if cycle == nil {
		return "-"
	}
	return cycle.DisplayName()
}

func formatDueDate(dueDate *string) string {
	if dueDate == nil || strings.TrimSpace(*dueDate) == "" {
		return "-"
	}
	return strings.TrimSpace(*dueDate)
}

func formatEstimate(estimate *float64) string {
	if estimate == nil {
		return "-"
	}
	return strconv.FormatFloat(*estimate, 'f', -1, 64)
}

func formatMilestoneName(milestone *linearapi.ProjectMilestoneRef) string {
	if milestone == nil || strings.TrimSpace(milestone.Name) == "" {
		return "-"
	}
	return milestone.Name
}

// renderIssueRow formats an issue for display in the table.
// This is a helper function that can be used for testing.
func renderIssueRow(issue linearapi.Issue) []string {
	identifier := issue.Identifier
	if len(identifier) > 10 {
		identifier = identifier[:10]
	}

	state := issue.State
	if len(state) > 10 {
		state = state[:10]
	}

	priorityText, _ := formatPriority(issue.Priority, LinearTheme)

	assignee := issue.Assignee
	if assignee == "" {
		assignee = "Unassigned"
	}
	if len(assignee) > 10 {
		assignee = assignee[:10]
	}

	cycle := formatCycleName(issue.Cycle)
	if len(cycle) > 10 {
		cycle = cycle[:10]
	}

	dueDate := formatDueDate(issue.DueDate)
	estimate := formatEstimate(issue.Estimate)
	milestone := formatMilestoneName(issue.ProjectMilestone)
	if len(milestone) > 10 {
		milestone = milestone[:10]
	}

	return []string{identifier, state, priorityText, assignee, cycle, dueDate, estimate, milestone, issue.Title}
}
