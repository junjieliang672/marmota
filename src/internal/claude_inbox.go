package internal

import (
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Inbox (deck item 8) is what needs you: a session that asks you
// something, fails, or finishes a reply you haven't read. When that happens
// to a session you can't see, a toast shows in superfile's top right corner,
// and while superfile isn't focused WezTerm posts a notification too. Focus
// never moves: ! opens the toast's session, or else the one that has waited
// longest, and esc puts the toast away.

const (
	// claudeToastDuration is how long a toast about a finished or failed
	// session stays. One about a session that needs you stays until it
	// doesn't.
	claudeToastDuration = 10 * time.Second
	claudeToastMaxWidth = 56
	claudeToastMinWidth = 34
	// The toast covers at most this share of superfile's width.
	claudeToastWidthShare = 2
	claudeNoticeMaxRunes  = 200
)

// inboxKind is what a toast is about, the most pressing first.
type inboxKind int

const (
	inboxNeedsYou inboxKind = iota
	inboxFailed
	inboxDone
)

// claudeToast is the toast on screen.
type claudeToast struct {
	id   string
	kind inboxKind
	// until is when a toast about a finished or failed session goes away.
	until time.Time
}

// claudeInboxPhase is the part of a session's state the Inbox follows.
type claudeInboxPhase struct {
	waiting bool
	failed  string
	// turnEnd is when its latest turn ended; a later one is a new reply.
	turnEnd time.Time
}

// Test seam.
var (
	// terminalNotify asks the terminal to post a notification (OSC 9), which
	// WezTerm shows as a system notification. bubbletea writes the window
	// title the same way, between frames.
	terminalNotify = func(text string) { //nolint:gochecknoglobals // test seam
		_, _ = io.WriteString(os.Stdout, "\x1b]9;"+text+"\a")
	}
)

// updateClaudeInbox looks at what changed since the last refresh. A session
// you can't see (neither beside superfile nor shown in the preview) that
// now needs you, failed, or finished a reply gets a toast, and a
// notification while superfile isn't focused.
func (m *model) updateClaudeInbox() tea.Cmd {
	now := time.Now()
	first := m.claude.inbox == nil
	phases := make(map[string]claudeInboxPhase, len(m.claude.snapshot.Live))
	since := make(map[string]time.Time)
	var news *claudeToast
	var about claudesession.Session
	for _, s := range m.claude.snapshot.Live {
		p := claudeInboxPhase{waiting: s.Status == claudesession.StatusWaiting, failed: s.LastError,
			turnEnd: s.UpdateMark()}
		phases[s.ID] = p
		if p.waiting {
			since[s.ID] = now
			if t, ok := m.claude.waitingSince[s.ID]; ok {
				since[s.ID] = t
			}
		}
		prev, known := m.claude.inbox[s.ID]
		if first || m.isClaudeSessionOnScreen(s) {
			continue
		}
		if kind, ok := inboxNews(prev, p, known, m.claude.unread[s.ID]); ok && (news == nil || kind < news.kind) {
			news, about = &claudeToast{id: s.ID, kind: kind}, s
		}
	}
	m.claude.inbox, m.claude.waitingSince = phases, since
	if t := m.claude.toast; t != nil && !m.claudeToastDue(*t) {
		m.claude.toast = nil
	}
	if news == nil {
		return nil
	}
	if news.kind != inboxNeedsYou {
		news.until = now.Add(claudeToastDuration)
	}
	m.claude.toast = news
	if m.terminalFocused {
		return nil
	}
	notice := claudeNotice(about, news.kind)
	return func() tea.Msg {
		terminalNotify(notice)
		return nil
	}
}

// inboxNews is what a session's change from prev to now tells you, if
// anything. known is false for a session the last refresh didn't list, and
// unread says you haven't seen its latest reply.
func inboxNews(prev, now claudeInboxPhase, known, unread bool) (inboxKind, bool) {
	switch {
	case now.waiting && (!known || !prev.waiting):
		return inboxNeedsYou, true
	case now.failed != "" && now.failed != prev.failed:
		return inboxFailed, true
	case known && unread && now.turnEnd.After(prev.turnEnd):
		return inboxDone, true
	}
	return 0, false
}

// claudeToastDue reports whether a toast still has something to tell: its
// session still needs you, or its time isn't up and you haven't looked.
func (m *model) claudeToastDue(t claudeToast) bool {
	s, ok := m.claude.snapshot.Find(t.id)
	if !ok || !s.Live || m.isClaudeSessionOnScreen(s) {
		return false
	}
	switch t.kind {
	case inboxNeedsYou:
		return s.Status == claudesession.StatusWaiting
	case inboxDone:
		return m.claude.unread[t.id] && time.Now().Before(t.until)
	case inboxFailed:
		return time.Now().Before(t.until)
	}
	return false
}

// visibleClaudeToast returns the toast to draw, if any.
func (m *model) visibleClaudeToast() (claudeToast, bool) {
	if m.claude.toast == nil || !m.claudeToastDue(*m.claude.toast) {
		return claudeToast{}, false
	}
	return *m.claude.toast, true
}

// dismissClaudeToast puts the toast away ("esc later"); what it was about
// stays in the Inbox for !.
func (m *model) dismissClaudeToast() bool {
	if _, ok := m.visibleClaudeToast(); !ok {
		return false
	}
	m.claude.toast = nil
	return true
}

// claudeInboxTarget is the session the Inbox opens: the toast's, or else the
// one that has waited longest for you.
func (m *model) claudeInboxTarget() string {
	if m.claude.toast != nil && m.claudeToastDue(*m.claude.toast) {
		return m.claude.toast.id
	}
	target := ""
	var oldest time.Time
	for _, s := range m.claude.snapshot.Live {
		var at time.Time
		switch {
		case s.Status == claudesession.StatusWaiting:
			at = m.claude.waitingSince[s.ID]
		case m.claude.unread[s.ID]:
			at = s.UpdateMark()
		default:
			continue
		}
		if target == "" || at.Before(oldest) {
			target, oldest = s.ID, at
		}
	}
	return target
}

// claudeToastText is what a toast says: the session's name, what about it
// (needs you, failed, done), what it waits on, and where and since when.
func (m *model) claudeToastText(t claudeToast, s claudesession.Session, now time.Time,
) (string, string, string, string) {
	title := s.DisplayTitle()
	where := m.sessionShortPlace(s)
	switch t.kind {
	case inboxNeedsYou:
		if age := waitingAge(now, m.claude.waitingSince[s.ID]); age != "" {
			where += " · waiting " + age
		}
		return title, "needs you", firstNonEmpty(s.PendingTool, s.WaitingFor), where
	case inboxFailed:
		return title, "failed", s.LastError, where
	case inboxDone:
		detail := s.LastPrompt
		if detail != "" {
			detail = "“" + detail + "”"
		}
		return title, "done", detail, where + " · reply unread"
	}
	return title, "", "", where
}

// waitingAge is how long a session has waited, in seconds for the first
// minute ("40s"), then as the sheet says it ("4m").
func waitingAge(now, since time.Time) string {
	if since.IsZero() {
		return ""
	}
	if d := now.Sub(since); d < time.Minute {
		if d < time.Second {
			return ""
		}
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	return shortAge(now, since)
}

// sessionShortPlace is where a session runs, briefly: "hidden", "other tab".
func (m *model) sessionShortPlace(s claudesession.Session) string {
	switch place := m.sessionPlace(s); place {
	case "in another WezTerm tab":
		return "other tab"
	case "in the background":
		return "background"
	default:
		return place
	}
}

// renderClaudeToast draws the toast, as wide as its text allows.
func (m *model) renderClaudeToast(t claudeToast) string {
	s, _ := m.claude.snapshot.Find(t.id)
	title, what, detail, when := m.claudeToastText(t, s, time.Now())
	color, glyph := lipgloss.Color(common.ClaudeNeedsYouColor), "⚑"
	switch t.kind {
	case inboxNeedsYou:
	case inboxFailed:
		color, glyph = lipgloss.Color(common.Theme.Error), "✗"
	case inboxDone:
		color, glyph = lipgloss.Color(common.Theme.Correct), "✓"
	}
	bg := lipgloss.Color(common.Theme.FilePanelBG)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(common.Theme.Hint)).Background(bg).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(common.GitUntrackedColor)).Background(bg)
	keys := keyStyle.Render(firstKey(common.Hotkeys.JumpToUnreadSession)) + dim.Render(" open  ") +
		keyStyle.Render("esc") + dim.Render(" later")

	width := min(claudeToastMaxWidth, max(claudeToastMinWidth, m.fullWidth/claudeToastWidthShare))
	inner := width - common.BorderPadding - common.BorderPadding // border, then a space each side
	// The name gives way first, so "needs you" always shows.
	suffix := " · " + what
	name := ansi.Truncate(title, max(1, inner-ansi.StringWidth(glyph+" "+suffix)), "…")
	lines := []string{
		lipgloss.NewStyle().Foreground(color).Background(bg).Bold(true).Render(glyph + " " + name + suffix),
	}
	if detail != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(common.Theme.FilePanelFG)).
			Background(bg).Render(ansi.Truncate(detail, inner, "…")))
	}
	whenRoom := inner - ansi.StringWidth(keys) - 1
	whenText := dim.Render(ansi.Truncate(when, max(0, whenRoom), "…"))
	gap := max(1, inner-ansi.StringWidth(whenText)-ansi.StringWidth(keys))
	lines = append(lines, whenText+dim.Render(strings.Repeat(" ", gap))+keys)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).
		BorderBackground(bg).Background(bg).Padding(0, 1).Width(inner + common.BorderPadding).
		Render(strings.Join(lines, "\n"))
}

// claudeNotice is the notification's text, plain and on one line.
func claudeNotice(s claudesession.Session, kind inboxKind) string {
	title := s.DisplayTitle()
	var text string
	switch kind {
	case inboxNeedsYou:
		text = "⚑ " + title + " needs you"
		if detail := firstNonEmpty(s.PendingTool, s.WaitingFor); detail != "" {
			text += ": " + detail
		}
	case inboxFailed:
		text = "✗ " + title + " failed: " + s.LastError
	case inboxDone:
		text = "✓ " + title + " is done"
	}
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	return ansi.Truncate(clean, claudeNoticeMaxRunes, "…")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
