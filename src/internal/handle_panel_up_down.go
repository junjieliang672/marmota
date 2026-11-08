package internal

// ======================================== File panel controller ========================================

func (panel *filePanel) scrollToCursor(mainPanelHeight int) {
	if panel.cursor < 0 || panel.cursor >= len(panel.element) {
		panel.cursor = 0
		panel.render = 0
		return
	}

	renderCount := panel.elementHeight(mainPanelHeight)
	if panel.cursor < panel.render {
		panel.render = max(0, panel.cursor-renderCount+1)
	} else if panel.cursor > panel.render+renderCount-1 {
		panel.render = panel.cursor - renderCount + 1
	}
}

// Control file panel list up
func (panel *filePanel) listUp(mainPanelHeight int) {
	if len(panel.element) == 0 {
		return
	}
	if panel.cursor > 0 {
		panel.cursor--
		if panel.cursor < panel.render {
			panel.render--
		}
	} else {
		if len(panel.element) > panel.elementHeight(mainPanelHeight) {
			panel.render = len(panel.element) - panel.elementHeight(mainPanelHeight)
			panel.cursor = len(panel.element) - 1
		} else {
			panel.cursor = len(panel.element) - 1
		}
	}
}

// Control file panel list down
func (panel *filePanel) listDown(mainPanelHeight int) {
	if len(panel.element) == 0 {
		return
	}
	if panel.cursor < len(panel.element)-1 {
		panel.cursor++
		if panel.cursor > panel.render+panel.elementHeight(mainPanelHeight)-1 {
			panel.render++
		}
	} else {
		panel.render = 0
		panel.cursor = 0
	}
}

// Go to top of list
func (panel *filePanel) listTop() {
	panel.cursor = 0
	panel.render = 0
}

// Go to bottom of list
func (panel *filePanel) listBottom(mainPanelHeight int) {
	if len(panel.element) == 0 {
		return
	}
	panel.cursor = len(panel.element) - 1
	panHeight := panel.elementHeight(mainPanelHeight)
	if len(panel.element) > panHeight {
		panel.render = len(panel.element) - panHeight
	} else {
		panel.render = 0
	}
}

func (panel *filePanel) jumpToIndex(target int, mainPanelHeight int) {
	if len(panel.element) == 0 {
		return
	}

	if target <= 0 {
		target = 1
	}
	if target > len(panel.element) {
		target = len(panel.element)
	}

	panel.cursor = target - 1
	panel.scrollToCursor(mainPanelHeight)
}

func (panel *filePanel) pgUp(mainPanelHeight int) {
	panlen := len(panel.element)
	if panlen == 0 {
		return
	}

	panHeight := panel.elementHeight(mainPanelHeight)
	panCenter := panHeight / 2 //nolint:mnd // For making sure the cursor is at the center of the panel

	if panHeight >= panlen {
		panel.cursor = 0
	} else {
		if panel.cursor-panHeight <= 0 {
			panel.cursor = 0
			panel.render = 0
		} else {
			panel.cursor -= panHeight
			panel.render = panel.cursor - panCenter

			if panel.render < 0 {
				panel.render = 0
			}
		}
	}
}

func (panel *filePanel) pgDown(mainPanelHeight int) {
	panlen := len(panel.element)
	if panlen == 0 {
		return
	}

	panHeight := panel.elementHeight(mainPanelHeight)
	panCenter := panHeight / 2 //nolint:mnd // For making sure the cursor is at the center of the panel

	if panHeight >= panlen {
		panel.cursor = panlen - 1
	} else {
		if panel.cursor+panHeight >= panlen {
			panel.cursor = panlen - 1
			panel.render = panel.cursor - panCenter
		} else {
			panel.cursor += panHeight
			panel.render = panel.cursor - panCenter
		}
	}

	// Validate render position bounds
	maxRender := max(len(panel.element)-panHeight, 0)
	if panel.render > maxRender {
		panel.render = maxRender
	}
	if panel.render < 0 {
		panel.render = 0
	}
}

// Handles the action of selecting an item in the file panel upwards. (only work on select mode)
// This basically just toggles the "selected" status of element that is pointed by the cursor
// and then moves the cursor up
// TODO : Add unit tests for itemSelectUp and singleItemSelect
func (panel *filePanel) itemSelectUp(mainPanelHeight int) {
	panel.listUp(mainPanelHeight)
	panel.singleItemSelect()
}

// Handles the action of selecting an item in the file panel downwards. (only work on select mode)
func (panel *filePanel) itemSelectDown(mainPanelHeight int) {
	panel.listDown(mainPanelHeight)
	panel.singleItemSelect()
}

// ======================================== Metadata controller ========================================

// ======================================== Processbar controller ========================================
