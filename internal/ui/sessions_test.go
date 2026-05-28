package ui

import (
	"testing"
	"time"

	"github.com/VladimirFilipovic/tuai/internal/storage"
)

func TestFuzzyScoreMatches(t *testing.T) {
	cases := []struct {
		hay, needle string
		match       bool
	}{
		{"hello world", "hw", true},
		{"hello world", "hlo", true},
		{"hello world", "xyz", false},
		{"hello world", "", true},
		{"Foo Bar", "fb", true},        // case-insensitive
		{"render-cache fix", "rcf", true},
	}
	for _, c := range cases {
		_, ok := fuzzyScore(c.hay, c.needle)
		if ok != c.match {
			t.Errorf("fuzzyScore(%q,%q) match=%v want %v", c.hay, c.needle, ok, c.match)
		}
	}
}

func TestFuzzyScoreRanksContiguousHigher(t *testing.T) {
	// "fb" should score higher in "foo-bar" (word-boundary match on 'b')
	// than in "fxxxxxxxxxxxxxb" (long sparse match).
	good, ok1 := fuzzyScore("foo-bar", "fb")
	bad, ok2 := fuzzyScore("fxxxxxxxxxxxxxb", "fb")
	if !ok1 || !ok2 {
		t.Fatalf("both should match: %v %v", ok1, ok2)
	}
	if good <= bad {
		t.Errorf("word-boundary match (%d) should score higher than sparse (%d)", good, bad)
	}
}

func TestRecomputeFilteredAppliesProjectAndSearch(t *testing.T) {
	now := time.Now()
	m := sessionsModel{
		sessions: []*storage.Session{
			{ID: "1", Name: "alpha", Project: "/p/foo", UpdatedAt: now},
			{ID: "2", Name: "beta", Project: "/p/bar", UpdatedAt: now},
			{ID: "3", Name: "alpaca", Project: "/p/foo", UpdatedAt: now},
		},
	}
	m.recomputeFiltered()
	if len(m.filtered) != 3 {
		t.Fatalf("no filter: want 3 got %d", len(m.filtered))
	}

	m.projectFilter = "/p/foo"
	m.recomputeFiltered()
	if len(m.filtered) != 2 {
		t.Fatalf("project filter: want 2 got %d", len(m.filtered))
	}

	m.filter.SetValue("alp")
	m.recomputeFiltered()
	if len(m.filtered) != 2 {
		t.Fatalf("project+search: want 2 got %d", len(m.filtered))
	}

	m.filter.SetValue("zzz")
	m.recomputeFiltered()
	if len(m.filtered) != 0 {
		t.Fatalf("no-match search: want 0 got %d", len(m.filtered))
	}
	if m.cursor != 0 {
		t.Errorf("cursor should clamp to 0 when filtered is empty, got %d", m.cursor)
	}
}
