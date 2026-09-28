package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func commentsModalComment(id, body, authorID string, isMe bool) linearapi.Comment {
	return linearapi.Comment{
		ID:        id,
		Body:      body,
		CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		Author: linearapi.User{
			ID:          authorID,
			Name:        authorID,
			DisplayName: authorID,
			IsMe:        isMe,
		},
	}
}

func commentsModalOptions() CommentsModalOptions {
	return CommentsModalOptions{
		IssueID:        "issue-1",
		CurrentUserID:  "user-1",
		SelectedID:     "comment-1",
		ReactionEmojis: []string{"👍", "🚀"},
		Comments: []CommentThread{
			{
				Comment: commentsModalComment("comment-1", "Root comment", "user-1", true),
				Replies: []CommentThread{{
					Comment:  commentsModalComment("reply-1", "A reply", "user-2", false),
					ParentID: "comment-1",
				}},
			},
			{Comment: commentsModalComment("comment-2", "Another root", "user-2", false)},
		},
	}
}

func TestCommentsModalShowThreadsNavigationAndHelp(t *testing.T) {
	modal := NewCommentsModal(nil)
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{})

	if got := modal.SelectedCommentID(); got != "comment-1" {
		t.Fatalf("selected comment = %q, want comment-1", got)
	}
	if got := len(modal.VisibleComments()); got != 2 {
		t.Fatalf("visible comments = %d, want 2 collapsed roots", got)
	}
	if !strings.Contains(modal.HelpText(), "j/k") || !strings.Contains(modal.HelpText(), "Enter/Space") || !strings.Contains(modal.HelpText(), "Esc") {
		t.Fatalf("help text = %q, missing contextual shortcuts", modal.HelpText())
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if got := len(modal.VisibleComments()); got != 3 {
		t.Fatalf("visible comments after expand = %d, want 3", got)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if got := modal.SelectedCommentID(); got != "reply-1" {
		t.Fatalf("selected after j = %q, want reply-1", got)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if got := modal.SelectedCommentID(); got != "comment-2" {
		t.Fatalf("selected after down = %q, want comment-2", got)
	}
}

func TestCommentsModalReloadPreservesStableSelection(t *testing.T) {
	modal := NewCommentsModal(nil)
	options := commentsModalOptions()
	modal.Show(options, CommentsModalCallbacks{})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if modal.SelectedCommentID() != "reply-1" {
		t.Fatalf("selected before reload = %q, want reply-1", modal.SelectedCommentID())
	}

	reloaded := []CommentThread{
		{Comment: commentsModalComment("comment-2", "Another root updated", "user-2", false)},
		{Comment: commentsModalComment("comment-1", "Root updated", "user-1", true), Replies: []CommentThread{{
			Comment:  commentsModalComment("reply-1", "Reply updated", "user-2", false),
			ParentID: "comment-1",
		}}},
	}
	modal.Reload(reloaded)
	if got := modal.SelectedCommentID(); got != "reply-1" {
		t.Fatalf("selected after reload = %q, want reply-1", got)
	}
	if got := modal.SelectedComment().Comment.Body; got != "Reply updated" {
		t.Fatalf("selected body after reload = %q, want Reply updated", got)
	}

	modal.Reload([]CommentThread{{Comment: commentsModalComment("new", "New", "user-3", false)}})
	if got := modal.SelectedCommentID(); got != "new" {
		t.Fatalf("selected after removed selection = %q, want new", got)
	}
}

func TestCommentsModalEditOwnCommentUsesCallbackAndUpdatesBody(t *testing.T) {
	var gotID, gotBody string
	modal := NewCommentsModal(nil)
	options := commentsModalOptions()
	modal.Show(options, CommentsModalCallbacks{
		OnEdit: func(commentID, body string) error {
			gotID, gotBody = commentID, body
			return nil
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if !modal.IsEditing() {
		t.Fatal("e did not enter edit mode")
	}
	modal.EditField().SetText("Edited body", true)
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl))
	if gotID != "comment-1" || gotBody != "Edited body" {
		t.Fatalf("edit callback = (%q, %q), want comment-1/Edited body", gotID, gotBody)
	}
	if modal.IsEditing() {
		t.Fatal("successful edit left edit mode open")
	}
	if got := modal.SelectedComment().Comment.Body; got != "Edited body" {
		t.Fatalf("selected body = %q, want Edited body", got)
	}
}

func TestCommentsModalRejectsEditingOtherCommentAndReportsError(t *testing.T) {
	modal := NewCommentsModal(nil)
	options := commentsModalOptions()
	modal.Show(options, CommentsModalCallbacks{})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if modal.IsEditing() {
		t.Fatal("e entered edit mode for another user's comment")
	}
	if !strings.Contains(strings.ToLower(modal.ErrorMessage()), "own") {
		t.Fatalf("error = %q, want ownership error", modal.ErrorMessage())
	}
}

func TestCommentsModalDeleteConfirmationAndEscCancel(t *testing.T) {
	deleted := ""
	modal := NewCommentsModal(nil)
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{
		OnDelete: func(commentID string) error {
			deleted = commentID
			return nil
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if !modal.IsDeleteConfirmationOpen() {
		t.Fatal("d did not open delete confirmation")
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if modal.IsDeleteConfirmationOpen() || deleted != "" {
		t.Fatalf("Esc confirmation state = (%v, %q)", modal.IsDeleteConfirmationOpen(), deleted)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if deleted != "comment-1" {
		t.Fatalf("delete callback ID = %q, want comment-1", deleted)
	}
	if modal.SelectedCommentID() != "comment-2" {
		t.Fatalf("selection after delete = %q, want comment-2", modal.SelectedCommentID())
	}
}

func TestCommentsModalReactionPickerAddsAndRemoves(t *testing.T) {
	var addedID, addedEmoji, removedID string
	options := commentsModalOptions()
	options.Comments[0].Reactions = []linearapi.Reaction{{
		ID:        "reaction-1",
		Emoji:     "👍",
		CommentID: "comment-1",
		User:      linearapi.User{ID: "user-2"},
	}}
	modal := NewCommentsModal(nil)
	modal.Show(options, CommentsModalCallbacks{
		OnAddReaction: func(commentID, emoji string) error {
			addedID, addedEmoji = commentID, emoji
			return nil
		},
		OnRemoveReaction: func(reactionID string) error {
			removedID = reactionID
			return nil
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	if !modal.IsReactionPickerOpen() {
		t.Fatal("r did not open reaction picker")
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if addedID != "comment-1" || addedEmoji != "👍" {
		t.Fatalf("add reaction callback = (%q, %q), want comment-1/👍", addedID, addedEmoji)
	}
	// The existing current-user reaction is removed when its emoji is selected.
	options.Comments[0].Reactions[0].User.IsMe = true
	modal.Reload(options.Comments)
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if removedID != "reaction-1" {
		t.Fatalf("remove reaction callback ID = %q, want reaction-1", removedID)
	}
}

func TestCommentsModalReactionPickerRemovesAPIAliasForUnicodePickerEmoji(t *testing.T) {
	var added, removed string
	options := commentsModalOptions()
	options.Comments[0].Reactions = []linearapi.Reaction{{
		ID:        "reaction-plus-one",
		Emoji:     "+1",
		CommentID: "comment-1",
		User:      linearapi.User{ID: "user-1", IsMe: true},
	}}
	modal := NewCommentsModal(nil)
	modal.Show(options, CommentsModalCallbacks{
		OnAddReaction: func(commentID, emoji string) error {
			added = commentID + ":" + emoji
			return nil
		},
		OnRemoveReaction: func(reactionID string) error {
			removed = reactionID
			return nil
		},
	})
	if got := formatCommentSecondary(commentsModalRow{
		Comment:   options.Comments[0].Comment,
		Reactions: options.Comments[0].Reactions,
	}); !strings.HasSuffix(got, "  👍") {
		t.Fatalf("reaction display = %q, want Unicode thumbs-up", got)
	}

	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if added != "" {
		t.Fatalf("add reaction callback = %q, want no add", added)
	}
	if removed != "reaction-plus-one" {
		t.Fatalf("remove reaction callback ID = %q, want reaction-plus-one", removed)
	}
}

func TestNormalizedCommentReactionEmojiCoversDefaultPickerAliases(t *testing.T) {
	for alias, want := range map[string]string{
		"+1":               "👍",
		"heart":            "❤️",
		"tada":             "🎉",
		"rocket":           "🚀",
		"eyes":             "👀",
		"white_check_mark": "✅",
	} {
		if got := normalizedCommentReactionEmoji(alias); got != want {
			t.Errorf("normalizedCommentReactionEmoji(%q) = %q, want %q", alias, got, want)
		}
	}
	if got := normalizedCommentReactionEmoji(":custom:"); got != ":custom:" {
		t.Fatalf("custom reaction normalized to %q", got)
	}
}

func TestCommentsModalLoadingErrorProgressAndEmptyState(t *testing.T) {
	modal := NewCommentsModal(nil)
	modal.Show(CommentsModalOptions{}, CommentsModalCallbacks{})
	if !strings.Contains(strings.ToLower(modal.EmptyStateText()), "no comments") {
		t.Fatalf("empty state = %q", modal.EmptyStateText())
	}
	modal.SetLoading(true)
	if !modal.Loading() || !strings.Contains(strings.ToLower(modal.StatusText()), "loading") {
		t.Fatalf("loading status = (%v, %q)", modal.Loading(), modal.StatusText())
	}
	modal.SetProgress("2/5")
	if modal.ProgressMessage() != "2/5" {
		t.Fatalf("progress = %q, want 2/5", modal.ProgressMessage())
	}
	modal.SetError("request failed")
	if modal.ErrorMessage() != "request failed" || !strings.Contains(modal.StatusText(), "request failed") {
		t.Fatalf("error status = (%q, %q)", modal.ErrorMessage(), modal.StatusText())
	}
	modal.SetLoading(false)
	if modal.Loading() {
		t.Fatal("SetLoading(false) left loading true")
	}
}

func TestCommentsModalLifecycle(t *testing.T) {
	app := newCommentsModalTestApp("")
	closed := 0
	modal := NewCommentsModal(app)
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{OnClose: func() { closed++ }})
	if !app.pages.HasPage("comments_modal") {
		t.Fatal("Show did not add comments_modal page")
	}
	if modal.GetModal() == nil {
		t.Fatal("GetModal returned nil")
	}
	if !strings.Contains(modal.HelpText(), "reaction") {
		t.Fatalf("help = %q, want reaction shortcut", modal.HelpText())
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if app.pages.HasPage("comments_modal") || closed != 1 {
		t.Fatalf("close state = (page=%v, callbacks=%d)", app.pages.HasPage("comments_modal"), closed)
	}
}

func newCommentsModalTestApp(mode string) *App {
	return NewApp(&linearapi.Client{}, config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		LinearAPIKey:   "test-key",
		APIEndpoint:    config.DefaultAPIEndpoint,
		LogLevel:       config.DefaultLogLevel,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
	}, nil)
}

func TestCommentsModalDefensiveSelectionNil(t *testing.T) {
	modal := NewCommentsModal(nil)
	modal.Show(CommentsModalOptions{Comments: []CommentThread{{Comment: commentsModalComment("one", "body", "u", false)}}}, CommentsModalCallbacks{})
	if modal.SelectedComment() == nil || !reflect.DeepEqual(modal.SelectedComment().Comment.ID, "one") {
		t.Fatalf("selected = %#v, want one", modal.SelectedComment())
	}
	modal.Reload(nil)
	if modal.SelectedComment() != nil || modal.SelectedCommentID() != "" {
		t.Fatalf("selected after empty reload = (%#v, %q)", modal.SelectedComment(), modal.SelectedCommentID())
	}
	modal.HandleKey(nil)
}

func TestAppCommentsUseFetchedThreadsAndReactionIDs(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	root := commentsModalComment("root", "Root", "user-1", true)
	root.Reactions = []linearapi.Reaction{{
		ID:        "reaction-fetched",
		Emoji:     "👍",
		CommentID: "root",
		User:      linearapi.User{ID: "user-1", IsMe: true},
	}}
	reply := commentsModalComment("reply", "Reply", "user-2", false)
	reply.ParentID = "root"
	issue := linearapi.Issue{ID: "issue-comments", Identifier: "ABC-9", Comments: []linearapi.Comment{root, reply}}
	app.selectedIssue = &issue
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) { return issue, nil }
	app.openCommentsModal()
	if got := len(app.commentsModal.VisibleComments()); got != 1 {
		t.Fatalf("collapsed fetched threads = %d, want one root", got)
	}
	selected := app.commentsModal.SelectedComment()
	if selected == nil || len(selected.Reactions) != 1 || selected.Reactions[0].ID != "reaction-fetched" {
		t.Fatalf("fetched reaction = %#v, want reaction-fetched", selected)
	}
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if got := len(app.commentsModal.VisibleComments()); got != 2 {
		t.Fatalf("expanded fetched threads = %d, want root plus reply", got)
	}
	removed := make(chan string, 1)
	app.commentsModal.callbacks.OnRemoveReaction = func(reactionID string) error {
		removed <- reactionID
		return nil
	}
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	select {
	case got := <-removed:
		if got != "reaction-fetched" {
			t.Fatalf("removed reaction ID = %q, want fetched ID", got)
		}
	case <-time.After(time.Second):
		t.Fatal("fetched reaction removal did not run")
	}
	waitForCommentsRunner(t, app.commentsModal)
}

func TestCommentsModalDropsClosedMutationAndSurfacesErrors(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	started := make(chan struct{})
	release := make(chan struct{})
	modal := NewCommentsModal(app)
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{
		OnEdit: func(string, string) error {
			close(started)
			<-release
			return nil
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	modal.EditField().SetText("must not apply", true)
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	<-started
	modal.Hide()
	close(release)
	waitForCommentsRunner(t, modal)
	var closedBody string
	workspaceUIRead(app, func() {
		if selected := modal.SelectedComment(); selected != nil {
			closedBody = selected.Comment.Body
		}
	})
	if closedBody != "Root comment" {
		t.Fatalf("closed edit body = %q, want original", closedBody)
	}

	var calls atomic.Int32
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{
		OnEdit: func(string, string) error {
			calls.Add(1)
			return errors.New("edit failed")
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	modal.EditField().SetText("error", true)
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	waitForCommentsRunner(t, modal)
	var errorMessage string
	workspaceUIRead(app, func() { errorMessage = modal.ErrorMessage() })
	if calls.Load() != 1 || !strings.Contains(errorMessage, "edit failed") {
		t.Fatalf("error edit = calls:%d error:%q", calls.Load(), errorMessage)
	}
}

func waitForCommentsRunner(t *testing.T, modal *CommentsModal) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for modal.actionRunner != nil && modal.actionRunner.InFlight() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if modal.actionRunner != nil && modal.actionRunner.InFlight() {
		t.Fatal("comment action runner remained in flight")
	}
	if modal.app != nil {
		workspaceUIRead(modal.app, func() {})
	}
}

func TestCommentsModalAppMutationReturnsImmediately(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	started := make(chan struct{})
	release := make(chan struct{})
	modal := NewCommentsModal(app)
	modal.Show(commentsModalOptions(), CommentsModalCallbacks{
		OnEdit: func(string, string) error {
			close(started)
			<-release
			return nil
		},
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	modal.EditField().SetText("edited asynchronously", true)
	done := make(chan struct{})
	go func() {
		modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("edit callback did not start")
	}
	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("comment edit key handler blocked on callback")
	}
	close(release)
	waitForCommentsRunner(t, modal)
	var loading bool
	var body string
	workspaceUIRead(app, func() {
		loading = modal.Loading()
		if selected := modal.SelectedComment(); selected != nil {
			body = selected.Comment.Body
		}
	})
	if loading {
		t.Fatal("comment edit remained loading after callback completion")
	}
	if body != "edited asynchronously" {
		t.Fatalf("edited body = %q, want async result", body)
	}
}

func TestAppCommentsMutationSelectionMoveStillRefreshesAuthoritatively(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	first := commentsModalComment("comment-move-1", "Original", "user-1", true)
	second := commentsModalComment("comment-move-2", "Second", "user-1", true)
	issue := linearapi.Issue{ID: "issue-move-comments", Identifier: "ABC-15", Comments: []linearapi.Comment{first, second}}
	authoritative := issue
	authoritative.Comments = append([]linearapi.Comment(nil), issue.Comments...)
	authoritative.Comments[0].Body = "Server authoritative"
	app.selectedIssue = &issue
	var updateCalls, refreshCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	app.updateCommentFunc = func(ctx context.Context, commentID, body string) (linearapi.Comment, error) {
		updateCalls.Add(1)
		if commentID != first.ID || body != "Edited locally" {
			t.Errorf("update target = (%q, %q), want (%q, %q)", commentID, body, first.ID, "Edited locally")
		}
		close(started)
		<-release
		updated := first
		updated.Body = body
		return updated, nil
	}
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
		refreshCalls.Add(1)
		return authoritative, nil
	}

	app.openCommentsModal()
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	app.commentsModal.EditField().SetText("Edited locally", true)
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("comment mutation did not start")
	}
	app.commentsModal.moveSelection(1)
	if got := app.commentsModal.SelectedCommentID(); got != second.ID {
		t.Fatalf("selection during mutation = %q, want %q", got, second.ID)
	}
	close(release)
	waitForCommentsRunner(t, app.commentsModal)
	waitForCondition(t, time.Second, func() bool {
		selectedID, firstBody := "", ""
		workspaceUIRead(app, func() {
			selectedID = app.commentsModal.SelectedCommentID()
			if selected := findComment(app.commentsModal.comments, first.ID); selected != nil {
				firstBody = selected.Comment.Body
			}
		})
		return refreshCalls.Load() == 1 && selectedID == second.ID && firstBody == "Server authoritative"
	})
	var selectedID, firstBody string
	workspaceUIRead(app, func() {
		selectedID = app.commentsModal.SelectedCommentID()
		if selected := findComment(app.commentsModal.comments, first.ID); selected != nil {
			firstBody = selected.Comment.Body
		}
	})
	if updateCalls.Load() != 1 || refreshCalls.Load() != 1 {
		t.Fatalf("moved comment mutation calls = update:%d refresh:%d, want one each", updateCalls.Load(), refreshCalls.Load())
	}
	if selectedID != second.ID {
		t.Fatalf("selection after authoritative reload = %q, want %q", selectedID, second.ID)
	}
	if firstBody != "Server authoritative" {
		t.Fatalf("comment body after authoritative reload = %q, want server value", firstBody)
	}
}

func TestAppCommentsMutationCancellationCancelsContextAndSkipsRefresh(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	issue := linearapi.Issue{
		ID:         "issue-cancel-comments",
		Identifier: "ABC-10",
		Comments:   []linearapi.Comment{commentsModalComment("comment-cancel", "Original", "user-1", true)},
	}
	app.selectedIssue = &issue
	var refreshCalls atomic.Int32
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
		refreshCalls.Add(1)
		return issue, nil
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	app.updateCommentFunc = func(ctx context.Context, _, _ string) (linearapi.Comment, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return linearapi.Comment{}, ctx.Err()
	}
	app.openCommentsModal()
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	app.commentsModal.EditField().SetText("must be canceled", true)
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("comment mutation did not start")
	}
	app.commentsModal.Hide()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("comment mutation context was not canceled on Hide")
	}
	waitForCommentsRunner(t, app.commentsModal)
	if refreshCalls.Load() != 0 {
		t.Fatalf("refresh calls after canceled comment mutation = %d, want zero", refreshCalls.Load())
	}
}

func TestAppCommentsMutationEscapeCancelsContextAndSkipsRefresh(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	issue := linearapi.Issue{
		ID:         "issue-escape-comments",
		Identifier: "ABC-12",
		Comments:   []linearapi.Comment{commentsModalComment("comment-escape", "Original", "user-1", true)},
	}
	app.selectedIssue = &issue
	var refreshCalls atomic.Int32
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
		refreshCalls.Add(1)
		return issue, nil
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	app.updateCommentFunc = func(ctx context.Context, _, _ string) (linearapi.Comment, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return linearapi.Comment{}, ctx.Err()
	}
	app.openCommentsModal()
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	app.commentsModal.EditField().SetText("must be canceled", true)
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("comment mutation did not start")
	}
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("comment mutation context was not canceled on Escape")
	}
	waitForCommentsRunner(t, app.commentsModal)
	if refreshCalls.Load() != 0 {
		t.Fatalf("refresh calls after Escape-canceled comment mutation = %d, want zero", refreshCalls.Load())
	}
}

func TestAppCommentsMutationNewIssueInvalidationCancelsContext(t *testing.T) {
	app := newCommentsModalTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	oldIssue := linearapi.Issue{
		ID:         "issue-old-comments",
		Identifier: "ABC-13",
		Comments:   []linearapi.Comment{commentsModalComment("comment-old", "Original", "user-1", true)},
	}
	newIssue := linearapi.Issue{
		ID:         "issue-new-comments",
		Identifier: "ABC-14",
		Comments:   []linearapi.Comment{commentsModalComment("comment-new", "New issue", "user-1", true)},
	}
	app.selectedIssue = &oldIssue
	var refreshCalls atomic.Int32
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
		refreshCalls.Add(1)
		return oldIssue, nil
	}
	started := make(chan struct{})
	canceled := make(chan struct{})
	app.updateCommentFunc = func(ctx context.Context, _, _ string) (linearapi.Comment, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return linearapi.Comment{}, ctx.Err()
	}
	app.openCommentsModal()
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	app.commentsModal.EditField().SetText("must be canceled", true)
	app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("comment mutation did not start")
	}
	app.issuesMu.Lock()
	app.selectedIssue = &newIssue
	app.issuesMu.Unlock()
	app.openCommentsModal()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("comment mutation context was not canceled on new issue")
	}
	waitForCommentsRunner(t, app.commentsModal)
	if refreshCalls.Load() != 0 {
		t.Fatalf("refresh calls after new-issue comment invalidation = %d, want zero", refreshCalls.Load())
	}
}

func TestCommentsModalReactionReloadOrderingDoesNotDuplicate(t *testing.T) {
	for _, reloadFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "mutation-before-reload", true: "reload-before-mutation"}[reloadFirst], func(t *testing.T) {
			app := newCommentsModalTestApp("")
			app.queueUpdateDraw = func(fn func()) { fn() }
			options := commentsModalOptions()
			options.Comments[0].Reactions = nil
			modal := NewCommentsModal(app)
			started := make(chan struct{})
			release := make(chan struct{})
			authoritative := cloneCommentThreads(options.Comments)
			authoritative[0].Reactions = []linearapi.Reaction{{
				ID:        "reaction-authoritative",
				Emoji:     "👍",
				CommentID: "comment-1",
				User:      linearapi.User{ID: "user-1", IsMe: true},
			}}
			modal.Show(options, CommentsModalCallbacks{
				OnAddReactionContext: func(ctx context.Context, commentID, emoji string) error {
					close(started)
					<-release
					return nil
				},
			})
			modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
			modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("reaction mutation did not start")
			}
			if reloadFirst {
				modal.Reload(authoritative)
			}
			close(release)
			waitForCommentsRunner(t, modal)
			if !reloadFirst {
				modal.Reload(authoritative)
			}
			selected := modal.SelectedComment()
			if selected == nil || len(selected.Reactions) != 1 || selected.Reactions[0].ID != "reaction-authoritative" {
				t.Fatalf("reaction state after %s = %#v, want one authoritative reaction", map[bool]string{false: "mutation-before-reload", true: "reload-before-mutation"}[reloadFirst], selected)
			}
		})
	}
}

func TestAppCommentsAddReactionRefreshOrderingUsesOneAuthoritativeReaction(t *testing.T) {
	for _, reloadFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "mutation-before-reload", true: "reload-before-mutation"}[reloadFirst], func(t *testing.T) {
			app := newCommentsModalTestApp("")
			issue := linearapi.Issue{
				ID:         "issue-reaction-order",
				Identifier: "ABC-11",
				Comments:   []linearapi.Comment{commentsModalComment("comment-reaction-order", "Root", "user-1", true)},
			}
			app.selectedIssue = &issue
			authoritative := issue
			authoritative.Comments = append([]linearapi.Comment(nil), issue.Comments...)
			authoritative.Comments[0].Reactions = []linearapi.Reaction{{
				ID:        "reaction-authoritative-app",
				Emoji:     "👍",
				CommentID: "comment-reaction-order",
				User:      linearapi.User{ID: "user-1", IsMe: true},
			}}
			var addCalls, refreshCalls atomic.Int32
			addStarted := make(chan struct{})
			releaseAdd := make(chan struct{})
			refreshStarted := make(chan struct{}, 1)
			refreshApplied := make(chan struct{}, 1)
			app.queueUpdateDraw = func(fn func()) {
				fn()
				if app.commentsModal == nil {
					return
				}
				selected := app.commentsModal.SelectedComment()
				if selected == nil || len(selected.Reactions) != 1 || selected.Reactions[0].ID != "reaction-authoritative-app" {
					return
				}
				select {
				case refreshApplied <- struct{}{}:
				default:
				}
			}
			app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
				refreshCalls.Add(1)
				select {
				case refreshStarted <- struct{}{}:
				default:
				}
				return authoritative, nil
			}
			app.addCommentReactionFunc = func(ctx context.Context, _, _ string) (linearapi.Reaction, error) {
				addCalls.Add(1)
				close(addStarted)
				<-releaseAdd
				return linearapi.Reaction{ID: "reaction-authoritative-app", Emoji: "👍"}, nil
			}
			app.openCommentsModal()
			app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
			app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			select {
			case <-addStarted:
			case <-time.After(time.Second):
				t.Fatal("app reaction mutation did not start")
			}
			if reloadFirst {
				app.commentsModal.Reload(BuildCommentThreads(authoritative.Comments))
			}
			close(releaseAdd)
			select {
			case <-refreshStarted:
			case <-time.After(time.Second):
				t.Fatal("authoritative reaction refresh did not start")
			}
			if !reloadFirst {
				select {
				case <-refreshApplied:
				case <-time.After(time.Second):
					t.Fatal("authoritative reaction refresh did not apply")
				}
			} else {
				select {
				case <-refreshApplied:
				case <-time.After(time.Second):
					t.Fatal("authoritative reaction refresh did not apply after mutation")
				}
			}
			waitForCommentsRunner(t, app.commentsModal)
			selected := app.commentsModal.SelectedComment()
			if addCalls.Load() != 1 || refreshCalls.Load() != 1 {
				t.Fatalf("reaction calls after %s = add:%d refresh:%d, want one each", map[bool]string{false: "mutation-before-reload", true: "reload-before-mutation"}[reloadFirst], addCalls.Load(), refreshCalls.Load())
			}
			if selected == nil || len(selected.Reactions) != 1 || selected.Reactions[0].ID != "reaction-authoritative-app" {
				t.Fatalf("reaction after %s = %#v, want one authoritative reaction", map[bool]string{false: "mutation-before-reload", true: "reload-before-mutation"}[reloadFirst], selected)
			}
		})
	}
}
