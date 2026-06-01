package ui

import (
	"sort"

	"github.com/VladimirFilipovic/tuai/internal/storage"
)

// projectFilter scopes the sessions list to a single project. selected is
// "" (all projects), "." (a sentinel for the working directory captured at
// startup), or a literal project path. cwd is captured once at creation and
// used both as the "." target and as the front of the cycle order.
type projectFilter struct {
	selected string
	cwd      string
}

// active reports whether any project scope is in effect.
func (pf projectFilter) active() bool { return pf.selected != "" }

// clear drops back to "all projects".
func (pf *projectFilter) clear() { pf.selected = "" }

// resolved expands the "." sentinel to the captured cwd so a freshly-launched
// tui filters to "this project" by tab-toggle.
func (pf projectFilter) resolved() string {
	if pf.selected == "." {
		return pf.cwd
	}
	return pf.selected
}

// cycle walks "" → cwd → each other project seen in sessions → "".
func (pf *projectFilter) cycle(sessions []*storage.Session) {
	projects := pf.uniqueProjects(sessions)
	if len(projects) == 0 {
		return
	}

	current := pf.resolved()
	// Find current position in the ordered list. "" is index -1.
	pos := -1
	for i, p := range projects {
		if p == current {
			pos = i
			break
		}
	}
	next := pos + 1
	if next >= len(projects) {
		pf.selected = ""
		return
	}
	if projects[next] == pf.cwd {
		pf.selected = "."
	} else {
		pf.selected = projects[next]
	}
}

// uniqueProjects returns each non-empty project path that appears in the given
// sessions, with cwd pushed to the front when present. Stable order (cwd
// first, then alphabetical for the rest).
func (pf projectFilter) uniqueProjects(sessions []*storage.Session) []string {
	seen := map[string]bool{}
	var rest []string
	hasCwd := false
	for _, sess := range sessions {
		if sess.Project == "" || seen[sess.Project] {
			continue
		}
		seen[sess.Project] = true
		if sess.Project == pf.cwd {
			hasCwd = true
			continue
		}
		rest = append(rest, sess.Project)
	}
	sort.Strings(rest)
	if hasCwd {
		return append([]string{pf.cwd}, rest...)
	}
	return rest
}
