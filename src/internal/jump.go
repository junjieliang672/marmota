package internal

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/adrg/xdg"
	tea "github.com/charmbracelet/bubbletea"
	zoxidelib "github.com/lazysegtree/go-zoxide"

	"github.com/junjieliang672/marmota/src/internal/common"
	askui "github.com/junjieliang672/marmota/src/internal/ui/ask"
	"github.com/junjieliang672/marmota/src/internal/ui/jump"
	sessionsui "github.com/junjieliang672/marmota/src/internal/ui/sessions"
	"github.com/junjieliang672/marmota/src/internal/ui/zoxide"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
	"github.com/junjieliang672/marmota/src/pkg/codexsession"
)

// The s list (src/internal/ui/jump) goes to a pinned folder or file, a
// session (running, or ended to resume), a folder zoxide knows or a file
// edited lately. It is drawn in the focused file panel's place, and the
// preview beside it shows the item enter goes to: a folder's contents, a
// file, a session's conversation. This file gives it its items, acts on
// them (enter, and the keys an item takes elsewhere: t pins, r renames, a
// session's keys are the Sessions box's) and keeps the preview on them.

// jumpState is the s list's side of the model.
type jumpState struct {
	// zoxide is zoxide's answer for zoxideFor, what was typed when it was
	// asked.
	zoxide    []zoxidelib.Result
	zoxideFor string
	// forResume is set when the Ask box opened it (ask_resume): enter on a
	// session makes it the one the box resumes.
	forResume bool
	// convo is the conversation of the session the list is on, for the
	// preview.
	convo sessionsConversation
}

// jumpZoxideMsg is zoxide's answer for query.
type jumpZoxideMsg struct {
	query   string
	results []zoxidelib.Result
}

// setJumpSize sizes the list as the focused file panel it is drawn in. Its
// keys go to the status line when there is one: it is wider.
func (m *model) setJumpSize() {
	m.jumpUI.KeysElsewhere = common.Config.ShowStatusLine && m.statusLineModel.IsEnabled()
	if len(m.fileModel.filePanels) == 0 {
		return
	}
	m.jumpUI.SetSize(m.filePanelWidthAt(m.filePanelFocusIndex)+common.BorderPadding,
		m.mainPanelHeight+common.BorderPadding)
}

// openJump opens the s list on every section, nothing typed.
func (m *model) openJump() tea.Cmd {
	m.jump.forResume = false
	return m.startJump()
}

// openJumpForResume opens the s list on the Resume section alone, for the
// Ask box to pick the session it resumes.
func (m *model) openJumpForResume() tea.Cmd {
	m.jump.forResume = true
	return m.startJump(jump.Ended)
}

func (m *model) startJump(only ...jump.Kind) tea.Cmd {
	m.jumpUI.ShortPath = shortPath
	m.jumpUI.Keys = m.jumpKeys
	m.jump.zoxide, m.jump.zoxideFor = nil, ""
	m.setJumpSize()
	focus := m.jumpUI.Open(m.jumpSections(), only...)
	return tea.Batch(focus, m.jumpZoxideCmd(""), m.readEndedCmd(), m.jumpPreviewCmd())
}

// refreshJump gives the open list its items again, and reads the
// conversation of the session it is on when it changed.
func (m *model) refreshJump() {
	if !m.jumpUI.IsOpen() {
		return
	}
	m.jumpUI.SetSections(m.jumpSections())
	if s, ok := m.jumpPreviewSession(); ok {
		m.jump.convo = readConversation(m.jump.convo, s)
	}
}

// jumpTarget is what the preview shows while the list is open: the item
// enter goes to, or home when enter goes home.
func (m *model) jumpTarget() (jump.Item, bool) {
	if it, ok := m.jumpUI.Picked(); ok {
		return it, true
	}
	if m.jumpUI.GoesHome() {
		return jump.Item{Kind: jump.Folder, Key: xdg.Home, Dir: true}, true
	}
	return jump.Item{}, false
}

// jumpPreviewSession is the session the list is on, while the preview
// shows it.
func (m *model) jumpPreviewSession() (claudesession.Session, bool) {
	if !m.jumpUI.IsOpen() || !m.fileModel.filePreview.IsOpen() {
		return claudesession.Session{}, false
	}
	it, ok := m.jumpTarget()
	switch {
	case !ok:
	case it.Kind == jump.Session:
		if s, found := m.findSession(it.Key); found {
			return s, true
		}
		return m.findResumable(it.Key)
	case it.Kind == jump.Ended:
		return m.findResumable(it.Key)
	}
	return claudesession.Session{}, false
}

// jumpSessionPreview draws session s in the preview, as the Sessions box
// would: its question if it asks one, its conversation.
func (m *model) jumpSessionPreview(s claudesession.Session, width, height int) string {
	if st := m.unlistedStart(s.ID); st != nil {
		return askui.RenderSession(m.startingSessionPaneView(st), "", width, height, 0, false)
	}
	convo := m.jump.convo
	if convo.id != s.ID {
		convo = sessionsConversation{id: s.ID}
	}
	return askui.RenderSession(m.sessionPaneViewOf(s, convo), "", width, height, 0, false)
}

// jumpPreviewPath is the file or folder the preview shows while the list
// is open; none while it is on a session or on nothing.
func (m *model) jumpPreviewPath() (string, bool) {
	it, ok := m.jumpTarget()
	if !ok || it.Kind == jump.Session || it.Kind == jump.Ended {
		return "", false
	}
	return it.Key, true
}

// jumpPreviewCmd keeps the preview on what the list is on, after a key or
// an answer moved it: a session's conversation is read at once, a file or
// folder is drawn after the debounce the cursor's moves have. Closed, the
// list gives the preview back to the panel's cursor.
func (m *model) jumpPreviewCmd() tea.Cmd {
	if !m.fileModel.filePreview.IsOpen() {
		return nil
	}
	if !m.jumpUI.IsOpen() {
		return m.requestFilePreviewRender(false)
	}
	if s, ok := m.jumpPreviewSession(); ok {
		m.jump.convo = readConversation(m.jump.convo, s)
		return nil
	}
	return schedulePreviewRefresh(m)
}

// jumpZoxideCmd asks zoxide for the folders matching query.
func (m *model) jumpZoxideCmd(query string) tea.Cmd {
	client := m.zClient
	if client == nil || !common.Config.ZoxideSupport || m.jump.forResume {
		return nil
	}
	return func() tea.Msg {
		return jumpZoxideMsg{query: query, results: zoxide.Query(client, query)}
	}
}

func (m *model) applyJumpZoxide(msg jumpZoxideMsg) tea.Cmd {
	if !m.jumpUI.IsOpen() || msg.query != m.jumpUI.Query() {
		return nil // typed on since
	}
	m.jump.zoxide, m.jump.zoxideFor = msg.results, msg.query
	m.refreshJump()
	return m.jumpPreviewCmd() // the first match may be a folder it found
}

// jumpSections are the list's items, section by section.
func (m *model) jumpSections() []jump.Section {
	now := time.Now()
	pinned := m.sidebarModel.PinnedEntries()
	pinnedAt := map[string]bool{}
	sections := make([]jump.Section, 0, len(jump.Kinds))

	sec := jump.Section{Kind: jump.Pinned}
	for _, e := range pinned {
		pinnedAt[e.Location] = true
		sec.Items = append(sec.Items, jump.Item{Kind: jump.Pinned, Key: e.Location, Title: e.Name,
			Where: filepath.Dir(e.Location), Dir: isDir(e.Location)})
	}
	sections = append(sections, sec)

	sec = jump.Section{Kind: jump.Session}
	for _, s := range m.dockSessions() {
		sec.Items = append(sec.Items, jump.Item{Kind: jump.Session, Key: s.ID, Title: s.DisplayTitle(), Where: s.Cwd,
			Tag: agentTag(s), Meta: jumpSessionState(m.sessionState(s))})
	}
	sections = append(sections, sec)

	sec = jump.Section{Kind: jump.Ended}
	ended := append(m.endedSessions(false), m.endedSessions(true)...)
	slices.SortStableFunc(ended, func(a, b claudesession.Session) int { return b.LastActivity.Compare(a.LastActivity) })
	for _, s := range ended {
		sec.Items = append(sec.Items, jump.Item{Kind: jump.Ended, Key: s.ID, Title: s.DisplayTitle(), Where: s.Cwd,
			Tag: agentTag(s), Meta: shortAge(now, s.LastActivity)})
	}
	sections = append(sections, sec)

	// zoxide searched what was typed itself; a pinned folder shows once.
	sec = jump.Section{Kind: jump.Folder, Matched: true}
	if m.jump.zoxideFor == m.jumpUI.Query() {
		for _, r := range m.jump.zoxide {
			if pinnedAt[r.Path] {
				continue
			}
			sec.Items = append(sec.Items, jump.Item{Kind: jump.Folder, Key: r.Path, Title: filepath.Base(r.Path),
				Where: filepath.Dir(r.Path), Meta: strconv.Itoa(int(r.Score)), Dir: true})
		}
	}
	sections = append(sections, sec)

	sec = jump.Section{Kind: jump.File}
	for _, e := range m.sidebarModel.RecentEntries() {
		sec.Items = append(sec.Items, jump.Item{Kind: jump.File, Key: e.Location, Title: filepath.Base(e.Location),
			Where: filepath.Dir(e.Location), Meta: shortAge(now, e.At)})
	}
	return append(sections, sec)
}

func agentTag(s claudesession.Session) string {
	if s.IsCodex() {
		return "co"
	}
	return "cc"
}

func jumpSessionState(state sessionsui.State) string {
	switch state {
	case sessionsui.NeedsYou:
		return "needs you"
	case sessionsui.Working:
		return "working"
	case sessionsui.Unread:
		return "unread"
	case sessionsui.Idle, sessionsui.Ended:
	}
	return "idle"
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// jumpKey takes a key while the list is open, and keeps the preview on
// what it is on.
func (m *model) jumpKey(msg tea.KeyMsg) tea.Cmd {
	m.setJumpSize() // a panel may have been opened or closed since
	cmd := m.jumpAct(m.jumpUI.HandleKey(msg))
	return tea.Batch(cmd, m.jumpPreviewCmd())
}

// jumpAct does what a key in the list asked.
func (m *model) jumpAct(act jump.Action) tea.Cmd {
	switch act.Kind {
	case jump.Closed:
		if m.jump.forResume {
			return nil // the Ask box keeps the keyboard
		}
		return m.popupClosed()
	case jump.Home:
		m.jumpUI.Close()
		m.jumpGoToPath(xdg.Home)
	case jump.Go:
		return m.jumpGo(act.Item)
	case jump.NewPanel:
		if act.Item.Kind == jump.Session || act.Item.Kind == jump.Ended {
			return nil
		}
		dir := act.Item.Key
		if !act.Item.Dir {
			dir = filepath.Dir(dir)
		}
		m.jumpUI.Close()
		if err := m.createNewFilePanel(dir); err != nil {
			m.flashClaude(err.Error())
		}
	case jump.QueryChanged:
		m.refreshJump()
		return m.jumpZoxideCmd(m.jumpUI.Query())
	case jump.Renamed:
		return m.jumpRename(act.Item, act.Text)
	case jump.ItemKey:
		return m.jumpItemKey(act.Item, act.Key)
	case jump.None:
	}
	return nil
}

// jumpGo is enter on an item: a folder is opened in the panel, a file's
// folder is with the cursor on it, a session is shown beside Marmota, an
// ended one is resumed in a hidden pane; the Sessions box goes on its row.
func (m *model) jumpGo(it jump.Item) tea.Cmd {
	if m.jump.forResume {
		m.jumpUI.Close()
		if s, ok := m.findResumable(it.Key); ok && it.Kind == jump.Ended {
			m.setResumeTarget(s)
		}
		return nil
	}
	m.jumpUI.Close()
	m.dock.popupFrom = false
	switch it.Kind {
	case jump.Ended:
		s, ok := m.findResumable(it.Key)
		if !ok {
			m.flashClaude(it.Title + " is no longer listed")
			return nil
		}
		cmd, err := m.resumeHidden(s, resumeCommand(s))
		if err != nil {
			m.flashClaude(err.Error())
			return nil
		}
		m.dock.lastID = s.ID
		return tea.Batch(cmd, m.focusDockSessionsOn(s.ID))
	case jump.Session:
		s, ok := m.findResumable(it.Key)
		if !ok {
			m.flashClaude(it.Title + " is no longer listed")
			return nil
		}
		message, err := m.jumpToClaudeSession(s, false)
		if err != nil {
			m.flashClaude(err.Error())
			return nil
		}
		m.sessions.pendingSelect, m.dock.lastID = s.ID, s.ID
		var cmd tea.Cmd
		if len(m.dockSessions()) > 0 {
			cmd = m.focusDockSessionsOn(s.ID)
		}
		m.flashClaude(message)
		return cmd
	case jump.Pinned, jump.Folder, jump.File:
		m.jumpGoToPath(it.Key)
	}
	return nil
}

// jumpGoToPath opens a folder in the panel, or a file's folder with the
// cursor on the file, and gives the panel the keyboard.
func (m *model) jumpGoToPath(path string) {
	dir, file := path, ""
	if !isDir(path) {
		dir, file = filepath.Dir(path), filepath.Base(path)
	}
	if err := m.updateCurrentFilePanelDir(dir); err != nil {
		m.flashClaude("Couldn't open " + shortPath(dir) + ": " + err.Error())
		return
	}
	panel := m.getFocusedFilePanel()
	panel.targetFile = file
	m.focusPanel = nonePanelFocus
	panel.isFocused = true
}

// jumpItemKey is a key on the picked item: the one it is elsewhere.
func (m *model) jumpItemKey(it jump.Item, msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	is := func(binding []string) bool { return slices.Contains(binding, key) }
	switch it.Kind {
	case jump.Session:
		// The Sessions box's keys, on its row.
		if m.jump.forResume {
			return nil
		}
		m.jumpUI.Close()
		m.dock.popupFrom = false
		return tea.Batch(m.focusDockSessionsOn(it.Key), m.dockSessionsKey(msg))
	case jump.Ended:
		return m.jumpEndedKey(it, is)
	case jump.Pinned, jump.Folder, jump.File:
	}
	if m.jump.forResume {
		return nil
	}
	path := it.Key
	switch {
	case is(common.Hotkeys.PinnedDirectory):
		if err := m.sidebarModel.TogglePinnedDirectory(path); err != nil {
			m.flashClaude(err.Error())
			return nil
		}
		if it.Kind == jump.Pinned {
			m.flashClaude("Unpinned " + it.Title)
		} else {
			m.flashClaude("Pinned " + it.Title)
		}
		m.refreshJump()
	case is(common.Hotkeys.FilePanelItemRename):
		return m.jumpUI.StartRename(it.Title)
	case is(common.Hotkeys.CopyPath):
		if err := writeClipboard(path); err != nil {
			m.flashClaude("Couldn't copy the path: " + err.Error())
			return nil
		}
		m.flashClaude("Copied " + shortPath(path))
	case is(common.Hotkeys.SessionMention):
		cwd := m.getFocusedFilePanel().location
		if m.sessions.composer.AddMention(formatAgentReferences([]agentPath{{path: path, dir: it.Dir}}, cwd), path) {
			m.flashClaude("Added " + it.Title + " to your question")
		} else {
			m.flashClaude("Already in your question")
		}
	case is(common.Hotkeys.OpenFileWithEditor) && !it.Dir:
		m.jumpUI.Close()
		return m.openPathWithEditor(path, true)
	case is(common.Hotkeys.OpenCurrentDirectoryWithEditor) && it.Dir:
		m.jumpUI.Close()
		m.jumpGoToPath(path)
		return m.openDirectoryWithEditor()
	case is(common.Hotkeys.SendToShelf):
		return m.shelfCmd([]string{path})
	case it.Kind == jump.Pinned && (key == "shift+up" || key == "shift+down"):
		step := 1
		if key == "shift+up" {
			step = -1
		}
		if err := m.sidebarModel.MovePinned(path, step); err != nil {
			m.flashClaude(err.Error())
		}
		m.refreshJump()
	case it.Kind == jump.File && is(common.Hotkeys.DeleteItems):
		if err := m.sidebarModel.RemoveRecentEdit(path); err != nil {
			m.flashClaude(err.Error())
			return nil
		}
		m.flashClaude("Removed " + it.Title + " from the recent files (the file stays)")
		m.refreshJump()
	}
	return nil
}

// jumpEndedKey is a key on an ended session: K resumes it once you write
// to it, w forks it, r renames it, p copies its folder.
func (m *model) jumpEndedKey(it jump.Item, is func([]string) bool) tea.Cmd {
	s, ok := m.findResumable(it.Key)
	if !ok {
		return nil
	}
	switch {
	case is(common.Hotkeys.OpenAskBar):
		m.jumpUI.Close()
		if m.jump.forResume {
			m.setResumeTarget(s)
			return nil
		}
		cmd := m.openDockAsk("")
		m.setResumeTarget(s)
		return cmd
	case m.jump.forResume:
	case is(common.Hotkeys.SessionFork):
		m.jumpUI.Close()
		m.forkClaudeSession(s)
	case is(common.Hotkeys.SessionRename):
		return m.jumpUI.StartRename(it.Title)
	case is(common.Hotkeys.CopyPath):
		m.copySessionFolder(s)
	}
	return nil
}

// jumpRename gives the picked item the name typed: a pin's name in the
// list, a session's title, or a folder's or file's name on disk.
func (m *model) jumpRename(it jump.Item, name string) tea.Cmd {
	switch it.Kind {
	case jump.Pinned:
		if err := m.sidebarModel.RenamePinned(it.Key, name); err != nil {
			m.flashClaude(err.Error())
		}
	case jump.Ended:
		if s, ok := m.findResumable(it.Key); ok {
			cmd := m.renameEndedSession(s, name)
			m.refreshJump()
			return cmd
		}
	case jump.Folder, jump.File:
		to, err := renameOnDisk(it.Key, name)
		if err != nil {
			m.flashClaude("Couldn't rename " + it.Title + ": " + err.Error())
			return nil
		}
		if it.Kind == jump.File {
			_ = m.sidebarModel.RemoveRecentEdit(it.Key)
			_ = m.sidebarModel.RecordRecentEdit(to)
		}
		_ = m.sidebarModel.RepointPinned(it.Key, to, name)
		m.flashClaude("Renamed " + it.Title + " to " + name)
		m.refreshJump()
		return m.jumpZoxideCmd(m.jumpUI.Query())
	case jump.Session:
	}
	m.refreshJump()
	return nil
}

// renameEndedSession names an ended session: through its listing when the
// sessions list has it, else in its transcript (Claude Code) or through
// Codex's server.
func (m *model) renameEndedSession(s claudesession.Session, name string) tea.Cmd {
	if _, ok := m.claude.snapshot.Find(s.ID); ok {
		return m.renameClaudeSession(s.ID, name)
	}
	var err error
	if s.IsCodex() {
		err = codexsession.Rename(codexHome(), s.ID, name)
	} else {
		err = claudesession.SetCustomTitle(s.TranscriptPath, s.ID, name)
	}
	if err != nil {
		m.flashClaude("Couldn't rename " + s.DisplayTitle() + ": " + err.Error())
		return nil
	}
	for i := range m.resume.ended {
		if m.resume.ended[i].ID == s.ID {
			m.resume.ended[i].Title = name
		}
	}
	m.flashClaude("Renamed to " + name)
	return nil
}

// renameOnDisk renames path to name in the same folder, refusing a name
// that is taken or that names another folder.
func renameOnDisk(path, name string) (string, error) {
	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", errors.New("a name can't hold a slash")
	}
	to := filepath.Join(filepath.Dir(path), name)
	if _, err := os.Lstat(to); err == nil {
		return "", errors.New(name + " is already there")
	}
	return to, os.Rename(path, to)
}

// jumpKeys are the keys the picked item takes, for the list's foot.
func (m *model) jumpKeys(it jump.Item) []string {
	k := firstKey
	if m.jump.forResume {
		return []string{"enter", "resume this one"}
	}
	switch it.Kind {
	case jump.Session:
		return []string{"enter", "beside", k(common.Hotkeys.OpenAskBar), "write", k(common.Hotkeys.SessionMention), "mention",
			k(common.Hotkeys.SessionRename), "rename", k(common.Hotkeys.SessionFork), "fork",
			k(common.Hotkeys.SessionStop), "stop", k(common.Hotkeys.MoveSessionHere), "move here",
			k(common.Hotkeys.ShowSessionFiles), "files", k(common.Hotkeys.CopyPath), "copy folder"}
	case jump.Ended:
		return []string{"enter", "resume", k(common.Hotkeys.OpenAskBar), "resume with a message",
			k(common.Hotkeys.SessionFork), "fork", k(common.Hotkeys.SessionRename), "rename",
			k(common.Hotkeys.CopyPath), "copy folder"}
	case jump.Pinned, jump.Folder, jump.File:
	}
	pin := "pin"
	if it.Kind == jump.Pinned {
		pin = "unpin"
	}
	keys := []string{"enter", "go"}
	if !it.Dir {
		keys = append(keys, k(common.Hotkeys.OpenFileWithEditor), "edit")
	}
	keys = append(keys, k(common.Hotkeys.PinnedDirectory), pin, k(common.Hotkeys.FilePanelItemRename), "rename")
	if it.Kind == jump.Pinned {
		keys = append(keys, "shift+↑↓", "move")
	}
	keys = append(keys, k(common.Hotkeys.CopyPath), "copy path", k(common.Hotkeys.SessionMention), "mention",
		k(common.Hotkeys.SendToShelf), "shelf")
	if it.Kind == jump.File {
		keys = append(keys, k(common.Hotkeys.DeleteItems), "forget")
	}
	if it.Dir {
		keys = append(keys, "ctrl+n", "new panel")
	}
	return keys
}
