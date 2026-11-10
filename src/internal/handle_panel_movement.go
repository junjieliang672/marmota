package internal

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"

	variable "github.com/junjieliang672/marmota/src/config"
)

// Back to parent directory
func (m *model) parentDirectory() {
	err := m.getFocusedFilePanel().parentDirectory()
	if err != nil {
		slog.Error("Error while changing to parent directory", "error", err)
	}
}

// Enter directory or open file with default application
// TODO: Unit test this
func (m *model) enterPanel() {
	panel := m.getFocusedFilePanel()

	if len(panel.element) == 0 {
		return
	}
	selectedItem := panel.getSelectedItem()
	if selectedItem.directory {
		targetPath := selectedItem.location

		if selectedItem.info.Mode()&os.ModeSymlink != 0 {
			var symlinkErr error
			targetPath, symlinkErr = filepath.EvalSymlinks(targetPath)
			if symlinkErr != nil {
				return
			}

			// targetPath shouldn't be a link now, so Stat and Lstat should be same
			if targetInfo, lstatErr := os.Lstat(targetPath); lstatErr != nil || !targetInfo.IsDir() {
				return
			}
		}
		// TODO : Propagate error out from this this function. Return here, instead of logging
		err := m.updateCurrentFilePanelDir(targetPath)
		if err != nil {
			slog.Error("Error while changing to directory", "error", err, "target", targetPath)
		}
		return
	}

	if variable.ChooserFile != "" {
		chooserErr := m.chooserFileWriteAndQuit(panel.element[panel.cursor].location)
		if chooserErr == nil {
			return
		}
		// Continue with preview if file is not writable
		slog.Error("Error while writing to chooser file, continuing with file open", "error", chooserErr)
	}
	m.executeOpenCommand()
}

func (m *model) executeOpenCommand() {
	panel := m.getFocusedFilePanel()

	filePath := panel.element[panel.cursor].location

	openCommand := "xdg-open"
	switch runtime.GOOS {
	case utils.OsDarwin:
		openCommand = "open"
	case utils.OsWindows:
		dllpath := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "rundll32.exe")
		dllfile := "url.dll,FileProtocolHandler"

		cmd := exec.Command(dllpath, dllfile, filePath) //nolint:gosec // Windows' own handler, on the file you chose
		err := cmd.Start()
		if err != nil {
			slog.Error("Error while open file with", "error", err)
		}

		return
	}

	cmd := exec.Command(openCommand, filePath)
	utils.DetachFromTerminal(cmd)
	err := cmd.Start()
	if err != nil {
		slog.Error("Error while open file with", "error", err)
	}
}

// Select all item in the file panel (only work on select mode)
func (m *model) selectAllItem() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	for _, item := range panel.element {
		panel.selected[item.location] = struct{}{}
	}
}

// Select the item where cursor located (only work on select mode)
func (panel *filePanel) singleItemSelect() {
	if len(panel.element) > 0 && panel.cursor >= 0 && panel.cursor < len(panel.element) {
		elementLocation := panel.element[panel.cursor].location

		if _, exists := panel.selected[elementLocation]; exists {
			delete(panel.selected, elementLocation)
		} else {
			panel.selected[elementLocation] = struct{}{}
		}
	}
}

// toggleSelectionYaziStyle enters select mode and toggles current item selection (yazi-style)
func (m *model) toggleSelectionYaziStyle() {
	panel := m.getFocusedFilePanel()

	// Enter select mode if not already
	if panel.panelMode != selectMode {
		panel.panelMode = selectMode
	}

	// Toggle selection on current item
	panel.singleItemSelect()
}

// Toggle dotfile display or not
func (m *model) toggleDotFileController() {
	m.toggleDotFile = !m.toggleDotFile
	m.updatedToggleDotFile = true
	for i := range m.fileModel.filePanels {
		m.fileModel.filePanels[i].invalidateElementCache()
	}
	err := utils.WriteBoolFile(variable.ToggleDotFile, m.toggleDotFile)
	if err != nil {
		slog.Error("Error while updating toggleDotFile data", "error", err)
	}
}

// Toggle footer display or not
func (m *model) toggleFooterController() tea.Cmd {
	m.toggleFooter = !m.toggleFooter
	err := utils.WriteBoolFile(variable.ToggleFooter, m.toggleFooter)
	if err != nil {
		slog.Error("Error while updating toggleFooter data", "error", err)
	}
	// Recalculate heights when footer is toggled
	m.setHeightValues(m.fullHeight)
	// Update footer panel dimensions
	m.setDetailsSize()
	// File preview panel requires explicit height update, unlike sidebar/file panels
	// which receive height as render parameters and update automatically on each frame
	if m.fileModel.filePreview.IsOpen() {
		m.setFilePreviewPanelSize()
		// Force re-render of preview content with new dimensions
		return m.getFilePreviewCmd(true)
	}
	return nil
}

// Focus on search bar
func (m *model) searchBarFocus() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	m.clearFilePanelJumpCount()
	if panel.searchBar.Focused() {
		panel.searchBar.Blur()
	} else {
		// If we have recursive base elements from a previous search,
		// clear the search bar so user can type a new filter
		if len(panel.recursiveBaseElems) > 0 {
			panel.searchBar.SetValue("")
		}
		panel.searchBar.Focus()
		m.firstTextInput = true
		// The search row takes a row from the items; keep the cursor on screen.
		panel.scrollToCursor(m.mainPanelHeight)
	}

	// config search bar width
	panel.searchBar.Width = m.fileModel.width - common.InnerPadding
}
