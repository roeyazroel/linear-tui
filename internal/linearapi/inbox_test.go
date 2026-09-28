package linearapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestListNotificationsPaginatesAndMapsIssue(t *testing.T) {
	var requests []map[string]interface{}
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		if page == 0 {
			_, _ = w.Write([]byte(`{"data":{"notifications":{"nodes":[{"id":"notification-1","type":"issueAssigned","title":"Review issue","subtitle":"A review is waiting","createdAt":"2026-07-01T10:00:00Z","updatedAt":"2026-07-02T10:00:00Z","archivedAt":null,"readAt":null,"snoozedUntilAt":"2026-07-03T10:00:00Z","unsnoozedAt":null,"category":"issue","url":"https://linear.app/acme/issue/ACM-1","inboxUrl":"https://linear.app/acme/inbox/notification-1","issue":{"id":"issue-1","identifier":"ACM-1","title":"Review issue"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":{"notifications":{"nodes":[{"id":"notification-2","type":"projectUpdate","title":"Project update","subtitle":"A project changed","createdAt":"2026-07-04T10:00:00Z","updatedAt":"2026-07-04T11:00:00Z","archivedAt":"2026-07-05T10:00:00Z","readAt":"2026-07-04T12:00:00Z","snoozedUntilAt":null,"unsnoozedAt":"2026-07-04T12:30:00Z","category":"project","url":"https://linear.app/acme/project/p-1","inboxUrl":"https://linear.app/acme/inbox/notification-2"}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
		}
		page++
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	notifications, err := client.ListNotifications(context.Background(), NotificationListOptions{
		First:           1,
		IncludeArchived: true,
		OrderBy:         OrderByUpdatedAt,
		Filter: NotificationFilter{
			"type": map[string]interface{}{"eq": "issueAssigned"},
		},
	})
	if err != nil {
		t.Fatalf("ListNotifications() error: %v", err)
	}
	if len(notifications) != 2 {
		t.Fatalf("notifications length = %d, want 2", len(notifications))
	}

	first := notifications[0]
	if first.ID != "notification-1" || first.Type != "issueAssigned" || first.Title != "Review issue" {
		t.Fatalf("first notification identity = %+v", first)
	}
	if first.CreatedAt != mustParseInboxTime(t, "2026-07-01T10:00:00Z") || first.UpdatedAt != mustParseInboxTime(t, "2026-07-02T10:00:00Z") {
		t.Fatalf("first notification times = %+v", first)
	}
	if first.Read || first.Archived || !first.Snoozed || first.ReadAt != nil || first.ArchivedAt != nil || first.SnoozedUntilAt == nil {
		t.Fatalf("first notification state = %+v", first)
	}
	if first.Issue == nil || first.Issue.ID != "issue-1" || first.Issue.Identifier != "ACM-1" || first.Issue.Title != "Review issue" {
		t.Fatalf("first notification issue = %+v", first.Issue)
	}

	second := notifications[1]
	if !second.Read || !second.Archived || second.Snoozed || second.ReadAt == nil || second.ArchivedAt == nil || second.SnoozedUntilAt != nil {
		t.Fatalf("second notification state = %+v", second)
	}
	if second.Issue != nil {
		t.Fatalf("second notification issue = %+v, want nil", second.Issue)
	}

	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	query, _ := requests[0]["query"].(string)
	if !strings.Contains(query, "notifications") || !strings.Contains(query, "... on IssueNotification") {
		t.Fatalf("query = %q, want notifications and IssueNotification fragment", query)
	}
	variables := requests[0]["variables"].(map[string]interface{})
	if variables["first"] != float64(1) || variables["includeArchived"] != true || variables["orderBy"] != "updatedAt" {
		t.Fatalf("first variables = %#v", variables)
	}
	if variables["after"] != nil {
		t.Fatalf("first after = %#v, want nil", variables["after"])
	}
	filter := variables["filter"].(map[string]interface{})
	if !reflect.DeepEqual(filter["type"], map[string]interface{}{"eq": "issueAssigned"}) {
		t.Fatalf("filter = %#v", filter)
	}
	secondVariables := requests[1]["variables"].(map[string]interface{})
	if secondVariables["after"] != "cursor-1" {
		t.Fatalf("second after = %#v, want cursor-1", secondVariables["after"])
	}
}

func TestListNotificationsDefaultsAndValidationAvoidNetwork(t *testing.T) {
	calls := 0
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"notifications":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})

	if _, err := client.ListNotifications(context.Background(), NotificationListOptions{}); err != nil {
		t.Fatalf("ListNotifications() defaults error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls after default list = %d, want 1", calls)
	}
	if len(requests) != 1 {
		t.Fatalf("request count after default list = %d, want 1", len(requests))
	}
	variables, ok := requests[0]["variables"].(map[string]interface{})
	if !ok {
		t.Fatalf("default variables = %#v, want object", requests[0]["variables"])
	}
	if orderBy, ok := variables["orderBy"].(string); !ok || orderBy != string(OrderByCreatedAt) {
		t.Fatalf("default orderBy = %#v, want non-null %q", variables["orderBy"], OrderByCreatedAt)
	}
	for _, options := range []NotificationListOptions{
		{First: -1},
		{OrderBy: PaginationOrderBy("unsupported")},
	} {
		if _, err := client.ListNotifications(context.Background(), options); err == nil {
			t.Fatalf("ListNotifications(%+v) error = nil, want validation error", options)
		}
	}
	if calls != 1 {
		t.Fatalf("calls after invalid options = %d, want 1", calls)
	}
}

func TestListNotificationsReportsAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"forbidden"}]}`))
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	if _, err := client.ListNotifications(context.Background(), NotificationListOptions{}); err == nil {
		t.Fatal("ListNotifications() error = nil, want API error")
	}
}

func TestNotificationActionsUseSchemaMutations(t *testing.T) {
	var requests []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		query, _ := request["query"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(query, "notificationArchive"):
			_, _ = w.Write([]byte(`{"data":{"notificationArchive":{"success":true}}}`))
		case strings.Contains(query, "notificationUnarchive"):
			_, _ = w.Write([]byte(`{"data":{"notificationUnarchive":{"success":true}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"notificationUpdate":{"success":true,"notification":{"id":"notification-1","type":"issueAssigned","title":"Updated notification","subtitle":"Subtitle","createdAt":"2026-07-01T10:00:00Z","updatedAt":"2026-07-02T10:00:00Z","archivedAt":null,"readAt":"2026-07-02T12:00:00Z","snoozedUntilAt":null,"unsnoozedAt":null,"category":"issue","url":"url","inboxUrl":"inbox"}}}}`))
		}
	}))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})

	readAt := mustParseInboxTime(t, "2026-08-01T09:10:11Z")
	notification, err := client.MarkNotificationRead(context.Background(), "notification-1", readAt)
	if err != nil {
		t.Fatalf("MarkNotificationRead() error: %v", err)
	}
	if notification.ID != "notification-1" || !notification.Read {
		t.Fatalf("marked notification = %+v", notification)
	}
	if _, err := client.MarkNotificationUnread(context.Background(), "notification-1"); err != nil {
		t.Fatalf("MarkNotificationUnread() error: %v", err)
	}
	snoozedUntil := mustParseInboxTime(t, "2026-08-02T09:10:11Z")
	if _, err := client.SnoozeNotification(context.Background(), "notification-1", snoozedUntil); err != nil {
		t.Fatalf("SnoozeNotification() error: %v", err)
	}
	if _, err := client.UnsnoozeNotification(context.Background(), "notification-1"); err != nil {
		t.Fatalf("UnsnoozeNotification() error: %v", err)
	}
	if err := client.ArchiveNotification(context.Background(), "notification-1"); err != nil {
		t.Fatalf("ArchiveNotification() error: %v", err)
	}
	if err := client.UnarchiveNotification(context.Background(), "notification-1"); err != nil {
		t.Fatalf("UnarchiveNotification() error: %v", err)
	}

	if len(requests) != 6 {
		t.Fatalf("request count = %d, want 6", len(requests))
	}
	for i, request := range requests {
		query, _ := request["query"].(string)
		if i < 4 && !strings.Contains(query, "notificationUpdate") {
			t.Errorf("request %d query = %q, want notificationUpdate", i, query)
		}
		if i == 4 && !strings.Contains(query, "notificationArchive") {
			t.Errorf("archive query = %q", query)
		}
		if i == 5 && !strings.Contains(query, "notificationUnarchive") {
			t.Errorf("unarchive query = %q", query)
		}
	}
	readInput := requests[0]["variables"].(map[string]interface{})["input"].(map[string]interface{})
	if readInput["readAt"] != "2026-08-01T09:10:11Z" {
		t.Fatalf("readAt input = %#v", readInput["readAt"])
	}
	unreadInput := requests[1]["variables"].(map[string]interface{})["input"].(map[string]interface{})
	if unreadInput["readAt"] != nil {
		t.Fatalf("unread readAt = %#v, want null", unreadInput["readAt"])
	}
	snoozeInput := requests[2]["variables"].(map[string]interface{})["input"].(map[string]interface{})
	if snoozeInput["snoozedUntilAt"] != "2026-08-02T09:10:11Z" {
		t.Fatalf("snoozedUntilAt input = %#v", snoozeInput["snoozedUntilAt"])
	}
	unsnoozeInput := requests[3]["variables"].(map[string]interface{})["input"].(map[string]interface{})
	if unsnoozeInput["snoozedUntilAt"] != nil {
		t.Fatalf("unsnooze snoozedUntilAt = %#v, want null", unsnoozeInput["snoozedUntilAt"])
	}
}

func TestNotificationActionValidationAvoidsNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer server.Close()
	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	zero := time.Time{}
	tests := []struct {
		name string
		call func() error
	}{
		{"read empty id", func() error { _, err := client.MarkNotificationRead(context.Background(), " ", time.Now()); return err }},
		{"read zero timestamp", func() error {
			_, err := client.MarkNotificationRead(context.Background(), "notification-1", zero)
			return err
		}},
		{"unread empty id", func() error { _, err := client.MarkNotificationUnread(context.Background(), ""); return err }},
		{"snooze empty id", func() error { _, err := client.SnoozeNotification(context.Background(), "", time.Now()); return err }},
		{"snooze zero timestamp", func() error {
			_, err := client.SnoozeNotification(context.Background(), "notification-1", zero)
			return err
		}},
		{"unsnooze empty id", func() error { _, err := client.UnsnoozeNotification(context.Background(), " "); return err }},
		{"archive empty id", func() error { return client.ArchiveNotification(context.Background(), "") }},
		{"unarchive empty id", func() error { return client.UnarchiveNotification(context.Background(), " ") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("error = nil, want validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestNotificationActionsReportAPIAndOperationErrors(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		call     func(*Client) error
	}{
		{
			name:     "graphql error",
			response: `{"errors":[{"message":"forbidden"}]}`,
			call: func(client *Client) error {
				_, err := client.MarkNotificationUnread(context.Background(), "notification-1")
				return err
			},
		},
		{
			name:     "update success false",
			response: `{"data":{"notificationUpdate":{"success":false,"notification":{"id":"notification-1"}}}}`,
			call: func(client *Client) error {
				_, err := client.MarkNotificationUnread(context.Background(), "notification-1")
				return err
			},
		},
		{
			name:     "archive success false",
			response: `{"data":{"notificationArchive":{"success":false}}}`,
			call: func(client *Client) error {
				return client.ArchiveNotification(context.Background(), "notification-1")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
			if err := test.call(client); err == nil {
				t.Fatal("error = nil, want operation/API error")
			}
		})
	}
}

func mustParseInboxTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse test time %q: %v", value, err)
	}
	return parsed
}
