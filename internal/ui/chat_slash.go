package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/VladimirFilipovic/tuai/internal/storage"
	vimpkg "github.com/VladimirFilipovic/tuai/internal/vim"
)

// handleSlash parses /commands typed in the input. Returns the tea.Cmd to
// dispatch (which may emit a message that the parent App handles).
func (m *chatModel) handleSlash(input string) tea.Cmd {
	parts := strings.Fields(input)
	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/model":
		if len(args) == 0 {
			return func() tea.Msg { return openModelPickerMsg{} }
		}
		alias := args[0]
		if alias == "default" {
			alias = ""
		}
		m.client.SetModel(alias)
		cur, _ := storage.LoadConfig()
		cur.Model = alias
		_ = storage.SaveConfig(cur)
		m.notice = "model: " + m.client.Model()
		m.refreshViewport()
		return nil

	case "/theme":
		return func() tea.Msg { return openThemePickerMsg{} }

	case "/light", "/dark", "/auto":
		mode := strings.TrimPrefix(cmd, "/")
		SetAppearanceMode(mode)
		cur, _ := storage.LoadConfig()
		if mode == "auto" {
			cur.Appearance = ""
		} else {
			cur.Appearance = mode
		}
		_ = storage.SaveConfig(cur)
		if mode == "auto" {
			m.notice = "appearance: auto (follows terminal)"
		} else {
			m.notice = "appearance: " + mode
		}
		m.refreshViewport()
		return nil

	case "/new":
		if m.streaming {
			return nil
		}
		return func() tea.Msg { return newSessionMsg{} }

	case "/clear":
		if m.streaming {
			return nil
		}
		m.session.Messages = []storage.Message{}
		m.pending = ""
		m.err = nil
		_ = m.store.Save(m.session)
		m.refreshViewport()
		return nil

	case "/vim":
		// Toggle modal editing. Persist the new state so it sticks across
		// sessions; show a short notice so the user can see what changed
		// without having to remember which way the toggle flipped.
		cur, _ := storage.LoadConfig()
		if m.vim == nil {
			m.vim = vimpkg.New()
			cur.Vim = true
			m.notice = "vim mode on · esc → Normal, i → Insert"
		} else {
			m.vim = nil
			cur.Vim = false
			m.notice = "vim mode off"
		}
		_ = storage.SaveConfig(cur)
		m.refreshViewport()
		return nil

	case "/help":
		m.notice = "commands: /model [name] · /theme · /light · /dark · /auto · /vim · /new · /clear · /help"
		m.refreshViewport()
		return nil

	default:
		m.notice = "unknown command: " + cmd + " (try /help)"
		m.refreshViewport()
		return nil
	}
}
