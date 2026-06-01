package fuzzy

import "testing"

func TestScoreMatches(t *testing.T) {
	cases := []struct {
		hay, needle string
		match       bool
	}{
		{"hello world", "hw", true},
		{"hello world", "hlo", true},
		{"hello world", "xyz", false},
		{"hello world", "", true},
		{"Foo Bar", "fb", true}, // case-insensitive
		{"render-cache fix", "rcf", true},
	}
	for _, c := range cases {
		_, ok := Score(c.hay, c.needle)
		if ok != c.match {
			t.Errorf("Score(%q,%q) match=%v want %v", c.hay, c.needle, ok, c.match)
		}
	}
}

func TestScoreRanksContiguousHigher(t *testing.T) {
	// "fb" should score higher in "foo-bar" (word-boundary match on 'b')
	// than in "fxxxxxxxxxxxxxb" (long sparse match).
	good, ok1 := Score("foo-bar", "fb")
	bad, ok2 := Score("fxxxxxxxxxxxxxb", "fb")
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if good <= bad {
		t.Errorf("word-boundary match (%d) should score higher than sparse (%d)", good, bad)
	}
}

func TestScoreEmptyHaystack(t *testing.T) {
	if _, ok := Score("", "x"); ok {
		t.Error("non-empty needle on empty haystack must not match")
	}
	if _, ok := Score("", ""); !ok {
		t.Error("empty needle on empty haystack should match (no filter)")
	}
}

func TestIsBoundaryEdgeCases(t *testing.T) {
	// Position 0 is always a boundary.
	if !isBoundary([]rune("anything"), 0) {
		t.Error("position 0 should be a boundary")
	}
	// Letter→letter is not a boundary.
	if isBoundary([]rune("abc"), 1) {
		t.Error("letter→letter is not a boundary")
	}
	// Punctuation→letter is a boundary.
	if !isBoundary([]rune("a-b"), 2) {
		t.Error("punct→letter should be a boundary")
	}
}
