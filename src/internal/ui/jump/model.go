// Package jump is the s list, drawn in the focused file panel's place: one
// place to go to a pinned folder or file,
// a session (running, or ended to resume), a folder zoxide knows, or a file
// edited lately. It has two modes. Searching, what you type filters every
// section and enter goes to the first item (with nothing typed, home).
// Picking (↓ from the search), the arrows move over the numbered items,
// [n]g and G jump, and the keys that act on an item elsewhere act on the
// picked one: t pins, r renames, a session's keys are the Sessions box's.
// esc steps back: from picking to the search, then clears what is typed,
// then closes. The sections sit one under the other: Pinned, Sessions,
// Resume, Folders, Files. The preview beside it shows the item enter goes
// to (Picked).
package jump

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

// Kind is what an item is.
type Kind int

const (
	Pinned  Kind = iota // a pinned folder or file
	Session             // a running session
	Ended               // a session that has ended, to resume
	Folder              // a folder zoxide knows
	File                // a file edited lately
)

// Kinds are the sections in order.
var Kinds = []Kind{Pinned, Session, Ended, Folder, File}

func (k Kind) Title() string {
	return [...]string{"Pinned", "Sessions", "Resume", "Folders", "Files"}[k]
}

// capped is how many folders and files show while nothing is typed; the
// pinned ones, the sessions, and the five ended of each agent all show.
const capped = 5

// Item is one row.
type Item struct {
	Kind Kind
	// Key is the item's path, or the session's id.
	Key   string
	Title string
	// Where is the folder it is in, or a session's folder, shown dim.
	Where string
	// Meta is on the right: how long ago, what a session is doing.
	Meta string
	// Tag is cc or co for a session.
	Tag string
	// Dir is set for a folder.
	Dir bool
}

// Section is a kind's items. Matched is set when they already match what
// was typed (zoxide's, which zoxide searched), so the list keeps them all.
type Section struct {
	Kind    Kind
	Items   []Item
	Matched bool
}

// ActionKind is what a key did.
type ActionKind int

const (
	None         ActionKind = iota
	Closed                  // esc with nothing typed
	Home                    // enter with nothing typed: home
	Go                      // enter: go to the item
	NewPanel                // ctrl+n: the item's folder in a new panel
	ItemKey                 // a key for the picked item, for the host to act on
	QueryChanged            // what is typed changed
	Renamed                 // a rename was confirmed: Text is the new name
)

// Action is what HandleKey reports.
type Action struct {
	Kind ActionKind
	Item Item
	Key  tea.KeyMsg
	Text string
}

// Model is the list.
type Model struct {
	open    bool
	input   textinput.Model
	picking bool
	// scope is -1 for every section, else the index in Kinds of the one
	// shown alone.
	scope int
	// only is set when the list shows one section and tab can't leave it,
	// as when the Ask box lists the sessions it can resume.
	only     bool
	sections []Section
	shown    []Section // what matches what is typed
	cursor   int       // in the numbered items, while picking
	count    string    // digits typed while picking, for g
	renaming bool
	rename   textinput.Model
	// width and height are the file panel's it is drawn in, its frame
	// included: what arrives late (zoxide's answer, older sessions) or tab
	// doesn't resize it.
	width    int
	height   int
	numWidth int // the digits of the last row's number, as last drawn
	// ShortPath writes a folder as the rest of Marmota does (~ for home).
	ShortPath func(string) string
	// Keys are the keys the picked item takes, each key then what it does,
	// for the foot of the list.
	Keys func(Item) []string
	// HomeText is where enter goes with nothing typed.
	HomeText string
	// KeysElsewhere is set while the status line lists FootKeys, so the
	// list doesn't.
	KeysElsewhere bool
}

func New() Model {
	in := common.GeneratePromptTextInput()
	in.Prompt = ""
	onPanel(&in)
	return Model{input: in, scope: -1, width: 100, height: 30, HomeText: "~"} //nolint:mnd // until sized
}

// Open shows sections, searching every one, with nothing typed. With only
// it shows that kind's section alone, and tab can't leave it.
func (m *Model) Open(sections []Section, only ...Kind) tea.Cmd {
	m.open, m.picking, m.renaming, m.cursor, m.count = true, false, false, 0, ""
	m.scope, m.only = -1, len(only) > 0
	if m.only {
		m.scope = indexOf(only[0])
	}
	m.input.SetValue("")
	m.input.Placeholder = m.placeholder()
	m.SetSections(sections)
	return m.input.Focus()
}

func (m *Model) Close() {
	m.open, m.picking, m.renaming = false, false, false
	m.input.Blur()
}

func (m *Model) IsOpen() bool   { return m.open }
func (m *Model) Picking() bool  { return m.picking }
func (m *Model) Renaming() bool { return m.renaming }
func (m *Model) Query() string  { return m.input.Value() }

// SetSize is the list's size: the file panel's, its frame included.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
}

// SetSections gives the list its items again: after zoxide answered, a
// pin changed, or the ended sessions were read. The picked item stays
// picked when it is still there.
func (m *Model) SetSections(sections []Section) {
	picked, wasPicked := m.Picked()
	m.sections = sections
	m.filter()
	if !wasPicked || !m.picking {
		return
	}
	items := m.visible()
	for i, it := range items {
		if it.Kind == picked.Kind && it.Key == picked.Key {
			m.cursor = i
			return
		}
	}
	m.cursor = min(m.cursor, max(0, len(items)-1))
	if len(items) == 0 {
		m.picking = false
	}
}

// filter keeps, of each section, the items whose name or folder's name
// holds every word typed, best match first; with nothing typed, the first
// few of the long sections. Words, not scattered letters: enter goes to
// the first match, so a loose one would send you somewhere you didn't mean.
func (m *Model) filter() {
	query := strings.TrimSpace(m.input.Value())
	m.shown = m.shown[:0]
	for _, s := range m.sections {
		if m.scope >= 0 && s.Kind != Kinds[m.scope] {
			continue
		}
		switch {
		case query == "":
			if m.scope < 0 && (s.Kind == Folder || s.Kind == File) && len(s.Items) > capped {
				s.Items = s.Items[:capped]
			}
			m.shown = append(m.shown, s)
		case s.Matched:
			m.shown = append(m.shown, s)
		default:
			m.shown = append(m.shown, Section{Kind: s.Kind, Items: matching(s.Items, query)})
		}
	}
}

// Picked is the item enter goes to: the picked one while picking, else
// the first one shown once something is typed.
func (m *Model) Picked() (Item, bool) {
	items := m.visible()
	if m.picking {
		if m.cursor < len(items) {
			return items[m.cursor], true
		}
		return Item{}, false
	}
	if strings.TrimSpace(m.input.Value()) == "" {
		return Item{}, false
	}
	for _, kind := range enterOrder {
		for _, it := range items {
			if it.Kind == kind {
				return it, true
			}
		}
	}
	return Item{}, false
}

// placeholder is what the search box says while nothing is typed: how to
// search, and where enter goes now. The sections are named just below.
func (m *Model) placeholder() string {
	if m.only {
		return "type a session's name or folder"
	}
	return "type a name or folder · enter: " + m.HomeText
}

// GoesHome reports whether enter goes home: nothing typed or picked, and
// not a list of one section.
func (m *Model) GoesHome() bool {
	return !m.picking && !m.only && strings.TrimSpace(m.input.Value()) == ""
}

// enterOrder is where enter looks for its match while searching: places
// before sessions, though sessions are listed above folders. A word such as
// a project's name finds both, and you mostly mean the folder.
var enterOrder = []Kind{Pinned, Folder, File, Session, Ended}

// HandleKey takes a key.
func (m *Model) HandleKey(msg tea.KeyMsg) Action {
	if m.renaming {
		return m.renameKey(msg)
	}
	if m.picking {
		return m.pickKey(msg)
	}
	return m.searchKey(msg)
}

func (m *Model) searchKey(msg tea.KeyMsg) Action {
	typed := strings.TrimSpace(m.input.Value()) != ""
	switch msg.String() {
	case "esc", "ctrl+c":
		if m.input.Value() != "" {
			m.input.SetValue("") // clear first, close next
			m.filter()
			return Action{Kind: QueryChanged}
		}
		m.Close()
		return Action{Kind: Closed}
	case "enter":
		if it, ok := m.Picked(); ok {
			return Action{Kind: Go, Item: it}
		}
		if !typed && !m.only {
			return Action{Kind: Home}
		}
		return Action{}
	case "down":
		if len(m.visible()) > 0 {
			m.picking, m.cursor, m.count = true, 0, ""
		}
		return Action{}
	case "tab", "shift+tab":
		m.cycleScope(msg.String() == "tab")
		return Action{}
	case "ctrl+n":
		if it, ok := m.Picked(); ok {
			return Action{Kind: NewPanel, Item: it}
		}
		return Action{}
	}
	before := m.input.Value()
	m.input, _ = m.input.Update(msg)
	if m.input.Value() == before {
		return Action{}
	}
	m.filter()
	return Action{Kind: QueryChanged}
}

func (m *Model) pickKey(msg tea.KeyMsg) Action {
	items := m.visible()
	key := msg.String()
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] >= '0' && msg.Runes[0] <= '9' &&
		(m.count != "" || msg.Runes[0] != '0') {
		m.count += key
		return Action{}
	}
	n, _ := strconv.Atoi(m.count)
	m.count = ""
	switch key {
	case "esc":
		m.picking = false // back to the search, keeping what is typed
		return Action{}
	case "up", "k":
		if m.cursor == 0 {
			m.picking = false
		} else {
			m.cursor--
		}
		return Action{}
	case "down", "j":
		m.cursor = min(m.cursor+1, len(items)-1)
		return Action{}
	case "g":
		m.cursor = min(max(n, 1), len(items)) - 1
		return Action{}
	case "G":
		if n > 0 {
			m.cursor = min(n, len(items)) - 1
		} else {
			m.cursor = len(items) - 1
		}
		return Action{}
	case "tab", "shift+tab":
		m.cycleScope(key == "tab")
		return Action{}
	}
	it, ok := m.Picked()
	if !ok {
		return Action{}
	}
	switch key {
	case "enter":
		return Action{Kind: Go, Item: it}
	case "ctrl+n":
		return Action{Kind: NewPanel, Item: it}
	}
	return Action{Kind: ItemKey, Item: it, Key: msg}
}

// StartRename asks for a new name for the picked item, starting from name.
func (m *Model) StartRename(name string) tea.Cmd {
	m.renaming = true
	m.rename = common.GeneratePinnedRenameTextInput(len([]rune(name)), name)
	onPanel(&m.rename)
	return m.rename.Focus()
}

// onPanel draws in on the file panel's background, the list's, not on a
// dialog's.
func onPanel(in *textinput.Model) {
	in.TextStyle = common.FilePanelStyle
	in.PlaceholderStyle = common.PopupStyles(false).Faint
	in.Cursor.TextStyle = common.FilePanelStyle
	in.Cursor.Style = in.Cursor.Style.Background(common.FilePanelBGColor)
}

func (m *Model) renameKey(msg tea.KeyMsg) Action {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.renaming = false
		return Action{}
	case "enter":
		m.renaming = false
		it, _ := m.Picked()
		name := strings.TrimSpace(m.rename.Value())
		if name == "" || name == it.Title {
			return Action{}
		}
		return Action{Kind: Renamed, Item: it, Text: name}
	}
	m.rename, _ = m.rename.Update(msg)
	return Action{}
}

// cycleScope shows the next section alone, or every section after the
// last.
func (m *Model) cycleScope(forward bool) {
	if m.only {
		return
	}
	n := len(Kinds) + 1 // every section, then each one
	step := 1
	if !forward {
		step = n - 1
	}
	m.scope = (m.scope+1+step)%n - 1
	m.cursor = 0
	m.filter()
	if len(m.visible()) == 0 {
		m.picking = false
	}
}

func indexOf(k Kind) int {
	for i, kind := range Kinds {
		if kind == k {
			return i
		}
	}
	return -1
}

// Visible is the items shown, in the order of their numbers.
func (m *Model) Visible() []Item { return m.visible() }

// matching is the items holding every word of query in their name or their
// folder's name, ignoring case, ranked by fzf's score.
func matching(items []Item, query string) []Item {
	words := strings.Fields(strings.ToLower(query))
	var kept []Item
	var haystack []string
	for _, it := range items {
		text := strings.ToLower(it.Title + " " + filepath.Base(it.Where))
		all := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				all = false
				break
			}
		}
		if all {
			kept = append(kept, it)
			haystack = append(haystack, it.Title+" "+filepath.Base(it.Where))
		}
	}
	if len(kept) < 2 { //nolint:mnd // nothing to rank
		return kept
	}
	ranked := make([]Item, 0, len(kept))
	seen := make([]bool, len(kept))
	for _, r := range utils.FzfSearch(strings.Join(words, " "), haystack) {
		ranked = append(ranked, kept[r.HayIndex])
		seen[r.HayIndex] = true
	}
	for i, it := range kept {
		if !seen[i] {
			ranked = append(ranked, it)
		}
	}
	return ranked
}
