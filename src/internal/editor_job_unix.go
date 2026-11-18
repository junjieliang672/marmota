//go:build unix

package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// editorJob runs an editor in Marmota's place as the terminal's foreground
// job, in a process group of its own, the way a shell with job control runs a
// program. Marmota takes the terminal back when the editor ends, and also
// when it stops itself, as micro does on cmd+M and vim on ctrl+z: the editor
// is then put away, its files and what it shows kept as they are, and Run
// called again continues it.
//
// A terminal asked what its pane runs says the foreground job: the editor
// while it runs, Marmota while it is put away. That is how WezTerm knows
// when to send micro the Mac's keys (the keys block in ~/.wezterm.lua).
type editorJob struct {
	name string
	args []string
	env  []string

	stdin          io.Reader
	stdout, stderr io.Writer

	// pid is the editor's process, which leads its process group; 0 until
	// it has started.
	pid int
	// stopped is set by Run when the editor stopped instead of ending.
	stopped bool
	// noTerminal leaves the terminal alone, for tests, which have none.
	noTerminal bool
}

// canPutEditorsAway reports whether an editor in Marmota's place runs as a
// job Marmota can take the terminal back from: with a terminal to hand over.
func canPutEditorsAway() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func (j *editorJob) SetStdin(r io.Reader)  { j.stdin = r }
func (j *editorJob) SetStdout(w io.Writer) { j.stdout = w }
func (j *editorJob) SetStderr(w io.Writer) { j.stderr = w }

// Run starts the editor, or continues it when it was put away, and waits
// until it ends or stops. It is nil when the editor stopped, with stopped
// set, or ended well.
func (j *editorJob) Run() error {
	tty := j.terminal()
	if j.pid == 0 {
		if err := j.start(tty); err != nil {
			return err
		}
	} else {
		j.proceed(tty)
	}
	return j.wait(tty)
}

// terminal is the terminal to hand to the editor: the one Bubble Tea reads.
func (j *editorJob) terminal() *os.File {
	if j.noTerminal {
		return nil
	}
	if f, ok := j.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return f
	}
	return nil
}

func (j *editorJob) start(tty *os.File) error {
	cmd := exec.Command(j.name, j.args...) //nolint:gosec // the editor you configured, on the files you chose
	cmd.Env = j.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = j.stdin, j.stdout, j.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if tty != nil {
		// The child makes its group the terminal's foreground one before
		// the editor's program replaces it.
		cmd.SysProcAttr.Foreground = true
		cmd.SysProcAttr.Ctty = int(tty.Fd())
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	j.pid = cmd.Process.Pid
	// The process is waited for by its number, since a wait through cmd
	// doesn't say that it stopped.
	_ = cmd.Process.Release()
	return nil
}

// proceed hands the terminal back to an editor that was put away and
// continues it.
func (j *editorJob) proceed(tty *os.File) {
	j.stopped = false
	setForeground(tty, j.pid)
	// One that has ended meanwhile is found by the wait.
	_ = unix.Kill(-j.pid, unix.SIGCONT)
}

func (j *editorJob) wait(tty *os.File) error {
	var status unix.WaitStatus
	for {
		_, err := unix.Wait4(j.pid, &status, unix.WUNTRACED, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		setForeground(tty, unix.Getpgrp())
		if err != nil {
			return fmt.Errorf("waiting for the editor: %w", err)
		}
		break
	}
	switch {
	// Continued too: on a Mac, Go reads a process stopped by SIGSTOP, which
	// is how micro stops itself, as one that continued. A wait that doesn't
	// ask for those that continued is told of none.
	case status.Stopped() || status.Continued():
		j.stopped = true
	case status.Signaled():
		return fmt.Errorf("the editor was ended by signal %d", status.Signal())
	case status.ExitStatus() != 0:
		return fmt.Errorf("the editor exited with status %d", status.ExitStatus())
	}
	return nil
}

// setForeground makes the process group pgid the terminal's foreground one.
// A group in the background, as Marmota's is while the editor has the
// terminal, is stopped for doing that unless it ignores the signal meant to
// stop it.
func setForeground(tty *os.File, pgid int) {
	if tty == nil {
		return
	}
	signal.Ignore(syscall.SIGTTOU)
	defer signal.Reset(syscall.SIGTTOU)
	// It fails when the group has gone, as an editor's that ended while it
	// was put away: the wait that follows finds that, and sets Marmota's.
	_ = unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSPGRP, pgid)
}
