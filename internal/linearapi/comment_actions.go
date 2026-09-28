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

// CommentUpdateInput is a custom scalar type for Linear's CommentUpdateInput.
// The Go type name must match the GraphQL input type name exactly.
type CommentUpdateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (CommentUpdateInput) GetGraphQLType() string {
	return "CommentUpdateInput"
}

// MarshalJSON implements json.Marshaler for CommentUpdateInput.
func (i CommentUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// ReactionCreateInput is a custom scalar type for Linear's ReactionCreateInput.
// The Go type name must match the GraphQL input type name exactly.
type ReactionCreateInput map[string]interface{}

// GetGraphQLType returns the GraphQL type name for the input.
func (ReactionCreateInput) GetGraphQLType() string {
	return "ReactionCreateInput"
}

// MarshalJSON implements json.Marshaler for ReactionCreateInput.
func (i ReactionCreateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// Reaction represents an emoji reaction returned by Linear.
type Reaction struct {
	ID        string
	Emoji     string
	CreatedAt time.Time
	UpdatedAt time.Time
	CommentID string
	IssueID   string
	User      User
	// Author mirrors User for callers that use comment-oriented terminology.
	Author User
}

// CommentReaction is an alias for Reaction kept for callers that prefer an
// explicit comment-oriented name.
type CommentReaction = Reaction

// commentActionNode is the subset of a Linear Comment needed by comment
// mutation methods. It intentionally maps to the existing Comment model
// rather than introducing a second comment representation.
type commentActionNode struct {
	ID        graphql.String
	Body      graphql.String
	CreatedAt graphql.String
	UpdatedAt graphql.String
	IssueID   graphql.String
	Issue     *struct {
		ID graphql.String
	}
	Parent *struct {
		ID graphql.String
	}
	User struct {
		ID          graphql.String
		Name        graphql.String
		DisplayName graphql.String
		Email       graphql.String
		IsMe        graphql.Boolean
	}
	Reactions []reactionActionNode
}

func (n commentActionNode) comment() Comment {
	issueID := string(n.IssueID)
	if issueID == "" && n.Issue != nil {
		issueID = string(n.Issue.ID)
	}

	author := User{
		ID:          string(n.User.ID),
		Name:        string(n.User.Name),
		DisplayName: string(n.User.DisplayName),
		Email:       string(n.User.Email),
		IsMe:        bool(n.User.IsMe),
	}
	comment := Comment{
		ID:        string(n.ID),
		Body:      string(n.Body),
		CreatedAt: parseTime(string(n.CreatedAt)),
		UpdatedAt: parseTime(string(n.UpdatedAt)),
		Author:    author,
		IssueID:   issueID,
	}
	if n.Parent != nil {
		comment.ParentID = string(n.Parent.ID)
		comment.Parent = &CommentRef{ID: comment.ParentID}
	}
	if len(n.Reactions) > 0 {
		comment.Reactions = make([]Reaction, 0, len(n.Reactions))
		for _, reaction := range n.Reactions {
			comment.Reactions = append(comment.Reactions, reaction.reaction())
		}
	}
	return comment
}

type reactionActionNode struct {
	ID        graphql.String
	Emoji     graphql.String
	CreatedAt graphql.String
	UpdatedAt graphql.String
	Comment   *struct {
		ID      graphql.String
		IssueID graphql.String
		Issue   *struct {
			ID graphql.String
		}
	}
	User struct {
		ID          graphql.String
		Name        graphql.String
		DisplayName graphql.String
		Email       graphql.String
		IsMe        graphql.Boolean
	}
}

func (n reactionActionNode) reaction() Reaction {
	author := User{
		ID:          string(n.User.ID),
		Name:        string(n.User.Name),
		DisplayName: string(n.User.DisplayName),
		Email:       string(n.User.Email),
		IsMe:        bool(n.User.IsMe),
	}
	reaction := Reaction{
		ID:        string(n.ID),
		Emoji:     string(n.Emoji),
		CreatedAt: parseTime(string(n.CreatedAt)),
		UpdatedAt: parseTime(string(n.UpdatedAt)),
		User:      author,
		Author:    author,
	}
	if n.Comment != nil {
		reaction.CommentID = string(n.Comment.ID)
		reaction.IssueID = string(n.Comment.IssueID)
		if reaction.IssueID == "" && n.Comment.Issue != nil {
			reaction.IssueID = string(n.Comment.Issue.ID)
		}
	}
	return reaction
}

// UpdateComment updates the body of a comment and returns the updated comment.
func (c *Client) UpdateComment(ctx context.Context, commentID, body string) (Comment, error) {
	if strings.TrimSpace(commentID) == "" {
		return Comment{}, fmt.Errorf("update comment: comment ID must not be empty")
	}
	if strings.TrimSpace(body) == "" {
		return Comment{}, fmt.Errorf("update comment %s: body must not be empty", commentID)
	}

	var mutation struct {
		CommentUpdate struct {
			Success graphql.Boolean
			Comment commentActionNode
		} `graphql:"commentUpdate(id: $id, input: $input)"`
	}

	variables := map[string]interface{}{
		"id": graphql.String(commentID),
		"input": CommentUpdateInput{
			"body": graphql.String(body),
		},
	}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UpdateComment failed comment_id=%s", commentID)
		return Comment{}, fmt.Errorf("update comment %s: %w", commentID, err)
	}
	if !bool(mutation.CommentUpdate.Success) {
		logger.Error("linearapi.client: UpdateComment operation failed success=false comment_id=%s", commentID)
		return Comment{}, fmt.Errorf("update comment %s: operation failed", commentID)
	}
	return mutation.CommentUpdate.Comment.comment(), nil
}

// DeleteComment deletes a comment by ID.
func (c *Client) DeleteComment(ctx context.Context, commentID string) error {
	if strings.TrimSpace(commentID) == "" {
		return fmt.Errorf("delete comment: comment ID must not be empty")
	}

	var mutation struct {
		CommentDelete struct {
			Success graphql.Boolean
		} `graphql:"commentDelete(id: $id)"`
	}
	variables := map[string]interface{}{"id": graphql.String(commentID)}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: DeleteComment failed comment_id=%s", commentID)
		return fmt.Errorf("delete comment %s: %w", commentID, err)
	}
	if !bool(mutation.CommentDelete.Success) {
		logger.Error("linearapi.client: DeleteComment operation failed success=false comment_id=%s", commentID)
		return fmt.Errorf("delete comment %s: operation failed", commentID)
	}
	return nil
}

// AddCommentReaction adds an emoji reaction to a comment.
func (c *Client) AddCommentReaction(ctx context.Context, commentID, emoji string) (Reaction, error) {
	if strings.TrimSpace(commentID) == "" {
		return Reaction{}, fmt.Errorf("add comment reaction: comment ID must not be empty")
	}
	if strings.TrimSpace(emoji) == "" {
		return Reaction{}, fmt.Errorf("add comment reaction %s: emoji must not be empty", commentID)
	}

	var mutation struct {
		ReactionCreate struct {
			Success  graphql.Boolean
			Reaction reactionActionNode
		} `graphql:"reactionCreate(input: $input)"`
	}
	variables := map[string]interface{}{
		"input": ReactionCreateInput{
			"commentId": graphql.String(commentID),
			"emoji":     graphql.String(emoji),
		},
	}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: AddCommentReaction failed comment_id=%s", commentID)
		return Reaction{}, fmt.Errorf("add comment reaction %s: %w", commentID, err)
	}
	if !bool(mutation.ReactionCreate.Success) {
		logger.Error("linearapi.client: AddCommentReaction operation failed success=false comment_id=%s", commentID)
		return Reaction{}, fmt.Errorf("add comment reaction %s: operation failed", commentID)
	}
	return mutation.ReactionCreate.Reaction.reaction(), nil
}

// RemoveCommentReaction removes an emoji reaction by reaction ID.
func (c *Client) RemoveCommentReaction(ctx context.Context, reactionID string) error {
	if strings.TrimSpace(reactionID) == "" {
		return fmt.Errorf("remove comment reaction: reaction ID must not be empty")
	}

	var mutation struct {
		ReactionDelete struct {
			Success graphql.Boolean
		} `graphql:"reactionDelete(id: $id)"`
	}
	variables := map[string]interface{}{"id": graphql.String(reactionID)}
	if err := c.client.Mutate(ctx, &mutation, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: RemoveCommentReaction failed reaction_id=%s", reactionID)
		return fmt.Errorf("remove comment reaction %s: %w", reactionID, err)
	}
	if !bool(mutation.ReactionDelete.Success) {
		logger.Error("linearapi.client: RemoveCommentReaction operation failed success=false reaction_id=%s", reactionID)
		return fmt.Errorf("remove comment reaction %s: operation failed", reactionID)
	}
	return nil
}
