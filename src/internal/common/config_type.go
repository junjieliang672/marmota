package common

// Theme configuration
type ThemeType struct {
	// Code syntax highlight theme
	CodeSyntaxHighlightTheme string `toml:"code_syntax_highlight"`

	// Border
	FilePanelBorder string `toml:"file_panel_border"`
	SidebarBorder   string `toml:"sidebar_border"`
	FooterBorder    string `toml:"footer_border"`

	// Border Active
	FilePanelBorderActive string `toml:"file_panel_border_active"`
	SidebarBorderActive   string `toml:"sidebar_border_active"`
	FooterBorderActive    string `toml:"footer_border_active"`
	ModalBorderActive     string `toml:"modal_border_active"`

	// Background (bg)
	FullScreenBG string `toml:"full_screen_bg"`
	FilePanelBG  string `toml:"file_panel_bg"`
	SidebarBG    string `toml:"sidebar_bg"`
	FooterBG     string `toml:"footer_bg"`
	ModalBG      string `toml:"modal_bg"`

	// Foreground (fg)
	FullScreenFG string `toml:"full_screen_fg"`
	FilePanelFG  string `toml:"file_panel_fg"`
	SidebarFG    string `toml:"sidebar_fg"`
	FooterFG     string `toml:"footer_fg"`
	ModalFG      string `toml:"modal_fg"`

	// Special Color
	Cursor             string   `toml:"cursor"`
	Correct            string   `toml:"correct"`
	Error              string   `toml:"error"`
	Hint               string   `toml:"hint"`
	Cancel             string   `toml:"cancel"`
	GradientColor      []string `toml:"gradient_color"`
	DirectoryIconColor string   `toml:"directory_icon_color"`

	// File Panel Special Items
	FilePanelTopDirectoryIcon string `toml:"file_panel_top_directory_icon"`
	FilePanelTopPath          string `toml:"file_panel_top_path"`
	FilePanelItemSelectedFG   string `toml:"file_panel_item_selected_fg"`
	FilePanelItemSelectedBG   string `toml:"file_panel_item_selected_bg"`
	// Optional. With a background the cursor row is drawn as a highlighted
	// bar in these colors; without one only the cursor icon marks it.
	FilePanelCursorFG string `toml:"file_panel_cursor_fg"`
	FilePanelCursorBG string `toml:"file_panel_cursor_bg"`
	// Optional. Folder names and icons in file panels take this color.
	FilePanelDirectoryFG string `toml:"file_panel_directory_fg"`

	// Sidebar Special Items
	SidebarTitle          string `toml:"sidebar_title"`
	SidebarItemSelectedFG string `toml:"sidebar_item_selected_fg"`
	SidebarItemSelectedBG string `toml:"sidebar_item_selected_bg"`
	SidebarDivider        string `toml:"sidebar_divider"`

	// Modal Special Items
	ModalCancelFG  string `toml:"modal_cancel_fg"`
	ModalCancelBG  string `toml:"modal_cancel_bg"`
	ModalConfirmFG string `toml:"modal_confirm_fg"`
	ModalConfirmBG string `toml:"modal_confirm_bg"`

	HelpMenuHotkey string `toml:"help_menu_hotkey"`
	HelpMenuTitle  string `toml:"help_menu_title"`
}

// Configuration settings
type ConfigType struct {
	Theme string `toml:"theme" comment:"Your theme: the name of a file in the theme folder, without .toml"`

	Editor                 string `toml:"editor"                    comment:"\nThe editor files will be opened with (e key). (Leave blank to use the EDITOR environment variable)."`
	SecondEditor           string `toml:"second_editor"             comment:"\nThe second editor for opening files (E key). Leave blank to use the same as 'editor'."`
	DirEditor              string `toml:"dir_editor"                comment:"\nThe editor directories will be opened with. (Leave blank to use the default editors)."`
	ShelfApp               string `toml:"shelf_app"                 comment:"\nThe macOS app the send_to_shelf key (Y) hands files to, such as Yoink or Dropover. (Leave blank for Marmota's own shelf, y-shelf, at the right edge of the screen.)"`
	CodexCommand           string `toml:"codex_command"             comment:"\nThe codex program Codex sessions start with. (Leave blank for the newest one on PATH or in the ChatGPT app. No -c or --config: with one, Codex runs the session out of Marmota's sight.)"`
	CdOnQuit               bool   `toml:"cd_on_quit"                comment:"\nCd on quit: when you quit, your shell moves to the folder you were in (through the y shell function build.sh writes)"`
	DefaultOpenFilePreview bool   `toml:"default_open_file_preview" comment:"\nWhether to open file preview automatically every time Marmota is opened."`
	ShowImagePreview       bool   `toml:"show_image_preview"        comment:"\nWhether to show image preview."`
	ShowPanelFooterInfo    bool   `toml:"show_panel_footer_info"    comment:"\nNo longer used: a file panel's place and selection count are in its title, the sort order in the sidebar."`
	DefaultDirectory       string `toml:"default_directory"         comment:"\nThe path of the first file panel when Marmota is opened."`
	FileSizeUseSI          bool   `toml:"file_size_use_si"          comment:"\nDisplay file sizes using powers of 1000 (kB, MB, GB) instead of powers of 1024 (KiB, MiB, GiB)."`
	DefaultSortType        int    `toml:"default_sort_type"         comment:"\nDefault sort type (0: Name, 1: Size, 2: Date Modified, 3: Type)."`
	SortOrderReversed      bool   `toml:"sort_order_reversed"       comment:"\nDefault sort order (false: Ascending, true: Descending)."`
	CaseSensitiveSort      bool   `toml:"case_sensitive_sort"       comment:"\nCase sensitive sort by name (capital \"B\" comes before \"a\" if true)."`
	ShellCloseOnSuccess    bool   `toml:"shell_close_on_success"    comment:"\nWhether to close the shell on successful command execution."`
	Debug                  bool   `toml:"debug"                     comment:"\nWhether to enable debug mode."`
	// IgnoreMissingFields controls whether warnings about missing TOML fields are suppressed.
	IgnoreMissingFields bool `toml:"ignore_missing_fields" comment:"\nWhether to ignore warnings about missing fields in the config file."`

	Nerdfont              bool   `toml:"nerdfont"               comment:"\n================   Style =================\n\n If you don't have or don't want Nerdfont installed you can turn this off"`
	ShowSelectIcons       bool   `toml:"show_select_icons"      comment:"\nShow checkbox icons in select mode (requires nerdfont)"`
	TransparentBackground bool   `toml:"transparent_background" comment:"\nSet transparent background or not (this only work when your terminal background is transparent)"`
	FilePreviewWidth      int    `toml:"file_preview_width"     comment:"\nFile preview width allow '0' (this mean same as file panel),'x' x must be less than 10 and greater than 1 (This means that the width of the file preview will be one xth of the total width.)"`
	CodePreviewer         string `toml:"code_previewer"         comment:"\nWhether to use the builtin syntax highlighting with chroma or use bat. Values: \"\" for builtin chroma, \"bat\" for bat"`

	BorderTop         string `toml:"border_top"          comment:"\nBorder style"`
	BorderBottom      string `toml:"border_bottom"`
	BorderLeft        string `toml:"border_left"`
	BorderRight       string `toml:"border_right"`
	BorderTopLeft     string `toml:"border_top_left"`
	BorderTopRight    string `toml:"border_top_right"`
	BorderBottomLeft  string `toml:"border_bottom_left"`
	BorderBottomRight string `toml:"border_bottom_right"`
	BorderMiddleLeft  string `toml:"border_middle_left"`
	BorderMiddleRight string `toml:"border_middle_right"`

	Metadata          bool `toml:"metadata"            comment:"\n==========PLUGINS========== #\nPlugins means that you need to install some external dependencies to use them.\n\nShow more detailed metadata, please install exiftool before enabling this plugin!"`
	EnableMD5Checksum bool `toml:"enable_md5_checksum" comment:"Enable MD5 checksum generation for files"`
	ZoxideSupport     bool `toml:"zoxide_support"      comment:"Zoxide support for the fast navigation"`

	// UI Enhancements
	ShowStatusLine bool `toml:"show_status_line" comment:"\n========== UI ENHANCEMENTS ==========\nShow bottom status line with useful hotkeys"`

	// Directory Preview Settings
	DirPreviewTreeView       bool `toml:"dir_preview_tree_view"        comment:"\nDirectory Preview Settings\nUse tree view for directory previews (shows nested structure)"`
	DirPreviewMaxDepth       int  `toml:"dir_preview_max_depth"        comment:"Maximum depth for tree view (0=list only, -1=unlimited, default=2)"`
	DirPreviewMaxFiles       int  `toml:"dir_preview_max_files"        comment:"Maximum number of files/dirs to show at root level (default=20)"`
	DirPreviewMaxFilesNested int  `toml:"dir_preview_max_files_nested" comment:"Maximum files to show in nested directories to prevent overwhelming (default=5)"`
	DirPreviewShowHidden     bool `toml:"dir_preview_show_hidden"      comment:"Show hidden files in directory preview"`
	DirPreviewCompactTree    bool `toml:"dir_preview_compact_tree"     comment:"Use compact tree symbols for directory preview"`
	DirPreviewShowCount      bool `toml:"dir_preview_show_count"       comment:"Show file/directory counts in tree view"`

	// IME integration (macOS). See src/pkg/ime.
	IMEASCIIHold    bool   `toml:"ime_ascii_hold"     comment:"\n========== IME INTEGRATION (macOS) ==========\nAsk the input method to pass keys through as plain ASCII while Marmota is\nin hotkey mode, releasing it only while a text field (rename, search, prompt)\nhas focus. Without this, a CJK input method swallows single-letter hotkeys\ninto its composition buffer before Marmota ever sees them.\nNeeds an input method that honours the ascii-hold protocol, such as MarmotIM.\nIgnored on other platforms."`
	IMEASCIIHoldDir string `toml:"ime_ascii_hold_dir" comment:"Directory the ascii-hold request files are written to.\nEmpty uses the default: ~/Library/Application Support/MarmotIM/ascii-hold"`
}

// GetIgnoreMissingFields reports whether warnings about missing TOML fields should be ignored.
func (c *ConfigType) GetIgnoreMissingFields() bool {
	return c.IgnoreMissingFields
}

type HotkeysType struct {
	Confirm []string `toml:"confirm" comment:"=================================================================================================\nGlobal hotkeys (cannot conflict with other hotkeys)"`
	Quit    []string `toml:"quit"`
	CdQuit  []string `toml:"cd_quit"`

	// movement
	ListUp     []string `toml:"list_up"     comment:"movement"`
	ListDown   []string `toml:"list_down"`
	ListTop    []string `toml:"list_top"`
	ListBottom []string `toml:"list_bottom"`
	PageUp     []string `toml:"page_up"`
	PageDown   []string `toml:"page_down"`

	CloseFilePanel         []string `toml:"close_file_panel"          comment:"file panel control"`
	CreateNewFilePanel     []string `toml:"create_new_file_panel"`
	NextFilePanel          []string `toml:"next_file_panel"`
	PreviousFilePanel      []string `toml:"previous_file_panel"`
	ToggleFilePreviewPanel []string `toml:"toggle_file_preview_panel"`
	OpenSortOptionsMenu    []string `toml:"open_sort_options_menu"`
	ToggleReverseSort      []string `toml:"toggle_reverse_sort"`

	// GoTo opens the s popup: pinned folders, sessions, folders zoxide
	// knows, files edited lately.
	GoTo                []string `toml:"go_to"`
	FocusOnMetaData     []string `toml:"focus_on_metadata"`
	JumpToUnreadSession []string `toml:"jump_to_unread_session"`
	NextSession         []string `toml:"next_session"`
	PreviousSession     []string `toml:"previous_session"`
	SendToAgent         []string `toml:"send_to_agent"`
	MoveSessionHere     []string `toml:"move_session_here"`
	OpenAskBar          []string `toml:"open_ask_bar"`

	FilePanelItemCreate []string `toml:"file_panel_item_create" comment:"create file/directory and rename "`
	FilePanelItemRename []string `toml:"file_panel_item_rename"`

	CopyItems              []string `toml:"copy_items"               comment:"file operate"`
	PasteItems             []string `toml:"paste_items"`
	CutItems               []string `toml:"cut_items"`
	DeleteItems            []string `toml:"delete_items"`
	PermanentlyDeleteItems []string `toml:"permanently_delete_items"`

	ExtractFile  []string `toml:"extract_file"  comment:"compress and extract"`
	CompressFile []string `toml:"compress_file"`

	OpenFileWithEditor             []string `toml:"open_file_with_editor"              comment:"editor"`
	OpenFileWithSecondEditor       []string `toml:"open_file_with_second_editor"`
	OpenCurrentDirectoryWithEditor []string `toml:"open_current_directory_with_editor"`
	SendToShelf                    []string `toml:"send_to_shelf"`
	ShowSessionFiles               []string `toml:"show_session_files"`
	DiffNextChange                 []string `toml:"diff_next_change"`
	DiffPreviousChange             []string `toml:"diff_previous_change"`
	PinViewer                      []string `toml:"pin_viewer"`

	PinnedDirectory []string `toml:"pinned_directory"  comment:"other"`
	ToggleDotFile   []string `toml:"toggle_dot_file"`
	ChangePanelMode []string `toml:"change_panel_mode"`
	OpenHelpMenu    []string `toml:"open_help_menu"`
	OpenCommandLine []string `toml:"open_command_line"`
	OpenAgentPrompt []string `toml:"open_agent_prompt"`

	CopyPath       []string `toml:"copy_path"`
	CopyParentPath []string `toml:"copy_parent_path"`
	CopyPWD        []string `toml:"copy_present_working_directory"`

	ToggleFooter []string `toml:"toggle_footer"`

	PdfNextPage     []string `toml:"pdf_next_page"     comment:"PDF preview navigation"`
	PdfPreviousPage []string `toml:"pdf_previous_page"`
	PdfFirstPage    []string `toml:"pdf_first_page"`

	ICloudFilterMenu    []string `toml:"icloud_filter_menu"    comment:"iCloud operations (macOS only)"`
	ICloudRefreshStatus []string `toml:"icloud_refresh_status"`
	ICloudDownload      []string `toml:"icloud_download"`
	ICloudEvict         []string `toml:"icloud_evict"`
	ICloudUpload        []string `toml:"icloud_upload"`

	ConfirmTyping []string `toml:"confirm_typing" comment:"=================================================================================================\nTyping hotkeys (can conflict with all hotkeys)"`
	CancelTyping  []string `toml:"cancel_typing"`

	ParentDirectory []string `toml:"parent_directory"  comment:"=================================================================================================\nNormal mode hotkeys (can conflict with other modes, cannot conflict with global hotkeys)"`
	SearchBar       []string `toml:"search_bar"`
	SearchInFolder  []string `toml:"search_in_folder"`
	PinParentFolder []string `toml:"pin_parent_folder"`

	FilePanelSelectModeItemsSelectDown []string `toml:"file_panel_select_mode_items_select_down" comment:"=================================================================================================\nSelect mode hotkeys (can conflict with other modes, cannot conflict with global hotkeys)"`
	FilePanelSelectModeItemsSelectUp   []string `toml:"file_panel_select_mode_items_select_up"`
	FilePanelSelectAllItem             []string `toml:"file_panel_select_all_items"`

	SelectModeToggleSelection []string `toml:"select_mode_toggle_selection" comment:"=================================================================================================\nSelect mode single-key operations (yazi-style)"`
	SelectModeCopy            []string `toml:"select_mode_copy"`
	SelectModeDelete          []string `toml:"select_mode_delete"`
	SelectModeCut             []string `toml:"select_mode_cut"`
	SelectModePaste           []string `toml:"select_mode_paste"`
	SelectModeZip             []string `toml:"select_mode_zip"`
	SelectModeExtract         []string `toml:"select_mode_extract"`
	ManageTags                []string `toml:"manage_tags"`

	SessionNew     []string `toml:"session_new" comment:"=================================================================================================\nSessions box hotkeys (only while the Sessions box at the foot has the keyboard, so they can reuse other keys)"`
	SessionMention []string `toml:"session_mention"`
	SessionRename  []string `toml:"session_rename"`
	SessionFork    []string `toml:"session_fork"`
	SessionStop    []string `toml:"session_stop"`

	// Whether a session the Ask bar starts runs in Claude Code or Codex.
	AskChooseAgent  []string `toml:"ask_choose_agent"  comment:"=================================================================================================\nAsk bar hotkeys (only while the Ask bar has the keyboard; it takes typing, so use keys that don't type text)"`
	AskChooseModel  []string `toml:"ask_choose_model"`
	AskChooseEffort []string `toml:"ask_choose_effort"`
	// The mode a session the Ask bar starts or resumes runs in.
	AskChoosePermissionMode []string `toml:"ask_choose_permission_mode"`
	AskClear                []string `toml:"ask_clear"`
	AskPrevious             []string `toml:"ask_previous"`
	AskNext                 []string `toml:"ask_next"`
	// Resume a session that has ended, picked from a list, in place of
	// starting one.
	AskResume []string `toml:"ask_resume"`
}
