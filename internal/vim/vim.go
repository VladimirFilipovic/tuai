// Package vim adds a small modal-editing layer on top of charm.land/bubbles
// v2 textarea. Only Normal + Insert modes are implemented; Visual and
// command-line modes are deliberately out of scope. The motion/operator
// state machine follows the same prefix-map idea used by vimtea
// (github.com/kujtimiihoxha/vimtea), but instead of replacing the textarea
// we treat it as a buffer we drive: Value/SetValue + cursor positioning.
//
// Wiring contract (see internal/ui/chat.go):
//   - On every key, the host calls e.HandleKey(ta, keyString). The returned
//     bool says whether the key was consumed — the host must skip its own
//     textarea.Update for that key when true.
//   - Insert mode always returns false (the textarea handles the key).
//     The single exception is "esc", which Insert mode consumes to switch
//     back to Normal.
//   - The textarea is passed as a pointer on every call rather than
//     captured on the Editor; bubbles models routinely flow by value
//     through Update() so a stored pointer goes stale on the first frame.
//   - The host is also responsible for surfacing the current mode in its
//     own chrome (status indicator, cursor shape, etc.).
package vim

import "charm.land/bubbles/v2/textarea"

type Mode int

const (
	ModeInsert Mode = iota
	ModeNormal
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeNormal:
		return "NORMAL"
	}
	return "?"
}

// Editor holds the modal state. One per chat session.
type Editor struct {
	mode        Mode
	countBuf    string // digit-accumulator for things like "5j" or "d3w"
	op          rune   // pending operator: 'd', 'y', 'c', or 0
	opCount     int    // count captured at the time the operator was set
	keyBuf      string // multi-key prefix accumulator (currently only "g")
	register    string // unnamed yank register
	regLinewise bool   // whether register holds whole lines (changes p behavior)
	undoStack   []snapshot
	redoStack   []snapshot
}

type snapshot struct {
	value    string
	row, col int
}

func New() *Editor {
	return &Editor{mode: ModeInsert}
}

func (e *Editor) Mode() Mode { return e.mode }

// HasPending reports whether a partial command is buffered (count digits,
// pending operator, or a g-prefix waiting for its second key). Used by
// hosts that want to swallow esc only when there's something to clear.
func (e *Editor) HasPending() bool {
	return e.countBuf != "" || e.op != 0 || e.keyBuf != ""
}

// snap pushes the current buffer+cursor onto the undo stack. Call this
// *before* a mutating op so `u` rolls back to the pre-op state.
func (e *Editor) snap(ta *textarea.Model) {
	row, col := cursor(ta)
	e.undoStack = append(e.undoStack, snapshot{value: ta.Value(), row: row, col: col})
	e.redoStack = nil
}

func (e *Editor) undo(ta *textarea.Model) {
	if len(e.undoStack) == 0 {
		return
	}
	row, col := cursor(ta)
	e.redoStack = append(e.redoStack, snapshot{value: ta.Value(), row: row, col: col})
	last := e.undoStack[len(e.undoStack)-1]
	e.undoStack = e.undoStack[:len(e.undoStack)-1]
	ta.SetValue(last.value)
	setCursor(ta, last.row, last.col)
}

func (e *Editor) redoOp(ta *textarea.Model) {
	if len(e.redoStack) == 0 {
		return
	}
	row, col := cursor(ta)
	e.undoStack = append(e.undoStack, snapshot{value: ta.Value(), row: row, col: col})
	last := e.redoStack[len(e.redoStack)-1]
	e.redoStack = e.redoStack[:len(e.redoStack)-1]
	ta.SetValue(last.value)
	setCursor(ta, last.row, last.col)
}

// EnterNormal switches to Normal mode and follows vim's convention of
// nudging the cursor one cell left (since Insert sat after the last typed
// char and Normal sits on it).
func (e *Editor) EnterNormal(ta *textarea.Model) {
	if e.mode == ModeNormal {
		e.resetPending()
		return
	}
	e.mode = ModeNormal
	e.resetPending()
	row, col := cursor(ta)
	if col > 0 {
		setCursor(ta, row, col-1)
	}
}

func (e *Editor) enterInsert() {
	e.mode = ModeInsert
	e.resetPending()
}

func (e *Editor) resetPending() {
	e.countBuf = ""
	e.op = 0
	e.opCount = 0
	e.keyBuf = ""
}
