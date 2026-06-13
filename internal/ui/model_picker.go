package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

type modelOption struct {
	Alias       string // value passed to claude --model; "" = let claude pick
	Label       string
	Description string
}

var Models = []modelOption{
	{Alias: "", Label: "default", Description: "Let Claude Code decide."},
	{Alias: "fable", Label: "fable", Description: "Most capable (Fable 5)."},
	{Alias: "opus", Label: "opus", Description: "Highly capable (Opus 4.x)."},
	{Alias: "sonnet", Label: "sonnet", Description: "Smart + fast (Sonnet 4.x)."},
	{Alias: "haiku", Label: "haiku", Description: "Fastest (Haiku 4.x)."},
	{Alias: "claude-fable-5", Label: "claude-fable-5", Description: "Fable 5 (full id)."},
	{Alias: "claude-opus-4-7", Label: "claude-opus-4-7", Description: "Opus 4.7 (full id)."},
	{Alias: "claude-sonnet-4-6", Label: "claude-sonnet-4-6", Description: "Sonnet 4.6 (full id)."},
	{Alias: "claude-haiku-4-5-20251001", Label: "claude-haiku-4-5", Description: "Haiku 4.5 (full id)."},
}

type modelPickerModel struct {
	cursor   int
	width    int
	height   int
	previous string // value to restore on cancel
}

type modelPickedMsg struct{ alias string }
type modelCanceledMsg struct{}

func newModelPicker(currentAlias string) modelPickerModel {
	cursor := 0
	for i, m := range Models {
		if m.Alias == currentAlias {
			cursor = i
			break
		}
	}
	return modelPickerModel{cursor: cursor, previous: currentAlias}
}

func (m modelPickerModel) Init() tea.Cmd { return nil }

func (m modelPickerModel) Update(msg tea.Msg) (modelPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			m.cursor = (m.cursor + 1) % len(Models)
		case "k", "up":
			m.cursor = (m.cursor - 1 + len(Models)) % len(Models)
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = len(Models) - 1
		case "enter":
			return m, func() tea.Msg { return modelPickedMsg{alias: Models[m.cursor].Alias} }
		case "esc", "ctrl+c", "q":
			return m, func() tea.Msg { return modelCanceledMsg{} }
		}
	}
	return m, nil
}

func (m modelPickerModel) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder

	chip := s.HeaderChip.Render("model")
	sub := s.Subtle.Render("  pick a model")
	b.WriteString(" " + chip + sub + "\n")
	b.WriteString(divider(m.width) + "\n\n")

	for i, opt := range Models {
		marker := "  "
		nameStyle := s.SessionNormal
		if i == m.cursor {
			marker = "▶ "
			nameStyle = s.SessionSelected
		}
		name := nameStyle.Render(marker + opt.Label)
		desc := s.SessionMeta.Render("  " + opt.Description)
		b.WriteString(name + desc + "\n")
	}

	b.WriteString("\n" + divider(m.width) + "\n")
	b.WriteString(s.Help.Render("↑/↓ navigate • enter apply • esc cancel"))
	return b.String()
}

func (m *modelPickerModel) setSize(w, h int) {
	m.width = w
	m.height = h
}
