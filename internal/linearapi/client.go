package linearapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// Issue represents a Linear issue.
type Issue struct {
	ID          string
	Identifier  string
	Title       string
	Description string
	State       string
	StateID     string
	Assignee    string
	AssigneeID  string
	Priority    int
	UpdatedAt   time.Time
	CreatedAt   time.Time
	TeamID      string
	ProjectID   string
	URL         string
	Archived    bool
	Labels      []IssueLabel
	Parent      *IssueRef       // Parent issue reference (nil if top-level)
	Children    []IssueChildRef // Child/sub-issue references
	Comments    []Comment       // Comments on this issue
}

// IssueFetchProgress describes progress for a paginated issue fetch.
type IssueFetchProgress struct {
	Page    int
	Fetched int
}

// FetchIssuesParams contains parameters for fetching issues.
type FetchIssuesParams struct {
	TeamID    string
	ProjectID string
	Search    string
	// OrderBy specifies the sort order. Valid API values are "updatedAt" and "createdAt".
	// "priority" is also supported and will be sorted client-side after fetching.
	OrderBy string
	First   int
	// OnProgress is an optional callback invoked after each page is fetched.
	OnProgress func(IssueFetchProgress)
}

// CreateIssueInput contains input for creating a new issue.
type CreateIssueInput struct {
	TeamID      string
	Title       string
	Description string
	ProjectID   string
	StateID     string
	AssigneeID  string
	Priority    int
	ParentID    string // Parent issue ID (empty for top-level issues)
}

// UpdateIssueInput contains input for updating an issue.
type UpdateIssueInput struct {
	ID          string
	Title       *string
	Description *string
	StateID     *string
	AssigneeID  *string
	Priority    *int
	LabelIDs    *[]string // nil = no change, empty slice = clear all, non-empty = set labels
	ParentID    *string   // nil = no change, empty string = clear parent, non-empty = set parent
}

// CreateCommentInput contains input for creating a new comment.
type CreateCommentInput struct {
	IssueID string
	Body    string
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
		logger.ErrorWithErr(err, "API: ListTeams failed")
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
		logger.ErrorWithErr(err, "API: ListProjects failed for team %s", teamID)
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
		logger.ErrorWithErr(err, "API: ListUsers failed for team %s", teamID)
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
		logger.ErrorWithErr(err, "API: GetCurrentUser failed")
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
		logger.ErrorWithErr(err, "API: ListWorkflowStates failed for team %s", teamID)
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

// buildIssueFilter builds the GraphQL issue filter for the given params.
func buildIssueFilter(params FetchIssuesParams) IssueFilter {
	filter := make(IssueFilter)
	if params.TeamID != "" {
		filter["team"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.TeamID}}
	}
	if params.ProjectID != "" {
		filter["project"] = map[string]interface{}{"id": map[string]interface{}{"eq": params.ProjectID}}
	}

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
	filter["and"] = andFilters
	return filter
}

// buildSearchOrFilters returns per-term OR filters for issue search.
func buildSearchOrFilters(term string) []map[string]interface{} {
	return []map[string]interface{}{
		{"title": map[string]interface{}{"containsIgnoreCase": term}},
		{"description": map[string]interface{}{"containsIgnoreCase": term}},
		{"identifier": map[string]interface{}{"containsIgnoreCase": term}},
	}
}

// FetchIssues fetches issues with optional filtering and sorting.
func (c *Client) FetchIssues(ctx context.Context, params FetchIssuesParams) ([]Issue, error) {
	first := params.First
	if first <= 0 {
		first = 50
	}

	// Build the filter based on params
	// Build filter
	filter := buildIssueFilter(params)

	// Determine if client-side sorting is needed.
	// Linear API only supports "createdAt" and "updatedAt" for PaginationOrderBy.
	// For "priority" sorting, we fetch by updatedAt and sort client-side.
	sortByPriority := params.OrderBy == "priority"

	orderBy := PaginationOrderBy(params.OrderBy)
	if orderBy == "" || sortByPriority {
		orderBy = OrderByUpdatedAt
	}

	var after *graphql.String
	page := 0
	issues := make([]Issue, 0)
	for {
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
			"after":   after,
		}

		err := c.client.Query(ctx, &query, variables)
		if err != nil {
			logger.ErrorWithErr(err, "API: FetchIssues failed")
			return nil, fmt.Errorf("fetch issues: %w", err)
		}

		for _, node := range query.Issues.Nodes {
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

			archived := node.ArchivedAt != nil

			// Parse labels
			labels := make([]IssueLabel, 0, len(node.Labels.Nodes))
			for _, lbl := range node.Labels.Nodes {
				labels = append(labels, IssueLabel{
					ID:    string(lbl.ID),
					Name:  string(lbl.Name),
					Color: string(lbl.Color),
				})
			}

			// Parse parent
			var parent *IssueRef
			if node.Parent != nil {
				parent = &IssueRef{
					ID:         string(node.Parent.ID),
					Identifier: string(node.Parent.Identifier),
					Title:      string(node.Parent.Title),
				}
			}

			// Parse children
			children := make([]IssueChildRef, 0, len(node.Children.Nodes))
			for _, child := range node.Children.Nodes {
				children = append(children, IssueChildRef{
					ID:         string(child.ID),
					Identifier: string(child.Identifier),
					Title:      string(child.Title),
					State:      string(child.State.Name),
					StateID:    string(child.State.ID),
				})
			}

			issues = append(issues, Issue{
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
				URL:         string(node.URL),
				Archived:    archived,
				Labels:      labels,
				Parent:      parent,
				Children:    children,
			})
		}

		page++
		if params.OnProgress != nil {
			params.OnProgress(IssueFetchProgress{
				Page:    page,
				Fetched: len(issues),
			})
		}

		if !bool(query.Issues.PageInfo.HasNextPage) {
			break
		}

		nextCursor := query.Issues.PageInfo.EndCursor
		after = &nextCursor
	}

	// Sort by priority client-side if requested.
	// Linear priority: 0 = No priority, 1 = Urgent, 2 = High, 3 = Normal, 4 = Low.
	// We sort with Urgent (1) first, then High (2), Normal (3), Low (4), and No priority (0) last.
	if sortByPriority {
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

	return issues, nil
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
		logger.ErrorWithErr(err, "API: FetchIssueByID failed for issue %s", id)
		return Issue{}, fmt.Errorf("fetch issue %s: %w", id, err)
	}

	updatedAt := parseTime(string(query.Issue.UpdatedAt))
	createdAt := parseTime(string(query.Issue.CreatedAt))

	assignee := ""
	assigneeID := ""
	if query.Issue.Assignee != nil {
		assignee = string(query.Issue.Assignee.Name)
		assigneeID = string(query.Issue.Assignee.ID)
	}

	description := ""
	if query.Issue.Description != nil {
		description = string(*query.Issue.Description)
	}

	projectID := ""
	if query.Issue.Project != nil {
		projectID = string(query.Issue.Project.ID)
	}

	archived := query.Issue.ArchivedAt != nil

	// Parse labels
	labels := make([]IssueLabel, 0, len(query.Issue.Labels.Nodes))
	for _, lbl := range query.Issue.Labels.Nodes {
		labels = append(labels, IssueLabel{
			ID:    string(lbl.ID),
			Name:  string(lbl.Name),
			Color: string(lbl.Color),
		})
	}

	// Parse parent
	var parent *IssueRef
	if query.Issue.Parent != nil {
		parent = &IssueRef{
			ID:         string(query.Issue.Parent.ID),
			Identifier: string(query.Issue.Parent.Identifier),
			Title:      string(query.Issue.Parent.Title),
		}
	}

	// Parse children
	children := make([]IssueChildRef, 0, len(query.Issue.Children.Nodes))
	for _, child := range query.Issue.Children.Nodes {
		children = append(children, IssueChildRef{
			ID:         string(child.ID),
			Identifier: string(child.Identifier),
			Title:      string(child.Title),
			State:      string(child.State.Name),
			StateID:    string(child.State.ID),
		})
	}

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

	return Issue{
		ID:          string(query.Issue.ID),
		Identifier:  string(query.Issue.Identifier),
		Title:       string(query.Issue.Title),
		State:       string(query.Issue.State.Name),
		StateID:     string(query.Issue.State.ID),
		Assignee:    assignee,
		AssigneeID:  assigneeID,
		Priority:    int(query.Issue.Priority),
		UpdatedAt:   updatedAt,
		CreatedAt:   createdAt,
		Description: description,
		TeamID:      string(query.Issue.Team.ID),
		ProjectID:   projectID,
		URL:         string(query.Issue.URL),
		Archived:    archived,
		Labels:      labels,
		Parent:      parent,
		Children:    children,
		Comments:    comments,
	}, nil
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
		logger.ErrorWithErr(err, "API: CreateIssue failed")
		return Issue{}, fmt.Errorf("create issue: %w", err)
	}

	if !bool(mutation.IssueCreate.Success) {
		logger.Error("API: CreateIssue operation failed (success=false)")
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
		URL:         string(node.URL),
		Labels:      labels,
	}, nil
}

// UpdateIssue updates an existing issue.
func (c *Client) UpdateIssue(ctx context.Context, input UpdateIssueInput) (Issue, error) {
	var mutation struct {
		IssueUpdate struct {
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
				Labels struct {
					Nodes []struct {
						ID    graphql.String
						Name  graphql.String
						Color graphql.String
					}
				}
				URL graphql.String
			}
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

	variables := map[string]interface{}{
		"id":    graphql.String(input.ID),
		"input": issueInput,
	}

	err := c.client.Mutate(ctx, &mutation, variables)
	if err != nil {
		logger.ErrorWithErr(err, "API: UpdateIssue failed for issue %s", input.ID)
		return Issue{}, fmt.Errorf("update issue %s: %w", input.ID, err)
	}

	if !bool(mutation.IssueUpdate.Success) {
		logger.Error("API: UpdateIssue operation failed (success=false) for issue %s", input.ID)
		return Issue{}, fmt.Errorf("update issue %s: operation failed", input.ID)
	}

	node := mutation.IssueUpdate.Issue
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
		URL:         string(node.URL),
		Labels:      labels,
	}, nil
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
		logger.ErrorWithErr(err, "API: CreateComment failed for issue %s", input.IssueID)
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}

	if !bool(mutation.CommentCreate.Success) {
		logger.Error("API: CreateComment operation failed (success=false) for issue %s", input.IssueID)
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
		logger.ErrorWithErr(err, "API: ArchiveIssue failed for issue %s", issueID)
		return fmt.Errorf("archive issue %s: %w", issueID, err)
	}

	if !bool(mutation.IssueArchive.Success) {
		logger.Error("API: ArchiveIssue operation failed (success=false) for issue %s", issueID)
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
		logger.ErrorWithErr(err, "API: UnarchiveIssue failed for issue %s", issueID)
		return fmt.Errorf("unarchive issue %s: %w", issueID, err)
	}

	if !bool(mutation.IssueUnarchive.Success) {
		logger.Error("API: UnarchiveIssue operation failed (success=false) for issue %s", issueID)
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
		logger.ErrorWithErr(err, "API: ListWorkspaceLabels failed")
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
		logger.ErrorWithErr(err, "API: ListTeamLabels failed for team %s", teamID)
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
