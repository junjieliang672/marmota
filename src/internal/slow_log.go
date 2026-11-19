package internal

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// slowLog is where an update or a drawing of the screen that took longer
// than slowLimit is written, when MARMOTA_SLOW_LOG names a file: what keeps
// the screen from following the keys and the wheel.
var (
	slowLogOnce sync.Once              //nolint:gochecknoglobals // opened once
	slowLogFile *os.File               //nolint:gochecknoglobals // opened once
	slowLimit   = 8 * time.Millisecond //nolint:gochecknoglobals // about half a frame
)

func slowLogWriter() *os.File {
	slowLogOnce.Do(func() {
		if ms, err := strconv.Atoi(os.Getenv("MARMOTA_SLOW_MS")); err == nil {
			slowLimit = time.Duration(ms) * time.Millisecond // 0: everything
		}
		if path := os.Getenv("MARMOTA_SLOW_LOG"); path != "" {
			slowLogFile, _ = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		}
	})
	return slowLogFile
}

// noteSlow writes what took long, if anything did and the log is on.
func noteSlow(what string, msg any, start time.Time) {
	f := slowLogWriter()
	took := time.Since(start)
	if f == nil || took < slowLimit {
		return
	}
	{
		name := fmt.Sprint(reflect.TypeOf(msg))
		if k, ok := msg.(tea.KeyMsg); ok {
			name += " " + k.String()
		}
		fmt.Fprintf(f, "%s %s %s %s\n", time.Now().Format("15:04:05.000"), what, name, took)
	}
}
