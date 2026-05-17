package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyMsg builds a synthetic KeyPressMsg for a printable code with optional
// modifiers. Text mirrors what most terminals would send alongside (just the
// code as a string), which is what triggers msg.String() to drop the modifier.
func keyMsg(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod, Text: string(code)})
}

// keyMsgNoText is like keyMsg but with Text="". Control-character keys
// (ctrl+p, ctrl+c, …) arrive without printable text.
func keyMsgNoText(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}

func TestOpenPaletteKey_CmdP_Super(t *testing.T) {
	// On Kitty-protocol terminals (Ghostty, etc.) Cmd reports as ModSuper +
	// printable 'p' Text. msg.String() returns "p", so we must match via
	// Keystroke().
	msg := keyMsg('p', tea.ModSuper)
	if !openPaletteKey(msg) {
		t.Errorf("Cmd+P (ModSuper) should open palette; String()=%q Keystroke()=%q",
			msg.String(), msg.Keystroke())
	}
}

func TestOpenPaletteKey_CmdP_Meta(t *testing.T) {
	// Some macOS terminals report Cmd as ModMeta.
	msg := keyMsg('p', tea.ModMeta)
	if !openPaletteKey(msg) {
		t.Errorf("Cmd+P (ModMeta) should open palette; String()=%q Keystroke()=%q",
			msg.String(), msg.Keystroke())
	}
}

func TestOpenPaletteKey_CtrlP(t *testing.T) {
	// Universal fallback. ctrl+p is a control character so Text is empty.
	msg := keyMsgNoText('p', tea.ModCtrl)
	if !openPaletteKey(msg) {
		t.Errorf("Ctrl+P should open palette; String()=%q Keystroke()=%q",
			msg.String(), msg.Keystroke())
	}
}

func TestOpenPaletteKey_PlainPDoesNot(t *testing.T) {
	// Typing 'p' must not open the palette — that would make the chat
	// useless.
	msg := keyMsg('p', 0)
	if openPaletteKey(msg) {
		t.Errorf("plain 'p' must not open palette")
	}
}

func TestOpenPaletteKey_ShiftPDoesNot(t *testing.T) {
	// Capital P with no other modifier — same: just a typed character.
	msg := keyMsg('p', tea.ModShift)
	if openPaletteKey(msg) {
		t.Errorf("shift+p must not open palette; Keystroke()=%q", msg.Keystroke())
	}
}

func TestOpenPaletteKey_AltPDoesNot(t *testing.T) {
	// alt+p is sometimes bound by terminals (π on macOS); we don't want
	// it triggering the palette.
	msg := keyMsg('p', tea.ModAlt)
	if openPaletteKey(msg) {
		t.Errorf("alt+p must not open palette; Keystroke()=%q", msg.Keystroke())
	}
}

func TestOpenPaletteKey_OtherKeysDoNot(t *testing.T) {
	for _, r := range []rune{'a', 'k', 'q', 'P'} {
		msg := keyMsg(r, tea.ModSuper)
		if openPaletteKey(msg) {
			t.Errorf("super+%c should not open palette; Keystroke()=%q", r, msg.Keystroke())
		}
	}
}
