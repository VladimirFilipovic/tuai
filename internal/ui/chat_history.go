package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/VladimirFilipovic/tuai/internal/storage"
)

// insertImageRef inserts a short `[Pasted image #N name.png]` placeholder at
// the cursor and stows the real `@<absolute-path>` under that token in
// m.pastes. The placeholder keeps the input bar scannable; expandPastes swaps
// it back to the @-ref on send so Claude still receives the file path.
func (m *chatModel) insertImageRef(path string) {
	if m.pastes == nil {
		m.pastes = map[string]string{}
	}
	m.pasteSeq++
	placeholder := fmt.Sprintf("[Pasted image #%d %s]", m.pasteSeq, filepath.Base(path))
	// Surrounding spaces in the stored value preserve the @-ref tokenization
	// rules expandPastes used to enforce inline (since the placeholder itself
	// has no leading "@").
	m.pastes[placeholder] = "@" + path

	cur := m.textarea.Value()
	if cur == "" {
		m.textarea.SetValue(placeholder + " ")
	} else {
		sep := ""
		if r := lastRune(cur); r != ' ' && r != '\n' && r != '\t' {
			sep = " "
		}
		m.textarea.SetValue(cur + sep + placeholder + " ")
	}
	m.textarea.CursorEnd()
	m.relayout()
}

// Pastes are stowed under a placeholder when they're large enough that
// having them inline would clutter the input bar — same threshold the
// claude.ai web UI uses for its chips. Anything multi-line or longer than
// ~200 chars qualifies; trivial pastes (a path, a single short snippet)
// still flow through untouched.
const (
	pasteMinLines = 2
	pasteMinChars = 200
)

// stowPaste decides whether a clipboard paste should be replaced with a
// short placeholder in the input bar. Returns the placeholder + true when
// stowed, "" + false when the paste should flow through to the textarea
// untouched.
func (m *chatModel) stowPaste(content string) (string, bool) {
	lines := strings.Count(content, "\n") + 1
	if lines < pasteMinLines && len(content) < pasteMinChars {
		return "", false
	}
	if m.pastes == nil {
		m.pastes = map[string]string{}
	}
	m.pasteSeq++
	// Use the more informative dimension for the chip label: line count
	// for multi-line, char count for a long single-line paste (e.g. a URL).
	var placeholder string
	if lines >= pasteMinLines {
		placeholder = fmt.Sprintf("[Pasted text #%d +%d lines]", m.pasteSeq, lines)
	} else {
		placeholder = fmt.Sprintf("[Pasted text #%d +%d chars]", m.pasteSeq, len(content))
	}
	m.pastes[placeholder] = content
	return placeholder, true
}

// expandPastes swaps every stowed-paste placeholder in the input back to its
// original content. Pastes the user has deleted from the textarea since are
// silently dropped — that's the intended way to discard a paste.
func (m *chatModel) expandPastes(input string) string {
	if len(m.pastes) == 0 {
		return input
	}
	for placeholder, content := range m.pastes {
		input = strings.ReplaceAll(input, placeholder, content)
	}
	return input
}

func (m *chatModel) clearPastes() {
	m.pastes = nil
	m.pasteSeq = 0
}

// userHistory returns prior user messages newest-first. Built on demand
// rather than cached because the session can be cleared/modified out from
// under us and the list is small (one entry per turn).
func (m *chatModel) userHistory() []string {
	var out []string
	for i := len(m.session.Messages) - 1; i >= 0; i-- {
		if m.session.Messages[i].Role == storage.RoleUser {
			out = append(out, m.session.Messages[i].Content)
		}
	}
	return out
}

// historyUp walks one step further into the past. Returns false when there
// is no history to recall so the caller can let the textarea handle the
// keypress normally (e.g. for cursor movement on an empty buffer).
func (m *chatModel) historyUp() bool {
	hist := m.userHistory()
	if len(hist) == 0 {
		return false
	}
	if m.historyIdx == -1 {
		m.historyDraft = m.textarea.Value()
		m.historyIdx = 0
	} else if m.historyIdx < len(hist)-1 {
		m.historyIdx++
	} else {
		return true // already at oldest — swallow the key but don't move
	}
	m.textarea.SetValue(hist[m.historyIdx])
	m.textarea.CursorEnd()
	m.relayout()
	return true
}

// historyDown is the inverse of historyUp. Stepping below index 0 restores
// the draft the user had before they started browsing.
func (m *chatModel) historyDown() {
	hist := m.userHistory()
	if m.historyIdx == -1 {
		return
	}
	if m.historyIdx == 0 {
		m.textarea.SetValue(m.historyDraft)
		m.historyDraft = ""
		m.historyIdx = -1
	} else {
		m.historyIdx--
		m.textarea.SetValue(hist[m.historyIdx])
	}
	m.textarea.CursorEnd()
	m.relayout()
}

func (m *chatModel) resetHistoryNav() {
	m.historyIdx = -1
	m.historyDraft = ""
}

// isAutoSessionName reports whether the session is still wearing its default
// placeholder name (the "Session Jan 2 15:04" format the store hands out).
// We only auto-rename in that case, so any user-picked name survives.
func isAutoSessionName(name string) bool {
	return strings.HasPrefix(name, "Session ")
}

// deriveSessionName turns the first user message into a short session label.
// Takes the first non-empty line, trims it, and caps it to ~50 visible chars.
func deriveSessionName(input string) string {
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const max = 50
		r := []rune(line)
		if len(r) > max {
			return string(r[:max-1]) + "…"
		}
		return line
	}
	return ""
}

func lastRune(s string) rune {
	if s == "" {
		return 0
	}
	r := []rune(s)
	return r[len(r)-1]
}
