package sidebar

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c",
		"commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// A repository with no commits yet has a branch all the same, and git's
// complaint about HEAD isn't it.
func TestFetchGitStatusNamesTheBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "trunk")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600))

	s := FetchGitStatus(dir)
	assert.True(t, s.IsGitRepo)
	assert.Equal(t, "trunk", s.BranchName, "before its first commit")
	assert.Empty(t, s.LastCommit)
	assert.Equal(t, 1, s.UnstagedCount)

	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "a")
	assert.Equal(t, "trunk", FetchGitStatus(dir).BranchName)

	git(t, dir, "checkout", "-q", "--detach")
	assert.Equal(t, "HEAD", FetchGitStatus(dir).BranchName, "detached")
}
