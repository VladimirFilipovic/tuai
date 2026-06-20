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

	for i := 0; i < len(lines); i++ {
		line := lines[i]
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
		if isTableHeader(lines, i) {
			rows, aligns, consumed := collectTable(lines, i)
			out.WriteString(renderTable(rows, aligns, wrapWidth))
			out.WriteString("\n")
			i += consumed - 1
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

// highlightForFile returns syntax-highlighted code for the given filename
// (lexer matched by extension), with no surrounding rule lines or language
// header — callers indent as they wish. Used for tool-body code that
// already lives inside a tool block (e.g. Write).
func highlightForFile(code, path string) string {
	lexer := lexers.Match(path)
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
		return code
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iter); err != nil {
		return code
	}
	return strings.TrimRight(buf.String(), "\n")
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

// --- GFM tables ---

const (
	alignLeft = iota
	alignCenter
	alignRight
)

// isTableHeader reports whether lines[i] starts a GFM table: a row containing
// at least one "|" and at least one non-empty cell, immediately followed by a
// delimiter row (`---|:--:|--:` style).
func isTableHeader(lines []string, i int) bool {
	if !strings.Contains(lines[i], "|") {
		return false
	}
	if i+1 >= len(lines) || !isDelimiterRow(lines[i+1]) {
		return false
	}
	for _, c := range splitTableRow(lines[i]) {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}

// isDelimiterRow reports whether s is a table delimiter row: every cell is made
// only of '-', ':' and spaces, and at least one cell carries a dash.
func isDelimiterRow(s string) bool {
	if !strings.Contains(s, "|") && !strings.Contains(s, "-") {
		return false
	}
	cells := splitTableRow(s)
	if len(cells) == 0 {
		return false
	}
	sawDash := false
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' && r != ' ' {
				return false
			}
		}
		if strings.ContainsRune(c, '-') {
			sawDash = true
		}
	}
	return sawDash
}

// splitTableRow splits a "| a | b |" row into trimmed cell strings, tolerating
// rows with or without the leading/trailing pipe.
func splitTableRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// parseAligns reads per-column alignment from a delimiter row.
func parseAligns(s string) []int {
	cells := splitTableRow(s)
	aligns := make([]int, len(cells))
	for i, c := range cells {
		c = strings.TrimSpace(c)
		left := strings.HasPrefix(c, ":")
		right := strings.HasSuffix(c, ":")
		switch {
		case left && right:
			aligns[i] = alignCenter
		case right:
			aligns[i] = alignRight
		default:
			aligns[i] = alignLeft
		}
	}
	return aligns
}

// collectTable gathers a table block beginning at lines[start] (the header).
// Returns the parsed rows (row 0 is the header), per-column alignments, and how
// many source lines were consumed.
func collectTable(lines []string, start int) (rows [][]string, aligns []int, consumed int) {
	aligns = parseAligns(lines[start+1])
	rows = append(rows, splitTableRow(lines[start]))
	i := start + 2
	for i < len(lines) {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" || !strings.Contains(ln, "|") {
			break
		}
		if fenceOpenRe.MatchString(ln) {
			break
		}
		rows = append(rows, splitTableRow(ln))
		i++
	}
	return rows, aligns, i - start
}

// renderTable lays a parsed table out with box-drawing borders, a bold header,
// per-column alignment, and column widths that fit wrapWidth (shrinking and
// wrapping cells when the natural layout would overflow). Rows are indented two
// cells so the frame sits inside the message bubble.
func renderTable(rows [][]string, aligns []int, wrapWidth int) string {
	if len(rows) == 0 {
		return ""
	}
	t := CurrentTheme()
	border := lipgloss.NewStyle().Foreground(t.Border())
	head := lipgloss.NewStyle().Foreground(t.Accent()).Bold(true)

	ncols := len(aligns)
	for _, r := range rows {
		if len(r) > ncols {
			ncols = len(r)
		}
	}
	if ncols == 0 {
		return ""
	}
	for len(aligns) < ncols {
		aligns = append(aligns, alignLeft)
	}

	// Natural width per column from the widest cell.
	widths := make([]int, ncols)
	for _, r := range rows {
		for c := 0; c < ncols; c++ {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			if w := lipgloss.Width(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	for c := range widths {
		if widths[c] < 1 {
			widths[c] = 1
		}
	}

	// Fit to the bubble: innerWidth minus the frame overhead (one bar between
	// and around each column, plus one padding cell on each side of a column).
	innerWidth := wrapWidth - 2
	if innerWidth < 8 {
		innerWidth = 8
	}
	avail := innerWidth - (ncols + 1) - 2*ncols
	if avail < ncols {
		avail = ncols
	}
	shrinkWidths(widths, avail)

	var b strings.Builder
	b.WriteString(tableRule(widths, "┌", "┬", "┐", border) + "\n")
	for ri, r := range rows {
		b.WriteString(tableRow(r, widths, aligns, ri == 0, border, head) + "\n")
		if ri == 0 {
			b.WriteString(tableRule(widths, "├", "┼", "┤", border) + "\n")
		}
	}
	b.WriteString(tableRule(widths, "└", "┴", "┘", border))
	return b.String()
}

// shrinkWidths reduces column widths in place to fit avail, scaling each column
// proportionally to its natural width (min 1) when the total overflows.
func shrinkWidths(widths []int, avail int) {
	total := 0
	for _, w := range widths {
		total += w
	}
	if total <= avail {
		return
	}
	used := 0
	for i, w := range widths {
		sw := w * avail / total
		if sw < 1 {
			sw = 1
		}
		widths[i] = sw
		used += sw
	}
	// Hand out / claw back the rounding remainder against the widest columns.
	for used < avail {
		mi := 0
		for j := range widths {
			if widths[j] < widths[mi] {
				mi = j
			}
		}
		widths[mi]++
		used++
	}
	for used > avail {
		mi := 0
		for j := range widths {
			if widths[j] > widths[mi] {
				mi = j
			}
		}
		if widths[mi] <= 1 {
			break
		}
		widths[mi]--
		used--
	}
}

// tableRule renders a horizontal frame line with the given corner/junction runes.
func tableRule(widths []int, left, mid, right string, border lipgloss.Style) string {
	segs := make([]string, len(widths))
	for i, w := range widths {
		segs[i] = strings.Repeat("─", w+2)
	}
	return "  " + border.Render(left+strings.Join(segs, mid)+right)
}

// tableRow renders one (possibly multi-line) table row, wrapping each cell to
// its column width and aligning the result.
func tableRow(cells []string, widths, aligns []int, isHeader bool, border, head lipgloss.Style) string {
	wrapped := make([][]string, len(widths))
	rowH := 1
	for c := range widths {
		cell := ""
		if c < len(cells) {
			cell = cells[c]
		}
		wrapped[c] = wrapCell(cell, widths[c])
		if len(wrapped[c]) > rowH {
			rowH = len(wrapped[c])
		}
	}
	bar := border.Render("│")
	var b strings.Builder
	for line := 0; line < rowH; line++ {
		b.WriteString("  " + bar)
		for c := range widths {
			txt := ""
			if line < len(wrapped[c]) {
				txt = wrapped[c][line]
			}
			if isHeader && txt != "" {
				txt = head.Render(txt)
			}
			b.WriteString(" " + padCell(txt, widths[c], aligns[c]) + " " + bar)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// wrapCell word-wraps plain cell text to width w, hard-cutting any single line
// still too wide (an unbreakable token).
func wrapCell(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	if w < 1 {
		w = 1
	}
	var out []string
	for _, ln := range strings.Split(wordwrap.String(s, w), "\n") {
		for lipgloss.Width(ln) > w {
			r := []rune(ln)
			out = append(out, string(r[:w]))
			ln = string(r[w:])
		}
		out = append(out, ln)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// padCell pads styled cell text to width per its alignment (width-aware so ANSI
// styling on the content doesn't throw the math off).
func padCell(s string, width, align int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	switch align {
	case alignRight:
		return strings.Repeat(" ", gap) + s
	case alignCenter:
		l := gap / 2
		return strings.Repeat(" ", l) + s + strings.Repeat(" ", gap-l)
	default:
		return s + strings.Repeat(" ", gap)
	}
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
