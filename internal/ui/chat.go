package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/VladimirFilipovic/tuai/internal/claude"
	"github.com/VladimirFilipovic/tuai/internal/clipboard"
	"github.com/VladimirFilipovic/tuai/internal/storage"
	vimpkg "github.com/VladimirFilipovic/tuai/internal/vim"
)

// Mouse diagnostics: when TUAI_DEBUG_MOUSE=1 is set, every mouse-flavoured
// message that lands in chat.Update gets appended to /tmp/tuai-mouse.log
// together with a timestamp and the message's concrete Go type. Lets us
// figure out what a real terminal (Ghostty, iTerm2, …) actually sends when
// you turn the wheel — independently of the bubbletea decoder's choices.
var (
	mouseLogOnce sync.Once
	mouseLogF    *os.File
	mouseLogOn   bool
)

func mouseLog(msg tea.Msg) {
	mouseLogOnce.Do(func() {
		if os.Getenv("TUAI_DEBUG_MOUSE") != "1" {
			return
		}
		f, err := os.OpenFile("/tmp/tuai-mouse.log",
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		mouseLogF = f
		mouseLogOn = true
		fmt.Fprintf(mouseLogF, "\n--- session start %s ---\n", time.Now().Format(time.RFC3339))
	})
	if !mouseLogOn {
		return
	}
	switch m := msg.(type) {
	case tea.MouseMsg:
		mu := m.Mouse()
		fmt.Fprintf(mouseLogF, "%s  %-20T  btn=%d(%s) x=%d y=%d mod=%v str=%q\n",
			time.Now().Format("15:04:05.000"), msg, mu.Button, mu.Button, mu.X, mu.Y, mu.Mod, m.String())
	}
}

const (
	// Textarea starts at minInputLines rows so the input feels roomy and
	// short messages don't make it scroll. It grows up to maxInputLines
	// when the user types newlines (shift+enter / alt+enter / ctrl+j).
	inputLines    = 5
	minInputLines = 5
	maxInputLines = 10
)

type toolEvent struct {
	name  string
	input string
}

type chatModel struct {
	session    *storage.Session
	store      *storage.Store
	client     *claude.Client
	viewport   viewport.Model
	textarea   textarea.Model
	spin       spinner.Model
	streaming  bool
	pending    string      // final assistant text accumulated during the stream
	thinking   string      // live thinking text (not persisted)
	tools      []toolEvent // tool calls in this turn (not persisted)
	queued     []string    // messages typed while streaming, sent after done
	notice     string      // transient one-line notice (e.g. "model set to opus")
	streamCh   <-chan claude.Chunk
	cancel     context.CancelFunc
	width      int
	height     int
	err        error
	atBottom   bool
	lastRender time.Time
	lastCost   float64
	lastDur    int64

	// staticCache memoizes the rendered block of all persisted messages.
	// renderMessages runs on every spinner tick and stream chunk; re-running
	// chroma/markdown/shadeBubble over the whole history each time made long
	// conversations crawl. Persisted messages only change by appending (or a
	// /clear), so we rebuild only when staticKey — a digest of message count,
	// width, and active theme/appearance — changes.
	staticCache string
	staticKey   string

	// Pastes maps a placeholder token shown in the textarea (e.g. "[Pasted
	// text #1 +12 lines]") back to the full content. Multi-line pastes get
	// stowed here so the input bar stays scannable; on send the placeholders
	// are expanded back to their full text before the prompt leaves.
	pastes   map[string]string
	pasteSeq int

	// historyIdx tracks position in the user-message history when the user
	// presses up/down on the top/bottom line of the input. -1 means "not
	// browsing", 0 means the most recent user message, growing into the past.
	// historyDraft preserves whatever was in the textarea before browsing
	// started so down-past-most-recent restores it.
	historyIdx   int
	historyDraft string

	// vim is nil when modal editing is off. When non-nil it intercepts
	// keypresses ahead of the textarea; the textarea still handles every
	// key in Insert mode (vim returns "not consumed" there).
	vim *vimpkg.Editor

	// ac powers the @-mention path autocomplete. Activates whenever the
	// cursor sits inside an @-fragment; up/down/tab/enter/esc are routed
	// here ahead of the textarea while it's open.
	ac pathAutocompleteModel

	// selActive is true while a mouse drag-selection is in progress over the
	// chat viewport. selAnchor / selCursor hold the drag endpoints in content
	// coordinates; on release the spanned text is cleaned and copied to the
	// clipboard. See chat_select.go.
	selActive bool
	selAnchor selPoint
	selCursor selPoint

	// Copy-toast animation state for the "copied N chars" popup in the
	// header's right corner. copyToastChars==0 means hidden. copyToastFrame
	// drives the three render styles (pop → settled → fade); see
	// copyToastTickMsg in chat_select.go. copyToastID invalidates stale
	// ticks when a fresh copy lands mid-animation.
	copyToastChars int
	copyToastFrame int
	copyToastID    int
}

type chunkMsg struct {
	text      string
	thinking  string
	toolUse   string
	toolInput string
	sessionID string
	model     string
	cost      float64
	durMs     int64
	done      bool
	err       error
}

type backMsg struct{}

// clearSessionMsg is emitted by the command palette to wipe the current
// session's messages without leaving the chat view.
type clearSessionMsg struct{}

func newChatModel(sess *storage.Session, store *storage.Store, client *claude.Client) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Message Claude…  (enter send · shift+enter newline · esc cancel/back)"
	ta.Focus()
	ta.CharLimit = 0
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.SetWidth(80)
	ta.SetHeight(minInputLines)
	ta.MaxHeight = maxInputLines
	// Enter sends; shift+enter / alt+enter / ctrl+j inserts a newline.
	// shift+enter only works on terminals that speak the Kitty keyboard protocol
	// (Kitty, Ghostty, WezTerm, modern iTerm2). Alt+enter and ctrl+j are the
	// universal fallbacks.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	styleTextarea(&ta)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SetContent("")

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(CurrentTheme().Accent())

	m := chatModel{
		session:    sess,
		store:      store,
		client:     client,
		viewport:   vp,
		textarea:   ta,
		spin:       sp,
		atBottom:   true,
		historyIdx: -1,
	}
	// Vim is opt-in; it starts in Insert mode so typing always works on the
	// first keystroke without an extra `i`. Toggle on/off with `/vim`.
	if cfg, err := storage.LoadConfig(); err == nil && cfg.Vim {
		m.vim = vimpkg.New()
	}
	return m
}

func waitChunk(ch <-chan claude.Chunk) tea.Cmd {
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return chunkMsg{done: true}
		}
		return chunkMsg{
			text:      c.Text,
			thinking:  c.Thinking,
			toolUse:   c.ToolUse,
			toolInput: c.ToolInput,
			sessionID: c.SessionID,
			model:     c.Model,
			cost:      c.Cost,
			durMs:     c.DurMs,
			done:      c.Done,
			err:       c.Err,
		}
	}
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return textarea.Blink() },
		m.viewport.Init(),
	)
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	mouseLog(msg)
	var cmds []tea.Cmd
	var vpCmd, taCmd, spCmd tea.Cmd

	// Multi-line pastes get stowed under a short placeholder so the input
	// bar doesn't expand to wrap a 200-line paste. The textarea sees the
	// modified PasteMsg (containing only the placeholder) and inserts it
	// normally; sendMessage swaps the placeholder back to the real content.
	if p, ok := msg.(tea.PasteMsg); ok {
		if placeholder, stowed := m.stowPaste(p.Content); stowed {
			msg = tea.PasteMsg{Content: placeholder}
		}
	}

	// Spinner ticks: only forward while we're streaming, otherwise drop the
	// tick so the goroutine chain quietly stops. The spinner lives inside
	// the viewport, so re-render the viewport each frame to actually animate.
	if _, ok := msg.(spinner.TickMsg); ok {
		if !m.streaming {
			return m, nil
		}
		m.spin, spCmd = m.spin.Update(msg)
		m.refreshViewport()
		return m, spCmd
	}

	switch msg := msg.(type) {
	case clearSessionMsg:
		if !m.streaming {
			m.session.Messages = []storage.Message{}
			m.pending = ""
			m.err = nil
			m.resetHistoryNav()
			_ = m.store.Save(m.session)
			m.refreshViewport()
		}
		return m, nil

	case copyToastTickMsg:
		// Stale ticks (from a previous copy whose id no longer matches) are
		// dropped so they can't yank a fresh toast off-screen early.
		if msg.id != m.copyToastID {
			return m, nil
		}
		id := m.copyToastID
		switch m.copyToastFrame {
		case 0: // pop → settled
			m.copyToastFrame = 1
			return m, tea.Tick(copyToastHoldDuration, func(time.Time) tea.Msg {
				return copyToastTickMsg{id: id}
			})
		case 1: // settled → fade
			m.copyToastFrame = 2
			return m, tea.Tick(copyToastFadeDuration, func(time.Time) tea.Msg {
				return copyToastTickMsg{id: id}
			})
		default: // fade → hidden
			m.copyToastChars = 0
			m.copyToastFrame = 0
		}
		return m, nil

	case tea.KeyPressMsg:
		// Any keypress dismisses a lingering selection highlight from the
		// last copy so it doesn't sit there while the user types or scrolls.
		m.clearSelection()
		// Vim hook runs before the host's own key handling so motions,
		// operators, and the i/a/o family don't fall through to the
		// textarea. Insert mode reports "not consumed" for everything
		// except esc, so plain typing keeps working unchanged. Normal
		// mode reports "not consumed" only when esc is pressed with no
		// partial command — that lets the host's esc handler (back to
		// sessions / cancel stream) keep working.
		if m.vim != nil {
			if m.vim.HandleKey(&m.textarea, msg.String()) {
				m.refreshViewport()
				return m, nil
			}
		}
		// Path autocomplete intercepts nav / accept / dismiss keys while
		// the popup is open. Plain typing still flows through to the
		// textarea below; the popup refreshes from textarea state at the
		// end of Update so each new character re-filters the matches.
		if m.ac.active {
			switch msg.String() {
			case "up", "ctrl+p":
				m.ac.moveUp()
				return m, nil
			case "down", "ctrl+n":
				m.ac.moveDown()
				return m, nil
			case "tab", "enter":
				if m.ac.accept(&m.textarea) {
					m.relayout()
					return m, nil
				}
			case "esc":
				m.ac.close()
				m.relayout()
				return m, nil
			}
		}
		switch msg.String() {
		case "esc":
			if m.streaming && m.cancel != nil {
				m.cancel()
				m.streaming = false
				m.cancel = nil
				m.refreshViewport()
				return m, nil
			}
			return m, func() tea.Msg { return backMsg{} }

		case "ctrl+c":
			if m.streaming && m.cancel != nil {
				m.cancel()
			}
			return m, func() tea.Msg { return backMsg{} }

		case "enter", "ctrl+s":
			raw := strings.TrimSpace(m.textarea.Value())
			if raw == "" {
				return m, nil
			}
			m.notice = ""
			if strings.HasPrefix(raw, "/") {
				m.textarea.Reset()
				m.resetHistoryNav()
				return m, m.handleSlash(raw)
			}
			input := m.expandPastes(raw)
			if m.streaming {
				// Queue and let the current stream finish before sending.
				m.queued = append(m.queued, input)
				m.textarea.Reset()
				m.clearPastes()
				m.resetHistoryNav()
				m.refreshViewport()
				return m, nil
			}
			m.textarea.Reset()
			m.clearPastes()
			m.resetHistoryNav()
			return m, m.sendMessage(input)

		case "up":
			// Recall the previous user message when on the top line. If the
			// cursor is anywhere below row 0 we let the textarea move the
			// cursor up within the input instead. Viewport scrolling is on
			// PgUp/PgDn/Home/End and the mouse wheel — arrows stay history-
			// only so the user can recall past messages with one keystroke.
			if m.textarea.Line() == 0 && m.historyUp() {
				return m, nil
			}

		case "down":
			// Mirror of "up": only intercept on the last row, and only when
			// we're actually browsing history. Otherwise the down arrow keeps
			// its normal cursor-movement role.
			if m.historyIdx >= 0 && m.textarea.Line() >= m.textarea.LineCount()-1 {
				m.historyDown()
				return m, nil
			}

		case "ctrl+n":
			if !m.streaming {
				return m, func() tea.Msg { return newSessionMsg{} }
			}

		case "ctrl+l":
			if !m.streaming {
				m.session.Messages = []storage.Message{}
				m.pending = ""
				m.err = nil
				_ = m.store.Save(m.session)
				m.refreshViewport()
				return m, nil
			}

		case "ctrl+v":
			// Try image paste first. If the OS clipboard has a PNG, write
			// it to disk and drop an @-ref into the textarea so Claude can
			// see it. If there's no image, fall through to the textarea's
			// default ctrl+v handler (text paste).
			if path, err := clipboard.SaveImageToFile(); err == nil {
				m.insertImageRef(path)
				m.notice = "pasted image · " + path
				m.refreshViewport()
				return m, nil
			}

		case "pgup", "pgdown", "home", "end":
			// Let viewport handle navigation; user is now scrolling.
			m.atBottom = false
		}

	case chunkMsg:
		switch {
		case msg.sessionID != "":
			// system.init — capture session id (first turn only) and let header
			// repaint now that c.ActiveModel() is populated.
			if m.session.ResumeID == "" {
				m.session.ResumeID = msg.sessionID
				_ = m.store.Save(m.session)
			}
			cmds = append(cmds, waitChunk(m.streamCh))

		case msg.err != nil:
			m.streaming = false
			m.err = msg.err
			m.persistTools()
			m.pending = ""
			m.thinking = ""
			m.cancel = nil
			m.refreshViewport()

		case msg.done:
			m.streaming = false
			m.lastCost = msg.cost
			m.lastDur = msg.durMs
			m.persistTools()
			if m.pending != "" {
				m.session.Messages = append(m.session.Messages, storage.Message{
					Role:    storage.RoleAssistant,
					Content: m.pending,
					At:      time.Now(),
				})
				m.pending = ""
				_ = m.store.Save(m.session)
			}
			m.thinking = ""
			m.cancel = nil
			m.refreshViewport()

			// Drain a queued message, if any.
			if len(m.queued) > 0 {
				next := m.queued[0]
				m.queued = m.queued[1:]
				cmds = append(cmds, m.sendMessage(next))
			}

		case msg.toolUse != "":
			m.tools = append(m.tools, toolEvent{name: msg.toolUse})
			m.refreshViewport()
			cmds = append(cmds, waitChunk(m.streamCh))

		case msg.toolInput != "":
			if len(m.tools) > 0 {
				m.tools[len(m.tools)-1].input += msg.toolInput
				now := time.Now()
				if now.Sub(m.lastRender) > 50*time.Millisecond {
					m.refreshViewport()
					m.lastRender = now
				}
			}
			cmds = append(cmds, waitChunk(m.streamCh))

		case msg.thinking != "":
			m.thinking += msg.thinking
			now := time.Now()
			if now.Sub(m.lastRender) > 50*time.Millisecond {
				m.refreshViewport()
				m.lastRender = now
			}
			cmds = append(cmds, waitChunk(m.streamCh))

		default:
			m.pending += msg.text
			now := time.Now()
			if strings.ContainsRune(msg.text, '\n') || now.Sub(m.lastRender) > 50*time.Millisecond {
				m.refreshViewport()
				m.lastRender = now
			}
			cmds = append(cmds, waitChunk(m.streamCh))
		}
	}

	// The viewport's default keymap binds ctrl+u/ctrl+d/u/d/j/k/h/l/f/b/space
	// to scrolling. Forwarding every key to it lets the viewport hijack
	// keys the textarea needs (ctrl+u = delete-to-BOL, plain letters
	// the user is typing, and cmd+backspace which terminals translate
	// to ctrl+u). Only navigation keys we explicitly route here reach
	// the viewport; non-key events (mouse wheel, animations) still flow.
	switch k := msg.(type) {
	case tea.KeyPressMsg:
		switch k.String() {
		case "pgup", "pgdown", "home", "end":
			m.viewport, vpCmd = m.viewport.Update(msg)
		}
	case tea.MouseClickMsg:
		// Left press starts a drag-selection if it lands inside the viewport.
		// Other buttons (and presses on the input/help rows) are swallowed —
		// nothing in this view wants a raw click.
		if k.Button == tea.MouseLeft && m.inViewport(k.Y) {
			m.beginSelection(k.X, k.Y)
		}
		return m, nil
	case tea.MouseMotionMsg:
		// Drag with the left button held extends the live selection.
		if m.selActive && k.Button == tea.MouseLeft {
			m.updateSelection(k.X, k.Y)
		}
		return m, nil
	case tea.MouseReleaseMsg:
		// Release ends the drag: copy the spanned text to the clipboard.
		if m.selActive {
			return m, m.finishSelection()
		}
		return m, nil
	case tea.MouseMsg:
		// Catch every flavour of mouse message — wheel, click, motion,
		// release — so none of them slip through to the textarea's inner
		// viewport. The inner viewport scrolls horizontally on wheel-left /
		// wheel-right (Mac trackpad two-finger swipe) and on shift+wheel,
		// which the user reads as "the mouse moves my cursor sideways
		// instead of scrolling the chat".
		//
		// Some terminal/decoder combinations report wheel buttons via
		// MouseClickMsg instead of MouseWheelMsg; re-cast to MouseWheelMsg
		// so the chat viewport reliably treats them as scroll input.
		mouse := k.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp, tea.MouseWheelDown,
			tea.MouseWheelLeft, tea.MouseWheelRight:
			m.viewport, vpCmd = m.viewport.Update(tea.MouseWheelMsg(mouse))
			m.atBottom = m.viewport.AtBottom()
			prevHeight := m.textarea.Height()
			if want := m.desiredInputHeight(); want != prevHeight {
				m.relayout()
			}
			return m, vpCmd
		}
		// Non-wheel mouse events (clicks, motion, release): swallow.
		// Nothing in this view positions the caret via mouse, and forwarding
		// them lets the textarea's inner viewport eat them as scroll input
		// on terminals that bundle a click with their wheel reports.
		return m, nil
	default:
		m.viewport, vpCmd = m.viewport.Update(msg)
	}
	prevHeight := m.textarea.Height()
	prevAcHeight := m.ac.height()
	m.textarea, taCmd = m.textarea.Update(msg)
	cmds = append(cmds, vpCmd, taCmd)

	// Track whether user is pinned to bottom. Must sync both ways: when the
	// user scrolls up (mouse wheel, pgup, etc.) we need to drop the flag so
	// the next stream chunk's refreshViewport doesn't snap them back down.
	m.atBottom = m.viewport.AtBottom()

	// Refresh autocomplete state from the (possibly updated) textarea. Only
	// react to key events — mouse and timer messages can't change cursor or
	// value, and refreshing on every spinner tick would re-stat the disk.
	if _, isKey := msg.(tea.KeyPressMsg); isKey {
		m.ac.refresh(m.textarea)
	}

	// Grow/shrink the input bar to match its current content. Also relayout
	// when the autocomplete popup opens/closes/resizes so the viewport gives
	// up (or reclaims) the rows it needs.
	if want := m.desiredInputHeight(); want != prevHeight || m.ac.height() != prevAcHeight {
		m.relayout()
	}

	return m, tea.Batch(cmds...)
}

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

func (m *chatModel) sendMessage(input string) tea.Cmd {
	// Auto-name the session from its first user message if it still has the
	// default "Session <date>" placeholder. Mirrors what the claude.ai UI
	// does — turns "Session May 15 22:48" into something searchable.
	if len(m.session.Messages) == 0 && isAutoSessionName(m.session.Name) {
		if newName := deriveSessionName(input); newName != "" {
			m.session.Name = newName
		}
	}

	m.session.Messages = append(m.session.Messages, storage.Message{
		Role:    storage.RoleUser,
		Content: input,
		At:      time.Now(),
	})
	_ = m.store.Save(m.session)
	m.streaming = true
	m.pending = ""
	m.thinking = ""
	m.tools = nil
	m.err = nil
	m.atBottom = true
	m.refreshViewport()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	ch := m.client.Stream(ctx, input, m.session.ResumeID)
	m.streamCh = ch
	return tea.Batch(waitChunk(ch), m.spin.Tick)
}

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

// persistTools appends any live tool events from the current turn to the
// session as RoleTool messages, then clears the live buffer. Called when the
// turn ends — on `done` or on error — so the user keeps a permanent record of
// what Claude ran even after the spinner goes away.
func (m *chatModel) persistTools() {
	if len(m.tools) == 0 {
		return
	}
	now := time.Now()
	for _, t := range m.tools {
		m.session.Messages = append(m.session.Messages, storage.Message{
			Role:    storage.RoleTool,
			Tool:    t.name,
			Content: t.input,
			At:      now,
		})
	}
	m.tools = nil
	_ = m.store.Save(m.session)
}

func (m *chatModel) refreshViewport() {
	wasAtBottom := m.atBottom
	content := m.renderMessages()
	if m.hasSelection() {
		lo, hi := m.normalizedSelection()
		content = applySelectionStyle(content, lo, hi, selStyle())
	}
	m.viewport.SetContent(content)
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// hasSelection reports whether anchor and cursor span a non-empty range —
// used by refreshViewport so a stream chunk arriving mid-drag (or right
// after finishSelection) doesn't wipe the highlight via SetContent.
func (m *chatModel) hasSelection() bool {
	return m.selAnchor != m.selCursor
}

// viewportBottomPad keeps the last bubble's left bar off the input bar.
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
			preview := q
			if len(preview) > wrap-4 {
				preview = preview[:wrap-7] + "…"
			}
			b.WriteString("  " + s.Subtle.Render("• "+preview) + "\n")
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

func renderToolBlock(t toolEvent, wrap int) string {
	nameStyle := lipgloss.NewStyle().Foreground(CurrentTheme().Accent()).Bold(true)
	var b strings.Builder
	b.WriteString("  " + toolIcon(t.name) + " " + nameStyle.Render(t.name))

	body := renderToolInput(t.name, t.input, wrap)
	if body == "" {
		return b.String()
	}
	b.WriteString("\n")
	b.WriteString(body)
	return strings.TrimRight(b.String(), "\n")
}

// renderToolInput turns the raw streaming-JSON tool input into a readable
// block. We try to decode it as JSON first — once the stream finishes it's
// always valid JSON. For specific tools (Edit, Bash, Write, etc.) we render
// only the interesting fields and use the diff renderer when it fits. While
// the stream is still in flight the JSON is incomplete; we fall back to a
// dim raw preview so the user still sees progress.
func renderToolInput(name, raw string, wrap int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim())

	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		// Partial / not-yet-complete JSON. Show a compact dim preview that
		// hides the raw escape noise as best we can.
		preview := previewPartialJSON(raw, wrap-6)
		return dim.Render("    " + preview)
	}

	innerWrap := max(wrap-6, 20)

	switch strings.ToLower(name) {
	case "edit":
		return renderEditInput(fields, innerWrap)
	case "multiedit":
		return renderMultiEditInput(fields, innerWrap)
	case "write":
		return renderWriteInput(fields, innerWrap)
	case "read":
		return renderPathInput(fields, "file_path", innerWrap)
	case "bash":
		return renderBashInput(fields, innerWrap)
	case "grep":
		return renderGrepInput(fields, innerWrap)
	case "glob":
		return renderGlobInput(fields, innerWrap)
	case "webfetch":
		return renderKVInput(fields, []string{"url", "prompt"}, innerWrap)
	case "websearch":
		return renderKVInput(fields, []string{"query"}, innerWrap)
	case "task", "agent":
		return renderKVInput(fields, []string{"subagent_type", "description", "prompt"}, innerWrap)
	}
	return renderKVInput(fields, sortedKeys(fields), innerWrap)
}

func previewPartialJSON(raw string, width int) string {
	// Drop the most disruptive escape sequences so a stream-in-progress is
	// still scannable. We don't try to be perfect — once the stream lands
	// we re-render properly through the JSON path.
	r := strings.NewReplacer(`\n`, " ", `\t`, " ", `\"`, `"`)
	s := r.Replace(raw)
	if width > 0 && lipgloss.Width(s) > width {
		runes := []rune(s)
		if len(runes) > width-1 {
			s = string(runes[:width-1]) + "…"
		}
	}
	return s
}

func renderEditInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	old := stringField(f, "old_string")
	new := stringField(f, "new_string")
	diff := buildEditDiff(old, new)
	if diff == "" {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(indentLines(renderDiff(diff, wrap), "    "))
	return strings.TrimRight(b.String(), "\n")
}

func renderMultiEditInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	edits, _ := f["edits"].([]any)
	for i, e := range edits {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		old := stringField(em, "old_string")
		new := stringField(em, "new_string")
		diff := buildEditDiff(old, new)
		if diff == "" {
			continue
		}
		b.WriteString(dimLine(fmt.Sprintf("edit %d/%d", i+1, len(edits)), wrap))
		b.WriteString("\n")
		b.WriteString(indentLines(renderDiff(diff, wrap), "    "))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderWriteInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	content := stringField(f, "content")
	if content == "" {
		return strings.TrimRight(b.String(), "\n")
	}
	dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim())
	for _, ln := range strings.Split(content, "\n") {
		b.WriteString(dim.Render("    "+truncateLine(ln, wrap)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderBashInput(f map[string]any, wrap int) string {
	cmd := stringField(f, "command")
	if cmd == "" {
		return ""
	}
	var b strings.Builder
	codeStyle := lipgloss.NewStyle().Foreground(CurrentTheme().Accent())
	for i, ln := range strings.Split(cmd, "\n") {
		prefix := "    $ "
		if i > 0 {
			prefix = "      "
		}
		b.WriteString(codeStyle.Render(prefix+truncateLine(ln, wrap-2)) + "\n")
	}
	if desc := stringField(f, "description"); desc != "" {
		dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim()).Italic(true)
		b.WriteString(dim.Render("    "+truncateLine(desc, wrap)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGrepInput(f map[string]any, wrap int) string {
	var b strings.Builder
	pattern := stringField(f, "pattern")
	if pattern != "" {
		b.WriteString(dimLine("pattern: "+pattern, wrap))
		b.WriteString("\n")
	}
	for _, k := range []string{"path", "glob", "type", "output_mode"} {
		if v := stringField(f, k); v != "" {
			b.WriteString(dimLine(k+": "+v, wrap))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGlobInput(f map[string]any, wrap int) string {
	var b strings.Builder
	for _, k := range []string{"pattern", "path"} {
		if v := stringField(f, k); v != "" {
			b.WriteString(dimLine(k+": "+v, wrap))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderPathInput(f map[string]any, key string, wrap int) string {
	if v := stringField(f, key); v != "" {
		return dimLine(v, wrap)
	}
	return ""
}

func renderKVInput(f map[string]any, keys []string, wrap int) string {
	var b strings.Builder
	for _, k := range keys {
		v := stringField(f, k)
		if v == "" {
			continue
		}
		first := true
		for _, ln := range strings.Split(v, "\n") {
			prefix := k + ": "
			if !first {
				prefix = strings.Repeat(" ", len(k)+2)
			}
			b.WriteString(dimLine(prefix+ln, wrap))
			b.WriteString("\n")
			first = false
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func dimLine(s string, wrap int) string {
	dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim())
	return dim.Render("    " + truncateLine(s, wrap))
}

func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width-1 {
		return s
	}
	return string(runes[:width-1]) + "…"
}

func stringField(f map[string]any, key string) string {
	v, ok := f[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%v", x)
	case bool:
		return fmt.Sprintf("%v", x)
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return ""
}

func sortedKeys(f map[string]any) []string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildEditDiff renders old_string → new_string as a unified-diff body so the
// existing diff renderer can color it. We don't try to compute a real LCS;
// the simple "block of - lines then block of + lines" is enough to convey
// what changed for the typical Edit call.
func buildEditDiff(oldStr, newStr string) string {
	if oldStr == "" && newStr == "" {
		return ""
	}
	var b strings.Builder
	for _, ln := range strings.Split(strings.TrimRight(oldStr, "\n"), "\n") {
		b.WriteString("-" + ln + "\n")
	}
	for _, ln := range strings.Split(strings.TrimRight(newStr, "\n"), "\n") {
		b.WriteString("+" + ln + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// prettyModelName shortens long model ids (e.g. "claude-opus-4-5-20251022")
// for header display, and falls back to the active model from stream init
// when the user hasn't picked one.
func prettyModelName(c *claude.Client) string {
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

// renderInputArea wraps the textarea so every row of the input bar
// shares the same shaded background that opencode uses, even when the
// rows below the caret are empty. bubbles' textarea only fills width
// on rows that hold content; the empty rows collapse to a single EOB
// character. We pad each line to outerWidth-2 (the 2-cell left margin
// chatModel.View prepends) using the same subtleBg() as the textarea
// so the shading reads as one continuous block.
// unshadedSpaceRun matches an SGR reset followed by two or more plain spaces.
// bubbles' textarea pads empty rows with its EndOfBufferCharacter (a single
// space) and lets the inner viewport's Width style fill the rest with bare
// spaces — none of which carry the bg colour. Those bare spaces fall just
// after a reset and let the terminal background bleed through the shaded
// input bar.
var unshadedSpaceRun = regexp.MustCompile(`(\x1b\[0?m)( {2,})`)

// padLinesToWidth right-pads every line of text with plain spaces so each
// visible row reaches `width` cells. Width is measured with lipgloss so
// embedded ANSI escapes don't count. The trailing spaces themselves carry no
// color — shadeBubble re-shades them after the bubble wraps the content.
func padLinesToWidth(text string, width int) string {
	if width <= 0 {
		return text
	}
	var b strings.Builder
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ln)
		w := lipgloss.Width(ln)
		if w < width {
			b.WriteString(strings.Repeat(" ", width-w))
		}
	}
	return b.String()
}

// resetSGR matches every SGR reset emitted inside the bubble's content —
// chroma drops one between every syntax token, our markdown helpers do too
// for bold/inline-code. Each reset strips the bubble's background; we need
// to re-apply it after each so the shaded surface reads as one continuous
// block instead of fragments behind each colored span.
var resetSGR = regexp.MustCompile(`\x1b\[0?m`)

// bubbleBgSGR returns the raw SGR sequence that paints the current bubble
// background. We hardcode it from subtleBg() so we can splice it directly
// into a rendered string without round-tripping through lipgloss (which would
// emit its own reset and re-introduce the same bleed).
func bubbleBgSGR() string {
	if isDark() {
		return "\x1b[48;2;28;30;34m" // #1c1e22
	}
	return "\x1b[48;2;241;235;217m" // #f1ebd9
}

// pageBgSGR is the SGR for the chat *page* background — what fills the area
// around bubbles, gap rows, label margins, and any spot the viewport doesn't
// otherwise paint. Picked one step darker than the bubble bg so bubbles still
// read as raised cards on the page in both light and dark modes.
func pageBgSGR() string {
	if isDark() {
		return "\x1b[48;2;22;24;28m" // #16181c
	}
	return "\x1b[48;2;236;228;205m" // #ece4cd
}

// shadePage paints the page background across every line of the rendered
// chat view. Each line gets right-padded to width with bg-coloured spaces,
// and every internal SGR reset re-applies the page bg so inner styled spans
// don't punch holes back to the terminal default. Bubble lines have already
// been through shadeBubble, so their internal bg is bubble-tone; that wins
// over the page tone we splice in here because shadeBubble's bg sequence
// follows ours in the byte stream.
func shadePage(rendered string, width int) string {
	bg := pageBgSGR()
	const reset = "\x1b[0m"
	withBg := func(m string) string { return m + bg }

	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		processed := resetSGR.ReplaceAllStringFunc(ln, withBg)
		var pad string
		if w := lipgloss.Width(ln); w < width {
			pad = strings.Repeat(" ", width-w)
		}
		lines[i] = bg + processed + bg + pad + reset
	}
	return strings.Join(lines, "\n")
}

// shadeBubble re-applies the bubble's subtle background after every SGR
// reset inside the rendered content, line by line. This covers two cases at
// once: trailing pad spaces after the last reset on a row (the original bug
// the input bar had), and *text* sitting between a reset and the next styled
// span (the case that breaks chroma-highlighted code blocks).
//
// Each line gets a trailing explicit reset so the active bg never leaks past
// EOL into the next row.
func shadeBubble(rendered string) string {
	bg := bubbleBgSGR()
	withBg := func(m string) string { return m + bg }

	lines := strings.Split(rendered, "\n")
	for i, ln := range lines {
		processed := resetSGR.ReplaceAllStringFunc(ln, withBg)
		// We just appended bg after every reset, including the closing reset
		// at the end of the line. Strip that trailing bg so the line ends
		// clean and bg doesn't bleed into whatever follows.
		processed = strings.TrimSuffix(processed, bg)
		// And make sure the line really does end with a reset — lipgloss
		// usually emits one, but pad-only rows we built ourselves may not.
		if !strings.HasSuffix(processed, "\x1b[0m") && !strings.HasSuffix(processed, "\x1b[m") {
			processed += "\x1b[0m"
		}
		lines[i] = processed
	}
	return strings.Join(lines, "\n")
}

func renderInputArea(ta textarea.Model, outerWidth int) string {
	_ = outerWidth
	view := strings.TrimRight(ta.View(), "\n")
	bg := lipgloss.NewStyle().Background(subtleBg())
	// Make sure any unstyled gap inside the textarea picks up the shaded bg.
	view = unshadedSpaceRun.ReplaceAllStringFunc(view, func(match string) string {
		m := unshadedSpaceRun.FindStringSubmatch(match)
		return m[1] + bg.Render(m[2])
	})

	// Wrap the textarea in a thick left bar + horizontal padding *once*,
	// per row. Doing this externally (rather than via the textarea's Base
	// style) avoids the textarea drawing its own border inside ours.
	t := CurrentTheme()
	barColor := t.Accent()
	if !ta.Focused() {
		barColor = t.Dim()
	}
	bar := lipgloss.NewStyle().Foreground(barColor).Render("┃")
	pad := bg.Render(" ")
	prefix := bar + pad

	var b strings.Builder
	for i, ln := range strings.Split(view, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(prefix)
		b.WriteString(ln)
	}
	return b.String()
}

// styleTextarea sets only color styles on the textarea — no border, no
// padding. The thick left bar and shaded background that frame the input
// are drawn externally in renderInputArea so we don't end up with the
// textarea's internal viewport drawing one border and our Base style
// drawing a second one around it.
func styleTextarea(ta *textarea.Model) {
	t := CurrentTheme()
	st := ta.Styles()

	bg := lipgloss.NewStyle().Background(subtleBg())
	st.Focused.Base = bg
	st.Blurred.Base = bg
	st.Focused.CursorLine = bg
	st.Blurred.CursorLine = bg
	st.Focused.Text = bg
	st.Blurred.Text = bg
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(t.Dim()).Background(subtleBg())
	st.Blurred.Placeholder = lipgloss.NewStyle().Foreground(t.Dim()).Background(subtleBg())

	ta.SetStyles(st)
}

func wordwrapKeepBlank(text string, width int) string {
	var out strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		if strings.TrimSpace(line) == "" {
			out.WriteString(line)
			continue
		}
		out.WriteString(wrapLine(line, width))
	}
	return out.String()
}

// indentLines prepends prefix to every line of s. Used to shift multi-line
// styled blocks (e.g. the textarea) without losing per-row alignment — a
// plain "prefix + s" only indents the first row.
//
// Trailing newlines are preserved verbatim so we don't synthesize a phantom
// blank-and-padded row at the bottom of the input bar. (textarea.View()
// terminates its placeholder/content with "\n", which used to render as an
// extra empty shaded line below the real input row.)
func indentLines(text, prefix string) string {
	if prefix == "" {
		return text
	}
	trailingNL := strings.HasSuffix(text, "\n")
	trimmed := strings.TrimRight(text, "\n")
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	out := strings.Join(lines, "\n")
	if trailingNL {
		out += "\n"
	}
	return out
}

func wrapLine(line string, width int) string {
	if width < 1 {
		return line
	}
	var b strings.Builder
	for len(line) > width {
		// find last space
		idx := strings.LastIndex(line[:width], " ")
		if idx < width/2 {
			idx = width
		}
		b.WriteString(line[:idx])
		b.WriteByte('\n')
		line = strings.TrimLeft(line[idx:], " ")
	}
	b.WriteString(line)
	return b.String()
}

func (m chatModel) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder

	chip := s.HeaderChip.Render("tuai")
	name := s.Title.Render("  " + m.session.Name)
	modelChip := s.ModelChip.Render(prettyModelName(m.client))
	meta := s.Subtle.Render(fmt.Sprintf(" · %d msgs", len(m.session.Messages)))
	stat := ""
	switch {
	case m.streaming:
		stat = lipgloss.NewStyle().Foreground(CurrentTheme().Assistant()).Render("  ● streaming")
		if len(m.queued) > 0 {
			stat += s.Subtle.Render(fmt.Sprintf(" · %d queued", len(m.queued)))
		}
	case m.lastCost > 0 || m.lastDur > 0:
		stat = s.Subtle.Render(fmt.Sprintf("  · $%.4f · %.1fs", m.lastCost, float64(m.lastDur)/1000))
	}
	header := " " + chip + name + "  " + modelChip + meta + stat
	headerLine := lipgloss.NewStyle().MaxWidth(m.width).Render(header)
	if m.copyToastChars > 0 {
		toast := renderCopyToast(m.copyToastFrame, m.copyToastChars)
		hw := lipgloss.Width(headerLine)
		tw := lipgloss.Width(toast)
		if hw+tw+1 <= m.width {
			headerLine += strings.Repeat(" ", m.width-hw-tw) + toast
		}
	}
	b.WriteString(headerLine + "\n")
	b.WriteString(divider(m.width) + "\n")

	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.ac.active {
		b.WriteString(m.ac.view(m.width))
		b.WriteString("\n")
	}
	input := renderInputArea(m.textarea, m.width)
	b.WriteString(indentLines(input, "  "))
	b.WriteString("\n")

	helpText := "enter send • alt+enter/ctrl+j newline • ↑↓ history • pgup/pgdn scroll • drag to copy • ctrl+v paste • ctrl+p palette • /help • esc back"
	if m.ac.active {
		helpText = "↑↓ pick • tab/enter accept • esc dismiss"
	}
	if m.vim != nil {
		// Surface the active mode prominently — without it the user has no
		// way to tell why h/j/k/l are or aren't being typed as characters.
		modeBadge := lipgloss.NewStyle().
			Foreground(CurrentTheme().Accent()).
			Bold(true).
			Render("-- " + m.vim.Mode().String() + " --")
		help := s.Help.Render(helpText)
		b.WriteString(lipgloss.NewStyle().MaxWidth(m.width).Render(modeBadge + " " + help))
	} else {
		b.WriteString(lipgloss.NewStyle().MaxWidth(m.width).Render(s.Help.Render(helpText)))
	}

	return shadePage(b.String(), m.width)
}

func (m *chatModel) setSize(w, h int) {
	m.width = w
	m.height = h
	m.relayout()
}

// relayout sizes the textarea to fit its current content (capped) and gives
// the viewport whatever is left. Called whenever the input height may have
// changed (resize, paste, newline).
func (m *chatModel) relayout() {
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		return
	}
	taContent := m.desiredInputHeight()
	acHeight := m.ac.height()
	// Layout: header (1) + divider (1) + autocomplete (acHeight) + textarea
	// (taContent) + help (1). The autocomplete eats viewport rows when open;
	// the viewport reclaims them once it closes.
	vpHeight := max(h-3-taContent-acHeight, 3)
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(vpHeight)
	m.textarea.SetWidth(w - 4)
	m.textarea.SetHeight(taContent)
	m.refreshViewport()
}

func (m *chatModel) desiredInputHeight() int {
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	return min(max(lines, minInputLines), maxInputLines)
}
