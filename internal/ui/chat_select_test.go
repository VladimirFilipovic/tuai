package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func TestStripBubbleFrame(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"margin bar padding", "  ┃  Good catch — let me check.", "Good catch — let me check."},
		{"trailing pad trimmed", "  ┃  short reply           ", "short reply"},
		{"code indent kept beyond padding", "  ┃    func main() {", "  func main() {"},
		{"blank bubble line", "  ┃  ", ""},
		{"no bar left untouched but rtrimmed", "  You  15:04   ", "  You  15:04"},
		{"bar with single padding space", "┃ x", "x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripBubbleFrame(c.in); got != c.want {
				t.Errorf("stripBubbleFrame(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestIsRuleLine(t *testing.T) {
	if !isRuleLine("──────────") {
		t.Error("a run of box dashes should be a rule line")
	}
	if !isRuleLine("   ────  ") {
		t.Error("surrounding space should not stop rule detection")
	}
	if isRuleLine("") {
		t.Error("empty line is not a rule line")
	}
	if isRuleLine("real text") {
		t.Error("prose is not a rule line")
	}
	if isRuleLine("-- not box dashes --") {
		t.Error("ascii hyphens are not box-drawing rules")
	}
}

func TestCleanSelection(t *testing.T) {
	// A selection spanning two wrapped bubble lines, a fenced rule, and a
	// trailing blank — the messy reality the user pasted.
	raw := strings.Join([]string{
		"  ┃  You're right — get.wasm has no body. It reads PARAMS",
		"  ┃  JSON as a url string and a headers map.",
		"  ┃  ────────────────────────",
		"  ┃    retry_options optional",
		"  ┃  ",
	}, "\n")

	want := strings.Join([]string{
		"You're right — get.wasm has no body. It reads PARAMS",
		"JSON as a url string and a headers map.",
		"  retry_options optional",
	}, "\n")

	if got := cleanSelection(raw); got != want {
		t.Errorf("cleanSelection mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestCleanSelectionTrimsBlankEnds(t *testing.T) {
	raw := "  ┃  \n  ┃  middle\n  ┃  "
	if got := cleanSelection(raw); got != "middle" {
		t.Errorf("expected blank ends trimmed, got %q", got)
	}
}

func TestByteOffsetOf(t *testing.T) {
	lines := []string{"abc", "héllo", "xyz"}
	// content = "abc\nhéllo\nxyz"; 'é' is 2 bytes.
	cases := []struct {
		line, col, want int
	}{
		{0, 0, 0},     // start
		{0, 3, 3},     // end of "abc"
		{1, 0, 4},     // start of line 1 (after "abc\n")
		{1, 1, 5},     // after 'h'
		{1, 2, 7},     // after 'é' (2 bytes) → 4+1+2 = 7
		{1, 99, 10},   // clamp col to rune length (5 runes, 6 bytes) → 4+6
		{2, 0, 11},    // start of line 2: 4 + 6 + 1
		{99, 0, 0xff}, // line past end returns end-of-content offset
	}
	full := strings.Join(lines, "\n")
	for _, c := range cases {
		got := byteOffsetOf(lines, c.line, c.col)
		want := c.want
		if c.want == 0xff {
			want = len(full) + 1 // loop adds a trailing +1 for the final \n it never subtracts
		}
		if got != want {
			t.Errorf("byteOffsetOf(line=%d,col=%d) = %d, want %d", c.line, c.col, got, want)
		}
	}
}

// buildSelectableChat makes a minimal chat whose viewport holds crafted bubble
// lines at scroll offset 0, so screen rows map straight onto content lines.
func buildSelectableChat(content string) chatModel {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	vp.SetContent(content)
	vp.SetYOffset(0)
	return chatModel{viewport: vp, width: 80, height: 20, historyIdx: -1}
}

func TestSelectionRangeExtractsCleanText(t *testing.T) {
	content := "  ┃  hello world\n  ┃  second line"
	m := buildSelectableChat(content)
	// Anchor on the 'h' of "hello" (col 5 = 2 margin + 1 bar + 2 padding),
	// drag past the end of the second line.
	m.selAnchor = selPoint{line: 0, col: 5}
	m.selCursor = selPoint{line: 1, col: 99}

	lo, hi := m.selectionRange()
	got := cleanSelection(content[lo:hi])
	want := "hello world\nsecond line"
	if got != want {
		t.Errorf("selection extract = %q, want %q (lo=%d hi=%d)", got, want, lo, hi)
	}
}

func TestMousePressDragSetsSelection(t *testing.T) {
	m := buildSelectableChat("  ┃  alpha\n  ┃  beta")

	// Press inside the viewport (y=2 is the first content row).
	down := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 2}
	m, _ = m.Update(down)
	if !m.selActive {
		t.Fatal("left press in viewport should start a selection")
	}
	if m.selAnchor != (selPoint{line: 0, col: 5}) {
		t.Fatalf("anchor = %+v, want {0,5}", m.selAnchor)
	}

	// Drag down to the second line.
	drag := tea.MouseMotionMsg{Button: tea.MouseLeft, X: 9, Y: 3}
	m, _ = m.Update(drag)
	if m.selCursor != (selPoint{line: 1, col: 9}) {
		t.Fatalf("cursor = %+v, want {1,9}", m.selCursor)
	}
}

func TestMousePressOutsideViewportIgnored(t *testing.T) {
	m := buildSelectableChat("  ┃  alpha")
	// y=0 is the header row, above the viewport.
	down := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 0}
	m, _ = m.Update(down)
	if m.selActive {
		t.Fatal("press above the viewport should not start a selection")
	}
}
