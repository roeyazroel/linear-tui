package tui

import (
	"fmt"
	"strings"

	"github.com/roeyazroel/linear-tui/internal/agents"
)

// StreamLineKind identifies the type of a streaming output line.
type StreamLineKind string

const (
	StreamLineSystem    StreamLineKind = "system"
	StreamLineUser      StreamLineKind = "user"
	StreamLineAssistant StreamLineKind = "assistant"
	StreamLineThinking  StreamLineKind = "thinking"
	StreamLineTool      StreamLineKind = "tool"
	StreamLineResult    StreamLineKind = "result"
	StreamLineUnknown   StreamLineKind = "unknown"
)

// StreamLine is a single line for the stream view.
type StreamLine struct {
	Kind StreamLineKind
	Text string
}

// StreamUpdate contains new stream lines and optional final output.
type StreamUpdate struct {
	Lines     []StreamLine
	FinalText string
	Done      bool
}

// AgentStreamBuffer aggregates streaming events into stream lines and final output.
type AgentStreamBuffer struct {
	assistant        strings.Builder
	thinking         strings.Builder
	thinkingLastChar byte
	hasThinkingChar  bool
}

const thinkingFlushChars = 200

// NewAgentStreamBuffer constructs a new stream buffer.
func NewAgentStreamBuffer() *AgentStreamBuffer {
	return &AgentStreamBuffer{}
}

// Append converts an AgentEvent into stream lines and final text when complete.
func (b *AgentStreamBuffer) Append(event agents.AgentEvent) StreamUpdate {
	update := StreamUpdate{}

	switch event.Type {
	case agents.AgentEventThinking:
		b.appendThinkingText(event.Text)
		if shouldFlushThinking(event.Text, b.thinking.Len()) {
			b.flushThinkingLine(&update)
		}
		return update
	case agents.AgentEventSystem:
		b.flushThinkingLine(&update)
	case agents.AgentEventUser:
		b.flushThinkingLine(&update)
	case agents.AgentEventAssistant, agents.AgentEventAssistantDelta:
		b.flushThinkingLine(&update)
		if event.Text != "" {
			if b.assistant.Len() > 0 {
				b.assistant.WriteString("\n")
			}
			b.assistant.WriteString(event.Text)
		}
	case agents.AgentEventToolCall:
		b.flushThinkingLine(&update)
		update.Lines = append(update.Lines, StreamLine{
			Kind: StreamLineTool,
			Text: formatToolLine(event),
		})
	case agents.AgentEventResult:
		b.flushThinkingLine(&update)
		update.FinalText = strings.TrimSpace(b.assistant.String())
		update.Done = true
	default:
	}

	return update
}

// appendThinkingText appends reasoning text while preserving spacing.
func (b *AgentStreamBuffer) appendThinkingText(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}

	if b.thinking.Len() > 0 && needsThinkingSpace(b.thinkingLastChar, text) {
		b.thinking.WriteByte(' ')
		b.thinkingLastChar = ' '
		b.hasThinkingChar = true
	}

	b.thinking.WriteString(text)
	if len(text) > 0 {
		b.thinkingLastChar = text[len(text)-1]
		b.hasThinkingChar = true
	}
}

// flushThinkingLine emits the buffered thinking text as a single line.
func (b *AgentStreamBuffer) flushThinkingLine(update *StreamUpdate) {
	content := strings.TrimSpace(b.thinking.String())
	if content == "" {
		b.resetThinkingBuffer()
		return
	}

	update.Lines = append(update.Lines, StreamLine{
		Kind: StreamLineThinking,
		Text: content,
	})
	b.resetThinkingBuffer()
}

// resetThinkingBuffer clears the thinking buffer state.
func (b *AgentStreamBuffer) resetThinkingBuffer() {
	b.thinking.Reset()
	b.thinkingLastChar = 0
	b.hasThinkingChar = false
}

// shouldFlushThinking determines when to emit a thinking line.
func shouldFlushThinking(latest string, currentLen int) bool {
	if currentLen >= thinkingFlushChars {
		return true
	}
	return strings.Contains(latest, "\n")
}

// needsThinkingSpace checks if a space is needed between tokens.
func needsThinkingSpace(lastChar byte, next string) bool {
	if next == "" {
		return false
	}
	if isSpaceByte(lastChar) {
		return false
	}
	return !isSpaceByte(next[0])
}

// isSpaceByte reports whether a byte is treated as whitespace.
func isSpaceByte(value byte) bool {
	switch value {
	case ' ', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}

// formatToolLine returns a tool call line suitable for the stream view.
func formatToolLine(event agents.AgentEvent) string {
	if event.Tool == nil || event.Tool.Name == "" {
		return "Tool call: (unknown)"
	}

	label := "Tool call"
	if event.Subtype != "" {
		label = fmt.Sprintf("Tool call %s", event.Subtype)
	}

	if event.Tool.Path != "" {
		if event.Tool.Summary != "" {
			return fmt.Sprintf("%s: %s (%s) %s", label, event.Tool.Name, event.Tool.Path, event.Tool.Summary)
		}
		return fmt.Sprintf("%s: %s (%s)", label, event.Tool.Name, event.Tool.Path)
	}

	if event.Tool.Summary != "" {
		return fmt.Sprintf("%s: %s %s", label, event.Tool.Name, event.Tool.Summary)
	}
	return fmt.Sprintf("%s: %s", label, event.Tool.Name)
}
