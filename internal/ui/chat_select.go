package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/VladimirFilipovic/tuai/internal/clipboard"
	"github.com/charmbracelet/x/ansi"
)

// copyToastDuration is how long the "copied N chars" popup lingers in the
// top-right corner after a drag-selection is released.
const copyToastDuration = 2200 * time.Millisecond

// clearCopyToastMsg is fired by a tea.Tick after a copy toast is shown. The
// id guards against a newer copy clearing an older toast: each finishSelection
// bumps copyToastID, and a stale tick is dropped when the ids don't match.
type clearCopyToastMsg struct{ id int }

// chatHeaderRows is how many screen rows sit above the chat viewport: the
// header line and the divider beneath it (see chatModel.View). Mouse Y
// coordinates are absolute, so we subtract this to land in viewport space.
const chatHeaderRows = 2

// selPoint is a position in the viewport's *content* — a line index into the
// rendered scrollback (not a screen row) and a column in cells. Storing
// content coordinates means a selection stays anchored to the same text even
// as the viewport scrolls under the mouse.
type selPoint struct {
	line int
	col  int
}

// pointAt maps an absolute screen coordinate to a content position, accounting
// for the header rows above the viewport and its current scroll offset.
func (m *chatModel) pointAt(x, y int) selPoint {
	line := max(m.viewport.YOffset()+(y-chatHeaderRows), 0)
	return selPoint{line: line, col: max(x, 0)}
}

// inViewport reports whether a screen row falls inside the chat viewport's
// drawn area — used to ignore presses on the header, input bar, or help line.
func (m *chatModel) inViewport(y int) bool {
	return y >= chatHeaderRows && y < chatHeaderRows+m.viewport.Height()
}

// selStyle is the highlight applied to the live selection. Black text on the
// theme accent, bolded — that combo punches through colorized spans (code
// blocks, links) where SetHighlights overlays don't fully replace inner
// styling, which had left the highlight looking washed-out in dark themes.
func selStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0a0a0c")).
		Background(CurrentTheme().Accent()).
		Bold(true)
}

func (m *chatModel) beginSelection(x, y int) {
	m.viewport.ClearHighlights()
	m.viewport.HighlightStyle = selStyle()
	m.selActive = true
	p := m.pointAt(x, y)
	m.selAnchor = p
	m.selCursor = p
}

func (m *chatModel) updateSelection(x, y int) {
	m.selCursor = m.pointAt(x, y)
	lo, hi := m.selectionRange()
	if hi <= lo {
		m.viewport.ClearHighlights()
		return
	}
	m.viewport.SetHighlights([][]int{{lo, hi}})
}

// finishSelection copies the highlighted text (with bubble frames stripped) to
// the clipboard and returns a tea.Cmd. The highlight is re-applied after the
// notice repaint (SetContent clears it) so it lingers as confirmation until
// the next key press or new selection.
func (m *chatModel) finishSelection() tea.Cmd {
	m.selActive = false
	lo, hi := m.selectionRange()
	if hi <= lo {
		m.viewport.ClearHighlights()
		return nil
	}
	stripped := ansi.Strip(m.viewport.GetContent())
	if hi > len(stripped) {
		hi = len(stripped)
	}
	text := cleanSelection(stripped[lo:hi])
	if text == "" {
		m.viewport.ClearHighlights()
		return nil
	}
	n := len([]rune(text))
	m.copyToast = "copied " + plural(n, "char")
	m.copyToastID++
	toastID := m.copyToastID
	m.refreshViewport()
	// refreshViewport's SetContent wiped the highlight; restore it so the user
	// sees exactly what was copied. The toast lives in the header, so the
	// message-region offsets we just computed are still valid.
	m.viewport.SetHighlights([][]int{{lo, hi}})
	clearCmd := tea.Tick(copyToastDuration, func(time.Time) tea.Msg {
		return clearCopyToastMsg{id: toastID}
	})
	if err := clipboard.WriteText(text); err != nil {
		// OSC52 fallback for terminals where no native clipboard tool is
		// reachable (e.g. over SSH). Best-effort; the toast already reported.
		return tea.Batch(clearCmd, tea.SetClipboard(text))
	}
	return clearCmd
}

// clearSelection drops any visible highlight and resets in-progress state.
func (m *chatModel) clearSelection() {
	m.selActive = false
	m.viewport.ClearHighlights()
}

// selectionRange returns the [lo, hi) byte offsets of the current selection in
// the ANSI-stripped viewport content — the form SetHighlights and slicing both
// expect.
func (m *chatModel) selectionRange() (int, int) {
	lines := strings.Split(ansi.Strip(m.viewport.GetContent()), "\n")
	a := byteOffsetOf(lines, m.selAnchor.line, m.selAnchor.col)
	b := byteOffsetOf(lines, m.selCursor.line, m.selCursor.col)
	if a > b {
		a, b = b, a
	}
	return a, b
}

// byteOffsetOf converts a (line, col) content position into a byte offset into
// the lines joined by "\n". col is a cell index; it's clamped to the line's
// rune length and mapped to the matching byte boundary.
func byteOffsetOf(lines []string, line, col int) int {
	if line < 0 {
		line = 0
	}
	off := 0
	for i := 0; i < line; i++ {
		if i >= len(lines) {
			return off
		}
		off += len(lines[i]) + 1 // +1 for the '\n'
	}
	if line >= len(lines) {
		return off
	}
	runes := []rune(lines[line])
	if col > len(runes) {
		col = len(runes)
	}
	off += len(string(runes[:col]))
	return off
}

// cleanSelection turns a raw slice of the rendered scrollback into plain text:
// it strips each bubble's left frame (margin + "┃" bar + padding), drops the
// horizontal rule lines that fence code blocks, and trims the right-pad spaces
// every bubble line carries. Leading/trailing blank lines are trimmed too.
func cleanSelection(text string) string {
	in := strings.Split(text, "\n")
	out := make([]string, 0, len(in))
	for _, ln := range in {
		c := stripBubbleFrame(ln)
		if isRuleLine(c) {
			continue
		}
		out = append(out, c)
	}
	// Trim leading/trailing blank lines so a selection that grazes a gap row
	// doesn't come back wrapped in empty lines.
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// stripBubbleFrame removes the bubble's left decoration from one line: the
// "┃" bar (with its leading margin) and up to two of the padding spaces that
// follow it. Lines with no bar are returned with only their trailing pad
// trimmed. The bubble's right pad (trailing spaces) is always trimmed.
func stripBubbleFrame(ln string) string {
	if i := strings.IndexRune(ln, '┃'); i >= 0 {
		rest := ln[i+len("┃"):]
		// Drop up to the two padding spaces lipgloss inserts after the bar.
		for n := 0; n < 2 && len(rest) > 0 && rest[0] == ' '; n++ {
			rest = rest[1:]
		}
		ln = rest
	}
	return strings.TrimRight(ln, " ")
}

// isRuleLine reports whether a (frame-stripped) line is one of the horizontal
// rules used to fence code/diff blocks — a run of box-drawing dashes only.
func isRuleLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '─' {
			return false
		}
	}
	return true
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
