package codexsession

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// thread is a session as the server describes it.
type thread struct {
	ID              string       `json:"id"`
	Cwd             string       `json:"cwd"`
	Name            *string      `json:"name"`
	Preview         string       `json:"preview"`
	Model           string       `json:"model"`
	ReasoningEffort string       `json:"reasoningEffort"`
	CreatedAt       int64        `json:"createdAt"`
	UpdatedAt       int64        `json:"updatedAt"`
	Status          threadStatus `json:"status"`
	Path            *string      `json:"path"`
	Ephemeral       bool         `json:"ephemeral"`
	ParentThreadID  *string      `json:"parentThreadId"`
	ForkedFromID    *string      `json:"forkedFromId"`
}

type threadStatus struct {
	Type        string   `json:"type"` // notLoaded, idle, active, systemError
	ActiveFlags []string `json:"activeFlags"`
}

// Options controls one Collect call.
type Options struct {
	// Home is Codex's folder; "" is Home().
	Home    string
	Tracker *Tracker
	Timeout time.Duration
	// Since leaves out the sessions not loaded that haven't changed since.
	Since time.Time
	// Folders are the folders whose finished sessions are listed, and
	// Include more sessions to list whatever their folder.
	Folders []string
	Include map[string]string
	// Limit is how many of the latest sessions are looked at.
	Limit int
}

const (
	defaultTimeout = 2 * time.Second
	defaultLimit   = 60
	titleRunes     = 60
)

// Collect lists the sessions the server has loaded, and the finished ones
// of Options.Folders and Options.Include changed since Options.Since. It
// returns ErrNoServer, and nothing, when no Codex server runs.
//
// A loaded session's Status is what Codex is doing; Live is set for every
// loaded one. One that is idle with no window may count as finished: its
// caller knows the windows.
func Collect(o Options) ([]claudesession.Session, error) {
	if o.Home == "" {
		o.Home = Home()
	}
	if o.Tracker == nil {
		o.Tracker = NewTracker()
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.Limit <= 0 {
		o.Limit = defaultLimit
	}
	c, cancel, err := connect(o.Home, o.Timeout)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer c.Close()

	var loaded struct {
		Data []string `json:"data"`
	}
	if err := c.Call("thread/loaded/list", map[string]any{}, &loaded); err != nil {
		return nil, err
	}
	var out []claudesession.Session
	listed := map[string]bool{}
	for _, id := range loaded.Data {
		var read struct {
			Thread thread `json:"thread"`
		}
		if c.Call("thread/read", map[string]any{"threadId": id}, &read) != nil || !wanted(read.Thread) ||
			listed[read.Thread.ID] {
			continue
		}
		listed[read.Thread.ID] = true
		out = append(out, toSession(read.Thread, o.Tracker))
	}

	var recent struct {
		Data []thread `json:"data"`
	}
	if err := c.Call("thread/list", map[string]any{
		"limit": o.Limit, "sortKey": "updated_at",
		// Without it the server first reads rollouts to repair its index,
		// which takes some 200 ms each time.
		"useStateDbOnly": true,
	}, &recent); err != nil {
		return out, nil //nolint:nilerr // the loaded ones are listed
	}
	for _, t := range recent.Data {
		if listed[t.ID] || !wanted(t) || time.Unix(t.UpdatedAt, 0).Before(o.Since) {
			continue
		}
		if _, included := o.Include[t.ID]; !included && !inFolders(t.Cwd, o.Folders) {
			continue
		}
		listed[t.ID] = true
		out = append(out, toSession(t, o.Tracker))
	}
	return out, nil
}

// wanted leaves out the sessions Codex runs for itself: subagents, and
// sessions that keep no history.
func wanted(t thread) bool {
	return t.ID != "" && !t.Ephemeral && (t.ParentThreadID == nil || *t.ParentThreadID == "")
}

func inFolders(cwd string, folders []string) bool {
	cwd = filepath.Clean(cwd)
	return slices.ContainsFunc(folders, func(f string) bool { return filepath.Clean(f) == cwd })
}

func toSession(t thread, tracker *Tracker) claudesession.Session {
	s := claudesession.Session{
		ID:        t.ID,
		Agent:     claudesession.AgentCodex,
		Cwd:       t.Cwd,
		Live:      t.Status.Type != "notLoaded" && t.Status.Type != "",
		Status:    status(t.Status),
		RawStatus: t.Status.Type,
		StartedAt: time.Unix(t.CreatedAt, 0),
		Model:     t.Model,
		Effort:    t.ReasoningEffort,
	}
	if t.UpdatedAt > 0 {
		s.LastActivity = time.Unix(t.UpdatedAt, 0)
	}
	if t.Name != nil {
		s.Title = singleLine(*t.Name)
	}
	if s.Title == "" {
		s.Title = truncateRunes(singleLine(claudesession.StripSuperfileContext(t.Preview)), titleRunes)
	}
	if t.ForkedFromID != nil {
		s.ForkedFrom = *t.ForkedFromID
	}
	if t.Status.Type == "systemError" {
		s.LastError = "Codex stopped with an error"
	}
	if t.Path == nil || *t.Path == "" {
		return s
	}
	s.TranscriptPath = *t.Path
	sum, err := tracker.Summary(s.TranscriptPath)
	if err != nil {
		return s
	}
	if s.Cwd == "" {
		s.Cwd = sum.Cwd
	}
	if sum.LastActivity.After(s.LastActivity) {
		s.LastActivity = sum.LastActivity
	}
	s.LastTurnEnd = sum.LastTurnEnd
	s.LastPrompt = sum.LastPrompt
	s.EditedFiles = sum.EditedFiles
	if sum.Model != "" {
		s.Model = sum.Model
	}
	if sum.Effort != "" {
		s.Effort = sum.Effort
	}
	if s.Title == "" && sum.LastPrompt != "" {
		s.Title = truncateRunes(sum.LastPrompt, titleRunes)
	}
	return s
}

// status is what a session does: working, needing you (an approval or a
// question), or idle; ended when not loaded.
func status(st threadStatus) claudesession.Status {
	switch st.Type {
	case "active":
		for _, f := range st.ActiveFlags {
			if strings.HasPrefix(f, "waitingOn") {
				return claudesession.StatusWaiting
			}
		}
		return claudesession.StatusWorking
	case "idle", "systemError":
		return claudesession.StatusIdle
	default:
		return claudesession.StatusEnded
	}
}

// TurnOptions are what a prompt sent to a session changes for it, from
// that turn on; "" keeps the session's.
type TurnOptions struct {
	Model  string
	Effort string
	// Reviewer is who reviews its approval requests: "auto_review" for
	// Codex's automatic review.
	Reviewer string
}

// Send sends a prompt to a loaded session, as if typed in its window, with
// the turn's options. Codex runs it after the turn under way, if there is
// one.
func Send(home, id, text string, o TurnOptions) error {
	c, cancel, err := connect(home, defaultTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	defer c.Close()
	params := map[string]any{
		"threadId": id,
		"input":    []map[string]any{{"type": "text", "text": text}},
	}
	if o.Model != "" {
		params["model"] = o.Model
	}
	if o.Effort != "" {
		params["effort"] = o.Effort
	}
	if o.Reviewer != "" {
		params["approvalsReviewer"] = o.Reviewer
	}
	return c.Call("turn/start", params, nil)
}

// Rename names a session, as Codex's own rename does.
func Rename(home, id, name string) error {
	c, cancel, err := connect(home, defaultTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	defer c.Close()
	return c.Call("thread/name/set", map[string]any{"threadId": id, "name": name}, nil)
}

// Interrupt stops the turn a session is on, if it is on one.
func Interrupt(home, id string) error {
	c, cancel, err := connect(home, defaultTimeout)
	if err != nil {
		return err
	}
	defer cancel()
	defer c.Close()
	var turns struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := c.Call("thread/turns/list", map[string]any{"threadId": id, "limit": 5}, &turns); err != nil {
		return err
	}
	for _, t := range turns.Data {
		if t.Status == "inProgress" {
			return c.Call("turn/interrupt", map[string]any{"threadId": id, "turnId": t.ID}, nil)
		}
	}
	return nil
}
