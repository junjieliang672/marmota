package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/ui"
	"github.com/junjieliang672/marmota/src/internal/ui/rendering"
	filepreview "github.com/junjieliang672/marmota/src/pkg/file_preview"
	"github.com/junjieliang672/marmota/src/pkg/icloud"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/config/icon"
)

// The sidebar popup's place and size: over the left of the file panels.
const (
	// At the left edge: a panel's rows start with icons whose width the
	// terminal and superfile can count differently, and an overlay that
	// starts among them lands a column off.
	sidebarPopupMaxWidth = 52
	sidebarPopupMaxList  = 20
)

// sidebarListHeight is how many rows the popup's list has.
func (m *model) sidebarListHeight() int {
	return max(4, min(sidebarPopupMaxList, m.fullHeight-10)) //nolint:mnd // a short list; room for the frame
}

// popupWidth is the inside width of the s and z popups.
func (m *model) popupWidth() int {
	return max(20, min(sidebarPopupMaxWidth, m.fullWidth-common.BorderPadding)) //nolint:mnd // narrowest
}

// popupXY is where the s and z popups open: in the middle of the screen,
// placed by their list area and a one-line path, so a path that wraps
// doesn't move the top.
func (m *model) popupXY() (int, int) {
	width := m.popupWidth() + common.BorderPadding
	height := m.sidebarListHeight() + common.BorderPadding + 2 //nolint:mnd // the path line's rule and one line
	return max(0, (m.fullWidth-width)/common.CenterDivisor), max(0, (m.fullHeight-height)/common.CenterDivisor)
}

// sidebarPopupRender draws the sidebar as a popup: pinned folders, recent
// files and disks, then the picked row's whole path.
func (m *model) sidebarPopupRender() string {
	common.SidebarWidth = m.popupWidth()
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]
	footer := sidebarPathLines(m.sidebarModel.GetCurrentDirectoryLocation(), common.SidebarWidth)
	if id, ok := sidebar.SessionID(m.sidebarModel.GetCurrentDirectoryLocation()); ok {
		footer = m.sidebarSessionLines(id, common.SidebarWidth)
	}
	return m.sidebarModel.Render(m.sidebarListHeight(), true, panel.location, footer...)
}

// syncSidebarSessions gives the sidebar the live sessions to list.
func (m *model) syncSidebarSessions() {
	var entries []sidebar.SessionEntry
	for _, s := range m.dockSessions() {
		entries = append(entries, sidebar.SessionEntry{ID: s.ID, Title: s.DisplayTitle(), Codex: s.IsCodex(),
			Remote: s.RemoteControl})
	}
	m.sidebarModel.SetSessions(entries)
	m.sidebarModel.UpdateDirectories()
}

// sidebarSessionLines is the popup's foot on a session: enter writes to it,
// and where it works.
func (m *model) sidebarSessionLines(id string, width int) []string {
	s, ok := m.claude.snapshot.Find(id)
	if !ok {
		return nil
	}
	lines := sidebarPathLines(s.Cwd, width)
	st := common.PopupStyles(false)
	return append(lines, ansi.Truncate(st.Key.Render("enter")+st.Text.Render(" go to "+s.DisplayTitle()), width, "…"))
}

// sidebarPathLines is the picked row's whole path, wrapped at "/" to width
// rather than cut, under a rule.
func sidebarPathLines(path string, width int) []string {
	if path == "" {
		return nil
	}
	st := common.PopupStyles(false)
	lines := []string{st.Rule.Render(strings.Repeat("─", width))}
	const label = "path "
	for i, l := range strings.Split(ansi.Wrap(path, max(1, width-len(label)), "/"), "\n") {
		head := strings.Repeat(" ", len(label))
		if i == 0 {
			head = label
		}
		lines = append(lines, st.Muted.Render(head)+st.Value.Render(l))
	}
	return lines
}

// This also modifies the m.fileModel.filePanels, which it should not
// what modifications we do on this model object are of no consequence.
// Since bubblea passed this 'model' by value in View() function.
func (m *model) filePanelRender() string {
	f := make([]string, len(m.fileModel.filePanels))
	badges := m.fileBadges()
	for i, filePanel := range m.fileModel.filePanels {
		// check if cursor or render out of range
		// TODO - instead of this, have a filepanel.validateAndFix(), and log Error
		// This should not ever happen
		if filePanel.cursor > len(filePanel.element)-1 {
			filePanel.cursor = 0
			filePanel.render = 0
		}
		m.fileModel.filePanels[i] = filePanel

		// Calculate remainder for file panel space (excluding preview)
		var filePanelWidth int
		filePanelSpace := m.fullWidth -
			(common.BorderPadding + (len(m.fileModel.filePanels)-1)*common.BorderPadding)

		if m.fileModel.filePreview.IsOpen() {
			filePanelSpace -= m.fileModel.filePreview.GetWidth()
		}

		// Last panel ALWAYS gets the remainder to ensure exact width match
		if i == len(m.fileModel.filePanels)-1 && filePanelSpace%len(m.fileModel.filePanels) != 0 {
			filePanelWidth = m.fileModel.width + (filePanelSpace % len(m.fileModel.filePanels))
		} else {
			filePanelWidth = m.fileModel.width
		}

		f[i] = filePanel.renderWithBadges(m.mainPanelHeight, filePanelWidth, filePanel.isFocused, badges)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, f...)
}

func (panel *filePanel) Render(mainPanelHeight int, filePanelWidth int, focussed bool) string {
	return panel.renderWithBadges(mainPanelHeight, filePanelWidth, focussed, fileBadges{})
}

func (panel *filePanel) renderWithBadges(mainPanelHeight int, filePanelWidth int, focussed bool,
	badges fileBadges,
) string {
	r := ui.FilePanelRenderer(mainPanelHeight+common.BorderPadding, filePanelWidth+common.BorderPadding, focussed)

	panel.renderTitle(r, uint(len(panel.selected)))
	panel.renderTopBar(r, filePanelWidth)
	if panel.showsSearchRow() {
		panel.renderSearchBar(r)
	}
	panel.renderFileEntries(r, mainPanelHeight, filePanelWidth, badges)

	return r.Render()
}

// renderTitle names the folder in the panel's top border, with the cursor's
// place after it ("asset 9/15"), and how many items are selected in select
// mode.
func (panel *filePanel) renderTitle(r *rendering.Renderer, selectedCount uint) {
	name := filepath.Base(panel.location)
	switch panel.location {
	case variable.HomeDir:
		name = "~"
	case string(filepath.Separator):
		name = panel.location
	}
	r.SetBorderTitle(name)
	// The sort order, on the panel's bottom edge: there is no sidebar to
	// show it.
	if opts := panel.sortOptions.data; opts.selected >= 0 && opts.selected < len(opts.options) {
		kind := opts.options[opts.selected]
		r.SetBorderInfoItems(strings.ToLower(kind) + " " + sortArrow(kind, opts.reversed))
	}
	suffix := panel.getCursorString()
	if panel.panelMode == selectMode {
		suffix += fmt.Sprintf(" · %d selected", selectedCount)
	}
	r.SetBorderTitleSuffix(suffix)
}

// renderTopBar shows the panel's folder, then a divider above the items.
func (panel *filePanel) renderTopBar(r *rendering.Renderer, filePanelWidth int) {
	// TODO - Add ansitruncate left in renderer and remove truncation here
	path := panel.location
	if rel, ok := strings.CutPrefix(path, variable.HomeDir); ok && variable.HomeDir != "" &&
		(rel == "" || strings.HasPrefix(rel, string(filepath.Separator))) {
		path = "~" + rel
	}
	truncatedPath := common.TruncateTextBeginning(path, filePanelWidth-common.InnerPadding, "...")
	r.AddLines(common.FilePanelStyle.Render(" ") + common.FilePanelTopPathStyle.Render(truncatedPath))
	r.AddLines(common.FilePanelPathDividerStyle.Render(strings.Repeat(common.Config.BorderTop, filePanelWidth)))
}

func (panel *filePanel) renderSearchBar(r *rendering.Renderer) {
	// When typing, show mode indicator + input
	if panel.searchBar.Focused() {
		indicator := panel.searchStack.GetCurrentModeIndicator(
			panel.recursiveSearch,
			panel.searchBar.Value(), // Pass current search term
		)
		r.AddLines(" " + indicator + panel.searchBar.View())
		return
	}

	// Not typing - show full hierarchy if stack is not empty
	if !panel.searchStack.IsEmpty() {
		hierarchy := panel.searchStack.RenderHierarchy()
		r.AddLines(" " + hierarchy)
		return
	}

	// OLD: Fallback to old rendering for now (during migration)
	if len(panel.recursiveBaseElems) > 0 {
		var searchInfo string
		if panel.lastFilterTerm != "" {
			// Showing both recursive search and filter
			searchInfo = icon.Search + " " + panel.recursiveSearchTerm + "  " + icon.SortDesc + " " + panel.lastFilterTerm
		} else {
			// Showing only recursive search
			searchInfo = icon.Search + " " + panel.recursiveSearchTerm
		}
		r.AddLines(" " + searchInfo)
		return
	}

	// No active search - show empty search bar
	r.AddLines(" " + panel.searchBar.View())
}

func (panel *filePanel) renderFileEntries(r *rendering.Renderer, mainPanelHeight, filePanelWidth int,
	badges fileBadges,
) {
	if len(panel.element) == 0 {
		r.AddLines(common.FilePanelNoneText)
		return
	}

	end := min(panel.render+panel.elementHeight(mainPanelHeight), len(panel.element))
	lineNumberWidth := len(strconv.Itoa(len(panel.element)))

	for i := panel.render; i < end; i++ {
		if panel.renaming && i == panel.cursor {
			r.AddLines(panel.rename.View())
			continue
		}
		line, onCursorBar := panel.renderEntry(i, lineNumberWidth, filePanelWidth, badges)
		if onCursorBar {
			r.AddLineWithCustomTruncate(line, rendering.TailsTruncateRight)
		} else {
			r.AddLines(line)
		}
	}
}

// renderEntry draws the row of element i: cursor, line number, select box,
// iCloud icon, name, and at the end the row's badges. onCursorBar is set for
// the cursor row of a theme with a cursor bar, drawn on the bar to the edge.
func (panel *filePanel) renderEntry(i, lineNumberWidth, filePanelWidth int, badges fileBadges,
) (string, bool) {
	el := panel.element[i]
	_, isSelected := panel.selected[el.location]
	cursor := " "
	isCursor := i == panel.cursor && !panel.searchBar.Focused()
	if isCursor {
		cursor = icon.Cursor
	}
	// Themes with a cursor bar draw the whole cursor row on it.
	rowBG := common.FilePanelBGColor
	cursorBar := isCursor && common.FilePanelCursorRowBGColor != ""
	if cursorBar {
		rowBG = common.FilePanelCursorRowBGColor
	}

	selectBox := panel.renderSelectBox(isSelected, rowBG)
	lineNumber := fmt.Sprintf("%*d ", lineNumberWidth, i+1)

	// Render iCloud status icon (only in iCloud folders)
	iCloudIcon := panel.renderICloudIcon(el.iCloudStatus, rowBG)

	// Calculate the actual prefix width for proper alignment
	prefixWidth := lipgloss.Width(cursor+" ") + lipgloss.Width(lineNumber) +
		lipgloss.Width(selectBox) + lipgloss.Width(iCloudIcon)

	// ✳ and git's letter go at the end of the row; the name makes room.
	badge := badges.render(el.location, el.directory, rowBG)
	badgeWidth := ansi.StringWidth(badge)
	if badgeWidth > 0 {
		badgeWidth++ // a space before it
	}

	// Don't truncate items during search, or show full filename for cursor position
	noTruncate := (!panel.searchStack.IsEmpty() || (i == panel.cursor)) && badge == ""

	isLink := el.info.Mode()&os.ModeSymlink != 0
	renderedName := common.PrettierName(el.name, filePanelWidth-prefixWidth-badgeWidth, el.directory, isLink,
		isSelected, isCursor, rowBG, noTruncate)

	onBar := lipgloss.NewStyle().Foreground(common.FilePanelFGColor).Background(rowBG)
	cursorCell, number := common.FilePanelCursorStyle.Render(cursor+" "), lineNumber
	if cursorBar {
		cursorCell, number = common.FilePanelCursorStyle.Background(rowBG).Render(cursor+" "), onBar.Render(lineNumber)
	}
	line := cursorCell + number + selectBox + iCloudIcon + renderedName
	if badge != "" {
		line += padBeforeBadge(line, badge, filePanelWidth, rowBG)
	}
	if !cursorBar {
		return line, false
	}
	if gap := filePanelWidth - ansi.StringWidth(line); gap > 0 {
		line += onBar.Render(strings.Repeat(" ", gap))
	}
	return line, true
}

// padBeforeBadge returns the spaces that push a row's badge to its right
// edge, then the badge.
func padBeforeBadge(line, badge string, width int, bg lipgloss.Color) string {
	gap := max(1, width-ansi.StringWidth(line)-ansi.StringWidth(badge))
	return lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", gap)) + badge
}

// renderICloudIcon returns the iCloud status icon for a file.
// Only shows icons in iCloud folders.
func (panel *filePanel) renderICloudIcon(status icloud.Status, bg lipgloss.Color) string {
	if !panel.isICloudFolder {
		return ""
	}

	iconStr := status.Icon()
	if iconStr == "" {
		return ""
	}

	// Apply color based on status
	color := "#808080" // Gray for unknown
	switch status {
	case icloud.StatusPendingUpload, icloud.StatusUploading:
		color = "#FFA500" // Orange for pending upload
	case icloud.StatusSynced:
		color = "#32CD32" // Green for synced
	case icloud.StatusCloudOnly, icloud.StatusDownloading:
		color = "#4169E1" // Blue for cloud-only
	case icloud.StatusUnknown, icloud.StatusNotICloud:
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Background(bg).
		Render(iconStr + " ")
}

func (panel *filePanel) getCursorString() string {
	cursor := panel.cursor
	if len(panel.element) > 0 {
		cursor++ // Convert to 1-based
	}
	return fmt.Sprintf("%d/%d", cursor, len(panel.element))
}

func (panel *filePanel) renderSelectBox(isSelected bool, bg lipgloss.Color) string {
	if !common.Config.ShowSelectIcons || !common.Config.Nerdfont || panel.panelMode != selectMode {
		return ""
	}
	if bg != common.FilePanelBGColor {
		// On the cursor bar: the same boxes, drawn on the bar's color.
		color, box := common.FilePanelBorderColor, icon.CheckboxEmpty
		if panel.isFocused {
			color = common.FilePanelBorderActiveColor
		}
		if isSelected {
			box = icon.CheckboxChecked
		}
		return common.FilePanelSelectBoxStyle.Foreground(color).Background(bg).Render(box + icon.Space)
	}

	if panel.isFocused {
		if isSelected {
			return common.CheckboxCheckedFocused
		}
		return common.CheckboxEmptyFocused
	}
	if isSelected {
		return common.CheckboxChecked
	}
	return common.CheckboxEmpty
}

func (m *model) statusLineRender() string {
	right := m.claudeStatusSummary()
	if ops := m.opsStatus(); ops != "" {
		sep := lipgloss.NewStyle().Background(lipgloss.Color(common.Theme.FilePanelBG)).Render("   ")
		right = ops + sep + right
	}
	m.statusLineModel.SetRight(right)
	if m.focusPanel == dockSessionsFocus {
		return m.dockStatusLine()
	}
	if m.focusPanel == dockAskFocus {
		return m.dockAskStatusLine()
	}

	// Determine the current context for context-sensitive hotkeys
	panel := &m.fileModel.filePanels[m.filePanelFocusIndex]

	if panel.searchBar.Focused() {
		return m.statusLineModel.RenderForContext("search")
	}

	if panel.panelMode == selectMode {
		return m.statusLineModel.RenderForContext("select")
	}

	return m.statusLineModel.Render()
}

func (m *model) terminalSizeWarnRender() string {
	fullWidthString := strconv.Itoa(m.fullWidth)
	fullHeightString := strconv.Itoa(m.fullHeight)
	minimumWidthString := strconv.Itoa(common.MinimumWidth)
	minimumHeightString := strconv.Itoa(common.MinimumHeight)
	if m.fullHeight < common.MinimumHeight {
		fullHeightString = common.TerminalTooSmall.Render(fullHeightString)
	}
	if m.fullWidth < common.MinimumWidth {
		fullWidthString = common.TerminalTooSmall.Render(fullWidthString)
	}
	fullHeightString = common.TerminalCorrectSize.Render(fullHeightString)
	fullWidthString = common.TerminalCorrectSize.Render(fullWidthString)

	heightString := common.MainStyle.Render(" Height = ")
	return common.FullScreenStyle(m.fullHeight, m.fullWidth).Render(`Terminal size too small:`+"\n"+
		"Width = "+fullWidthString+
		heightString+fullHeightString+"\n\n"+
		"Needed for current config:"+"\n"+
		"Width = "+common.TerminalCorrectSize.Render(minimumWidthString)+
		heightString+common.TerminalCorrectSize.Render(minimumHeightString)) + filepreview.ClearKittyImages()
}

func (m *model) terminalSizeWarnAfterFirstRender() string {
	minimumWidthInt := common.FilePanelWidthUnit*len(
		m.fileModel.filePanels,
	) + common.FilePanelWidthUnit - 1
	minimumWidthString := strconv.Itoa(minimumWidthInt)
	fullWidthString := strconv.Itoa(m.fullWidth)
	fullHeightString := strconv.Itoa(m.fullHeight)
	minimumHeightString := strconv.Itoa(common.MinimumHeight)

	if m.fullHeight < common.MinimumHeight {
		fullHeightString = common.TerminalTooSmall.Render(fullHeightString)
	}
	if m.fullWidth < minimumWidthInt {
		fullWidthString = common.TerminalTooSmall.Render(fullWidthString)
	}
	fullHeightString = common.TerminalCorrectSize.Render(fullHeightString)
	fullWidthString = common.TerminalCorrectSize.Render(fullWidthString)

	heightString := common.MainStyle.Render(" Height = ")
	return common.FullScreenStyle(m.fullHeight, m.fullWidth).Render(`You change your terminal size too small:`+"\n"+
		"Width = "+fullWidthString+
		heightString+fullHeightString+"\n\n"+
		"Needed for current config:"+"\n"+
		"Width = "+common.TerminalCorrectSize.Render(minimumWidthString)+
		heightString+common.TerminalCorrectSize.Render(minimumHeightString)) + filepreview.ClearKittyImages()
}

func (m *model) typineModalRender() string {
	previewPath := filepath.Join(m.typingModal.location, m.typingModal.textInput.Value())

	fileLocation := common.FilePanelTopDirectoryIconStyle.Render(" "+icon.Directory+icon.Space) +
		common.FilePanelTopPathStyle.Render(
			common.TruncateTextBeginning(previewPath, common.ModalWidth-common.InnerPadding, "..."),
		) + "\n"

	confirm := common.ModalConfirm.Render(" (" + common.Hotkeys.ConfirmTyping[0] + ") Create ")
	cancel := common.ModalCancel.Render(" (" + common.Hotkeys.CancelTyping[0] + ") Cancel ")

	tip := confirm +
		lipgloss.NewStyle().Background(common.ModalBGColor).Render("           ") +
		cancel

	var err string
	if m.typingModal.errorMesssage != "" {
		err = "\n\n" + common.ModalErrorStyle.Render(m.typingModal.errorMesssage)
	}
	// TODO : Move this all to rendering package to avoid specifying newlines manually
	return common.ModalBorderStyle(common.ModalHeight, common.ModalWidth).
		Render(fileLocation + "\n" + m.typingModal.textInput.View() + "\n\n" + tip + err)
}

func (m *model) introduceModalRender() string {
	title := common.SidebarTitleStyle.Render(" Thanks for using Marmota!!") +
		common.ModalStyle.Render("\n You can read the following information before starting to use it!")
	vimUserWarn := common.ProcessErrorStyle.Render("  ** If you use Vim keys **, copy vimHotkeys.toml from the repository's\n" +
		"  src/superfile_config over your hotkeys.toml (y path-list prints where it is)")
	subOne := common.SidebarTitleStyle.Render("  (1)") +
		common.ModalStyle.Render(" If this is your first time, a short tour shows what is where:\n"+
			"      "+variable.TutorialURL)
	subTwo := common.SidebarTitleStyle.Render("  (2)") +
		common.ModalStyle.Render(" If you forget the relevant keys during use,\n"+
			"      you can press \"?\" (shift+/) at any time to query the keys!")
	subThree := common.SidebarTitleStyle.Render("  (3)") +
		common.ModalStyle.Render(" ! opens the Sessions box, K the Ask box, > starts Claude Code or\n"+
			"      Codex beside Marmota")
	subFour := common.SidebarTitleStyle.Render("  (4)") +
		common.ModalStyle.Render(" Thank you again for using Marmota.\n"+
			"      If you have any questions, please feel free to ask at:\n"+
			"      "+variable.RepoURL+"/issues\n"+
			"      Of course, you can always open a new issue to share your idea \n"+
			"      or report a bug!")
	return common.FirstUseModal(m.helpMenu.height, m.helpMenu.width).
		Render(title + "\n\n" + vimUserWarn + "\n\n" + subOne + "\n\n" +
			subTwo + "\n\n" + subThree + "\n\n" + subFour + "\n\n")
}

func (m *model) promptModalRender() string {
	return m.promptModal.Render()
}

// zoxideModalRender is the z popup, drawn as the s popup is: with the path
// line of where enter goes.
func (m *model) zoxideModalRender() string {
	m.zoxideModal.ShortPath = shortPath
	var footer []string
	if target, ok := m.zoxideModal.Target(); ok {
		footer = sidebarPathLines(target, m.zoxideModal.GetWidth()-common.BorderPadding)
	}
	return m.zoxideModal.Render(footer...)
}

func (m *model) helpMenuRender() string {
	r := ui.HelpMenuRenderer(m.helpMenu.height, m.helpMenu.width)
	r.AddLines(" " + m.helpMenu.searchBar.View())
	r.AddLines("") // one-line separation between searchbar and content

	totalTitleCount := 0
	cursorBeenTitleCount := 0

	for i, data := range m.helpMenu.filteredData {
		if data.subTitle != "" {
			if i < m.helpMenu.cursor {
				cursorBeenTitleCount++
			}
			totalTitleCount++
		}
	}

	lines, _, _ := m.helpMenuLines()
	m.helpMenuClampTop(len(lines))
	end := min(len(lines), m.helpMenu.renderIndex+m.helpMenuViewLines())
	r.AddLines(lines[m.helpMenu.renderIndex:end]...)

	current := m.helpMenu.cursor + 1 - cursorBeenTitleCount
	if len(m.helpMenu.filteredData) == 0 {
		current = 0
	}
	r.SetBorderInfoItems(fmt.Sprintf("%s/%s",
		strconv.Itoa(current),
		strconv.Itoa(len(m.helpMenu.filteredData)-totalTitleCount)))
	return r.Render()
}

// formatHotkeyForDisplay converts a key string to a display-friendly format
// e.g., space " " becomes "[space]" for visibility
func formatHotkeyForDisplay(key string) string {
	if key == " " {
		return "[space]"
	}
	return key
}

func (m *model) sortOptionsRender() string {
	panel := m.fileModel.filePanels[m.filePanelFocusIndex]
	var sortOptionsContent strings.Builder
	sortOptionsContent.WriteString(common.ModalTitleStyle.Render(" Sort Options") + "\n\n")
	for i, option := range panel.sortOptions.data.options {
		cursor := " "
		if i == panel.sortOptions.cursor {
			cursor = common.FilePanelCursorStyle.Render(icon.Cursor)
		}

		// Add arrow indicator for the currently active sort option
		arrow := ""
		if i == panel.sortOptions.data.selected {
			sortType := panel.sortOptions.data.options[i]
			arrow = " " + sortArrow(sortType, panel.sortOptions.data.reversed)
		}

		sortOptionsContent.WriteString(cursor + common.ModalStyle.Render(" "+option+arrow) + "\n")
	}
	bottomBorder := common.GenerateFooterBorder(fmt.Sprintf("%s/%s", strconv.Itoa(panel.sortOptions.cursor+1),
		strconv.Itoa(len(panel.sortOptions.data.options))), panel.sortOptions.width-common.BorderPadding)

	return common.SortOptionsModalBorderStyle(panel.sortOptions.height, panel.sortOptions.width,
		bottomBorder).Render(sortOptionsContent.String())
}

func (m *model) filePreviewPanelRender() string {
	if m.fileModel.renaming && m.fileModel.filePreview.IsOpen() {
		selectedItem := m.getFocusedFilePanel().getSelectedItemPtr()
		if selectedItem != nil && strings.EqualFold(filepath.Ext(selectedItem.location), ".pdf") {
			return m.fileModel.filePreview.RenderText(" Preview paused while renaming PDF...")
		}
	}
	return m.fileModel.filePreview.GetContent()
}

func (m *model) iCloudFilterRender() string {
	panel := m.fileModel.filePanels[m.filePanelFocusIndex]
	var content strings.Builder
	content.WriteString(common.ModalTitleStyle.Render(" iCloud Filter") + "\n\n")

	options := iCloudFilterOptions()
	for i, option := range options {
		cursor := " "
		if i == panel.iCloudFilter.cursor {
			cursor = common.FilePanelCursorStyle.Render(icon.Cursor)
		}

		// Add indicator for the currently active filter
		indicator := ""
		if i == int(panel.iCloudFilter.selected) {
			indicator = " *"
		}

		content.WriteString(cursor + common.ModalStyle.Render(" "+option+indicator) + "\n")
	}

	// Add separator and refresh option
	content.WriteString(common.ModalStyle.Render(" ──────────────") + "\n")
	content.WriteString(common.ModalStyle.Render(" Refresh (I)") + "\n")

	bottomBorder := common.GenerateFooterBorder(fmt.Sprintf("%d/%d",
		panel.iCloudFilter.cursor+1, len(options)), panel.iCloudFilter.width-common.BorderPadding)

	return common.SortOptionsModalBorderStyle(panel.iCloudFilter.height, panel.iCloudFilter.width,
		bottomBorder).Render(content.String())
}
