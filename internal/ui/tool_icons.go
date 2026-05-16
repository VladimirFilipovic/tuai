package ui

import "strings"

// toolIcon returns a glyph for a Claude Code tool call. Falls back to a
// generic marker for tools we don't have a custom icon for. Keep icons to
// single-cell BMP characters so width math elsewhere stays predictable.
func toolIcon(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "📖"
	case "edit", "multiedit":
		return "✎"
	case "write":
		return "✚"
	case "bash":
		return "▶"
	case "grep":
		return "⌕"
	case "glob":
		return "❖"
	case "ls":
		return "▤"
	case "webfetch":
		return "🌐"
	case "websearch":
		return "🔎"
	case "task", "agent":
		return "🤖"
	case "todowrite", "taskcreate", "taskupdate", "tasklist":
		return "☑"
	case "notebookedit":
		return "📓"
	default:
		return "▸"
	}
}
