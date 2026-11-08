package sidebar

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGitLettersForFilesAndFolders(t *testing.T) {
	porcelain := " M src/model.go\n" +
		"A  src/new.go\n" +
		"R  old.go -> docs/renamed.go\n" +
		"?? notes.md\n" +
		"?? scratch/\n" +
		"?? \"my notes.txt\"\n"
	g := GitStatus{Root: "/repo", Changes: parseGitChanges(porcelain)}

	assert.Equal(t, "M", g.Letter("/repo/src/model.go", false))
	assert.Equal(t, "A", g.Letter("/repo/src/new.go", false))
	assert.Equal(t, "R", g.Letter("/repo/docs/renamed.go", false), "the new name of a rename")
	assert.Equal(t, "?", g.Letter("/repo/notes.md", false))
	assert.Equal(t, "?", g.Letter("/repo/my notes.txt", false), "quoted paths")
	assert.Equal(t, "?", g.Letter("/repo/scratch/a/b.txt", false), "inside an untracked folder")
	assert.Equal(t, "?", g.Letter("/repo/scratch", true))
	assert.Equal(t, "M", g.Letter("/repo/src", true), "a folder with changes inside")
	assert.Equal(t, "M", g.Letter("/repo/docs", true))
	assert.Empty(t, g.Letter("/repo/src/clean.go", false))
	assert.Empty(t, g.Letter("/repo/vendor", true))
	assert.Empty(t, g.Letter("/elsewhere/src/model.go", false), "outside the repo")
	assert.Empty(t, GitStatus{}.Letter("/repo/src/model.go", false), "not a repo")
}
