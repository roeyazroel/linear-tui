package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

// Run the real application loop against an empty local workspace. Frames are
// captured on the UI goroutine so assertions never race the terminal renderer.
func startStartupAcceptanceApp(t *testing.T, check func(context.Context) string) (*App, <-chan string, <-chan struct{}, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body strings.Builder
		_, _ = io.Copy(&body, r.Body)
		query := body.String()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(query, "viewer"):
			_, _ = fmt.Fprint(w, `{"data":{"viewer":{"id":"user","name":"Tester","displayName":"Tester","email":"test@example.com"}}}`)
		case strings.Contains(query, "favorites"):
			_, _ = fmt.Fprint(w, `{"data":{"favorites":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`)
		case strings.Contains(query, "teams"):
			_, _ = fmt.Fprint(w, `{"data":{"teams":{"nodes":[]}}}`)
		default:
			t.Errorf("unexpected startup query: %s", query)
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	cfg := config.Config{APIEndpoint: server.URL, Timeout: time.Second, CacheTTL: time.Minute, PageSize: 10, Theme: config.ThemeCatppuccinMocha}
	app := NewApp(linearapi.NewClient(linearapi.ClientConfig{Endpoint: server.URL, Timeout: time.Second}), cfg, nil)
	app.fetchIssuesPage = func(context.Context, linearapi.FetchIssuesParams, *string) (linearapi.IssuePage, error) {
		return linearapi.IssuePage{}, nil
	}
	refreshed := installRefreshCompletionHook(app)
	app.SetStartupUpdateCheck(check)
	frames := make(chan string, 128)
	app.app.SetAfterDrawFunc(func(screen tcell.Screen) {
		width, height := screen.Size()
		var line strings.Builder
		for x := 0; x < width; x++ {
			cell, _, _ := screen.Get(x, height-1)
			line.WriteString(cell)
		}
		select {
		case frames <- line.String():
		default:
		}
	})
	screen := tcell.NewSimulationScreen("UTF-8")
	app.app.SetScreen(screen)
	screen.SetSize(120, 30)
	finished := make(chan error, 1)
	go func() { finished <- app.Run() }()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) {
			return
		}
		app.app.Stop()
		select {
		case err := <-finished:
			if err != nil {
				t.Errorf("application stopped with error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("application did not stop")
		}
	}
	t.Cleanup(stop)
	waitForRefreshCompletion(t, refreshed)
	return app, frames, refreshed, stop
}

func TestStartupNoticeArrivesWhileIdleAndChecksOnlyOnce(t *testing.T) {
	const notice = "New linear-tui version v9.0.0 available"
	var checks atomic.Int32
	release := make(chan struct{})
	app, frames, refreshed, _ := startStartupAcceptanceApp(t, func(ctx context.Context) string {
		checks.Add(1)
		select {
		case <-release:
			return notice
		case <-ctx.Done():
			return ""
		}
	})
	// Initial loading completed while the check was blocked. Drain its frames;
	// nothing below sends user input or requests another draw before the notice.
	app.app.QueueUpdateDraw(func() {})
	for len(frames) > 0 {
		<-frames
	}
	close(release)
	deadline := time.After(2 * time.Second)
	waitForNotice := func() {
		t.Helper()
		for {
			select {
			case line := <-frames:
				if strings.HasPrefix(strings.TrimSpace(line), notice) {
					return
				}
			case <-deadline:
				t.Fatal("startup notice was not visibly drawn while idle")
			}
		}
	}
	waitForNotice()
	app.app.QueueUpdateDraw(func() { app.applySettings(app.config) })
	app.refreshIssues()
	waitForRefreshCompletion(t, refreshed)
	waitForNotice()
	if checks.Load() != 1 {
		t.Fatalf("startup checks = %d, want exactly one after settings and refresh", checks.Load())
	}
}

func TestStartupCheckCancelledWhenAppCloses(t *testing.T) {
	canceled := make(chan struct{})
	app, _, _, stop := startStartupAcceptanceApp(t, func(ctx context.Context) string {
		<-ctx.Done()
		close(canceled)
		return ""
	})
	_ = app
	stop()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("startup check was not canceled on shutdown")
	}
}
