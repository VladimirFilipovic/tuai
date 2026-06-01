package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	"github.com/VladimirFilipovic/tuai/internal/prompt"
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
	client     claude.Streamer
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

func newChatModel(sess *storage.Session, store *storage.Store, client claude.Streamer) chatModel {
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
			case "tab":
				if m.ac.accept(&m.textarea) {
					m.relayout()
				}
				return m, nil
			case "enter":
				// Enter while the popup is open must never fall through to
				// the outer 'enter' branch (which would send the half-typed
				// @-fragment as a prompt). Accept if there's a match, dismiss
				// otherwise.
				if m.ac.accept(&m.textarea) {
					m.relayout()
				} else {
					m.ac.close()
					m.relayout()
				}
				return m, nil
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
			if !m.streaming {
				// User already cancelled; the trailing err chunk from the
				// closing stream is noise.
				break
			}
			m.streaming = false
			if !errors.Is(msg.err, context.Canceled) {
				m.err = msg.err
			}
			m.persistTools()
			m.pending = ""
			m.thinking = ""
			m.cancel = nil
			m.refreshViewport()

		case msg.done:
			if !m.streaming {
				// Stream done arrived after esc-cancel (or after we already
				// processed an err for this stream). Suppress drain/persist —
				// a cancelled turn must not auto-send queued messages.
				break
			}
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
	// Inline @file references just before the prompt leaves; the stored
	// message above keeps the user's raw text, not the expanded attachments.
	expanded := prompt.ExpandAtRefs(input, m.client.Cwd())
	ch := m.client.Stream(ctx, expanded, m.session.ResumeID)
	m.streamCh = ch
	return tea.Batch(waitChunk(ch), m.spin.Tick)
}

// handleSlash parses /commands typed in the input. Returns the tea.Cmd to
// dispatch (which may emit a message that the parent App handles).
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
