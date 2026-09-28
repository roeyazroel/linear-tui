package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// SettingsFile represents the on-disk JSON with optional fields.
type SettingsFile struct {
	APIEndpoint    *string           `json:"api_endpoint"`
	Timeout        *string           `json:"timeout"`
	PageSize       *int              `json:"page_size"`
	CacheTTL       *string           `json:"cache_ttl"`
	SearchDebounce *string           `json:"search_debounce"`
	LogFile        *string           `json:"log_file"`
	LogLevel       *string           `json:"log_level"`
	Theme          *string           `json:"theme"`
	Density        *string           `json:"density"`
	AgentProvider  *string           `json:"agent_provider"`
	AgentSandbox   *string           `json:"agent_sandbox"`
	AgentModel     *string           `json:"agent_model"`
	AgentWorkspace *string           `json:"agent_workspace"`
	Keybindings    map[string]string `json:"keybindings"`
	DefaultTeam    *string           `json:"default_team"`
	DefaultProject *string           `json:"default_project"`
}

// Settings contains concrete settings values for UI and persistence.
type Settings struct {
	APIEndpoint    string            `json:"api_endpoint"`
	Timeout        string            `json:"timeout"`
	PageSize       int               `json:"page_size"`
	CacheTTL       string            `json:"cache_ttl"`
	SearchDebounce string            `json:"search_debounce"`
	LogFile        string            `json:"log_file"`
	LogLevel       string            `json:"log_level"`
	Theme          string            `json:"theme"`
	Density        string            `json:"density"`
	AgentProvider  string            `json:"agent_provider"`
	AgentSandbox   string            `json:"agent_sandbox"`
	AgentModel     string            `json:"agent_model"`
	AgentWorkspace string            `json:"agent_workspace"`
	Keybindings    map[string]string `json:"keybindings,omitempty"`
	DefaultTeam    string            `json:"default_team"`
	DefaultProject string            `json:"default_project"`
}

// DefaultSettings returns the default settings for the config file and UI.
func DefaultSettings() Settings {
	return Settings{
		APIEndpoint:    DefaultAPIEndpoint,
		Timeout:        DefaultTimeout.String(),
		PageSize:       DefaultPageSize,
		CacheTTL:       DefaultCacheTTL.String(),
		SearchDebounce: DefaultSearchDebounce.String(),
		LogFile:        getDefaultLogFile(),
		LogLevel:       DefaultLogLevel,
		Theme:          DefaultTheme,
		Density:        DefaultDensity,
		AgentProvider:  DefaultAgentProvider,
		AgentSandbox:   DefaultAgentSandbox,
		AgentModel:     "",
		AgentWorkspace: "",
		DefaultTeam:    "",
		DefaultProject: "",
	}
}

// SettingsFromConfig converts runtime config into settings values.
func SettingsFromConfig(cfg Config) Settings {
	return Settings{
		APIEndpoint:    cfg.APIEndpoint,
		Timeout:        cfg.Timeout.String(),
		PageSize:       cfg.PageSize,
		CacheTTL:       cfg.CacheTTL.String(),
		SearchDebounce: cfg.SearchDebounce.String(),
		LogFile:        cfg.LogFile,
		LogLevel:       cfg.LogLevel,
		Theme:          cfg.Theme,
		Density:        cfg.Density,
		AgentProvider:  cfg.AgentProvider,
		AgentSandbox:   cfg.AgentSandbox,
		AgentModel:     cfg.AgentModel,
		AgentWorkspace: cfg.AgentWorkspace,
		Keybindings:    cfg.Keybindings,
		DefaultTeam:    cfg.DefaultTeam,
		DefaultProject: cfg.DefaultProject,
	}
}

// ConfigFromSettings builds runtime configuration from settings and a resolved auth token.
// Callers should resolve the token via LINEAR_API_KEY or OAuth credentials before calling.
func ConfigFromSettings(apiKey string, settings Settings) (Config, error) {
	if apiKey == "" {
		return Config{}, fmt.Errorf("auth token is empty")
	}

	timeout, err := parseDuration(settings.Timeout, "timeout")
	if err != nil {
		return Config{}, err
	}

	cacheTTL, err := parseDuration(settings.CacheTTL, "cache_ttl")
	if err != nil {
		return Config{}, err
	}

	searchDebounce, err := parsePositiveDuration(settings.SearchDebounce, "search_debounce")
	if err != nil {
		return Config{}, err
	}

	if err := validatePageSize(settings.PageSize, "page_size"); err != nil {
		return Config{}, err
	}

	if err := validateLogLevel(settings.LogLevel, "log_level"); err != nil {
		return Config{}, err
	}

	theme := strings.TrimSpace(settings.Theme)
	if theme == "" {
		theme = DefaultTheme
	}
	if err := validateTheme(theme, "theme"); err != nil {
		return Config{}, err
	}

	density := strings.TrimSpace(settings.Density)
	if density == "" {
		density = DefaultDensity
	}
	if err := validateDensity(density, "density"); err != nil {
		return Config{}, err
	}

	if err := validateAgentProvider(settings.AgentProvider, "agent_provider"); err != nil {
		return Config{}, err
	}

	if err := validateAgentSandbox(settings.AgentSandbox, "agent_sandbox"); err != nil {
		return Config{}, err
	}

	normalizedKeybindings, err := normalizeKeybindings(settings.Keybindings, "keybindings")
	if err != nil {
		return Config{}, err
	}

	return Config{
		LinearAPIKey:   apiKey,
		APIEndpoint:    settings.APIEndpoint,
		Timeout:        timeout,
		PageSize:       settings.PageSize,
		CacheTTL:       cacheTTL,
		SearchDebounce: searchDebounce,
		LogFile:        settings.LogFile,
		LogLevel:       settings.LogLevel,
		Theme:          theme,
		Density:        density,
		AgentProvider:  settings.AgentProvider,
		AgentSandbox:   settings.AgentSandbox,
		AgentModel:     settings.AgentModel,
		AgentWorkspace: settings.AgentWorkspace,
		Keybindings:    normalizedKeybindings,
		DefaultTeam:    settings.DefaultTeam,
		DefaultProject: settings.DefaultProject,
	}, nil
}

// ConfigFilePath returns the default settings file path.
func ConfigFilePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}

	return filepath.Join(homeDir, ".linear-tui", "config.json"), nil
}

// EnsureSettingsFile ensures the settings file exists and returns its settings.
func EnsureSettingsFile(path string) (Settings, error) {
	if path == "" {
		return Settings{}, fmt.Errorf("settings path is empty")
	}

	if _, err := os.Stat(path); err == nil {
		return LoadSettings(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Settings{}, fmt.Errorf("stat settings file: %w", err)
	}

	settings := DefaultSettings()
	if err := SaveSettings(path, settings); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// LoadSettings loads settings from a JSON file and applies defaults.
func LoadSettings(path string) (Settings, error) {
	if path == "" {
		return Settings{}, fmt.Errorf("settings path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings file: %w", err)
	}

	var file SettingsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Settings{}, fmt.Errorf("parse settings file: %w", err)
	}

	settings := DefaultSettings()
	if file.APIEndpoint != nil {
		settings.APIEndpoint = *file.APIEndpoint
	}
	if file.Timeout != nil {
		settings.Timeout = *file.Timeout
	}
	if file.PageSize != nil {
		settings.PageSize = *file.PageSize
	}
	if file.CacheTTL != nil {
		settings.CacheTTL = *file.CacheTTL
	}
	if file.SearchDebounce != nil {
		settings.SearchDebounce = *file.SearchDebounce
	}
	if file.LogFile != nil {
		settings.LogFile = *file.LogFile
	}
	if file.LogLevel != nil {
		settings.LogLevel = *file.LogLevel
	}
	if file.Theme != nil {
		settings.Theme = *file.Theme
	}
	if file.Density != nil {
		settings.Density = *file.Density
	}
	if file.AgentProvider != nil {
		settings.AgentProvider = *file.AgentProvider
	}
	if file.AgentSandbox != nil {
		settings.AgentSandbox = *file.AgentSandbox
	}
	if file.AgentModel != nil {
		settings.AgentModel = *file.AgentModel
	}
	if file.AgentWorkspace != nil {
		settings.AgentWorkspace = *file.AgentWorkspace
	}
	if file.Keybindings != nil {
		settings.Keybindings = file.Keybindings
	}
	if file.DefaultTeam != nil {
		settings.DefaultTeam = *file.DefaultTeam
	}
	if file.DefaultProject != nil {
		settings.DefaultProject = *file.DefaultProject
	}

	return settings, nil
}

// SaveSettings writes settings to a JSON file, creating directories as needed.
func SaveSettings(path string, settings Settings) error {
	if path == "" {
		return fmt.Errorf("settings path is empty")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write settings file: %w", err)
	}

	return nil
}

// parseDuration parses a duration string with a labeled error message.
func parseDuration(value string, label string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", label, value, err)
	}

	return duration, nil
}

func parsePositiveDuration(value string, label string) (time.Duration, error) {
	duration, err := parseDuration(value, label)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0, got %s", label, duration)
	}
	return duration, nil
}

const (
	// KeybindingContextGlobal identifies actions handled by the application
	// capture before pane-specific dispatch.
	KeybindingContextGlobal = "global"
	// KeybindingContextNavigationIssues identifies palette command shortcuts.
	// They are dispatched from the navigation and issues panes only; details
	// deliberately has no command-shortcut fallback.
	KeybindingContextNavigationIssues = "navigation/issues"
)

// configurableKeybindingActions is the single allowlist shared by settings
// validation and the TUI keybinding editor. Keep hardwired actions (for
// example clear_search on Esc) out of this map so they cannot be advertised
// as editable settings.
var configurableKeybindingActions = map[string]string{
	"quit":                 KeybindingContextGlobal,
	"open_palette":         KeybindingContextGlobal,
	"search":               KeybindingContextGlobal,
	"navigate_all":         KeybindingContextGlobal,
	"navigate_mine":        KeybindingContextGlobal,
	"navigate_inbox":       KeybindingContextGlobal,
	"navigate_triage":      KeybindingContextGlobal,
	"navigate_views":       KeybindingContextGlobal,
	"navigate_favorites":   KeybindingContextGlobal,
	"navigate_projects":    KeybindingContextGlobal,
	"navigate_initiatives": KeybindingContextGlobal,
	"navigate_cycles":      KeybindingContextGlobal,
	"refresh":              KeybindingContextNavigationIssues,
	"open_browser":         KeybindingContextNavigationIssues,
	"copy_id":              KeybindingContextNavigationIssues,
	"copy_url":             KeybindingContextNavigationIssues,
	"assign_me":            KeybindingContextNavigationIssues,
	"unassign":             KeybindingContextNavigationIssues,
	"change_status":        KeybindingContextNavigationIssues,
	"set_cycle":            KeybindingContextNavigationIssues,
	"assign_user":          KeybindingContextNavigationIssues,
	"archive":              KeybindingContextNavigationIssues,
	"edit_title":           KeybindingContextNavigationIssues,
	"create_issue":         KeybindingContextNavigationIssues,
	"edit_issue":           KeybindingContextNavigationIssues,
	"view_parent":          KeybindingContextNavigationIssues,
	"expand_all":           KeybindingContextNavigationIssues,
	"collapse_all":         KeybindingContextNavigationIssues,
	"create_sub_issue":     KeybindingContextNavigationIssues,
	"set_parent":           KeybindingContextNavigationIssues,
	"remove_parent":        KeybindingContextNavigationIssues,
	"add_comment":          KeybindingContextNavigationIssues,
}

// keybindingAliases preserve the existing config spellings accepted by the
// navigation dispatcher. They are intentionally not listed in the settings
// editor, which exposes one canonical row per navigation destination.
var keybindingAliases = map[string]string{
	"go_all":         "navigate_all",
	"all":            "navigate_all",
	"go_mine":        "navigate_mine",
	"mine":           "navigate_mine",
	"go_inbox":       "navigate_inbox",
	"inbox":          "navigate_inbox",
	"go_triage":      "navigate_triage",
	"triage":         "navigate_triage",
	"go_views":       "navigate_views",
	"views":          "navigate_views",
	"go_favorites":   "navigate_favorites",
	"favorites":      "navigate_favorites",
	"go_projects":    "navigate_projects",
	"projects":       "navigate_projects",
	"go_initiatives": "navigate_initiatives",
	"initiatives":    "navigate_initiatives",
	"go_cycles":      "navigate_cycles",
	"cycles":         "navigate_cycles",
}

func canonicalKeybindingAction(action string) string {
	action = strings.TrimSpace(action)
	if canonical, ok := keybindingAliases[action]; ok {
		return canonical
	}
	return action
}

func keybindingActionContext(action string) (string, bool) {
	if context, ok := configurableKeybindingActions[action]; ok {
		return context, true
	}
	canonical, ok := keybindingAliases[action]
	if !ok {
		return "", false
	}
	context, ok := configurableKeybindingActions[canonical]
	return context, ok
}

func isNavigationKeybindingAction(action string) bool {
	if _, ok := defaultNavigationKeySequences[action]; ok {
		return true
	}
	canonical, ok := keybindingAliases[action]
	if !ok {
		return false
	}
	_, ok = defaultNavigationKeySequences[canonical]
	return ok
}

// ConfigurableKeybindingActions returns the action/context allowlist used by
// settings validation and the keybinding editor.
func ConfigurableKeybindingActions() map[string]string {
	actions := make(map[string]string, len(configurableKeybindingActions))
	for action, context := range configurableKeybindingActions {
		actions[action] = context
	}
	return actions
}

// KeybindingActionContext reports whether action is editable and, if so, the
// pane context where its configured value is dispatched.
func KeybindingActionContext(action string) (string, bool) {
	return keybindingActionContext(strings.TrimSpace(action))
}

// validatePageSize validates the allowed page size range.
func validatePageSize(pageSize int, label string) error {
	if pageSize < 1 || pageSize > 250 {
		return fmt.Errorf("%s must be between 1 and 250, got %d", label, pageSize)
	}

	return nil
}

// validateLogLevel validates the allowed log level values.
func validateLogLevel(logLevel string, label string) error {
	switch logLevel {
	case "debug", "info", "warning", "error":
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be debug, info, warning, or error", label, logLevel)
	}
}

// validateTheme validates the allowed theme values.
func validateTheme(theme string, label string) error {
	switch theme {
	case ThemeLinear, ThemeHighContrast, ThemeColorBlind, ThemeCatppuccinMocha:
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be linear, high_contrast, color_blind, or catppuccin_mocha", label, theme)
	}
}

// NormalizeKeySequence returns the canonical form of a key binding. Tokens
// are separated by one space and each token is one printable rune. Named
// terminal keys (for example Ctrl-K or Tab) are intentionally rejected until
// the input path can dispatch them consistently in every pane.
func NormalizeKeySequence(value string) (string, error) {
	return normalizeKeySequenceForAction("", value)
}

// NormalizeKeySequenceForAction is the action-aware form used by the runtime
// dispatcher. It permits an action to keep its own reserved default (for
// example open_palette mapped explicitly to ':') while rejecting that key for
// unrelated commands and rejecting actions that have no reachable dispatcher.
func NormalizeKeySequenceForAction(action, value string) (string, error) {
	return normalizeKeySequenceForAction(strings.TrimSpace(action), value)
}

func normalizeKeySequenceForAction(action, value string) (string, error) {
	if action != "" {
		if _, ok := keybindingActionContext(action); !ok {
			return "", fmt.Errorf("keybinding action %q is not configurable or reachable", action)
		}
	}
	tokens := strings.Fields(value)
	if len(tokens) == 0 {
		return "", fmt.Errorf("key sequence cannot be empty")
	}
	for _, token := range tokens {
		runes := []rune(token)
		if len(runes) != 1 {
			return "", fmt.Errorf("key sequence token %q must be a single printable character; named keys are unsupported", token)
		}
		if unicode.IsControl(runes[0]) || unicode.IsSpace(runes[0]) || !unicode.IsPrint(runes[0]) {
			return "", fmt.Errorf("key sequence token %q is not a printable key", token)
		}
		switch runes[0] {
		case '?':
			return "", fmt.Errorf("key sequence token %q is reserved for help", token)
		case ':':
			if action != "open_palette" {
				return "", fmt.Errorf("key sequence token %q is reserved for the command palette", token)
			}
		}
	}
	context, _ := keybindingActionContext(action)
	if context == KeybindingContextNavigationIssues && len(tokens) == 1 && isHardwiredIssueNavigationRune([]rune(tokens[0])[0]) {
		return "", fmt.Errorf("key sequence token %q is reserved for classic issue navigation or marking", tokens[0])
	}
	return strings.Join(tokens, " "), nil
}

func isHardwiredIssueNavigationRune(key rune) bool {
	switch key {
	case 'v', 'V', 'h', 'j', 'k', 'l', 'g', 'G':
		return true
	default:
		return false
	}
}

// KeySequenceTokens parses a canonical key sequence into its individual
// tokens. It is useful to callers that need deterministic duplicate/prefix
// validation without reimplementing the syntax rules.
func KeySequenceTokens(value string) ([]string, error) {
	normalized, err := NormalizeKeySequence(value)
	if err != nil {
		return nil, err
	}
	return strings.Split(normalized, " "), nil
}

// defaultKeybindingSequences mirrors every editable runtime binding. Keeping
// this contract in config (rather than deriving it from the TUI command
// palette) lets settings validation account for omitted values: an override
// can collide with a default that the user did not repeat in the config file.
// The values intentionally use the same canonical IDs and runes as the
// global input capture and DefaultCommands.
var defaultKeybindingSequences = map[string]string{
	"quit":             "q",
	"open_palette":     ":",
	"search":           "/",
	"refresh":          "r",
	"open_browser":     "o",
	"copy_id":          "y",
	"copy_url":         "w",
	"assign_me":        "m",
	"unassign":         "u",
	"change_status":    "s",
	"set_cycle":        "c",
	"assign_user":      "a",
	"archive":          "x",
	"create_issue":     "n",
	"edit_title":       "e",
	"view_parent":      "p",
	"expand_all":       "]",
	"collapse_all":     "[",
	"create_sub_issue": "b",
	"set_parent":       "i",
	"remove_parent":    "d",
	"add_comment":      "t",
}

// defaultNavigationKeySequences identifies the supported opt-in navigation
// chord actions. They deliberately have no default sequence: classic g/G
// table navigation remains available until a user configures a chord.
var defaultNavigationKeySequences = map[string]string{
	"navigate_all":         "",
	"navigate_mine":        "",
	"navigate_inbox":       "",
	"navigate_triage":      "",
	"navigate_views":       "",
	"navigate_favorites":   "",
	"navigate_projects":    "",
	"navigate_initiatives": "",
	"navigate_cycles":      "",
}

type normalizedKeybinding struct {
	action string
	tokens []string
}

// normalizeKeybindings canonicalizes user values and rejects deterministic
// duplicate/prefix conflicts across the global command/chord namespace.
func normalizeKeybindings(bindings map[string]string, label string) (map[string]string, error) {
	if bindings == nil {
		return nil, nil
	}
	actions := make([]string, 0, len(bindings))
	for action := range bindings {
		actions = append(actions, action)
	}
	sort.Strings(actions)

	normalized := make(map[string]string, len(bindings))
	owners := make(map[string]string, len(bindings))
	for _, action := range actions {
		canonical := canonicalKeybindingAction(action)
		if _, ok := keybindingActionContext(canonical); !ok {
			return nil, fmt.Errorf("invalid %s entry %q: action is not configurable or reachable", label, action)
		}
		if previous, exists := owners[canonical]; exists {
			return nil, fmt.Errorf("invalid %s: actions %q and %q duplicate after canonicalization as %q", label, previous, action, canonical)
		}
		sequence, err := normalizeKeySequenceForAction(canonical, bindings[action])
		if err != nil {
			return nil, fmt.Errorf("invalid %s entry %q: %w", label, action, err)
		}
		if isNavigationKeybindingAction(canonical) {
			tokens := strings.Split(sequence, " ")
			if len(tokens) < 2 {
				return nil, fmt.Errorf("invalid %s entry %q: navigation chord must contain at least two tokens", label, action)
			}
		} else if len(strings.Fields(sequence)) > 1 {
			return nil, fmt.Errorf("invalid %s entry %q: multi-key mappings are supported only for navigation chords", label, action)
		}
		normalized[canonical] = sequence
		owners[canonical] = action
	}

	// Build the effective set by overlaying every configured action on its
	// runtime default. This catches conflicts with defaults the user did not
	// explicitly repeat in the config file, including global-vs-context
	// collisions where the global input capture would otherwise win first.
	// Explicit single-key mappings claim an issue-command key exactly as the
	// palette runtime does: an untouched issue-command default using that key is
	// removed. Global defaults remain in the effective set so a command cannot
	// silently shadow a global capture key.
	claimedRunes := make(map[rune]string, len(normalized))
	for action, sequence := range normalized {
		tokens := strings.Fields(sequence)
		if len(tokens) != 1 {
			continue
		}
		runes := []rune(tokens[0])
		if len(runes) == 1 {
			claimedRunes[runes[0]] = action
		}
	}
	effective := make([]normalizedKeybinding, 0, len(defaultKeybindingSequences)+len(normalized))
	defaultActions := make([]string, 0, len(defaultKeybindingSequences))
	for action := range defaultKeybindingSequences {
		defaultActions = append(defaultActions, action)
	}
	sort.Strings(defaultActions)
	for _, action := range defaultActions {
		sequence := defaultKeybindingSequences[action]
		if configured, ok := normalized[action]; ok {
			sequence = configured
		} else if context, _ := keybindingActionContext(action); context == KeybindingContextNavigationIssues {
			runes := []rune(sequence)
			if len(runes) == 1 && claimedRunes[runes[0]] != "" && claimedRunes[runes[0]] != action {
				continue
			}
		}
		tokens := strings.Fields(sequence)
		effective = append(effective, normalizedKeybinding{action: action, tokens: tokens})
	}
	canonicalActions := make([]string, 0, len(normalized))
	for action := range normalized {
		canonicalActions = append(canonicalActions, action)
	}
	sort.Strings(canonicalActions)
	for _, action := range canonicalActions {
		if _, hasDefault := defaultKeybindingSequences[action]; hasDefault {
			continue
		}
		tokens := strings.Fields(normalized[action])
		effective = append(effective, normalizedKeybinding{action: action, tokens: tokens})
	}
	sort.SliceStable(effective, func(i, j int) bool { return effective[i].action < effective[j].action })
	for index, current := range effective {
		for _, previous := range effective[:index] {
			if sameKeySequenceTokens(previous.tokens, current.tokens) {
				return nil, fmt.Errorf("invalid %s: duplicate key sequence %q is bound to both %q and %q", label, strings.Join(current.tokens, " "), previous.action, current.action)
			}
			if keySequenceTokensPrefix(previous.tokens, current.tokens) || keySequenceTokensPrefix(current.tokens, previous.tokens) {
				return nil, fmt.Errorf("invalid %s: key sequence %q for %q conflicts with prefix %q for %q", label, strings.Join(current.tokens, " "), current.action, strings.Join(previous.tokens, " "), previous.action)
			}
		}
	}
	return normalized, nil
}

// NormalizeKeybindings canonicalizes a user keybinding map and validates its
// global duplicate/prefix safety rules.
func NormalizeKeybindings(bindings map[string]string) (map[string]string, error) {
	return normalizeKeybindings(bindings, "keybindings")
}

// ValidateKeybindings validates a user keybinding map without returning a
// normalized copy.
func ValidateKeybindings(bindings map[string]string) error {
	return validateKeybindings(bindings, "keybindings")
}

func sameKeySequenceTokens(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func keySequenceTokensPrefix(prefix, full []string) bool {
	if len(prefix) >= len(full) {
		return false
	}
	for index := range prefix {
		if prefix[index] != full[index] {
			return false
		}
	}
	return true
}

// validateKeybindings preserves the historical helper used by tests and
// callers while applying the canonical multi-token validation rules.
func validateKeybindings(bindings map[string]string, label string) error {
	_, err := normalizeKeybindings(bindings, label)
	return err
}

// validateDensity validates the allowed density values.
func validateDensity(density string, label string) error {
	switch density {
	case DensityComfortable, DensityCompact:
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be comfortable or compact", label, density)
	}
}

// validateAgentProvider validates the allowed agent providers.
func validateAgentProvider(provider string, label string) error {
	switch provider {
	case "cursor", "claude":
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be cursor or claude", label, provider)
	}
}

// validateAgentSandbox validates the allowed sandbox values.
func validateAgentSandbox(sandbox string, label string) error {
	switch sandbox {
	case "enabled", "disabled":
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be enabled or disabled", label, sandbox)
	}
}
