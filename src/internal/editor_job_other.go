//go:build !unix

package internal

import (
	"errors"
	"io"
)

// editorJob is an editor Marmota can take the terminal back from while it
// still runs, which it can only where programs stop and continue: see
// editor_job_unix.go. Elsewhere the editor runs until it ends.
type editorJob struct {
	name string
	args []string
	env  []string

	pid        int
	stopped    bool
	noTerminal bool
}

func canPutEditorsAway() bool { return false }

func (j *editorJob) SetStdin(io.Reader)  {}
func (j *editorJob) SetStdout(io.Writer) {}
func (j *editorJob) SetStderr(io.Writer) {}

func (j *editorJob) Run() error {
	return errors.New("an editor can't be put away on this system")
}
