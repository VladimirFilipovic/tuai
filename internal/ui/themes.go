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
			// Soft, washed-out warmth so the cream page bg never fights the
			// brand. Terracotta accent + dusty blue + sage green keep the
			// opencode identity but drop the saturation so chips read pastel.
			accent: "#c97a4a", user: "#6b95a8", assistant: "#9aa66e",
			dim: "#a89684", border: "#e6d9b8", errFg: "#c25656",
			codeBg: "#fbf1c7", codeFg: "#3c3836",
			chroma: "gruvbox-light",
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
			accent: "#8aa6e0", user: "#b59ce0", assistant: "#9bb37a",
			dim: "#a5acc4", border: "#d0d2d8", errFg: "#e58497",
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
			accent: "#7287fd", user: "#c6a0f6", assistant: "#8ec07c",
			dim: "#9ca0b0", border: "#dce0e8", errFg: "#e88a9c",
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
			accent: "#a08fdc", user: "#d693b8", assistant: "#86b884",
			dim: "#a09aa0", border: "#e0dde8", errFg: "#dc8c84",
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
			accent: "#8ab09a", user: "#c098a8", assistant: "#b8b46a",
			dim: "#a89684", border: "#e6d9b8", errFg: "#c25656",
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
			accent: "#7cb6d4", user: "#a89bcf", assistant: "#9cbf72",
			dim: "#a5abb3", border: "#e2e2e2", errFg: "#e88aa8",
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
			accent: "#8ab0e8", user: "#c89bc6", assistant: "#9cc09a",
			dim: "#b0b1b6", border: "#dedede", errFg: "#dc9088",
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
			accent: "#7b9cc8", user: "#a092c8", assistant: "#b0b478",
			dim: "#9c9a94", border: "#e0dcc8", errFg: "#cd8a7e",
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
			accent: "#7cc0d0", user: "#90b0d8", assistant: "#86c4a8",
			dim: "#9caab5", border: "#d8e4ec", errFg: "#d88a90",
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
