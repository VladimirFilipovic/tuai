package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// cellIndexOf returns the cell-column of `text` within `line` (-1 if absent).
// applySelectionStyle works in lipgloss cell coordinates, which differ from
// byte offsets whenever the line contains multi-byte runes like '┃'.
func cellIndexOf(line, text string) int {
	at := strings.Index(line, text)
	if at < 0 {
		return -1
	}
	return lipgloss.Width(line[:at])
}

func TestByteOffsetOf_WideRunes(t *testing.T) {
	// "你好 world" in cells:
	//   你=2, 好=2, space=1, w=1, o=1, r=1, l=1, d=1 → total 10
	// In bytes: each CJK rune is 3 bytes, space=1, ASCII=1 → "你好" = 6, " world" = 6.
	lines := []string{"你好 world"}

	cases := []struct {
		name        string
		col         int
		wantByteOff int // offset into "你好 world"
	}{
		{"col 0 → start", 0, 0},
		{"col 2 → after 你", 2, 3},
		{"col 4 → after 好", 4, 6},
		{"col 5 → after space", 5, 7},
		{"col 10 → end", 10, 12},
		{"col past end clamps", 99, 12},
		// Critical: col 1 lands inside the wide rune 你. Should land at the
		// rune boundary BEFORE it (offset 0), not split bytes.
		{"col 1 inside wide rune clamps before", 1, 0},
		{"col 3 inside 好 clamps before", 3, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := byteOffsetOf(lines, 0, c.col)
			if got != c.wantByteOff {
				t.Errorf("byteOffsetOf col=%d: got %d, want %d", c.col, got, c.wantByteOff)
			}
		})
	}
}

func TestByteOffsetOf_PlainASCIIUnchanged(t *testing.T) {
	lines := []string{"hello world", "second line"}
	if got := byteOffsetOf(lines, 0, 5); got != 5 {
		t.Errorf("col 5 on ASCII line: got %d, want 5", got)
	}
	if got := byteOffsetOf(lines, 1, 6); got != len("hello world")+1+6 {
		t.Errorf("col 6 on line 1: got %d, want %d", got, len("hello world")+1+6)
	}
}

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

// TestApplySelectionStyleSingleLine paints a yellow highlight onto a single
// styled bubble line and checks the SGR immediately preceding the target
// text is our yellow-marker style — not the bubble's grey bg.
func TestApplySelectionStyleSingleLine(t *testing.T) {
	rebuildStyles()
	bubble := s.UserBubble.Render("fdfdfdfd")
	stripped := ansi.Strip(bubble)
	col := cellIndexOf(stripped, "fdfdfdfd")
	if col < 0 {
		t.Fatalf("test setup: 'fdfdfdfd' not in stripped %q", stripped)
	}

	out := applySelectionStyle(bubble, selPoint{0, col}, selPoint{0, col + 8}, selStyle())
	assertWrappedByYellow(t, out, "fdfdfdfd")
}

// TestApplySelectionStyleAcrossMessages mirrors the screenshot bug: a user
// bubble ("fdfdfdfd") followed by an assistant bubble, each line wrapped in
// its own lipgloss SGR pair. The selection must land on the user text on
// line 1, not be misrouted to the chip row on line 0 by bubbles' broken
// parseMatches.
func TestApplySelectionStyleAcrossMessages(t *testing.T) {
	rebuildStyles()
	userChip := "  " + s.UserLabel.Render("You") + s.SessionMeta.Render("  12:50")
	userBubble := s.UserBubble.Render("fdfdfdfd")
	asstChip := "  " + s.AssistantLabel.Render("Claude") + s.SessionMeta.Render("  12:50")
	asstBubble := s.AssistantBubble.Render("Unclear input — did you mean to send something specific?")

	content := strings.Join([]string{userChip, userBubble, "", asstChip, asstBubble}, "\n")

	stripLines := strings.Split(ansi.Strip(content), "\n")
	var fdLine, fdCol int
	for i, ln := range stripLines {
		if c := cellIndexOf(ln, "fdfdfdfd"); c >= 0 {
			fdLine, fdCol = i, c
			break
		}
	}
	out := applySelectionStyle(content,
		selPoint{fdLine, fdCol}, selPoint{fdLine, fdCol + 8}, selStyle())

	assertWrappedByYellow(t, out, "fdfdfdfd")
}

// TestApplySelectionStyleMultiLine: drag spans two lines — should paint
// lo.col → EOL on line 0, BOL → hi.col on line 1.
func TestApplySelectionStyleMultiLine(t *testing.T) {
	rebuildStyles()
	l0 := s.UserBubble.Render("alpha")
	l1 := s.UserBubble.Render("bravo")
	content := l0 + "\n" + l1

	// Locate "lpha" and "br" in the stripped lines (cell coords).
	strip := strings.Split(ansi.Strip(content), "\n")
	startCol := cellIndexOf(strip[0], "lpha")
	endCol := cellIndexOf(strip[1], "br") + 2
	out := applySelectionStyle(content,
		selPoint{0, startCol}, selPoint{1, endCol}, selStyle())

	assertWrappedByYellow(t, out, "lpha")
	assertWrappedByYellow(t, out, "br")
}

// assertWrappedByYellow checks that `text` appears in `out` immediately
// preceded by an SGR sequence containing our yellow-marker bg (48;2;255;214;10).
func assertWrappedByYellow(t *testing.T, out, text string) {
	t.Helper()
	at := strings.Index(out, text)
	if at < 0 {
		t.Fatalf("%q not present in rendered output:\n%q", text, out)
	}
	prefix := out[:at]
	lastEsc := strings.LastIndex(prefix, "\x1b[")
	if lastEsc < 0 {
		t.Fatalf("no SGR precedes %q in %q", text, out)
	}
	openSGR := prefix[lastEsc:]
	if !strings.Contains(openSGR, "48;2;255;214;10") {
		t.Errorf("expected %q wrapped by yellow-marker SGR; preceding SGR was %q",
			text, openSGR)
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
