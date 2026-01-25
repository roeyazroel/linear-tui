package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/agents"
)

// AgentOutputModal displays streaming output from an agent run.
type AgentOutputModal struct {
	app           *App
	modal         *tview.Flex
	modalContent  *tview.Flex
	streamView    *tview.TextView
	finalView     *tview.TextView
	statusView    *tview.TextView
	sessionView   *tview.TextView
	resumeView    *tview.TextView
	helpView      *tview.TextView
	headerView    *tview.Flex
	footerView    *tview.Flex
	onCancel      func()
	buffer        *AgentStreamBuffer
	spinner       *agentSpinner
	statusText    string
	structured    bool
	resumeCommand string

	streamMu    sync.Mutex
	pending     []StreamLine
	flushTicker *time.Ticker
	flushStop   chan struct{}
}

const maxFlushLines = 200

// NewAgentOutputModal creates a new agent output modal.
func NewAgentOutputModal(app *App) *AgentOutputModal {
	om := &AgentOutputModal{
		app:     app,
		buffer:  NewAgentStreamBuffer(),
		spinner: newAgentSpinner(),
	}

	om.statusView = tview.NewTextView()
	om.statusView.SetDynamicColors(true).
		SetTextColor(app.theme.SecondaryText).
		SetBackgroundColor(app.theme.HeaderBg)

	om.sessionView = tview.NewTextView()
	om.sessionView.SetDynamicColors(true).
		SetTextColor(app.theme.SecondaryText).
		SetBackgroundColor(app.theme.HeaderBg)
	om.sessionView.SetTextAlign(tview.AlignRight)

	om.resumeView = tview.NewTextView()
	om.resumeView.SetDynamicColors(true).
		SetTextColor(app.theme.SecondaryText).
		SetBackgroundColor(app.theme.HeaderBg)
	om.resumeView.SetTextAlign(tview.AlignCenter)

	om.streamView = tview.NewTextView()
	om.streamView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" Stream ").
		SetTitleColor(app.theme.Foreground)

	om.finalView = tview.NewTextView()
	om.finalView.SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" Final ").
		SetTitleColor(app.theme.Foreground)

	om.helpView = tview.NewTextView()
	om.helpView.SetText("Esc: cancel • c: copy • r: resume cmd • ↑↓/j/k: scroll")
	om.helpView.SetTextColor(app.theme.SecondaryText)
	om.helpView.SetBackgroundColor(app.theme.HeaderBg)
	om.helpView.SetTextAlign(tview.AlignCenter)

	om.footerView = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(om.helpView, 1, 0, false).
		AddItem(om.resumeView, 1, 0, false)
	om.footerView.SetBackgroundColor(app.theme.HeaderBg)

	om.headerView = tview.NewFlex().
		AddItem(om.statusView, 0, 1, false).
		AddItem(om.sessionView, 0, 1, false)
	om.headerView.SetBackgroundColor(app.theme.HeaderBg)

	streamSection := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(om.headerView, 1, 0, false).
		AddItem(om.streamView, 0, 1, false)

	om.modalContent = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(streamSection, 0, 2, false).
		AddItem(om.finalView, 0, 3, false).
		AddItem(om.footerView, 1, 0, false)
	om.modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	om.modalContent.SetBackgroundColor(app.theme.HeaderBg)
	padding := app.density.ModalPadding
	om.modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	om.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(om.modalContent, 32, 0, true).
			AddItem(nil, 0, 1, false), 110, 0, true).
		AddItem(nil, 0, 1, false)
	om.modal.SetBackgroundColor(app.theme.Background)

	return om
}

// ApplyTheme updates modal colors to match the active theme.
func (om *AgentOutputModal) ApplyTheme(theme Theme) {
	if om.statusView != nil {
		om.statusView.SetTextColor(theme.SecondaryText).SetBackgroundColor(theme.HeaderBg)
	}
	if om.sessionView != nil {
		om.sessionView.SetTextColor(theme.SecondaryText).SetBackgroundColor(theme.HeaderBg)
	}
	if om.resumeView != nil {
		om.resumeView.SetTextColor(theme.SecondaryText).SetBackgroundColor(theme.HeaderBg)
	}
	if om.helpView != nil {
		om.helpView.SetTextColor(theme.SecondaryText).SetBackgroundColor(theme.HeaderBg)
	}
	if om.headerView != nil {
		om.headerView.SetBackgroundColor(theme.HeaderBg)
	}
	if om.footerView != nil {
		om.footerView.SetBackgroundColor(theme.HeaderBg)
	}
	if om.streamView != nil {
		om.streamView.SetBackgroundColor(theme.HeaderBg).
			SetBorderColor(theme.Accent).
			SetTitleColor(theme.Foreground)
	}
	if om.finalView != nil {
		om.finalView.SetBackgroundColor(theme.HeaderBg).
			SetBorderColor(theme.Accent).
			SetTitleColor(theme.Foreground)
	}
	if om.modalContent != nil {
		om.modalContent.SetBackgroundColor(theme.HeaderBg)
	}
	if om.modal != nil {
		om.modal.SetBackgroundColor(theme.Background)
	}
}

// ApplyDensity updates modal padding based on the density profile.
func (om *AgentOutputModal) ApplyDensity(density DensityProfile) {
	if om.modalContent != nil {
		padding := density.ModalPadding
		om.modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)
	}
}

// Show displays the output modal with a title and cancel handler.
func (om *AgentOutputModal) Show(title string, onCancel func()) {
	om.onCancel = onCancel
	om.streamView.Clear()
	om.finalView.Clear()
	om.resumeView.Clear()
	om.sessionView.Clear()
	om.resumeCommand = ""
	om.streamView.SetTitle(fmt.Sprintf(" Stream - %s ", title))
	om.finalView.SetTitle(" Final ")
	om.statusView.SetText("Status: Running")
	om.buffer = NewAgentStreamBuffer()
	om.spinner.Start()
	om.startFlushTicker()
	om.streamMu.Lock()
	om.statusText = "Status: Running"
	om.structured = false
	om.streamMu.Unlock()

	om.app.pages.AddPage("agent_output", om.modal, true, true)
	om.app.pages.SendToFront("agent_output")
	om.app.app.SetFocus(om.streamView)
}

// AppendEvent appends a structured event to the stream view.
func (om *AgentOutputModal) AppendEvent(event agents.AgentEvent) {
	om.streamMu.Lock()
	om.structured = true
	update := om.buffer.Append(event)
	if len(update.Lines) > 0 {
		om.pending = append(om.pending, update.Lines...)
	}
	if update.Done {
		om.spinner.Stop()
		om.statusText = "Status: Completed"
		finalText := update.FinalText
		om.streamMu.Unlock()
		om.renderFinal(finalText)
		return
	}
	om.streamMu.Unlock()

	if event.Type == agents.AgentEventSystem {
		if event.SessionID != "" {
			om.setSessionID(event.SessionID)
		}
		if event.ResumeCommand != "" {
			om.setResumeHint(event.ResumeCommand)
		}
	}
}

// AppendRawLine appends a raw line to the stream view.
func (om *AgentOutputModal) AppendRawLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	om.streamMu.Lock()
	structured := om.structured
	om.streamMu.Unlock()
	if structured {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "error:") {
			om.setStatusText("Status: Error - " + strings.TrimSpace(line))
		}
		return
	}
	om.streamMu.Lock()
	om.pending = append(om.pending, StreamLine{Kind: StreamLineUnknown, Text: line})
	om.streamMu.Unlock()
}

// AppendLine appends a raw line to the stream view.
func (om *AgentOutputModal) AppendLine(line string) {
	om.AppendRawLine(line)
}

// Hide hides the output modal.
func (om *AgentOutputModal) Hide() {
	om.stopFlushTicker()
	om.spinner.Stop()
	om.app.pages.RemovePage("agent_output")
	om.app.updateFocus()
}

// HandleKey handles keyboard input for the output modal.
func (om *AgentOutputModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEscape:
		if om.onCancel != nil {
			om.onCancel()
		}
		om.Hide()
		return nil
	case tcell.KeyRune:
		if event.Rune() == 'c' {
			finalText := om.finalView.GetText(true)
			if err := copyToClipboard(finalText); err != nil {
				om.app.updateStatusBarWithError(err)
			}
			return nil
		}
		if event.Rune() == 'r' {
			om.copyResumeCommand()
			return nil
		}
	}
	return event
}

// StopSpinner stops the spinner and updates the status line.
func (om *AgentOutputModal) StopSpinner() {
	om.spinner.Stop()
	om.setStatusText("Status: Completed")
}

// setResumeHint updates the footer with a resume command hint.
func (om *AgentOutputModal) setResumeHint(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	om.streamMu.Lock()
	om.resumeCommand = command
	om.streamMu.Unlock()
	om.app.QueueUpdateDraw(func() {
		om.resumeView.SetText(command)
	})
}

// setSessionID updates the session display in the header.
func (om *AgentOutputModal) setSessionID(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	om.app.QueueUpdateDraw(func() {
		om.sessionView.SetText("Session: " + sessionID)
	})
}

// copyResumeCommand copies the resume command to the clipboard.
func (om *AgentOutputModal) copyResumeCommand() {
	om.streamMu.Lock()
	command := strings.TrimSpace(om.resumeCommand)
	om.streamMu.Unlock()
	if command == "" {
		return
	}
	if err := copyToClipboard(command); err != nil {
		om.app.updateStatusBarWithError(err)
	}
}

// setStatusText updates the status text safely.
func (om *AgentOutputModal) setStatusText(text string) {
	om.streamMu.Lock()
	om.statusText = text
	om.streamMu.Unlock()
	om.app.QueueUpdateDraw(func() {
		om.statusView.SetText(text)
	})
}

// startFlushTicker begins periodic flushing of stream lines and status.
func (om *AgentOutputModal) startFlushTicker() {
	if om.flushTicker != nil {
		return
	}
	om.flushTicker = time.NewTicker(100 * time.Millisecond)
	om.flushStop = make(chan struct{})

	go func() {
		for {
			select {
			case <-om.flushStop:
				return
			case <-om.flushTicker.C:
				om.flushStreamLines()
				om.updateStatusLine()
			}
		}
	}()
}

// stopFlushTicker stops the periodic flush.
func (om *AgentOutputModal) stopFlushTicker() {
	if om.flushTicker == nil {
		return
	}
	om.flushTicker.Stop()
	om.flushTicker = nil
	if om.flushStop != nil {
		close(om.flushStop)
		om.flushStop = nil
	}
}

// flushStreamLines renders pending stream lines to the stream view.
func (om *AgentOutputModal) flushStreamLines() {
	om.streamMu.Lock()
	var lines []StreamLine
	if len(om.pending) > maxFlushLines {
		lines = append([]StreamLine(nil), om.pending[:maxFlushLines]...)
		om.pending = append([]StreamLine(nil), om.pending[maxFlushLines:]...)
	} else {
		lines = append([]StreamLine(nil), om.pending...)
		om.pending = nil
	}
	om.streamMu.Unlock()
	if len(lines) == 0 {
		return
	}

	om.app.QueueUpdateDraw(func() {
		writer := tview.ANSIWriter(om.streamView)
		for _, line := range lines {
			om.writeStreamLine(writer, line)
		}
		om.streamView.ScrollToEnd()
	})
}

// updateStatusLine refreshes the running status and spinner frame.
func (om *AgentOutputModal) updateStatusLine() {
	om.streamMu.Lock()
	statusText := om.statusText
	om.streamMu.Unlock()

	if om.spinner.Running() {
		frame := om.spinner.NextFrame()
		statusText = fmt.Sprintf("%s %s", statusText, frame)
	}

	if statusText == "" {
		return
	}

	om.app.QueueUpdateDraw(func() {
		om.statusView.SetText(statusText)
	})
}

// renderFinal renders the final assistant output in markdown once.
func (om *AgentOutputModal) renderFinal(text string) {
	go func() {
		rendered := renderMarkdown(text)
		om.app.QueueUpdateDraw(func() {
			om.finalView.Clear()
			writer := tview.ANSIWriter(om.finalView)
			_, _ = fmt.Fprintln(writer, rendered)
			om.finalView.ScrollToBeginning()
		})
	}()
}

// writeStreamLine writes a stream line to the writer with minimal styling.
func (om *AgentOutputModal) writeStreamLine(writer io.Writer, line StreamLine) {
	switch line.Kind {
	case StreamLineThinking:
		tag := om.app.themeTags.SecondaryText
		_, _ = fmt.Fprintln(writer, tag+"Thinking:[-]")
		if line.Text != "" {
			_, _ = fmt.Fprintln(writer, tag+line.Text+"[-]")
		}
		_, _ = fmt.Fprintln(writer, "")
	case StreamLineUser:
		_, _ = fmt.Fprintln(writer, "User:")
		if line.Text != "" {
			_, _ = fmt.Fprintln(writer, line.Text)
		}
		_, _ = fmt.Fprintln(writer, "")
	case StreamLineAssistant:
		_, _ = fmt.Fprintln(writer, "Assistant:")
		if line.Text != "" {
			_, _ = fmt.Fprintln(writer, line.Text)
		}
		_, _ = fmt.Fprintln(writer, "")
	default:
		if line.Text != "" {
			_, _ = fmt.Fprintln(writer, line.Text)
		}
	}
}
