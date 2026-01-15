# linear-tui

A terminal user interface (TUI) for Linear built with Go and tview.

## Screenshots

![Main interface](docs/main.jpeg)

![Create issue](docs/create.png)

![Assign issue](docs/assign.png)

## Features

- 3-pane layout (navigation tree + issues list + details view)
- Command palette for quick actions with keyboard shortcuts
- Vim-style keyboard navigation (j/k, h/l, gg/G)
- Mouse support (click to focus, scroll to navigate)
- Issue descriptions with markdown rendering
- Sub-issues support (expand/collapse, create, view parent)
- Issue management (create, edit title, edit labels, archive)
- Comments (view and add)
- Status management (change status, assign/unassign)
- Search and filtering
- Sorting (by updated, created, or priority)
- My Issues vs Other Issues sections
- Real-time issue fetching from Linear API
- Comprehensive logging system for debugging

## Requirements

- Linear API key (set as `LINEAR_API_KEY` environment variable)

## Configuration

All configuration is done via environment variables:

### Required

- `LINEAR_API_KEY` - Your Linear API key (required)

### Optional

- `LINEAR_API_ENDPOINT` - Linear GraphQL API endpoint (default: `https://api.linear.app/graphql`)
- `LINEAR_TIMEOUT` - HTTP request timeout (default: `30s`, format: Go duration like `30s`, `1m`, `5m`)
- `LINEAR_PAGE_SIZE` - Number of issues to fetch per page (default: `50`, range: 1-250)
- `LINEAR_CACHE_TTL` - Time-to-live for cached team metadata (default: `5m`, format: Go duration)
- `LINEAR_LOG_FILE` - Path to log file (default: `$HOME/.linear-tui/app.log`, set to empty string to disable logging)
- `LINEAR_LOG_LEVEL` - Minimum log level (default: `warning`, options: `debug`, `info`, `warning`, `error`)

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

Example with all optional environment variables:

```bash
export LINEAR_API_KEY="your-api-key-here"
export LINEAR_API_ENDPOINT="https://api.linear.app/graphql"
export LINEAR_TIMEOUT="30s"
export LINEAR_PAGE_SIZE="50"
export LINEAR_CACHE_TTL="5m"
export LINEAR_LOG_FILE="$HOME/.linear-tui/app.log"
export LINEAR_LOG_LEVEL="warning"  # Options: debug, info, warning, error
./linear-tui
```

### Disable Logging

To disable logging, set `LINEAR_LOG_FILE` to an empty string:

```bash
export LINEAR_LOG_FILE=""
./linear-tui
```

## Keyboard Shortcuts

### Navigation

- `j` / `↓` - Move down
- `k` / `↑` - Move up
- `h` / `←` - Focus left pane
- `l` / `→` - Focus right pane
- `gg` - Jump to top
- `G` - Jump to bottom
- `Tab` / `Shift+Tab` - Cycle between panes
- `Space` - Toggle expand/collapse sub-issues
- `Enter` - Select issue / Execute command
- `Esc` - Close palette / Cancel
- `q` - Quit

### Command Palette

- `:` - Open command palette
- `/` - Open search palette

### Command Shortcuts (⌘+key on macOS, Ctrl+key on Linux/Windows)

- `⌘+R` / `Ctrl+R` - Refresh issues
- `⌘+K` / `Ctrl+K` - Clear search
- `⌘+N` / `Ctrl+N` - Create new issue
- `⌘+E` / `Ctrl+E` - Edit issue title
- `⌘+L` / `Ctrl+L` - Edit issue labels
- `⌘+S` / `Ctrl+S` - Change status
- `⌘+A` / `Ctrl+A` - Assign to user
- `⌘+M` / `Ctrl+M` - Assign to me
- `⌘+U` / `Ctrl+U` - Unassign issue
- `⌘+T` / `Ctrl+T` - Add comment
- `⌘+O` / `Ctrl+O` - Open in browser
- `⌘+Y` / `Ctrl+Y` - Copy issue ID
- `⌘+X` / `Ctrl+X` - Archive issue
- `⌘+B` / `Ctrl+B` - Create sub-issue
- `⌘+P` / `Ctrl+P` - View parent issue
- `⌘+I` / `Ctrl+I` - Set parent issue
- `⌘+D` / `Ctrl+D` - Remove parent
- `⌘+]` / `Ctrl+]` - Expand all sub-issues
- `⌘+[` / `Ctrl+[` - Collapse all sub-issues

## Development

Run tests:

```bash
go test ./...
```

Build:

```bash
go build ./cmd/linear-tui
```
