package zoxide

func (m *Model) navigateUp() {
	if len(m.results) == 0 {
		return
	}
	m.picked = true
	if m.cursor > 0 {
		m.cursor--
	} else {
		m.cursor = len(m.results) - 1 // Wrap to bottom
	}
	m.updateRenderIndex()
}

func (m *Model) navigateDown() {
	if len(m.results) == 0 {
		return
	}
	m.picked = true
	if m.cursor < len(m.results)-1 {
		m.cursor++
	} else {
		m.cursor = 0 // Wrap to top
	}
	m.updateRenderIndex()
}

func (m *Model) updateRenderIndex() {
	if len(m.results) == 0 {
		m.renderIndex = 0
		return
	}

	// If cursor is above visible range, scroll up
	if m.cursor < m.renderIndex {
		m.renderIndex = m.cursor
	}

	// If cursor is below visible range, scroll down
	if m.cursor >= m.renderIndex+m.visibleResults() {
		m.renderIndex = m.cursor - m.visibleResults() + 1
	}

	// Ensure renderIndex is within bounds
	if m.renderIndex < 0 {
		m.renderIndex = 0
	}
	maxRenderIndex := len(m.results) - m.visibleResults()
	if maxRenderIndex < 0 {
		maxRenderIndex = 0
	}
	if m.renderIndex > maxRenderIndex {
		m.renderIndex = maxRenderIndex
	}
}

// visibleResults is how many entries fit in the list area.
func (m *Model) visibleResults() int {
	return max(1, m.maxHeight-listChrome)
}
