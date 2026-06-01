package ui

import (
	"testing"

	"github.com/VladimirFilipovic/tuai/internal/storage"
)

func TestProjectFilterResolvedExpandsSentinel(t *testing.T) {
	pf := projectFilter{selected: ".", cwd: "/work/here"}
	if got := pf.resolved(); got != "/work/here" {
		t.Errorf("resolved(\".\") = %q, want /work/here", got)
	}
	pf.selected = "/other"
	if got := pf.resolved(); got != "/other" {
		t.Errorf("resolved literal = %q, want /other", got)
	}
	pf.selected = ""
	if pf.active() {
		t.Error("empty selection should be inactive")
	}
}

func TestProjectFilterUniqueProjectsCwdFirst(t *testing.T) {
	sessions := []*storage.Session{
		{Project: "/p/zeta"},
		{Project: "/work/here"},
		{Project: "/p/alpha"},
		{Project: "/work/here"}, // dup
		{Project: ""},           // skipped
	}
	pf := projectFilter{cwd: "/work/here"}
	got := pf.uniqueProjects(sessions)
	want := []string{"/work/here", "/p/alpha", "/p/zeta"}
	if len(got) != len(want) {
		t.Fatalf("uniqueProjects = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniqueProjects[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProjectFilterCycle(t *testing.T) {
	sessions := []*storage.Session{
		{Project: "/work/here"},
		{Project: "/p/alpha"},
	}
	pf := projectFilter{cwd: "/work/here"}

	// "" → cwd (stored as the "." sentinel) → other → back to "".
	pf.cycle(sessions)
	if pf.selected != "." {
		t.Fatalf("first cycle: selected = %q, want \".\"", pf.selected)
	}
	pf.cycle(sessions)
	if pf.selected != "/p/alpha" {
		t.Fatalf("second cycle: selected = %q, want /p/alpha", pf.selected)
	}
	pf.cycle(sessions)
	if pf.selected != "" {
		t.Fatalf("third cycle: selected = %q, want \"\" (all)", pf.selected)
	}
}

func TestProjectFilterCycleNoProjectsIsNoop(t *testing.T) {
	pf := projectFilter{cwd: "/work/here"}
	pf.cycle(nil)
	if pf.selected != "" {
		t.Errorf("cycle over no sessions changed selection to %q", pf.selected)
	}
}
