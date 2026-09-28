package tui

import (
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestBuildCommentThreadsSortsNestsOrphansAndPreservesReactions(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	comments := []linearapi.Comment{
		{
			ID:        "comment-3",
			Body:      "grandchild",
			CreatedAt: base.Add(3 * time.Minute),
			ParentID:  "comment-2",
			Parent:    &linearapi.CommentRef{ID: "comment-2"},
			Reactions: []linearapi.Reaction{{ID: "reaction-3", Emoji: "🎉"}},
		},
		{
			ID:        "orphan",
			Body:      "orphan",
			CreatedAt: base.Add(4 * time.Minute),
			ParentID:  "missing",
		},
		{
			ID:        "comment-1",
			Body:      "root",
			CreatedAt: base,
			Reactions: []linearapi.Reaction{{ID: "reaction-1", Emoji: "👍"}},
		},
		{
			ID:        "comment-2",
			Body:      "reply",
			CreatedAt: base.Add(2 * time.Minute),
			ParentID:  "comment-1",
			Parent:    &linearapi.CommentRef{ID: "comment-1"},
		},
	}

	threads := BuildCommentThreads(comments)
	if len(threads) != 2 {
		t.Fatalf("root threads = %d, want 2", len(threads))
	}
	if threads[0].Comment.ID != "comment-1" || threads[1].Comment.ID != "orphan" {
		t.Fatalf("root order = [%s %s], want [comment-1 orphan]", threads[0].Comment.ID, threads[1].Comment.ID)
	}
	if len(threads[0].Replies) != 1 || threads[0].Replies[0].Comment.ID != "comment-2" {
		t.Fatalf("replies = %#v, want comment-2", threads[0].Replies)
	}
	if len(threads[0].Replies[0].Replies) != 1 || threads[0].Replies[0].Replies[0].Comment.ID != "comment-3" {
		t.Fatalf("nested replies = %#v, want comment-3", threads[0].Replies[0].Replies)
	}
	if len(threads[0].Comment.Reactions) != 1 || threads[0].Comment.Reactions[0].ID != "reaction-1" || threads[0].Replies[0].Replies[0].Comment.Reactions[0].ID != "reaction-3" {
		t.Fatalf("reaction IDs were not preserved: %#v", threads[0])
	}
}

func TestBuildCommentThreadsBreaksCyclesWithoutDuplicates(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	comments := []linearapi.Comment{
		{ID: "comment-2", CreatedAt: base.Add(time.Minute), ParentID: "comment-1"},
		{ID: "comment-1", CreatedAt: base, ParentID: "comment-2"},
		{ID: "self", CreatedAt: base.Add(2 * time.Minute), ParentID: "self"},
	}
	threads := BuildCommentThreads(comments)
	if len(threads) != 2 {
		t.Fatalf("root threads = %d, want 2 after breaking cycles", len(threads))
	}
	if threads[0].Comment.ID != "comment-1" || threads[1].Comment.ID != "self" {
		t.Fatalf("root order = [%s %s], want [comment-1 self]", threads[0].Comment.ID, threads[1].Comment.ID)
	}
	if len(threads[0].Replies) != 1 || threads[0].Replies[0].Comment.ID != "comment-2" {
		t.Fatalf("cycle resolution = %#v, want comment-1 -> comment-2", threads[0])
	}

	seen := make(map[string]int)
	var visit func([]CommentThread)
	visit = func(nodes []CommentThread) {
		for _, node := range nodes {
			seen[node.Comment.ID]++
			visit(node.Replies)
		}
	}
	visit(threads)
	if len(seen) != len(comments) {
		t.Fatalf("seen IDs = %#v, want all comments", seen)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("comment %s appears %d times, want once", id, count)
		}
	}
}
