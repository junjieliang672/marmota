package sidebar

import (
	"errors"
	"path/filepath"
	"time"
)

// Entry is a pinned or recently edited path, for the s list.
type Entry struct {
	Location string
	// Name is what the list calls it: a pin's own name, else its base name.
	Name string
	// At is when it was pinned or last edited.
	At time.Time
}

func entries(dirs []directory) []Entry {
	out := make([]Entry, 0, len(dirs))
	for _, d := range dirs {
		at := d.PinnedAt
		if d.EditedAt != 0 {
			at = d.EditedAt
		}
		e := Entry{Location: d.Location, Name: d.Name}
		if at != 0 {
			e.At = time.Unix(0, at)
		}
		out = append(out, e)
	}
	return out
}

// PinnedEntries are the pinned folders and files, in their order.
func (s *Model) PinnedEntries() []Entry {
	if s.pinnedMgr == nil {
		return nil
	}
	return entries(s.pinnedMgr.Load())
}

// RecentEntries are the files edited lately, the newest first.
func (s *Model) RecentEntries() []Entry {
	if s.recentMgr == nil {
		return nil
	}
	return entries(s.recentMgr.Load())
}

// RenamePinned gives the pin of location another name.
func (s *Model) RenamePinned(location, name string) error {
	if s.pinnedMgr == nil {
		return errors.New("no pinned list")
	}
	dirs := s.pinnedMgr.Load()
	for i := range dirs {
		if dirs[i].Location == location {
			dirs[i].Name = name
		}
	}
	return s.pinnedMgr.Save(dirs)
}

// MovePinned moves the pin of location step places up (-1) or down (1).
func (s *Model) MovePinned(location string, step int) error {
	if s.pinnedMgr == nil {
		return errors.New("no pinned list")
	}
	dirs := s.pinnedMgr.Load()
	at := pinnedIndexByLocation(dirs, location)
	if at < 0 {
		return nil
	}
	// movePinnedDirectoryToIndex counts places from 1.
	return s.pinnedMgr.Save(movePinnedDirectoryToIndex(dirs, location, min(max(at+step, 0), len(dirs)-1)+1))
}

// RepointPinned keeps a pin on its path after the path was renamed.
func (s *Model) RepointPinned(from, to, name string) error {
	if s.pinnedMgr == nil {
		return nil
	}
	dirs := s.pinnedMgr.Load()
	changed := false
	for i := range dirs {
		if dirs[i].Location == from {
			dirs[i].Location, changed = to, true
			if dirs[i].Name == filepath.Base(from) {
				dirs[i].Name = name
			}
		}
	}
	if !changed {
		return nil
	}
	return s.pinnedMgr.Save(dirs)
}
