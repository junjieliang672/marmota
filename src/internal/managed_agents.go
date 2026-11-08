// Managed agent panes: the WezTerm panes superfile opened for agents, with one
// shown beside superfile and the rest parked in hidden tabs.
package internal

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
)

const (
	managedAgentHiddenSlot = 0
	managedAgentSlotOne    = 1
	managedAgentSlotTwo    = 2
)

func (m *model) refreshManagedWeztermState() {
	paneID, err := currentWeztermPaneID()
	if err != nil {
		return
	}

	state, err := loadWeztermPaneLayoutState(paneID)
	if err != nil {
		if os.IsNotExist(err) {
			m.setManagedWeztermState(paneID, weztermPaneLayoutState{loaded: true})
		}
		return
	}

	focusPanelDir := m.fileModel.filePanels[m.filePanelFocusIndex].location
	state, err = reconcileManagedAgentSessionsWithWezterm(focusPanelDir, paneID, state)
	if err != nil {
		return
	}
	if len(state.Sessions) > 0 && len(visibleManagedAgentPaneIDs(state.Sessions)) == 0 {
		foregroundState, foregroundErr := m.foregroundManagedAgentSessionByPaneID(
			paneID,
			state,
			state.Sessions[0].PaneID,
		)
		if foregroundErr == nil {
			state = foregroundState
		}
	}

	persistWeztermPaneLayoutState(paneID, state)
	state.loaded = true
	m.setManagedWeztermState(paneID, state)
}

func (m *model) setManagedWeztermState(paneID int, state weztermPaneLayoutState) {
	m.managedWeztermPaneID = paneID
	m.managedWeztermState = state
}

func managedAgentFolderName(cwd string) string {
	base := filepath.Base(cwd)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "?"
	}
	return base
}

func clampManagedAgentSlot(slot int) int {
	switch slot {
	case managedAgentSlotOne:
		return slot
	default:
		return managedAgentHiddenSlot
	}
}

func visibleManagedAgentPaneIDs(sessions []managedAgentSession) []int {
	paneIDs := make([]int, 0, 1)
	for _, session := range sessions {
		if session.VisibleSlot == managedAgentSlotOne {
			paneIDs = append(paneIDs, session.PaneID)
			break
		}
	}
	return paneIDs
}

func normalizeWeztermPaneLayoutState(state weztermPaneLayoutState) weztermPaneLayoutState {
	normalizedSessions, normalizedLegacy := normalizeManagedAgentSessions(
		state.Sessions,
		state.ManagedPaneIDs,
		state.ForegroundPaneID,
	)
	if normalizedLegacy {
		slog.Warn("Normalizing legacy multi-visible managed agent state into a single foreground session")
	}
	state.Sessions = normalizedSessions
	state.ManagedPaneIDs = visibleManagedAgentPaneIDs(normalizedSessions)
	state.ForegroundPaneID = 0
	if len(state.ManagedPaneIDs) > 0 {
		state.ForegroundPaneID = state.ManagedPaneIDs[0]
	}
	return state
}

func normalizeManagedAgentSessions(
	sessions []managedAgentSession,
	fallbackPaneIDs []int,
	foregroundPaneID int,
) ([]managedAgentSession, bool) {
	normalizedLegacy := false
	if len(sessions) == 0 && len(fallbackPaneIDs) > 0 {
		sessions = sessionsFromLegacyPaneIDs(fallbackPaneIDs)
	}

	normalized := make([]managedAgentSession, 0, len(sessions))
	seenPanes := make(map[int]struct{}, len(sessions))
	firstLegacyVisibleIdx := -1
	foregroundIdx := -1

	for _, session := range sessions {
		if session.PaneID == 0 {
			continue
		}
		if _, ok := seenPanes[session.PaneID]; ok {
			continue
		}
		seenPanes[session.PaneID] = struct{}{}

		originalSlot := session.VisibleSlot
		session.VisibleSlot = clampManagedAgentSlot(session.VisibleSlot)
		if originalSlot == managedAgentSlotTwo {
			normalizedLegacy = true
		}
		if session.DisplayName == "" {
			session.DisplayName = managedAgentFolderName(session.Cwd)
		}
		if session.PaneID == foregroundPaneID {
			foregroundIdx = len(normalized)
		}
		if firstLegacyVisibleIdx == -1 && session.VisibleSlot != managedAgentHiddenSlot {
			firstLegacyVisibleIdx = len(normalized)
		}
		session.VisibleSlot = managedAgentHiddenSlot

		normalized = append(normalized, session)
	}

	foregroundIdx = foregroundSessionIndex(foregroundIdx, firstLegacyVisibleIdx, len(normalized))

	for i := range normalized {
		normalized[i].VisibleSlot = managedAgentHiddenSlot
	}
	if foregroundIdx >= 0 {
		normalized[foregroundIdx].VisibleSlot = managedAgentSlotOne
	}

	if firstLegacyVisibleIdx >= 0 && firstLegacyVisibleIdx != foregroundIdx {
		normalizedLegacy = true
	}

	return normalized, normalizedLegacy
}

// sessionsFromLegacyPaneIDs builds sessions for state files written before
// sessions were recorded, where the first pane was the visible one.
func sessionsFromLegacyPaneIDs(paneIDs []int) []managedAgentSession {
	sessions := make([]managedAgentSession, 0, len(paneIDs))
	for i, paneID := range slices.Compact(slices.Clone(paneIDs)) {
		visibleSlot := managedAgentHiddenSlot
		if i == 0 {
			visibleSlot = managedAgentSlotOne
		}
		sessions = append(sessions, managedAgentSession{
			PaneID:      paneID,
			Provider:    managedAgentProviderUnknown,
			VisibleSlot: visibleSlot,
		})
	}
	return sessions
}

// foregroundSessionIndex picks the session to show: the recorded foreground
// one, else the first one an older state file marked visible, else the first.
func foregroundSessionIndex(recorded, legacyVisible, count int) int {
	switch {
	case recorded >= 0:
		return recorded
	case legacyVisible >= 0:
		return legacyVisible
	case count > 0:
		return 0
	default:
		return -1
	}
}
