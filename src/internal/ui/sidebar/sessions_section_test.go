package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
