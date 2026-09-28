package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newRoadmapViewTestApp(mode string) *App {
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

func roadmapViewTestData() RoadmapViewOptions {
	return RoadmapViewOptions{
		Initiatives: []linearapi.Initiative{{
			ID:   "initiative-1",
			Name: "Platform",
			Projects: []linearapi.Project{
				{ID: "project-1", Name: "CLI"},
				{ID: "project-2", Name: "API"},
			},
		}},
		ProjectUpdates: map[string][]linearapi.ProjectUpdate{
			"project-1": {{ID: "update-1", ProjectID: "project-1", Body: "Needs review", Health: "atRisk", Author: linearapi.User{DisplayName: "Ada"}}},
		},
	}
}

func TestRoadmapViewShowsHierarchyAndStableSelection(t *testing.T) {
	app := newRoadmapViewTestApp("")
	view := NewRoadmapView(app)
	view.Show(roadmapViewTestData())

	if !app.pages.HasPage("roadmap") {
		t.Fatal("Show did not add roadmap page")
	}
	if view.List == nil || view.List.GetItemCount() != 1 {
		t.Fatalf("initial roadmap rows = %d, want collapsed initiative", view.List.GetItemCount())
	}
	if !strings.Contains(view.List.GetItemText(0)) {
		t.Fatal("initiative row is empty")
	}
	if view.SelectedID() != "initiative-1" {
		t.Fatalf("initial selected ID = %q, want initiative-1", view.SelectedID())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if view.List.GetItemCount() != 3 {
		t.Fatalf("expanded initiative rows = %d, want 3", view.List.GetItemCount())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if view.SelectedID() != "project-1" {
		t.Fatalf("selected project ID = %q, want project-1", view.SelectedID())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if view.List.GetItemCount() != 4 {
		t.Fatalf("expanded project rows = %d, want 4", view.List.GetItemCount())
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if view.SelectedID() != "update-1" {
		t.Fatalf("selected update ID = %q, want update-1", view.SelectedID())
	}
	if details := view.Details.GetText(true); !strings.Contains(details, "Needs review") || !strings.Contains(details, "atRisk") {
		t.Fatalf("update details = %q, want body and health", details)
	}
}

func TestRoadmapViewLoadingEmptyAndErrorStates(t *testing.T) {
	app := newRoadmapViewTestApp("")
	view := NewRoadmapView(app)
	view.Show(RoadmapViewOptions{Loading: true})
	got, _ := view.List.GetItemText(0)
	if !strings.Contains(strings.ToLower(got), "loading") {
		t.Fatalf("loading row = %q", got)
	}
	view.Show(RoadmapViewOptions{})
	got, _ = view.List.GetItemText(0)
	if !strings.Contains(strings.ToLower(got), "no initiatives") {
		t.Fatalf("empty row = %q", got)
	}
	view.Show(RoadmapViewOptions{Error: errors.New("roadmap unavailable")})
	got, _ = view.List.GetItemText(0)
	if !strings.Contains(got, "roadmap unavailable") {
		t.Fatalf("error row = %q", got)
	}
}

func TestRoadmapViewGatesRowActionsOutsideDataState(t *testing.T) {
	states := []struct {
		name string
		opts RoadmapViewOptions
	}{
		// Keep stale hierarchy data attached to loading/error snapshots so row
		// actions must honor the visible state instead of old backing rows.
		{name: "loading", opts: func() RoadmapViewOptions {
			opts := roadmapViewTestData()
			opts.Loading = true
			return opts
		}()},
		{name: "error", opts: func() RoadmapViewOptions {
			opts := roadmapViewTestData()
			opts.Error = errors.New("roadmap unavailable")
			return opts
		}()},
		{name: "empty", opts: RoadmapViewOptions{}},
	}

	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			for _, key := range []rune{'c', 'e', 'a', 'd'} {
				app := newRoadmapViewTestApp("")
				var creates, edits, archives, deletes int
				state.opts.Actions = RoadmapViewActions{
					OnCreateProject:        func(string) { creates++ },
					OnCreateProjectUpdate:  func(string) { creates++ },
					OnEditInitiative:       func(linearapi.Initiative) { edits++ },
					OnEditProject:          func(linearapi.Project) { edits++ },
					OnEditProjectUpdate:    func(linearapi.ProjectUpdate) { edits++ },
					OnArchiveInitiative:    func(linearapi.Initiative) { archives++ },
					OnArchiveProjectUpdate: func(linearapi.ProjectUpdate) { archives++ },
					OnDeleteInitiative:     func(linearapi.Initiative) { deletes++ },
					OnDeleteProject:        func(linearapi.Project) { deletes++ },
				}
				view := NewRoadmapView(app)
				view.Show(state.opts)
				view.HandleKey(tcell.NewEventKey(tcell.KeyRune, key, tcell.ModNone))

				if creates != 0 || edits != 0 || archives != 0 || deletes != 0 {
					t.Errorf("%s key %q callbacks = create:%d edit:%d archive:%d delete:%d, want all zero", state.name, key, creates, edits, archives, deletes)
				}
				if view.ArchivePending() || app.pages.HasPage("confirmation") {
					t.Errorf("%s key %q opened confirmation: pending:%v page:%v", state.name, key, view.ArchivePending(), app.pages.HasPage("confirmation"))
				}
			}

			view := NewRoadmapView(newRoadmapViewTestApp(""))
			view.Show(state.opts)
			help := strings.ToLower(view.Help.GetText(true))
			for _, forbidden := range []string{"expand", "c: create", "e: edit", "archive", "d: delete"} {
				if strings.Contains(help, forbidden) {
					t.Fatalf("%s-state help advertises unavailable %q action: %q", state.name, forbidden, help)
				}
			}
			if !strings.Contains(help, "new initiative") || !strings.Contains(help, "close") {
				t.Fatalf("%s-state help omits safe global actions: %q", state.name, help)
			}

			created := 0
			safeOptions := state.opts
			safeOptions.Actions = RoadmapViewActions{OnCreateInitiative: func() { created++ }}
			safeView := NewRoadmapView(newRoadmapViewTestApp(""))
			safeView.Show(safeOptions)
			safeView.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
			if created != 1 {
				t.Fatalf("%s-state new initiative calls = %d, want 1", state.name, created)
			}
			safeView.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
			if safeView.app.pages.HasPage(roadmapPageName) {
				t.Fatalf("%s-state Esc did not hide roadmap", state.name)
			}
		})
	}
}

func TestRoadmapViewDropsPendingArchiveWhenDataBecomesUnavailable(t *testing.T) {
	app := newRoadmapViewTestApp("")
	archived := 0
	view := NewRoadmapView(app)
	options := roadmapViewTestData()
	options.Actions = RoadmapViewActions{OnArchiveInitiative: func(linearapi.Initiative) { archived++ }}
	view.Show(options)
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !view.ArchivePending() || !app.pages.HasPage("confirmation") {
		t.Fatal("archive did not enter confirmation state")
	}

	view.options.Loading = true
	view.refreshRows()
	view.ConfirmArchive()
	if archived != 0 {
		t.Fatalf("archive callback after loading transition = %d, want zero", archived)
	}
	if view.ArchivePending() || app.pages.HasPage("confirmation") {
		t.Fatalf("loading transition left pending archive: pending:%v confirmation:%v", view.ArchivePending(), app.pages.HasPage("confirmation"))
	}
	if help := strings.ToLower(view.Help.GetText(true)); strings.Contains(help, "archive") {
		t.Fatalf("loading-state help advertises archive after transition: %q", help)
	}
}

func TestRoadmapViewConfirmationCancellationOwnsPendingState(t *testing.T) {
	app := newRoadmapViewTestApp("")
	var archived int
	view := NewRoadmapView(app)
	options := roadmapViewTestData()
	options.Actions = RoadmapViewActions{OnArchiveInitiative: func(linearapi.Initiative) { archived++ }}
	view.Show(options)
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !view.ArchivePending() || !app.pages.HasPage("confirmation") {
		t.Fatal("archive did not enter confirmation state")
	}
	if got := app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatalf("global Escape returned event %v, want consumed", got)
	}
	if view.ArchivePending() || app.pages.HasPage("confirmation") {
		t.Fatalf("global Escape left pending state: pending:%v confirmation:%v", view.ArchivePending(), app.pages.HasPage("confirmation"))
	}
	view.ConfirmArchive()
	if archived != 0 {
		t.Fatalf("archive callback after global Escape = %d, want zero", archived)
	}

	view.Show(options)
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	app.confirmationModal.Show("Other action", "Keep this confirmation", "Confirm", nil)
	other := app.pages.GetPage("confirmation")
	if view.ArchivePending() {
		t.Fatal("replacing confirmation left roadmap action pending")
	}
	if other == nil || app.pages.GetPage("confirmation") != other {
		t.Fatal("replacement confirmation page was not preserved")
	}
	view.Hide()
	if app.pages.GetPage("confirmation") != other {
		t.Fatal("roadmap Hide removed another modal's confirmation")
	}
	app.confirmationModal.Hide()
}

func TestRoadmapViewActionsAndArchiveConfirmation(t *testing.T) {
	app := newRoadmapViewTestApp("")
	app.queueUpdateDraw = func(f func()) { f() }
	var created string
	var edited string
	var archived string
	view := NewRoadmapView(app)
	options := roadmapViewTestData()
	options.Actions = RoadmapViewActions{
		OnCreateProjectUpdate: func(projectID string) { created = projectID },
		OnEditProjectUpdate:   func(update linearapi.ProjectUpdate) { edited = update.ID },
		OnArchiveProjectUpdate: func(update linearapi.ProjectUpdate) {
			archived = update.ID
		},
	}
	view.Show(options)
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)) // expand initiative
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))  // project
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone)) // expand project
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))  // update
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if edited != "update-1" {
		t.Fatalf("edit callback ID = %q, want update-1", edited)
	}
	if archived != "" {
		t.Fatalf("archive callback ran before confirmation: %q", archived)
	}
	if !view.ArchivePending() {
		t.Fatal("archive action did not enter confirmation state")
	}
	view.ConfirmArchive()
	if archived != "update-1" {
		t.Fatalf("archive callback ID = %q, want update-1", archived)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	if created != "project-1" {
		t.Fatalf("create callback project = %q, want project-1", created)
	}
}

func TestRoadmapViewInitiativeAndProjectActions(t *testing.T) {
	app := newRoadmapViewTestApp("")
	app.queueUpdateDraw = func(f func()) { f() }
	var createdInitiative bool
	var createdProject string
	var editedInitiative string
	var editedProject string
	var archivedInitiative string
	var deletedInitiative string
	var deletedProject string
	view := NewRoadmapView(app)
	options := roadmapViewTestData()
	options.Actions = RoadmapViewActions{
		OnCreateInitiative: func() { createdInitiative = true },
		OnEditInitiative: func(initiative linearapi.Initiative) {
			editedInitiative = initiative.ID
		},
		OnArchiveInitiative: func(initiative linearapi.Initiative) {
			archivedInitiative = initiative.ID
		},
		OnDeleteInitiative: func(initiative linearapi.Initiative) {
			deletedInitiative = initiative.ID
		},
		OnCreateProject: func(initiativeID string) { createdProject = initiativeID },
		OnEditProject: func(project linearapi.Project) {
			editedProject = project.ID
		},
		OnDeleteProject: func(project linearapi.Project) { deletedProject = project.ID },
	}
	view.Show(options)

	// Initiative actions are reachable while collapsed. n creates a new
	// initiative, c creates a child project, while e edits the initiative.
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if !createdInitiative || createdProject != "initiative-1" || editedInitiative != "initiative-1" {
		t.Fatalf("initiative callbacks = new %v/create project %q/edit %q, want true/initiative-1/initiative-1", createdInitiative, createdProject, editedInitiative)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !view.ArchivePending() || archivedInitiative != "" {
		t.Fatalf("initiative archive state = pending %v/callback %q, want pending without callback", view.ArchivePending(), archivedInitiative)
	}
	view.ConfirmArchive()
	if archivedInitiative != "initiative-1" {
		t.Fatalf("archived initiative = %q, want initiative-1", archivedInitiative)
	}

	// d uses the same explicit confirmation gate for permanent initiative
	// deletion.
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if !view.ArchivePending() || deletedInitiative != "" {
		t.Fatalf("initiative delete state = pending %v/callback %q, want pending without callback", view.ArchivePending(), deletedInitiative)
	}
	view.ConfirmArchive()
	if deletedInitiative != "initiative-1" {
		t.Fatalf("deleted initiative = %q, want initiative-1", deletedInitiative)
	}

	// Expand and select the first project. Project e edits the selected
	// project, and a/d both route through the schema-backed delete hook.
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if editedProject != "project-1" {
		t.Fatalf("edited project = %q, want project-1", editedProject)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone))
	if !view.ArchivePending() || deletedProject != "" {
		t.Fatalf("project delete state = pending %v/callback %q, want pending without callback", view.ArchivePending(), deletedProject)
	}
	view.ConfirmArchive()
	if deletedProject != "project-1" {
		t.Fatalf("deleted project = %q, want project-1", deletedProject)
	}
	view.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'c', tcell.ModNone))
	if !strings.Contains(strings.ToLower(view.Help.GetText(true)), "delete") {
		t.Fatalf("help = %q, want contextual delete guidance", view.Help.GetText(true))
	}
}

func TestRoadmapViewHealthChoicesAndComposition(t *testing.T) {
	choices := RoadmapHealthChoices()
	if len(choices) != 3 || choices[0] != linearapi.ProjectUpdateHealthOnTrack || choices[1] != linearapi.ProjectUpdateHealthAtRisk || choices[2] != linearapi.ProjectUpdateHealthOffTrack {
		t.Fatalf("health choices = %#v, want only onTrack/atRisk/offTrack", choices)
	}
	classicApp := newRoadmapViewTestApp("")
	classic := NewRoadmapView(classicApp)
	if classic.GetModal() == nil || classic.GetModal().GetItemCount() != 3 {
		t.Fatalf("classic shell shape = %#v", classic.GetModal())
	}
}

func TestRoadmapViewHideAndSelectionSurviveRefresh(t *testing.T) {
	app := newRoadmapViewTestApp("")
	view := NewRoadmapView(app)
	view.Show(roadmapViewTestData())
	view.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	view.HandleKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone))
	if view.SelectedID() != "project-1" {
		t.Fatalf("selected before refresh = %q", view.SelectedID())
	}
	view.SetData(roadmapViewTestData())
	if view.SelectedID() != "project-1" {
		t.Fatalf("selected after refresh = %q, want stable project-1", view.SelectedID())
	}
	view.Hide()
	if app.pages.HasPage("roadmap") {
		t.Fatal("Hide left roadmap page visible")
	}
}
