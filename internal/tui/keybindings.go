package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
)

// KeySequenceDestination identifies a navigation destination requested by a
// key chord. Keeping this typed prevents an unavailable destination from
// accidentally falling through to a different navigation action.
type KeySequenceDestination string

const (
	KeySequenceDestinationAll         KeySequenceDestination = "all"
	KeySequenceDestinationMine        KeySequenceDestination = "mine"
	KeySequenceDestinationInbox       KeySequenceDestination = "inbox"
	KeySequenceDestinationTriage      KeySequenceDestination = "triage"
	KeySequenceDestinationViews       KeySequenceDestination = "views"
	KeySequenceDestinationFavorites   KeySequenceDestination = "favorites"
	KeySequenceDestinationProjects    KeySequenceDestination = "projects"
	KeySequenceDestinationInitiatives KeySequenceDestination = "initiatives"
	KeySequenceDestinationCycles      KeySequenceDestination = "cycles"
)

// defaultKeySequenceBindings returns the approved navigation chords. All
// bindings are global so the dispatcher can apply them from any non-editable
// pane while still preserving exact-context precedence for future additions.
func defaultKeySequenceBindings() []KeySequenceBinding {
	return []KeySequenceBinding{
		{Sequence: "g a", CommandID: "navigate_all", Context: "global", Hint: "All Issues"},
		{Sequence: "g m", CommandID: "navigate_mine", Context: "global", Hint: "Mine"},
		{Sequence: "g i", CommandID: "navigate_inbox", Context: "global", Hint: "Inbox"},
		{Sequence: "g t", CommandID: "navigate_triage", Context: "global", Hint: "Triage"},
		{Sequence: "g v", CommandID: "navigate_views", Context: "global", Hint: "Views"},
		{Sequence: "g f", CommandID: "navigate_favorites", Context: "global", Hint: "Favorites"},
		{Sequence: "g p", CommandID: "navigate_projects", Context: "global", Hint: "Projects"},
		{Sequence: "g n", CommandID: "navigate_initiatives", Context: "global", Hint: "Initiatives"},
		{Sequence: "g c", CommandID: "navigate_cycles", Context: "global", Hint: "Cycles"},
	}
}

// KeybindingSetting describes one editable shortcut in the Settings editor.
// Defaults are display values; an omitted value in the config map restores
// the default without writing a duplicate entry.
type KeybindingSetting struct {
	ID      string
	Label   string
	Default string
	Context string
}

// DefaultKeybindingSettings returns the deterministic set of single-key
// commands and navigation chords exposed by the Settings editor.
func DefaultKeybindingSettings() []KeybindingSetting {
	entries := []KeybindingSetting{
		{ID: "quit", Label: "Quit", Default: "q", Context: config.KeybindingContextGlobal},
		{ID: "open_palette", Label: "Command palette", Default: ":", Context: config.KeybindingContextGlobal},
		{ID: "search", Label: "Search issues", Default: "/", Context: config.KeybindingContextGlobal},
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		seen[entry.ID] = struct{}{}
	}
	for _, binding := range defaultKeySequenceBindings() {
		if _, exists := seen[binding.CommandID]; exists {
			continue
		}
		entries = append(entries, KeybindingSetting{
			ID:      binding.CommandID,
			Label:   binding.Hint,
			Default: "",
			Context: binding.Context,
		})
		seen[binding.CommandID] = struct{}{}
	}
	for _, command := range DefaultCommands(nil) {
		if command.ShortcutRune == 0 {
			continue
		}
		context, configurable := config.KeybindingActionContext(command.ID)
		if !configurable {
			continue
		}
		if _, exists := seen[command.ID]; exists {
			continue
		}
		entries = append(entries, KeybindingSetting{
			ID:      command.ID,
			Label:   command.Title,
			Default: string(command.ShortcutRune),
			Context: context,
		})
		seen[command.ID] = struct{}{}
	}
	if _, exists := seen["edit_issue"]; !exists {
		entries = append(entries, KeybindingSetting{ID: "edit_issue", Label: "Edit issue", Context: config.KeybindingContextNavigationIssues})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].Context != entries[right].Context {
			return entries[left].Context < entries[right].Context
		}
		return entries[left].Label < entries[right].Label
	})
	return entries
}

func cloneKeybindingMap(bindings map[string]string) map[string]string {
	if bindings == nil {
		return nil
	}
	clone := make(map[string]string, len(bindings))
	for key, value := range bindings {
		clone[key] = value
	}
	return clone
}

// buildKeySequenceDispatcher applies optional sequence overrides from the
// regular keybindings map and validates deterministic duplicate/prefix and
// single-key conflicts before installing a dispatcher.
func buildKeySequenceDispatcher(bindings map[string]string) (*KeySequenceDispatcher, error) {
	canonicalBindings, err := config.NormalizeKeybindings(bindings)
	if err != nil {
		return nil, err
	}
	sequences, err := keySequenceBindingsForConfig(canonicalBindings)
	if err != nil {
		return nil, err
	}

	// A configured one-key command may not consume the first rune of a
	// sequence. Otherwise the dispatcher would shadow the explicitly bound
	// single-key command before it can run.
	actions := make([]string, 0, len(canonicalBindings))
	for action := range canonicalBindings {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	for _, action := range actions {
		value, err := config.NormalizeKeySequenceForAction(action, canonicalBindings[action])
		if err != nil {
			return nil, fmt.Errorf("keybinding %q: %w", action, err)
		}
		fields := strings.Fields(value)
		if len(fields) != 1 {
			continue
		}
		runes := []rune(fields[0])
		if len(runes) != 1 {
			continue
		}
		for _, sequence := range sequences {
			sequenceRunes := []rune(strings.ReplaceAll(sequence.Sequence, " ", ""))
			if len(sequenceRunes) > 0 && sequenceRunes[0] == runes[0] && !containsString(keySequenceBindingIDs(sequence.CommandID), action) {
				return nil, fmt.Errorf("keybinding %q conflicts with sequence %q", action, sequence.Sequence)
			}
		}
	}

	return NewKeySequenceDispatcher(sequences, 750*time.Millisecond)
}

func keySequenceBindingsForConfig(bindings map[string]string) ([]KeySequenceBinding, error) {
	canonicalBindings, err := config.NormalizeKeybindings(bindings)
	if err != nil {
		return nil, err
	}
	for action, value := range canonicalBindings {
		normalized, err := config.NormalizeKeySequenceForAction(action, value)
		if err != nil {
			return nil, fmt.Errorf("keybinding %q: %w", action, err)
		}
		if len(strings.Fields(normalized)) > 1 && !isNavigationSequenceAction(action) {
			return nil, fmt.Errorf("keybinding %q: multi-key mappings are supported only for navigation chords", action)
		}
	}

	sequences := make([]KeySequenceBinding, 0, len(defaultKeySequenceBindings()))
	for _, template := range defaultKeySequenceBindings() {
		for _, id := range keySequenceBindingIDs(template.CommandID) {
			if value, ok := canonicalBindings[id]; ok {
				normalized, err := config.NormalizeKeySequenceForAction(id, value)
				if err != nil {
					return nil, fmt.Errorf("keybinding %q: %w", id, err)
				}
				if len(strings.Fields(normalized)) < 2 {
					return nil, fmt.Errorf("keybinding %q navigation chord must contain at least two tokens", id)
				}
				template.Sequence = normalized
				sequences = append(sequences, template)
				break
			}
		}
	}
	return sequences, nil
}

func isNavigationSequenceAction(action string) bool {
	for _, binding := range defaultKeySequenceBindings() {
		if binding.CommandID == action {
			return true
		}
	}
	return false
}

func keySequenceBindingIDs(commandID string) []string {
	switch commandID {
	case "navigate_all":
		return []string{"navigate_all", "go_all", "all"}
	case "navigate_mine":
		return []string{"navigate_mine", "go_mine", "mine"}
	case "navigate_inbox":
		return []string{"navigate_inbox", "go_inbox", "inbox"}
	case "navigate_triage":
		return []string{"navigate_triage", "go_triage", "triage"}
	case "navigate_views":
		return []string{"navigate_views", "go_views", "views"}
	case "navigate_favorites":
		return []string{"navigate_favorites", "go_favorites", "favorites"}
	case "navigate_projects":
		return []string{"navigate_projects", "go_projects", "projects"}
	case "navigate_initiatives":
		return []string{"navigate_initiatives", "go_initiatives", "initiatives"}
	case "navigate_cycles":
		return []string{"navigate_cycles", "go_cycles", "cycles"}
	default:
		return []string{commandID}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// applyCommandKeybindings overrides palette command shortcut runes from the
// keybindings config, keyed by command id. Explicit mappings own their keys:
// a command whose default shortcut is claimed by a mapping loses it.
func applyCommandKeybindings(commands []Command, bindings map[string]string) {
	if len(bindings) == 0 {
		return
	}

	claimed := make(map[rune]string, len(bindings))
	for id, key := range bindings {
		normalized, err := config.NormalizeKeySequenceForAction(id, key)
		if err != nil {
			continue
		}
		if runes := []rune(normalized); len(runes) == 1 {
			claimed[runes[0]] = id
		}
	}

	for i := range commands {
		if key, ok := bindings[commands[i].ID]; ok {
			normalized, err := config.NormalizeKeySequenceForAction(commands[i].ID, key)
			if err == nil {
				if runes := []rune(normalized); len(runes) == 1 {
					commands[i].ShortcutRune = runes[0]
					commands[i].ShortcutDisplay = ""
				} else if len(strings.Fields(normalized)) > 1 {
					// Unsupported multi-key command mappings are rejected by
					// settings/runtime validation. If an App is constructed with
					// an unvalidated config, do not leave the command's default
					// shortcut advertised as if it were still reachable.
					commands[i].ShortcutRune = 0
					commands[i].ShortcutDisplay = ""
				}
			}
			continue
		}
		if owner, taken := claimed[commands[i].ShortcutRune]; taken && owner != commands[i].ID {
			commands[i].ShortcutRune = 0
		}
	}
}

// actionKey returns the configured key for a UI action id, or the fallback.
func (a *App) actionKey(action string, fallback rune) rune {
	if key, ok := a.config.Keybindings[action]; ok {
		normalized, err := config.NormalizeKeySequenceForAction(action, key)
		if err == nil {
			if runes := []rune(normalized); len(runes) == 1 {
				return runes[0]
			}
		}
	}
	return fallback
}
