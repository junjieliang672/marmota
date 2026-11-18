package words

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// UpdateField is a text field's Update, with moving and deleting by word
// (option+← →, option+backspace, and option+delete to the right) going by
// the system's words. The field's own rule is what lies between two spaces,
// a whole sentence in Chinese. Where the system isn't asked for words, and
// for any other key, it is the field's own Update.
func UpdateField(in textinput.Model, msg tea.Msg) (textinput.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || !Supported() || !in.Focused() || in.EchoMode != textinput.EchoNormal {
		return in.Update(msg)
	}
	value, pos := []rune(in.Value()), in.Position()
	switch {
	case key.Matches(k, in.KeyMap.WordForward):
		in.SetCursor(Right(value, pos))
	case key.Matches(k, in.KeyMap.WordBackward):
		in.SetCursor(Left(value, pos))
	case key.Matches(k, in.KeyMap.DeleteWordBackward):
		cut(&in, value, Left(value, pos), pos)
	case key.Matches(k, in.KeyMap.DeleteWordForward):
		cut(&in, value, pos, Right(value, pos))
	default:
		return in.Update(msg)
	}
	return in, nil
}

// cut takes the characters from from up to to out of the field, and leaves
// the cursor where they were.
func cut(in *textinput.Model, value []rune, from, to int) {
	if from >= to {
		return
	}
	rest := make([]rune, 0, len(value)-(to-from))
	rest = append(rest, value[:from]...)
	rest = append(rest, value[to:]...)
	in.SetValue(string(rest))
	in.SetCursor(from)
}
