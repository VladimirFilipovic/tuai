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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

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

	// Expand @file references in the prompt by inlining file contents.
	// Claude Code's `-p` mode does not auto-resolve @ refs the way interactive
	// mode does, so we inline them ourselves.
	expanded := expandAtRefs(prompt, c.cwd)

	args := []string{
		"-p", expanded,
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

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			c.dispatchLine(line, ch, ctx)
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
			ch <- Chunk{Err: errors.New(msg), Done: true}
			return
		}
		if ctx.Err() != nil {
			ch <- Chunk{Err: ctx.Err(), Done: true}
			return
		}
	}()

	return ch
}

// dispatchLine parses a single stream-json line and forwards relevant events.
func (c *Client) dispatchLine(line []byte, ch chan<- Chunk, ctx context.Context) {
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
			if ev.Event.ContentBlock.Type == "tool_use" && ev.Event.ContentBlock.Name != "" {
				send(ctx, ch, Chunk{ToolUse: ev.Event.ContentBlock.Name})
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

var atRefRe = regexp.MustCompile(`(^|\s)@([^\s]+)`)

func isImageExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// expandAtRefs scans the prompt for @-references (e.g. `@foo.go`, `@~/notes.md`,
// `@./src/x.py`) and appends the file contents at the end of the prompt as
// fenced attachment blocks. Unresolved refs are left as plain text.
func expandAtRefs(prompt, cwd string) string {
	matches := atRefRe.FindAllStringSubmatch(prompt, -1)
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
			atts.WriteString(fmt.Sprintf("\n\n[attached image: %s]\n", path))
			continue
		}
		if info.Size() > maxBytes {
			atts.WriteString(fmt.Sprintf("\n\n--- @%s (skipped: %d bytes > %d limit) ---\n",
				ref, info.Size(), maxBytes))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		atts.WriteString(fmt.Sprintf("\n\n--- @%s ---\n%s\n--- end @%s ---\n",
			ref, string(data), ref))
	}
	if atts.Len() == 0 {
		return prompt
	}
	return prompt + atts.String()
}

// --- stream-json schema (only the fields we read) ---

type streamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	// stream_event payload
	Event streamInner `json:"event,omitempty"`

	// result payload
	IsError        bool    `json:"is_error,omitempty"`
	APIErrorStatus string  `json:"api_error_status,omitempty"`
	Result         string  `json:"result,omitempty"`
	TotalCostUSD   float64 `json:"total_cost_usd,omitempty"`
	DurationMS     int64   `json:"duration_ms,omitempty"`
}

type streamInner struct {
	Type         string      `json:"type"`
	Delta        deltaPart   `json:"delta,omitempty"`
	ContentBlock contentPart `json:"content_block,omitempty"`
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
