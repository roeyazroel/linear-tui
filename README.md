# linear-tui

A terminal user interface (TUI) for Linear built with Go and tview.

## Screenshots

![Main interface](docs/main.jpeg)

![Create issue](docs/create.png)

![Assign issue](docs/assign.png)

## Demo

![Agent demo](docs/agent-demo.gif)

## Features

- 3-pane layout (navigation tree + issues list + details view)
- Command palette for quick actions with keyboard shortcuts
- Vim-style keyboard navigation (j/k, h/l, g/G)
- Mouse support (click to focus, scroll to navigate)
- Issue descriptions with markdown rendering
- Sub-issues support (expand/collapse, create, view parent)
- Issue management (create, edit title, edit labels, archive)
- Comments (view and add)
- Status management (change status, assign/unassign)
- Search and filtering
- Sorting (by updated, created, or priority)
- My Issues vs Other Issues sections
- Agent runs via command palette (Claude or Cursor Agent)
- Agent prompt templates and streaming output with copy/resume
- Real-time issue fetching from Linear API
- Comprehensive logging system for debugging
- Settings modal with live config updates
- Themes (linear, high_contrast, color_blind) and density modes
- Status bar with context and search info
- Clipboard actions (issue ID, issue URL, agent output)

## Requirements

- Linear API key (set as `LINEAR_API_KEY` environment variable)
- Agent CLI for the agent command:
  - Claude provider: `claude`
  - Cursor provider: `cursor-agent` (preferred) or `agent`

## Configuration

- `LINEAR_API_KEY` is required (the API key is not stored on disk).
- Settings are stored in `~/.linear-tui/config.json` and created on first start.
- Use the Settings modal from the command palette (`:` -> `Settings`) to edit and apply settings immediately.
- UI settings in `config.json`: `theme` (`linear`, `high_contrast`, `color_blind`) and `density` (`comfortable`, `compact`).
- Agent settings live in `config.json`: `agent_provider` (`cursor` or `claude`), `agent_sandbox` (`enabled` or `disabled`), `agent_model` (optional), and `agent_workspace` (optional).
- Prompt templates are stored in `~/.linear-tui/prompts.json` and edited via the "Edit agent prompt templates" command.
- `agent_workspace` is the default workspace for agent runs and can be overridden per run in the Ask Agent modal (overrides are not persisted).

Example `~/.linear-tui/config.json`:

```json
{
  "api_endpoint": "https://api.linear.app/graphql",
  "timeout": "30s",
  "page_size": 50,
  "cache_ttl": "5m",
  "log_file": "/Users/you/.linear-tui/app.log",
  "log_level": "warning",
  "theme": "linear",
  "density": "comfortable",
  "agent_provider": "cursor",
  "agent_sandbox": "enabled",
  "agent_model": "",
  "agent_workspace": ""
}
```

## Installation

### Homebrew (macOS)

```bash
brew install roeyazroel/linear-tui/linear-tui
```

### From Source

Requires Go 1.24 or later:

```bash
go install github.com/roeyazroel/linear-tui/cmd/linear-tui@latest
```

Or clone and build locally:

```bash
git clone https://github.com/roeyazroel/linear-tui.git
cd linear-tui
go build ./cmd/linear-tui
```

### Download Binary

Download pre-built binaries from the [Releases](https://github.com/roeyazroel/linear-tui/releases) page.

## Usage

### Basic Usage

Set your Linear API key and run the application:

```bash
export LINEAR_API_KEY="your-api-key-here"
./linear-tui
```

### Advanced Configuration

Example `~/.linear-tui/config.json`:

```json
{
  "api_endpoint": "https://api.linear.app/graphql",
  "timeout": "30s",
  "page_size": 50,
  "cache_ttl": "5m",
  "log_file": "/Users/you/.linear-tui/app.log",
  "log_level": "warning",
  "theme": "linear",
  "density": "comfortable",
  "agent_provider": "cursor",
  "agent_sandbox": "enabled",
  "agent_model": "",
  "agent_workspace": ""
}
```

### Disable Logging

To disable logging, set `log_file` to an empty string in the settings file or via the Settings modal:

```json
{
  "log_file": ""
}
```

## Keyboard Shortcuts

### Navigation

- `j` / `↓` - Move down
- `k` / `↑` - Move up
- `h` / `←` - Focus left pane
- `l` / `→` - Focus right pane
- `g` - Jump to top
- `G` - Jump to bottom
- `Tab` / `Shift+Tab` - Cycle between panes
- `Space` - Toggle expand/collapse sub-issues
- `Enter` - Select issue / Execute command
- `Esc` - Close palette / Cancel / Clear search
- `q` - Quit

### Command Palette

- `:` - Open command palette
- `/` - Open search palette
- `ask agent` - Run a terminal agent on the selected issue

### Quick Commands

- `r` - Refresh issues
- `n` - Create new issue
- `e` - Edit issue title
- `g` - Edit issue labels
- `s` - Change status
- `a` - Assign to user
- `m` - Assign to me
- `u` - Unassign issue
- `t` - Add comment
- `o` - Open in browser
- `y` - Copy issue ID
- `w` - Copy issue URL
- `x` - Archive issue
- `b` - Create sub-issue
- `p` - View parent issue
- `i` - Set parent issue
- `d` - Remove parent
- `]` - Expand all sub-issues
- `[` - Collapse all sub-issues

## Development

Run tests:

```bash
go test ./...
```

Build:

```bash
go build ./cmd/linear-tui
```
