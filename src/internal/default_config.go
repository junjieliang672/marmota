package internal

import (
	"path/filepath"

	zoxidelib "github.com/lazysegtree/go-zoxide"

	"github.com/junjieliang672/marmota/src/internal/search"
	"github.com/junjieliang672/marmota/src/internal/ui/details"
	"github.com/junjieliang672/marmota/src/internal/ui/metadata"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"

	"github.com/junjieliang672/marmota/src/internal/common"
	jumpui "github.com/junjieliang672/marmota/src/internal/ui/jump"
	"github.com/junjieliang672/marmota/src/internal/ui/preview"
	"github.com/junjieliang672/marmota/src/internal/ui/prompt"
	"github.com/junjieliang672/marmota/src/pkg/ime"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

// Generate and return model containing default configurations for interface
// Maybe we can replace slice of strings with var args - Should we ?
// TODO: Move the configuration parameters to a ModelConfig struct.
// Something like `RendererConfig` struct for `Renderer` struct in ui/renderer package
func defaultModelConfig(toggleDotFile, toggleFooter, firstUse bool,
	firstPanelPaths []string, zClient *zoxidelib.Client) *model {
	return &model{
		filePanelFocusIndex: 0,
		focusPanel:          nonePanelFocus,
		processBarModel:     processbar.New(),
		sidebarModel:        sidebar.New(),
		statusLineModel:     statusline.New(),
		fileMetaData:        metadata.New(),
		details:             details.New(),
		macSampler:          &sysstat.Sampler{},
		fileModel: fileModel{
			filePanels:  filePanelSlice(firstPanelPaths),
			filePreview: preview.New(),
			width:       common.DefaultFilePanelWidth,
		},
		helpMenu:          newHelpMenuModal(),
		promptModal:       prompt.DefaultModel(prompt.PromptMinHeight, prompt.PromptMinWidth),
		jumpUI:            jumpui.New(),
		sessions:          newSessionsState(),
		zClient:           zClient,
		modelQuitState:    notQuitting,
		toggleDotFile:     toggleDotFile,
		toggleFooter:      toggleFooter,
		firstUse:          firstUse,
		hasTrash:          common.InitTrash(),
		searchCoordinator: search.NewCoordinator(search.FilePanelConfig()),

		imeHold: ime.New(common.Config.IMEASCIIHold, common.Config.IMEASCIIHoldDir),
		// Terminals that do not report focus never send FocusMsg, so assume
		// we have it until told otherwise.
		terminalFocused: true,
	}
}

func newHelpMenuModal() helpMenuModal {
	helpMenuData := getHelpMenuData()

	return helpMenuModal{
		renderIndex:  0,
		cursor:       1,
		data:         helpMenuData,
		filteredData: helpMenuData,
		open:         false,
		searchBar:    common.GenerateSearchBar(),
	}
}

// Return help menu for Hotkeys
//
//nolint:funlen,maintidx // one table of every entry, self contained
func getHelpMenuData() []helpMenuModalData {
	data := []helpMenuModalData{
		{
			subTitle: "General",
		},
		{
			hotkey:         []string{"y", ""},
			description:    "Open Marmota (in your shell)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.Confirm,
			description:    "Confirm your select or typing",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.Quit,
			description:    "Quit typing, modal or Marmota",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CdQuit,
			description:    "Quit Marmota and change directory to current folder",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ConfirmTyping,
			description:    "Confirm typing",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CancelTyping,
			description:    "Cancel/dismiss (typing, search, modals, select mode)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.OpenHelpMenu,
			description:    "Open help menu (hotkeylist)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.OpenAgentPrompt,
			description:    "Agent mode: start Claude Code or Codex beside Marmota (claude, claude rc, codex)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.OpenCommandLine,
			description:    "Run a shell command in the current folder",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"mouse wheel", ""},
			description:    "Over a preview, scroll it: the file preview (sideways too, with the trackpad or shift), or the session it shows",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Sessions (Claude Code and Codex)",
		},
		{
			hotkey:         common.Hotkeys.OpenAskBar,
			description:    "Ask box at the foot: write to the session it names (→), or after n in the Sessions box, a new one",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.JumpToUnreadSession,
			description:    "Sessions box at the foot: the session that most recently needs you, else the last one, else the first; again to go back",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.NextSession,
			description:    "Sessions box at the foot, on the next session (from the files: after the one it was last on); round at the end",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PreviousSession,
			description:    "Sessions box at the foot, on the previous session; in a session's files (W) these two go through the diff instead",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SendToAgent,
			description:    "Send the selection to the session beside Marmota",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.MoveSessionHere,
			description:    "Move the session beside Marmota (in the Sessions box, the selected one) here: Claude Code's /cd; Codex from its next turn",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Sessions box",
		},
		{
			hotkey:         []string{"esc", ""},
			description:    "Back to the files; q quits, as everywhere",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.Confirm,
			description:    "Open the session beside Marmota, with the keyboard",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{firstKey(common.Hotkeys.OpenAskBar), ""},
			description:    "Write to the session in the Ask box, or answer what it asks",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SessionNew,
			description:    "Start a new session in this folder, from the Ask box",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SessionMention,
			description:    "Mention the running session (@name) in the Ask draft, and copy it",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SessionRename,
			description:    "Rename the session (a running one with its own /rename)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SessionFork,
			description:    "Continue a copy of the session in a new git worktree",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SessionStop,
			description:    "Stop the session, after asking (the conversation is kept)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{firstKey(common.Hotkeys.CopyPath), ""},
			description:    "Copy the folder the session works in",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{firstKey(common.Hotkeys.SendToShelf), ""},
			description:    "Put the files the session changed on the shelf, as one pile, to drag into another app",
			hotkeyWorkType: globalType,
		},
		{
			hotkey: []string{
				firstKey(common.Hotkeys.OpenAgentPrompt) + "/" + firstKey(common.Hotkeys.OpenCommandLine), "",
			},
			description:    "Start a session (>claude, >codex) or run a command, in the panel's folder",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{firstKey(common.Hotkeys.OpenHelpMenu), ""},
			description:    "This help, from the Sessions box too",
			hotkeyWorkType: globalType,
		},
		{
			hotkey: []string{
				"[n]" + firstKey(common.Hotkeys.ListTop) + "/" + firstKey(common.Hotkeys.ListBottom), "",
			},
			description:    "The first or last session, or session n",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{firstKey(common.Hotkeys.PageUp) + "/" + firstKey(common.Hotkeys.PageDown), ""},
			description:    "Scroll the selected session's conversation in the preview",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Ask bar",
		},
		{
			hotkey:         []string{"enter", ""},
			description:    "Send: typed into the selected session, or after n, a new session in the background",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"up/down", ""},
			description:    "Earlier and later questions, as in Claude Code; in a question over several rows, a row first",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"pgup/pgdown", ""},
			description:    "Scroll the session above it a page (the mouse wheel over it scrolls too)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskChoosePermissionMode,
			description:    "The permission mode a session it starts runs in (bypass, accept edits, auto, plan, manual)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskChooseAgent,
			description:    "Whether a session it starts runs in Claude Code or Codex",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskChooseModel,
			description:    "Choose the model",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskChooseEffort,
			description:    "Choose the effort",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskClear,
			description:    "Clear the question (it stays otherwise, whichever session you pick)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskPrevious,
			description:    "The question sent before",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskNext,
			description:    "The question sent after, back to what you were writing",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.AskResume,
			description:    "Writing to a new session: resume an ended one instead, picked from a list of Claude Code's or Codex's",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"esc/" + firstKey(common.Hotkeys.OpenAskBar), ""},
			description:    "Back to the list; your question stays",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Panel navigation",
		},
		{
			hotkey:         common.Hotkeys.CreateNewFilePanel,
			description:    "Create new file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CloseFilePanel,
			description:    "Close the focused file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ToggleFilePreviewPanel,
			description:    "Toggle file preview panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.OpenSortOptionsMenu,
			description:    "Open sort options menu",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ToggleReverseSort,
			description:    "Toggle reverse sort",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ToggleFooter,
			description:    "Toggle footer",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.NextFilePanel,
			description:    "Focus on the next file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PreviousFilePanel,
			description:    "Focus on the previous file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.GoTo,
			description:    "Go to a pinned folder, a session (running, or ended to resume), a folder zoxide knows, a file edited lately",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.FocusOnMetaData,
			description:    "Focus Details (file, operations, this Mac); ↑↓ or the wheel scroll it",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Panel movement",
		},
		{
			hotkey:         common.Hotkeys.ListUp,
			description:    "Up",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ListDown,
			description:    "Down",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ListTop,
			description:    "Go to top",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ListBottom,
			description:    "Go to bottom",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PageUp,
			description:    "Page up",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PageDown,
			description:    "Page down",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"[count]g/G", ""},
			description:    "File panel, or picking in the s list: jump to item number (not while typing search)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.ParentDirectory,
			description:    "Return to parent folder",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.FilePanelSelectAllItem,
			description:    "Select all items in focused file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.FilePanelSelectModeItemsSelectUp,
			description:    "Select up with your course",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.FilePanelSelectModeItemsSelectDown,
			description:    "Select down with your course",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Selection Mode (yazi-style)",
		},
		{
			hotkey:         common.Hotkeys.SelectModeToggleSelection,
			description:    "Toggle selection (enters select mode)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SelectModeCopy,
			description:    "Copy selected items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.SelectModeCut,
			description:    "Cut selected items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.SelectModeDelete,
			description:    "Delete selected items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.SelectModePaste,
			description:    "Paste items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.SelectModeZip,
			description:    "Zip selected items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.SelectModeExtract,
			description:    "Extract selected archives",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.ManageTags,
			description:    "Manage tags on selected items",
			hotkeyWorkType: selectType,
		},
		{
			hotkey:         common.Hotkeys.ToggleDotFile,
			description:    "Toggle dot file display",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SearchBar,
			description:    "Toggle active search bar",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.SearchInFolder,
			description:    "Search files inside folder at cursor",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PinParentFolder,
			description:    "File panel: pin parent folder of cursor (normal mode, search inactive)",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ChangePanelMode,
			description:    "Change between selection mode or normal mode",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PinnedDirectory,
			description:    "Pin the item, or unpin a pinned one (in the s list too)",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "File operations",
		},
		{
			hotkey:         common.Hotkeys.FilePanelItemCreate,
			description:    "Create file or folder(end with " + string(filepath.Separator) + " to create a folder)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.FilePanelItemRename,
			description:    "Rename file or folder",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CopyItems,
			description:    "Copy selected items to the clipboard",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CutItems,
			description:    "Cut selected items to the clipboard",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PasteItems,
			description:    "Paste clipboard items into the current file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.DeleteItems,
			description:    "Delete selected items",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PermanentlyDeleteItems,
			description:    "Permanently delete selected items",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CopyPath,
			description:    "Copy the highlighted file or folder's path (in the s list too)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CopyParentPath,
			description:    "Copy the path of the folder the file panel shows",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.CopyPWD,
			description:    "Copy the path of the current folder",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ExtractFile,
			description:    "Extract compressed file",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.CompressFile,
			description:    "Compress (zip) file or folder to .zip file",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.OpenFileWithEditor,
			description:    "Open file with your default editor; in select mode, every selected file",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.OpenFileWithSecondEditor,
			description:    "Open file with second editor",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.OpenCurrentDirectoryWithEditor,
			description:    "Open current directory with default editor (Finder highlights the selected items)",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ShowSessionFiles,
			description:    "The files a session changed, over the panel's folder (again or esc: the folder); the preview shows each one's diff",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.PinViewer,
			description:    "Pin the preview to a session while you browse files (again: unpin)",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.DiffNextChange,
			description:    "In a session's files (W): the diff's next change",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.DiffPreviousChange,
			description:    "In a session's files (W): the diff's previous change",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.SendToShelf,
			description:    "Put selected items on the shelf, to drag them into another app",
			hotkeyWorkType: normalType,
		},
		{
			subTitle: "PDF Preview Navigation",
		},
		{
			hotkey:         common.Hotkeys.PdfNextPage,
			description:    "Next page in PDF preview",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PdfPreviousPage,
			description:    "Previous page in PDF preview",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         common.Hotkeys.PdfFirstPage,
			description:    "Jump to first page in PDF preview",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "Go to (s)",
		},
		{
			hotkey:         []string{"enter", ""},
			description:    "Searching: the first match (↵: pinned, folders and files before sessions), or home with nothing typed; picking: the picked item",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"↓", ""},
			description:    "Pick an item: its keys are the ones it takes elsewhere (t, r, p, e, m, Y; a session's are the Sessions box's)",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"←/→", ""},
			description:    "Picking: the other column, on the nearest row",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"tab", ""},
			description:    "One section alone, the next each time; then every section again",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"shift+↑/↓", ""},
			description:    "Picking a pin: move it up or down the pinned list",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"ctrl+n", ""},
			description:    "The item's folder in a new file panel",
			hotkeyWorkType: globalType,
		},
		{
			hotkey:         []string{"esc", ""},
			description:    "Back one step: picking to searching, then clear the search, then close",
			hotkeyWorkType: globalType,
		},
		{
			subTitle: "iCloud Operations (macOS)",
		},
		{
			hotkey:         common.Hotkeys.ICloudFilterMenu,
			description:    "Open iCloud filter menu",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ICloudRefreshStatus,
			description:    "Refresh iCloud status",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ICloudDownload,
			description:    "Download from iCloud",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ICloudEvict,
			description:    "Evict local copy (keep in cloud)",
			hotkeyWorkType: normalType,
		},
		{
			hotkey:         common.Hotkeys.ICloudUpload,
			description:    "Trigger upload to iCloud",
			hotkeyWorkType: normalType,
		},
	}

	return data
}
