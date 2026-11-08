package internal

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// V pins the Viewer to a session: the preview keeps showing the session the
// Sessions box is on, live, while you browse files. In files V pins the
// session the Ask box writes to; in the Sessions box, the selected one, and
// goes back to the files. V again, W, or the session ending unpins it.

// viewerPinned reports whether the preview shows the pinned session rather
// than the cursor's file.
func (m *model) viewerPinned() bool {
	return m.dock.pinned && m.fileModel.filePreview.IsOpen()
}

func (m *model) togglePinViewer() tea.Cmd {
	if m.dock.pinned {
		m.unpinViewer()
		return m.requestFilePreviewRender(true)
	}
	if !m.fileModel.filePreview.IsOpen() {
		m.flashClaude("The preview is hidden: " + firstKey(common.Hotkeys.ToggleFilePreviewPanel) + " shows it")
		return nil
	}
	if m.focusPanel == dockSessionsFocus {
		if m.sessions.list.NewSelected() || m.sessions.list.SelectedID() == "" {
			m.flashClaude("Pick a session to pin")
			return nil
		}
		m.dock.pinned = true
		m.leaveDockSessions()
		return m.sessionsSelectionChanged()
	}
	id := m.dockAskTarget()
	if id == "" {
		m.flashClaude("No live sessions")
		return nil
	}
	m.refreshSessionsList()
	m.sessions.list.Select(id)
	m.dock.lastID = id
	m.dock.pinned = true
	return m.sessionsSelectionChanged()
}

// unpinViewer gives the preview back to the cursor's file.
func (m *model) unpinViewer() {
	m.dock.pinned = false
	if m.focusPanel != dockSessionsFocus && m.focusPanel != dockAskFocus {
		m.forgetClaudeQuestion()
	}
}

// keepViewerPin unpins the Viewer when its session is gone.
func (m *model) keepViewerPin() {
	if !m.dock.pinned {
		return
	}
	if s, ok := m.selectedSession(); !ok || !s.Live {
		m.unpinViewer()
	}
}
