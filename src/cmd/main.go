package cmd

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/urfave/cli/v3"

	variable "github.com/junjieliang672/marmota/src/config"
	internal "github.com/junjieliang672/marmota/src/internal"
)

// Run superfile app
func Run(content embed.FS) {
	// Enable custom colored help output
	cli.HelpPrinter = CustomHelpPrinter //nolint:reassign // Intentionally reassigning to customize help output

	// Before we open log file, set all "non debug" logs to stdout
	utils.SetRootLoggerToStdout(false)

	// Settings and state from before the program was Marmota come along
	for _, m := range variable.MoveOldDirs() {
		slog.Debug("Moved a folder from superfile", "move", m)
	}

	common.LoadInitialPrerenderedVariables()
	common.LoadAllDefaultConfig(content)

	app := &cli.Command{
		Name:    "marmota",
		Version: variable.CurrentVersion + variable.PreReleaseSuffix,
		Description: "A terminal file manager that works beside your Claude Code and Codex sessions.\n" +
			"By Junjie Liang. Adapted from superfile by Yorukot (github.com/junjieliang672/marmota, MIT License).",
		Authors:   []any{"Junjie Liang"},
		ArgsUsage: "[PATH]...",
		Commands: []*cli.Command{
			{
				Name:    "path-list",
				Aliases: []string{"pl"},
				Usage:   "Print the path to the configuration and directory",
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Bool("lastdir-file") {
						fmt.Println(variable.LastDirFile)
						return nil
					}
					fmt.Printf("%-*s %s\n",
						common.HelpKeyColumnWidth,
						lipgloss.NewStyle().Foreground(lipgloss.Color("#66b2ff")).Render("[Configuration file path]"),
						variable.ConfigFile,
					)
					fmt.Printf("%-*s %s\n",
						common.HelpKeyColumnWidth,
						lipgloss.NewStyle().Foreground(lipgloss.Color("#ffcc66")).Render("[Hotkeys file path]"),
						variable.HotkeysFile,
					)
					logStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#66ff66"))
					configStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff9999"))
					dataStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff66ff"))
					fmt.Printf("%-*s %s\n", common.HelpKeyColumnWidth,
						logStyle.Render("[Log file path]"), variable.LogFile)
					fmt.Printf("%-*s %s\n", common.HelpKeyColumnWidth,
						configStyle.Render("[Configuration directory path]"), variable.SuperFileMainDir)
					fmt.Printf("%-*s %s\n", common.HelpKeyColumnWidth,
						dataStyle.Render("[Data directory path]"), variable.SuperFileDataDir)
					return nil
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "lastdir-file",
						Aliases: []string{"ld"},
						Usage:   "Print path to lastdir file (Where last dir is written when cd_on_quit config is true)",
						Value:   false,
					},
				},
			},
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "fix-hotkeys",
				Aliases: []string{"fh"},
				Usage:   "Adds any missing hotkeys to the hotkey config file",
				Value:   false,
			},
			&cli.BoolFlag{
				Name:    "fix-config-file",
				Aliases: []string{"fch"},
				Usage:   "Adds any missing hotkeys to the hotkey config file",
				Value:   false,
			},
			&cli.BoolFlag{
				Name: "fix-only",
				Usage: "Load the config and hotkey files, applying --fix-config-file and --fix-hotkeys, " +
					"then exit without opening Marmota",
				Value: false,
			},
			&cli.BoolFlag{
				Name:    "print-last-dir",
				Aliases: []string{"pld"},
				Usage:   "Print the last dir to stdout on exit (to use for cd)",
				Value:   false,
			},
			&cli.StringFlag{
				Name:    "config-file",
				Aliases: []string{"c"},
				Usage:   "Specify the path to a different config file",
				Value:   "", // Default to the blank string indicating non-usage of flag
			},
			&cli.StringFlag{
				Name:    "hotkey-file",
				Aliases: []string{"hf"},
				Usage:   "Specify the path to a different hotkey file",
				Value:   "", // Default to the blank string indicating non-usage of flag
			},
			&cli.StringFlag{
				Name:    "chooser-file",
				Aliases: []string{"cf"},
				Usage:   "On trying to open any file, Marmota will write to its path to this file, and exit",
				Value:   "", // Default to the blank string indicating non-usage of flag
			},
		},
		Action: spfAppAction,
	}

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		utils.PrintlnAndExit(err)
	}
}

func spfAppAction(_ context.Context, c *cli.Command) error {
	// If no args are called along with y, use current dir
	firstPanelPaths := []string{""}
	if c.Args().Present() {
		firstPanelPaths = c.Args().Slice()
	}

	variable.UpdateVarFromCliArgs(c)

	InitConfigFile()

	// Scripts like build.sh fix the config this way; without it, superfile
	// would open its interface after fixing and wait for keys forever.
	if c.Bool("fix-only") {
		common.LoadConfigFile()
		common.LoadHotkeysFile(common.Config.IgnoreMissingFields)
		return nil
	}

	firstUse := checkFirstUse()

	// WithReportFocus drives the IME ascii-hold: superfile releases its
	// pass-through request when the terminal loses focus, so a superfile left
	// in hotkey mode in a background tab cannot force ASCII input elsewhere.
	p := tea.NewProgram(internal.InitialModel(firstPanelPaths, firstUse),
		tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus())
	if _, err := p.Run(); err != nil {
		utils.PrintfAndExitf("Alas, there's been an error: %v", err)
	}

	if variable.PrintLastDir {
		fmt.Println(variable.LastDir)
	}

	return nil
}

// Create proper directories for storing configuration and write default
// configurations to Config and Hotkeys toml
func InitConfigFile() {
	// Create directories
	if err := utils.CreateDirectories(
		variable.SuperFileMainDir,
		variable.SuperFileDataDir,
		variable.SuperFileStateDir,
		variable.ThemeFolder,
	); err != nil {
		utils.PrintlnAndExit("Error creating directories:", err)
	}

	// Create files
	if err := utils.CreateFiles(
		variable.ToggleDotFile,
		variable.LogFile,
		variable.ThemeFileVersion,
		variable.ToggleFooter,
	); err != nil {
		utils.PrintlnAndExit("Error creating files:", err)
	}

	// Write config file
	if err := writeConfigFile(variable.ConfigFile, common.ConfigTomlString); err != nil {
		utils.PrintlnAndExit("Error writing config file:", err)
	}

	if err := writeConfigFile(variable.HotkeysFile, common.HotkeysTomlString); err != nil {
		utils.PrintlnAndExit("Error writing config file:", err)
	}

	if err := initJSONFile(variable.PinnedFile); err != nil {
		utils.PrintlnAndExit("Error initializing json file:", err)
	}
}

// Check if is the first time initializing the app, if it is create
// use check file
func checkFirstUse() bool {
	file := variable.FirstUseCheck
	firstUse := false
	if _, err := os.Stat(file); os.IsNotExist(err) {
		firstUse = true
		if err = os.WriteFile(file, nil, utils.ConfigFilePerm); err != nil {
			utils.PrintfAndExitf("Failed to create file: %v", err)
		}
	}
	return firstUse
}

// Write data to the path file if it does not exists
func writeConfigFile(path, data string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.WriteFile(path, []byte(data), utils.ConfigFilePerm); err != nil {
			return fmt.Errorf("failed to write config file %s: %w", path, err)
		}
	}
	return nil
}

func initJSONFile(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err = os.WriteFile(path, []byte("null"), utils.ConfigFilePerm); err != nil {
			return fmt.Errorf("failed to initialize json file %s: %w", path, err)
		}
	}
	return nil
}
