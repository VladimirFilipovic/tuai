package ui

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/textarea"
)

func TestFragmentAtCursor(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		cursor    int
		wantFrag  string
		wantOk    bool
		wantAtPos int
	}{
		{name: "empty", value: "", cursor: 0, wantOk: false},
		{name: "no @", value: "hello world", cursor: 5, wantOk: false},
		{name: "@ at start", value: "@foo", cursor: 4, wantFrag: "foo", wantOk: true, wantAtPos: 0},
		{name: "@ after space", value: "see @foo", cursor: 8, wantFrag: "foo", wantOk: true, wantAtPos: 4},
		{name: "@ after newline", value: "see\n@foo", cursor: 8, wantFrag: "foo", wantOk: true, wantAtPos: 4},
		{name: "just @", value: "@", cursor: 1, wantFrag: "", wantOk: true, wantAtPos: 0},
		{name: "email-like rejected", value: "foo@bar", cursor: 7, wantOk: false},
		{name: "space breaks fragment", value: "@foo bar", cursor: 8, wantOk: false},
		{name: "subdir fragment", value: "@internal/ui/cha", cursor: 16, wantFrag: "internal/ui/cha", wantOk: true, wantAtPos: 0},
		{name: "cursor mid-fragment", value: "@foobar", cursor: 4, wantFrag: "foo", wantOk: true, wantAtPos: 0},
		{name: "cursor before @", value: "hi @foo", cursor: 2, wantOk: false},
		{name: "out-of-bounds cursor clamped", value: "@x", cursor: 100, wantFrag: "x", wantOk: true, wantAtPos: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			frag, atPos, ok := fragmentAtCursor(c.value, c.cursor)
			if ok != c.wantOk {
				t.Fatalf("ok = %v, want %v", ok, c.wantOk)
			}
			if !ok {
				return
			}
			if frag != c.wantFrag {
				t.Errorf("frag = %q, want %q", frag, c.wantFrag)
			}
			if atPos != c.wantAtPos {
				t.Errorf("atPos = %d, want %d", atPos, c.wantAtPos)
			}
		})
	}
}

func TestSearchPaths(t *testing.T) {
	dir := t.TempDir()
	// Lay out: alpha.go, beta.go, .hidden, src/, src/main.go
	mustWrite(t, filepath.Join(dir, "alpha.go"), "package x")
	mustWrite(t, filepath.Join(dir, "beta.go"), "package x")
	mustWrite(t, filepath.Join(dir, ".hidden"), "x")
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "src", "main.go"), "package main")

	chdir(t, dir)

	t.Run("empty fragment lists cwd, no hidden", func(t *testing.T) {
		got := searchPaths("")
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3 (alpha, beta, src). got=%v", len(got), got)
		}
		// Directories first.
		if !got[0].isDir || got[0].display != "src" {
			t.Errorf("first should be src/, got %+v", got[0])
		}
		if got[0].insert != "src/" {
			t.Errorf("dir insert should have trailing slash, got %q", got[0].insert)
		}
	})

	t.Run("hidden shown when prefix starts with dot", func(t *testing.T) {
		got := searchPaths(".")
		found := false
		for _, m := range got {
			if m.display == ".hidden" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected .hidden in results, got %v", got)
		}
	})

	t.Run("prefix filter is case-insensitive", func(t *testing.T) {
		got := searchPaths("Alp")
		if len(got) != 1 || got[0].display != "alpha.go" {
			t.Errorf("want [alpha.go], got %v", got)
		}
	})

	t.Run("subdir fragment", func(t *testing.T) {
		got := searchPaths("src/m")
		if len(got) != 1 || got[0].display != "src/main.go" {
			t.Errorf("want [src/main.go], got %v", got)
		}
	})

	t.Run("trailing slash lists dir contents", func(t *testing.T) {
		got := searchPaths("src/")
		if len(got) != 1 || got[0].display != "src/main.go" {
			t.Errorf("want [src/main.go], got %v", got)
		}
	})

	t.Run("non-existent dir yields nothing", func(t *testing.T) {
		got := searchPaths("nope/x")
		if got != nil {
			t.Errorf("want nil, got %v", got)
		}
	})
}

func TestReplaceWithCursor(t *testing.T) {
	ta := textarea.New()
	ta.SetWidth(80)
	ta.SetHeight(5)

	// Single-line: replace "@fo" with "@foo/" and verify cursor sits between
	// the two halves so further typing extends the path naturally.
	replaceWithCursor(&ta, "hello @foo/", "bar")
	if got := ta.Value(); got != "hello @foo/bar" {
		t.Fatalf("value = %q", got)
	}
	// Insert a marker char to probe where the cursor is.
	ta.InsertRune('X')
	if got := ta.Value(); got != "hello @foo/Xbar" {
		t.Errorf("after marker insert: %q (cursor was not between halves)", got)
	}
}

func TestAcceptInsertsPath(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"), "x")
	chdir(t, dir)

	ta := textarea.New()
	ta.SetWidth(80)
	ta.SetHeight(5)
	ta.SetValue("see @al")

	var ac pathAutocompleteModel
	ac.refresh(ta)
	if !ac.active {
		t.Fatal("autocomplete should be active after typing @al")
	}
	if len(ac.matches) == 0 {
		t.Fatal("expected at least one match for @al")
	}
	if !ac.accept(&ta) {
		t.Fatal("accept returned false")
	}
	if got := ta.Value(); got != "see @alpha.go" {
		t.Errorf("value = %q, want %q", got, "see @alpha.go")
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"a\x1b[1;32mb\x1b[mc", "abc"},
	}
	for _, c := range cases {
		if got := stripANSI(c.in); got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
