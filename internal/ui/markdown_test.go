package ui

import "testing"

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
