package ui

import (
	"strings"
	"testing"
)

func TestDeriveSessionName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  \n\n  ", ""},
		{"hello world", "hello world"},
		{"\n\nfirst non-empty\nsecond", "first non-empty"},
		{"   trim me  ", "trim me"},
		{strings.Repeat("a", 60), strings.Repeat("a", 49) + "…"},
	}
	for _, c := range cases {
		if got := deriveSessionName(c.in); got != c.want {
			t.Errorf("deriveSessionName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsAutoSessionName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"Session May 16 12:00", true},
		{"Session ", true},
		{"session may 16", false}, // case-sensitive on purpose
		{"My Custom Name", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isAutoSessionName(c.in); got != c.want {
			t.Errorf("isAutoSessionName(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLastRune(t *testing.T) {
	cases := []struct {
		in   string
		want rune
	}{
		{"", 0},
		{"a", 'a'},
		{"hello", 'o'},
		{"héllo", 'o'},
		{"日本語", '語'},
		{"x ", ' '},
	}
	for _, c := range cases {
		if got := lastRune(c.in); got != c.want {
			t.Errorf("lastRune(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShortModelID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"opus", "opus"},
		{"sonnet", "sonnet"},
		{"claude-opus-4-7", "opus-4-7"},
		{"claude-sonnet-4-6-20251022", "sonnet-4-6"},
		{"claude-haiku-4-5-20251001", "haiku-4-5"},
		// trailing 8-digit suffix is only stripped if all-digits.
		{"claude-opus-4-7-foo20251022", "opus-4-7-foo20251022"},
		{"claude-opus-4-7-12345", "opus-4-7-12345"},
	}
	for _, c := range cases {
		if got := shortModelID(c.in); got != c.want {
			t.Errorf("shortModelID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStowPaste(t *testing.T) {
	m := &chatModel{}

	// Short single-line pastes flow through untouched.
	if _, stowed := m.stowPaste("short"); stowed {
		t.Errorf("short paste should not be stowed")
	}
	if _, stowed := m.stowPaste("a single long line"); stowed {
		t.Errorf("short single line should not be stowed")
	}

	// Multi-line pastes are stowed.
	placeholder, stowed := m.stowPaste("line1\nline2\nline3")
	if !stowed {
		t.Fatal("multi-line paste should be stowed")
	}
	if !strings.Contains(placeholder, "+3 lines") {
		t.Errorf("placeholder should report line count, got %q", placeholder)
	}
	if got := m.pastes[placeholder]; got != "line1\nline2\nline3" {
		t.Errorf("stowed content mismatch: got %q", got)
	}

	// Long single-line paste is stowed under "+N chars".
	long := strings.Repeat("x", 300)
	placeholder2, stowed := m.stowPaste(long)
	if !stowed {
		t.Fatal("long single-line paste should be stowed")
	}
	if !strings.Contains(placeholder2, "+300 chars") {
		t.Errorf("placeholder should report char count, got %q", placeholder2)
	}
}

func TestExpandPastes(t *testing.T) {
	m := &chatModel{}
	placeholder, _ := m.stowPaste("line1\nline2\nline3")

	input := "hey check this: " + placeholder + " — what do you think?"
	expanded := m.expandPastes(input)

	want := "hey check this: line1\nline2\nline3 — what do you think?"
	if expanded != want {
		t.Errorf("expandPastes: got %q, want %q", expanded, want)
	}

	// Deleting the placeholder drops the paste.
	if m.expandPastes("nothing here") != "nothing here" {
		t.Errorf("placeholder deletion should drop the paste content")
	}

	m.clearPastes()
	if len(m.pastes) != 0 || m.pasteSeq != 0 {
		t.Errorf("clearPastes should reset state, got pastes=%v seq=%d", m.pastes, m.pasteSeq)
	}
}
