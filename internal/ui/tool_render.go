package ui

import (
	lipgloss "charm.land/lipgloss/v2"

	"github.com/VladimirFilipovic/tuai/internal/toolview"
)

// renderToolBlock renders a tool event through the toolview package, wiring in
// the active theme colors and the ui-side helpers (icon lookup, syntax
// highlighting, diff painting) that toolview needs but doesn't own.
func renderToolBlock(t toolEvent, wrap int) string {
	theme := CurrentTheme()
	return toolview.Render(t.name, t.input, wrap, toolview.Deps{
		Accent:    theme.Accent(),
		Dim:       theme.Dim(),
		Icon:      toolIcon,
		Highlight: highlightForFile,
		Diff:      renderDiff,
	})
}

// truncateLine cell-truncates s to width, appending "…" when it overflows.
func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width-1 {
		return s
	}
	return string(runes[:width-1]) + "…"
}
