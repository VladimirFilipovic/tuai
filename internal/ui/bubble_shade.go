package ui

import (
	"regexp"
	"strings"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
)

// renderInputArea wraps the textarea so every row of the input bar
// shares the same shaded background that opencode uses, even when the
// rows below the caret are empty. bubbles' textarea only fills width
// on rows that hold content; the empty rows collapse to a single EOB
// character. We pad each line to outerWidth-2 (the 2-cell left margin
// chatModel.View prepends) using the same subtleBg() as the textarea
// so the shading reads as one continuous block.
//
// unshadedSpaceRun matches an SGR reset followed by two or more plain spaces.
// bubbles' textarea pads empty rows with its EndOfBufferCharacter (a single
// space) and lets the inner viewport's Width style fill the rest with bare
// spaces — none of which carry the bg colour. Those bare spaces fall just
// after a reset and let the terminal background bleed through the shaded
// input bar.
var unshadedSpaceRun = regexp.MustCompile(`(\x1b\[0?m)( {2,})`)

// padLinesToWidth right-pads every line of text with plain spaces so each
// visible row reaches `width` cells. Width is measured with lipgloss so
// embedded ANSI escapes don't count. The trailing spaces themselves carry no
// color — shadeBubble re-shades them after the bubble wraps the content.
func padLinesToWidth(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ln)
		w := lipgloss.Width(ln)
		if w < width {
			b.WriteString(strings.Repeat(" ", width-w))
		}
	}
	return b.String()
}

// resetSGR matches every SGR reset emitted inside the bubble's content —
// chroma drops one between every syntax token, our markdown helpers do too
// for bold/inline-code. Each reset strips the bubble's background; we need
// to re-apply it after each so the shaded surface reads as one continuous
// block instead of fragments behind each colored span.
var resetSGR = regexp.MustCompile(`\x1b\[0?m`)

// bubbleBgSGR returns the raw SGR sequence that paints the current bubble
// background. We hardcode it from subtleBg() so we can splice it directly
// into a rendered string without round-tripping through lipgloss (which would
// emit its own reset and re-introduce the same bleed).
func bubbleBgSGR() string {
	if isDark() {
		return "\x1b[48;2;28;30;34m" // #1c1e22
	}
	return "\x1b[48;2;241;235;217m" // #f1ebd9
}

// pageBgSGR is the SGR for the chat *page* background — what fills the area
// around bubbles, gap rows, label margins, and any spot the viewport doesn't
// otherwise paint. Picked one step darker than the bubble bg so bubbles still
// read as raised cards on the page in both light and dark modes.
func pageBgSGR() string {
	if isDark() {
		return "\x1b[48;2;22;24;28m" // #16181c
	}
	return "\x1b[48;2;236;228;205m" // #ece4cd
}

// shadePage paints the page background across every line of the rendered
// chat view. Each line gets right-padded to width with bg-coloured spaces,
// and every internal SGR reset re-applies the page bg so inner styled spans
// don't punch holes back to the terminal default. Bubble lines have already
// been through shadeBubble, so their internal bg is bubble-tone; that wins
// over the page tone we splice in here because shadeBubble's bg sequence
// follows ours in the byte stream.
func shadePage(rendered string, width int) string {
	bg := pageBgSGR()
	const reset = "\x1b[0m"
	withBg := func(m string) string { return m + bg }

	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		processed := resetSGR.ReplaceAllStringFunc(ln, withBg)
		var pad string
		if w := lipgloss.Width(ln); w < width {
			pad = strings.Repeat(" ", width-w)
		}
		lines[i] = bg + processed + bg + pad + reset
	}
	return strings.Join(lines, "\n")
}

// shadeBubble re-applies the bubble's subtle background after every SGR
// reset inside the rendered content, line by line. This covers two cases at
// once: trailing pad spaces after the last reset on a row (the original bug
// the input bar had), and *text* sitting between a reset and the next styled
// span (the case that breaks chroma-highlighted code blocks).
//
// Each line gets a trailing explicit reset so the active bg never leaks past
// EOL into the next row.
func shadeBubble(rendered string) string {
	bg := bubbleBgSGR()
	withBg := func(m string) string { return m + bg }

	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		processed := resetSGR.ReplaceAllStringFunc(ln, withBg)
		// We just appended bg after every reset, including the closing reset
		// at the end of the line. Strip that trailing bg so the line ends
		// clean and bg doesn't bleed into whatever follows.
		processed = strings.TrimSuffix(processed, bg)
		// And make sure the line really does end with a reset — lipgloss
		// usually emits one, but pad-only rows we built ourselves may not.
		if !strings.HasSuffix(processed, "\x1b[0m") && !strings.HasSuffix(processed, "\x1b[m") {
			processed += "\x1b[0m"
		}
		lines[i] = processed
	}
	return strings.Join(lines, "\n")
}

func renderInputArea(ta textarea.Model, outerWidth int) string {
	_ = outerWidth
	view := strings.TrimRight(ta.View(), "\n")
	bg := lipgloss.NewStyle().Background(subtleBg())
	// Make sure any unstyled gap inside the textarea picks up the shaded bg.
	view = unshadedSpaceRun.ReplaceAllStringFunc(view, func(match string) string {
		m := unshadedSpaceRun.FindStringSubmatch(match)
		return m[1] + bg.Render(m[2])
	})

	// Wrap the textarea in a thick left bar + horizontal padding *once*,
	// per row. Doing this externally (rather than via the textarea's Base
	// style) avoids the textarea drawing its own border inside ours.
	t := CurrentTheme()
	barColor := t.Accent()
	if !ta.Focused() {
		barColor = t.Dim()
	}
	bar := lipgloss.NewStyle().Foreground(barColor).Render("┃")
	pad := bg.Render(" ")
	prefix := bar + pad

	var b strings.Builder
	for i, ln := range strings.Split(view, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(prefix)
		b.WriteString(ln)
	}
	return b.String()
}

// styleTextarea sets only color styles on the textarea — no border, no
// padding. The thick left bar and shaded background that frame the input
// are drawn externally in renderInputArea so we don't end up with the
// textarea's internal viewport drawing one border and our Base style
// drawing a second one around it.
func styleTextarea(ta *textarea.Model) {
	t := CurrentTheme()
	st := ta.Styles()

	bg := lipgloss.NewStyle().Background(subtleBg())
	st.Focused.Base = bg
	st.Blurred.Base = bg
	st.Focused.CursorLine = bg
	st.Blurred.CursorLine = bg
	st.Focused.Text = bg
	st.Blurred.Text = bg
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(t.Dim()).Background(subtleBg())
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(t.Dim()).Background(subtleBg())

	ta.SetStyles(st)
}

func wordwrapKeepBlank(text string, width int) string {
	var out strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if strings.TrimSpace(line) == "" {
			out.WriteString(line)
			continue
		}
		out.WriteString(wrapLine(line, width))
	}
	return out.String()
}

// indentLines prepends prefix to every line of s. Used to shift multi-line
// styled blocks (e.g. the textarea) without losing per-row alignment — a
// plain "prefix + s" only indents the first row.
//
// Trailing newlines are preserved verbatim so we don't synthesize a phantom
// blank-and-padded row at the bottom of the input bar. (textarea.View()
// terminates its placeholder/content with "\n", which used to render as an
// extra empty shaded line below the real input row.)
func indentLines(text, prefix string) string {
	if prefix == "" {
		return text
	}
	trailingNL := strings.HasSuffix(text, "\n")
	trimmed := strings.TrimRight(text, "\n")
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	out := strings.Join(lines, "\n")
	if trailingNL {
		out += "\n"
	}
	return out
}

// wrapLine breaks `line` so each row's cell width is ≤ width, preferring word
// boundaries. Walks runes (not bytes) and uses lipgloss.Width per-rune so CJK,
// emoji, and multi-byte Latin text never get split mid-codepoint and wide
// cells don't blow past the bubble's right edge.
func wrapLine(line string, width int) string {
	if width < 1 {
		return line
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	var b strings.Builder
	runes := []rune(line)
	for len(runes) > 0 {
		// Find the rune index whose cumulative cell width would cross `width`.
		cells := 0
		cut := len(runes)
		for i, r := range runes {
			rw := lipgloss.Width(string(r))
			if cells+rw > width {
				cut = i
				break
			}
			cells += rw
		}
		if cut == len(runes) {
			b.WriteString(string(runes))
			return b.String()
		}
		if cut == 0 {
			// One rune already exceeds width. Emit it alone rather than loop.
			cut = 1
		}
		// Prefer to break at the last space within the chunk, but not so far
		// back that we leave a stubby first half — mirrors the original's
		// `idx < width/2 → use width` heuristic.
		spaceAt := -1
		for i := cut - 1; i > cut/2; i-- {
			if runes[i] == ' ' {
				spaceAt = i
				break
			}
		}
		if spaceAt > 0 {
			b.WriteString(string(runes[:spaceAt]))
			b.WriteByte('\n')
			// Skip the breaking space(s).
			i := spaceAt
			for i < len(runes) && runes[i] == ' ' {
				i++
			}
			runes = runes[i:]
		} else {
			b.WriteString(string(runes[:cut]))
			b.WriteByte('\n')
			i := cut
			for i < len(runes) && runes[i] == ' ' {
				i++
			}
			runes = runes[i:]
		}
	}
	return b.String()
}
