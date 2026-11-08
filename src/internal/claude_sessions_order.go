package internal

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/utils"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// The Sessions list keeps one order for the day, so a number means the same
// session all day. A session that starts during the day joins the end of
// its folder's group, a new folder the end of the list. The first refresh
// of a new day sorts again by last activity. The order is saved, so
// restarting superfile doesn't sort it either. Finished sessions are listed
// for the day they were active, except the folder's Ask session.

const (
	// claudeSessionsRecentFolders is how many folders sessions were last
	// active in are listed, besides those with a running session.
	claudeSessionsRecentFolders = 10
	// claudeSessionsPerFolder is how many of the day's finished sessions
	// each folder lists at most.
	claudeSessionsPerFolder = 10
	claudeDayFormat         = "2006-01-02"
)

// claudeSessionsOrder is the list's order for one day.
type claudeSessionsOrder struct {
	Day     string   `json:"day"`
	Folders []string `json:"folders"`
	// Sessions lists each folder's sessions, in order.
	Sessions map[string][]string `json:"sessions"`
}

// claudeSessionsGroup is one folder of the list and its sessions, in order.
type claudeSessionsGroup struct {
	Folder   string
	Sessions []claudesession.Session
}

func today() string {
	return time.Now().Format(claudeDayFormat)
}

// startOfDay is midnight, local time, at the start of day.
func startOfDay(day string) time.Time {
	now := time.Now()
	if t, err := time.ParseInLocation(claudeDayFormat, day, now.Location()); err == nil {
		return t
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func loadClaudeSessionsOrder() claudeSessionsOrder {
	var o claudeSessionsOrder
	data, err := os.ReadFile(variable.ClaudeSessionsOrderFile)
	if err == nil {
		if err := json.Unmarshal(data, &o); err != nil {
			slog.Debug("Ignoring an unreadable sessions order", "error", err)
			o = claudeSessionsOrder{}
		}
	}
	if o.Sessions == nil {
		o.Sessions = make(map[string][]string)
	}
	return o
}

func (o *claudeSessionsOrder) save() {
	data, err := json.Marshal(o)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(variable.ClaudeSessionsOrderFile), utils.ConfigDirPerm)
	}
	if err == nil {
		err = os.WriteFile(variable.ClaudeSessionsOrderFile, data, utils.ConfigFilePerm)
	}
	if err != nil {
		slog.Debug("Could not save the sessions order", "error", err)
	}
}

// stale reports whether the order is from another day and the list sorts
// again.
func (o *claudeSessionsOrder) stale(day string) bool {
	return o.Day != day || len(o.Folders) == 0
}

// include lists today's sessions with their folders, for the next listing
// to find whatever their age.
func (o *claudeSessionsOrder) include() map[string]string {
	ids := make(map[string]string)
	for folder, list := range o.Sessions {
		for _, id := range list {
			ids[id] = folder
		}
	}
	return ids
}

// update fits sessions into the order: on a new day it sorts everything
// again by last activity; otherwise sessions it doesn't know yet join the
// end of their folder's group. It reports whether the order changed.
func (o *claudeSessionsOrder) update(sessions []claudesession.Session, day string) bool {
	if o.stale(day) {
		o.sortAgain(sessions, day)
		return true
	}
	changed := false
	for _, s := range sessions {
		folder := sessionFolder(s)
		if folder == "" || slices.Contains(o.Sessions[folder], s.ID) {
			continue
		}
		if !slices.Contains(o.Folders, folder) {
			o.Folders = append(o.Folders, folder)
		}
		o.Sessions[folder] = append(o.Sessions[folder], s.ID)
		changed = true
	}
	return changed
}

// sortAgain orders folders by their most recent session and sessions
// newest first.
func (o *claudeSessionsOrder) sortAgain(sessions []claudesession.Session, day string) {
	byFolder := make(map[string][]claudesession.Session)
	var folders []string
	for _, s := range sessions {
		folder := sessionFolder(s)
		if folder == "" {
			continue
		}
		if _, ok := byFolder[folder]; !ok {
			folders = append(folders, folder)
		}
		byFolder[folder] = append(byFolder[folder], s)
	}
	newest := func(list []claudesession.Session) time.Time {
		var t time.Time
		for _, s := range list {
			if a := sessionActivity(s); a.After(t) {
				t = a
			}
		}
		return t
	}
	sort.SliceStable(folders, func(i, j int) bool {
		return newest(byFolder[folders[i]]).After(newest(byFolder[folders[j]]))
	})
	o.Day, o.Folders, o.Sessions = day, folders, make(map[string][]string, len(folders))
	for _, folder := range folders {
		list := byFolder[folder]
		sort.SliceStable(list, func(i, j int) bool { return sessionActivity(list[i]).After(sessionActivity(list[j])) })
		for _, s := range list {
			o.Sessions[folder] = append(o.Sessions[folder], s.ID)
		}
	}
}

// groups lays sessions out in the order. Sessions the order doesn't know
// yet, as before the first refresh is applied, come at the end of their
// group; those with no folder at the very end.
func (o *claudeSessionsOrder) groups(sessions []claudesession.Session) []claudeSessionsGroup {
	byID := make(map[string]claudesession.Session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	listed := make(map[string]bool)
	var groups []claudeSessionsGroup
	add := func(folder string, s claudesession.Session) {
		for i := range groups {
			if groups[i].Folder == folder {
				groups[i].Sessions = append(groups[i].Sessions, s)
				return
			}
		}
		groups = append(groups, claudeSessionsGroup{Folder: folder, Sessions: []claudesession.Session{s}})
	}
	for _, folder := range o.Folders {
		for _, id := range o.Sessions[folder] {
			if s, ok := byID[id]; ok && !listed[id] {
				add(folder, s)
				listed[id] = true
			}
		}
	}
	for _, s := range sessions {
		if folder := sessionFolder(s); folder != "" && !listed[s.ID] {
			add(folder, s)
			listed[s.ID] = true
		}
	}
	for _, s := range sessions {
		if !listed[s.ID] {
			add("", s)
			listed[s.ID] = true
		}
	}
	return groups
}

// sessionFolder is the folder a session works in, which names its group.
func sessionFolder(s claudesession.Session) string {
	if s.Cwd == "" {
		return ""
	}
	return filepath.Clean(s.Cwd)
}

// sessionActivity is when a session was last active.
func sessionActivity(s claudesession.Session) time.Time {
	if s.LastActivity.After(s.StartedAt) {
		return s.LastActivity
	}
	return s.StartedAt
}

// hereFolder is the group the file panel's folder belongs to: the nearest
// folder of a group that holds it, else the folder itself. Home and the
// filesystem root hold everything, so they never count.
func hereFolder(panelFolder string, groups []claudeSessionsGroup) string {
	panelFolder = filepath.Clean(panelFolder)
	home, _ := os.UserHomeDir()
	best := ""
	for _, g := range groups {
		f := g.Folder
		if f == "/" || f == filepath.Clean(home) {
			continue
		}
		if (panelFolder == f || strings.HasPrefix(panelFolder, f+string(filepath.Separator))) && len(f) > len(best) {
			best = f
		}
	}
	if best == "" {
		return panelFolder
	}
	return best
}
