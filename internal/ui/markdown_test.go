package ui

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func TestIsDiffLang(t *testing.T) {
	cases := []struct {
		lang string
		want bool
	}{
		{"diff", true},
		{"DIFF", true},
		{" diff ", true},
		{"patch", true},
		{"udiff", true},
		{"unified-diff", true},
		// Non-diff languages must NOT match — a regression here would render
		// code blocks as colored +/- diffs.
		{"go", false},
		{"python", false},
		{"", false},
		{"text", false},
		{"diff-like", false},
	}
	for _, c := range cases {
		if got := isDiffLang(c.lang); got != c.want {
			t.Errorf("isDiffLang(%q) = %v, want %v", c.lang, got, c.want)
		}
	}
}

func TestLooksLikeDiff(t *testing.T) {
	realDiff := `@@ -1,3 +1,4 @@
 unchanged
-old line
+new line
+also new`
	if !looksLikeDiff(realDiff) {
		t.Error("real unified diff with @@ + +/- should match")
	}

	hunkOnlyNoChange := `@@ -1,3 +1,4 @@
 just context
 more context`
	if looksLikeDiff(hunkOnlyNoChange) {
		t.Error("@@ alone without +/- should not match (false positive risk)")
	}

	bulletList := `- first bullet
+ plus bullet
- third bullet`
	if looksLikeDiff(bulletList) {
		t.Error("prose with leading +/- but no @@ hunk should not match")
	}

	emptyCode := ""
	if looksLikeDiff(emptyCode) {
		t.Error("empty code block should not match")
	}

	fileHeadersAndChange := `--- a/file.go
+++ b/file.go
-removed
+added`
	if !looksLikeDiff(fileHeadersAndChange) {
		t.Error("file headers (---/+++) plus +/- changes should match (the +++/--- count as hunk markers)")
	}
}

func TestIsDelimiterRow(t *testing.T) {
	cases := []struct {
		row  string
		want bool
	}{
		{"|---|---|", true},
		{"| --- | --- |", true},
		{"|:--|:--:|--:|", true},
		{"---|---", true},
		{":-:", true},
		{"| a | b |", false},   // real content, not a delimiter
		{"| -- | x |", false},  // a cell with a letter disqualifies it
		{"plain text", false},  // no pipes, no dashes
		{"| | |", false},       // empty cells
	}
	for _, c := range cases {
		if got := isDelimiterRow(c.row); got != c.want {
			t.Errorf("isDelimiterRow(%q) = %v, want %v", c.row, got, c.want)
		}
	}
}

func TestIsTableHeader(t *testing.T) {
	lines := []string{
		"intro",
		"| A | B |",
		"|---|---|",
		"| 1 | 2 |",
		"trailing",
	}
	if !isTableHeader(lines, 1) {
		t.Error("expected line 1 to be a table header")
	}
	if isTableHeader(lines, 0) {
		t.Error("prose line must not be a table header")
	}
	if isTableHeader(lines, 3) {
		t.Error("body row (no delimiter following) must not be a header")
	}
	// A pipe row with no delimiter underneath is not a table.
	if isTableHeader([]string{"| A | B |", "| 1 | 2 |"}, 0) {
		t.Error("pipe row without a delimiter must not be a table header")
	}
}

func TestParseAligns(t *testing.T) {
	got := parseAligns("|:--|:--:|--:|----|")
	want := []int{alignLeft, alignCenter, alignRight, alignLeft}
	if len(got) != len(want) {
		t.Fatalf("parseAligns len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("align[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestCollectTable(t *testing.T) {
	lines := []string{
		"| A | B |",
		"|---|---|",
		"| 1 | 2 |",
		"| 3 | 4 |",
		"",
		"after",
	}
	rows, aligns, consumed := collectTable(lines, 0)
	if consumed != 4 {
		t.Errorf("consumed = %d, want 4 (header+delim+2 body)", consumed)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2 body)", len(rows))
	}
	if rows[0][0] != "A" || rows[2][1] != "4" {
		t.Errorf("unexpected cell contents: %v", rows)
	}
	if len(aligns) != 2 {
		t.Errorf("aligns = %d, want 2", len(aligns))
	}
}

func TestRenderTableFitsWidth(t *testing.T) {
	md := "| Col A | Col B |\n|---|---|\n" +
		"| a very long cell that will need to wrap several times | short |\n"
	out := renderMarkdown(md, 40)
	if !strings.Contains(out, "┌") || !strings.Contains(out, "└") {
		t.Fatal("expected box-drawing frame in table output")
	}
	for _, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 40 {
			t.Errorf("table line exceeds wrap width 40: %d cols: %q", w, ln)
		}
	}
}
