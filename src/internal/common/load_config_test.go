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

	defaultHotkeys, err := os.ReadFile(filepath.Join("..", "..", "marmota_config", "hotkeys.toml"))
	require.NoError(t, err)
	HotkeysTomlString = string(defaultHotkeys)

	Hotkeys = HotkeysType{
		FocusOnMetaData: []string{"m", ""},
	}

	fillMissingHotkeysFromDefaults()

	assert.Equal(t, "M", Hotkeys.FocusOnMetaData[0])
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
	defaultHotkeys, err := os.ReadFile(filepath.Join("..", "..", "marmota_config", "hotkeys.toml"))
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
		assert.True(t, fixOldKeys(file))
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Equal(t, "quit = ['q', '']\nopen_ask_bar = ['K', 'ctrl+k']\n", string(data), old)
	}
}

// ! went to the session that needs you and s or z opened the go to list:
// now s goes to the session, z opens the list, and ! is no key. A file that
// still has those defaults gets the new ones, and --fix-hotkeys writes them.
func TestTheOldSessionAndGoToKeysBecomeSAndZ(t *testing.T) {
	originalHotkeys := Hotkeys
	originalHotkeysToml := HotkeysTomlString
	defer func() {
		Hotkeys = originalHotkeys
		HotkeysTomlString = originalHotkeysToml
	}()
	defaultHotkeys, err := os.ReadFile(filepath.Join("..", "..", "marmota_config", "hotkeys.toml"))
	require.NoError(t, err)
	HotkeysTomlString = string(defaultHotkeys)

	Hotkeys = HotkeysType{JumpToUnreadSession: []string{"!", ""}, GoTo: []string{"s", "z"}}
	fillMissingHotkeysFromDefaults()
	assert.Equal(t, []string{"s", ""}, Hotkeys.JumpToUnreadSession)
	assert.Equal(t, []string{"z", ""}, Hotkeys.GoTo)

	// The Vim keys' file: its list opens on ctrl+s, which stays.
	Hotkeys = HotkeysType{JumpToUnreadSession: []string{"!", ""}, GoTo: []string{"ctrl+s", ""}}
	fillMissingHotkeysFromDefaults()
	assert.Equal(t, []string{"s", ""}, Hotkeys.JumpToUnreadSession)
	assert.Equal(t, []string{"ctrl+s", ""}, Hotkeys.GoTo)

	Hotkeys = HotkeysType{JumpToUnreadSession: []string{"J", ""}, GoTo: []string{"g", "z"}}
	fillMissingHotkeysFromDefaults()
	assert.Equal(t, []string{"J", ""}, Hotkeys.JumpToUnreadSession, "a key you chose stays")
	assert.Equal(t, []string{"g", "z"}, Hotkeys.GoTo, "a key you chose stays")

	file := filepath.Join(t.TempDir(), "hotkeys.toml")
	require.NoError(t, os.WriteFile(file,
		[]byte("go_to = ['s', 'z']\njump_to_unread_session = ['!', '']\nquit = ['q', '']\n"), 0o600))
	assert.True(t, fixOldKeys(file))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "go_to = ['z', '']\njump_to_unread_session = ['s', '']\nquit = ['q', '']\n", string(data))
	assert.False(t, fixOldKeys(file), "nothing old left: the file isn't written again")

	// No default says ! any more, in either file of keys.
	for _, name := range []string{"hotkeys.toml", "vimHotkeys.toml"} {
		keys, err := os.ReadFile(filepath.Join("..", "..", "marmota_config", name))
		require.NoError(t, err)
		assert.NotContains(t, string(keys), "'!'", name)
	}
}
