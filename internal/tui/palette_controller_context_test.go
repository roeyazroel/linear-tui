package tui

import "testing"

// TestPaletteIssueContextRanking verifies issue-scoped commands rank first
// when opened from an issue pane and last otherwise.
func TestPaletteIssueContextRanking(t *testing.T) {
	commands := []Command{
		{ID: "refresh", Title: "Refresh issues"},
		{ID: "copy_id", Title: "Copy issue ID"},
		{ID: "settings", Title: "Settings"},
		{ID: "archive", Title: "Archive issue"},
	}
	pc := NewPaletteController(commands)

	pc.SetIssueContext(true)
	filtered := pc.Filtered()
	if filtered[0].ID != "copy_id" || filtered[1].ID != "archive" {
		t.Errorf("issue context order = %v", []string{filtered[0].ID, filtered[1].ID})
	}

	pc.SetIssueContext(false)
	filtered = pc.Filtered()
	if filtered[0].ID != "refresh" || filtered[1].ID != "settings" {
		t.Errorf("view context order = %v", []string{filtered[0].ID, filtered[1].ID})
	}
}
