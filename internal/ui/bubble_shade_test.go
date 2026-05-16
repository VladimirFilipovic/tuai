package ui

import (
	"regexp"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// resetThenContent matches an SGR reset followed by any non-escape content
// (text OR space) on the same line. If shadeBubble worked correctly, every
// line of a rendered bubble should *either* end after the reset *or* have
// the bg re-applied between the reset and the content.
//
// "Re-applied" means a 48;2;...m (true-color bg) or 49m (default bg → not
// what we want, but we treat anything that begins another SGR sequence as
// "another style is being applied"). We accept any \x1b[...m immediately
// following the reset as evidence that a new style took over.
var resetThenBareContent = regexp.MustCompile(`\x1b\[0?m[^\x1b\n][^\n]*`)

func TestBubble_NoBgGapAfterReset_PlainProse(t *testing.T) {
	body := padLinesToWidth(renderMarkdown("Plain prose with `inline code` and **bold** words.", 60), 60)
	out := shadeBubble(s.AssistantBubble.Render(body))
	assertNoBgGap(t, out)
}

func TestBubble_NoBgGapAfterReset_CodeBlock(t *testing.T) {
	src := "```go\n" +
		"func hello(name string) string {\n" +
		"    return \"hi \" + name\n" +
		"}\n" +
		"```"
	body := padLinesToWidth(renderMarkdown(src, 60), 60)
	out := shadeBubble(s.AssistantBubble.Render(body))
	assertNoBgGap(t, out)
}

// assertNoBgGap fails if any line of the rendered bubble has an SGR reset
// immediately followed by non-escape content with no bg re-applied. That
// pattern is exactly what produces a visual "unshaded gap" in the bubble.
func assertNoBgGap(t *testing.T, rendered string) {
	t.Helper()
	for i, ln := range strings.Split(rendered, "\n") {
		// Find each reset on the line and look at what comes immediately
		// after. If the next thing is a non-escape character (so no new SGR
		// style is starting), that character has lost the bg.
		idx := 0
		for {
			loc := resetThenBareContent.FindStringIndex(ln[idx:])
			if loc == nil {
				break
			}
			match := ln[idx+loc[0] : idx+loc[1]]
			t.Errorf("line %d: bg-gap after reset:\n  full: %q\n  match: %q\n  width: %d",
				i, ln, match, lipgloss.Width(ln))
			idx += loc[1]
		}
	}
}
