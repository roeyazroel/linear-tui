package tui

import (
	"fmt"

	"github.com/rivo/tview"
)

const (
	// Palette rows are intentionally kept compact so the command palette does
	// not take over the whole terminal when many commands are available.
	paletteModalMinHeight  = 8
	paletteModalMaxHeight  = 15
	paletteModalChromeRows = 2 // input and help rows
	paletteModalBorderRows = 2
)

// paletteModalHeight returns the total modal height, including its border.
// An available height of zero means the page has not been laid out yet.
func paletteModalHeight(commandCount, spacerLines, availableHeight int) int {
	if commandCount < 0 {
		commandCount = 0
	}
	if spacerLines < 0 {
		spacerLines = 0
	}

	contentHeight := commandCount + paletteModalChromeRows + (2 * spacerLines)
	height := contentHeight + paletteModalBorderRows
	if height < paletteModalMinHeight {
		height = paletteModalMinHeight
	}
	if height > paletteModalMaxHeight {
		height = paletteModalMaxHeight
	}
	if availableHeight > 0 && height > availableHeight {
		height = availableHeight
	}
	if height < 1 {
		height = 1
	}
	return height
}

func (a *App) paletteAvailableHeight() int {
	if a == nil || a.pages == nil {
		return 0
	}
	x, y, width, height := a.pages.GetRect()
	_ = x
	_ = y
	_ = width
	return height
}

func paletteModalTitle(searchMode bool) string {
	if searchMode {
		return " Search Issues "
	}
	return " Commands "
}

// buildPaletteModal creates and configures the command palette modal overlay.
func (a *App) buildPaletteModal() *tview.Flex {
	// Create input field for query with improved styling
	a.paletteInput = tview.NewInputField()
	a.paletteInput.
		SetLabel("> ").
		SetLabelColor(a.theme.Accent).
		SetFieldWidth(0). // Use full available width
		SetPlaceholder("Type to filter commands...").
		SetPlaceholderTextColor(a.theme.SecondaryText).
		SetFieldBackgroundColor(a.theme.InputBg).
		SetFieldTextColor(a.theme.Foreground).
		SetBackgroundColor(a.theme.HeaderBg)

	// Create list for filtered commands
	a.paletteList = tview.NewList().
		ShowSecondaryText(false).
		SetMainTextColor(a.theme.Foreground).
		SetSelectedBackgroundColor(a.theme.Accent).
		SetSelectedTextColor(a.theme.SelectionText).
		SetHighlightFullLine(true)
	a.paletteList.SetBackgroundColor(a.theme.HeaderBg)

	// Create help text with better formatting
	helpText := tview.NewTextView()
	helpText.SetText("↑↓ Navigate  •  Enter Execute  •  Esc Close").
		SetTextColor(a.theme.SecondaryText).
		SetBackgroundColor(a.theme.HeaderBg)
	helpText.SetTextAlign(tview.AlignCenter)

	// Build modal content with better spacing
	// Add a small spacer before input for visual breathing room
	spacerTop := tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)
	spacerBottom := tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)

	a.paletteModalContent = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(spacerTop, a.density.PaletteSpacerLines, 0, false).
		AddItem(a.paletteInput, 1, 0, true).
		AddItem(a.paletteList, 0, 1, false).
		AddItem(spacerBottom, a.density.PaletteSpacerLines, 0, false).
		AddItem(helpText, 1, 0, false)
	a.paletteModalContent.Box = tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)
	// Set border and styling - must be set after creating the flex but before adding to parent
	a.paletteModalContent.
		SetBackgroundColor(a.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(a.theme.Accent).
		SetBorderPadding(0, 0, 0, 0). // No padding - content uses full width
		SetTitle(" Commands ").
		SetTitleColor(a.theme.Accent) // Use accent color for title to match border

	// Center the modal on screen with wider width (60 instead of 50) for better readability
	initialHeight := paletteModalHeight(0, a.density.PaletteSpacerLines, a.paletteAvailableHeight())
	centeredContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(a.paletteModalContent, initialHeight, 0, true).
		AddItem(nil, 0, 1, false)

	horizontalCentered := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(centeredContent, 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Use darker background to create dimming effect (darker than main background)
	modalBg := a.theme.Background
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(horizontalCentered, 0, 1, true).
		AddItem(nil, 0, 1, false)
	modal.SetBackgroundColor(modalBg)

	return modal
}

// updatePaletteList updates the palette list with filtered commands.
func (a *App) updatePaletteList() {
	a.paletteList.Clear()
	a.paletteList.SetOffset(0, 0)
	filtered := a.paletteCtrl.Filtered()
	cursor := a.paletteCtrl.Cursor()

	// Add all filtered commands to the list with shortcut hints
	// Format: [shortcut] Command Title - with shortcut right-aligned in a fixed column
	for _, cmd := range filtered {
		var shortcutHint string
		if cmd.ShortcutDisplay != "" {
			// Use custom display text (e.g., "/" or "Esc")
			shortcutHint = cmd.ShortcutDisplay
		} else if cmd.ShortcutRune != 0 {
			shortcutHint = FormatShortcut(cmd.ShortcutRune)
		}
		var displayText string
		if shortcutHint != "" {
			// Use fixed width shortcut column (8 chars) followed by command title
			displayText = fmt.Sprintf("%s%8s[-]  %s", a.themeTags.SecondaryText, shortcutHint, cmd.Title)
		} else {
			// No shortcut - pad with spaces for alignment
			displayText = fmt.Sprintf("%s%8s[-]  %s", a.themeTags.SecondaryText, "", cmd.Title)
		}
		a.paletteList.AddItem(displayText, "", 0, nil)
	}

	// Set selected item to match cursor position
	if len(filtered) > 0 {
		if cursor >= len(filtered) {
			cursor = len(filtered) - 1
		}
		if cursor < 0 {
			cursor = 0
		}
		a.paletteList.SetCurrentItem(cursor)
	}

	// Keep the modal compact and let tview.List own the viewport scrolling.
	requiredHeight := paletteModalHeight(
		len(filtered),
		a.density.PaletteSpacerLines,
		a.paletteAvailableHeight(),
	)
	if a.paletteModalContent != nil {
		x, y, width, currentHeight := a.paletteModalContent.GetRect()
		_ = x
		_ = y
		_ = width
		currentTitle := paletteModalTitle(a.paletteCtrl.IsSearchMode())
		if currentHeight == requiredHeight && a.paletteModalContent.GetTitle() == currentTitle {
			if a.focusedPane == FocusPalette {
				a.pages.ShowPage("palette")
				a.pages.SendToFront("palette")
			}
			return
		}
	}

	// Rebuild modalContent with correct list height
	// Create help text with improved formatting
	helpText := tview.NewTextView()
	helpText.SetText("↑↓ Navigate  •  Enter Execute  •  Esc Close").
		SetTextColor(a.theme.SecondaryText).
		SetBackgroundColor(a.theme.HeaderBg)
	helpText.SetTextAlign(tview.AlignCenter)

	// Add spacers for visual breathing room
	spacerTop := tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)
	spacerBottom := tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)

	// Keep the list flexible so it becomes a viewport when the command set is
	// larger than the compact modal height.
	a.paletteModalContent = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(spacerTop, a.density.PaletteSpacerLines, 0, false).
		AddItem(a.paletteInput, 1, 0, true).
		AddItem(a.paletteList, 0, 1, false).
		AddItem(spacerBottom, a.density.PaletteSpacerLines, 0, false).
		AddItem(helpText, 1, 0, false)
	a.paletteModalContent.Box = tview.NewBox().SetBackgroundColor(a.theme.HeaderBg)
	// Set border and styling - must be set after creating the flex but before adding to parent
	a.paletteModalContent.
		SetBackgroundColor(a.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(a.theme.Accent).
		SetBorderPadding(0, 0, 0, 0). // No padding - content uses full width
		SetTitle(paletteModalTitle(a.paletteCtrl.IsSearchMode())).
		SetTitleColor(a.theme.Accent) // Use accent color for title to match border

	// Rebuild the entire modal with updated height (including border)
	centeredContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(a.paletteModalContent, requiredHeight, 0, true).
		AddItem(nil, 0, 1, false)

	horizontalCentered := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(centeredContent, 60, 0, true).
		AddItem(nil, 0, 1, false)

	// Use darker background to create dimming effect (darker than main background)
	modalBg := a.theme.Background
	a.paletteModal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(horizontalCentered, 0, 1, true).
		AddItem(nil, 0, 1, false)
	a.paletteModal.SetBackgroundColor(modalBg)

	// Replace the modal in pages
	a.pages.RemovePage("palette")
	a.pages.AddPage("palette", a.paletteModal, true, false)
	if a.focusedPane == FocusPalette {
		a.pages.ShowPage("palette")
		a.pages.SendToFront("palette")
	}
}
