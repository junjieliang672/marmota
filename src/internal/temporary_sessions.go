package internal

// Temporary sessions: started by a program to answer its own questions, as
// Marmota's plugin for micro starts one for a question asked from the editor
// (`y session start --temporary`). Marmota's Sessions box, its lists of
// ended sessions and `y session list` leave them out, running or ended, and
// each closes when the program that started it does. They are written to,
// read and shown by their ids as any session is (`y session ask`, `show`).

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// temporaryKept is how long a temporary session is remembered: past it, its
// conversation is too old for a list of ended sessions to show anyway.
const temporaryKept = 60 * 24 * time.Hour

// temporaryWindow starts the name of a temporary Codex session's window
// until its id is known (see tmuxWindowName): Marmota, which lists the
// sessions Codex's server has, knows by it not to list this one meanwhile.
const temporaryWindow = unnamedWindow + "tmp-"

// temporaryRead is the record as last read, and the file's time then.
//
//nolint:gochecknoglobals // a cache of a file
var temporaryRead struct {
	mu       sync.Mutex
	file     string
	modified time.Time
	sessions map[string]time.Time
}

// temporarySessions are the temporary sessions on record, with when each
// was started. The file is read again only when it has changed.
func temporarySessions() map[string]time.Time {
	r := &temporaryRead
	r.mu.Lock()
	defer r.mu.Unlock()
	file := variable.TemporarySessionsFile
	info, err := os.Stat(file)
	if err != nil {
		r.sessions, r.file = nil, ""
		return nil
	}
	if r.sessions != nil && r.file == file && info.ModTime().Equal(r.modified) {
		return r.sessions
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	sessions := map[string]time.Time{}
	if err := json.Unmarshal(data, &sessions); err != nil {
		slog.Warn("Ignoring an unreadable record of temporary sessions", "file", file, "error", err)
	}
	r.sessions, r.file, r.modified = sessions, file, info.ModTime()
	return sessions
}

// isTemporarySession reports a session on the record.
func isTemporarySession(id string) bool {
	_, ok := temporarySessions()[id]
	return ok
}

// rememberTemporarySession puts a session on the record, and takes off it
// those too old to matter.
func rememberTemporarySession(id string) error {
	sessions := map[string]time.Time{}
	for known, at := range temporarySessions() {
		if time.Since(at) < temporaryKept {
			sessions[known] = at
		}
	}
	sessions[id] = time.Now()
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(variable.TemporarySessionsFile), 0o755); err != nil {
		return err
	}
	tmp := variable.TemporarySessionsFile + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, variable.TemporarySessionsFile)
}

// withoutTemporary leaves the temporary sessions out of a list.
func withoutTemporary(sessions []claudesession.Session) []claudesession.Session {
	temporary := temporarySessions()
	if len(temporary) == 0 {
		return sessions
	}
	return slices.DeleteFunc(sessions, func(s claudesession.Session) bool {
		_, ok := temporary[s.ID]
		return ok
	})
}

// closesWith is the start of a session's command that has it close when the
// process owner has gone: a watcher beside the session, which goes on as
// the window's own program, so its end closes the window. "" for no owner.
func closesWith(owner int) string {
	if owner <= 1 {
		return ""
	}
	pid := strconv.Itoa(owner)
	return `(while kill -0 ` + pid + ` 2>/dev/null; do sleep 2; done; kill -TERM $$ 2>/dev/null) >/dev/null 2>&1 & exec `
}
