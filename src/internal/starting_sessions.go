package internal

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// A session the Ask box starts is listed by Claude Code (or found on Codex's
// server) only once it has started. Until then the Sessions box shows it as
// a row of its own: starting, or asking you something first, such as whether
// to trust its folder. The preview shows just that question and its choices,
// cut out of its pane, and K answers it there, as any session's question;
// enter brings the session beside Marmota to see all of it.

// startingSession is the row of a session the Ask box started, while it
// isn't listed; false once it is.
func (m *model) startingSession(st *askStart) (claudesession.Session, bool) {
	if st == nil || st.listed || st.paneID == 0 {
		return claudesession.Session{}, false
	}
	if st.codex && !st.resumed {
		if _, found := m.findCodexStart(st); found {
			return claudesession.Session{}, false
		}
	} else if s, ok := m.claude.snapshot.Find(st.id); ok && s.Live {
		return claudesession.Session{}, false
	}
	s := claudesession.Session{
		ID: st.id, Title: st.title, LastPrompt: st.title, Cwd: st.folder, Live: true,
		Status: claudesession.StatusWorking, PendingTool: "starting", StartedAt: st.began,
	}
	if st.codex {
		s.Agent = claudesession.AgentCodex
	}
	if st.asks != "" {
		s.Status, s.PendingTool = claudesession.StatusWaiting, st.asks
	}
	return s, true
}

// startingSessions are the rows of the sessions the Ask box started that
// aren't listed yet, newest first.
func (m *model) startingSessions() []claudesession.Session {
	var out []claudesession.Session
	for _, st := range m.ask.starts {
		if s, ok := m.startingSession(st); ok {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b claudesession.Session) int { return b.StartedAt.Compare(a.StartedAt) })
	return out
}

// unlistedStart is the start that the row id is, or nil.
func (m *model) unlistedStart(id string) *askStart {
	st := m.ask.starts[id]
	if _, ok := m.startingSession(st); !ok {
		return nil
	}
	return st
}

// needsYouSince is when a session started needing you: zero when unknown.
func (m *model) needsYouSince(id string) time.Time {
	if at, ok := m.claude.waitingSince[id]; ok {
		return at
	}
	if st := m.unlistedStart(id); st != nil && st.asks != "" {
		return st.asksSince
	}
	return time.Time{}
}

// setStartDialog notes what a starting session's pane asks ("" for nothing)
// and its dialog's lines, read off its pane. When it starts or stops asking,
// the preview's question follows.
func (m *model) setStartDialog(st *askStart, dialog []string, asks string) tea.Cmd {
	changed := (asks == "") != (st.asks == "")
	if asks != "" && st.asks == "" {
		st.asksSince = time.Now()
		m.flashClaude("The new session in " + folderName(st.folder) + " asks: " + asks + " " +
			firstKey(common.Hotkeys.OpenAskBar) + " in the Sessions box answers it")
	}
	st.dialog, st.asks = dialog, asks
	if q := &m.sessions.question; q.start && q.id == st.id {
		q.lines = dialog
		if asks == "" && m.sessions.focus == sessionsFocusAnswer && len(q.keys) == 0 && !q.sending {
			m.sessions.focus = sessionsFocusList
			m.flashClaude("Answered")
		}
	}
	if changed {
		return m.watchClaudeQuestion()
	}
	return nil
}

// startDialog cuts the question out of the screen of a session that hasn't
// started: what it asks, then its choices, such as Claude Code's "Do you
// trust the files in this folder?" or Codex's "Trust this folder?". The rest
// of the screen stays out. A dialog is a choice under the cursor (❯ or ›)
// with a line under it that says enter answers and esc leaves; with none,
// lines is nil.
func startDialog(screen string) (lines []string, question string) {
	text := strings.Split(ansi.Strip(strings.ReplaceAll(screen, "\r", "")), "\n")
	edge := make([]bool, len(text))
	for i, l := range text {
		l = strings.TrimRight(l, " ")
		if at := strings.Index(l, "│"); at >= 0 && strings.TrimSpace(l[:at]) == "" {
			l = strings.TrimRight(strings.TrimSuffix(l[at+len("│"):], "│"), " ") // inside a box
		}
		if t := strings.TrimSpace(l); t != "" && strings.Trim(t, "─━▔═╭╮╰╯┌┐└┘│") == "" {
			edge[i], l = true, ""
		}
		text[i] = l
	}
	foot := -1
	for i := len(text) - 1; i >= 0 && foot < 0; i-- {
		if l := strings.ToLower(text[i]); strings.Contains(l, "enter") && strings.Contains(l, "esc") {
			foot = i
		}
	}
	cursor := -1
	for i := foot - 1; i >= 0 && cursor < 0; i-- {
		if t := strings.TrimSpace(text[i]); strings.HasPrefix(t, "❯") || strings.HasPrefix(t, "›") {
			cursor = i
		}
	}
	if cursor < 0 {
		return nil, ""
	}
	// The choices are the lines next to the cursor's whose text starts
	// where the text of the one under it does.
	runes := []rune(text[cursor])
	mark := slices.IndexFunc(runes, func(r rune) bool { return r != ' ' })
	col := mark + 1
	for col < len(runes) && runes[col] == ' ' {
		col++
	}
	choice := func(i int) bool {
		r := []rune(text[i])
		at := slices.IndexFunc(r, func(c rune) bool { return c != ' ' })
		return at == col || (at == mark && (r[at] == '❯' || r[at] == '›'))
	}
	first, last := cursor, cursor
	for first > 0 && choice(first-1) {
		first--
	}
	for last+1 < foot && choice(last+1) {
		last++
	}
	top := 0
	for i := first - 1; i >= 0; i-- {
		if edge[i] {
			top = i + 1
			break
		}
	}
	question = dialogQuestion(text[top:first])
	lines = []string{question, ""}
	for _, l := range text[first : last+1] {
		lines = append(lines, string([]rune(l)[min(mark, len([]rune(l))):]))
	}
	return lines, question
}

// dialogQuestion is what a dialog asks: the first sentence of its text
// ending in a question mark, else its first paragraph.
func dialogQuestion(text []string) string {
	var paragraphs []string
	var cur []string
	for i := 0; i <= len(text); i++ {
		if i < len(text) && strings.TrimSpace(text[i]) != "" {
			cur = append(cur, strings.TrimSpace(text[i]))
		} else if len(cur) > 0 {
			paragraphs = append(paragraphs, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, p := range paragraphs {
		if end := strings.Index(p, "?"); end >= 0 {
			q := p[:end+1]
			if start := strings.LastIndex(q, ". "); start >= 0 {
				q = q[start+2:]
			}
			return q
		}
	}
	if len(paragraphs) > 0 {
		return paragraphs[0]
	}
	return "a question"
}
