package tui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/roeyazroel/linear-tui/internal/agents"
	"github.com/roeyazroel/linear-tui/internal/config"
	"github.com/roeyazroel/linear-tui/internal/logger"
)

const (
	defaultAgentModelLabel       = "default (use provider default)"
	settingsModalWidth           = 110
	settingsModalScreenMargin    = 4
	settingsModalHeaderFooterRow = 2
	settingsFormItemPadding      = 1
	settingsFormBorderPadding    = 1
)

// agentModelOption pairs a model id with its display label.
type agentModelOption struct {
	id    string
	label string
}

// cursorModelOptions returns Cursor model options, preferring the CLI list.
func cursorModelOptions() []agentModelOption {
	options, err := cursorModelOptionsFromCLI()
	if err == nil && len(options) > 0 {
		return options
	}
	return cursorModelFallbackOptions()
}

// cursorModelFallbackOptions returns a static fallback list for Cursor models.
func cursorModelFallbackOptions() []agentModelOption {
	return []agentModelOption{
		{id: "auto", label: "auto - Auto"},
		{id: "composer-1", label: "composer-1 - Composer 1"},
		{id: "gpt-5.2-codex", label: "gpt-5.2-codex - GPT-5.2 Codex"},
		{id: "gpt-5.2-codex-high", label: "gpt-5.2-codex-high - GPT-5.2 Codex High"},
		{id: "gpt-5.2-codex-low", label: "gpt-5.2-codex-low - GPT-5.2 Codex Low"},
		{id: "gpt-5.2-codex-xhigh", label: "gpt-5.2-codex-xhigh - GPT-5.2 Codex Extra High"},
		{id: "gpt-5.2-codex-fast", label: "gpt-5.2-codex-fast - GPT-5.2 Codex Fast"},
		{id: "gpt-5.2-codex-high-fast", label: "gpt-5.2-codex-high-fast - GPT-5.2 Codex High Fast"},
		{id: "gpt-5.2-codex-low-fast", label: "gpt-5.2-codex-low-fast - GPT-5.2 Codex Low Fast"},
		{id: "gpt-5.2-codex-xhigh-fast", label: "gpt-5.2-codex-xhigh-fast - GPT-5.2 Codex Extra High Fast"},
		{id: "gpt-5.1-codex-max", label: "gpt-5.1-codex-max - GPT-5.1 Codex Max"},
		{id: "gpt-5.1-codex-max-high", label: "gpt-5.1-codex-max-high - GPT-5.1 Codex Max High"},
		{id: "gpt-5.2", label: "gpt-5.2 - GPT-5.2"},
		{id: "opus-4.5-thinking", label: "opus-4.5-thinking - Claude 4.5 Opus (Thinking)"},
		{id: "gpt-5.2-high", label: "gpt-5.2-high - GPT-5.2 High"},
		{id: "gemini-3-pro", label: "gemini-3-pro - Gemini 3 Pro"},
		{id: "opus-4.5", label: "opus-4.5 - Claude 4.5 Opus"},
		{id: "sonnet-4.5", label: "sonnet-4.5 - Claude 4.5 Sonnet"},
		{id: "sonnet-4.5-thinking", label: "sonnet-4.5-thinking - Claude 4.5 Sonnet (Thinking)"},
		{id: "gpt-5.1-high", label: "gpt-5.1-high - GPT-5.1 High"},
		{id: "gemini-3-flash", label: "gemini-3-flash - Gemini 3 Flash"},
		{id: "grok", label: "grok - Grok"},
	}
}

// cursorModelOptionsFromCLI loads model options from cursor-agent.
func cursorModelOptionsFromCLI() ([]agentModelOption, error) {
	binary, err := resolveCursorAgentBinary()
	if err != nil {
		return nil, err
	}
	output, err := exec.Command(binary, "--list-models").CombinedOutput()
	if err != nil {
		logger.Debug("tui.settings: failed to list cursor models binary=%s error=%v", binary, err)
		return nil, fmt.Errorf("list models: %w", err)
	}
	options := parseCursorModelOptions(string(output))
	if len(options) == 0 {
		return nil, fmt.Errorf("no cursor models parsed")
	}
	return options, nil
}

// resolveCursorAgentBinary resolves the cursor-agent executable path.
func resolveCursorAgentBinary() (string, error) {
	if path, err := exec.LookPath("cursor-agent"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("agent"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("cursor-agent not found in PATH")
}

// parseCursorModelOptions parses `cursor-agent --list-models` output into options.
func parseCursorModelOptions(output string) []agentModelOption {
	clean := stripANSICodes(output)
	lines := strings.Split(clean, "\n")
	var options []agentModelOption
	for _, line := range lines {
		item := strings.TrimSpace(line)
		if item == "" {
			continue
		}
		lower := strings.ToLower(item)
		if strings.HasPrefix(lower, "loading models") || strings.HasPrefix(lower, "available models") || strings.HasPrefix(lower, "tip:") {
			continue
		}
		item = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(item, "(current)", ""), "(default)", ""))
		if item == "" {
			continue
		}
		id, label := parseModelLine(item)
		if id == "" {
			continue
		}
		options = append(options, agentModelOption{id: id, label: label})
	}
	return options
}

// parseModelLine splits a "id - label" line into id and label.
func parseModelLine(item string) (string, string) {
	parts := strings.SplitN(item, " - ", 2)
	if len(parts) == 1 {
		id := strings.TrimSpace(parts[0])
		return id, id
	}
	id := strings.TrimSpace(parts[0])
	label := strings.TrimSpace(parts[1])
	if id == "" || label == "" {
		return "", ""
	}
	return id, fmt.Sprintf("%s - %s", id, label)
}

// stripANSICodes removes ANSI escape sequences from CLI output.
func stripANSICodes(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	skipping := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if skipping {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				skipping = false
			}
			continue
		}
		if ch == 0x1b {
			skipping = true
			continue
		}
		if ch == '\r' {
			continue
		}
		builder.WriteByte(ch)
	}
	return builder.String()
}

// claudeModelOptions returns Claude model options supported by `claude --model`.
func claudeModelOptions() []agentModelOption {
	return []agentModelOption{
		{id: "sonnet", label: "Claude Sonnet"},
		{id: "opus", label: "Claude Opus"},
		{id: "haiku", label: "Claude Haiku"},
	}
}

// defaultAgentModelOptions returns the default-only model dropdown values.
func defaultAgentModelOptions() ([]string, []string) {
	return []string{defaultAgentModelLabel}, []string{""}
}

// selectAvailableProvider chooses a valid provider key from available options.
func selectAvailableProvider(configProvider string, available []string) string {
	normalized := strings.ToLower(strings.TrimSpace(configProvider))
	for _, option := range available {
		if option == normalized {
			return option
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return ""
}

// agentModelOptionsForProvider builds model labels and values for a provider.
func agentModelOptionsForProvider(provider string) ([]string, []string) {
	labels, values := defaultAgentModelOptions()
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return labels, values
	}
	var options []agentModelOption
	switch normalized {
	case "cursor":
		options = cursorModelOptions()
	case "claude":
		options = claudeModelOptions()
	default:
		return labels, values
	}
	for _, option := range options {
		labels = append(labels, option.label)
		values = append(values, option.id)
	}
	return labels, values
}

// SettingsModal manages the settings form overlay.
type SettingsModal struct {
	app                  *App
	modal                *tview.Flex
	modalBody            *tview.Flex
	modalContent         *tview.Flex
	form                 *tview.Form
	endpointField        *tview.InputField
	timeoutField         *tview.InputField
	pageSizeField        *tview.InputField
	cacheTTLField        *tview.InputField
	searchDebounceField  *tview.InputField
	logFileField         *tview.InputField
	logLevelField        *tview.DropDown
	logLevelOptions      []string
	themeField           *tview.DropDown
	themeOptions         []string
	themeValues          []string
	densityField         *tview.DropDown
	densityOptions       []string
	densityValues        []string
	agentProviderField   *tview.DropDown
	agentProviderOptions []string
	agentSandboxField    *tview.DropDown
	agentSandboxOptions  []string
	agentModelField      *tview.DropDown
	agentModelOptions    []string
	agentModelValues     []string
	agentWorkspaceField  *tview.InputField
	defaultTeamField     *tview.InputField
	defaultProjectField  *tview.InputField
}

// NewSettingsModal creates a new settings modal.
func NewSettingsModal(app *App) *SettingsModal {
	availableProviders := agents.AvailableProviderKeys(exec.LookPath)
	selectedProvider := selectAvailableProvider(config.DefaultAgentProvider, availableProviders)
	modelLabels, modelValues := agentModelOptionsForProvider(selectedProvider)
	sm := &SettingsModal{
		app:                  app,
		logLevelOptions:      []string{"debug", "info", "warning", "error"},
		themeOptions:         []string{"Linear", "High contrast", "Color-blind friendly"},
		themeValues:          []string{config.ThemeLinear, config.ThemeHighContrast, config.ThemeColorBlind},
		densityOptions:       []string{"Comfortable", "Compact"},
		densityValues:        []string{config.DensityComfortable, config.DensityCompact},
		agentProviderOptions: availableProviders,
		agentSandboxOptions:  []string{"enabled", "disabled"},
		agentModelOptions:    modelLabels,
		agentModelValues:     modelValues,
	}

	sm.form = tview.NewForm()
	sm.form.SetItemPadding(settingsFormItemPadding)
	sm.form.SetBorderPadding(settingsFormBorderPadding, settingsFormBorderPadding, settingsFormBorderPadding, settingsFormBorderPadding)
	sm.form.SetBackgroundColor(app.theme.HeaderBg)
	sm.form.SetFieldBackgroundColor(app.theme.InputBg)
	sm.form.SetFieldTextColor(app.theme.Foreground)
	sm.form.SetButtonBackgroundColor(app.theme.Accent)
	sm.form.SetButtonTextColor(app.theme.SelectionText)
	sm.form.SetLabelColor(app.theme.Foreground)
	sm.form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			sm.Hide()
			return nil
		}
		return event
	})

	sm.endpointField = tview.NewInputField().
		SetLabel("API endpoint").
		SetFieldWidth(60)
	sm.form.AddFormItem(sm.endpointField)

	sm.timeoutField = tview.NewInputField().
		SetLabel("Timeout").
		SetFieldWidth(20)
	sm.form.AddFormItem(sm.timeoutField)

	sm.pageSizeField = tview.NewInputField().
		SetLabel("Page size").
		SetFieldWidth(10)
	sm.form.AddFormItem(sm.pageSizeField)

	sm.cacheTTLField = tview.NewInputField().
		SetLabel("Cache TTL").
		SetFieldWidth(20)
	sm.form.AddFormItem(sm.cacheTTLField)

	sm.searchDebounceField = tview.NewInputField().
		SetLabel("Search debounce").
		SetFieldWidth(20)
	sm.form.AddFormItem(sm.searchDebounceField)

	sm.logFileField = tview.NewInputField().
		SetLabel("Log file").
		SetFieldWidth(60)
	sm.form.AddFormItem(sm.logFileField)

	sm.logLevelField = tview.NewDropDown().
		SetLabel("Log level").
		SetOptions(sm.logLevelOptions, nil)
	sm.logLevelField.SetFieldWidth(20)
	sm.logLevelField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.logLevelField)

	sm.themeField = tview.NewDropDown().
		SetLabel("Theme").
		SetOptions(sm.themeOptions, nil)
	sm.themeField.SetFieldWidth(30)
	sm.themeField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.themeField)

	sm.densityField = tview.NewDropDown().
		SetLabel("Density").
		SetOptions(sm.densityOptions, nil)
	sm.densityField.SetFieldWidth(20)
	sm.densityField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.densityField)

	sm.agentProviderField = tview.NewDropDown().
		SetLabel("Agent provider").
		SetOptions(sm.agentProviderOptions, func(text string, index int) {
			_ = index
			sm.setAgentModelOptionsForProvider(text)
		})
	sm.agentProviderField.SetFieldWidth(20)
	sm.agentProviderField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.agentProviderField)

	sm.agentSandboxField = tview.NewDropDown().
		SetLabel("Agent sandbox").
		SetOptions(sm.agentSandboxOptions, nil)
	sm.agentSandboxField.SetFieldWidth(20)
	sm.agentSandboxField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.agentSandboxField)

	sm.agentModelField = tview.NewDropDown().
		SetLabel("Agent model").
		SetOptions(sm.agentModelOptions, nil)
	sm.agentModelField.SetFieldWidth(40)
	sm.agentModelField.SetListStyles(
		tcell.StyleDefault.Background(app.theme.HeaderBg).Foreground(app.theme.Foreground),
		tcell.StyleDefault.Background(app.theme.Accent).Foreground(app.theme.SelectionText),
	)
	sm.form.AddFormItem(sm.agentModelField)

	sm.agentWorkspaceField = tview.NewInputField().
		SetLabel("Agent workspace (optional; blank uses CWD)").
		SetFieldWidth(60)
	sm.form.AddFormItem(sm.agentWorkspaceField)

	sm.defaultTeamField = tview.NewInputField().
		SetLabel("Default team (key or name; blank opens All Issues)").
		SetFieldWidth(40)
	sm.form.AddFormItem(sm.defaultTeamField)

	sm.defaultProjectField = tview.NewInputField().
		SetLabel("Default project (name; requires default team)").
		SetFieldWidth(40)
	sm.form.AddFormItem(sm.defaultProjectField)

	sm.form.AddButton("Save", func() {
		sm.saveSettings()
	})
	sm.form.AddButton("Cancel", func() {
		sm.Hide()
	})

	titleView := tview.NewTextView()
	titleView.SetText("Settings")
	titleView.SetTextColor(app.theme.Accent)
	titleView.SetBackgroundColor(app.theme.HeaderBg)

	helpView := tview.NewTextView()
	helpView.SetText("Tab: next field | Enter: open dropdown | Esc: cancel")
	helpView.SetTextColor(app.theme.SecondaryText)
	helpView.SetBackgroundColor(app.theme.HeaderBg)
	helpView.SetTextAlign(tview.AlignCenter)

	sm.modalContent = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(titleView, 1, 0, false).
		AddItem(sm.form, 0, 1, true).
		AddItem(helpView, 1, 0, false)
	sm.modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	sm.modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" Settings ").
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	sm.modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	sm.modalBody = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(sm.modalContent, sm.settingsModalHeight(), 0, true).
		AddItem(nil, 0, 1, false)

	sm.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(sm.modalBody, settingsModalWidth, 0, true).
		AddItem(nil, 0, 1, false)
	sm.modal.SetBackgroundColor(app.theme.Background)

	return sm
}

// Show displays the settings modal with current configuration values.
func (sm *SettingsModal) Show() {
	logger.Debug("tui.settings: showing settings modal")
	settings := config.SettingsFromConfig(sm.app.config)
	availableProviders := agents.AvailableProviderKeys(exec.LookPath)
	sm.setAgentProviderOptions(availableProviders)
	selectedProvider := selectAvailableProvider(settings.AgentProvider, availableProviders)

	sm.endpointField.SetText(settings.APIEndpoint)
	sm.timeoutField.SetText(settings.Timeout)
	sm.pageSizeField.SetText(strconv.Itoa(settings.PageSize))
	sm.cacheTTLField.SetText(settings.CacheTTL)
	sm.searchDebounceField.SetText(settings.SearchDebounce)
	sm.logFileField.SetText(settings.LogFile)
	sm.setLogLevelSelection(settings.LogLevel)
	sm.setThemeSelection(settings.Theme)
	sm.setDensitySelection(settings.Density)
	sm.setAgentProviderSelection(selectedProvider)
	sm.setAgentSandboxSelection(settings.AgentSandbox)
	sm.setAgentModelOptionsForProvider(selectedProvider)
	sm.setAgentModelSelection(settings.AgentModel)
	sm.agentWorkspaceField.SetText(settings.AgentWorkspace)
	sm.defaultTeamField.SetText(settings.DefaultTeam)
	sm.defaultProjectField.SetText(settings.DefaultProject)

	sm.updateModalHeight()
	sm.app.pages.AddPage("settings", sm.modal, true, true)
	sm.app.pages.SendToFront("settings")
	sm.app.app.SetFocus(sm.form)
}

// updateModalHeight recalculates and applies the modal height to fit content.
func (sm *SettingsModal) updateModalHeight() {
	if sm.modalBody == nil || sm.modalContent == nil {
		return
	}
	sm.modalBody.ResizeItem(sm.modalContent, sm.settingsModalHeight(), 0)
}

// settingsModalHeight calculates the modal height with screen-aware clamping.
func (sm *SettingsModal) settingsModalHeight() int {
	contentHeight := sm.settingsFormHeight() + settingsModalHeaderFooterRow
	padding := sm.app.density.ModalPadding
	totalHeight := contentHeight + padding.Top + padding.Bottom + 2
	maxHeight := 0
	if sm.app != nil && sm.app.pages != nil {
		_, _, _, screenHeight := sm.app.pages.GetRect()
		if screenHeight > 0 {
			maxHeight = screenHeight - settingsModalScreenMargin
			if maxHeight < 1 {
				maxHeight = screenHeight
			}
		}
	}
	if maxHeight > 0 && totalHeight > maxHeight {
		return maxHeight
	}
	return totalHeight
}

// settingsFormHeight computes the form height including padding and buttons.
func (sm *SettingsModal) settingsFormHeight() int {
	if sm.form == nil {
		return 0
	}
	itemCount := sm.form.GetFormItemCount()
	height := settingsFormBorderPadding * 2
	for i := 0; i < itemCount; i++ {
		item := sm.form.GetFormItem(i)
		itemHeight := item.GetFieldHeight()
		if itemHeight <= 0 {
			itemHeight = tview.DefaultFormFieldHeight
		}
		height += itemHeight + settingsFormItemPadding
	}
	if sm.form.GetButtonCount() > 0 {
		height++
	}
	return height
}

// currentAgentModelValue returns the currently selected model value.
func (sm *SettingsModal) currentAgentModelValue() string {
	index, _ := sm.agentModelField.GetCurrentOption()
	if index >= 0 && index < len(sm.agentModelValues) {
		return sm.agentModelValues[index]
	}
	return ""
}

// setAgentModelOptionsForProvider updates model options for the given provider.
func (sm *SettingsModal) setAgentModelOptionsForProvider(provider string) {
	currentValue := sm.currentAgentModelValue()
	labels, values := agentModelOptionsForProvider(provider)
	sm.agentModelOptions = labels
	sm.agentModelValues = values
	sm.agentModelField.SetOptions(sm.agentModelOptions, nil)
	if currentValue != "" {
		sm.setAgentModelSelection(currentValue)
		return
	}
	sm.setAgentModelSelection("")
}

// setAgentProviderOptions updates the provider dropdown options and callback.
func (sm *SettingsModal) setAgentProviderOptions(options []string) {
	sm.agentProviderOptions = options
	if sm.agentProviderField == nil {
		return
	}
	sm.agentProviderField.SetOptions(sm.agentProviderOptions, func(text string, index int) {
		_ = index
		sm.setAgentModelOptionsForProvider(text)
	})
}

// Hide hides the settings modal.
func (sm *SettingsModal) Hide() {
	logger.Debug("tui.settings: hiding settings modal")
	sm.app.pages.RemovePage("settings")
	sm.app.updateFocus()
}

// HandleKey handles keyboard input for the settings modal.
func (sm *SettingsModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		sm.Hide()
		return nil
	}
	return event
}

// saveSettings validates input, persists settings, and applies them to the app.
func (sm *SettingsModal) saveSettings() {
	settings, err := sm.settingsFromForm()
	if err != nil {
		logger.ErrorWithErr(err, "tui.settings: failed to build settings from form")
		sm.app.updateStatusBarWithError(err)
		return
	}

	newCfg, err := config.ConfigFromSettings(sm.app.config.LinearAPIKey, settings)
	if err != nil {
		logger.ErrorWithErr(err, "tui.settings: failed to parse settings")
		sm.app.updateStatusBarWithError(err)
		return
	}

	settingsPath, err := config.ConfigFilePath()
	if err != nil {
		logger.ErrorWithErr(err, "tui.settings: failed to get config file path")
		sm.app.updateStatusBarWithError(err)
		return
	}

	if err := config.SaveSettings(settingsPath, settings); err != nil {
		logger.ErrorWithErr(err, "tui.settings: failed to save settings path=%s", settingsPath)
		sm.app.updateStatusBarWithError(err)
		return
	}

	logger.Debug("tui.settings: settings saved successfully path=%s", settingsPath)
	sm.Hide()
	sm.app.applySettings(newCfg)
}

func (sm *SettingsModal) settingsFromForm() (config.Settings, error) {
	pageSizeText := strings.TrimSpace(sm.pageSizeField.GetText())
	pageSize, err := strconv.Atoi(pageSizeText)
	if err != nil {
		return config.Settings{}, fmt.Errorf("page size must be a number: %w", err)
	}

	_, logLevel := sm.logLevelField.GetCurrentOption()
	if logLevel == "" {
		logLevel = config.DefaultLogLevel
	}

	theme := sm.currentThemeValue()
	if theme == "" {
		theme = config.DefaultTheme
	}

	density := sm.currentDensityValue()
	if density == "" {
		density = config.DefaultDensity
	}

	_, agentProvider := sm.agentProviderField.GetCurrentOption()
	if len(sm.agentProviderOptions) == 0 {
		agentProvider = strings.TrimSpace(sm.app.config.AgentProvider)
	}
	if agentProvider == "" {
		agentProvider = config.DefaultAgentProvider
	}

	_, agentSandbox := sm.agentSandboxField.GetCurrentOption()
	if agentSandbox == "" {
		agentSandbox = config.DefaultAgentSandbox
	}

	agentModel := ""
	modelIndex, _ := sm.agentModelField.GetCurrentOption()
	if modelIndex >= 0 && modelIndex < len(sm.agentModelValues) {
		agentModel = sm.agentModelValues[modelIndex]
	}

	settings := config.Settings{
		APIEndpoint:    strings.TrimSpace(sm.endpointField.GetText()),
		Timeout:        strings.TrimSpace(sm.timeoutField.GetText()),
		PageSize:       pageSize,
		CacheTTL:       strings.TrimSpace(sm.cacheTTLField.GetText()),
		SearchDebounce: strings.TrimSpace(sm.searchDebounceField.GetText()),
		LogFile:        strings.TrimSpace(sm.logFileField.GetText()),
		LogLevel:       logLevel,
		Theme:          theme,
		Density:        density,
		AgentProvider:  agentProvider,
		AgentSandbox:   agentSandbox,
		AgentModel:     agentModel,
		AgentWorkspace: strings.TrimSpace(sm.agentWorkspaceField.GetText()),
		DefaultTeam:    strings.TrimSpace(sm.defaultTeamField.GetText()),
		DefaultProject: strings.TrimSpace(sm.defaultProjectField.GetText()),
	}
	return settings, nil
}

// setLogLevelSelection updates the dropdown selection to match the provided level.
func (sm *SettingsModal) setLogLevelSelection(level string) {
	selected := 0
	for i, option := range sm.logLevelOptions {
		if option == config.DefaultLogLevel {
			selected = i
		}
		if option == level {
			selected = i
			break
		}
	}
	sm.logLevelField.SetCurrentOption(selected)
}

// currentThemeValue returns the currently selected theme value.
func (sm *SettingsModal) currentThemeValue() string {
	index, _ := sm.themeField.GetCurrentOption()
	if index >= 0 && index < len(sm.themeValues) {
		return sm.themeValues[index]
	}
	return ""
}

// setThemeSelection updates the dropdown selection to match the provided theme.
func (sm *SettingsModal) setThemeSelection(theme string) {
	selected := 0
	for i, value := range sm.themeValues {
		if value == config.DefaultTheme {
			selected = i
		}
		if value == theme {
			selected = i
			break
		}
	}
	sm.themeField.SetCurrentOption(selected)
}

// currentDensityValue returns the currently selected density value.
func (sm *SettingsModal) currentDensityValue() string {
	index, _ := sm.densityField.GetCurrentOption()
	if index >= 0 && index < len(sm.densityValues) {
		return sm.densityValues[index]
	}
	return ""
}

// setDensitySelection updates the dropdown selection to match the provided density.
func (sm *SettingsModal) setDensitySelection(density string) {
	selected := 0
	for i, value := range sm.densityValues {
		if value == config.DefaultDensity {
			selected = i
		}
		if value == density {
			selected = i
			break
		}
	}
	sm.densityField.SetCurrentOption(selected)
}

// setAgentProviderSelection updates the dropdown selection to match the provided provider.
func (sm *SettingsModal) setAgentProviderSelection(provider string) {
	if len(sm.agentProviderOptions) == 0 {
		return
	}
	selected := 0
	for i, option := range sm.agentProviderOptions {
		if option == provider {
			selected = i
			break
		}
	}
	sm.agentProviderField.SetCurrentOption(selected)
}

// setAgentSandboxSelection updates the dropdown selection to match the provided sandbox value.
func (sm *SettingsModal) setAgentSandboxSelection(sandbox string) {
	selected := 0
	for i, option := range sm.agentSandboxOptions {
		if option == config.DefaultAgentSandbox {
			selected = i
		}
		if option == sandbox {
			selected = i
			break
		}
	}
	sm.agentSandboxField.SetCurrentOption(selected)
}

// setAgentModelSelection updates the dropdown selection to match the provided model.
func (sm *SettingsModal) setAgentModelSelection(model string) {
	selected := 0
	for i, value := range sm.agentModelValues {
		if value == "" {
			selected = i
		}
		if value == model {
			selected = i
			break
		}
	}
	sm.agentModelField.SetCurrentOption(selected)
}
