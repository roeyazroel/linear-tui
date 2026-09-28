package tui

import (
	"testing"

	"github.com/rivo/tview"
)

func TestLegacyTViewBordersUseRoundedCorners(t *testing.T) {
	want := map[string]rune{
		"TopLeft":          '╭',
		"TopRight":         '╮',
		"BottomLeft":       '╰',
		"BottomRight":      '╯',
		"TopLeftFocus":     '╭',
		"TopRightFocus":    '╮',
		"BottomLeftFocus":  '╰',
		"BottomRightFocus": '╯',
	}

	got := map[string]rune{
		"TopLeft":          tview.Borders.TopLeft,
		"TopRight":         tview.Borders.TopRight,
		"BottomLeft":       tview.Borders.BottomLeft,
		"BottomRight":      tview.Borders.BottomRight,
		"TopLeftFocus":     tview.Borders.TopLeftFocus,
		"TopRightFocus":    tview.Borders.TopRightFocus,
		"BottomLeftFocus":  tview.Borders.BottomLeftFocus,
		"BottomRightFocus": tview.Borders.BottomRightFocus,
	}

	for name, wantRune := range want {
		if gotRune := got[name]; gotRune != wantRune {
			t.Errorf("tview.Borders.%s = %q, want %q", name, gotRune, wantRune)
		}
	}
}
