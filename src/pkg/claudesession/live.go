package claudesession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Runner runs an external command and returns its standard output. It is a
// seam so tests can fake `claude` and `ps`.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the command for real.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// flexInt accepts a JSON number, a numeric string, or null.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	s := strings.Trim(string(b), `"`)
	if s == "" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("not an integer: %s", b)
	}
	*f = flexInt(n)
	return nil
}

type liveEntry struct {
	PID        flexInt `json:"pid"`
	Cwd        string  `json:"cwd"`
	Kind       string  `json:"kind"`
	StartedAt  flexInt `json:"startedAt"`
	SessionID  string  `json:"sessionId"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	WaitingFor string  `json:"waitingFor"`
}

// ParseLive parses the output of `claude agents --json`.
func ParseLive(data []byte) ([]Session, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var entries []liveEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse claude agents output: %w", err)
	}
	sessions := make([]Session, 0, len(entries))
	for _, e := range entries {
		if e.SessionID == "" {
			continue
		}
		s := Session{
			ID:         e.SessionID,
			Name:       e.Name,
			Cwd:        e.Cwd,
			Live:       true,
			Background: strings.EqualFold(e.Kind, "background"),
			RawStatus:  e.Status,
			Status:     mapStatus(e.Status),
			WaitingFor: e.WaitingFor,
			PID:        int(e.PID),
		}
		if e.StartedAt > 0 {
			s.StartedAt = time.UnixMilli(int64(e.StartedAt))
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func mapStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "busy", "running", "working", "thinking":
		return StatusWorking
	case "waiting":
		return StatusWaiting
	case "idle", "ready", "done":
		return StatusIdle
	default:
		return StatusUnknown
	}
}

// ProcInfo is what `ps` tells us about a session's process.
type ProcInfo struct {
	TTY     string
	Command string
}

// ParsePS parses `ps -o pid=,tty=,command=` output.
func ParsePS(out []byte) map[int]ProcInfo {
	result := make(map[int]ProcInfo)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		const pidAndTTY = 2
		if len(fields) < pidAndTTY {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		tty := fields[1]
		switch {
		case tty == "??" || tty == "?" || tty == "-":
			tty = ""
		case !strings.HasPrefix(tty, "/dev/"):
			tty = "/dev/" + tty
		}
		result[pid] = ProcInfo{TTY: tty, Command: strings.Join(fields[2:], " ")}
	}
	return result
}

// LookupProcesses asks `ps` for the terminal and command line of each pid.
func LookupProcesses(ctx context.Context, run Runner, pids []int) (map[int]ProcInfo, error) {
	if len(pids) == 0 {
		return map[int]ProcInfo{}, nil
	}
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			ids = append(ids, strconv.Itoa(pid))
		}
	}
	if len(ids) == 0 {
		return map[int]ProcInfo{}, nil
	}
	out, err := run(ctx, "ps", "-o", "pid=,tty=,command=", "-p", strings.Join(ids, ","))
	// ps exits 1 when some pids are gone but still prints the rest.
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return ParsePS(out), nil
}

// commandIsPrintRun reports whether a claude command line is a print-mode
// run (`claude -p`): it answers once and exits, so there is nothing to
// switch to or type into. superfile's own Ask bar starts such runs, and
// Claude Code lists them as interactive sessions while they work.
func commandIsPrintRun(command string) bool {
	for _, f := range strings.Fields(command) {
		switch f {
		case "--":
			return false // the prompt follows
		case "-p", "--print":
			return true
		}
	}
	return false
}

// commandHasRemoteControl reports whether a claude command line enables
// Remote Control.
func commandHasRemoteControl(command string) bool {
	for _, f := range strings.Fields(command) {
		if f == "--remote-control" || strings.HasPrefix(f, "--remote-control=") {
			return true
		}
	}
	return false
}
