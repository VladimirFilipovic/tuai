package vim

import "unicode"

// Word semantics here follow vim's "word" (lowercase w): a maximal run of
// "word characters" (letters/digits/_) OR a maximal run of punctuation,
// separated by whitespace. We don't implement WORD (uppercase, whitespace-
// separated only) — it's the same code with a simpler predicate; trivial to
// add later if anyone misses it.

type charClass int

const (
	classSpace charClass = iota
	classWord
	classPunct
)

func classify(r rune) charClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r), unicode.IsDigit(r), r == '_':
		return classWord
	default:
		return classPunct
	}
}

// nextWordStart returns the position of the next word start from (row, col).
// Walks forward across the joined buffer treating \n as whitespace, so "w"
// at end-of-line correctly jumps to the first word on the next line.
func nextWordStart(lines []string, row, col int) (int, int) {
	r, c := row, col
	curRunes := []rune(lines[r])

	// 1. Skip past the rest of the current "thing" (word or punct run).
	if c < len(curRunes) {
		cls := classify(curRunes[c])
		if cls != classSpace {
			for c < len(curRunes) && classify(curRunes[c]) == cls {
				c++
			}
		}
	}
	// 2. Skip whitespace, wrapping across newlines.
	for {
		if c >= len(curRunes) {
			if r >= len(lines)-1 {
				// End of buffer — park on the last char.
				if len(curRunes) > 0 {
					return r, len(curRunes) - 1
				}
				return r, 0
			}
			r++
			c = 0
			curRunes = []rune(lines[r])
			// vim treats an empty line as a word boundary — landing on it
			// stops the motion, even if the next line has more content.
			if len(curRunes) == 0 {
				return r, 0
			}
			continue
		}
		if classify(curRunes[c]) != classSpace {
			return r, c
		}
		c++
	}
}

// prevWordStart walks backwards. Symmetric to nextWordStart but lands on the
// *start* of the previous word — not the end — which is what `b` does.
func prevWordStart(lines []string, row, col int) (int, int) {
	r, c := row, col
	// Step one cell back to begin with; otherwise repeated `b` from the same
	// position would never make progress.
	if c == 0 {
		if r == 0 {
			return 0, 0
		}
		r--
		c = len([]rune(lines[r]))
	} else {
		c--
	}
	curRunes := []rune(lines[r])

	// Skip backwards across whitespace and empty lines.
	for {
		if c < 0 || len(curRunes) == 0 {
			if r == 0 {
				return 0, 0
			}
			r--
			curRunes = []rune(lines[r])
			c = len(curRunes) - 1
			continue
		}
		if classify(curRunes[c]) != classSpace {
			break
		}
		c--
	}
	if c < 0 {
		return r, 0
	}
	// Now we're sitting on a non-space char — walk back to the run's start.
	cls := classify(curRunes[c])
	for c > 0 && classify(curRunes[c-1]) == cls {
		c--
	}
	return r, c
}

// wordEnd walks forward to the *end* of the current (or next, if already
// on a boundary) word. Mirrors vim's `e`.
func wordEnd(lines []string, row, col int) (int, int) {
	r, c := row, col
	curRunes := []rune(lines[r])

	// If the next cell would still be in the same class, stay; otherwise
	// step forward into the next word so we don't sit still on repeated e.
	advance := func() bool {
		if c+1 < len(curRunes) {
			c++
			return true
		}
		if r < len(lines)-1 {
			r++
			c = 0
			curRunes = []rune(lines[r])
			return true
		}
		return false
	}

	// Skip if we're already at end-of-word (the next cell is a different
	// class or doesn't exist) so a fresh `e` makes progress.
	if c < len(curRunes) {
		cur := classify(curRunes[c])
		if cur == classSpace || c+1 >= len(curRunes) || classify(curRunes[c+1]) != cur {
			if !advance() {
				return r, c
			}
		}
	} else if !advance() {
		return r, c
	}

	// Skip whitespace to land on a word.
	for c >= len(curRunes) || classify(curRunes[c]) == classSpace {
		if !advance() {
			if len(curRunes) > 0 {
				return r, len(curRunes) - 1
			}
			return r, 0
		}
	}
	// Walk to end of this word.
	cls := classify(curRunes[c])
	for c+1 < len(curRunes) && classify(curRunes[c+1]) == cls {
		c++
	}
	return r, c
}
