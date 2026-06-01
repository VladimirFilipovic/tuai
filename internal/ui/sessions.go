package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/VladimirFilipovic/tuai/internal/fuzzy"
	"github.com/VladimirFilipovic/tuai/internal/storage"
)

type sessionsModel struct {
	sessions []*storage.Session
	// filtered indexes into sessions after applying the project filter and
	// fuzzy-match on filter text. cursor is an index into filtered.
	filtered []int
	cursor   int
	store    *storage.Store
	width    int
	height   int
	err      error

	// filter is the search box. filterActive routes key events to it; when
	// inactive the user navigates the list normally.
	filter       textinput.Model
	filterActive bool

	// project scopes the list to one project (tab cycles through them).
	project projectFilter

	// lastFilterNeedle remembers the filter text the last recomputeFiltered
	// ran with. When it changes (user types or clears the query) the cursor
	// resets to 0 so the highlight tracks the new top result, not the index
	// it happened to be parked at.
	lastFilterNeedle string
}

type sessionsLoadedMsg struct {
	sessions []*storage.Session
	err      error
}

type openSessionMsg struct{ session *storage.Session }
type newSessionMsg struct{}
type deleteSessionMsg struct{ id string }

func newSessionsModel(store *storage.Store) sessionsModel {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search…"
	ti.Prompt = ""
	ti.SetWidth(40)
	cwd, _ := os.Getwd()
	return sessionsModel{
		store:   store,
		filter:  ti,
		project: projectFilter{cwd: cwd},
	}
}

func loadSessions(store *storage.Store) tea.Cmd {
	return func() tea.Msg {
		sessions, err := store.List()
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

func (m sessionsModel) Init() tea.Cmd {
	return loadSessions(m.store)
}

func (m sessionsModel) Update(msg tea.Msg) (sessionsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		m.err = msg.err
		m.sessions = msg.sessions
		m.recomputeFiltered()

	case tea.KeyPressMsg:
		// While the filter input is focused, route most keys to it. Only
		// a few control keys (enter, esc, tab, up/down) keep their list
		// semantics so the user can navigate without leaving the field.
		if m.filterActive {
			switch msg.String() {
			case "esc":
				m.filterActive = false
				m.filter.Blur()
				return m, nil
			case "enter":
				if len(m.filtered) > 0 {
					sess := m.sessions[m.filtered[m.cursor]]
					return m, func() tea.Msg { return openSessionMsg{session: sess} }
				}
				return m, nil
			case "tab":
				m.project.cycle(m.sessions)
				m.recomputeFiltered()
				return m, nil
			case "down", "ctrl+n":
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
				}
				return m, nil
			case "up", "ctrl+p":
				if m.cursor > 0 {
					m.cursor--
				}
				return m, nil
			case "home":
				m.cursor = 0
				return m, nil
			case "end":
				m.cursor = max(0, len(m.filtered)-1)
				return m, nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.recomputeFiltered()
			return m, cmd
		}

		switch msg.String() {
		case "/":
			m.filterActive = true
			return m, m.filter.Focus()
		case "tab":
			m.project.cycle(m.sessions)
			m.recomputeFiltered()
		case "j", "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = max(0, len(m.filtered)-1)
		case "enter":
			if len(m.filtered) > 0 {
				sess := m.sessions[m.filtered[m.cursor]]
				return m, func() tea.Msg { return openSessionMsg{session: sess} }
			}
		case "n":
			return m, func() tea.Msg { return newSessionMsg{} }
		case "d", "x":
			if len(m.filtered) > 0 {
				id := m.sessions[m.filtered[m.cursor]].ID
				return m, func() tea.Msg { return deleteSessionMsg{id: id} }
			}
		case "r":
			return m, loadSessions(m.store)
		case "e":
			if len(m.filtered) > 0 {
				sess := m.sessions[m.filtered[m.cursor]]
				id := sess.ID
				name := sess.Name
				return m, func() tea.Msg { return openRenameMsg{sessionID: id, current: name} }
			}
		case "esc":
			// Clear an active filter when the user backs out of the list.
			if m.filter.Value() != "" || m.project.active() {
				m.filter.Reset()
				m.project.clear()
				m.recomputeFiltered()
			}
		}
	}
	return m, nil
}

func (m sessionsModel) View() string {
	if m.width == 0 {
		return ""
	}

	var b strings.Builder

	title := s.TitleBar.Render("  claude")
	subtitle := s.Subtle.Render(" sessions")
	b.WriteString(title + subtitle + "\n")
	b.WriteString(divider(m.width) + "\n")

	// Filter bar: project chip + search field. Always visible so the
	// keybindings (/, tab) are discoverable.
	chipLabel := "project: all"
	if m.project.active() {
		chipLabel = "project: " + shortenProject(m.project.resolved())
	}
	chip := s.HeaderChip.Render(chipLabel)

	searchPrefix := s.Subtle.Render("  /")
	searchField := m.filter.View()
	if !m.filterActive && m.filter.Value() == "" {
		searchField = s.Subtle.Render("search (press /)")
	}
	b.WriteString(" " + chip + searchPrefix + " " + searchField + "\n")
	b.WriteString(divider(m.width) + "\n\n")

	if m.err != nil {
		b.WriteString(s.Error.Render("error: "+m.err.Error()) + "\n")
	}

	if len(m.sessions) == 0 {
		empty := s.Subtle.Render("  No sessions yet. Press n to start a new one.")
		b.WriteString(empty + "\n")
	} else if len(m.filtered) == 0 {
		empty := s.Subtle.Render("  No sessions match.")
		b.WriteString(empty + "\n")
	} else {
		maxVisible := max(m.height-10, 1)
		start := 0
		if m.cursor >= maxVisible {
			start = m.cursor - maxVisible + 1
		}

		for i := start; i < len(m.filtered) && i < start+maxVisible; i++ {
			sess := m.sessions[m.filtered[i]]
			name := sess.Name
			meta := fmt.Sprintf("%s  %s  %d msgs",
				shortenProject(sess.Project),
				relTime(sess.UpdatedAt),
				len(sess.Messages))

			nameWidth := m.width - lipgloss.Width(meta) - 8
			if nameWidth > 0 {
				name = truncateLine(name, nameWidth)
			}

			padding := ""
			nameCells := lipgloss.Width(name)
			if nameWidth > nameCells {
				padding = strings.Repeat(" ", nameWidth-nameCells)
			}

			metaStr := s.SessionMeta.Render(meta)

			if i == m.cursor {
				row := s.SessionSelected.Render("▶ "+name) + padding + "  " + metaStr
				b.WriteString(row + "\n")
			} else {
				row := s.SessionNormal.Render(name) + padding + "  " + metaStr
				b.WriteString(row + "\n")
			}
		}
	}

	b.WriteString("\n" + divider(m.width) + "\n")
	var help string
	if m.filterActive {
		help = "type to filter • tab project • ↑/↓ move • enter open • esc done"
	} else {
		help = "/ search • tab project • enter open • n new • e rename • d delete • r refresh • ctrl+p palette • q quit"
	}
	b.WriteString(s.Help.Render(help))

	return b.String()
}

func (m *sessionsModel) setSize(w, h int) {
	m.width = w
	m.height = h
	m.filter.SetWidth(max(w-20, 10))
}

// recomputeFiltered rebuilds the filtered index slice based on the current
// project filter and fuzzy search text. Cursor resets to 0 when the search
// needle changes (so the highlight tracks the new top match rather than
// staying parked on the previous numeric index); a pure list refresh (same
// needle, e.g. after sessionsLoadedMsg) keeps the cursor and only clamps it.
func (m *sessionsModel) recomputeFiltered() {
	needle := strings.TrimSpace(m.filter.Value())
	target := m.project.resolved()
	needleChanged := needle != m.lastFilterNeedle
	m.lastFilterNeedle = needle

	type scored struct {
		idx   int
		score int
	}
	var matched []scored
	for i, sess := range m.sessions {
		if target != "" && sess.Project != target {
			continue
		}
		score, ok := fuzzy.Score(sess.Name, needle)
		if !ok {
			continue
		}
		matched = append(matched, scored{idx: i, score: score})
	}

	// With a search needle, rank by score (higher = better). Without one
	// keep the store's recency order.
	if needle != "" {
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].score > matched[j].score
		})
	}

	m.filtered = m.filtered[:0]
	for _, x := range matched {
		m.filtered = append(m.filtered, x.idx)
	}
	if needleChanged {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

// shortenProject renders a project path as just the trailing directory name,
// since the full path is usually long and only the project name carries info.
func shortenProject(p string) string {
	if p == "" {
		return "—"
	}
	return filepath.Base(p)
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2")
	}
}
