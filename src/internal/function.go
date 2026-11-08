package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/search"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/icloud"
)

const (
	sortingName         sortingKind = "Name"
	sortingSize         sortingKind = "Size"
	sortingDateModified sortingKind = "Date Modified"
	sortingFileType     sortingKind = "Type"
)

var suffixRegexp = regexp.MustCompile(`^(.*)\((\d+)\)$`)

// parseSearchTerm extracts the file type filter and the clean query from a
// search term. Syntax: "f:query" (files only), "d:query" (dirs only),
// "query" (all)
func parseSearchTerm(searchTerm string) (string, string) {
	if strings.HasPrefix(searchTerm, "f:") {
		return search.FilterTypeFiles, strings.TrimPrefix(searchTerm, "f:")
	}
	if strings.HasPrefix(searchTerm, "d:") {
		return search.FilterTypeDirs, strings.TrimPrefix(searchTerm, "d:")
	}
	return "", searchTerm
}

// shouldIncludeByFilter checks if an item matches the file type filter
func shouldIncludeByFilter(isDirectory bool, filterType string) bool {
	switch filterType {
	case search.FilterTypeFiles:
		return !isDirectory
	case search.FilterTypeDirs:
		return isDirectory
	default:
		return true
	}
}

// Check if the directory is external disk path
// TODO : This function should be give two directories, and it should return
// if the two share a different disk partition.
// Ideally we shouldn't even try to figure that out in our file operations, and let OS handles it.
// But at least right now its not okay. This returns if `path` is an External disk
// from perspective of `/`, but it should tell from perspective of currently open directory
// The usage of this function in cut/paste is not as expected.
func isExternalDiskPath(path string) bool {
	// This is very vague. You cannot tell if a path is belonging to an external partition
	// if you dont define the source path to compare with
	// But making this true will cause slow file operations based on current implementation
	if runtime.GOOS == utils.OsWindows {
		return false
	}

	// exclude timemachine on MacOS
	if strings.HasPrefix(path, "/Volumes/.timemachine") {
		return false
	}

	// to filter out mounted partitions like /, /boot etc
	return strings.HasPrefix(path, "/mnt") ||
		strings.HasPrefix(path, "/media") ||
		strings.HasPrefix(path, "/run/media") ||
		strings.HasPrefix(path, "/Volumes")
}

// isInTrashDirectory checks if the given path is inside the system trash directory.
// When deleting from trash, we should permanently delete instead of trying to move to trash again.
func isInTrashDirectory(path string) bool {
	switch runtime.GOOS {
	case utils.OsDarwin:
		return strings.HasPrefix(path, variable.DarwinTrashDirectory)
	case utils.OsLinux:
		return strings.HasPrefix(path, variable.LinuxTrashDirectory)
	default:
		return false
	}
}

func returnFocusType(focusPanel focusPanelType) bool {
	return focusPanel == nonePanelFocus
}

// TODO : Take common.Config.CaseSensitiveSort as a function parameter
// and also consider testing this caseSensitive with both true and false in
// our unit_test TestReturnDirElement
// dirEntryInfo pairs a DirEntry with its pre-fetched FileInfo to avoid redundant stat calls.
type dirEntryInfo struct {
	entry os.DirEntry
	info  os.FileInfo
}

func returnDirElement(location string, displayDotFile bool, sortOptions sortOptionsModelData) []element {
	dirEntries, err := os.ReadDir(location)
	if err != nil {
		slog.Error("Error while returning folder elements", "error", err)
		return nil
	}

	// Filter and collect FileInfo in a single pass to avoid calling Info() twice per entry.
	entries := make([]dirEntryInfo, 0, len(dirEntries))
	for _, e := range dirEntries {
		if strings.HasPrefix(e.Name(), ".") && !displayDotFile {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entries = append(entries, dirEntryInfo{entry: e, info: info})
	}

	// No files/directories to process
	if len(entries) == 0 {
		return nil
	}
	return sortFileElement(sortOptions, entries, location)
}

// returnDirElementBySearchString runs the non-recursive '/' search on a
// single directory. Results are ordered by FZF relevance (best match first).
// Explicit sort-kind override via the `o` menu is applied in confirmSortOptions.
func returnDirElementBySearchString(location string, displayDotFile bool, searchString string) []element {
	// Parse filter prefix
	filterType, cleanQuery := parseSearchTerm(searchString)

	items, err := os.ReadDir(location)
	if err != nil {
		slog.Error("Error while return folder element function", "error", err)
		return nil
	}

	if len(items) == 0 {
		return nil
	}

	entries, names := searchableEntries(items, displayDotFile, filterType)
	// fzf returns matches ordered by score (best first). Preserve that order
	// as the default for search results. The user's explicit `o` sort choice
	// takes over only after confirmSortOptions toggles the layer's
	// manualSortOverride flag.
	fzfResults := utils.FzfSearch(cleanQuery, names)

	results := make([]element, 0, len(fzfResults))
	for _, match := range fzfResults {
		results = append(results, elementOf(location, entries[match.Key]))
	}
	// Surface each item's iCloud status, as sortFileElement does, without
	// the sort step.
	setICloudStatus(location, results)
	return results
}

// searchableEntries are a folder's items a search looks at, by name, and
// their names: hidden ones only with displayDotFile, and only those the file
// type filter takes.
func searchableEntries(items []os.DirEntry, displayDotFile bool, filterType string,
) (map[string]dirEntryInfo, []string) {
	entries := map[string]dirEntryInfo{}
	names := []string{}
	for _, item := range items {
		fileInfo, err := item.Info()
		if err != nil {
			continue
		}
		if !displayDotFile && strings.HasPrefix(fileInfo.Name(), ".") {
			continue
		}
		if !shouldIncludeByFilter(item.IsDir(), filterType) {
			continue
		}
		names = append(names, item.Name())
		entries[item.Name()] = dirEntryInfo{entry: item, info: fileInfo}
	}
	return entries, names
}

// elementOf is the element for an item of the folder location.
func elementOf(location string, item dirEntryInfo) element {
	return element{
		name:      item.entry.Name(),
		directory: item.entry.IsDir() || isSymlinkToDir(location, item.info, item.entry.Name()),
		location:  filepath.Join(location, item.entry.Name()),
		info:      item.info,
	}
}

// setICloudStatus gives each element its iCloud status when location is an
// iCloud folder, fetched for all of them at once.
func setICloudStatus(location string, elements []element) {
	if len(elements) == 0 || !icloud.IsICloudFolder(location) {
		return
	}
	paths := make([]string, len(elements))
	for i, e := range elements {
		paths[i] = e.location
	}
	statusMap := icloud.GetBatchStatusCached(paths)
	for i := range elements {
		if status, ok := statusMap[elements[i].location]; ok {
			elements[i].iCloudStatus = status
		}
	}
}

const MaxSearchDepth = 5 // Maximum directory depth for recursive search

// recursivelyCollectFiles walks a directory tree and collects all files as elements.
// This returns elements with name=filename (for FZF search) and location=fullpath (for navigation).
// Hitting the depth limit isn't reported: the elements found up to it are returned.
//
// Per spec-006 decision 006-005 this function now routes through the shared
// search.Coordinator so the live-typing recursive path reuses the same
// tree-index cache as the post-Enter async path. First keystroke: cache miss,
// walk runs. Subsequent keystrokes on the same (dir, showHidden, filterType):
// cache hit, FZF-only cost.
//
// If coord is nil (tests that construct `recursivelyCollectFiles` without a
// coordinator) a transient coordinator is constructed locally — cache benefit
// is lost but correctness is preserved.
func recursivelyCollectFiles(
	coord *search.Coordinator,
	rootPath string,
	displayDotFile bool,
	filterType string,
) ([]element, error) {
	if coord == nil {
		coord = search.NewCoordinator(search.FilePanelConfig())
	}
	source := panelSearchSource{baseDir: rootPath, showHidden: displayDotFile}
	items, _, err := coord.CollectCached(context.Background(), source, filterType)
	if err != nil {
		return nil, err
	}

	elements := make([]element, 0, len(items))
	for _, s := range items {
		info, _ := s.GetMetadata().(os.FileInfo)
		elements = append(elements, element{
			name:      s.GetName(),
			location:  s.GetLocation(),
			directory: s.IsDirectory(),
			info:      info,
		})
	}
	return elements, nil
}

// returnDirElementBySearchStringRecursive searches recursively through directory tree
// Searches by filename only (element.name), making it consistent with filterElementsBySearchString
//
// Default result order is FZF relevance (best match first). Explicit sort via
// the `o` menu applies in confirmSortOptions which sets
// searchLayer.manualSortOverride and re-sorts cachedElems directly. See
// spec-006 live-perf notes.
func returnDirElementBySearchStringRecursive(
	coord *search.Coordinator,
	location string,
	displayDotFile bool,
	searchString string,
) []element {
	// Parse filter prefix
	filterType, cleanQuery := parseSearchTerm(searchString)

	// Collect all files recursively with filter applied
	elements, err := recursivelyCollectFiles(coord, location, displayDotFile, filterType)
	if err != nil {
		slog.Error("Error while recursively collecting files", "error", err)
		return nil
	}

	if len(elements) == 0 {
		return nil
	}

	// Build haystack from element.name (filename only) - CONSISTENT with filterElementsBySearchString
	haystack := make([]string, len(elements))
	for i, elem := range elements {
		haystack[i] = elem.name // Search by filename, not full path
	}

	// FZF search with clean query
	fzfResults := utils.FzfSearch(cleanQuery, haystack)

	// Map results back to elements — ordered by FZF score (best first).
	// No secondary alphabetical sort: search results use fzf-relevance by
	// default; user opt-in via `o` menu reshuffles via sortElements in
	// confirmSortOptions.
	results := make([]element, 0, len(fzfResults))
	for _, result := range fzfResults {
		results = append(results, elements[result.HayIndex])
	}

	return results
}

// startAsyncRecursiveSearch returns a Cmd that performs search in background.
//
// Pre-spec-006 this function ran its own filepath.Walk + FZF pipeline inline.
// After spec-006 T5 it delegates to the shared search.Coordinator which
// provides the tree-index cache (AC-PERF-5 / AC-MEM-2), cancellation slot
// (AC-PERF-6 / AC-CORRECT-3) and streaming first-batch partial messages
// (AC-PERF-3). The returned cmd still emits the legacy
// asyncRecursiveSearchMsg so model.Update's existing handler keeps working.
func startAsyncRecursiveSearch(m *model, location string, displayDotFile bool, searchTerm string) tea.Cmd {
	coord := m.getSearchCoordinator()
	source := panelSearchSource{baseDir: location, showHidden: displayDotFile}

	cmd := coord.StartSearch(searchTerm, source, true)
	if cmd == nil {
		return nil
	}

	// Unwrap the coordinator's tea.Msg (search.AsyncSearchCompleteMsg) into
	// the internal-package asyncRecursiveSearchMsg that Update already
	// understands. This keeps the migration minimally invasive per decision
	// 006-005 (keep the old path as a thin wrapper).
	return func() tea.Msg {
		raw := cmd()
		return coordinatorMsgToInternal(raw, searchTerm)
	}
}

// coordinatorMsgToInternal adapts the search-package complete/partial messages
// to the internal package's legacy msg types. Returns nil for unknown or
// suppressed messages (e.g. a nil returned by a cancelled walk — see
// Coordinator.startAsyncRecursiveSearch).
func coordinatorMsgToInternal(msg tea.Msg, originalSearchTerm string) tea.Msg {
	switch m := msg.(type) {
	case search.AsyncSearchCompleteMsg:
		return asyncRecursiveSearchMsg{
			elements:     searchablesToElements(m.Results),
			searchTerm:   originalSearchTerm,
			depthLimited: m.DepthLimited,
			timedOut:     m.TimedOut,
			err:          m.Error,
		}
	case search.AsyncSearchPartialMsg:
		return asyncRecursiveSearchPartialMsg{
			elements:   searchablesToElements(m.Results),
			searchTerm: originalSearchTerm,
			truncated:  m.Truncated,
		}
	case nil:
		return nil
	default:
		return msg
	}
}

// searchablesToElements converts the generic search.Searchable slice returned
// by the search package back into the internal `element` type used by the file
// panel's rendering pipeline.
func searchablesToElements(items []search.Searchable) []element {
	if len(items) == 0 {
		return nil
	}
	out := make([]element, 0, len(items))
	for _, s := range items {
		info, _ := s.GetMetadata().(os.FileInfo)
		out = append(out, element{
			name:      s.GetName(),
			location:  s.GetLocation(),
			directory: s.IsDirectory(),
			info:      info,
		})
	}
	return out
}

// panelSearchSource adapts a file panel's (location, showHidden) tuple to the
// search.SearchSource interface consumed by the Coordinator. Only the
// GetBaseDirectory method is exercised by the recursive walk path — GetItems
// is only called for non-recursive modes which this source never invokes.
type panelSearchSource struct {
	baseDir    string
	showHidden bool
}

func (p panelSearchSource) GetItems() []search.Searchable { return nil }
func (p panelSearchSource) GetBaseDirectory() string      { return p.baseDir }

// ShowHidden satisfies search.HiddenAwareSource so the Coordinator's recursive
// walk uses the same dotfile visibility as the live-typing path (spec-006
// post-Enter / live-typing parity).
func (p panelSearchSource) ShowHidden() bool { return p.showHidden }

// getSearchCoordinator returns the per-model search coordinator, lazily
// constructing one on first use so naked `&model{}` test builders Just Work.
func (m *model) getSearchCoordinator() *search.Coordinator {
	if m.searchCoordinator == nil {
		m.searchCoordinator = search.NewCoordinator(search.FilePanelConfig())
	}
	return m.searchCoordinator
}

// listenForSearchPartialCmd returns a blocking tea.Cmd that waits for the next
// search.AsyncSearchPartialMsg from the coordinator's partial-msg channel,
// converts it to the internal asyncRecursiveSearchPartialMsg shim, and delivers
// that to the Bubble Tea runtime. Caller is responsible for re-registering this
// cmd after handling each partial to keep the listener active across searches.
func (m *model) listenForSearchPartialCmd() tea.Cmd {
	coord := m.getSearchCoordinator()
	inner := coord.ListenForPartial()
	if inner == nil {
		return nil
	}
	return func() tea.Msg {
		raw := inner()
		partial, ok := raw.(search.AsyncSearchPartialMsg)
		if !ok {
			return raw
		}
		return asyncRecursiveSearchPartialMsg{
			elements:   searchablesToElements(partial.Results),
			searchTerm: partial.Query,
			truncated:  partial.Truncated,
		}
	}
}

// filterElementsBySearchString filters existing elements using fzf search.
// Default order is FZF relevance; the user's explicit `o` sort-kind choice
// takes over via confirmSortOptions (manualSortOverride).
func filterElementsBySearchString(elements []element, searchString string) []element {
	if len(elements) == 0 {
		return nil
	}

	// Parse filter prefix
	filterType, cleanQuery := parseSearchTerm(searchString)

	// Build haystack from filtered elements
	haystack := make([]string, 0, len(elements))
	elementMap := make(map[string]element)

	for _, elem := range elements {
		// Apply file type filter
		if !shouldIncludeByFilter(elem.directory, filterType) {
			continue
		}

		haystack = append(haystack, elem.name)
		elementMap[elem.name] = elem
	}

	// FZF search with clean query — best match first.
	fzfResults := utils.FzfSearch(cleanQuery, haystack)

	// Convert results back to elements; preserve fzf score order.
	filteredElements := make([]element, 0, len(fzfResults))
	for _, result := range fzfResults {
		if elem, ok := elementMap[result.Key]; ok {
			filteredElements = append(filteredElements, elem)
		}
	}

	return filteredElements
}

func sortFileElement(sortOptions sortOptionsModelData, entries []dirEntryInfo, location string) []element {
	elements := make([]element, 0, len(entries))
	for _, item := range entries {
		// Use pre-fetched info — no redundant stat call.
		elements = append(elements, elementOf(location, item))
	}
	setICloudStatus(location, elements)

	sortElements(elements, sortOptions)
	return elements
}

// sortElements sorts an existing slice of elements in-place according to sort options.
// Used to apply sorting to search/filter results.
func sortElements(elements []element, sortOptions sortOptionsModelData) {
	if len(elements) == 0 {
		return
	}
	sort.Slice(elements, getOrderingFunc(elements,
		sortOptions.reversed, sortOptions.options[sortOptions.selected]))
}

// Symlinks to directories are to be identified as directories
func isSymlinkToDir(location string, info os.FileInfo, name string) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		targetInfo, errStat := os.Stat(filepath.Join(location, name))
		return errStat == nil && targetInfo.IsDir()
	}
	return false
}

func getOrderingFunc(elements []element, reversed bool, sortOption string) sliceOrderFunc {
	var order func(i, j int) bool
	switch sortOption {
	case string(sortingName):
		order = func(i, j int) bool {
			// One of them is a directory, and other is not
			if elements[i].directory != elements[j].directory {
				return elements[i].directory
			}
			if common.Config.CaseSensitiveSort {
				return elements[i].name < elements[j].name != reversed
			}
			return strings.ToLower(elements[i].name) < strings.ToLower(elements[j].name) != reversed
		}
	case string(sortingSize):
		order = getSizeOrderingFunc(elements, reversed)
	case string(sortingDateModified):
		order = func(i, j int) bool {
			return elements[i].info.ModTime().After(elements[j].info.ModTime()) != reversed
		}
	case string(sortingFileType):
		order = getTypeOrderingFunc(elements, reversed)
	}
	return order
}

func getSizeOrderingFunc(elements []element, reversed bool) sliceOrderFunc {
	// Pre-compute child counts for directories to avoid O(N log N) ReadDir calls
	// inside the sort comparator.
	childCounts := make(map[string]int, len(elements))
	for _, elem := range elements {
		if elem.directory {
			files, err := os.ReadDir(elem.location)
			if err != nil {
				slog.Error("Error when reading directory during sort", "error", err)
			}
			childCounts[elem.location] = len(files)
		}
	}

	return func(i, j int) bool {
		// Directories at the top sorted by direct child count (not recursive)
		// Files sorted by size

		// One of them is a directory, and other is not
		if elements[i].directory != elements[j].directory {
			return elements[i].directory
		}

		if elements[i].directory && elements[j].directory {
			return childCounts[elements[i].location] < childCounts[elements[j].location] != reversed
		}
		return elements[i].info.Size() < elements[j].info.Size() != reversed
	}
}

func getTypeOrderingFunc(elements []element, reversed bool) sliceOrderFunc {
	return func(i, j int) bool {
		// One of them is a directory, and the other is not
		if elements[i].directory != elements[j].directory {
			return elements[i].directory
		}

		var extI, extJ string
		if !elements[i].directory {
			extI = strings.ToLower(filepath.Ext(elements[i].name))
		}
		if !elements[j].directory {
			extJ = strings.ToLower(filepath.Ext(elements[j].name))
		}

		// Compare by extension/type
		if extI != extJ {
			return (extI < extJ) != reversed
		}

		// If same type, fall back to name
		if common.Config.CaseSensitiveSort {
			return (elements[i].name < elements[j].name) != reversed
		}

		return (strings.ToLower(elements[i].name) < strings.ToLower(elements[j].name)) != reversed
	}
}

// filePanelChromeRows are the rows a file panel shows above its items: the
// path and the divider under it. The search row comes on top while it is in
// use.
const filePanelChromeRows = 2

// elementHeight is how many items the panel shows in mainPanelHeight rows.
func (panel *filePanel) elementHeight(mainPanelHeight int) int {
	rows := mainPanelHeight - filePanelChromeRows
	if panel.showsSearchRow() {
		rows--
	}
	return rows
}

// showsSearchRow reports whether the panel's search row is on screen: while
// you type a search, or while one is narrowing the panel.
func (panel *filePanel) showsSearchRow() bool {
	return panel.searchBar.Focused() || panel.searchBar.Value() != "" || !panel.searchStack.IsEmpty() ||
		len(panel.recursiveBaseElems) > 0
}

func checkFileNameValidity(name string) error {
	switch {
	case name == ".", name == "..":
		return errors.New("file name cannot be '.' or '..'")
	case strings.HasSuffix(name, fmt.Sprintf("%c.", filepath.Separator)),
		strings.HasSuffix(name, fmt.Sprintf("%c..", filepath.Separator)):
		return fmt.Errorf("file name cannot end with '%c.' or '%c..'", filepath.Separator, filepath.Separator)
	default:
		return nil
	}
}

func renameIfDuplicate(destination string) (string, error) {
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		return destination, nil
	} else if err != nil {
		return "", err
	}

	dir := filepath.Dir(destination)
	base := filepath.Base(destination)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]

	// Extract base name without existing suffix
	counter := 1
	//nolint:mnd // 3 = full match + 2 capture groups
	if match := suffixRegexp.FindStringSubmatch(name); len(match) == 3 {
		name = match[1] // base name without (N)
		if num, err := strconv.Atoi(match[2]); err == nil {
			counter = num + 1 // start from next number
		}
	}

	// Find first available name
	for i := counter; i < 10_000; i++ {
		newName := fmt.Sprintf("%s(%d)%s", name, i, ext)
		newPath := filepath.Join(dir, newName)
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath, nil
		}
	}

	return "", fmt.Errorf("could not find free name for %s after many attempts", destination)
}

// sortArrow returns the direction arrow for a sort kind and reversed flag.
// Intuitive arrows: up=ascending, down=descending.
// For Name/Size/Type: reversed=false is ascending (↑), reversed=true is descending (↓).
// For Date Modified: reversed=false is newest-first (↓), reversed=true is oldest-first (↑).
// Unknown kinds return "?" as a safe fallback.
func sortArrow(kind string, reversed bool) string {
	switch kind {
	case string(sortingName), string(sortingSize), string(sortingFileType):
		if reversed {
			return "↓"
		}
		return "↑"
	case string(sortingDateModified):
		if reversed {
			return "↑"
		}
		return "↓"
	default:
		return "?"
	}
}

// TODO : Replace all usage of "m.fileModel.filePanels[m.filePanelFocusIndex]" with this
// There are many usage
func (m *model) getFocusedFilePanel() *filePanel {
	return &m.fileModel.filePanels[m.filePanelFocusIndex]
}

// Count how many file in the directory
func countFiles(dirPath string) (int, error) {
	count := 0

	err := filepath.Walk(dirPath, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})

	return count, err
}

func processCmdToTeaCmd(cmd processbar.Cmd) tea.Cmd {
	if cmd == nil {
		// To prevent us from running cmd() on nil cmd
		return nil
	}
	return func() tea.Msg {
		updateMsg := cmd()
		return ProcessBarUpdateMsg{
			pMsg: updateMsg,
			BaseMessage: BaseMessage{
				reqID: updateMsg.GetReqID(),
			},
		}
	}
}

func getCopyOrCutOperationName(cut bool) string {
	if cut {
		return "cut"
	}
	return "copy"
}
