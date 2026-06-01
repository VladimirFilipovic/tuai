// Package prompt assembles the text that leaves the UI for the claude CLI.
// It owns prompt-shaping concerns — @-file expansion today — that are about
// what the user typed, not about how the CLI subprocess is driven.
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var atRefRe = regexp.MustCompile(`(^|\s)@([^\s]+)`)

// stripFencedBlocks replaces the body of every ``` ... ``` fenced block with
// spaces (preserving line/byte structure isn't necessary — we just need a
// version of the prompt that has no @-refs *inside* a fence). Used by
// ExpandAtRefs so a user quoting `@.env` in a code fence doesn't accidentally
// inline that file into the outgoing prompt.
func stripFencedBlocks(prompt string) string {
	var out strings.Builder
	out.Grow(len(prompt))
	lines := strings.Split(prompt, "\n")
	inFence := false
	for i, ln := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out.WriteString(ln)
	}
	return out.String()
}

func isImageExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// ExpandAtRefs scans the prompt for @-references (e.g. `@foo.go`, `@~/notes.md`,
// `@./src/x.py`) and appends the file contents at the end of the prompt as
// fenced attachment blocks. @-refs inside fenced code blocks (``` ... ```)
// are skipped — the user is quoting something, not asking to inline it.
// Unresolved refs are left as plain text. Relative refs resolve against cwd.
//
// Claude Code's `-p` mode does not auto-resolve @ refs the way interactive
// mode does, so the UI inlines them here before handing the prompt to the CLI.
func ExpandAtRefs(prompt, cwd string) string {
	prose := stripFencedBlocks(prompt)
	matches := atRefRe.FindAllStringSubmatch(prose, -1)
	if len(matches) == 0 {
		return prompt
	}
	const maxBytes = 256 * 1024
	const trailingPunct = ".,;:!?)]}>"
	seen := map[string]bool{}
	var atts strings.Builder
	for _, m := range matches {
		ref := m[2]
		for len(ref) > 0 && strings.ContainsRune(trailingPunct, rune(ref[len(ref)-1])) {
			ref = ref[:len(ref)-1]
		}
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true

		path := ref
		if strings.HasPrefix(path, "~/") {
			if home, herr := os.UserHomeDir(); herr == nil {
				path = filepath.Join(home, path[2:])
			}
		} else if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		// For images (and other binary files) don't try to inline content —
		// just note the absolute path so Claude can Read it with its tools.
		if isImageExt(path) {
			fmt.Fprintf(&atts, "\n\n[attached image: %s]\n", path)
			continue
		}
		if info.Size() > maxBytes {
			fmt.Fprintf(&atts, "\n\n--- @%s (skipped: %d bytes > %d limit) ---\n",
				ref, info.Size(), maxBytes)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Fprintf(&atts, "\n\n--- @%s ---\n%s\n--- end @%s ---\n",
			ref, string(data), ref)
	}
	if atts.Len() == 0 {
		return prompt
	}
	return prompt + atts.String()
}
