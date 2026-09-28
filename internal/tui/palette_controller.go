package tui

import (
	"strings"
)

// PaletteController manages the command palette filtering and selection logic.
type PaletteController struct {
	commands     []Command
	query        string
	cursor       int
	filtered     []Command
	isSearchMode bool
	// issueContext ranks issue-scoped commands first when the palette was
	// opened from an issue-focused pane, and last otherwise.
	issueContext bool
}

// issueScopedCommands lists command ids that act on the selected issue.
var issueScopedCommands = map[string]bool{
	"open_browser": true, "open_github": true, "copy_id": true,
	"copy_url": true, "copy_branch": true, "ask_agent": true,
	"set_due_date": true, "clear_due_date": true, "edit_estimate": true,
	"clear_estimate": true, "set_milestone": true, "clear_milestone": true,
	"add_issue_relation": true, "remove_issue_relation": true,
	"subscribe_issue": true, "unsubscribe_issue": true,
	"open_attachment": true, "copy_attachment_url": true,
	"assign_me": true, "unassign": true, "archive": true,
	"change_status": true, "set_cycle": true, "clear_cycle": true,
	"assign_user": true, "edit_title": true, "edit_labels": true,
	"toggle_sub_issues": true, "view_parent": true,
	"create_sub_issue": true, "set_parent": true, "remove_parent": true,
	"add_comment": true,
}

// SetIssueContext records whether the palette was opened from an
// issue-focused pane, reordering results accordingly.
func (p *PaletteController) SetIssueContext(issueContext bool) {
	p.issueContext = issueContext
	if !p.isSearchMode {
		p.filterCommands()
	}
}

// rankByContext stable-partitions commands so the contextually relevant scope
// comes first.
func (p *PaletteController) rankByContext(commands []Command) []Command {
	ranked := make([]Command, 0, len(commands))
	var deferred []Command
	for _, cmd := range commands {
		if issueScopedCommands[cmd.ID] == p.issueContext {
			ranked = append(ranked, cmd)
		} else {
			deferred = append(deferred, cmd)
		}
	}
	return append(ranked, deferred...)
}

// NewPaletteController creates a new palette controller with the given commands.
func NewPaletteController(commands []Command) *PaletteController {
	pc := &PaletteController{
		commands: commands,
		filtered: commands,
	}
	return pc
}

// SetQuery sets the search query and filters commands.
func (p *PaletteController) SetQuery(q string) {
	p.query = q
	if !p.isSearchMode {
		p.filterCommands()
	}
	p.cursor = 0
}

// Query returns the current query.
func (p *PaletteController) Query() string {
	return p.query
}

// Filtered returns the filtered list of commands.
func (p *PaletteController) Filtered() []Command {
	return p.filtered
}

// Selected returns the currently selected command and whether one is selected.
func (p *PaletteController) Selected() (Command, bool) {
	if p.isSearchMode {
		return Command{}, false
	}
	if len(p.filtered) == 0 || p.cursor < 0 || p.cursor >= len(p.filtered) {
		return Command{}, false
	}
	return p.filtered[p.cursor], true
}

// Cursor returns the current cursor position.
func (p *PaletteController) Cursor() int {
	return p.cursor
}

// SetCursor sets the cursor position, clamping to valid range.
func (p *PaletteController) SetCursor(pos int) {
	switch {
	case pos < 0:
		p.cursor = 0
	case pos >= len(p.filtered):
		p.cursor = len(p.filtered) - 1
		if p.cursor < 0 {
			p.cursor = 0
		}
	default:
		p.cursor = pos
	}
}

// MoveCursorUp moves the cursor up by one.
func (p *PaletteController) MoveCursorUp() {
	if p.cursor > 0 {
		p.cursor--
	}
}

// MoveCursorDown moves the cursor down by one.
func (p *PaletteController) MoveCursorDown() {
	if p.cursor < len(p.filtered)-1 {
		p.cursor++
	}
}

// Reset resets the query and cursor to initial state.
func (p *PaletteController) Reset() {
	p.query = ""
	p.cursor = 0
	p.filtered = p.rankByContext(p.commands)
	p.isSearchMode = false
}

// SetSearchMode sets whether the palette is in search mode.
// In search mode, the query is used for issue search, not command filtering.
func (p *PaletteController) SetSearchMode(mode bool) {
	p.isSearchMode = mode
	if mode {
		p.filtered = nil
	} else {
		p.filtered = p.rankByContext(p.commands)
	}
}

// IsSearchMode returns whether the palette is in search mode.
func (p *PaletteController) IsSearchMode() bool {
	return p.isSearchMode
}

// filterCommands filters commands based on the query.
func (p *PaletteController) filterCommands() {
	if p.query == "" {
		p.filtered = p.rankByContext(p.commands)
		return
	}

	tokens := strings.Fields(strings.ToLower(p.query))
	filtered := make([]Command, 0)

	for _, cmd := range p.commands {
		searchable := strings.ToLower(cmd.Title + " " + strings.Join(cmd.Keywords, " "))
		matched := true
		for _, token := range tokens {
			if !strings.Contains(searchable, token) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, cmd)
		}
	}

	p.filtered = p.rankByContext(filtered)
}
