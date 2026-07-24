package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

// applyDefaultNavigation selects the configured default team (and optionally
// project) once teams have loaded. It must run off the UI goroutine; UI
// mutations are queued. Missing teams or projects log a warning and flash the
// status bar, leaving the standard "All Issues" selection in place.
func (a *App) applyDefaultNavigation(ctx context.Context, teams []linearapi.Team) bool {
	teamQuery := strings.TrimSpace(a.config.DefaultTeam)
	if teamQuery == "" {
		return false
	}

	team := findTeamByKeyOrName(teams, teamQuery)
	if team == nil {
		logger.Warning("tui.app: default team not found team=%q", teamQuery)
		a.queueUpdateDraw(func() {
			a.flashStatus(fmt.Sprintf("Default team %q not found", teamQuery))
		})
		return false
	}

	projects, projectsErr := a.fetchProjectsFunc(ctx, team.ID)
	states, statesErr := a.fetchWorkflowStatesFunc(ctx, team.ID)
	cycles, cyclesErr := a.fetchCyclesFunc(ctx, team.ID)
	childrenLoaded := projectsErr == nil && statesErr == nil && cyclesErr == nil
	if !childrenLoaded {
		logger.Warning("tui.app: failed to load default team children team_id=%s projects_err=%v states_err=%v cycles_err=%v", team.ID, projectsErr, statesErr, cyclesErr)
	}

	var project *linearapi.Project
	projectQuery := strings.TrimSpace(a.config.DefaultProject)
	if projectQuery != "" {
		if projectsErr != nil {
			logger.Warning("tui.app: failed to load default project team_id=%s project=%q error=%v", team.ID, projectQuery, projectsErr)
			a.queueUpdateDraw(func() {
				a.flashStatus(fmt.Sprintf("Could not load default project %q", projectQuery))
			})
		} else {
			project = findProjectByName(projects, projectQuery)
			switch {
			case project == nil:
				logger.Warning("tui.app: default project not found team_id=%s project=%q", team.ID, projectQuery)
				a.queueUpdateDraw(func() {
					a.flashStatus(fmt.Sprintf("Default project %q not found", projectQuery))
				})
			case !childrenLoaded:
				logger.Warning("tui.app: could not apply default project after partial child load failure team_id=%s project=%q", team.ID, projectQuery)
				a.queueUpdateDraw(func() {
					a.flashStatus(fmt.Sprintf("Could not load default project %q", projectQuery))
				})
			}
		}
	}

	a.queueUpdateDraw(func() {
		teamNode := a.findTeamTreeNode(team.ID)
		if teamNode == nil {
			go a.refreshIssues()
			return
		}
		// Leave children unpopulated on fetch errors so expanding the team retries.
		if childrenLoaded && len(teamNode.GetChildren()) == 0 {
			a.populateTeamNodeChildren(teamNode, team.ID, projects, states, cycles)
		}
		if len(teamNode.GetChildren()) > 0 {
			teamNode.SetExpanded(true)
		}

		target := teamNode
		if project != nil {
			if projectNode := findProjectTreeNode(teamNode, project.ID); projectNode != nil {
				target = projectNode
			}
		}
		a.navigationTree.SetCurrentNode(target)
		nav, ok := target.GetReference().(*NavigationNode)
		if !ok {
			go a.refreshIssues()
			return
		}
		a.onNavigationSelected(nav)
	})
	return true
}

// findTeamTreeNode returns the tree node for a team ID, or nil if absent.
func (a *App) findTeamTreeNode(teamID string) *tview.TreeNode {
	root := a.navigationTree.GetRoot()
	if root == nil {
		return nil
	}
	for _, child := range root.GetChildren() {
		if nav, ok := child.GetReference().(*NavigationNode); ok && nav.IsTeam && nav.TeamID == teamID {
			return child
		}
	}
	return nil
}

// findProjectTreeNode returns the child node for a project ID, or nil if absent.
func findProjectTreeNode(teamNode *tview.TreeNode, projectID string) *tview.TreeNode {
	for _, child := range teamNode.GetChildren() {
		if nav, ok := child.GetReference().(*NavigationNode); ok && nav.IsProject && nav.ID == projectID {
			return child
		}
	}
	return nil
}

// findTeamByKeyOrName returns the team whose key or name matches the query
// (case-insensitive, whitespace-trimmed), or nil if no team matches.
func findTeamByKeyOrName(teams []linearapi.Team, query string) *linearapi.Team {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	for i := range teams {
		if strings.EqualFold(teams[i].Key, query) || strings.EqualFold(teams[i].Name, query) {
			return &teams[i]
		}
	}
	return nil
}

// findProjectByName returns the project whose name matches the query
// (case-insensitive, whitespace-trimmed), or nil if no project matches.
func findProjectByName(projects []linearapi.Project, query string) *linearapi.Project {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	for i := range projects {
		if strings.EqualFold(projects[i].Name, query) {
			return &projects[i]
		}
	}
	return nil
}
