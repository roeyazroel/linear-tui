package tui

import "github.com/rivo/tview"

// composeShell preserves the original fixed three-pane tview composition.
func (a *App) composeShell() {
	if a == nil || a.mainLayout == nil {
		return
	}
	content := tview.NewFlex().
		AddItem(a.navigationTree, 0, 2, true).
		AddItem(a.issuesColumn, 0, 5, false).
		AddItem(a.detailsView, 0, 3, false)
	content.SetBackgroundColor(a.theme.Background)
	a.mainLayout.Clear().SetDirection(tview.FlexRow).
		AddItem(content, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)
}
