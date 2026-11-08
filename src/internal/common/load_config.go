package common

import (
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"

	"github.com/charmbracelet/x/ansi"
	"github.com/pelletier/go-toml/v2"

	"github.com/junjieliang672/marmota/src/config/icon"
	"github.com/junjieliang672/marmota/src/internal/utils"

	variable "github.com/junjieliang672/marmota/src/config"
)

// Load configurations from the configuration file. Compares the content
// with the default values and modify the config file to include default configs
// if the FixConfigFile flag is on
// TODO : Fix the code duplication with LoadHotkeysFile().
func LoadConfigFile() {
	err := utils.LoadTomlFile(variable.ConfigFile, ConfigTomlString, &Config, variable.FixConfigFile, false)
	if err != nil {
		userMsg := fmt.Sprintf("%s%s", LipglossError, err.Error())

		toExit := true
		var loadError *utils.TomlLoadError
		if errors.As(err, &loadError) && loadError != nil {
			if loadError.MissingFields() && !variable.FixConfigFile {
				// Had missing fields and we did not fix
				userMsg += "\nTo add missing fields to configuration file automatically run Marmota " +
					"with the --fix-config-file flag `spf --fix-config-file`"
			}
			toExit = loadError.IsFatal()
		}
		if toExit {
			utils.PrintfAndExitf("%s\n", userMsg)
		} else {
			fmt.Println(userMsg)
		}
	}

	// Even if there is a missing field, we want to validate fields that are present
	if err := ValidateConfig(&Config); err != nil {
		// If config is incorrect we cannot continue. We need to exit
		utils.PrintlnAndExit(err.Error())
	}
}

func ValidateConfig(c *ConfigType) error {
	if (c.FilePreviewWidth > 10 || c.FilePreviewWidth < 2) && c.FilePreviewWidth != 0 {
		return errors.New(LoadConfigError("file_preview_width"))
	}

	if c.DefaultSortType < 0 || c.DefaultSortType > 3 {
		return errors.New(LoadConfigError("default_sort_type"))
	}

	if ansi.StringWidth(c.BorderTop) != 1 {
		return errors.New(LoadConfigError("border_top"))
	}
	if ansi.StringWidth(c.BorderBottom) != 1 {
		return errors.New(LoadConfigError("border_bottom"))
	}
	if ansi.StringWidth(c.BorderLeft) != 1 {
		return errors.New(LoadConfigError("border_left"))
	}
	if ansi.StringWidth(c.BorderRight) != 1 {
		return errors.New(LoadConfigError("border_right"))
	}
	if ansi.StringWidth(c.BorderBottomLeft) != 1 {
		return errors.New(LoadConfigError("border_bottom_left"))
	}
	if ansi.StringWidth(c.BorderBottomRight) != 1 {
		return errors.New(LoadConfigError("border_bottom_right"))
	}
	if ansi.StringWidth(c.BorderTopLeft) != 1 {
		return errors.New(LoadConfigError("border_top_left"))
	}
	if ansi.StringWidth(c.BorderTopRight) != 1 {
		return errors.New(LoadConfigError("border_top_right"))
	}
	if ansi.StringWidth(c.BorderMiddleLeft) != 1 {
		return errors.New(LoadConfigError("border_middle_left"))
	}
	if ansi.StringWidth(c.BorderMiddleRight) != 1 {
		return errors.New(LoadConfigError("border_middle_right"))
	}

	return nil
}

// Load keybinds from the hotkeys file. Compares the content
// with the default values and modify the hotkeys if the FixHotkeys flag is on.
func LoadHotkeysFile(ignoreMissingFields bool) {
	err := utils.LoadTomlFile(
		variable.HotkeysFile,
		HotkeysTomlString,
		&Hotkeys,
		variable.FixHotkeys,
		ignoreMissingFields,
	)
	if err != nil {
		reportHotkeysLoadError(err)
	}

	fillMissingHotkeysFromDefaults()
	if variable.FixHotkeys {
		fixOldAskBarKey(variable.HotkeysFile)
	}
	validateHotkeys()
}

// reportHotkeysLoadError tells the user what loading the hotkeys file got
// wrong, and exits when superfile can't go on.
func reportHotkeysLoadError(err error) {
	var loadError *utils.TomlLoadError
	isLoadError := errors.As(err, &loadError)
	if isLoadError && loadError.MissingFields() && !variable.FixHotkeys {
		// User hotkey files are initialized from defaults before decode, so missing
		// fields already retain safe default values. Keep backwards compatibility
		// without forcing immediate manual fixes for newly added hotkeys.
		return
	}
	userMsg := fmt.Sprintf("%s%s", LipglossError, err.Error())
	toExit := true
	if isLoadError {
		toExit = loadError.IsFatal()
	}
	if toExit {
		utils.PrintfAndExitf("%s\n", userMsg)
	} else {
		fmt.Println(userMsg)
	}
}

// validateHotkeys exits when a hotkey has no key.
func validateHotkeys() {
	val := reflect.ValueOf(Hotkeys)
	for i := range val.NumField() {
		field := val.Type().Field(i)
		value := val.Field(i)

		// Although this is redundant as Hotkey is always a slice
		// This adds a layer against accidental struct modifications
		// Makes sure its always be a string slice. It's somewhat like a unit test
		if value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.String {
			utils.PrintlnAndExit(LoadHotkeysError(field.Name))
		}

		hotkeysList, ok := value.Interface().([]string)
		if !ok || len(hotkeysList) == 0 || hotkeysList[0] == "" {
			utils.PrintlnAndExit(LoadHotkeysError(field.Name))
		}
	}
}

func fillMissingHotkeysFromDefaults() {
	var defaultHotkeys HotkeysType
	if err := toml.Unmarshal([]byte(HotkeysTomlString), &defaultHotkeys); err != nil {
		return
	}

	currentVal := reflect.ValueOf(&Hotkeys).Elem()
	defaultVal := reflect.ValueOf(defaultHotkeys)
	for i := range currentVal.NumField() {
		curField := currentVal.Field(i)
		defField := defaultVal.Field(i)
		if curField.Len() == 0 {
			curField.Set(defField)
			continue
		}
		curSlice, ok := curField.Interface().([]string)
		if !ok {
			continue
		}
		if len(curSlice) == 0 || curSlice[0] == "" {
			curField.Set(defField)
		}
	}

	// Compatibility migration: older user hotkey files commonly have
	// focus_on_metadata = ['m', '']. Since move_pinned_item also defaults to
	// 'm', preserving both would make the new sidebar move action unreachable.
	// Only rewrite metadata when the collision matches the historical default.
	if len(Hotkeys.FocusOnMetaData) > 0 && len(Hotkeys.MovePinnedItem) > 0 &&
		Hotkeys.FocusOnMetaData[0] == "m" && Hotkeys.MovePinnedItem[0] == "m" {
		Hotkeys.FocusOnMetaData = defaultHotkeys.FocusOnMetaData
	}

	// The Ask bar's key was ctrl+k until 2026-11-05, then alt+k until
	// 2026-11-07, when it became K (ctrl+k second). A file that still has an
	// old default gets the new one.
	for _, old := range oldAskBarKeys {
		if slices.Equal(Hotkeys.OpenAskBar, old) {
			Hotkeys.OpenAskBar = defaultHotkeys.OpenAskBar
		}
	}
}

// oldAskBarKeys are open_ask_bar's earlier defaults.
var oldAskBarKeys = [][]string{{"ctrl+k", ""}, {"alt+k", ""}} //nolint:gochecknoglobals // fixed old defaults

var oldAskBarLine = regexp.MustCompile(`(?m)^open_ask_bar\s*=\s*\[\s*'(ctrl|alt)\+k'\s*,\s*''\s*\]`)

// fixOldAskBarKey writes the new Ask bar key into a hotkeys file that still
// has the old default, so the file says what superfile does.
func fixOldAskBarKey(path string) {
	data, err := os.ReadFile(path)
	if err != nil || !oldAskBarLine.Match(data) {
		return
	}
	fixed := oldAskBarLine.ReplaceAll(data, []byte("open_ask_bar = ['K', 'ctrl+k']"))
	//nolint:gosec // the hotkeys file superfile was started with
	if err := os.WriteFile(path, fixed, utils.ConfigFilePerm); err != nil {
		slog.Error("Could not write the new Ask bar key to the hotkeys file", "path", path, "error", err)
	}
}

// LoadThemeFile : Load configurations from theme file into &theme
// set default values if we cant read user's theme file
func LoadThemeFile() {
	themeFile := filepath.Join(variable.ThemeFolder, Config.Theme+".toml")
	data, err := os.ReadFile(themeFile)
	if err == nil {
		unmarshalErr := toml.Unmarshal(data, &Theme)
		if unmarshalErr == nil {
			return
		}
		slog.Error("Could not unmarshal theme file. Falling back to default theme",
			"unmarshalErr", unmarshalErr)
	} else {
		slog.Error("Could not read user's theme file. Falling back to default theme", "path", themeFile, "error", err)
	}

	err = toml.Unmarshal([]byte(DefaultThemeString), &Theme)
	if err != nil {
		utils.PrintfAndExitf("Unexpected error while reading default theme file : %v. Exiting...", err)
	}
}

// LoadAllDefaultConfig : Load all default configurations from embedded superfile_config folder into global
// configurations variables and write theme files if its needed.
func LoadAllDefaultConfig(content embed.FS) {
	err := LoadConfigStringGlobals(content)
	if err != nil {
		slog.Error("Could not load default config from embed FS", "error", err)
		return
	}

	currentThemeVersion, err := os.ReadFile(variable.ThemeFileVersion)
	if err != nil && !os.IsNotExist(err) {
		slog.Error("Unexpected error reading from file:", "error", err)
		return
	}

	if string(currentThemeVersion) == variable.CurrentVersion {
		// We don't need to update themes as its already up to date
		return
	}

	// Write theme files to theme directory
	err = WriteThemeFiles(content)
	if err != nil {
		slog.Error("Error while writing default theme directories", "error", err)
		return
	}

	// Prevent failure for first time app run by making sure parent directories exists
	if err = os.MkdirAll(filepath.Dir(variable.ThemeFileVersion), utils.ConfigDirPerm); err != nil {
		slog.Error("Error creating theme file parent directory", "error", err)
		return
	}

	err = os.WriteFile(variable.ThemeFileVersion, []byte(variable.CurrentVersion), utils.ConfigFilePerm)
	if err != nil {
		slog.Error("Error writing theme file version", "error", err)
	}
}

func LoadConfigStringGlobals(content embed.FS) error {
	hotkeyData, err := content.ReadFile(variable.EmbedHotkeysFile)
	if err != nil {
		return err
	}
	HotkeysTomlString = string(hotkeyData)

	configData, err := content.ReadFile(variable.EmbedConfigFile)
	if err != nil {
		return err
	}
	ConfigTomlString = string(configData)

	themeData, err := content.ReadFile(variable.EmbedThemeCatppuccinFile)
	if err != nil {
		return err
	}
	DefaultThemeString = string(themeData)
	return nil
}

func WriteThemeFiles(content embed.FS) error {
	_, err := os.Stat(variable.ThemeFolder)

	if os.IsNotExist(err) {
		if err = os.MkdirAll(variable.ThemeFolder, utils.ConfigDirPerm); err != nil {
			slog.Error("Error creating theme directory", "error", err)
			return err
		}
	}

	files, err := content.ReadDir(variable.EmbedThemeDir)
	if err != nil {
		slog.Error("Error reading theme directory from embed", "error", err)
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		// This will not break in windows. This is a relative path for Embed FS. It uses "/" only
		src, err := content.ReadFile(variable.EmbedThemeDir + "/" + file.Name())
		if err != nil {
			slog.Error("Error reading theme file from embed", "error", err)
			return err
		}

		curThemeFile, err := os.Create(filepath.Join(variable.ThemeFolder, file.Name()))
		if err != nil {
			slog.Error("Error creating theme file from embed", "error", err)
			return err
		}
		defer curThemeFile.Close()
		_, err = curThemeFile.Write(src)
		if err != nil {
			slog.Error("Error writing theme file from embed", "error", err)
			return err
		}
	}
	return nil
}

// Used only in unit tests
// Populate config variables based on given file
func PopulateGlobalConfigs() error {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("failed to determine source file location")
	}

	// This is src/internal/common/load_config.go
	// we want src/superfile_config
	spfConfigDir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filename))),
		"superfile_config")

	configFilePath := filepath.Join(spfConfigDir, "config.toml")
	hotkeyFilePath := filepath.Join(spfConfigDir, "hotkeys.toml")
	themeFilePath := filepath.Join(spfConfigDir, "theme", "monokai.toml")

	err := PopulateConfigFromFile(configFilePath)
	if err != nil {
		return err
	}
	err = PopulateHotkeyFromFile(hotkeyFilePath)
	if err != nil {
		return err
	}
	err = PopulateThemeFromFile(themeFilePath)
	if err != nil {
		return err
	}

	// Populate fixed variables
	LoadInitialPrerenderedVariables()
	icon.InitIcon(Config.Nerdfont, Theme.DirectoryIconColor)
	LoadPrerenderedVariables()
	return nil
}

// No validation required
func populateFromFile(filePath string, target interface{}) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	err = toml.Unmarshal(data, target)
	if err != nil {
		return err
	}
	return nil
}

func PopulateConfigFromFile(configFilePath string) error {
	return populateFromFile(configFilePath, &Config)
}

func PopulateHotkeyFromFile(hotkeyFilePath string) error {
	return populateFromFile(hotkeyFilePath, &Hotkeys)
}

func PopulateThemeFromFile(themeFilePath string) error {
	return populateFromFile(themeFilePath, &Theme)
}

func InitTrash() bool {
	// Create trash directories
	if runtime.GOOS != utils.OsLinux {
		return true
	}
	err := utils.CreateDirectories(
		variable.LinuxTrashDirectory,
		variable.LinuxTrashDirectoryFiles,
		variable.LinuxTrashDirectoryInfo,
	)
	if err != nil {
		slog.Warn("Failed to initialize XDG trash; falling back to permanent delete",
			"error", err, "trashDir", variable.LinuxTrashDirectory)
		return false
	}
	return true
}
