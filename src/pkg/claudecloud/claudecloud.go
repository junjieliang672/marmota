// Package claudecloud lists the account's Claude Code sessions as claude.ai
// knows them, the ones the Claude app shows: those with Remote Control on,
// on any computer, and cloud ones. It asks what `claude --teleport` asks,
// with the login Claude Code keeps, and only reads it.
package claudecloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBaseURL is where the sessions are listed.
const DefaultBaseURL = "https://api.anthropic.com"

// maxPages bounds one listing: a page holds 20 sessions, newest first.
const maxPages = 10

// Session is one session claude.ai lists.
type Session struct {
	// ID is claude.ai's id for it, cse_….
	ID    string
	Title string
	// Bucket is what it is doing: working, blocked (waiting for you) or
	// completed.
	Bucket string
	// Unread is set when it has a reply you haven't looked at.
	Unread bool
	// Connected is set while its computer is connected.
	Connected bool
	// Repo and Branch are where it works, as owner/name and branch.
	Repo   string
	Branch string
	// NeedsAction is what it waits for you to do, in its own words.
	NeedsAction string
	// Updated is when it last changed.
	Updated time.Time
}

// LinkID is the id its claude.ai link carries, session_….
func (s Session) LinkID() string {
	return "session_" + strings.TrimPrefix(s.ID, "cse_")
}

// AppURL opens it in the Claude app.
func (s Session) AppURL() string {
	return "claude://claude.ai/code/" + s.LinkID()
}

// WebURL opens it on claude.ai.
func (s Session) WebURL() string {
	return "https://claude.ai/code/" + s.LinkID()
}

var (
	// ErrNoLogin is returned when Claude Code was never logged in here.
	ErrNoLogin = errors.New("Claude Code isn't logged in")
	// ErrExpired is returned when its login expired: Claude Code renews it
	// the next time it runs.
	ErrExpired = errors.New("Claude Code's login has expired")
)

// Credentials is the login Claude Code keeps.
type Credentials struct {
	Token   string
	Expires time.Time
}

// Client lists the sessions.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	// Login reads Claude Code's login; ReadLogin when nil.
	Login func(ctx context.Context) (Credentials, error)
}

// List is the active sessions, newest first.
func (c Client) List(ctx context.Context) ([]Session, error) {
	login := c.Login
	if login == nil {
		login = ReadLogin
	}
	cred, err := login(ctx)
	if err != nil {
		return nil, err
	}
	if !cred.Expires.IsZero() && time.Now().After(cred.Expires) {
		return nil, ErrExpired
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	var out []Session
	cursor := ""
	for range maxPages {
		page, next, err := listPage(ctx, client, base, cred.Token, cursor)
		if err != nil {
			return nil, err
		}
		for _, s := range page {
			if s.Status == "active" {
				out = append(out, s.session())
			}
		}
		if next == "" || len(page) == 0 {
			break
		}
		cursor = next
	}
	return out, nil
}

func listPage(ctx context.Context, client *http.Client, base, token, cursor string,
) ([]apiSession, string, error) {
	u := base + "/v1/code/sessions"
	if cursor != "" {
		u += "?cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, "", ErrExpired
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200)) //nolint:mnd // enough to say why
		return nil, "", fmt.Errorf("claude.ai answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var page struct {
		Data       []apiSession `json:"data"`
		NextCursor string       `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("claude.ai's list of sessions can't be read: %w", err)
	}
	return page.Data, page.NextCursor, nil
}

// apiSession is a session as claude.ai lists it, the parts read here.
type apiSession struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	StatusBucket     string    `json:"status_bucket"`
	Unread           bool      `json:"unread"`
	ConnectionStatus string    `json:"connection_status"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastEventAt      time.Time `json:"last_event_at"`
	Config           struct {
		Sources []struct {
			Type     string `json:"type"`
			URL      string `json:"url"`
			Revision string `json:"revision"`
		} `json:"sources"`
	} `json:"config"`
	ExternalMetadata struct {
		CurrentBranches map[string]string `json:"current_branches"`
		PostTurnSummary *struct {
			NeedsAction string `json:"needs_action"`
		} `json:"post_turn_summary"`
	} `json:"external_metadata"`
}

func (a apiSession) session() Session {
	s := Session{ID: a.ID, Title: a.Title, Bucket: a.StatusBucket, Unread: a.Unread,
		Connected: a.ConnectionStatus == "connected", Updated: a.LastEventAt}
	if s.Updated.IsZero() {
		s.Updated = a.UpdatedAt
	}
	if p := a.ExternalMetadata.PostTurnSummary; p != nil {
		s.NeedsAction = strings.TrimSpace(p.NeedsAction)
	}
	for _, src := range a.Config.Sources {
		if src.Type == "git_repository" {
			s.Repo, s.Branch = repoName(src.URL), src.Revision
			break
		}
	}
	// The branch it is on now, rather than the one it started on.
	for repo, branch := range a.ExternalMetadata.CurrentBranches {
		if s.Repo == "" || strings.HasSuffix(repo, s.Repo) || strings.HasSuffix(s.Repo, repo) {
			s.Repo, s.Branch = repo, branch
			break
		}
	}
	return s
}

// repoName is owner/name of a repository's URL.
func repoName(u string) string {
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	parts := strings.Split(u, "/")
	if len(parts) < 2 { //nolint:mnd // owner and name
		return u
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// keychain reads Claude Code's login from the macOS keychain.
var keychain = func(ctx context.Context) ([]byte, error) { //nolint:gochecknoglobals // tests replace it
	return exec.CommandContext(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w").
		Output()
}

// credentialsFile is where Claude Code keeps its login outside macOS.
var credentialsFile = func() string { //nolint:gochecknoglobals // tests replace it
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

// ReadLogin reads the login Claude Code keeps: in its credentials file, or
// in the macOS keychain.
func ReadLogin(ctx context.Context) (Credentials, error) {
	data, err := os.ReadFile(credentialsFile())
	if err != nil {
		data, err = keychain(ctx)
	}
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return Credentials{}, ErrNoLogin
	}
	var stored struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &stored); err != nil || stored.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, ErrNoLogin
	}
	cred := Credentials{Token: stored.ClaudeAiOauth.AccessToken}
	if ms := stored.ClaudeAiOauth.ExpiresAt; ms > 0 {
		cred.Expires = time.UnixMilli(ms)
	}
	return cred, nil
}
