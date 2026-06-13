package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	lipgloss "charm.land/lipgloss/v2"
)

// The @ autocomplete shows a popup of matching paths whenever the user is
// typing an @-mention. Triggered as soon as `@` is typed at a word boundary;
// the popup tracks the fragment after the `@` and offers up to a handful of
// matches. Tab/enter accepts; esc dismisses.

const (
	autocompleteMaxResults  = 50
	autocompleteVisibleRows = 6
)

type pathMatch struct {
	display string // path relative to cwd, with `/` for dirs
	insert  string // same, plus trailing `/` when a directory (cursor stays inside)
	isDir   bool
}

type pathAutocompleteModel struct {
	active   bool
	fragment string      // text after '@' up to the cursor
	matches  []pathMatch // pre-filtered list
	sel      int         // selected index into matches
}

func (p pathAutocompleteModel) height() int {
	if !p.active {
		return 0
	}
	if len(p.matches) == 0 {
		// The "no matches" placeholder still occupies one row.
		return 1
	}
	rows := min(len(p.matches), autocompleteVisibleRows)
	// +1 for the header row that explains what's being matched.
	return rows + 1
}

// refresh recomputes autocomplete state from the textarea. If the cursor sits
// inside an @-mention we re-search; otherwise we deactivate.
func (p *pathAutocompleteModel) refresh(ta textarea.Model) {
	frag, _, ok := fragmentAtCursor(ta.Value(), cursorByteOffset(ta))
	if !ok {
		p.active = false
		p.fragment = ""
		p.matches = nil
		p.sel = 0
		return
	}
	if p.active && frag == p.fragment {
		return // nothing changed
	}
	// Fragment changed: a new query means the previous selected index points
	// at a different match (or none). Reset to the top so the highlight
	// tracks the best new match rather than parking on an unrelated entry.
	p.active = true
	p.fragment = frag
	p.matches = searchPaths(frag)
	p.sel = 0
}

func (p *pathAutocompleteModel) close() {
	p.active = false
	p.fragment = ""
	p.matches = nil
	p.sel = 0
}

func (p *pathAutocompleteModel) moveUp() {
	if len(p.matches) == 0 {
		return
	}
	p.sel = (p.sel - 1 + len(p.matches)) % len(p.matches)
}

func (p *pathAutocompleteModel) moveDown() {
	if len(p.matches) == 0 {
		return
	}
	p.sel = (p.sel + 1) % len(p.matches)
}

// accept replaces the @-fragment in `ta` with the currently selected match
// and repositions the cursor right after the inserted path. Returns true
// when something was inserted (i.e. there was a match to accept).
func (p *pathAutocompleteModel) accept(ta *textarea.Model) bool {
	if !p.active || len(p.matches) == 0 {
		return false
	}
	m := p.matches[p.sel]
	value := ta.Value()
	cursor := cursorByteOffset(*ta)
	_, atPos, ok := fragmentAtCursor(value, cursor)
	if !ok {
		return false
	}
	// Keep the literal `@`; everything after it is the path. The cursor lands
	// at the end of the inserted text so the user can keep typing (or, if it's
	// a directory, the popup re-opens with the directory's contents).
	before := value[:atPos+1] + m.insert
	after := value[cursor:]
	replaceWithCursor(ta, before, after)
	// For dirs, keep the popup open with the new fragment so the user can
	// drill in. For files, dismiss.
	if m.isDir {
		p.refresh(*ta)
	} else {
		p.close()
	}
	return true
}

func (p pathAutocompleteModel) view(width int) string {
	if !p.active {
		return ""
	}
	t := CurrentTheme()
	dim := lipgloss.NewStyle().Foreground(t.Dim())
	if len(p.matches) == 0 {
		hint := "@" + p.fragment
		return dim.Render("  no matches for " + hint)
	}

	rows := min(len(p.matches), autocompleteVisibleRows)
	// Window the visible slice around the selection.
	start := 0
	if p.sel >= rows {
		start = p.sel - rows + 1
	}
	end := start + rows
	if end > len(p.matches) {
		end = len(p.matches)
		start = max(end-rows, 0)
	}

	var b strings.Builder
	header := dim.Render("  paths matching @" + p.fragment)
	if len(p.matches) > rows {
		header += dim.Render(" · " + strconv.Itoa(p.sel+1) + "/" + strconv.Itoa(len(p.matches)))
	}
	b.WriteString(header + "\n")

	normal := lipgloss.NewStyle().Foreground(t.Dim())
	selected := lipgloss.NewStyle().Foreground(t.Accent()).Bold(true)
	dirAccent := lipgloss.NewStyle().Foreground(t.Assistant())
	for i := start; i < end; i++ {
		m := p.matches[i]
		marker := "  "
		style := normal
		if i == p.sel {
			marker = "▶ "
			style = selected
		}
		label := m.display
		if m.isDir {
			label = dirAccent.Render(label)
		}
		b.WriteString(style.Render(marker) + truncateRow(label, max(width-4, 10)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// fragmentAtCursor returns the text typed after the most recent '@' that's
// still "open" — i.e. preceded by start-of-input or whitespace, and has no
// whitespace between it and the cursor. Returns ok=false otherwise.
func fragmentAtCursor(value string, cursorByte int) (frag string, atPos int, ok bool) {
	if cursorByte < 0 {
		return "", 0, false
	}
	if cursorByte > len(value) {
		cursorByte = len(value)
	}
	pos := cursorByte
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(value[:pos])
		if r == '@' {
			startOfAt := pos - size
			if startOfAt == 0 {
				return value[pos:cursorByte], startOfAt, true
			}
			prev, _ := utf8.DecodeLastRuneInString(value[:startOfAt])
			if unicode.IsSpace(prev) {
				return value[pos:cursorByte], startOfAt, true
			}
			return "", 0, false
		}
		if unicode.IsSpace(r) {
			return "", 0, false
		}
		pos -= size
	}
	return "", 0, false
}

// searchPaths returns filesystem entries matching the @-fragment, sorted with
// directories before files. The fragment is treated as a path relative to cwd;
// the last component is the prefix to match. An empty fragment lists the cwd.
func searchPaths(fragment string) []pathMatch {
	dir := "."
	prefix := fragment
	if d, ok := strings.CutSuffix(fragment, "/"); ok {
		dir = d
		if dir == "" {
			dir = "/"
		}
		prefix = ""
	} else if strings.Contains(fragment, "/") {
		dir = filepath.Dir(fragment)
		prefix = filepath.Base(fragment)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	showHidden := strings.HasPrefix(prefix, ".")
	lowerPrefix := strings.ToLower(prefix)
	var out []pathMatch
	for _, e := range entries {
		name := e.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), lowerPrefix) {
			continue
		}
		var display string
		if dir == "." {
			display = name
		} else {
			display = filepath.ToSlash(filepath.Join(dir, name))
		}
		isDir := e.IsDir()
		insert := display
		if isDir {
			insert += "/"
		}
		out = append(out, pathMatch{display: display, insert: insert, isDir: isDir})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].isDir != out[j].isDir {
			return out[i].isDir
		}
		return strings.ToLower(out[i].display) < strings.ToLower(out[j].display)
	})
	if len(out) > autocompleteMaxResults {
		out = out[:autocompleteMaxResults]
	}
	return out
}

// cursorByteOffset converts the textarea's (line, column) cursor to a byte
// offset into ta.Value(). Column is in runes, not bytes, so we re-encode.
func cursorByteOffset(ta textarea.Model) int {
	val := ta.Value()
	line := ta.Line()
	col := ta.Column()
	pieces := strings.Split(val, "\n")
	offset := 0
	for i := 0; i < line && i < len(pieces); i++ {
		offset += len(pieces[i]) + 1
	}
	if line >= 0 && line < len(pieces) {
		runes := []rune(pieces[line])
		if col > len(runes) {
			col = len(runes)
		}
		offset += len(string(runes[:col]))
	}
	return offset
}

// replaceWithCursor rewrites the textarea so the value is `before + after` and
// the cursor lands at the end of `before` (i.e. just before `after`). We
// achieve this by writing `after` first, then prepending `before` via
// InsertString — which advances the cursor through the inserted runes, leaving
// it at the boundary we want.
func replaceWithCursor(ta *textarea.Model, before, after string) {
	ta.SetValue(after)
	ta.MoveToBegin()
	ta.InsertString(before)
}

// truncateRow shortens s (which may contain ANSI escapes) so it fits in width
// terminal cells. Uses lipgloss.Width for ANSI-aware measurement.
func truncateRow(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// Strip ANSI then truncate — easier than walking escapes. Acceptable here
	// because the popup styles are simple foreground colors and the row is
	// dim/normal regardless.
	plain := stripANSI(s)
	runes := []rune(plain)
	if len(runes) <= width-1 {
		return plain
	}
	return string(runes[:width-1]) + "…"
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
