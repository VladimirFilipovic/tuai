package ui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// renameDialogModel collects a new name for a session via a single-line
// textarea modal. Triggered from the command palette or the sessions list.
type renameDialogModel struct {
	ta          textarea.Model
	width       int
	height      int
	sessionID   string
	originalNm  string
}

type openRenameMsg struct {
	sessionID string
	current   string
}

type renameAppliedMsg struct {
	sessionID string
	name      string
}

type renameCanceledMsg struct{}

func newRenameDialog(sessionID, current string) renameDialogModel {
	ta := textarea.New()
	ta.Placeholder = "Session name…"
	ta.Focus()
	ta.CharLimit = 200
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetHeight(1)
	ta.MaxHeight = 1
	ta.SetWidth(60)
	// Single-line input: Enter submits, no newline insertion.
	ta.KeyMap.InsertNewline.SetKeys()
	ta.SetValue(current)
	ta.CursorEnd()
	styleTextarea(&ta)
	return renameDialogModel{
		ta:         ta,
		sessionID:  sessionID,
		originalNm: current,
	}
}

func (m renameDialogModel) Init() tea.Cmd {
	return func() tea.Msg { return textarea.Blink() }
}

func (m renameDialogModel) Update(msg tea.Msg) (renameDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.ta.Value())
			if name == "" {
				name = m.originalNm
			}
			id := m.sessionID
			return m, func() tea.Msg { return renameAppliedMsg{sessionID: id, name: name} }
		case "esc", "ctrl+c":
			return m, func() tea.Msg { return renameCanceledMsg{} }
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m renameDialogModel) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder

	chip := s.HeaderChip.Render("rename")
	sub := s.Subtle.Render("  session")
	b.WriteString(" " + chip + sub + "\n")
	b.WriteString(divider(m.width) + "\n\n")

	b.WriteString("  " + s.Subtle.Render("name") + "\n")

	input := renderInputArea(m.ta, m.width)
	b.WriteString(indentLines(input, "  ") + "\n\n")

	b.WriteString(divider(m.width) + "\n")
	b.WriteString(s.Help.Render("enter save • esc cancel"))

	return b.String()
}

func (m *renameDialogModel) setSize(w, h int) {
	m.width = w
	m.height = h
	innerWidth := w - 8
	if innerWidth < 20 {
		innerWidth = 20
	}
	m.ta.SetWidth(innerWidth)
}
