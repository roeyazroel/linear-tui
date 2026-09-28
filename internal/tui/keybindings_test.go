package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/roeyazroel/linear-tui/internal/config"
)

// TestApplyCommandKeybindings verifies overrides apply and claimed default
// shortcuts are cleared.
func TestApplyCommandKeybindings(t *testing.T) {
	commands := []Command{
		{ID: "copy_id", ShortcutRune: 'y'},
		{ID: "copy_url", ShortcutRune: 'w'},
		{ID: "set_parent", ShortcutRune: 'i'},
	}
	applyCommandKeybindings(commands, map[string]string{
		"copy_url": "y",
		"copy_id":  "i",
	})
	if commands[1].ShortcutRune != 'y' {
		t.Errorf("copy_url = %q, want y", commands[1].ShortcutRune)
	}
	if commands[0].ShortcutRune != 'i' {
		t.Errorf("copy_id = %q, want i", commands[0].ShortcutRune)
	}
	if commands[2].ShortcutRune != 0 {
		t.Errorf("set_parent kept %q, want cleared (key claimed)", commands[2].ShortcutRune)
	}
}

func TestDefaultCommandsDoNotAdvertiseChordPrefixAsSingleShortcut(t *testing.T) {
	for _, command := range DefaultCommands(nil) {
		if command.ID == "edit_labels" {
			if command.ShortcutRune != 0 {
				t.Fatalf("edit_labels shortcut = %q, want no single-key shortcut because g starts navigation chords", command.ShortcutRune)
			}
			return
		}
	}
	t.Fatal("edit_labels command missing")
}

func TestDefaultKeybindingSettingsMatchClassicCommandShortcuts(t *testing.T) {
	entries := make(map[string]KeybindingSetting)
	for _, entry := range DefaultKeybindingSettings() {
		entries[entry.ID] = entry
	}
	for id, want := range map[string]string{
		"archive":        "x",
		"edit_title":     "e",
		"edit_issue":     "",
		"navigate_inbox": "",
	} {
		if got, ok := entries[id]; !ok || got.Default != want {
			t.Fatalf("settings default %q = %q, want %q", id, got.Default, want)
		}
	}
}

func TestUnsupportedMultiKeyCommandBindingIsRejected(t *testing.T) {
	for _, bindings := range []map[string]string{
		{"refresh": "g x"},
		{"open_palette": "; p"},
	} {
		if _, err := buildKeySequenceDispatcher(bindings); err == nil || !strings.Contains(strings.ToLower(err.Error()), "navigation") {
			t.Fatalf("buildKeySequenceDispatcher(%#v) error = %v, want unsupported non-navigation sequence error", bindings, err)
		}
		if _, err := config.NormalizeKeybindings(bindings); err == nil || !strings.Contains(strings.ToLower(err.Error()), "navigation") {
			t.Fatalf("NormalizeKeybindings(%#v) error = %v, want unsupported non-navigation sequence error", bindings, err)
		}
	}
}

func TestKeySequenceDispatcherRejectsUnreachableActions(t *testing.T) {
	for _, action := range []string{"clear_search", "not_a_real_action"} {
		if _, err := buildKeySequenceDispatcher(map[string]string{action: "x"}); err == nil {
			t.Errorf("buildKeySequenceDispatcher accepted unreachable action %q", action)
		}
	}
}

func TestKeySequenceDispatcherRejectsEffectiveGlobalContextCollisions(t *testing.T) {
	for _, bindings := range []map[string]string{
		{"refresh": "q"}, // retained global quit default
		{"refresh": "g"}, // classic table top-navigation key
	} {
		if _, err := buildKeySequenceDispatcher(bindings); err == nil {
			t.Fatalf("buildKeySequenceDispatcher(%#v) accepted an unreachable effective collision", bindings)
		}
	}
	if _, err := buildKeySequenceDispatcher(map[string]string{"quit": "r"}); err != nil {
		t.Fatalf("global quit key should claim the refresh default: %v", err)
	}
	if _, err := buildKeySequenceDispatcher(map[string]string{"quit": "z", "refresh": "q"}); err != nil {
		t.Fatalf("buildKeySequenceDispatcher should accept q after quit override: %v", err)
	}
}

func TestKeySequenceAliasDispatcherCanonicalizesEveryNavigationAlias(t *testing.T) {
	aliases := []struct {
		alias     string
		canonical string
	}{
		{"go_all", "navigate_all"}, {"all", "navigate_all"},
		{"go_mine", "navigate_mine"}, {"mine", "navigate_mine"},
		{"go_inbox", "navigate_inbox"}, {"inbox", "navigate_inbox"},
		{"go_triage", "navigate_triage"}, {"triage", "navigate_triage"},
		{"go_views", "navigate_views"}, {"views", "navigate_views"},
		{"go_favorites", "navigate_favorites"}, {"favorites", "navigate_favorites"},
		{"go_projects", "navigate_projects"}, {"projects", "navigate_projects"},
		{"go_initiatives", "navigate_initiatives"}, {"initiatives", "navigate_initiatives"},
		{"go_cycles", "navigate_cycles"}, {"cycles", "navigate_cycles"},
	}
	for _, test := range aliases {
		dispatcher, err := buildKeySequenceDispatcher(map[string]string{test.alias: "g z"})
		if err != nil || dispatcher == nil {
			t.Fatalf("alias %q dispatcher = (%v, %v), want nonnil", test.alias, dispatcher, err)
		}
		now := time.Unix(100, 0)
		dispatcher.Feed("global", 'g', now)
		result := dispatcher.Feed("global", 'z', now.Add(time.Millisecond))
		if result.CommandID != test.canonical {
			t.Fatalf("alias %q command = %q, want %q", test.alias, result.CommandID, test.canonical)
		}
	}
}

func TestDefaultKeybindingSettingsExposeOnlyReachableContexts(t *testing.T) {
	entries := make(map[string]KeybindingSetting)
	for _, entry := range DefaultKeybindingSettings() {
		entries[entry.ID] = entry
	}
	if _, ok := entries["clear_search"]; ok {
		t.Fatal("clear_search is hardwired to Esc and must not be configurable")
	}
	for action, context := range config.ConfigurableKeybindingActions() {
		entry, ok := entries[action]
		if !ok {
			t.Fatalf("configurable action %q is missing from settings", action)
		}
		if entry.Context != context {
			t.Fatalf("action %q context = %q, want %q", action, entry.Context, context)
		}
	}
	for _, entry := range DefaultKeybindingSettings() {
		if _, ok := config.KeybindingActionContext(entry.ID); !ok {
			t.Fatalf("settings exposes unregistered action %q", entry.ID)
		}
	}

	for _, command := range DefaultCommands(nil) {
		if command.ShortcutRune == 0 {
			continue
		}
		entry, ok := entries[command.ID]
		if !ok {
			t.Fatalf("shortcut command %q is missing from settings", command.ID)
		}
		if entry.Context != "navigation/issues" {
			t.Fatalf("shortcut command %q context = %q, want navigation/issues (details has no dispatcher)", command.ID, entry.Context)
		}
	}
}

func TestKeybindingReachabilityAcrossMainPanes(t *testing.T) {
	globalIDs := map[string]bool{
		"quit": true, "open_palette": true, "search": true,
		"navigate_all": true, "navigate_mine": true, "navigate_inbox": true,
		"navigate_triage": true, "navigate_views": true, "navigate_favorites": true,
		"navigate_projects": true, "navigate_initiatives": true, "navigate_cycles": true,
	}
	for _, entry := range DefaultKeybindingSettings() {
		wantContext := config.KeybindingContextNavigationIssues
		if globalIDs[entry.ID] {
			wantContext = config.KeybindingContextGlobal
		}
		if entry.Context != wantContext {
			t.Fatalf("action %q context = %q, want %q", entry.ID, entry.Context, wantContext)
		}
		for _, pane := range []string{"navigation", "issues", "details"} {
			wantReachable := wantContext == config.KeybindingContextGlobal || pane != "details"
			gotReachable := entry.Context == config.KeybindingContextGlobal ||
				(entry.Context == config.KeybindingContextNavigationIssues && pane != "details")
			if gotReachable != wantReachable {
				t.Fatalf("action %q reachability in %s = %v, want %v", entry.ID, pane, gotReachable, wantReachable)
			}
		}
	}
}
