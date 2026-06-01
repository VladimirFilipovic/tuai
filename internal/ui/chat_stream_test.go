package ui

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/VladimirFilipovic/tuai/internal/claude"
	"github.com/VladimirFilipovic/tuai/internal/storage"
)

// fakeStreamer satisfies claude.Streamer with canned chunks, so the chat model
// can be driven through a full turn in a test without spawning a real `claude`
// subprocess. lastPrompt records what the model sent (post @-ref expansion).
type fakeStreamer struct {
	chunks     []claude.Chunk
	lastPrompt string
	lastResume string
	model      string
	cwd        string
}

func (f *fakeStreamer) Stream(_ context.Context, prompt, resumeID string) <-chan claude.Chunk {
	f.lastPrompt = prompt
	f.lastResume = resumeID
	ch := make(chan claude.Chunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func (f *fakeStreamer) Model() string       { return f.model }
func (f *fakeStreamer) ModelRaw() string    { return f.model }
func (f *fakeStreamer) ActiveModel() string { return f.model }
func (f *fakeStreamer) SetModel(m string)   { f.model = m }
func (f *fakeStreamer) Cwd() string         { return f.cwd }

// drainTurn pumps the stream channel through Update until a done/err chunk
// lands, mimicking the bubbletea loop without a real event source.
func drainTurn(m chatModel) chatModel {
	for {
		msg := waitChunk(m.streamCh)()
		cm := msg.(chunkMsg)
		m, _ = m.Update(cm)
		if !m.streaming {
			return m
		}
	}
}

func TestChatTurnStreamsWithoutSubprocess(t *testing.T) {
	store, err := storage.NewStoreAt(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	sess := store.New("test")
	fake := &fakeStreamer{
		cwd: t.TempDir(),
		chunks: []claude.Chunk{
			{SessionID: "sess-123", Model: "claude-test"},
			{Text: "Hello"},
			{Text: ", world"},
			{Done: true, Cost: 0.01, DurMs: 42},
		},
	}

	m := newChatModel(sess, store, fake)
	m.streaming = true
	cmd := m.sendMessage("hi there")
	if cmd == nil {
		t.Fatal("sendMessage returned nil cmd")
	}
	m = drainTurn(m)

	if fake.lastPrompt != "hi there" {
		t.Errorf("streamed prompt = %q, want %q", fake.lastPrompt, "hi there")
	}
	if m.session.ResumeID != "sess-123" {
		t.Errorf("ResumeID = %q, want sess-123", m.session.ResumeID)
	}
	if m.streaming {
		t.Error("model still streaming after done chunk")
	}
	// The user message + the assembled assistant reply should be persisted.
	if n := len(m.session.Messages); n != 2 {
		t.Fatalf("session has %d messages, want 2 (user + assistant)", n)
	}
	last := m.session.Messages[1]
	if last.Role != storage.RoleAssistant || last.Content != "Hello, world" {
		t.Errorf("assistant message = %+v, want role=assistant content=%q", last, "Hello, world")
	}
	if m.lastCost != 0.01 || m.lastDur != 42 {
		t.Errorf("cost/dur = %v/%v, want 0.01/42", m.lastCost, m.lastDur)
	}
}
