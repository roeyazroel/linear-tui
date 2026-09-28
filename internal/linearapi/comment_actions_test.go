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

func TestUpdateComment(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"commentUpdate":{"success":true,"comment":{"id":"comment-1","body":"updated body","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z","issueId":"issue-1","user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	comment, err := client.UpdateComment(context.Background(), "comment-1", "updated body")
	if err != nil {
		t.Fatalf("UpdateComment() error: %v", err)
	}

	want := Comment{
		ID:        "comment-1",
		Body:      "updated body",
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		Author: User{
			ID:          "user-1",
			Name:        "Ada",
			DisplayName: "Ada Lovelace",
			Email:       "ada@example.com",
			IsMe:        true,
		},
		IssueID: "issue-1",
	}
	if !reflect.DeepEqual(comment, want) {
		t.Fatalf("comment = %#v, want %#v", comment, want)
	}

	query, _ := request["query"].(string)
	if !strings.Contains(query, "commentUpdate") {
		t.Fatalf("query = %q, want commentUpdate mutation", query)
	}
	variables, _ := request["variables"].(map[string]interface{})
	if variables["id"] != "comment-1" {
		t.Fatalf("id variable = %#v, want comment-1", variables["id"])
	}
	input, _ := variables["input"].(map[string]interface{})
	if input["body"] != "updated body" {
		t.Fatalf("input = %#v, want body", input)
	}
}

func TestUpdateCommentPreservesParentAndReactions(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"commentUpdate":{"success":true,"comment":{"id":"comment-2","body":"reply","createdAt":"2025-01-01T00:02:00Z","updatedAt":"2025-01-02T00:02:00Z","issueId":"issue-1","parent":{"id":"comment-1"},"user":{"id":"user-2","name":"Grace","displayName":"Grace Hopper","email":"grace@example.com","isMe":false},"reactions":[{"id":"reaction-2","emoji":"✅","createdAt":"2025-01-01T00:03:00Z","updatedAt":"2025-01-01T00:03:00Z","comment":{"id":"comment-2","issue":{"id":"issue-1"}},"user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}]}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	comment, err := client.UpdateComment(context.Background(), "comment-2", "reply")
	if err != nil {
		t.Fatalf("UpdateComment() error: %v", err)
	}
	if comment.ParentID != "comment-1" || comment.Parent == nil || comment.Parent.ID != "comment-1" {
		t.Fatalf("parent = %#v, want comment-1", comment.Parent)
	}
	if len(comment.Reactions) != 1 || comment.Reactions[0].ID != "reaction-2" || comment.Reactions[0].CommentID != "comment-2" || comment.Reactions[0].User.ID != "user-1" {
		t.Fatalf("reactions = %#v, want reaction-2/comment-2/user-1", comment.Reactions)
	}
	query, _ := request["query"].(string)
	for _, field := range []string{"parent", "reactions"} {
		if !strings.Contains(query, field) {
			t.Fatalf("query = %q, want %q", query, field)
		}
	}
}

func TestDeleteComment(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"commentDelete":{"success":true}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	if err := client.DeleteComment(context.Background(), "comment-1"); err != nil {
		t.Fatalf("DeleteComment() error: %v", err)
	}
	query, _ := request["query"].(string)
	if !strings.Contains(query, "commentDelete") {
		t.Fatalf("query = %q, want commentDelete mutation", query)
	}
	variables, _ := request["variables"].(map[string]interface{})
	if variables["id"] != "comment-1" {
		t.Fatalf("id variable = %#v, want comment-1", variables["id"])
	}
}

func TestAddCommentReaction(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"reactionCreate":{"success":true,"reaction":{"id":"reaction-1","emoji":"👍","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z","comment":{"id":"comment-1"},"user":{"id":"user-1","name":"Ada","displayName":"Ada Lovelace","email":"ada@example.com","isMe":true}}}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	reaction, err := client.AddCommentReaction(context.Background(), "comment-1", "👍")
	if err != nil {
		t.Fatalf("AddCommentReaction() error: %v", err)
	}
	if reaction.ID != "reaction-1" || reaction.Emoji != "👍" || reaction.CommentID != "comment-1" {
		t.Fatalf("reaction = %#v, want reaction-1 👍 comment-1", reaction)
	}
	if reaction.User.ID != "user-1" || !reaction.User.IsMe {
		t.Fatalf("reaction.User = %#v, want user-1 current user", reaction.User)
	}
	query, _ := request["query"].(string)
	if !strings.Contains(query, "reactionCreate") {
		t.Fatalf("query = %q, want reactionCreate mutation", query)
	}
	variables, _ := request["variables"].(map[string]interface{})
	input, _ := variables["input"].(map[string]interface{})
	if input["commentId"] != "comment-1" || input["emoji"] != "👍" {
		t.Fatalf("input = %#v, want commentId and emoji", input)
	}
}

func TestRemoveCommentReaction(t *testing.T) {
	var request map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"reactionDelete":{"success":true}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	if err := client.RemoveCommentReaction(context.Background(), "reaction-1"); err != nil {
		t.Fatalf("RemoveCommentReaction() error: %v", err)
	}
	query, _ := request["query"].(string)
	if !strings.Contains(query, "reactionDelete") {
		t.Fatalf("query = %q, want reactionDelete mutation", query)
	}
	variables, _ := request["variables"].(map[string]interface{})
	if variables["id"] != "reaction-1" {
		t.Fatalf("id variable = %#v, want reaction-1", variables["id"])
	}
}

func TestCommentActionValidationDoesNotReachNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "update empty comment id",
			call: func() error {
				_, err := client.UpdateComment(context.Background(), " ", "body")
				return err
			},
		},
		{
			name: "update empty body",
			call: func() error {
				_, err := client.UpdateComment(context.Background(), "comment-1", " ")
				return err
			},
		},
		{
			name: "delete empty comment id",
			call: func() error {
				return client.DeleteComment(context.Background(), "")
			},
		},
		{
			name: "add empty comment id",
			call: func() error {
				_, err := client.AddCommentReaction(context.Background(), "", "👍")
				return err
			},
		},
		{
			name: "add empty emoji",
			call: func() error {
				_, err := client.AddCommentReaction(context.Background(), "comment-1", " ")
				return err
			},
		},
		{
			name: "remove empty reaction id",
			call: func() error {
				return client.RemoveCommentReaction(context.Background(), "")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0 for validation errors", calls)
	}
}

func TestCommentActionOperationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"commentDelete":{"success":false}}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{Token: "test-token", Endpoint: server.URL})
	err := client.DeleteComment(context.Background(), "comment-1")
	if err == nil || !strings.Contains(err.Error(), "operation failed") {
		t.Fatalf("DeleteComment() error = %v, want operation failed", err)
	}
}
