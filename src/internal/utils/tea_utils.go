package utils

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TeaRuneKeyMsg creates a KeyMsg for simple rune keys (single characters).
// For modifier keys like "ctrl+c", use TeaKeyMsg instead.
func TeaRuneKeyMsg(msg string) tea.KeyMsg {
	return tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(msg),
	}
}

// TeaKeyMsg creates a proper KeyMsg from a hotkey string like "ctrl+c", "alt+a", or "enter".
// This handles modifier keys correctly for testing.
func TeaKeyMsg(hotkey string) tea.KeyMsg {
	// Store original for rune keys (case-sensitive)
	originalHotkey := hotkey
	// Lowercase for comparing special keys and modifiers
	lowerHotkey := strings.ToLower(hotkey)

	// Handle ctrl+letter combinations: Ctrl+A to Ctrl+Z are the control
	// codes 1 to 26, in order.
	if letter, ok := strings.CutPrefix(lowerHotkey, "ctrl+"); ok && len(letter) == 1 &&
		letter[0] >= 'a' && letter[0] <= 'z' {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(letter[0]-'a')}
	}

	// Handle alt+letter combinations
	if strings.HasPrefix(lowerHotkey, "alt+") {
		letter := strings.TrimPrefix(originalHotkey, "alt+")
		if len(letter) == 1 {
			return tea.KeyMsg{
				Type:  tea.KeyRunes,
				Runes: []rune(letter),
				Alt:   true,
			}
		}
	}

	// Handle special keys
	switch lowerHotkey {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "esc", "escape":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyShiftUp}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyShiftDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup", "pageup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown", "pagedown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}

	// Any other key Bubble Tea has a name for, such as shift+tab or f2, and
	// with alt held.
	if t, ok := namedKeys[lowerHotkey]; ok {
		return tea.KeyMsg{Type: t}
	}
	if name, ok := strings.CutPrefix(lowerHotkey, "alt+"); ok {
		if t, ok := namedKeys[name]; ok {
			return tea.KeyMsg{Type: t, Alt: true}
		}
	}

	// Default: treat as rune key (preserve original case)
	return tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune(originalHotkey),
	}
}

// namedKeys are the keys Bubble Tea names, by the name it gives them.
var namedKeys = func() map[string]tea.KeyType { //nolint:gochecknoglobals // a table, built once
	const first, last = -200, 127 // Bubble Tea's key types lie between these
	names := map[string]tea.KeyType{}
	for t := tea.KeyType(first); t <= last; t++ {
		if t == tea.KeyRunes {
			continue
		}
		if name := (tea.KeyMsg{Type: t}).String(); name != "" {
			names[name] = t
		}
	}
	return names
}()
