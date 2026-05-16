package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/vladafilipovic/claudetui/internal/storage"
)

type sessionsModel struct {
	sessions []*storage.Session
	cursor   int
	store    *storage.Store
	width    int
	height   int
	err      error
}

type sessionsLoadedMsg struct {
	sessions []*storage.Session
	err      error
}

type openSessionMsg struct{ session *storage.Session }
type newSessionMsg struct{}
type deleteSessionMsg struct{ id string }

func newSessionsModel(store *storage.Store) sessionsModel {
	return sessionsModel{store: store}
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
		if m.cursor >= len(m.sessions) {
			m.cursor = max(0, len(m.sessions)-1)
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = max(0, len(m.sessions)-1)
		case "enter":
			if len(m.sessions) > 0 {
				sess := m.sessions[m.cursor]
				return m, func() tea.Msg { return openSessionMsg{session: sess} }
			}
		case "n":
			return m, func() tea.Msg { return newSessionMsg{} }
		case "d", "x":
			if len(m.sessions) > 0 {
				id := m.sessions[m.cursor].ID
				return m, func() tea.Msg { return deleteSessionMsg{id: id} }
			}
		case "r":
			return m, loadSessions(m.store)
		case "e":
			if len(m.sessions) > 0 {
				sess := m.sessions[m.cursor]
				id := sess.ID
				name := sess.Name
				return m, func() tea.Msg { return openRenameMsg{sessionID: id, current: name} }
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
	b.WriteString(divider(m.width) + "\n\n")

	if m.err != nil {
		b.WriteString(s.Error.Render("error: "+m.err.Error()) + "\n")
	}

	if len(m.sessions) == 0 {
		empty := s.Subtle.Render("  No sessions yet. Press n to start a new one.")
		b.WriteString(empty + "\n")
	} else {
		maxVisible := m.height - 8
		if maxVisible < 1 {
			maxVisible = 1
		}
		start := 0
		if m.cursor >= maxVisible {
			start = m.cursor - maxVisible + 1
		}

		for i := start; i < len(m.sessions) && i < start+maxVisible; i++ {
			sess := m.sessions[i]
			name := sess.Name
			meta := fmt.Sprintf("%s  %d msgs", relTime(sess.UpdatedAt), len(sess.Messages))

			nameWidth := m.width - len(meta) - 8
			if nameWidth > 0 && len(name) > nameWidth {
				name = name[:nameWidth-1] + "…"
			}

			padding := ""
			if nameWidth > len(name) {
				padding = strings.Repeat(" ", nameWidth-len(name))
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
	help := s.Help.Render("enter open • n new • e rename • d delete • r refresh • ctrl+p palette • g/G top/bottom • q quit")
	b.WriteString(help)

	return b.String()
}

func (m *sessionsModel) setSize(w, h int) {
	m.width = w
	m.height = h
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
