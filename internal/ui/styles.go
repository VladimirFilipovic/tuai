package ui

import (
	"image/color"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// terminalIsDark tracks the terminal background brightness, populated from
// tea.BackgroundColorMsg in the app. Defaults to dark which is the common
// case; styles get rebuilt once we know for sure.
var terminalIsDark = true

// SetTerminalIsDark records the detected terminal brightness and rebuilds
// the style cache. Called from the bubbletea Update on BackgroundColorMsg.
func SetTerminalIsDark(dark bool) {
	if terminalIsDark == dark {
		return
	}
	terminalIsDark = dark
	rebuildStyles()
}

// subtleBg returns the bubble background tint — a small step away from the
// terminal background, picked to read as "shaded" without fighting the theme.
func subtleBg() color.Color {
	if terminalIsDark {
		return lipgloss.Color("#1f2125")
	}
	return lipgloss.Color("#ebe4d0")
}

type styleSet struct {
	Title           lipgloss.Style
	TitleBar        lipgloss.Style
	Subtle          lipgloss.Style
	Help            lipgloss.Style
	SessionNormal   lipgloss.Style
	SessionSelected lipgloss.Style
	SessionMeta     lipgloss.Style
	UserLabel       lipgloss.Style
	UserBubble      lipgloss.Style
	AssistantLabel  lipgloss.Style
	AssistantBubble lipgloss.Style
	AssistantBar    lipgloss.Style
	InputBox        lipgloss.Style
	HeaderChip      lipgloss.Style
	ModelChip       lipgloss.Style
	Error           lipgloss.Style
	Cursor          lipgloss.Style
	CodeBlock       lipgloss.Style
	CodeLang        lipgloss.Style
	Inline          lipgloss.Style
}

// s is rebuilt every time the theme changes. All renderers read from it.
var s styleSet

func init() {
	rebuildStyles()
}

func rebuildStyles() {
	t := CurrentTheme()
	s = styleSet{
		Title:    lipgloss.NewStyle().Bold(true).Foreground(t.Accent()),
		TitleBar: lipgloss.NewStyle().Bold(true).Foreground(t.Accent()).PaddingLeft(2).PaddingRight(2),
		Subtle:   lipgloss.NewStyle().Foreground(t.Dim()),
		Help:     lipgloss.NewStyle().Foreground(t.Dim()).PaddingLeft(2),

		SessionNormal:   lipgloss.NewStyle().PaddingLeft(4),
		SessionSelected: lipgloss.NewStyle().PaddingLeft(2).Foreground(t.Accent()).Bold(true),
		SessionMeta:     lipgloss.NewStyle().Foreground(t.Dim()),

		UserLabel: lipgloss.NewStyle().Bold(true).Foreground(t.User()),
		UserBubble: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(t.User()).
			Background(subtleBg()).
			Padding(0, 2).
			MarginLeft(2),
		AssistantLabel: lipgloss.NewStyle().Bold(true).Foreground(t.Assistant()),
		AssistantBubble: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(t.Assistant()).
			Background(subtleBg()).
			Padding(0, 2).
			MarginLeft(2),
		AssistantBar: lipgloss.NewStyle().Foreground(t.Assistant()),

		InputBox: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(t.Accent()).
			Background(subtleBg()).
			Padding(0, 2),

		HeaderChip: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.CodeFg()).
			Background(t.Accent()).
			Padding(0, 1),

		ModelChip: lipgloss.NewStyle().
			Foreground(t.Assistant()).
			Bold(true),

		Error:  lipgloss.NewStyle().Foreground(t.Error()).PaddingLeft(2),
		Cursor: lipgloss.NewStyle().Foreground(t.Accent()).Bold(true),

		CodeBlock: lipgloss.NewStyle().Background(t.CodeBg()).Foreground(t.CodeFg()),
		CodeLang:  lipgloss.NewStyle().Foreground(t.Accent()).Italic(true),
		Inline:    lipgloss.NewStyle().Foreground(t.Accent()),
	}
}

// applyTheme switches the active theme and rebuilds style cache.
func applyTheme(name string) bool {
	if !SetThemeByName(name) {
		return false
	}
	rebuildStyles()
	return true
}

func dividerColor(width int, c color.Color) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("─", width))
}

func divider(width int) string {
	return dividerColor(width, CurrentTheme().Border())
}
