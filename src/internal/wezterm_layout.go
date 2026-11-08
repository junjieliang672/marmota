package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

var execInShell = utils.ExecuteCommandInShell //nolint:gochecknoglobals // test seam for shell-backed layout commands

const weztermSplitDividerCols = 1

// managedAgentHiddenWorkspace is where agent panes wait while another one is
// shown beside superfile. WezTerm only draws the active workspace, so panes
// parked here stay out of the tab bar until they're pulled back into a split.
const managedAgentHiddenWorkspace = "superfile-agents"

type managedAgentProvider string

const (
	managedAgentProviderUnknown managedAgentProvider = "unknown"
	managedAgentProviderClaude  managedAgentProvider = "claude"
	managedAgentProviderCodex   managedAgentProvider = "codex"
)

type managedAgentSession struct {
	PaneID      int                  `json:"pane_id"`
	Provider    managedAgentProvider `json:"provider"`
	Cwd         string               `json:"cwd,omitempty"`
	DisplayName string               `json:"display_name,omitempty"`
	VisibleSlot int                  `json:"visible_slot,omitempty"`
	// CodexSession is the Codex session in the pane, once known: Codex
	// titles a fork's pane as the forked session's, so a title can't tell.
	CodexSession string `json:"codex_session,omitempty"`
	RunningTask  bool   `json:"-"`
}

func (s managedAgentSession) providerToken() string {
	switch s.Provider {
	case managedAgentProviderClaude:
		return "c"
	case managedAgentProviderCodex:
		return "x"
	case managedAgentProviderUnknown:
	}
	return "?"
}

type weztermPaneLayoutState struct {
	// ManagedPaneIDs are kept in SPF slot order:
	// 1 = the single foreground managed agent pane.
	ManagedPaneIDs     []int                 `json:"managed_pane_ids"`
	ForegroundPaneID   int                   `json:"foreground_pane_id,omitempty"`
	FixedSuperfileCols int                   `json:"fixed_superfile_cols,omitempty"`
	Sessions           []managedAgentSession `json:"sessions,omitempty"`

	loaded bool `json:"-"`
}

type weztermPane struct {
	WindowID  int             `json:"window_id"`
	TabID     int             `json:"tab_id"`
	PaneID    int             `json:"pane_id"`
	Title     string          `json:"title"`
	Cwd       string          `json:"cwd"`
	LeftCol   int             `json:"left_col"`
	TopRow    int             `json:"top_row"`
	Size      weztermPaneSize `json:"size"`
	TTYName   string          `json:"tty_name,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
}

type weztermPaneSize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

type weztermManagedLayoutPlan struct {
	RowPaneCounts []int
	RowHeights    []int
	AgentWidth    int
}

// launchManagedSessionInWeztermAt starts an agent pane whose working folder is
// cwd instead of the focused panel's folder.
func (m *model) launchManagedSessionInWeztermAt(cwd string, command string, provider managedAgentProvider,
	successMessage string,
) (string, error) {
	superfilePaneID, err := currentWeztermPaneID()
	if err != nil {
		return "", err
	}
	return m.launchManagedSessionInWeztermForPaneAt(superfilePaneID, cwd, command, provider, successMessage)
}

func (m *model) launchManagedSessionInWeztermForPaneAt(superfilePaneID int, focusPanelDir string, command string,
	provider managedAgentProvider, successMessage string,
) (string, error) {
	state, err := loadWeztermPaneLayoutState(superfilePaneID)
	if err != nil {
		slog.Warn("Ignoring unreadable WezTerm pane state", "paneID", superfilePaneID, "error", err)
		state = weztermPaneLayoutState{}
	}

	state = normalizeWeztermPaneLayoutState(state)
	if len(state.ManagedPaneIDs) == 0 && len(state.Sessions) == 0 {
		return m.launchFirstManagedSessionInWezterm(focusPanelDir, superfilePaneID, command, provider, successMessage,
			nil)
	}

	allPanes, err := listWeztermPanes(focusPanelDir)
	if err != nil {
		return "", fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}
	if _, err = findWeztermPaneByID(allPanes, superfilePaneID); err != nil {
		return "", err
	}
	tabPanes, err := filterWeztermTabPanes(allPanes, superfilePaneID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	state = pruneDeadManagedPanes(state, allPanes)
	state = normalizeWeztermPaneLayoutState(state)

	if len(state.ManagedPaneIDs) == 0 {
		// Nothing is beside superfile, but parked sessions stay managed.
		return m.launchFirstManagedSessionInWezterm(focusPanelDir, superfilePaneID, command, provider, successMessage,
			state.Sessions)
	}

	if hasUnmanagedWeztermTabPanes(tabPanes, superfilePaneID, state.ManagedPaneIDs) {
		return "", errors.New("managed agent layout requires the tab to contain only Marmota and managed agent panes")
	}

	superfilePane, err := findWeztermPaneByID(tabPanes, superfilePaneID)
	if err != nil {
		return "", err
	}
	if state.FixedSuperfileCols == 0 {
		state.FixedSuperfileCols = superfilePane.Size.Cols
	}

	hiddenPaneID, hiddenErr := spawnBackgroundManagedSessionInWezterm(focusPanelDir, command)
	if hiddenErr != nil {
		return "", fmt.Errorf("failed to spawn background agent session: %w", hiddenErr)
	}

	state.Sessions = append(state.Sessions, managedAgentSession{
		PaneID:      hiddenPaneID,
		Provider:    provider,
		Cwd:         focusPanelDir,
		DisplayName: managedAgentFolderName(focusPanelDir),
		VisibleSlot: managedAgentHiddenSlot,
	})
	state = normalizeWeztermPaneLayoutState(state)

	persistWeztermPaneLayoutState(superfilePaneID, state)
	state.loaded = true
	m.setManagedWeztermState(superfilePaneID, state)
	return successMessage, nil
}

// launchFirstManagedSessionInWezterm splits superfile's pane for a new agent
// when no agent is beside superfile. Parked sessions in keep stay parked.
func (m *model) launchFirstManagedSessionInWezterm(focusPanelDir string, superfilePaneID int,
	command string, provider managedAgentProvider, successMessage string, keep []managedAgentSession,
) (string, error) {
	tabPanes, err := listWeztermTabPanes(focusPanelDir, superfilePaneID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	superfilePane, err := findWeztermPaneByID(tabPanes, superfilePaneID)
	if err != nil {
		return "", err
	}

	superfileCols, agentCols, err := initialManagedWeztermSplitWidths(superfilePane.Size.Cols)
	if err != nil {
		return "", err
	}

	newPaneID, err := splitWeztermPane(focusPanelDir,
		fmt.Sprintf("wezterm cli split-pane --pane-id %d --right --cells %d --cwd %q -- %s",
			superfilePaneID, agentCols, focusPanelDir, command))
	if err != nil {
		return "", fmt.Errorf("failed to split pane: %w", err)
	}

	sessions := make([]managedAgentSession, 0, len(keep)+1)
	for _, session := range keep {
		session.VisibleSlot = managedAgentHiddenSlot
		sessions = append(sessions, session)
	}
	state := normalizeWeztermPaneLayoutState(weztermPaneLayoutState{
		ManagedPaneIDs:     []int{newPaneID},
		ForegroundPaneID:   newPaneID,
		FixedSuperfileCols: superfileCols,
		Sessions: append(sessions, managedAgentSession{
			PaneID:      newPaneID,
			Provider:    provider,
			Cwd:         focusPanelDir,
			DisplayName: managedAgentFolderName(focusPanelDir),
			VisibleSlot: managedAgentSlotOne,
		}),
	})

	persistWeztermPaneLayoutState(superfilePaneID, state)
	state.loaded = true
	m.setManagedWeztermState(superfilePaneID, state)

	return successMessage, nil
}

// managedAgentSplitWidth is the width of an agent pane split off superfile
// in a tab totalCols wide. It keeps superfile at its recorded width, or at
// the first split's width when none is recorded or the recorded one would
// leave no room for the agent.
func managedAgentSplitWidth(totalCols, superfileCols int) (int, error) {
	agentCols := totalCols - superfileCols - weztermSplitDividerCols
	if superfileCols > 0 && agentCols > 0 {
		return agentCols, nil
	}
	_, agentCols, err := initialManagedWeztermSplitWidths(totalCols)
	return agentCols, err
}

func initialManagedWeztermSplitWidths(totalCols int) (int, int, error) {
	if totalCols <= 0 {
		return 0, 0, fmt.Errorf("wezterm pane width must be positive, got %d", totalCols)
	}

	superfileCols := max(totalCols*common.SuperfileInitialWidthPct/100, //nolint:mnd // a percentage
		common.SuperfileManagedMinCols)
	agentCols := totalCols - superfileCols - weztermSplitDividerCols
	if agentCols <= 0 {
		return 0, 0, fmt.Errorf(
			"wezterm pane width %d is too small to keep the Marmota pane at %d columns and still split an agent pane",
			totalCols,
			superfileCols,
		)
	}

	return superfileCols, agentCols, nil
}

func currentWeztermPaneID() (int, error) {
	paneIDRaw := strings.TrimSpace(os.Getenv("WEZTERM_PANE"))
	if paneIDRaw == "" {
		return 0, errors.New("WEZTERM_PANE is not set")
	}

	paneID, err := strconv.Atoi(paneIDRaw)
	if err != nil {
		return 0, fmt.Errorf("invalid WEZTERM_PANE value %q: %w", paneIDRaw, err)
	}

	return paneID, nil
}

func splitWeztermPane(cwd string, command string) (int, error) {
	retCode, output, err := execInShell(common.DefaultCommandTimeout, cwd, command)
	if err != nil || retCode != 0 {
		return 0, weztermCommandError("wezterm split failed", retCode, err)
	}

	paneID, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0, fmt.Errorf("wezterm returned invalid pane ID %q: %w", strings.TrimSpace(output), err)
	}

	return paneID, nil
}

func splitWeztermPaneWithMovedPane(cwd string, paneID int, direction string, cells int, movePaneID int) (int, error) {
	flag := "--right"
	switch direction {
	case "left":
		flag = "--left"
	case "right":
		flag = "--right"
	case "top":
		flag = "--top"
	case "bottom":
		flag = "--bottom"
	}

	return splitWeztermPane(cwd,
		fmt.Sprintf("wezterm cli split-pane --pane-id %d %s --cells %d --move-pane-id %d",
			paneID, flag, cells, movePaneID))
}

func killWeztermPane(cwd string, paneID int) error {
	retCode, _, err := execInShell(common.DefaultCommandTimeout, cwd,
		fmt.Sprintf("wezterm cli kill-pane --pane-id %d", paneID))
	if err != nil || retCode != 0 {
		return weztermCommandError("wezterm kill-pane failed", retCode, err)
	}
	return nil
}

func listWeztermPanes(cwd string) ([]weztermPane, error) {
	retCode, output, err := execInShell(common.DefaultCommandTimeout, cwd, "wezterm cli list --format json")
	if err != nil || retCode != 0 {
		return nil, weztermCommandError("wezterm list failed", retCode, err)
	}

	var panes []weztermPane
	if err := json.Unmarshal([]byte(output), &panes); err != nil {
		return nil, fmt.Errorf("failed to parse wezterm pane list: %w", err)
	}

	return panes, nil
}

func listWeztermTabPanes(cwd string, currentPaneID int) ([]weztermPane, error) {
	panes, err := listWeztermPanes(cwd)
	if err != nil {
		return nil, err
	}

	return filterWeztermTabPanes(panes, currentPaneID)
}

func weztermTitleShowsRunningTask(title string) bool {
	title = strings.TrimSpace(title)
	for _, prefix := range []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏", "⠐"} {
		if strings.HasPrefix(title, prefix) {
			return true
		}
	}
	return false
}

func codexPaneTextShowsRunningTask(text string) bool {
	return strings.Contains(text, "◦ Working (") ||
		strings.Contains(text, "esc to interrupt") ||
		strings.Contains(text, "press esc to interrupt")
}

func codexPaneTextLooksLikeCodex(text string) bool {
	return strings.Contains(text, "OpenAI Codex")
}

func claudePaneTitleLooksLikeClaude(title string) bool {
	title = strings.TrimSpace(title)
	return strings.HasPrefix(title, "✳ ")
}

func claudePaneTextLooksLikeClaude(text string) bool {
	return strings.Contains(text, "Claude Code") ||
		strings.Contains(text, "[Opus ") ||
		strings.Contains(text, "bypass permissions on")
}

func getWeztermPaneText(cwd string, paneID int) (string, error) {
	retCode, output, err := execInShell(common.DefaultCommandTimeout, cwd,
		fmt.Sprintf("wezterm cli get-text --pane-id %d --start-line -20", paneID))
	if err != nil || retCode != 0 {
		return "", weztermCommandError("wezterm get-text failed", retCode, err)
	}
	return output, nil
}

func managedAgentSessionRunningTask(cwd string, pane weztermPane, provider managedAgentProvider) bool {
	if weztermTitleShowsRunningTask(pane.Title) {
		return true
	}

	if provider != managedAgentProviderCodex {
		return false
	}

	text, err := getWeztermPaneText(cwd, pane.PaneID)
	if err != nil {
		return false
	}
	return codexPaneTextShowsRunningTask(text)
}

func inferManagedAgentProvider(
	cwd string,
	pane weztermPane,
	currentProvider managedAgentProvider,
) managedAgentProvider {
	if currentProvider != managedAgentProviderUnknown {
		return currentProvider
	}

	if claudePaneTitleLooksLikeClaude(pane.Title) {
		return managedAgentProviderClaude
	}

	text, err := getWeztermPaneText(cwd, pane.PaneID)
	if err != nil {
		return managedAgentProviderUnknown
	}

	switch {
	case codexPaneTextLooksLikeCodex(text):
		return managedAgentProviderCodex
	case claudePaneTextLooksLikeClaude(text):
		return managedAgentProviderClaude
	default:
		return managedAgentProviderUnknown
	}
}

func weztermPaneCwdPath(rawCwd string) string {
	if strings.TrimSpace(rawCwd) == "" {
		return ""
	}

	parsed, err := url.Parse(rawCwd)
	if err != nil {
		return rawCwd
	}
	if parsed.Scheme == "file" && parsed.Path != "" {
		return parsed.Path
	}
	return rawCwd
}

func filterWeztermTabPanes(panes []weztermPane, currentPaneID int) ([]weztermPane, error) {
	currentPane, err := findWeztermPaneByID(panes, currentPaneID)
	if err != nil {
		return nil, err
	}

	tabPanes := make([]weztermPane, 0, len(panes))
	for _, pane := range panes {
		if pane.TabID == currentPane.TabID {
			tabPanes = append(tabPanes, pane)
		}
	}

	return tabPanes, nil
}

// spawnBackgroundManagedSessionInWezterm starts an agent in the hidden
// workspace. Spawning there leaves the user's window, tab and focus alone.
func spawnBackgroundManagedSessionInWezterm(cwd string, command string) (int, error) {
	return splitWeztermPane(cwd, fmt.Sprintf("wezterm cli spawn --new-window --workspace %s --cwd %q -- %s",
		managedAgentHiddenWorkspace, cwd, command))
}

// parkWeztermPane moves a pane into a window of its own in the hidden
// workspace, where it keeps running without a tab in the user's window.
func parkWeztermPane(cwd string, paneID int) error {
	retCode, _, err := execInShell(common.DefaultCommandTimeout, cwd,
		fmt.Sprintf("wezterm cli move-pane-to-new-tab --pane-id %d --new-window --workspace %s",
			paneID, managedAgentHiddenWorkspace))
	if err != nil || retCode != 0 {
		return weztermCommandError("wezterm move-pane-to-new-tab failed", retCode, err)
	}
	return nil
}

func activateWeztermPane(cwd string, paneID int) error {
	retCode, _, err := execInShell(common.DefaultCommandTimeout, cwd,
		fmt.Sprintf("wezterm cli activate-pane --pane-id %d", paneID))
	if err != nil || retCode != 0 {
		return weztermCommandError("wezterm activate-pane failed", retCode, err)
	}
	return nil
}

func findWeztermPaneByID(panes []weztermPane, paneID int) (weztermPane, error) {
	for _, pane := range panes {
		if pane.PaneID == paneID {
			return pane, nil
		}
	}
	return weztermPane{}, fmt.Errorf("current wezterm pane %d was not found", paneID)
}

func totalWeztermTabWidth(tabPanes []weztermPane) int {
	if len(tabPanes) == 0 {
		return 0
	}

	minLeft := tabPanes[0].LeftCol
	maxRight := tabPanes[0].LeftCol + tabPanes[0].Size.Cols
	for _, pane := range tabPanes[1:] {
		if pane.LeftCol < minLeft {
			minLeft = pane.LeftCol
		}
		rightCol := pane.LeftCol + pane.Size.Cols
		if rightCol > maxRight {
			maxRight = rightCol
		}
	}

	return maxRight - minLeft
}

func hasUnmanagedWeztermTabPanes(tabPanes []weztermPane, superfilePaneID int, managedPaneIDs []int) bool {
	knownPaneIDs := make(map[int]struct{}, len(managedPaneIDs)+1)
	knownPaneIDs[superfilePaneID] = struct{}{}
	for _, paneID := range managedPaneIDs {
		knownPaneIDs[paneID] = struct{}{}
	}

	for _, pane := range tabPanes {
		if _, ok := knownPaneIDs[pane.PaneID]; !ok {
			return true
		}
	}

	return false
}

func planManagedWeztermLayout(agentWidth int, agentHeight int, paneCount int) (weztermManagedLayoutPlan, error) {
	if paneCount <= 0 {
		return weztermManagedLayoutPlan{}, errors.New("paneCount must be positive")
	}

	if paneCount > common.ManagedAgentPaneCapacity {
		return weztermManagedLayoutPlan{}, fmt.Errorf(
			"opening another agent pane would exceed the %dx%d managed layout capacity",
			common.ManagedAgentPaneCols,
			common.ManagedAgentPaneRows,
		)
	}

	rowPaneCounts := make([]int, paneCount)
	for i := range rowPaneCounts {
		rowPaneCounts[i] = common.ManagedAgentPaneCols
	}

	return weztermManagedLayoutPlan{
		RowPaneCounts: rowPaneCounts,
		RowHeights:    distributeCellsEvenly(agentHeight, len(rowPaneCounts)),
		AgentWidth:    agentWidth,
	}, nil
}

func distributeCellsEvenly(totalCells int, bucketCount int) []int {
	if bucketCount == 0 {
		return nil
	}

	base := totalCells / bucketCount
	extra := totalCells % bucketCount
	result := make([]int, bucketCount)
	for i := range result {
		result[i] = base
		if i < extra {
			result[i]++
		}
	}

	return result
}

// pruneDeadManagedPanes drops the panes that no longer exist from the state:
// its sessions, and the pane ids that older state files list on their own.
// Pruning only the sessions would leave those ids behind, and normalizing
// would bring a closed session back as an unnamed one.
func pruneDeadManagedPanes(state weztermPaneLayoutState, panes []weztermPane) weztermPaneLayoutState {
	live := make(map[int]bool, len(panes))
	for _, pane := range panes {
		live[pane.PaneID] = true
	}
	state.Sessions = pruneManagedAgentSessions(state.Sessions, panes)
	ids := make([]int, 0, len(state.ManagedPaneIDs))
	for _, id := range state.ManagedPaneIDs {
		if live[id] {
			ids = append(ids, id)
		}
	}
	state.ManagedPaneIDs = ids
	if !live[state.ForegroundPaneID] {
		state.ForegroundPaneID = 0
	}
	return state
}

func pruneManagedAgentSessions(sessions []managedAgentSession, panes []weztermPane) []managedAgentSession {
	livePaneIDs := make(map[int]struct{}, len(panes))
	for _, pane := range panes {
		livePaneIDs[pane.PaneID] = struct{}{}
	}

	pruned := make([]managedAgentSession, 0, len(sessions))
	for _, session := range sessions {
		if _, ok := livePaneIDs[session.PaneID]; ok {
			pruned = append(pruned, session)
		}
	}

	return pruned
}

func reconcileManagedAgentSessionsWithWezterm(cwd string, superfilePaneID int,
	state weztermPaneLayoutState,
) (weztermPaneLayoutState, error) {
	allPanes, err := listWeztermPanes(cwd)
	if err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	superfilePane, err := findWeztermPaneByID(allPanes, superfilePaneID)
	if err != nil {
		return state, err
	}

	tabPanes, err := filterWeztermTabPanes(allPanes, superfilePaneID)
	if err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	state = pruneDeadManagedPanes(state, allPanes)
	state = normalizeWeztermPaneLayoutState(state)

	panesByID := make(map[int]weztermPane, len(allPanes))
	for _, pane := range allPanes {
		panesByID[pane.PaneID] = pane
	}

	sessionIndexByPaneID := refreshManagedSessions(cwd, state.Sessions, panesByID)
	visiblePanes := visibleManagedPanes(tabPanes, superfilePaneID, sessionIndexByPaneID)

	foregroundPaneID := state.ForegroundPaneID
	if len(visiblePanes) > 0 {
		if superfilePane.Size.Cols > 0 {
			state.FixedSuperfileCols = superfilePane.Size.Cols
		}
		foregroundPaneID = visiblePanes[0].PaneID
	}

	parkStrayManagedPanes(cwd, state.Sessions, visiblePanes, panesByID)

	if len(visiblePanes) == 0 {
		for i := range state.Sessions {
			state.Sessions[i].VisibleSlot = managedAgentHiddenSlot
		}
		state.ManagedPaneIDs = nil
		state.ForegroundPaneID = 0
		return state, nil
	}
	if foregroundPaneID != 0 {
		if sessionIdx, ok := sessionIndexByPaneID[foregroundPaneID]; ok {
			state.Sessions[sessionIdx].VisibleSlot = managedAgentSlotOne
		}
	}

	state.ForegroundPaneID = foregroundPaneID
	return normalizeWeztermPaneLayoutState(state), nil
}

// refreshManagedSessions hides every session, then fills in each one with a
// live pane: its provider, folder, name, and whether it runs a task. It
// returns each session's index by its pane.
func refreshManagedSessions(cwd string, sessions []managedAgentSession,
	panesByID map[int]weztermPane,
) map[int]int {
	indexByPaneID := make(map[int]int, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		s.VisibleSlot = managedAgentHiddenSlot
		if pane, ok := panesByID[s.PaneID]; ok {
			s.Provider = inferManagedAgentProvider(cwd, pane, s.Provider)
			if s.Cwd == "" {
				s.Cwd = weztermPaneCwdPath(pane.Cwd)
			}
			if s.DisplayName == "" {
				s.DisplayName = managedAgentFolderName(s.Cwd)
			}
			s.RunningTask = managedAgentSessionRunningTask(cwd, pane, s.Provider)
		}
		indexByPaneID[s.PaneID] = i
	}
	return indexByPaneID
}

// visibleManagedPanes are the agent panes in superfile's tab, left to right,
// then top to bottom.
func visibleManagedPanes(tabPanes []weztermPane, superfilePaneID int,
	sessionIndexByPaneID map[int]int,
) []weztermPane {
	visiblePanes := make([]weztermPane, 0, 1)
	for _, pane := range tabPanes {
		if pane.PaneID == superfilePaneID {
			continue
		}
		if _, ok := sessionIndexByPaneID[pane.PaneID]; ok {
			visiblePanes = append(visiblePanes, pane)
		}
	}

	slices.SortFunc(visiblePanes, func(a weztermPane, b weztermPane) int {
		if a.LeftCol != b.LeftCol {
			return a.LeftCol - b.LeftCol
		}
		return a.TopRow - b.TopRow
	})
	return visiblePanes
}

// parkStrayManagedPanes moves hidden agent panes that still sit in an ordinary
// tab (older superfile versions parked them that way) into the hidden
// workspace, so they stop cluttering the tab bar.
func parkStrayManagedPanes(cwd string, sessions []managedAgentSession, visiblePanes []weztermPane,
	panesByID map[int]weztermPane,
) {
	visible := make(map[int]bool, len(visiblePanes))
	for _, pane := range visiblePanes {
		visible[pane.PaneID] = true
	}
	for _, session := range sessions {
		pane, ok := panesByID[session.PaneID]
		if !ok || visible[pane.PaneID] || pane.Workspace == "" || pane.Workspace == managedAgentHiddenWorkspace {
			continue
		}
		if err := parkWeztermPane(cwd, pane.PaneID); err != nil {
			slog.Warn("Could not park hidden agent pane", "paneID", pane.PaneID, "error", err)
			continue
		}
		slog.Info("Parked hidden agent pane in the hidden workspace", "paneID", pane.PaneID,
			"from", pane.Workspace)
	}
}

func (m *model) foregroundManagedAgentSessionByPaneID(
	superfilePaneID int,
	state weztermPaneLayoutState,
	targetPaneID int,
) (weztermPaneLayoutState, error) {
	focusPanelDir := m.fileModel.filePanels[m.filePanelFocusIndex].location

	targetIdx := -1
	for i := range state.Sessions {
		if state.Sessions[i].PaneID == targetPaneID {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return state, os.ErrNotExist
	}

	allPanes, err := listWeztermPanes(focusPanelDir)
	if err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}
	// superfile's pane and its tab must be there before anything moves.
	if _, err = findWeztermPaneByID(allPanes, superfilePaneID); err != nil {
		return state, err
	}
	if _, err = filterWeztermTabPanes(allPanes, superfilePaneID); err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	currentForegroundIdx := managedAgentSessionIndexBySlot(state.Sessions, managedAgentSlotOne)
	if currentForegroundIdx >= 0 && state.Sessions[currentForegroundIdx].PaneID == targetPaneID {
		state = normalizeWeztermPaneLayoutState(state)
		return state, nil
	}

	if currentForegroundIdx >= 0 {
		// A session that has exited has no pane left to park.
		if _, findErr := findWeztermPaneByID(allPanes, state.Sessions[currentForegroundIdx].PaneID); findErr == nil {
			if err = parkWeztermPane(focusPanelDir, state.Sessions[currentForegroundIdx].PaneID); err != nil {
				return state, err
			}
		}
		state.Sessions[currentForegroundIdx].VisibleSlot = managedAgentHiddenSlot
	}

	allPanes, err = listWeztermPanes(focusPanelDir)
	if err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}
	currentPane, err := findWeztermPaneByID(allPanes, superfilePaneID)
	if err != nil {
		return state, err
	}
	tabPanes, err := filterWeztermTabPanes(allPanes, superfilePaneID)
	if err != nil {
		return state, fmt.Errorf("failed to inspect wezterm panes: %w", err)
	}

	if err := attachManagedPaneToSlot(
		focusPanelDir,
		currentPane,
		tabPanes,
		state,
		managedAgentSlotOne,
		targetPaneID,
	); err != nil {
		return state, err
	}

	for i := range state.Sessions {
		state.Sessions[i].VisibleSlot = managedAgentHiddenSlot
	}
	state.Sessions[targetIdx].VisibleSlot = managedAgentSlotOne
	state.ForegroundPaneID = targetPaneID
	return normalizeWeztermPaneLayoutState(state), nil
}

func loadWeztermPaneLayoutState(superfilePaneID int) (weztermPaneLayoutState, error) {
	data, err := os.ReadFile(weztermPaneStatePath(superfilePaneID))
	if err != nil {
		if os.IsNotExist(err) {
			return weztermPaneLayoutState{}, nil
		}
		return weztermPaneLayoutState{}, err
	}

	var state weztermPaneLayoutState
	if err := json.Unmarshal(data, &state); err != nil {
		return weztermPaneLayoutState{}, err
	}

	return normalizeWeztermPaneLayoutState(state), nil
}

func persistWeztermPaneLayoutState(superfilePaneID int, state weztermPaneLayoutState) {
	if err := saveWeztermPaneLayoutState(superfilePaneID, state); err != nil {
		slog.Warn("Failed to persist WezTerm pane state", "paneID", superfilePaneID, "error", err)
	}
}

func saveWeztermPaneLayoutState(superfilePaneID int, state weztermPaneLayoutState) error {
	state = normalizeWeztermPaneLayoutState(state)

	statePath := weztermPaneStatePath(superfilePaneID)
	if len(state.ManagedPaneIDs) == 0 && len(state.Sessions) == 0 {
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	existingData, err := os.ReadFile(statePath)
	if err == nil && string(existingData) == string(data) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.WriteFile(statePath, data, utils.ConfigFilePerm)
}

func managedAgentSessionIndexBySlot(sessions []managedAgentSession, slot int) int {
	for i, session := range sessions {
		if session.VisibleSlot == slot {
			return i
		}
	}
	return -1
}

func attachManagedPaneToSlot(cwd string, currentPane weztermPane, tabPanes []weztermPane,
	state weztermPaneLayoutState, slot int, paneID int,
) error {
	visiblePaneIDs := visibleManagedAgentPaneIDs(state.Sessions)
	switch len(visiblePaneIDs) {
	case 0:
		agentWidth, err := managedAgentSplitWidth(totalWeztermTabWidth(tabPanes), state.FixedSuperfileCols)
		if err != nil {
			// A split WezTerm refuses can still take the moved pane out of
			// its tab, stranding the session; never ask for one.
			return err
		}
		_, err = splitWeztermPane(cwd,
			fmt.Sprintf("wezterm cli split-pane --pane-id %d --right --cells %d --move-pane-id %d",
				currentPane.PaneID, agentWidth, paneID))
		return err
	case 1:
		anchorPane, err := findWeztermPaneByID(tabPanes, visiblePaneIDs[0])
		if err != nil {
			return err
		}
		direction := "bottom"
		cells := max(1, anchorPane.Size.Rows/2) //nolint:mnd // halves
		if slot == managedAgentSlotOne {
			direction = "top"
		}
		_, err = splitWeztermPaneWithMovedPane(cwd, anchorPane.PaneID, direction, cells, paneID)
		return err
	default:
		return fmt.Errorf("cannot attach managed pane when %d visible panes already exist", len(visiblePaneIDs))
	}
}

func weztermPaneStatePath(superfilePaneID int) string {
	return filepath.Join(variable.SuperFileStateDir, fmt.Sprintf("wezterm-pane-state-%d.json", superfilePaneID))
}

func weztermCommandError(prefix string, retCode int, err error) error {
	if err != nil {
		return fmt.Errorf("%s (retCode=%d): %w", prefix, retCode, err)
	}
	return fmt.Errorf("%s (retCode=%d)", prefix, retCode)
}
