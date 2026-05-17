package ui

import (
	"image/color"

	lipgloss "charm.land/lipgloss/v2"
)

// Palette is one half of a theme — the set of color slots used for a given
// terminal brightness. Dark palettes assume a dark terminal background, light
// palettes assume a light one.
//
// Slots mirror opencode-ai/opencode's theme slots:
//
//	accent    <- Primary
//	user      <- Secondary
//	assistant <- Success
//	dim       <- TextMuted
//	border    <- BorderNormal
//	errFg     <- Error
//	codeBg    <- Background
//	codeFg    <- Text
//
// `chroma` names the chroma syntax-highlight style used for fenced code in
// this variant. A nonexistent name falls back to chroma's default.
type Palette struct {
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

// Theme bundles a name and both light/dark palettes. The active palette is
// chosen at access time based on the detected terminal brightness, so a single
// named theme stays usable whether the user runs in a light or dark terminal.
type Theme struct {
	Name        string
	Description string
	Dark        Palette
	Light       Palette
}

func (t Theme) active() Palette {
	if isDark() {
		return t.Dark
	}
	return t.Light
}

func (t Theme) Accent() color.Color    { return lipgloss.Color(t.active().accent) }
func (t Theme) User() color.Color      { return lipgloss.Color(t.active().user) }
func (t Theme) Assistant() color.Color { return lipgloss.Color(t.active().assistant) }
func (t Theme) Dim() color.Color       { return lipgloss.Color(t.active().dim) }
func (t Theme) Border() color.Color    { return lipgloss.Color(t.active().border) }
func (t Theme) Error() color.Color     { return lipgloss.Color(t.active().errFg) }
func (t Theme) CodeBg() color.Color    { return lipgloss.Color(t.active().codeBg) }
func (t Theme) CodeFg() color.Color    { return lipgloss.Color(t.active().codeFg) }
func (t Theme) Chroma() string         { return t.active().chroma }

var Themes = []Theme{
	{
		Name:        "opencode",
		Description: "OpenCode brand. Orange primary, blue secondary.",
		Dark: Palette{
			accent: "#fab283", user: "#5c9cf5", assistant: "#7fd88f",
			dim: "#6a6a6a", border: "#4b4c5c", errFg: "#e06c75",
			codeBg: "#212121", codeFg: "#e0e0e0",
			chroma: "monokai",
		},
		Light: Palette{
			accent: "#d97706", user: "#2563eb", assistant: "#16a34a",
			dim: "#71717a", border: "#d4d4d8", errFg: "#dc2626",
			codeBg: "#f8f8f8", codeFg: "#1f2937",
			chroma: "github",
		},
	},
	{
		Name:        "tokyonight",
		Description: "Tokyo Night. Quiet blues and purples.",
		Dark: Palette{
			accent: "#82aaff", user: "#c099ff", assistant: "#c3e88d",
			dim: "#636da6", border: "#3b4261", errFg: "#ff757f",
			codeBg: "#222436", codeFg: "#c8d3f5",
			chroma: "tokyonight-moon",
		},
		Light: Palette{
			accent: "#2e7de9", user: "#9854f1", assistant: "#587539",
			dim: "#848cb5", border: "#b4b5b9", errFg: "#f52a65",
			codeBg: "#e1e2e7", codeFg: "#3760bf",
			chroma: "tokyonight-day",
		},
	},
	{
		Name:        "catppuccin",
		Description: "Catppuccin. Soothing pastels.",
		Dark: Palette{
			accent: "#89b4fa", user: "#cba6f7", assistant: "#a6e3a1",
			dim: "#a6adc8", border: "#45475a", errFg: "#f38ba8",
			codeBg: "#1e1e2e", codeFg: "#cdd6f4",
			chroma: "catppuccin-mocha",
		},
		Light: Palette{
			accent: "#1e66f5", user: "#8839ef", assistant: "#40a02b",
			dim: "#6c6f85", border: "#ccd0da", errFg: "#d20f39",
			codeBg: "#eff1f5", codeFg: "#4c4f69",
			chroma: "catppuccin-latte",
		},
	},
	{
		Name:        "dracula",
		Description: "Iconic violet. Dracula / Alucard.",
		Dark: Palette{
			accent: "#bd93f9", user: "#ff79c6", assistant: "#50fa7b",
			dim: "#6272a4", border: "#44475a", errFg: "#ff5555",
			codeBg: "#282a36", codeFg: "#f8f8f2",
			chroma: "dracula",
		},
		Light: Palette{
			accent: "#644ac9", user: "#a4205f", assistant: "#14710a",
			dim: "#635d62", border: "#cfcfde", errFg: "#cb3a2a",
			codeBg: "#f1f1f0", codeFg: "#1f1f1f",
			chroma: "github",
		},
	},
	{
		Name:        "gruvbox",
		Description: "Retro warm earth tones.",
		Dark: Palette{
			accent: "#83a598", user: "#d3869b", assistant: "#b8bb26",
			dim: "#928374", border: "#504945", errFg: "#fb4934",
			codeBg: "#282828", codeFg: "#ebdbb2",
			chroma: "gruvbox",
		},
		Light: Palette{
			accent: "#427b58", user: "#8f3f71", assistant: "#79740e",
			dim: "#7c6f64", border: "#d5c4a1", errFg: "#9d0006",
			codeBg: "#fbf1c7", codeFg: "#3c3836",
			chroma: "gruvbox-light",
		},
	},
	{
		Name:        "monokai",
		Description: "Monokai. Bold pastel highlights.",
		Dark: Palette{
			accent: "#78dce8", user: "#ab9df2", assistant: "#a9dc76",
			dim: "#727072", border: "#403e41", errFg: "#ff6188",
			codeBg: "#2d2a2e", codeFg: "#fcfcfa",
			chroma: "monokai",
		},
		Light: Palette{
			accent: "#029cdd", user: "#7058be", assistant: "#519c00",
			dim: "#797f88", border: "#dddddd", errFg: "#f9005a",
			codeBg: "#fafafa", codeFg: "#2c292d",
			chroma: "monokailight",
		},
	},
	{
		Name:        "onedark",
		Description: "Atom One. Dark and Light.",
		Dark: Palette{
			accent: "#61afef", user: "#c678dd", assistant: "#98c379",
			dim: "#5c6370", border: "#3b4048", errFg: "#e06c75",
			codeBg: "#282c34", codeFg: "#abb2bf",
			chroma: "onedark",
		},
		Light: Palette{
			accent: "#4078f2", user: "#a626a4", assistant: "#50a14f",
			dim: "#a0a1a7", border: "#d4d4d4", errFg: "#e45649",
			codeBg: "#fafafa", codeFg: "#383a42",
			chroma: "github",
		},
	},
	{
		Name:        "flexoki",
		Description: "Inky, paper-like. Steph Ango's palette.",
		Dark: Palette{
			accent: "#4385be", user: "#8b7ec8", assistant: "#879a39",
			dim: "#878580", border: "#403e3c", errFg: "#d14d41",
			codeBg: "#100f0f", codeFg: "#cecdc3",
			chroma: "doom-one2",
		},
		Light: Palette{
			accent: "#205ea6", user: "#5e409d", assistant: "#66800b",
			dim: "#6f6e69", border: "#cecdc3", errFg: "#af3029",
			codeBg: "#fffcf0", codeFg: "#100f0f",
			chroma: "github",
		},
	},
	{
		Name:        "tron",
		Description: "Neon grid. Cyan and blue.",
		Dark: Palette{
			accent: "#00d9ff", user: "#007fff", assistant: "#00ff8f",
			dim: "#4d6b87", border: "#1a2633", errFg: "#ff3333",
			codeBg: "#0c141f", codeFg: "#caf0ff",
			chroma: "nord",
		},
		Light: Palette{
			accent: "#0099b8", user: "#1f5fbf", assistant: "#0a8554",
			dim: "#6b7c8a", border: "#c0d4e3", errFg: "#b3000d",
			codeBg: "#f0f8ff", codeFg: "#0c141f",
			chroma: "github",
		},
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
