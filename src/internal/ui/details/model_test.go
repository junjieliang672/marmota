package details

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func folder() *File {
	return &File{Path: "/Users/me/Developer/superfile/asset/", IsDir: true, Items: 21, Size: "3.4 MB",
		Modified: time.Date(2025, 12, 14, 12, 1, 0, 0, time.UTC), Permissions: "drwxr-xr-x"}
}

func mac() *sysstat.Stats {
	return &sysstat.Stats{CPUOK: true, CPUPercent: 23, Cores: 24,
		MemOK: true, MemUsed: 74 << 30, MemTotal: 192 << 30,
		DiskOK: true, DiskUsed: 732e9, DiskTotal: 995e9, DiskFree: 263e9}
}

// plain is the box without colours, one string per line.
func plain(s string) []string {
	return strings.Split(ansi.Strip(s), "\n")
}

// inner is a line of the box without its left and right edge, whatever
// characters the border is drawn with.
func inner(line string) string {
	r := []rune(line)
	if len(r) < 2 { //nolint:mnd // both edges
		return ""
	}
	return strings.TrimRight(string(r[1:len(r)-1]), " ")
}

func TestSectionsInOrder(t *testing.T) {
	m := New()
	m.SetDimensions(90, 13)
	in := Input{File: folder(), Mac: mac(), Now: now, Ops: []Op{
		{Name: "copy 12 → ~/Downloads", State: OpRunning, Done: 6, Total: 12},
		{Name: "delete 2", State: OpFailed, DoneTime: now.Add(-5 * time.Minute)},
	}}
	got := plain(m.Render(in, false, "M"))

	want := []string{
		" FILE  /Users/me/Developer/superfile/asset/",
		"  ▎    folder · 21 items · 3.4 MB · " + icon.Calendar + " Dec 14, 2025 12:01",
		"",
		" MAC   cpu    ▓▓░░░░░░░░ 23%",
		"  ▎    memory ▓▓▓▓░░░░░░ 74 / 192 GB",
		"  ▎    disk   ▓▓▓▓▓▓▓░░░ 732 / 995 GB",
		"",
		" OPS   copy 12 → ~/Downloads ▓▓▓▓▓░░░░░ 50% · 6 of 12",
		"  ▎    ✗ delete 2 5m ago",
	}
	require.Len(t, got, 13)
	assert.Contains(t, got[0], "Details")
	for i, w := range want {
		assert.Equal(t, w, inner(got[i+1]), "line %d", i+1)
	}
	assert.Contains(t, got[12], "9/9 · M focus")
}

func TestEmptySectionsAreLeftOut(t *testing.T) {
	m := New()
	m.SetDimensions(70, 8)
	got := ansi.Strip(m.Render(Input{File: folder(), Now: now}, false, "M"))

	assert.Contains(t, got, "FILE")
	assert.NotContains(t, got, "OPS")
	assert.NotContains(t, got, "MAC")
}

func TestScrolling(t *testing.T) {
	m := New()
	m.SetDimensions(70, 5) // 3 lines of the 8
	in := Input{File: folder(), Mac: mac(), Now: now,
		Ops: []Op{{Name: "zip", State: OpDone, DoneTime: now}}}

	first := plain(m.Render(in, true, "M"))
	assert.Contains(t, first[1], "FILE")
	assert.Contains(t, first[4], "3/8 · ↑↓ scroll")

	m.ScrollBottom()
	last := plain(m.Render(in, true, "M"))
	assert.Contains(t, last[3], "OPS")
	assert.Contains(t, last[4], "8/8")

	m.ScrollBy(100)
	m.ScrollBy(-100)
	assert.Contains(t, plain(m.Render(in, true, "M"))[1], "FILE", "scrolling stops at the top")
}

func TestOverfullOperationFillsTheMeter(t *testing.T) {
	m := New()
	m.SetDimensions(70, 5)
	in := Input{Now: now, Ops: []Op{{Name: "unzip", State: OpRunning, Done: 5, Total: 1}}}

	assert.Contains(t, ansi.Strip(m.Render(in, false, "M")), "▓▓▓▓▓▓▓▓▓▓ 100%")
}

func TestLongPathsWrap(t *testing.T) {
	m := New()
	m.SetDimensions(40, 8) // 31 columns after the label
	f := folder()
	f.Path = "/Users/me/Library/Mobile Documents/com~apple~CloudDocs/report.pdf"
	lines := plain(m.Render(Input{File: f, Now: now}, false, "M"))

	assert.Equal(t, " FILE  /Users/me/Library/", inner(lines[1]))
	assert.Equal(t, "  ▎    Mobile Documents/", inner(lines[2]))
	assert.Equal(t, "  ▎    com~apple~CloudDocs/report.pdf", inner(lines[3]))
	for _, l := range lines {
		assert.LessOrEqual(t, ansi.StringWidth(l), 40)
	}
}

func TestWrapPath(t *testing.T) {
	assert.Equal(t, []string{"/a/b/"}, wrapPath("/a/b/", 10))
	assert.Equal(t, []string{"/Users/", "me/", "file.go"}, wrapPath("/Users/me/file.go", 8))
	assert.Equal(t, []string{"/abcd", "efghi", "j/x"}, wrapPath("/abcdefghij/x", 5),
		"a name wider than the line is split")
}

func TestGitShowsOnlyChanges(t *testing.T) {
	m := New()
	m.SetDimensions(100, 5)
	f := folder()
	assert.NotContains(t, ansi.Strip(m.Render(Input{File: f, Now: now}, false, "M")), "git")

	f.Git = "M"
	assert.Contains(t, ansi.Strip(m.Render(Input{File: f, Now: now}, false, "M")), "· git M")
}

func TestNothingIsCutOff(t *testing.T) {
	m := New()
	m.SetDimensions(36, 20)
	in := Input{File: folder(), Mac: mac(), Now: now, Ops: []Op{
		{Name: "copy 12 files → ~/Downloads/a-folder-with-a-long-name", State: OpRunning, Done: 6, Total: 12}}}
	out := ansi.Strip(m.Render(in, false, "M"))

	assert.NotContains(t, out, "…", "lines wrap instead")
	assert.Contains(t, out, "a-long-name", "the end of the long name is there")
	for _, l := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, ansi.StringWidth(l), 36)
	}
}
