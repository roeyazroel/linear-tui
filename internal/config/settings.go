package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SettingsFile represents the on-disk JSON with optional fields.
type SettingsFile struct {
	APIEndpoint    *string `json:"api_endpoint"`
	Timeout        *string `json:"timeout"`
	PageSize       *int    `json:"page_size"`
	CacheTTL       *string `json:"cache_ttl"`
	SearchDebounce *string `json:"search_debounce"`
	LogFile        *string `json:"log_file"`
	LogLevel       *string `json:"log_level"`
	Theme          *string `json:"theme"`
	Density        *string `json:"density"`
	AgentProvider  *string `json:"agent_provider"`
	AgentSandbox   *string `json:"agent_sandbox"`
	AgentModel     *string `json:"agent_model"`
	AgentWorkspace *string `json:"agent_workspace"`
	DefaultTeam    *string `json:"default_team"`
	DefaultProject *string `json:"default_project"`
}

// Settings contains concrete settings values for UI and persistence.
type Settings struct {
	APIEndpoint    string `json:"api_endpoint"`
	Timeout        string `json:"timeout"`
	PageSize       int    `json:"page_size"`
	CacheTTL       string `json:"cache_ttl"`
	SearchDebounce string `json:"search_debounce"`
	LogFile        string `json:"log_file"`
	LogLevel       string `json:"log_level"`
	Theme          string `json:"theme"`
	Density        string `json:"density"`
	AgentProvider  string `json:"agent_provider"`
	AgentSandbox   string `json:"agent_sandbox"`
	AgentModel     string `json:"agent_model"`
	AgentWorkspace string `json:"agent_workspace"`
	DefaultTeam    string `json:"default_team"`
	DefaultProject string `json:"default_project"`
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
		DefaultTeam:    cfg.DefaultTeam,
		DefaultProject: cfg.DefaultProject,
	}
}

// ConfigFromSettings builds runtime configuration from settings and API key.
func ConfigFromSettings(apiKey string, settings Settings) (Config, error) {
	if apiKey == "" {
		return Config{}, fmt.Errorf("%s environment variable is not set", LinearAPIKeyEnv)
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
	case ThemeLinear, ThemeHighContrast, ThemeColorBlind:
		return nil
	default:
		return fmt.Errorf("invalid %s value %q: must be linear, high_contrast, or color_blind", label, theme)
	}
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
