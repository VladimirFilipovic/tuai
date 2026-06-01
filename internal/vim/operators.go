package vim

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
)

// rng is a motion's result: the start/end position plus flags that tell
// the operator how to slice the range. inclusiveEnd is for motions like `l`
// and `e` where the end cell itself is part of the range; linewise marks
// motions like `j`/`k`/`G`/`gg` that snap to whole-line ranges.
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
		// "G" alone goes to the last line; "NG" jumps to line N. Mirrors the
		// "gg" handler — count=1 (default, no prefix) keeps the unprefixed
		// behavior, count>1 is the explicit target.
		target := lineCount(ta) - 1
		if count > 1 {
			target = count - 1
			if target >= lineCount(ta) {
				target = lineCount(ta) - 1
			}
		}
		if target < 0 {
			target = 0
		}
		r.endRow = target
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
	// Linewise registers are joined with a newline between repetitions so
	// `3p` of a yy'd line produces three new lines, not one concatenated
	// line. Charwise registers use plain repetition.
	var body string
	if e.regLinewise && count > 1 {
		parts := make([]string, count)
		for i := range parts {
			parts[i] = e.register
		}
		body = strings.Join(parts, "\n")
	} else {
		body = strings.Repeat(e.register, count)
	}

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
	// Cursor on the last pasted character (mirrors the single-line branch's
	// `-1`). Clamp to 0 when the final part is empty (e.g. trailing newline).
	endCol := runeLen(parts[len(parts)-1]) - 1
	if endCol < 0 {
		endCol = 0
	}
	setCursor(ta, row+len(parts)-1, endCol)
}
