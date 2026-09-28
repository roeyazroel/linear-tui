package tui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestSavedViewsReachabilityAndStaleReload(t *testing.T) {
	for _, mode := range []string{"", ""} {
		t.Run(mode, func(t *testing.T) {
			app := newWorkspaceViewsIntegrationApp(t, mode)
			firstStarted := make(chan struct{})
			releaseFirst := make(chan struct{})
			var calls atomic.Int32
			app.listCustomViewsFunc = func(ctx context.Context) ([]linearapi.CustomView, error) {
				if calls.Add(1) == 1 {
					close(firstStarted)
					<-releaseFirst
					return []linearapi.CustomView{{ID: "stale", Name: "Stale"}}, nil
				}
				return []linearapi.CustomView{{ID: "fresh", Name: "Fresh"}}, nil
			}

			app.openSavedViews()
			<-firstStarted
			app.openSavedViews()
			close(releaseFirst)
			waitForCondition(t, time.Second, func() bool {
				matched := false
				workspaceUIRead(app, func() {
					matched = app.savedViewsModal != nil && app.savedViewsModal.SelectedID() == "fresh"
				})
				return matched
			})
			visible := false
			workspaceUIRead(app, func() { visible = app.pages.HasPage(savedViewsPageName) })
			if !visible {
				t.Fatal("saved views manager page is not visible")
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("list calls = %d, want 2", got)
			}
		})
	}
}

func TestSavedViewsMutationErrorRetainsManager(t *testing.T) {
	app := newWorkspaceViewsIntegrationApp(t, "")
	app.savedViewsModal.Show(app.savedViewsOptions([]linearapi.CustomView{{ID: "view-1", Name: "Open"}}, false, nil))
	app.updateCustomViewFunc = func(context.Context, string, linearapi.UpdateCustomViewInput) (linearapi.CustomView, error) {
		return linearapi.CustomView{}, errors.New("update failed")
	}
	app.savedViewsModal.options.OnEdit(SavedViewEditorValues{ID: "view-1", Name: "Renamed", FilterJSON: `{}`})
	waitForCondition(t, time.Second, func() bool {
		retained := false
		workspaceUIRead(app, func() {
			retained = app.pages.HasPage(savedViewsPageName) && app.savedViewsModal.StatusText() != ""
		})
		return retained
	})
}

func TestSavedViewsModalAppUpdateOmitsUnchangedFieldsOnWire(t *testing.T) {
	var request map[string]interface{}
	requestReceived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode GraphQL request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"customViewUpdate":{"success":true,"customView":{"id":"view-1","name":"Renamed","description":"Description","color":"#5E6AD2","filterData":{},"shared":false,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"}}}}`))
		close(requestReceived)
	}))
	defer server.Close()

	client := linearapi.NewClient(linearapi.ClientConfig{Token: "test-token", Endpoint: server.URL})
	app := NewApp(client, config.Config{
		Theme:          config.DefaultTheme,
		Density:        config.DefaultDensity,
		PageSize:       1,
		CacheTTL:       time.Minute,
		SearchDebounce: time.Millisecond,
		AgentProvider:  config.DefaultAgentProvider,
		AgentSandbox:   config.DefaultAgentSandbox,
	}, nil)
	app.queueUpdateDraw = func(f func()) { f() }
	app.listCustomViewsFunc = func(context.Context) ([]linearapi.CustomView, error) { return nil, nil }
	app.savedViewsModal.Show(app.savedViewsOptions([]linearapi.CustomView{{
		ID: "view-1", Name: "Original", Description: "Description", Color: "#5E6AD2", FilterJSON: `{"state":{"name":{"eq":"Todo"}}}`,
	}}, false, nil))
	app.savedViewsModal.OpenEdit()
	app.savedViewsModal.EditorName.SetText("Renamed")
	app.savedViewsModal.SaveEditor()

	select {
	case <-requestReceived:
	case <-time.After(time.Second):
		t.Fatal("saved-view update did not reach API")
	}
	variables, _ := request["variables"].(map[string]interface{})
	input, _ := variables["input"].(map[string]interface{})
	if input["name"] != "Renamed" {
		t.Fatalf("wire input name = %#v, want Renamed", input["name"])
	}
	for _, field := range []string{"description", "color", "filterData"} {
		if _, ok := input[field]; ok {
			t.Fatalf("name-only modal update included %s: %#v", field, input)
		}
	}
}
