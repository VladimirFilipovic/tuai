package ui

import "testing"

func TestToolIcon_KnownNames(t *testing.T) {
	cases := map[string]string{
		"read":      "📖",
		"Read":      "📖", // case-insensitive
		"EDIT":      "✎",
		"multiedit": "✎",
		"write":     "✚",
		"bash":      "▶",
		"grep":      "⌕",
		"glob":      "❖",
		"webfetch":  "🌐",
		"websearch": "🔎",
		"task":      "🤖",
		"agent":     "🤖",
	}
	for name, want := range cases {
		if got := toolIcon(name); got != want {
			t.Errorf("toolIcon(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestToolIcon_UnknownReturnsFallback(t *testing.T) {
	if got := toolIcon("not-a-real-tool"); got != "▸" {
		t.Errorf("toolIcon(unknown) = %q, want fallback ▸", got)
	}
}
