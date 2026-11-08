package internal

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"

	variable "github.com/junjieliang672/marmota/src/config"
)

// Pin or unpin the exact item at the cursor.
func (m *model) pinnedDirectory() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	// Check if panel is empty
	if len(panel.element) == 0 {
		return
	}

	// Get item at cursor
	item := panel.element[panel.cursor]

	err := m.sidebarModel.TogglePinnedDirectory(item.location)
	if err != nil {
		slog.Error("Error while toggling pinned item", "error", err)
	}
}

func (m *model) pinParentDirectoryOfCursor() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	if len(panel.element) == 0 {
		return
	}

	item := panel.element[panel.cursor]
	locationToPin := filepath.Dir(item.location)
	if locationToPin == "" {
		return
	}

	if err := m.sidebarModel.TogglePinnedDirectory(locationToPin); err != nil {
		slog.Error("Error while toggling parent directory from file panel", "error", err)
	}
}

func (m *model) sidebarPinnedDirectory() {
	location := m.sidebarModel.GetCurrentDirectoryLocation()
	if location == "" {
		return
	}

	if m.sidebarModel.IsCurrentDirectoryPinned() {
		err := m.sidebarModel.TogglePinnedDirectory(location)
		if err != nil {
			slog.Error("Error while toggling pinned item from sidebar", "error", err)
			return
		}

		m.sidebarModel.UpdateDirectories()
		return
	}

	if !m.sidebarModel.IsCurrentItemRecentEdit() {
		return
	}

	err := m.sidebarModel.RemoveRecentEdit(location)
	if err != nil {
		slog.Error("Error while removing recent edit from sidebar", "error", err)
		return
	}

	m.sidebarModel.UpdateDirectories()
}

// Create new file panel
func (m *model) createNewFilePanel(location string) error {
	// In case we have model width and height zero, maxFilePanel would be 0
	// But we would have len() here as 1. Hence there would be discrepency here.
	// Although this is not possible in actual usage, and can be only reproduced in tests.
	if len(m.fileModel.filePanels) == m.fileModel.maxFilePanel {
		// TODO : Define as a predefined error in errors.go
		return errors.New("maximum panel count reached")
	}

	if location == "" {
		location = variable.HomeDir
	}

	if _, err := os.Stat(location); err != nil {
		return fmt.Errorf("cannot access location : %s", location)
	}

	// Create new panel, with the focused panel's sort for folders other than
	// Downloads
	focused := &m.fileModel.filePanels[m.filePanelFocusIndex]
	newPanel := filePanel{
		location:         location,
		sortOptions:      focused.sortOptions,
		panelMode:        browserMode,
		isFocused:        false,
		selected:         make(map[string]struct{}),
		directoryRecords: make(map[string]directoryRecord),
		searchBar:        common.GenerateSearchBar(),
	}
	newPanel.sortOptions.data = focused.ownSort()
	newPanel.sortOptions.cursor = newPanel.sortOptions.data.selected
	newPanel.followFolderSort()

	// Insert new panel immediately after current panel (not at the end)
	insertIdx := m.filePanelFocusIndex + 1
	m.fileModel.filePanels = slices.Insert(m.fileModel.filePanels, insertIdx, newPanel)

	if m.fileModel.filePreview.IsOpen() {
		// File preview panel width same as file panel
		if common.Config.FilePreviewWidth == 0 {
			m.fileModel.filePreview.SetWidth((m.fullWidth -
				(common.BorderPadding + (len(m.fileModel.filePanels))*common.BorderPadding)) / (len(m.fileModel.filePanels) + 1))
		} else {
			m.fileModel.filePreview.SetWidth(
				(m.fullWidth) / common.Config.FilePreviewWidth,
			)
		}
	}

	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = false
	m.fileModel.filePanels[m.filePanelFocusIndex+1].isFocused = returnFocusType(m.focusPanel)
	m.fileModel.width = (m.fullWidth - m.fileModel.filePreview.GetWidth() -
		(common.BorderPadding + (len(m.fileModel.filePanels)-1)*common.BorderPadding)) / len(m.fileModel.filePanels)
	m.filePanelFocusIndex++

	m.fileModel.maxFilePanel = (m.fullWidth - m.fileModel.filePreview.GetWidth()) / common.FilePanelWidthUnit

	for i := range m.fileModel.filePanels {
		m.fileModel.filePanels[i].searchBar.Width = m.fileModel.width - common.InnerPadding
	}
	return nil
}

// Close current focus file panel
func (m *model) closeFilePanel() {
	if len(m.fileModel.filePanels) == 1 {
		return
	}

	m.fileModel.filePanels = append(m.fileModel.filePanels[:m.filePanelFocusIndex],
		m.fileModel.filePanels[m.filePanelFocusIndex+1:]...)

	if m.fileModel.filePreview.IsOpen() {
		// File preview panel width same as file panel
		if common.Config.FilePreviewWidth == 0 {
			m.fileModel.filePreview.SetWidth((m.fullWidth -
				(common.BorderPadding + (len(m.fileModel.filePanels))*common.BorderPadding)) / (len(m.fileModel.filePanels) + 1))
		} else {
			m.fileModel.filePreview.SetWidth(
				(m.fullWidth) / common.Config.FilePreviewWidth,
			)
		}
	}

	if m.filePanelFocusIndex != 0 {
		m.filePanelFocusIndex--
	}

	m.fileModel.width = (m.fullWidth - m.fileModel.filePreview.GetWidth() -
		(common.BorderPadding + (len(m.fileModel.filePanels)-1)*common.BorderPadding)) / len(m.fileModel.filePanels)
	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = returnFocusType(m.focusPanel)

	m.fileModel.maxFilePanel = (m.fullWidth - m.fileModel.filePreview.GetWidth()) / common.FilePanelWidthUnit

	for i := range m.fileModel.filePanels {
		m.fileModel.filePanels[i].searchBar.Width = m.fileModel.width - common.InnerPadding
	}
}

func (m *model) toggleFilePreviewPanel() {
	m.fileModel.filePreview.ToggleOpen()
	m.fileModel.filePreview.SetWidth(0)
	m.fileModel.filePreview.SetHeight(m.mainPanelHeight + common.BorderPadding)
	if m.fileModel.filePreview.IsOpen() {
		// File preview panel width same as file panel
		if common.Config.FilePreviewWidth == 0 {
			m.fileModel.filePreview.SetWidth((m.fullWidth -
				(common.BorderPadding + (len(m.fileModel.filePanels))*common.BorderPadding)) / (len(m.fileModel.filePanels) + 1))
		} else {
			m.fileModel.filePreview.SetWidth(
				(m.fullWidth) / common.Config.FilePreviewWidth,
			)
		}
	}

	m.fileModel.width = (m.fullWidth - m.fileModel.filePreview.GetWidth() -
		(common.BorderPadding + (len(m.fileModel.filePanels)-1)*common.BorderPadding)) / len(m.fileModel.filePanels)

	m.fileModel.maxFilePanel = (m.fullWidth - m.fileModel.filePreview.GetWidth()) / common.FilePanelWidthUnit

	for i := range m.fileModel.filePanels {
		m.fileModel.filePanels[i].searchBar.Width = m.fileModel.width - common.InnerPadding
	}
}

// Focus on next file panel
func (m *model) nextFilePanel() {
	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = false
	if m.filePanelFocusIndex == (len(m.fileModel.filePanels) - 1) {
		m.filePanelFocusIndex = 0
	} else {
		m.filePanelFocusIndex++
	}

	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = returnFocusType(m.focusPanel)
}

// Focus on previous file panel
func (m *model) previousFilePanel() {
	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = false
	if m.filePanelFocusIndex == 0 {
		m.filePanelFocusIndex = (len(m.fileModel.filePanels) - 1)
	} else {
		m.filePanelFocusIndex--
	}

	m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = returnFocusType(m.focusPanel)
}

// Focus on sidebar
func (m *model) focusOnSideBar() {
	m.syncSidebarSessions()
	if m.focusPanel == sidebarFocus {
		m.focusPanel = nonePanelFocus
		m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = true
	} else {
		m.focusPanel = sidebarFocus
		m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = false
	}
}

// focusOnMetadata gives the Details box the keyboard, or gives it back. A
// hidden Details box (F) is shown first.
func (m *model) focusOnMetadata() tea.Cmd {
	var cmd tea.Cmd
	if !m.toggleFooter {
		cmd = m.toggleFooterController()
	}

	if m.focusPanel == metadataFocus {
		m.focusPanel = nonePanelFocus
		m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = true
	} else {
		m.focusPanel = metadataFocus
		m.fileModel.filePanels[m.filePanelFocusIndex].isFocused = false
	}
	return cmd
}

// searchInFolderAtCursor enables recursive search in the current panel directory
func (m *model) searchInFolderAtCursor() {
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	// Enable recursive search mode
	panel.recursiveSearch = true

	// Open the search bar
	m.searchBarFocus()
}

// pdfNextPage navigates to the next page in PDF preview
func (m *model) pdfNextPage() tea.Cmd {
	slog.Debug("pdfNextPage called")

	// Check if preview panel is open
	if !m.fileModel.filePreview.IsOpen() {
		slog.Debug("pdfNextPage: preview panel not open")
		return nil
	}

	// Get the currently previewed file location
	currentLocation := m.fileModel.filePreview.GetLocation()
	if currentLocation == "" {
		return nil
	}

	// Check if it's a PDF file
	if !strings.HasSuffix(strings.ToLower(currentLocation), ".pdf") {
		return nil
	}

	// Get PDF previewer and navigate to next page
	pdfPreviewer := m.fileModel.filePreview.GetPdfPreviewer()
	if pdfPreviewer == nil {
		return nil
	}

	// Try to go to next page
	if pdfPreviewer.NextPage() {
		// Force preview and metadata refresh
		previewCmd := m.getFilePreviewCmd(true)
		metadataCmd := m.getMetadataCmd()
		return tea.Batch(previewCmd, metadataCmd)
	}

	return nil
}

// pdfPreviousPage navigates to the previous page in PDF preview
func (m *model) pdfPreviousPage() tea.Cmd {
	// Check if preview panel is open
	if !m.fileModel.filePreview.IsOpen() {
		return nil
	}

	// Get the currently previewed file location
	currentLocation := m.fileModel.filePreview.GetLocation()
	if currentLocation == "" {
		return nil
	}

	// Check if it's a PDF file
	if !strings.HasSuffix(strings.ToLower(currentLocation), ".pdf") {
		return nil
	}

	// Get PDF previewer and navigate to previous page
	pdfPreviewer := m.fileModel.filePreview.GetPdfPreviewer()
	if pdfPreviewer == nil {
		return nil
	}

	// Try to go to previous page
	if pdfPreviewer.PreviousPage() {
		// Force preview and metadata refresh
		previewCmd := m.getFilePreviewCmd(true)
		metadataCmd := m.getMetadataCmd()
		return tea.Batch(previewCmd, metadataCmd)
	}

	return nil
}

// pdfFirstPage jumps to the first page in PDF preview
func (m *model) pdfFirstPage() tea.Cmd {
	// Check if preview panel is open
	if !m.fileModel.filePreview.IsOpen() {
		return nil
	}

	// Get the currently previewed file location
	currentLocation := m.fileModel.filePreview.GetLocation()
	if currentLocation == "" {
		return nil
	}

	// Check if it's a PDF file
	if !strings.HasSuffix(strings.ToLower(currentLocation), ".pdf") {
		return nil
	}

	// Get PDF previewer and jump to first page
	pdfPreviewer := m.fileModel.filePreview.GetPdfPreviewer()
	if pdfPreviewer == nil {
		return nil
	}

	// Try to jump to first page
	if pdfPreviewer.FirstPage() {
		// Force preview and metadata refresh
		previewCmd := m.getFilePreviewCmd(true)
		metadataCmd := m.getMetadataCmd()
		return tea.Batch(previewCmd, metadataCmd)
	}

	return nil
}
