package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ConfirmationModal manages a small confirm/cancel overlay for risky actions.
type ConfirmationModal struct {
	app       *App
	modal     *tview.Modal
	onConfirm func()
	onCancel  func()
	active    bool
}

// NewConfirmationModal creates a confirmation modal.
func NewConfirmationModal(app *App) *ConfirmationModal {
	return &ConfirmationModal{app: app}
}

// Show displays the confirmation prompt. An optional cancel callback is
// invoked exactly once when the prompt is canceled by Escape, the Cancel
// button, Hide, or replacement by another prompt.
func (cm *ConfirmationModal) Show(title, message, confirmLabel string, onConfirm func(), onCancel ...func()) {
	if cm == nil || cm.app == nil || cm.app.pages == nil {
		return
	}
	if cm.active {
		cm.finish(false, cm.modal)
	}
	var cancel func()
	if len(onCancel) > 0 {
		cancel = onCancel[0]
	}
	cm.onConfirm = onConfirm
	cm.onCancel = cancel
	cm.active = true
	var modal *tview.Modal
	modal = tview.NewModal().
		SetText(message).
		AddButtons([]string{confirmLabel, "Cancel"}).
		SetDoneFunc(func(buttonIndex int, _ string) {
			cm.handleButton(buttonIndex, modal)
		})
	cm.modal = modal
	cm.modal.SetBackgroundColor(cm.app.theme.HeaderBg)
	cm.modal.SetTextColor(cm.app.theme.Foreground)
	cm.modal.SetButtonBackgroundColor(cm.app.theme.Accent)
	cm.modal.SetButtonTextColor(cm.app.theme.SelectionText)
	cm.modal.SetBorder(true).
		SetBorderColor(cm.app.theme.Accent).
		SetTitle(" " + title + " ").
		SetTitleColor(cm.app.theme.Foreground)

	cm.app.pages.AddPage("confirmation", cm.modal, true, true)
	cm.app.pages.SendToFront("confirmation")
	cm.app.app.SetFocus(cm.modal)
}

// handleButton is the single terminal path used by both tview button events
// and tests that dispatch a button index without a running terminal.
func (cm *ConfirmationModal) handleButton(buttonIndex int, modal *tview.Modal) {
	if cm == nil || !cm.active || cm.modal != modal {
		return
	}
	cm.finish(buttonIndex == 0, modal)
}

// Hide closes the confirmation prompt.
func (cm *ConfirmationModal) Hide() {
	if cm == nil || cm.app == nil || cm.app.pages == nil {
		return
	}
	if cm.active {
		cm.finish(false, cm.modal)
		return
	}
	if cm.modal != nil && cm.app.pages.GetPage("confirmation") == cm.modal {
		cm.app.pages.RemovePage("confirmation")
		cm.app.updateFocus()
	}
}

// HandleKey handles confirmation-level shortcuts.
func (cm *ConfirmationModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		cm.Hide()
		return nil
	}
	return event
}

// finish closes one active prompt and dispatches exactly one terminal callback.
// The active flag is cleared before invoking owner code so a callback that
// cleans up its page cannot re-enter the shared modal and fire twice.
func (cm *ConfirmationModal) finish(confirmed bool, modal *tview.Modal) {
	if cm == nil || !cm.active || cm.modal != modal {
		return
	}
	onConfirm := cm.onConfirm
	onCancel := cm.onCancel
	cm.active = false
	cm.onCancel = nil
	if cm.app != nil && cm.app.pages != nil && cm.app.pages.GetPage("confirmation") == modal {
		cm.app.pages.RemovePage("confirmation")
		cm.app.updateFocus()
	}
	if confirmed {
		if onConfirm != nil {
			onConfirm()
		}
		return
	}
	if onCancel != nil {
		onCancel()
	}
}

// release marks a prompt inactive when its owner has already completed or
// canceled the action directly. The owner remains responsible for removing
// its page; clearing the shared active state prevents a later replacement from
// invoking the old cancellation callback against a new pending action.
func (cm *ConfirmationModal) release(modal tview.Primitive) {
	if cm == nil || !cm.active || cm.modal != modal {
		return
	}
	cm.active = false
	cm.onCancel = nil
}
