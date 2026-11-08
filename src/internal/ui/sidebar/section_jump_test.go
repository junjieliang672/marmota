package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSidebarSectionJumpWrapsBetweenPinnedAndRecent(t *testing.T) {
	s := Model{
		directories: formSidebarSections(dirSlice(2), dirSlice(2)),
		cursor:      1,
	}

	s.JumpToNextSection(20)
	assert.Equal(t, 4, s.cursor)

	s.JumpToPreviousSection(20)
	assert.Equal(t, 1, s.cursor)
}

func TestSidebarSectionJumpSkipsEmptySection(t *testing.T) {
	s := Model{
		directories: formSidebarSections(dirSlice(2), nil),
		cursor:      1,
	}

	s.JumpToNextSection(20)
	assert.Equal(t, 1, s.cursor)
}
