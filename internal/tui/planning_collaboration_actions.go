package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

var issueRelationTypeLabels = []PickerItem{
	{ID: "blocking", Label: "blocking"},
	{ID: "blocked by", Label: "blocked by"},
	{ID: "related", Label: "related"},
	{ID: "duplicate", Label: "duplicate"},
	{ID: "similar", Label: "similar"},
}

func validateLinearDate(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("date is required")
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value {
		return fmt.Errorf("date must be YYYY-MM-DD")
	}
	return nil
}

func parseEstimateInput(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("estimate is required")
	}
	estimate, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("estimate must be numeric")
	}
	if estimate < 0 {
		return 0, fmt.Errorf("estimate must be non-negative")
	}
	return estimate, nil
}

func (a *App) runIssueUpdate(input linearapi.UpdateIssueInput, successMessage string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	input.ID = issue.ID
	updateIssue := a.updateIssueFunc
	if updateIssue == nil {
		updateIssue = a.api.UpdateIssue
	}
	go func(issueID string) {
		_, err := updateIssue(context.Background(), input)
		a.QueueUpdateDraw(func() {
			if err != nil {
				logger.ErrorWithErr(err, "tui.planning: issue update failed issue=%s", issue.Identifier)
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus(successMessage)
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) showSetDueDateModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	initial := ""
	if issue.DueDate != nil {
		initial = *issue.DueDate
	}
	a.textInputModal.Show("Set Due Date", "YYYY-MM-DD: ", initial, func(value string) {
		if err := validateLinearDate(value); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.setDueDateForSelectedIssue(value)
	})
}

func (a *App) setDueDateForSelectedIssue(value string) {
	value = strings.TrimSpace(value)
	if err := validateLinearDate(value); err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.runIssueUpdate(linearapi.UpdateIssueInput{DueDate: &value}, "Updated due date")
}

func (a *App) clearDueDateForSelectedIssue() {
	empty := ""
	a.runIssueUpdate(linearapi.UpdateIssueInput{DueDate: &empty}, "Cleared due date")
}

func (a *App) showEditEstimateModal() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	initial := ""
	if issue.Estimate != nil {
		initial = formatEstimate(issue.Estimate)
	}
	a.textInputModal.Show("Edit Estimate", "Points: ", initial, func(value string) {
		a.editEstimateForSelectedIssue(value)
	})
}

func (a *App) editEstimateForSelectedIssue(value string) {
	estimate, err := parseEstimateInput(value)
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	a.runIssueUpdate(linearapi.UpdateIssueInput{Estimate: &estimate}, "Updated estimate")
}

func (a *App) clearEstimateForSelectedIssue() {
	a.runIssueUpdate(linearapi.UpdateIssueInput{ClearEstimate: true}, "Cleared estimate")
}

func (a *App) selectedIssueProjectID() (string, bool) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return "", false
	}
	if strings.TrimSpace(issue.ProjectID) == "" {
		a.updateStatusBarWithError(fmt.Errorf("issue must have a project"))
		return "", false
	}
	return issue.ProjectID, true
}

func (a *App) showProjectMilestonePicker(title string, onSelect func(linearapi.ProjectMilestone)) {
	projectID, ok := a.selectedIssueProjectID()
	if !ok {
		return
	}
	go func() {
		milestones, err := a.cache.GetProjectMilestones(context.Background(), projectID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			if len(milestones) == 0 {
				a.flashStatus("No project milestones available")
				return
			}
			items := make([]PickerItem, 0, len(milestones))
			byID := make(map[string]linearapi.ProjectMilestone, len(milestones))
			for _, milestone := range milestones {
				label := milestone.Name
				if milestone.TargetDate != nil && *milestone.TargetDate != "" {
					label += " (" + *milestone.TargetDate + ")"
				}
				items = append(items, PickerItem{ID: milestone.ID, Label: label})
				byID[milestone.ID] = milestone
			}
			a.pickerActive = true
			a.pickerModal.Show(title, items, func(item PickerItem) {
				a.pickerActive = false
				if onSelect != nil {
					onSelect(byID[item.ID])
				}
			})
		})
	}()
}

func (a *App) listProjectMilestonesForSelectedIssue() {
	a.showProjectMilestonePicker("Project Milestones", func(milestone linearapi.ProjectMilestone) {
		a.flashStatus(fmt.Sprintf("Milestone: %s", milestone.Name))
	})
}

func (a *App) showSetMilestonePicker() {
	a.showProjectMilestonePicker("Set Milestone", func(milestone linearapi.ProjectMilestone) {
		milestoneID := milestone.ID
		a.runIssueUpdate(linearapi.UpdateIssueInput{ProjectMilestoneID: &milestoneID}, "Set milestone")
	})
}

func (a *App) clearMilestoneForSelectedIssue() {
	empty := ""
	a.runIssueUpdate(linearapi.UpdateIssueInput{ProjectMilestoneID: &empty}, "Cleared milestone")
}

func (a *App) applyFiltersAndRefresh(message string) {
	a.flashStatus(message)
	go a.refreshIssues()
}

func (a *App) clearFilters() {
	a.richFilters = IssueFilters{}
	a.searchQuery = ""
	a.applyFiltersAndRefresh("Cleared filters")
}

func (a *App) showFilterIssuesPicker() {
	items := []PickerItem{
		{ID: "assignee", Label: "Assignee"},
		{ID: "labels", Label: "Labels"},
		{ID: "status", Label: "Status"},
		{ID: "project", Label: "Project"},
		{ID: "cycle", Label: "Cycle"},
		{ID: "due", Label: "Due date"},
		{ID: "estimate", Label: "Estimate"},
		{ID: "text", Label: "Text search"},
		{ID: "clear", Label: "Clear filters"},
	}
	a.pickerActive = true
	a.pickerModal.Show("Filter Issues", items, func(item PickerItem) {
		a.pickerActive = false
		switch item.ID {
		case "assignee":
			a.showAssigneeFilter()
		case "labels":
			a.showLabelFilter()
		case "status":
			a.showStatusFilter()
		case "project":
			a.showProjectFilter()
		case "cycle":
			a.showCycleFilter()
		case "due":
			a.showDueDateFilter()
		case "estimate":
			a.showEstimateFilter()
		case "text":
			a.showTextFilter()
		case "clear":
			a.clearFilters()
		}
	})
}

func (a *App) showAssigneeFilter() {
	a.ShowUserPicker(func(userID string) {
		a.richFilters.AssigneeID = userID
		a.richFilters.AssigneeName = userDisplayNameByID(a.teamUsers, userID)
		a.applyFiltersAndRefresh("Applied assignee filter")
	})
}

func (a *App) showLabelFilter() {
	teamID := a.GetSelectedTeamID()
	if teamID == "" {
		a.updateStatusBarWithError(fmt.Errorf("team context is required"))
		return
	}
	go func() {
		labels, err := a.cache.GetIssueLabels(context.Background(), teamID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			items := make([]MultiSelectItem, 0, len(labels))
			labelByID := make(map[string]string, len(labels))
			for _, label := range labels {
				items = append(items, MultiSelectItem{ID: label.ID, Label: label.Name})
				labelByID[label.ID] = label.Name
			}
			a.multiSelectModal.Show("Filter Labels", items, a.richFilters.LabelIDs, func(ids []string) {
				a.richFilters.LabelIDs = ids
				a.richFilters.LabelNames = namesForIDs(ids, labelByID)
				a.applyFiltersAndRefresh("Applied label filters")
			})
		})
	}()
}

func (a *App) showStatusFilter() {
	a.ShowStatusPicker(func(stateID string) {
		a.richFilters.StateID = stateID
		a.richFilters.StateName = workflowStateNameByID(a.workflowStates, stateID)
		a.applyFiltersAndRefresh("Applied status filter")
	})
}

// showProjectFilter is an alias for the project scope picker, kept so the
// "Filter by project" palette entry and the filter-issues sub-picker route to
// the same cross-team, type-to-filter project picker.
func (a *App) showProjectFilter() {
	a.showProjectScopePicker()
}

// favoriteProjectIDs returns the IDs of the viewer's favorite projects.
func (a *App) favoriteProjectIDs() []string {
	if len(a.favoriteProjects) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.favoriteProjects))
	for _, p := range a.favoriteProjects {
		ids = append(ids, p.ID)
	}
	return ids
}

// toggleMyIssuesOnly switches between showing only the current user's issues and
// showing issues for all assignees, then refreshes.
func (a *App) toggleMyIssuesOnly() {
	a.myIssuesOnly = !a.myIssuesOnly
	if a.myIssuesOnly {
		a.applyFiltersAndRefresh("Showing my issues")
	} else {
		a.applyFiltersAndRefresh("Showing all assignees")
	}
}

// showProjectScopePicker scopes the issue list to a single favorite project (or
// "All Favourite Projects") via a type-to-filter picker. This is the primary
// navigation control now that the navigation tree is gone.
func (a *App) showProjectScopePicker() {
	if len(a.favoriteProjects) > 0 {
		a.presentProjectScopePicker(a.favoriteProjects)
		return
	}
	a.flashStatus("Loading favorite projects...")
	go func() {
		projects, err := a.api.ListFavoriteProjects(context.Background())
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.favoriteProjects = projects
			a.presentProjectScopePicker(projects)
		})
	}()
}

func (a *App) presentProjectScopePicker(projects []linearapi.Project) {
	items := make([]PickerItem, 0, len(projects)+1)
	items = append(items, PickerItem{ID: "", Label: "All Favourite Projects"})
	projectNames := make(map[string]string, len(projects))
	projectTeams := make(map[string]string, len(projects))
	for _, project := range projects {
		items = append(items, PickerItem{ID: project.ID, Label: project.Name})
		projectNames[project.ID] = project.Name
		projectTeams[project.ID] = project.TeamID
	}
	a.filterPickerModal.Show("Select Project", items, func(item PickerItem) {
		a.setProjectScope(item.ID, projectNames[item.ID], projectTeams[item.ID])
	})
}

// setProjectScope applies (or clears, when projectID is empty) the project
// filter and loads the project's team metadata so team-dependent commands
// (status, labels, cycle, create-issue) have context.
func (a *App) setProjectScope(projectID, projectName, teamID string) {
	a.richFilters.ProjectID = projectID
	a.richFilters.ProjectName = projectName
	a.selectedProjectTeamID = teamID
	if projectID == "" {
		a.applyFiltersAndRefresh("Showing all favourite projects")
		return
	}
	if teamID != "" && a.loadTeamMetadataFunc != nil {
		go a.loadTeamMetadataFunc(teamID)
	}
	a.applyFiltersAndRefresh(fmt.Sprintf("Scoped to project: %s", projectName))
}

// loadTeamMetadata loads users, projects, workflow states, and cycles for a team
// so team-scoped commands and pickers have data to work with.
func (a *App) loadTeamMetadata(teamID string) {
	ctx := context.Background()
	_ = a.cache.PreloadTeamMetadata(ctx, teamID)
	users, _ := a.cache.GetUsers(ctx, teamID)
	projects, _ := a.cache.GetProjects(ctx, teamID)
	states, _ := a.cache.GetWorkflowStates(ctx, teamID)
	cycles, _ := a.cache.GetCycles(ctx, teamID)
	a.QueueUpdateDraw(func() {
		a.teamUsers = users
		a.teamProjects = projects
		a.workflowStates = states
		a.teamCycles = cycles
	})
}

func (a *App) showCycleFilter() {
	a.ShowCyclePicker(func(cycleID string) {
		a.richFilters.CycleID = cycleID
		a.richFilters.CycleName = cycleNameByID(a.teamCycles, cycleID)
		a.applyFiltersAndRefresh("Applied cycle filter")
	})
}

func (a *App) showDueDateFilter() {
	initial := formatDateFilterSummary(a.richFilters.DueDate)
	a.textInputModal.Show("Filter Due Date", "YYYY-MM-DD: ", initial, func(value string) {
		if err := validateLinearDate(value); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.richFilters.DueDate = linearapi.DateFilter{Eq: value}
		a.applyFiltersAndRefresh("Applied due date filter")
	})
}

func (a *App) showEstimateFilter() {
	initial := formatNumberFilterSummary(a.richFilters.Estimate)
	a.textInputModal.Show("Filter Estimate", "Points: ", initial, func(value string) {
		estimate, err := parseEstimateInput(value)
		if err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.richFilters.Estimate = linearapi.NumberFilter{Eq: &estimate}
		a.applyFiltersAndRefresh("Applied estimate filter")
	})
}

func (a *App) showTextFilter() {
	a.textInputModal.Show("Filter Text", "Search: ", a.searchQuery, func(value string) {
		a.searchQuery = strings.TrimSpace(value)
		a.applyFiltersAndRefresh("Applied text filter")
	})
}

func userDisplayNameByID(users []linearapi.User, id string) string {
	for _, user := range users {
		if user.ID == id {
			return formatUserDisplayName(user)
		}
	}
	return id
}

func workflowStateNameByID(states []linearapi.WorkflowState, id string) string {
	for _, state := range states {
		if state.ID == id {
			return state.Name
		}
	}
	return id
}

func cycleNameByID(cycles []linearapi.Cycle, id string) string {
	for _, cycle := range cycles {
		if cycle.ID == id {
			return cycle.DisplayName()
		}
	}
	return id
}

func namesForIDs(ids []string, names map[string]string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if name := names[id]; name != "" {
			result = append(result, name)
			continue
		}
		result = append(result, id)
	}
	return result
}

func relationInputForIssue(issueID, label, targetIssueID string) (linearapi.CreateIssueRelationInput, error) {
	label = strings.ToLower(strings.TrimSpace(label))
	targetIssueID = strings.TrimSpace(targetIssueID)
	if issueID == "" {
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("no issue selected")
	}
	if targetIssueID == "" {
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("related issue ID is required")
	}
	switch label {
	case "blocking":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationBlocks}, nil
	case "blocked by":
		return linearapi.CreateIssueRelationInput{IssueID: targetIssueID, RelatedIssueID: issueID, Type: linearapi.IssueRelationBlocks}, nil
	case "related":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationRelated}, nil
	case "duplicate":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationDuplicate}, nil
	case "similar":
		return linearapi.CreateIssueRelationInput{IssueID: issueID, RelatedIssueID: targetIssueID, Type: linearapi.IssueRelationSimilar}, nil
	default:
		return linearapi.CreateIssueRelationInput{}, fmt.Errorf("unsupported relation type %q", label)
	}
}

func (a *App) showAddIssueRelationPicker() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	a.pickerActive = true
	a.pickerModal.Show("Relation Type", issueRelationTypeLabels, func(item PickerItem) {
		a.pickerActive = false
		a.textInputModal.Show("Related Issue", "Issue ID: ", "", func(targetIssueID string) {
			a.createIssueRelationForSelectedIssue(item.ID, targetIssueID)
		})
	})
}

func (a *App) createIssueRelationForSelectedIssue(label, targetIssueID string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	input, err := relationInputForIssue(issue.ID, label, targetIssueID)
	if err != nil {
		a.updateStatusBarWithError(err)
		return
	}
	createRelation := a.createIssueRelationFunc
	if createRelation == nil {
		createRelation = a.api.CreateIssueRelation
	}
	go func(issueID string) {
		_, err := createRelation(context.Background(), input)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Added issue relation")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) showRemoveIssueRelationPicker() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	if len(issue.Relations) == 0 {
		a.flashStatus("No issue relations")
		return
	}
	items := make([]PickerItem, 0, len(issue.Relations))
	for _, relation := range issue.Relations {
		ref := relation.RelatedIssue
		if relation.Inverse {
			ref = relation.Issue
		}
		items = append(items, PickerItem{
			ID:    relation.ID,
			Label: relation.DisplayType() + " " + formatIssueReference(ref),
		})
	}
	a.pickerActive = true
	a.pickerModal.Show("Remove Relation", items, func(item PickerItem) {
		a.pickerActive = false
		a.deleteIssueRelationForSelectedIssue(item.ID)
	})
}

func (a *App) deleteIssueRelationForSelectedIssue(relationID string) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	deleteRelation := a.deleteIssueRelationFunc
	if deleteRelation == nil {
		deleteRelation = a.api.DeleteIssueRelation
	}
	go func(issueID string) {
		err := deleteRelation(context.Background(), relationID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Removed issue relation")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) subscribeSelectedIssue() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	subscribe := a.subscribeIssueFunc
	if subscribe == nil {
		subscribe = a.api.SubscribeToIssue
	}
	go func(issueID string) {
		_, err := subscribe(context.Background(), issueID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Subscribed to issue")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) unsubscribeSelectedIssue() {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	unsubscribe := a.unsubscribeIssueFunc
	if unsubscribe == nil {
		unsubscribe = a.api.UnsubscribeFromIssue
	}
	go func(issueID string) {
		_, err := unsubscribe(context.Background(), issueID)
		a.QueueUpdateDraw(func() {
			if err != nil {
				a.updateStatusBarWithError(err)
				return
			}
			a.flashStatus("Unsubscribed from issue")
			go a.refreshIssues(issueID)
		})
	}(issue.ID)
}

func (a *App) openSelectedAttachment() {
	a.runAttachmentAction("Open Attachment", func(attachment linearapi.Attachment) {
		openFn := a.openURLFunc
		if openFn == nil {
			openFn = openURL
		}
		if err := openFn(attachment.URL); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.flashStatus("Opened attachment")
	})
}

func (a *App) copySelectedAttachmentURL() {
	a.runAttachmentAction("Copy Attachment URL", func(attachment linearapi.Attachment) {
		copyFn := a.copyToClipboardFunc
		if copyFn == nil {
			copyFn = copyToClipboard
		}
		if err := copyFn(attachment.URL); err != nil {
			a.updateStatusBarWithError(err)
			return
		}
		a.flashStatus("Copied attachment URL")
	})
}

func (a *App) runAttachmentAction(title string, action func(linearapi.Attachment)) {
	issue := a.GetSelectedIssue()
	if issue == nil {
		a.flashStatus("No issue selected")
		return
	}
	attachments := issue.Attachments
	if len(attachments) == 0 {
		a.flashStatus("No attachments")
		return
	}
	if len(attachments) == 1 {
		action(attachments[0])
		return
	}
	byID := make(map[string]linearapi.Attachment, len(attachments))
	items := make([]PickerItem, 0, len(attachments))
	for _, attachment := range attachments {
		label := attachment.Title
		if label == "" {
			label = attachment.URL
		}
		if attachment.SourceType != "" {
			label += " (" + attachment.SourceType + ")"
		}
		byID[attachment.ID] = attachment
		items = append(items, PickerItem{ID: attachment.ID, Label: label})
	}
	a.pickerActive = true
	a.pickerModal.Show(title, items, func(item PickerItem) {
		a.pickerActive = false
		action(byID[item.ID])
	})
}
