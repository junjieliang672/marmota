package variable

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/junjieliang672/marmota/src/internal/utils"

	"github.com/adrg/xdg"
)

// appDirName names Marmota's folders; oldDirName is what they were called
// before the program was Marmota. It is said here and nowhere else in the
// program: MoveOldDirs needs it to find those folders.
const (
	appDirName = "marmota"
	oldDirName = "superfile"
)

// getConfigDir returns the config directory called name.
// On macOS, we use ~/.config/<name> to follow Unix conventions,
// rather than ~/Library/Application Support which xdg library defaults to.
func getConfigDir(name string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(xdg.Home, ".config", name)
	}
	return filepath.Join(xdg.ConfigHome, name)
}

// MoveOldDirs moves the folders an install from before the rename used
// (its config folder, and its data, state and cache folders) to Marmota's,
// each where Marmota's doesn't exist yet, so settings and state carry over.
// It reports what it moved.
func MoveOldDirs() []string {
	var moved []string
	for _, p := range [][2]string{
		{getConfigDir(oldDirName), MarmotaMainDir},
		{filepath.Join(xdg.DataHome, oldDirName), MarmotaDataDir},
		{filepath.Join(xdg.StateHome, oldDirName), MarmotaStateDir},
		{filepath.Join(xdg.CacheHome, oldDirName), MarmotaCacheDir},
	} {
		if _, err := os.Stat(p[1]); err == nil {
			continue
		}
		if info, err := os.Stat(p[0]); err != nil || !info.IsDir() {
			continue
		}
		if os.Rename(p[0], p[1]) == nil {
			moved = append(moved, p[0]+" -> "+p[1])
		}
	}
	return moved
}

const (
	// RepoURL is where this program lives: its README, issues and source.
	RepoURL = "https://github.com/junjieliang672/marmota"
	// TutorialURL is the tour for new users (docs/tutorial.html, on GitHub Pages).
	TutorialURL = "https://junjieliang672.github.io/marmota/tutorial.html"

	CurrentVersion = "v1.4.0"
	// Allowing pre-releases with non production version
	// Set this to "" for production releases
	PreReleaseSuffix = ""

	// This will not break in windows. This is a relative path for Embed FS. It uses "/" only
	EmbedConfigDir           = "src/marmota_config"
	EmbedConfigFile          = EmbedConfigDir + "/config.toml"
	EmbedHotkeysFile         = EmbedConfigDir + "/hotkeys.toml"
	EmbedThemeDir            = EmbedConfigDir + "/theme"
	EmbedThemeCatppuccinFile = EmbedThemeDir + "/catppuccin-mocha.toml"
)

var (
	HomeDir         = xdg.Home
	DownloadsDir    = xdg.UserDirs.Download
	MarmotaMainDir  = getConfigDir(appDirName)
	MarmotaCacheDir = filepath.Join(xdg.CacheHome, appDirName)
	MarmotaDataDir  = filepath.Join(xdg.DataHome, appDirName)
	MarmotaStateDir = filepath.Join(xdg.StateHome, appDirName)

	// MainDir files
	ThemeFolder = filepath.Join(MarmotaMainDir, "theme")

	// DataDir files
	ThemeFileVersion = filepath.Join(MarmotaDataDir, "themeFileVersion")
	FirstUseCheck    = filepath.Join(MarmotaDataDir, "firstUseCheck")
	PinnedFile       = filepath.Join(MarmotaDataDir, "pinned.json")
	RecentEditsFile  = filepath.Join(MarmotaDataDir, "recent_edits.json")
	ToggleDotFile    = filepath.Join(MarmotaDataDir, "toggleDotFile")
	ToggleFooter     = filepath.Join(MarmotaDataDir, "toggleFooter")

	// StateDir files
	LogFile     = filepath.Join(MarmotaStateDir, "marmota.log")
	LastDirFile = filepath.Join(MarmotaStateDir, "lastdir")
	// When you last looked at each Claude session, for unread markers.
	ClaudeSessionsSeenFile = filepath.Join(MarmotaStateDir, "claude_sessions_seen.json")
	// What was sent from the Ask bar, and its model and effort choices.
	AskHistoryFile = filepath.Join(MarmotaStateDir, "ask_history.json")
	// The order of the Sessions list for the day, so a restart keeps it.
	ClaudeSessionsOrderFile = filepath.Join(MarmotaStateDir, "claude_sessions_order.json")
	// TmuxSinceFile holds when sessions first ran in tmux: running sessions
	// Marmota didn't start are listed only when they started before.
	TmuxSinceFile = filepath.Join(MarmotaStateDir, "tmux_since")
	// RemotesFile holds the other computers sessions were started on, and
	// how to reach them.
	RemotesFile = filepath.Join(MarmotaStateDir, "remotes.json")
	// TemporarySessionsFile holds the sessions started to answer one
	// program's questions, as the editor's: Marmota doesn't list them.
	TemporarySessionsFile = filepath.Join(MarmotaStateDir, "temporary_sessions.json")
	// SessionsRowsFile holds how many lines the Sessions box last had, so
	// the next start gives it that height before the sessions are listed,
	// and no box moves when they are.
	SessionsRowsFile = filepath.Join(MarmotaStateDir, "sessions_rows")
	// AppSessionsSeenFile holds how far each session of the Sessions box's
	// Remote section has been seen, so a restart doesn't show them unread.
	AppSessionsSeenFile = filepath.Join(MarmotaStateDir, "app_sessions_seen.json")
	// TranscriptSummariesFile holds what was read of each transcript, so a
	// start reads only what was written since and not every one whole.
	TranscriptSummariesFile = filepath.Join(MarmotaCacheDir, "transcript_summaries.json")
	// RemoteTranscriptsDir holds copies of the transcripts of sessions on
	// other computers, read a piece at a time.
	RemoteTranscriptsDir = filepath.Join(MarmotaCacheDir, "remote-transcripts")

	// Trash Directories
	DarwinTrashDirectory = filepath.Join(HomeDir, ".Trash")

	// These are used by github.com/rkoesters/xdg/trash package
	// We need to make sure that these directories exist
	LinuxTrashDirectory      = filepath.Join(xdg.DataHome, "Trash")
	LinuxTrashDirectoryFiles = filepath.Join(xdg.DataHome, "Trash", "files")
	LinuxTrashDirectoryInfo  = filepath.Join(xdg.DataHome, "Trash", "info")
)

// These variables are actually not fixed, they are sometimes updated dynamically
var (
	ConfigFile  = filepath.Join(MarmotaMainDir, "config.toml")
	HotkeysFile = filepath.Join(MarmotaMainDir, "hotkeys.toml")

	// ChooserFile is the path where Marmota will write the file's path, which is to be
	// opened, before exiting
	ChooserFile = ""

	// Other state variables
	FixHotkeys    = false
	FixConfigFile = false
	LastDir       = ""
	PrintLastDir  = false
)

// Still we are preventing other packages to directly modify them via reassign linter

func SetLastDir(path string) {
	LastDir = path
}

func SetChooserFile(path string) {
	ChooserFile = path
}

func UpdateVarFromCliArgs(c *cli.Command) {
	// Setting the config file path
	configFileArg := c.String("config-file")

	// Validate the config file exists
	if configFileArg != "" {
		if _, err := os.Stat(configFileArg); err != nil {
			utils.PrintfAndExitf("Error: While reading config file '%s' from argument : %v", configFileArg, err)
		}
		ConfigFile = configFileArg
	}

	hotkeyFileArg := c.String("hotkey-file")

	if hotkeyFileArg != "" {
		if _, err := os.Stat(hotkeyFileArg); err != nil {
			utils.PrintfAndExitf("Error: While reading hotkey file '%s' from argument : %v", hotkeyFileArg, err)
		}
		HotkeysFile = hotkeyFileArg
	}

	// It could be non existent. We are writing to the file. If file doesn't exists, we would attempt to create it.
	SetChooserFile(c.String("chooser-file"))

	FixHotkeys = c.Bool("fix-hotkeys")
	FixConfigFile = c.Bool("fix-config-file")
	PrintLastDir = c.Bool("print-last-dir")
}
