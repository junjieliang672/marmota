package internal

import (
	"os"
	"time"

	zoxidelib "github.com/lazysegtree/go-zoxide"

	"github.com/junjieliang672/marmota/src/internal/search"
	"github.com/junjieliang672/marmota/src/internal/ui/details"
	"github.com/junjieliang672/marmota/src/internal/ui/metadata"
	"github.com/junjieliang672/marmota/src/internal/ui/notify"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/ui/sidebar"
	"github.com/junjieliang672/marmota/src/internal/ui/statusline"
	"github.com/junjieliang672/marmota/src/pkg/icloud"
	"github.com/junjieliang672/marmota/src/pkg/ime"

	"github.com/charmbracelet/bubbles/textinput"

	jumpui "github.com/junjieliang672/marmota/src/internal/ui/jump"
	"github.com/junjieliang672/marmota/src/internal/ui/preview"
	"github.com/junjieliang672/marmota/src/internal/ui/prompt"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

// Type representing the mode of the panel
type panelMode uint

// Type representing the type of focused panel
type focusPanelType int

type hotkeyType int

type modelQuitStateType int

// TODO: Convert to integer enum
type sortingKind string

const (
	globalType hotkeyType = iota
	normalType
	selectType
)

// Constants for panel with no focus
const (
	nonePanelFocus focusPanelType = iota
	metadataFocus
	// dockSessionsFocus is the Sessions box at the foot (!).
	dockSessionsFocus
	// dockAskFocus is the Ask box at the foot (K), grown while it has it.
	dockAskFocus
)

// Constants for select mode or browser mode
const (
	selectMode panelMode = iota
	browserMode
)

const (
	notQuitting modelQuitStateType = iota
	quitInitiated
	quitConfirmationInitiated
	quitConfirmationReceived
	quitDone
)

// Main model
// TODO : We could consider using *model as tea.Model, instead of model.
// for reducing re-allocations. The struct is 20K bytes. But this could lead to
// issues like race conditions and whatnot, which are hidden since we are creating
// new model in each tea update.
type model struct {
	// Main Panels
	fileModel       fileModel
	sidebarModel    sidebar.Model
	processBarModel processbar.Model
	statusLineModel statusline.Model
	focusPanel      focusPanelType
	copyItems       copyItems

	// Modals
	notifyModel notify.Model
	typingModal typingModal
	helpMenu    helpMenuModal
	promptModal prompt.Model
	// jumpUI is the s popup; jump its side of the model.
	jumpUI jumpui.Model
	jump   jumpState
	resume resumeState
	// sessions is what the dock shows of Claude sessions: the Sessions box's
	// list, the Ask box's composer, the selected session's conversation.
	sessions sessionsState
	// ask is the read-only run answering a question from the Ask bar.
	ask askState

	// Zoxide client for directory tracking
	zClient *zoxidelib.Client

	fileMetaData metadata.Model
	// details is the box at the foot of the screen; macSampler reads the
	// machine for it every 2 seconds, and macStats is the last reading.
	details    details.Model
	macSampler *sysstat.Sampler
	macStats   *sysstat.Stats
	// dock is what the Sessions box at the foot keeps between visits.
	dock                 dockState
	diff                 diffView // the preview of a session's change to the cursor's file (W)
	ioReqCnt             int
	modelQuitState       modelQuitStateType
	firstTextInput       bool
	toggleDotFile        bool
	updatedToggleDotFile bool
	toggleFooter         bool
	firstLoadingComplete bool
	firstUse             bool

	// This entirely disables metadata fetching. Used in test model
	disableMetadata         bool
	filePanelFocusIndex     int
	wasSelectModeKeyHandled bool

	// Height in number of lines of actual viewport of
	// main panel and sidebar excluding border
	mainPanelHeight int

	// Height in number of lines of actual viewport of
	// footer panels - process/metadata/clipboard - excluding border
	footerHeight int
	fullWidth    int
	fullHeight   int

	// whether usable trash directory exists or not
	hasTrash bool

	// Git status debounce - prevents rapid refreshes during bulk file operations
	gitRefreshPending bool

	// Metadata debounce - prevents excessive I/O during rapid cursor movement
	metadataRefreshPending bool

	// Preview debounce - coalesces rapid cursor movement into a single preview render
	previewRefreshPending bool
	previewRenderInFlight bool
	previewRenderQueued   bool
	previewRenderForced   bool

	// Paths that need sudo rm to delete (permission denied)
	sudoDeletePaths []string

	// Pending numeric prefix for file-panel `[count]G` navigation.
	filePanelJumpCount string

	// Asks the input method to pass keys through as plain ASCII while
	// superfile is in hotkey mode. See src/pkg/ime and typing_state.go.
	imeHold ime.Hold

	// Whether the terminal reports us as focused. Starts true so terminals
	// that do not support focus reporting behave as they always have.
	//
	// The ascii-hold must be released on blur: superfile stays in hotkey mode
	// while it is not frontmost, and a hold left in place from a background
	// tab would disable Chinese input in whatever the user switched to.
	terminalFocused bool

	// Whether a child process currently owns the terminal (an editor, or the
	// sudo password prompt). While it does, superfile has no business telling
	// the input method what mode to be in.
	externalProcessRunning bool

	managedWeztermState  weztermPaneLayoutState
	managedWeztermPaneID int

	// Claude Code sessions: the status line, unread markers and the keys
	// that show sessions beside superfile.
	claude claudeSessionsState
	codex  codexState

	// Spec-006: shared per-model recursive search coordinator. Provides the
	// tree-index cache (cache hits on same-directory re-searches) and the
	// cancellation slot for in-flight walks. Nil-safe — paths that build a
	// raw `model{}` in tests fall back to a fresh coordinator on first use
	// via model.getSearchCoordinator().
	searchCoordinator *search.Coordinator
}

// Modal
type helpMenuModal struct {
	height       int
	width        int
	open         bool
	renderIndex  int
	cursor       int
	data         []helpMenuModalData
	filteredData []helpMenuModalData
	searchBar    textinput.Model
}

type helpMenuModalData struct {
	hotkey         []string
	description    string
	hotkeyWorkType hotkeyType
	subTitle       string
}

type typingModal struct {
	location      string
	open          bool
	textInput     textinput.Model
	errorMesssage string
}

// Copied items
type copyItems struct {
	items []string
	cut   bool
}

/* FILE WINDOWS TYPE START*/
// Model for file windows
type fileModel struct {
	filePanels   []filePanel
	width        int
	renaming     bool
	maxFilePanel int
	filePreview  preview.Model
}

// Panel representing a file
type filePanel struct {
	cursor           int
	render           int
	isFocused        bool
	location         string
	sortOptions      sortOptionsModel
	iCloudFilter     iCloudFilterModel // iCloud filter menu
	panelMode        panelMode
	selected         map[string]struct{} // O(1) lookup for selected items
	element          []element
	directoryRecords map[string]directoryRecord
	rename           textinput.Model
	renaming         bool
	searchBar        textinput.Model

	// Stack-based search hierarchy
	searchStack searchStack

	// Recursive search support
	recursiveSearch     bool             // when true, search recursively in subdirectories
	recursiveBaseElems  []element        // stores recursive search results for further filtering
	recursiveSearchTerm string           // the search term that generated recursiveBaseElems
	lastFilterTerm      string           // the last filter term applied to recursiveBaseElems
	lastFilterResults   []element        // cached filter results
	layerFilter         layerFilterCache // what is typed over a search layer, filtering it (liveLayerFilter)

	lastTimeGetElement time.Time
	targetFile         string // filename to position cursor on after load

	// Directory element cache - avoids re-reading dir on every tick.
	// Invalidated on directory change, sort change, dotfile toggle, file operations.
	cachedElements     []element
	cachedLocation     string
	cachedDotFile      bool
	cachedSortSelected int
	cachedSortReversed bool
	elementCacheValid  bool

	// iCloud status
	isICloudFolder bool // whether current folder is in iCloud

	// sortElsewhere is the panel's sort for other folders, kept while it
	// shows the Downloads folder, which opens newest first.
	sortElsewhere *sortOptionsModelData
}

// Sort options
type sortOptionsModel struct {
	width  int
	height int
	open   bool
	cursor int
	data   sortOptionsModelData
}

type sortOptionsModelData struct {
	options  []string
	selected int
	reversed bool
}

// iCloud filter options
type iCloudFilterModel struct {
	width    int
	height   int
	open     bool
	cursor   int
	selected icloud.ICloudFilter // currently active filter
}

// Record for directory navigation
type directoryRecord struct {
	directoryCursor int
	directoryRender int
}

// Element within a file panel
type element struct {
	name         string
	location     string
	directory    bool
	metaData     *metadata.Metadata // the last fetched, kept whole for Details
	info         os.FileInfo
	iCloudStatus icloud.Status // iCloud sync status (macOS only)
}

/* FILE WINDOWS TYPE END*/

type editorFinishedMsg struct{ err error }

// asyncRecursiveSearchMsg contains results from async recursive search
type asyncRecursiveSearchMsg struct {
	elements     []element
	searchTerm   string
	depthLimited bool
	timedOut     bool
	err          error
}

// asyncRecursiveSearchPartialMsg carries partial results emitted mid-walk by
// the streaming search engine (spec-006 decision 006-004 — streaming first-batch
// only). It mirrors search.AsyncSearchPartialMsg but stays inside the internal
// package so the model.Update type switch can dispatch without importing
// package-private types at every call site.
type asyncRecursiveSearchPartialMsg struct {
	elements   []element
	searchTerm string
	truncated  bool
}

type sliceOrderFunc func(i, j int) bool

// layerFilterCache is a search layer's list filtered by a term being typed.
type layerFilterCache struct {
	term    string
	base    int // the list's length when filtered
	results []element
}
