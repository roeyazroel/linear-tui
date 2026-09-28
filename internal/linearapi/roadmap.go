package linearapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/logger"
	"github.com/shurcooL/graphql"
)

// ProjectUpdateHealthType is Linear's health enum for project updates.
type ProjectUpdateHealthType string

// GetGraphQLType returns the schema enum type used by project update inputs.
func (ProjectUpdateHealthType) GetGraphQLType() string {
	return "ProjectUpdateHealthType"
}

const (
	ProjectUpdateHealthOnTrack  ProjectUpdateHealthType = "onTrack"
	ProjectUpdateHealthAtRisk   ProjectUpdateHealthType = "atRisk"
	ProjectUpdateHealthOffTrack ProjectUpdateHealthType = "offTrack"
)

// Initiative is a Linear initiative and its lightweight project membership.
type Initiative struct {
	ID          string
	Name        string
	Description string
	Status      string
	TargetDate  string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Projects    []Project
}

// ProjectUpdate is a status update posted to a Linear project.
type ProjectUpdate struct {
	ID        string
	Body      string
	Health    string
	CreatedAt time.Time
	UpdatedAt time.Time
	ProjectID string
	Author    User
}

// InitiativeStatus is Linear's schema enum for initiative status values.
// The enum names are case-sensitive and intentionally match the public
// GraphQL schema rather than display labels.
type InitiativeStatus string

// GetGraphQLType returns the schema enum type used by initiative inputs.
func (InitiativeStatus) GetGraphQLType() string {
	return "InitiativeStatus"
}

const (
	InitiativeStatusProposed  InitiativeStatus = "Proposed"
	InitiativeStatusPlanned   InitiativeStatus = "Planned"
	InitiativeStatusActive    InitiativeStatus = "Active"
	InitiativeStatusCompleted InitiativeStatus = "Completed"
	InitiativeStatusCanceled  InitiativeStatus = "Canceled"
)

func validInitiativeStatus(status InitiativeStatus) bool {
	switch status {
	case "", InitiativeStatusProposed, InitiativeStatusPlanned, InitiativeStatusActive, InitiativeStatusCompleted, InitiativeStatusCanceled:
		return true
	default:
		return false
	}
}

// CreateProjectInput contains the schema-confirmed fields supported by the
// roadmap project editor. TeamIDs is preferred when several teams are needed;
// TeamID is a convenience for the common single-team case.
type CreateProjectInput struct {
	ID                   string
	Name                 string
	TeamID               string
	TeamIDs              []string
	Icon                 string
	Color                string
	StatusID             string
	Description          string
	Content              string
	LeadID               string
	LeadTeamID           string
	MemberIDs            []string
	StartDate            string
	StartDateResolution  string
	TargetDate           string
	TargetDateResolution string
	TemplateID           string
	Priority             *float64
}

// UpdateProjectInput contains optional project patch fields. A nil pointer
// omits a field; a non-nil empty pointer explicitly clears nullable fields.
// TeamIDs follows the same rule, so an empty slice clears all team links.
type UpdateProjectInput struct {
	ID                   string
	Name                 *string
	TeamID               *string
	TeamIDs              *[]string
	Icon                 *string
	Color                *string
	StatusID             *string
	Description          *string
	Content              *string
	LeadID               *string
	LeadTeamID           *string
	MemberIDs            *[]string
	StartDate            *string
	StartDateResolution  *string
	TargetDate           *string
	TargetDateResolution *string
	TemplateID           *string
	Priority             *float64
	Trashed              *bool
}

// CreateInitiativeInput contains schema-confirmed fields supported by the
// roadmap initiative editor. Name is required by Linear.
type CreateInitiativeInput struct {
	ID                   string
	Name                 string
	Description          string
	OwnerID              string
	LeadTeamID           string
	SortOrder            *float64
	Color                string
	Icon                 string
	Status               InitiativeStatus
	TargetDate           string
	TargetDateResolution string
	Content              string
	Priority             *float64
	LabelIDs             []string
}

// CreateInitiativeToProjectInput contains the required IDs for associating an
// existing project with an initiative. SortOrder is optional and is omitted
// when nil.
type CreateInitiativeToProjectInput struct {
	InitiativeID string
	ProjectID    string
	SortOrder    *float64
}

// UpdateInitiativeInput contains optional initiative patch fields. Nil means
// omit; a non-nil empty pointer clears nullable string/list fields.
type UpdateInitiativeInput struct {
	ID                   string
	Name                 *string
	Description          *string
	OwnerID              *string
	LeadTeamID           *string
	SortOrder            *float64
	Color                *string
	Icon                 *string
	Status               *InitiativeStatus
	TargetDate           *string
	TargetDateResolution *string
	Content              *string
	Priority             *float64
	LabelIDs             *[]string
	Trashed              *bool
}

// ProjectCreateInput is the schema-named GraphQL input object used by
// projectCreate. It remains map-backed so the variable type emitted by
// shurcooL/graphql exactly matches Linear's public schema.
type ProjectCreateInput map[string]interface{}

func (ProjectCreateInput) GetGraphQLType() string { return "ProjectCreateInput" }

func (input ProjectCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// ProjectUpdateInput is the schema-named GraphQL input object used by
// projectUpdate.
type ProjectUpdateInput map[string]interface{}

func (ProjectUpdateInput) GetGraphQLType() string { return "ProjectUpdateInput" }

func (input ProjectUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// InitiativeCreateInput is the schema-named GraphQL input object used by
// initiativeCreate.
type InitiativeCreateInput map[string]interface{}

func (InitiativeCreateInput) GetGraphQLType() string { return "InitiativeCreateInput" }

func (input InitiativeCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// InitiativeToProjectCreateInput is the schema-named GraphQL input object
// used by initiativeToProjectCreate.
type InitiativeToProjectCreateInput map[string]interface{}

func (InitiativeToProjectCreateInput) GetGraphQLType() string {
	return "InitiativeToProjectCreateInput"
}

func (input InitiativeToProjectCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// InitiativeUpdateInput is the schema-named GraphQL input object used by
// initiativeUpdate.
type InitiativeUpdateInput map[string]interface{}

func (InitiativeUpdateInput) GetGraphQLType() string { return "InitiativeUpdateInput" }

func (input InitiativeUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// CreateProjectUpdateInput contains the schema-confirmed fields used when
// creating a project update. ProjectID and Body are required by this client
// workflow; Health is optional and omitted when empty.
type CreateProjectUpdateInput struct {
	ProjectID string
	Body      string
	Health    ProjectUpdateHealthType
}

// UpdateProjectUpdateInput contains the schema-confirmed fields used when
// updating a project update. ID and Body are required by this client workflow;
// Health is optional and omitted when empty.
type UpdateProjectUpdateInput struct {
	ID     string
	Body   string
	Health ProjectUpdateHealthType
}

// ProjectUpdateFilter is a custom input type for Linear's
// ProjectUpdateFilter GraphQL input object.
type ProjectUpdateFilter map[string]interface{}

// GetGraphQLType returns the GraphQL input type name for the filter.
func (ProjectUpdateFilter) GetGraphQLType() string {
	return "ProjectUpdateFilter"
}

// MarshalJSON implements json.Marshaler for ProjectUpdateFilter.
func (f ProjectUpdateFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(f))
}

// ProjectUpdateCreateInput is the schema-confirmed GraphQL input object used
// by projectUpdateCreate. Callers normally use the typed
// CreateProjectUpdateInput above, while this map type preserves the exact
// GraphQL variable type name required by shurcooL/graphql.
type ProjectUpdateCreateInput map[string]interface{}

func (ProjectUpdateCreateInput) GetGraphQLType() string {
	return "ProjectUpdateCreateInput"
}

func (i ProjectUpdateCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// ProjectUpdateUpdateInput is the schema-confirmed GraphQL input object used
// by projectUpdateUpdate. Callers normally use the typed
// UpdateProjectUpdateInput above, while this map type preserves the exact
// GraphQL variable type name required by shurcooL/graphql.
type ProjectUpdateUpdateInput map[string]interface{}

func (ProjectUpdateUpdateInput) GetGraphQLType() string {
	return "ProjectUpdateUpdateInput"
}

func (i ProjectUpdateUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

type roadmapPageInfo struct {
	HasNextPage graphql.Boolean
	EndCursor   *graphql.String
}

type initiativeProjectNode struct {
	ID          graphql.String
	Name        graphql.String
	Description *graphql.String
	Teams       struct {
		Nodes []struct {
			ID graphql.String
		}
	} `graphql:"teams(first: $teamsFirst)"`
}

type initiativeProjectsConnection struct {
	Nodes    []initiativeProjectNode
	PageInfo roadmapPageInfo
}

type initiativeNode struct {
	ID          graphql.String
	Name        graphql.String
	Description *graphql.String
	Status      graphql.String
	TargetDate  *graphql.String
	CreatedAt   graphql.String
	UpdatedAt   graphql.String
	Projects    initiativeProjectsConnection `graphql:"projects(first: $projectsFirst, after: $projectsAfter)"`
}

type projectMutationNode struct {
	ID          graphql.String
	Name        graphql.String
	Description *graphql.String
	Teams       struct {
		Nodes []struct {
			ID graphql.String
		}
	}
}

type initiativeMutationNode struct {
	ID          graphql.String
	Name        graphql.String
	Description *graphql.String
	Status      graphql.String
	TargetDate  *graphql.String
}

type projectUpdateNode struct {
	ID        graphql.String
	Body      graphql.String
	Health    graphql.String
	CreatedAt graphql.String
	UpdatedAt graphql.String
	Project   struct {
		ID   graphql.String
		Name graphql.String
	}
	User struct {
		ID          graphql.String
		Name        graphql.String
		DisplayName graphql.String
		Email       graphql.String
		IsMe        graphql.Boolean
	}
}

func parseInitiative(node initiativeNode) Initiative {
	description := ""
	if node.Description != nil {
		description = string(*node.Description)
	}
	targetDate := ""
	if node.TargetDate != nil {
		targetDate = string(*node.TargetDate)
	}
	projects := make([]Project, 0, len(node.Projects.Nodes))
	for _, project := range node.Projects.Nodes {
		projects = append(projects, parseInitiativeProject(project))
	}
	return Initiative{
		ID:          string(node.ID),
		Name:        string(node.Name),
		Description: description,
		Status:      string(node.Status),
		TargetDate:  targetDate,
		CreatedAt:   parseTime(string(node.CreatedAt)),
		UpdatedAt:   parseTime(string(node.UpdatedAt)),
		Projects:    projects,
	}
}

func parseInitiativeProject(node initiativeProjectNode) Project {
	description := ""
	if node.Description != nil {
		description = string(*node.Description)
	}
	project := Project{ID: string(node.ID), Name: string(node.Name), Description: description}
	if len(node.Teams.Nodes) > 0 {
		project.TeamID = string(node.Teams.Nodes[0].ID)
	}
	return project
}

func parseProjectMutation(node projectMutationNode) Project {
	description := ""
	if node.Description != nil {
		description = string(*node.Description)
	}
	project := Project{ID: string(node.ID), Name: string(node.Name), Description: description}
	if len(node.Teams.Nodes) > 0 {
		project.TeamID = string(node.Teams.Nodes[0].ID)
	}
	return project
}

func parseInitiativeMutation(node initiativeMutationNode) Initiative {
	description := ""
	if node.Description != nil {
		description = string(*node.Description)
	}
	targetDate := ""
	if node.TargetDate != nil {
		targetDate = string(*node.TargetDate)
	}
	return Initiative{
		ID:          string(node.ID),
		Name:        string(node.Name),
		Description: description,
		Status:      string(node.Status),
		TargetDate:  targetDate,
		Projects:    []Project{},
	}
}

func parseProjectUpdate(node projectUpdateNode) ProjectUpdate {
	return ProjectUpdate{
		ID:        string(node.ID),
		Body:      string(node.Body),
		Health:    string(node.Health),
		CreatedAt: parseTime(string(node.CreatedAt)),
		UpdatedAt: parseTime(string(node.UpdatedAt)),
		ProjectID: string(node.Project.ID),
		Author: User{
			ID:          string(node.User.ID),
			Name:        string(node.User.Name),
			DisplayName: string(node.User.DisplayName),
			Email:       string(node.User.Email),
			IsMe:        bool(node.User.IsMe),
		},
	}
}

func validProjectUpdateHealth(health ProjectUpdateHealthType) bool {
	switch health {
	case "", ProjectUpdateHealthOnTrack, ProjectUpdateHealthAtRisk, ProjectUpdateHealthOffTrack:
		return true
	default:
		return false
	}
}

// ListInitiatives fetches all non-archived initiatives and their projects.
// Both the top-level initiative connection and each nested project connection
// are paged until Linear reports no next page.
func (c *Client) ListInitiatives(ctx context.Context) ([]Initiative, error) {
	var after *graphql.String
	initiatives := make([]Initiative, 0)

	for {
		var query struct {
			Initiatives struct {
				Nodes    []initiativeNode
				PageInfo roadmapPageInfo
			} `graphql:"initiatives(first: $first, after: $after, includeArchived: $includeArchived)"`
		}

		variables := map[string]interface{}{
			"first":           graphql.Int(50),
			"after":           after,
			"includeArchived": graphql.Boolean(false),
			"projectsFirst":   graphql.Int(50),
			"projectsAfter":   (*graphql.String)(nil),
			"teamsFirst":      graphql.Int(1),
		}
		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListInitiatives failed")
			return nil, fmt.Errorf("list initiatives: %w", err)
		}

		for _, node := range query.Initiatives.Nodes {
			initiative := parseInitiative(node)
			if bool(node.Projects.PageInfo.HasNextPage) {
				cursor := node.Projects.PageInfo.EndCursor
				if cursor == nil || strings.TrimSpace(string(*cursor)) == "" {
					return nil, fmt.Errorf("list initiatives: project page has next page but no end cursor for initiative %s", node.ID)
				}
				projects, err := c.listInitiativeProjects(ctx, string(node.ID), string(*cursor))
				if err != nil {
					return nil, fmt.Errorf("list initiatives: %w", err)
				}
				initiative.Projects = append(initiative.Projects, projects...)
			}
			initiatives = append(initiatives, initiative)
		}

		if !bool(query.Initiatives.PageInfo.HasNextPage) {
			break
		}
		cursor := query.Initiatives.PageInfo.EndCursor
		if cursor == nil || strings.TrimSpace(string(*cursor)) == "" {
			return nil, fmt.Errorf("list initiatives: page has next page but no end cursor")
		}
		after = cursor
	}

	return initiatives, nil
}

func (c *Client) listInitiativeProjects(ctx context.Context, initiativeID, after string) ([]Project, error) {
	projects := make([]Project, 0)
	for {
		var query struct {
			Initiative struct {
				Projects initiativeProjectsConnection `graphql:"projects(first: $projectsFirst, after: $projectsAfter)"`
			} `graphql:"initiative(id: $initiativeID)"`
		}

		cursor := graphql.String(after)
		variables := map[string]interface{}{
			"initiativeID":  graphql.String(initiativeID),
			"projectsFirst": graphql.Int(50),
			"projectsAfter": &cursor,
			"teamsFirst":    graphql.Int(1),
		}
		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListInitiatives nested projects failed initiative_id=%s", initiativeID)
			return nil, fmt.Errorf("list projects for initiative %s: %w", initiativeID, err)
		}

		for _, node := range query.Initiative.Projects.Nodes {
			projects = append(projects, parseInitiativeProject(node))
		}
		if !bool(query.Initiative.Projects.PageInfo.HasNextPage) {
			break
		}
		next := query.Initiative.Projects.PageInfo.EndCursor
		if next == nil || strings.TrimSpace(string(*next)) == "" {
			return nil, fmt.Errorf("list projects for initiative %s: page has next page but no end cursor", initiativeID)
		}
		after = string(*next)
	}
	return projects, nil
}

// ListProjectUpdates fetches all non-archived updates for a project.
func (c *Client) ListProjectUpdates(ctx context.Context, projectID string) ([]ProjectUpdate, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("list project updates: project ID must not be empty")
	}

	var after *graphql.String
	updates := make([]ProjectUpdate, 0)
	filter := ProjectUpdateFilter{
		"project": map[string]interface{}{
			"id": map[string]interface{}{
				"eq": graphql.ID(projectID),
			},
		},
	}
	for {
		var query struct {
			ProjectUpdates struct {
				Nodes    []projectUpdateNode
				PageInfo roadmapPageInfo
			} `graphql:"projectUpdates(first: $first, after: $after, filter: $filter, includeArchived: $includeArchived)"`
		}
		variables := map[string]interface{}{
			"first":           graphql.Int(50),
			"after":           after,
			"filter":          filter,
			"includeArchived": graphql.Boolean(false),
		}
		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListProjectUpdates failed project_id=%s", projectID)
			return nil, fmt.Errorf("list project updates for project %s: %w", projectID, err)
		}
		for _, node := range query.ProjectUpdates.Nodes {
			updates = append(updates, parseProjectUpdate(node))
		}
		if !bool(query.ProjectUpdates.PageInfo.HasNextPage) {
			break
		}
		cursor := query.ProjectUpdates.PageInfo.EndCursor
		if cursor == nil || strings.TrimSpace(string(*cursor)) == "" {
			return nil, fmt.Errorf("list project updates for project %s: page has next page but no end cursor", projectID)
		}
		after = cursor
	}
	return updates, nil
}

// CreateProjectUpdate creates a project update with the schema-confirmed
// projectId, body, and optional health fields.
func (c *Client) CreateProjectUpdate(ctx context.Context, input CreateProjectUpdateInput) (ProjectUpdate, error) {
	if strings.TrimSpace(input.ProjectID) == "" {
		return ProjectUpdate{}, fmt.Errorf("create project update: project ID must not be empty")
	}
	if strings.TrimSpace(input.Body) == "" {
		return ProjectUpdate{}, fmt.Errorf("create project update: body must not be empty")
	}
	if !validProjectUpdateHealth(input.Health) {
		return ProjectUpdate{}, fmt.Errorf("create project update: invalid health %q", input.Health)
	}

	var mutation struct {
		ProjectUpdateCreate struct {
			Success       graphql.Boolean
			ProjectUpdate projectUpdateNode
		} `graphql:"projectUpdateCreate(input: $input)"`
	}
	projectInput := ProjectUpdateCreateInput{
		"projectId": graphql.String(input.ProjectID),
		"body":      graphql.String(input.Body),
	}
	if input.Health != "" {
		projectInput["health"] = input.Health
	}
	variables := map[string]interface{}{"input": projectInput}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateProjectUpdate failed project_id=%s", input.ProjectID)
		return ProjectUpdate{}, fmt.Errorf("create project update: %w", err)
	}
	if !bool(mutation.ProjectUpdateCreate.Success) {
		logger.Error("linearapi.client: CreateProjectUpdate operation failed success=false project_id=%s", input.ProjectID)
		return ProjectUpdate{}, fmt.Errorf("create project update: operation failed")
	}
	return parseProjectUpdate(mutation.ProjectUpdateCreate.ProjectUpdate), nil
}

// UpdateProjectUpdate updates a project update with the schema-confirmed body
// and optional health fields.
func (c *Client) UpdateProjectUpdate(ctx context.Context, input UpdateProjectUpdateInput) (ProjectUpdate, error) {
	if strings.TrimSpace(input.ID) == "" {
		return ProjectUpdate{}, fmt.Errorf("update project update: update ID must not be empty")
	}
	if strings.TrimSpace(input.Body) == "" {
		return ProjectUpdate{}, fmt.Errorf("update project update %s: body must not be empty", input.ID)
	}
	if !validProjectUpdateHealth(input.Health) {
		return ProjectUpdate{}, fmt.Errorf("update project update %s: invalid health %q", input.ID, input.Health)
	}

	var mutation struct {
		ProjectUpdateUpdate struct {
			Success       graphql.Boolean
			ProjectUpdate projectUpdateNode
		} `graphql:"projectUpdateUpdate(id: $id, input: $input)"`
	}
	projectInput := ProjectUpdateUpdateInput{"body": graphql.String(input.Body)}
	if input.Health != "" {
		projectInput["health"] = input.Health
	}
	variables := map[string]interface{}{
		"id":    graphql.String(input.ID),
		"input": projectInput,
	}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateProjectUpdate failed update_id=%s", input.ID)
		return ProjectUpdate{}, fmt.Errorf("update project update %s: %w", input.ID, err)
	}
	if !bool(mutation.ProjectUpdateUpdate.Success) {
		logger.Error("linearapi.client: UpdateProjectUpdate operation failed success=false update_id=%s", input.ID)
		return ProjectUpdate{}, fmt.Errorf("update project update %s: operation failed", input.ID)
	}
	return parseProjectUpdate(mutation.ProjectUpdateUpdate.ProjectUpdate), nil
}

// ArchiveProjectUpdate archives a project update. Linear exposes archive for
// this resource rather than a projectUpdateDelete mutation.
func (c *Client) ArchiveProjectUpdate(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("archive project update: update ID must not be empty")
	}

	var mutation struct {
		ProjectUpdateArchive struct {
			Success graphql.Boolean
			Entity  *projectUpdateNode
		} `graphql:"projectUpdateArchive(id: $id)"`
	}
	variables := map[string]interface{}{"id": graphql.String(id)}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ArchiveProjectUpdate failed update_id=%s", id)
		return fmt.Errorf("archive project update %s: %w", id, err)
	}
	if !bool(mutation.ProjectUpdateArchive.Success) {
		logger.Error("linearapi.client: ArchiveProjectUpdate operation failed success=false update_id=%s", id)
		return fmt.Errorf("archive project update %s: operation failed", id)
	}
	return nil
}

func normalizeRoadmapIDs(field string, ids []string) ([]string, error) {
	result := make([]string, len(ids))
	for index, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("%s must not contain empty IDs", field)
		}
		result[index] = id
	}
	return result, nil
}

func addRoadmapCreateString(input map[string]interface{}, key, value string) {
	if strings.TrimSpace(value) != "" {
		input[key] = graphql.String(value)
	}
}

func addRoadmapUpdateString(input map[string]interface{}, key string, value *string) {
	if value != nil {
		input[key] = graphql.String(*value)
	}
}

func addRoadmapCreateStringList(input map[string]interface{}, key string, values []string) error {
	if len(values) == 0 {
		return nil
	}
	normalized, err := normalizeRoadmapIDs(key, values)
	if err != nil {
		return err
	}
	input[key] = normalized
	return nil
}

func addRoadmapUpdateStringList(input map[string]interface{}, key string, values *[]string) error {
	if values == nil {
		return nil
	}
	normalized, err := normalizeRoadmapIDs(key, *values)
	if err != nil {
		return err
	}
	input[key] = normalized
	return nil
}

func buildProjectCreateInput(input CreateProjectInput) (ProjectCreateInput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("create project: name must not be empty")
	}
	teamIDs := input.TeamIDs
	if teamIDs == nil && strings.TrimSpace(input.TeamID) != "" {
		teamIDs = []string{input.TeamID}
	}
	if len(teamIDs) == 0 {
		return nil, fmt.Errorf("create project: at least one team ID is required")
	}
	normalizedTeams, err := normalizeRoadmapIDs("team IDs", teamIDs)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	result := ProjectCreateInput{
		"name":    graphql.String(name),
		"teamIds": normalizedTeams,
	}
	if strings.TrimSpace(input.ID) != "" {
		result["id"] = graphql.String(strings.TrimSpace(input.ID))
	}
	addRoadmapCreateString(result, "icon", input.Icon)
	addRoadmapCreateString(result, "color", input.Color)
	addRoadmapCreateString(result, "statusId", input.StatusID)
	addRoadmapCreateString(result, "description", input.Description)
	addRoadmapCreateString(result, "content", input.Content)
	addRoadmapCreateString(result, "leadId", input.LeadID)
	addRoadmapCreateString(result, "leadTeamId", input.LeadTeamID)
	if err := addRoadmapCreateStringList(result, "memberIds", input.MemberIDs); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	addRoadmapCreateString(result, "startDate", input.StartDate)
	addRoadmapCreateString(result, "startDateResolution", input.StartDateResolution)
	addRoadmapCreateString(result, "targetDate", input.TargetDate)
	addRoadmapCreateString(result, "targetDateResolution", input.TargetDateResolution)
	addRoadmapCreateString(result, "templateId", input.TemplateID)
	if input.Priority != nil {
		result["priority"] = graphql.Float(*input.Priority)
	}
	return result, nil
}

func buildProjectUpdateInput(input UpdateProjectInput) (ProjectUpdateInput, error) {
	result := ProjectUpdateInput{}
	if input.Name != nil {
		if strings.TrimSpace(*input.Name) == "" {
			return nil, fmt.Errorf("update project %s: name must not be empty", input.ID)
		}
		result["name"] = graphql.String(*input.Name)
	}
	if input.TeamIDs != nil {
		normalized, err := normalizeRoadmapIDs("team IDs", *input.TeamIDs)
		if err != nil {
			return nil, fmt.Errorf("update project %s: %w", input.ID, err)
		}
		result["teamIds"] = normalized
	} else if input.TeamID != nil {
		teamID := strings.TrimSpace(*input.TeamID)
		if teamID == "" {
			result["teamIds"] = []string{}
		} else {
			result["teamIds"] = []string{teamID}
		}
	}
	addRoadmapUpdateString(result, "icon", input.Icon)
	addRoadmapUpdateString(result, "color", input.Color)
	addRoadmapUpdateString(result, "statusId", input.StatusID)
	addRoadmapUpdateString(result, "description", input.Description)
	addRoadmapUpdateString(result, "content", input.Content)
	addRoadmapUpdateString(result, "leadId", input.LeadID)
	addRoadmapUpdateString(result, "leadTeamId", input.LeadTeamID)
	if err := addRoadmapUpdateStringList(result, "memberIds", input.MemberIDs); err != nil {
		return nil, fmt.Errorf("update project %s: %w", input.ID, err)
	}
	addRoadmapUpdateString(result, "startDate", input.StartDate)
	addRoadmapUpdateString(result, "startDateResolution", input.StartDateResolution)
	addRoadmapUpdateString(result, "targetDate", input.TargetDate)
	addRoadmapUpdateString(result, "targetDateResolution", input.TargetDateResolution)
	addRoadmapUpdateString(result, "templateId", input.TemplateID)
	if input.Priority != nil {
		result["priority"] = graphql.Float(*input.Priority)
	}
	if input.Trashed != nil {
		result["trashed"] = graphql.Boolean(*input.Trashed)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("update project %s: at least one field is required", input.ID)
	}
	return result, nil
}

func buildInitiativeCreateInput(input CreateInitiativeInput) (InitiativeCreateInput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("create initiative: name must not be empty")
	}
	if !validInitiativeStatus(input.Status) {
		return nil, fmt.Errorf("create initiative: invalid status %q", input.Status)
	}
	result := InitiativeCreateInput{"name": graphql.String(name)}
	if strings.TrimSpace(input.ID) != "" {
		result["id"] = graphql.String(strings.TrimSpace(input.ID))
	}
	addRoadmapCreateString(result, "description", input.Description)
	addRoadmapCreateString(result, "ownerId", input.OwnerID)
	addRoadmapCreateString(result, "leadTeamId", input.LeadTeamID)
	addRoadmapCreateString(result, "color", input.Color)
	addRoadmapCreateString(result, "icon", input.Icon)
	if input.SortOrder != nil {
		result["sortOrder"] = graphql.Float(*input.SortOrder)
	}
	if input.Status != "" {
		result["status"] = input.Status
	}
	addRoadmapCreateString(result, "targetDate", input.TargetDate)
	addRoadmapCreateString(result, "targetDateResolution", input.TargetDateResolution)
	addRoadmapCreateString(result, "content", input.Content)
	if input.Priority != nil {
		result["priority"] = graphql.Float(*input.Priority)
	}
	if err := addRoadmapCreateStringList(result, "labelIds", input.LabelIDs); err != nil {
		return nil, fmt.Errorf("create initiative: %w", err)
	}
	return result, nil
}

func buildInitiativeToProjectCreateInput(input CreateInitiativeToProjectInput) (InitiativeToProjectCreateInput, error) {
	initiativeID := strings.TrimSpace(input.InitiativeID)
	if initiativeID == "" {
		return nil, fmt.Errorf("create initiative to project: initiative ID must not be empty")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("create initiative to project: project ID must not be empty")
	}
	result := InitiativeToProjectCreateInput{
		"initiativeId": graphql.String(initiativeID),
		"projectId":    graphql.String(projectID),
	}
	if input.SortOrder != nil {
		result["sortOrder"] = graphql.Float(*input.SortOrder)
	}
	return result, nil
}

func buildInitiativeUpdateInput(input UpdateInitiativeInput) (InitiativeUpdateInput, error) {
	result := InitiativeUpdateInput{}
	addRoadmapUpdateString(result, "name", input.Name)
	addRoadmapUpdateString(result, "description", input.Description)
	addRoadmapUpdateString(result, "ownerId", input.OwnerID)
	addRoadmapUpdateString(result, "leadTeamId", input.LeadTeamID)
	addRoadmapUpdateString(result, "color", input.Color)
	addRoadmapUpdateString(result, "icon", input.Icon)
	if input.SortOrder != nil {
		result["sortOrder"] = graphql.Float(*input.SortOrder)
	}
	if input.Status != nil {
		if !validInitiativeStatus(*input.Status) {
			return nil, fmt.Errorf("update initiative %s: invalid status %q", input.ID, *input.Status)
		}
		result["status"] = *input.Status
	}
	addRoadmapUpdateString(result, "targetDate", input.TargetDate)
	addRoadmapUpdateString(result, "targetDateResolution", input.TargetDateResolution)
	addRoadmapUpdateString(result, "content", input.Content)
	if input.Priority != nil {
		result["priority"] = graphql.Float(*input.Priority)
	}
	if err := addRoadmapUpdateStringList(result, "labelIds", input.LabelIDs); err != nil {
		return nil, fmt.Errorf("update initiative %s: %w", input.ID, err)
	}
	if input.Trashed != nil {
		result["trashed"] = graphql.Boolean(*input.Trashed)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("update initiative %s: at least one field is required", input.ID)
	}
	return result, nil
}

// CreateProject creates a project using the public projectCreate mutation.
func (c *Client) CreateProject(ctx context.Context, input CreateProjectInput) (Project, error) {
	graphqlInput, err := buildProjectCreateInput(input)
	if err != nil {
		return Project{}, err
	}
	var mutation struct {
		ProjectCreate struct {
			Success graphql.Boolean
			Project *projectMutationNode
		} `graphql:"projectCreate(input: $input)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"input": graphqlInput}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateProject failed")
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	if !bool(mutation.ProjectCreate.Success) {
		return Project{}, fmt.Errorf("create project: operation failed")
	}
	if mutation.ProjectCreate.Project == nil || strings.TrimSpace(string(mutation.ProjectCreate.Project.ID)) == "" {
		return Project{}, fmt.Errorf("create project: operation returned no project")
	}
	return parseProjectMutation(*mutation.ProjectCreate.Project), nil
}

// UpdateProject updates a project with explicit set/clear semantics.
func (c *Client) UpdateProject(ctx context.Context, input UpdateProjectInput) (Project, error) {
	projectID := strings.TrimSpace(input.ID)
	if projectID == "" {
		return Project{}, fmt.Errorf("update project: project ID must not be empty")
	}
	graphqlInput, err := buildProjectUpdateInput(input)
	if err != nil {
		return Project{}, err
	}
	var mutation struct {
		ProjectUpdate struct {
			Success graphql.Boolean
			Project *projectMutationNode
		} `graphql:"projectUpdate(id: $id, input: $input)"`
	}
	variables := map[string]interface{}{"id": graphql.String(projectID), "input": graphqlInput}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateProject failed project_id=%s", projectID)
		return Project{}, fmt.Errorf("update project %s: %w", projectID, err)
	}
	if !bool(mutation.ProjectUpdate.Success) {
		return Project{}, fmt.Errorf("update project %s: operation failed", projectID)
	}
	if mutation.ProjectUpdate.Project == nil || strings.TrimSpace(string(mutation.ProjectUpdate.Project.ID)) == "" {
		return Project{}, fmt.Errorf("update project %s: operation returned no project", projectID)
	}
	return parseProjectMutation(*mutation.ProjectUpdate.Project), nil
}

// DeleteProject uses Linear's public projectDelete mutation. The schema does
// not expose projectArchive; projectDelete is the only destructive project
// operation and is therefore surfaced truthfully under this name.
func (c *Client) DeleteProject(ctx context.Context, id string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return fmt.Errorf("delete project: project ID must not be empty")
	}
	var mutation struct {
		ProjectDelete struct {
			Success graphql.Boolean
			Entity  *projectMutationNode
		} `graphql:"projectDelete(id: $id)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"id": graphql.String(projectID)}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: DeleteProject failed project_id=%s", projectID)
		return fmt.Errorf("delete project %s: %w", projectID, err)
	}
	if !bool(mutation.ProjectDelete.Success) {
		return fmt.Errorf("delete project %s: operation failed", projectID)
	}
	return nil
}

// CreateInitiative creates an initiative using the public initiativeCreate
// mutation.
func (c *Client) CreateInitiative(ctx context.Context, input CreateInitiativeInput) (Initiative, error) {
	graphqlInput, err := buildInitiativeCreateInput(input)
	if err != nil {
		return Initiative{}, err
	}
	var mutation struct {
		InitiativeCreate struct {
			Success    graphql.Boolean
			Initiative initiativeMutationNode
		} `graphql:"initiativeCreate(input: $input)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"input": graphqlInput}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateInitiative failed")
		return Initiative{}, fmt.Errorf("create initiative: %w", err)
	}
	if !bool(mutation.InitiativeCreate.Success) {
		return Initiative{}, fmt.Errorf("create initiative: operation failed")
	}
	if strings.TrimSpace(string(mutation.InitiativeCreate.Initiative.ID)) == "" {
		return Initiative{}, fmt.Errorf("create initiative: operation returned no initiative")
	}
	return parseInitiativeMutation(mutation.InitiativeCreate.Initiative), nil
}

// CreateInitiativeToProject associates an existing project with an initiative
// using Linear's initiativeToProjectCreate mutation. The operation is
// intentionally one-way: this method does not attempt a compensating delete
// when the mutation fails.
func (c *Client) CreateInitiativeToProject(ctx context.Context, input CreateInitiativeToProjectInput) error {
	graphqlInput, err := buildInitiativeToProjectCreateInput(input)
	if err != nil {
		return err
	}
	var mutation struct {
		InitiativeToProjectCreate struct {
			Success graphql.Boolean
		} `graphql:"initiativeToProjectCreate(input: $input)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"input": graphqlInput}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateInitiativeToProject failed initiative_id=%s project_id=%s", input.InitiativeID, input.ProjectID)
		return fmt.Errorf("create initiative to project: %w", err)
	}
	if !bool(mutation.InitiativeToProjectCreate.Success) {
		return fmt.Errorf("create initiative to project: operation failed")
	}
	return nil
}

// UpdateInitiative updates an initiative with explicit set/clear semantics.
func (c *Client) UpdateInitiative(ctx context.Context, input UpdateInitiativeInput) (Initiative, error) {
	initiativeID := strings.TrimSpace(input.ID)
	if initiativeID == "" {
		return Initiative{}, fmt.Errorf("update initiative: initiative ID must not be empty")
	}
	graphqlInput, err := buildInitiativeUpdateInput(input)
	if err != nil {
		return Initiative{}, err
	}
	var mutation struct {
		InitiativeUpdate struct {
			Success    graphql.Boolean
			Initiative initiativeMutationNode
		} `graphql:"initiativeUpdate(id: $id, input: $input)"`
	}
	variables := map[string]interface{}{"id": graphql.String(initiativeID), "input": graphqlInput}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateInitiative failed initiative_id=%s", initiativeID)
		return Initiative{}, fmt.Errorf("update initiative %s: %w", initiativeID, err)
	}
	if !bool(mutation.InitiativeUpdate.Success) {
		return Initiative{}, fmt.Errorf("update initiative %s: operation failed", initiativeID)
	}
	if strings.TrimSpace(string(mutation.InitiativeUpdate.Initiative.ID)) == "" {
		return Initiative{}, fmt.Errorf("update initiative %s: operation returned no initiative", initiativeID)
	}
	return parseInitiativeMutation(mutation.InitiativeUpdate.Initiative), nil
}

// ArchiveInitiative archives an initiative using the public archive mutation.
func (c *Client) ArchiveInitiative(ctx context.Context, id string) error {
	initiativeID := strings.TrimSpace(id)
	if initiativeID == "" {
		return fmt.Errorf("archive initiative: initiative ID must not be empty")
	}
	var mutation struct {
		InitiativeArchive struct {
			Success  graphql.Boolean
			Entity   *initiativeMutationNode
			EntityID graphql.String
		} `graphql:"initiativeArchive(id: $id)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"id": graphql.String(initiativeID)}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ArchiveInitiative failed initiative_id=%s", initiativeID)
		return fmt.Errorf("archive initiative %s: %w", initiativeID, err)
	}
	if !bool(mutation.InitiativeArchive.Success) {
		return fmt.Errorf("archive initiative %s: operation failed", initiativeID)
	}
	return nil
}

// DeleteInitiative permanently deletes an initiative using the public
// initiativeDelete mutation.
func (c *Client) DeleteInitiative(ctx context.Context, id string) error {
	initiativeID := strings.TrimSpace(id)
	if initiativeID == "" {
		return fmt.Errorf("delete initiative: initiative ID must not be empty")
	}
	var mutation struct {
		InitiativeDelete struct {
			Success  graphql.Boolean
			EntityID graphql.String
			Entity   *initiativeMutationNode
		} `graphql:"initiativeDelete(id: $id)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{"id": graphql.String(initiativeID)}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: DeleteInitiative failed initiative_id=%s", initiativeID)
		return fmt.Errorf("delete initiative %s: %w", initiativeID, err)
	}
	if !bool(mutation.InitiativeDelete.Success) {
		return fmt.Errorf("delete initiative %s: operation failed", initiativeID)
	}
	return nil
}
