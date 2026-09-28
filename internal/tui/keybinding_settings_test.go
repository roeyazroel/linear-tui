package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/linearapi"
)

func newKeybindingSettingsTestApp(t *testing.T) *App {
	t.Helper()
	settings := config.DefaultSettings()
	cfg, err := config.ConfigFromSettings("test-key", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}
	return NewApp(&linearapi.Client{}, cfg, nil)
}

func keybindingIndexByID(modal *SettingsModal, id string) int {
	for index, entry := range modal.keybindingEntries {
		if entry.ID == id {
			return index
		}
	}
	return -1
}

func selectKeybindingForTest(t *testing.T, modal *SettingsModal, id string) {
	t.Helper()
	index := keybindingIndexByID(modal, id)
	if index < 0 {
		t.Fatalf("keybinding %q is not exposed by the editor", id)
	}
	modal.keybindingList.SetCurrentItem(index)
	modal.selectKeybinding(index)
}

func TestSettingsKeybindingEditorDraftLifecycle(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	original := map[string]string{"navigate_all": "g x"}
	app.config.Keybindings = original
	modal := NewSettingsModal(app)
	modal.Show()
	modal.OpenKeybindingEditor()
	if !app.pages.HasPage(settingsKeybindingsPageName) {
		t.Fatal("OpenKeybindingEditor did not add editor page")
	}

	selectKeybindingForTest(t, modal, "navigate_all")
	modal.keybindingInput.SetText("g z")
	if !modal.commitSelectedKeybinding() {
		t.Fatal("valid multi-key binding was rejected")
	}
	if got := modal.keybindingDraft["navigate_all"]; got != "g z" {
		t.Fatalf("draft navigate_all = %q, want g z", got)
	}

	selectKeybindingForTest(t, modal, "refresh")
	modal.keybindingInput.SetText("g z")
	if modal.commitSelectedKeybinding() {
		t.Fatal("duplicate/prefix binding unexpectedly committed")
	}
	if !strings.Contains(strings.ToLower(modal.keybindingStatus.GetText(true)), "navigation") {
		t.Fatalf("validation status = %q, want unsupported non-navigation sequence error", modal.keybindingStatus.GetText(true))
	}
	if _, exists := modal.keybindingDraft["refresh"]; exists {
		t.Fatal("invalid binding mutated the draft")
	}

	selectKeybindingForTest(t, modal, "navigate_all")
	modal.resetSelectedKeybinding()
	if _, exists := modal.keybindingDraft["navigate_all"]; exists {
		t.Fatal("reset did not remove the override")
	}
	modal.closeKeybindingEditor(false)
	if got := modal.keybindingDraft["navigate_all"]; got != "g x" {
		t.Fatalf("canceled editor draft = %q, want original g x", got)
	}
	if app.config.Keybindings["navigate_all"] != "g x" {
		t.Fatalf("canceled editor mutated app config: %#v", app.config.Keybindings)
	}

	modal.OpenKeybindingEditor()
	selectKeybindingForTest(t, modal, "refresh")
	modal.keybindingInput.SetText("R")
	if !modal.commitSelectedKeybinding() {
		t.Fatal("valid single-key binding was rejected")
	}
	modal.closeKeybindingEditor(true)
	settings, err := modal.settingsFromForm()
	if err != nil {
		t.Fatalf("settingsFromForm() error: %v", err)
	}
	if settings.Keybindings["refresh"] != "R" || settings.Keybindings["navigate_all"] != "g x" {
		t.Fatalf("settings keybindings = %#v, want refresh=R and navigate_all=g x", settings.Keybindings)
	}
	newCfg, err := config.ConfigFromSettings(app.config.LinearAPIKey, settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}
	if settingsRequireDataReload(app.config, newCfg) {
		t.Fatal("keybinding-only settings change requested a network/data reload")
	}
	modal.Hide()
	if app.config.Keybindings["refresh"] != "" {
		t.Fatalf("settings cancel changed app config: %#v", app.config.Keybindings)
	}
}

func TestSettingsKeybindingEditorBuildsClassicShell(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	modal := NewSettingsModal(app)
	modal.Show()
	modal.OpenKeybindingEditor()
	if modal.keybindingEditor == nil || modal.keybindingList == nil || modal.keybindingInput == nil {
		t.Fatal("keybinding editor did not build its shell")
	}
	if modal.keybindingEditor.GetItemCount() != 3 {
		t.Fatalf("editor outer shell item count = %d, want 3", modal.keybindingEditor.GetItemCount())
	}
	modal.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	if app.pages.HasPage(settingsKeybindingsPageName) {
		t.Fatal("Esc did not cancel the keybinding editor")
	}
}

func TestSettingsThemeSelectionPreservesCatppuccin(t *testing.T) {
	app := NewApp(&linearapi.Client{}, config.Config{Theme: config.ThemeCatppuccinMocha}, nil)
	modal := NewSettingsModal(app)
	modal.Show()
	defer modal.Hide()
	if got := modal.currentThemeValue(); got != config.ThemeCatppuccinMocha {
		t.Fatalf("selected theme = %q, want %q", got, config.ThemeCatppuccinMocha)
	}
	if index, label := modal.themeField.GetCurrentOption(); index < 0 || label != "Catppuccin Mocha" {
		t.Fatalf("theme dropdown selection = (%d, %q), want Catppuccin Mocha", index, label)
	}
}

func TestSettingsKeybindingEditorStatesReachableContexts(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	modal := NewSettingsModal(app)
	modal.Show()
	modal.OpenKeybindingEditor()
	if keybindingIndexByID(modal, "clear_search") >= 0 {
		t.Fatal("settings editor exposes hardwired clear_search Esc binding")
	}
	selectKeybindingForTest(t, modal, "refresh")
	if status := modal.keybindingStatus.GetText(true); !strings.Contains(status, "navigation/issues") {
		t.Fatalf("refresh status = %q, want navigation/issues context", status)
	}
	selectKeybindingForTest(t, modal, "search")
	if status := modal.keybindingStatus.GetText(true); !strings.Contains(status, "global") {
		t.Fatalf("search status = %q, want global context", status)
	}
}

func TestSettingsKeybindingEditorAllowsCommandShortcutClaims(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	modal := NewSettingsModal(app)
	modal.Show()
	defer modal.Hide()
	modal.OpenKeybindingEditor()
	selectKeybindingForTest(t, modal, "copy_url")
	modal.keybindingInput.SetText("y")
	if !modal.commitSelectedKeybinding() {
		t.Fatalf("copy_url=y was rejected: %s", modal.keybindingStatus.GetText(true))
	}
	modal.closeKeybindingEditor(true)
	settings, err := modal.settingsFromForm()
	if err != nil {
		t.Fatalf("settingsFromForm() error: %v", err)
	}
	if _, err := config.ConfigFromSettings(app.config.LinearAPIKey, settings); err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}
	commands := DefaultCommands(app)
	applyCommandKeybindings(commands, settings.Keybindings)
	if findCommandByID(commands, "copy_url").ShortcutRune != 'y' || findCommandByID(commands, "copy_id").ShortcutRune != 0 {
		t.Fatalf("claimed command shortcuts = copy_url:%q copy_id:%q, want y and none", findCommandByID(commands, "copy_url").ShortcutRune, findCommandByID(commands, "copy_id").ShortcutRune)
	}
}

func TestSettingsKeybindingEditorCommitsFirstBindingFromDefaultSettings(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	modal := NewSettingsModal(app)
	modal.Show()
	defer modal.Hide()
	modal.OpenKeybindingEditor()
	selectKeybindingForTest(t, modal, "navigate_inbox")
	modal.handleKeybindingEditorKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	input := modal.keybindingInput.InputHandler()
	for _, key := range []rune{'g', ' ', 'i'} {
		input(tcell.NewEventKey(tcell.KeyRune, key, tcell.ModNone), func(tview.Primitive) {})
	}
	modal.handleKeybindingEditorKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if got := modal.keybindingDraft["navigate_inbox"]; got != "g i" {
		t.Fatalf("first configured binding = %q, want g i", got)
	}
}

func TestSettingsKeybindingEditorCtrlSAppliesAndOuterSavePersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	app := newKeybindingSettingsTestApp(t)
	modal := app.settingsModal
	modal.Show()
	modal.OpenKeybindingEditor()
	selectKeybindingForTest(t, modal, "navigate_inbox")
	capture := app.app.GetInputCapture()
	modal.keybindingInput.SetText("g i")

	if got := capture(tcell.NewEventKey(tcell.KeyCtrlS, 0, tcell.ModCtrl)); got != nil {
		t.Fatalf("Ctrl-S input capture returned %v, want consumed", got)
	}
	if app.pages.HasPage(settingsKeybindingsPageName) {
		t.Fatal("Ctrl-S did not apply and close the keybinding editor")
	}
	if got := modal.keybindingDraft["navigate_inbox"]; got != "g i" {
		t.Fatalf("Ctrl-S draft binding = %q, want g i", got)
	}

	// The outer Settings Save action delegates to saveSettings.
	modal.saveSettings()
	path, err := config.ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath() error: %v", err)
	}
	if want := filepath.Join(home, ".linear-tui", "config.json"); path != want {
		t.Fatalf("ConfigFilePath() = %q, want %q", path, want)
	}
	settings, err := config.LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}
	if got := settings.Keybindings["navigate_inbox"]; got != "g i" {
		t.Fatalf("persisted navigate_inbox binding = %q, want g i", got)
	}
}

func TestSettingsKeybindingEditorCtrlEnterStillApplies(t *testing.T) {
	app := newKeybindingSettingsTestApp(t)
	modal := app.settingsModal
	modal.Show()
	modal.OpenKeybindingEditor()
	selectKeybindingForTest(t, modal, "navigate_all")
	modal.keybindingInput.SetText("g a")

	if got := app.app.GetInputCapture()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModCtrl)); got != nil {
		t.Fatalf("Ctrl-Enter input capture returned %v, want consumed", got)
	}
	if app.pages.HasPage(settingsKeybindingsPageName) {
		t.Fatal("Ctrl-Enter did not apply and close the keybinding editor")
	}
	if got := modal.keybindingDraft["navigate_all"]; got != "g a" {
		t.Fatalf("Ctrl-Enter draft binding = %q, want g a", got)
	}
}
