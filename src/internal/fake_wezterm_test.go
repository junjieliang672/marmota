package internal

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// fakeWezterm stands in for `wezterm cli`: it keeps a table of panes and
// answers the commands superfile sends the way WezTerm does, so layout tests
// can check where panes end up instead of an exact sequence of commands.
type fakeWezterm struct {
	panes    map[int]*weztermPane
	nextPane int
	nextTab  int
	active   int // the pane the last activate-pane (or split) focused
	commands []string
}

func newFakeWezterm(t *testing.T, panes ...weztermPane) *fakeWezterm {
	t.Helper()
	f := &fakeWezterm{panes: map[int]*weztermPane{}, nextPane: 100, nextTab: 100}
	for i := range panes {
		p := panes[i]
		if p.Workspace == "" {
			p.Workspace = "default"
		}
		f.panes[p.PaneID] = &p
	}
	original := execInShell
	execInShell = func(_ time.Duration, _ string, cmd string) (int, string, error) {
		f.commands = append(f.commands, cmd)
		out, err := f.run(cmd)
		if err != nil {
			return 1, "", err
		}
		return 0, out, nil
	}
	t.Cleanup(func() { execInShell = original })
	return f
}

var (
	fakeSplitNew  = regexp.MustCompile(`^wezterm cli split-pane --pane-id (\d+) --right --cells (\d+) --cwd .* -- `)
	fakeSplitMove = regexp.MustCompile(`^wezterm cli split-pane --pane-id (\d+) (?:--right|--top|--bottom) ` +
		`--cells (\d+) --move-pane-id (\d+)$`)
	fakeSpawn = regexp.MustCompile(`^wezterm cli spawn --new-window --workspace (\S+) --cwd `)
	fakePark  = regexp.MustCompile(`^wezterm cli move-pane-to-new-tab --pane-id (\d+) --new-window ` +
		`--workspace (\S+)$`)
	fakeActivate = regexp.MustCompile(`^wezterm cli activate-pane --pane-id (\d+)$`)
	fakeKill     = regexp.MustCompile(`^wezterm cli kill-pane --pane-id (\d+)$`)
)

func (f *fakeWezterm) run(cmd string) (string, error) {
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	switch {
	case cmd == "wezterm cli list --format json":
		list := make([]weztermPane, 0, len(f.panes))
		for _, p := range f.panes {
			list = append(list, *p)
		}
		return marshalWeztermPanesForTest(list...), nil
	case fakeSplitMove.MatchString(cmd):
		m := fakeSplitMove.FindStringSubmatch(cmd)
		anchor, moved := f.panes[atoi(m[1])], f.panes[atoi(m[3])]
		cells := atoi(m[2])
		if anchor == nil || moved == nil || cells <= 0 || cells >= anchor.Size.Cols {
			// WezTerm refuses, and the moved pane is lost from its tab.
			if moved != nil {
				delete(f.panes, moved.PaneID)
			}
			return "", errors.New("split refused")
		}
		moved.TabID, moved.WindowID, moved.Workspace = anchor.TabID, anchor.WindowID, anchor.Workspace
		moved.Size.Cols = cells
		anchor.Size.Cols -= cells + 1
		f.active = moved.PaneID
		return fmt.Sprintf("%d\n", moved.PaneID), nil
	case fakeSplitNew.MatchString(cmd):
		m := fakeSplitNew.FindStringSubmatch(cmd)
		anchor := f.panes[atoi(m[1])]
		cells := atoi(m[2])
		if anchor == nil || cells <= 0 || cells >= anchor.Size.Cols {
			return "", errors.New("split refused")
		}
		f.nextPane++
		p := &weztermPane{PaneID: f.nextPane, TabID: anchor.TabID, WindowID: anchor.WindowID,
			Workspace: anchor.Workspace, Size: weztermPaneSize{Rows: anchor.Size.Rows, Cols: cells}}
		anchor.Size.Cols -= cells + 1
		f.panes[p.PaneID] = p
		f.active = p.PaneID
		return fmt.Sprintf("%d\n", p.PaneID), nil
	case fakeSpawn.MatchString(cmd):
		m := fakeSpawn.FindStringSubmatch(cmd)
		f.nextPane++
		f.nextTab++
		f.panes[f.nextPane] = &weztermPane{PaneID: f.nextPane, TabID: f.nextTab, WindowID: f.nextTab,
			Workspace: m[1], Size: weztermPaneSize{Rows: 24, Cols: 80}}
		return fmt.Sprintf("%d\n", f.nextPane), nil
	case fakePark.MatchString(cmd):
		m := fakePark.FindStringSubmatch(cmd)
		p := f.panes[atoi(m[1])]
		if p == nil {
			return "", errors.New("no such pane")
		}
		f.giveWidthBack(p)
		f.nextTab++
		p.TabID, p.WindowID, p.Workspace, p.Size.Cols = f.nextTab, f.nextTab, m[2], 80
		return "", nil
	case fakeActivate.MatchString(cmd):
		f.active = atoi(fakeActivate.FindStringSubmatch(cmd)[1])
		return "", nil
	case fakeKill.MatchString(cmd):
		id := atoi(fakeKill.FindStringSubmatch(cmd)[1])
		f.closePane(id)
		return "", nil
	}
	return "", nil
}

// giveWidthBack widens the panes left in p's tab when p leaves it.
func (f *fakeWezterm) giveWidthBack(p *weztermPane) {
	for _, other := range f.panes {
		if other.TabID == p.TabID && other.PaneID != p.PaneID {
			other.Size.Cols += p.Size.Cols + 1
		}
	}
}

// closePane is a pane's process exiting: the pane goes, and WezTerm puts the
// keyboard in another pane of its tab.
func (f *fakeWezterm) closePane(id int) {
	p := f.panes[id]
	if p == nil {
		return
	}
	f.giveWidthBack(p)
	delete(f.panes, id)
	if f.active == id {
		f.active = 0
		for _, other := range f.panes {
			if other.TabID == p.TabID {
				f.active = other.PaneID
			}
		}
	}
}

// paneList is what superfile's last refresh would have listed.
func (f *fakeWezterm) paneList() []weztermPane {
	list := make([]weztermPane, 0, len(f.panes))
	for _, p := range f.panes {
		list = append(list, *p)
	}
	return list
}

func (f *fakeWezterm) tabOf(paneID int) int {
	if p := f.panes[paneID]; p != nil {
		return p.TabID
	}
	return -1
}
