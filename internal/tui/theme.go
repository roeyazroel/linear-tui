package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/roeyazroel/linear-tui/internal/config"
)

// Theme defines the color palette and styles for the application.
type Theme struct {
	Background    tcell.Color
	Foreground    tcell.Color
	Border        tcell.Color
	BorderFocus   tcell.Color
	SelectionText tcell.Color
	SelectionBg   tcell.Color
	HeaderBg      tcell.Color
	HeaderText    tcell.Color
	SecondaryText tcell.Color
	Accent        tcell.Color
	InputBg       tcell.Color

	// Status Colors
	StatusTodo       tcell.Color
	StatusInProgress tcell.Color
	StatusInReview   tcell.Color
	StatusTriage     tcell.Color
	StatusBacklog    tcell.Color
	StatusDone       tcell.Color
	StatusCanceled   tcell.Color

	// Status Background Colors (for pill/badge rendering in the table)
	StatusBgTodo       tcell.Color
	StatusBgInProgress tcell.Color
	StatusBgInReview   tcell.Color
	StatusBgTriage     tcell.Color
	StatusBgBacklog    tcell.Color
	StatusBgDone       tcell.Color
	StatusBgCanceled   tcell.Color

	// Priority Colors
	PriorityUrgent tcell.Color
	PriorityHigh   tcell.Color
	PriorityNormal tcell.Color
	PriorityLow    tcell.Color
	PriorityNone   tcell.Color

	// Priority Background Colors (for pill/badge rendering in the table)
	PriorityBgUrgent tcell.Color
	PriorityBgHigh   tcell.Color
	PriorityBgNormal tcell.Color
	PriorityBgLow    tcell.Color
}

// LinearTheme is the default dark theme inspired by Linear.
var LinearTheme = Theme{
	Background:    tcell.NewRGBColor(18, 18, 18),    // #121212
	Foreground:    tcell.NewRGBColor(235, 235, 245), // #EBEBF5
	Border:        tcell.NewRGBColor(60, 60, 60),    // #3C3C3C
	BorderFocus:   tcell.NewRGBColor(94, 106, 210),  // #5E6AD2 (Linear Purple-ish)
	SelectionText: tcell.ColorWhite,
	SelectionBg:   tcell.NewRGBColor(40, 40, 50),    // Slight purple tint dark bg
	HeaderBg:      tcell.NewRGBColor(30, 30, 30),    // #1E1E1E
	HeaderText:    tcell.NewRGBColor(160, 160, 160), // #A0A0A0
	SecondaryText: tcell.NewRGBColor(120, 120, 120), // #787878
	Accent:        tcell.NewRGBColor(94, 106, 210),  // #5E6AD2
	InputBg:       tcell.ColorDarkGray,

	StatusTodo:       tcell.NewRGBColor(94, 139, 255),  // Blue
	StatusInProgress: tcell.NewRGBColor(242, 201, 76),  // Yellow
	StatusInReview:   tcell.NewRGBColor(46, 204, 113),  // Green
	StatusTriage:     tcell.NewRGBColor(255, 152, 0),   // Orange
	StatusBacklog:    tcell.NewRGBColor(155, 89, 182),  // Purple
	StatusDone:       tcell.NewRGBColor(38, 166, 154),  // Teal
	StatusCanceled:   tcell.NewRGBColor(255, 80, 80),   // Red

	StatusBgTodo:       tcell.NewRGBColor(25, 40, 80),   // Dark blue bg
	StatusBgInProgress: tcell.NewRGBColor(80, 70, 20),   // Dark amber bg
	StatusBgInReview:   tcell.NewRGBColor(15, 70, 35),   // Dark green bg
	StatusBgTriage:     tcell.NewRGBColor(80, 50, 10),   // Dark orange bg
	StatusBgBacklog:    tcell.NewRGBColor(50, 25, 60),   // Dark purple bg
	StatusBgDone:       tcell.NewRGBColor(15, 55, 50),   // Dark teal bg
	StatusBgCanceled:   tcell.NewRGBColor(80, 25, 25),   // Dark red bg

	PriorityUrgent: tcell.NewRGBColor(255, 80, 80),   // Red
	PriorityHigh:   tcell.NewRGBColor(255, 152, 0),   // Orange
	PriorityNormal: tcell.NewRGBColor(46, 204, 113),  // Green
	PriorityLow:    tcell.NewRGBColor(46, 204, 113),  // Green
	PriorityNone:   tcell.NewRGBColor(120, 120, 120), // Secondary text

	PriorityBgUrgent: tcell.NewRGBColor(80, 25, 25),  // Dark red bg
	PriorityBgHigh:   tcell.NewRGBColor(80, 50, 10),  // Dark orange bg
	PriorityBgNormal: tcell.NewRGBColor(15, 70, 35),  // Dark green bg
	PriorityBgLow:    tcell.NewRGBColor(15, 70, 35),  // Dark green bg
}

// HighContrastTheme is a high contrast theme for improved legibility.
var HighContrastTheme = Theme{
	Background:    tcell.NewRGBColor(0, 0, 0),       // #000000
	Foreground:    tcell.NewRGBColor(255, 255, 255), // #FFFFFF
	Border:        tcell.NewRGBColor(255, 255, 255), // #FFFFFF
	BorderFocus:   tcell.NewRGBColor(255, 255, 0),   // #FFFF00
	SelectionText: tcell.NewRGBColor(0, 0, 0),       // #000000
	SelectionBg:   tcell.NewRGBColor(255, 255, 255), // #FFFFFF
	HeaderBg:      tcell.NewRGBColor(0, 0, 0),       // #000000
	HeaderText:    tcell.NewRGBColor(255, 255, 255), // #FFFFFF
	SecondaryText: tcell.NewRGBColor(200, 200, 200), // #C8C8C8
	Accent:        tcell.NewRGBColor(255, 255, 0),   // #FFFF00
	InputBg:       tcell.NewRGBColor(30, 30, 30),    // #1E1E1E

	StatusTodo:       tcell.NewRGBColor(100, 149, 255), // Blue
	StatusInProgress: tcell.NewRGBColor(255, 255, 0),   // Yellow
	StatusInReview:   tcell.NewRGBColor(0, 255, 0),     // Green
	StatusTriage:     tcell.NewRGBColor(255, 165, 0),   // Orange
	StatusBacklog:    tcell.NewRGBColor(200, 120, 255), // Purple
	StatusDone:       tcell.NewRGBColor(0, 200, 180),   // Teal
	StatusCanceled:   tcell.NewRGBColor(255, 0, 0),     // Red

	StatusBgTodo:       tcell.NewRGBColor(30, 45, 70),   // Dark blue bg
	StatusBgInProgress: tcell.NewRGBColor(60, 60, 0),    // Dark yellow bg
	StatusBgInReview:   tcell.NewRGBColor(0, 60, 0),     // Dark green bg
	StatusBgTriage:     tcell.NewRGBColor(60, 40, 0),    // Dark orange bg
	StatusBgBacklog:    tcell.NewRGBColor(50, 30, 70),   // Dark purple bg
	StatusBgDone:       tcell.NewRGBColor(0, 60, 55),    // Dark teal bg
	StatusBgCanceled:   tcell.NewRGBColor(60, 0, 0),     // Dark red bg

	PriorityUrgent: tcell.NewRGBColor(255, 60, 60),    // Bright red
	PriorityHigh:   tcell.NewRGBColor(255, 180, 0),    // Bright orange
	PriorityNormal: tcell.NewRGBColor(0, 255, 80),     // Bright green
	PriorityLow:    tcell.NewRGBColor(0, 255, 80),     // Bright green
	PriorityNone:   tcell.NewRGBColor(200, 200, 200),  // Secondary text

	PriorityBgUrgent: tcell.NewRGBColor(60, 15, 15),   // Dark red bg
	PriorityBgHigh:   tcell.NewRGBColor(60, 40, 0),    // Dark orange bg
	PriorityBgNormal: tcell.NewRGBColor(0, 60, 20),    // Dark green bg
	PriorityBgLow:    tcell.NewRGBColor(0, 60, 20),    // Dark green bg
}

// ColorBlindTheme is a color-blind friendly palette.
var ColorBlindTheme = Theme{
	Background:    tcell.NewRGBColor(16, 16, 16),    // #101010
	Foreground:    tcell.NewRGBColor(230, 230, 230), // #E6E6E6
	Border:        tcell.NewRGBColor(74, 74, 74),    // #4A4A4A
	BorderFocus:   tcell.NewRGBColor(0, 114, 178),   // #0072B2
	SelectionText: tcell.NewRGBColor(255, 255, 255), // #FFFFFF
	SelectionBg:   tcell.NewRGBColor(38, 54, 86),    // #263656
	HeaderBg:      tcell.NewRGBColor(28, 28, 28),    // #1C1C1C
	HeaderText:    tcell.NewRGBColor(207, 207, 207), // #CFCFCF
	SecondaryText: tcell.NewRGBColor(154, 154, 154), // #9A9A9A
	Accent:        tcell.NewRGBColor(0, 114, 178),   // #0072B2
	InputBg:       tcell.NewRGBColor(42, 42, 42),    // #2A2A2A

	StatusTodo:       tcell.NewRGBColor(86, 180, 233),  // Blue (#56B4E9)
	StatusInProgress: tcell.NewRGBColor(240, 228, 66),  // Yellow (#F0E442)
	StatusInReview:   tcell.NewRGBColor(0, 158, 115),   // Green (#009E73)
	StatusTriage:     tcell.NewRGBColor(230, 159, 0),   // Orange (#E69F00)
	StatusBacklog:    tcell.NewRGBColor(204, 121, 167), // Purple (#CC79A7)
	StatusDone:       tcell.NewRGBColor(0, 158, 115),   // Teal/Green (#009E73) — same green, distinct by context
	StatusCanceled:   tcell.NewRGBColor(213, 94, 0),    // Red-Orange (#D55E00)

	StatusBgTodo:       tcell.NewRGBColor(25, 55, 75),   // Dark blue bg
	StatusBgInProgress: tcell.NewRGBColor(70, 65, 20),   // Dark yellow bg
	StatusBgInReview:   tcell.NewRGBColor(10, 55, 40),   // Dark green bg
	StatusBgTriage:     tcell.NewRGBColor(65, 45, 10),   // Dark orange bg
	StatusBgBacklog:    tcell.NewRGBColor(55, 35, 50),   // Dark purple bg
	StatusBgDone:       tcell.NewRGBColor(10, 55, 40),   // Dark teal bg
	StatusBgCanceled:   tcell.NewRGBColor(65, 30, 10),   // Dark red-orange bg

	PriorityUrgent: tcell.NewRGBColor(213, 94, 0),    // Red-orange
	PriorityHigh:   tcell.NewRGBColor(230, 159, 0),   // Orange
	PriorityNormal: tcell.NewRGBColor(0, 158, 115),   // Green (#009E73)
	PriorityLow:    tcell.NewRGBColor(0, 158, 115),   // Green (#009E73)
	PriorityNone:   tcell.NewRGBColor(154, 154, 154), // Secondary text

	PriorityBgUrgent: tcell.NewRGBColor(60, 25, 10),  // Dark red-orange bg
	PriorityBgHigh:   tcell.NewRGBColor(60, 40, 10),  // Dark orange bg
	PriorityBgNormal: tcell.NewRGBColor(10, 50, 35),  // Dark green bg
	PriorityBgLow:    tcell.NewRGBColor(10, 50, 35),  // Dark green bg
}

// ThemeTags provides tview tag strings derived from a theme.
type ThemeTags struct {
	Foreground    string
	SecondaryText string
	HeaderText    string
	Accent        string
	Border        string
	Warning       string
	Error         string
}

// ThemeRegistry maps theme identifiers to theme palettes.
var ThemeRegistry = map[string]Theme{
	config.ThemeLinear:       LinearTheme,
	config.ThemeHighContrast: HighContrastTheme,
	config.ThemeColorBlind:   ColorBlindTheme,
}

// ResolveTheme returns the theme for a given name, or the default theme.
func ResolveTheme(name string) Theme {
	if theme, ok := ThemeRegistry[name]; ok {
		return theme
	}
	return LinearTheme
}

// NewThemeTags builds tag strings for dynamic color usage.
func NewThemeTags(theme Theme) ThemeTags {
	return ThemeTags{
		Foreground:    colorTag(theme.Foreground),
		SecondaryText: colorTag(theme.SecondaryText),
		HeaderText:    colorTag(theme.HeaderText),
		Accent:        colorTag(theme.Accent),
		Border:        colorTag(theme.Border),
		Warning:       colorTag(theme.StatusInProgress),
		Error:         colorTag(theme.StatusCanceled),
	}
}

func colorTag(color tcell.Color) string {
	if !color.Valid() {
		return "[default]"
	}
	css := color.CSS()
	if css == "" {
		if color.IsRGB() {
			css = fmt.Sprintf("#%06x", color.Hex())
		}
	}
	if css == "" {
		css = "default"
	}
	return "[" + css + "]"
}

// Icons for various UI elements.
var Icons = struct {
	Team       string
	Project    string
	List       string
	Todo       string
	InProgress string
	Done       string
	Priority   string
}{
	Team:       "📁 ",
	Project:    "📄 ",
	List:       "📑 ",
	Todo:       "○ ",
	InProgress: "◐ ",
	Done:       "✔ ", // or ●
	Priority:   "⚡",
}