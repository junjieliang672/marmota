package claudesession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// A Tracker reads a transcript once and then only what is written after;
// kept between runs, a start doesn't read every transcript whole again. A
// transcript of a day's work is tens of megabytes, a hundred megabytes a
// second to read, and the first listing of sessions waits for all of them.

// trackerCache is a Tracker's files as kept in a file. Stamp says which
// program wrote it: one that reads a transcript differently mustn't use
// another's summaries.
type trackerCache struct {
	Stamp string                `json:"stamp"`
	Files map[string]cachedFile `json:"files"`
}

// cachedFile is a trackedFile, with the part of its summary that Summary
// keeps to itself.
type cachedFile struct {
	Size          int64     `json:"size"`
	ModTime       time.Time `json:"mod_time"`
	Offset        int64     `json:"offset"`
	Summary       Summary   `json:"summary"`
	PendingToolID string    `json:"pending_tool_id,omitempty"`
	TypedPrompts  int       `json:"typed_prompts,omitempty"`
	PrintPrompts  int       `json:"print_prompts,omitempty"`
}

// Load fills the tracker from the file Save wrote at path, if the program
// stamp wrote it. A transcript that changed since is read on from where
// its summary ends; one that got shorter is read whole again. It reports
// whether anything was loaded.
func (t *Tracker) Load(path, stamp string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var c trackerCache
	//nolint:musttag // Summary's field names are the keys: only the build that wrote the file reads it
	if json.Unmarshal(data, &c) != nil || c.Stamp != stamp || stamp == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for file, f := range c.Files {
		if _, read := t.files[file]; read {
			continue // read in this run already: that is newer
		}
		f.Summary.pendingToolID = f.PendingToolID
		f.Summary.typedPrompts, f.Summary.printPrompts = f.TypedPrompts, f.PrintPrompts
		t.files[file] = &trackedFile{size: f.Size, modTime: f.ModTime, offset: f.Offset, summary: f.Summary}
	}
	return len(c.Files) > 0
}

// Save writes the tracker's files at path for the program stamp, when a
// transcript was read since the last Save and that was at least every ago
// (0: whenever one was read). It reports whether it wrote.
func (t *Tracker) Save(path, stamp string, every time.Duration) (bool, error) {
	t.mu.Lock()
	if !t.changed || stamp == "" || (!t.savedAt.IsZero() && time.Since(t.savedAt) < every) {
		t.mu.Unlock()
		return false, nil
	}
	c := trackerCache{Stamp: stamp, Files: make(map[string]cachedFile, len(t.files))}
	for file, f := range t.files {
		c.Files[file] = cachedFile{Size: f.size, ModTime: f.modTime, Offset: f.offset, Summary: copySummary(f.summary),
			PendingToolID: f.summary.pendingToolID, TypedPrompts: f.summary.typedPrompts,
			PrintPrompts: f.summary.printPrompts}
	}
	t.changed, t.savedAt = false, time.Now()
	t.mu.Unlock()

	data, err := json.Marshal(c) //nolint:musttag // as in Load
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, err
	}
	// Whole or not at all: a file half written would be no cache. Yours
	// alone: what you asked your sessions is in it.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}
