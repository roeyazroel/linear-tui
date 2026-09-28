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

func newWorkspaceViewsIntegrationApp(t *testing.T, mode string) *App {
	t.Helper()
	app := NewApp(&linearapi.Client{}, config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
		Keybindings:    map[string]string{"navigate_inbox": "g i", "navigate_initiatives": "g n"},
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.preloadTeamMetadataFunc = func(string) {}
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{}, nil
	}
	app.selectedNavigation = &NavigationNode{ID: "all", Text: "All Issues"}
	app.focusedPane = FocusNavigation
	return app
}

func workspaceUIRead(app *App, fn func()) {
	app.uiUpdateMu.Lock()
	defer app.uiUpdateMu.Unlock()
	fn()
}

func TestWorkspaceViewsInboxChordLoadsAndActionsOnce(t *testing.T) {
	for _, mode := range []string{"", ""} {
		t.Run(mode, func(t *testing.T) {
			app := newWorkspaceViewsIntegrationApp(t, mode)
			var loads atomic.Int32
			var markReads atomic.Int32
			loaded := make(chan struct{}, 2)
			app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
				loads.Add(1)
				loaded <- struct{}{}
				return []linearapi.Notification{{
					ID: "notification-1", Title: "Mentioned", Issue: &linearapi.IssueRef{ID: "issue-1", Identifier: "ENG-1", Title: "Fix"},
				}}, nil
			}
			app.markNotificationReadFunc = func(context.Context, string, time.Time) (linearapi.Notification, error) {
				markReads.Add(1)
				return linearapi.Notification{}, nil
			}
			app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
				return linearapi.Issue{ID: "issue-1", Identifier: "ENG-1", Title: "Fix"}, nil
			}

			capture := app.app.GetInputCapture()
			capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
			capture(tcell.NewEventKey(tcell.KeyRune, 'i', tcell.ModNone))
			select {
			case <-loaded:
			case <-time.After(time.Second):
				t.Fatal("inbox load did not start")
			}
			var inboxVisible bool
			workspaceUIRead(app, func() { inboxVisible = app.pages.HasPage(inboxViewPageName) })
			if !inboxVisible || app.inboxView == nil {
				t.Fatal("g i did not open inbox view")
			}
			deadline := time.Now().Add(time.Second)
			var selectedID string
			for selectedID == "" && time.Now().Before(deadline) {
				workspaceUIRead(app, func() { selectedID = app.inboxView.SelectedNotificationID() })
				time.Sleep(time.Millisecond)
			}
			workspaceUIRead(app, func() {
				app.inboxView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
			})
			waitForCondition(t, time.Second, func() bool { return markReads.Load() == 1 })
			if markReads.Load() != 1 {
				t.Fatalf("mark read calls = %d, want 1", markReads.Load())
			}
			select {
			case <-loaded:
			case <-time.After(time.Second):
				t.Fatal("inbox action did not refresh")
			}
			if loads.Load() != 2 {
				t.Fatalf("inbox load calls = %d, want initial + one action refresh", loads.Load())
			}
			workspaceUIRead(app, func() {
				app.inboxView.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
			})
			workspaceUIRead(app, func() { inboxVisible = app.pages.HasPage(inboxViewPageName) })
			if inboxVisible {
				t.Fatal("opening related issue left inbox visible")
			}
		})
	}
}

func TestWorkspaceViewsInboxIgnoresStaleLoad(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			<-release
			return []linearapi.Notification{{ID: "stale", Title: "stale"}}, nil
		}
		return []linearapi.Notification{{ID: "fresh", Title: "fresh"}}, nil
	}
	app.openInbox()
	<-firstStarted
	app.openInbox()
	close(release)
	time.Sleep(20 * time.Millisecond)
	var got string
	workspaceUIRead(app, func() { got = app.inboxView.SelectedNotificationID() })
	if got != "fresh" {
		t.Fatalf("stale inbox selection = %q, want fresh", got)
	}
}

func TestWorkspaceViewsInboxClosedMutationDropsReload(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	loaded := make(chan struct{}, 4)
	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		loads.Add(1)
		loaded <- struct{}{}
		return []linearapi.Notification{{ID: "notification-1", Title: "Mentioned"}}, nil
	}
	app.markNotificationReadFunc = func(context.Context, string, time.Time) (linearapi.Notification, error) {
		close(started)
		<-release
		return linearapi.Notification{}, nil
	}
	app.openInbox()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("initial inbox load did not start")
	}
	waitForCondition(t, time.Second, func() bool {
		selected := ""
		workspaceUIRead(app, func() { selected = app.inboxView.SelectedNotificationID() })
		return selected == "notification-1"
	})
	workspaceUIRead(app, func() {
		app.inboxView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'u', tcell.ModNone))
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inbox mutation did not start")
	}
	baseline := loads.Load()
	workspaceUIRead(app, func() { app.inboxView.Hide() })
	close(release)
	waitForInboxRunner(t, app.inboxView)
	time.Sleep(20 * time.Millisecond)
	if got := loads.Load(); got != baseline {
		t.Fatalf("closed inbox mutation reloads = %d, want unchanged baseline %d", got, baseline)
	}
}

func TestWorkspaceViewsCommentsPaletteLoadsAndRefreshesOnce(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.selectedIssue = &linearapi.Issue{
		ID: "issue-1", Identifier: "ENG-1", Title: "Fix", Comments: []linearapi.Comment{{
			ID: "comment-1", Body: "old", Author: linearapi.User{ID: "user-1", IsMe: true},
		}},
	}
	var edits atomic.Int32
	var fetches atomic.Int32
	app.updateCommentFunc = func(context.Context, string, string) (linearapi.Comment, error) {
		edits.Add(1)
		return linearapi.Comment{}, nil
	}
	app.fetchIssueByID = func(context.Context, string) (linearapi.Issue, error) {
		fetches.Add(1)
		return *app.selectedIssue, nil
	}
	for _, command := range app.paletteCtrl.commands {
		if command.ID == "open_comments" {
			command.Run(app)
			break
		}
	}
	var commentsVisible bool
	workspaceUIRead(app, func() { commentsVisible = app.pages.HasPage(commentsModalPageName) })
	if !commentsVisible {
		t.Fatal("comments palette entry did not open modal")
	}
	workspaceUIRead(app, func() {
		app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
		app.commentsModal.EditField().SetText("updated", true)
		app.commentsModal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	})
	deadline := time.Now().Add(time.Second)
	for fetches.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if edits.Load() != 1 || fetches.Load() != 1 {
		t.Fatalf("comment calls = (edit=%d, refresh=%d), want (1,1)", edits.Load(), fetches.Load())
	}
	workspaceUIRead(app, func() { commentsVisible = app.pages.HasPage(commentsModalPageName) })
	if !commentsVisible {
		t.Fatal("successful edit unexpectedly closed comments modal")
	}
}

func TestWorkspaceViewsRoadmapChordLoadsAndReportsErrors(t *testing.T) {
	for _, mode := range []string{"", ""} {
		t.Run(mode, func(t *testing.T) {
			app := newWorkspaceViewsIntegrationApp(t, mode)
			app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
				return []linearapi.Initiative{{ID: "initiative-1", Name: "Platform", Projects: []linearapi.Project{{ID: "project-1", Name: "CLI"}}}}, nil
			}
			app.listProjectUpdatesFunc = func(context.Context, string) ([]linearapi.ProjectUpdate, error) {
				return []linearapi.ProjectUpdate{{ID: "update-1", ProjectID: "project-1", Body: "status"}}, nil
			}
			capture := app.app.GetInputCapture()
			capture(tcell.NewEventKey(tcell.KeyRune, 'g', tcell.ModNone))
			capture(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
			deadline := time.Now().Add(time.Second)
			var roadmapVisible bool
			for !roadmapVisible && time.Now().Before(deadline) {
				workspaceUIRead(app, func() { roadmapVisible = app.pages.HasPage(roadmapPageName) })
				time.Sleep(time.Millisecond)
			}
			if !roadmapVisible || app.roadmapView == nil {
				t.Fatal("g n did not open roadmap view")
			}
			app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
				return nil, errors.New("roadmap offline")
			}
			workspaceUIRead(app, func() { app.reloadRoadmap() })
			deadline = time.Now().Add(time.Second)
			var details string
			for !strings.Contains(details, "roadmap offline") && time.Now().Before(deadline) {
				workspaceUIRead(app, func() { details = app.roadmapView.Details.GetText(true) })
				time.Sleep(time.Millisecond)
			}
			if !strings.Contains(details, "roadmap offline") {
				t.Fatalf("roadmap error = %q", details)
			}
		})
	}
}

func TestWorkspaceViewsModeSwitchRebuildsShellWithoutReload(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	notification := linearapi.Notification{ID: "notification-1", Title: "Keep selection"}
	workspaceUIRead(app, func() {
		app.inboxView.Show(InboxViewOptions{Notifications: []linearapi.Notification{notification}}, InboxViewCallbacks{})
	})
	var loads atomic.Int32
	app.listNotificationsFunc = func(context.Context, linearapi.NotificationListOptions) ([]linearapi.Notification, error) {
		loads.Add(1)
		return nil, nil
	}
	app.applySettings(config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
	})
	var selected string
	workspaceUIRead(app, func() {
		selected = app.inboxView.SelectedNotificationID()
	})
	if selected != notification.ID {
		t.Fatalf("selection after mode switch = %q, want %q", selected, notification.ID)
	}
	if loads.Load() != 0 {
		t.Fatalf("mode switch triggered %d inbox loads, want 0", loads.Load())
	}
}
