package ui

import (
	"bytes"
	"regexp"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	chroma "github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/muesli/reflow/wordwrap"
)

var (
	fenceOpenRe  = regexp.MustCompile("^\\s{0,3}```([\\w+\\-]*)\\s*$")
	fenceCloseRe = regexp.MustCompile("^\\s{0,3}```\\s*$")
	inlineCodeRe = regexp.MustCompile("`([^`\\n]+)`")
	boldRe       = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
)

// renderMarkdown turns assistant text into styled output. Splits on fenced
// code blocks; everything in a fence goes through chroma at the declared
// language (or "text" if unknown). Outside fences we render inline code and
// bold, then word-wrap.
func renderMarkdown(text string, wrapWidth int) string {
	if wrapWidth < 10 {
		wrapWidth = 10
	}

	var out strings.Builder
	lines := strings.Split(text, "\n")

	var (
		inFence    bool
		fenceLang  string
		fenceLines []string
	)

	flushFence := func() {
		code := strings.Join(fenceLines, "\n")
		if isDiffLang(fenceLang) || (fenceLang == "" && looksLikeDiff(code)) {
			out.WriteString(renderDiff(code, wrapWidth))
			fenceLines = fenceLines[:0]
			fenceLang = ""
			return
		}
		if len(fenceLines) == 0 {
			out.WriteString(highlightCode("", fenceLang, wrapWidth))
			fenceLang = ""
			return
		}
		out.WriteString(highlightCode(code, fenceLang, wrapWidth))
		fenceLines = fenceLines[:0]
		fenceLang = ""
	}

	for _, line := range lines {
		if inFence {
			if fenceCloseRe.MatchString(line) {
				flushFence()
				inFence = false
				continue
			}
			fenceLines = append(fenceLines, line)
			continue
		}
		if m := fenceOpenRe.FindStringSubmatch(line); m != nil {
			inFence = true
			fenceLang = m[1]
			continue
		}
		out.WriteString(renderProseLine(line, wrapWidth))
		out.WriteString("\n")
	}

	if inFence {
		flushFence()
	}

	// Trim trailing blank line
	s := out.String()
	return strings.TrimRight(s, "\n")
}

func renderProseLine(line string, wrapWidth int) string {
	if line == "" {
		return ""
	}
	// Bold
	line = boldRe.ReplaceAllStringFunc(line, func(m string) string {
		inner := m[2 : len(m)-2]
		return lipgloss.NewStyle().Bold(true).Render(inner)
	})
	// Inline code
	line = inlineCodeRe.ReplaceAllStringFunc(line, func(m string) string {
		inner := m[1 : len(m)-1]
		return s.Inline.Render(inner)
	})
	return wordwrap.String(line, wrapWidth)
}

func highlightCode(code, lang string, wrapWidth int) string {
	if lang == "" {
		lang = "text"
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get(CurrentTheme().Chroma())
	if style == nil {
		style = styles.Fallback
	}

	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iter, err := lexer.Tokenise(nil, code)
	if err != nil {
		return renderPlainCodeBlock(code, lang, wrapWidth)
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iter); err != nil {
		return renderPlainCodeBlock(code, lang, wrapWidth)
	}

	rendered := buf.String()
	return wrapCodeBlock(rendered, lang, wrapWidth)
}

func wrapCodeBlock(rendered, lang string, wrapWidth int) string {
	var b strings.Builder
	// Account for the 2-cell "  " prefix we prepend to every row so the
	// horizontal rule sits inside the bubble instead of poking past it.
	innerWidth := wrapWidth - 2
	if innerWidth < 4 {
		innerWidth = 4
	}
	hr := dividerColor(innerWidth, CurrentTheme().Border())
	if lang != "" && lang != "text" {
		b.WriteString("  " + s.CodeLang.Render(lang) + "\n")
	}
	b.WriteString("  " + hr + "\n")
	for _, ln := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		b.WriteString("  " + ln + "\n")
	}
	b.WriteString("  " + hr)
	return b.String()
}

// isDiffLang reports whether a fence's language tag should be rendered as a
// unified diff. Covers the spellings Claude (and most tools) emit.
func isDiffLang(lang string) bool {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "diff", "patch", "udiff", "unified-diff":
		return true
	}
	return false
}

// looksLikeDiff sniffs whether an unlabelled fenced block is a unified diff.
// We require at least one hunk header (@@) AND one +/- line so we don't
// false-positive on prose that happens to start with "-".
func looksLikeDiff(code string) bool {
	var hasHunk, hasChange bool
	for _, ln := range strings.Split(code, "\n") {
		switch {
		case strings.HasPrefix(ln, "@@"):
			hasHunk = true
		case strings.HasPrefix(ln, "+++ "), strings.HasPrefix(ln, "--- "):
			hasHunk = true
		case strings.HasPrefix(ln, "+"), strings.HasPrefix(ln, "-"):
			hasChange = true
		}
		if hasHunk && hasChange {
			return true
		}
	}
	return false
}

// renderDiff paints a unified diff with theme-aware colors instead of going
// through chroma. We do it ourselves because (a) chroma's diff lexer uses its
// own style palette which fights the active theme, and (b) we want each line
// to carry only a foreground color so the bubble's subtle background reads
// through. Lines aren't wrapped — alignment matters in diffs more than fit.
func renderDiff(code string, wrapWidth int) string {
	t := CurrentTheme()
	add := lipgloss.NewStyle().Foreground(t.Assistant())
	del := lipgloss.NewStyle().Foreground(t.Error())
	hunk := lipgloss.NewStyle().Foreground(t.Accent()).Bold(true)
	file := lipgloss.NewStyle().Foreground(t.Dim()).Bold(true)
	meta := lipgloss.NewStyle().Foreground(t.Dim()).Italic(true)

	var b strings.Builder
	innerWidth := wrapWidth - 2
	if innerWidth < 4 {
		innerWidth = 4
	}
	hr := dividerColor(innerWidth, t.Border())
	b.WriteString("  " + s.CodeLang.Render("diff") + "\n")
	b.WriteString("  " + hr + "\n")
	for _, ln := range strings.Split(strings.TrimRight(code, "\n"), "\n") {
		var styled string
		switch {
		case strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"):
			styled = file.Render(ln)
		case strings.HasPrefix(ln, "@@"):
			styled = hunk.Render(ln)
		case strings.HasPrefix(ln, "diff "), strings.HasPrefix(ln, "index "),
			strings.HasPrefix(ln, "new file"), strings.HasPrefix(ln, "deleted file"),
			strings.HasPrefix(ln, "rename "), strings.HasPrefix(ln, "similarity "),
			strings.HasPrefix(ln, "Binary "):
			styled = meta.Render(ln)
		case strings.HasPrefix(ln, "+"):
			styled = add.Render(ln)
		case strings.HasPrefix(ln, "-"):
			styled = del.Render(ln)
		default:
			styled = ln
		}
		b.WriteString("  " + styled + "\n")
	}
	b.WriteString("  " + hr)
	return b.String()
}

func renderPlainCodeBlock(code, lang string, wrapWidth int) string {
	var b strings.Builder
	innerWidth := wrapWidth - 2
	if innerWidth < 4 {
		innerWidth = 4
	}
	hr := dividerColor(innerWidth, CurrentTheme().Border())
	if lang != "" && lang != "text" {
		b.WriteString("  " + s.CodeLang.Render(lang) + "\n")
	}
	b.WriteString("  " + hr + "\n")
	for _, ln := range strings.Split(strings.TrimRight(code, "\n"), "\n") {
		b.WriteString("  " + s.CodeBlock.Render(ln) + "\n")
	}
	b.WriteString("  " + hr)
	return b.String()
}
