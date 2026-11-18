package internal

// `y session pick`: the sessions in fzf, to pick one, with each row in its
// colors (see session_rows.go). It prints the row picked: the session's id,
// agent, host and title, tab-separated.
//
// fzf runs on a terminal of y's own, and what it draws is copied to the real
// one with its color codes rewritten: fzf starts each with an empty first
// parameter, "ESC[;38;5;209m", which the terminal pane micro runs programs
// in ignores, colors and all (measured on micro 2.0.15 and fzf 0.65), while
// it draws "ESC[0;38;5;209m". That pane is where the editor's picker runs.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// pickCanceled is fzf's exit status when nothing was picked: esc, ctrl+C.
const pickCanceled = 130

// pickDrawnWait is how long the last of what fzf drew is waited for once
// it has ended.
const pickDrawnWait = 500 * time.Millisecond

// stderrFD is where the picker's terminal is given to fzf, which makes it
// its controlling terminal.
const stderrFD = 2

// ErrNothingPicked says the picker was left without a pick.
var ErrNothingPicked = errors.New("nothing picked")

// SessionPick shows the sessions in fzf on the terminal y runs in and
// returns the row picked. With newIn, a folder, the first rows stand for a
// session not started yet, one for each agent.
func SessionPick(newIn, prompt string) (string, error) {
	fzf, err := exec.LookPath("fzf")
	if err != nil {
		return "", errors.New("fzf isn't installed: brew install fzf")
	}
	sessions, _ := SessionList()
	var rows bytes.Buffer
	if err := WriteSessionRows(&rows, sessions, newIn); err != nil {
		return "", err
	}
	screen, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("the picker needs a terminal: %w", err)
	}
	defer screen.Close()

	own, tty, err := pty.Open()
	if err != nil {
		return "", err
	}
	defer own.Close()
	_ = pty.InheritSize(screen, own)

	var picked bytes.Buffer
	cmd := exec.Command(fzf, "--ansi", "--delimiter=\t", "--with-nth=5", "--layout=reverse", "--no-multi",
		"--prompt="+prompt)
	// 256 colors by number, which micro's pane draws: told of 24-bit colors,
	// as WezTerm says of itself, fzf would draw in those, which it doesn't.
	cmd.Env = append(withoutEnv(os.Environ(), "COLORTERM", "TERM"), "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = &rows, &picked, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: stderrFD}
	if err := cmd.Start(); err != nil {
		tty.Close()
		return "", err
	}
	tty.Close()

	// The real terminal hands every key on as it is typed, and is put back
	// as it was after.
	if state, err := term.MakeRaw(int(screen.Fd())); err == nil {
		defer func() { _ = term.Restore(int(screen.Fd()), state) }()
	}
	resized := make(chan os.Signal, 1)
	signal.Notify(resized, syscall.SIGWINCH)
	defer signal.Stop(resized)
	go func() {
		for range resized {
			_ = pty.InheritSize(screen, own)
		}
	}()
	go func() { _, _ = io.Copy(own, screen) }() // keys and the mouse, to fzf
	drawn := make(chan struct{})
	go func() {
		defer close(drawn)
		copyDrawn(screen, own)
	}()

	err = cmd.Wait()
	// What fzf drew last puts the screen back as it was: it is read to its
	// end, which comes once fzf has closed its side.
	select {
	case <-drawn:
	case <-time.After(pickDrawnWait):
		own.Close()
		<-drawn
	}
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && (exit.ExitCode() == pickCanceled || exit.ExitCode() == 1):
		return "", ErrNothingPicked
	case err != nil:
		return "", err
	}
	row := strings.TrimRight(picked.String(), "\r\n")
	if row == "" {
		return "", ErrNothingPicked
	}
	return row, nil
}

// withoutEnv is env without the variables named.
func withoutEnv(env []string, names ...string) []string {
	kept := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			drop = drop || n == name
		}
		if !drop {
			kept = append(kept, kv)
		}
	}
	return kept
}

// copyDrawn copies what a program draws to the terminal, its color codes
// rewritten (see rewriteColors), until the program's side closes.
func copyDrawn(to io.Writer, from io.Reader) {
	var held []byte // the start of an escape sequence cut by a read
	buf := make([]byte, 32*1024)
	for {
		n, err := from.Read(buf)
		if n > 0 {
			var out []byte
			out, held = rewriteColors(append(held, buf[:n]...))
			if _, werr := to.Write(out); werr != nil {
				return
			}
		}
		if err != nil {
			_, _ = to.Write(held)
			return
		}
	}
}

// rewriteColors gives each color code's empty parameters their value, 0:
// "ESC[;1;38;5;110m" becomes "ESC[0;1;38;5;110m", which means the same and
// is drawn by terminals that ignore the first. Other sequences pass as they
// are. An escape sequence cut by the end of data is returned as rest, for
// the next call to finish.
func rewriteColors(data []byte) (out, rest []byte) {
	out = make([]byte, 0, len(data)+16)
	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			out = append(out, data[i])
			i++
			continue
		}
		if i+1 >= len(data) {
			return out, append([]byte(nil), data[i:]...)
		}
		if data[i+1] != '[' {
			out = append(out, data[i])
			i++
			continue
		}
		end := i + 2
		for end < len(data) && (data[end] < 0x40 || data[end] > 0x7e) {
			end++
		}
		if end >= len(data) {
			return out, append([]byte(nil), data[i:]...)
		}
		params := data[i+2 : end]
		empty := len(params) == 0 || params[0] == ';' || params[len(params)-1] == ';' ||
			bytes.Contains(params, []byte(";;"))
		if data[end] != 'm' || !empty {
			out = append(out, data[i:end+1]...)
			i = end + 1
			continue
		}
		out = append(out, 0x1b, '[')
		for n, p := range bytes.Split(params, []byte{';'}) {
			if n > 0 {
				out = append(out, ';')
			}
			if len(p) == 0 {
				p = []byte{'0'}
			}
			out = append(out, p...)
		}
		out = append(out, 'm')
		i = end + 1
	}
	return out, nil
}
