package tui

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

const commentsModalPageName = "comments_modal"

// CommentThread is one comment and its nested replies. ParentID is useful
// when callers load a flat activity stream; nested Replies are rendered below
// the parent when its thread is expanded.
type CommentThread struct {
	Comment   linearapi.Comment
	ParentID  string
	Replies   []CommentThread
	Reactions []linearapi.Reaction
	Expanded  bool
}

// CommentsModalComment and CommentNode are descriptive aliases for callers
// that use either activity or tree terminology.
type CommentsModalComment = CommentThread
type CommentNode = CommentThread

// CommentsModalOptions contains the issue activity and local presentation
// state for a CommentsModal.
type CommentsModalOptions struct {
	IssueID        string
	CurrentUserID  string
	SelectedID     string
	Comments       []CommentThread
	ReactionEmojis []string
}

// CommentsModalCallbacks own all network work. Callbacks return errors so the
// modal can present failures synchronously without timers or hidden goroutines.
// The shorter aliases are accepted for callers that prefer verb-first names.
type CommentsModalCallbacks struct {
	OnEdit                  func(commentID, body string) error
	OnEditContext           func(ctx context.Context, commentID, body string) error
	OnDelete                func(commentID string) error
	OnDeleteContext         func(ctx context.Context, commentID string) error
	OnAddReaction           func(commentID, emoji string) error
	OnAddReactionContext    func(ctx context.Context, commentID, emoji string) error
	OnRemoveReaction        func(reactionID string) error
	OnRemoveReactionContext func(ctx context.Context, reactionID string) error
	OnActionComplete        func(name, commentID string)
	OnClose                 func()

	Edit                  func(commentID, body string) error
	EditContext           func(ctx context.Context, commentID, body string) error
	Delete                func(commentID string) error
	DeleteContext         func(ctx context.Context, commentID string) error
	AddReaction           func(commentID, emoji string) error
	AddReactionContext    func(ctx context.Context, commentID, emoji string) error
	RemoveReaction        func(reactionID string) error
	RemoveReactionContext func(ctx context.Context, reactionID string) error
	ActionComplete        func(name, commentID string)
	Close                 func()
}

// CommentsModal is a reusable comments/activity browser. It is deliberately
// presentation-only: callers inject callbacks for edits, deletion, and
// reactions and can reload the activity after their network operation.
type CommentsModal struct {
	app   *App
	modal *tview.Flex

	list         *tview.List
	header       *tview.TextView
	status       *tview.TextView
	help         *tview.TextView
	editField    *tview.TextArea
	reactionList *tview.List

	// Exported handles make the component easy to compose and inspect in tests.
	List     *tview.List
	Header   *tview.TextView
	Status   *tview.TextView
	Help     *tview.TextView
	EditArea *tview.TextArea

	issueID        string
	currentUserID  string
	comments       []CommentThread
	rows           []commentsModalRow
	selectedID     string
	expanded       map[string]bool
	reactionEmojis []string
	reactionIndex  int

	callbacks     CommentsModalCallbacks
	actionRunner  *AsyncViewActionRunner
	loading       bool
	errorMessage  string
	progress      string
	editOpen      bool
	confirmDelete bool
	pendingDelete string
	reactionOpen  bool
}

type commentsModalRow struct {
	ID         string
	ParentID   string
	Depth      int
	Comment    linearapi.Comment
	Reactions  []linearapi.Reaction
	HasReplies bool
}

var defaultCommentReactionEmojis = []string{"👍", "❤️", "🎉", "🚀", "👀", "✅"}

var commentReactionAliases = map[string]string{
	"+1":               "👍",
	"heart":            "❤️",
	"tada":             "🎉",
	"rocket":           "🚀",
	"eyes":             "👀",
	"white_check_mark": "✅",
}

func normalizedCommentReactionEmoji(emoji string) string {
	if unicode, ok := commentReactionAliases[emoji]; ok {
		return unicode
	}
	return emoji
}

// NewCommentsModal creates a comments/activity modal. A nil app is supported
// for embedding in another view or for deterministic unit tests.
func NewCommentsModal(app *App) *CommentsModal {
	modal := &CommentsModal{
		app:            app,
		expanded:       make(map[string]bool),
		reactionEmojis: append([]string(nil), defaultCommentReactionEmojis...),
	}
	if app != nil {
		modal.actionRunner = NewAsyncViewActionRunner(app.QueueUpdateDraw, AsyncViewActionHooks{
			OnStart: func(name string) {
				modal.errorMessage = ""
				modal.progress = name + "…"
				modal.SetLoading(true)
			},
			OnProgress: func(_, message string) {
				modal.progress = strings.TrimSpace(message)
				modal.updateStatus()
				modal.updatePage()
			},
			OnError: func(_ string, err error) {
				if err != nil {
					modal.SetError(err.Error())
				}
			},
			OnComplete: func(_ string, _ error) {
				modal.SetLoading(false)
			},
		})
	}
	modal.ensureWidgets()
	modal.buildModal()
	return modal
}

func (cm *CommentsModal) ensureWidgets() {
	if cm == nil {
		return
	}
	theme := cm.theme()
	if cm.list == nil {
		cm.list = tview.NewList().
			ShowSecondaryText(true).
			SetMainTextColor(theme.Foreground).
			SetSecondaryTextColor(theme.SecondaryText).
			SetSelectedBackgroundColor(theme.Accent).
			SetSelectedTextColor(theme.SelectionText).
			SetHighlightFullLine(true)
		cm.list.SetBackgroundColor(theme.HeaderBg)
		cm.list.SetSelectedFunc(func(int, string, string, rune) {
			cm.toggleSelectedThread()
		})
	}
	if cm.header == nil {
		cm.header = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	if cm.status == nil {
		cm.status = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	if cm.help == nil {
		cm.help = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	if cm.editField == nil {
		cm.editField = tview.NewTextArea().SetWrap(true).SetWordWrap(true)
	}
	if cm.reactionList == nil {
		cm.reactionList = tview.NewList().
			ShowSecondaryText(false).
			SetMainTextColor(theme.Foreground).
			SetSelectedBackgroundColor(theme.Accent).
			SetSelectedTextColor(theme.SelectionText).
			SetHighlightFullLine(true)
		cm.reactionList.SetBackgroundColor(theme.HeaderBg)
	}
	cm.List = cm.list
	cm.Header = cm.header
	cm.Status = cm.status
	cm.Help = cm.help
	cm.EditArea = cm.editField
}

func (cm *CommentsModal) theme() Theme {
	if cm != nil && cm.app != nil {
		return cm.app.theme
	}
	return LinearTheme
}

func (cm *CommentsModal) density() DensityProfile {
	if cm != nil && cm.app != nil {
		return cm.app.density
	}
	return ResolveDensity(config.DefaultDensity)
}

// Show replaces the activity snapshot, resets transient interaction state,
// and presents the modal when it is attached to an App.
func (cm *CommentsModal) Show(options CommentsModalOptions, callbacks CommentsModalCallbacks) {
	if cm == nil {
		return
	}
	if cm.actionRunner != nil {
		cm.actionRunner.Invalidate()
	}
	if cm.app != nil {
		cm.app.commentsLoadGeneration.Add(1)
	}
	cm.callbacks = callbacks
	cm.issueID = options.IssueID
	cm.currentUserID = options.CurrentUserID
	cm.selectedID = strings.TrimSpace(options.SelectedID)
	cm.reactionEmojis = cloneNonEmptyStrings(options.ReactionEmojis)
	if len(cm.reactionEmojis) == 0 {
		cm.reactionEmojis = append([]string(nil), defaultCommentReactionEmojis...)
	}
	cm.expanded = make(map[string]bool)
	cm.editOpen = false
	cm.confirmDelete = false
	cm.pendingDelete = ""
	cm.reactionOpen = false
	cm.loading = false
	cm.errorMessage = ""
	cm.progress = ""
	cm.Reload(options.Comments)
	cm.showPage()
	cm.focusList()
}

// Reload replaces comments while preserving selection by stable comment ID.
// Existing expanded thread state is retained where the parent still exists.
func (cm *CommentsModal) Reload(comments []CommentThread) {
	if cm == nil {
		return
	}
	previousSelection := cm.selectedID
	previousExpanded := cm.expanded
	cm.comments = cloneCommentThreads(comments)
	cm.expanded = make(map[string]bool)
	for _, thread := range cm.comments {
		copyExpandedCommentState(thread, previousExpanded, cm.expanded)
	}
	if previousSelection != "" && containsCommentID(cm.comments, previousSelection) {
		cm.selectedID = previousSelection
		ensureCommentParentsExpanded(cm.comments, previousSelection, cm.expanded)
	} else {
		cm.selectedID = firstCommentID(cm.comments)
	}
	cm.refreshRows()
	cm.updatePage()
}

// ReloadComments is a descriptive alias for Reload.
func (cm *CommentsModal) ReloadComments(comments []CommentThread) { cm.Reload(comments) }

// SetComments replaces the current activity snapshot and preserves selection.
func (cm *CommentsModal) SetComments(comments []CommentThread) { cm.Reload(comments) }

// Hide closes the modal and clears callbacks so a stale action cannot fire
// after cancellation.
func (cm *CommentsModal) Hide() {
	if cm == nil {
		return
	}
	if cm.actionRunner != nil {
		cm.actionRunner.Invalidate()
	}
	if cm.app != nil {
		cm.app.commentsLoadGeneration.Add(1)
	}
	cm.editOpen = false
	cm.confirmDelete = false
	cm.pendingDelete = ""
	cm.reactionOpen = false
	if cm.app != nil && cm.app.pages != nil {
		cm.app.pages.RemovePage(commentsModalPageName)
		if cm.app.app != nil {
			cm.app.updateFocus()
		}
	}
	closeCallback := cm.callbacks.OnClose
	if closeCallback == nil {
		closeCallback = cm.callbacks.Close
	}
	cm.callbacks.OnClose = nil
	cm.callbacks.Close = nil
	if closeCallback != nil {
		closeCallback()
	}
}

// HandleKey handles deterministic modal-level navigation and action keys.
// Ordinary text entry is delegated to the edit TextArea while editing.
func (cm *CommentsModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if cm == nil || event == nil {
		return event
	}
	if cm.confirmDelete {
		return cm.handleDeleteConfirmation(event)
	}
	if cm.editOpen {
		return cm.handleEditKey(event)
	}
	if cm.reactionOpen {
		return cm.handleReactionKey(event)
	}
	switch event.Key() {
	case tcell.KeyEscape:
		cm.Hide()
		return nil
	case tcell.KeyUp:
		cm.moveSelection(-1)
		return nil
	case tcell.KeyDown:
		cm.moveSelection(1)
		return nil
	case tcell.KeyEnter, tcell.KeyRune:
		if event.Key() == tcell.KeyEnter {
			cm.toggleSelectedThread()
			return nil
		}
		switch event.Rune() {
		case 'j':
			cm.moveSelection(1)
			return nil
		case 'k':
			cm.moveSelection(-1)
			return nil
		case ' ':
			cm.toggleSelectedThread()
			return nil
		case 'e':
			cm.beginEdit()
			return nil
		case 'd':
			cm.beginDelete()
			return nil
		case 'r':
			cm.beginReactionPicker()
			return nil
		}
	}
	return event
}

func (cm *CommentsModal) handleDeleteConfirmation(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		cm.cancelDelete()
		return nil
	case tcell.KeyEnter:
		cm.confirmSelectedDelete()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'y', 'Y':
			cm.confirmSelectedDelete()
			return nil
		case 'n', 'N', 'c', 'C':
			cm.cancelDelete()
			return nil
		}
	}
	return nil
}

func (cm *CommentsModal) handleEditKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		cm.cancelEdit()
		return nil
	case tcell.KeyEnter:
		// Ctrl/Cmd+Enter is the documented submit shortcut; plain Enter is
		// accepted as well so keyboard-only callers do not get stuck.
		cm.submitEdit()
		return nil
	}
	if cm.editField != nil {
		if handler := cm.editField.InputHandler(); handler != nil {
			handler(event, nil)
			return nil
		}
	}
	return event
}

func (cm *CommentsModal) handleReactionKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		cm.cancelReactionPicker()
		return nil
	case tcell.KeyEnter:
		cm.applyReaction()
		return nil
	case tcell.KeyUp:
		cm.moveReaction(-1)
		return nil
	case tcell.KeyDown:
		cm.moveReaction(1)
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'k':
			cm.moveReaction(-1)
			return nil
		case 'j':
			cm.moveReaction(1)
			return nil
		}
	}
	return nil
}

func (cm *CommentsModal) beginEdit() {
	comment := cm.SelectedComment()
	if comment == nil {
		cm.SetError("No comment selected")
		return
	}
	if !cm.canMutate(comment.Comment) {
		cm.SetError("You can edit only your own comments")
		return
	}
	cm.errorMessage = ""
	cm.editOpen = true
	cm.editField.SetText(comment.Comment.Body, true)
	cm.buildModal()
	cm.updatePage()
	cm.focusEdit()
}

func (cm *CommentsModal) cancelEdit() {
	if cm.actionRunner != nil {
		cm.actionRunner.Cancel()
	}
	cm.editOpen = false
	cm.buildModal()
	cm.updatePage()
	cm.focusList()
}

func (cm *CommentsModal) submitEdit() {
	comment := cm.SelectedComment()
	if comment == nil {
		cm.cancelEdit()
		cm.SetError("No comment selected")
		return
	}
	body := cm.editField.GetText()
	if strings.TrimSpace(body) == "" {
		cm.SetError("Comment body cannot be blank")
		return
	}
	var operation func(context.Context) error
	if callback := cm.callbacks.OnEdit; callback != nil {
		operation = func(context.Context) error { return callback(comment.Comment.ID, body) }
	} else if callback := cm.callbacks.Edit; callback != nil {
		operation = func(context.Context) error { return callback(comment.Comment.ID, body) }
	} else if callback := cm.callbacks.OnEditContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, comment.Comment.ID, body) }
	} else if callback := cm.callbacks.EditContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, comment.Comment.ID, body) }
	}
	if operation == nil {
		cm.SetError("Edit callback unavailable")
		return
	}
	id := comment.Comment.ID
	cm.runMutation("edit comment", id, operation, func() {
		updateCommentBody(cm.comments, id, body)
		cm.editOpen = false
		cm.errorMessage = ""
		cm.progress = ""
		cm.refreshRows()
		cm.buildModal()
		cm.updatePage()
		cm.focusList()
	})
}

func (cm *CommentsModal) beginDelete() {
	comment := cm.SelectedComment()
	if comment == nil {
		cm.SetError("No comment selected")
		return
	}
	if !cm.canMutate(comment.Comment) {
		cm.SetError("You can delete only your own comments")
		return
	}
	cm.pendingDelete = comment.Comment.ID
	cm.confirmDelete = true
	cm.errorMessage = ""
	cm.buildModal()
	cm.updatePage()
}

func (cm *CommentsModal) cancelDelete() {
	cm.confirmDelete = false
	cm.pendingDelete = ""
	cm.buildModal()
	cm.updatePage()
	cm.focusList()
}

func (cm *CommentsModal) confirmSelectedDelete() {
	if !cm.confirmDelete || cm.pendingDelete == "" {
		return
	}
	var operation func(context.Context) error
	if callback := cm.callbacks.OnDelete; callback != nil {
		operation = func(context.Context) error { return callback(cm.pendingDelete) }
	} else if callback := cm.callbacks.Delete; callback != nil {
		operation = func(context.Context) error { return callback(cm.pendingDelete) }
	} else if callback := cm.callbacks.OnDeleteContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, cm.pendingDelete) }
	} else if callback := cm.callbacks.DeleteContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, cm.pendingDelete) }
	}
	if operation == nil {
		cm.SetError("Delete callback unavailable")
		return
	}
	id := cm.pendingDelete
	cm.runMutation("delete comment", id, operation, func() {
		removeCommentFromSlice(&cm.comments, id)
		cm.confirmDelete = false
		cm.pendingDelete = ""
		// Preserve a user move made while the request was in flight. Only clear
		// the highlight when the deleted row is still selected so Reload can pick
		// the nearest surviving row in that case.
		if cm.selectedID == id {
			cm.selectedID = ""
		}
		cm.errorMessage = ""
		cm.progress = ""
		cm.Reload(cm.comments)
		cm.focusList()
	})
}

func (cm *CommentsModal) beginReactionPicker() {
	if cm.SelectedComment() == nil {
		cm.SetError("No comment selected")
		return
	}
	if len(cm.reactionEmojis) == 0 {
		cm.SetError("No reactions available")
		return
	}
	cm.errorMessage = ""
	cm.reactionOpen = true
	cm.reactionIndex = 0
	cm.refreshReactionList()
	cm.buildModal()
	cm.updatePage()
	cm.focusReaction()
}

func (cm *CommentsModal) cancelReactionPicker() {
	cm.reactionOpen = false
	cm.buildModal()
	cm.updatePage()
	cm.focusList()
}

func (cm *CommentsModal) applyReaction() {
	if cm == nil {
		return
	}
	comment := findComment(cm.comments, cm.selectedID)
	if comment == nil || cm.reactionIndex < 0 || cm.reactionIndex >= len(cm.reactionEmojis) {
		cm.cancelReactionPicker()
		return
	}
	emoji := cm.reactionEmojis[cm.reactionIndex]
	var ownReaction *linearapi.Reaction
	for i := range comment.Reactions {
		reaction := &comment.Reactions[i]
		if normalizedCommentReactionEmoji(reaction.Emoji) == emoji && cm.isCurrentUser(reaction.User) {
			ownReaction = reaction
			break
		}
	}
	if ownReaction != nil {
		var operation func(context.Context) error
		if callback := cm.callbacks.OnRemoveReaction; callback != nil {
			operation = func(context.Context) error { return callback(ownReaction.ID) }
		} else if callback := cm.callbacks.RemoveReaction; callback != nil {
			operation = func(context.Context) error { return callback(ownReaction.ID) }
		} else if callback := cm.callbacks.OnRemoveReactionContext; callback != nil {
			operation = func(ctx context.Context) error { return callback(ctx, ownReaction.ID) }
		} else if callback := cm.callbacks.RemoveReactionContext; callback != nil {
			operation = func(ctx context.Context) error { return callback(ctx, ownReaction.ID) }
		}
		if operation == nil {
			cm.SetError("Remove reaction callback unavailable")
			return
		}
		id := ownReaction.ID
		cm.runMutation("remove reaction", comment.Comment.ID, operation, func() {
			removeReactionFromComment(comment, id)
			cm.reactionOpen = false
			cm.errorMessage = ""
			cm.progress = ""
			cm.refreshRows()
			cm.buildModal()
			cm.updatePage()
			cm.focusList()
		})
		return
	}
	var operation func(context.Context) error
	if callback := cm.callbacks.OnAddReaction; callback != nil {
		operation = func(context.Context) error { return callback(comment.Comment.ID, emoji) }
	} else if callback := cm.callbacks.AddReaction; callback != nil {
		operation = func(context.Context) error { return callback(comment.Comment.ID, emoji) }
	} else if callback := cm.callbacks.OnAddReactionContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, comment.Comment.ID, emoji) }
	} else if callback := cm.callbacks.AddReactionContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, comment.Comment.ID, emoji) }
	}
	if operation == nil {
		cm.SetError("Add reaction callback unavailable")
		return
	}
	id := comment.Comment.ID
	cm.runMutation("add reaction", id, operation, func() {
		cm.reactionOpen = false
		cm.errorMessage = ""
		cm.progress = ""
		cm.refreshRows()
		cm.buildModal()
		cm.updatePage()
		cm.focusList()
	})
}

func (cm *CommentsModal) runMutation(name, selectedID string, operation func(context.Context) error, onSuccess func()) {
	if cm == nil || operation == nil || onSuccess == nil {
		return
	}
	if cm.app == nil || cm.actionRunner == nil {
		err := operation(context.Background())
		if err != nil {
			cm.SetError(err.Error())
			return
		}
		onSuccess()
		cm.notifyActionComplete(name, selectedID)
		return
	}
	accepted := cm.actionRunner.Run(name, operation, func(err error) {
		if err != nil || !cm.commentActionStillCurrent(selectedID) {
			return
		}
		onSuccess()
		cm.notifyActionComplete(name, selectedID)
	})
	if !accepted {
		cm.SetError("Another comment action is already in progress")
	}
}

func (cm *CommentsModal) notifyActionComplete(name, commentID string) {
	if cm == nil {
		return
	}
	callback := cm.callbacks.OnActionComplete
	if callback == nil {
		callback = cm.callbacks.ActionComplete
	}
	if callback != nil {
		callback(name, commentID)
	}
}

func (cm *CommentsModal) commentActionStillCurrent(_ string) bool {
	if cm == nil {
		return false
	}
	if cm.app == nil {
		return true
	}
	if selected := cm.app.GetSelectedIssue(); selected != nil && selected.ID != cm.issueID {
		return false
	}
	return cm.app.pages == nil || cm.app.pages.HasPage(commentsModalPageName)
}

func (cm *CommentsModal) toggleSelectedThread() {
	row := cm.selectedRow()
	if row == nil || !row.HasReplies {
		return
	}
	cm.expanded[row.ID] = !cm.expanded[row.ID]
	cm.refreshRows()
}

func (cm *CommentsModal) moveSelection(delta int) {
	if cm == nil || len(cm.rows) == 0 {
		return
	}
	index := cm.list.GetCurrentItem() + delta
	if index < 0 {
		index = 0
	}
	if index >= len(cm.rows) {
		index = len(cm.rows) - 1
	}
	cm.list.SetCurrentItem(index)
	cm.selectedID = cm.rows[index].ID
}

func (cm *CommentsModal) moveReaction(delta int) {
	if len(cm.reactionEmojis) == 0 {
		return
	}
	cm.reactionIndex += delta
	if cm.reactionIndex < 0 {
		cm.reactionIndex = 0
	}
	if cm.reactionIndex >= len(cm.reactionEmojis) {
		cm.reactionIndex = len(cm.reactionEmojis) - 1
	}
	cm.reactionList.SetCurrentItem(cm.reactionIndex)
}

func (cm *CommentsModal) selectedRow() *commentsModalRow {
	if cm == nil || len(cm.rows) == 0 {
		return nil
	}
	index := cm.list.GetCurrentItem()
	if index < 0 || index >= len(cm.rows) {
		return nil
	}
	return &cm.rows[index]
}

func (cm *CommentsModal) canMutate(comment linearapi.Comment) bool {
	if cm.currentUserID != "" {
		return comment.Author.ID == cm.currentUserID || comment.Author.IsMe
	}
	return comment.Author.IsMe
}

func (cm *CommentsModal) isCurrentUser(user linearapi.User) bool {
	if cm.currentUserID != "" {
		return user.ID == cm.currentUserID || user.IsMe
	}
	return user.IsMe
}

// SelectedComment returns a copy of the selected comment thread, or nil for
// an empty state. The copy prevents callers from mutating modal state.
func (cm *CommentsModal) SelectedComment() *CommentThread {
	if cm == nil || cm.selectedID == "" {
		return nil
	}
	thread := findComment(cm.comments, cm.selectedID)
	if thread == nil {
		return nil
	}
	threadCopy := cloneCommentThread(*thread)
	return &threadCopy
}

// VisibleComments returns the currently rendered rows in navigation order.
func (cm *CommentsModal) VisibleComments() []CommentThread {
	if cm == nil || len(cm.rows) == 0 {
		return nil
	}
	visible := make([]CommentThread, 0, len(cm.rows))
	for _, row := range cm.rows {
		visible = append(visible, CommentThread{
			Comment:   row.Comment,
			ParentID:  row.ParentID,
			Reactions: cloneReactions(row.Reactions),
		})
	}
	return visible
}

// SelectedCommentID returns the stable ID used to preserve selection across
// Reload calls.
func (cm *CommentsModal) SelectedCommentID() string {
	if cm == nil {
		return ""
	}
	return cm.selectedID
}

// IsEditing reports whether the edit body is active.
func (cm *CommentsModal) IsEditing() bool { return cm != nil && cm.editOpen }

// IsDeleteConfirmationOpen reports whether delete confirmation is active.
func (cm *CommentsModal) IsDeleteConfirmationOpen() bool {
	return cm != nil && cm.confirmDelete
}

// IsReactionPickerOpen reports whether the reaction picker is active.
func (cm *CommentsModal) IsReactionPickerOpen() bool {
	return cm != nil && cm.reactionOpen
}

// EditField returns the active edit text area for callers that provide their
// own text-input routing or tests that set the draft directly.
func (cm *CommentsModal) EditField() *tview.TextArea {
	if cm == nil {
		return nil
	}
	return cm.editField
}

// GetModal returns the shell primitive for adding to pages.
func (cm *CommentsModal) GetModal() *tview.Flex {
	if cm == nil {
		return nil
	}
	return cm.modal
}

// HelpText returns the current contextual shortcut string.
func (cm *CommentsModal) HelpText() string {
	if cm == nil {
		return ""
	}
	return cm.helpText()
}

// SetLoading updates loading state and the status line without using timers.
func (cm *CommentsModal) SetLoading(loading bool) {
	if cm == nil {
		return
	}
	cm.loading = loading
	cm.updateStatus()
	cm.updatePage()
}

// Loading reports current loading state.
func (cm *CommentsModal) Loading() bool { return cm != nil && cm.loading }

// SetError records and displays an error message. Pass an empty string to
// clear it.
func (cm *CommentsModal) SetError(message string) {
	if cm == nil {
		return
	}
	cm.errorMessage = strings.TrimSpace(message)
	if cm.errorMessage != "" {
		cm.loading = false
	}
	cm.updateStatus()
	cm.updatePage()
}

// ErrorMessage returns the current error text.
func (cm *CommentsModal) ErrorMessage() string {
	if cm == nil {
		return ""
	}
	return cm.errorMessage
}

// SetProgress records a caller-owned loading/progress description.
func (cm *CommentsModal) SetProgress(progress string) {
	if cm == nil {
		return
	}
	cm.progress = strings.TrimSpace(progress)
	cm.updateStatus()
	cm.updatePage()
}

// ProgressMessage returns the current progress text.
func (cm *CommentsModal) ProgressMessage() string {
	if cm == nil {
		return ""
	}
	return cm.progress
}

// StatusText returns the status line currently shown by the modal.
func (cm *CommentsModal) StatusText() string {
	if cm == nil {
		return ""
	}
	return cm.statusText()
}

// EmptyStateText returns the defensive empty-state label used by the list.
func (cm *CommentsModal) EmptyStateText() string {
	if cm == nil || len(cm.rows) == 0 {
		return "No comments or activity"
	}
	return ""
}

func (cm *CommentsModal) showPage() {
	if cm == nil || cm.app == nil || cm.app.pages == nil {
		return
	}
	cm.app.pages.AddPage(commentsModalPageName, cm.modal, true, true)
	cm.app.pages.SendToFront(commentsModalPageName)
}

func (cm *CommentsModal) updatePage() {
	if cm == nil || cm.app == nil || cm.app.pages == nil {
		return
	}
	if cm.app.pages.HasPage(commentsModalPageName) {
		cm.app.pages.AddPage(commentsModalPageName, cm.modal, true, true)
		cm.app.pages.SendToFront(commentsModalPageName)
	}
}

func (cm *CommentsModal) buildModal() {
	if cm == nil {
		return
	}
	cm.ensureWidgets()
	theme := cm.theme()
	density := cm.density()

	cm.header.SetText("Comments / Activity")
	cm.header.SetTextColor(theme.Accent)
	cm.header.SetBackgroundColor(theme.HeaderBg)
	cm.status.SetTextColor(theme.SecondaryText)
	cm.status.SetBackgroundColor(theme.HeaderBg)
	cm.help.SetTextColor(theme.SecondaryText)
	cm.help.SetBackgroundColor(theme.HeaderBg)
	cm.list.SetBackgroundColor(theme.HeaderBg)
	cm.editField.SetBackgroundColor(theme.InputBg)
	cm.reactionList.SetBackgroundColor(theme.HeaderBg)
	cm.refreshReactionList()

	content := tview.NewFlex().SetDirection(tview.FlexRow)
	content.AddItem(cm.header, 1, 0, false)
	content.AddItem(cm.status, 1, 0, false)
	if cm.editOpen {
		content.AddItem(cm.editField, 5, 0, true)
	}
	if cm.reactionOpen {
		content.AddItem(cm.reactionList, 6, 0, true)
	}
	content.AddItem(cm.list, 0, 1, true)
	content.AddItem(cm.help, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(theme.Accent).
		SetTitle(" Comments / Activity ").
		SetTitleColor(theme.Foreground)
	content.SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)

	const modalWidth = 96
	const modalHeight = 24
	inner := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, modalHeight, 0, true).
		AddItem(nil, 0, 1, false)
	cm.modal = newResponsiveWorkspaceModal(theme.Background, inner, modalWidth, nil)
	cm.updateStatus()
	cm.updateHelp()
	cm.refreshRows()
	cm.List = cm.list
	cm.Header = cm.header
	cm.Status = cm.status
	cm.Help = cm.help
	cm.EditArea = cm.editField
}

func (cm *CommentsModal) refreshRows() {
	if cm == nil {
		return
	}
	cm.ensureWidgets()
	rows := make([]commentsModalRow, 0)
	for _, thread := range cm.comments {
		cm.appendRows(&rows, thread, 0, "")
	}
	cm.rows = rows
	cm.list.Clear()
	if len(rows) == 0 {
		cm.list.AddItem(cm.EmptyStateText(), "", 0, nil)
		cm.list.SetCurrentItem(0)
		cm.selectedID = ""
		cm.updateHelp()
		return
	}
	selectedIndex := 0
	for index, row := range rows {
		if row.ID == cm.selectedID {
			selectedIndex = index
			break
		}
	}
	for _, row := range rows {
		cm.list.AddItem(formatCommentRow(row, cm.expanded[row.ID]), formatCommentSecondary(row), 0, nil)
	}
	cm.list.SetCurrentItem(selectedIndex)
	cm.selectedID = rows[selectedIndex].ID
	cm.updateHelp()
}

func (cm *CommentsModal) appendRows(rows *[]commentsModalRow, thread CommentThread, depth int, parentID string) {
	if strings.TrimSpace(thread.Comment.ID) == "" {
		return
	}
	if thread.ParentID != "" {
		parentID = thread.ParentID
	}
	*rows = append(*rows, commentsModalRow{
		ID:         thread.Comment.ID,
		ParentID:   parentID,
		Depth:      depth,
		Comment:    thread.Comment,
		Reactions:  cloneReactions(thread.Reactions),
		HasReplies: len(thread.Replies) > 0,
	})
	if len(thread.Replies) == 0 || !cm.expanded[thread.Comment.ID] {
		return
	}
	for _, reply := range thread.Replies {
		cm.appendRows(rows, reply, depth+1, thread.Comment.ID)
	}
}

func formatCommentRow(row commentsModalRow, expanded bool) string {
	prefix := strings.Repeat("  ", row.Depth)
	threadMarker := "  "
	if row.HasReplies {
		if expanded {
			threadMarker = "▾ "
		} else {
			threadMarker = "▸ "
		}
	}
	author := row.Comment.Author.DisplayName
	if author == "" {
		author = row.Comment.Author.Name
	}
	if author == "" {
		author = row.Comment.Author.ID
	}
	if author == "" {
		author = "Unknown"
	}
	return prefix + threadMarker + author + "  " + formatCommentTime(row.Comment.CreatedAt)
}

func formatCommentSecondary(row commentsModalRow) string {
	body := strings.TrimSpace(strings.ReplaceAll(row.Comment.Body, "\n", " "))
	if body == "" {
		body = "(empty comment)"
	}
	if len(row.Reactions) == 0 {
		return body
	}
	reactions := make([]string, 0, len(row.Reactions))
	for _, reaction := range row.Reactions {
		if reaction.Emoji != "" {
			reactions = append(reactions, normalizedCommentReactionEmoji(reaction.Emoji))
		}
	}
	if len(reactions) == 0 {
		return body
	}
	return body + "  " + strings.Join(reactions, " ")
}

func formatCommentTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04")
}

func (cm *CommentsModal) refreshReactionList() {
	if cm == nil || cm.reactionList == nil {
		return
	}
	cm.reactionList.Clear()
	for _, emoji := range cm.reactionEmojis {
		cm.reactionList.AddItem(emoji, "", 0, nil)
	}
	if len(cm.reactionEmojis) > 0 {
		if cm.reactionIndex < 0 || cm.reactionIndex >= len(cm.reactionEmojis) {
			cm.reactionIndex = 0
		}
		cm.reactionList.SetCurrentItem(cm.reactionIndex)
	}
}

func (cm *CommentsModal) updateStatus() {
	if cm == nil || cm.status == nil {
		return
	}
	cm.status.SetText(cm.statusText())
}

func (cm *CommentsModal) statusText() string {
	if cm.loading {
		return "Loading…"
	}
	if cm.errorMessage != "" {
		return "Error: " + cm.errorMessage
	}
	if cm.progress != "" {
		return cm.progress
	}
	return ""
}

func (cm *CommentsModal) updateHelp() {
	if cm == nil || cm.help == nil {
		return
	}
	cm.help.SetText(cm.helpText())
	cm.help.SetTextAlign(tview.AlignCenter)
}

func (cm *CommentsModal) helpText() string {
	switch {
	case cm.confirmDelete:
		return "Enter/Y: delete | Esc/N: cancel"
	case cm.editOpen:
		return "Ctrl+Enter / Enter: save edit | Esc: cancel"
	case cm.reactionOpen:
		return "j/k or ↑/↓: choose reaction | Enter: apply | Esc: cancel"
	default:
		return "j/k or ↑/↓: navigate | Enter/Space: thread | e: edit | d: delete | r: reaction | Esc: close"
	}
}

func (cm *CommentsModal) focusList() {
	if cm == nil || cm.app == nil || cm.app.app == nil || cm.list == nil {
		return
	}
	cm.app.app.SetFocus(cm.list)
}

func (cm *CommentsModal) focusEdit() {
	if cm == nil || cm.app == nil || cm.app.app == nil || cm.editField == nil {
		return
	}
	cm.app.app.SetFocus(cm.editField)
}

func (cm *CommentsModal) focusReaction() {
	if cm == nil || cm.app == nil || cm.app.app == nil || cm.reactionList == nil {
		return
	}
	cm.app.app.SetFocus(cm.reactionList)
}

func cloneNonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func cloneCommentThreads(threads []CommentThread) []CommentThread {
	if len(threads) == 0 {
		return nil
	}
	result := make([]CommentThread, len(threads))
	for i, thread := range threads {
		result[i] = cloneCommentThread(thread)
	}
	return result
}

func cloneCommentThread(thread CommentThread) CommentThread {
	thread.Replies = cloneCommentThreads(thread.Replies)
	thread.Reactions = cloneReactions(thread.Reactions)
	return thread
}

func cloneReactions(reactions []linearapi.Reaction) []linearapi.Reaction {
	if len(reactions) == 0 {
		return nil
	}
	return append([]linearapi.Reaction(nil), reactions...)
}

func copyExpandedCommentState(thread CommentThread, previous, current map[string]bool) {
	if previous[thread.Comment.ID] || thread.Expanded {
		current[thread.Comment.ID] = true
	}
	for _, reply := range thread.Replies {
		copyExpandedCommentState(reply, previous, current)
	}
}

func containsCommentID(threads []CommentThread, id string) bool {
	for _, thread := range threads {
		if thread.Comment.ID == id || containsCommentID(thread.Replies, id) {
			return true
		}
	}
	return false
}

func firstCommentID(threads []CommentThread) string {
	for _, thread := range threads {
		if strings.TrimSpace(thread.Comment.ID) != "" {
			return thread.Comment.ID
		}
		if id := firstCommentID(thread.Replies); id != "" {
			return id
		}
	}
	return ""
}

func ensureCommentParentsExpanded(threads []CommentThread, id string, expanded map[string]bool) bool {
	for _, thread := range threads {
		if thread.Comment.ID == id {
			return true
		}
		if ensureCommentParentsExpanded(thread.Replies, id, expanded) {
			expanded[thread.Comment.ID] = true
			return true
		}
	}
	return false
}

func findComment(threads []CommentThread, id string) *CommentThread {
	for i := range threads {
		if threads[i].Comment.ID == id {
			return &threads[i]
		}
		if found := findComment(threads[i].Replies, id); found != nil {
			return found
		}
	}
	return nil
}

func updateCommentBody(threads []CommentThread, id, body string) bool {
	for i := range threads {
		if threads[i].Comment.ID == id {
			threads[i].Comment.Body = body
			threads[i].Comment.UpdatedAt = time.Now()
			return true
		}
		if updateCommentBody(threads[i].Replies, id, body) {
			return true
		}
	}
	return false
}

func removeCommentFromSlice(threads *[]CommentThread, id string) bool {
	if threads == nil {
		return false
	}
	for i := range *threads {
		if (*threads)[i].Comment.ID == id {
			*threads = append((*threads)[:i], (*threads)[i+1:]...)
			return true
		}
		if removeCommentFromSlice(&(*threads)[i].Replies, id) {
			return true
		}
	}
	return false
}

func removeReactionFromComment(comment *CommentThread, id string) bool {
	if comment == nil {
		return false
	}
	for index := range comment.Reactions {
		if comment.Reactions[index].ID == id {
			comment.Reactions = append(comment.Reactions[:index], comment.Reactions[index+1:]...)
			return true
		}
	}
	return false
}
