package internal

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/pkg/icloud"
)

// iCloud filter menu dimensions
const (
	iCloudFilterMenuWidth  = 20
	iCloudFilterMenuHeight = 8
)

// iCloudFilterOptions are the iCloud filter menu's entries.
func iCloudFilterOptions() []string {
	return []string{"All files", "Cloud only", "Local only", "Cloud & local"}
}

// openICloudFilterMenu opens the iCloud filter menu (only in iCloud folders)
func (m *model) openICloudFilterMenu() {
	panel := m.getFocusedFilePanel()

	// Only allow filter menu in iCloud folders
	if !panel.isICloudFolder {
		return
	}

	panel.iCloudFilter.open = true
	panel.iCloudFilter.width = iCloudFilterMenuWidth
	panel.iCloudFilter.height = iCloudFilterMenuHeight
}

// cancelICloudFilter closes the iCloud filter menu without applying changes
func (m *model) cancelICloudFilter() {
	panel := m.getFocusedFilePanel()
	panel.iCloudFilter.open = false
	panel.iCloudFilter.cursor = int(panel.iCloudFilter.selected) // Reset cursor to current selection
}

// confirmICloudFilter applies the selected filter and closes the menu
func (m *model) confirmICloudFilter() {
	panel := m.getFocusedFilePanel()
	panel.iCloudFilter.selected = icloud.ICloudFilter(panel.iCloudFilter.cursor)
	panel.iCloudFilter.open = false
	panel.invalidateElementCache()

	// Apply filter to elements
	m.applyICloudFilter()
}

// applyICloudFilter filters the file panel elements based on iCloud status
func (m *model) applyICloudFilter() {
	panel := m.getFocusedFilePanel()

	if panel.iCloudFilter.selected == icloud.FilterAll {
		// No filtering needed - reload elements
		panel.element = returnDirElement(panel.location, m.toggleDotFile, panel.sortOptions.data)
		return
	}

	// Get all elements first
	allElements := returnDirElement(panel.location, m.toggleDotFile, panel.sortOptions.data)

	// Filter based on iCloud status
	filtered := make([]element, 0, len(allElements))
	for _, elem := range allElements {
		if panel.iCloudFilter.selected.Matches(elem.iCloudStatus) {
			filtered = append(filtered, elem)
		}
	}

	panel.element = filtered

	// Reset cursor if out of bounds
	if panel.cursor >= len(panel.element) {
		panel.cursor = max(0, len(panel.element)-1)
		panel.render = 0
	}
}

// refreshICloudStatus reloads iCloud status for all files
func (m *model) refreshICloudStatus() {
	panel := m.getFocusedFilePanel()

	// Only works in iCloud folders
	if !panel.isICloudFolder {
		return
	}

	// Store current filter
	currentFilter := panel.iCloudFilter.selected

	// Temporarily set to all to get full list
	panel.iCloudFilter.selected = icloud.FilterAll

	// Reload elements (this fetches fresh iCloud status)
	icloud.ClearStatusCache()
	panel.invalidateElementCache()
	panel.element = returnDirElement(panel.location, m.toggleDotFile, panel.sortOptions.data)

	// Re-apply the filter
	panel.iCloudFilter.selected = currentFilter
	if currentFilter != icloud.FilterAll {
		m.applyICloudFilter()
	}
}

// iCloudFilterListUp moves cursor up in iCloud filter menu
func (m *model) iCloudFilterListUp() {
	panel := m.getFocusedFilePanel()
	if panel.iCloudFilter.cursor > 0 {
		panel.iCloudFilter.cursor--
	} else {
		panel.iCloudFilter.cursor = len(iCloudFilterOptions()) - 1
	}
}

// iCloudFilterListDown moves cursor down in iCloud filter menu
func (m *model) iCloudFilterListDown() {
	panel := m.getFocusedFilePanel()
	if panel.iCloudFilter.cursor < len(iCloudFilterOptions())-1 {
		panel.iCloudFilter.cursor++
	} else {
		panel.iCloudFilter.cursor = 0
	}
}

// iCloudFilterKey handles key inputs inside iCloud filter menu
func (m *model) iCloudFilterKey(msg string) {
	switch {
	case slices.Contains(common.Hotkeys.ICloudFilterMenu, msg):
		m.cancelICloudFilter()
	case slices.Contains(common.Hotkeys.CancelTyping, msg):
		m.cancelICloudFilter()
	case slices.Contains(common.Hotkeys.Quit, msg):
		m.cancelICloudFilter()
	case slices.Contains(common.Hotkeys.Confirm, msg):
		m.confirmICloudFilter()
	case slices.Contains(common.Hotkeys.ListUp, msg):
		m.iCloudFilterListUp()
	case slices.Contains(common.Hotkeys.ListDown, msg):
		m.iCloudFilterListDown()
	case slices.Contains(common.Hotkeys.ICloudRefreshStatus, msg):
		// Refresh can be triggered from within the menu
		m.refreshICloudStatus()
		m.cancelICloudFilter()
	}
}

// iCloudDownload downloads selected files from iCloud
func (m *model) iCloudDownload() tea.Cmd {
	panel := m.getFocusedFilePanel()

	if !panel.isICloudFolder {
		return nil
	}

	// Get paths to download
	var paths []string
	if panel.panelMode == selectMode && len(panel.selected) > 0 {
		paths = panel.selectedToSlice()
	} else if panel.cursor >= 0 && panel.cursor < len(panel.element) {
		paths = []string{panel.element[panel.cursor].location}
	}

	if len(paths) == 0 {
		return nil
	}

	slog.Info("iCloud download", "paths", paths)

	// Run operation in background with process tracking
	processBarModel := &m.processBarModel
	return func() tea.Msg {
		state := iCloudDownloadOperation(processBarModel, paths)
		return iCloudOperationCompleteMsg{operation: "download", state: state}
	}
}

// iCloudEvict removes local copies of selected files
func (m *model) iCloudEvict() tea.Cmd {
	panel := m.getFocusedFilePanel()

	if !panel.isICloudFolder {
		return nil
	}

	// Get paths to evict
	var paths []string
	if panel.panelMode == selectMode && len(panel.selected) > 0 {
		paths = panel.selectedToSlice()
	} else if panel.cursor >= 0 && panel.cursor < len(panel.element) {
		paths = []string{panel.element[panel.cursor].location}
	}

	if len(paths) == 0 {
		return nil
	}

	slog.Info("iCloud evict", "paths", paths)

	// Run operation in background with process tracking
	processBarModel := &m.processBarModel
	return func() tea.Msg {
		state := iCloudEvictOperation(processBarModel, paths)
		return iCloudOperationCompleteMsg{operation: "evict", state: state}
	}
}

// iCloudUpload triggers upload for selected files
func (m *model) iCloudUpload() tea.Cmd {
	panel := m.getFocusedFilePanel()

	if !panel.isICloudFolder {
		return nil
	}

	// Get paths to upload
	var paths []string
	if panel.panelMode == selectMode && len(panel.selected) > 0 {
		paths = panel.selectedToSlice()
	} else if panel.cursor >= 0 && panel.cursor < len(panel.element) {
		paths = []string{panel.element[panel.cursor].location}
	}

	if len(paths) == 0 {
		return nil
	}

	slog.Info("iCloud upload", "paths", paths)

	// Run operation in background with process tracking
	processBarModel := &m.processBarModel
	return func() tea.Msg {
		state := iCloudUploadOperation(processBarModel, paths)
		return iCloudOperationCompleteMsg{operation: "upload", state: state}
	}
}

// iCloudOperationCompleteMsg is sent when an iCloud operation completes
type iCloudOperationCompleteMsg struct {
	operation string
	state     processbar.ProcessState
}

// iCloudOperation runs act on each of the paths, with progress tracking:
// glyph and name are the process's icon and what it does ("download").
func iCloudOperation(processBarModel *processbar.Model, paths []string, glyph, name string,
	act func(string) error,
) processbar.ProcessState {
	if len(paths) == 0 {
		return processbar.Cancelled
	}

	processName := glyph + icon.Space + filepath.Base(paths[0])
	if len(paths) > 1 {
		processName = glyph + icon.Space + fmt.Sprintf("%d files", len(paths))
	}

	p, err := processBarModel.SendAddProcessMsg(processName, len(paths), true)
	if err != nil {
		slog.Error("Cannot spawn a new process for iCloud "+name, "error", err)
		return processbar.Failed
	}

	var failed int
	for i, path := range paths {
		if err := act(path); err != nil {
			slog.Error("iCloud "+name+" failed", "path", path, "error", err)
			failed++
		}
		p.Done = i + 1
		p.Name = glyph + icon.Space + filepath.Base(path)
		processBarModel.TrySendingUpdateProcessMsg(p)
	}

	if failed > 0 {
		p.State = processbar.Failed
	} else {
		p.State = processbar.Successful
	}
	processBarModel.TrySendingUpdateProcessMsg(p)

	return p.State
}

// iCloudDownloadOperation performs the download with progress tracking
func iCloudDownloadOperation(processBarModel *processbar.Model, paths []string) processbar.ProcessState {
	return iCloudOperation(processBarModel, paths, icon.CloudDown, "download", icloud.Download)
}

// iCloudEvictOperation performs the evict with progress tracking
func iCloudEvictOperation(processBarModel *processbar.Model, paths []string) processbar.ProcessState {
	return iCloudOperation(processBarModel, paths, icon.CloudEvict, "evict", icloud.Evict)
}

// iCloudUploadOperation performs the upload with progress tracking
func iCloudUploadOperation(processBarModel *processbar.Model, paths []string) processbar.ProcessState {
	if len(paths) == 0 {
		return processbar.Cancelled
	}

	processName := icon.Upload + icon.Space + filepath.Base(paths[0])
	if len(paths) > 1 {
		processName = icon.Upload + icon.Space + fmt.Sprintf("%d files", len(paths))
	}

	p, err := processBarModel.SendAddProcessMsg(processName, len(paths), true)
	if err != nil {
		slog.Error("Cannot spawn a new process for iCloud upload", "error", err)
		return processbar.Failed
	}

	var skipped int
	for i, path := range paths {
		success := icloud.TriggerUpload(path)
		if !success {
			skipped++
		}
		p.Done = i + 1
		p.Name = icon.Upload + icon.Space + filepath.Base(path)
		processBarModel.TrySendingUpdateProcessMsg(p)
	}

	// Upload is successful even if some files failed
	p.State = processbar.Successful
	processBarModel.TrySendingUpdateProcessMsg(p)

	slog.Info("iCloud upload complete", "total", len(paths), "skipped", skipped)
	return p.State
}
