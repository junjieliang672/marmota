package internal

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// The mouse wheel scrolls the preview under the pointer: the file preview,
// down and sideways, or the session it shows, which wraps its lines and so
// only scrolls down. It also scrolls the
// Details box at the foot, without giving it the keyboard, and the help
// menu while it is open. Everything else takes
// nothing from the mouse, the file panels and the session list included
// (the user, 2026-11-06). Mouse reporting stays on all the same (see
// restoreMouseReporting).

// wheelLines is how far a turn of the wheel scrolls a preview, wheelColumns
// how far a sideways one does (vim's mousescroll).
const (
	wheelLines   = 3
	wheelColumns = 6
)

type previewArea int

const (
	noPreview previewArea = iota
	filePreviewArea
	sessionPreviewArea
	detailsArea
)

// filePreviewDocMsg brings the document the file preview scrolls through,
// drawn in the background, its rows docWidth wide.
type filePreviewDocMsg struct {
	location string
	width    int
	doc      []string
	docWidth int
}

// handleMouse scrolls the preview under the pointer with the wheel: down and
// up, and sideways with a trackpad's sideways scroll or shift. The rest of
// the mouse does nothing.
func (m *model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress || !m.firstLoadingComplete {
		return nil
	}
	lines, cols, ok := wheelScroll(msg)
	if !ok {
		return nil
	}
	if m.helpMenu.open {
		// The help menu sits over everything; the wheel scrolls it.
		if lines != 0 {
			m.helpMenuScrollBy(lines)
		}
		return nil
	}
	if m.overlayOpen() {
		return nil
	}
	switch m.previewUnder(msg.X, msg.Y) {
	case filePreviewArea:
		return m.scrollFilePreview(lines, cols)
	case sessionPreviewArea:
		m.scrollSessionsPaneBy(-lines) // the session scrolls back, to older turns, when positive
	case detailsArea:
		m.details.ScrollBy(lines)
	case noPreview:
	}
	return nil
}

// wheelScroll is how far a wheel event scrolls: lines down, cols right.
func wheelScroll(msg tea.MouseMsg) (int, int, bool) {
	switch msg.Button { //nolint:exhaustive // only the wheel scrolls
	case tea.MouseButtonWheelUp:
		if msg.Shift {
			return 0, -wheelColumns, true
		}
		return -wheelLines, 0, true
	case tea.MouseButtonWheelDown:
		if msg.Shift {
			return 0, wheelColumns, true
		}
		return wheelLines, 0, true
	case tea.MouseButtonWheelLeft:
		return 0, -wheelColumns, true
	case tea.MouseButtonWheelRight:
		return 0, wheelColumns, true
	}
	return 0, 0, false
}

// previewUnder is the preview drawn at the screen cell x, y, from 0. WezTerm
// sends a trackpad's sideways scroll to the focused pane, wherever the
// pointer is: a cell outside superfile's screen is no preview.
func (m *model) previewUnder(x, y int) previewArea {
	switch {
	case m.fullHeight < common.MinimumHeight || m.fullWidth < common.MinimumWidth:
		return noPreview // a warning about the size is all there is
	case x < 0 || y < 0 || x >= m.fullWidth || y >= m.fullHeight:
		return noPreview
	}
	pv := &m.fileModel.filePreview
	if pv.IsOpen() && x >= m.fullWidth-pv.GetWidth() && y < pv.GetHeight() {
		if m.previewShowsASession() {
			return sessionPreviewArea // the preview shows the session
		}
		return filePreviewArea
	}
	if m.toggleFooter {
		// The dock sits right under the panels: Sessions, then Details.
		top := m.mainPanelHeight + common.BorderPadding
		left := m.dockSessionsWidth()
		if !m.dockAskOpen() && y >= top && y < top+m.footerHeight+common.BorderPadding &&
			x >= left && x < left+m.detailsWidth() {
			return detailsArea
		}
	}
	return noPreview
}

// scrollFilePreview scrolls the file preview by lines, down when positive,
// and by cols, right when positive. The first scroll down or right has its
// document drawn in the background, and scrolls once it is.
func (m *model) scrollFilePreview(lines, cols int) tea.Cmd {
	pv := &m.fileModel.filePreview
	if pv.ScrollBy(lines, cols) {
		return nil
	}
	location, width, ok := pv.RequestDoc(lines, cols)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		doc, docWidth := pv.RenderDoc(location, width)
		return filePreviewDocMsg{location: location, width: width, doc: doc, docWidth: docWidth}
	}
}

// overlayOpen reports whether a modal is over the screen: the wheel does
// nothing then.
func (m *model) overlayOpen() bool {
	panel := m.getFocusedFilePanel()
	return m.helpMenu.open || m.promptModal.IsOpen() || m.zoxideModal.IsOpen() || panel.sortOptions.open ||
		panel.iCloudFilter.open || m.firstUse || m.typingModal.open || m.notifyModel.IsOpen()
}
