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
// before the program was Marmota (superfile).
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
// (~/.config/superfile, and its data, state and cache folders) to Marmota's,
// each where Marmota's doesn't exist yet, so settings and state carry over.
// It reports what it moved.
func MoveOldDirs() []string {
	var moved []string
	for _, p := range [][2]string{
		{getConfigDir(oldDirName), SuperFileMainDir},
		{filepath.Join(xdg.DataHome, oldDirName), SuperFileDataDir},
		{filepath.Join(xdg.StateHome, oldDirName), SuperFileStateDir},
		{filepath.Join(xdg.CacheHome, oldDirName), SuperFileCacheDir},
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

	CurrentVersion = "v1.4.0"
	// Allowing pre-releases with non production version
	// Set this to "" for production releases
	PreReleaseSuffix = ""

	// This will not break in windows. This is a relative path for Embed FS. It uses "/" only
	EmbedConfigDir           = "src/superfile_config"
	EmbedConfigFile          = EmbedConfigDir + "/config.toml"
	EmbedHotkeysFile         = EmbedConfigDir + "/hotkeys.toml"
	EmbedThemeDir            = EmbedConfigDir + "/theme"
	EmbedThemeCatppuccinFile = EmbedThemeDir + "/catppuccin-mocha.toml"
)

var (
	HomeDir           = xdg.Home
	DownloadsDir      = xdg.UserDirs.Download
	SuperFileMainDir  = getConfigDir(appDirName)
	SuperFileCacheDir = filepath.Join(xdg.CacheHome, appDirName)
	SuperFileDataDir  = filepath.Join(xdg.DataHome, appDirName)
	SuperFileStateDir = filepath.Join(xdg.StateHome, appDirName)

	// MainDir files
	ThemeFolder = filepath.Join(SuperFileMainDir, "theme")

	// DataDir files
	ThemeFileVersion = filepath.Join(SuperFileDataDir, "themeFileVersion")
	FirstUseCheck    = filepath.Join(SuperFileDataDir, "firstUseCheck")
	PinnedFile       = filepath.Join(SuperFileDataDir, "pinned.json")
	RecentEditsFile  = filepath.Join(SuperFileDataDir, "recent_edits.json")
	ToggleDotFile    = filepath.Join(SuperFileDataDir, "toggleDotFile")
	ToggleFooter     = filepath.Join(SuperFileDataDir, "toggleFooter")

	// StateDir files
	LogFile     = filepath.Join(SuperFileStateDir, "marmota.log")
	LastDirFile = filepath.Join(SuperFileStateDir, "lastdir")
	// When you last looked at each Claude session, for unread markers.
	ClaudeSessionsSeenFile = filepath.Join(SuperFileStateDir, "claude_sessions_seen.json")
	// What was sent from the Ask bar, and its model and effort choices.
	AskHistoryFile = filepath.Join(SuperFileStateDir, "ask_history.json")
	// The order of the Sessions list for the day, so a restart keeps it.
	ClaudeSessionsOrderFile = filepath.Join(SuperFileStateDir, "claude_sessions_order.json")

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
	ConfigFile  = filepath.Join(SuperFileMainDir, "config.toml")
	HotkeysFile = filepath.Join(SuperFileMainDir, "hotkeys.toml")

	// ChooserFile is the path where superfile will write the file's path, which is to be
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
