package ask

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/pkg/words"
)

// option+← and option+backspace in the Ask box go by the system's words: a
// Chinese sentence isn't one word, and deleting takes what moving goes over.
func TestTheAskBoxMovesAndDeletesByTheWordsOfChinese(t *testing.T) {
	if !words.Supported() {
		t.Skip("this system isn't asked for words")
	}
	c := NewComposer()
	c.Focus()
	c.SetValue("我认为应该这样，enter 进入")

	left := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}
	c.HandleKey(left)
	c.HandleKey(left)
	c.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	assert.Equal(t, "我认为应该这样，Xenter 进入", c.Value(), "two words to the left")

	c.SetValue("我认为应该这样，enter 进入")
	deleteWord := tea.KeyMsg{Type: tea.KeyCtrlW} // option+backspace, as WezTerm sends it
	c.HandleKey(deleteWord)
	assert.Equal(t, "我认为应该这样，enter ", c.Value())
	c.HandleKey(deleteWord)
	c.HandleKey(deleteWord)
	assert.Equal(t, "我认为应该", c.Value(), "the comma goes with the word before it")

	// A line break in the question parts words as a space does.
	c.SetValue("第一行\n进入")
	c.HandleKey(deleteWord)
	assert.Equal(t, "第一行\n", c.Value())
}
