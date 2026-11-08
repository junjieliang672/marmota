package internal

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
	"github.com/junjieliang672/marmota/src/internal/utils"
)

func TestModel_Update_Prompt(t *testing.T) {
	curTestDir := filepath.Join(testDir, "TestPrompt")
	dir1 := filepath.Join(curTestDir, "dir1")
	dir2 := filepath.Join(curTestDir, "dir2")
	file1 := filepath.Join(dir1, "file1.txt")

	utils.SetupDirectories(t, curTestDir, dir1, dir2)
	utils.SetupFiles(t, file1)
	t.Cleanup(func() {
		os.RemoveAll(curTestDir)
	})

	// We want to test these. TODO : complete important tests
	// 1. Being able to open prompt
	// 1a. Open in shell mode, 1b. Open in prompt mode 1c. Switching between then

	// 2. Being able to execute shell commands
	// 3. Shell command failure is handled and prompt stays open
	// 4. Agent mode only starts claude or codex; anything else is an error
	// 10. Model closing
	// 10a. Pressing escape or ctrl+c and model closes
	// 10b. Autoclose based on config

	// Dont test shell command substitution here.

	// We might want to wrap os command execution in an interface and
	// ? Use a mock os command executor to have timeouts, and
	// custom command behaviour

	// Other tests cases
	// -- UI
	// 1. Entire model's rendering with promptModel open/closed
	// 2. Rendering not breaking when user pastes/enter special character or too much text
	// 3. Prompt gets resized based on total screen size. And always fits in

	// -- Functionality
	// 1. Shell command Timeout. Testing timeout is a pain. We should use async, and configure low timeout
	// like 1 sec for testing
	// 2. In case we plan to show output, we need to test case of
	// too big Shell command output

	testBasicPromptFunctionality(t, dir1)
	testAgentModeOnlyStartsAgents(t, dir1)
	testShellCommandsWithQuotes(t, curTestDir, dir1)
}

// testBasicPromptFunctionality tests opening, closing and basic command execution
func testBasicPromptFunctionality(t *testing.T, dir1 string) {
	t.Run("Basic Prompt Opening", func(t *testing.T) {
		m := defaultTestModel(dir1)
		assert.False(t, m.promptModal.IsOpen())
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenCommandLine[0]))
		assert.True(t, m.promptModal.IsOpen())
		assert.True(t, m.promptModal.IsShellMode())

		// Switching between modes
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenAgentPrompt[0]))
		assert.False(t, m.promptModal.IsShellMode(), "Pressing prompt key should switch to prompt mode")
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenCommandLine[0]))
		assert.True(t, m.promptModal.IsShellMode(), "Pressing shell key should switch to shell mode")

		// Closing and opening in prompt mode
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.CancelTyping[0]))
		assert.False(t, m.promptModal.IsOpen())
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenAgentPrompt[0]))
		assert.True(t, m.promptModal.IsOpen())
		assert.False(t, m.promptModal.IsShellMode())
	})

	t.Run("Shell command execution", func(t *testing.T) {
		m := defaultTestModel(dir1)

		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenCommandLine[0]))
		// Prefer cross platform command
		TeaUpdate(m, utils.TeaRuneKeyMsg("mkdir test_dir"))
		TeaUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
		assert.True(t, m.promptModal.LastActionSucceeded())
		assert.DirExists(t, filepath.Join(dir1, "test_dir"))

		// Invalid command shouldn't cause issues.
		TeaUpdate(m, utils.TeaRuneKeyMsg("xyz_non_exisiting_command"))
		TeaUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
		assert.False(t, m.promptModal.LastActionSucceeded())
		assert.True(t, m.promptModal.IsOpen())
	})

	t.Run("Model closing", func(t *testing.T) {
		m := defaultTestModel(dir1)
		for _, key := range common.Hotkeys.CancelTyping {
			TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenAgentPrompt[0]))
			assert.True(t, m.promptModal.IsOpen())
			TeaUpdate(m, utils.TeaRuneKeyMsg(key))
			assert.False(t, m.promptModal.IsOpen(), "Prompt should get closed")
		}
	})
}

// testAgentModeOnlyStartsAgents checks that the commands Agent mode no longer has
// fail and leave the prompt open.
func testAgentModeOnlyStartsAgents(t *testing.T, dir1 string) {
	t.Run("Agent mode only starts agents", func(t *testing.T) {
		m := defaultTestModel(dir1)
		TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenAgentPrompt[0]))
		for _, command := range []string{"split", "cd ..", "open .", "coco", "ttadk", "agent"} {
			TeaUpdate(m, utils.TeaRuneKeyMsg(command))
			TeaUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
			assert.False(t, m.promptModal.LastActionSucceeded(), command)
			assert.True(t, m.promptModal.IsOpen(), command)
		}
		assert.Len(t, m.fileModel.filePanels, 1)
		assert.Equal(t, dir1, m.getFocusedFilePanel().location)
	})
}

// testShellCommandsWithQuotes tests shell command execution with quoted arguments
func testShellCommandsWithQuotes(t *testing.T, curTestDir, dir1 string) {
	t.Run("Shell command with quotes", func(t *testing.T) {
		dirWithSpaces := filepath.Join(curTestDir, "test dir with spaces")
		utils.SetupDirectories(t, dirWithSpaces)

		t.Run("shell command with double quotes", func(t *testing.T) {
			m := defaultTestModel(dir1)
			TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenCommandLine[0]))

			TeaUpdate(m, utils.TeaRuneKeyMsg(`mkdir "`+filepath.Join(dir1, "new dir with spaces")+`"`))
			TeaUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
			assert.True(t, m.promptModal.LastActionSucceeded(), "shell command with quotes should work")
			assert.DirExists(t, filepath.Join(dir1, "new dir with spaces"))
		})

		t.Run("shell command with single quotes", func(t *testing.T) {
			m := defaultTestModel(dir1)
			TeaUpdate(m, utils.TeaKeyMsg(common.Hotkeys.OpenCommandLine[0]))

			TeaUpdate(m, utils.TeaRuneKeyMsg(`mkdir '`+filepath.Join(dir1, "another dir with spaces")+`'`))
			TeaUpdate(m, tea.KeyMsg{Type: tea.KeyEnter})
			assert.True(t, m.promptModal.LastActionSucceeded(), "shell command with single quotes should work")
			assert.DirExists(t, filepath.Join(dir1, "another dir with spaces"))
		})
	})
}
