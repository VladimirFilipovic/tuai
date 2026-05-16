package ui

import (
	"image/color"

	lipgloss "charm.land/lipgloss/v2"
)

// Theme bundles palette + chroma style for syntax highlighting. UI colors
// roughly track the chroma style's vibe so things don't clash.
//
// Palettes mirror the opencode-ai/opencode TUI theme files
// (internal/tui/theme/*.go) where one exists, mapped to our slots:
//   accent    <- opencode Primary
//   user      <- opencode Secondary
//   assistant <- opencode Success
//   dim       <- opencode TextMuted
//   border    <- opencode BorderNormal
//   errFg     <- opencode Error
//   codeBg    <- opencode Background
//   codeFg    <- opencode Text
type Theme struct {
	Name        string
	Description string

	accent    string
	user      string
	assistant string
	dim       string
	border    string
	errFg     string
	codeBg    string
	codeFg    string
	chroma    string
}

func (t Theme) Accent() color.Color    { return lipgloss.Color(t.accent) }
func (t Theme) User() color.Color      { return lipgloss.Color(t.user) }
func (t Theme) Assistant() color.Color { return lipgloss.Color(t.assistant) }
func (t Theme) Dim() color.Color       { return lipgloss.Color(t.dim) }
func (t Theme) Border() color.Color    { return lipgloss.Color(t.border) }
func (t Theme) Error() color.Color     { return lipgloss.Color(t.errFg) }
func (t Theme) CodeBg() color.Color    { return lipgloss.Color(t.codeBg) }
func (t Theme) CodeFg() color.Color    { return lipgloss.Color(t.codeFg) }
func (t Theme) Chroma() string         { return t.chroma }

var Themes = []Theme{
	{
		Name:        "opencode",
		Description: "OpenCode brand. Orange primary, blue secondary.",
		accent:      "#fab283", user: "#5c9cf5", assistant: "#7fd88f",
		dim: "#6a6a6a", border: "#4b4c5c", errFg: "#e06c75",
		codeBg: "#212121", codeFg: "#e0e0e0",
		chroma: "monokai",
	},
	{
		Name:        "tokyonight",
		Description: "Tokyo Night Moon. Quiet blues and purples.",
		accent:      "#82aaff", user: "#c099ff", assistant: "#c3e88d",
		dim: "#636da6", border: "#3b4261", errFg: "#ff757f",
		codeBg: "#222436", codeFg: "#c8d3f5",
		chroma: "tokyonight-moon",
	},
	{
		Name:        "catppuccin",
		Description: "Catppuccin Mocha. Soothing pastel dark.",
		accent:      "#89b4fa", user: "#cba6f7", assistant: "#a6e3a1",
		dim: "#a6adc8", border: "#45475a", errFg: "#f38ba8",
		codeBg: "#1e1e2e", codeFg: "#cdd6f4",
		chroma: "catppuccin-mocha",
	},
	{
		Name:        "dracula",
		Description: "Iconic dark violet.",
		accent:      "#bd93f9", user: "#ff79c6", assistant: "#50fa7b",
		dim: "#6272a4", border: "#44475a", errFg: "#ff5555",
		codeBg: "#282a36", codeFg: "#f8f8f2",
		chroma: "dracula",
	},
	{
		Name:        "gruvbox",
		Description: "Retro warm earth tones.",
		accent:      "#83a598", user: "#d3869b", assistant: "#b8bb26",
		dim: "#928374", border: "#504945", errFg: "#fb4934",
		codeBg: "#282828", codeFg: "#ebdbb2",
		chroma: "gruvbox",
	},
	{
		Name:        "monokai",
		Description: "Monokai Pro.",
		accent:      "#78dce8", user: "#ab9df2", assistant: "#a9dc76",
		dim: "#727072", border: "#403e41", errFg: "#ff6188",
		codeBg: "#2d2a2e", codeFg: "#fcfcfa",
		chroma: "monokai",
	},
	{
		Name:        "onedark",
		Description: "Atom One Dark.",
		accent:      "#61afef", user: "#c678dd", assistant: "#98c379",
		dim: "#5c6370", border: "#3b4048", errFg: "#e06c75",
		codeBg: "#282c34", codeFg: "#abb2bf",
		chroma: "onedark",
	},
	{
		Name:        "flexoki",
		Description: "Inky, paper-like. Inspired by Steph Ango's palette.",
		accent:      "#4385be", user: "#8b7ec8", assistant: "#879a39",
		dim: "#878580", border: "#403e3c", errFg: "#d14d41",
		codeBg: "#100f0f", codeFg: "#cecdc3",
		chroma: "doom-one2",
	},
	{
		Name:        "tron",
		Description: "Neon grid. Cyan and blue.",
		accent:      "#00d9ff", user: "#007fff", assistant: "#00ff8f",
		dim: "#4d6b87", border: "#1a2633", errFg: "#ff3333",
		codeBg: "#0c141f", codeFg: "#caf0ff",
		chroma: "nord",
	},
}

var current Theme

func init() {
	current = Themes[0]
}

func CurrentTheme() Theme { return current }

func SetThemeByName(name string) bool {
	for _, t := range Themes {
		if t.Name == name {
			current = t
			return true
		}
	}
	return false
}
