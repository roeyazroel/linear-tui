package agents

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/roeyazroel/linear-tui/internal/logger"
)

// CursorProvider invokes the Cursor Agent CLI.
type CursorProvider struct {
	lookPath func(string) (string, error)
}

// NewCursorProvider creates a Cursor provider with an optional lookPath override.
func NewCursorProvider(lookPath func(string) (string, error)) *CursorProvider {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return &CursorProvider{lookPath: lookPath}
}

// Name returns the display name for this provider.
func (p *CursorProvider) Name() string {
	return "Cursor"
}

// ResolveBinary finds the Cursor Agent CLI binary.
func (p *CursorProvider) ResolveBinary() (string, bool) {
	if path, err := p.lookPath("cursor-agent"); err == nil {
		return path, true
	}
	if path, err := p.lookPath("agent"); err == nil {
		return path, true
	}
	return "", false
}

// BuildArgs builds argv for a non-interactive Cursor run.
func (p *CursorProvider) BuildArgs(prompt string, issueContext string, options AgentRunOptions) []string {
	fullPrompt := buildAgentPrompt(prompt, issueContext)
	args := []string{"--force", "--print", "--output-format", "stream-json"}
	if options.Sandbox != "" {
		args = append(args, "--sandbox", options.Sandbox)
	}
	if options.Model != "" {
		args = append(args, "--model", options.Model)
	}
	if options.Workspace != "" {
		args = append(args, "--workspace", options.Workspace)
	}
	args = append(args, "-p", fullPrompt)
	return args
}

// ParseStreamLine attempts to extract display text from Cursor stream-json.
func (p *CursorProvider) ParseStreamLine(line []byte) (string, bool) {
	event, ok := p.ParseEvent(line)
	if !ok || event == nil {
		return "", false
	}

	return formatEventLine(*event), true
}

// cursorStreamEvent captures common Cursor stream-json fields.
type cursorStreamEvent struct {
	Type           string `json:"type"`
	Subtype        string `json:"subtype"`
	Text           string `json:"text"`
	Content        string `json:"content"`
	APIKeySource   string `json:"apiKeySource"`
	Cwd            string `json:"cwd"`
	SessionID      string `json:"session_id"`
	Model          string `json:"model"`
	PermissionMode string `json:"permissionMode"`
	DurationMs     int64  `json:"duration_ms"`
	DurationAPIMs  int64  `json:"duration_api_ms"`
	IsError        bool   `json:"is_error"`
	RequestID      string `json:"request_id"`
	Result         string `json:"result"`
	Message        struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Delta struct {
		Content string `json:"content"`
		Text    string `json:"text"`
	} `json:"delta"`
	ToolCall cursorToolCall `json:"tool_call"`
}

// cursorToolCall captures tool call metadata for progress output.
type cursorToolCall struct {
	ReadToolCall *struct {
		Args struct {
			Path string `json:"path"`
		} `json:"args"`
		Result cursorToolCallResult `json:"result"`
	} `json:"readToolCall"`
	WriteToolCall *struct {
		Args struct {
			Path string `json:"path"`
		} `json:"args"`
		Result cursorToolCallResult `json:"result"`
	} `json:"writeToolCall"`
	Function *struct {
		Name string `json:"name"`
	} `json:"function"`
}

// cursorToolCallResult captures success/error metadata for tool calls.
type cursorToolCallResult struct {
	Success *struct {
		Content       string `json:"content"`
		IsEmpty       bool   `json:"isEmpty"`
		ExceededLimit bool   `json:"exceededLimit"`
		TotalLines    int    `json:"totalLines"`
		TotalChars    int    `json:"totalChars"`
		Path          string `json:"path"`
		LinesCreated  int    `json:"linesCreated"`
		FileSize      int    `json:"fileSize"`
	} `json:"success"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// extractMessageText joins text entries from a message content array.
func extractMessageText(items []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Text == "" {
			continue
		}
		builder.WriteString(item.Text)
	}
	return builder.String()
}

// formatSystemEvent returns a summary line for system init events.
// ParseEvent parses a stream-json line into an AgentEvent.
func (p *CursorProvider) ParseEvent(line []byte) (*AgentEvent, bool) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}

	var event cursorStreamEvent
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		logger.ErrorWithErr(err, "Cursor provider: failed to parse stream event")
		return nil, false
	}

	switch event.Type {
	case "system":
		return &AgentEvent{
			Type:          AgentEventSystem,
			Subtype:       event.Subtype,
			Model:         event.Model,
			SessionID:     event.SessionID,
			ResumeCommand: buildCursorResumeCommand(event.SessionID),
		}, true
	case "user":
		return &AgentEvent{
			Type: AgentEventUser,
			Text: extractMessageText(event.Message.Content),
		}, true
	case "assistant":
		return &AgentEvent{
			Type: AgentEventAssistant,
			Text: extractMessageText(event.Message.Content),
		}, true
	case "thinking":
		return &AgentEvent{
			Type: AgentEventThinking,
			Text: coalesceText(event.Text, event.Content),
		}, true
	case "tool_call":
		return buildToolCallEvent(event), true
	case "result":
		if event.IsError {
			logger.Error("agents.cursor: result error subtype=%s duration_ms=%d request_id=%s", event.Subtype, event.DurationMs, strings.TrimSpace(event.RequestID))
		}
		return &AgentEvent{
			Type:       AgentEventResult,
			Subtype:    event.Subtype,
			DurationMs: event.DurationMs,
			IsError:    event.IsError,
		}, true
	}

	if delta := coalesceText(event.Delta.Text, event.Delta.Content); delta != "" {
		return &AgentEvent{
			Type: AgentEventAssistantDelta,
			Text: delta,
		}, true
	}

	if text := coalesceText(event.Text, event.Content); text != "" {
		return &AgentEvent{
			Type: AgentEventUnknown,
			Text: text,
		}, true
	}

	return nil, false
}

// buildCursorResumeCommand returns a resume command when a session id is available.
func buildCursorResumeCommand(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return fmt.Sprintf("cursor-agent --resume %s", sessionID)
}

// formatToolCall returns a short status line for tool call events.
// buildToolCallEvent converts a tool call stream event into an AgentEvent.
func buildToolCallEvent(event cursorStreamEvent) *AgentEvent {
	name, detail := extractToolCallDetails(event.ToolCall)
	tool := &AgentToolCall{
		Name:   name,
		Path:   detail,
		Status: event.Subtype,
	}
	if event.Subtype == "completed" {
		if errMessage := extractToolCallError(event.ToolCall); errMessage != "" {
			logger.Error("agents.cursor: tool call failed tool=%s error=%s", name, errMessage)
		}
		tool.Summary = formatToolResult(event.ToolCall)
	}

	return &AgentEvent{
		Type:    AgentEventToolCall,
		Subtype: event.Subtype,
		Tool:    tool,
	}
}

// extractToolCallError returns the first tool-call error message, if any.
func extractToolCallError(call cursorToolCall) string {
	if call.ReadToolCall != nil && call.ReadToolCall.Result.Error != nil {
		return strings.TrimSpace(call.ReadToolCall.Result.Error.Message)
	}
	if call.WriteToolCall != nil && call.WriteToolCall.Result.Error != nil {
		return strings.TrimSpace(call.WriteToolCall.Result.Error.Message)
	}
	return ""
}

// extractToolCallDetails pulls a tool name and optional detail for display.
func extractToolCallDetails(call cursorToolCall) (string, string) {
	if call.ReadToolCall != nil {
		return "read", strings.TrimSpace(call.ReadToolCall.Args.Path)
	}
	if call.WriteToolCall != nil {
		return "write", strings.TrimSpace(call.WriteToolCall.Args.Path)
	}
	if call.Function != nil {
		return strings.TrimSpace(call.Function.Name), ""
	}
	return "", ""
}

// formatToolResult renders a concise summary of tool results.
func formatToolResult(call cursorToolCall) string {
	if call.ReadToolCall != nil {
		return summarizeReadResult(call.ReadToolCall.Result)
	}
	if call.WriteToolCall != nil {
		return summarizeWriteResult(call.WriteToolCall.Result)
	}
	return ""
}

func summarizeReadResult(result cursorToolCallResult) string {
	if result.Success != nil {
		parts := []string{}
		if result.Success.TotalLines > 0 {
			parts = append(parts, fmt.Sprintf("%d lines", result.Success.TotalLines))
		}
		if result.Success.TotalChars > 0 {
			parts = append(parts, fmt.Sprintf("%d chars", result.Success.TotalChars))
		}
		if result.Success.IsEmpty {
			parts = append(parts, "empty")
		}
		if result.Success.ExceededLimit {
			parts = append(parts, "limit-exceeded")
		}
		if len(parts) == 0 {
			return "(success)"
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
	}
	if result.Error != nil && result.Error.Message != "" {
		return fmt.Sprintf("(error: %s)", result.Error.Message)
	}
	return ""
}

func summarizeWriteResult(result cursorToolCallResult) string {
	if result.Success != nil {
		parts := []string{}
		if result.Success.Path != "" {
			parts = append(parts, result.Success.Path)
		}
		if result.Success.LinesCreated > 0 {
			parts = append(parts, fmt.Sprintf("%d lines", result.Success.LinesCreated))
		}
		if result.Success.FileSize > 0 {
			parts = append(parts, fmt.Sprintf("%s bytes", strconv.Itoa(result.Success.FileSize)))
		}
		if len(parts) == 0 {
			return "(success)"
		}
		return fmt.Sprintf("(%s)", strings.Join(parts, ", "))
	}
	if result.Error != nil && result.Error.Message != "" {
		return fmt.Sprintf("(error: %s)", result.Error.Message)
	}
	return ""
}

// formatTextEvent builds a prefixed line from the provided text content.
// formatEventLine formats a parsed AgentEvent into a display line.
func formatEventLine(event AgentEvent) string {
	switch event.Type {
	case AgentEventSystem:
		if event.Model != "" {
			return fmt.Sprintf("System init model=%s", event.Model)
		}
		return "System init"
	case AgentEventUser:
		if event.Text == "" {
			return "User: (no prompt content)"
		}
		return "User: " + event.Text
	case AgentEventAssistant:
		if event.Text == "" {
			return "Assistant: (no content)"
		}
		return "Assistant: " + event.Text
	case AgentEventAssistantDelta:
		if event.Text == "" {
			return ""
		}
		return "Assistant delta: " + event.Text
	case AgentEventThinking:
		if event.Text == "" {
			return "Thinking: (no details)"
		}
		return "Thinking: " + event.Text
	case AgentEventToolCall:
		if event.Tool == nil || event.Tool.Name == "" {
			return "Tool call: (unknown)"
		}
		label := "Tool call"
		if event.Subtype != "" {
			label = fmt.Sprintf("Tool call %s", event.Subtype)
		}
		detail := strings.TrimSpace(event.Tool.Path)
		if detail != "" {
			if event.Tool.Summary != "" {
				return fmt.Sprintf("%s: %s (%s) %s", label, event.Tool.Name, detail, event.Tool.Summary)
			}
			return fmt.Sprintf("%s: %s (%s)", label, event.Tool.Name, detail)
		}
		if event.Tool.Summary != "" {
			return fmt.Sprintf("%s: %s %s", label, event.Tool.Name, event.Tool.Summary)
		}
		return fmt.Sprintf("%s: %s", label, event.Tool.Name)
	case AgentEventResult:
		subtype := event.Subtype
		if subtype == "" {
			subtype = "completed"
		}
		parts := []string{fmt.Sprintf("Result %s", subtype)}
		if event.IsError {
			parts = append(parts, "error=true")
		}
		if event.DurationMs > 0 {
			parts = append(parts, fmt.Sprintf("duration=%dms", event.DurationMs))
		}
		return strings.Join(parts, " ")
	default:
		if event.Text != "" {
			return "Event: " + event.Text
		}
		return "Event: unknown"
	}
}

// truncatePreview shortens long lines for event previews.
// coalesceText returns the first non-empty trimmed text value.
func coalesceText(primary string, secondary string) string {
	value := strings.TrimSpace(primary)
	if value == "" {
		value = strings.TrimSpace(secondary)
	}
	return value
}
