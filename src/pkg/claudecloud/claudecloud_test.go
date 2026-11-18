package claudecloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Never the keychain or the login of the computer the tests run on.
	keychain = func(context.Context) ([]byte, error) { return nil, os.ErrNotExist }
	credentialsFile = func() string { return filepath.Join(os.TempDir(), "claudecloud-no-such-file") }
	os.Exit(m.Run())
}

const page1 = `{"data":[
 {"id":"cse_015YPex","title":"b1","status":"active","status_bucket":"blocked","unread":false,
  "connection_status":"connected","last_event_at":"2026-11-17T23:01:57Z",
  "config":{"sources":[{"type":"git_repository","url":"https://code.example.com/sec-algo/redteam-root","revision":"main"}]},
  "external_metadata":{"current_branches":{"sec-algo/redteam-root":"arch/agent-swarm"},
   "post_turn_summary":{"needs_action":"bring b2 back online","recent_action":"report written"}}},
 {"id":"cse_old","title":"done long ago","status":"archived"}
],"next_cursor":"PAGE2"}`

const page2 = `{"data":[
 {"id":"cse_01Mmx","title":"b2","status":"active","status_bucket":"completed","unread":true,
  "connection_status":"disconnected","updated_at":"2026-11-17T21:34:42Z","config":{},"external_metadata":{"post_turn_summary":null}}
]}`

func login(context.Context) (Credentials, error) {
	return Credentials{Token: "tok", Expires: time.Now().Add(time.Hour)}, nil
}

func TestListReadsEveryPageAndKeepsTheActiveSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/code/sessions", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		if r.URL.Query().Get("cursor") == "PAGE2" {
			_, _ = w.Write([]byte(page2))
			return
		}
		_, _ = w.Write([]byte(page1))
	}))
	defer srv.Close()

	got, err := Client{BaseURL: srv.URL, Login: login}.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2, "the archived one is left out")

	b1 := got[0]
	assert.Equal(t, "cse_015YPex", b1.ID)
	assert.Equal(t, "blocked", b1.Bucket)
	assert.True(t, b1.Connected)
	assert.Equal(t, "sec-algo/redteam-root", b1.Repo)
	assert.Equal(t, "arch/agent-swarm", b1.Branch, "the branch it is on now")
	assert.Equal(t, "bring b2 back online", b1.NeedsAction)
	assert.Equal(t, "claude://claude.ai/code/session_015YPex", b1.AppURL())
	assert.Equal(t, "https://claude.ai/code/session_015YPex", b1.WebURL())

	b2 := got[1]
	assert.False(t, b2.Connected)
	assert.True(t, b2.Unread)
	assert.Equal(t, time.Date(2026, 11, 17, 21, 34, 42, 0, time.UTC), b2.Updated.UTC(),
		"updated_at when there is no last event")
}

func TestAnExpiredLoginIsntSent(t *testing.T) {
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { asked = true }))
	defer srv.Close()
	expired := func(context.Context) (Credentials, error) {
		return Credentials{Token: "tok", Expires: time.Now().Add(-time.Minute)}, nil
	}
	_, err := Client{BaseURL: srv.URL, Login: expired}.List(context.Background())
	require.ErrorIs(t, err, ErrExpired)
	assert.False(t, asked)
}

func TestARefusedLoginIsExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := Client{BaseURL: srv.URL, Login: login}.List(context.Background())
	require.ErrorIs(t, err, ErrExpired)
}

func TestAServerErrorSaysWhat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()
	_, err := Client{BaseURL: srv.URL, Login: login}.List(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
	assert.Contains(t, err.Error(), "upstream down")
}

func TestReadLogin(t *testing.T) {
	_, err := ReadLogin(context.Background())
	require.ErrorIs(t, err, ErrNoLogin, "no file and no keychain item")

	file := filepath.Join(t.TempDir(), ".credentials.json")
	require.NoError(t, os.WriteFile(file,
		[]byte(`{"claudeAiOauth":{"accessToken":"abc","expiresAt":1795058432990}}`), 0o600))
	was := credentialsFile
	credentialsFile = func() string { return file }
	t.Cleanup(func() { credentialsFile = was })
	cred, err := ReadLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc", cred.Token)
	assert.Equal(t, int64(1795058432990), cred.Expires.UnixMilli())

	credentialsFile = func() string { return filepath.Join(t.TempDir(), "none") }
	keychain = func(context.Context) ([]byte, error) { return []byte(`{"claudeAiOauth":{"accessToken":"kc"}}`), nil }
	t.Cleanup(func() { keychain = func(context.Context) ([]byte, error) { return nil, os.ErrNotExist } })
	cred, err = ReadLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "kc", cred.Token, "from the keychain when there is no file")
	assert.True(t, cred.Expires.IsZero())
}
