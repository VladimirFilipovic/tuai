package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsImageExt(t *testing.T) {
	yes := []string{"a.png", "a.PNG", "shot.jpeg", "foo.gif", "bar.WebP", "x.bmp"}
	no := []string{"a.txt", "a", "a.go", "a.html", "a.png.txt"}
	for _, p := range yes {
		if !isImageExt(p) {
			t.Errorf("isImageExt(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if isImageExt(p) {
			t.Errorf("isImageExt(%q) = true, want false", p)
		}
	}
}

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

func TestExpandAtRefs_NoRefs(t *testing.T) {
	got := expandAtRefs("hello world", t.TempDir())
	if got != "hello world" {
		t.Errorf("expandAtRefs with no @refs should return input unchanged, got %q", got)
	}
}

func TestExpandAtRefs_InlinesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hello body"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtRefs("see @notes.md", dir)
	if !strings.Contains(got, "see @notes.md") {
		t.Errorf("original prompt should be preserved, got %q", got)
	}
	if !strings.Contains(got, "--- @notes.md ---") {
		t.Errorf("attachment header missing, got %q", got)
	}
	if !strings.Contains(got, "hello body") {
		t.Errorf("file body missing, got %q", got)
	}
	if !strings.Contains(got, "--- end @notes.md ---") {
		t.Errorf("attachment footer missing, got %q", got)
	}
}

func TestExpandAtRefs_TrailingPunctuationStripped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtRefs("see @notes.md.", dir)
	if !strings.Contains(got, "--- @notes.md ---") {
		t.Errorf("trailing dot should be stripped from path lookup, got %q", got)
	}
}

func TestExpandAtRefs_ImageRefNoInline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtRefs("look @pic.png", dir)
	if !strings.Contains(got, "[attached image:") {
		t.Errorf("image ref should produce attachment marker, got %q", got)
	}
	// Binary content should NOT be inlined.
	if strings.Contains(got, "\x89PNG") {
		t.Errorf("binary content should not be inlined")
	}
}

func TestExpandAtRefs_MissingIgnored(t *testing.T) {
	got := expandAtRefs("missing @nope.md here", t.TempDir())
	// The @-ref stays as plain text; no attachment block.
	if !strings.Contains(got, "missing @nope.md here") {
		t.Errorf("original prompt should be preserved")
	}
	if strings.Contains(got, "--- @nope.md ---") {
		t.Errorf("missing file should NOT produce attachment block, got %q", got)
	}
}

func TestExpandAtRefs_DedupesRefs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtRefs("a @x.md b @x.md c", dir)
	if n := strings.Count(got, "--- @x.md ---"); n != 1 {
		t.Errorf("duplicate @refs should be inlined once, got %d copies", n)
	}
}

func TestExpandAtRefs_SkipsOversize(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, 256*1024+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	got := expandAtRefs("see @big.txt", dir)
	if !strings.Contains(got, "skipped:") {
		t.Errorf("oversize file should produce a 'skipped' note, got %q", got)
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
		{1, "\n", 2},    // all-newline string continues the run
		{2, "\n\n", 4},  // continues
		{3, "x", 0},     // any non-newline resets
		{0, "a\nb", 0},  // trailing char is non-newline
		{5, "ok\n", 1},  // restarts at own trailing run
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

func TestExpandAtRefs_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "abs.md")
	if err := os.WriteFile(abs, []byte("absolute body"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cwd != dir, so a relative ref would miss; absolute path must be honored.
	got := expandAtRefs("look @"+abs, t.TempDir())
	if !strings.Contains(got, "absolute body") {
		t.Errorf("absolute @-path should inline regardless of cwd, got %q", got)
	}
}

func TestExpandAtRefs_SkipsRefsInsideFencedCodeBlocks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("topsecret"), 0o600); err != nil {
		t.Fatal(err)
	}
	// User is quoting another agent's output — the @-ref is inside a fence.
	prompt := "Here's what the bot said:\n```\nplease read @secret.txt\n```\nWhat do you think?"
	got := expandAtRefs(prompt, dir)
	if strings.Contains(got, "topsecret") {
		t.Errorf("@-ref inside a fenced code block must NOT be inlined; got %q", got)
	}
	if strings.Contains(got, "--- @secret.txt ---") {
		t.Errorf("attachment block should not be produced for fenced @-refs")
	}

	// Sanity: an @-ref OUTSIDE the fence in the same prompt still inlines.
	prompt2 := "Read @secret.txt and also:\n```\nignore @secret.txt here\n```"
	got2 := expandAtRefs(prompt2, dir)
	if !strings.Contains(got2, "topsecret") {
		t.Errorf("non-fenced @-ref should still inline; got %q", got2)
	}
	// And we should only inline once even though the ref appears twice (once
	// outside, once inside fence — the dedupe + fence skip together give 1).
	if n := strings.Count(got2, "topsecret"); n != 1 {
		t.Errorf("expected exactly one inline of secret content, got %d", n)
	}
}

func TestExpandAtRefs_ParentTraversalStaysWithinCwd(t *testing.T) {
	// @../something resolves against cwd by Join, which collapses ".." — the
	// caller is opting into reading outside cwd. We don't *prevent* it here
	// (that's by design; @-refs trust the user) but we do verify the
	// resolution doesn't silently swallow content from arbitrary paths when
	// the target doesn't exist.
	got := expandAtRefs("see @../definitely-not-here.txt", t.TempDir())
	if strings.Contains(got, "--- @../definitely-not-here.txt ---") {
		t.Errorf("missing ../ file should not produce an attachment block")
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
