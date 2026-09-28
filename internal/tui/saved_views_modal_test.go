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

func newSavedViewsModalTestApp(mode string) *App {
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

func savedViewsModalTestViews() []linearapi.CustomView {
	return []linearapi.CustomView{
		{ID: "view-1", Name: "Open bugs", Description: "Todo bugs", Color: "#ff0000", FilterJSON: `{"priority":{"eq":1}}`},
		{ID: "view-2", Name: "My work", Description: "Assigned to me", Color: "#00ff00"},
	}
}

func TestSavedViewsModalListApplyAndNavigation(t *testing.T) {
	app := newSavedViewsModalTestApp("")
	var applied string
	modal := NewSavedViewsModal(app)
	modal.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews(), OnApply: func(view linearapi.CustomView) { applied = view.ID }})
	if !app.pages.HasPage("saved_views") {
		t.Fatal("Show did not add saved_views page")
	}
	if modal.List.GetItemCount() != 2 || modal.SelectedID() != "view-1" {
		t.Fatalf("list count/selection = %d/%q, want 2/view-1", modal.List.GetItemCount(), modal.SelectedID())
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone))
	if modal.SelectedID() != "view-2" {
		t.Fatalf("j selected = %q, want view-2", modal.SelectedID())
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if applied != "view-2" {
		t.Fatalf("apply callback = %q, want view-2", applied)
	}
	if app.pages.HasPage("saved_views") {
		t.Fatal("Enter left saved views page visible")
	}
}

func TestSavedViewsModalStatesAndReload(t *testing.T) {
	app := newSavedViewsModalTestApp("")
	var reloads int
	modal := NewSavedViewsModal(app)
	modal.Show(SavedViewsModalOptions{Loading: true, OnReload: func() { reloads++ }})
	if got, _ := modal.List.GetItemText(0); !strings.Contains(strings.ToLower(got), "loading") {
		t.Fatalf("loading row = %q", got)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
	if reloads != 1 {
		t.Fatalf("reload calls = %d, want 1", reloads)
	}
	modal.Show(SavedViewsModalOptions{})
	if got, _ := modal.List.GetItemText(0); !strings.Contains(strings.ToLower(got), "no saved views") {
		t.Fatalf("empty row = %q", got)
	}
	modal.Show(SavedViewsModalOptions{Error: errors.New("views unavailable")})
	if got, _ := modal.List.GetItemText(0); !strings.Contains(got, "views unavailable") {
		t.Fatalf("error row = %q", got)
	}
}

func TestSavedViewsModalGatesRowActionsOutsideDataState(t *testing.T) {
	states := []struct {
		name string
		opts SavedViewsModalOptions
	}{
		// Keep stale data attached to loading/error snapshots so this proves the
		// visible non-data row, rather than an empty backing slice, controls input.
		{name: "loading", opts: SavedViewsModalOptions{Loading: true, Views: savedViewsModalTestViews()}},
		{name: "error", opts: SavedViewsModalOptions{Error: errors.New("views unavailable"), Views: savedViewsModalTestViews()}},
		{name: "empty", opts: SavedViewsModalOptions{}},
	}

	for _, state := range states {
		t.Run(state.name, func(t *testing.T) {
			for _, event := range []*tcell.EventKey{
				tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
				tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone),
				tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone),
			} {
				app := newSavedViewsModalTestApp("")
				var applied, edited, deleted int
				modal := NewSavedViewsModal(app)
				state.opts.OnApply = func(linearapi.CustomView) { applied++ }
				state.opts.OnEdit = func(SavedViewEditorValues) { edited++ }
				state.opts.OnDelete = func(linearapi.CustomView) { deleted++ }
				modal.Show(state.opts)
				modal.HandleKey(event)

				if applied != 0 || edited != 0 || deleted != 0 {
					t.Errorf("row callbacks in %s state for %v = apply:%d edit:%d delete:%d, want all zero", state.name, event, applied, edited, deleted)
				}
				if modal.EditorVisible() || modal.DeletePending() || app.pages.HasPage("confirmation") {
					t.Errorf("row action opened modal in %s state for %v: editor:%v pending:%v confirmation:%v", state.name, event, modal.EditorVisible(), modal.DeletePending(), app.pages.HasPage("confirmation"))
				}
			}

			app := newSavedViewsModalTestApp("")
			modal := NewSavedViewsModal(app)
			modal.Show(state.opts)
			help := strings.ToLower(modal.Help.GetText(true))
			for _, forbidden := range []string{"apply", "edit", "delete"} {
				if strings.Contains(help, forbidden) {
					t.Fatalf("%s-state help advertises unavailable %q action: %q", state.name, forbidden, help)
				}
			}
			for _, required := range []string{"reload", "close", "new"} {
				if !strings.Contains(help, required) {
					t.Fatalf("%s-state help omits safe %q action: %q", state.name, required, help)
				}
			}

			var reloads int
			state.opts.OnReload = func() { reloads++ }
			modal.Show(state.opts)
			modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'r', tcell.ModNone))
			if reloads != 1 {
				t.Fatalf("%s-state reload calls = %d, want 1", state.name, reloads)
			}
			modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
			if !modal.EditorVisible() {
				t.Fatalf("%s-state new action did not open editor", state.name)
			}
			modal.CancelEditor()
			modal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
			if app.pages.HasPage(savedViewsPageName) {
				t.Fatalf("%s-state Esc did not hide saved views", state.name)
			}
		})
	}
}

func TestSavedViewsModalDropsPendingDeleteWhenDataBecomesUnavailable(t *testing.T) {
	app := newSavedViewsModalTestApp("")
	deleted := 0
	modal := NewSavedViewsModal(app)
	modal.Show(SavedViewsModalOptions{
		Views:    savedViewsModalTestViews(),
		OnDelete: func(linearapi.CustomView) { deleted++ },
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if !modal.DeletePending() || !app.pages.HasPage("confirmation") {
		t.Fatal("delete did not enter confirmation state")
	}

	modal.options.Loading = true
	modal.refreshList()
	modal.ConfirmDelete()
	if deleted != 0 {
		t.Fatalf("delete callback after loading transition = %d, want zero", deleted)
	}
	if modal.DeletePending() || app.pages.HasPage("confirmation") {
		t.Fatalf("loading transition left pending delete: pending:%v confirmation:%v", modal.DeletePending(), app.pages.HasPage("confirmation"))
	}
	if help := strings.ToLower(modal.Help.GetText(true)); strings.Contains(help, "delete") {
		t.Fatalf("loading-state help advertises delete after transition: %q", help)
	}
}

func TestSavedViewsModalEditorValidationAndActions(t *testing.T) {
	app := newSavedViewsModalTestApp("")
	var created, edited, deleted string
	modal := NewSavedViewsModal(app)
	modal.Show(SavedViewsModalOptions{
		Views:    savedViewsModalTestViews(),
		OnCreate: func(values SavedViewEditorValues) { created = values.Name },
		OnEdit:   func(values SavedViewEditorValues) { edited = values.ID },
		OnDelete: func(view linearapi.CustomView) { deleted = view.ID },
	})
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'n', tcell.ModNone))
	if !modal.EditorVisible() {
		t.Fatal("n did not open editor")
	}
	modal.EditorName.SetText("New view")
	modal.EditorFilter.SetText("not json", true)
	modal.SaveEditor()
	if created != "" || !modal.EditorVisible() {
		t.Fatalf("invalid filter saved: created=%q editor=%v", created, modal.EditorVisible())
	}
	if !strings.Contains(strings.ToLower(modal.StatusText()), "filter") {
		t.Fatalf("validation status = %q, want filter error", modal.StatusText())
	}
	modal.EditorFilter.SetText(`{"state":{"name":{"eq":"Todo"}}}`, true)
	modal.SaveEditor()
	if created != "New view" {
		t.Fatalf("create values name = %q, want New view", created)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModNone))
	if !modal.EditorVisible() {
		t.Fatal("e did not open editor")
	}
	modal.SaveEditor()
	if edited != "view-1" {
		t.Fatalf("edit values ID = %q, want view-1", edited)
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
	if deleted != "" || !modal.DeletePending() {
		t.Fatalf("delete did not require confirmation: deleted=%q pending=%v", deleted, modal.DeletePending())
	}
	modal.ConfirmDelete()
	if deleted != "view-1" {
		t.Fatalf("delete callback ID = %q, want view-1", deleted)
	}
}

func TestSavedViewsModalEditFieldPresence(t *testing.T) {
	app := newSavedViewsModalTestApp("")
	var edited SavedViewEditorValues
	modal := NewSavedViewsModal(app)
	modal.Show(SavedViewsModalOptions{
		Views: savedViewsModalTestViews(),
		OnEdit: func(values SavedViewEditorValues) {
			edited = values
		},
	})

	modal.OpenEdit()
	modal.EditorName.SetText("Renamed")
	modal.EditorDescription.SetText("", true)
	modal.EditorColor.SetText(" ")
	modal.EditorFilter.SetText("", true)
	modal.SaveEditor()
	if edited.Description != linearapi.CustomViewFieldClear || edited.Color != linearapi.CustomViewFieldClear || edited.FilterJSON != linearapi.CustomViewFieldClear {
		t.Fatalf("cleared edit values = %#v, want explicit clear markers", edited)
	}

	modal.OpenEdit()
	modal.EditorName.SetText("Renamed again")
	modal.SaveEditor()
	if edited.Description != linearapi.CustomViewFieldUnchanged || edited.Color != linearapi.CustomViewFieldUnchanged || edited.FilterJSON != linearapi.CustomViewFieldUnchanged {
		t.Fatalf("untouched edit values = %#v, want unchanged markers", edited)
	}
}

func TestSavedViewsModalConfirmationOwnership(t *testing.T) {
	t.Run("global escape cancels owner on first press", func(t *testing.T) {
		app := newSavedViewsModalTestApp("")
		var deleted int
		modal := NewSavedViewsModal(app)
		modal.Show(SavedViewsModalOptions{
			Views:    savedViewsModalTestViews(),
			OnDelete: func(linearapi.CustomView) { deleted++ },
		})
		modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		if !modal.DeletePending() || !app.pages.HasPage("confirmation") {
			t.Fatal("delete did not enter confirmation state")
		}
		if got := app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
			t.Fatalf("global Escape returned event %v, want consumed", got)
		}
		if modal.DeletePending() || app.pages.HasPage("confirmation") {
			t.Fatalf("global Escape left pending state: pending:%v confirmation:%v", modal.DeletePending(), app.pages.HasPage("confirmation"))
		}
		modal.ConfirmDelete()
		if deleted != 0 {
			t.Fatalf("delete callback after global Escape = %d, want zero", deleted)
		}
	})

	t.Run("cancel button notifies owner exactly once", func(t *testing.T) {
		app := newSavedViewsModalTestApp("")
		var deleted int
		modal := NewSavedViewsModal(app)
		modal.Show(SavedViewsModalOptions{
			Views:    savedViewsModalTestViews(),
			OnDelete: func(linearapi.CustomView) { deleted++ },
		})
		modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		confirmation := app.confirmationModal.modal
		app.confirmationModal.handleButton(1, confirmation)
		if modal.DeletePending() || app.pages.HasPage("confirmation") {
			t.Fatalf("cancel button left pending state: pending:%v confirmation:%v", modal.DeletePending(), app.pages.HasPage("confirmation"))
		}
		if deleted != 0 {
			t.Fatalf("delete callback after cancel button = %d, want zero", deleted)
		}
		modal.CancelDelete()
		if deleted != 0 {
			t.Fatalf("delete callback after repeated cancel = %d, want zero", deleted)
		}
	})

	t.Run("cancel preserves another modal confirmation", func(t *testing.T) {
		app := newSavedViewsModalTestApp("")
		modal := NewSavedViewsModal(app)
		modal.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
		modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		owned := app.pages.GetPage("confirmation")
		if owned == nil {
			t.Fatal("delete did not create a confirmation page")
		}

		app.confirmationModal.Show("Other action", "Keep this confirmation", "Confirm", nil)
		other := app.pages.GetPage("confirmation")
		if other == nil || other == owned {
			t.Fatal("other confirmation did not replace the saved-view confirmation")
		}

		modal.CancelDelete()
		if !app.pages.HasPage("confirmation") || app.pages.GetPage("confirmation") != other {
			t.Fatal("cancel removed another modal's confirmation page")
		}
		if modal.DeletePending() {
			t.Fatal("cancel left saved-view delete pending")
		}
	})

	t.Run("hide preserves another modal confirmation", func(t *testing.T) {
		app := newSavedViewsModalTestApp("")
		modal := NewSavedViewsModal(app)
		modal.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
		modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		app.confirmationModal.Show("Other action", "Keep this confirmation", "Confirm", nil)
		other := app.pages.GetPage("confirmation")

		modal.Hide()
		if !app.pages.HasPage("confirmation") || app.pages.GetPage("confirmation") != other {
			t.Fatal("hide removed another modal's confirmation page")
		}
		if app.pages.HasPage(savedViewsPageName) {
			t.Fatal("hide left saved views page visible")
		}
	})

	t.Run("repeated show clears its own confirmation", func(t *testing.T) {
		app := newSavedViewsModalTestApp("")
		modal := NewSavedViewsModal(app)
		modal.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
		modal.HandleKey(tcell.NewEventKey(tcell.KeyRune, 'd', tcell.ModNone))
		if !app.pages.HasPage("confirmation") {
			t.Fatal("delete did not create a confirmation page")
		}

		modal.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
		if app.pages.HasPage("confirmation") {
			t.Fatal("repeated show left a stale saved-view confirmation page")
		}
		if modal.DeletePending() {
			t.Fatal("repeated show left saved-view delete pending")
		}
	})
}

func TestSavedViewsModalCompositionAndLifecycle(t *testing.T) {
	classicApp := newSavedViewsModalTestApp("")
	classic := NewSavedViewsModal(classicApp)
	if classic.GetModal() == nil || classic.GetModal().GetItemCount() != 3 {
		t.Fatalf("classic shell = %#v", classic.GetModal())
	}
	classic.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
	classic.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if classicApp.pages.HasPage("saved_views") {
		t.Fatal("Esc did not hide saved views")
	}
}
