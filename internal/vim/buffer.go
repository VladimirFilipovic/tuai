package vim

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textarea"
)

// The buffer helpers wrap a bubbles textarea so the vim layer can think in
// terms of "lines + (row, col)" without poking the textarea's private
// cursor state. Every mutating op goes Value → mutate slice of lines →
// SetValue → reposition. That is wasteful per-keystroke but keeps the two
// cursors honest with no chance of drift, which matters more than
// throughput on an input bar.
//
// The textarea pointer is passed into every call rather than captured on
// the Editor — the host (bubbles ecosystem) routinely copies its model
// struct through Update(), so a long-lived pointer would go stale.

func lines(ta *textarea.Model) []string {
	return strings.Split(ta.Value(), "\n")
}

func line(ta *textarea.Model, row int) string {
	ls := lines(ta)
	if row < 0 || row >= len(ls) {
		return ""
	}
	return ls[row]
}

func lineCount(ta *textarea.Model) int {
	return strings.Count(ta.Value(), "\n") + 1
}

func cursor(ta *textarea.Model) (row, col int) {
	return ta.Line(), ta.Column()
}

// setCursor moves the textarea cursor to (row, col). We walk from the top
// rather than relying on relative delta — relative moves are correct in
// isolation but compose poorly when row counts can change between calls
// (e.g. after deleting a line).
func setCursor(ta *textarea.Model, row, col int) {
	ta.MoveToBegin()
	for i := 0; i < row; i++ {
		ta.CursorDown()
	}
	if col < 0 {
		col = 0
	}
	ta.SetCursorColumn(col)
}

func setLines(ta *textarea.Model, ls []string) {
	ta.SetValue(strings.Join(ls, "\n"))
}

func setLine(ta *textarea.Model, row int, s string) {
	ls := lines(ta)
	if row < 0 || row >= len(ls) {
		return
	}
	ls[row] = s
	setLines(ta, ls)
}

func runeLen(s string) int { return len([]rune(s)) }

// clampCol caps col to the line's last valid char position. In Normal mode
// the cursor sits *on* a character, so a 5-char line allows cols 0..4; an
// empty line allows only col 0. Insert mode is one cell more permissive,
// but we don't model that here — Insert mode forwards keys to the textarea
// unchanged so its native cursor handling takes over.
func clampCol(line string, col int) int {
	max := runeLen(line) - 1
	if max < 0 {
		max = 0
	}
	if col > max {
		return max
	}
	if col < 0 {
		return 0
	}
	return col
}

func firstNonBlank(line string) int {
	for i, r := range []rune(line) {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
}

// insertAt inserts s at position i, growing the slice. Used for o/O which
// open a fresh line above or below the cursor.
func insertAt(ls []string, i int, s string) []string {
	if i < 0 {
		i = 0
	}
	if i > len(ls) {
		i = len(ls)
	}
	ls = append(ls, "")
	copy(ls[i+1:], ls[i:])
	ls[i] = s
	return ls
}
