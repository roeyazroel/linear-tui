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

// DateTime is the Linear GraphQL DateTime scalar represented as an RFC3339
// string. A named type is required because shurcooL/graphql derives variable
// types from the Go type name (time.Time would produce an invalid Time type).
type DateTime string

// GetGraphQLType returns Linear's scalar type name.
func (DateTime) GetGraphQLType() string { return "DateTime" }

// NotificationFilter is Linear's NotificationFilter input object. The map is
// intentionally open because Linear adds comparator fields over time; callers
// can pass schema-confirmed comparator objects such as {"type": {"eq": ...}}.
type NotificationFilter map[string]interface{}

// GetGraphQLType returns the GraphQL input object name.
func (NotificationFilter) GetGraphQLType() string { return "NotificationFilter" }

// MarshalJSON implements json.Marshaler for NotificationFilter.
func (f NotificationFilter) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(f))
}

// NotificationUpdateInput is Linear's NotificationUpdateInput object. The
// map preserves exact schema field names and supports nullable fields (for
// example readAt: null to mark a notification unread).
type NotificationUpdateInput map[string]interface{}

// GetGraphQLType returns the GraphQL input object name.
func (NotificationUpdateInput) GetGraphQLType() string { return "NotificationUpdateInput" }

// MarshalJSON implements json.Marshaler for NotificationUpdateInput.
func (i NotificationUpdateInput) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(i))
}

// Notification is a notification delivered to the current user's inbox.
// Read, Snoozed, and Archived are convenience booleans derived from the
// corresponding nullable schema timestamps.
type Notification struct {
	ID       string
	Type     string
	Category string
	Title    string
	Subtitle string
	URL      string
	InboxURL string

	CreatedAt time.Time
	UpdatedAt time.Time
	ReadAt    *time.Time
	EmailedAt *time.Time

	SnoozedUntilAt *time.Time
	UnsnoozedAt    *time.Time
	ArchivedAt     *time.Time

	Read     bool
	Snoozed  bool
	Archived bool

	// Issue is populated for IssueNotification values. Other notification
	// variants leave it nil because the public interface has no issue field.
	Issue *IssueRef
}

// NotificationListOptions controls ListNotifications. A zero value requests
// the first page with Linear's default ordering and excludes archived entries.
type NotificationListOptions struct {
	Filter          NotificationFilter
	IncludeArchived bool
	OrderBy         PaginationOrderBy
	First           int
}

// ListNotificationsOptions is an alias kept for callers that prefer the
// method-name form.
type ListNotificationsOptions = NotificationListOptions

// NotificationListParams is an alias matching the params naming used by the
// issue APIs in this package.
type NotificationListParams = NotificationListOptions

// NotificationPage is one page of inbox notifications. ListNotifications
// follows EndCursor until HasNext is false; this type is exposed for callers
// that need page-at-a-time control in a future extension.
type NotificationPage struct {
	Notifications []Notification
	HasNext       bool
	EndCursor     *string
}

// ListNotifications fetches all notifications matching options, following
// the schema's forward cursor pagination until the connection is exhausted.
func (c *Client) ListNotifications(ctx context.Context, options NotificationListOptions) ([]Notification, error) {
	if err := validateNotificationListOptions(options); err != nil {
		return nil, err
	}

	var after *string
	notifications := make([]Notification, 0)
	for {
		page, err := c.listNotificationsPage(ctx, options, after)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, page.Notifications...)
		if !page.HasNext {
			return notifications, nil
		}
		if page.EndCursor == nil || strings.TrimSpace(*page.EndCursor) == "" {
			return nil, fmt.Errorf("list notifications: response has next page but no end cursor")
		}
		after = page.EndCursor
	}
}

func (c *Client) listNotificationsPage(ctx context.Context, options NotificationListOptions, after *string) (NotificationPage, error) {
	first := options.First
	if first == 0 {
		first = 50
	}

	var filter *NotificationFilter
	if options.Filter != nil {
		filterCopy := options.Filter
		filter = &filterCopy
	}
	orderByValue := options.OrderBy
	if orderByValue == "" {
		orderByValue = OrderByCreatedAt
	}
	orderBy := &orderByValue
	var afterCursor *graphql.String
	if after != nil {
		cursor := graphql.String(*after)
		afterCursor = &cursor
	}

	var query struct {
		Notifications struct {
			Nodes    []notificationNode
			PageInfo struct {
				HasNextPage graphql.Boolean
				EndCursor   graphql.String
			}
		} `graphql:"notifications(filter: $filter, before: $before, after: $after, first: $first, includeArchived: $includeArchived, orderBy: $orderBy)"`
	}
	variables := map[string]interface{}{
		"after":           afterCursor,
		"before":          (*graphql.String)(nil),
		"filter":          filter,
		"first":           graphql.Int(first),
		"includeArchived": graphql.Boolean(options.IncludeArchived),
		"orderBy":         orderBy,
	}
	if err := c.client.Query(ctx, &query, variables); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ListNotifications failed")
		return NotificationPage{}, fmt.Errorf("list notifications: %w", err)
	}

	notifications := make([]Notification, 0, len(query.Notifications.Nodes))
	for _, node := range query.Notifications.Nodes {
		notifications = append(notifications, parseNotificationNode(node))
	}
	var endCursor *string
	if cursor := string(query.Notifications.PageInfo.EndCursor); cursor != "" {
		endCursor = &cursor
	}
	return NotificationPage{
		Notifications: notifications,
		HasNext:       bool(query.Notifications.PageInfo.HasNextPage),
		EndCursor:     endCursor,
	}, nil
}

// MarkNotificationRead records a read timestamp through notificationUpdate.
func (c *Client) MarkNotificationRead(ctx context.Context, notificationID string, readAt time.Time) (Notification, error) {
	if err := validateNotificationID(notificationID); err != nil {
		return Notification{}, err
	}
	if err := validateNotificationTimestamp("readAt", readAt); err != nil {
		return Notification{}, err
	}
	return c.updateNotification(ctx, notificationID, NotificationUpdateInput{
		"readAt": notificationDateTime(readAt),
	})
}

// MarkNotificationUnread clears the nullable readAt field through
// notificationUpdate. The public schema has no separate single-item unread
// mutation.
func (c *Client) MarkNotificationUnread(ctx context.Context, notificationID string) (Notification, error) {
	if err := validateNotificationID(notificationID); err != nil {
		return Notification{}, err
	}
	var clearedAt *DateTime
	return c.updateNotification(ctx, notificationID, NotificationUpdateInput{
		"readAt": clearedAt,
	})
}

// SnoozeNotification sets snoozedUntilAt through notificationUpdate.
func (c *Client) SnoozeNotification(ctx context.Context, notificationID string, snoozedUntilAt time.Time) (Notification, error) {
	if err := validateNotificationID(notificationID); err != nil {
		return Notification{}, err
	}
	if err := validateNotificationTimestamp("snoozedUntilAt", snoozedUntilAt); err != nil {
		return Notification{}, err
	}
	return c.updateNotification(ctx, notificationID, NotificationUpdateInput{
		"snoozedUntilAt": notificationDateTime(snoozedUntilAt),
	})
}

// UnsnoozeNotification clears the nullable snoozedUntilAt field through
// notificationUpdate. The single-item schema input does not require an
// unsnoozedAt timestamp.
func (c *Client) UnsnoozeNotification(ctx context.Context, notificationID string) (Notification, error) {
	if err := validateNotificationID(notificationID); err != nil {
		return Notification{}, err
	}
	var clearedAt *DateTime
	return c.updateNotification(ctx, notificationID, NotificationUpdateInput{
		"snoozedUntilAt": clearedAt,
	})
}

// ArchiveNotification archives (dismisses) one notification using the exact
// notificationArchive schema mutation.
func (c *Client) ArchiveNotification(ctx context.Context, notificationID string) error {
	if err := validateNotificationID(notificationID); err != nil {
		return err
	}
	var mutation struct {
		NotificationArchive struct {
			Success graphql.Boolean
		} `graphql:"notificationArchive(id: $id)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{
		"id": graphql.String(notificationID),
	}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: ArchiveNotification failed notification_id=%s", notificationID)
		return fmt.Errorf("archive notification %s: %w", notificationID, err)
	}
	if !bool(mutation.NotificationArchive.Success) {
		return fmt.Errorf("archive notification %s: operation failed", notificationID)
	}
	return nil
}

// DismissNotification is a truthful alias for archive: Linear's public API
// exposes notificationArchive, not a distinct dismiss operation.
func (c *Client) DismissNotification(ctx context.Context, notificationID string) error {
	return c.ArchiveNotification(ctx, notificationID)
}

// UnarchiveNotification restores one archived notification.
func (c *Client) UnarchiveNotification(ctx context.Context, notificationID string) error {
	if err := validateNotificationID(notificationID); err != nil {
		return err
	}
	var mutation struct {
		NotificationUnarchive struct {
			Success graphql.Boolean
		} `graphql:"notificationUnarchive(id: $id)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{
		"id": graphql.String(notificationID),
	}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: UnarchiveNotification failed notification_id=%s", notificationID)
		return fmt.Errorf("unarchive notification %s: %w", notificationID, err)
	}
	if !bool(mutation.NotificationUnarchive.Success) {
		return fmt.Errorf("unarchive notification %s: operation failed", notificationID)
	}
	return nil
}

func (c *Client) updateNotification(ctx context.Context, notificationID string, input NotificationUpdateInput) (Notification, error) {
	if len(input) == 0 {
		return Notification{}, fmt.Errorf("update notification %s: input must not be empty", notificationID)
	}
	var mutation struct {
		NotificationUpdate struct {
			Success      graphql.Boolean
			Notification notificationNode
		} `graphql:"notificationUpdate(id: $id, input: $input)"`
	}
	if err := c.client.Mutate(ctx, &mutation, map[string]interface{}{
		"id":    graphql.String(notificationID),
		"input": input,
	}); err != nil {
		logger.ErrorWithErr(err, "linearapi.client: updateNotification failed notification_id=%s", notificationID)
		return Notification{}, fmt.Errorf("update notification %s: %w", notificationID, err)
	}
	if !bool(mutation.NotificationUpdate.Success) {
		return Notification{}, fmt.Errorf("update notification %s: operation failed", notificationID)
	}
	return parseNotificationNode(mutation.NotificationUpdate.Notification), nil
}

func validateNotificationListOptions(options NotificationListOptions) error {
	if options.First < 0 {
		return fmt.Errorf("list notifications: first must be non-negative")
	}
	switch options.OrderBy {
	case "", OrderByCreatedAt, OrderByUpdatedAt:
		return nil
	default:
		return fmt.Errorf("list notifications: unsupported orderBy %q", options.OrderBy)
	}
}

func validateNotificationID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("notification id must not be empty")
	}
	return nil
}

func validateNotificationTimestamp(name string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("notification %s must not be zero", name)
	}
	return nil
}

func notificationDateTime(value time.Time) DateTime {
	return DateTime(value.UTC().Format(time.RFC3339Nano))
}

type notificationNode struct {
	ID                graphql.String
	Type              graphql.String
	Category          graphql.String
	Title             graphql.String
	Subtitle          graphql.String
	URL               graphql.String
	InboxURL          graphql.String
	CreatedAt         graphql.String
	UpdatedAt         graphql.String
	ArchivedAt        *graphql.String
	ReadAt            *graphql.String
	EmailedAt         *graphql.String
	SnoozedUntilAt    *graphql.String
	UnsnoozedAt       *graphql.String
	IssueNotification notificationIssueFragment `graphql:"... on IssueNotification"`
}

type notificationIssueFragment struct {
	Issue struct {
		ID         graphql.String
		Identifier graphql.String
		Title      graphql.String
	}
}

func parseNotificationNode(node notificationNode) Notification {
	readAt := parseOptionalNotificationTime(node.ReadAt)
	emailedAt := parseOptionalNotificationTime(node.EmailedAt)
	snoozedUntilAt := parseOptionalNotificationTime(node.SnoozedUntilAt)
	unsnoozedAt := parseOptionalNotificationTime(node.UnsnoozedAt)
	archivedAt := parseOptionalNotificationTime(node.ArchivedAt)
	var issue *IssueRef
	if id := string(node.IssueNotification.Issue.ID); id != "" {
		issue = &IssueRef{
			ID:         id,
			Identifier: string(node.IssueNotification.Issue.Identifier),
			Title:      string(node.IssueNotification.Issue.Title),
		}
	}
	return Notification{
		ID:             string(node.ID),
		Type:           string(node.Type),
		Category:       string(node.Category),
		Title:          string(node.Title),
		Subtitle:       string(node.Subtitle),
		URL:            string(node.URL),
		InboxURL:       string(node.InboxURL),
		CreatedAt:      parseTime(string(node.CreatedAt)),
		UpdatedAt:      parseTime(string(node.UpdatedAt)),
		ReadAt:         readAt,
		EmailedAt:      emailedAt,
		SnoozedUntilAt: snoozedUntilAt,
		UnsnoozedAt:    unsnoozedAt,
		ArchivedAt:     archivedAt,
		Read:           readAt != nil,
		Snoozed:        snoozedUntilAt != nil,
		Archived:       archivedAt != nil,
		Issue:          issue,
	}
}

func parseOptionalNotificationTime(value *graphql.String) *time.Time {
	if value == nil {
		return nil
	}
	parsed := parseTime(string(*value))
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}
