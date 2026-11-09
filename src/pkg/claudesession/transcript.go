package claudesession

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Summary is what superfile needs from one transcript file.
type Summary struct {
	SessionID string
	// Title is Claude's generated title; CustomTitle is a name the person
	// gave the session with Claude Code's rename.
	Title          string
	CustomTitle    string
	Cwd            string
	LastPrompt     string
	AwaySummary    string
	RemoteControl  bool
	RemoteURL      string
	PermissionMode string
	// Model and Effort are what Claude last answered with: the model's id
	// ("claude-opus-5-5") and the effort level ("medium"), "" when unknown.
	Model, Effort string
	LastActivity  time.Time
	LastTurnEnd   time.Time
	EditedFiles   []string
	// Todos is the latest task list Claude wrote with TodoWrite.
	Todos []Todo
	// PendingTool is the tool call Claude made last that has no result yet,
	// such as "Bash(go test ./...)", or the question it asks you.
	PendingTool string
	// LastError is the API error Claude's last reply was, such as "Login
	// expired · Please run /login"; "" once it replies again.
	LastError string
	// AgentName is the name Claude Code addresses the session by, once it
	// was renamed.
	AgentName string
	// PrintOnly reports a session only `claude -p` ever ran, such as a
	// question asked from superfile's Ask.
	PrintOnly bool

	pendingToolID string
	// typedPrompts and printPrompts count the prompts typed in a session
	// and those sent with `claude -p`.
	typedPrompts, printPrompts int
}

const (
	maxEditedFiles = 500 // W lists them all
	maxPromptRunes = 600
	maxToolRunes   = 120
)

// Transcript record and content block types.
const (
	recordUser        = "user"
	recordCustomTitle = "custom-title"
	recordAssistant   = "assistant"
	blockText         = "text"
	blockToolUse      = "tool_use"
	blockToolResult   = "tool_result"
)

// todoWriteTool replaces the session's task list with its todos input.
const todoWriteTool = "TodoWrite"

// editTools are the tools whose file_path input means Claude changed a file.
var editTools = map[string]string{ //nolint:gochecknoglobals // lookup table
	"Edit":         "file_path",
	"MultiEdit":    "file_path",
	"Write":        "file_path",
	"NotebookEdit": "notebook_path",
}

type record struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	Timestamp       string          `json:"timestamp"`
	Cwd             string          `json:"cwd"`
	SessionID       string          `json:"sessionId"`
	AiTitle         string          `json:"aiTitle"`
	CustomTitle     *string         `json:"customTitle"`
	LastPrompt      string          `json:"lastPrompt"`
	Content         json.RawMessage `json:"content"`
	URL             string          `json:"url"`
	PermissionMode  string          `json:"permissionMode"`
	Effort          string          `json:"effort"`
	BridgeSessionID string          `json:"bridgeSessionId"`
	IsSidechain     bool            `json:"isSidechain"`
	IsMeta          bool            `json:"isMeta"`
	IsAPIError      bool            `json:"isApiErrorMessage"`
	Entrypoint      string          `json:"entrypoint"`
	AgentName       string          `json:"agentName"`
	RelocatedCwd    string          `json:"relocatedCwd"`
	// ToolUseResult is a tool's structured result. It is an object for some
	// tools and plain text for others, so it is decoded only when needed.
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	Message       *struct {
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type      string                     `json:"type"`
	Text      string                     `json:"text"`
	ID        string                     `json:"id"`
	ToolUseID string                     `json:"tool_use_id"`
	Name      string                     `json:"name"`
	Input     map[string]json.RawMessage `json:"input"`
}

func (s *Summary) apply(r record) {
	if s.SessionID == "" && r.SessionID != "" {
		s.SessionID = r.SessionID
	}
	if s.Cwd == "" && r.Cwd != "" {
		s.Cwd = r.Cwd
	}
	ts := parseTime(r.Timestamp)
	if ts.After(s.LastActivity) {
		s.LastActivity = ts
	}

	switch r.Type {
	case "ai-title":
		if t := singleLine(r.AiTitle); t != "" {
			s.Title = t
		}
	case recordCustomTitle:
		if r.CustomTitle != nil {
			s.CustomTitle = singleLine(*r.CustomTitle) // empty clears a name
		}
	case "agent-name":
		s.AgentName = singleLine(r.AgentName)
	case "relocated":
		// Moved with /cd: it works in the new folder from now on.
		if r.RelocatedCwd != "" {
			s.Cwd = r.RelocatedCwd
		}
	case "last-prompt":
		s.setPrompt(singleLine(r.LastPrompt))
	case "bridge-session":
		s.RemoteControl = true
	case "permission-mode":
		if r.PermissionMode != "" {
			s.PermissionMode = r.PermissionMode
		}
	case "system":
		s.applySystem(r, ts)
	case recordAssistant, recordUser:
		s.applyMessage(r)
	}
}

func (s *Summary) setPrompt(p string) {
	if p = singleLine(StripContext(p)); p != "" {
		s.LastPrompt = truncateRunes(p, maxPromptRunes)
	}
}

// ContextOpenTag and ContextCloseTag wrap the context block Marmota adds to
// the requests it sends (package claudeask writes it).
const (
	ContextOpenTag  = "<marmota-context>"
	ContextCloseTag = "</marmota-context>"
)

// The tags Marmota wrote before it was renamed: transcripts from then still
// hold them.
const (
	oldContextOpenTag  = "<superfile-context>"
	oldContextCloseTag = "</superfile-context>"
)

// StripContext leaves out the context blocks Marmota added to a prompt, so
// a prompt reads as what the person typed.
func StripContext(text string) string {
	return stripBlocks(stripBlocks(text, ContextOpenTag, ContextCloseTag), oldContextOpenTag, oldContextCloseTag)
}

func stripBlocks(text, open, closing string) string {
	for {
		start := strings.Index(text, open)
		if start < 0 {
			return text
		}
		end := strings.Index(text[start:], closing)
		if end < 0 {
			return text[:start]
		}
		text = text[:start] + text[start+end+len(closing):]
	}
}

// applyMessage handles conversation records. Sidechain records belong to
// subagents and meta records are plumbing, so both are skipped.
func (s *Summary) applyMessage(r record) {
	if r.IsSidechain || r.Message == nil {
		return
	}
	if r.Type == recordAssistant {
		s.applyAssistant(r)
		return
	}
	s.applyToolResult(r.ToolUseResult)
	s.settlePendingTool(r.Message.Content)
	if r.IsMeta {
		return
	}
	prompt := userPromptText(r.Message.Content)
	s.setPrompt(prompt)
	if prompt == "" {
		return
	}
	if r.Entrypoint == printEntrypoint {
		s.printPrompts++
	} else {
		s.typedPrompts++
	}
	s.PrintOnly = s.printPrompts > 0 && s.typedPrompts == 0
}

// printEntrypoint marks the records of a `claude -p` run.
const printEntrypoint = "sdk-cli"

// settlePendingTool forgets the pending tool call once a user record carries
// its result, or once you write a prompt instead.
func (s *Summary) settlePendingTool(raw json.RawMessage) {
	if s.pendingToolID == "" {
		return
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		s.PendingTool, s.pendingToolID = "", "" // a typed prompt
		return
	}
	for _, b := range blocks {
		if b.Type == blockToolResult && b.ToolUseID == s.pendingToolID {
			s.PendingTool, s.pendingToolID = "", ""
			return
		}
	}
}

// applyToolResult records the files a shell command changed: Claude Code
// lists them under bashEditDiff when Bash writes files.
func (s *Summary) applyToolResult(raw json.RawMessage) {
	if len(raw) == 0 || raw[0] != '{' {
		return
	}
	var result struct {
		BashEditDiff *struct {
			ChangedFiles []string `json:"changedFiles"`
		} `json:"bashEditDiff"`
	}
	if json.Unmarshal(raw, &result) != nil || result.BashEditDiff == nil {
		return
	}
	for _, path := range result.BashEditDiff.ChangedFiles {
		if path != "" {
			s.addEditedFile(path)
		}
	}
}

func (s *Summary) applySystem(r record, ts time.Time) {
	switch r.Subtype {
	case "turn_duration":
		if ts.After(s.LastTurnEnd) {
			s.LastTurnEnd = ts
		}
	case "bridge_status":
		s.RemoteControl = true
		if r.URL != "" {
			s.RemoteURL = r.URL
		}
	case "away_summary":
		var text string
		if json.Unmarshal(r.Content, &text) == nil && strings.TrimSpace(text) != "" {
			s.AwaySummary = strings.TrimSpace(text)
		}
	}
}

func (s *Summary) applyAssistant(r record) {
	var blocks []contentBlock
	if json.Unmarshal(r.Message.Content, &blocks) != nil {
		return
	}
	s.LastError = ""
	if r.IsAPIError {
		s.LastError = firstText(blocks)
	} else if model := r.Message.Model; model != "" && !strings.HasPrefix(model, "<") {
		s.Model = model // not Claude Code's own "<synthetic>" messages
		if r.Effort != "" {
			s.Effort = r.Effort
		}
	}
	for _, b := range blocks {
		if b.Type != blockToolUse {
			continue
		}
		if b.ID != "" {
			s.PendingTool, s.pendingToolID = describeToolCall(b), b.ID
		}
		if b.Name == todoWriteTool {
			var todos []Todo
			if json.Unmarshal(b.Input["todos"], &todos) == nil {
				s.Todos = todos
			}
			continue
		}
		key, ok := editTools[b.Name]
		if !ok {
			continue
		}
		var path string
		if json.Unmarshal(b.Input[key], &path) == nil && path != "" {
			s.addEditedFile(path)
		}
	}
}

// firstText is a message's first text, on one line.
func firstText(blocks []contentBlock) string {
	for _, b := range blocks {
		if b.Type == blockText && strings.TrimSpace(b.Text) != "" {
			return truncateRunes(singleLine(b.Text), maxToolRunes)
		}
	}
	return ""
}

// describeToolCall says what a tool call does, as Claude Code shows it:
// "Bash(go test ./...)", "Edit(render.go)", or the question it asks.
func describeToolCall(b contentBlock) string {
	str := func(key string) string {
		var v string
		_ = json.Unmarshal(b.Input[key], &v)
		return singleLine(v)
	}
	var detail string
	switch b.Name {
	case "AskUserQuestion":
		var questions []struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(b.Input["questions"], &questions) == nil && len(questions) > 0 {
			return truncateRunes(singleLine(questions[0].Question), maxToolRunes)
		}
	case "ExitPlanMode":
		return "approve the plan"
	case "Bash":
		detail = str("command")
	case "Read", "Edit", "MultiEdit", "Write":
		detail = filepath.Base(str("file_path"))
	case "NotebookEdit":
		detail = filepath.Base(str("notebook_path"))
	case "WebFetch":
		detail = str("url")
	case "Glob", "Grep":
		detail = str("pattern")
	}
	if detail == "" || detail == "." {
		return b.Name
	}
	return truncateRunes(b.Name+"("+detail+")", maxToolRunes)
}

func (s *Summary) addEditedFile(path string) {
	for i, p := range s.EditedFiles {
		if p == path {
			s.EditedFiles = append(s.EditedFiles[:i], s.EditedFiles[i+1:]...)
			break
		}
	}
	s.EditedFiles = append(s.EditedFiles, path)
	if len(s.EditedFiles) > maxEditedFiles {
		s.EditedFiles = s.EditedFiles[len(s.EditedFiles)-maxEditedFiles:]
	}
}

// userPromptText returns the text a person typed, or "" for tool results and
// command plumbing.
func userPromptText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return promptOrEmpty(text)
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type != blockText {
			return "" // tool results and images are not prompts
		}
		parts = append(parts, b.Text)
	}
	return promptOrEmpty(strings.Join(parts, " "))
}

// pastedTag is how Claude Code marks pasted text in a prompt it records:
// <pasted_content id="7bde"> … </pasted_content id="7bde">.
var pastedTag = regexp.MustCompile(`</?pasted_content(?:\s+id="[^"]*")?>`)

func promptOrEmpty(text string) string {
	text = singleLine(StripContext(pastedTag.ReplaceAllString(text, "")))
	if text == "" || strings.HasPrefix(text, "<") {
		// <command-name>, <local-command-stdout>, <task-notification> …
		return ""
	}
	return text
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Tracker summarises transcript files incrementally: a file is parsed once,
// then only the bytes appended since the last call are read.
type Tracker struct {
	mu    sync.Mutex
	files map[string]*trackedFile
}

type trackedFile struct {
	size    int64
	modTime time.Time
	offset  int64
	summary Summary
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{files: make(map[string]*trackedFile)}
}

// Summary returns the up-to-date summary of the transcript at path.
func (t *Tracker) Summary(path string) (Summary, error) {
	info, err := os.Stat(path)
	if err != nil {
		t.mu.Lock()
		delete(t.files, path)
		t.mu.Unlock()
		return Summary{}, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	tf := t.files[path]
	if tf != nil && tf.size == info.Size() && tf.modTime.Equal(info.ModTime()) {
		return copySummary(tf.summary), nil
	}
	if tf == nil || info.Size() < tf.offset {
		tf = &trackedFile{}
	}
	if err := tf.readFrom(path); err != nil {
		return Summary{}, err
	}
	tf.size = info.Size()
	tf.modTime = info.ModTime()
	t.files[path] = tf
	return copySummary(tf.summary), nil
}

func copySummary(s Summary) Summary {
	s.EditedFiles = append([]string(nil), s.EditedFiles...)
	s.Todos = append([]Todo(nil), s.Todos...)
	return s
}

func (tf *trackedFile) readFrom(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(tf.offset, io.SeekStart); err != nil {
		return err
	}
	const readBufferSize = 64 * 1024
	reader := bufio.NewReaderSize(f, readBufferSize)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			tf.offset += int64(len(line))
			tf.applyLine(line)
		}
		// A final line without '\n' is still being written; read it next time.
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (tf *trackedFile) applyLine(line []byte) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var r record
	if json.Unmarshal(line, &r) != nil {
		return
	}
	tf.summary.apply(r)
}

// DefaultProjectsDir is where Claude Code keeps transcripts, honouring
// CLAUDE_CONFIG_DIR like Claude Code does.
func DefaultProjectsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// ProjectSlug is the folder name Claude Code uses for a working directory:
// every character other than a letter or digit becomes '-'.
func ProjectSlug(dir string) string {
	var b strings.Builder
	b.Grow(len(dir))
	for _, r := range dir {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// FindTranscript returns the transcript path of a session, or "".
func FindTranscript(projectsDir, cwd, sessionID string) string {
	if projectsDir == "" || sessionID == "" {
		return ""
	}
	if cwd != "" {
		p := filepath.Join(projectsDir, ProjectSlug(cwd), sessionID+".jsonl")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	matches, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// ListsFolder reports whether PastTranscripts for folder includes the
// sessions started in cwd: those of folder itself and of its parents.
func ListsFolder(folder, home, cwd string) bool {
	if folder == "" || cwd == "" {
		return false
	}
	slug := ProjectSlug(filepath.Clean(cwd))
	for _, dir := range ancestorDirs(folder, home) {
		if ProjectSlug(dir) == slug {
			return true
		}
	}
	return false
}

// PastTranscripts returns the newest transcripts of sessions started in folder
// or one of its ancestors (excluding the filesystem root and the home folder),
// newest first.
func PastTranscripts(projectsDir, folder, home string, limit int) []string {
	if projectsDir == "" || folder == "" || limit <= 0 {
		return nil
	}
	var candidates []transcriptFile
	for _, dir := range ancestorDirs(folder, home) {
		candidates = append(candidates, transcriptsIn(filepath.Join(projectsDir, ProjectSlug(dir)))...)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].modTime.After(candidates[j].modTime) })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	paths := make([]string, len(candidates))
	for i, c := range candidates {
		paths[i] = c.path
	}
	return paths
}

type transcriptFile struct {
	path    string
	modTime time.Time
}

// ancestorDirs returns folder and its parents, stopping before the
// filesystem root and the home folder.
func ancestorDirs(folder, home string) []string {
	home = filepath.Clean(home)
	var dirs []string
	for dir := filepath.Clean(folder); dir != "/" && dir != "." && dir != home; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return dirs
}

// transcriptsIn lists the non-empty transcript files in one project folder.
func transcriptsIn(slugDir string) []transcriptFile {
	entries, err := os.ReadDir(slugDir)
	if err != nil {
		return nil
	}
	var files []transcriptFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		files = append(files, transcriptFile{filepath.Join(slugDir, e.Name()), info.ModTime()})
	}
	return files
}

// SessionIDFromPath returns the session id encoded in a transcript file name.
func SessionIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}
