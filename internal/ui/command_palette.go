package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// commandID identifies which action a palette entry triggers. The app
// translates the picked command into the corresponding message.
type commandID int

const (
	cmdChangeTheme commandID = iota
	cmdChangeModel
	cmdToggleAppearance
	cmdRenameSession
	cmdNewSession
	cmdClearSession
	cmdBackToSessions
)

type paletteEntry struct {
	id          commandID
	label       string
	description string
	chatOnly    bool // hide when the palette is opened from the sessions list
}

var paletteEntries = []paletteEntry{
	{id: cmdChangeTheme, label: "Change theme", description: "Pick a color theme."},
	{id: cmdChangeModel, label: "Change model", description: "Switch the Claude model."},
	{id: cmdToggleAppearance, label: "Toggle appearance", description: "Cycle light / dark / auto."},
	{id: cmdRenameSession, label: "Rename session", description: "Edit the current session's name."},
	{id: cmdNewSession, label: "New session", description: "Start a fresh conversation."},
	{id: cmdClearSession, label: "Clear session", description: "Remove all messages in this session.", chatOnly: true},
	{id: cmdBackToSessions, label: "Back to sessions", description: "Return to the sessions list.", chatOnly: true},
}

type commandPaletteModel struct {
	cursor   int
	width    int
	height   int
	entries  []paletteEntry
	fromChat bool
}

type openCommandPaletteMsg struct{}
type commandPalettePickedMsg struct{ id commandID }
type commandPaletteCanceledMsg struct{}

func newCommandPalette(fromChat bool) commandPaletteModel {
	var entries []paletteEntry
	for _, e := range paletteEntries {
		if e.chatOnly && !fromChat {
			continue
		}
		entries = append(entries, e)
	}
	return commandPaletteModel{entries: entries, fromChat: fromChat}
}

func (m commandPaletteModel) Init() tea.Cmd { return nil }

func (m commandPaletteModel) Update(msg tea.Msg) (commandPaletteModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Toggle-close on the same shortcut that opens the palette. See
		// openPaletteKey for why we check Keystroke() rather than String().
		if openPaletteKey(msg) {
			return m, func() tea.Msg { return commandPaletteCanceledMsg{} }
		}
		switch msg.String() {
		case "j", "down":
			m.cursor = (m.cursor + 1) % len(m.entries)
		case "k", "up":
			m.cursor = (m.cursor - 1 + len(m.entries)) % len(m.entries)
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = len(m.entries) - 1
		case "enter":
			id := m.entries[m.cursor].id
			return m, func() tea.Msg { return commandPalettePickedMsg{id: id} }
		case "esc", "ctrl+c", "q":
			return m, func() tea.Msg { return commandPaletteCanceledMsg{} }
		}
	}
	return m, nil
}

func (m commandPaletteModel) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder

	chip := s.HeaderChip.Render("command")
	sub := s.Subtle.Render("  palette")
	b.WriteString(" " + chip + sub + "\n")
	b.WriteString(divider(m.width) + "\n\n")

	for i, e := range m.entries {
		marker := "  "
		nameStyle := s.SessionNormal
		if i == m.cursor {
			marker = "▶ "
			nameStyle = s.SessionSelected
		}
		name := nameStyle.Render(marker + e.label)
		desc := s.SessionMeta.Render("  " + e.description)
		b.WriteString(name + desc + "\n")
	}

	b.WriteString("\n" + divider(m.width) + "\n")
	b.WriteString(s.Help.Render("↑/↓ navigate • enter run • esc cancel"))
	return b.String()
}

func (m *commandPaletteModel) setSize(w, h int) {
	m.width = w
	m.height = h
}
