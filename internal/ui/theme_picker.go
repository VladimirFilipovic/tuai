package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

type themePickerModel struct {
	cursor    int
	width     int
	height    int
	prevTheme string // restored on cancel
}

type themePickedMsg struct{ name string }
type themeCanceledMsg struct{}

func newThemePicker() themePickerModel {
	cur := CurrentTheme().Name
	cursor := 0
	for i, t := range Themes {
		if t.Name == cur {
			cursor = i
			break
		}
	}
	return themePickerModel{cursor: cursor, prevTheme: cur}
}

func (m themePickerModel) Init() tea.Cmd { return nil }

func (m themePickerModel) Update(msg tea.Msg) (themePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			m.cursor = (m.cursor + 1) % len(Themes)
			applyTheme(Themes[m.cursor].Name) // live preview
		case "k", "up":
			m.cursor = (m.cursor - 1 + len(Themes)) % len(Themes)
			applyTheme(Themes[m.cursor].Name)
		case "g", "home":
			m.cursor = 0
			applyTheme(Themes[m.cursor].Name)
		case "G", "end":
			m.cursor = len(Themes) - 1
			applyTheme(Themes[m.cursor].Name)
		case "enter":
			return m, func() tea.Msg { return themePickedMsg{name: Themes[m.cursor].Name} }
		case "esc", "ctrl+c", "q":
			applyTheme(m.prevTheme)
			return m, func() tea.Msg { return themeCanceledMsg{} }
		}
	}
	return m, nil
}

func (m themePickerModel) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder

	title := s.TitleBar.Render("  theme")
	sub := s.Subtle.Render(" picker · live preview")
	b.WriteString(title + sub + "\n")
	b.WriteString(divider(m.width) + "\n\n")

	maxVisible := m.height - 12
	if maxVisible < 1 {
		maxVisible = 1
	}
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}

	for i := start; i < len(Themes) && i < start+maxVisible; i++ {
		t := Themes[i]
		marker := "  "
		nameStyle := s.SessionNormal
		if i == m.cursor {
			marker = "▶ "
			nameStyle = s.SessionSelected
		}
		// Color chip preview of accent/user/assistant
		chip := func(c lipgloss.Style) string { return c.Render("●") }
		chips := chip(lipgloss.NewStyle().Foreground(t.Accent())) +
			chip(lipgloss.NewStyle().Foreground(t.User())) +
			chip(lipgloss.NewStyle().Foreground(t.Assistant())) +
			chip(lipgloss.NewStyle().Foreground(t.Border())) +
			chip(lipgloss.NewStyle().Foreground(t.Error()))

		name := nameStyle.Render(marker + t.Name)
		desc := s.SessionMeta.Render("  " + t.Description)
		b.WriteString(name + "  " + chips + desc + "\n")
	}

	b.WriteString("\n" + divider(m.width) + "\n")

	// Sample preview pane
	b.WriteString(themeSamplePreview(m.width))

	b.WriteString("\n" + divider(m.width) + "\n")
	b.WriteString(s.Help.Render("↑/↓ navigate • enter apply • esc cancel"))

	return b.String()
}

func themeSamplePreview(width int) string {
	wrap := width - 4
	if wrap < 20 {
		wrap = 20
	}
	sample := "```go\n" +
		"func greet(name string) string {\n" +
		"    // a quick highlight sample\n" +
		"    return fmt.Sprintf(\"hello, %s!\", name)\n" +
		"}\n" +
		"```\n" +
		"_inline `code` and **bold** prose._"
	return renderMarkdown(sample, wrap)
}

func (m *themePickerModel) setSize(w, h int) {
	m.width = w
	m.height = h
}
