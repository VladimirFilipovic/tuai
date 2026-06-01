// Package clipboard reads images from the OS clipboard and writes them to
// disk so they can be referenced by path in a chat prompt.
//
// Terminal apps can't reliably receive image bytes via paste — most
// terminals don't transmit binary clipboard data at all. So instead of
// hooking the paste stream we expose a "paste image" action that pulls the
// image directly from the OS clipboard and saves it where Claude can read
// it. The caller then inserts the returned path as an @-reference.
package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SaveImageToFile pulls a PNG image from the OS clipboard, writes it to a
// temp file under ~/.claude/tuai-images (created on demand), and returns
// the absolute path. Returns an error with no side effects if the clipboard
// does not contain an image.
func SaveImageToFile() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return saveDarwin()
	case "linux":
		return saveLinux()
	default:
		return "", fmt.Errorf("image paste not supported on %s", runtime.GOOS)
	}
}

func cacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "tuai-images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func newImagePath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("paste-%d.png", time.Now().UnixNano())), nil
}

// saveDarwin uses AppleScript to coerce the pasteboard to PNG and write it
// out. Returns a "no image on clipboard" error if the coercion fails.
func saveDarwin() (string, error) {
	path, err := newImagePath()
	if err != nil {
		return "", err
	}
	script := fmt.Sprintf(`
		try
			set pngData to the clipboard as «class PNGf»
		on error
			return "no-image"
		end try
		set outFile to open for access POSIX file %q with write permission
		try
			set eof outFile to 0
			write pngData to outFile
			close access outFile
		on error errMsg
			try
				close access outFile
			end try
			return "write-failed: " & errMsg
		end try
		return "ok"
	`, path)
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return "", fmt.Errorf("osascript: %w", err)
	}
	result := string(out)
	// osascript appends a newline.
	if len(result) > 0 && result[len(result)-1] == '\n' {
		result = result[:len(result)-1]
	}
	switch result {
	case "ok":
		return path, nil
	case "no-image":
		return "", fmt.Errorf("no image on clipboard")
	default:
		return "", fmt.Errorf("clipboard read failed: %s", result)
	}
}

// saveLinux tries `wl-paste` (Wayland) first, then `xclip` (X11). The PNG is
// streamed directly to the destination file via cmd.Stdout — never buffered
// into Go memory — so multi-hundred-MB clipboard images don't blow up the
// TUI's RSS. Returns "no image on clipboard" only when no tool exists; tool
// failures surface as descriptive errors so the user can act on them.
func saveLinux() (string, error) {
	hasWl := lookPathOK("wl-paste")
	hasXc := lookPathOK("xclip")
	if !hasWl && !hasXc {
		return "", fmt.Errorf("no image on clipboard (install wl-paste or xclip)")
	}

	if hasWl {
		path, err := pasteToFile("wl-paste", []string{"--type", "image/png"})
		switch {
		case err == nil:
			return path, nil
		case isNoClipboardData(err):
			// fall through to X11 — Wayland may not have the selection but
			// xclip might, when running under XWayland.
		default:
			return "", fmt.Errorf("wl-paste: %w", err)
		}
	}
	if hasXc {
		path, err := pasteToFile("xclip", []string{"-selection", "clipboard", "-t", "image/png", "-o"})
		switch {
		case err == nil:
			return path, nil
		case isNoClipboardData(err):
			return "", fmt.Errorf("no image on clipboard")
		default:
			return "", fmt.Errorf("xclip: %w", err)
		}
	}
	return "", fmt.Errorf("no image on clipboard")
}

// pasteToFile runs the clipboard-read tool with its stdout piped straight to
// a fresh image file. Removes the file on any failure so we never leave a
// zero-byte / half-written PNG behind. Returns ErrNoData when the tool ran
// successfully but produced no bytes — distinguishes "empty clipboard" from
// "tool crashed".
func pasteToFile(bin string, args []string) (string, error) {
	path, err := newImagePath()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(path)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = f
	if runErr := cmd.Run(); runErr != nil {
		cleanup()
		return "", runErr
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	st, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if st.Size() == 0 {
		_ = os.Remove(path)
		return "", errNoClipboardData
	}
	return path, nil
}

var errNoClipboardData = fmt.Errorf("no clipboard data")

func isNoClipboardData(err error) bool { return err == errNoClipboardData }

// WriteText puts s on the OS clipboard. Uses the platform's native clipboard
// tool (pbcopy on macOS; wl-copy then xclip on Linux) by piping s to its
// stdin. Returns an error with no side effects when no tool is available, so
// the caller can fall back to an OSC52 escape (tea.SetClipboard).
func WriteText(s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		switch {
		case lookPathOK("wl-copy"):
			cmd = exec.Command("wl-copy")
		case lookPathOK("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard")
		default:
			return fmt.Errorf("no clipboard tool found (install wl-copy or xclip)")
		}
	default:
		return fmt.Errorf("clipboard copy not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func lookPathOK(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// IsImagePath returns true if the path has an extension Claude can read as
// an image attachment.
func IsImagePath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}
