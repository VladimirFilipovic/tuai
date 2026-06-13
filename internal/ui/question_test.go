package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/VladimirFilipovic/tuai/internal/claude"
	"github.com/VladimirFilipovic/tuai/internal/storage"
)

const questionJSON = `{"questions":[
	{"question":"Which auth method should we use?","header":"Auth","multiSelect":false,
	 "options":[{"label":"JWT","description":"stateless"},{"label":"Sessions","description":"server-side"}]},
	{"question":"Which features do you want?","header":"Features","multiSelect":true,
	 "options":[{"label":"SSO"},{"label":"2FA"},{"label":"Magic links"}]}
]}`

func TestParseQuestions(t *testing.T) {
	qs := parseQuestions(questionJSON)
	if len(qs) != 2 {
		t.Fatalf("parsed %d questions, want 2", len(qs))
	}
	if qs[0].Header != "Auth" || qs[0].MultiSelect {
		t.Errorf("q0 = %+v, want header Auth, single-select", qs[0])
	}
	if len(qs[1].Options) != 3 || !qs[1].MultiSelect {
		t.Errorf("q1 = %+v, want 3 options, multi-select", qs[1])
	}

	if parseQuestions(`{"questions":[{"question":"x","opt`) != nil {
		t.Error("partial JSON should parse to nil")
	}
	if parseQuestions(`{"questions":[]}`) != nil {
		t.Error("empty questions should parse to nil")
	}
	if parseQuestions(`{"questions":[{"question":"x","options":[]}]}`) != nil {
		t.Error("question without options should be dropped")
	}
}

func TestQuestionConfirmFlow(t *testing.T) {
	var q questionModel
	q.open(parseQuestions(questionJSON))
	if !q.active {
		t.Fatal("panel should be active after open")
	}

	// Q1 (single-select): move to "Sessions", enter picks the cursor option.
	q.moveDown()
	answer, done := q.confirm()
	if done || answer != "" {
		t.Fatalf("confirm after q1 = (%q, %v), want pending second question", answer, done)
	}
	if q.qIdx != 1 || q.cursor != 0 {
		t.Fatalf("after q1: qIdx=%d cursor=%d, want 1,0", q.qIdx, q.cursor)
	}

	// Q2 (multi-select): toggle SSO and Magic links.
	q.toggle()
	q.moveDown()
	q.moveDown()
	q.toggle()
	answer, done = q.confirm()
	if !done {
		t.Fatal("confirm after last question should report done")
	}
	want := "Auth: Sessions\nFeatures: SSO, Magic links"
	if answer != want {
		t.Errorf("answer = %q, want %q", answer, want)
	}
}

func TestQuestionSingleSelectIsExclusive(t *testing.T) {
	var q questionModel
	q.open(parseQuestions(questionJSON))
	q.toggle() // pick JWT
	q.moveDown()
	q.toggle() // pick Sessions — must unpick JWT
	if q.selected[0][0] || !q.selected[0][1] {
		t.Errorf("selected = %v, want only Sessions", q.selected[0])
	}
}

func TestPendingQuestionsScansLastCompleteCall(t *testing.T) {
	tools := []toolEvent{
		{name: "Read", input: `{"file_path":"x.go"}`},
		{name: "AskUserQuestion", input: questionJSON},
	}
	if qs := pendingQuestions(tools); len(qs) != 2 {
		t.Fatalf("pendingQuestions = %d questions, want 2", len(qs))
	}
	if pendingQuestions([]toolEvent{{name: "Bash", input: `{}`}}) != nil {
		t.Error("no AskUserQuestion call should yield nil")
	}
	// Incomplete JSON (stream cut off) must not activate the panel.
	if pendingQuestions([]toolEvent{{name: "AskUserQuestion", input: `{"questions":[{"qu`}}) != nil {
		t.Error("partial input should yield nil")
	}
}

// newQuestionChat drives a full fake turn that ends in an AskUserQuestion
// call and returns the model with the panel open.
func newQuestionChat(t *testing.T) (chatModel, *fakeStreamer) {
	t.Helper()
	store, err := storage.NewStoreAt(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeStreamer{
		cwd: t.TempDir(),
		chunks: []claude.Chunk{
			{SessionID: "sess-q", Model: "claude-test"},
			{ToolUse: "AskUserQuestion"},
			{ToolInput: questionJSON},
			{Done: true},
		},
	}
	m := newChatModel(store.New("test"), store, fake)
	m.setSize(100, 40)
	if cmd := m.sendMessage("set up auth"); cmd == nil {
		t.Fatal("sendMessage returned nil cmd")
	}
	m = drainTurn(m)
	if !m.question.active {
		t.Fatal("question panel should open after a turn ending in AskUserQuestion")
	}
	return m, fake
}

func TestQuestionPanelRendersInView(t *testing.T) {
	m, _ := newQuestionChat(t)
	view := m.View()
	for _, want := range []string{"Claude asks", "Which auth method should we use?", "JWT", "Sessions"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if !strings.Contains(view, "enter answer") {
		t.Error("help line should describe the question keys")
	}
}

func TestQuestionPanelOpensAndAnswers(t *testing.T) {
	m, fake := newQuestionChat(t)

	// Enter on Q1 (cursor on JWT) advances to Q2; enter on Q2 with nothing
	// toggled picks the cursor option and sends the composed answer.
	m, _ = m.Update(keyMsgNoText(tea.KeyEnter, 0))
	if !m.question.active || m.question.qIdx != 1 {
		t.Fatalf("after first enter: active=%v qIdx=%d, want open on q2", m.question.active, m.question.qIdx)
	}
	m, cmd := m.Update(keyMsgNoText(tea.KeyEnter, 0))
	if m.question.active {
		t.Fatal("panel should close after the last answer")
	}
	if cmd == nil || !m.streaming {
		t.Fatal("answering should start the next turn")
	}
	want := "Auth: JWT\nFeatures: SSO"
	if fake.lastPrompt != want {
		t.Errorf("sent answer = %q, want %q", fake.lastPrompt, want)
	}
}

func TestQuestionPanelNumberPick(t *testing.T) {
	m, fake := newQuestionChat(t)

	// "2" on the single-select question = pick Sessions and confirm.
	m, _ = m.Update(keyMsg('2', 0))
	if m.question.qIdx != 1 {
		t.Fatalf("qIdx = %d, want 1 after number pick", m.question.qIdx)
	}
	// "1" toggles SSO on the multi-select (no auto-confirm), enter sends.
	m, _ = m.Update(keyMsg('1', 0))
	if !m.question.active {
		t.Fatal("multi-select number press must not auto-confirm")
	}
	m, _ = m.Update(keyMsgNoText(tea.KeyEnter, 0))
	want := "Auth: Sessions\nFeatures: SSO"
	if fake.lastPrompt != want {
		t.Errorf("sent answer = %q, want %q", fake.lastPrompt, want)
	}
}

func TestQuestionPanelEscDismissesAndTypingWins(t *testing.T) {
	m, fake := newQuestionChat(t)

	m, _ = m.Update(keyMsgNoText(tea.KeyEscape, 0))
	if m.question.active {
		t.Fatal("esc should dismiss the panel")
	}

	// Free-form reply still works as the answer path.
	m.textarea.SetValue("neither, use passkeys")
	m, _ = m.Update(keyMsgNoText(tea.KeyEnter, 0))
	if fake.lastPrompt != "neither, use passkeys" {
		t.Errorf("typed reply = %q", fake.lastPrompt)
	}
}

func TestQuestionPanelTypedTextKeepsKeys(t *testing.T) {
	m, _ := newQuestionChat(t)

	// With text in the textarea the panel must not own keys: "2" types.
	m.textarea.SetValue("use ")
	m, _ = m.Update(keyMsg('2', 0))
	if got := m.textarea.Value(); got != "use 2" {
		t.Errorf("textarea = %q, want %q (panel must not steal typed keys)", got, "use 2")
	}
	if m.question.qIdx != 0 {
		t.Error("panel state should be untouched while typing")
	}
}

func TestStaleStreamChunksDropped(t *testing.T) {
	store, err := storage.NewStoreAt(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeStreamer{cwd: t.TempDir(), chunks: []claude.Chunk{{Done: true}}}
	m := newChatModel(store.New("test"), store, fake)
	m.setSize(100, 40)
	_ = m.sendMessage("hi")

	// A done chunk from a previous generation (cancelled turn whose channel
	// drained late) must not end the current stream.
	m, _ = m.Update(chunkMsg{gen: m.streamGen - 1, done: true})
	if !m.streaming {
		t.Fatal("stale done chunk ended the live stream")
	}
	m, _ = m.Update(chunkMsg{gen: m.streamGen, done: true})
	if m.streaming {
		t.Fatal("current-generation done chunk should end the stream")
	}
}

func TestInterruptedTurnPersists(t *testing.T) {
	store, err := storage.NewStoreAt(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeStreamer{cwd: t.TempDir(), chunks: []claude.Chunk{{Text: "partial"}}}
	m := newChatModel(store.New("test"), store, fake)
	m.vim = nil // newChatModel reads the real user config; keep esc deterministic
	m.setSize(100, 40)
	_ = m.sendMessage("hi")
	m.pending = "partial answer so far"
	m.tools = []toolEvent{{name: "Bash", input: `{"command":"ls"}`}}

	m, _ = m.Update(keyMsgNoText(tea.KeyEscape, 0))
	if m.streaming {
		t.Fatal("esc should cancel the stream")
	}
	msgs := m.session.Messages
	if len(msgs) != 3 { // user + tool + partial assistant
		t.Fatalf("persisted %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[1].Role != storage.RoleTool || msgs[1].Tool != "Bash" {
		t.Errorf("message 1 = %+v, want the Bash tool record", msgs[1])
	}
	if msgs[2].Role != storage.RoleAssistant || msgs[2].Content != "partial answer so far" {
		t.Errorf("message 2 = %+v, want the partial assistant text", msgs[2])
	}
	if !strings.Contains(m.notice, "interrupted") {
		t.Errorf("notice = %q, want an interrupted notice", m.notice)
	}
}
