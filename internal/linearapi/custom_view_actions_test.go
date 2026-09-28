package linearapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func customViewRequest(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode GraphQL request: %v", err)
	}
	return request
}

func customViewVariables(request map[string]interface{}) map[string]interface{} {
	variables, _ := request["variables"].(map[string]interface{})
	return variables
}

func customViewJSON(id, name string) string {
	return `{"id":"` + id + `","name":"` + name + `","description":"Desc","color":"#5E6AD2","icon":"eye","filterData":{"state":{"name":{"eq":"Todo"}}},"shared":false,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}`
}

func TestCustomViewActionsSchemaMutations(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = customViewRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"customViewCreate":{"success":true,"customView":` + customViewJSON("view-1", "Open bugs") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		view, err := client.CreateCustomView(context.Background(), CreateCustomViewInput{
			Name:        "Open bugs",
			Description: "Desc",
			Color:       "#5E6AD2",
			FilterJSON:  `{"state":{"name":{"eq":"Todo"}}}`,
		})
		if err != nil {
			t.Fatalf("CreateCustomView() error: %v", err)
		}
		if view.ID != "view-1" || view.Name != "Open bugs" {
			t.Fatalf("view = %#v, want mapped custom view", view)
		}
		if view.FilterData["state"].(map[string]interface{})["name"].(map[string]interface{})["eq"] != "Todo" {
			t.Fatalf("filterData = %#v, want decoded filter object", view.FilterData)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "customViewCreate") || !strings.Contains(query, "CustomViewCreateInput") {
			t.Fatalf("query = %q, want schema-confirmed create", query)
		}
		input := customViewVariables(request)["input"].(map[string]interface{})
		if input["name"] != "Open bugs" || input["description"] != "Desc" || input["color"] != "#5E6AD2" {
			t.Fatalf("input = %#v, want name/description/color", input)
		}
		if _, ok := input["filterData"].(map[string]interface{}); !ok {
			t.Fatalf("filterData = %#v, want object", input["filterData"])
		}
	})

	t.Run("update", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = customViewRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"customViewUpdate":{"success":true,"customView":` + customViewJSON("view-1", "Renamed") + `}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		view, err := client.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{Name: "Renamed", FilterJSON: `{"priority":{"eq":1}}`})
		if err != nil || view.Name != "Renamed" {
			t.Fatalf("UpdateCustomView() = %#v, error %v", view, err)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "customViewUpdate") || !strings.Contains(query, "CustomViewUpdateInput") {
			t.Fatalf("query = %q, want schema-confirmed update", query)
		}
		variables := customViewVariables(request)
		if variables["id"] != "view-1" {
			t.Fatalf("id = %#v, want view-1", variables["id"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		var request map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			request = customViewRequest(t, r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"customViewDelete":{"success":true,"entityId":"view-1"}}}`))
		}))
		defer server.Close()
		client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
		if err := client.DeleteCustomView(context.Background(), "view-1"); err != nil {
			t.Fatalf("DeleteCustomView() error: %v", err)
		}
		query, _ := request["query"].(string)
		if !strings.Contains(query, "customViewDelete") {
			t.Fatalf("query = %q, want delete operation", query)
		}
		if customViewVariables(request)["id"] != "view-1" {
			t.Fatalf("id = %#v, want view-1", customViewVariables(request)["id"])
		}
	})
}

func TestCustomViewListAndValidation(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		request := customViewRequest(t, r)
		w.Header().Set("Content-Type", "application/json")
		if customViewVariables(request)["after"] == nil {
			_, _ = w.Write([]byte(`{"data":{"customViews":{"nodes":[` + customViewJSON("view-1", "One") + `],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"customViews":{"nodes":[` + customViewJSON("view-2", "Two") + `],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	views, err := client.ListCustomViews(context.Background())
	if err != nil || len(views) != 2 || views[1].ID != "view-2" {
		t.Fatalf("ListCustomViews() = %#v, error %v", views, err)
	}
	if !strings.Contains(views[0].FilterJSON, `"state"`) || !strings.Contains(views[0].FilterJSON, `"Todo"`) {
		t.Fatalf("listed filter JSON = %q, want preserved JSONObject", views[0].FilterJSON)
	}
	if calls != 2 {
		t.Fatalf("list calls = %d, want 2 pages", calls)
	}

	bad := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	tests := []struct {
		name string
		call func() error
	}{
		{"create name", func() error {
			_, err := bad.CreateCustomView(context.Background(), CreateCustomViewInput{})
			return err
		}},
		{"create filter", func() error {
			_, err := bad.CreateCustomView(context.Background(), CreateCustomViewInput{Name: "x", FilterJSON: "not-json"})
			return err
		}},
		{"update id", func() error {
			_, err := bad.UpdateCustomView(context.Background(), " ", UpdateCustomViewInput{Name: "x"})
			return err
		}},
		{"update fields", func() error {
			_, err := bad.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{})
			return err
		}},
		{"update filter", func() error {
			_, err := bad.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{FilterJSON: "[]"})
			return err
		}},
		{"delete id", func() error { return bad.DeleteCustomView(context.Background(), " ") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil {
				t.Fatal("call succeeded, want validation error")
			}
		})
	}
}

func TestCustomViewActionsReportGraphQLErrorsAndFalseSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"denied"}]}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	if _, err := client.CreateCustomView(context.Background(), CreateCustomViewInput{Name: "x"}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("GraphQL create error = %v, want denied", err)
	}

	falseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"customViewDelete":{"success":false,"entityId":"view-1"}}}`))
	}))
	defer falseServer.Close()
	client = NewClient(ClientConfig{Token: "test-token", Endpoint: falseServer.URL})
	if err := client.DeleteCustomView(context.Background(), "view-1"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "operation failed") {
		t.Fatalf("false success error = %v, want operation failed", err)
	}
}

func TestCustomViewUpdateExplicitFieldPresence(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, customViewRequest(t, r))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"customViewUpdate":{"success":true,"customView":` + customViewJSON("view-1", "Updated") + `}}}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})

	if _, err := client.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{Name: "Only name"}); err != nil {
		t.Fatalf("name-only update error: %v", err)
	}
	nameOnly := customViewVariables(requests[0])["input"].(map[string]interface{})
	for _, field := range []string{"description", "color", "filterData"} {
		if _, ok := nameOnly[field]; ok {
			t.Fatalf("name-only input unexpectedly included %s: %#v", field, nameOnly)
		}
	}

	empty := ""
	if _, err := client.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{
		Name:             "Set fields",
		DescriptionValue: &empty,
		ColorValue:       &empty,
		FilterJSONValue:  stringPointer(`{}`),
	}); err != nil {
		t.Fatalf("explicit set update error: %v", err)
	}
	setFields := customViewVariables(requests[1])["input"].(map[string]interface{})
	if value, ok := setFields["description"]; !ok || value != "" {
		t.Fatalf("description set value = %#v, want explicit empty string", value)
	}
	if value, ok := setFields["color"]; !ok || value != "" {
		t.Fatalf("color set value = %#v, want explicit empty string", value)
	}
	if value, ok := setFields["filterData"].(map[string]interface{}); !ok || len(value) != 0 {
		t.Fatalf("filterData set value = %#v, want empty object", setFields["filterData"])
	}

	var clearFilter map[string]interface{}
	if _, err := client.UpdateCustomView(context.Background(), "view-1", UpdateCustomViewInput{
		Name:            "Clear fields",
		Description:     CustomViewFieldClear,
		Color:           CustomViewFieldClear,
		FilterDataValue: &clearFilter,
	}); err != nil {
		t.Fatalf("explicit clear update error: %v", err)
	}
	clearFields := customViewVariables(requests[2])["input"].(map[string]interface{})
	for _, field := range []string{"description", "color", "filterData"} {
		value, ok := clearFields[field]
		if !ok || value != nil {
			t.Fatalf("%s clear value = (%#v, %v), want JSON null", field, value, ok)
		}
	}
}

func TestCustomViewUpdateUnchangedMarkersAreOmittedFromGenericFields(t *testing.T) {
	input, err := buildCustomViewUpdateInput(UpdateCustomViewInput{
		Name:        "Renamed",
		Description: CustomViewFieldUnchanged,
		Icon:        CustomViewFieldUnchanged,
		Color:       CustomViewFieldUnchanged,
		TeamID:      CustomViewFieldUnchanged,
		ProjectID:   CustomViewFieldUnchanged,
		OwnerID:     CustomViewFieldUnchanged,
		FilterJSON:  CustomViewFieldUnchanged,
	})
	if err != nil {
		t.Fatalf("buildCustomViewUpdateInput() error: %v", err)
	}
	for _, field := range []string{"description", "icon", "color", "teamId", "projectId", "ownerId", "filterData"} {
		if _, ok := input[field]; ok {
			t.Fatalf("unchanged update included %s: %#v", field, input)
		}
	}
}

func stringPointer(value string) *string { return &value }
