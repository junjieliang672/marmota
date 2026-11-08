package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFillMissingHotkeysFromDefaults_MigratesMetadataMoveCollision(t *testing.T) {
	originalHotkeys := Hotkeys
	originalHotkeysToml := HotkeysTomlString
	defer func() {
		Hotkeys = originalHotkeys
		HotkeysTomlString = originalHotkeysToml
	}()

	defaultHotkeys, err := os.ReadFile(filepath.Join("..", "..", "superfile_config", "hotkeys.toml"))
	require.NoError(t, err)
	HotkeysTomlString = string(defaultHotkeys)

	Hotkeys = HotkeysType{
		FocusOnMetaData: []string{"m", ""},
		MovePinnedItem:  []string{"m", ""},
	}

	fillMissingHotkeysFromDefaults()

	assert.Equal(t, "M", Hotkeys.FocusOnMetaData[0])
	assert.Equal(t, "m", Hotkeys.MovePinnedItem[0])
}

// The Ask bar's key moved from ctrl+k to alt+k: a file that still has the
// old default gets the new one, and --fix-hotkeys writes it.
func TestTheOldAskBarKeysBecomeK(t *testing.T) {
	originalHotkeys := Hotkeys
	originalHotkeysToml := HotkeysTomlString
	defer func() {
		Hotkeys = originalHotkeys
		HotkeysTomlString = originalHotkeysToml
	}()
	defaultHotkeys, err := os.ReadFile(filepath.Join("..", "..", "superfile_config", "hotkeys.toml"))
	require.NoError(t, err)
	HotkeysTomlString = string(defaultHotkeys)

	for _, old := range []string{"ctrl+k", "alt+k"} {
		Hotkeys = HotkeysType{OpenAskBar: []string{old, ""}}
		fillMissingHotkeysFromDefaults()
		assert.Equal(t, []string{"K", "ctrl+k"}, Hotkeys.OpenAskBar, old)
	}

	Hotkeys = HotkeysType{OpenAskBar: []string{"ctrl+y", ""}}
	fillMissingHotkeysFromDefaults()
	assert.Equal(t, []string{"ctrl+y", ""}, Hotkeys.OpenAskBar, "a key you chose stays")

	file := filepath.Join(t.TempDir(), "hotkeys.toml")
	for _, old := range []string{"ctrl+k", "alt+k"} {
		require.NoError(t, os.WriteFile(file, []byte("quit = ['q', '']\nopen_ask_bar = ['"+old+"', '']\n"), 0o600))
		fixOldAskBarKey(file)
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Equal(t, "quit = ['q', '']\nopen_ask_bar = ['K', 'ctrl+k']\n", string(data), old)
	}
}
