package sidebar

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

const (
	// gitRootTimeout bounds finding the repository's top folder.
	gitRootTimeout = 500 * time.Millisecond
	// gitTimeout bounds git's other quick answers: whether this is a
	// repository, the branch, how far it is from upstream, the last commit.
	gitTimeout = 500 * time.Millisecond
	// gitStatusTimeout bounds git status, which reads the whole work tree.
	gitStatusTimeout = time.Second
	// porcelainStatusLen is the two status letters a porcelain line starts
	// with: the index's, then the work tree's.
	porcelainStatusLen = 2
	// aheadBehindFields are the two counts rev-list --left-right prints.
	aheadBehindFields = 2
)

// GitStatus holds the current git repository status
type GitStatus struct {
	IsGitRepo     bool
	BranchName    string
	StagedCount   int    // Files staged for commit (+)
	UnstagedCount int    // Modified/untracked files (~)
	AheadCount    int    // Commits ahead of upstream (↑)
	BehindCount   int    // Commits behind upstream (↓)
	LastCommit    string // Short hash of last commit
	LastPath      string // Directory this status is for
	// Root is the repository's top folder. Changes holds each changed path,
	// relative to Root, with its letter: "M", "A", "D", "R", "U", or "?" for
	// an untracked one. An untracked folder is listed once, ending in "/".
	Root    string
	Changes map[string]string
}

// Letter returns the change git shows for a file or folder under Root: a
// folder gets "M" when anything tracked inside changed, else "?" when it holds
// untracked files. It is "" for an unchanged path or one outside the repo.
func (g GitStatus) Letter(path string, isDir bool) string {
	if g.Root == "" || len(g.Changes) == 0 {
		return ""
	}
	rel, err := filepath.Rel(g.Root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if letter, ok := g.Changes[rel]; ok && !isDir {
		return letter
	}
	// Inside an untracked folder, everything is untracked.
	for dir := parentOf(rel); dir != ""; dir = parentOf(dir) {
		if g.Changes[dir+"/"] == "?" {
			return "?"
		}
	}
	if !isDir {
		return ""
	}
	if g.Changes[rel+"/"] == "?" {
		return "?"
	}
	letter := ""
	for changed, l := range g.Changes {
		if strings.HasPrefix(changed, rel+"/") {
			if l != "?" {
				return "M"
			}
			letter = "?"
		}
	}
	return letter
}

func parentOf(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i <= 0 {
		return ""
	}
	return rel[:i]
}

// parseGitChanges reads `git status --porcelain` into paths and letters.
func parseGitChanges(output string) map[string]string {
	changes := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		const minLine = 4 // "XY p"
		if len(line) < minLine {
			continue
		}
		x, y, path := line[0], line[1], line[3:]
		if _, renamed, ok := strings.Cut(path, " -> "); ok {
			path = renamed
		}
		if strings.HasPrefix(path, `"`) {
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			}
		}
		switch {
		case x == '?' && y == '?':
			changes[path] = "?"
		case x == '!':
			// ignored
		case y != ' ':
			changes[path] = string(y)
		default:
			changes[path] = string(x)
		}
	}
	return changes
}

// GitStatusUnavailable returns a status indicating git is not available
func GitStatusUnavailable() GitStatus {
	return GitStatus{IsGitRepo: false}
}

// FetchGitStatus fetches git status for the given directory
func FetchGitStatus(dir string) GitStatus {
	status := GitStatus{LastPath: dir}

	// Check if git repo
	exitCode, _, err := utils.ExecuteCommand(gitTimeout, dir, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil || exitCode != 0 {
		return status
	}
	status.IsGitRepo = true

	// The branch. symbolic-ref names it before its first commit too, where
	// rev-parse fails, its error mixed into the output; rev-parse says HEAD
	// for a detached one.
	if code, out, _ := utils.ExecuteCommand(gitTimeout, dir, "git", "symbolic-ref", "--short", "-q",
		"HEAD"); code == 0 {
		status.BranchName = strings.TrimSpace(out)
	} else if code, out, _ := utils.ExecuteCommand(gitTimeout, dir, "git", "rev-parse", "--abbrev-ref",
		"HEAD"); code == 0 {
		status.BranchName = strings.TrimSpace(out)
	}

	// Get status using porcelain format
	_, porcelainOutput, _ := utils.ExecuteCommand(gitStatusTimeout, dir, "git", "status", "--porcelain")
	status.StagedCount, status.UnstagedCount = parseGitPorcelain(porcelainOutput)
	if rootCode, root, _ := utils.ExecuteCommand(gitRootTimeout, dir, "git", "rev-parse",
		"--show-toplevel"); rootCode == 0 {
		status.Root = strings.TrimSpace(root)
		status.Changes = parseGitChanges(porcelainOutput)
	}

	// Get ahead/behind count (may fail if no upstream is set)
	exitCode, aheadBehindOutput, _ := utils.ExecuteCommand(
		gitTimeout,
		dir,
		"git",
		"rev-list",
		"--left-right",
		"--count",
		"@{u}...HEAD",
	)
	if exitCode == 0 {
		status.BehindCount, status.AheadCount = parseAheadBehind(aheadBehindOutput)
	}

	// Get last commit short hash
	exitCode, commitOutput, _ := utils.ExecuteCommand(gitTimeout, dir, "git", "log", "-1", "--format=%h")
	if exitCode == 0 {
		status.LastCommit = strings.TrimSpace(commitOutput)
	}

	return status
}

// parseAheadBehind parses the output of git rev-list --left-right --count
// Format: "behind\tahead" (e.g., "3\t2" means 3 behind, 2 ahead)
func parseAheadBehind(output string) (int, int) {
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) < aheadBehindFields {
		return 0, 0
	}
	// First number is behind (left), second is ahead (right/HEAD)
	return parseIntOrZero(parts[0]), parseIntOrZero(parts[1])
}

// parseIntOrZero parses a string to int, returning 0 on error
func parseIntOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// parseGitPorcelain parses git status --porcelain output
// Returns (staged count, unstaged count)
func parseGitPorcelain(output string) (int, int) {
	staged, unstaged := 0, 0
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if len(line) < porcelainStatusLen {
			continue
		}
		indexStatus := line[0]    // Status in index (staged)
		workTreeStatus := line[1] // Status in work tree (unstaged)

		// Staged changes: A, M, D, R, C in index position (not space or ?)
		if indexStatus != ' ' && indexStatus != '?' {
			staged++
		}
		// Unstaged changes: M, D in work tree position (not space)
		if workTreeStatus != ' ' && workTreeStatus != '?' {
			unstaged++
		}
		// Untracked files: ?? - count as unstaged
		if indexStatus == '?' && workTreeStatus == '?' {
			unstaged++
		}
	}
	return staged, unstaged
}
