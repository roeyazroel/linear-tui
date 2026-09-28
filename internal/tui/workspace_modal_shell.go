package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const workspaceModalScreenMargin = 4

// newResponsiveWorkspaceModal keeps a centered shell inside a resized
// terminal while retaining its preferred width on ordinary screens. The
// draw hook lives on the returned Flex so GetModal callers retain the same
// responsive behavior as app-mounted callers.
func newResponsiveWorkspaceModal(background tcell.Color, center tview.Primitive, preferredWidth int, onResize func(width int)) *tview.Flex {
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(center, preferredWidth, 0, true).
		AddItem(nil, 0, 1, false)
	modal.SetBackgroundColor(background)
	modal.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
		modalWidth := workspaceModalWidth(width, preferredWidth)
		modal.ResizeItem(center, modalWidth, 0)
		if onResize != nil {
			onResize(modalWidth)
		}
		return x, y, width, height
	})
	return modal
}

func workspaceModalWidth(screenWidth, preferredWidth int) int {
	if screenWidth <= 0 {
		return preferredWidth
	}
	width := screenWidth - workspaceModalScreenMargin
	if width < 1 {
		width = screenWidth
	}
	if width < preferredWidth {
		return width
	}
	return preferredWidth
}
