package internal

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/search"
	"github.com/junjieliang672/marmota/src/internal/ui/metadata"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/utils"

	"github.com/barasher/go-exiftool"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	variable "github.com/junjieliang672/marmota/src/config"
	zoxideui "github.com/junjieliang672/marmota/src/internal/ui/zoxide"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
	filepreview "github.com/junjieliang672/marmota/src/pkg/file_preview"
	stringfunction "github.com/junjieliang672/marmota/src/pkg/string_function"
)

// These represent model's state information, its not a global preperty
var (
	LastTimeCursorMove = [2]int{int(time.Now().UnixMicro()), 0} //nolint: gochecknoglobals // TODO: Move to model struct
	et                 *exiftool.Exiftool                       //nolint: gochecknoglobals // TODO: Move to model struct
)

// Initialize and return model with default configs
// It returns only tea.Model because when it used in main, the return value
// is passed to tea.NewProgram() which accepts tea.Model
// Either way type 'model' is not exported, so there is not way main package can
// be aware of it, and use it directly
func InitialModel(firstPanelPaths []string, firstUseCheck bool) tea.Model {
	toggleDotFile, toggleFooter, zClient := initialConfig(firstPanelPaths)
	return defaultModelConfig(toggleDotFile, toggleFooter, firstUseCheck, firstPanelPaths, zClient)
}

// Init function to be called by Bubble tea framework, sets windows title,
// cursos blinking and starts message streamming channel
// Note : What init should do, for example read file panel data, read sidebar directories, and
// disk, is being done in at the creation of model of object. Right now creation of model object
// and its initialization isn't well separated.
func (m *model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("Marmota"),
		textinput.Blink, // Assuming textinput.Blink is a valid command
		processCmdToTeaCmd(m.processBarModel.GetListenCmd()),
		scheduleHeartbeat(),
		m.macSampleCmd(),
		m.claudeSessionsRefreshCmd(),
		// Spec-006 T5: keep the recursive-search partial-result pump alive
		// for the lifetime of the model. Each delivered partial msg is
		// re-registered inside the Update handler so the pump stays active
		// across successive searches.
		m.listenForSearchPartialCmd(),
	)
}

// Update function for bubble tea to provide internal communication to the
// application.
//
// The real work is in update(). This wrapper exists so that syncIMEHold runs
// after every message, including the ones update() returns from early.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.update(msg)
	m.syncIMEHold()
	return newModel, cmd
}

// syncIMEHold tells the input method whether to pass keys through as plain
// ASCII. Held whenever superfile owns the keyboard and is reading hotkeys;
// released as soon as a text field has focus, the terminal loses focus, or a
// child process takes over the terminal.
//
// Cheap to call on every message: ime.Hold.Set returns immediately unless the
// state actually changed, and never does I/O on this goroutine.
func (m *model) syncIMEHold() {
	if m.imeHold == nil {
		return
	}
	m.imeHold.Set(m.terminalFocused &&
		!m.externalProcessRunning &&
		!m.isTextInputActive())
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// TODO : We could check for m.modelQuitState and skip doing anything
	// If its quitDone. But if we are at this state, its already bad, so we need
	// to first figure out if its possible in testing, and fix it.
	slog.Debug("model.Update() called", "msgType", reflect.TypeOf(msg))
	if msg, ok := msg.(tea.MouseMsg); ok {
		// Only the wheel does something, over a preview (mouse.go).
		return m, m.handleMouse(msg)
	}
	var sidebarCmd, inputCmd, updateCmd, panelCmd, heartbeatCmd tea.Cmd
	gotModelUpdateMsg := false

	sidebarCmd = m.sidebarModel.UpdateState(msg)

	// this is similar to m.sidebarModel.UpdateState(msg) but since helpMenu is not a Model
	// we call .Update() manually here
	var helpMenuCmd tea.Cmd
	if m.helpMenu.searchBar.Focused() {
		m.helpMenu.searchBar, helpMenuCmd = m.helpMenu.searchBar.Update(msg)
	}

	if cmd, done := m.handleOwnMsg(msg); done {
		return m, cmd
	}

	forcePreviewRender := false

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Must re-render file preview on resize
		m.handleWindowResize(msg)
		forcePreviewRender = true
	case tea.KeyMsg:
		inputCmd = m.handleKeyInput(msg)

	// Focus reporting exists for the IME ascii-hold. superfile stays in hotkey
	// mode while it is in a background tab, and a hold left in place from there
	// would force ASCII input in whatever the user switched to.
	case tea.FocusMsg:
		m.terminalFocused = true
	case tea.BlurMsg:
		m.terminalFocused = false

	case editorFinishedMsg:
		updateCmd = m.applyEditorFinished(msg)

	// Has to handle zoxide messages separately as they could be generated via
	// zoxide update commands, or batched commands from textinput
	// Cannot do it like processbar messages
	case zoxideui.UpdateMsg:
		slog.Debug("Got ModelUpdate message", "id", msg.GetReqID())
		gotModelUpdateMsg = true
		updateCmd = msg.Apply(&m.zoxideModal)
	case ModelUpdateMessage:
		// TODO: Some of these updates messages should trigger filePanel state update
		// For example a success message for delete operation
		// But, we cant do that as of now, because if every opertion including metadata operation
		// keeps triggering model update below, which will trigger another metadata fetch,
		// we will be stuck in a loop.
		slog.Debug("Got ModelUpdate message", "id", msg.GetReqID())
		gotModelUpdateMsg = true
		updateCmd = msg.ApplyToModel(m)

	case heartbeatMsg:
		// Falls through to the state refresh below; that refresh is the point.
		heartbeatCmd = scheduleHeartbeat()

	case asyncRecursiveSearchMsg:
		if err := m.applyAsyncSearch(msg); err != nil {
			slog.Error("Async search error", "error", err)
			return m, nil
		}

	case asyncRecursiveSearchPartialMsg:
		// Spec-006 T5: streaming first-batch partial update. Mirrors the
		// staleness check on asyncRecursiveSearchMsg — if the top layer's
		// query has shifted (user typed a newer query), the partial is from
		// a superseded walk and must be discarded.
		updateCmd = m.applySearchPartial(msg.searchTerm, msg.elements)

	case search.AsyncSearchPartialMsg:
		// Defensive path: if a raw search-package partial msg ever reaches
		// Update without passing through the listener wrapper (e.g. test
		// harness feeds it directly), handle it identically.
		updateCmd = m.applySearchPartial(msg.Query, searchablesToElements(msg.Results))

	default:
		slog.Debug("Message of type that is not handled")
	}

	// This is needed for blink, etc to work
	panelCmd = m.updateFilePanelsState(msg)

	m.updateModelStateAfterMsgForType(msg)

	// Schedule debounced updates to prevent excessive I/O during rapid interactions
	var refreshCmd tea.Cmd
	if !gotModelUpdateMsg {
		refreshCmd = m.scheduleRefreshes(forcePreviewRender)
	}

	return m, tea.Batch(sidebarCmd, helpMenuCmd, inputCmd, updateCmd, panelCmd, refreshCmd, heartbeatCmd)
}

// handleOwnMsg handles the messages that update one thing and need no
// refresh of the panels after it, and reports whether msg was one.
func (m *model) handleOwnMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case gitRefreshDebounceMsg:
		// Debounce timer fired - refresh git status now
		m.gitRefreshPending = false
		m.sidebarModel.ClearGitStatusCache()
		return m.getGitStatusCmd(), true

	case metadataDebounceMsg:
		// Debounce timer fired - fetch metadata now
		m.metadataRefreshPending = false
		if m.fileModel.renaming {
			return nil, true
		}
		return m.getMetadataCmd(), true

	case previewDebounceMsg:
		// Debounce timer fired - render preview for the latest hovered item
		m.previewRefreshPending = false
		if m.fileModel.renaming {
			return nil, true
		}
		return m.requestFilePreviewRender(false), true

	case claudeSessionsTickMsg:
		return m.applyClaudeSessionsTick(msg), true

	case claudeSessionsSnapshotMsg:
		return m.applyClaudeSessionsSnapshot(msg), true

	case askDelegatedMsg:
		m.applyAskDelegated(msg)
		return nil, true

	case diffDocMsg:
		return m.applyDiffDoc(msg), true

	case macTickMsg:
		return m.applyMacTick(), true
	case macStatsMsg:
		return m.applyMacStats(msg), true

	case shelfSentMsg:
		m.applyShelfSent(msg)
		return nil, true

	case claudeMovedMsg:
		m.applyClaudeMoved(msg)
		return nil, true

	case claudeRenamedMsg:
		m.applyClaudeRenamed(msg)
		return nil, true

	case claudeQuestionMsg:
		return m.applyClaudeQuestion(msg), true

	case claudeQuestionTickMsg:
		return m.applyClaudeQuestionTick(msg), true

	case claudeKeysSentMsg:
		return m.applyClaudeKeysSent(msg), true

	case sudoDeleteFinishedMsg:
		return m.applySudoDeleteFinished(msg), true

	case askStartTickMsg:
		return m.applyAskStartTick(msg), true

	case filePreviewDocMsg:
		m.fileModel.filePreview.SetDoc(msg.location, msg.width, msg.doc, msg.docWidth)
		return nil, true
	}
	return nil, false
}

// applySudoDeleteFinished finishes a sudo delete's process and refreshes
// the panel.
func (m *model) applySudoDeleteFinished(msg sudoDeleteFinishedMsg) tea.Cmd {
	// sudo handed the terminal back.
	m.externalProcessRunning = false
	// sudo rm completed - update process bar and refresh panel
	if msg.processBarModel != nil {
		p := msg.process
		p.Done = p.Total
		if msg.err != nil {
			slog.Error("sudo rm failed", "error", msg.err)
			p.State = processbar.Failed
			p.Name = fmt.Sprintf("Failed: %v", msg.err)
		} else {
			slog.Info("sudo rm completed successfully")
			p.State = processbar.Successful
		}
		p.DoneTime = time.Now()
		msg.processBarModel.TrySendingUpdateProcessMsg(p)
	}
	// Refresh the file panel to reflect changes
	m.getFocusedFilePanel().resetSelected()
	return tea.Batch(scheduleGitRefresh(m), restoreMouseReporting)
}

// applyAsyncSearch shows a finished walk's results in its search layer,
// when that layer still asks for them, and returns the walk's error then.
func (m *model) applyAsyncSearch(msg asyncRecursiveSearchMsg) error {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	// Only apply if search term hasn't changed
	if panel.searchStack.IsEmpty() {
		return nil
	}
	top := panel.searchStack.Peek()
	if top == nil || msg.searchTerm != top.searchTerm {
		return nil
	}
	if msg.err != nil {
		return msg.err
	}

	// Update layer status
	top.cachedElems = msg.elements
	top.depthLimited = msg.depthLimited
	top.timedOut = msg.timedOut
	top.searching = false

	// If the user confirmed an explicit sort on this layer while
	// the walk was in flight, re-apply sortElements so the
	// displayed order matches their choice. Default is fzf-order.
	if top.manualSortOverride {
		sortElements(top.cachedElems, panel.sortOptions.data)
	}

	// Update old system (during migration)
	panel.recursiveBaseElems = msg.elements
	return nil
}

// applyEditorFinished takes the terminal back from the editor.
func (m *model) applyEditorFinished(msg editorFinishedMsg) tea.Cmd {
	// The child process handed the terminal back.
	m.externalProcessRunning = false
	if msg.err != nil {
		slog.Error("Editor exited with an error", "error", msg.err)
	}
	return restoreMouseReporting
}

// applySearchPartial shows a walk's first results when the layer still
// searches for term.
func (m *model) applySearchPartial(term string, elements []element) tea.Cmd {
	if top := m.searchLayerAwaiting(term); top != nil {
		m.showPartialResults(top, elements)
	}
	// Re-register the listener so the next partial msg (from any future
	// search) continues to flow into Update.
	return m.listenForSearchPartialCmd()
}

// searchLayerAwaiting is the focused panel's top search layer while it is
// still searching for term, else nil.
func (m *model) searchLayerAwaiting(term string) *searchLayer {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if panel.searchStack.IsEmpty() {
		return nil
	}
	top := panel.searchStack.Peek()
	if top == nil || term != top.searchTerm || !top.searching {
		return nil
	}
	return top
}

// showPartialResults shows a walk's first results in its search layer.
// Deliberately DO NOT clear top.searching — the walk is still in flight
// and a final AsyncSearchCompleteMsg will close it.
func (m *model) showPartialResults(top *searchLayer, elements []element) {
	top.cachedElems = elements
	if top.manualSortOverride {
		sortElements(top.cachedElems, m.getFocusedFilePanel().sortOptions.data)
	}
}

// scheduleRefreshes debounces the metadata fetch, to reduce I/O during
// rapid cursor movement, and the preview render (at once with
// forcePreviewRender), and fetches the git status.
func (m *model) scheduleRefreshes(forcePreviewRender bool) tea.Cmd {
	metadataCmd := scheduleMetadataRefresh(m)
	var filePreviewCmd tea.Cmd
	if forcePreviewRender {
		m.previewRefreshPending = false
		filePreviewCmd = m.requestFilePreviewRender(true)
	} else {
		filePreviewCmd = schedulePreviewRefresh(m)
	}
	return tea.Batch(metadataCmd, filePreviewCmd, m.getGitStatusCmd())
}

// restoreMouseReporting turns mouse reporting back on once a program that ran
// in superfile's terminal, such as the editor, hands it back: bubbletea turns
// it off for the program and leaves it off. superfile takes only the wheel,
// over a preview, but unreported, WezTerm sends scrolling as arrow keys. A
// trackpad's sideways scroll, which WezTerm sends to the focused pane even
// while you scroll the one beside it, then went to the parent folder or into
// a folder.
func restoreMouseReporting() tea.Msg {
	return tea.EnableMouseCellMotion()
}

// updateModelStateAfterMsgForType lets the caller declare what kind of msg
// triggered this pass so we can skip state refreshes that cannot be affected
// by that msg. Passing nil preserves the pre-spec-006 behaviour (refresh
// everything).
func (m *model) updateModelStateAfterMsgForType(msg tea.Msg) {
	// Sidebar refresh is I/O-heavy on macOS (disk.Partitions + tmutil).
	// Async recursive search msgs only touch the focused file panel's
	// search stack — the sidebar's directory list cannot have changed as
	// a side-effect of them, so skip the refresh on the fast path. This
	// keeps AC-PERF-7 (Update <50ms) honest and lets AC-PERF-5's
	// cache-hit ratio target be observable without being drowned out by
	// the sidebar's per-tick overhead.
	skipSidebarRefresh := false
	switch msg.(type) {
	case asyncRecursiveSearchMsg,
		asyncRecursiveSearchPartialMsg,
		search.AsyncSearchCompleteMsg,
		search.AsyncSearchPartialMsg:
		skipSidebarRefresh = true
	}
	if !skipSidebarRefresh {
		m.sidebarModel.UpdateDirectories()
	}
	m.getFilePanelItems()
	// TODO: Move to utility
	if m.focusPanel != metadataFocus {
		m.fileMetaData.ResetRender()
	}
	// TODO: Entirely remove the need of this variable, and handle first loading via Init()
	// Init() should return a basic model object with all IO waiting via a tea.Cmd
	if !m.firstLoadingComplete {
		m.firstLoadingComplete = true
	}
}

func (m *model) getFilePreviewCmd(forcePreviewRender bool) tea.Cmd {
	if !m.fileModel.filePreview.IsOpen() {
		return nil
	}
	if len(m.getFocusedFilePanel().element) == 0 {
		// Sync call because this will be fast
		m.fileModel.filePreview.SetContentWithRenderText("")
		return nil
	}
	selectedItem := m.getFocusedFilePanel().getSelectedItem()
	if m.fileModel.filePreview.GetLocation() == selectedItem.location && !forcePreviewRender {
		return nil
	}
	if layer := m.touchedLayer(); layer != nil && !selectedItem.directory {
		return m.diffPreviewCmd(layer, selectedItem.location) // what the session changed in it
	}

	m.fileModel.filePreview.SetLocation(selectedItem.location)
	m.fileModel.filePreview.SetContentWithRenderText("Loading...")
	reqCnt := m.ioReqCnt
	m.ioReqCnt++
	slog.Debug("Submitting file preview render request", "id", reqCnt, "path", selectedItem.location)

	// Copy to a local variable to be used in below closure.
	fullModalWidth := m.fullWidth

	return func() tea.Msg {
		return NewFilePreviewUpdateMsg(selectedItem.location,
			m.fileModel.filePreview.RenderWithPath(selectedItem.location, fullModalWidth), reqCnt)
	}
}

func (m *model) requestFilePreviewRender(forcePreviewRender bool) tea.Cmd {
	if !m.fileModel.filePreview.IsOpen() || m.fileModel.renaming {
		return nil
	}

	if m.previewRenderInFlight {
		m.previewRenderQueued = true
		m.previewRenderForced = m.previewRenderForced || forcePreviewRender
		return nil
	}

	cmd := m.getFilePreviewCmd(forcePreviewRender)
	if cmd == nil {
		return nil
	}

	m.previewRenderInFlight = true
	return cmd
}

func (m *model) currentMetadataTargetPath() string {
	if m.focusPanel == sidebarFocus {
		return m.sidebarModel.GetCurrentDirectoryLocation()
	}

	selectedItem := m.getFocusedFilePanel().getSelectedItemPtr()
	if selectedItem == nil {
		return ""
	}

	return selectedItem.location
}

// Note : Maybe we should not trigger metadata fetch for updates
// that dont change the currently selected file panel element
// TODO : At least dont trigger metadata fetch when user is scrolling
// through the metadata panel
func (m *model) getMetadataCmd() tea.Cmd {
	if m.disableMetadata {
		return nil
	}
	if m.fileModel.renaming {
		return nil
	}

	targetPath := m.currentMetadataTargetPath()
	if targetPath == "" {
		m.fileMetaData.SetBlank()
		return nil
	}

	selectedItem := m.getFocusedFilePanel().getSelectedItemPtr()
	if m.focusPanel != sidebarFocus && selectedItem != nil && selectedItem.metaData != nil &&
		selectedItem.metaData.GetPath() == selectedItem.location {
		m.fileMetaData.SetMetadata(*selectedItem.metaData)
		return nil
	}

	// Note : This will cause metadata not being refreshed when you are not scrolling,
	// or filepanel is not getting updated. Its not a big problem as we repeatedly refresh filepanel
	// In case this is a significant issue, we would implement metadata caching.
	// But need to implement it carefully if we do. Make sure cache is not unbounded
	// Remove metadata from filepanel.elemets[] and have cache as source of truth.
	// Have a TTL for expiry, or lister for file update events.
	if m.focusPanel == sidebarFocus && m.fileMetaData.GetMetadata().GetPath() == targetPath {
		return nil
	}
	if m.fileMetaData.IsBlank() {
		m.fileMetaData.SetInfoMsg(icon.InOperation + icon.Space + "Loading metadata...")
	}
	metadataFocussed := m.focusPanel == metadataFocus
	reqCnt := m.ioReqCnt
	m.ioReqCnt++

	// Get PDF page info if applicable
	pdfCurrentPage := 0
	pdfTotalPages := 0
	if pdfPreviewer := m.fileModel.filePreview.GetPdfPreviewer(); pdfPreviewer != nil {
		pdfCurrentPage = pdfPreviewer.GetCurrentPage()
		pdfTotalPages = pdfPreviewer.GetTotalPages()
	}

	// If there are too many metadata fetches, we need to have a cache with path as a key
	// and timeout based eviction
	slog.Debug("Submitting metadata fetch request", "id", reqCnt, "path", targetPath)
	return func() tea.Msg {
		return NewMetadataMsg(
			metadata.GetMetadata(targetPath, metadataFocussed, et, pdfCurrentPage, pdfTotalPages), reqCnt)
	}
}

// getGitStatusCmd returns a tea.Cmd that fetches git status asynchronously
func (m *model) getGitStatusCmd() tea.Cmd {
	panel := m.getFocusedFilePanel()
	currentDir := panel.location

	// Skip if sidebar is focused (show "not available")
	if m.focusPanel == sidebarFocus {
		m.sidebarModel.SetGitStatus(sidebar.GitStatusUnavailable())
		return nil
	}

	// Skip if we already have status for this directory
	currentStatus := m.sidebarModel.GetGitStatus()
	if currentStatus.LastPath == currentDir {
		return nil
	}

	reqCnt := m.ioReqCnt
	m.ioReqCnt++

	slog.Debug("Submitting git status fetch request", "id", reqCnt, "path", currentDir)
	return func() tea.Msg {
		return NewGitStatusUpdateMsg(sidebar.FetchGitStatus(currentDir), reqCnt)
	}
}

// Adjust window size based on msg information
func (m *model) handleWindowResize(msg tea.WindowSizeMsg) {
	m.fullHeight = msg.Height
	m.fullWidth = msg.Width

	m.setHeightValues(msg.Height)

	if m.fileModel.filePreview.IsOpen() {
		// File preview panel width same as file panel
		m.setFilePreviewPanelSize()
	}

	m.setFilePanelsSize(msg.Width)
	m.setHelpMenuSize()
	m.setDetailsSize()
	m.setPromptModelSize()
	m.setZoxideModelSize()
	m.setStatusLineModelSize()

	if m.fileModel.maxFilePanel >= common.FilePanelMax {
		m.fileModel.maxFilePanel = common.FilePanelMax
	}
}

func (m *model) setFilePreviewPanelSize() {
	m.fileModel.filePreview.SetWidth(m.getFilePreviewWidth())
	m.fileModel.filePreview.SetHeight(m.mainPanelHeight + common.BorderPadding)
}

// Set file preview panel Widht to width. Assure that
func (m *model) getFilePreviewWidth() int {
	if common.Config.FilePreviewWidth == 0 {
		return (m.fullWidth -
			(common.BorderPadding + (len(m.fileModel.filePanels))*common.BorderPadding)) / (len(m.fileModel.filePanels) + 1)
	}
	return (m.fullWidth) / common.Config.FilePreviewWidth
}

// Proper set panels size. Assure that panels do not overlap
func (m *model) setFilePanelsSize(width int) {
	// set each file panel size and max file panel amount
	availableWidth := width - m.fileModel.filePreview.GetWidth()
	requiredPadding := common.BorderPadding + (len(m.fileModel.filePanels)-1)*common.BorderPadding

	// Prevent negative/zero width
	if availableWidth-requiredPadding <= 0 {
		slog.Error("Insufficient width for file panels",
			"availableWidth", availableWidth,
			"requiredPadding", requiredPadding,
			"numPanels", len(m.fileModel.filePanels))
		m.fileModel.width = common.MinWidthForRename
		m.fileModel.maxFilePanel = 1
		return
	}

	m.fileModel.width = (availableWidth - requiredPadding) / len(m.fileModel.filePanels)

	// Enforce minimum
	if m.fileModel.width < common.MinWidthForRename {
		slog.Warn("File panel width below minimum",
			"calculatedWidth", m.fileModel.width,
			"minimum", common.MinWidthForRename)
		m.fileModel.width = common.MinWidthForRename
	}

	m.fileModel.maxFilePanel = availableWidth / common.FilePanelWidthUnit

	for i := range m.fileModel.filePanels {
		m.fileModel.filePanels[i].searchBar.Width = m.fileModel.width - common.InnerPadding
	}
}

func (m *model) setHeightValues(height int) {
	//nolint: gocritic // This is to be separated out to a function, and made better later. No need to refactor here
	if !m.toggleFooter {
		m.footerHeight = 0
	} else if height < common.HeightBreakA {
		m.footerHeight = 6
	} else if height < common.HeightBreakB {
		m.footerHeight = 7
	} else if height < common.HeightBreakC {
		m.footerHeight = 8
	} else if height < common.HeightBreakD {
		m.footerHeight = 9
	} else {
		m.footerHeight = 10
	}
	// TODO : Make it grow even more for bigger screen sizes.
	// TODO : Calculate the value , instead of manually hard coding it.

	// Calculate status line height
	statusLineHeight := 0
	if common.Config.ShowStatusLine && m.statusLineModel.IsEnabled() {
		statusLineHeight = m.statusLineModel.GetHeight()
	}

	// Main panel height = Total terminal height - 2(file panel border) - footer height - status line height
	m.mainPanelHeight = height - common.BorderPadding - utils.FullFooterHeight(
		m.footerHeight,
		m.toggleFooter,
	) - statusLineHeight

	for index := range m.fileModel.filePanels {
		m.fileModel.filePanels[index].handleResize(m.mainPanelHeight)
	}
}

// Set help menu size
func (m *model) setHelpMenuSize() {
	m.helpMenu.height = m.fullHeight - common.BorderPadding
	m.helpMenu.width = m.fullWidth - common.BorderPadding

	if m.fullHeight > common.HeightBreakB {
		m.helpMenu.height = 30
	}

	if m.fullWidth > common.ResponsiveWidthThreshold {
		m.helpMenu.width = 90
	}
	// 2 for border, 1 for left padding, 2 for placeholder icon of searchbar
	// 1 for additional character that View() of search bar function mysteriously adds.
	m.helpMenu.searchBar.Width = m.helpMenu.width - (common.InnerPadding + common.BorderPadding)
}

func (m *model) setPromptModelSize() {
	// Scale prompt model's maxHeight - 33% of total height
	m.promptModal.SetMaxHeight(m.fullHeight / 3) //nolint:mnd // modal uses third height for layout

	// Scale prompt model's maxHeight - 50% of total height
	m.promptModal.SetWidth(m.fullWidth / 2) //nolint:mnd // modal uses half width for layout
}

func (m *model) setZoxideModelSize() {
	// The s popup's size: its list area, and its width with the frame.
	m.zoxideModal.SetMaxHeight(m.sidebarListHeight())
	m.zoxideModal.SetWidth(m.popupWidth() + common.BorderPadding)
}

func (m *model) setStatusLineModelSize() {
	m.statusLineModel.SetWidth(m.fullWidth)
	m.statusLineModel.SetEnabled(common.Config.ShowStatusLine)
}

// Identify the current state of the application m and properly handle the
// msg keybind pressed
func (m *model) handleKeyInput(msg tea.KeyMsg) tea.Cmd {
	m.logKeyInput(msg)
	if m.firstUse {
		m.firstUse = false
		return nil
	}

	if !m.isFilePanelCountJumpEligible() {
		m.clearFilePanelJumpCount()
	}
	if !m.isSidebarCountJumpEligible() {
		m.clearSidebarJumpCount()
	}

	var cmd tea.Cmd
	cdOnQuit := common.Config.CdOnQuit
	switch {
	case m.typingModal.open:
		m.typingModalOpenKey(msg.String())
	case m.promptModal.IsOpen():
		// Ignore keypress. It will be handled in Update call via
		// updateFilePanelState
		// TODO: Convert that to async via tea.Cmd
	case m.zoxideModal.IsOpen():
		// Ignore keypress. It will be handled in Update call via
		// updateFilePanelState

	// Handles all warn models except the warn model for confirming to quit
	case m.notifyModel.IsOpen():
		cmd = m.notifyModelOpenKey(msg.String())

	// The Sessions box in the dock takes the keys while it has them, and
	// the Ask box all of them: you type there.
	case m.focusPanel == dockSessionsFocus && !m.isDockQuitKey(msg.String()):
		cmd = m.dockSessionsKey(msg)
	case m.focusPanel == dockAskFocus:
		cmd = m.dockAskKey(msg)

	// If renaming a object
	case m.fileModel.renaming:
		cmd = m.renamingKey(msg.String())
	case m.sidebarModel.IsRenaming():
		m.sidebarRenamingKey(msg.String())
	case m.sidebarModel.IsMovingPinned():
		m.sidebarMoveKey(msg)
	case m.escLeavesSelectMode(msg.String()):
		m.getFocusedFilePanel().changeFilePanelMode() // Exit selection mode
	// If search bar is open
	case m.fileModel.filePanels[m.filePanelFocusIndex].searchBar.Focused():
		cmd = m.focusOnSearchbarKey(msg.String())
	// If sort options menu is open (check before search layer ESC handling)
	case m.fileModel.filePanels[m.filePanelFocusIndex].sortOptions.open:
		m.sortOptionsKey(msg.String())
	// If iCloud filter menu is open
	case m.fileModel.filePanels[m.filePanelFocusIndex].iCloudFilter.open:
		m.iCloudFilterKey(msg.String())
	// If search layers are active (but search bar not focused), allow ESC to pop layers
	case !m.fileModel.filePanels[m.filePanelFocusIndex].searchStack.IsEmpty() && slices.Contains(common.Hotkeys.CancelTyping, msg.String()):
		m.cancelSearch()
	case m.sidebarModel.SearchBarFocused():
		cmd = m.sidebarModel.HandleSearchBarKey(msg.String())
	// If help menu is open
	case m.helpMenu.open:
		m.helpMenuKey(msg.String())

	// esc puts a toast away ("esc later"); what it is about stays in the
	// Inbox for !.
	case msg.String() == "esc" && m.dismissClaudeToast():

	case slices.Contains(common.Hotkeys.Quit, msg.String()):
		m.modelQuitState = quitInitiated

	case slices.Contains(common.Hotkeys.CdQuit, msg.String()):
		m.modelQuitState = quitInitiated
		cdOnQuit = true

	default:
		cmd = m.regularKey(msg)
	}

	return m.quitIfAsked(cmd, cdOnQuit)
}

// escLeavesSelectMode reports whether key takes the file panel out of select
// mode: ESC does, before search is handled, but ctrl+c only when nothing is
// selected (otherwise ctrl+c copies).
func (m *model) escLeavesSelectMode(key string) bool {
	return m.focusPanel == nonePanelFocus &&
		m.getFocusedFilePanel().panelMode == selectMode &&
		slices.Contains(common.Hotkeys.CancelTyping, key) &&
		(len(m.getFocusedFilePanel().selected) == 0 || !slices.Contains(common.Hotkeys.CopyItems, key))
}

// regularKey handles msg in the regular state of the application: a count
// typed before a jump, or the general hotkeys.
func (m *model) regularKey(msg tea.KeyMsg) tea.Cmd {
	if m.handleFilePanelJumpCountInput(msg) || m.handleSidebarJumpCountInput(msg) {
		return nil
	}
	if !slices.Contains(common.Hotkeys.ListBottom, msg.String()) &&
		!slices.Contains(common.Hotkeys.ListTop, msg.String()) {
		m.clearJumpCounts()
	}
	// Handles general kinds of inputs in the regular state of the application
	slog.Debug("Calling mainKey", "msg", msg.String())
	return m.mainKey(msg.String())
}

// quitIfAsked quits superfile when a quit key was pressed: it asks first
// while processes still run, else quits at once.
func (m *model) quitIfAsked(cmd tea.Cmd, cdOnQuit bool) tea.Cmd {
	if m.modelQuitState == quitInitiated {
		if m.processBarModel.HasRunningProcesses() {
			// Dont quit now, get a confirmation first.
			m.modelQuitState = quitConfirmationInitiated
			m.warnModalForQuit()
			return cmd
		}
		m.modelQuitState = quitConfirmationReceived
	}
	if m.modelQuitState == quitConfirmationReceived {
		m.quitSuperfile(cdOnQuit)
		return tea.Quit
	}
	return cmd
}

// logKeyInput logs a key and the state it arrives in.
func (m *model) logKeyInput(msg tea.KeyMsg) {
	slog.Debug("model.handleKeyInput", "msg", msg, "typestr", msg.Type.String(),
		"runes", msg.Runes, "type", int(msg.Type), "paste", msg.Paste,
		"alt", msg.Alt)
	slog.Debug("model.handleKeyInput. model info. ",
		"filePanelFocusIndex", m.filePanelFocusIndex,
		"filePanel.isFocused", m.fileModel.filePanels[m.filePanelFocusIndex].isFocused,
		"filePanel.panelMode", m.fileModel.filePanels[m.filePanelFocusIndex].panelMode,
		"typingModal.open", m.typingModal.open,
		"notifyModel.open", m.notifyModel.IsOpen(),
		"promptModal.open", m.promptModal.IsOpen(),
		"fileModel.renaming", m.fileModel.renaming,
		"searchBar.focussed", m.fileModel.filePanels[m.filePanelFocusIndex].searchBar.Focused(),
		"helpMenu.open", m.helpMenu.open,
		"firstTextInput", m.firstTextInput,
		"focusPanel", m.focusPanel,
	)
}

// Update the file panel state. Change name of renamed files, filter out files
// in search, update typingb bar, etc
func (m *model) updateFilePanelsState(msg tea.Msg) tea.Cmd {
	focusPanel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	var cmd tea.Cmd
	var action common.ModelAction
	switch {
	case m.firstTextInput:
		m.firstTextInput = false
	case m.fileModel.renaming:
		focusPanel.rename, cmd = focusPanel.rename.Update(msg)
	case focusPanel.searchBar.Focused():
		focusPanel.searchBar, cmd = focusPanel.searchBar.Update(msg)
	case m.typingModal.open:
		m.typingModal.textInput, cmd = m.typingModal.textInput.Update(msg)
	case m.promptModal.IsOpen():
		// TODO : Separate this to a utility
		cwdLocation := m.fileModel.filePanels[m.filePanelFocusIndex].location
		action, cmd = m.promptModal.HandleUpdate(msg, cwdLocation)
		m.applyPromptModalAction(action)
	case m.zoxideModal.IsOpen():
		action, cmd = m.zoxideModal.HandleUpdate(msg)
		m.applyZoxideModalAction(action)
		if _, none := action.(common.NoAction); !m.zoxideModal.IsOpen() && none {
			cmd = tea.Batch(cmd, m.popupClosed())
		}
	}

	// TODO : This is like duct taping a bigger problem
	// The code should never reach this state.
	if focusPanel.cursor < 0 {
		focusPanel.cursor = 0
	}

	return cmd
}

// Apply the Action and notify the promptModal
func (m *model) applyPromptModalAction(action common.ModelAction) {
	successMsg, actionErr := m.logAndExecuteAction(action)
	if actionErr != nil {
		m.promptModal.HandleSPFActionResults(false, actionErr.Error())
	} else if successMsg != "" {
		m.promptModal.HandleSPFActionResults(true, successMsg)
	}

	// Close prompt immediately after successful managed agent command
	_, isClaudeAction := action.(common.ClaudeAction)
	_, isCodexAction := action.(common.CodexAction)
	if (isClaudeAction || isCodexAction) && actionErr == nil {
		m.promptModal.Close()
	}
}

// Utility function to log and execute actions, reducing duplication
func (m *model) logAndExecuteAction(action common.ModelAction) (string, error) {
	// Only log actions that aren't NoAction to reduce debug noise
	if _, ok := action.(common.NoAction); !ok {
		slog.Debug("Applying model action", "action", action)
	}

	switch action := action.(type) {
	case common.NoAction:
		return "", nil
	case common.ShellCommandAction:
		// Shell commands are handled separately and don't return here
		m.applyShellCommandAction(action.Command)
		return "", nil
	case common.CDCurrentPanelAction:
		return "Panel directory changed", m.updateCurrentFilePanelDir(action.Location)
	case common.OpenPanelAction:
		return "New panel opened", m.createNewFilePanelRelativeToCurrent(action.Location)
	case common.ClaudeAction:
		return m.launchClaudeInWezterm(action.RemoteControl)
	case common.CodexAction:
		return m.launchCodexInWezterm()
	default:
		return "", errors.New("unhandled action type")
	}
}

// Apply the Action for zoxide modal (no result notifications needed)
func (m *model) applyZoxideModalAction(action common.ModelAction) {
	_, _ = m.logAndExecuteAction(action)
}

// TODO : Move them around to appropriate places
func (m *model) applyShellCommandAction(shellCommand string) {
	focusPanelDir := m.fileModel.filePanels[m.filePanelFocusIndex].location

	retCode, output, err := execInShell(common.DefaultCommandTimeout, focusPanelDir, shellCommand)

	m.promptModal.HandleShellCommandResults(retCode, output)

	if err != nil {
		slog.Error("Command execution failed", "retCode", retCode,
			"error", err, "output", output)
		return
	}
}

// launchAgentBesideSuperfile starts an agent in the focused panel's folder
// and shows it beside superfile, with the keyboard in it.
func (m *model) launchAgentBesideSuperfile(command string, provider managedAgentProvider,
	message string,
) (string, error) {
	return m.launchVisibleAgent(m.fileModel.filePanels[m.filePanelFocusIndex].location, command, provider, message,
		true)
}

func (m *model) launchClaudeInWezterm(remoteControl bool) (string, error) {
	if remoteControl {
		return m.launchAgentBesideSuperfile(
			"claude --dangerously-skip-permissions --remote-control",
			managedAgentProviderClaude,
			"Claude Code (Remote Control) launched in new pane",
		)
	}
	return m.launchAgentBesideSuperfile(
		"claude --dangerously-skip-permissions",
		managedAgentProviderClaude,
		"Claude Code launched in new pane",
	)
}

func (m *model) launchCodexInWezterm() (string, error) {
	return m.launchAgentBesideSuperfile(
		codexLaunch(codexsession.LaunchArgs()...),
		managedAgentProviderCodex,
		"Codex CLI launched in new pane",
	)
}

func (m *model) createNewFilePanelRelativeToCurrent(path string) error {
	currentDir := m.fileModel.filePanels[m.filePanelFocusIndex].location
	return m.createNewFilePanel(utils.ResolveAbsPath(currentDir, path))
}

// simulates a 'cd' action
func (m *model) updateCurrentFilePanelDir(path string) error {
	panel := m.getFocusedFilePanel()
	err := panel.updateCurrentFilePanelDir(path)
	if err == nil {
		// Track the directory change with zoxide
		m.trackDirectoryWithZoxide(panel.location)
	}
	return err
}

// trackDirectoryWithZoxide adds the directory to zoxide database if zoxide is available and enabled
func (m *model) trackDirectoryWithZoxide(path string) {
	if !common.Config.ZoxideSupport || m.zClient == nil {
		return
	}

	err := m.zClient.Add(path)
	if err != nil {
		slog.Debug("Failed to add directory to zoxide", "path", path, "error", err)
	}
}

// Check if there's any processes running in background

// Triggers a warn for confirm quiting
func (m *model) warnModalForQuit() {
	m.notifyModel = notify.New(true, "Confirm to quit Marmota",
		"You still have files being processed. Are you sure you want to exit?",
		notify.QuitAction)
}

// Implement View function for bubble tea model to handle visualization.
func (m *model) View() string {
	slog.Debug("model.View() called", "mainPanelHeight", m.mainPanelHeight,
		"footerHeight", m.footerHeight, "fullHeight", m.fullHeight,
		"fullWidth", m.fullWidth)

	if !m.firstLoadingComplete {
		return "Loading..."
	}

	// check is the terminal size enough
	if m.fullHeight < common.MinimumHeight || m.fullWidth < common.MinimumWidth {
		return m.terminalSizeWarnRender()
	}
	if m.fileModel.width < common.MinWidthForRename {
		return m.terminalSizeWarnAfterFirstRender()
	}

	if err := m.validateLayout(); err != nil {
		slog.Error("Invalid layout", "error", err)
	}

	// No sidebar column: s draws it as a popup over the panels.
	sidebar := ""

	filePanel := m.filePanelRender()

	filePreview := m.filePreviewPanelRender()
	if m.previewShowsASession() {
		// The preview shows the session the Sessions box is on. An image
		// the file preview drew, such as a PDF's page, stays on the screen
		// over it until cleared; back in the files, it is drawn again.
		filePreview = m.dockSessionPreview(m.fileModel.filePreview.GetWidth(),
			m.fileModel.filePreview.GetHeight()) + filepreview.ClearKittyImages()
	}

	mainPanel := lipgloss.JoinHorizontal(0, sidebar, filePanel, filePreview)

	if common.Config.Debug {
		showRenderDebugStatsMain(sidebar, filePanel, filePreview)
	}

	var footer string
	slog.Debug("Rendering, toggleFooter state", "value", m.toggleFooter, "footerHeight", m.footerHeight)
	if m.toggleFooter {
		footer = m.dockRender()
	}

	var finalRender string

	if m.toggleFooter {
		finalRender = lipgloss.JoinVertical(0, mainPanel, footer)
	} else {
		finalRender = mainPanel
	}

	// Add status line at the bottom if enabled
	if common.Config.ShowStatusLine && m.statusLineModel.IsEnabled() {
		statusLine := m.statusLineRender()
		finalRender = lipgloss.JoinVertical(0, finalRender, statusLine)
	}

	finalRender = m.updateRenderForOverlay(finalRender)

	return finalRender
}

func (m *model) updateRenderForOverlay(finalRender string) string {
	// The sidebar popup (s) sits over the file panels while it has the
	// keyboard.
	if m.focusPanel == sidebarFocus {
		x, y := m.popupXY()
		finalRender = stringfunction.PlaceOverlay(x, y, m.sidebarPopupRender(), finalRender)
	}
	// A toast sits in the top right corner, under any modal.
	if t, ok := m.visibleClaudeToast(); ok {
		toast := m.renderClaudeToast(t)
		finalRender = stringfunction.PlaceOverlay(max(0, m.fullWidth-lipgloss.Width(toast)), 0, toast, finalRender)
	}

	// check if need pop up modal
	if m.helpMenu.open {
		helpMenu := m.helpMenuRender()
		overlayX := m.fullWidth/common.CenterDivisor - m.helpMenu.width/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - m.helpMenu.height/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, helpMenu, finalRender)
	}

	if m.promptModal.IsOpen() {
		promptModal := m.promptModalRender()
		overlayX := m.fullWidth/common.CenterDivisor - m.promptModal.GetWidth()/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - m.promptModal.GetMaxHeight()/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, promptModal, finalRender)
	}

	if m.zoxideModal.IsOpen() {
		x, y := m.popupXY()
		return stringfunction.PlaceOverlay(x, y, m.zoxideModalRender(), finalRender)
	}

	panel := m.fileModel.filePanels[m.filePanelFocusIndex]

	if panel.sortOptions.open {
		sortOptions := m.sortOptionsRender()
		overlayX := m.fullWidth/common.CenterDivisor - panel.sortOptions.width/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - panel.sortOptions.height/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, sortOptions, finalRender)
	}

	if panel.iCloudFilter.open {
		iCloudFilter := m.iCloudFilterRender()
		overlayX := m.fullWidth/common.CenterDivisor - panel.iCloudFilter.width/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - panel.iCloudFilter.height/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, iCloudFilter, finalRender)
	}

	if m.firstUse {
		introduceModal := m.introduceModalRender()
		overlayX := m.fullWidth/common.CenterDivisor - m.helpMenu.width/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - m.helpMenu.height/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, introduceModal, finalRender)
	}

	if m.typingModal.open {
		typingModal := m.typineModalRender()
		overlayX := m.fullWidth/common.CenterDivisor - common.ModalWidth/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - common.ModalHeight/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, typingModal, finalRender)
	}

	if m.notifyModel.IsOpen() {
		notifyModal := m.notifyModel.Render()
		overlayX := m.fullWidth/common.CenterDivisor - common.ModalWidth/common.CenterDivisor
		overlayY := m.fullHeight/common.CenterDivisor - common.ModalHeight/common.CenterDivisor
		return stringfunction.PlaceOverlay(overlayX, overlayY, notifyModal, finalRender)
	}
	return finalRender
}

func showRenderDebugStatsMain(sidebar, filePanel, filePreview string) {
	slog.Debug("Render stats for main panel",
		"sidebarLineCnt", getLineCnt(sidebar), "sidebarMaxW", getMaxW(sidebar),
		"filePanelLineCnt", getLineCnt(filePanel), "filePanelMaxW", getMaxW(filePanel),
		"filePreviewLineCnt", getLineCnt(filePreview), "filePreviewMaxW", getMaxW(filePreview),
	)
}

func getLineCnt(s string) int {
	return strings.Count(s, "\n") + 1
}

func getMaxW(s string) int {
	maxW := 0
	for line := range strings.Lines(s) {
		maxW = max(maxW, ansi.StringWidth(line))
	}
	return maxW
}

// Render and update file panel items. Check for changes and updates in files and
// folders in the current directory.
func (m *model) getFilePanelItems() {
	focusPanel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	for i := range m.fileModel.filePanels {
		filePanel := &m.fileModel.filePanels[i]
		nowTime := time.Now()
		if !m.shouldSkipPanelUpdate(filePanel, focusPanel, nowTime) {
			// Throttle expired — invalidate cache to pick up external filesystem changes.
			filePanel.invalidateElementCache()
			// Load elements for this panel (with/without search filter)
			filePanel.element = m.getElementsForPanel(filePanel)
			// Update file panel list
			filePanel.lastTimeGetElement = nowTime

			// For hover to file on first time loading
			if filePanel.targetFile != "" {
				filePanel.applyTargetFileCursor()
			}
		}
		// Due to applyTargetFileCursor, cursor might go out of range
		filePanel.scrollToCursor(m.mainPanelHeight)
	}

	m.updatedToggleDotFile = false
}

// Helper to decide whether to skip updating a panel this tick.
func (m *model) shouldSkipPanelUpdate(filePanel *filePanel, focusPanel *filePanel, nowTime time.Time) bool {
	if !filePanel.elementCacheValid {
		return false
	}

	// Throttle non-focused panels unless dotfile toggle changed
	if !filePanel.isFocused && nowTime.Sub(filePanel.lastTimeGetElement) < 3*time.Second {
		if !m.updatedToggleDotFile {
			return true
		}
	}

	focusPanelReRender := focusPanel.needsReRender()
	reRenderTime := max(1, int(float64(len(filePanel.element))/common.ReRenderChunkDivisor))
	if filePanel.isFocused && !focusPanelReRender &&
		nowTime.Sub(filePanel.lastTimeGetElement) < time.Duration(reRenderTime)*time.Second {
		return true
	}
	return false
}

// Checks whether the focus panel directory changed and forces a re-render.
func (panel *filePanel) needsReRender() bool {
	if len(panel.element) > 0 {
		return filepath.Dir(panel.element[0].location) != panel.location
	}
	return true
}

// Retrieves elements for a panel based on search bar value and sort options.
func (m *model) getElementsForPanel(filePanel *filePanel) []element {
	// Stack-based search system
	if !filePanel.searchStack.IsEmpty() {
		elems := m.searchLayerElements(filePanel)
		if term := filePanel.searchBar.Value(); term != "" {
			// Typing over a layer: its list filtered as you type, as enter's
			// filter layer will.
			return filePanel.liveLayerFilter(elems, term)
		}
		return elems
	}

	// OLD: Fallback to old system during migration
	if filePanel.searchBar.Value() != "" {
		return m.legacySearchElements(filePanel)
	}
	if len(filePanel.recursiveBaseElems) > 0 {
		return filePanel.recursiveBaseElems
	}

	// Normal directory listing when no search is active, with the iCloud
	// filter applied
	return iCloudFiltered(filePanel, m.dirElements(filePanel))
}

// searchLayerElements are the results of the search layer on top of the
// panel's stack: computed once, then cached.
func (m *model) searchLayerElements(filePanel *filePanel) []element {
	top := filePanel.searchStack.Peek()
	if top == nil {
		// Shouldn't happen, but handle gracefully
		return returnDirElement(filePanel.location, m.toggleDotFile, filePanel.sortOptions.data)
	}

	// Return cached results if available and search term hasn't changed.
	// The searchBar.Value() check matches the "still typing" case. The
	// !top.searching check (spec-006 T5) covers the "confirmed search"
	// case: after confirmSearch the bar is cleared but the layer holds
	// the authoritative cachedElems — re-walking synchronously here
	// would (a) undo the coordinator's cache hit benefit and (b) risk
	// blocking Update() for >50ms (AC-PERF-7).
	if len(top.cachedElems) > 0 &&
		(top.searchTerm == filePanel.searchBar.Value() || !top.searching) {
		return top.cachedElems
	}

	// Compute results for this layer
	var newElems []element
	switch top.layerType {
	case searchLayerRecursive:
		newElems = returnDirElementBySearchStringRecursive(
			m.getSearchCoordinator(),
			filePanel.location,
			m.toggleDotFile,
			top.searchTerm,
		)
	case searchLayerFilter:
		newElems = filterElementsBySearchString(top.parentElems, top.searchTerm)
	case searchLayerTouched:
		newElems = top.cachedElems // listed when W put it on
	case searchLayerRegular:
		newElems = returnDirElementBySearchString(
			filePanel.location,
			m.toggleDotFile,
			top.searchTerm,
		)
	}

	// Default order is fzf-score (best match first). If the user has
	// confirmed an explicit sort kind on this layer via the `o` menu,
	// the layer's manualSortOverride flag is true and we re-apply
	// sortElements here so the explicit choice wins.
	if top.manualSortOverride {
		sortElements(newElems, filePanel.sortOptions.data)
	}

	// Cache the results
	top.cachedElems = newElems
	return newElems
}

// legacySearchElements are the search bar's results in the old search
// system, kept during the migration.
func (m *model) legacySearchElements(filePanel *filePanel) []element {
	term := filePanel.searchBar.Value()
	if filePanel.recursiveSearch {
		if filePanel.recursiveSearchTerm != term {
			filePanel.recursiveBaseElems = returnDirElementBySearchStringRecursive(
				m.getSearchCoordinator(),
				filePanel.location,
				m.toggleDotFile,
				term,
			)
			filePanel.recursiveSearchTerm = term
			filePanel.lastFilterTerm = ""
			filePanel.lastFilterResults = nil
		}
		return filePanel.recursiveBaseElems
	}
	if len(filePanel.recursiveBaseElems) > 0 {
		if filePanel.lastFilterTerm != term {
			filePanel.lastFilterResults = filterElementsBySearchString(filePanel.recursiveBaseElems, term)
			filePanel.lastFilterTerm = term
		}
		return filePanel.lastFilterResults
	}
	return returnDirElementBySearchString(filePanel.location, m.toggleDotFile, term)
}

// dirElements is the panel's folder listing. The cached one is used while
// the folder, the hidden files setting and the sort are the same.
func (m *model) dirElements(filePanel *filePanel) []element {
	data := filePanel.sortOptions.data
	if filePanel.elementCacheValid &&
		filePanel.cachedLocation == filePanel.location &&
		filePanel.cachedDotFile == m.toggleDotFile &&
		filePanel.cachedSortSelected == data.selected &&
		filePanel.cachedSortReversed == data.reversed {
		return filePanel.cachedElements
	}
	elements := returnDirElement(filePanel.location, m.toggleDotFile, data)
	filePanel.cachedElements = elements
	filePanel.cachedLocation = filePanel.location
	filePanel.cachedDotFile = m.toggleDotFile
	filePanel.cachedSortSelected = data.selected
	filePanel.cachedSortReversed = data.reversed
	filePanel.elementCacheValid = true
	return elements
}

// iCloudFiltered is elements without those the panel's iCloud filter
// leaves out.
func iCloudFiltered(filePanel *filePanel, elements []element) []element {
	if filePanel.iCloudFilter.selected == 0 { // 0 is FilterAll
		return elements
	}
	filtered := make([]element, 0, len(elements))
	for _, elem := range elements {
		if filePanel.iCloudFilter.selected.Matches(elem.iCloudStatus) {
			filtered = append(filtered, elem)
		}
	}
	return filtered
}

// Applies targetFile cursor positioning, if configured for the panel.
func (panel *filePanel) applyTargetFileCursor() {
	for idx, el := range panel.element {
		if el.name == panel.targetFile {
			panel.cursor = idx
			break
		}
	}
	panel.targetFile = ""
}

// Close superfile application. Cd into the current dir if CdOnQuit on and save
// the path in state direcotory
func (m *model) quitSuperfile(cdOnQuit bool) {
	m.saveAskHistory() // the question being written
	// Resource cleanup
	if common.Config.Metadata && et != nil {
		_ = et.Close()
	}
	m.fileModel.filePreview.CleanUp()

	// Blocks until the request file is gone, so we never exit leaving the
	// input method stuck in ASCII mode.
	if m.imeHold != nil {
		m.imeHold.Close()
	}

	// cd on quit
	currentDir := m.fileModel.filePanels[m.filePanelFocusIndex].location
	variable.SetLastDir(currentDir)

	if cdOnQuit {
		// escape single quote
		currentDir = strings.ReplaceAll(currentDir, "'", "'\\''")
		err := os.WriteFile(variable.LastDirFile, []byte("cd '"+currentDir+"'"), utils.ConfigFilePerm)
		if err != nil {
			slog.Error("Error during writing lastdir file", "error", err)
		}
	}
	m.modelQuitState = quitDone
	slog.Debug("Quitting Marmota", "current dir", currentDir)
}
