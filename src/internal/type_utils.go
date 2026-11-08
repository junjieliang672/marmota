package internal

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
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
	if 0 < m.footerHeight && m.footerHeight < common.MinFooterHeight {
		return fmt.Errorf("footerHeight %v is too small", m.footerHeight)
	}
	if !m.toggleFooter && m.footerHeight != 0 {
		return fmt.Errorf("footer closed and footerHeight %v is non zero", m.footerHeight)
	}

	// Calculate status line height
	statusLineHeight := 0
	if common.Config.ShowStatusLine && m.statusLineModel.IsEnabled() {
		statusLineHeight = m.statusLineModel.GetHeight()
	}

	// PanelHeight + 2 lines (main border) + actual footer height + status line height
	expectedHeight := (m.mainPanelHeight + common.BorderPadding) + utils.FullFooterHeight(
		m.footerHeight,
		m.toggleFooter,
	) + statusLineHeight
	if m.fullHeight != expectedHeight {
		return fmt.Errorf(
			"invalid model layout, fullHeight : %v, mainPanelHeight : %v, footerHeight : %v, statusLineHeight : %v",
			m.fullHeight,
			m.mainPanelHeight,
			m.footerHeight,
			statusLineHeight,
		)
	}

	// Validate width calculations
	if m.fileModel.width < 0 {
		return fmt.Errorf("invalid file panel width: %v (negative)", m.fileModel.width)
	}

	// Validate total width calculation
	expectedMainWidth := (m.fileModel.width * len(m.fileModel.filePanels)) +
		m.fileModel.filePreview.GetWidth() +
		(len(m.fileModel.filePanels)-1)*common.BorderPadding +
		common.BorderPadding

	// Allow small discrepancies due to remainder (up to numPanels-1 pixels)
	widthDiff := m.fullWidth - expectedMainWidth
	if widthDiff < 0 || widthDiff >= len(m.fileModel.filePanels) {
		slog.Warn("Width calculation mismatch",
			"fullWidth", m.fullWidth,
			"expectedMainWidth", expectedMainWidth,
			"diff", widthDiff)
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
	case sidebarFocus:
		return "sidebarFocus"
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
