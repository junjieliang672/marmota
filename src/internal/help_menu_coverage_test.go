package internal

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/junjieliang672/marmota/src/internal/common"
)

// Every configurable hotkey must be discoverable from the help menu (?).
// Help entries hold the very slices from common.Hotkeys, so an entry belongs
// to a hotkey when it shares that slice's backing array.
func TestHelpMenuListsEveryHotkey(t *testing.T) {
	data := getHelpMenuData()
	listed := func(binding []string) bool {
		for _, entry := range data {
			if len(entry.hotkey) > 0 && len(binding) > 0 && &entry.hotkey[0] == &binding[0] {
				return true
			}
		}
		return false
	}

	hotkeys := reflect.ValueOf(common.Hotkeys)
	hotkeysType := hotkeys.Type()
	for i := range hotkeysType.NumField() {
		field := hotkeysType.Field(i)
		binding, ok := hotkeys.Field(i).Interface().([]string)
		if !ok || len(binding) == 0 || binding[0] == "" {
			continue
		}
		assert.True(t, listed(binding), "hotkey %s (%s = %v) is missing from the help menu",
			field.Name, field.Tag.Get("toml"), binding)
	}
}
