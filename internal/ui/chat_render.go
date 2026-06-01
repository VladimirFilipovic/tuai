package ui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/VladimirFilipovic/tuai/internal/claude"
	"github.com/VladimirFilipovic/tuai/internal/storage"
)

const viewportBottomPad = 1

// streamStatus describes what Claude is currently doing — used as the label
// next to the spinner so the user sees more than just an opaque ticking dot.
func (m *chatModel) streamStatus() string {
	switch {
	case len(m.tools) > 0:
		last := m.tools[len(m.tools)-1]
		switch strings.ToLower(last.name) {
		case "read":
			return "reading file…"
		case "edit", "multiedit":
			return "editing file…"
		case "write":
			return "writing file…"
		case "bash":
			return "running shell…"
		case "grep":
			return "searching…"
		case "glob":
			return "listing files…"
		case "webfetch", "websearch":
			return "browsing the web…"
		case "task", "agent":
			return "running subagent…"
		default:
			return "running " + last.name + "…"
		}
	case m.thinking != "":
		return "thinking…"
	case m.pending == "":
		return "waiting on Claude…"
	default:
		return "writing…"
	}
}

func (m *chatModel) renderMessages() string {
	if m.width == 0 {
		return ""
	}
	wrap := max(m.width-4, 20)
	// Inner width inside the bubble: margin-left (2) + left border (1) + horizontal padding (4).
	bubbleInner := max(wrap-7, 12)

	var b strings.Builder

	if len(m.session.Messages) == 0 && !m.streaming && m.pending == "" {
		b.WriteString("\n  " + s.Subtle.Render("Ask anything. Code, design, debugging, whatever you need.") + "\n")
	}

	// Persisted messages render through a memo: identical between ticks/chunks
	// until something appends or the layout/theme changes.
	static := m.renderStaticMessages(wrap, bubbleInner)
	b.WriteString(static)
	// A blank separator divides the last persisted message from the live tail.
	if static != "" && (m.streaming || m.pending != "") {
		b.WriteString("\n")
	}

	if m.streaming || m.pending != "" || m.thinking != "" || len(m.tools) > 0 {
		indicator := ""
		if m.streaming {
			status := m.streamStatus()
			indicator = "  " + s.Cursor.Render(m.spin.View()) +
				s.Subtle.Render(" "+status)
		}
		b.WriteString("  " + s.AssistantLabel.Render("Claude") + indicator + "\n")

		if m.thinking != "" {
			b.WriteString(renderThinkingBlock(m.thinking, wrap) + "\n")
		}
		for _, t := range m.tools {
			b.WriteString(renderToolBlock(t, wrap) + "\n")
		}
		if m.pending != "" {
			body := padLinesToWidth(renderMarkdown(m.pending, bubbleInner), bubbleInner)
			b.WriteString(shadeBubble(s.AssistantBubble.Render(body)) + "\n")
		}
		b.WriteString("\n")
	}

	if len(m.queued) > 0 {
		b.WriteString("  " + s.Subtle.Render(fmt.Sprintf("queued · %d msg(s) waiting to send:", len(m.queued))) + "\n")
		for _, q := range m.queued {
			b.WriteString("  " + s.Subtle.Render("• "+truncateLine(q, wrap-4)) + "\n")
		}
		b.WriteString("\n")
	}

	if m.notice != "" {
		b.WriteString("  " + s.Subtle.Render(m.notice) + "\n")
	}

	if m.err != nil {
		b.WriteString(s.Error.Render("error: "+m.err.Error()) + "\n")
	}

	// Always end on blank rows so the last bubble's left bar never sits
	// flush against the input bar's left bar.
	for range viewportBottomPad {
		b.WriteString("\n")
	}

	return b.String()
}

// renderStaticMessages renders the persisted-message block, memoized. The
// blocks are separated by a blank line; there is no trailing blank (the caller
// adds one before the live tail when needed). The cache invalidates whenever
// the message count, wrap width, or active theme/appearance changes — the only
// inputs that can alter how a persisted (immutable) message draws.
func (m *chatModel) renderStaticMessages(wrap, bubbleInner int) string {
	key := fmt.Sprintf("%d|%d|%s|%t", len(m.session.Messages), wrap, CurrentTheme().Name, isDark())
	if key == m.staticKey {
		return m.staticCache
	}

	var b strings.Builder
	for i, msg := range m.session.Messages {
		if i > 0 {
			b.WriteString("\n")
		}
		switch msg.Role {
		case storage.RoleUser:
			label := s.UserLabel.Render("You")
			ts := s.SessionMeta.Render("  " + msg.At.Format("15:04"))
			b.WriteString("  " + label + ts + "\n")
			wrapped := padLinesToWidth(wordwrapKeepBlank(msg.Content, bubbleInner), bubbleInner)
			b.WriteString(shadeBubble(s.UserBubble.Render(wrapped)) + "\n")
		case storage.RoleAssistant:
			label := s.AssistantLabel.Render("Claude")
			ts := s.SessionMeta.Render("  " + msg.At.Format("15:04"))
			b.WriteString("  " + label + ts + "\n")
			body := padLinesToWidth(renderMarkdown(msg.Content, bubbleInner), bubbleInner)
			b.WriteString(shadeBubble(s.AssistantBubble.Render(body)) + "\n")
		case storage.RoleTool:
			b.WriteString(renderToolBlock(toolEvent{name: msg.Tool, input: msg.Content}, wrap) + "\n")
		}
	}

	m.staticCache = b.String()
	m.staticKey = key
	return m.staticCache
}

func renderThinkingBlock(text string, wrap int) string {
	style := lipgloss.NewStyle().Foreground(CurrentTheme().Dim()).Italic(true)
	header := style.Render("  💭 thinking")
	wrapped := wordwrapKeepBlank(strings.TrimSpace(text), wrap-4)
	var lines []string
	lines = append(lines, header)
	for _, ln := range strings.Split(wrapped, "\n") {
		lines = append(lines, style.Render("    "+ln))
	}
	return strings.Join(lines, "\n")
}

// prettyModelName shortens long model ids (e.g. "claude-opus-4-5-20251022")
// for header display, and falls back to the active model from stream init
// when the user hasn't picked one.
func prettyModelName(c claude.Streamer) string {
	raw := c.ModelRaw()
	if raw == "" {
		raw = c.ActiveModel()
	}
	if raw == "" {
		return "default"
	}
	return shortModelID(raw)
}

func shortModelID(id string) string {
	// strip a trailing yyyymmdd date if present (e.g. "claude-opus-4-5-20251022")
	parts := strings.Split(id, "-")
	if n := len(parts); n > 0 {
		last := parts[n-1]
		if len(last) == 8 {
			allDigits := true
			for _, r := range last {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				parts = parts[:n-1]
			}
		}
	}
	out := strings.Join(parts, "-")
	return strings.TrimPrefix(out, "claude-")
}
