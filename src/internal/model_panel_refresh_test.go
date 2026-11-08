package internal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestShouldSkipPanelUpdate_FocusedPanelUsesMinimumRefreshInterval(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	m := defaultTestModel(root)
	panel := m.getFocusedFilePanel()
	panel.element = make([]element, 91)
	panel.element[0].location = filepath.Join(root, "file00.txt")
	panel.lastTimeGetElement = time.Now()

	assert.True(t, m.shouldSkipPanelUpdate(panel, panel, panel.lastTimeGetElement.Add(500*time.Millisecond)))
	assert.False(t, m.shouldSkipPanelUpdate(panel, panel, panel.lastTimeGetElement.Add(1100*time.Millisecond)))
}

func TestShouldSkipPanelUpdate_FocusedPanelKeepsLargerDirectoryThrottle(t *testing.T) {
	tempDir := t.TempDir()
	root := filepath.Join(tempDir, "root")
	utils.SetupDirectories(t, root)

	m := defaultTestModel(root)
	panel := m.getFocusedFilePanel()
	panel.element = make([]element, 250)
	panel.element[0].location = filepath.Join(root, "file00.txt")
	panel.lastTimeGetElement = time.Now()

	assert.True(t, m.shouldSkipPanelUpdate(panel, panel, panel.lastTimeGetElement.Add(1500*time.Millisecond)))
	assert.False(t, m.shouldSkipPanelUpdate(panel, panel, panel.lastTimeGetElement.Add(2100*time.Millisecond)))
}
