package internal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenLinkRequest(t *testing.T) {
	paste := func(s string) tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true}
	}
	cases := []struct {
		name string
		msg  tea.Msg
		path string
		line int
		ok   bool
	}{
		{"with a line", paste(openLinkMark + "/a/b c.go\n12"), "/a/b c.go", 12, true},
		{"without a line", paste(openLinkMark + "/a/b.go\n"), "/a/b.go", 0, true},
		{"no path", paste(openLinkMark + "\n3"), "", 0, false},
		{"a paste of your own", paste("/a/b.go\n12"), "", 0, false},
		{"typed, not pasted", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(openLinkMark + "/a")}, "", 0, false},
	}
	for _, c := range cases {
		path, line, ok := openLinkRequest(c.msg)
		if path != c.path || line != c.line || ok != c.ok {
			t.Errorf("%s: got %q %d %v, want %q %d %v", c.name, path, line, ok, c.path, c.line, c.ok)
		}
	}
}
