package ui

import (
	"testing"
	"time"

	"github.com/VladimirFilipovic/tuai/internal/storage"
)

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

func TestRecomputeFilteredResetsCursorOnNeedleChange(t *testing.T) {
	now := time.Now()
	m := sessionsModel{
		sessions: []*storage.Session{
			{ID: "1", Name: "alpha", UpdatedAt: now},
			{ID: "2", Name: "beta", UpdatedAt: now},
			{ID: "3", Name: "gamma", UpdatedAt: now},
			{ID: "4", Name: "delta", UpdatedAt: now},
			{ID: "5", Name: "epsilon", UpdatedAt: now},
		},
	}
	m.recomputeFiltered()
	// Scroll down to index 3.
	m.cursor = 3

	// Type a query that still matches enough items to keep index 3 in range.
	m.filter.SetValue("a")
	m.recomputeFiltered()
	if m.cursor != 0 {
		t.Errorf("cursor should reset to 0 when filter text changes; got %d", m.cursor)
	}

	// Sanity: a recompute with same needle (e.g. loadSessions refresh) keeps cursor.
	m.cursor = 1
	m.recomputeFiltered()
	if m.cursor != 1 {
		t.Errorf("same-needle recompute should preserve cursor; got %d (want 1)", m.cursor)
	}
}
