package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/vladafilipovic/claudetui/internal/claude"
	"github.com/vladafilipovic/claudetui/internal/ui"
)

func main() {
	if err := claude.NewClient().CheckAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprintln(os.Stderr, "install Claude Code: https://docs.claude.com/en/docs/claude-code")
		os.Exit(1)
	}

	p := tea.NewProgram(ui.NewApp())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
