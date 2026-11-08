package sidebar

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

const maxRecentEditedFiles = 10

type RecentEditsManager struct {
	filePath string
}

func NewRecentEditsManager(filePath string) RecentEditsManager {
	return RecentEditsManager{
		filePath: filePath,
	}
}

func (mgr *RecentEditsManager) Load() []directory {
	directories := []directory{}

	jsonData, err := os.ReadFile(mgr.filePath)
	if os.IsNotExist(err) {
		// Nothing edited yet. This runs on every refresh, so it stays quiet.
		return directories
	}
	if err != nil {
		slog.Error("Error reading recent edits file", "error", err)
		return directories
	}

	if err := json.Unmarshal(jsonData, &directories); err != nil {
		slog.Error("Error parsing recent edits data", "error", err)
		return directories
	}

	cleanedDirs := mgr.Clean(directories)
	return sortRecentEditedDirectories(cleanedDirs)
}

func (mgr *RecentEditsManager) Save(dirs []directory) error {
	dirs = sortRecentEditedDirectories(append([]directory(nil), dirs...))
	if len(dirs) > maxRecentEditedFiles {
		dirs = dirs[:maxRecentEditedFiles]
	}

	data, err := json.Marshal(dirs)
	if err != nil {
		return fmt.Errorf("error marshaling recent edits: %w", err)
	}

	if err := os.WriteFile(mgr.filePath, data, utils.ConfigFilePerm); err != nil {
		return fmt.Errorf("error writing recent edits file: %w", err)
	}

	return nil
}

func (mgr *RecentEditsManager) Add(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("error stating recent edit file: %w", err)
	}
	if info.IsDir() {
		return nil
	}

	dirs := mgr.Load()
	filtered := dirs[:0]
	for _, dir := range dirs {
		if dir.Location != filePath {
			filtered = append(filtered, dir)
		}
	}

	filtered = append(filtered, directory{
		Location: filePath,
		Name:     filepath.Base(filePath),
		EditedAt: time.Now().UnixNano(),
	})

	if err := mgr.Save(filtered); err != nil {
		return fmt.Errorf("error saving recent edits: %w", err)
	}

	return nil
}

func (mgr *RecentEditsManager) Remove(filePath string) error {
	dirs := mgr.Load()
	filtered := dirs[:0]
	for _, dir := range dirs {
		if dir.Location != filePath {
			filtered = append(filtered, dir)
		}
	}

	if err := mgr.Save(filtered); err != nil {
		return fmt.Errorf("error saving recent edits: %w", err)
	}

	return nil
}

func (mgr *RecentEditsManager) Clean(dirs []directory) []directory {
	cleanedDirs := make([]directory, 0, len(dirs))
	for _, dir := range dirs {
		info, err := os.Stat(dir.Location)
		if err == nil && !info.IsDir() {
			cleanedDirs = append(cleanedDirs, dir)
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			slog.Warn("error while checking recent edit file", "path", dir.Location, "error", err)
		}
	}

	if len(cleanedDirs) == len(dirs) {
		return cleanedDirs
	}

	if err := mgr.Save(cleanedDirs); err != nil {
		slog.Error("error saving recent edits", "error", err)
	}

	return cleanedDirs
}

func sortRecentEditedDirectories(dirs []directory) []directory {
	sort.SliceStable(dirs, func(i, j int) bool {
		return dirs[i].EditedAt > dirs[j].EditedAt
	})
	return dirs
}
