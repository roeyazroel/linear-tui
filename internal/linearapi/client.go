package linearapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/logger"
	"github.com/shurcooL/graphql"
)

// parseTime safely parses an RFC3339 time string, returning zero time on error.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// IssueFilter is a custom scalar type for Linear's IssueFilter input.
// It allows passing complex filter objects to the GraphQL API.
type IssueFilter map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the filter.
func (IssueFilter) GetGraphQLType() string {
	return "IssueFilter"
}

// MarshalJSON implements json.Marshaler for IssueFilter.
func (f IssueFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(f))
}

// IssueCreateInput is a custom scalar type for Linear's IssueCreateInput.
// The Go type name must match the GraphQL type name exactly.
type IssueCreateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (IssueCreateInput) GetGraphQLType() string {
	return "IssueCreateInput"
}

// MarshalJSON implements json.Marshaler for IssueCreateInput.
func (i IssueCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// ProjectMilestoneFilter is a custom scalar type for Linear's ProjectMilestoneFilter input.
type ProjectMilestoneFilter map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the filter.
func (ProjectMilestoneFilter) GetGraphQLType() string {
	return "ProjectMilestoneFilter"
}

// MarshalJSON implements json.Marshaler for ProjectMilestoneFilter.
func (f ProjectMilestoneFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(f))
}

// IssueUpdateInput is a custom scalar type for Linear's IssueUpdateInput.
// The Go type name must match the GraphQL type name exactly.
type IssueUpdateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (IssueUpdateInput) GetGraphQLType() string {
	return "IssueUpdateInput"
}

// MarshalJSON implements json.Marshaler for IssueUpdateInput.
func (i IssueUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// CommentCreateInput is a custom scalar type for Linear's CommentCreateInput.
// The Go type name must match the GraphQL type name exactly.
type CommentCreateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (CommentCreateInput) GetGraphQLType() string {
	return "CommentCreateInput"
}

// MarshalJSON implements json.Marshaler for CommentCreateInput.
func (c CommentCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(c))
}

// IssueRelationCreateInput is a custom scalar type for Linear's IssueRelationCreateInput.
type IssueRelationCreateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (IssueRelationCreateInput) GetGraphQLType() string {
	return "IssueRelationCreateInput"
}

// MarshalJSON implements json.Marshaler for IssueRelationCreateInput.
func (i IssueRelationCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// IssueRelationType is Linear's issue relation enum.
type IssueRelationType string

// GetGraphQLType returns the GraphQL type name for the enum.
func (IssueRelationType) GetGraphQLType() string {
	return "IssueRelationType"
}

const (
	IssueRelationBlocks    IssueRelationType = "blocks"
	IssueRelationRelated   IssueRelationType = "related"
	IssueRelationDuplicate IssueRelationType = "duplicate"
	IssueRelationSimilar   IssueRelationType = "similar"
)

// PaginationOrderBy is a custom type for Linear's PaginationOrderBy enum.
// Valid values are "createdAt" and "updatedAt".
type PaginationOrderBy string

// GetGraphQLType returns the GraphQL type name for the enum.
func (PaginationOrderBy) GetGraphQLType() string {
	return "PaginationOrderBy"
}

// Common PaginationOrderBy values.
const (
	OrderByCreatedAt PaginationOrderBy = "createdAt"
	OrderByUpdatedAt PaginationOrderBy = "updatedAt"
)

const (
	// DefaultEndpoint is the default Linear API GraphQL endpoint.
	DefaultEndpoint = "https://api.linear.app/graphql"
)

// ClientConfig contains configuration for creating a new Linear API client.
type ClientConfig struct {
	// Token is the Linear API key for authentication.
	Token string
	// Endpoint is the GraphQL API endpoint (defaults to Linear's production endpoint).
	Endpoint string
	// HTTPClient is an optional custom HTTP client (useful for testing).
	HTTPClient *http.Client
	// Timeout is the HTTP request timeout (defaults to 30s).
	Timeout time.Duration
}

// Client is a client for interacting with the Linear GraphQL API.
type Client struct {
	httpClient *http.Client
	endpoint   string
	token      string
	client     *graphql.Client
}

// Team represents a Linear team.
type Team struct {
	ID   string
	Key  string
	Name string
}

// Project represents a Linear project.
type Project struct {
	ID     string
	Name   string
	TeamID string
}

// ProjectMilestoneRef represents a lightweight reference to a Linear project milestone.
type ProjectMilestoneRef struct {
	ID         string
	Name       string
	ProjectID  string
	TargetDate *string
	Status     string
	SortOrder  float64
	Progress   float64
}

// ProjectMilestone represents a Linear project milestone.
type ProjectMilestone = ProjectMilestoneRef

// CycleRef represents a lightweight reference to a Linear cycle.
type CycleRef struct {
	ID         string
	Name       string
	Number     int
	StartsAt   time.Time
	EndsAt     time.Time
	IsActive   bool
	IsFuture   bool
	IsPast     bool
	IsNext     bool
	IsPrevious bool
}

// DisplayName returns the user-facing cycle name, falling back to the cycle number.
func (c CycleRef) DisplayName() string {
	if strings.TrimSpace(c.Name) != "" {
		return c.Name
	}
	if c.Number > 0 {
		return fmt.Sprintf("Cycle %d", c.Number)
	}
	return "Cycle"
}

// Cycle represents a Linear cycle.
type Cycle struct {
	ID          string
	Name        string
	Number      int
	StartsAt    time.Time
	EndsAt      time.Time
	IsActive    bool
	IsFuture    bool
	IsPast      bool
	IsNext      bool
	IsPrevious  bool
	Description string
	TeamID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DisplayName returns the user-facing cycle name, falling back to the cycle number.
func (c Cycle) DisplayName() string {
	return CycleRef{Name: c.Name, Number: c.Number}.DisplayName()
}

// User represents a Linear user.
type User struct {
	ID          string
	Name        string
	DisplayName string
	Email       string
	IsMe        bool
}

// WorkflowState represents a workflow state in a Linear team.
type WorkflowState struct {
	ID       string
	Name     string
	Type     string // backlog, unstarted, started, completed, canceled
	Position float64
	TeamID   string
}

// IssueLabel represents a label that can be applied to issues.
type IssueLabel struct {
	ID    string
	Name  string
	Color string // Hex color code (e.g., "#ff0000")
}

// IssueRef represents a lightweight reference to an issue (for parent relationships).
type IssueRef struct {
	ID         string
	Identifier string
	Title      string
}

// IssueChildRef represents a lightweight reference to a child issue.
type IssueChildRef struct {
	ID         string
	Identifier string
	Title      string
	State      string
	StateID    string
}

// Comment represents a comment on a Linear issue.
type Comment struct {
	ID        string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Author    User
	IssueID   string
}

// IssueRelation represents a Linear issue relation.
type IssueRelation struct {
	ID           string
	Type         string
	Issue        IssueRef
	RelatedIssue IssueRef
	Inverse      bool
}

// DisplayType returns the relation label from the selected issue's perspective.
func (r IssueRelation) DisplayType() string {
	switch r.Type {
	case string(IssueRelationBlocks):
		if r.Inverse {
			return "blocked by"
		}
		return "blocking"
	case string(IssueRelationDuplicate):
		if r.Inverse {
			return "duplicate of"
		}
		return "duplicate"
	case string(IssueRelationRelated):
		return "related"
	case string(IssueRelationSimilar):
		return "similar"
	default:
		if r.Inverse {
			return r.Type + " by"
		}
		return r.Type
	}
}

// Attachment represents an external resource linked to a Linear issue.
type Attachment struct {
	ID         string
	Title      string
	Subtitle   string
	URL        string
	SourceType string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Issue represents a Linear issue.
type Issue struct {
	ID               string
	Identifier       string
	Title            string
	Description      string
	State            string
	StateID          string
	Assignee         string
	AssigneeID       string
	Priority         int
	UpdatedAt        time.Time
	CreatedAt        time.Time
	TeamID           string
	ProjectID        string
	Cycle            *CycleRef
	DueDate          *string
	Estimate         *float64
	ProjectMilestone *ProjectMilestoneRef
	URL              string
	Archived         bool
	Labels           []IssueLabel
	Parent           *IssueRef       // Parent issue reference (nil if top-level)
	Children         []IssueChildRef // Child/sub-issue references
	Comments         []Comment       // Comments on this issue
	Relations        []IssueRelation
	Subscribers      []User
	Attachments      []Attachment
}

// IssueFetchProgress describes progress for a paginated issue fetch.
type IssueFetchProgress struct {
	Page    int
	Fetched int
}

// IssuePage represents a single page of issues with pagination info.
type IssuePage struct {
	Issues    []Issue
	HasNext   bool
	EndCursor *string
}

// FetchIssuesParams contains parameters for fetching issues.
type FetchIssuesParams struct {
	TeamID    string
	ProjectID string
	// ProjectIDs restricts results to issues in any of these projects
	// (project.id.in). Ignored when ProjectID is set (single-project takes
	// precedence).
	ProjectIDs         []string
	StateID            string
	CycleID            string
	AssigneeID         string
	LabelIDs           []string
	ProjectMilestoneID string
	DueDate            DateFilter
	Estimate           NumberFilter
	Search             string
	// OrderBy specifies the sort order. Valid API values are "updatedAt" and "createdAt".
	// "priority" is also supported and will be sorted client-side after fetching.
	OrderBy string
	First   int
	// OnProgress is an optional callback invoked after each page is fetched.
	OnProgress func(IssueFetchProgress)
}

// DateFilter describes a Linear timeless date filter.
type DateFilter struct {
	Eq   string
	GT   string
	GTE  string
	LT   string
	LTE  string
	Null *bool
}

// Empty returns whether no date filter fields are set.
func (f DateFilter) Empty() bool {
	return f.Eq == "" && f.GT == "" && f.GTE == "" && f.LT == "" && f.LTE == "" && f.Null == nil
}

// NumberFilter describes a Linear numeric filter.
type NumberFilter struct {
	Eq   *float64
	GT   *float64
	GTE  *float64
	LT   *float64
	LTE  *float64
	Null *bool
}

// Empty returns whether no numeric filter fields are set.
func (f NumberFilter) Empty() bool {
	return f.Eq == nil && f.GT == nil && f.GTE == nil && f.LT == nil && f.LTE == nil && f.Null == nil
}

// CreateIssueInput contains input for creating a new issue.
type CreateIssueInput struct {
	TeamID      string
	Title       string
	Description string
	ProjectID   string
	StateID     string
	CycleID     string
	AssigneeID  string
	Priority    int
	ParentID    string // Parent issue ID (empty for top-level issues)
}

// UpdateIssueInput contains input for updating an issue.
type UpdateIssueInput struct {
	ID                 string
	Title              *string
	Description        *string
	StateID            *string
	CycleID            *string // nil = no change, empty string = clear cycle, non-empty = set cycle
	AssigneeID         *string
	Priority           *int
	LabelIDs           *[]string // nil = no change, empty slice = clear all, non-empty = set labels
	ParentID           *string   // nil = no change, empty string = clear parent, non-empty = set parent
	DueDate            *string   // nil = no change, empty string = clear due date, non-empty = set YYYY-MM-DD date
	Estimate           *float64  // nil = no change, non-nil = set estimate
	ClearEstimate      bool      // true = clear estimate
	ProjectMilestoneID *string   // nil = no change, empty string = clear milestone, non-empty = set milestone
}

// CreateCommentInput contains input for creating a new comment.
type CreateCommentInput struct {
	IssueID string
	Body    string
}

// CreateIssueRelationInput contains input for creating an issue relation.
type CreateIssueRelationInput struct {
	IssueID        string
	RelatedIssueID string
	Type           IssueRelationType
}

type issueMutationNode struct {
	ID         graphql.String
	Identifier graphql.String
	Title      graphql.String
	State      struct {
		ID   graphql.String
		Name graphql.String
	}
	Assignee *struct {
		ID   graphql.String
		Name graphql.String
	}
	Priority    graphql.Float
	UpdatedAt   graphql.String
	CreatedAt   graphql.String
	Description *graphql.String
	Team        struct {
		ID graphql.String
	}
	Project *struct {
		ID graphql.String
	}
	Cycle *struct {
		ID         graphql.String
		Name       *graphql.String
		Number     graphql.Float
		StartsAt   graphql.String
		EndsAt     graphql.String
		IsActive   graphql.Boolean
		IsFuture   graphql.Boolean
		IsPast     graphql.Boolean
		IsNext     graphql.Boolean
		IsPrevious graphql.Boolean
	}
	Labels struct {
		Nodes []struct {
			ID    graphql.String
			Name  graphql.String
			Color graphql.String
		}
	}
	URL graphql.String
}

// NewClient creates a new Linear API client with the provided configuration.
func NewClient(cfg ClientConfig) *Client {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	var httpClient *http.Client
	if cfg.HTTPClient != nil {
		// Use provided HTTP client but wrap its transport with auth
		httpClient = cfg.HTTPClient
		if httpClient.Transport == nil {
			httpClient.Transport = http.DefaultTransport
		}
		httpClient.Transport = &authTransport{
			Token: cfg.Token,
			Base:  httpClient.Transport,
		}
	} else {
		// Create a new HTTP client
		httpClient = &http.Client{
			Timeout: timeout,
			Transport: &authTransport{
				Token: cfg.Token,
				Base:  http.DefaultTransport,
			},
		}
	}

	client := graphql.NewClient(endpoint, httpClient)

	return &Client{
		httpClient: httpClient,
		endpoint:   endpoint,
		token:      cfg.Token,
		client:     client,
	}
}

// NewClientWithToken creates a new Linear API client with just a token (convenience method).
func NewClientWithToken(token string) *Client {
	return NewClient(ClientConfig{Token: token})
}

// authTransport adds the Authorization header to requests.
type authTransport struct {
	Token string
	Base  http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", t.Token)
	if t.Base == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	return t.Base.RoundTrip(req)
}

// Endpoint returns the GraphQL endpoint being used.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// ListTeams fetches all teams the user has access to.
func (c *Client) ListTeams(ctx context.Context) ([]Team, error) {
	var query struct {
		Teams struct {
			Nodes []struct {
				ID   graphql.String
				Key  graphql.String
				Name graphql.String
			}
		} `graphql:"teams"`
	}

	err := c.client.Query(ctx, &query, nil)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListTeams failed")
		return nil, fmt.Errorf("list teams: %w", err)
	}

	teams := make([]Team, 0, len(query.Teams.Nodes))
	for _, node := range query.Teams.Nodes {
		teams = append(teams, Team{
			ID:   string(node.ID),
			Key:  string(node.Key),
			Name: string(node.Name),
		})
	}

	return teams, nil
}

// ListProjects fetches all projects for a team.
func (c *Client) ListProjects(ctx context.Context, teamID string) ([]Project, error) {
	var query struct {
		Team struct {
			Projects struct {
				Nodes []struct {
					ID   graphql.String
					Name graphql.String
				}
			}
		} `graphql:"team(id: $teamId)"`
	}

	variables := map[string]interface{}{
		"teamId": graphql.String(teamID),
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListProjects failed team_id=%s", teamID)
		return nil, fmt.Errorf("list projects for team %s: %w", teamID, err)
	}

	projects := make([]Project, 0, len(query.Team.Projects.Nodes))
	for _, node := range query.Team.Projects.Nodes {
		projects = append(projects, Project{
			ID:     string(node.ID),
			Name:   string(node.Name),
			TeamID: teamID,
		})
	}

	return projects, nil
}

// ListFavoriteProjects fetches the viewer's favorited projects, paginating
// through the favorites connection and keeping only favorites that reference a
// project. A project can belong to multiple teams; TeamID is set to its first
// team so that team-scoped actions have a sensible default context.
func (c *Client) ListFavoriteProjects(ctx context.Context) ([]Project, error) {
	var after *string
	projects := make([]Project, 0)
	seen := make(map[string]bool)

	for {
		var query struct {
			Favorites struct {
				Nodes []struct {
					Project *struct {
						ID    graphql.String
						Name  graphql.String
						Teams struct {
							Nodes []struct {
								ID graphql.String
							}
						} `graphql:"teams(first: 1)"`
					}
				}
				PageInfo struct {
					HasNextPage graphql.Boolean
					EndCursor   graphql.String
				}
			} `graphql:"favorites(first: $first, after: $after)"`
		}

		var afterCursor *graphql.String
		if after != nil {
			cursor := graphql.String(*after)
			afterCursor = &cursor
		}

		variables := map[string]interface{}{
			"first": graphql.Int(100),
			"after": afterCursor,
		}

		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListFavoriteProjects failed")
			return nil, fmt.Errorf("list favorite projects: %w", err)
		}

		for _, node := range query.Favorites.Nodes {
			if node.Project == nil {
				continue
			}
			id := string(node.Project.ID)
			if seen[id] {
				continue
			}
			seen[id] = true
			teamID := ""
			if len(node.Project.Teams.Nodes) > 0 {
				teamID = string(node.Project.Teams.Nodes[0].ID)
			}
			projects = append(projects, Project{
				ID:     id,
				Name:   string(node.Project.Name),
				TeamID: teamID,
			})
		}

		if !bool(query.Favorites.PageInfo.HasNextPage) {
			break
		}
		cursor := string(query.Favorites.PageInfo.EndCursor)
		after = &cursor
	}

	return projects, nil
}

// ListProjectMilestones fetches all non-archived milestones for a project.
func (c *Client) ListProjectMilestones(ctx context.Context, projectID string) ([]ProjectMilestone, error) {
	var after *string
	milestones := make([]ProjectMilestone, 0)

	for {
		var query struct {
			ProjectMilestones struct {
				Nodes []struct {
					ID         graphql.String
					Name       graphql.String
					TargetDate *graphql.String
					Status     graphql.String
					SortOrder  graphql.Float
					Progress   graphql.Float
					Project    struct {
						ID graphql.String
					}
				}
				PageInfo struct {
					HasNextPage graphql.Boolean
					EndCursor   graphql.String
				}
			} `graphql:"projectMilestones(first: $first, after: $after, filter: $filter, includeArchived: $includeArchived)"`
		}

		var afterCursor *graphql.String
		if after != nil {
			cursor := graphql.String(*after)
			afterCursor = &cursor
		}

		filter := ProjectMilestoneFilter{
			"project": map[string]interface{}{"id": map[string]interface{}{"eq": projectID}},
		}
		variables := map[string]interface{}{
			"first":           graphql.Int(50),
			"after":           afterCursor,
			"filter":          filter,
			"includeArchived": graphql.Boolean(false),
		}

		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListProjectMilestones failed project_id=%s", projectID)
			return nil, fmt.Errorf("list project milestones for project %s: %w", projectID, err)
		}

		for _, node := range query.ProjectMilestones.Nodes {
			var targetDate *string
			if node.TargetDate != nil {
				value := string(*node.TargetDate)
				targetDate = &value
			}
			milestones = append(milestones, ProjectMilestone{
				ID:         string(node.ID),
				Name:       string(node.Name),
				ProjectID:  string(node.Project.ID),
				TargetDate: targetDate,
				Status:     string(node.Status),
				SortOrder:  float64(node.SortOrder),
				Progress:   float64(node.Progress),
			})
		}

		if !bool(query.ProjectMilestones.PageInfo.HasNextPage) {
			break
		}
		cursor := string(query.ProjectMilestones.PageInfo.EndCursor)
		after = &cursor
	}

	return milestones, nil
}

// ListCycles fetches all non-archived cycles for a team.
func (c *Client) ListCycles(ctx context.Context, teamID string) ([]Cycle, error) {
	var after *string
	cycles := make([]Cycle, 0)

	for {
		var query struct {
			Team struct {
				Cycles struct {
					Nodes []struct {
						ID          graphql.String
						Name        *graphql.String
						Number      graphql.Float
						Description *graphql.String
						StartsAt    graphql.String
						EndsAt      graphql.String
						IsActive    graphql.Boolean
						IsFuture    graphql.Boolean
						IsPast      graphql.Boolean
						IsNext      graphql.Boolean
						IsPrevious  graphql.Boolean
						CreatedAt   graphql.String
						UpdatedAt   graphql.String
						Team        struct {
							ID graphql.String
						}
					}
					PageInfo struct {
						HasNextPage graphql.Boolean
						EndCursor   graphql.String
					}
				} `graphql:"cycles(first: $first, after: $after, includeArchived: $includeArchived)"`
			} `graphql:"team(id: $teamId)"`
		}

		var afterCursor *graphql.String
		if after != nil {
			cursor := graphql.String(*after)
			afterCursor = &cursor
		}

		variables := map[string]interface{}{
			"teamId":          graphql.String(teamID),
			"first":           graphql.Int(50),
			"after":           afterCursor,
			"includeArchived": graphql.Boolean(false),
		}

		if err := c.client.Query(ctx, &query, variables); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListCycles failed team_id=%s", teamID)
			return nil, fmt.Errorf("list cycles for team %s: %w", teamID, err)
		}

		for _, node := range query.Team.Cycles.Nodes {
			name := ""
			if node.Name != nil {
				name = string(*node.Name)
			}
			description := ""
			if node.Description != nil {
				description = string(*node.Description)
			}
			cycles = append(cycles, Cycle{
				ID:          string(node.ID),
				Name:        name,
				Number:      int(node.Number),
				StartsAt:    parseTime(string(node.StartsAt)),
				EndsAt:      parseTime(string(node.EndsAt)),
				IsActive:    bool(node.IsActive),
				IsFuture:    bool(node.IsFuture),
				IsPast:      bool(node.IsPast),
				IsNext:      bool(node.IsNext),
				IsPrevious:  bool(node.IsPrevious),
				Description: description,
				TeamID:      string(node.Team.ID),
				CreatedAt:   parseTime(string(node.CreatedAt)),
				UpdatedAt:   parseTime(string(node.UpdatedAt)),
			})
		}

		if !bool(query.Team.Cycles.PageInfo.HasNextPage) {
			break
		}
		cursor := string(query.Team.Cycles.PageInfo.EndCursor)
		after = &cursor
	}

	return cycles, nil
}

// ListUsers fetches all users in a team.
func (c *Client) ListUsers(ctx context.Context, teamID string) ([]User, error) {
	var query struct {
		Team struct {
			Members struct {
				Nodes []struct {
					ID          graphql.String
					Name        graphql.String
					DisplayName graphql.String
					Email       graphql.String
					IsMe        graphql.Boolean
				}
			}
		} `graphql:"team(id: $teamId)"`
	}

	variables := map[string]interface{}{
		"teamId": graphql.String(teamID),
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListUsers failed team_id=%s", teamID)
		return nil, fmt.Errorf("list users for team %s: %w", teamID, err)
	}

	users := make([]User, 0, len(query.Team.Members.Nodes))
	for _, node := range query.Team.Members.Nodes {
		users = append(users, User{
			ID:          string(node.ID),
			Name:        string(node.Name),
			DisplayName: string(node.DisplayName),
			Email:       string(node.Email),
			IsMe:        bool(node.IsMe),
		})
	}

	return users, nil
}

// GetCurrentUser fetches the current authenticated user.
func (c *Client) GetCurrentUser(ctx context.Context) (User, error) {
	var query struct {
		Viewer struct {
			ID          graphql.String
			Name        graphql.String
			DisplayName graphql.String
			Email       graphql.String
		}
	}

	err := c.client.Query(ctx, &query, nil)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: GetCurrentUser failed")
		return User{}, fmt.Errorf("get current user: %w", err)
	}

	return User{
		ID:          string(query.Viewer.ID),
		Name:        string(query.Viewer.Name),
		DisplayName: string(query.Viewer.DisplayName),
		Email:       string(query.Viewer.Email),
		IsMe:        true,
	}, nil
}

// ListWorkflowStates fetches all workflow states for a team.
func (c *Client) ListWorkflowStates(ctx context.Context, teamID string) ([]WorkflowState, error) {
	var query struct {
		Team struct {
			States struct {
				Nodes []struct {
					ID       graphql.String
					Name     graphql.String
					Type     graphql.String
					Position graphql.Float
				}
			}
		} `graphql:"team(id: $teamId)"`
	}

	variables := map[string]interface{}{
		"teamId": graphql.String(teamID),
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListWorkflowStates failed team_id=%s", teamID)
		return nil, fmt.Errorf("list workflow states for team %s: %w", teamID, err)
	}

	states := make([]WorkflowState, 0, len(query.Team.States.Nodes))
	for _, node := range query.Team.States.Nodes {
		states = append(states, WorkflowState{
			ID:       string(node.ID),
			Name:     string(node.Name),
			Type:     string(node.Type),
			Position: float64(node.Position),
			TeamID:   teamID,
		})
	}

	return states, nil
}

// buildBaseIssueFilter builds the base issue filter without search terms.
func buildBaseIssueFilter(params FetchIssuesParams) IssueFilter {
	filter := make(IssueFilter)
	if params.TeamID != "" {
		filter["team"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.TeamID}}
	}
	if params.ProjectID != "" {
		filter["project"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.ProjectID}}
	} else if len(params.ProjectIDs) > 0 {
		filter["project"] = map[string]interface{}{"id": map[string]interface{}{"in": params.ProjectIDs}}
	}
	if params.StateID != "" {
		filter["state"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.StateID}}
	}
	if params.CycleID != "" {
		filter["cycle"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.CycleID}}
	}
	return filter
}

// buildStructuredIssueFilter builds issue filters that can be passed alongside
// Linear's full-text search term.
func buildStructuredIssueFilter(params FetchIssuesParams) IssueFilter {
	filter := buildBaseIssueFilter(params)
	if params.AssigneeID != "" {
		filter["assignee"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.AssigneeID}}
	}
	if params.ProjectMilestoneID != "" {
		filter["projectMilestone"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.ProjectMilestoneID}}
	}
	if !params.DueDate.Empty() {
		filter["dueDate"] = buildDateComparator(params.DueDate)
	}
	if !params.Estimate.Empty() {
		filter["estimate"] = buildNumberComparator(params.Estimate)
	}
	if len(params.LabelIDs) > 0 {
		andFilters := make([]map[string]interface{}, 0, len(params.LabelIDs))
		for _, labelID := range params.LabelIDs {
			labelID = strings.TrimSpace(labelID)
			if labelID == "" {
				continue
			}
			andFilters = append(andFilters, map[string]interface{}{
				"labels": map[string]interface{}{
					"some": map[string]interface{}{
						"id": map[string]interface{}{"eq": labelID},
					},
				},
			})
		}
		appendIssueAndFilters(filter, andFilters...)
	}
	return filter
}

func buildDateComparator(dateFilter DateFilter) map[string]interface{} {
	comparator := make(map[string]interface{})
	if dateFilter.Eq != "" {
		comparator["eq"] = dateFilter.Eq
	}
	if dateFilter.GT != "" {
		comparator["gt"] = dateFilter.GT
	}
	if dateFilter.GTE != "" {
		comparator["gte"] = dateFilter.GTE
	}
	if dateFilter.LT != "" {
		comparator["lt"] = dateFilter.LT
	}
	if dateFilter.LTE != "" {
		comparator["lte"] = dateFilter.LTE
	}
	if dateFilter.Null != nil {
		comparator["null"] = *dateFilter.Null
	}
	return comparator
}

func buildNumberComparator(numberFilter NumberFilter) map[string]interface{} {
	comparator := make(map[string]interface{})
	if numberFilter.Eq != nil {
		comparator["eq"] = *numberFilter.Eq
	}
	if numberFilter.GT != nil {
		comparator["gt"] = *numberFilter.GT
	}
	if numberFilter.GTE != nil {
		comparator["gte"] = *numberFilter.GTE
	}
	if numberFilter.LT != nil {
		comparator["lt"] = *numberFilter.LT
	}
	if numberFilter.LTE != nil {
		comparator["lte"] = *numberFilter.LTE
	}
	if numberFilter.Null != nil {
		comparator["null"] = *numberFilter.Null
	}
	return comparator
}

func appendIssueAndFilters(filter IssueFilter, filters ...map[string]interface{}) {
	if len(filters) == 0 {
		return
	}
	existing, _ := filter["and"].([]map[string]interface{})
	existing = append(existing, filters...)
	filter["and"] = existing
}

// buildIssueFilter builds the GraphQL issue filter for the given params.
func buildIssueFilter(params FetchIssuesParams) IssueFilter {
	filter := buildStructuredIssueFilter(params)

	searchTerm := strings.TrimSpace(params.Search)
	if searchTerm == "" {
		return filter
	}

	terms := strings.Fields(searchTerm)
	if len(terms) == 1 {
		filter["or"] = buildSearchOrFilters(terms[0])
		return filter
	}

	// Require every term to match at least one field for free-text search.
	andFilters := make([]map[string]interface{}, 0, len(terms))
	for _, term := range terms {
		andFilters = append(andFilters, map[string]interface{}{
			"or": buildSearchOrFilters(term),
		})
	}
	appendIssueAndFilters(filter, andFilters...)
	return filter
}

// buildSearchOrFilters returns per-term OR filters for issue search.
// Note: identifier is not a filterable field in Linear's IssueFilter type,
// so we only filter by title and description.
func buildSearchOrFilters(term string) []map[string]interface{} {
	return []map[string]interface{}{
		{"title": map[string]interface{}{"containsIgnoreCase": term}},
		{"description": map[string]interface{}{"containsIgnoreCase": term}},
	}
}

// FetchIssuesPage fetches a single page of issues with optional filtering and sorting.
// It returns pagination metadata to allow callers to continue fetching.
func (c *Client) FetchIssuesPage(ctx context.Context, params FetchIssuesParams, after *string) (IssuePage, error) {
	searchTerm := strings.TrimSpace(params.Search)
	if searchTerm != "" {
		params.Search = searchTerm
		return c.searchIssuesPage(ctx, params, after)
	}

	return c.fetchIssuesWithFilterPage(ctx, params, after)
}

// FetchIssues fetches issues with optional filtering and sorting.
// When a search term is provided, it uses Linear's searchIssues query which
// supports searching by identifier, title, description, and comments.
func (c *Client) FetchIssues(ctx context.Context, params FetchIssuesParams) ([]Issue, error) {
	// If search term is provided, use searchIssues query for better identifier matching.
	searchTerm := strings.TrimSpace(params.Search)
	if searchTerm != "" {
		params.Search = searchTerm
		return c.searchIssues(ctx, params)
	}

	return c.fetchIssuesWithFilter(ctx, params)
}

// searchIssues uses Linear's searchIssues query which supports full-text search
// including identifier, title, description, and comments.
func (c *Client) searchIssues(ctx context.Context, params FetchIssuesParams) ([]Issue, error) {
	sortByPriority := params.OrderBy == "priority"

	var after *string
	page := 0
	issues := make([]Issue, 0)
	for {
		pageResult, err := c.FetchIssuesPage(ctx, params, after)
		if err != nil {
			return nil, err
		}

		issues = append(issues, pageResult.Issues...)
		page++
		if params.OnProgress != nil {
			params.OnProgress(IssueFetchProgress{
				Page:    page,
				Fetched: len(issues),
			})
		}

		if !pageResult.HasNext {
			break
		}
		after = pageResult.EndCursor
	}

	// Sort by priority client-side if requested.
	if sortByPriority {
		c.sortByPriority(issues)
	}

	return issues, nil
}

// searchIssuesPage fetches a single page of issues using Linear's searchIssues query.
//
//nolint:dupl // GraphQL library requires inline struct definitions; duplication with fetchIssuesWithFilterPage is unavoidable.
func (c *Client) searchIssuesPage(ctx context.Context, params FetchIssuesParams, after *string) (IssuePage, error) {
	first := params.First
	if first <= 0 {
		first = 50
	}

	searchTerm := strings.TrimSpace(params.Search)
	// Build filter for team/project/state constraints only (search handles the text matching).
	filter := buildStructuredIssueFilter(params)

	var afterCursor *graphql.String
	if after != nil {
		cursor := graphql.String(*after)
		afterCursor = &cursor
	}

	var query struct {
		SearchIssues struct {
			Nodes []struct {
				ID         graphql.String
				Identifier graphql.String
				Title      graphql.String
				State      struct {
					ID   graphql.String
					Name graphql.String
				}
				Assignee *struct {
					ID   graphql.String
					Name graphql.String
				}
				Priority    graphql.Float
				UpdatedAt   graphql.String
				CreatedAt   graphql.String
				Description *graphql.String
				Team        struct {
					ID graphql.String
				}
				Project *struct {
					ID graphql.String
				}
				Cycle *struct {
					ID         graphql.String
					Name       *graphql.String
					Number     graphql.Float
					StartsAt   graphql.String
					EndsAt     graphql.String
					IsActive   graphql.Boolean
					IsFuture   graphql.Boolean
					IsPast     graphql.Boolean
					IsNext     graphql.Boolean
					IsPrevious graphql.Boolean
				}
				DueDate          *graphql.String
				Estimate         *graphql.Float
				ProjectMilestone *struct {
					ID         graphql.String
					Name       graphql.String
					TargetDate *graphql.String
					Status     graphql.String
					Project    struct {
						ID graphql.String
					}
				}
				Labels struct {
					Nodes []struct {
						ID    graphql.String
						Name  graphql.String
						Color graphql.String
					}
				}
				URL        graphql.String
				ArchivedAt *graphql.String
				Parent     *struct {
					ID         graphql.String
					Identifier graphql.String
					Title      graphql.String
				}
				Children struct {
					Nodes []struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
						State      struct {
							ID   graphql.String
							Name graphql.String
						}
					}
				}
			}
			PageInfo struct {
				HasNextPage graphql.Boolean
				EndCursor   graphql.String
			}
		} `graphql:"searchIssues(term: $term, first: $first, after: $after, filter: $filter)"`
	}

	variables := map[string]interface{}{
		"term":   graphql.String(searchTerm),
		"first":  graphql.Int(first),
		"filter": filter,
		"after":  afterCursor,
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: searchIssues failed")
		return IssuePage{}, fmt.Errorf("search issues: %w", err)
	}

	issues := make([]Issue, 0, len(query.SearchIssues.Nodes))
	for _, node := range query.SearchIssues.Nodes {
		issue := c.parseIssueNode(node)
		issues = append(issues, issue)
	}

	hasNext := bool(query.SearchIssues.PageInfo.HasNextPage)
	var endCursor *string
	if hasNext {
		cursor := string(query.SearchIssues.PageInfo.EndCursor)
		endCursor = &cursor
	}

	return IssuePage{
		Issues:    issues,
		HasNext:   hasNext,
		EndCursor: endCursor,
	}, nil
}

// fetchIssuesWithFilter fetches issues using the standard issues query with filters.
func (c *Client) fetchIssuesWithFilter(ctx context.Context, params FetchIssuesParams) ([]Issue, error) {
	// Determine if client-side sorting is needed.
	// Linear API only supports "createdAt" and "updatedAt" for PaginationOrderBy.
	// For "priority" sorting, we fetch by updatedAt and sort client-side.
	sortByPriority := params.OrderBy == "priority"

	var after *string
	page := 0
	issues := make([]Issue, 0)
	for {
		pageResult, err := c.FetchIssuesPage(ctx, params, after)
		if err != nil {
			return nil, err
		}

		issues = append(issues, pageResult.Issues...)
		page++
		if params.OnProgress != nil {
			params.OnProgress(IssueFetchProgress{
				Page:    page,
				Fetched: len(issues),
			})
		}

		if !pageResult.HasNext {
			break
		}
		after = pageResult.EndCursor
	}

	// Sort by priority client-side if requested.
	if sortByPriority {
		c.sortByPriority(issues)
	}

	return issues, nil
}

// fetchIssuesWithFilterPage fetches a single page of issues using the standard issues query.
//
//nolint:dupl // GraphQL library requires inline struct definitions; duplication with searchIssuesPage is unavoidable.
func (c *Client) fetchIssuesWithFilterPage(ctx context.Context, params FetchIssuesParams, after *string) (IssuePage, error) {
	first := params.First
	if first <= 0 {
		first = 50
	}

	// Build filter.
	filter := buildIssueFilter(params)

	// Determine if client-side sorting is needed.
	// Linear API only supports "createdAt" and "updatedAt" for PaginationOrderBy.
	// For "priority" sorting, we fetch by updatedAt and sort client-side.
	sortByPriority := params.OrderBy == "priority"

	orderBy := PaginationOrderBy(params.OrderBy)
	if orderBy == "" || sortByPriority {
		orderBy = OrderByUpdatedAt
	}

	var afterCursor *graphql.String
	if after != nil {
		cursor := graphql.String(*after)
		afterCursor = &cursor
	}

	var query struct {
		Issues struct {
			Nodes []struct {
				ID         graphql.String
				Identifier graphql.String
				Title      graphql.String
				State      struct {
					ID   graphql.String
					Name graphql.String
				}
				Assignee *struct {
					ID   graphql.String
					Name graphql.String
				}
				Priority    graphql.Float
				UpdatedAt   graphql.String
				CreatedAt   graphql.String
				Description *graphql.String
				Team        struct {
					ID graphql.String
				}
				Project *struct {
					ID graphql.String
				}
				Cycle *struct {
					ID         graphql.String
					Name       *graphql.String
					Number     graphql.Float
					StartsAt   graphql.String
					EndsAt     graphql.String
					IsActive   graphql.Boolean
					IsFuture   graphql.Boolean
					IsPast     graphql.Boolean
					IsNext     graphql.Boolean
					IsPrevious graphql.Boolean
				}
				DueDate          *graphql.String
				Estimate         *graphql.Float
				ProjectMilestone *struct {
					ID         graphql.String
					Name       graphql.String
					TargetDate *graphql.String
					Status     graphql.String
					Project    struct {
						ID graphql.String
					}
				}
				Labels struct {
					Nodes []struct {
						ID    graphql.String
						Name  graphql.String
						Color graphql.String
					}
				}
				URL        graphql.String
				ArchivedAt *graphql.String
				Parent     *struct {
					ID         graphql.String
					Identifier graphql.String
					Title      graphql.String
				}
				Children struct {
					Nodes []struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
						State      struct {
							ID   graphql.String
							Name graphql.String
						}
					}
				}
			}
			PageInfo struct {
				HasNextPage graphql.Boolean
				EndCursor   graphql.String
			}
		} `graphql:"issues(first: $first, after: $after, filter: $filter, orderBy: $orderBy)"`
	}

	variables := map[string]interface{}{
		"first":   graphql.Int(first),
		"filter":  filter,
		"orderBy": orderBy,
		"after":   afterCursor,
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: FetchIssues failed")
		return IssuePage{}, fmt.Errorf("fetch issues: %w", err)
	}

	issues := make([]Issue, 0, len(query.Issues.Nodes))
	for _, node := range query.Issues.Nodes {
		issue := c.parseIssueNode(node)
		issues = append(issues, issue)
	}

	hasNext := bool(query.Issues.PageInfo.HasNextPage)
	var endCursor *string
	if hasNext {
		cursor := string(query.Issues.PageInfo.EndCursor)
		endCursor = &cursor
	}

	return IssuePage{
		Issues:    issues,
		HasNext:   hasNext,
		EndCursor: endCursor,
	}, nil
}

func parseCycleRefValue(v reflect.Value) *CycleRef {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	id := reflectStringField(v, "ID")
	if id == "" {
		return nil
	}

	return &CycleRef{
		ID:         id,
		Name:       reflectStringField(v, "Name"),
		Number:     reflectIntField(v, "Number"),
		StartsAt:   parseTime(reflectStringField(v, "StartsAt")),
		EndsAt:     parseTime(reflectStringField(v, "EndsAt")),
		IsActive:   reflectBoolField(v, "IsActive"),
		IsFuture:   reflectBoolField(v, "IsFuture"),
		IsPast:     reflectBoolField(v, "IsPast"),
		IsNext:     reflectBoolField(v, "IsNext"),
		IsPrevious: reflectBoolField(v, "IsPrevious"),
	}
}

func parseProjectMilestoneRefValue(v reflect.Value) *ProjectMilestoneRef {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	id := reflectStringField(v, "ID")
	if id == "" {
		return nil
	}

	var targetDate *string
	if value := reflectStringField(v, "TargetDate"); value != "" {
		targetDate = &value
	}
	projectID := ""
	projectField := v.FieldByName("Project")
	if projectField.IsValid() {
		projectID = reflectStringField(projectField, "ID")
	}

	return &ProjectMilestoneRef{
		ID:         id,
		Name:       reflectStringField(v, "Name"),
		ProjectID:  projectID,
		TargetDate: targetDate,
		Status:     reflectStringField(v, "Status"),
		SortOrder:  reflectFloatField(v, "SortOrder"),
		Progress:   reflectFloatField(v, "Progress"),
	}
}

func reflectStringField(v reflect.Value, name string) string {
	if !v.IsValid() {
		return ""
	}
	field := v.FieldByName(name)
	if !field.IsValid() {
		return ""
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return ""
		}
		field = field.Elem()
	}
	if field.Kind() == reflect.String {
		return field.String()
	}
	return ""
}

func reflectStringPointerField(v reflect.Value, name string) *string {
	value := reflectStringField(v, name)
	if value == "" {
		return nil
	}
	return &value
}

func reflectIntField(v reflect.Value, name string) int {
	if !v.IsValid() {
		return 0
	}
	field := v.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		return int(field.Float())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(field.Int())
	default:
		return 0
	}
}

func reflectFloatField(v reflect.Value, name string) float64 {
	if !v.IsValid() {
		return 0
	}
	field := v.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return 0
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		return field.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(field.Int())
	default:
		return 0
	}
}

func reflectFloatPointerField(v reflect.Value, name string) *float64 {
	if !v.IsValid() {
		return nil
	}
	field := v.FieldByName(name)
	if !field.IsValid() {
		return nil
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil
		}
		field = field.Elem()
	}
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		value := field.Float()
		return &value
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := float64(field.Int())
		return &value
	default:
		return nil
	}
}

func reflectBoolField(v reflect.Value, name string) bool {
	if !v.IsValid() {
		return false
	}
	field := v.FieldByName(name)
	if !field.IsValid() {
		return false
	}
	if field.Kind() == reflect.Bool {
		return field.Bool()
	}
	return false
}

func parseCycleRef(node interface{}) *CycleRef {
	return parseCycleRefValue(reflect.ValueOf(node))
}

func parseIssueRefValue(v reflect.Value) IssueRef {
	return IssueRef{
		ID:         reflectStringField(v, "ID"),
		Identifier: reflectStringField(v, "Identifier"),
		Title:      reflectStringField(v, "Title"),
	}
}

func parseIssueRelationNodes(v reflect.Value, inverse bool) []IssueRelation {
	if !v.IsValid() {
		return nil
	}
	nodesField := v.FieldByName("Nodes")
	if !nodesField.IsValid() {
		return nil
	}
	relations := make([]IssueRelation, 0, nodesField.Len())
	for i := 0; i < nodesField.Len(); i++ {
		node := nodesField.Index(i)
		relations = append(relations, IssueRelation{
			ID:           reflectStringField(node, "ID"),
			Type:         reflectStringField(node, "Type"),
			Issue:        parseIssueRefValue(node.FieldByName("Issue")),
			RelatedIssue: parseIssueRefValue(node.FieldByName("RelatedIssue")),
			Inverse:      inverse,
		})
	}
	return relations
}

func parseUserNodes(v reflect.Value) []User {
	if !v.IsValid() {
		return nil
	}
	nodesField := v.FieldByName("Nodes")
	if !nodesField.IsValid() {
		return nil
	}
	users := make([]User, 0, nodesField.Len())
	for i := 0; i < nodesField.Len(); i++ {
		node := nodesField.Index(i)
		users = append(users, User{
			ID:          reflectStringField(node, "ID"),
			Name:        reflectStringField(node, "Name"),
			DisplayName: reflectStringField(node, "DisplayName"),
			Email:       reflectStringField(node, "Email"),
			IsMe:        reflectBoolField(node, "IsMe"),
		})
	}
	return users
}

func parseAttachmentNodes(v reflect.Value) []Attachment {
	if !v.IsValid() {
		return nil
	}
	nodesField := v.FieldByName("Nodes")
	if !nodesField.IsValid() {
		return nil
	}
	attachments := make([]Attachment, 0, nodesField.Len())
	for i := 0; i < nodesField.Len(); i++ {
		node := nodesField.Index(i)
		attachments = append(attachments, Attachment{
			ID:         reflectStringField(node, "ID"),
			Title:      reflectStringField(node, "Title"),
			Subtitle:   reflectStringField(node, "Subtitle"),
			URL:        reflectStringField(node, "URL"),
			SourceType: reflectStringField(node, "SourceType"),
			CreatedAt:  parseTime(reflectStringField(node, "CreatedAt")),
			UpdatedAt:  parseTime(reflectStringField(node, "UpdatedAt")),
		})
	}
	return attachments
}

// parseIssueNode converts a GraphQL issue node to an Issue struct.
func (c *Client) parseIssueNode(node interface{}) Issue {
	// Use type assertion to handle the node
	// This is a workaround since Go generics with GraphQL structs are complex
	v := reflect.ValueOf(node)

	id := v.FieldByName("ID").String()
	identifier := v.FieldByName("Identifier").String()
	title := v.FieldByName("Title").String()

	stateField := v.FieldByName("State")
	stateID := stateField.FieldByName("ID").String()
	stateName := stateField.FieldByName("Name").String()

	updatedAt := parseTime(v.FieldByName("UpdatedAt").String())
	createdAt := parseTime(v.FieldByName("CreatedAt").String())

	priority := int(v.FieldByName("Priority").Float())

	assignee := ""
	assigneeID := ""
	assigneeField := v.FieldByName("Assignee")
	if assigneeField.IsValid() && assigneeField.Kind() == reflect.Pointer && !assigneeField.IsNil() {
		assigneeID = assigneeField.Elem().FieldByName("ID").String()
		assignee = assigneeField.Elem().FieldByName("Name").String()
	}

	description := ""
	descField := v.FieldByName("Description")
	if descField.IsValid() && descField.Kind() == reflect.Pointer && !descField.IsNil() {
		description = descField.Elem().String()
	}

	teamID := v.FieldByName("Team").FieldByName("ID").String()

	projectID := ""
	projectField := v.FieldByName("Project")
	if projectField.IsValid() && projectField.Kind() == reflect.Pointer && !projectField.IsNil() {
		projectID = projectField.Elem().FieldByName("ID").String()
	}

	cycle := parseCycleRefValue(v.FieldByName("Cycle"))
	dueDate := reflectStringPointerField(v, "DueDate")
	estimate := reflectFloatPointerField(v, "Estimate")
	projectMilestone := parseProjectMilestoneRefValue(v.FieldByName("ProjectMilestone"))

	url := v.FieldByName("URL").String()

	archivedField := v.FieldByName("ArchivedAt")
	archived := archivedField.IsValid() && archivedField.Kind() == reflect.Pointer && !archivedField.IsNil()

	// Parse labels
	labels := make([]IssueLabel, 0)
	labelsConn := v.FieldByName("Labels")
	if labelsConn.IsValid() {
		labelsField := labelsConn.FieldByName("Nodes")
		labels = make([]IssueLabel, 0, labelsField.Len())
		for i := 0; i < labelsField.Len(); i++ {
			lbl := labelsField.Index(i)
			labels = append(labels, IssueLabel{
				ID:    lbl.FieldByName("ID").String(),
				Name:  lbl.FieldByName("Name").String(),
				Color: lbl.FieldByName("Color").String(),
			})
		}
	}

	// Parse parent
	var parent *IssueRef
	parentField := v.FieldByName("Parent")
	if parentField.IsValid() && parentField.Kind() == reflect.Pointer && !parentField.IsNil() {
		parent = &IssueRef{
			ID:         parentField.Elem().FieldByName("ID").String(),
			Identifier: parentField.Elem().FieldByName("Identifier").String(),
			Title:      parentField.Elem().FieldByName("Title").String(),
		}
	}

	// Parse children
	children := make([]IssueChildRef, 0)
	childrenConn := v.FieldByName("Children")
	if childrenConn.IsValid() {
		childrenField := childrenConn.FieldByName("Nodes")
		children = make([]IssueChildRef, 0, childrenField.Len())
		for i := 0; i < childrenField.Len(); i++ {
			child := childrenField.Index(i)
			children = append(children, IssueChildRef{
				ID:         child.FieldByName("ID").String(),
				Identifier: child.FieldByName("Identifier").String(),
				Title:      child.FieldByName("Title").String(),
				State:      child.FieldByName("State").FieldByName("Name").String(),
				StateID:    child.FieldByName("State").FieldByName("ID").String(),
			})
		}
	}

	relations := make([]IssueRelation, 0)
	relations = append(relations, parseIssueRelationNodes(v.FieldByName("Relations"), false)...)
	relations = append(relations, parseIssueRelationNodes(v.FieldByName("InverseRelations"), true)...)
	subscribers := parseUserNodes(v.FieldByName("Subscribers"))
	attachments := parseAttachmentNodes(v.FieldByName("Attachments"))

	return Issue{
		ID:               id,
		Identifier:       identifier,
		Title:            title,
		State:            stateName,
		StateID:          stateID,
		Assignee:         assignee,
		AssigneeID:       assigneeID,
		Priority:         priority,
		UpdatedAt:        updatedAt,
		CreatedAt:        createdAt,
		Description:      description,
		TeamID:           teamID,
		ProjectID:        projectID,
		Cycle:            cycle,
		DueDate:          dueDate,
		Estimate:         estimate,
		ProjectMilestone: projectMilestone,
		URL:              url,
		Archived:         archived,
		Labels:           labels,
		Parent:           parent,
		Children:         children,
		Relations:        relations,
		Subscribers:      subscribers,
		Attachments:      attachments,
	}
}

// sortByPriority sorts issues by priority.
// Linear priority: 0 = No priority, 1 = Urgent, 2 = High, 3 = Normal, 4 = Low.
// We sort with Urgent (1) first, then High (2), Normal (3), Low (4), and No priority (0) last.
func (c *Client) sortByPriority(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		pi, pj := issues[i].Priority, issues[j].Priority
		// Map 0 (no priority) to a high value so it sorts last
		if pi == 0 {
			pi = 5
		}
		if pj == 0 {
			pj = 5
		}
		return pi < pj
	})
}

// FetchIssueByID fetches a single issue by its ID.
func (c *Client) FetchIssueByID(ctx context.Context, id string) (Issue, error) {
	var query struct {
		Issue struct {
			ID         graphql.String
			Identifier graphql.String
			Title      graphql.String
			State      struct {
				ID   graphql.String
				Name graphql.String
			}
			Assignee *struct {
				ID   graphql.String
				Name graphql.String
			}
			Priority    graphql.Float
			UpdatedAt   graphql.String
			CreatedAt   graphql.String
			Description *graphql.String
			Team        struct {
				ID graphql.String
			}
			Project *struct {
				ID graphql.String
			}
			Cycle *struct {
				ID         graphql.String
				Name       *graphql.String
				Number     graphql.Float
				StartsAt   graphql.String
				EndsAt     graphql.String
				IsActive   graphql.Boolean
				IsFuture   graphql.Boolean
				IsPast     graphql.Boolean
				IsNext     graphql.Boolean
				IsPrevious graphql.Boolean
			}
			DueDate          *graphql.String
			Estimate         *graphql.Float
			ProjectMilestone *struct {
				ID         graphql.String
				Name       graphql.String
				TargetDate *graphql.String
				Status     graphql.String
				Project    struct {
					ID graphql.String
				}
			}
			Labels struct {
				Nodes []struct {
					ID    graphql.String
					Name  graphql.String
					Color graphql.String
				}
			}
			URL        graphql.String
			ArchivedAt *graphql.String
			Parent     *struct {
				ID         graphql.String
				Identifier graphql.String
				Title      graphql.String
			}
			Children struct {
				Nodes []struct {
					ID         graphql.String
					Identifier graphql.String
					Title      graphql.String
					State      struct {
						ID   graphql.String
						Name graphql.String
					}
				}
			}
			Relations struct {
				Nodes []struct {
					ID    graphql.String
					Type  graphql.String
					Issue struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
					}
					RelatedIssue struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
					}
				}
			} `graphql:"relations(first: 50)"`
			InverseRelations struct {
				Nodes []struct {
					ID    graphql.String
					Type  graphql.String
					Issue struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
					}
					RelatedIssue struct {
						ID         graphql.String
						Identifier graphql.String
						Title      graphql.String
					}
				}
			} `graphql:"inverseRelations(first: 50)"`
			Subscribers struct {
				Nodes []struct {
					ID          graphql.String
					Name        graphql.String
					DisplayName graphql.String
					Email       graphql.String
					IsMe        graphql.Boolean
				}
			} `graphql:"subscribers(first: 50)"`
			Attachments struct {
				Nodes []struct {
					ID         graphql.String
					Title      graphql.String
					Subtitle   *graphql.String
					URL        graphql.String
					SourceType *graphql.String
					CreatedAt  graphql.String
					UpdatedAt  graphql.String
				}
			} `graphql:"attachments(first: 50)"`
			Comments struct {
				Nodes []struct {
					ID        graphql.String
					Body      graphql.String
					CreatedAt graphql.String
					UpdatedAt graphql.String
					User      struct {
						ID          graphql.String
						Name        graphql.String
						DisplayName graphql.String
						Email       graphql.String
						IsMe        graphql.Boolean
					}
				}
			} `graphql:"comments(first: 100, orderBy: createdAt)"`
		} `graphql:"issue(id: $id)"`
	}

	variables := map[string]interface{}{
		"id": graphql.String(id),
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: FetchIssueByID failed issue_id=%s", id)
		return Issue{}, fmt.Errorf("fetch issue %s: %w", id, err)
	}

	issue := c.parseIssueNode(query.Issue)

	// Parse comments
	comments := make([]Comment, 0, len(query.Issue.Comments.Nodes))
	for _, node := range query.Issue.Comments.Nodes {
		commentCreatedAt := parseTime(string(node.CreatedAt))
		commentUpdatedAt := parseTime(string(node.UpdatedAt))
		comments = append(comments, Comment{
			ID:        string(node.ID),
			Body:      string(node.Body),
			CreatedAt: commentCreatedAt,
			UpdatedAt: commentUpdatedAt,
			Author: User{
				ID:          string(node.User.ID),
				Name:        string(node.User.Name),
				DisplayName: string(node.User.DisplayName),
				Email:       string(node.User.Email),
				IsMe:        bool(node.User.IsMe),
			},
			IssueID: string(query.Issue.ID),
		})
	}

	issue.Comments = comments
	return issue, nil
}

// CreateIssue creates a new issue.
func (c *Client) CreateIssue(ctx context.Context, input CreateIssueInput) (Issue, error) {
	var mutation struct {
		IssueCreate struct {
			Success graphql.Boolean
			Issue   struct {
				ID         graphql.String
				Identifier graphql.String
				Title      graphql.String
				State      struct {
					ID   graphql.String
					Name graphql.String
				}
				Assignee *struct {
					ID   graphql.String
					Name graphql.String
				}
				Priority    graphql.Float
				UpdatedAt   graphql.String
				CreatedAt   graphql.String
				Description *graphql.String
				Team        struct {
					ID graphql.String
				}
				Project *struct {
					ID graphql.String
				}
				Cycle *struct {
					ID         graphql.String
					Name       *graphql.String
					Number     graphql.Float
					StartsAt   graphql.String
					EndsAt     graphql.String
					IsActive   graphql.Boolean
					IsFuture   graphql.Boolean
					IsPast     graphql.Boolean
					IsNext     graphql.Boolean
					IsPrevious graphql.Boolean
				}
				DueDate          *graphql.String
				Estimate         *graphql.Float
				ProjectMilestone *struct {
					ID         graphql.String
					Name       graphql.String
					TargetDate *graphql.String
					Status     graphql.String
					Project    struct {
						ID graphql.String
					}
				}
				Labels struct {
					Nodes []struct {
						ID    graphql.String
						Name  graphql.String
						Color graphql.String
					}
				}
				URL graphql.String
			}
		} `graphql:"issueCreate(input: $input)"`
	}

	// Build input object
	issueInput := make(IssueCreateInput)
	issueInput["teamId"] = graphql.ID(input.TeamID)
	issueInput["title"] = graphql.String(input.Title)
	if input.Description != "" {
		issueInput["description"] = graphql.String(input.Description)
	}
	if input.ProjectID != "" {
		issueInput["projectId"] = graphql.ID(input.ProjectID)
	}
	if input.StateID != "" {
		issueInput["stateId"] = graphql.ID(input.StateID)
	}
	if input.CycleID != "" {
		issueInput["cycleId"] = graphql.ID(input.CycleID)
	}
	if input.AssigneeID != "" {
		issueInput["assigneeId"] = graphql.ID(input.AssigneeID)
	}
	if input.Priority > 0 {
		issueInput["priority"] = graphql.Int(input.Priority)
	}
	if input.ParentID != "" {
		issueInput["parentId"] = graphql.ID(input.ParentID)
	}

	variables := map[string]interface{}{
		"input": issueInput,
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateIssue failed")
		return Issue{}, fmt.Errorf("create issue: %w", err)
	}

	if !bool(mutation.IssueCreate.Success) {
		logger.Error("linearapi.client: CreateIssue operation failed success=false")
		return Issue{}, fmt.Errorf("create issue: operation failed")
	}

	node := mutation.IssueCreate.Issue
	updatedAt := parseTime(string(node.UpdatedAt))
	createdAt := parseTime(string(node.CreatedAt))

	assignee := ""
	assigneeID := ""
	if node.Assignee != nil {
		assignee = string(node.Assignee.Name)
		assigneeID = string(node.Assignee.ID)
	}

	description := ""
	if node.Description != nil {
		description = string(*node.Description)
	}

	projectID := ""
	if node.Project != nil {
		projectID = string(node.Project.ID)
	}

	cycle := parseCycleRef(node.Cycle)

	// Parse labels
	labels := make([]IssueLabel, 0, len(node.Labels.Nodes))
	for _, lbl := range node.Labels.Nodes {
		labels = append(labels, IssueLabel{
			ID:    string(lbl.ID),
			Name:  string(lbl.Name),
			Color: string(lbl.Color),
		})
	}

	return Issue{
		ID:          string(node.ID),
		Identifier:  string(node.Identifier),
		Title:       string(node.Title),
		State:       string(node.State.Name),
		StateID:     string(node.State.ID),
		Assignee:    assignee,
		AssigneeID:  assigneeID,
		Priority:    int(node.Priority),
		UpdatedAt:   updatedAt,
		CreatedAt:   createdAt,
		Description: description,
		TeamID:      string(node.Team.ID),
		ProjectID:   projectID,
		Cycle:       cycle,
		URL:         string(node.URL),
		Labels:      labels,
	}, nil
}

// UpdateIssue updates an existing issue.
func (c *Client) UpdateIssue(ctx context.Context, input UpdateIssueInput) (Issue, error) {
	var mutation struct {
		IssueUpdate struct {
			Success graphql.Boolean
			Issue   issueMutationNode
		} `graphql:"issueUpdate(id: $id, input: $input)"`
	}

	// Build input object with only provided fields
	issueInput := make(IssueUpdateInput)
	if input.Title != nil {
		issueInput["title"] = graphql.String(*input.Title)
	}
	if input.Description != nil {
		issueInput["description"] = graphql.String(*input.Description)
	}
	if input.StateID != nil {
		issueInput["stateId"] = graphql.ID(*input.StateID)
	}
	if input.CycleID != nil {
		if *input.CycleID == "" {
			issueInput["cycleId"] = (*graphql.ID)(nil)
		} else {
			issueInput["cycleId"] = graphql.ID(*input.CycleID)
		}
	}
	if input.AssigneeID != nil {
		if *input.AssigneeID == "" {
			// Unassign by passing null
			issueInput["assigneeId"] = (*graphql.ID)(nil)
		} else {
			issueInput["assigneeId"] = graphql.ID(*input.AssigneeID)
		}
	}
	if input.Priority != nil {
		issueInput["priority"] = graphql.Int(*input.Priority)
	}
	if input.LabelIDs != nil {
		// Convert string slice to []graphql.ID for the GraphQL mutation
		labelIDs := make([]graphql.ID, len(*input.LabelIDs))
		for i, id := range *input.LabelIDs {
			labelIDs[i] = graphql.ID(id)
		}
		issueInput["labelIds"] = labelIDs
	}
	if input.ParentID != nil {
		if *input.ParentID == "" {
			// Remove parent by passing null
			issueInput["parentId"] = (*graphql.ID)(nil)
		} else {
			issueInput["parentId"] = graphql.ID(*input.ParentID)
		}
	}
	if input.DueDate != nil {
		if *input.DueDate == "" {
			issueInput["dueDate"] = (*graphql.String)(nil)
		} else {
			issueInput["dueDate"] = graphql.String(*input.DueDate)
		}
	}
	if input.ClearEstimate {
		issueInput["estimate"] = (*graphql.Float)(nil)
	} else if input.Estimate != nil {
		issueInput["estimate"] = graphql.Float(*input.Estimate)
	}
	if input.ProjectMilestoneID != nil {
		if *input.ProjectMilestoneID == "" {
			issueInput["projectMilestoneId"] = (*graphql.ID)(nil)
		} else {
			issueInput["projectMilestoneId"] = graphql.ID(*input.ProjectMilestoneID)
		}
	}

	variables := map[string]interface{}{
		"id":    graphql.String(input.ID),
		"input": issueInput,
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateIssue failed issue_id=%s", input.ID)
		return Issue{}, fmt.Errorf("update issue %s: %w", input.ID, err)
	}

	if !bool(mutation.IssueUpdate.Success) {
		logger.Error("linearapi.client: UpdateIssue operation failed success=false issue_id=%s", input.ID)
		return Issue{}, fmt.Errorf("update issue %s: operation failed", input.ID)
	}

	return c.parseIssueNode(mutation.IssueUpdate.Issue), nil
}

// CreateIssueRelation creates a relation between two issues.
func (c *Client) CreateIssueRelation(ctx context.Context, input CreateIssueRelationInput) (IssueRelation, error) {
	var mutation struct {
		IssueRelationCreate struct {
			Success       graphql.Boolean
			IssueRelation struct {
				ID    graphql.String
				Type  graphql.String
				Issue struct {
					ID         graphql.String
					Identifier graphql.String
					Title      graphql.String
				}
				RelatedIssue struct {
					ID         graphql.String
					Identifier graphql.String
					Title      graphql.String
				}
			}
		} `graphql:"issueRelationCreate(input: $input)"`
	}

	relationInput := IssueRelationCreateInput{
		"issueId":        graphql.String(input.IssueID),
		"relatedIssueId": graphql.String(input.RelatedIssueID),
		"type":           string(input.Type),
	}
	variables := map[string]interface{}{
		"input": relationInput,
	}

	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateIssueRelation failed issue_id=%s related_issue_id=%s", input.IssueID, input.RelatedIssueID)
		return IssueRelation{}, fmt.Errorf("create issue relation: %w", err)
	}
	if !bool(mutation.IssueRelationCreate.Success) {
		return IssueRelation{}, fmt.Errorf("create issue relation: operation failed")
	}

	node := mutation.IssueRelationCreate.IssueRelation
	return IssueRelation{
		ID:   string(node.ID),
		Type: string(node.Type),
		Issue: IssueRef{
			ID:         string(node.Issue.ID),
			Identifier: string(node.Issue.Identifier),
			Title:      string(node.Issue.Title),
		},
		RelatedIssue: IssueRef{
			ID:         string(node.RelatedIssue.ID),
			Identifier: string(node.RelatedIssue.Identifier),
			Title:      string(node.RelatedIssue.Title),
		},
	}, nil
}

// DeleteIssueRelation deletes an issue relation.
func (c *Client) DeleteIssueRelation(ctx context.Context, relationID string) error {
	var mutation struct {
		IssueRelationDelete struct {
			Success graphql.Boolean
		} `graphql:"issueRelationDelete(id: $id)"`
	}

	variables := map[string]interface{}{
		"id": graphql.String(relationID),
	}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: DeleteIssueRelation failed relation_id=%s", relationID)
		return fmt.Errorf("delete issue relation %s: %w", relationID, err)
	}
	if !bool(mutation.IssueRelationDelete.Success) {
		return fmt.Errorf("delete issue relation %s: operation failed", relationID)
	}
	return nil
}

// SubscribeToIssue subscribes the current user to an issue.
func (c *Client) SubscribeToIssue(ctx context.Context, issueID string) (Issue, error) {
	return c.setIssueSubscription(ctx, issueID, true)
}

// UnsubscribeFromIssue unsubscribes the current user from an issue.
func (c *Client) UnsubscribeFromIssue(ctx context.Context, issueID string) (Issue, error) {
	return c.setIssueSubscription(ctx, issueID, false)
}

func (c *Client) setIssueSubscription(ctx context.Context, issueID string, subscribe bool) (Issue, error) {
	if subscribe {
		var mutation struct {
			IssueSubscribe struct {
				Success graphql.Boolean
				Issue   issueMutationNode
			} `graphql:"issueSubscribe(id: $id)"`
		}
		variables := map[string]interface{}{"id": graphql.String(issueID)}
		if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
			return Issue{}, fmt.Errorf("subscribe to issue %s: %w", issueID, err)
		}
		if !bool(mutation.IssueSubscribe.Success) {
			return Issue{}, fmt.Errorf("subscribe to issue %s: operation failed", issueID)
		}
		return c.parseIssueNode(mutation.IssueSubscribe.Issue), nil
	}

	var mutation struct {
		IssueUnsubscribe struct {
			Success graphql.Boolean
			Issue   issueMutationNode
		} `graphql:"issueUnsubscribe(id: $id)"`
	}
	variables := map[string]interface{}{"id": graphql.String(issueID)}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		return Issue{}, fmt.Errorf("unsubscribe from issue %s: %w", issueID, err)
	}
	if !bool(mutation.IssueUnsubscribe.Success) {
		return Issue{}, fmt.Errorf("unsubscribe from issue %s: operation failed", issueID)
	}
	return c.parseIssueNode(mutation.IssueUnsubscribe.Issue), nil
}

// CreateComment creates a new comment on an issue.
func (c *Client) CreateComment(ctx context.Context, input CreateCommentInput) (Comment, error) {
	var mutation struct {
		CommentCreate struct {
			Success graphql.Boolean
			Comment struct {
				ID        graphql.String
				Body      graphql.String
				CreatedAt graphql.String
				UpdatedAt graphql.String
				User      struct {
					ID          graphql.String
					Name        graphql.String
					DisplayName graphql.String
					Email       graphql.String
					IsMe        graphql.Boolean
				}
			}
		} `graphql:"commentCreate(input: $input)"`
	}

	// Build input object
	commentInput := make(CommentCreateInput)
	commentInput["issueId"] = graphql.ID(input.IssueID)
	commentInput["body"] = graphql.String(input.Body)

	variables := map[string]interface{}{
		"input": commentInput,
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateComment failed issue_id=%s", input.IssueID)
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}

	if !bool(mutation.CommentCreate.Success) {
		logger.Error("linearapi.client: CreateComment operation failed success=false issue_id=%s", input.IssueID)
		return Comment{}, fmt.Errorf("create comment: operation failed")
	}

	node := mutation.CommentCreate.Comment
	createdAt := parseTime(string(node.CreatedAt))
	updatedAt := parseTime(string(node.UpdatedAt))

	return Comment{
		ID:        string(node.ID),
		Body:      string(node.Body),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Author: User{
			ID:          string(node.User.ID),
			Name:        string(node.User.Name),
			DisplayName: string(node.User.DisplayName),
			Email:       string(node.User.Email),
			IsMe:        bool(node.User.IsMe),
		},
		IssueID: input.IssueID,
	}, nil
}

// ArchiveIssue archives an issue.
func (c *Client) ArchiveIssue(ctx context.Context, issueID string) error {
	var mutation struct {
		IssueArchive struct {
			Success graphql.Boolean
		} `graphql:"issueArchive(id: $id)"`
	}

	variables := map[string]interface{}{
		"id": graphql.String(issueID),
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ArchiveIssue failed issue_id=%s", issueID)
		return fmt.Errorf("archive issue %s: %w", issueID, err)
	}

	if !bool(mutation.IssueArchive.Success) {
		logger.Error("linearapi.client: ArchiveIssue operation failed success=false issue_id=%s", issueID)
		return fmt.Errorf("archive issue %s: operation failed", issueID)
	}

	return nil
}

// UnarchiveIssue unarchives an issue.
func (c *Client) UnarchiveIssue(ctx context.Context, issueID string) error {
	var mutation struct {
		IssueUnarchive struct {
			Success graphql.Boolean
		} `graphql:"issueUnarchive(id: $id)"`
	}

	variables := map[string]interface{}{
		"id": graphql.String(issueID),
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UnarchiveIssue failed issue_id=%s", issueID)
		return fmt.Errorf("unarchive issue %s: %w", issueID, err)
	}

	if !bool(mutation.IssueUnarchive.Success) {
		logger.Error("linearapi.client: UnarchiveIssue operation failed success=false issue_id=%s", issueID)
		return fmt.Errorf("unarchive issue %s: operation failed", issueID)
	}

	return nil
}

// ListWorkspaceLabels fetches all workspace-level labels (not scoped to a team).
func (c *Client) ListWorkspaceLabels(ctx context.Context) ([]IssueLabel, error) {
	var query struct {
		IssueLabels struct {
			Nodes []struct {
				ID    graphql.String
				Name  graphql.String
				Color graphql.String
			}
		} `graphql:"issueLabels(first: 250)"`
	}

	err := c.client.Query(ctx, &query, nil)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListWorkspaceLabels failed")
		return nil, fmt.Errorf("list workspace labels: %w", err)
	}

	labels := make([]IssueLabel, 0, len(query.IssueLabels.Nodes))
	for _, node := range query.IssueLabels.Nodes {
		labels = append(labels, IssueLabel{
			ID:    string(node.ID),
			Name:  string(node.Name),
			Color: string(node.Color),
		})
	}

	return labels, nil
}

// ListTeamLabels fetches labels scoped to a specific team.
func (c *Client) ListTeamLabels(ctx context.Context, teamID string) ([]IssueLabel, error) {
	var query struct {
		Team struct {
			Labels struct {
				Nodes []struct {
					ID    graphql.String
					Name  graphql.String
					Color graphql.String
				}
			}
		} `graphql:"team(id: $teamId)"`
	}

	variables := map[string]interface{}{
		"teamId": graphql.String(teamID),
	}

	err := c.client.Query(ctx, &query, variables)
	if err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListTeamLabels failed team_id=%s", teamID)
		return nil, fmt.Errorf("list team labels for team %s: %w", teamID, err)
	}

	labels := make([]IssueLabel, 0, len(query.Team.Labels.Nodes))
	for _, node := range query.Team.Labels.Nodes {
		labels = append(labels, IssueLabel{
			ID:    string(node.ID),
			Name:  string(node.Name),
			Color: string(node.Color),
		})
	}

	return labels, nil
}

// ListIssueLabels fetches both workspace and team labels, merges them, and returns a sorted list.
// Labels are de-duplicated by ID, with team labels taking precedence.
func (c *Client) ListIssueLabels(ctx context.Context, teamID string) ([]IssueLabel, error) {
	// Fetch workspace labels
	workspaceLabels, err := c.ListWorkspaceLabels(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch team labels
	teamLabels, err := c.ListTeamLabels(ctx, teamID)
	if err != nil {
		return nil, err
	}

	// Merge and de-duplicate by ID (team labels override workspace labels if same ID)
	labelMap := make(map[string]IssueLabel)
	for _, lbl := range workspaceLabels {
		labelMap[lbl.ID] = lbl
	}
	for _, lbl := range teamLabels {
		labelMap[lbl.ID] = lbl
	}

	// Convert to slice and sort by name
	labels := make([]IssueLabel, 0, len(labelMap))
	for _, lbl := range labelMap {
		labels = append(labels, lbl)
	}
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Name < labels[j].Name
	})

	return labels, nil
}
