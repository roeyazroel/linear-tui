package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

type workspaceShell interface {
	tview.Primitive
	GetItem(int) tview.Primitive
}

func TestWorkspaceShellsClampToNarrowTerminal(t *testing.T) {
	tests := []struct {
		name           string
		preferredWidth int
		title          string
		firstText      string
		newShell       func() workspaceShell
	}{
		{
			name:           "inbox",
			preferredWidth: 90,
			title:          "Inbox / Notifications",
			firstText:      "Review issue",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				view := NewInboxView(app)
				view.Show(InboxViewOptions{Notifications: inboxViewNotifications()}, InboxViewCallbacks{})
				return view.GetModal()
			},
		},
		{
			name:           "saved views",
			preferredWidth: 108,
			title:          "Saved Views",
			firstText:      "Open bugs",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				view := NewSavedViewsModal(app)
				view.Show(SavedViewsModalOptions{Views: savedViewsModalTestViews()})
				return view.GetModal()
			},
		},
		{
			name:           "roadmap",
			preferredWidth: 108,
			title:          "Roadmap",
			firstText:      "Platform",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				view := NewRoadmapView(app)
				view.Show(RoadmapViewOptions{Initiatives: []linearapi.Initiative{{ID: "initiative-1", Name: "Platform"}}})
				return view.GetModal()
			},
		},
		{
			name:           "comments",
			preferredWidth: 96,
			title:          "Comments / Activity",
			firstText:      "Root comment",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				view := NewCommentsModal(app)
				view.Show(commentsModalOptions(), CommentsModalCallbacks{})
				return view.GetModal()
			},
		},
		{
			name:           "issue editor",
			preferredWidth: 88,
			title:          "Edit Issue",
			firstText:      "E2E issue",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				view := NewIssueEditorModal(app)
				view.Show(IssueEditorOptions{Values: IssueEditorValues{IssueID: "issue-1", Title: "E2E issue"}}, nil)
				return view.GetModal()
			},
		},
		{
			name:           "keybindings",
			preferredWidth: 86,
			title:          "Keybindings",
			firstText:      "Quit",
			newShell: func() workspaceShell {
				app := newWorkspaceShellTestApp()
				app.settingsModal.Show()
				app.settingsModal.OpenKeybindingEditor()
				return app.settingsModal.keybindingEditor
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell := tt.newShell()
			screen := tcell.NewSimulationScreen("UTF-8")
			if err := screen.Init(); err != nil {
				t.Fatal(err)
			}
			defer screen.Fini()

			screen.SetSize(120, 30)
			shell.SetRect(0, 0, 120, 30)
			shell.Draw(screen)
			if _, _, width, _ := workspaceShellFrame(t, shell).GetRect(); width != tt.preferredWidth {
				t.Fatalf("wide frame width = %d, want preferred %d", width, tt.preferredWidth)
			}

			screen.SetSize(77, 30)
			screen.Clear()
			shell.SetRect(0, 0, 77, 30)
			shell.Draw(screen)
			x, y, width, height := workspaceShellFrame(t, shell).GetRect()
			if x < 0 || y < 0 || x+width > 77 || y+height > 30 {
				t.Fatalf("frame rect = (%d,%d %dx%d), outside 77x30 viewport", x, y, width, height)
			}
			for row := y + 1; row < y+height-1; row++ {
				for column := x + width; column < 77; column++ {
					text, _, _ := screen.Get(column, row)
					if text != "" && text != " " && text != "\x00" {
						t.Fatalf("shell painted %q outside right border at (%d,%d)", text, column, row)
					}
				}
			}
			drawn := workspaceShellScreenText(screen, 77, 30)
			for _, want := range []string{tt.title, tt.firstText} {
				if !strings.Contains(drawn, want) {
					t.Fatalf("narrow shell omits %q:\n%s", want, drawn)
				}
			}
		})
	}
}

func newWorkspaceShellTestApp() *App {
	return NewApp(&linearapi.Client{}, config.Config{Theme: config.DefaultTheme, Density: config.DefaultDensity}, nil)
}

func workspaceShellFrame(t *testing.T, shell workspaceShell) *tview.Flex {
	t.Helper()
	vertical, ok := shell.GetItem(1).(*tview.Flex)
	if !ok {
		t.Fatal("shell center is not a vertical flex")
	}
	frame, ok := vertical.GetItem(1).(*tview.Flex)
	if !ok {
		t.Fatal("shell content is not a flex")
	}
	return frame
}

func workspaceShellScreenText(screen tcell.SimulationScreen, width, height int) string {
	var text strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell, _, _ := screen.Get(x, y)
			text.WriteString(cell)
		}
		text.WriteByte('\n')
	}
	return text.String()
}
