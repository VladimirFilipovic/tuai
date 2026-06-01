package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Streamer is the slice of *Client the chat UI depends on. Defining it lets
// tests drive the chat model with a fake that returns canned chunks instead
// of spawning a real `claude` subprocess. *Client satisfies it.
type Streamer interface {
	// Stream invokes one user turn and returns a channel of chunks.
	Stream(ctx context.Context, prompt, resumeID string) <-chan Chunk
	// Model returns the best display name for the active/selected model.
	Model() string
	// ModelRaw returns the user-selected alias ("" if none).
	ModelRaw() string
	// ActiveModel returns the model system.init last reported ("" until then).
	ActiveModel() string
	// SetModel changes the user-selected model alias.
	SetModel(string)
	// Cwd is the working directory the subprocess runs in.
	Cwd() string
}

// Client wraps the `claude` CLI as a streaming subprocess. Each Stream call
// invokes `claude -p ... --output-format stream-json` once. Multi-turn lives
// inside Claude Code's own session store via --resume <id>.
type Client struct {
	bin    string
	model  string // user-selected alias ("" = let claude pick)
	active string // last model reported by `system.init`
	cwd    string
}

func NewClient() *Client {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		bin = "claude"
	}
	cwd, _ := os.Getwd()
	return &Client{
		bin:   bin,
		model: os.Getenv("CLAUDE_MODEL"), // empty = let claude pick
		cwd:   cwd,
	}
}

// CheckAvailable returns nil if the claude binary is on PATH.
func (c *Client) CheckAvailable() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("%q not found on PATH (set CLAUDE_BIN to override)", c.bin)
	}
	return nil
}

// Model returns the best name to show in the UI: the user-selected alias if
// set, otherwise the model `system.init` last reported, otherwise a generic
// placeholder.
func (c *Client) Model() string {
	if c.model != "" {
		return c.model
	}
	if c.active != "" {
		return c.active
	}
	return "default"
}

// ActiveModel is the model `system.init` last reported (the resolved model
// claude actually used). Empty until the first stream lands.
func (c *Client) ActiveModel() string { return c.active }

func (c *Client) ModelRaw() string { return c.model }

func (c *Client) SetModel(m string) { c.model = m }

func (c *Client) Bin() string { return c.bin }
func (c *Client) Cwd() string { return c.cwd }

// Chunk is a single event emitted by the streaming pipeline. Most chunks
// carry only Text. The first chunk of a fresh session also sets SessionID.
// ToolUse chunks announce a tool invocation by name.
type Chunk struct {
	Text      string
	Thinking  string // streamed thinking text
	ToolUse   string // tool name, non-empty when a tool call begins
	ToolInput string // streamed tool input JSON fragments
	SessionID string // populated from `system.init`
	Model     string // populated from `system.init`
	Cost      float64
	DurMs     int64
	Done      bool
	Err       error
}

// Stream invokes `claude` for one user turn. If resumeID is non-empty the
// existing session is continued. Caller reads the returned channel until it
// closes; pass a cancellable context to abort.
func (c *Client) Stream(ctx context.Context, prompt, resumeID string) <-chan Chunk {
	ch := make(chan Chunk, 128)

	// The prompt arrives already shaped by the caller (e.g. @-file expansion
	// lives in internal/prompt). This wrapper only drives the subprocess.
	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--input-format", "text",
		"--verbose",
		"--include-partial-messages",
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Dir = c.cwd
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		go func() { ch <- Chunk{Err: err, Done: true}; close(ch) }()
		return ch
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		go func() { ch <- Chunk{Err: err, Done: true}; close(ch) }()
		return ch
	}

	if err := cmd.Start(); err != nil {
		go func() { ch <- Chunk{Err: err, Done: true}; close(ch) }()
		return ch
	}

	// Drain stderr in background — surface it only if the process fails.
	var errBuf strings.Builder
	var errMu sync.Mutex
	go func() {
		b, _ := io.ReadAll(stderr)
		errMu.Lock()
		errBuf.Write(b)
		errMu.Unlock()
	}()

	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)

		st := &streamState{}
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			c.dispatchLine(ctx, line, ch, st)
		}
		// Surface scanner errors (e.g. bufio.ErrTooLong on a >32MB line, or
		// a mid-stream read failure). Without this a truncated stream looks
		// indistinguishable from a clean EOF and the user sees the turn just
		// stop with no error.
		if scanErr := scanner.Err(); scanErr != nil && ctx.Err() == nil {
			_ = cmd.Wait() // reap the child; output already truncated
			send(ctx, ch, Chunk{Err: fmt.Errorf("stream read: %w", scanErr), Done: true})
			return
		}

		// Wait for process to exit; surface stderr if non-zero.
		waitErr := cmd.Wait()
		if waitErr != nil && ctx.Err() == nil {
			errMu.Lock()
			tail := strings.TrimSpace(errBuf.String())
			errMu.Unlock()
			msg := waitErr.Error()
			if tail != "" {
				msg = fmt.Sprintf("%s: %s", waitErr, lastLine(tail))
			}
			// Non-blocking: if the consumer has stopped reading (e.g. the
			// UI already moved on), drop the final chunk rather than leak
			// this goroutine on a full buffer.
			select {
			case ch <- Chunk{Err: errors.New(msg), Done: true}:
			default:
			}
			return
		}
		if ctx.Err() != nil {
			// Consumer cancelled — they already know. Try to deliver but
			// never block: defer close(ch) signals end-of-stream regardless.
			select {
			case ch <- Chunk{Err: ctx.Err(), Done: true}:
			default:
			}
			return
		}
	}()

	return ch
}

// streamState carries the per-Stream bookkeeping dispatchLine needs across
// lines. An agentic turn emits several text content blocks separated by
// tool_use blocks; without tracking we'd glue the end of one text block onto
// the start of the next ("…flow first.Stack's up."). We remember whether any
// text has been emitted and how many newlines it trailed with, so a new text
// block can be prefixed with just enough newlines to make a clean paragraph
// break.
type streamState struct {
	sawText    bool
	trailingNL int
}

// dispatchLine parses a single stream-json line and forwards relevant events.
func (c *Client) dispatchLine(ctx context.Context, line []byte, ch chan<- Chunk, st *streamState) {
	var ev streamLine
	if err := json.Unmarshal(line, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			if ev.Model != "" {
				c.active = ev.Model
			}
			send(ctx, ch, Chunk{SessionID: ev.SessionID, Model: ev.Model})
		}

	case "stream_event":
		switch ev.Event.Type {
		case "content_block_delta":
			switch ev.Event.Delta.Type {
			case "text_delta":
				if ev.Event.Delta.Text != "" {
					send(ctx, ch, Chunk{Text: ev.Event.Delta.Text})
					st.sawText = true
					st.trailingNL = updateTrailingNL(st.trailingNL, ev.Event.Delta.Text)
				}
			case "thinking_delta":
				if ev.Event.Delta.Thinking != "" {
					send(ctx, ch, Chunk{Thinking: ev.Event.Delta.Thinking})
				}
			case "input_json_delta":
				if ev.Event.Delta.PartialJSON != "" {
					send(ctx, ch, Chunk{ToolInput: ev.Event.Delta.PartialJSON})
				}
			}
		case "content_block_start":
			switch ev.Event.ContentBlock.Type {
			case "tool_use":
				if ev.Event.ContentBlock.Name != "" {
					send(ctx, ch, Chunk{ToolUse: ev.Event.ContentBlock.Name})
				}
			case "text":
				// A new text block after earlier text: inject however many
				// newlines are still needed to reach a blank-line separator,
				// so the two blocks read as distinct paragraphs.
				if st.sawText && st.trailingNL < 2 {
					sep := strings.Repeat("\n", 2-st.trailingNL)
					send(ctx, ch, Chunk{Text: sep})
					st.trailingNL = 2
				}
			}
		}

	case "result":
		send(ctx, ch, Chunk{
			Done:  true,
			Cost:  ev.TotalCostUSD,
			DurMs: ev.DurationMS,
			Err:   resultErr(ev),
		})
	}
}

// updateTrailingNL returns the count of consecutive '\n' at the end of the
// emitted text after appending s. If s is entirely newlines the run continues
// from cur; otherwise it restarts at s's own trailing newline run.
func updateTrailingNL(cur int, s string) int {
	n := 0
	for i := len(s) - 1; i >= 0 && s[i] == '\n'; i-- {
		n++
	}
	if n == len(s) {
		return cur + n
	}
	return n
}

func resultErr(ev streamLine) error {
	if !ev.IsError {
		return nil
	}
	if ev.APIErrorStatus != "" {
		return fmt.Errorf("claude error: %s", ev.APIErrorStatus)
	}
	if ev.Result != "" {
		return fmt.Errorf("claude error: %s", ev.Result)
	}
	return errors.New("claude reported an error")
}

func send(ctx context.Context, ch chan<- Chunk, c Chunk) {
	select {
	case ch <- c:
	case <-ctx.Done():
	}
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// --- stream-json schema (only the fields we read) ---

type streamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// stream_event payload
	Event streamInner `json:"event"`

	// result payload
	IsError        bool    `json:"is_error,omitempty"`
	APIErrorStatus string  `json:"api_error_status,omitempty"`
	Result         string  `json:"result,omitempty"`
	TotalCostUSD   float64 `json:"total_cost_usd,omitempty"`
	DurationMS     int64   `json:"duration_ms,omitempty"`
}

type streamInner struct {
	Type         string      `json:"type"`
	Delta        deltaPart   `json:"delta"`
	ContentBlock contentPart `json:"content_block"`
}

type deltaPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type contentPart struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}
