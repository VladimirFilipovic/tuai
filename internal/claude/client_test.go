package claude

import (
	"context"
	"testing"
)

func TestLastLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"only", "only"},
		{"first\nsecond", "second"},
		{"a\nb\nc", "c"},
		{"trailing\n", ""},
	}
	for _, c := range cases {
		if got := lastLine(c.in); got != c.want {
			t.Errorf("lastLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUpdateTrailingNL(t *testing.T) {
	cases := []struct {
		cur  int
		s    string
		want int
	}{
		{0, "hello", 0},
		{0, "hello\n", 1},
		{0, "hello\n\n", 2},
		{1, "\n", 2},   // all-newline string continues the run
		{2, "\n\n", 4}, // continues
		{3, "x", 0},    // any non-newline resets
		{0, "a\nb", 0}, // trailing char is non-newline
		{5, "ok\n", 1}, // restarts at own trailing run
	}
	for _, c := range cases {
		if got := updateTrailingNL(c.cur, c.s); got != c.want {
			t.Errorf("updateTrailingNL(%d,%q) = %d, want %d", c.cur, c.s, got, c.want)
		}
	}
}

// TestDispatchSeparatesTextBlocks is the regression test for glued text
// blocks: a turn of text → tool_use → text must come back with a blank-line
// break between the two text blocks, not "first.Stack's up.".
func TestDispatchSeparatesTextBlocks(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"agent flow first."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Bash"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Stack's up."}}}`,
	}

	c := &Client{}
	ch := make(chan Chunk, 64)
	st := &streamState{}
	for _, ln := range lines {
		c.dispatchLine(context.Background(), []byte(ln), ch, st)
	}
	close(ch)

	var text string
	var sawTool bool
	for chunk := range ch {
		text += chunk.Text
		if chunk.ToolUse == "Bash" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatal("expected a Bash tool-use chunk")
	}
	want := "agent flow first.\n\nStack's up."
	if text != want {
		t.Errorf("joined text = %q, want %q", text, want)
	}
}

func TestResultErr(t *testing.T) {
	cases := []struct {
		name string
		in   streamLine
		want string // "" = nil error
	}{
		{"not-error", streamLine{IsError: false}, ""},
		{"api-status", streamLine{IsError: true, APIErrorStatus: "529"}, "claude error: 529"},
		{"result-text", streamLine{IsError: true, Result: "rate limited"}, "claude error: rate limited"},
		{"api-wins-over-result", streamLine{IsError: true, APIErrorStatus: "500", Result: "ignored"}, "claude error: 500"},
		{"empty-error", streamLine{IsError: true}, "claude reported an error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resultErr(c.in)
			if c.want == "" {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}
			if got == nil || got.Error() != c.want {
				t.Errorf("got %v, want %q", got, c.want)
			}
		})
	}
}

// TestDispatchNoLeadingBreak makes sure the very first text block isn't
// prefixed with a spurious separator.
func TestDispatchNoLeadingBreak(t *testing.T) {
	lines := []string{
		`{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}`,
	}
	c := &Client{}
	ch := make(chan Chunk, 16)
	st := &streamState{}
	for _, ln := range lines {
		c.dispatchLine(context.Background(), []byte(ln), ch, st)
	}
	close(ch)
	var text string
	for chunk := range ch {
		text += chunk.Text
	}
	if text != "hello" {
		t.Errorf("first block text = %q, want %q", text, "hello")
	}
}
