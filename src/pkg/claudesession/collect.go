package claudesession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"
)

const (
	defaultTimeout = 5 * time.Second
	// fallbackTitleRunes caps a title made from the last prompt.
	fallbackTitleRunes = 60
)

// Options controls one Collect call.
type Options struct {
	Run         Runner
	ProjectsDir string
	Tracker     *Tracker
	Home        string
	Timeout     time.Duration
	// Folders are the folders whose finished sessions are listed, PerFolder
	// of each, newest first. Sessions only `claude -p` ever ran, such as
	// the questions superfile's Ask sent, are left out.
	Folders   []string
	PerFolder int
	// RecentFolders adds this many of the folders sessions were last active
	// in to Folders, most recent first.
	RecentFolders int
	// Since, when set, leaves out the finished sessions whose transcript
	// hasn't changed since, but for those in Always.
	Since time.Time
	// Include are sessions listed whatever their kind: id → folder.
	Include map[string]string
	// Always are sessions listed whatever their kind or age, such as a
	// folder's Ask session: id → folder.
	Always map[string]string
}

// Snapshot is the result of one Collect call.
type Snapshot struct {
	Live []Session // running sessions, newest first
	// Past holds the finished sessions of Folders, Include and Always,
	// newest first.
	Past []Session
	// Folders are Options.Folders, then the recent folders found.
	Folders []string
	LiveErr error // why live sessions couldn't be listed, if they couldn't
	At      time.Time
}

// Find returns the session with the given id.
func (s Snapshot) Find(id string) (Session, bool) {
	for _, sess := range append(append([]Session(nil), s.Live...), s.Past...) {
		if sess.ID == id {
			return sess, true
		}
	}
	return Session{}, false
}

// Collect lists live sessions and the past sessions of Options.Folder.
func Collect(ctx context.Context, o Options) Snapshot {
	if o.Run == nil {
		o.Run = ExecRunner
	}
	if o.Tracker == nil {
		o.Tracker = NewTracker()
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.Home == "" {
		o.Home, _ = os.UserHomeDir()
	}
	snap := Snapshot{At: time.Now()}

	live, err := listLive(ctx, o)
	if err != nil {
		snap.LiveErr = err
	}
	snap.Live = live

	liveIDs := make(map[string]bool, len(live))
	for _, s := range live {
		liveIDs[s.ID] = true
	}
	snap.Folders = append([]string(nil), o.Folders...)
	if o.RecentFolders > 0 {
		for _, f := range RecentFolders(o.ProjectsDir, o.Tracker, o.RecentFolders) {
			if !slices.Contains(snap.Folders, f) {
				snap.Folders = append(snap.Folders, f)
			}
		}
	}
	snap.Past = pastSessions(o, snap.Folders, liveIDs)
	return snap
}

// pastSessions lists the finished sessions of folders and o.Include, newest
// first, leaving out the running ones.
func pastSessions(o Options, folders []string, liveIDs map[string]bool) []Session {
	p := pastList{listed: make(map[string]bool)}
	p.addSessions(o, o.Always, liveIDs, time.Time{})
	p.addSessions(o, o.Include, liveIDs, o.Since)
	for _, folder := range folders {
		p.addFolder(o, folder, liveIDs)
	}
	sort.SliceStable(p.sessions, func(i, j int) bool {
		return p.sessions[i].LastActivity.After(p.sessions[j].LastActivity)
	})
	return p.sessions
}

// pastList gathers finished sessions, each once.
type pastList struct {
	sessions []Session
	listed   map[string]bool
}

// addSessions adds the sessions of ids, id → folder, whose transcripts
// changed since since (any, when it is zero).
func (p *pastList) addSessions(o Options, ids map[string]string, liveIDs map[string]bool, since time.Time) {
	for id, folder := range ids {
		if liveIDs[id] || p.listed[id] {
			continue
		}
		path := FindTranscript(o.ProjectsDir, folder, id)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err != nil || info.ModTime().Before(since) {
			continue
		}
		if sum, err := o.Tracker.Summary(path); err == nil {
			p.add(id, path, sum)
		}
	}
}

func (p *pastList) add(id, path string, sum Summary) {
	s := Session{ID: id, Status: StatusEnded, TranscriptPath: path}
	applySummary(&s, sum)
	p.sessions = append(p.sessions, s)
	p.listed[id] = true
}

// addFolder adds the folder's newest o.PerFolder sessions that you typed in
// and that changed since o.Since, leaving out those only `claude -p` ran.
func (p *pastList) addFolder(o Options, folder string, liveIDs map[string]bool) {
	n := 0
	for _, f := range newestFirst(transcriptsIn(filepath.Join(o.ProjectsDir, ProjectSlug(folder)))) {
		if n >= o.PerFolder || f.modTime.Before(o.Since) {
			return // newest first: the rest are older still
		}
		id := SessionIDFromPath(f.path)
		if liveIDs[id] || p.listed[id] {
			continue
		}
		sum, err := o.Tracker.Summary(f.path)
		// Two folders can share a project name; the transcript knows.
		if err != nil || sum.PrintOnly || (sum.Cwd != "" && filepath.Clean(sum.Cwd) != filepath.Clean(folder)) {
			continue
		}
		p.add(id, f.path, sum)
		n++
	}
}

// RecentFolders returns the folders sessions were last active in, most
// recent first, at most limit of them.
func RecentFolders(projectsDir string, tracker *Tracker, limit int) []string {
	entries, err := os.ReadDir(projectsDir)
	if err != nil || limit <= 0 {
		return nil
	}
	type active struct {
		folder string
		at     time.Time
	}
	var found []active
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files := newestFirst(transcriptsIn(filepath.Join(projectsDir, e.Name())))
		if len(files) == 0 {
			continue
		}
		sum, err := tracker.Summary(files[0].path)
		if err != nil || sum.Cwd == "" {
			continue
		}
		found = append(found, active{folder: filepath.Clean(sum.Cwd), at: files[0].modTime})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	var folders []string
	for _, f := range found {
		if !slices.Contains(folders, f.folder) {
			folders = append(folders, f.folder)
		}
		if len(folders) == limit {
			break
		}
	}
	return folders
}

func newestFirst(files []transcriptFile) []transcriptFile {
	sort.SliceStable(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	return files
}

func listLive(ctx context.Context, o Options) ([]Session, error) {
	runCtx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	out, err := o.Run(runCtx, "claude", "agents", "--json")
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	live, err := ParseLive(out)
	if err != nil {
		return nil, err
	}

	pids := make([]int, 0, len(live))
	for _, s := range live {
		pids = append(pids, s.PID)
	}
	procs, _ := LookupProcesses(runCtx, o.Run, pids)

	kept := live[:0]
	for _, s := range live {
		p, ok := procs[s.PID]
		if ok && commandIsPrintRun(p.Command) {
			continue
		}
		if ok {
			s.TTY = p.TTY
			s.Command = p.Command
			s.RemoteControl = commandHasRemoteControl(p.Command)
		}
		kept = append(kept, s)
	}
	live = kept
	for i := range live {
		s := &live[i]
		s.TranscriptPath = FindTranscript(o.ProjectsDir, s.Cwd, s.ID)
		if s.TranscriptPath != "" {
			if sum, err := o.Tracker.Summary(s.TranscriptPath); err == nil {
				applySummary(s, sum)
			}
		}
	}
	sort.SliceStable(live, func(i, j int) bool { return live[i].StartedAt.After(live[j].StartedAt) })
	return live, nil
}

func applySummary(s *Session, sum Summary) {
	switch {
	case sum.CustomTitle != "":
		s.Title = sum.CustomTitle
	case sum.Title != "":
		s.Title = sum.Title
	case s.Title == "" && sum.LastPrompt != "":
		s.Title = truncateRunes(sum.LastPrompt, fallbackTitleRunes)
	}
	if s.Cwd == "" {
		s.Cwd = sum.Cwd
	}
	s.RemoteControl = s.RemoteControl || sum.RemoteControl
	s.RemoteURL = sum.RemoteURL
	s.LastActivity = sum.LastActivity
	s.LastTurnEnd = sum.LastTurnEnd
	s.LastPrompt = sum.LastPrompt
	s.AwaySummary = sum.AwaySummary
	s.EditedFiles = sum.EditedFiles
	s.Todos = sum.Todos
	s.PermissionMode = sum.PermissionMode
	s.Model, s.Effort = sum.Model, sum.Effort
	s.PendingTool = sum.PendingTool
	s.LastError = sum.LastError
	s.PrintOnly = sum.PrintOnly
	if s.Name == "" {
		s.Name = sum.AgentName
	}
}
