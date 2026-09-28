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

const inboxViewPageName = "inbox_view"

// InboxViewOptions contains the notification snapshot and optional snooze
// choices supplied by the caller. SnoozeChoices are used when no explicit
// OnChooseSnooze callback is installed.
type InboxViewOptions struct {
	Notifications []linearapi.Notification
	SelectedID    string
	SnoozeChoices []time.Time
}

// InboxViewCallbacks own all network work for the view. The view updates its
// local snapshot only after a callback succeeds, so callers can reload from
// the server whenever their operation changes more than one field.
type InboxViewCallbacks struct {
	OnOpenIssue         func(issueID string)
	OnToggleRead        func(notificationID string, read bool) error
	OnToggleReadContext func(ctx context.Context, notificationID string, read bool) error
	OnMarkRead          func(notificationID string) error
	OnMarkReadContext   func(ctx context.Context, notificationID string) error
	OnMarkUnread        func(notificationID string) error
	OnMarkUnreadContext func(ctx context.Context, notificationID string) error
	OnChooseSnooze      func(notificationID string) (time.Time, error)
	OnSnooze            func(notificationID string, until time.Time) error
	OnSnoozeContext     func(ctx context.Context, notificationID string, until time.Time) error
	OnUnsnooze          func(notificationID string) error
	OnUnsnoozeContext   func(ctx context.Context, notificationID string) error
	OnArchive           func(notificationID string) error
	OnArchiveContext    func(ctx context.Context, notificationID string) error
	OnRefresh           func() error
	OnRefreshContext    func(ctx context.Context) error
	OnActionComplete    func(name, notificationID string)
	OnClose             func()

	OpenIssue         func(issueID string)
	ToggleRead        func(notificationID string, read bool) error
	ToggleReadContext func(ctx context.Context, notificationID string, read bool) error
	MarkRead          func(notificationID string) error
	MarkReadContext   func(ctx context.Context, notificationID string) error
	MarkUnread        func(notificationID string) error
	MarkUnreadContext func(ctx context.Context, notificationID string) error
	ChooseSnooze      func(notificationID string) (time.Time, error)
	Snooze            func(notificationID string, until time.Time) error
	SnoozeContext     func(ctx context.Context, notificationID string, until time.Time) error
	Unsnooze          func(notificationID string) error
	UnsnoozeContext   func(ctx context.Context, notificationID string) error
	Archive           func(notificationID string) error
	ArchiveContext    func(ctx context.Context, notificationID string) error
	Refresh           func() error
	RefreshContext    func(ctx context.Context) error
	ActionComplete    func(name, notificationID string)
	Close             func()
}

// InboxView is a reusable, presentation-only notifications inbox. It does
// not call Linear directly; injected callbacks own mutations and refreshes.
type InboxView struct {
	app    *App
	modal  *tview.Flex
	list   *tview.List
	header *tview.TextView
	status *tview.TextView
	help   *tview.TextView

	List   *tview.List
	Header *tview.TextView
	Status *tview.TextView
	Help   *tview.TextView

	notifications  []linearapi.Notification
	selectedID     string
	snoozeChoices  []time.Time
	callbacks      InboxViewCallbacks
	actionRunner   *AsyncViewActionRunner
	loading        bool
	errorMessage   string
	progress       string
	confirmArchive bool
	pendingArchive string
}

// NotificationsView is an alias for callers that use the API's resource
// name rather than the product's Inbox label.
type NotificationsView = InboxView
type NotificationsViewOptions = InboxViewOptions
type NotificationsViewCallbacks = InboxViewCallbacks

// NewNotificationsView is an alias constructor for NewInboxView.
func NewNotificationsView(app *App) *InboxView { return NewInboxView(app) }

// NewInboxView creates an inbox view. A nil App is supported for embedding
// and deterministic tests.
func NewInboxView(app *App) *InboxView {
	view := &InboxView{app: app}
	if app != nil {
		view.actionRunner = NewAsyncViewActionRunner(app.QueueUpdateDraw, AsyncViewActionHooks{
			OnStart: func(name string) {
				// Run is accepted by the UI key handler, so this hook runs on the
				// UI goroutine. Worker-side completion hooks below are dispatched
				// through QueueUpdateDraw before touching view state.
				view.errorMessage = ""
				view.progress = name + "…"
				view.SetLoading(true)
			},
			OnProgress: func(_, message string) {
				view.progress = strings.TrimSpace(message)
				view.updateStatus()
				view.updatePage()
			},
			OnError: func(_ string, err error) {
				if err != nil {
					view.SetError(err.Error())
				}
			},
			OnComplete: func(_ string, _ error) {
				view.SetLoading(false)
			},
		})
	}
	view.ensureWidgets()
	view.buildModal()
	return view
}

func (iv *InboxView) ensureWidgets() {
	if iv == nil {
		return
	}
	theme := iv.theme()
	if iv.list == nil {
		iv.list = tview.NewList().
			ShowSecondaryText(true).
			SetMainTextColor(theme.Foreground).
			SetSecondaryTextColor(theme.SecondaryText).
			SetSelectedBackgroundColor(theme.Accent).
			SetSelectedTextColor(theme.SelectionText).
			SetHighlightFullLine(true)
		iv.list.SetBackgroundColor(theme.HeaderBg)
	}
	if iv.header == nil {
		iv.header = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	if iv.status == nil {
		iv.status = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	if iv.help == nil {
		iv.help = tview.NewTextView().SetWrap(false).SetWordWrap(false)
	}
	iv.List, iv.Header, iv.Status, iv.Help = iv.list, iv.header, iv.status, iv.help
}

func (iv *InboxView) theme() Theme {
	if iv != nil && iv.app != nil {
		return iv.app.theme
	}
	return LinearTheme
}

func (iv *InboxView) density() DensityProfile {
	if iv != nil && iv.app != nil {
		return iv.app.density
	}
	return ResolveDensity(config.DefaultDensity)
}

// Show replaces the snapshot, resets transient state, and presents the view.
func (iv *InboxView) Show(options InboxViewOptions, callbacks InboxViewCallbacks) {
	if iv == nil {
		return
	}
	if iv.actionRunner != nil {
		iv.actionRunner.Invalidate()
	}
	iv.callbacks = callbacks
	iv.selectedID = strings.TrimSpace(options.SelectedID)
	iv.snoozeChoices = cloneInboxTimes(options.SnoozeChoices)
	iv.confirmArchive = false
	iv.pendingArchive = ""
	iv.loading = false
	iv.errorMessage = ""
	iv.progress = ""
	iv.Reload(options.Notifications)
	iv.showPage()
	iv.focusList()
}

// Reload replaces notifications while preserving selection by stable ID.
func (iv *InboxView) Reload(notifications []linearapi.Notification) {
	if iv == nil {
		return
	}
	iv.notifications = cloneNotifications(notifications)
	if iv.selectedID == "" || findNotification(iv.notifications, iv.selectedID) == nil {
		iv.selectedID = firstNotificationID(iv.notifications)
	}
	iv.refreshList()
	iv.updatePage()
}

// SetNotifications is an alias for Reload.
func (iv *InboxView) SetNotifications(notifications []linearapi.Notification) {
	iv.Reload(notifications)
}

// Hide closes the page and clears close callbacks after invoking them once.
func (iv *InboxView) Hide() {
	if iv == nil {
		return
	}
	if iv.actionRunner != nil {
		iv.actionRunner.Invalidate()
	}
	iv.confirmArchive = false
	iv.pendingArchive = ""
	if iv.app != nil && iv.app.pages != nil {
		iv.app.pages.RemovePage(inboxViewPageName)
		if iv.app.app != nil {
			iv.app.updateFocus()
		}
	}
	callback := iv.callbacks.OnClose
	if callback == nil {
		callback = iv.callbacks.Close
	}
	iv.callbacks.OnClose, iv.callbacks.Close = nil, nil
	if callback != nil {
		callback()
	}
}

// HandleKey implements navigation and inbox actions.
func (iv *InboxView) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if iv == nil || event == nil {
		return event
	}
	if iv.confirmArchive {
		return iv.handleArchiveConfirmation(event)
	}
	switch event.Key() {
	case tcell.KeyEscape:
		iv.Hide()
		return nil
	case tcell.KeyUp:
		iv.moveSelection(-1)
		return nil
	case tcell.KeyDown:
		iv.moveSelection(1)
		return nil
	case tcell.KeyEnter:
		iv.openSelectedIssue()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'j':
			iv.moveSelection(1)
			return nil
		case 'k':
			iv.moveSelection(-1)
			return nil
		case 'u':
			iv.toggleRead()
			return nil
		case 's':
			iv.snoozeSelected()
			return nil
		case 'a':
			iv.beginArchive()
			return nil
		case 'r':
			iv.refresh()
			return nil
		}
	}
	return event
}

func (iv *InboxView) handleArchiveConfirmation(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		iv.cancelArchive()
		return nil
	case tcell.KeyEnter:
		iv.confirmSelectedArchive()
		return nil
	case tcell.KeyRune:
		switch event.Rune() {
		case 'y', 'Y':
			iv.confirmSelectedArchive()
			return nil
		case 'n', 'N', 'c', 'C':
			iv.cancelArchive()
			return nil
		}
	}
	return nil
}

func (iv *InboxView) openSelectedIssue() {
	notification := iv.SelectedNotification()
	if notification == nil {
		iv.SetError("No notification selected")
		return
	}
	if notification.Issue == nil || strings.TrimSpace(notification.Issue.ID) == "" {
		iv.SetError("Selected notification has no related issue")
		return
	}
	callback := iv.callbacks.OnOpenIssue
	if callback == nil {
		callback = iv.callbacks.OpenIssue
	}
	if callback == nil {
		iv.SetError("Open issue callback unavailable")
		return
	}
	callback(notification.Issue.ID)
}

func (iv *InboxView) toggleRead() {
	notification := iv.selectedNotificationMutable()
	if notification == nil {
		iv.SetError("No notification selected")
		return
	}
	targetRead := !notification.Read
	var operation func(context.Context) error
	if callback := iv.callbacks.OnToggleReadContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, notification.ID, targetRead) }
	} else if callback := iv.callbacks.ToggleReadContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, notification.ID, targetRead) }
	} else if callback := iv.callbacks.OnToggleRead; callback != nil {
		operation = func(context.Context) error { return callback(notification.ID, targetRead) }
	} else if callback := iv.callbacks.ToggleRead; callback != nil {
		operation = func(context.Context) error { return callback(notification.ID, targetRead) }
	}
	if operation == nil {
		if targetRead {
			if callback := iv.callbacks.OnMarkReadContext; callback != nil {
				operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
			} else if callback := iv.callbacks.MarkReadContext; callback != nil {
				operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
			} else if callback := iv.callbacks.OnMarkRead; callback != nil {
				operation = func(context.Context) error { return callback(notification.ID) }
			} else if callback := iv.callbacks.MarkRead; callback != nil {
				operation = func(context.Context) error { return callback(notification.ID) }
			}
		} else {
			if callback := iv.callbacks.OnMarkUnreadContext; callback != nil {
				operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
			} else if callback := iv.callbacks.MarkUnreadContext; callback != nil {
				operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
			} else if callback := iv.callbacks.OnMarkUnread; callback != nil {
				operation = func(context.Context) error { return callback(notification.ID) }
			} else if callback := iv.callbacks.MarkUnread; callback != nil {
				operation = func(context.Context) error { return callback(notification.ID) }
			}
		}
	}
	if operation == nil {
		iv.SetError("Read state callback unavailable")
		return
	}
	id := notification.ID
	iv.runMutation("toggle read", id, operation, func() {
		notification := findNotification(iv.notifications, id)
		if notification == nil {
			return
		}
		notification.Read = targetRead
		if targetRead {
			now := time.Now()
			notification.ReadAt = &now
		} else {
			notification.ReadAt = nil
		}
		iv.errorMessage = ""
		iv.progress = ""
		iv.refreshList()
		iv.notifyActionComplete("toggle read", id)
	})
}

func (iv *InboxView) snoozeSelected() {
	notification := iv.selectedNotificationMutable()
	if notification == nil {
		iv.SetError("No notification selected")
		return
	}
	if notification.Snoozed {
		var operation func(context.Context) error
		if callback := iv.callbacks.OnUnsnoozeContext; callback != nil {
			operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
		} else if callback := iv.callbacks.UnsnoozeContext; callback != nil {
			operation = func(ctx context.Context) error { return callback(ctx, notification.ID) }
		} else if callback := iv.callbacks.OnUnsnooze; callback != nil {
			operation = func(context.Context) error { return callback(notification.ID) }
		} else if callback := iv.callbacks.Unsnooze; callback != nil {
			operation = func(context.Context) error { return callback(notification.ID) }
		}
		if operation == nil {
			iv.SetError("Unsnooze callback unavailable")
			return
		}
		id := notification.ID
		iv.runMutation("unsnooze", id, operation, func() {
			currentNotification := findNotification(iv.notifications, id)
			if currentNotification == nil {
				return
			}
			currentNotification.Snoozed = false
			currentNotification.SnoozedUntilAt = nil
			iv.errorMessage = ""
			iv.progress = ""
			iv.refreshList()
			iv.notifyActionComplete("unsnooze", id)
		})
		return
	}
	choice := iv.callbacks.OnChooseSnooze
	if choice == nil {
		choice = iv.callbacks.ChooseSnooze
	}
	var until time.Time
	var err error
	switch {
	case choice != nil:
		until, err = choice(notification.ID)
	case len(iv.snoozeChoices) > 0:
		until = iv.snoozeChoices[0]
	default:
		iv.SetError("Snooze choice unavailable")
		return
	}
	if err != nil {
		iv.SetError(err.Error())
		return
	}
	if until.IsZero() {
		iv.SetError("Snooze time must not be zero")
		return
	}
	var operation func(context.Context) error
	if callback := iv.callbacks.OnSnoozeContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, notification.ID, until) }
	} else if callback := iv.callbacks.SnoozeContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, notification.ID, until) }
	} else if callback := iv.callbacks.OnSnooze; callback != nil {
		operation = func(context.Context) error { return callback(notification.ID, until) }
	} else if callback := iv.callbacks.Snooze; callback != nil {
		operation = func(context.Context) error { return callback(notification.ID, until) }
	}
	if operation != nil {
		id := notification.ID
		iv.runMutation("snooze", id, operation, func() {
			currentNotification := findNotification(iv.notifications, id)
			if currentNotification == nil {
				return
			}
			currentNotification.Snoozed = true
			currentNotification.SnoozedUntilAt = &until
			iv.errorMessage = ""
			iv.progress = ""
			iv.refreshList()
			iv.notifyActionComplete("snooze", id)
		})
		return
	}
	notification.Snoozed = true
	notification.SnoozedUntilAt = &until
	iv.errorMessage = ""
	iv.refreshList()
}

func (iv *InboxView) beginArchive() {
	if iv.SelectedNotification() == nil {
		iv.SetError("No notification selected")
		return
	}
	iv.confirmArchive = true
	iv.pendingArchive = iv.selectedID
	iv.errorMessage = ""
	iv.buildModal()
	iv.updatePage()
}

func (iv *InboxView) cancelArchive() {
	iv.confirmArchive = false
	iv.pendingArchive = ""
	iv.buildModal()
	iv.updatePage()
	iv.focusList()
}

func (iv *InboxView) confirmSelectedArchive() {
	if !iv.confirmArchive || iv.pendingArchive == "" {
		return
	}
	var operation func(context.Context) error
	if callback := iv.callbacks.OnArchiveContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, iv.pendingArchive) }
	} else if callback := iv.callbacks.ArchiveContext; callback != nil {
		operation = func(ctx context.Context) error { return callback(ctx, iv.pendingArchive) }
	} else if callback := iv.callbacks.OnArchive; callback != nil {
		operation = func(context.Context) error { return callback(iv.pendingArchive) }
	} else if callback := iv.callbacks.Archive; callback != nil {
		operation = func(context.Context) error { return callback(iv.pendingArchive) }
	}
	if operation == nil {
		iv.SetError("Archive callback unavailable")
		return
	}
	id := iv.pendingArchive
	iv.runMutation("archive", id, operation, func() {
		removeNotification(&iv.notifications, id)
		iv.confirmArchive = false
		iv.pendingArchive = ""
		// Keep a stable highlight if the user moved while the archive request
		// was in flight; otherwise let Reload choose the next surviving row.
		if iv.selectedID == id {
			iv.selectedID = ""
		}
		iv.errorMessage = ""
		iv.progress = ""
		iv.Reload(iv.notifications)
		iv.focusList()
		iv.notifyActionComplete("archive", id)
	})
}

func (iv *InboxView) refresh() {
	var operation func(context.Context) error
	if callback := iv.callbacks.OnRefreshContext; callback != nil {
		operation = callback
	} else if callback := iv.callbacks.RefreshContext; callback != nil {
		operation = callback
	} else if callback := iv.callbacks.OnRefresh; callback != nil {
		operation = func(context.Context) error { return callback() }
	} else if callback := iv.callbacks.Refresh; callback != nil {
		operation = func(context.Context) error { return callback() }
	}
	if operation == nil {
		iv.SetError("Refresh callback unavailable")
		return
	}
	id := iv.selectedID
	iv.runMutation("refresh", id, operation, func() {
		iv.errorMessage = ""
		iv.progress = ""
		iv.updateStatus()
		iv.notifyActionComplete("refresh", id)
	})
}

func (iv *InboxView) notifyActionComplete(name, notificationID string) {
	if iv == nil {
		return
	}
	callback := iv.callbacks.OnActionComplete
	if callback == nil {
		callback = iv.callbacks.ActionComplete
	}
	if callback != nil {
		callback(name, notificationID)
	}
}

func (iv *InboxView) runMutation(name, selectedID string, operation func(context.Context) error, onSuccess func()) {
	if iv == nil || operation == nil || onSuccess == nil {
		return
	}
	if iv.app == nil || iv.actionRunner == nil {
		err := operation(context.Background())
		if err != nil {
			iv.SetError(err.Error())
			return
		}
		onSuccess()
		return
	}
	accepted := iv.actionRunner.Run(name, operation, func(err error) {
		if err != nil || !iv.notificationActionStillCurrent(selectedID) {
			return
		}
		onSuccess()
	})
	if !accepted {
		iv.SetError("Another Inbox action is already in progress")
	}
}

func (iv *InboxView) notificationActionStillCurrent(_ string) bool {
	if iv == nil {
		return false
	}
	return iv.app == nil || iv.app.pages == nil || iv.app.pages.HasPage(inboxViewPageName)
}

func (iv *InboxView) moveSelection(delta int) {
	if iv == nil || len(iv.notifications) == 0 {
		return
	}
	index := iv.list.GetCurrentItem() + delta
	if index < 0 {
		index = 0
	}
	if index >= len(iv.notifications) {
		index = len(iv.notifications) - 1
	}
	iv.list.SetCurrentItem(index)
	iv.selectedID = iv.notifications[index].ID
}

func (iv *InboxView) selectedNotificationMutable() *linearapi.Notification {
	if iv == nil {
		return nil
	}
	return findNotification(iv.notifications, iv.selectedID)
}

// SelectedNotification returns a defensive copy of the current notification.
func (iv *InboxView) SelectedNotification() *linearapi.Notification {
	notification := iv.selectedNotificationMutable()
	if notification == nil {
		return nil
	}
	selectedCopy := cloneNotification(*notification)
	return &selectedCopy
}

// VisibleNotifications returns the current list snapshot in display order.
func (iv *InboxView) VisibleNotifications() []linearapi.Notification {
	if iv == nil || len(iv.notifications) == 0 {
		return nil
	}
	result := make([]linearapi.Notification, len(iv.notifications))
	for i, notification := range iv.notifications {
		result[i] = cloneNotification(notification)
	}
	return result
}

// SelectedNotificationID returns the stable selection key.
func (iv *InboxView) SelectedNotificationID() string {
	if iv == nil {
		return ""
	}
	return iv.selectedID
}

// IsArchiveConfirmationOpen reports archive confirmation state.
func (iv *InboxView) IsArchiveConfirmationOpen() bool {
	return iv != nil && iv.confirmArchive
}

// GetModal returns the shell primitive for adding to pages.
func (iv *InboxView) GetModal() *tview.Flex {
	if iv == nil {
		return nil
	}
	return iv.modal
}

// HelpText returns the contextual shortcut line.
func (iv *InboxView) HelpText() string {
	if iv == nil {
		return ""
	}
	return iv.helpText()
}

// SetLoading updates loading state without timers.
func (iv *InboxView) SetLoading(loading bool) {
	if iv == nil {
		return
	}
	iv.loading = loading
	iv.updateStatus()
	iv.updatePage()
}

func (iv *InboxView) Loading() bool { return iv != nil && iv.loading }

// SetError records an error and makes it visible in the status line.
func (iv *InboxView) SetError(message string) {
	if iv == nil {
		return
	}
	iv.errorMessage = strings.TrimSpace(message)
	if iv.errorMessage != "" {
		iv.loading = false
	}
	iv.updateStatus()
	iv.updatePage()
}

func (iv *InboxView) ErrorMessage() string {
	if iv == nil {
		return ""
	}
	return iv.errorMessage
}

// SetProgress records caller-owned progress text.
func (iv *InboxView) SetProgress(progress string) {
	if iv == nil {
		return
	}
	iv.progress = strings.TrimSpace(progress)
	iv.updateStatus()
	iv.updatePage()
}

func (iv *InboxView) ProgressMessage() string {
	if iv == nil {
		return ""
	}
	return iv.progress
}

func (iv *InboxView) StatusText() string {
	if iv == nil {
		return ""
	}
	return iv.statusText()
}

func (iv *InboxView) EmptyStateText() string {
	if iv == nil || len(iv.notifications) == 0 {
		return "No notifications"
	}
	return ""
}

func (iv *InboxView) showPage() {
	if iv == nil || iv.app == nil || iv.app.pages == nil {
		return
	}
	iv.app.pages.AddPage(inboxViewPageName, iv.modal, true, true)
	iv.app.pages.SendToFront(inboxViewPageName)
}

func (iv *InboxView) updatePage() {
	if iv == nil || iv.app == nil || iv.app.pages == nil || !iv.app.pages.HasPage(inboxViewPageName) {
		return
	}
	iv.app.pages.AddPage(inboxViewPageName, iv.modal, true, true)
	iv.app.pages.SendToFront(inboxViewPageName)
}

func (iv *InboxView) buildModal() {
	if iv == nil {
		return
	}
	iv.ensureWidgets()
	theme := iv.theme()
	density := iv.density()
	iv.header.SetText("Inbox / Notifications")
	iv.header.SetTextColor(theme.Accent)
	iv.header.SetBackgroundColor(theme.HeaderBg)
	iv.status.SetTextColor(theme.SecondaryText)
	iv.status.SetBackgroundColor(theme.HeaderBg)
	iv.help.SetTextColor(theme.SecondaryText)
	iv.help.SetBackgroundColor(theme.HeaderBg)
	iv.list.SetBackgroundColor(theme.HeaderBg)

	content := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(iv.header, 1, 0, false).
		AddItem(iv.status, 1, 0, false).
		AddItem(iv.list, 0, 1, true).
		AddItem(iv.help, 1, 0, false)
	content.Box = tview.NewBox().SetBackgroundColor(theme.HeaderBg)
	content.SetBackgroundColor(theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(theme.Accent).
		SetTitle(" Inbox / Notifications ").
		SetTitleColor(theme.Foreground)
	content.SetBorderPadding(density.ModalPadding.Top, density.ModalPadding.Bottom, density.ModalPadding.Left, density.ModalPadding.Right)

	const modalWidth = 90
	const modalHeight = 22
	inner := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(content, modalHeight, 0, true).
		AddItem(nil, 0, 1, false)
	iv.modal = newResponsiveWorkspaceModal(theme.Background, inner, modalWidth, nil)
	iv.updateStatus()
	iv.updateHelp()
	iv.refreshList()
	iv.List, iv.Header, iv.Status, iv.Help = iv.list, iv.header, iv.status, iv.help
}

func (iv *InboxView) refreshList() {
	if iv == nil {
		return
	}
	iv.ensureWidgets()
	iv.list.Clear()
	if len(iv.notifications) == 0 {
		iv.list.AddItem(iv.EmptyStateText(), "", 0, nil)
		iv.list.SetCurrentItem(0)
		iv.selectedID = ""
		iv.updateHelp()
		return
	}
	selectedIndex := 0
	for i, notification := range iv.notifications {
		if notification.ID == iv.selectedID {
			selectedIndex = i
			break
		}
	}
	for _, notification := range iv.notifications {
		iv.list.AddItem(formatInboxNotification(notification), formatInboxSecondary(notification), 0, nil)
	}
	iv.list.SetCurrentItem(selectedIndex)
	iv.selectedID = iv.notifications[selectedIndex].ID
	iv.updateHelp()
}

func formatInboxNotification(notification linearapi.Notification) string {
	state := "READ"
	if !notification.Read {
		state = "UNREAD"
	}
	if notification.Snoozed {
		state = "SNOOZED"
	}
	if notification.Archived {
		state = "ARCHIVED"
	}
	title := strings.TrimSpace(notification.Title)
	if title == "" {
		title = "(untitled notification)"
	}
	return "(" + state + ") " + title
}

func formatInboxSecondary(notification linearapi.Notification) string {
	parts := make([]string, 0, 3)
	if notification.Type != "" {
		parts = append(parts, notification.Type)
	}
	if notification.Subtitle != "" {
		parts = append(parts, strings.TrimSpace(strings.ReplaceAll(notification.Subtitle, "\n", " ")))
	}
	if notification.Issue != nil && notification.Issue.Identifier != "" {
		parts = append(parts, notification.Issue.Identifier)
	}
	return strings.Join(parts, "  ·  ")
}

func (iv *InboxView) updateStatus() {
	if iv != nil && iv.status != nil {
		iv.status.SetText(iv.statusText())
	}
}

func (iv *InboxView) statusText() string {
	if iv.loading {
		return "Loading…"
	}
	if iv.errorMessage != "" {
		return "Error: " + iv.errorMessage
	}
	return iv.progress
}

func (iv *InboxView) updateHelp() {
	if iv != nil && iv.help != nil {
		iv.help.SetText(iv.helpText())
		iv.help.SetTextAlign(tview.AlignCenter)
	}
}

func (iv *InboxView) helpText() string {
	if iv.confirmArchive {
		return "Enter/Y: archive | Esc/N: cancel"
	}
	return "j/k or ↑/↓: navigate | Enter: open issue | u: read/unread | s: snooze | a: archive | r: refresh | Esc: close"
}

func (iv *InboxView) focusList() {
	if iv != nil && iv.app != nil && iv.app.app != nil && iv.list != nil {
		iv.app.app.SetFocus(iv.list)
	}
}

func cloneInboxTimes(values []time.Time) []time.Time {
	if len(values) == 0 {
		return nil
	}
	return append([]time.Time(nil), values...)
}

func cloneNotifications(values []linearapi.Notification) []linearapi.Notification {
	if len(values) == 0 {
		return nil
	}
	result := make([]linearapi.Notification, len(values))
	for i, value := range values {
		result[i] = cloneNotification(value)
	}
	return result
}

func cloneNotification(value linearapi.Notification) linearapi.Notification {
	if value.ReadAt != nil {
		readAtCopy := *value.ReadAt
		value.ReadAt = &readAtCopy
	}
	if value.EmailedAt != nil {
		emailedAtCopy := *value.EmailedAt
		value.EmailedAt = &emailedAtCopy
	}
	if value.SnoozedUntilAt != nil {
		snoozedUntilAtCopy := *value.SnoozedUntilAt
		value.SnoozedUntilAt = &snoozedUntilAtCopy
	}
	if value.UnsnoozedAt != nil {
		unsnoozedAtCopy := *value.UnsnoozedAt
		value.UnsnoozedAt = &unsnoozedAtCopy
	}
	if value.ArchivedAt != nil {
		archivedAtCopy := *value.ArchivedAt
		value.ArchivedAt = &archivedAtCopy
	}
	if value.Issue != nil {
		issue := *value.Issue
		value.Issue = &issue
	}
	return value
}

func findNotification(values []linearapi.Notification, id string) *linearapi.Notification {
	for i := range values {
		if values[i].ID == id {
			return &values[i]
		}
	}
	return nil
}

func firstNotificationID(values []linearapi.Notification) string {
	for _, value := range values {
		if strings.TrimSpace(value.ID) != "" {
			return value.ID
		}
	}
	return ""
}

func removeNotification(values *[]linearapi.Notification, id string) bool {
	if values == nil {
		return false
	}
	for i := range *values {
		if (*values)[i].ID == id {
			*values = append((*values)[:i], (*values)[i+1:]...)
			return true
		}
	}
	return false
}
