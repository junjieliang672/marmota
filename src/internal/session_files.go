package internal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// W puts the files a session changed in the focused file panel, as a layer
// over its folder (W again, or esc, takes it off). They are the files
// Claude Code recorded the session changing with its Edit and Write tools;
// files it changed through shell commands aren't among them. With the
// cursor on one, the preview shows what the session changed in it: the file
// now against Claude Code's backup of it from before the session first
// changed it, or against nothing when the session created it. A Codex
// session's files are those it changed with its patch tool, and the
// preview shows the changes as Codex recorded them. ] and [ go to the next
// and previous change; the wheel scrolls.

// touchedLayer is the focused panel's layer of a session's files, when the
// panel shows them: it is on top, or under filters of them (a search kept
// with enter).
func (m *model) touchedLayer() *searchLayer {
	layers := m.getFocusedFilePanel().searchStack.layers
	for i := len(layers) - 1; i >= 0; i-- {
		switch layers[i].layerType {
		case searchLayerTouched:
			return &layers[i]
		case searchLayerFilter:
			continue
		default:
			return nil
		}
	}
	return nil
}

// touchedSession is the session W shows: the Sessions box's while it has
// the keyboard, else the one you were last on, else the first.
func (m *model) touchedSession() (claudesession.Session, bool) {
	id := m.sessions.list.SelectedID()
	if m.focusPanel != dockSessionsFocus || m.sessions.list.NewSelected() {
		id = m.dockAskTarget()
	}
	if id == "" {
		return claudesession.Session{}, false
	}
	return m.claude.snapshot.Find(id)
}

// toggleSessionFiles is W: a session's files over the panel's folder, or the
// folder again.
func (m *model) toggleSessionFiles() tea.Cmd {
	panel := m.getFocusedFilePanel()
	if m.touchedLayer() != nil {
		// The filters of its files go with it.
		for !panel.searchStack.IsEmpty() && panel.searchStack.Pop().layerType != searchLayerTouched {
			continue
		}
		panel.cursor, panel.render = 0, 0
		m.diff = diffView{}
		m.reloadPanelElements(panel)
		return m.requestFilePreviewRender(true)
	}
	s, ok := m.touchedSession()
	if !ok {
		m.flashClaude("No live sessions")
		return nil
	}
	elems := touchedElements(s)
	if len(elems) == 0 {
		if s.IsCodex() {
			m.flashClaude(s.DisplayTitle() + " hasn't changed files with Codex's patch tool")
		} else {
			m.flashClaude(s.DisplayTitle() + " hasn't changed files with Claude Code's Edit or Write tools")
		}
		return nil
	}
	m.dock.pinned = false // the preview shows the diffs
	if m.focusPanel == dockSessionsFocus {
		m.leaveDockSessions() // the keyboard goes to the files
	}
	if m.focusPanel != nonePanelFocus {
		m.focusPanel = nonePanelFocus
		panel.isFocused = true
	}
	panel.searchStack.Push(searchLayer{
		layerType:   searchLayerTouched,
		searchTerm:  s.DisplayTitle(),
		cachedElems: elems,
		session: touchedSessionRef{id: s.ID, cwd: s.Cwd, transcript: s.TranscriptPath, title: s.DisplayTitle(),
			codex: s.IsCodex()},
	})
	panel.cursor, panel.render = 0, 0
	m.diff = diffView{}
	m.reloadPanelElements(panel)
	return m.requestFilePreviewRender(true)
}

// reloadPanelElements lists the panel's files now. Its refresh can't tell
// that W changed them when the first is in its folder either way, and would
// leave the old list, and the preview of its file, for up to seconds.
func (m *model) reloadPanelElements(panel *filePanel) {
	panel.invalidateElementCache()
	panel.element = m.getElementsForPanel(panel)
	panel.lastTimeGetElement = time.Now()
}

// touchedElements are a session's changed files that are still there,
// named from its folder, in path order.
func touchedElements(s claudesession.Session) []element {
	var out []element
	seen := map[string]bool{}
	for _, p := range s.EditedFiles {
		if seen[p] {
			continue
		}
		seen[p] = true
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue // gone since
		}
		name := shortPath(p)
		if rel, err := filepath.Rel(s.Cwd, p); err == nil && !strings.HasPrefix(rel, "..") && s.Cwd != "" {
			name = rel
		}
		out = append(out, element{name: name, location: p, info: info})
	}
	slices.SortFunc(out, func(a, b element) int { return strings.Compare(a.name, b.name) })
	return out
}

// touchedSessionRef is what a layer of a session's files keeps of it.
type touchedSessionRef struct {
	id, cwd, transcript, title string
	// codex is set for a Codex session, whose rollout records each change's
	// diff instead of keeping backups.
	codex bool
}

// diffView is the preview of a changed file: a header, then the diff, from
// offset; hunks are where each change starts in body.
type diffView struct {
	path   string
	header []string
	body   []diffLine
	hunks  []int
}

// diffLine is a line of a diff: its line numbers, then its text.
type diffLine struct {
	gutter, text string
}

type diffDocMsg struct {
	reqID int
	path  string
	view  diffView
}

// diffPreviewCmd draws the diff of path in the background.
func (m *model) diffPreviewCmd(layer *searchLayer, path string) tea.Cmd {
	pv := &m.fileModel.filePreview
	pv.SetLocation(path)
	pv.SetContentWithRenderText("Loading...")
	reqID := m.ioReqCnt
	m.ioReqCnt++
	ref, width := layer.session, pv.InnerWidth()
	configDir := filepath.Dir(claudeProjectsDir())
	return func() tea.Msg {
		return diffDocMsg{reqID: reqID, path: path, view: buildDiffView(configDir, ref, path, width)}
	}
}

func (m *model) applyDiffDoc(msg diffDocMsg) tea.Cmd {
	m.previewRenderInFlight = false
	var queued tea.Cmd
	if m.previewRenderQueued {
		force := m.previewRenderForced
		m.previewRenderQueued, m.previewRenderForced = false, false
		queued = m.requestFilePreviewRender(force)
	}
	item := m.getFocusedFilePanel().getSelectedItemPtr()
	if item == nil || item.location != msg.path || m.touchedLayer() == nil {
		return queued
	}
	m.diff = msg.view
	m.drawDiff()
	return queued
}

// drawDiff puts the diff in the preview, which scrolls it as it scrolls a
// file: its title stays at the top, and its line numbers when it scrolls
// sideways.
func (m *model) drawDiff() {
	rows := make([]string, len(m.diff.body))
	widest, gutter := 0, 0
	for i, l := range m.diff.body {
		rows[i] = l.gutter + l.text
		widest = max(widest, ansi.StringWidth(rows[i]))
		gutter = max(gutter, ansi.StringWidth(l.gutter))
	}
	m.fileModel.filePreview.ShowFixedDoc(m.diff.header, rows, widest, gutter)
}

// diffShown reports whether the preview shows the diff of the cursor's file.
func (m *model) diffShown() bool {
	item := m.getFocusedFilePanel().getSelectedItemPtr()
	return m.touchedLayer() != nil && item != nil && m.diff.path == item.location
}

// jumpDiffChange goes to the next change (step 1) or the previous (-1).
func (m *model) jumpDiffChange(step int) {
	if !m.diffShown() || len(m.diff.hunks) == 0 {
		return
	}
	pv := &m.fileModel.filePreview
	top, target := pv.DocTop(), -1
	for _, h := range m.diff.hunks {
		if step > 0 && h > top {
			target = h
			break
		}
		if step < 0 && h < top {
			target = h
		}
	}
	if target >= 0 {
		pv.ScrollBy(target-top, 0)
	}
}

// backupCache keeps each transcript's backup hashes until it grows.
var backupCache = struct { //nolint:gochecknoglobals // shared by background draws
	sync.Mutex
	byPath map[string]backupHashes
}{byPath: map[string]backupHashes{}}

type backupHashes struct {
	size   int64
	hashes map[string]string
	shell  map[string][]claudesession.Hunk
}

// cachedChanges is what a transcript says about the files its session
// changed: the backups of those it changed with Edit and Write, and the
// changes its shell commands made.
func cachedChanges(transcript, cwd string) backupHashes {
	info, err := os.Stat(transcript)
	if err != nil {
		return backupHashes{}
	}
	backupCache.Lock()
	defer backupCache.Unlock()
	if c, ok := backupCache.byPath[transcript]; ok && c.size == info.Size() {
		return c
	}
	hashes, _ := claudesession.BackupHashes(transcript, cwd)
	shell, _ := claudesession.ShellHunks(transcript)
	c := backupHashes{size: info.Size(), hashes: hashes, shell: shell}
	backupCache.byPath[transcript] = c
	return c
}

// codexChangesCache keeps each rollout's recorded changes until it grows.
var codexChangesCache = struct { //nolint:gochecknoglobals // shared by background draws
	sync.Mutex
	byPath map[string]codexChanges
}{byPath: map[string]codexChanges{}}

type codexChanges struct {
	size    int64
	changes map[string][]claudesession.Hunk
}

// cachedCodexChanges is what a Codex rollout recorded of the changes its
// session made, by file.
func cachedCodexChanges(rollout string) map[string][]claudesession.Hunk {
	info, err := os.Stat(rollout)
	if err != nil {
		return nil
	}
	codexChangesCache.Lock()
	defer codexChangesCache.Unlock()
	if c, ok := codexChangesCache.byPath[rollout]; ok && c.size == info.Size() {
		return c.changes
	}
	changes, _ := codexsession.Changes(rollout)
	codexChangesCache.byPath[rollout] = codexChanges{size: info.Size(), changes: changes}
	return changes
}

// buildDiffView diffs path against its state before the session first
// changed it, width wide.
func buildDiffView(configDir string, ref touchedSessionRef, path string, width int) diffView {
	v := diffView{path: path}
	st := diffStyles()
	before, label := "", ""
	if ref.codex {
		return codexDiffView(path, cachedCodexChanges(ref.transcript)[path], width)
	}
	changes := cachedChanges(ref.transcript, ref.cwd)
	hash, known := changes.hashes[path]
	if !known {
		// Changed this turn: the transcript names its backup only next turn.
		if _, ok := claudesession.OriginalFile(configDir, ref.id, claudesession.PathHash(path)); ok {
			hash, known = claudesession.PathHash(path), true
		}
	}
	switch {
	case !known && len(changes.shell[path]) > 0:
		return shellDiffView(path, changes.shell[path], width)
	case !known:
		v.header = wrapHeader(width, st.title(filepath.Base(path), 0, 0), st.dim.Render(
			"No backup to compare with: Claude Code has none for this file."), st.rule(width))
		v.body = fileLinesPlain(path)
		return v
	default:
		if orig, ok := claudesession.OriginalFile(configDir, ref.id, hash); ok {
			before = orig
			label = "before: as it was before " + ref.title + " first changed it"
			if info, err := os.Stat(orig); err == nil {
				label += ", " + info.ModTime().Format("Jan 2 15:04")
			}
		} else {
			before = os.DevNull
			label = "before: nothing, " + ref.title + " created it"
		}
	}
	out, err := gitDiff(before, path)
	if err != nil {
		v.header = wrapHeader(width, st.title(filepath.Base(path), 0, 0), st.bad.Render("Couldn't diff: "+err.Error()))
		return v
	}
	added, removed := 0, 0
	v.body, v.hunks, added, removed = renderUnifiedDiff(out)
	v.header = wrapHeader(width, st.title(filepath.Base(path), added, removed), st.dim.Render(label),
		st.rule(width))
	if len(v.body) == 0 {
		v.body = []diffLine{{text: st.dim.Render("No changes: it is as it was before.")}}
	}
	return v
}

// shellDiffView shows the changes the session's shell commands made to
// path, in the order it made them, each marked with when.
func shellDiffView(path string, hunks []claudesession.Hunk, width int) diffView {
	label := fmt.Sprintf("changed by shell commands: %d %s, as they were made", len(hunks),
		map[bool]string{true: "change", false: "changes"}[len(hunks) == 1])
	return hunksDiffView(path, hunks, label, width)
}

// codexDiffView shows the changes a Codex session made to path, as it
// recorded them: Codex keeps no copy of the file from before.
func codexDiffView(path string, hunks []claudesession.Hunk, width int) diffView {
	if len(hunks) == 0 {
		st := diffStyles()
		return diffView{path: path, body: fileLinesPlain(path), header: wrapHeader(width,
			st.title(filepath.Base(path), 0, 0), st.dim.Render("Codex recorded no change to this file."),
			st.rule(width))}
	}
	label := fmt.Sprintf("changed by Codex: %d %s, as they were made (it keeps no copy from before)", len(hunks),
		map[bool]string{true: "change", false: "changes"}[len(hunks) == 1])
	return hunksDiffView(path, hunks, label, width)
}

// hunksDiffView shows recorded changes to path in the order they were made,
// each marked with when, under label.
func hunksDiffView(path string, hunks []claudesession.Hunk, label string, width int) diffView {
	st := diffStyles()
	var b strings.Builder
	for _, h := range hunks {
		when := ""
		if !h.At.IsZero() {
			when = " " + h.At.Local().Format("Jan 2 15:04")
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@%s\n", h.OldStart, h.OldLines, h.NewStart, h.NewLines, when)
		for _, l := range h.Lines {
			b.WriteString(l + "\n")
		}
	}
	v := diffView{path: path}
	added, removed := 0, 0
	v.body, v.hunks, added, removed = renderUnifiedDiff(b.String())
	v.header = wrapHeader(width, st.title(filepath.Base(path), added, removed), st.dim.Render(label),
		st.rule(width))
	return v
}

// wrapHeader is the diff's header lines, each wrapped to width rather than
// cut: a long name, or a long "before" line, goes on underneath.
func wrapHeader(width int, lines ...string) []string {
	var out []string
	for _, l := range lines {
		if width < 1 {
			out = append(out, l)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(l, width, ""), "\n")...)
	}
	return out
}

// gitDiff is git's unified diff of two files, which needn't be in a
// repository.
func gitDiff(before, after string) (string, error) {
	out, err := exec.Command("git", "diff", "--no-index", "--no-color", "-U3", "--", before, after).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return string(out), nil // 1: they differ
	}
	return string(out), err
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$`)

// renderUnifiedDiff draws git's diff with old and new line numbers, and
// says where each change starts and how many lines were added and removed.
func renderUnifiedDiff(diff string) ([]diffLine, []int, int, int) {
	st := diffStyles()
	var lines []diffLine
	var hunks []int
	added, removed := 0, 0
	oldN, newN := 0, 0
	inBody := false
	num := func(n int) string { return fmt.Sprintf("%4d", n) }
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if mm := hunkHeader.FindStringSubmatch(l); mm != nil {
			oldN, _ = strconv.Atoi(mm[1])
			newN, _ = strconv.Atoi(mm[2])
			hunks = append(hunks, len(lines))
			// Under the line numbers' columns, so it scrolls with the code.
			lines = append(lines, diffLine{st.dim.Render(strings.Repeat(" ", diffGutter)), st.hunk.Render(l)})
			inBody = true
			continue
		}
		if !inBody {
			continue // git's file headers
		}
		switch {
		case strings.HasPrefix(l, "+"):
			lines = append(lines, diffLine{st.dim.Render("     " + num(newN) + " "), st.add.Render("+" + tabs(l[1:]))})
			newN++
			added++
		case strings.HasPrefix(l, "-"):
			lines = append(lines, diffLine{st.dim.Render(num(oldN) + "      "), st.del.Render("-" + tabs(l[1:]))})
			oldN++
			removed++
		case strings.HasPrefix(l, `\`):
			lines = append(lines, diffLine{st.dim.Render(strings.Repeat(" ", diffGutter)), st.dim.Render(l)})
		default:
			lines = append(lines, diffLine{st.dim.Render(num(oldN) + " " + num(newN) + " "),
				st.ctx.Render(" " + tabs(strings.TrimPrefix(l, " ")))})
			oldN++
			newN++
		}
	}
	return lines, hunks, added, removed
}

// diffGutter is how wide a diff line's numbers are: "1234 1234 ".
const diffGutter = 10

func tabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// fileLinesPlain is the start of a file, for a file with no backup.
func fileLinesPlain(path string) []diffLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []diffLine
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(out) < 2000 { //nolint:mnd // enough to read
		out = append(out, diffLine{text: tabs(sc.Text())})
	}
	return out
}

type diffStyleSet struct {
	dim, ctx, add, del, hunk, bad, chip, val lipgloss.Style
}

func (s diffStyleSet) title(name string, added, removed int) string {
	t := s.chip.Render(" diff ") + s.val.Render(" "+name)
	if added > 0 {
		t += s.add.Render(" +" + strconv.Itoa(added))
	}
	if removed > 0 {
		t += s.del.Render(" −" + strconv.Itoa(removed))
	}
	return t
}

func (s diffStyleSet) rule(width int) string {
	return s.dim.Render(strings.Repeat("─", max(1, width)))
}

func diffStyles() diffStyleSet {
	base := lipgloss.NewStyle().Background(common.FilePanelBGColor)
	return diffStyleSet{
		dim:  base.Foreground(lipgloss.Color(common.Theme.FooterBorder)),
		ctx:  base.Foreground(common.FilePanelFGColor),
		add:  base.Foreground(lipgloss.Color(common.Theme.Correct)),
		del:  base.Foreground(lipgloss.Color(common.Theme.Error)),
		hunk: base.Foreground(lipgloss.Color(common.Theme.Hint)),
		bad:  base.Foreground(lipgloss.Color(common.Theme.Error)),
		chip: lipgloss.NewStyle().Background(lipgloss.Color(common.Theme.Hint)).
			Foreground(lipgloss.Color(common.Theme.FullScreenBG)).Bold(true),
		val: base.Foreground(common.FilePanelFGColor).Bold(true),
	}
}

// touchedLayerTitle is how the panel's search hierarchy names the layer.
func touchedLayerTitle(title string) string {
	return "✳ " + title + "'s files"
}
