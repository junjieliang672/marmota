// Package claudeask asks Claude Code about the files superfile shows. It
// writes the context block that goes with every request superfile sends
// ("these files" means the ones you selected), the arguments of a read-only
// `claude -p` run, and reads that run's streamed output.
package claudeask

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/junjieliang672/marmota/src/pkg/claudesession"
)

// Context is what you are looking at in superfile when you ask.
type Context struct {
	// Folder is the file panel's folder.
	Folder string
	// Cursor is the name of the item under the cursor, "" in an empty folder.
	Cursor    string
	CursorDir bool
	// Selected are the names of the selected items, in panel order.
	Selected []string
	// Sort says how the panel is sorted, e.g. "name ↑".
	Sort       string
	ShowHidden bool
	// Filter is the panel's search filter, if one is narrowing it.
	Filter string
	// InGit marks a folder inside a git repository; GitChanged counts its
	// staged and modified files.
	InGit      bool
	GitBranch  string
	GitChanged int
	// Notes is the nearest CLAUDE.md in the folder or above it.
	Notes string
}

// maxSelectedNames is how many selected names the block lists before it
// only counts the rest.
const maxSelectedNames = 20

// Block is the context as Claude reads it, wrapped in claudesession's
// context tags so superfile can leave it out when it shows the prompt.
func (c Context) Block(home string) string {
	var b strings.Builder
	b.WriteString(claudesession.ContextOpenTag + "\n")
	line := func(key, value string) {
		fmt.Fprintf(&b, "%-9s %s\n", key+":", value)
	}
	line("cwd", DisplayPath(c.Folder, home))
	if c.Cursor != "" {
		line("cursor", c.Cursor+dirSuffix(c.CursorDir))
	}
	if n := len(c.Selected); n > 0 {
		names, more := c.Selected, ""
		if n > maxSelectedNames {
			names, more = names[:maxSelectedNames], fmt.Sprintf(", … and %d more", n-maxSelectedNames)
		}
		line("selected", fmt.Sprintf("%d %s: %s%s", n, plural(n, "item", "items"), strings.Join(names, ", "), more))
	}
	if view := c.view(); view != "" {
		line("view", view)
	}
	if c.InGit {
		git := c.GitBranch
		if git == "" {
			git = "detached"
		}
		if c.GitChanged > 0 {
			git += fmt.Sprintf(" · %d changed", c.GitChanged)
		} else {
			git += " · clean"
		}
		line("git", git)
	}
	if c.Notes != "" {
		line("notes", DisplayPath(c.Notes, home))
	}
	b.WriteString(claudesession.ContextCloseTag)
	return b.String()
}

func (c Context) view() string {
	var parts []string
	if c.Sort != "" {
		parts = append(parts, "sorted by "+c.Sort)
	}
	if c.ShowHidden {
		parts = append(parts, "hidden files shown")
	}
	if c.Filter != "" {
		parts = append(parts, fmt.Sprintf("filtered by %q", c.Filter))
	}
	return strings.Join(parts, ", ")
}

// Prompt is a request followed by the context block.
func Prompt(request string, c Context, home string) string {
	return strings.TrimSpace(request) + "\n\n" + c.Block(home)
}

// DisplayPath writes a path under home as ~/….
func DisplayPath(path, home string) string {
	if home != "" && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func dirSuffix(dir bool) string {
	if dir {
		return "/"
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
