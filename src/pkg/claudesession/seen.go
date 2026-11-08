package claudesession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SeenStore remembers when you last looked at each session, so a session
// with a newer finished turn can be shown as unread.
type SeenStore struct {
	mu    sync.Mutex
	path  string
	seen  map[string]int64 // unix milliseconds
	dirty bool
}

// LoadSeen reads the store at path. A missing or corrupt file starts empty.
func LoadSeen(path string) *SeenStore {
	s := &SeenStore{path: path, seen: make(map[string]int64)}
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &s.seen)
		if s.seen == nil {
			s.seen = make(map[string]int64)
		}
	}
	return s
}

// Unread reports whether the session has an update newer than the last time
// you looked. The first time a session is observed, its current state counts
// as seen, so nothing is flagged just because superfile started.
func (s *SeenStore) Unread(id string, update time.Time) bool {
	if id == "" || update.IsZero() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.seen[id]
	if !ok {
		s.seen[id] = update.UnixMilli()
		s.dirty = true
		return false
	}
	return update.UnixMilli() > last
}

// MarkSeen records that you looked at the session at time t.
func (s *SeenStore) MarkSeen(id string, t time.Time) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ms := t.UnixMilli(); ms > s.seen[id] {
		s.seen[id] = ms
		s.dirty = true
	}
}

// Forget drops sessions not in keep, so the file doesn't grow forever.
func (s *SeenStore) Forget(keep map[string]bool, olderThan time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := olderThan.UnixMilli()
	for id, ms := range s.seen {
		if !keep[id] && ms < cutoff {
			delete(s.seen, id)
			s.dirty = true
		}
	}
}

// Save writes the store if anything changed.
func (s *SeenStore) Save() error {
	s.mu.Lock()
	if !s.dirty || s.path == "" {
		s.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(s.seen)
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
