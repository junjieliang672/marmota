package internal

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func panelNames(m *model) []string {
	var names []string
	for _, e := range m.getFocusedFilePanel().element {
		names = append(names, e.name)
	}
	return names
}

// A folder's listing is looked at again once a second, for files changed
// outside Marmota: in the background, the panel keeping its listing until
// the new one is read. A panel that went to another folder meanwhile keeps
// that one's.
func TestAFoldersListingIsReadAgainInTheBackground(t *testing.T) {
	refreshesInBackground = true
	defer func() { refreshesInBackground = false }()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o600))
	m := defaultTestModel(dir)
	TeaUpdate(m, nil)
	require.Equal(t, []string{"a.txt"}, panelNames(m))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), nil, 0o600))
	m.getFocusedFilePanel().lastTimeGetElement = time.Now().Add(-time.Hour)
	cmd := TeaUpdate(m, heartbeatMsg{})
	assert.Equal(t, []string{"a.txt"}, panelNames(m), "not read on the screen's loop")
	assert.True(t, m.getFocusedFilePanel().refreshing)

	var msgs []dirRefreshMsg
	collect(cmd, func(msg any) {
		if r, ok := msg.(dirRefreshMsg); ok {
			msgs = append(msgs, r)
		}
	})
	require.Len(t, msgs, 1)
	TeaUpdate(m, msgs[0])
	assert.True(t, slices.Equal([]string{"a.txt", "b.txt"}, panelNames(m)), panelNames(m))
	assert.False(t, m.getFocusedFilePanel().refreshing)

	// read for a folder the panel has left: dropped
	stale := msgs[0]
	stale.location = filepath.Join(dir, "elsewhere")
	stale.elements = nil
	TeaUpdate(m, stale)
	assert.Equal(t, []string{"a.txt", "b.txt"}, panelNames(m))
}

// collect runs cmd and the commands of a batch, a little while each (a tick
// never ends in time, and is left out), and hands over their messages.
func collect(cmd tea.Cmd, each func(msg any)) {
	if cmd == nil {
		return
	}
	switch msg := ExecuteTeaCmdWithTimeout(cmd, 200*time.Millisecond).(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			collect(c, each)
		}
	case nil:
	default:
		each(msg)
	}
}
