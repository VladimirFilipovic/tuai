package toolview

import (
	"image/color"
	"strings"
	"testing"
)

// testDeps wires plain, theme-free hooks so the renderers can be exercised
// without the ui package — the whole point of the Deps injection.
func testDeps() Deps {
	return Deps{
		Accent: color.White,
		Dim:    color.White,
		Icon:   func(name string) string { return "*" },
		// Identity highlight; diff renderer just prefixes a marker so tests can
		// confirm the edit path routed through it.
		Highlight: func(code, _ string) string { return code },
		Diff:      func(code string, _ int) string { return "DIFF{" + code + "}" },
	}
}

func TestRenderBash(t *testing.T) {
	out := Render("Bash", `{"command":"ls -la","description":"list"}`, 80, testDeps())
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing tool name: %q", out)
	}
	if !strings.Contains(out, "ls -la") {
		t.Errorf("missing command: %q", out)
	}
	if !strings.Contains(out, "list") {
		t.Errorf("missing description: %q", out)
	}
}

func TestRenderEditRoutesThroughDiff(t *testing.T) {
	out := Render("Edit", `{"file_path":"a.go","old_string":"foo","new_string":"bar"}`, 80, testDeps())
	if !strings.Contains(out, "a.go") {
		t.Errorf("missing path: %q", out)
	}
	if !strings.Contains(out, "DIFF{") {
		t.Errorf("edit should route through the injected Diff renderer: %q", out)
	}
	if !strings.Contains(out, "-foo") || !strings.Contains(out, "+bar") {
		t.Errorf("diff body should hold the - / + lines: %q", out)
	}
}

func TestRenderPartialJSONFallsBackToPreview(t *testing.T) {
	// Mid-stream input isn't valid JSON yet; render a dim raw preview.
	out := Render("Write", `{"file_path":"x.go","content":"package m`, 80, testDeps())
	if !strings.Contains(out, "file_path") {
		t.Errorf("partial preview should show raw text: %q", out)
	}
}

func TestRenderNoInput(t *testing.T) {
	out := Render("Read", "", 80, testDeps())
	if strings.Contains(out, "\n") {
		t.Errorf("empty input should yield just the header line: %q", out)
	}
	if !strings.Contains(out, "Read") {
		t.Errorf("missing tool name: %q", out)
	}
}
