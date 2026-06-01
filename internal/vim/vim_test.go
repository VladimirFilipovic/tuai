package vim

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
)

// newTA spins up a textarea with the given content and the cursor sent to
// (row, col). Most tests start in Normal mode at the position they care
// about so the assertion can focus on what the keypress did.
func newTA(t *testing.T, content string, row, col int) (*textarea.Model, *Editor) {
	t.Helper()
	ta := textarea.New()
	ta.SetWidth(80)
	ta.SetHeight(10)
	ta.SetValue(content)
	e := New()
	setCursor(&ta, row, col)
	e.EnterNormal(&ta)
	// EnterNormal nudges the cursor one cell left; undo that so the test
	// gets the position it actually asked for.
	setCursor(&ta, row, col)
	return &ta, e
}

func press(ta *textarea.Model, e *Editor, keys ...string) {
	for _, k := range keys {
		e.HandleKey(ta, k)
	}
}

func TestMotionWord(t *testing.T) {
	ta, e := newTA(t, "hello world foo", 0, 0)
	press(ta, e, "w")
	if got := ta.Column(); got != 6 {
		t.Errorf("w: want col=6 (start of 'world'), got col=%d", got)
	}
	press(ta, e, "w")
	if got := ta.Column(); got != 12 {
		t.Errorf("ww: want col=12 (start of 'foo'), got col=%d", got)
	}
}

func TestMotionWordCount(t *testing.T) {
	ta, e := newTA(t, "alpha beta gamma delta", 0, 0)
	press(ta, e, "3", "w")
	if got := ta.Column(); got != 17 {
		t.Errorf("3w: want col=17 (start of 'delta'), got col=%d", got)
	}
}

func TestDeleteWord(t *testing.T) {
	ta, e := newTA(t, "hello world", 0, 0)
	press(ta, e, "d", "w")
	if got := ta.Value(); got != "world" {
		t.Errorf("dw: want value=%q, got %q", "world", got)
	}
}

func TestDeleteLine(t *testing.T) {
	ta, e := newTA(t, "line1\nline2\nline3", 1, 2)
	press(ta, e, "d", "d")
	if got := ta.Value(); got != "line1\nline3" {
		t.Errorf("dd: want value=%q, got %q", "line1\nline3", got)
	}
}

func TestDeleteLineCount(t *testing.T) {
	ta, e := newTA(t, "a\nb\nc\nd\ne", 0, 0)
	press(ta, e, "3", "d", "d")
	if got := ta.Value(); got != "d\ne" {
		t.Errorf("3dd: want value=%q, got %q", "d\ne", got)
	}
}

func TestYankPaste(t *testing.T) {
	ta, e := newTA(t, "abc", 0, 0)
	press(ta, e, "y", "l", "l") // y l → not how vim works; use yw-equivalent
	// reset and use a simpler scenario: yy then p
	ta.SetValue("first\nsecond")
	setCursor(ta, 0, 0)
	e.EnterNormal(ta)
	setCursor(ta, 0, 0)
	press(ta, e, "y", "y")
	press(ta, e, "p")
	if got := ta.Value(); got != "first\nfirst\nsecond" {
		t.Errorf("yyp: want value=%q, got %q", "first\nfirst\nsecond", got)
	}
}

func TestInsertModeAppend(t *testing.T) {
	ta, e := newTA(t, "hi", 0, 0)
	press(ta, e, "A")
	if e.Mode() != ModeInsert {
		t.Errorf("A: want ModeInsert, got %v", e.Mode())
	}
	if got := ta.Column(); got != 2 {
		t.Errorf("A: want col=2 (end of line), got %d", got)
	}
}

func TestEscFallsThroughWhenNoPending(t *testing.T) {
	ta, e := newTA(t, "abc", 0, 0)
	if consumed := e.HandleKey(ta, "esc"); consumed {
		t.Errorf("esc in Normal with no pending state: want fall-through, got consumed")
	}
}

func TestEscConsumedWhenPending(t *testing.T) {
	ta, e := newTA(t, "abc", 0, 0)
	press(ta, e, "d") // sets pending operator
	if consumed := e.HandleKey(ta, "esc"); !consumed {
		t.Errorf("esc with pending operator: want consumed (to cancel), got fall-through")
	}
}

func TestEnterPassesThroughInNormal(t *testing.T) {
	ta, e := newTA(t, "x", 0, 0)
	if consumed := e.HandleKey(ta, "enter"); consumed {
		t.Errorf("enter in Normal: want fall-through so host can send, got consumed")
	}
}

func TestGotoTopBottom(t *testing.T) {
	ta, e := newTA(t, "a\nb\nc\nd", 2, 0)
	press(ta, e, "g", "g")
	if got := ta.Line(); got != 0 {
		t.Errorf("gg: want row=0, got %d", got)
	}
	press(ta, e, "G")
	if got := ta.Line(); got != 3 {
		t.Errorf("G: want row=3, got %d", got)
	}
}

func TestGotoLineWithCount(t *testing.T) {
	// "5G" jumps to line 5 (row 4). Regression for: plain G ignoring count.
	ta, e := newTA(t, "a\nb\nc\nd\ne\nf\ng", 0, 0)
	press(ta, e, "5", "G")
	if got := ta.Line(); got != 4 {
		t.Errorf("5G: want row=4, got %d", got)
	}

	// Out-of-range count clamps to last line.
	ta2, e2 := newTA(t, "a\nb\nc", 0, 0)
	press(ta2, e2, "9", "9", "G")
	if got := ta2.Line(); got != 2 {
		t.Errorf("99G on 3-line buf: want row=2 (last), got %d", got)
	}
}

func TestUnknownGPrefixClearsState(t *testing.T) {
	// 'g' followed by an unknown key must not leave a dangling operator that
	// turns the next keypress into a delete target. After 'd', 'g', 'x',
	// pressing 'l' should move the cursor right — not delete it.
	ta, e := newTA(t, "abc", 0, 0)
	press(ta, e, "d", "g", "x") // 'gx' is unknown; abandons operator
	press(ta, e, "l")           // should be a plain right motion
	if got := ta.Value(); got != "abc" {
		t.Errorf("buffer should be untouched, got %q", got)
	}
	if e.op != 0 {
		t.Errorf("operator should be cleared after abandoned g-prefix, got %q", e.op)
	}
}

func TestPasteLinewiseWithCount(t *testing.T) {
	// Regression: 3p of a yy'd line used to concatenate into one line.
	ta, e := newTA(t, "first\nsecond", 0, 0)
	press(ta, e, "y", "y")    // yank line 0 linewise
	press(ta, e, "3", "p")    // paste 3 times below
	want := "first\nfirst\nfirst\nfirst\nsecond"
	if got := ta.Value(); got != want {
		t.Errorf("yy then 3p:\n  got  %q\n  want %q", got, want)
	}
}

func TestPasteLinewiseMultiLineRegisterWithCount(t *testing.T) {
	// Yank two lines linewise, then 2p — six new lines should appear below.
	ta, e := newTA(t, "a\nb\nc", 0, 0)
	press(ta, e, "2", "y", "y") // yank 2 lines linewise (a, b)
	press(ta, e, "2", "p")      // paste 2 copies below row 0
	// After paste cursor should be at the first inserted line; content:
	// original a, b, c → with a,b yanked and inserted twice after row 0:
	// a / a / b / a / b / b / c
	want := "a\na\nb\na\nb\nb\nc"
	if got := ta.Value(); got != want {
		t.Errorf("2yy then 2p:\n  got  %q\n  want %q", got, want)
	}
}
