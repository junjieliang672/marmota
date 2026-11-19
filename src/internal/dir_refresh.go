package internal

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// dirRefreshMsg is a panel's folder read again in the background: the
// listing a panel already has is checked for changes made outside Marmota
// about once a second, and reading a folder (its files' details, iCloud's
// status of each) takes the screen's loop 10 to 20 ms.
type dirRefreshMsg struct {
	panel    int
	location string
	dotFile  bool
	sort     sortOptionsModelData
	elements []element
}

// refreshesInBackground is whether that check reads in the background.
// Tests read at once, to look at the panel right after.
var refreshesInBackground = true //nolint:gochecknoglobals // test seam

// plainListing reports whether a panel shows its folder as it is (no
// search, no filter), the listing dirElements keeps.
func (panel *filePanel) plainListing() bool {
	return panel.searchStack.IsEmpty() && panel.searchBar.Value() == "" && len(panel.recursiveBaseElems) == 0
}

// refreshDirInBackground starts reading panel i's folder again, unless it
// is being read already. The panel keeps its listing meanwhile.
func (m *model) refreshDirInBackground(i int, now time.Time) {
	panel := &m.fileModel.filePanels[i]
	panel.lastTimeGetElement = now
	if panel.refreshing {
		return
	}
	panel.refreshing = true
	location, dotFile, sort := panel.location, m.toggleDotFile, panel.sortOptions.data
	m.dirRefreshes = append(m.dirRefreshes, func() tea.Msg {
		return dirRefreshMsg{panel: i, location: location, dotFile: dotFile, sort: sort,
			elements: returnDirElement(location, dotFile, sort)}
	})
}

// takeDirRefreshes is the folder reads started since it was last asked.
func (m *model) takeDirRefreshes() tea.Cmd {
	cmds := m.dirRefreshes
	m.dirRefreshes = nil
	return tea.Batch(cmds...)
}

// dirRefreshed puts a folder read in the background in its panel, when
// the panel still shows that folder as it was read.
func (m *model) dirRefreshed(msg dirRefreshMsg) {
	if msg.panel >= len(m.fileModel.filePanels) {
		return
	}
	panel := &m.fileModel.filePanels[msg.panel]
	panel.refreshing = false
	data := panel.sortOptions.data
	if !panel.elementCacheValid || !panel.plainListing() || panel.location != msg.location ||
		panel.cachedLocation != msg.location || m.toggleDotFile != msg.dotFile ||
		data.selected != msg.sort.selected || data.reversed != msg.sort.reversed {
		return // it changed meanwhile, and was read then
	}
	panel.cachedElements = msg.elements
	panel.element = iCloudFiltered(panel, msg.elements)
	panel.scrollToCursor(m.mainPanelHeight)
}
