package ui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/VladimirFilipovic/tuai/internal/storage"
)

// makeScrollableChat builds a chat model big enough that the viewport
// content overflows — we need that to exercise scrolling. The session is
// stuffed with enough user/assistant messages that renderMessages() produces
// many more lines than the viewport can show at once.
func makeScrollableChat(t *testing.T) chatModel {
	t.Helper()

	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.SetWidth(80)
	ta.SetHeight(minInputLines)
	ta.Focus()

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))

	sess := &storage.Session{ID: "test", Name: "scroll-test"}
	for range 30 {
		sess.Messages = append(sess.Messages,
			storage.Message{Role: storage.RoleUser, Content: "user msg " + strings.Repeat("x ", 20), At: time.Now()},
			storage.Message{Role: storage.RoleAssistant, Content: "assistant reply " + strings.Repeat("y ", 20), At: time.Now()},
		)
	}

	m := chatModel{
		session:    sess,
		viewport:   vp,
		textarea:   ta,
		width:      80,
		height:     20,
		atBottom:   true,
		historyIdx: -1,
	}
	// Populate the viewport with rendered content and size it like setSize() would.
	m.relayout()
	m.viewport.GotoBottom()
	m.atBottom = m.viewport.AtBottom()
	return m
}

// TestScrollUpReleasesBottomPin is the regression test for the core scroll
// bug: prior code only ever set atBottom=true, never false, so once the user
// reached the bottom, subsequent refreshViewport() calls (from spinner ticks
// and stream chunks) would yank them back down to the latest message — making
// the upper part of the conversation effectively unreachable during streaming.
func TestScrollUpReleasesBottomPin(t *testing.T) {
	m := makeScrollableChat(t)
	if !m.atBottom {
		t.Fatalf("setup: expected atBottom=true after GotoBottom")
	}

	// Mouse wheel up should scroll the viewport and clear the bottom pin.
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelUp}
	updated, _ := m.Update(wheel)
	if updated.atBottom {
		t.Fatalf("after wheel-up, atBottom should be false; viewport.AtBottom()=%v YOffset=%d",
			updated.viewport.AtBottom(), updated.viewport.YOffset())
	}
	if updated.viewport.AtBottom() {
		t.Fatalf("viewport should no longer be at bottom after wheel-up")
	}
}

// TestStreamingDoesNotSnapScrolledUserBack covers the user-visible symptom:
// while a response is streaming in, if the user has scrolled up to re-read
// earlier messages, each chunk's refreshViewport must NOT jump them back to
// the bottom.
func TestStreamingDoesNotSnapScrolledUserBack(t *testing.T) {
	m := makeScrollableChat(t)

	// User scrolls up via the wheel.
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelUp}
	m, _ = m.Update(wheel)
	pos := m.viewport.YOffset()
	if pos == 0 && m.viewport.AtBottom() {
		t.Fatalf("setup: viewport didn't actually scroll")
	}

	// Simulate a streaming chunk arriving. refreshViewport runs inside;
	// because atBottom is now false, the position should be preserved.
	m.streaming = true
	m.pending = "partial response text"
	m.refreshViewport()

	if m.viewport.YOffset() != pos {
		t.Errorf("streaming chunk yanked viewport: was at YOffset=%d, now at %d", pos, m.viewport.YOffset())
	}
	if m.atBottom {
		t.Errorf("atBottom should remain false during streaming when user scrolled up")
	}
}

// TestStreamingPinnedToBottomGrows is the happy-path complement: a user who
// has NOT scrolled up should follow the stream — every chunk re-pins them to
// the bottom so the latest text is always visible.
func TestStreamingPinnedToBottomGrows(t *testing.T) {
	m := makeScrollableChat(t)
	if !m.atBottom {
		t.Fatalf("setup: expected atBottom=true")
	}

	m.streaming = true
	m.pending = "first chunk"
	m.refreshViewport()
	if !m.atBottom || !m.viewport.AtBottom() {
		t.Errorf("expected viewport pinned to bottom while atBottom=true")
	}

	// More text arrives; should still be pinned to (the new) bottom.
	m.pending += "\n" + strings.Repeat("more text\n", 20)
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Errorf("viewport should follow growing content to bottom")
	}
}

// TestUpArrowDoesNotScrollViewport: up arrow is reserved for history navigation
// only — scrolling the chat is on PgUp/PgDn and the mouse wheel. Even with no
// user history to recall, up arrow must not move the viewport (it falls through
// to the textarea's own cursor handling).
func TestUpArrowDoesNotScrollViewport(t *testing.T) {
	m := makeScrollableChat(t)
	// Drop user messages so historyUp() returns false; only assistant content
	// remains in the viewport.
	m.session.Messages = nil
	for range 30 {
		m.session.Messages = append(m.session.Messages,
			storage.Message{Role: storage.RoleAssistant, Content: "assistant reply " + strings.Repeat("y ", 20), At: time.Now()},
		)
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
	m.atBottom = m.viewport.AtBottom()
	startY := m.viewport.YOffset()

	up := tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	m, _ = m.Update(up)

	if m.viewport.YOffset() != startY {
		t.Errorf("up arrow must not scroll the chat viewport: YOffset before=%d after=%d",
			startY, m.viewport.YOffset())
	}
}

// TestPageDownReturnsToBottomRestoresPin: after PgUp lets the user read back,
// PgDn back to the end should re-pin atBottom so subsequent stream chunks
// follow the conversation again.
func TestPageDownReturnsToBottomRestoresPin(t *testing.T) {
	m := makeScrollableChat(t)

	// Scroll up, confirm pin released.
	m.viewport.GotoTop()
	m.atBottom = m.viewport.AtBottom()
	if m.atBottom {
		t.Fatalf("setup: expected atBottom=false after GotoTop")
	}

	// Hammer PgDn until we're back at the bottom. Loop bound is generous —
	// content can easily run hundreds of lines past a 12-row viewport.
	pgdn := tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})
	for range 200 {
		m, _ = m.Update(pgdn)
		if m.viewport.AtBottom() {
			break
		}
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("PgDn loop didn't reach bottom; YOffset=%d", m.viewport.YOffset())
	}
	if !m.atBottom {
		t.Errorf("atBottom should be true once viewport reaches the bottom via PgDn")
	}
}

// TestSpinnerTickWhileScrolledUpKeepsPosition: the spinner ticks every frame
// during a stream and each tick calls refreshViewport(). If the bottom-pin
// tracking is broken, those ticks alone (without any chunk arriving) snap the
// user back to the bottom. Guard against that regression.
func TestSpinnerTickWhileScrolledUpKeepsPosition(t *testing.T) {
	m := makeScrollableChat(t)

	// User scrolls up.
	m.viewport.GotoTop()
	m.atBottom = m.viewport.AtBottom()
	pos := m.viewport.YOffset()

	// Simulate several refresh calls (one per spinner frame).
	m.streaming = true
	for range 10 {
		m.refreshViewport()
	}

	if m.viewport.YOffset() != pos {
		t.Errorf("spinner ticks moved viewport: was at YOffset=%d, now at %d", pos, m.viewport.YOffset())
	}
}

// TestPageUpReleasesBottomPin: PgUp routes to the viewport, so atBottom must
// reflect the resulting position. Otherwise the next refreshViewport snaps
// the user back to the bottom — same class of bug as the wheel case.
func TestPageUpReleasesBottomPin(t *testing.T) {
	m := makeScrollableChat(t)
	if !m.atBottom {
		t.Fatalf("setup: expected atBottom=true")
	}

	pgup := tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp})
	m, _ = m.Update(pgup)

	if m.atBottom {
		t.Errorf("after PgUp, atBottom should be false (viewport.AtBottom=%v YOffset=%d)",
			m.viewport.AtBottom(), m.viewport.YOffset())
	}
}
