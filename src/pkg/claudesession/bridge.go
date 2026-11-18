package claudesession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// BridgeIDs maps each Claude Code session running on this computer with
// Remote Control on to its claude.ai id (session_…), as Claude Code writes
// them in sessions/<pid>.json beside projects.
func BridgeIDs(projectsDir string) map[string]string {
	out := map[string]string{}
	dir := filepath.Join(filepath.Dir(projectsDir), "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s struct {
			SessionID       string `json:"sessionId"`
			BridgeSessionID string `json:"bridgeSessionId"`
		}
		if json.Unmarshal(data, &s) == nil && s.SessionID != "" && s.BridgeSessionID != "" {
			out[s.SessionID] = s.BridgeSessionID
		}
	}
	return out
}

// LinkID is the claude.ai id in a session's Remote Control link
// (https://claude.ai/code/session_…), or "".
func LinkID(remoteURL string) string {
	if i := strings.LastIndex(remoteURL, "/"); i >= 0 && strings.HasPrefix(remoteURL[i+1:], "session_") {
		return remoteURL[i+1:]
	}
	return ""
}
