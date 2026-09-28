package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func TestStatusBarRendersActionMessageBeforeHelpAndStartupNotice(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{}, nil)
	app.focusedPane = FocusIssues
	app.startupNotice = "Update available: v9.9.9"
	app.statusMessage = "Archive: 1 succeeded, 1 failed"
	app.updateStatusBar()

	if drawn := drawStatusBar(t, app, 167); !strings.Contains(drawn, app.statusMessage) {
		t.Fatalf("actionable status message is not visible at terminal width: %q", drawn)
	}
}

func TestStatusBarRendersIdleStartupNotice(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{}, nil)
	app.startupNotice = "Update available: v9.9.9"
	app.updateStatusBar()

	if drawn := drawStatusBar(t, app, 167); !strings.Contains(drawn, app.startupNotice) {
		t.Fatalf("idle startup notice is not visible at terminal width: %q", drawn)
	}
}

func drawStatusBar(t *testing.T, app *App, width int) string {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(width, 1)
	app.statusBar.SetRect(0, 0, width, 1)
	app.statusBar.Draw(screen)

	var drawn strings.Builder
	for column := 0; column < width; column++ {
		text, _, _ := screen.Get(column, 0)
		drawn.WriteString(text)
	}
	return drawn.String()
}
