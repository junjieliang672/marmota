package internal

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/utils"

	"github.com/junjieliang672/marmota/src/internal/common"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/config/icon"
)

// Number of parallel workers for file operations
const parallelWorkers = 4

var writeClipboard = clipboard.WriteAll //nolint:gochecknoglobals // test seam

// Create a file in the currently focus file panel
// TODO: Fix it. It doesn't creates a new file. It just opens a file model,
// that allows you to create a file. Actual creation happens here - createItem() in handle_modal.go
func (m *model) panelCreateNewFile() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	m.typingModal.location = panel.location
	m.typingModal.open = true
	m.typingModal.textInput = common.GenerateNewFileTextInput()
	m.firstTextInput = true
}

// TODO : This function does not needs the entire model. Only pass the panel object
func (m *model) IsRenamingConflicting() bool {
	// TODO : Replace this with m.getCurrentFilePanel() everywhere
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	if len(panel.element) == 0 {
		slog.Error("IsRenamingConflicting() being called on empty panel")
		return false
	}

	oldPath := panel.element[panel.cursor].location
	newPath := filepath.Join(panel.location, panel.rename.Value())

	if oldPath == newPath {
		return false
	}

	_, err := os.Stat(newPath)
	return err == nil
}

// TODO: Remove channel messaging and use tea.Cmd
func (m *model) warnModalForRenaming() tea.Cmd {
	reqID := m.ioReqCnt
	m.ioReqCnt++
	slog.Debug("Submitting rename notify model request", "reqID", reqID)
	res := func() tea.Msg {
		notifyModel := notify.New(true,
			common.SameRenameWarnTitle,
			common.SameRenameWarnContent,
			notify.RenameAction)
		return NewNotifyModalMsg(notifyModel, reqID)
	}
	return res
}

// Rename file where the cusror is located
// TODO: Fix this. It doesn't do any rename, just opens the rename text input
// Actual rename happens at confirmRename() in handle_modal.go
func (m *model) panelItemRename() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if len(panel.element) == 0 {
		return
	}

	cursorPos := -1
	nameRunes := []rune(panel.element[panel.cursor].name)
	nameLen := len(nameRunes)
	for i := nameLen - 1; i >= 0; i-- {
		if nameRunes[i] == '.' {
			cursorPos = i
			break
		}
	}
	if cursorPos == -1 || cursorPos == 0 && nameLen > 0 || panel.element[panel.cursor].directory {
		cursorPos = nameLen
	}

	m.fileModel.renaming = true
	panel.renaming = true
	m.firstTextInput = true
	panel.rename = common.GenerateRenameTextInput(
		m.fileModel.width-common.InnerPadding,
		cursorPos,
		panel.element[panel.cursor].name)
}

func (m *model) getDeleteCmd(permDelete bool) tea.Cmd {
	panel := m.getFocusedFilePanel()
	if len(panel.element) == 0 {
		return nil
	}

	var items []string
	if panel.panelMode == selectMode {
		items = panel.selectedToSlice()
	} else {
		items = []string{panel.getSelectedItem().location}
	}

	// Don't use trash if already in trash directory (permanent delete from trash)
	inTrash := isInTrashDirectory(panel.location)
	useTrash := m.hasTrash && !isExternalDiskPath(panel.location) && !permDelete && !inTrash

	reqID := m.ioReqCnt
	m.ioReqCnt++
	slog.Debug("Submitting delete request", "id", reqID, "items cnt", len(items))
	return func() tea.Msg {
		state, permissionDeniedPaths := deleteOperation(&m.processBarModel, items, useTrash)

		// If there are permission-denied paths, prompt for sudo
		if len(permissionDeniedPaths) > 0 && runtime.GOOS == utils.OsDarwin {
			return NewSudoDeleteRequiredMsg(permissionDeniedPaths, reqID)
		}

		return NewDeleteOperationMsg(state, reqID)
	}
}

// deleteJob represents a single item to be deleted
type deleteJob struct {
	path string
}

// deleteResult represents the result of a delete operation
type deleteResult struct {
	path string
	err  error
}

func deleteOperation(
	processBarModel *processbar.Model,
	items []string,
	useTrash bool,
) (processbar.ProcessState, []string) {
	if len(items) == 0 {
		return processbar.Cancelled, nil
	}
	p, err := processBarModel.SendAddProcessMsg(icon.Delete+icon.Space+filepath.Base(items[0]), len(items), true)
	if err != nil {
		slog.Error("Cannot spawn a new process", "error", err)
		return processbar.Failed, nil
	}

	// For single items, use sequential processing
	if len(items) == 1 {
		return deleteSequential(processBarModel, items, useTrash, p)
	}

	// For multiple items, use parallel processing
	return deleteParallel(processBarModel, items, useTrash, p)
}

// deleteSequential handles delete operations sequentially (for single files)
func deleteSequential(
	processBarModel *processbar.Model,
	items []string,
	useTrash bool,
	p processbar.Process,
) (processbar.ProcessState, []string) {
	deleteFunc := os.RemoveAll
	if useTrash {
		deleteFunc = moveToTrash
	}

	var failedItems []string
	var permissionDeniedPaths []string
	var lastError error

	for _, item := range items {
		err := deleteFunc(item)
		p.Name = icon.Delete + icon.Space + filepath.Base(item)
		p.Done++

		if err != nil {
			slog.Error("Error in delete operation", "item", item, "useTrash", useTrash, "error", err)
			failedItems = append(failedItems, filepath.Base(item))
			lastError = err

			// Check if it's a permission error
			if os.IsPermission(err) {
				permissionDeniedPaths = append(permissionDeniedPaths, item)
			}
		}
		processBarModel.TrySendingUpdateProcessMsg(p)
	}

	state := finalizeDeleteOperation(processBarModel, p, failedItems, lastError, len(items))
	return state, permissionDeniedPaths
}

// deleteParallel handles delete operations in parallel using a worker pool
func deleteParallel(
	processBarModel *processbar.Model,
	items []string,
	useTrash bool,
	p processbar.Process,
) (processbar.ProcessState, []string) {
	jobs := make(chan deleteJob, len(items))
	results := make(chan deleteResult, len(items))

	deleteFunc := os.RemoveAll
	if useTrash {
		deleteFunc = moveToTrash
	}

	// Track completed items for progress
	var completedCount atomic.Int32

	// Start worker pool
	var wg sync.WaitGroup
	numWorkers := min(parallelWorkers, len(items))
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				err := deleteFunc(job.path)
				results <- deleteResult{path: job.path, err: err}

				// Update progress
				completed := completedCount.Add(1)
				p.Done = int(completed)
				p.Name = icon.Delete + icon.Space + filepath.Base(job.path)
				processBarModel.TrySendingUpdateProcessMsg(p)
			}
		}()
	}

	// Send jobs to workers
	go func() {
		for _, item := range items {
			jobs <- deleteJob{path: item}
		}
		close(jobs)
	}()

	// Wait for all workers to finish and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var failedItems []string
	var permissionDeniedPaths []string
	var lastError error
	for result := range results {
		if result.err != nil {
			slog.Error("Error in delete operation", "item", result.path, "useTrash", useTrash, "error", result.err)
			failedItems = append(failedItems, filepath.Base(result.path))
			lastError = result.err

			// Check if it's a permission error
			if os.IsPermission(result.err) {
				permissionDeniedPaths = append(permissionDeniedPaths, result.path)
			}
		}
	}

	state := finalizeDeleteOperation(processBarModel, p, failedItems, lastError, len(items))
	return state, permissionDeniedPaths
}

// finalizeDeleteOperation sets the final state and updates the process bar
func finalizeDeleteOperation(processBarModel *processbar.Model, p processbar.Process,
	failedItems []string, lastError error, totalItems int,
) processbar.ProcessState {
	if len(failedItems) > 0 {
		p.State = processbar.Failed
		p.Name = fmt.Sprintf("Failed (%d/%d): %s - Error: %v",
			len(failedItems), totalItems,
			strings.Join(failedItems, ", "),
			lastError)
	} else {
		p.State = processbar.Successful
	}

	p.DoneTime = time.Now()
	err := processBarModel.SendUpdateProcessMsg(p, true)
	if err != nil {
		slog.Error("Failed to send final delete operation update", "error", err)
	}
	return p.State
}

// getSudoDeleteCmd returns a command that uses sudo rm to delete files
// This suspends the TUI to allow password input
func (m *model) getSudoDeleteCmd() tea.Cmd {
	if len(m.sudoDeletePaths) == 0 {
		return nil
	}

	// Build sudo rm -rf command with all paths
	args := append([]string{"rm", "-rf"}, m.sudoDeletePaths...)
	c := exec.Command("sudo", args...)

	// Clear the paths and store for the result handler
	paths := m.sudoDeletePaths
	m.sudoDeletePaths = nil

	slog.Info("Executing sudo rm", "paths", paths)

	// Create a process bar entry for the sudo delete
	processName := icon.Delete + icon.Space + filepath.Base(paths[0])
	if len(paths) > 1 {
		processName = icon.Delete + icon.Space + fmt.Sprintf("%d files (sudo)", len(paths))
	}
	p, err := m.processBarModel.SendAddProcessMsg(processName, len(paths), true)
	if err != nil {
		slog.Error("Cannot spawn a new process for sudo delete", "error", err)
	}

	// sudo prompts for a password on the real terminal.
	m.externalProcessRunning = true

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return sudoDeleteFinishedMsg{err: err, process: p, processBarModel: &m.processBarModel}
	})
}

func (m *model) getDeleteTriggerCmd(deletePermanent bool) tea.Cmd {
	panel := m.getFocusedFilePanel()
	if (panel.panelMode == selectMode && len(panel.selected) == 0) ||
		(panel.panelMode == browserMode && len(panel.element) == 0) {
		return nil
	}

	reqID := m.ioReqCnt
	m.ioReqCnt++

	return func() tea.Msg {
		title := common.TrashWarnTitle
		content := common.TrashWarnContent
		action := notify.DeleteAction

		// Show permanent delete warning if: no trash, external disk, explicit permanent delete, or already in trash
		inTrash := isInTrashDirectory(panel.location)
		if !m.hasTrash || isExternalDiskPath(panel.location) || deletePermanent || inTrash {
			title = common.PermanentDeleteWarnTitle
			content = common.PermanentDeleteWarnContent
			action = notify.PermanentDeleteAction
		}
		return NewNotifyModalMsg(notify.New(true, title, content, action), reqID)
	}
}

// Copy directory or file's path to superfile's clipboard
// set cut to true/false accordingly
func (m *model) copySingleItem(cut bool) {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	m.copyItems.reset(cut)
	if len(panel.element) == 0 {
		return
	}
	slog.Debug("handle_file_operations.copySingleItem", "cut", cut,
		"panel location", panel.element[panel.cursor].location)
	m.copyItems.items = append(m.copyItems.items, panel.element[panel.cursor].location)
}

// Copy all selected file or directory's paths to the clipboard
func (m *model) copyMultipleItem(cut bool) {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	m.copyItems.reset(cut)
	if len(panel.selected) == 0 {
		return
	}
	slog.Debug("handle_file_operations.copyMultipleItem", "cut", cut,
		"panel selected files", panel.selected)
	m.copyItems.items = panel.selectedToSlice()
}

func (m *model) getPasteItemCmd() tea.Cmd {
	copyItems := m.copyItems.items
	cut := m.copyItems.cut
	if len(copyItems) == 0 {
		return nil
	}

	// TODO: Do it via m.getNewReqID()
	// TODO: Have an IO Req Management, collecting info about pending IO Req too
	reqID := m.ioReqCnt
	m.ioReqCnt++
	panelLocation := m.getFocusedFilePanel().location

	slog.Debug("Submitting pasteItems request", "id", reqID, "items cnt", len(copyItems), "dest", panelLocation)
	return func() tea.Msg {
		err := validatePasteOperation(panelLocation, copyItems, cut)
		if err != nil {
			return NewNotifyModalMsg(notify.New(true, "Invalid paste location", err.Error(), notify.NoAction),
				reqID)
		}
		state := executePasteOperation(&m.processBarModel, panelLocation, copyItems, cut)
		return NewPasteOperationMsg(state, reqID)
	}
}

func validatePasteOperation(panelLocation string, copyItems []string, cut bool) error {
	// Check if trying to paste into source or subdirectory for both cut and copy operations
	for _, srcPath := range copyItems {
		// Check if trying to cut and paste into the same directory - this would be a no-op
		// and could potentially cause issues, so we prevent it
		if filepath.Dir(srcPath) == panelLocation && cut {
			return fmt.Errorf("cannot paste into parent directory of source, srcPath : %v, panelLocation : %v",
				srcPath, panelLocation)
		}
		if cut && srcPath == panelLocation {
			return errors.New("cannot paste a directory into itself")
		}

		if isAncestor(srcPath, panelLocation) {
			return fmt.Errorf("cannot %s and paste a directory into itself or its subdirectory",
				getCopyOrCutOperationName(cut))
		}
	}

	return nil
}

// new func to check and return an error that will go in m.content
// create a new error type

// pasteJob represents a single file/directory to be pasted
type pasteJob struct {
	srcPath string
	dstPath string
}

// pasteResult represents the result of a paste operation
type pasteResult struct {
	srcPath string
	err     error
}

// Paste all clipboard items using parallel workers
func executePasteOperation(processBarModel *processbar.Model,
	panelLocation string, copyItems []string, cut bool,
) processbar.ProcessState {
	slog.Debug("executePasteOperation", "items", copyItems, "cut", cut, "panel location", panelLocation)

	p, err := processBarModel.SendAddProcessMsg(
		icon.GetCopyOrCutIcon(cut)+icon.Space+filepath.Base(copyItems[0]),
		getTotalFilesCnt(copyItems), true)
	if err != nil {
		slog.Error("Cannot spawn a new process", "error", err)
		return processbar.Failed
	}

	// For single items, use sequential processing (simpler and avoids goroutine overhead)
	if len(copyItems) == 1 {
		return executePasteSequential(processBarModel, panelLocation, copyItems, cut, p)
	}

	// For multiple items, use parallel processing
	return executePasteParallel(processBarModel, panelLocation, copyItems, cut, p)
}

// executePasteSequential handles paste operations sequentially (for single files or simple cases)
func executePasteSequential(processBarModel *processbar.Model,
	panelLocation string, copyItems []string, cut bool, p processbar.Process,
) processbar.ProcessState {
	var failedItems []string
	var lastError error

	for _, filePath := range copyItems {
		var err error
		errMessage := "cut item error"
		if cut && !isExternalDiskPath(filePath) {
			err = moveElement(filePath, filepath.Join(panelLocation, filepath.Base(filePath)))
		} else {
			err = pasteDir(filePath, filepath.Join(panelLocation, filepath.Base(filePath)), &p, cut, processBarModel)
			if err != nil {
				errMessage = "paste item error"
			}
		}

		p.Name = icon.GetCopyOrCutIcon(cut) + icon.Space + filepath.Base(filePath)
		if err != nil {
			slog.Error(errMessage, "error", err, "file", filepath.Base(filePath))
			failedItems = append(failedItems, filepath.Base(filePath))
			lastError = err
		} else {
			processBarModel.TrySendingUpdateProcessMsg(p)
		}
	}

	return finalizePasteOperation(processBarModel, p, failedItems, lastError, len(copyItems))
}

// executePasteParallel handles paste operations in parallel using a worker pool
func executePasteParallel(processBarModel *processbar.Model,
	panelLocation string, copyItems []string, cut bool, p processbar.Process,
) processbar.ProcessState {
	jobs := make(chan pasteJob, len(copyItems))
	results := make(chan pasteResult, len(copyItems))

	// Track completed items for progress
	var completedCount atomic.Int32

	// Start worker pool
	var wg sync.WaitGroup
	numWorkers := min(parallelWorkers, len(copyItems))
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				var err error
				if cut && !isExternalDiskPath(job.srcPath) {
					err = moveElement(job.srcPath, job.dstPath)
				} else {
					// For parallel operations, we can't share the process struct
					// so we create individual progress tracking
					err = copyElement(job.srcPath, job.dstPath)
					if err == nil && cut {
						// For cut operations, remove source after successful copy
						err = os.RemoveAll(job.srcPath)
					}
				}
				results <- pasteResult{srcPath: job.srcPath, err: err}

				// Update progress
				completed := completedCount.Add(1)
				p.Done = int(completed)
				p.Name = icon.GetCopyOrCutIcon(cut) + icon.Space + filepath.Base(job.srcPath)
				processBarModel.TrySendingUpdateProcessMsg(p)
			}
		}()
	}

	// Send jobs to workers
	go func() {
		for _, filePath := range copyItems {
			dstPath := filepath.Join(panelLocation, filepath.Base(filePath))
			// Handle duplicates by renaming
			dstPath, _ = renameIfDuplicate(dstPath)
			jobs <- pasteJob{srcPath: filePath, dstPath: dstPath}
		}
		close(jobs)
	}()

	// Wait for all workers to finish and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var failedItems []string
	var lastError error
	for result := range results {
		if result.err != nil {
			slog.Error("paste item error", "error", result.err, "file", filepath.Base(result.srcPath))
			failedItems = append(failedItems, filepath.Base(result.srcPath))
			lastError = result.err
		}
	}

	return finalizePasteOperation(processBarModel, p, failedItems, lastError, len(copyItems))
}

// finalizePasteOperation sets the final state and updates the process bar
func finalizePasteOperation(processBarModel *processbar.Model, p processbar.Process,
	failedItems []string, lastError error, totalItems int,
) processbar.ProcessState {
	if len(failedItems) > 0 {
		p.State = processbar.Failed
		p.Name = fmt.Sprintf("Failed (%d/%d): %s - Error: %v",
			len(failedItems), totalItems,
			strings.Join(failedItems, ", "),
			lastError)
	}

	if p.State != processbar.Failed {
		p.State = processbar.Successful
		p.Done = p.Total
	}
	p.DoneTime = time.Now()
	err := processBarModel.SendUpdateProcessMsg(p, true)
	if err != nil {
		slog.Error("Could not send final update for process Bar", "error", err)
	}

	return p.State
}

func getTotalFilesCnt(copyItems []string) int {
	totalFiles := 0
	for _, folderPath := range copyItems {
		// TODO : Fix this. This is inefficient
		// In case of a cut operations for a directory with a lot of files
		// we are unnecessarily walking the whole directory recursively
		// while os will just perform a rename
		// So instead of few operations this will cause the cut paste
		// to read the whole directory recursively
		// we should avoid doing this.
		// Although this allows us a more detailed progress tracking
		// this make the copy/cut more inefficient
		// instead, we could just track progress based on total items in
		// copyItems
		// efficiency should be prioritized over more detailed feedback.
		count, err := countFiles(folderPath)
		if err != nil {
			slog.Error("Error in countFiles", "error", err)
			continue
		}
		totalFiles += count
	}
	return totalFiles
}

// Extract compressed file
// TODO : err should be returned and properly handled by the caller
func (m *model) getExtractFileCmd() tea.Cmd {
	panel := m.getFocusedFilePanel()
	if len(panel.element) == 0 {
		return nil
	}

	item := panel.getSelectedItem().location

	ext := strings.ToLower(filepath.Ext(item))
	if !common.IsExtensionExtractable(ext) {
		slog.Error("Error unexpected file", "extension type", ext, "item", item, "error", errors.ErrUnsupported)
		return nil
	}
	reqID := m.ioReqCnt
	m.ioReqCnt++

	slog.Debug("Submitting Extract file request", "reqID", reqID, "item", item)

	return func() tea.Msg {
		outputDir := common.FileNameWithoutExtension(item)
		outputDir, err := renameIfDuplicate(outputDir)
		if err != nil {
			slog.Error("Error while renaming for duplicates", "error", err)
			return NewCompressOperationMsg(processbar.Failed, reqID)
		}

		err = os.MkdirAll(
			outputDir,
			utils.ExtractedDirMode,
		)
		if err != nil {
			slog.Error("Error while making directory for extracting files", "error", err)
			return NewCompressOperationMsg(processbar.Failed, reqID)
		}
		err = extractCompressFile(item, outputDir, &m.processBarModel)
		if err != nil {
			slog.Error("Error extract file", "error", err)
			return NewCompressOperationMsg(processbar.Failed, reqID)
		}
		return NewCompressOperationMsg(processbar.Successful, reqID)
	}
}

// getExtractSelectedFilesCmd extracts all selected archive files iteratively
func (m *model) getExtractSelectedFilesCmd() tea.Cmd {
	filesToExtract := extractableFiles(m.getFocusedFilePanel())
	if len(filesToExtract) == 0 {
		slog.Debug("No extractable files selected")
		return nil
	}

	reqID := m.ioReqCnt
	m.ioReqCnt++

	slog.Debug("Submitting multi-extract request", "reqID", reqID, "count", len(filesToExtract))

	return func() tea.Msg {
		state := processbar.Successful
		for _, item := range filesToExtract {
			// A failure goes on to the next file instead of stopping
			if err := extractBeside(item, &m.processBarModel); err != nil {
				slog.Error("Error extracting file", "item", item, "error", err)
				state = processbar.Failed
			}
		}
		return NewCompressOperationMsg(state, reqID)
	}
}

// extractableFiles are the selected archives, or the one under the cursor
// when no selected item is an archive.
func extractableFiles(panel *filePanel) []string {
	var files []string
	if panel.panelMode == selectMode {
		for item := range panel.selected {
			if common.IsExtensionExtractable(strings.ToLower(filepath.Ext(item))) {
				files = append(files, item)
			}
		}
	}
	if len(files) == 0 && len(panel.element) > 0 {
		item := panel.getSelectedItem().location
		if common.IsExtensionExtractable(strings.ToLower(filepath.Ext(item))) {
			files = append(files, item)
		}
	}
	return files
}

// extractBeside extracts an archive into a new folder beside it, named after
// it.
func extractBeside(item string, processBarModel *processbar.Model) error {
	outputDir, err := renameIfDuplicate(common.FileNameWithoutExtension(item))
	if err != nil {
		return fmt.Errorf("renaming for duplicates: %w", err)
	}
	if err = os.MkdirAll(outputDir, utils.ExtractedDirMode); err != nil {
		return fmt.Errorf("creating the output directory: %w", err)
	}
	return extractCompressFile(item, outputDir, processBarModel)
}

func (m *model) getCompressSelectedFilesCmd() tea.Cmd {
	panel := m.getFocusedFilePanel()

	if len(panel.element) == 0 {
		return nil
	}
	var filesToCompress []string
	var firstFile string

	if len(panel.selected) == 0 {
		firstFile = panel.element[panel.cursor].location
		filesToCompress = append(filesToCompress, firstFile)
	} else {
		filesToCompress = panel.selectedToSlice()
		firstFile = filesToCompress[0]
	}

	reqID := m.ioReqCnt
	m.ioReqCnt++

	return func() tea.Msg {
		zipName, err := getZipArchiveName(filepath.Base(firstFile))
		if err != nil {
			slog.Error("Error in getZipArchiveName", "error", err)
			return NewCompressOperationMsg(processbar.Failed, reqID)
		}
		zipPath := filepath.Join(panel.location, zipName)
		if err := zipSources(filesToCompress, zipPath, &m.processBarModel); err != nil {
			slog.Error("Error in zipping files", "error", err)
			return NewCompressOperationMsg(processbar.Failed, reqID)
		}
		return NewCompressOperationMsg(processbar.Successful, reqID)
	}
}

func (m *model) chooserFileWriteAndQuit(path string) error {
	// Attempt to write to the file
	err := os.WriteFile(variable.ChooserFile, []byte(path), utils.ConfigFilePerm)
	if err != nil {
		return err
	}
	m.modelQuitState = quitInitiated
	return nil
}

// Open file with default editor
func (m *model) openFileWithEditor() tea.Cmd {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	// Check if panel is empty
	if len(panel.element) == 0 {
		return nil
	}

	selectedItem := panel.element[panel.cursor]
	return m.openPathWithEditor(selectedItem.location, !selectedItem.directory)
}

// Open file with second editor (E key)
func (m *model) openFileWithSecondEditor() tea.Cmd {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	// Check if panel is empty
	if len(panel.element) == 0 {
		return nil
	}

	filePath := panel.element[panel.cursor].location

	// Use second_editor if configured, otherwise fall back to editor
	editor := common.Config.SecondEditor
	if editor == "" {
		editor = common.Config.Editor
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}

	// Make sure there is an editor
	if editor == "" {
		switch runtime.GOOS {
		case utils.OsWindows:
			editor = "notepad"
		case utils.OsDarwin:
			editor = "nvim"
		default:
			editor = "nano"
		}
	}

	// Split the editor command into command and arguments
	parts := strings.Fields(editor)
	cmd := parts[0]

	//nolint:gocritic // appendAssign: intentionally creating a new slice
	args := append(parts[1:], filePath)

	c := exec.Command(cmd, args...) //nolint:gosec // the editor you configured, on the file you chose

	// The child owns the terminal from here until editorFinishedMsg
	// arrives; stop driving the input method's mode in the meantime.
	m.externalProcessRunning = true

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}

// Open directory with default editor
func (m *model) openDirectoryWithEditor() tea.Cmd {
	if variable.ChooserFile != "" {
		err := m.chooserFileWriteAndQuit(m.fileModel.filePanels[m.filePanelFocusIndex].location)
		if err == nil {
			return nil
		}
		// Continue with preview if file is not writable
		slog.Error("Error while writing to chooser file, continuing with open via directory editor", "error", err)
	}

	panel := m.fileModel.filePanels[m.filePanelFocusIndex]
	cmd, args := dirEditorCommand(common.Config.DirEditor, runtime.GOOS,
		panel.location, panel.actionTargets())

	c := exec.Command(cmd, args...)

	// The child owns the terminal from here until editorFinishedMsg
	// arrives; stop driving the input method's mode in the meantime.
	m.externalProcessRunning = true

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}

// dirEditorCommand builds the command for opening the panel's directory. With
// no dir_editor set on macOS, Finder opens with the targets highlighted: one
// with open -R, several through Finder's reveal (open -R would highlight only
// the last). With no targets, it opens the directory itself.
func dirEditorCommand(editor, goos, dir string, targets []string) (string, []string) {
	if editor == "" {
		switch goos {
		case utils.OsWindows:
			editor = "explorer"
		case utils.OsDarwin:
			switch len(targets) {
			case 0:
				editor = "open"
			case 1:
				return "open", []string{"-R", targets[0]}
			default:
				return "osascript", append(finderRevealScript(), targets...)
			}
		default:
			editor = "vi"
		}
	}

	parts := strings.Fields(editor)
	return parts[0], append(parts[1:], dir)
}

// finderRevealScript is osascript's arguments for revealing the paths that
// follow it, all selected in one Finder window. The paths arrive as argv, so
// nothing in them needs quoting.
func finderRevealScript() []string {
	lines := []string{
		"on run argv",
		"set targets to {}",
		"repeat with p in argv",
		"set end of targets to (POSIX file (p as text)) as alias",
		"end repeat",
		`tell application "Finder"`,
		"reveal targets",
		"activate",
		"end tell",
		"end run",
	}
	args := make([]string, 0, 2*len(lines)) //nolint:mnd // -e and its line
	for _, line := range lines {
		args = append(args, "-e", line)
	}
	return args
}

func (m *model) openPathWithEditor(filePath string, recordRecent bool) tea.Cmd {
	if filePath == "" {
		return nil
	}
	return m.openPathsWithEditor([]string{filePath}, recordRecent)
}

// openSelectedWithEditor opens every selected file in one editor, in the
// order the panel shows them. Folders in the selection are left out.
func (m *model) openSelectedWithEditor() tea.Cmd {
	files := m.getFocusedFilePanel().selectedFiles()
	if len(files) == 0 {
		m.flashClaude("No files selected to edit")
		return nil
	}
	return m.openPathsWithEditor(files, true)
}

// editorCommand is the command that opens paths in the editor: the
// configured one, else $EDITOR, else the platform's default.
func editorCommand(configured, envEditor, goos string, paths []string) (string, []string) {
	editor := configured
	if editor == "" {
		editor = envEditor
	}
	if editor == "" {
		switch goos {
		case utils.OsWindows:
			editor = "notepad"
		case utils.OsDarwin:
			editor = "nvim"
		default:
			editor = "nano"
		}
	}
	parts := strings.Fields(editor)
	return parts[0], append(parts[1:], paths...)
}

// openPathsWithEditor runs the editor on paths, all in one process.
func (m *model) openPathsWithEditor(paths []string, recordRecent bool) tea.Cmd {
	if variable.ChooserFile != "" {
		err := m.chooserFileWriteAndQuit(strings.Join(paths, "\n"))
		if err == nil {
			return nil
		}
		slog.Error("Error while writing to chooser file, continuing with open via file editor", "error", err)
	}

	if recordRecent {
		for _, p := range paths {
			if err := m.sidebarModel.RecordRecentEdit(p); err != nil {
				slog.Error("Error while recording recent edit", "path", p, "error", err)
			}
		}
	}

	cmd, args := editorCommand(common.Config.Editor, os.Getenv("EDITOR"), runtime.GOOS, paths)
	c := exec.Command(cmd, args...) //nolint:gosec // the editor you configured, on the files you chose

	// The child owns the terminal from here until editorFinishedMsg
	// arrives; stop driving the input method's mode in the meantime.
	m.externalProcessRunning = true

	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err}
	})
}

// Copy file path
// TODO: This is also an IO operations, do it via tea.Cmd
func (m *model) copyPath() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	if len(panel.element) == 0 {
		return
	}

	var pathToCopy string
	if len(panel.selected) > 0 {
		// Copy all selected file paths separated by ', '
		pathToCopy = strings.Join(panel.selectedToSlice(), ", ")
	} else {
		// Copy single file path at cursor
		pathToCopy = panel.element[panel.cursor].location
	}

	if err := writeClipboard(pathToCopy); err != nil {
		slog.Error("Error while copy path", "error", err)
	}
}

func (m *model) copyParentPath() {
	m.copyPWD()
}

// TODO: This is also an IO operations, do it via tea.Cmd
func (m *model) copyPWD() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if err := writeClipboard(panel.location); err != nil {
		slog.Error("Error while copy present working directory", "error", err)
	}
}
