package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestAppRoadmapActionsReachInitiativeEditor(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.selectedNavigation = &NavigationNode{ID: "team-1", TeamID: "team-1", IsTeam: true, Text: "Engineering"}
	actions := app.roadmapActions()
	if actions.OnCreateInitiative == nil {
		t.Fatal("roadmap create initiative callback is nil")
	}
	actions.OnCreateInitiative()
	waitForCondition(t, time.Second, func() bool {
		visible := false
		workspaceUIRead(app, func() { visible = app.pages.HasPage("roadmap_editor") })
		return visible
	})
}

func roadmapAppWithVisibleView(t *testing.T) *App {
	t.Helper()
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.selectedNavigation = &NavigationNode{ID: "team-1", TeamID: "team-1", IsTeam: true, Text: "Engineering"}
	app.roadmapView.Show(RoadmapViewOptions{Initiatives: []linearapi.Initiative{{
		ID:       "initiative-1",
		Name:     "Platform",
		Projects: []linearapi.Project{{ID: "project-1", Name: "CLI", TeamID: "team-1"}},
	}}, Actions: app.roadmapActions()})
	return app
}

func waitForRoadmapRunner(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for app.roadmapActionRunner != nil && app.roadmapActionRunner.InFlight() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if app.roadmapActionRunner != nil && app.roadmapActionRunner.InFlight() {
		t.Fatal("roadmap action runner remained in flight")
	}
	workspaceUIRead(app, func() {})
}

func waitForRoadmapReload(t *testing.T, app *App, reloadCalls *atomic.Int32, want int32) {
	t.Helper()
	waitForCondition(t, time.Second, func() bool {
		if reloadCalls.Load() != want {
			return false
		}
		loaded := false
		workspaceUIRead(app, func() { loaded = !app.roadmapView.options.Loading })
		return loaded
	})
}

func TestAppRoadmapInitiativeCRUDMapsInputsAndReloadsOnce(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var createCalls, updateCalls, reloadCalls atomic.Int32
	var createInput linearapi.CreateInitiativeInput
	var updateInput linearapi.UpdateInitiativeInput
	var inputMu sync.Mutex
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		return []linearapi.Initiative{{ID: "initiative-1", Name: "Fresh"}}, nil
	}
	app.createInitiativeFunc = func(_ context.Context, input linearapi.CreateInitiativeInput) (linearapi.Initiative, error) {
		createCalls.Add(1)
		inputMu.Lock()
		createInput = input
		inputMu.Unlock()
		return linearapi.Initiative{ID: "created"}, nil
	}
	app.updateInitiativeFunc = func(_ context.Context, input linearapi.UpdateInitiativeInput) (linearapi.Initiative, error) {
		updateCalls.Add(1)
		inputMu.Lock()
		updateInput = input
		inputMu.Unlock()
		return linearapi.Initiative{ID: input.ID}, nil
	}
	actions := app.roadmapActions()
	actions.OnCreateInitiative()
	editor := app.roadmapEditor
	editor.name.SetText("Launch")
	editor.description.SetText("Ship it", false)
	editor.status.SetCurrentOption(2)
	editor.targetDate.SetText("2026-12-31")
	editor.save()
	waitForCondition(t, time.Second, func() bool { return createCalls.Load() == 1 })
	waitForRoadmapReload(t, app, &reloadCalls, 1)
	inputMu.Lock()
	gotCreate := createInput
	inputMu.Unlock()
	if gotCreate.Name != "Launch" || gotCreate.Description != "Ship it" || gotCreate.Status != linearapi.InitiativeStatusActive || gotCreate.TargetDate != "2026-12-31" {
		t.Fatalf("create initiative input = %+v", gotCreate)
	}

	actions.OnEditInitiative(linearapi.Initiative{ID: "initiative-1", Name: "Launch", Description: "Old", Status: "Active", TargetDate: "2026-12-31"})
	editor = app.roadmapEditor
	editor.name.SetText("Launch v2")
	editor.description.SetText("", false)
	editor.targetDate.SetText("")
	editor.status.SetCurrentOption(3)
	editor.save()
	waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 1 })
	waitForRoadmapReload(t, app, &reloadCalls, 2)
	inputMu.Lock()
	gotUpdate := updateInput
	inputMu.Unlock()
	if gotUpdate.ID != "initiative-1" || gotUpdate.Name == nil || *gotUpdate.Name != "Launch v2" || gotUpdate.Description == nil || *gotUpdate.Description != "" || gotUpdate.TargetDate == nil || *gotUpdate.TargetDate != "" || gotUpdate.Status == nil || *gotUpdate.Status != linearapi.InitiativeStatusCompleted {
		t.Fatalf("update initiative input = %+v, want explicit clears", gotUpdate)
	}
}

func TestAppRoadmapProjectCRUDMapsInputsAndReloadsOnce(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var createCalls, updateCalls, reloadCalls atomic.Int32
	var createInput linearapi.CreateProjectInput
	var updateInput linearapi.UpdateProjectInput
	var inputMu sync.Mutex
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		return nil, nil
	}
	app.createProjectFunc = func(_ context.Context, input linearapi.CreateProjectInput) (linearapi.Project, error) {
		createCalls.Add(1)
		inputMu.Lock()
		createInput = input
		inputMu.Unlock()
		return linearapi.Project{ID: "created"}, nil
	}
	app.createInitiativeToProjectFunc = func(context.Context, linearapi.CreateInitiativeToProjectInput) error {
		return nil
	}
	app.updateProjectFunc = func(_ context.Context, input linearapi.UpdateProjectInput) (linearapi.Project, error) {
		updateCalls.Add(1)
		inputMu.Lock()
		updateInput = input
		inputMu.Unlock()
		return linearapi.Project{ID: input.ID}, nil
	}
	actions := app.roadmapActions()
	actions.OnCreateProject("initiative-1")
	editor := app.roadmapEditor
	editor.name.SetText("CLI")
	editor.description.SetText("Project docs", false)
	// The selected team's ID is the required Linear project team mapping.
	editor.save()
	waitForCondition(t, time.Second, func() bool { return createCalls.Load() == 1 })
	waitForRoadmapReload(t, app, &reloadCalls, 1)
	inputMu.Lock()
	gotCreate := createInput
	inputMu.Unlock()
	if gotCreate.Name != "CLI" || gotCreate.TeamID != "team-1" || gotCreate.Description != "Project docs" {
		t.Fatalf("create project input = %+v, want CLI/team-1", gotCreate)
	}

	actions.OnEditProject(linearapi.Project{ID: "project-1", Name: "CLI", Description: "Old docs", TeamID: "team-1"})
	editor = app.roadmapEditor
	editor.name.SetText("CLI v2")
	editor.save()
	waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 1 })
	waitForRoadmapReload(t, app, &reloadCalls, 2)
	inputMu.Lock()
	gotUpdate := updateInput
	inputMu.Unlock()
	if gotUpdate.ID != "project-1" || gotUpdate.Name == nil || *gotUpdate.Name != "CLI v2" || gotUpdate.Description != nil || gotUpdate.TeamID != nil || gotUpdate.TeamIDs != nil {
		t.Fatalf("ordinary project rename input = %+v, want team fields omitted", gotUpdate)
	}

	actions.OnEditProject(linearapi.Project{ID: "project-1", Name: "CLI v2", Description: "Old docs", TeamID: "team-1"})
	editor = app.roadmapEditor
	editor.name.SetText("CLI v3")
	editor.description.SetText("New docs", false)
	editor.teamID.SetText("")
	editor.save()
	waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 2 })
	waitForRoadmapReload(t, app, &reloadCalls, 3)
	inputMu.Lock()
	gotUpdate = updateInput
	inputMu.Unlock()
	if gotUpdate.Description == nil || *gotUpdate.Description != "New docs" || gotUpdate.TeamID != nil || gotUpdate.TeamIDs == nil || len(*gotUpdate.TeamIDs) != 0 {
		t.Fatalf("explicit project team clear input = %+v, want empty TeamIDs", gotUpdate)
	}

	actions.OnEditProject(linearapi.Project{ID: "project-1", Name: "CLI v3", Description: "New docs", TeamID: ""})
	editor = app.roadmapEditor
	editor.name.SetText("CLI v4")
	editor.description.SetText("", false)
	editor.teamID.SetText("team-2")
	editor.save()
	waitForCondition(t, time.Second, func() bool { return updateCalls.Load() == 3 })
	waitForRoadmapReload(t, app, &reloadCalls, 4)
	inputMu.Lock()
	gotUpdate = updateInput
	inputMu.Unlock()
	if gotUpdate.Description == nil || *gotUpdate.Description != "" || gotUpdate.TeamID != nil || gotUpdate.TeamIDs == nil || len(*gotUpdate.TeamIDs) != 1 || (*gotUpdate.TeamIDs)[0] != "team-2" {
		t.Fatalf("explicit project team set input = %+v, want team-2 TeamIDs", gotUpdate)
	}
}

func TestAppRoadmapCreateProjectAssociatesWithInitiativeAfterCreate(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var order []string
	var orderMu sync.Mutex
	var createInput linearapi.CreateProjectInput
	var associateInput linearapi.CreateInitiativeToProjectInput
	var reloadCalls atomic.Int32
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		return nil, nil
	}
	app.createProjectFunc = func(_ context.Context, input linearapi.CreateProjectInput) (linearapi.Project, error) {
		orderMu.Lock()
		order = append(order, "create")
		createInput = input
		orderMu.Unlock()
		return linearapi.Project{ID: "created-project", TeamID: "team-1"}, nil
	}
	app.createInitiativeToProjectFunc = func(_ context.Context, input linearapi.CreateInitiativeToProjectInput) error {
		orderMu.Lock()
		order = append(order, "associate")
		associateInput = input
		orderMu.Unlock()
		return nil
	}

	app.roadmapActions().OnCreateProject("initiative-1")
	app.roadmapEditor.name.SetText("CLI")
	app.roadmapEditor.save()
	waitForRoadmapReload(t, app, &reloadCalls, 1)
	orderMu.Lock()
	gotOrder := append([]string(nil), order...)
	gotCreate := createInput
	gotAssociate := associateInput
	orderMu.Unlock()
	if len(gotOrder) != 2 || gotOrder[0] != "create" || gotOrder[1] != "associate" {
		t.Fatalf("project mutation order = %#v, want create then associate", gotOrder)
	}
	if gotCreate.Name != "CLI" || gotCreate.TeamID != "team-1" {
		t.Fatalf("create project input = %+v, want CLI/team-1", gotCreate)
	}
	if gotAssociate.InitiativeID != "initiative-1" || gotAssociate.ProjectID != "created-project" {
		t.Fatalf("associate input = %+v, want initiative-1/created-project", gotAssociate)
	}
}

func TestAppRoadmapCreateProjectAssociationFailureDoesNotRetryOrReload(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var createCalls, associateCalls, reloadCalls atomic.Int32
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		return nil, nil
	}
	app.createProjectFunc = func(context.Context, linearapi.CreateProjectInput) (linearapi.Project, error) {
		createCalls.Add(1)
		return linearapi.Project{ID: "created-project"}, nil
	}
	app.createInitiativeToProjectFunc = func(context.Context, linearapi.CreateInitiativeToProjectInput) error {
		associateCalls.Add(1)
		return errors.New("association denied")
	}

	app.roadmapActions().OnCreateProject("initiative-1")
	app.roadmapEditor.name.SetText("CLI")
	app.roadmapEditor.save()
	waitForRoadmapRunner(t, app)
	var status string
	workspaceUIRead(app, func() { status = app.statusBar.GetText(true) })
	if createCalls.Load() != 1 || associateCalls.Load() != 1 || reloadCalls.Load() != 0 {
		t.Fatalf("association failure counts create=%d associate=%d reload=%d, want 1/1/0", createCalls.Load(), associateCalls.Load(), reloadCalls.Load())
	}
	if !strings.Contains(strings.ToLower(status), "project created") || !strings.Contains(strings.ToLower(status), "association denied") {
		t.Fatalf("association failure status = %q, want truthful project-created/association error", status)
	}
}

func TestAppRoadmapDestructiveActionsConfirmAndCancel(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	var archiveCalls, deleteCalls atomic.Int32
	var reloadCalls atomic.Int32
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloadCalls.Add(1)
		return []linearapi.Initiative{{
			ID: "initiative-1", Name: "Platform",
			Projects: []linearapi.Project{{ID: "project-1", Name: "CLI", TeamID: "team-1"}},
		}}, nil
	}
	app.archiveInitiativeFunc = func(context.Context, string) error {
		archiveCalls.Add(1)
		return nil
	}
	app.deleteProjectFunc = func(context.Context, string) error {
		deleteCalls.Add(1)
		return nil
	}
	workspaceUIRead(app, func() {
		app.roadmapView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	})
	if !app.roadmapView.ArchivePending() || archiveCalls.Load() != 0 {
		t.Fatalf("archive confirmation state = pending:%v calls:%d", app.roadmapView.ArchivePending(), archiveCalls.Load())
	}
	workspaceUIRead(app, func() { app.roadmapView.CancelArchive() })
	if archiveCalls.Load() != 0 {
		t.Fatalf("canceled archive calls = %d, want zero", archiveCalls.Load())
	}
	workspaceUIRead(app, func() {
		app.roadmapView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
		app.roadmapView.ConfirmArchive()
	})
	waitForRoadmapRunner(t, app)
	waitForRoadmapReload(t, app, &reloadCalls, 1)
	if archiveCalls.Load() != 1 {
		t.Fatalf("confirmed archive calls = %d, want one", archiveCalls.Load())
	}
	workspaceUIRead(app, func() {
		app.roadmapView.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
		app.roadmapView.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
		app.roadmapView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	})
	if !app.roadmapView.ArchivePending() || deleteCalls.Load() != 0 {
		t.Fatalf("project delete confirmation = pending:%v calls:%d", app.roadmapView.ArchivePending(), deleteCalls.Load())
	}
	workspaceUIRead(app, func() { app.roadmapView.ConfirmArchive() })
	waitForRoadmapRunner(t, app)
	waitForRoadmapReload(t, app, &reloadCalls, 2)
	if deleteCalls.Load() != 1 {
		t.Fatalf("confirmed project delete calls = %d, want one", deleteCalls.Load())
	}
}

func TestAppRoadmapMutationReturnsImmediatelyAndDropsClosedResult(t *testing.T) {
	app := roadmapAppWithVisibleView(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var reloads atomic.Int32
	app.createInitiativeFunc = func(context.Context, linearapi.CreateInitiativeInput) (linearapi.Initiative, error) {
		calls.Add(1)
		close(started)
		<-release
		return linearapi.Initiative{ID: "created"}, nil
	}
	app.listInitiativesFunc = func(context.Context) ([]linearapi.Initiative, error) {
		reloads.Add(1)
		return nil, nil
	}
	app.roadmapActions().OnCreateInitiative()
	app.roadmapEditor.name.SetText("Blocked")
	done := make(chan struct{})
	go func() {
		app.roadmapEditor.save()
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("roadmap create callback did not start")
	}
	select {
	case <-done:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("roadmap editor save blocked on mutation")
	}
	app.roadmapView.Hide()
	close(release)
	waitForRoadmapRunner(t, app)
	if calls.Load() != 1 || reloads.Load() != 0 {
		t.Fatalf("closed roadmap mutation calls=%d reloads=%d, want 1/0", calls.Load(), reloads.Load())
	}

	app.roadmapView.Show(RoadmapViewOptions{})
	app.createInitiativeFunc = func(context.Context, linearapi.CreateInitiativeInput) (linearapi.Initiative, error) {
		return linearapi.Initiative{}, errors.New("create failed")
	}
	app.roadmapActions().OnCreateInitiative()
	app.roadmapEditor.name.SetText("Failure")
	app.roadmapEditor.save()
	waitForRoadmapRunner(t, app)
	var status string
	workspaceUIRead(app, func() { status = app.statusBar.GetText(true) })
	if !strings.Contains(status, "create failed") || reloads.Load() != 0 {
		t.Fatalf("failed roadmap mutation status=%q reloads=%d", status, reloads.Load())
	}
}
