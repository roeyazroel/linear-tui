package tui

import (
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// favoriteLeafNode maps one favorite onto a navigation node, or nil for
// favorite types the navigation tree cannot display (labels, documents, ...).
func favoriteLeafNode(favorite linearapi.Favorite) *NavigationNode {
	switch favorite.Type {
	case "issue":
		if favorite.IssueID == "" {
			return nil
		}
		return &NavigationNode{
			ID:      favorite.IssueID,
			Text:    favorite.IssueIdentifier + " " + favorite.IssueTitle,
			TeamID:  favorite.IssueTeamID,
			IsIssue: true,
			IssueID: favorite.IssueID,
		}
	case "project":
		if favorite.ProjectID == "" {
			return nil
		}
		return &NavigationNode{
			ID:        favorite.ProjectID,
			Text:      favorite.ProjectName,
			TeamID:    favorite.ProjectTeamID,
			IsProject: true,
		}
	case "cycle":
		if favorite.CycleID == "" {
			return nil
		}
		name := linearapi.CycleRef{Name: favorite.CycleName, Number: favorite.CycleNumber}.DisplayName()
		return &NavigationNode{
			ID:        favorite.CycleID,
			Text:      name,
			TeamID:    favorite.CycleTeamID,
			IsCycle:   true,
			CycleID:   favorite.CycleID,
			CycleName: name,
		}
	case "team":
		if favorite.TeamID == "" {
			return nil
		}
		return &NavigationNode{
			ID:     favorite.TeamID,
			Text:   favorite.TeamName,
			TeamID: favorite.TeamID,
			IsTeam: true,
		}
	case "customView":
		if favorite.CustomViewID == "" {
			return nil
		}
		label := favorite.CustomViewName
		if favorite.Title != "" {
			label = favorite.Title
		}
		return &NavigationNode{
			ID:           favorite.CustomViewID,
			Text:         label,
			CustomViewID: favorite.CustomViewID,
		}
	case "predefinedView":
		switch favorite.PredefinedViewType {
		case string(KeySequenceDestinationTriage):
			label := favorite.Title
			if label == "" {
				label = "Triage"
			}
			return &NavigationNode{
				ID:        favorite.ID,
				Text:      label,
				TeamID:    favorite.PredefinedViewTeamID,
				StateType: string(KeySequenceDestinationTriage),
			}
		case "allIssues":
			label := favorite.Title
			if label == "" {
				label = "All Issues"
			}
			return &NavigationNode{ID: "all", Text: label}
		default:
			logger.Debug("tui.favorites: skipping unsupported predefined view type=%s id=%s", favorite.PredefinedViewType, favorite.ID)
			return nil
		}
	default:
		logger.Debug("tui.favorites: skipping unsupported favorite type=%s id=%s", favorite.Type, favorite.ID)
		return nil
	}
}

// favoriteNavigationNodes maps favorites onto navigation nodes, nesting
// favorites inside their Linear folders.
func favoriteNavigationNodes(favorites []linearapi.Favorite) []*NavigationNode {
	// Folders first, so children find their parent regardless of order.
	folders := make(map[string]*NavigationNode)
	for _, favorite := range favorites {
		if favorite.Type != "folder" {
			continue
		}
		label := favorite.Title
		if label == "" {
			label = favorite.FolderName
		}
		folders[favorite.ID] = &NavigationNode{
			ID:       favorite.ID,
			Text:     label,
			IsFolder: true,
		}
	}

	var roots []*NavigationNode
	place := func(favorite linearapi.Favorite, node *NavigationNode) {
		if parent, ok := folders[favorite.ParentID]; ok && parent != node {
			parent.Children = append(parent.Children, node)
			return
		}
		roots = append(roots, node)
	}

	for _, favorite := range favorites {
		if favorite.Type == "folder" {
			place(favorite, folders[favorite.ID])
			continue
		}
		if node := favoriteLeafNode(favorite); node != nil {
			place(favorite, node)
		}
	}
	return roots
}

// appendFavoritesSection adds a Favorites group under the navigation root.
// Favorites are additive: with no displayable favorites the section is omitted.
func (a *App) appendFavoritesSection(root *tview.TreeNode, favorites []linearapi.Favorite) {
	nodes := favoriteNavigationNodes(favorites)
	if len(nodes) == 0 {
		return
	}

	group := tview.NewTreeNode("Favorites").
		SetColor(a.theme.Accent).
		SetSelectable(false).
		SetExpanded(true)
	a.addFavoriteNodes(group, nodes)
	root.AddChild(group)
}

// addFavoriteNodes renders favorite navigation nodes under a tree node,
// recursing into folders.
func (a *App) addFavoriteNodes(parent *tview.TreeNode, nodes []*NavigationNode) {
	for _, navNode := range nodes {
		color := a.theme.Foreground
		if navNode.IsFolder {
			color = a.theme.SecondaryText
		}
		child := tview.NewTreeNode(navNode.Text).
			SetColor(color).
			SetReference(navNode).
			SetExpanded(true)
		if len(navNode.Children) > 0 {
			a.addFavoriteNodes(child, navNode.Children)
		}
		parent.AddChild(child)
	}
}
