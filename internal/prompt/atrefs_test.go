package prompt

import (
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

func TestExpandAtRefs_NoRefs(t *testing.T) {
	got := ExpandAtRefs("hello world", t.TempDir())
	if got != "hello world" {
		t.Errorf("ExpandAtRefs with no @refs should return input unchanged, got %q", got)
	}
}

func TestExpandAtRefs_InlinesFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hello body"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExpandAtRefs("see @notes.md", dir)
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
	got := ExpandAtRefs("see @notes.md.", dir)
	if !strings.Contains(got, "--- @notes.md ---") {
		t.Errorf("trailing dot should be stripped from path lookup, got %q", got)
	}
}

func TestExpandAtRefs_ImageRefNoInline(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExpandAtRefs("look @pic.png", dir)
	if !strings.Contains(got, "[attached image:") {
		t.Errorf("image ref should produce attachment marker, got %q", got)
	}
	// Binary content should NOT be inlined.
	if strings.Contains(got, "\x89PNG") {
		t.Errorf("binary content should not be inlined")
	}
}

func TestExpandAtRefs_MissingIgnored(t *testing.T) {
	got := ExpandAtRefs("missing @nope.md here", t.TempDir())
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
	got := ExpandAtRefs("a @x.md b @x.md c", dir)
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
	got := ExpandAtRefs("see @big.txt", dir)
	if !strings.Contains(got, "skipped:") {
		t.Errorf("oversize file should produce a 'skipped' note, got %q", got)
	}
}

func TestExpandAtRefs_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "abs.md")
	if err := os.WriteFile(abs, []byte("absolute body"), 0o644); err != nil {
		t.Fatal(err)
	}
	// cwd != dir, so a relative ref would miss; absolute path must be honored.
	got := ExpandAtRefs("look @"+abs, t.TempDir())
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
	got := ExpandAtRefs(prompt, dir)
	if strings.Contains(got, "topsecret") {
		t.Errorf("@-ref inside a fenced code block must NOT be inlined; got %q", got)
	}
	if strings.Contains(got, "--- @secret.txt ---") {
		t.Errorf("attachment block should not be produced for fenced @-refs")
	}

	// Sanity: an @-ref OUTSIDE the fence in the same prompt still inlines.
	prompt2 := "Read @secret.txt and also:\n```\nignore @secret.txt here\n```"
	got2 := ExpandAtRefs(prompt2, dir)
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
	got := ExpandAtRefs("see @../definitely-not-here.txt", t.TempDir())
	if strings.Contains(got, "--- @../definitely-not-here.txt ---") {
		t.Errorf("missing ../ file should not produce an attachment block")
	}
}
