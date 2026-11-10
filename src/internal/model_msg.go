package internal

import (
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/ui/metadata"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
)

// gitRefreshDebounceMsg is sent after a delay to trigger git status refresh
type gitRefreshDebounceMsg struct{}

// gitRefreshDebounceDelay is the delay before refreshing git status after file operations
const gitRefreshDebounceDelay = 500 * time.Millisecond

// scheduleGitRefresh schedules a debounced git status refresh if not already pending
func scheduleGitRefresh(m *model) tea.Cmd {
	if m.gitRefreshPending {
		return nil
	}
	m.gitRefreshPending = true
	return tea.Tick(gitRefreshDebounceDelay, func(t time.Time) tea.Msg {
		return gitRefreshDebounceMsg{}
	})
}

// metadataDebounceMsg is sent after a delay to trigger metadata refresh
type metadataDebounceMsg struct{}

// metadataDebounceDelay is the delay before fetching metadata after cursor movement
// This prevents excessive I/O during rapid scrolling
const metadataDebounceDelay = 100 * time.Millisecond

// scheduleMetadataRefresh schedules a debounced metadata fetch if not already pending
func scheduleMetadataRefresh(m *model) tea.Cmd {
	if m.metadataRefreshPending || m.fileModel.renaming {
		return nil
	}
	m.metadataRefreshPending = true
	return tea.Tick(metadataDebounceDelay, func(t time.Time) tea.Msg {
		return metadataDebounceMsg{}
	})
}

type previewDebounceMsg struct{}

// previewDebounceDelay is the delay before starting a preview render after cursor movement.
// This prevents rapid file navigation from queueing expensive PDF/image preview work.
const previewDebounceDelay = 75 * time.Millisecond

func schedulePreviewRefresh(m *model) tea.Cmd {
	if m.previewRefreshPending || m.fileModel.renaming || !m.fileModel.filePreview.IsOpen() {
		return nil
	}
	m.previewRefreshPending = true
	return tea.Tick(previewDebounceDelay, func(t time.Time) tea.Msg {
		return previewDebounceMsg{}
	})
}

// heartbeatMsg arrives every second so the model refreshes file panels and
// other state even when no key is pressed, e.g. after an agent adds files.
type heartbeatMsg struct{}

const heartbeatInterval = time.Second

func scheduleHeartbeat() tea.Cmd {
	return tea.Tick(heartbeatInterval, func(time.Time) tea.Msg {
		return heartbeatMsg{}
	})
}

type ModelUpdateMessage interface {
	ApplyToModel(m *model) tea.Cmd
	GetReqID() int
}

type BaseMessage struct {
	reqID int
}

func (msg BaseMessage) GetReqID() int {
	return msg.reqID
}

type PasteOperationMsg struct {
	BaseMessage

	state processbar.ProcessState
}

func NewPasteOperationMsg(state processbar.ProcessState, reqID int) PasteOperationMsg {
	return PasteOperationMsg{
		state: state,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg PasteOperationMsg) ApplyToModel(m *model) tea.Cmd {
	if msg.state == processbar.Successful && m.copyItems.cut {
		m.copyItems.reset(false)
	}
	// Invalidate element cache so panels pick up new/moved files
	if msg.state == processbar.Successful || msg.state == processbar.Failed {
		for i := range m.fileModel.filePanels {
			m.fileModel.filePanels[i].invalidateElementCache()
		}
		return scheduleGitRefresh(m)
	}
	return nil
}

type DeleteOperationMsg struct {
	BaseMessage

	state processbar.ProcessState
}

func NewDeleteOperationMsg(state processbar.ProcessState, reqID int) DeleteOperationMsg {
	return DeleteOperationMsg{
		state: state,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg DeleteOperationMsg) ApplyToModel(m *model) tea.Cmd {
	// Remove selection
	m.getFocusedFilePanel().resetSelected()
	// Invalidate element cache so panels pick up deleted files
	if msg.state == processbar.Successful || msg.state == processbar.Failed {
		for i := range m.fileModel.filePanels {
			m.fileModel.filePanels[i].invalidateElementCache()
		}
		return scheduleGitRefresh(m)
	}
	return nil
}

// SudoDeleteRequiredMsg is sent when delete fails due to permission errors
// and sudo rm is needed
type SudoDeleteRequiredMsg struct {
	BaseMessage

	FailedPaths []string
}

func NewSudoDeleteRequiredMsg(failedPaths []string, reqID int) SudoDeleteRequiredMsg {
	return SudoDeleteRequiredMsg{
		FailedPaths: failedPaths,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg SudoDeleteRequiredMsg) ApplyToModel(m *model) tea.Cmd {
	// Store the paths that need sudo deletion
	m.sudoDeletePaths = msg.FailedPaths
	// Show confirmation prompt
	return func() tea.Msg {
		return NewNotifyModalMsg(
			notify.New(true,
				"Permission denied",
				"Use sudo to delete? (requires password)",
				notify.SudoDeleteAction),
			msg.reqID)
	}
}

// sudoDeleteFinishedMsg is sent when sudo rm completes
type sudoDeleteFinishedMsg struct {
	err             error
	process         processbar.Process
	processBarModel *processbar.Model
}

type ProcessBarUpdateMsg struct {
	BaseMessage

	pMsg processbar.UpdateMsg
}

func (msg ProcessBarUpdateMsg) ApplyToModel(m *model) tea.Cmd {
	cmd, err := msg.pMsg.Apply(&m.processBarModel)
	if err != nil {
		slog.Error("Error applying processbar update", "error", err)
	}
	// Clean up old completed processes to prevent memory leak
	m.processBarModel.CleanupOldProcesses()
	return processCmdToTeaCmd(cmd)
}

type CompressOperationMsg struct {
	BaseMessage

	state processbar.ProcessState
}

func NewCompressOperationMsg(state processbar.ProcessState, reqID int) CompressOperationMsg {
	return CompressOperationMsg{
		state: state,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg CompressOperationMsg) ApplyToModel(m *model) tea.Cmd {
	if msg.state == processbar.Successful || msg.state == processbar.Failed {
		for i := range m.fileModel.filePanels {
			m.fileModel.filePanels[i].invalidateElementCache()
		}
		return scheduleGitRefresh(m)
	}
	return nil
}

type ExtractOperationMsg struct {
	BaseMessage

	state processbar.ProcessState
}

func NewExtractOperationMsg(state processbar.ProcessState, reqID int) ExtractOperationMsg {
	return ExtractOperationMsg{
		state: state,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg ExtractOperationMsg) ApplyToModel(m *model) tea.Cmd {
	if msg.state == processbar.Successful || msg.state == processbar.Failed {
		for i := range m.fileModel.filePanels {
			m.fileModel.filePanels[i].invalidateElementCache()
		}
		return scheduleGitRefresh(m)
	}
	return nil
}

type MetadataMsg struct {
	BaseMessage

	meta metadata.Metadata
}

func NewMetadataMsg(meta metadata.Metadata, reqID int) MetadataMsg {
	return MetadataMsg{
		meta: meta,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg MetadataMsg) ApplyToModel(m *model) tea.Cmd {
	targetPath := m.currentMetadataTargetPath()
	if targetPath == "" {
		slog.Debug("No active metadata target. Ignoring MetadataMsg")
		return nil
	}
	if targetPath != msg.meta.GetPath() {
		slog.Debug("MetadataMsg for older files. Ignoring")
		return nil
	}
	m.fileMetaData.SetMetadata(msg.meta)
	selectedItem := m.getFocusedFilePanel().getSelectedItemPtr()
	if selectedItem != nil && selectedItem.location == msg.meta.GetPath() {
		meta := msg.meta
		selectedItem.metaData = &meta
	}
	return nil
}

type NotifyModalUpdateMsg struct {
	BaseMessage

	m notify.Model
}

func NewNotifyModalMsg(m notify.Model, reqID int) NotifyModalUpdateMsg {
	return NotifyModalUpdateMsg{
		m: m,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg NotifyModalUpdateMsg) ApplyToModel(m *model) tea.Cmd {
	m.notifyModel = msg.m
	return nil
}

type FilePreviewUpdateMsg struct {
	BaseMessage

	location string
	content  string
}

func NewFilePreviewUpdateMsg(location string, content string, reqID int) FilePreviewUpdateMsg {
	return FilePreviewUpdateMsg{
		location: location,
		content:  content,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg FilePreviewUpdateMsg) ApplyToModel(m *model) tea.Cmd {
	m.previewRenderInFlight = false

	var queuedCmd tea.Cmd
	if m.previewRenderQueued {
		forcePreviewRender := m.previewRenderForced
		m.previewRenderQueued = false
		m.previewRenderForced = false
		queuedCmd = m.requestFilePreviewRender(forcePreviewRender)
	}

	location, ok := m.previewPath()
	if !ok {
		slog.Debug("Panel empty or cursor invalid. Ignoring FilePreviewUpdateMsg")
		return queuedCmd
	}
	if location != msg.location {
		slog.Debug("FilePreviewUpdateMsg for older files. Ignoring")
		return queuedCmd
	}
	m.fileModel.filePreview.SetContent(msg.content)
	return queuedCmd
}

type GitStatusUpdateMsg struct {
	BaseMessage

	status sidebar.GitStatus
}

func NewGitStatusUpdateMsg(status sidebar.GitStatus, reqID int) GitStatusUpdateMsg {
	return GitStatusUpdateMsg{
		status: status,
		BaseMessage: BaseMessage{
			reqID: reqID,
		},
	}
}

func (msg GitStatusUpdateMsg) ApplyToModel(m *model) tea.Cmd {
	// Only apply if the directory still matches current panel
	currentPanel := m.getFocusedFilePanel()
	if currentPanel.location != msg.status.LastPath {
		slog.Debug("GitStatusUpdateMsg for older directory. Ignoring")
		return nil
	}
	m.sidebarModel.SetGitStatus(msg.status)
	return nil
}
