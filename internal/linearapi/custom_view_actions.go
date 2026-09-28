package linearapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/logger"
	"github.com/shurcooL/graphql"
)

// CustomViewCreateInput is the schema-named GraphQL input object used by
// customViewCreate. Keeping it map-backed lets callers pass the exact nested
// IssueFilter shape without pretending the public filter schema is closed.
type CustomViewCreateInput map[string]interface{}

func (CustomViewCreateInput) GetGraphQLType() string { return "CustomViewCreateInput" }

func (input CustomViewCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// CustomViewUpdateInput is the schema-named GraphQL input object used by
// customViewUpdate.
type CustomViewUpdateInput map[string]interface{}

func (CustomViewUpdateInput) GetGraphQLType() string { return "CustomViewUpdateInput" }

func (input CustomViewUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(input))
}

// CustomViewFieldUnchanged and CustomViewFieldClear are wire markers used by
// the presentation layer when it must cross an older string-only callback
// boundary. They are consumed before GraphQL encoding and never reach Linear.
const (
	CustomViewFieldUnchanged = "\x00linearapi:unchanged"
	CustomViewFieldClear     = "\x00linearapi:clear"
)

// CustomView is the stable subset of Linear's CustomView object needed by the
// saved-views manager and issue loading path.
type CustomView struct {
	ID          string
	Name        string
	Description string
	Icon        string
	Color       string
	FilterData  map[string]interface{}
	FilterJSON  string
	Shared      bool
	TeamID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateCustomViewInput contains user-editable fields for a custom view.
// FilterJSON is parsed and validated as a JSON object before it reaches the
// schema's IssueFilter input; invalid text is never silently discarded.
type CreateCustomViewInput struct {
	Name         string
	Description  string
	Icon         string
	Color        string
	TeamID       string
	ProjectID    string
	InitiativeID string
	OwnerID      string
	Shared       *bool

	FilterJSON string
	FilterData map[string]interface{}
	// Filter is a concise alias for FilterData.
	Filter map[string]interface{}
}

// UpdateCustomViewInput contains optional fields accepted by
// CustomViewUpdateInput. At least one field must be supplied.
type UpdateCustomViewInput struct {
	Name        string
	Description string
	// DescriptionValue is an explicit optional value. nil omits the field;
	// nonnil, including a pointer to "", sends the value for clearing.
	DescriptionValue *string
	Icon             string
	Color            string
	// ColorValue follows the same nil/nonnull presence semantics as
	// DescriptionValue.
	ColorValue   *string
	TeamID       string
	ProjectID    string
	InitiativeID string
	OwnerID      string
	Shared       *bool

	FilterJSON string
	FilterData map[string]interface{}
	Filter     map[string]interface{}
	// FilterJSONValue and FilterDataValue make filter presence explicit. A nil
	// pointer means no change; a pointer to an empty JSON object/map sets an
	// empty filter, while a pointer to a nil map or empty/null JSON clears it.
	FilterJSONValue *string
	FilterDataValue *map[string]interface{}
}

// customViewRaw is decoded with encoding/json rather than shurcooL/graphql's
// reflection decoder. Linear exposes filterData as a JSONObject, whose keys
// are intentionally open-ended; the reflection decoder treats every nested key
// as a Go struct field and cannot safely decode it without rejecting the filter.
type customViewRaw struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description *string                `json:"description"`
	Icon        *string                `json:"icon"`
	Color       *string                `json:"color"`
	FilterData  map[string]interface{} `json:"filterData"`
	Shared      bool                   `json:"shared"`
	CreatedAt   string                 `json:"createdAt"`
	UpdatedAt   string                 `json:"updatedAt"`
	Team        *struct {
		ID string `json:"id"`
	} `json:"team"`
}

func (node customViewRaw) customView() CustomView {
	view := CustomView{
		ID:         node.ID,
		Name:       node.Name,
		FilterData: node.FilterData,
		Shared:     node.Shared,
		CreatedAt:  parseTime(node.CreatedAt),
		UpdatedAt:  parseTime(node.UpdatedAt),
	}
	if node.Description != nil {
		view.Description = *node.Description
	}
	if node.Icon != nil {
		view.Icon = *node.Icon
	}
	if node.Color != nil {
		view.Color = *node.Color
	}
	if node.Team != nil {
		view.TeamID = node.Team.ID
	}
	if view.FilterData != nil {
		if encoded, err := json.Marshal(view.FilterData); err == nil {
			view.FilterJSON = string(encoded)
		}
	}
	return view
}

// ListCustomViews fetches all non-archived custom views, following cursors.
func (c *Client) ListCustomViews(ctx context.Context) ([]CustomView, error) {
	var after *string
	views := make([]CustomView, 0)
	for {
		variables := map[string]interface{}{
			"first":           50,
			"after":           after,
			"includeArchived": false,
		}
		var result struct {
			CustomViews struct {
				Nodes    []customViewRaw `json:"nodes"`
				PageInfo struct {
					HasNextPage bool    `json:"hasNextPage"`
					EndCursor   *string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"customViews"`
		}
		if err := c.customViewGraphQL(ctx, customViewsQuery, variables, &result); err != nil {
			logger.ErrorWithErr(err, "linearapi.client: ListCustomViews failed")
			return nil, fmt.Errorf("list custom views: %w", err)
		}
		for _, node := range result.CustomViews.Nodes {
			views = append(views, node.customView())
		}
		if !result.CustomViews.PageInfo.HasNextPage {
			break
		}
		if result.CustomViews.PageInfo.EndCursor == nil || strings.TrimSpace(*result.CustomViews.PageInfo.EndCursor) == "" {
			return nil, fmt.Errorf("list custom views: page has next page but no end cursor")
		}
		next := strings.TrimSpace(*result.CustomViews.PageInfo.EndCursor)
		after = &next
	}
	return views, nil
}

// CreateCustomView creates a custom view using the schema-confirmed input
// fields and returns the created object.
func (c *Client) CreateCustomView(ctx context.Context, input CreateCustomViewInput) (CustomView, error) {
	graphqlInput, err := buildCustomViewCreateInput(input)
	if err != nil {
		return CustomView{}, err
	}
	var result struct {
		CustomViewCreate struct {
			Success    bool          `json:"success"`
			CustomView customViewRaw `json:"customView"`
		} `json:"customViewCreate"`
	}
	if err := c.customViewGraphQL(ctx, customViewCreateMutation, map[string]interface{}{"input": graphqlInput}, &result); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: CreateCustomView failed")
		return CustomView{}, fmt.Errorf("create custom view: %w", err)
	}
	if !result.CustomViewCreate.Success {
		return CustomView{}, fmt.Errorf("create custom view: operation failed")
	}
	return result.CustomViewCreate.CustomView.customView(), nil
}

// UpdateCustomView updates one custom view with the schema-confirmed input.
func (c *Client) UpdateCustomView(ctx context.Context, viewID string, input UpdateCustomViewInput) (CustomView, error) {
	if strings.TrimSpace(viewID) == "" {
		return CustomView{}, fmt.Errorf("update custom view: view ID must not be empty")
	}
	graphqlInput, err := buildCustomViewUpdateInput(input)
	if err != nil {
		return CustomView{}, err
	}
	var result struct {
		CustomViewUpdate struct {
			Success    bool          `json:"success"`
			CustomView customViewRaw `json:"customView"`
		} `json:"customViewUpdate"`
	}
	variables := map[string]interface{}{"id": strings.TrimSpace(viewID), "input": graphqlInput}
	if err := c.customViewGraphQL(ctx, customViewUpdateMutation, variables, &result); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateCustomView failed view_id=%s", viewID)
		return CustomView{}, fmt.Errorf("update custom view %s: %w", viewID, err)
	}
	if !result.CustomViewUpdate.Success {
		return CustomView{}, fmt.Errorf("update custom view %s: operation failed", viewID)
	}
	return result.CustomViewUpdate.CustomView.customView(), nil
}

// DeleteCustomView permanently removes one custom view through the public
// customViewDelete mutation.
func (c *Client) DeleteCustomView(ctx context.Context, viewID string) error {
	if strings.TrimSpace(viewID) == "" {
		return fmt.Errorf("delete custom view: view ID must not be empty")
	}
	var result struct {
		CustomViewDelete struct {
			Success bool `json:"success"`
		} `json:"customViewDelete"`
	}
	if err := c.customViewGraphQL(ctx, customViewDeleteMutation, map[string]interface{}{"id": strings.TrimSpace(viewID)}, &result); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: DeleteCustomView failed view_id=%s", viewID)
		return fmt.Errorf("delete custom view %s: %w", viewID, err)
	}
	if !result.CustomViewDelete.Success {
		return fmt.Errorf("delete custom view %s: operation failed", viewID)
	}
	return nil
}

const customViewSelection = `id name description icon color filterData shared createdAt updatedAt team { id }`

const customViewsQuery = `query ListCustomViews($first: Int, $after: String, $includeArchived: Boolean) {
  customViews(first: $first, after: $after, includeArchived: $includeArchived) {
    nodes { ` + customViewSelection + ` }
    pageInfo { hasNextPage endCursor }
  }
}`

const customViewCreateMutation = `mutation CreateCustomView($input: CustomViewCreateInput!) {
  customViewCreate(input: $input) {
    success
    customView { ` + customViewSelection + ` }
  }
}`

const customViewUpdateMutation = `mutation UpdateCustomView($id: String!, $input: CustomViewUpdateInput!) {
  customViewUpdate(id: $id, input: $input) {
    success
    customView { ` + customViewSelection + ` }
  }
}`

const customViewDeleteMutation = `mutation DeleteCustomView($id: String!) {
  customViewDelete(id: $id) { success entityId }
}`

type customViewGraphQLError struct {
	Message string `json:"message"`
}

type customViewGraphQLResponse struct {
	Data   json.RawMessage          `json:"data"`
	Errors []customViewGraphQLError `json:"errors"`
}

// customViewGraphQL performs the small subset of GraphQL transport needed by
// custom views. It intentionally uses encoding/json for response data so the
// open-ended JSONObject filterData survives round trips unchanged.
func (c *Client) customViewGraphQL(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	if c == nil || c.httpClient == nil || strings.TrimSpace(c.endpoint) == "" {
		return fmt.Errorf("client is not configured")
	}
	payload := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables,omitempty"`
	}{Query: query, Variables: variables}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode GraphQL request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send GraphQL request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read GraphQL response: %w", err)
	}
	var envelope customViewGraphQLResponse
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("GraphQL request failed with HTTP %s", resp.Status)
		}
		return fmt.Errorf("decode GraphQL response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, graphQLError := range envelope.Errors {
			if strings.TrimSpace(graphQLError.Message) != "" {
				messages = append(messages, graphQLError.Message)
			}
		}
		if len(messages) == 0 {
			return fmt.Errorf("GraphQL request failed")
		}
		return fmt.Errorf("GraphQL request failed: %s", strings.Join(messages, "; "))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("GraphQL request failed with HTTP %s", resp.Status)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("GraphQL response contained no data")
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return fmt.Errorf("decode GraphQL data: %w", err)
	}
	return nil
}

func buildCustomViewCreateInput(input CreateCustomViewInput) (CustomViewCreateInput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("create custom view: name must not be empty")
	}
	filter, err := customViewFilter(input.FilterJSON, input.FilterData, input.Filter)
	if err != nil {
		return nil, fmt.Errorf("create custom view: %w", err)
	}
	result := CustomViewCreateInput{"name": graphql.String(name)}
	addCommonCustomViewFields(result, input.Description, input.Icon, input.Color, input.TeamID, input.ProjectID, input.InitiativeID, input.OwnerID, input.Shared, filter)
	return result, nil
}

func buildCustomViewUpdateInput(input UpdateCustomViewInput) (CustomViewUpdateInput, error) {
	filter, filterPresent, filterClear, err := customViewUpdateFilter(input)
	if err != nil {
		return nil, fmt.Errorf("update custom view: %w", err)
	}
	result := CustomViewUpdateInput{}
	if strings.TrimSpace(input.Name) != "" {
		result["name"] = graphql.String(strings.TrimSpace(input.Name))
	}
	addCommonCustomViewFields(result, input.Description, input.Icon, input.Color, input.TeamID, input.ProjectID, input.InitiativeID, input.OwnerID, input.Shared, nil)
	if err := addCustomViewUpdateFields(result, input, filter, filterPresent, filterClear); err != nil {
		return nil, fmt.Errorf("update custom view: %w", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("update custom view: at least one field is required")
	}
	return result, nil
}

func customViewUpdateFilter(input UpdateCustomViewInput) (map[string]interface{}, bool, bool, error) {
	provided := 0
	var filter map[string]interface{}
	filterClear := false
	if input.FilterJSON != "" && input.FilterJSON != CustomViewFieldUnchanged {
		provided++
		if input.FilterJSON == CustomViewFieldClear {
			filterClear = true
		} else {
			parsed, err := customViewFilter(input.FilterJSON, nil, nil)
			if err != nil {
				return nil, false, false, err
			}
			filter = parsed
		}
	}
	if input.FilterData != nil {
		provided++
		filter = cloneJSONMap(input.FilterData)
		filterClear = false
	}
	if input.Filter != nil {
		provided++
		filter = cloneJSONMap(input.Filter)
		filterClear = false
	}
	if input.FilterJSONValue != nil {
		provided++
		value := strings.TrimSpace(*input.FilterJSONValue)
		switch value {
		case "", "null", CustomViewFieldClear:
			filterClear = true
			filter = nil
		case CustomViewFieldUnchanged:
			provided--
		default:
			parsed, err := customViewFilter(value, nil, nil)
			if err != nil {
				return nil, false, false, err
			}
			filter = parsed
			filterClear = false
		}
	}
	if input.FilterDataValue != nil {
		provided++
		if *input.FilterDataValue == nil {
			filter = nil
			filterClear = true
		} else {
			filter = cloneJSONMap(*input.FilterDataValue)
			filterClear = false
		}
	}
	if provided > 1 {
		return nil, false, false, fmt.Errorf("filter must be supplied once")
	}
	return filter, provided == 1, filterClear, nil
}

func addCustomViewUpdateFields(result map[string]interface{}, input UpdateCustomViewInput, filter map[string]interface{}, filterPresent, filterClear bool) error {
	if input.DescriptionValue != nil && input.Description != "" && input.Description != CustomViewFieldUnchanged {
		return fmt.Errorf("description supplied more than once")
	}
	if input.ColorValue != nil && input.Color != "" && input.Color != CustomViewFieldUnchanged {
		return fmt.Errorf("color supplied more than once")
	}
	addOptionalCustomViewString(result, "description", input.Description, input.DescriptionValue)
	addOptionalCustomViewString(result, "color", input.Color, input.ColorValue)
	if filterPresent {
		if filterClear {
			result["filterData"] = nil
		} else {
			result["filterData"] = IssueFilter(filter)
		}
	}
	return nil
}

func addOptionalCustomViewString(result map[string]interface{}, field, legacy string, explicit *string) {
	if explicit != nil {
		result[field] = graphql.String(*explicit)
		return
	}
	switch legacy {
	case "", CustomViewFieldUnchanged:
		return
	case CustomViewFieldClear:
		result[field] = nil
	default:
		result[field] = graphql.String(legacy)
	}
}

func addCommonCustomViewFields(result map[string]interface{}, description, icon, color, teamID, projectID, initiativeID, ownerID string, shared *bool, filter map[string]interface{}) {
	// Use the same marker-aware string helper for every shared field. Update
	// callers cross an older string-only callback boundary, where an unchanged
	// value is represented by CustomViewFieldUnchanged. Serializing that marker
	// as a literal value would overwrite the existing field in Linear before
	// the update-specific handling gets a chance to omit it.
	addOptionalCustomViewString(result, "description", description, nil)
	addOptionalCustomViewString(result, "icon", icon, nil)
	addOptionalCustomViewString(result, "color", color, nil)
	addOptionalCustomViewString(result, "teamId", teamID, nil)
	addOptionalCustomViewString(result, "projectId", projectID, nil)
	addOptionalCustomViewString(result, "initiativeId", initiativeID, nil)
	addOptionalCustomViewString(result, "ownerId", ownerID, nil)
	if shared != nil {
		result["shared"] = graphql.Boolean(*shared)
	}
	if filter != nil {
		result["filterData"] = IssueFilter(filter)
	}
}

func customViewFilter(filterJSON string, filterData, filter map[string]interface{}) (map[string]interface{}, error) {
	provided := 0
	if strings.TrimSpace(filterJSON) != "" {
		provided++
	}
	if filterData != nil {
		provided++
	}
	if filter != nil {
		provided++
	}
	if provided > 1 {
		return nil, fmt.Errorf("filter must be supplied once")
	}
	if strings.TrimSpace(filterJSON) != "" {
		var parsed interface{}
		if err := json.Unmarshal([]byte(filterJSON), &parsed); err != nil {
			return nil, fmt.Errorf("filter JSON is invalid: %w", err)
		}
		object, ok := parsed.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("filter JSON must be an object")
		}
		return object, nil
	}
	if filterData != nil {
		return cloneJSONMap(filterData), nil
	}
	if filter != nil {
		return cloneJSONMap(filter), nil
	}
	return nil, nil
}

func cloneJSONMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
