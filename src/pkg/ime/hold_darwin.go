//go:build darwin

package ime

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

const (
	holdDirPerm  = 0700
	holdFilePerm = 0600

	// defaultHoldSubPath is relative to the user's home directory. MarmotIM
	// owns this directory; it is named after the reader rather than after
	// superfile because the reader is what has to find it.
	defaultHoldSubPath = "Library/Application Support/MarmotIM/ascii-hold"
)

// terminalBundleIDs maps $TERM_PROGRAM to the bundle identifier the input
// method sees as its client.
//
// The hold is scoped to this app so that a superfile sitting in a background
// tab cannot disable Chinese input in whatever the user switched to. When the
// terminal is not recognised we write no scope at all, which the reader treats
// as "any app" — worse behaviour, but still better than a hold that never
// applies, and the reader has its own setting to refuse unscoped holds.
var terminalBundleIDs = map[string]string{ //nolint:gochecknoglobals // a read-only table
	"WezTerm":        "com.github.wez.wezterm",
	"iTerm.app":      "com.googlecode.iterm2",
	"Apple_Terminal": "com.apple.Terminal",
	"ghostty":        "com.mitchellh.ghostty",
	"vscode":         "com.microsoft.VSCode",
	"Hyper":          "co.zeit.hyper",
	"kitty":          "net.kovidgoyal.kitty",
	"alacritty":      "org.alacritty",
	"Tabby":          "org.tabby",
	"WarpTerminal":   "dev.warp.Warp-Stable",
}

// darwinHold writes the PID file described in the package doc.
//
// Set is called from Bubble Tea's update goroutine on every message, so it does
// no I/O: it records the wanted state and nudges a worker. The worker always
// re-reads the wanted state rather than consuming a queue of transitions, so a
// burst of mode changes collapses into a single write and a stalled filesystem
// can never make the TUI unresponsive.
type darwinHold struct {
	file string
	tmp  string
	body []byte

	mu      sync.Mutex
	desired bool
	closed  bool

	// applied is the state believed to be on disk. Only the worker goroutine
	// touches it.
	applied bool

	wake chan struct{}
	quit chan struct{}
	done chan struct{}

	closeOnce sync.Once
}

func newPlatformHold(dir string) Hold {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Debug("ime: cannot resolve home directory, ascii-hold disabled", "error", err)
			return noopHold{}
		}
		dir = filepath.Join(home, defaultHoldSubPath)
	}

	if err := os.MkdirAll(dir, holdDirPerm); err != nil {
		// The input method is probably not installed. Not an error.
		slog.Debug("ime: ascii-hold directory unusable, hold disabled", "dir", dir, "error", err)
		return noopHold{}
	}

	pid := strconv.Itoa(os.Getpid())
	h := &darwinHold{
		file: filepath.Join(dir, pid),
		tmp:  filepath.Join(dir, "."+pid+".tmp"),
		body: scopeBody(),
		wake: make(chan struct{}, 1),
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}

	// A previous superfile that was SIGKILLed cannot have cleaned up after
	// itself. The reader prunes dead holders too, but it may not be running,
	// and a stale file would then hold the input method in ASCII mode
	// indefinitely with no visible cause.
	pruneDeadHolds(dir)
	_ = os.Remove(h.tmp)

	go h.run()
	return h
}

// scopeBody is the JSON written into the hold file, or nil when the terminal
// could not be identified.
func scopeBody() []byte {
	bundleID, ok := terminalBundleIDs[os.Getenv("TERM_PROGRAM")]
	if !ok {
		slog.Debug( //nolint:gosec // a structured attribute, which slog quotes
			"ime: unrecognised TERM_PROGRAM, writing an unscoped ascii-hold",
			"term_program", os.Getenv("TERM_PROGRAM"))
		return nil
	}
	body, err := json.Marshal(struct {
		Apps []string `json:"apps"`
	}{Apps: []string{bundleID}})
	if err != nil {
		// Unreachable for this shape, but a marshal error must not cost us
		// the hold itself.
		slog.Debug("ime: cannot encode ascii-hold scope", "error", err)
		return nil
	}
	return body
}

func (h *darwinHold) Set(active bool) {
	h.mu.Lock()
	if h.closed || h.desired == active {
		h.mu.Unlock()
		return
	}
	h.desired = active
	h.mu.Unlock()

	// Capacity-1: if a nudge is already outstanding the worker has not yet
	// re-read desired, so dropping this one loses nothing.
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *darwinHold) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		h.closed = true
		h.mu.Unlock()
		close(h.quit)
		// Block until the release has landed. Leaving it to a goroutine
		// would race with process exit and leave the input method stuck.
		<-h.done
	})
}

func (h *darwinHold) run() {
	defer close(h.done)
	for {
		select {
		case <-h.wake:
			h.mu.Lock()
			want := h.desired
			h.mu.Unlock()
			h.apply(want)
		case <-h.quit:
			h.apply(false)
			return
		}
	}
}

func (h *darwinHold) apply(want bool) {
	if want == h.applied {
		return
	}

	var err error
	if want {
		err = h.acquire()
	} else {
		err = h.release()
	}
	if err != nil {
		// Leave applied alone so the next transition retries.
		slog.Debug("ime: cannot update ascii-hold", "want", want, "error", err)
		return
	}
	h.applied = want
}

// acquire publishes the hold file atomically. A plain create would let the
// reader observe the file after creat() but before write(), read an empty body,
// and widen a scoped hold to every app.
func (h *darwinHold) acquire() error {
	if err := os.WriteFile(h.tmp, h.body, holdFilePerm); err != nil {
		return err
	}
	if err := os.Rename(h.tmp, h.file); err != nil {
		_ = os.Remove(h.tmp)
		return err
	}
	return nil
}

func (h *darwinHold) release() error {
	if err := os.Remove(h.file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// pruneDeadHolds removes hold files belonging to processes that are gone.
//
// Entries whose name is not exactly a positive decimal PID are left alone: the
// directory is shared, and deleting something we cannot interpret is never our
// call.
func pruneDeadHolds(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("ime: cannot scan ascii-hold directory", "dir", dir, "error", err)
		return
	}
	for _, entry := range entries {
		pid, ok := parseHoldPID(entry.Name())
		if !ok || processAlive(pid) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			slog.Debug("ime: cannot remove stale ascii-hold", "name", entry.Name(), "error", err)
			continue
		}
		slog.Debug("ime: removed ascii-hold of an exited process", "pid", pid)
	}
}

// parseHoldPID accepts only a bare positive decimal, so neither dotfiles nor a
// client's in-flight ".<pid>.tmp" is ever mistaken for a hold.
func parseHoldPID(name string) (int, bool) {
	pid, err := strconv.Atoi(name)
	if err != nil || pid <= 0 || strconv.Itoa(pid) != name {
		return 0, false
	}
	return pid, true
}

// processAlive reports whether pid names a running process. EPERM means the
// process exists but belongs to someone else, which still counts as alive;
// only ESRCH means it is gone.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
