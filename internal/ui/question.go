package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// The question panel renders Claude's AskUserQuestion tool call as an
// interactive picker. When a turn ends with an AskUserQuestion invocation the
// panel opens above the input bar; the user moves through the options and the
// confirmed answers are sent back as the next user message (the headless
// `claude -p` turn can't answer the tool directly, so the conversation
// continues with the answer as a normal --resume turn).
//
// Focus rule: the panel only owns navigation keys while the textarea is
// empty. As soon as the user starts typing, every key (including enter)
// belongs to the textarea again — typing a free-form reply and sending it is
// the natural "Other" escape hatch and closes the panel.

const askUserQuestionTool = "askuserquestion"

// isQuestionTool reports whether a tool name is the AskUserQuestion tool.
func isQuestionTool(name string) bool {
	return strings.ToLower(name) == askUserQuestionTool
}

type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type claudeQuestion struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	MultiSelect bool             `json:"multiSelect"`
	Options     []questionOption `json:"options"`
}

type questionModel struct {
	active    bool
	questions []claudeQuestion
	qIdx      int      // question currently being answered
	cursor    int      // highlighted option within the current question
	selected  [][]bool // per question, per option — confirmed picks
}

// parseQuestions decodes an AskUserQuestion tool input into its questions.
// Returns nil when the JSON is incomplete (stream cut off mid-call) or holds
// no usable questions, so callers can simply skip activating the panel.
func parseQuestions(raw string) []claudeQuestion {
	var payload struct {
		Questions []claudeQuestion `json:"questions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil
	}
	var out []claudeQuestion
	for _, q := range payload.Questions {
		if q.Question == "" || len(q.Options) == 0 {
			continue
		}
		out = append(out, q)
	}
	return out
}

// open activates the panel for a parsed question set.
func (q *questionModel) open(questions []claudeQuestion) {
	if len(questions) == 0 {
		return
	}
	q.active = true
	q.questions = questions
	q.qIdx = 0
	q.cursor = 0
	q.selected = make([][]bool, len(questions))
	for i, qu := range questions {
		q.selected[i] = make([]bool, len(qu.Options))
	}
}

func (q *questionModel) close() {
	*q = questionModel{}
}

func (q *questionModel) current() claudeQuestion {
	return q.questions[q.qIdx]
}

func (q *questionModel) moveUp() {
	if q.cursor > 0 {
		q.cursor--
	}
}

func (q *questionModel) moveDown() {
	if q.cursor < len(q.current().Options)-1 {
		q.cursor++
	}
}

// toggle flips the cursor option in a multi-select question. For single
// select it marks the cursor option exclusively.
func (q *questionModel) toggle() {
	sel := q.selected[q.qIdx]
	if q.current().MultiSelect {
		sel[q.cursor] = !sel[q.cursor]
		return
	}
	for i := range sel {
		sel[i] = i == q.cursor
	}
}

// pick selects option n (0-based) directly. Single-select questions treat a
// number press as pick-and-confirm; multi-select treats it as a toggle. The
// bool reports whether n addressed a real option.
func (q *questionModel) pick(n int) bool {
	if n < 0 || n >= len(q.current().Options) {
		return false
	}
	q.cursor = n
	q.toggle()
	return true
}

// confirm locks in the current question and advances. For single-select with
// nothing marked yet, the cursor option counts as the pick (enter-to-choose).
// Returns the composed answer message and true once every question is
// answered; otherwise "" and false.
func (q *questionModel) confirm() (string, bool) {
	sel := q.selected[q.qIdx]
	any := false
	for _, v := range sel {
		any = any || v
	}
	if !any {
		sel[q.cursor] = true
	}
	if q.qIdx < len(q.questions)-1 {
		q.qIdx++
		q.cursor = 0
		return "", false
	}
	return q.composeAnswer(), true
}

// composeAnswer renders the confirmed picks as the reply message sent back to
// Claude — one "Header: label, label" line per question.
func (q *questionModel) composeAnswer() string {
	var lines []string
	for i, qu := range q.questions {
		var labels []string
		for j, opt := range qu.Options {
			if q.selected[i][j] {
				labels = append(labels, opt.Label)
			}
		}
		if len(labels) == 0 {
			continue
		}
		head := qu.Header
		if head == "" {
			head = qu.Question
		}
		lines = append(lines, head+": "+strings.Join(labels, ", "))
	}
	return strings.Join(lines, "\n")
}

// height reports the rows the panel occupies so relayout can size the
// viewport around it. Must mirror view()'s line count exactly.
func (q *questionModel) height(width int) int {
	if !q.active {
		return 0
	}
	// header (1) + wrapped question lines + one row per option.
	return 1 + len(q.questionLines(width)) + len(q.current().Options)
}

func (q *questionModel) questionLines(width int) []string {
	wrapped := wordwrapKeepBlank(q.current().Question, max(width-6, 20))
	return strings.Split(wrapped, "\n")
}

func (q *questionModel) view(width int) string {
	if !q.active {
		return ""
	}
	t := CurrentTheme()
	accent := lipgloss.NewStyle().Foreground(t.Accent()).Bold(true)
	dim := lipgloss.NewStyle().Foreground(t.Dim())
	pick := lipgloss.NewStyle().Foreground(t.User()).Bold(true)

	cur := q.current()
	var b strings.Builder

	// Header: tool glyph, progress, and the short header chip.
	progress := ""
	if len(q.questions) > 1 {
		progress = fmt.Sprintf(" %d/%d", q.qIdx+1, len(q.questions))
	}
	head := "  ❓ " + accent.Render("Claude asks"+progress)
	if cur.Header != "" {
		head += dim.Render(" · " + cur.Header)
	}
	if cur.MultiSelect {
		head += dim.Render(" · multi-select")
	}
	b.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(head) + "\n")

	for _, ln := range q.questionLines(width) {
		b.WriteString("  " + lipgloss.NewStyle().MaxWidth(width-2).Render(ln) + "\n")
	}

	sel := q.selected[q.qIdx]
	for i, opt := range cur.Options {
		marker := "  "
		if i == q.cursor {
			marker = accent.Render("▶ ")
		}
		box := ""
		if cur.MultiSelect {
			if sel[i] {
				box = pick.Render("[x] ")
			} else {
				box = dim.Render("[ ] ")
			}
		} else if sel[i] {
			box = pick.Render("● ")
		}
		num := dim.Render(fmt.Sprintf("%d ", i+1))
		label := opt.Label
		if i == q.cursor {
			label = accent.Render(label)
		}
		row := "  " + marker + num + box + label
		if opt.Description != "" {
			row += dim.Render(" — " + opt.Description)
		}
		b.WriteString(lipgloss.NewStyle().MaxWidth(width).Render(row) + "\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
