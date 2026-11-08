package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	variable "github.com/junjieliang672/marmota/src/config"
	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/ui/details"
	"github.com/junjieliang672/marmota/src/internal/ui/processbar"
	"github.com/junjieliang672/marmota/src/pkg/sysstat"
)

// The Details box at the foot of the screen (F shows or hides it, M gives
// it the keyboard) replaces the Processes and Metadata boxes: the cursor's
// file, file operations and the machine, in one box that scrolls.

// macInterval is how often the MAC section reads the machine.
const macInterval = 2 * time.Second

// opsFlashFor is how long the status line shows a finished operation while
// the Details box is hidden; a failed one stays longer.
const (
	opsDoneFlashFor   = 5 * time.Second
	opsFailedFlashFor = 30 * time.Second
)

type macTickMsg struct{}

type macStatsMsg struct{ stats sysstat.Stats }

// macSampleCmd reads the machine in the background, with the disk the
// focused panel's folder is on.
func (m *model) macSampleCmd() tea.Cmd {
	sampler, path := m.macSampler, m.getFocusedFilePanel().location
	if sampler == nil {
		return nil
	}
	return func() tea.Msg {
		return macStatsMsg{stats: sampler.Sample(path)}
	}
}

func scheduleMacTick() tea.Cmd {
	return tea.Tick(macInterval, func(time.Time) tea.Msg { return macTickMsg{} })
}

// applyMacTick reads the machine again, only while the box is on screen.
func (m *model) applyMacTick() tea.Cmd {
	if !m.toggleFooter {
		return scheduleMacTick()
	}
	return m.macSampleCmd()
}

func (m *model) applyMacStats(msg macStatsMsg) tea.Cmd {
	stats := msg.stats
	m.macStats = &stats
	return scheduleMacTick()
}

// detailsInput is what the Details box shows now.
func (m *model) detailsInput() details.Input {
	in := details.Input{Now: time.Now(), Mac: m.macStats}
	in.File = m.detailsFile()
	for _, p := range m.processBarModel.Processes() {
		in.Ops = append(in.Ops, detailsOp(p))
	}
	return in
}

// detailsFile is FILE: the metadata of the item under the cursor, with its
// git state.
func (m *model) detailsFile() *details.File {
	facts := m.fileMetaData.GetMetadata().Facts()
	if facts.Path == "" {
		if facts.Info == "" {
			return nil
		}
		return &details.File{Info: facts.Info}
	}
	f := &details.File{
		Path:        shortPath(facts.Path),
		IsDir:       facts.IsDir,
		Items:       facts.Items,
		Size:        facts.Size,
		Modified:    facts.Modified,
		Permissions: facts.Permissions,
		Resolution:  facts.Resolution,
		Page:        facts.Page,
		Info:        facts.Info,
	}
	if f.IsDir {
		f.Path = strings.TrimSuffix(f.Path, "/") + "/"
		if f.Items >= 0 && m.focusPanel != metadataFocus {
			// A folder's own size is its entry, not what's inside; the
			// real total is counted only while Details has the keyboard.
			f.Size = ""
		}
	}
	if facts.Modified.IsZero() && f.Info == "" && f.Size == "" {
		f.Info = "No metadata"
	}
	if gs := m.sidebarModel.GetGitStatus(); gs.IsGitRepo && gs.Root != "" &&
		strings.HasPrefix(facts.Path, gs.Root) {
		f.Git = gs.Letter(facts.Path, f.IsDir)
	}
	return f
}

// iCloudDocs is iCloud Drive's own folder under the home folder, and
// mobileDocs the folder holding it and each app's own iCloud folder.
const (
	mobileDocs   = "Library/Mobile Documents"
	iCloudDocs   = mobileDocs + "/com~apple~CloudDocs"
	cloudStorage = "Library/CloudStorage"
)

// cloudProviders names the folders macOS gives cloud drives under
// ~/Library/CloudStorage, by how their names start.
var cloudProviders = []struct{ prefix, name string }{ //nolint:gochecknoglobals // fixed names
	{"GoogleDrive", "Google Drive"}, {"Dropbox", "Dropbox"}, {"OneDrive", "OneDrive"}, {"Box", "Box"},
}

// shortPath writes a path shorter where the shorter name means only one
// place: iCloud Drive as "iCloud", an app's iCloud folder as "<app>
// (iCloud)", a cloud drive by its name, the home folder as "~", and
// /private/tmp as /tmp.
func shortPath(path string) string {
	under := func(dir string) (string, bool) {
		rest, ok := strings.CutPrefix(path, dir)
		return rest, ok && (rest == "" || strings.HasPrefix(rest, "/"))
	}
	if rest, ok := under("/private/tmp"); ok {
		return "/tmp" + rest
	}
	home := variable.HomeDir
	if home == "" {
		return path
	}
	if rest, ok := under(filepath.Join(home, iCloudDocs)); ok {
		return "iCloud" + rest
	}
	if rest, ok := under(filepath.Join(home, mobileDocs)); ok && rest != "" {
		container, more, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
		app := container[strings.LastIndex(container, "~")+1:]
		return app + " (iCloud)" + strings.TrimSuffix("/"+more, "/")
	}
	if rest, ok := under(filepath.Join(home, cloudStorage)); ok && rest != "" {
		drive, more, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
		if name := cloudDriveName(filepath.Join(home, cloudStorage), drive); name != "" {
			return name + strings.TrimSuffix("/"+more, "/")
		}
	}
	if rest, ok := under(home); ok {
		return "~" + rest
	}
	return path
}

// cloudDriveName is a cloud drive's folder by its provider's name, with the
// account after it only when that provider has more than one; "" for a
// folder no provider is known for.
func cloudDriveName(storage, drive string) string {
	for _, p := range cloudProviders {
		if !strings.HasPrefix(drive, p.prefix) {
			continue
		}
		entries, _ := os.ReadDir(storage)
		same := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), p.prefix) {
				same++
			}
		}
		if account := strings.TrimLeft(strings.TrimPrefix(drive, p.prefix), "-"); same > 1 && account != "" {
			return p.name + " (" + account + ")"
		}
		return p.name
	}
	return ""
}

func detailsOp(p processbar.Process) details.Op {
	op := details.Op{Name: p.Name, Done: p.Done, Total: p.Total, DoneTime: p.DoneTime}
	switch p.State {
	case processbar.InOperation:
		op.State = details.OpRunning
	case processbar.Successful:
		op.State = details.OpDone
	case processbar.Failed:
		op.State = details.OpFailed
	case processbar.Cancelled:
		op.State = details.OpCancelled
	}
	return op
}

func (m *model) detailsRender() string {
	return m.details.Render(m.detailsInput(), m.focusPanel == metadataFocus,
		firstKey(common.Hotkeys.FocusOnMetaData))
}

// setDetailsSize gives Details the dock's width the Sessions and Ask boxes
// leave.
func (m *model) setDetailsSize() {
	m.details.SetDimensions(m.detailsWidth(), m.footerHeight+common.BorderPadding)
}

func (m *model) detailsWidth() int {
	return m.fullWidth - m.dockSessionsWidth() - m.dockAskIdleWidth()
}

// opsStatus is the status line's word on file operations while the Details
// box is hidden: the running one with a meter, else one that just finished.
func (m *model) opsStatus() string {
	if m.toggleFooter && !m.dockAskOpen() {
		return "" // Details shows them
	}
	now := time.Now()
	running := 0
	var first, recent *processbar.Process
	procs := m.processBarModel.Processes()
	for i := range procs {
		p := &procs[i]
		switch {
		case p.State == processbar.InOperation:
			running++
			if first == nil {
				first = p
			}
		case recent == nil && !p.DoneTime.IsZero():
			recent = p
		}
	}
	bg := lipgloss.Color(common.Theme.FilePanelBG)
	style := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Background(bg) }
	switch {
	case first != nil:
		frac := 1.0
		if first.Total > 0 {
			// Done can pass Total: an extract counts its files, not its 1
			frac = min(float64(first.Done)/float64(first.Total), 1)
		}
		label := fmt.Sprintf("%s · %d of %d · %.0f%%", first.Name, first.Done, first.Total, frac*100) //nolint:mnd // percent
		if running > 1 {
			label = fmt.Sprintf("%d operations · %.0f%%", running, frac*100) //nolint:mnd // percent
		}
		n := int(frac*10 + 0.5) //nolint:mnd // a 10-cell meter, rounded
		return style(common.Theme.Correct).Render(label+" "+strings.Repeat("▓", n)) +
			style(common.Theme.FooterBorder).Render(strings.Repeat("░", 10-n)) //nolint:mnd // meter
	case recent != nil && recent.State == processbar.Failed && now.Sub(recent.DoneTime) < opsFailedFlashFor:
		return style(common.Theme.Error).Render("✗ " + recent.Name + " failed")
	case recent != nil && recent.State == processbar.Successful && now.Sub(recent.DoneTime) < opsDoneFlashFor:
		return style(common.Theme.Correct).Render("✓ " + recent.Name)
	}
	return ""
}
