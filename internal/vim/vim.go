// Package vim adds a small modal-editing layer on top of charm.land/bubbles
// v2 textarea. Only Normal + Insert modes are implemented; Visual and
// command-line modes are deliberately out of scope. The motion/operator
// state machine follows the same prefix-map idea used by vimtea
// (github.com/kujtimiihoxha/vimtea), but instead of replacing the textarea
// we treat it as a buffer we drive: Value/SetValue + cursor positioning.
//
// Wiring contract (see internal/ui/chat.go):
//   - On every key, the host calls e.HandleKey(ta, keyString). The returned
//     bool says whether the key was consumed — the host must skip its own
//     textarea.Update for that key when true.
//   - Insert mode always returns false (the textarea handles the key).
//     The single exception is "esc", which Insert mode consumes to switch
//     back to Normal.
//   - The textarea is passed as a pointer on every call rather than
//     captured on the Editor; bubbles models routinely flow by value
//     through Update() so a stored pointer goes stale on the first frame.
//   - The host is also responsible for surfacing the current mode in its
//     own chrome (status indicator, cursor shape, etc.).
package vim

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
)

type Mode int

const (
	ModeInsert Mode = iota
	ModeNormal
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeNormal:
		return "NORMAL"
	}
	return "?"
}

// Editor holds the modal state. One per chat session.
type Editor struct {
	mode        Mode
	countBuf    string // digit-accumulator for things like "5j" or "d3w"
	op          rune   // pending operator: 'd', 'y', 'c', or 0
	opCount     int    // count captured at the time the operator was set
	keyBuf      string // multi-key prefix accumulator (currently only "g")
	register    string // unnamed yank register
	regLinewise bool   // whether register holds whole lines (changes p behavior)
	undoStack   []snapshot
	redoStack   []snapshot
}

type snapshot struct {
	value    string
	row, col int
}

func New() *Editor {
	return &Editor{mode: ModeInsert}
}

func (e *Editor) Mode() Mode { return e.mode }

// HasPending reports whether a partial command is buffered (count digits,
// pending operator, or a g-prefix waiting for its second key). Used by
// hosts that want to swallow esc only when there's something to clear.
func (e *Editor) HasPending() bool {
	return e.countBuf != "" || e.op != 0 || e.keyBuf != ""
}

// snap pushes the current buffer+cursor onto the undo stack. Call this
// *before* a mutating op so `u` rolls back to the pre-op state.
func (e *Editor) snap(ta *textarea.Model) {
	row, col := cursor(ta)
	e.undoStack = append(e.undoStack, snapshot{value: ta.Value(), row: row, col: col})
	e.redoStack = nil
}

func (e *Editor) undo(ta *textarea.Model) {
	if len(e.undoStack) == 0 {
		return
	}
	row, col := cursor(ta)
	e.redoStack = append(e.redoStack, snapshot{value: ta.Value(), row: row, col: col})
	last := e.undoStack[len(e.undoStack)-1]
	e.undoStack = e.undoStack[:len(e.undoStack)-1]
	ta.SetValue(last.value)
	setCursor(ta, last.row, last.col)
}

func (e *Editor) redoOp(ta *textarea.Model) {
	if len(e.redoStack) == 0 {
		return
	}
	row, col := cursor(ta)
	e.undoStack = append(e.undoStack, snapshot{value: ta.Value(), row: row, col: col})
	last := e.redoStack[len(e.redoStack)-1]
	e.redoStack = e.redoStack[:len(e.redoStack)-1]
	ta.SetValue(last.value)
	setCursor(ta, last.row, last.col)
}

// EnterNormal switches to Normal mode and follows vim's convention of
// nudging the cursor one cell left (since Insert sat after the last typed
// char and Normal sits on it).
func (e *Editor) EnterNormal(ta *textarea.Model) {
	if e.mode == ModeNormal {
		e.resetPending()
		return
	}
	e.mode = ModeNormal
	e.resetPending()
	row, col := cursor(ta)
	if col > 0 {
		setCursor(ta, row, col-1)
	}
}

func (e *Editor) enterInsert() {
	e.mode = ModeInsert
	e.resetPending()
}

func (e *Editor) resetPending() {
	e.countBuf = ""
	e.op = 0
	e.opCount = 0
	e.keyBuf = ""
}

// HandleKey returns true when the keypress was consumed by the vim layer.
// The host should skip its own textarea routing for that key when true.
func (e *Editor) HandleKey(ta *textarea.Model, key string) bool {
	if e.mode == ModeInsert {
		if key == "esc" {
			e.EnterNormal(ta)
			return true
		}
		return false
	}

	// In Normal mode we map the arrow keys onto h/j/k/l so users who reach
	// for the arrows don't end up sending raw escape sequences into the
	// textarea. This is purely a convenience — vim itself does the same.
	switch key {
	case "up":
		key = "k"
	case "down":
		key = "j"
	case "left":
		key = "h"
	case "right":
		key = "l"
	}

	// Pass host-level keys through. The chat owns Enter (send), Ctrl-*
	// (palette, paste, clear, etc.), Tab, and the viewport scroll keys —
	// vim has no use for them and intercepting silently would strand the
	// user (e.g. unable to send from Normal mode).
	switch key {
	case "enter", "tab", "shift+tab", "pgup", "pgdown", "home", "end":
		return false
	}
	if key != "ctrl+r" && (strings.HasPrefix(key, "ctrl+") ||
		strings.HasPrefix(key, "alt+") ||
		strings.HasPrefix(key, "cmd+") ||
		strings.HasPrefix(key, "super+")) {
		return false
	}

	if key == "esc" {
		// In Normal mode esc clears any partial command; with nothing
		// pending we let the host see it (so the host's own esc binding —
		// "back to sessions" / "cancel stream" — still works).
		if e.HasPending() {
			e.resetPending()
			return true
		}
		return false
	}

	// Digits build a count, except that a leading 0 is the "line start"
	// motion (vim disambiguates the same way).
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		if !(key == "0" && e.countBuf == "") {
			e.countBuf += key
			return true
		}
	}

	count := 1
	if e.countBuf != "" {
		n := 0
		for _, r := range e.countBuf {
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			count = n
		}
		e.countBuf = ""
	}

	if e.keyBuf != "" {
		combined := e.keyBuf + key
		e.keyBuf = ""
		e.runCombined(ta, combined, count)
		return true
	}

	e.run(ta, key, count)
	return true
}

func (e *Editor) runCombined(ta *textarea.Model, seq string, count int) {
	switch seq {
	case "gg":
		// "gg" on its own goes to line 1 (idx 0); "Ngg" goes to line N.
		// As an operator target, it forms a linewise range from the
		// cursor to that line (the d/y/c branch below honors opCount).
		row := 0
		if count > 1 {
			row = count - 1
		}
		if row >= lineCount(ta) {
			row = lineCount(ta) - 1
		}
		if e.op != 0 {
			e.applyLinewise(ta, e.op, e.line2lineRange(ta, row))
			e.op = 0
			e.opCount = 0
			return
		}
		setCursor(ta, row, firstNonBlank(line(ta, row)))
	}
}

func (e *Editor) run(ta *textarea.Model, key string, count int) {
	// vim's `2d3w` style: the operator's count and the motion's count
	// multiply. Either alone is fine; opCount is 0 when none was given.
	motionCount := count
	if e.op != 0 && e.opCount > 0 {
		motionCount = count * e.opCount
	}

	if rg, ok := e.motion(ta, key, motionCount); ok {
		if e.op != 0 {
			e.applyOp(ta, e.op, rg)
			e.op = 0
			e.opCount = 0
			return
		}
		setCursor(ta, rg.endRow, clampCol(line(ta, rg.endRow), rg.endCol))
		return
	}

	if key == "g" {
		e.keyBuf = "g"
		return
	}

	// Operators. A second press of the same operator is the linewise form
	// (dd / yy / cc). The two counts multiply: `2d3d` deletes 6 lines.
	if key == "d" || key == "y" || key == "c" {
		opRune := rune(key[0])
		if e.op == opRune {
			total := count
			if e.opCount > 0 {
				total = count * e.opCount
			}
			row, _ := cursor(ta)
			end := row + total - 1
			if end >= lineCount(ta) {
				end = lineCount(ta) - 1
			}
			e.applyLinewise(ta, opRune, lineRange{startRow: row, endRow: end})
			e.op = 0
			e.opCount = 0
			return
		}
		e.op = opRune
		e.opCount = count
		return
	}

	switch key {
	case "i":
		e.enterInsert()
	case "a":
		row, col := cursor(ta)
		if col < runeLen(line(ta, row)) {
			col++
		}
		setCursor(ta, row, col)
		e.enterInsert()
	case "I":
		row, _ := cursor(ta)
		setCursor(ta, row, firstNonBlank(line(ta, row)))
		e.enterInsert()
	case "A":
		row, _ := cursor(ta)
		setCursor(ta, row, runeLen(line(ta, row)))
		e.enterInsert()
	case "o":
		e.snap(ta)
		row, _ := cursor(ta)
		ls := lines(ta)
		ls = insertAt(ls, row+1, "")
		setLines(ta, ls)
		setCursor(ta, row+1, 0)
		e.enterInsert()
	case "O":
		e.snap(ta)
		row, _ := cursor(ta)
		ls := lines(ta)
		ls = insertAt(ls, row, "")
		setLines(ta, ls)
		setCursor(ta, row, 0)
		e.enterInsert()
	case "x":
		e.deleteUnderCursor(ta, count)
	case "p":
		e.paste(ta, count, true)
	case "P":
		e.paste(ta, count, false)
	case "u":
		e.undo(ta)
	case "ctrl+r":
		e.redoOp(ta)
	}
}

type rng struct {
	startRow, startCol int
	endRow, endCol     int
	inclusiveEnd       bool
	linewise           bool
}

type lineRange struct {
	startRow, endRow int
}

func (e *Editor) line2lineRange(ta *textarea.Model, row int) lineRange {
	cur, _ := cursor(ta)
	if row < cur {
		return lineRange{startRow: row, endRow: cur}
	}
	return lineRange{startRow: cur, endRow: row}
}

func (e *Editor) motion(ta *textarea.Model, key string, count int) (rng, bool) {
	row, col := cursor(ta)
	r := rng{startRow: row, startCol: col, endRow: row, endCol: col}

	switch key {
	case "h":
		for i := 0; i < count && r.endCol > 0; i++ {
			r.endCol--
		}
	case "l":
		ln := line(ta, r.endRow)
		max := runeLen(ln) - 1
		if max < 0 {
			max = 0
		}
		for i := 0; i < count && r.endCol < max; i++ {
			r.endCol++
		}
		r.inclusiveEnd = true
	case "j":
		for i := 0; i < count && r.endRow < lineCount(ta)-1; i++ {
			r.endRow++
		}
		r.linewise = true
	case "k":
		for i := 0; i < count && r.endRow > 0; i++ {
			r.endRow--
		}
		r.linewise = true
	case "0":
		r.endCol = 0
	case "$":
		r.endCol = runeLen(line(ta, r.endRow))
		if e.op != 0 {
			r.inclusiveEnd = true
		} else {
			r.endCol = clampCol(line(ta, r.endRow), r.endCol)
		}
	case "^":
		r.endCol = firstNonBlank(line(ta, r.endRow))
	case "w":
		for i := 0; i < count; i++ {
			r.endRow, r.endCol = nextWordStart(lines(ta), r.endRow, r.endCol)
		}
	case "b":
		for i := 0; i < count; i++ {
			r.endRow, r.endCol = prevWordStart(lines(ta), r.endRow, r.endCol)
		}
	case "e":
		for i := 0; i < count; i++ {
			r.endRow, r.endCol = wordEnd(lines(ta), r.endRow, r.endCol)
		}
		r.inclusiveEnd = true
	case "G":
		r.endRow = lineCount(ta) - 1
		r.endCol = firstNonBlank(line(ta, r.endRow))
		r.linewise = true
	default:
		return rng{}, false
	}
	return r, true
}

func (e *Editor) deleteUnderCursor(ta *textarea.Model, count int) {
	row, col := cursor(ta)
	ln := line(ta, row)
	runes := []rune(ln)
	if col >= len(runes) {
		return
	}
	e.snap(ta)
	end := col + count
	if end > len(runes) {
		end = len(runes)
	}
	removed := string(runes[col:end])
	newLine := string(runes[:col]) + string(runes[end:])
	setLine(ta, row, newLine)
	e.register = removed
	e.regLinewise = false
	newCol := col
	if newCol >= runeLen(line(ta, row)) && newCol > 0 {
		newCol--
	}
	setCursor(ta, row, newCol)
}

func (e *Editor) applyOp(ta *textarea.Model, op rune, r rng) {
	if r.linewise {
		lr := lineRange{startRow: r.startRow, endRow: r.endRow}
		if lr.endRow < lr.startRow {
			lr.startRow, lr.endRow = lr.endRow, lr.startRow
		}
		e.applyLinewise(ta, op, lr)
		return
	}
	sR, sC, eR, eC := r.startRow, r.startCol, r.endRow, r.endCol
	if sR > eR || (sR == eR && sC > eC) {
		sR, eR = eR, sR
		sC, eC = eC, sC
	}
	if r.inclusiveEnd {
		eC++
	}
	e.snap(ta)
	ls := lines(ta)
	if sR == eR {
		runes := []rune(ls[sR])
		if eC > len(runes) {
			eC = len(runes)
		}
		if sC > len(runes) {
			sC = len(runes)
		}
		e.register = string(runes[sC:eC])
		e.regLinewise = false
		if op == 'y' {
			setCursor(ta, sR, sC)
			return
		}
		ls[sR] = string(runes[:sC]) + string(runes[eC:])
	} else {
		first := []rune(ls[sR])
		last := []rune(ls[eR])
		if sC > len(first) {
			sC = len(first)
		}
		if eC > len(last) {
			eC = len(last)
		}
		var b strings.Builder
		b.WriteString(string(first[sC:]))
		for i := sR + 1; i < eR; i++ {
			b.WriteByte('\n')
			b.WriteString(ls[i])
		}
		b.WriteByte('\n')
		b.WriteString(string(last[:eC]))
		e.register = b.String()
		e.regLinewise = false
		if op == 'y' {
			setCursor(ta, sR, sC)
			return
		}
		newLine := string(first[:sC]) + string(last[eC:])
		ls = append(ls[:sR], append([]string{newLine}, ls[eR+1:]...)...)
	}
	setLines(ta, ls)
	target := ""
	if sR < len(ls) {
		target = ls[sR]
	}
	setCursor(ta, sR, clampCol(target, sC))
	if op == 'c' {
		e.enterInsert()
	}
}

func (e *Editor) applyLinewise(ta *textarea.Model, op rune, lr lineRange) {
	if lr.startRow < 0 {
		lr.startRow = 0
	}
	if lr.endRow >= lineCount(ta) {
		lr.endRow = lineCount(ta) - 1
	}
	ls := lines(ta)
	if lr.startRow > lr.endRow || lr.startRow >= len(ls) {
		return
	}
	e.snap(ta)
	taken := strings.Join(ls[lr.startRow:lr.endRow+1], "\n")
	e.register = taken
	e.regLinewise = true
	if op == 'y' {
		return
	}
	if op == 'c' {
		newLs := append([]string{}, ls[:lr.startRow]...)
		newLs = append(newLs, "")
		newLs = append(newLs, ls[lr.endRow+1:]...)
		setLines(ta, newLs)
		setCursor(ta, lr.startRow, 0)
		e.enterInsert()
		return
	}
	newLs := append([]string{}, ls[:lr.startRow]...)
	newLs = append(newLs, ls[lr.endRow+1:]...)
	if len(newLs) == 0 {
		newLs = []string{""}
	}
	setLines(ta, newLs)
	target := lr.startRow
	if target >= len(newLs) {
		target = len(newLs) - 1
	}
	setCursor(ta, target, firstNonBlank(newLs[target]))
}

func (e *Editor) paste(ta *textarea.Model, count int, after bool) {
	if e.register == "" {
		return
	}
	e.snap(ta)
	body := strings.Repeat(e.register, count)

	if e.regLinewise {
		ls := lines(ta)
		row, _ := cursor(ta)
		newLines := strings.Split(body, "\n")
		var insertAtIdx int
		if after {
			insertAtIdx = row + 1
		} else {
			insertAtIdx = row
		}
		out := append([]string{}, ls[:insertAtIdx]...)
		out = append(out, newLines...)
		out = append(out, ls[insertAtIdx:]...)
		setLines(ta, out)
		setCursor(ta, insertAtIdx, firstNonBlank(newLines[0]))
		return
	}

	row, col := cursor(ta)
	ln := line(ta, row)
	runes := []rune(ln)
	insertCol := col
	if after && len(runes) > 0 {
		insertCol = col + 1
	}
	if insertCol > len(runes) {
		insertCol = len(runes)
	}

	if !strings.Contains(body, "\n") {
		newLine := string(runes[:insertCol]) + body + string(runes[insertCol:])
		setLine(ta, row, newLine)
		setCursor(ta, row, insertCol+runeLen(body)-1)
		return
	}
	parts := strings.Split(body, "\n")
	head := string(runes[:insertCol])
	tail := string(runes[insertCol:])
	ls := lines(ta)
	first := head + parts[0]
	mid := parts[1 : len(parts)-1]
	last := parts[len(parts)-1] + tail
	newLs := append([]string{}, ls[:row]...)
	newLs = append(newLs, first)
	newLs = append(newLs, mid...)
	newLs = append(newLs, last)
	newLs = append(newLs, ls[row+1:]...)
	setLines(ta, newLs)
	setCursor(ta, row+len(parts)-1, runeLen(parts[len(parts)-1]))
}
