package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

func renderToolBlock(t toolEvent, wrap int) string {
	nameStyle := lipgloss.NewStyle().Foreground(CurrentTheme().Accent()).Bold(true)
	var b strings.Builder
	b.WriteString("  " + toolIcon(t.name) + " " + nameStyle.Render(t.name))

	body := renderToolInput(t.name, t.input, wrap)
	if body == "" {
		return b.String()
	}
	b.WriteString("\n")
	b.WriteString(body)
	return strings.TrimRight(b.String(), "\n")
}

// renderToolInput turns the raw streaming-JSON tool input into a readable
// block. We try to decode it as JSON first — once the stream finishes it's
// always valid JSON. For specific tools (Edit, Bash, Write, etc.) we render
// only the interesting fields and use the diff renderer when it fits. While
// the stream is still in flight the JSON is incomplete; we fall back to a
// dim raw preview so the user still sees progress.
func renderToolInput(name, raw string, wrap int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim())

	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		// Partial / not-yet-complete JSON. Show a compact dim preview that
		// hides the raw escape noise as best we can.
		preview := previewPartialJSON(raw, wrap-6)
		return dim.Render("    " + preview)
	}

	innerWrap := max(wrap-6, 20)

	switch strings.ToLower(name) {
	case "edit":
		return renderEditInput(fields, innerWrap)
	case "multiedit":
		return renderMultiEditInput(fields, innerWrap)
	case "write":
		return renderWriteInput(fields, innerWrap)
	case "read":
		return renderPathInput(fields, "file_path", innerWrap)
	case "bash":
		return renderBashInput(fields, innerWrap)
	case "grep":
		return renderGrepInput(fields, innerWrap)
	case "glob":
		return renderGlobInput(fields, innerWrap)
	case "webfetch":
		return renderKVInput(fields, []string{"url", "prompt"}, innerWrap)
	case "websearch":
		return renderKVInput(fields, []string{"query"}, innerWrap)
	case "task", "agent":
		return renderKVInput(fields, []string{"subagent_type", "description", "prompt"}, innerWrap)
	}
	return renderKVInput(fields, sortedKeys(fields), innerWrap)
}

func previewPartialJSON(raw string, width int) string {
	// Drop the most disruptive escape sequences so a stream-in-progress is
	// still scannable. We don't try to be perfect — once the stream lands
	// we re-render properly through the JSON path.
	r := strings.NewReplacer(`\n`, " ", `\t`, " ", `\"`, `"`)
	s := r.Replace(raw)
	if width > 0 && lipgloss.Width(s) > width {
		runes := []rune(s)
		if len(runes) > width-1 {
			s = string(runes[:width-1]) + "…"
		}
	}
	return s
}

func renderEditInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	old := stringField(f, "old_string")
	new := stringField(f, "new_string")
	diff := buildEditDiff(old, new)
	if diff == "" {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString(indentLines(renderDiff(diff, wrap), "    "))
	return strings.TrimRight(b.String(), "\n")
}

func renderMultiEditInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	edits, _ := f["edits"].([]any)
	for i, e := range edits {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		old := stringField(em, "old_string")
		new := stringField(em, "new_string")
		diff := buildEditDiff(old, new)
		if diff == "" {
			continue
		}
		b.WriteString(dimLine(fmt.Sprintf("edit %d/%d", i+1, len(edits)), wrap))
		b.WriteString("\n")
		b.WriteString(indentLines(renderDiff(diff, wrap), "    "))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderWriteInput(f map[string]any, wrap int) string {
	var b strings.Builder
	path := stringField(f, "file_path")
	if path != "" {
		b.WriteString(dimLine(path, wrap))
		b.WriteString("\n")
	}
	content := stringField(f, "content")
	if content == "" {
		return strings.TrimRight(b.String(), "\n")
	}
	highlighted := highlightForFile(content, path)
	for _, ln := range strings.Split(highlighted, "\n") {
		b.WriteString("    " + ln + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderBashInput(f map[string]any, wrap int) string {
	cmd := stringField(f, "command")
	if cmd == "" {
		return ""
	}
	var b strings.Builder
	codeStyle := lipgloss.NewStyle().Foreground(CurrentTheme().Accent())
	for i, ln := range strings.Split(cmd, "\n") {
		prefix := "    $ "
		if i > 0 {
			prefix = "      "
		}
		b.WriteString(codeStyle.Render(prefix+truncateLine(ln, wrap-2)) + "\n")
	}
	if desc := stringField(f, "description"); desc != "" {
		dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim()).Italic(true)
		b.WriteString(dim.Render("    "+truncateLine(desc, wrap)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGrepInput(f map[string]any, wrap int) string {
	var b strings.Builder
	pattern := stringField(f, "pattern")
	if pattern != "" {
		b.WriteString(dimLine("pattern: "+pattern, wrap))
		b.WriteString("\n")
	}
	for _, k := range []string{"path", "glob", "type", "output_mode"} {
		if v := stringField(f, k); v != "" {
			b.WriteString(dimLine(k+": "+v, wrap))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGlobInput(f map[string]any, wrap int) string {
	var b strings.Builder
	for _, k := range []string{"pattern", "path"} {
		if v := stringField(f, k); v != "" {
			b.WriteString(dimLine(k+": "+v, wrap))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderPathInput(f map[string]any, key string, wrap int) string {
	if v := stringField(f, key); v != "" {
		return dimLine(v, wrap)
	}
	return ""
}

func renderKVInput(f map[string]any, keys []string, wrap int) string {
	var b strings.Builder
	for _, k := range keys {
		v := stringField(f, k)
		if v == "" {
			continue
		}
		first := true
		for _, ln := range strings.Split(v, "\n") {
			prefix := k + ": "
			if !first {
				prefix = strings.Repeat(" ", len(k)+2)
			}
			b.WriteString(dimLine(prefix+ln, wrap))
			b.WriteString("\n")
			first = false
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func dimLine(s string, wrap int) string {
	dim := lipgloss.NewStyle().Foreground(CurrentTheme().Dim())
	return dim.Render("    " + truncateLine(s, wrap))
}

func truncateLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	if len(runes) <= width-1 {
		return s
	}
	return string(runes[:width-1]) + "…"
}

func stringField(f map[string]any, key string) string {
	v, ok := f[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%v", x)
	case bool:
		return fmt.Sprintf("%v", x)
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return ""
}

func sortedKeys(f map[string]any) []string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildEditDiff renders old_string → new_string as a unified-diff body so the
// existing diff renderer can color it. We don't try to compute a real LCS;
// the simple "block of - lines then block of + lines" is enough to convey
// what changed for the typical Edit call.
func buildEditDiff(oldStr, newStr string) string {
	if oldStr == "" && newStr == "" {
		return ""
	}
	var b strings.Builder
	for _, ln := range strings.Split(strings.TrimRight(oldStr, "\n"), "\n") {
		b.WriteString("-" + ln + "\n")
	}
	for _, ln := range strings.Split(strings.TrimRight(newStr, "\n"), "\n") {
		b.WriteString("+" + ln + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
