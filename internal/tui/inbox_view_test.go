package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func inboxViewNotifications() []linearapi.Notification {
	readAt := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	snoozedUntil := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	return []linearapi.Notification{
		{
			ID: "notification-1", Type: "issueAssigned", Title: "Review issue", Subtitle: "Please review",
			Issue: &linearapi.IssueRef{ID: "issue-1", Identifier: "ABC-1", Title: "Review issue"},
		},
		{ID: "notification-2", Type: "commentMention", Title: "Mentioned", Subtitle: "A comment", Read: true, ReadAt: &readAt},
		{ID: "notification-3", Type: "issueUpdated", Title: "Snoozed", Subtitle: "Later", Snoozed: true, SnoozedUntilAt: &snoozedUntil},
	}
}

func TestInboxViewStateNavigationAndHelp(t *testing.T) {
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications(), SelectedID: "notification-1"}, InboxViewCallbacks{})

	if got := view.SelectedNotificationID(); got != "notification-1" {
		t.Fatalf("selected ID = %q, want notification-1", got)
	}
	visible := view.VisibleNotifications()
	if len(visible) != 3 || visible[0].Read || !visible[1].Read || !visible[2].Snoozed {
		t.Fatalf("visible notification states = %#v", visible)
	}
	firstMain, _ := view.List.GetItemText(0)
	secondMain, _ := view.List.GetItemText(1)
	thirdMain, _ := view.List.GetItemText(2)
	if !strings.Contains(firstMain, "UNREAD") || !strings.Contains(secondMain, "READ") || !strings.Contains(thirdMain, "SNOOZED") {
		t.Fatalf("list state labels = %q / %q / %q", firstMain, secondMain, thirdMain)
	}
	if !strings.Contains(view.HelpText(), "j/k") || !strings.Contains(view.HelpText(), "u") || !strings.Contains(view.HelpText(), "Esc") {
		t.Fatalf("help = %q, missing shortcuts", view.HelpText())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if got := view.SelectedNotificationID(); got != "notification-2" {
		t.Fatalf("selected after j = %q, want notification-2", got)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if got := view.SelectedNotificationID(); got != "notification-3" {
		t.Fatalf("selected after down = %q, want notification-3", got)
	}
}

func TestInboxViewRendersStatusMarkers(t *testing.T) {
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{})
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 12)
	view.List.SetRect(0, 0, 80, 12)
	view.List.Draw(screen)
	var rendered strings.Builder
	for y := 0; y < 12; y++ {
		for x := 0; x < 80; x++ {
			text, _, _ := screen.Get(x, y)
			rendered.WriteString(text)
		}
	}
	for _, marker := range []string{"(UNREAD)", "(READ)", "(SNOOZED)"} {
		if !strings.Contains(rendered.String(), marker) {
			t.Fatalf("rendered inbox = %q, missing %s", rendered.String(), marker)
		}
	}
}

func TestInboxViewReloadPreservesStableSelection(t *testing.T) {
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{})
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if view.SelectedNotificationID() != "notification-2" {
		t.Fatalf("selected before reload = %q", view.SelectedNotificationID())
	}
	view.Reload([]linearapi.Notification{
		{ID: "notification-3", Title: "Snoozed"},
		{ID: "notification-2", Title: "Mentioned updated", Read: true},
	})
	if got := view.SelectedNotificationID(); got != "notification-2" {
		t.Fatalf("selected after reload = %q, want notification-2", got)
	}
	if got := view.SelectedNotification().Title; got != "Mentioned updated" {
		t.Fatalf("selected title after reload = %q", got)
	}
	view.Reload([]linearapi.Notification{{ID: "replacement", Title: "Replacement"}})
	if got := view.SelectedNotificationID(); got != "replacement" {
		t.Fatalf("selected after removed selection = %q, want replacement", got)
	}
}

func TestInboxViewActionsUseInjectedCallbacks(t *testing.T) {
	opened, toggled := "", ""
	readTarget := false
	refreshed := 0
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{
		OnOpenIssue: func(issueID string) { opened = issueID },
		OnToggleRead: func(notificationID string, read bool) error {
			toggled, readTarget = notificationID, read
			return nil
		},
		OnRefresh: func() error { refreshed++; return nil },
	})
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if opened != "issue-1" {
		t.Fatalf("open issue ID = %q, want issue-1", opened)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	if toggled != "notification-1" || !readTarget || !view.SelectedNotification().Read {
		t.Fatalf("toggle callback/state = (%q, %v, %#v)", toggled, readTarget, view.SelectedNotification())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	if readTarget || view.SelectedNotification().Read {
		t.Fatalf("second toggle target/state = (%v, %#v)", readTarget, view.SelectedNotification())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	if refreshed != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshed)
	}
}

func TestInboxViewSnoozeChoiceAndArchiveConfirmation(t *testing.T) {
	snoozedID := ""
	var snoozedUntil time.Time
	archived := ""
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications(), SnoozeChoices: []time.Time{time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}}, InboxViewCallbacks{
		OnChooseSnooze: func(notificationID string) (time.Time, error) {
			snoozedID = notificationID
			return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC), nil
		},
		OnSnooze:  func(notificationID string, until time.Time) error { snoozedUntil = until; return nil },
		OnArchive: func(notificationID string) error { archived = notificationID; return nil },
	})
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModNone))
	if snoozedID != "notification-1" || snoozedUntil.IsZero() || !view.SelectedNotification().Snoozed {
		t.Fatalf("snooze callback/state = (%q, %v, %#v)", snoozedID, snoozedUntil, view.SelectedNotification())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !view.IsArchiveConfirmationOpen() {
		t.Fatal("a did not open archive confirmation")
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if view.IsArchiveConfirmationOpen() || archived != "" {
		t.Fatalf("archive Esc state = (%v, %q)", view.IsArchiveConfirmationOpen(), archived)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if archived != "notification-1" || view.SelectedNotificationID() == "notification-1" {
		t.Fatalf("archive callback/selection = (%q, %q)", archived, view.SelectedNotificationID())
	}
}

func TestInboxViewLoadingErrorProgressAndEmptyState(t *testing.T) {
	view := NewInboxView(nil)
	view.Show(InboxViewOptions{}, InboxViewCallbacks{})
	if !strings.Contains(strings.ToLower(view.EmptyStateText()), "no notifications") {
		t.Fatalf("empty state = %q", view.EmptyStateText())
	}
	view.SetLoading(true)
	if !view.Loading() || !strings.Contains(strings.ToLower(view.StatusText()), "loading") {
		t.Fatalf("loading status = (%v, %q)", view.Loading(), view.StatusText())
	}
	view.SetProgress("page 2/4")
	if view.ProgressMessage() != "page 2/4" {
		t.Fatalf("progress = %q", view.ProgressMessage())
	}
	view.SetError("refresh failed")
	if view.ErrorMessage() != "refresh failed" || !strings.Contains(view.StatusText(), "refresh failed") {
		t.Fatalf("error status = (%q, %q)", view.ErrorMessage(), view.StatusText())
	}
	view.SetLoading(false)
}

func TestInboxViewLifecycle(t *testing.T) {
	app := newInboxViewTestApp("")
	closed := 0
	view := NewInboxView(app)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{OnClose: func() { closed++ }})
	if !app.pages.HasPage("inbox_view") || view.GetModal() == nil {
		t.Fatal("Show did not present inbox view")
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if app.pages.HasPage("inbox_view") || closed != 1 {
		t.Fatalf("close state = (page=%v, callbacks=%d)", app.pages.HasPage("inbox_view"), closed)
	}
}

func newInboxViewTestApp(mode string) *App {
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

func TestInboxViewAppMutationReturnsImmediately(t *testing.T) {
	app := newInboxViewTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	started := make(chan struct{})
	release := make(chan struct{})
	view := NewInboxView(app)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{
		OnToggleRead: func(string, bool) error {
			close(started)
			<-release
			return nil
		},
	})
	done := make(chan struct{})
	go func() {
		view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("read callback did not start")
	}
	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("inbox read key handler blocked on callback")
	}
	close(release)
	waitForInboxRunner(t, view)
	var loading bool
	var selected *linearapi.Notification
	workspaceUIRead(app, func() {
		loading = view.Loading()
		selected = view.SelectedNotification()
	})
	if loading {
		t.Fatal("inbox read remained loading after callback completion")
	}
	if selected == nil || !selected.Read {
		t.Fatalf("selected notification after async read = %#v, want read", selected)
	}
}

func TestInboxViewAppliesMutationAfterSelectionMovesAndRejectsReentry(t *testing.T) {
	app := newInboxViewTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	view := NewInboxView(app)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{
		OnToggleRead: func(string, bool) error {
			calls.Add(1)
			close(started)
			<-release
			return nil
		},
	})
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	<-started
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	close(release)
	waitForInboxRunner(t, view)
	if calls.Load() != 1 {
		t.Fatalf("reentered inbox action calls = %d, want one", calls.Load())
	}
	var selected *linearapi.Notification
	var first *linearapi.Notification
	workspaceUIRead(app, func() {
		selected = view.SelectedNotification()
		first = findNotification(view.notifications, "notification-1")
	})
	if selected == nil || selected.ID != "notification-2" || !selected.Read {
		t.Fatalf("selection after moved mutation = %#v, want notification-2 unchanged", selected)
	}
	if first == nil || !first.Read {
		t.Fatalf("target notification after moved selection = %#v, want read", first)
	}
}

func TestInboxViewAsyncMutationSurfacesErrorOnce(t *testing.T) {
	app := newInboxViewTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	var calls atomic.Int32
	view := NewInboxView(app)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{
		OnToggleRead: func(string, bool) error {
			calls.Add(1)
			return errors.New("read failed")
		},
	})
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	waitForInboxRunner(t, view)
	var errorMessage string
	workspaceUIRead(app, func() { errorMessage = view.ErrorMessage() })
	if calls.Load() != 1 || !strings.Contains(errorMessage, "read failed") {
		t.Fatalf("async read error = calls:%d error:%q", calls.Load(), errorMessage)
	}
}

func TestInboxViewMutationUsesRunnerContextAndNotifiesCompletion(t *testing.T) {
	app := newInboxViewTestApp("")
	app.queueUpdateDraw = func(fn func()) { fn() }
	started := make(chan context.Context, 1)
	var completions atomic.Int32
	view := NewInboxView(app)
	view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{
		OnToggleReadContext: func(ctx context.Context, _ string, _ bool) error {
			started <- ctx
			return nil
		},
		OnActionComplete: func(name, notificationID string) {
			if name != "toggle read" || notificationID != "notification-1" {
				t.Errorf("completion = (%q, %q), want toggle read/notification-1", name, notificationID)
			}
			completions.Add(1)
		},
	})
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	var ctx context.Context
	select {
	case ctx = <-started:
	case <-time.After(time.Second):
		t.Fatal("context-aware inbox mutation did not start")
	}
	waitForInboxRunner(t, view)
	if ctx == nil || ctx.Done() == nil {
		t.Fatalf("runner context = %#v, want cancellable context", ctx)
	}
	if got := completions.Load(); got != 1 {
		t.Fatalf("completion count = %d, want one", got)
	}
}

func TestAppInboxMutationSelectionMoveReloadsOnceAndPreservesSelection(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	notifications := []linearapi.Notification{
		{ID: "notification-move-1", Title: "First"},
		{ID: "notification-move-2", Title: "Second"},
	}
	authoritative := append([]linearapi.Notification(nil), notifications...)
	authoritative[0].Read = true
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	authoritative[0].ReadAt = &now
	var markCalls, listCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	app.markNotificationReadFunc = func(ctx context.Context, notificationID string, _ time.Time) (linearapi.Notification, error) {
		markCalls.Add(1)
		if notificationID != notifications[0].ID {
			t.Errorf("mark-read target = %q, want %q", notificationID, notifications[0].ID)
		}
		close(started)
		<-release
		return authoritative[0], nil
	}
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		listCalls.Add(1)
		return authoritative, nil
	}
	app.inboxView = NewInboxView(app)
	app.inboxView.Show(InboxViewOptions{Notifications: notifications}, app.inboxCallbacks())
	app.inboxView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inbox mutation did not start")
	}
	app.inboxView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if got := app.inboxView.SelectedNotificationID(); got != notifications[1].ID {
		t.Fatalf("selection during inbox mutation = %q, want %q", got, notifications[1].ID)
	}
	close(release)
	waitForInboxRunner(t, app.inboxView)
	waitForCondition(t, time.Second, func() bool {
		selected := ""
		loading := true
		workspaceUIRead(app, func() {
			selected = app.inboxView.SelectedNotificationID()
			loading = app.inboxView.Loading()
		})
		return listCalls.Load() == 1 && selected == notifications[1].ID && !loading
	})
	var selectedID string
	var firstRead bool
	workspaceUIRead(app, func() {
		selectedID = app.inboxView.SelectedNotificationID()
		if selected := findNotification(app.inboxView.notifications, notifications[0].ID); selected != nil {
			firstRead = selected.Read
		}
	})
	if markCalls.Load() != 1 || listCalls.Load() != 1 {
		t.Fatalf("moved inbox mutation calls = mark:%d list:%d, want one each", markCalls.Load(), listCalls.Load())
	}
	if selectedID != notifications[1].ID {
		t.Fatalf("selection after inbox reload = %q, want %q", selectedID, notifications[1].ID)
	}
	if !firstRead {
		t.Fatal("authoritative inbox reload did not retain target read state")
	}
}

func waitForInboxRunner(t *testing.T, view *InboxView) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for view.actionRunner != nil && view.actionRunner.InFlight() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if view.actionRunner != nil && view.actionRunner.InFlight() {
		t.Fatal("Inbox action runner remained in flight")
	}
	if view.app != nil {
		workspaceUIRead(view.app, func() {})
	}
}
