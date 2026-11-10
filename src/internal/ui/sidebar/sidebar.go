package sidebar

import (
	variable "github.com/junjieliang672/marmota/src/config"
)

func (s *Model) TogglePinnedDirectory(dir string) error {
	return s.pinnedMgr.Toggle(dir)
}

func (s *Model) RecordRecentEdit(filePath string) error {
	if s.recentMgr == nil {
		return nil
	}
	return s.recentMgr.Add(filePath)
}

func (s *Model) RemoveRecentEdit(filePath string) error {
	if s.recentMgr == nil {
		return nil
	}
	return s.recentMgr.Remove(filePath)
}

// SetGitStatus keeps the git status of the focused panel's folder.
func (s *Model) SetGitStatus(status GitStatus) {
	s.gitStatus = status
}

// GetGitStatus returns the current git status
func (s *Model) GetGitStatus() GitStatus {
	return s.gitStatus
}

// ClearGitStatusCache clears the cached git status to force a refresh
func (s *Model) ClearGitStatusCache() {
	s.gitStatus.LastPath = ""
}

// New reads the pinned and recently edited lists from their files.
func New() Model {
	pinnedMgr := NewPinnedFileManager(variable.PinnedFile)
	recentMgr := NewRecentEditsManager(variable.RecentEditsFile)
	return Model{pinnedMgr: &pinnedMgr, recentMgr: &recentMgr}
}
