package internal

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/junjieliang672/marmota/src/internal/common"
)

const invalidTypeString = "InvalidType"

// reset the items slice and set the cut value
func (c *copyItems) reset(cut bool) {
	c.cut = cut
	c.items = c.items[:0]
}

// ================ Model related utils =======================

// Non fatal Validations. This indicates bug / programming errors, not user configuration mistake
func (m *model) validateLayout() error {
	l := m.lay
	if l.body+m.statusRows() != m.fullHeight {
		return fmt.Errorf(
			"invalid model layout, fullHeight : %v, rows above the status line : %v, statusLineHeight : %v",
			m.fullHeight, l.body, m.statusRows())
	}
	if m.fileModel.width < 0 {
		return fmt.Errorf("invalid file panel width: %v (negative)", m.fileModel.width)
	}
	// The sidebar's boxes fill it, and so do the boxes right of it.
	left := l.details.h
	for _, p := range l.panels {
		left += p.h
	}
	if right := l.preview.h + l.ask.h + l.sessions.h; left != l.body || right != l.body {
		return fmt.Errorf("invalid model layout, rows above the status line : %v, the sidebar's : %v, right of it : %v",
			l.body, left, right)
	}
	if l.sidebar.w+l.sessions.w != m.fullWidth {
		return fmt.Errorf("invalid model layout, fullWidth : %v, sidebar : %v, right of it : %v",
			m.fullWidth, l.sidebar.w, l.sessions.w)
	}
	return nil
}

// ================ filepanel

func filePanelSlice(paths []string) []filePanel {
	res := make([]filePanel, len(paths))
	for i := range paths {
		// Making the first panel as the focussed
		isFocus := i == 0
		res[i] = defaultFilePanel(paths[i], isFocus)
	}
	return res
}

func defaultFilePanel(path string, focused bool) filePanel {
	targetFile := ""
	panelPath := path
	// If path refers to a file, switch to its parent and remember the filename
	if stat, err := os.Stat(panelPath); err == nil && !stat.IsDir() {
		targetFile = filepath.Base(panelPath)
		panelPath = filepath.Dir(panelPath)
	}

	panel := filePanel{
		render:   0,
		cursor:   0,
		location: panelPath,
		sortOptions: sortOptionsModel{
			//nolint:mnd // default sort options dimensions
			width: 20,
			//nolint:mnd // default sort options dimensions
			height: 4,
			open:   false,
			cursor: common.Config.DefaultSortType,
			data: sortOptionsModelData{
				options: []string{
					string(sortingName), string(sortingSize),
					string(sortingDateModified), string(sortingFileType),
				},
				selected: common.Config.DefaultSortType,
				reversed: common.Config.SortOrderReversed,
			},
		},
		panelMode:        browserMode,
		isFocused:        focused,
		selected:         make(map[string]struct{}),
		directoryRecords: make(map[string]directoryRecord),
		searchBar:        common.GenerateSearchBar(),
		targetFile:       targetFile,
	}
	panel.followFolderSort()
	return panel
}

// ================ String method for easy logging =====================

func (f focusPanelType) String() string {
	switch f {
	case nonePanelFocus:
		return "nonePanelFocus"
	case metadataFocus:
		return "metadataFocus"
	case dockSessionsFocus:
		return "dockSessionsFocus"
	case dockAskFocus:
		return "dockAskFocus"
	default:
		return invalidTypeString
	}
}

func (p panelMode) String() string {
	switch p {
	case selectMode:
		return "selectMode"
	case browserMode:
		return "browserMode"
	default:
		return invalidTypeString
	}
}
