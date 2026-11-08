package claudesession

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// ErrTranscriptBusy means Claude was writing to the transcript, so a rename
// wasn't appended; trying again a moment later works.
var ErrTranscriptBusy = errors.New("the session is writing to its transcript; try again")

// customTitleRetries and customTitleWait bound how long SetCustomTitle waits
// for Claude to finish a half-written line.
const (
	customTitleRetries = 10
	customTitleWait    = 50 * time.Millisecond
)

// SetCustomTitle renames a session the way Claude Code's own rename does: it
// appends a custom-title record to the session's transcript, and the newest
// such record is the session's name. An empty title clears the name, and
// Claude's own title shows again.
func SetCustomTitle(transcriptPath, sessionID, title string) error {
	line, err := json.Marshal(struct {
		Type        string `json:"type"`
		CustomTitle string `json:"customTitle"`
		SessionID   string `json:"sessionId"`
	}{recordCustomTitle, title, sessionID})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(transcriptPath, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	// Never append to a line Claude is still writing.
	for attempt := 0; ; attempt++ {
		complete, checkErr := endsWithNewline(f)
		if checkErr != nil {
			return checkErr
		}
		if complete {
			break
		}
		if attempt >= customTitleRetries {
			return ErrTranscriptBusy
		}
		time.Sleep(customTitleWait)
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func endsWithNewline(f *os.File) (bool, error) {
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return true, nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return last[0] == '\n', nil
}
