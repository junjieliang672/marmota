package words

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The words of "ab, cd  ef": ab at 0-2, cd at 4-6, ef at 8-10.
var three = [][2]int{{0, 2}, {4, 6}, {8, 10}} //nolint:gochecknoglobals // a test's table

func TestMovingLeftGoesToTheStartOfTheNearestWordBefore(t *testing.T) {
	for pos, want := range map[int]int{
		10: 8, // from the end: the last word's start
		9:  8, // from inside a word: its start
		8:  4, // from a word's start: the word before, over the spaces
		7:  4,
		6:  4,
		4:  0, // over the comma and the space
		2:  0,
		1:  0,
		0:  0,
	} {
		assert.Equal(t, want, leftOf(three, pos), "from %d", pos)
	}
	assert.Equal(t, 0, leftOf(nil, 5), "a text without words: its start")
}

func TestMovingRightGoesToTheEndOfTheNearestWordAfter(t *testing.T) {
	for pos, want := range map[int]int{
		0:  2,
		1:  2,
		2:  6, // from a word's end: the next word's, over the comma and the space
		3:  6,
		5:  6,
		6:  10,
		9:  10,
		10: 12, // past the last word: the text's end
	} {
		assert.Equal(t, want, rightOf(three, pos, 12), "from %d", pos)
	}
	assert.Equal(t, 7, rightOf(nil, 5, 7), "a text without words: its end")
}
