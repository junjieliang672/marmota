package claudesession

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Claude Code backs up a file before a session's Edit or Write tools first
// change it: <config>/file-history/<session>/<hash>@v1 is the file as it was,
// and later versions follow. The transcript's file-history-snapshot records
// say which hash is which file (from @v2 on: v1 itself is never named). A
// file the session created has no @v1.
//
// The snapshot naming a file comes at the start of the next turn, so a file
// first changed in the current turn is found by PathHash instead.

// BackupHashes maps each file a session's transcript backs up, as an
// absolute path, to the hash its backups are named by. Paths the transcript
// gives relative are taken from cwd.
func BackupHashes(transcriptPath, cwd string) (map[string]string, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hashes := make(map[string]string)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20) //nolint:mnd // transcript lines can be long
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"file-history-snapshot"`) {
			continue
		}
		var r struct {
			Snapshot struct {
				TrackedFileBackups map[string]struct {
					BackupFileName *string `json:"backupFileName"`
				} `json:"trackedFileBackups"`
			} `json:"snapshot"`
		}
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		for path, b := range r.Snapshot.TrackedFileBackups {
			if b.BackupFileName == nil {
				continue
			}
			hash, _, _ := strings.Cut(*b.BackupFileName, "@")
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			if _, ok := hashes[path]; !ok {
				hashes[path] = hash
			}
		}
	}
	return hashes, sc.Err()
}

// PathHash is the hash Claude Code names a file's backups by: the first 16
// hex digits of the sha256 of its absolute path.
func PathHash(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// OriginalFile is the backup of a file as it was before the session first
// changed it, and whether there is one: none when the session created it.
func OriginalFile(configDir, sessionID, hash string) (string, bool) {
	path := filepath.Join(configDir, "file-history", sessionID, hash+"@v1")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, true
	}
	return "", false
}

// Hunk is one change a shell command made to a file, as Claude Code
// records it: where it is in the file before and after, and its diff lines
// (" " kept, "-" removed, "+" added).
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []string
	At                 time.Time
}

// ShellHunks maps each file a session's shell commands changed to the
// changes, in the order they were made. Claude Code records them under
// bashEditDiff in the command's result; files changed with the Edit and
// Write tools have backups instead (see BackupHashes).
func ShellHunks(transcriptPath string) (map[string][]Hunk, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string][]Hunk)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20) //nolint:mnd // transcript lines can be long
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"bashEditDiff"`) {
			continue
		}
		var r struct {
			Timestamp     string `json:"timestamp"`
			ToolUseResult struct {
				BashEditDiff struct {
					Files []struct {
						FilePath string `json:"filePath"`
						Hunks    []struct {
							OldStart int      `json:"oldStart"`
							OldLines int      `json:"oldLines"`
							NewStart int      `json:"newStart"`
							NewLines int      `json:"newLines"`
							Lines    []string `json:"lines"`
						} `json:"hunks"`
					} `json:"files"`
				} `json:"bashEditDiff"`
			} `json:"toolUseResult"`
		}
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		at := parseTime(r.Timestamp)
		for _, file := range r.ToolUseResult.BashEditDiff.Files {
			for _, h := range file.Hunks {
				out[file.FilePath] = append(out[file.FilePath], Hunk{OldStart: h.OldStart, OldLines: h.OldLines,
					NewStart: h.NewStart, NewLines: h.NewLines, Lines: h.Lines, At: at})
			}
		}
	}
	return out, sc.Err()
}
