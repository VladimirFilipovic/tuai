package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
)

// makeInput returns a textarea styled the same way chatModel does,
// plus the outer width passed to renderInputArea.
func makeInput(outerWidth int) (textarea.Model, int) {
	ta := textarea.New()
	ta.Placeholder = "Message Claude…  (enter send · shift+enter newline · esc cancel/back)"
	ta.Focus()
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetWidth(outerWidth - 4) // mirrors chatModel.setSize
	ta.SetHeight(inputLines - 1)
	styleTextarea(&ta)
	return ta, outerWidth
}

// resetThenPlainGap matches a SGR reset followed by 5+ unstyled spaces before
// the next escape. bubbles' textarea pads empty rows with `EndOfBufferCharacter`
// (one space) and then lets viewport.Width fill the rest with bare spaces —
// those bare spaces show up as terminal-background-coloured gaps inside the
// supposedly shaded input bar.
var resetThenPlainGap = regexp.MustCompile(`\x1b\[(?:0?)m {5,}`)

// TestInputBoxRowsAreFullyShaded verifies every row of the rendered input
// has the subtle background covering its full width — i.e. there's no run
// of plain spaces between a reset and the next escape that would let the
// terminal background bleed through.
func TestInputBoxRowsAreFullyShaded(t *testing.T) {
	ta, outer := makeInput(120)

	out := renderInputArea(ta, outer)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < inputLines-1 {
		t.Fatalf("expected at least %d rows, got %d", inputLines-1, len(lines))
	}

	for i, ln := range lines {
		if resetThenPlainGap.MatchString(ln) {
			t.Errorf("line %d has an unshaded gap (reset followed by plain spaces)\nrendered: %q", i, ln)
		}
	}
}

// TestInputBoxRowsAreEqualWidth keeps every row of the input area at the
// same visible width so the shaded bar reads as one continuous block.
func TestInputBoxRowsAreEqualWidth(t *testing.T) {
	ta, outer := makeInput(120)

	out := renderInputArea(ta, outer)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	widths := make([]int, len(lines))
	for i, ln := range lines {
		widths[i] = lipgloss.Width(ln)
	}
	want := widths[0]
	for i, w := range widths {
		if w != want {
			t.Errorf("line %d width = %d, want %d (all input rows must share the shaded width)\nrendered:\n%s",
				i, w, want, debugDump(lines))
		}
	}
}

// TestInputBoxTypedContentFullyShaded confirms the fix also covers the
// path where the user has typed something — in that case the placeholder
// path doesn't run; the regular line renderer pads the empty rows below
// the caret line with bare spaces and the shading would break the same way.
func TestInputBoxTypedContentFullyShaded(t *testing.T) {
	ta, outer := makeInput(120)
	ta.SetValue("hello")

	out := renderInputArea(ta, outer)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, ln := range lines {
		if resetThenPlainGap.MatchString(ln) {
			t.Errorf("typed-content line %d has unshaded gap\nrendered: %q", i, ln)
		}
	}
}

func debugDump(lines []string) string {
	var b strings.Builder
	for i, ln := range lines {
		fmt.Fprintf(&b, "  [%d w=%d] %q\n", i, lipgloss.Width(ln), ln)
	}
	return b.String()
}
