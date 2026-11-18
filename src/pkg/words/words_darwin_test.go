//go:build darwin && cgo

package words

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wordsOf is text's words, as text.
func wordsOf(text string) []string {
	runes := []rune(text)
	var out []string
	for _, w := range Bounds(text) {
		out = append(out, string(runes[w[0]:w[1]]))
	}
	return out
}

// A Chinese sentence has no spaces, and its characters are letters: only
// the system's tokenizer tells its words apart.
func TestTheSystemFindsTheWordsOfChinese(t *testing.T) {
	assert.Equal(t, []string{"我", "认为", "应该", "这样"}, wordsOf("我认为应该这样"))
	assert.Equal(t, []string{"在", "askbar", "用", "marmotim", "输入", "英文"}, wordsOf("在askbar用marmotim输入英文"))
	assert.Equal(t, []string{"收起", "micro", "的", "这个", "功能"}, wordsOf("收起micro的这个功能，"),
		"punctuation is in no word")
	assert.Equal(t, []string{"plain", "words", "too"}, wordsOf("plain words, too"))
	assert.Empty(t, wordsOf(""))
	assert.Empty(t, wordsOf("  ，。 "))
}

// Words are counted in characters, also after one that takes two of the
// units the tokenizer counts in.
func TestWordsAreCountedInCharacters(t *testing.T) {
	text := "😀 hello 世界"
	found := Bounds(text)
	require.GreaterOrEqual(t, len(found), 2)
	assert.Equal(t, [][2]int{{2, 7}, {8, 10}}, found[len(found)-2:])
	assert.Equal(t, []string{"hello", "世界"}, wordsOf(text)[len(found)-2:])
}

func field(text string) textinput.Model {
	in := textinput.New()
	in.Focus()
	in.SetValue(text)
	in.CursorEnd()
	return in
}

var (
	wordLeft    = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true} //nolint:gochecknoglobals // keys
	wordRight   = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true} //nolint:gochecknoglobals // keys
	deleteLeft  = tea.KeyMsg{Type: tea.KeyCtrlW}                                //nolint:gochecknoglobals // keys
	deleteRight = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d"), Alt: true} //nolint:gochecknoglobals // keys
)

func TestAFieldMovesByTheWordsOfChinese(t *testing.T) {
	in := field("我认为应该这样，enter 进入")
	var stops []int
	for range 6 {
		in, _ = UpdateField(in, wordLeft)
		stops = append(stops, in.Position())
	}
	// before 进入, enter, 这样 (over the comma), 应该, 认为, 我
	assert.Equal(t, []int{14, 8, 5, 3, 1, 0}, stops)

	stops = nil
	for range 3 {
		in, _ = UpdateField(in, wordRight)
		stops = append(stops, in.Position())
	}
	assert.Equal(t, []int{1, 3, 5}, stops, "after 我, 认为, 应该")
}

// What deleting a word takes is what moving by a word goes over: from
// every place in a text, in both directions.
func TestDeletingAWordTakesWhatMovingByAWordGoesOver(t *testing.T) {
	text := []rune("我认为应该这样，enter 进入claude app之后  如果拿不到。")
	for pos := 0; pos <= len(text); pos++ {
		moved := field(string(text))
		moved.SetCursor(pos)
		moved, _ = UpdateField(moved, wordLeft)
		deleted := field(string(text))
		deleted.SetCursor(pos)
		deleted, _ = UpdateField(deleted, deleteLeft)
		to := moved.Position()
		require.Equal(t, string(text[:to])+string(text[pos:]), deleted.Value(), "to the left from %d", pos)
		require.Equal(t, to, deleted.Position(), "the cursor from %d", pos)

		moved = field(string(text))
		moved.SetCursor(pos)
		moved, _ = UpdateField(moved, wordRight)
		deleted = field(string(text))
		deleted.SetCursor(pos)
		deleted, _ = UpdateField(deleted, deleteRight)
		to = moved.Position()
		require.Equal(t, string(text[:pos])+string(text[to:]), deleted.Value(), "to the right from %d", pos)
		require.Equal(t, pos, deleted.Position(), "the cursor from %d", pos)
	}
}

func TestAFieldTakesOtherKeysAsItDid(t *testing.T) {
	in := field("你好")
	in, _ = UpdateField(in, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("吗")})
	assert.Equal(t, "你好吗", in.Value())
	in, _ = UpdateField(in, tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "你好", in.Value())

	// One that hides what is written, as a password's, keeps its own rule.
	hidden := field("秘密 口令")
	hidden.EchoMode = textinput.EchoPassword
	hidden, _ = UpdateField(hidden, deleteLeft)
	assert.Empty(t, hidden.Value())
}
