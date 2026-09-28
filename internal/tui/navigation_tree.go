package tui

import (
	"github.com/rivo/tview"
)

// NavigationNode represents a node in the navigation tree.
type NavigationNode struct {
	ID        string
	Text      string
	TeamID    string // For team, project, and status nodes
	Children  []*NavigationNode
	IsTeam    bool
	IsProject bool
	IsStatus  bool
	IsCycle   bool
	IsIssue   bool
	StateID   string
	StateName string
	CycleID   string
	CycleName string
	IssueID   string
	// CustomViewID makes the node show a Linear custom view's issues.
	CustomViewID string
	// StateType filters by workflow state type (e.g. triage), scoped to
	// TeamID when set.
	StateType string
	// IsFolder marks a favorites folder; selecting it toggles expansion.
	IsFolder bool
}

// buildNavigationTree creates and configures the navigation tree widget.
func (a *App) buildNavigationTree() *tview.TreeView {
	tree := tview.NewTreeView()

	// Create initial root with "Loading..." placeholder
	root := tview.NewTreeNode("Linear").
		SetColor(a.theme.Accent).
		SetSelectable(false)

	loadingNode := tview.NewTreeNode("Loading teams...").
		SetColor(a.theme.SecondaryText).
		SetSelectable(false)
	root.AddChild(loadingNode)

	tree.SetBorder(true).
		SetTitle(" Navigation ").
		SetTitleColor(a.theme.Foreground).
		SetBorderColor(a.theme.Border)
	tree.SetBackgroundColor(a.theme.Background)
	tree.SetRoot(root)
	tree.SetCurrentNode(root)

	// Handle selection for all nodes (teams, projects, and "All Issues")
	tree.SetSelectedFunc(func(node *tview.TreeNode) {
		ref := node.GetReference()
		if ref != nil {
			if navNode, ok := ref.(*NavigationNode); ok {
				// Folders only expand and collapse.
				if navNode.IsFolder {
					node.SetExpanded(!node.IsExpanded())
					return
				}
				// For team nodes, handle expand/collapse
				if navNode.IsTeam {
					a.onTeamExpanded(navNode.TeamID, node)
				}
				// Update selection and refresh issues
				a.onNavigationSelected(navNode)
				// Selecting a view moves focus to the issues list.
				a.focusedPane = FocusIssues
				a.updateFocus()
			}
		}
	})

	return tree
}
