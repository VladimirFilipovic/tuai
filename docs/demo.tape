# VHS tape — short showcase of tuai.
# Run with: vhs docs/demo.tape    (needs `vhs` on PATH and built ./tuai)
#
# The tape backs up ~/.config/tuai/config.json, swaps in a Monokai theme for
# punchier syntax colors during recording, and restores the original on quit.

Output docs/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 700
Set Padding 16
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms

# Hidden: back up config, swap in monokai, cd to repo, launch tuai.
# A bash EXIT trap restores the original even if vhs aborts mid-run.
Hide
Type `_cfg=$HOME/.config/tuai/config.json; mkdir -p "$(dirname "$_cfg")"; [ -f "$_cfg" ] && cp "$_cfg" "$_cfg.bak" || rm -f "$_cfg.bak"; trap '[ -f "$_cfg.bak" ] && mv "$_cfg.bak" "$_cfg" || rm -f "$_cfg"' EXIT; printf '{"theme":"monokai"}' > "$_cfg"; cd /Users/vladafilipovic/Documents/claudetui && clear && ./tuai`
Enter
Sleep 1800ms
Show

# Fresh session.
Type "n"
Sleep 700ms

# A prompt that exercises TodoWrite + Write + Read in one shot.
Type "make a short todo list to plan, then write a tiny go hello-world to /tmp/tuai-demo.go, then read it back to confirm"
Sleep 300ms
Enter

# Wait for the multi-tool turn (todos, write, read, final answer).
Sleep 28s

# Let the final assistant line breathe, then exit.
Sleep 1500ms
Escape
Sleep 300ms
Type "q"
Sleep 300ms
