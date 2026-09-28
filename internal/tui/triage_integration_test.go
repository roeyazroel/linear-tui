package tui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestTriageCommandsAreOnlyAvailableInTriageContext(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	if got := triageCommandIDs(DefaultCommands(app)); len(got) != 0 {
		t.Fatalf("triage commands outside triage = %v, want none", got)
	}
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	want := []string{"triage_accept", "triage_duplicate", "triage_decline", "triage_snooze"}
	got := triageCommandIDs(DefaultCommands(app))
	for _, id := range want {
		if !containsString(got, id) {
			t.Fatalf("triage command %q missing in triage context: %v", id, got)
		}
	}
}

func triageCommandIDs(commands []Command) []string {
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		if len(command.ID) >= len("triage_") && command.ID[:len("triage_")] == "triage_" {
			ids = append(ids, command.ID)
		}
	}
	return ids
}

func TestTriageAcceptUsesOneNonTriageUpdateAndOneRefresh(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", Title: "Review"}
	app.workflowStates = []linearapi.WorkflowState{{ID: "triage-state", Name: "Triage", Type: "triage"}, {ID: "started-state", Name: "Started", Type: "started"}}
	var updateCalls, refreshCalls atomic.Int32
	refreshDone := installRefreshCompletionHook(app)
	app.updateIssueFunc = func(_ context.Context, input linearapi.UpdateIssueInput) (linearapi.Issue, error) {
		updateCalls.Add(1)
		if input.StateID == nil || *input.StateID != "started-state" {
			t.Fatalf("update input = %#v, want started state", input)
		}
		return linearapi.Issue{}, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{Issues: []linearapi.Issue{{ID: "issue-1"}, {ID: "issue-2"}}}, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_accept")
	if command == nil {
		t.Fatal("triage accept command missing")
	}
	command.Run(app)
	if !app.pickerActive {
		t.Fatal("triage accept did not open workflow state picker")
	}
	app.pickerModal.HandleKey(enterKey())
	waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 1 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
}

func TestTriageDuplicateUsesSourceToTargetRelationOnce(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "source-1", Identifier: "ENG-1", Title: "Review"}
	var relationCalls, refreshCalls atomic.Int32
	refreshDone := installRefreshCompletionHook(app)
	app.createIssueRelationFunc = func(_ context.Context, input linearapi.CreateIssueRelationInput) (linearapi.IssueRelation, error) {
		relationCalls.Add(1)
		if input.IssueID != "source-1" || input.RelatedIssueID != "target-1" || input.Type != linearapi.IssueRelationDuplicate {
			t.Fatalf("duplicate relation input = %#v", input)
		}
		return linearapi.IssueRelation{}, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_duplicate")
	command.Run(app)
	if !app.pages.HasPage("text_input") {
		t.Fatal("duplicate command did not open target input")
	}
	submit := app.textInputModal.onSubmit
	app.textInputModal.Hide()
	submit("target-1")
	waitForCondition(t, time.Second, func() bool { return relationCalls.Load() == 1 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
}

func TestTriageDeclineRequiresConfirmationBeforeArchive(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", Title: "Review"}
	var archiveCalls, refreshCalls atomic.Int32
	refreshDone := installRefreshCompletionHook(app)
	app.archiveIssueFunc = func(_ context.Context, issueID string) error {
		archiveCalls.Add(1)
		if issueID != "issue-1" {
			t.Fatalf("archive issue ID = %q", issueID)
		}
		return nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{Issues: []linearapi.Issue{{ID: "issue-1"}, {ID: "issue-2"}}}, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_decline")
	command.Run(app)
	if archiveCalls.Load() != 0 || !app.pages.HasPage("confirmation") {
		t.Fatalf("decline before confirmation = archive=%d confirmation=%v", archiveCalls.Load(), app.pages.HasPage("confirmation"))
	}
	confirm := app.confirmationModal.onConfirm
	app.confirmationModal.Hide()
	confirm()
	waitForCondition(t, time.Second, func() bool { return archiveCalls.Load() == 1 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
}

func TestTriageSnoozeRequiresKnownRelatedNotification(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", Title: "Review"}
	var snoozeCalls atomic.Int32
	app.snoozeNotificationFunc = func(context.Context, string, time.Time) (linearapi.Notification, error) {
		snoozeCalls.Add(1)
		return linearapi.Notification{}, nil
	}
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		return nil, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_snooze")
	command.Run(app)
	waitForCondition(t, time.Second, func() bool {
		var status string
		workspaceUIRead(app, func() { status = app.statusMessage })
		return snoozeCalls.Load() == 0 && status == "Triage snooze unavailable: no related notification"
	})

	app.inboxView.Reload([]linearapi.Notification{{ID: "notification-1", Issue: &linearapi.IssueRef{ID: "issue-1"}}})
	var refreshCalls atomic.Int32
	refreshDone := installRefreshCompletionHook(app)
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	command.Run(app)
	waitForCondition(t, time.Second, func() bool { return snoozeCalls.Load() == 1 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
}

func TestTriageSnoozeLoadsFreshNotificationOnceAndCachesIt(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-fresh", Identifier: "ENG-99", Title: "Review"}
	var listCalls, snoozeCalls, refreshCalls atomic.Int32
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		listCalls.Add(1)
		return []linearapi.Notification{{ID: "notification-fresh", Issue: &linearapi.IssueRef{ID: "issue-fresh"}}}, nil
	}
	app.snoozeNotificationFunc = func(_ context.Context, notificationID string, _ time.Time) (linearapi.Notification, error) {
		snoozeCalls.Add(1)
		if notificationID != "notification-fresh" {
			t.Fatalf("snooze notification ID = %q, want notification-fresh", notificationID)
		}
		return linearapi.Notification{}, nil
	}
	refreshDone := installRefreshCompletionHook(app)
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{}, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_snooze")
	command.Run(app)
	waitForCondition(t, time.Second, func() bool { return listCalls.Load() == 1 && snoozeCalls.Load() == 1 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
	var inboxVisible bool
	workspaceUIRead(app, func() { inboxVisible = app.pages.HasPage(inboxViewPageName) })
	if inboxVisible {
		t.Fatal("fresh triage snooze unexpectedly opened Inbox")
	}

	refreshDone = installRefreshCompletionHook(app)
	app.selectedIssue = &linearapi.Issue{ID: "issue-fresh", Identifier: "ENG-99", Title: "Review"}
	command = findTriageCommand(DefaultCommands(app), "triage_snooze")
	command.Run(app)
	waitForCondition(t, time.Second, func() bool { return snoozeCalls.Load() == 2 && refreshCalls.Load() == 2 })
	waitForRefreshCompletion(t, refreshDone)
	if listCalls.Load() != 1 {
		t.Fatalf("cached triage snooze list calls = %d, want one", listCalls.Load())
	}
}

func TestTriageSnoozeLookupDeduplicatesConcurrentLoadsAndSurfacesErrors(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.selectedIssue = &linearapi.Issue{ID: "issue-concurrent", Identifier: "ENG-100", Title: "Review"}
	started := make(chan struct{})
	release := make(chan struct{})
	snoozeCall := make(chan struct{}, 4)
	statusQueued := make(chan struct{}, 1)
	baseQueue := app.queueUpdateDraw
	app.queueUpdateDraw = func(fn func()) {
		baseQueue(func() {
			fn()
			if app.statusBar != nil && containsText(app.statusBar.GetText(true), "notification lookup failed") {
				select {
				case statusQueued <- struct{}{}:
				default:
				}
			}
		})
	}
	var listCalls, snoozeCalls atomic.Int32
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		if listCalls.Add(1) == 1 {
			close(started)
			<-release
		}
		return []linearapi.Notification{{ID: "notification-concurrent", Issue: &linearapi.IssueRef{ID: "issue-concurrent"}}}, nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{Issues: []linearapi.Issue{{ID: "issue-concurrent", Identifier: "ENG-100", Title: "Review"}}}, nil
	}
	app.snoozeNotificationFunc = func(context.Context, string, time.Time) (linearapi.Notification, error) {
		snoozeCalls.Add(1)
		select {
		case snoozeCall <- struct{}{}:
		default:
		}
		return linearapi.Notification{}, nil
	}
	refreshDone := installRefreshCompletionHook(app)
	command := findTriageCommand(DefaultCommands(app), "triage_snooze")
	command.Run(app)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("triage notification lookup did not start")
	}
	command.Run(app)
	close(release)
	watchdog := time.NewTimer(5 * time.Second)
	defer watchdog.Stop()
	waitForSignal := func(label string, signal <-chan struct{}) {
		select {
		case <-signal:
			return
		case <-watchdog.C:
			t.Fatalf("timed out waiting for %s (snooze calls=%d)", label, snoozeCalls.Load())
		}
	}
	waitForSignal("first snooze call", snoozeCall)
	waitForSignal("first triage refresh", refreshDone)
	waitForSignal("second snooze call", snoozeCall)
	waitForSignal("second triage refresh", refreshDone)
	if listCalls.Load() != 1 {
		t.Fatalf("concurrent triage lookup list calls = %d, want one", listCalls.Load())
	}
	if snoozeCalls.Load() != 2 {
		t.Fatalf("concurrent triage snooze calls = %d, want exactly 2", snoozeCalls.Load())
	}

	app.invalidateTriageNotificationLookup()
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		return nil, errors.New("notification lookup failed")
	}
	command.Run(app)
	errorWatchdog := time.NewTimer(5 * time.Second)
	defer errorWatchdog.Stop()
	select {
	case <-statusQueued:
	case <-errorWatchdog.C:
		t.Fatal("timed out waiting for queued triage lookup error status")
	}
	if snoozeCalls.Load() != 2 {
		t.Fatalf("snooze ran after lookup error: %d calls", snoozeCalls.Load())
	}
}

func TestTriageBulkRetainsFailedMarksAndRefreshesOnce(t *testing.T) {
	app := newTriageIntegrationApp(t)
	app.selectedNavigation = &NavigationNode{ID: "triage", Text: "Triage", StateType: "triage"}
	app.issuesMu.Lock()
	app.issues = []linearapi.Issue{{ID: "issue-1"}, {ID: "issue-2"}}
	app.issuesMu.Unlock()
	app.markedIssueSelection.Toggle("issue-1")
	app.markedIssueSelection.Toggle("issue-2")
	var archiveCalls, refreshCalls atomic.Int32
	refreshDone := installRefreshCompletionHook(app)
	app.archiveIssueFunc = func(_ context.Context, issueID string) error {
		archiveCalls.Add(1)
		if issueID == "issue-2" {
			return errors.New("archive failed")
		}
		return nil
	}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		refreshCalls.Add(1)
		return linearapi.IssuePage{Issues: []linearapi.Issue{{ID: "issue-1"}, {ID: "issue-2"}}}, nil
	}
	command := findTriageCommand(DefaultCommands(app), "triage_decline")
	command.Run(app)
	confirm := app.confirmationModal.onConfirm
	app.confirmationModal.Hide()
	confirm()
	waitForCondition(t, time.Second, func() bool { return archiveCalls.Load() == 2 && refreshCalls.Load() == 1 })
	waitForRefreshCompletion(t, refreshDone)
	if app.markedIssueSelection.IsMarked("issue-1") || !app.markedIssueSelection.IsMarked("issue-2") {
		t.Fatalf("marks after partial triage = issue-1:%v issue-2:%v", app.markedIssueSelection.IsMarked("issue-1"), app.markedIssueSelection.IsMarked("issue-2"))
	}
}

func newTriageIntegrationApp(t *testing.T) *App {
	t.Helper()
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.fetchIssueByID = func(_ context.Context, issueID string) (linearapi.Issue, error) {
		return linearapi.Issue{ID: issueID}, nil
	}
	return app
}

func enterKey() *tcell.EventKey { return tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone) }

func findTriageCommand(commands []Command, id string) *Command {
	for index := range commands {
		if commands[index].ID == id {
			return &commands[index]
		}
	}
	return nil
}
