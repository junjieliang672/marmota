package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

func orderIDs(groups []claudeSessionsGroup) map[string][]string {
	out := make(map[string][]string)
	for _, g := range groups {
		for _, s := range g.Sessions {
			out[g.Folder] = append(out[g.Folder], s.ID)
		}
	}
	return out
}

// The order holds for the day: a new session joins the end of its folder's
// group, however recent it is. A new day sorts again, newest first.
func TestTheSessionsOrderHoldsForTheDay(t *testing.T) {
	now := time.Now()
	at := func(minutes int) time.Time { return now.Add(time.Duration(-minutes) * time.Minute) }
	sessions := []claudesession.Session{
		{ID: "a-old", Cwd: "/w/a", LastActivity: at(50)},
		{ID: "b", Cwd: "/w/b", LastActivity: at(5)},
		{ID: "a-new", Cwd: "/w/a", LastActivity: at(10)},
	}
	var o claudeSessionsOrder
	require.True(t, o.update(sessions, "2026-11-05"))
	groups := o.groups(sessions)
	require.Len(t, groups, 2)
	assert.Equal(t, "/w/b", groups[0].Folder, "the folder with the newest session first")
	assert.Equal(t, []string{"a-new", "a-old"}, orderIDs(groups)["/w/a"])

	sessions = append(sessions, claudesession.Session{ID: "a-newest", Cwd: "/w/a", LastActivity: now},
		claudesession.Session{ID: "c", Cwd: "/w/c", LastActivity: now})
	sessions[0].LastActivity = now // busy again; it keeps its place
	require.True(t, o.update(sessions, "2026-11-05"))
	assert.False(t, o.update(sessions, "2026-11-05"), "nothing new")
	groups = o.groups(sessions)
	assert.Equal(t, []string{"/w/b", "/w/a", "/w/c"}, []string{groups[0].Folder, groups[1].Folder, groups[2].Folder})
	assert.Equal(t, []string{"a-new", "a-old", "a-newest"}, orderIDs(groups)["/w/a"])

	require.True(t, o.update(sessions, "2026-11-06"), "a new day")
	groups = o.groups(sessions)
	assert.Equal(t, []string{"a-old", "a-newest", "a-new"}, orderIDs(groups)["/w/a"], "sorted again by activity")
}

func TestTheSessionsOrderIsSaved(t *testing.T) {
	file := variable.ClaudeSessionsOrderFile
	variable.ClaudeSessionsOrderFile = filepath.Join(t.TempDir(), "order.json") //nolint:reassign // its own file
	t.Cleanup(func() { variable.ClaudeSessionsOrderFile = file })               //nolint:reassign // restored

	o := loadClaudeSessionsOrder()
	assert.True(t, o.stale("2026-11-05"), "nothing saved yet")
	o.update([]claudesession.Session{{ID: "x", Cwd: "/w/x"}}, "2026-11-05")
	o.save()

	again := loadClaudeSessionsOrder()
	assert.False(t, again.stale("2026-11-05"))
	assert.Equal(t, map[string]string{"x": "/w/x"}, again.include(), "today's sessions are listed whatever their age")

	require.NoError(t, os.WriteFile(variable.ClaudeSessionsOrderFile, []byte("{"), 0o600))
	broken := loadClaudeSessionsOrder()
	assert.True(t, broken.stale("2026-11-05"), "an unreadable file starts over")
}

func TestHereIsTheNearestGroupAboveThePanel(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	groups := []claudeSessionsGroup{{Folder: "/w/repo"}, {Folder: "/w/repo/docs"}, {Folder: home}, {Folder: "/"}}
	assert.Equal(t, "/w/repo/docs", hereFolder("/w/repo/docs/img", groups))
	assert.Equal(t, "/w/repo", hereFolder("/w/repo/src", groups))
	assert.Equal(t, "/w/other", hereFolder("/w/other", groups), "no group holds it: its own folder")
	assert.Equal(t, filepath.Join(home, "notes"), hereFolder(filepath.Join(home, "notes"), groups),
		"home holds everything, so it never counts")
}
