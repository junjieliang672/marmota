package sidebar

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func TestTheSessionsSitBetweenPinnedAndRecent(t *testing.T) {
	s := Model{}
	s.SetSessions([]SessionEntry{{ID: "a", Title: "fix tests"}, {ID: "b", Title: "write docs"}})
	pinned := []directory{{Location: "/p", Name: "p"}}
	recent := []directory{{Location: "/r1", Name: "r1"}, {Location: "/r2", Name: "r2"}}
	s.directories = s.withSessions(formSidebarSections(pinned, recent), "")

	names := []string{}
	for _, d := range s.directories {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"", "p", "", "fix tests", "write docs", "", "r1", "r2"}, names)

	start, end := s.pinnedIndexRange()
	assert.Equal(t, [2]int{1, 1}, [2]int{start, end})
	start, end = s.sessionsIndexRange()
	assert.Equal(t, [2]int{3, 4}, [2]int{start, end})
	start, end = s.recentEditsIndexRange()
	assert.Equal(t, [2]int{6, 7}, [2]int{start, end})
	start, end = s.indexedItemsRange()
	assert.Equal(t, [2]int{1, 7}, [2]int{start, end}, "numbered from the pin to the last recent")
	assert.Equal(t, 2, sidebarIndexedDisplayNumber(s.directories, 3), "the first session is 2")
	assert.Equal(t, 4, sidebarIndexedDisplayNumber(s.directories, 6), "Recent numbers on after them")
	assert.Equal(t, []int{1, 3, 6}, s.sectionStartIndexes())
}

func TestASessionWithRemoteControlIsMarked(t *testing.T) {
	nerdfont, width := common.Config.Nerdfont, common.SidebarWidth
	t.Cleanup(func() { common.Config.Nerdfont, common.SidebarWidth = nerdfont, width })
	common.Config.Nerdfont, common.SidebarWidth = false, 30
	s := Model{}
	s.SetSessions([]SessionEntry{
		{ID: "a", Title: "a long title that runs past the popup's edge", Remote: true},
		{ID: "b", Title: "write docs", Codex: true},
	})
	s.directories = s.withSessions(formSidebarSections(nil, nil), "")
	at := func(id string) int {
		return slices.IndexFunc(s.directories, func(d directory) bool { return d.Location == SessionLocationPrefix+id })
	}

	remote := ansi.Strip(s.directoryLine(at("a"), s.numbering(), "", false))
	assert.True(t, strings.HasSuffix(remote, "… rc"), "the name gives way to the mark: %q", remote)
	assert.LessOrEqual(t, ansi.StringWidth(remote), common.SidebarWidth)
	assert.False(t, strings.HasSuffix(ansi.Strip(s.directoryLine(at("b"), s.numbering(), "", false)), " rc"))
}
