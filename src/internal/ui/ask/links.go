package ask

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Links in what a session's preview draws: [text](url), a web address
// written out, and a file's path where the session works, such as
// `src/app.go:12`. Each is drawn with where it goes kept by the terminal
// (OSC 8), so cmd+click opens it: WezTerm does, as Marmota's block in
// ~/.wezterm.lua has it, a file in micro at its line. A link to a session on
// claude.ai goes to the Claude app.

var (
	// webAddress is an address written out in the text.
	webAddress = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
	// filePath is a path with a folder in it, and maybe a line: "src/app.go",
	// "./notes.md:12", "/etc/hosts", "~/x/y.txt:3-9".
	filePath = regexp.MustCompile(`(?:~|\.{1,2})?/?(?:[\w.@+-]+/)+[\w.@+-]+(?::\d+(?:-\d+)?)?`)
	// fileName is a code span's whole text when it names a file, with or
	// without a folder: "app.go", "app.go:12".
	fileName = regexp.MustCompile(`^[\w./~@+-]+\.[\w]+(?::\d+(?:-\d+)?)?$`)
	// claudeSession is a session's page on claude.ai, which the Claude app
	// opens as claude://claude.ai/code/session_….
	claudeSession = regexp.MustCompile(`^https://claude\.ai/code/(session_\w+)`)
)

// trailing is punctuation that ends a sentence, not an address.
const trailing = ".,;:!?'\"。，；：！？、）」』】》〉…"

// linkSpans finds the links in spans that aren't links yet: addresses and
// paths written out, and a code span that names a file. Code keeps its
// look; an address or path in the text is drawn as a link.
func linkSpans(spans []span, dir string) []span {
	out := make([]span, 0, len(spans))
	for _, s := range spans {
		switch {
		case s.url != "":
			s.url = linkTarget(s.url)
			out = append(out, s)
		case s.kind == codeSpan:
			if u := fileURL(strings.TrimSpace(s.text), dir, true); u != "" {
				s.url = u
			}
			out = append(out, s)
		default:
			out = append(out, splitLinks(s, dir)...)
		}
	}
	return out
}

// splitLinks cuts the addresses and paths out of s as spans of their own.
func splitLinks(s span, dir string) []span {
	type found struct {
		from, to int
		url      string
	}
	var links []found
	taken := func(from, to int) bool {
		for _, l := range links {
			if from < l.to && to > l.from {
				return true
			}
		}
		return false
	}
	for _, m := range webAddress.FindAllStringIndex(s.text, -1) {
		to := m[0] + len(trimAddress(s.text[m[0]:m[1]]))
		links = append(links, found{m[0], to, linkTarget(s.text[m[0]:to])})
	}
	if dir != "" {
		for _, m := range filePath.FindAllStringIndex(s.text, -1) {
			to := m[0] + len(strings.TrimRight(s.text[m[0]:m[1]], trailing))
			if taken(m[0], to) || (m[0] > 0 && !boundary(s.text[m[0]-1])) {
				continue
			}
			if u := fileURL(s.text[m[0]:to], dir, false); u != "" {
				links = append(links, found{m[0], to, u})
			}
		}
	}
	if len(links) == 0 {
		return []span{s}
	}
	slices.SortFunc(links, func(a, b found) int { return a.from - b.from })
	var out []span
	at := 0
	for _, l := range links {
		if l.from > at {
			out = append(out, span{text: s.text[at:l.from], kind: s.kind})
		}
		kind := s.kind
		if kind == plainSpan {
			kind = linkSpan
		}
		out = append(out, span{text: s.text[l.from:l.to], kind: kind, url: l.url})
		at = l.to
	}
	if at < len(s.text) {
		out = append(out, span{text: s.text[at:], kind: s.kind})
	}
	return out
}

// boundary reports whether a path may start after b: not inside a word or
// an address.
func boundary(b byte) bool {
	return b == ' ' || b == '(' || b == '[' || b == '"' || b == '\''
}

// trimAddress takes the punctuation that ends a sentence off an address,
// and a closing bracket it doesn't open: "(see https://a.com/x)".
func trimAddress(a string) string {
	for {
		trimmed := strings.TrimRight(a, trailing)
		if strings.HasSuffix(trimmed, ")") && strings.Count(trimmed, "(") < strings.Count(trimmed, ")") {
			trimmed = trimmed[:len(trimmed)-1]
		}
		if trimmed == a {
			return a
		}
		a = trimmed
	}
}

// linkTarget is where a link goes when clicked: a session on claude.ai in
// the Claude app, else the address itself.
func linkTarget(address string) string {
	if m := claudeSession.FindStringSubmatch(address); m != nil {
		return "claude://claude.ai/code/" + m[1]
	}
	return address
}

// fileURL is file:///abs/path#L12 for a path, with its line, when the file
// is there: relative to dir, from home with ~, or absolute. "" when it
// isn't a file. whole says the path is a code span's whole text, which may
// name a file without a folder.
func fileURL(text, dir string, whole bool) string {
	if dir == "" || strings.Contains(text, "://") {
		return ""
	}
	if whole && !fileName.MatchString(text) && !filePath.MatchString(text) {
		return ""
	}
	path, line := text, 0
	if i := strings.LastIndexByte(text, ':'); i > 0 {
		from, _, _ := strings.Cut(text[i+1:], "-")
		if n, err := strconv.Atoi(from); err == nil {
			path, line = text[:i], n
		}
	}
	switch {
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, path[2:])
	case !filepath.IsAbs(path):
		path = filepath.Join(dir, path)
	}
	if !isFile(path) {
		return ""
	}
	u := url.URL{Scheme: "file", Path: path}
	if line > 0 {
		u.Fragment = "L" + strconv.Itoa(line)
	}
	return u.String()
}

// hyperlink starts a link to url (OSC 8), or ends one with "". It ends in
// ESC and a backslash, not the BEL ansi.SetHyperlink ends in, which the
// panels' renderer takes out with the other control characters.
func hyperlink(url string) string {
	return "\x1b]8;;" + url + "\x1b\\"
}

var (
	filesMu sync.Mutex
	// files remembers which paths are files, as the preview is drawn often.
	files = map[string]bool{}
)

const maxFilesKnown = 4096

func isFile(path string) bool {
	filesMu.Lock()
	defer filesMu.Unlock()
	if is, ok := files[path]; ok {
		return is
	}
	if len(files) >= maxFilesKnown {
		files = map[string]bool{}
	}
	info, err := os.Stat(path)
	is := err == nil && info.Mode().IsRegular()
	files[path] = is
	return is
}
