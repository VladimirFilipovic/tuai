package vim

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
)

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
	default:
		// Unknown sequence after the 'g' prefix (e.g. user typed 'g' then 'x').
		// Vim treats this as an aborted command: discard the prefix, the count,
		// and any pending operator so the *next* keypress starts clean. The
		// alternative — leaving e.op set — would silently turn whatever key
		// the user types next into the operator's target.
		_ = count
		e.op = 0
		e.opCount = 0
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
