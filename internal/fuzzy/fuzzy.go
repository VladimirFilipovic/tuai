// Package fuzzy implements a small fuzzy-match scorer used by the session
// list and other pickers. The algorithm walks the haystack in order looking
// for each needle rune, awarding bonuses for adjacency and word boundaries
// so a tight prefix match scores higher than a sparse one.
package fuzzy

import (
	"strings"
	"unicode"
)

// Score returns (score, true) if every rune of needle appears in haystack
// in order (case-insensitive). Higher scores indicate better matches:
// adjacent and word-boundary matches each contribute a bonus, and shorter
// haystacks get a small signal-to-noise boost.
//
// An empty needle matches anything with score 0 — handy as the "no filter"
// case.
func Score(haystack, needle string) (int, bool) {
	if needle == "" {
		return 0, true
	}
	hs := []rune(strings.ToLower(haystack))
	ns := []rune(strings.ToLower(needle))

	score := 0
	prevMatch := -2
	hi := 0
	for _, nr := range ns {
		found := -1
		for ; hi < len(hs); hi++ {
			if hs[hi] == nr {
				found = hi
				break
			}
		}
		if found == -1 {
			return 0, false
		}
		// Bonuses: adjacent to previous match, or at a word boundary.
		if found == prevMatch+1 {
			score += 5
		} else {
			score += 1
		}
		if found == 0 || isBoundary(hs, found) {
			score += 3
		}
		prevMatch = found
		hi++
	}
	// Slight bonus for shorter haystacks (better signal/noise).
	if len(hs) < 20 {
		score += 20 - len(hs)
	}
	return score, true
}

func isBoundary(rs []rune, i int) bool {
	if i <= 0 {
		return true
	}
	prev := rs[i-1]
	return !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
}
