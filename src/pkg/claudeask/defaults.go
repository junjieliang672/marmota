package claudeask

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Defaults are the model and effort Claude Code uses when the Ask box keeps
// its own choice: those set in its settings.json, "" when it sets none.
func Defaults(configDir string) (string, string) {
	data, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		return "", ""
	}
	var settings struct {
		Model       string `json:"model"`
		EffortLevel string `json:"effortLevel"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return "", ""
	}
	return settings.Model, settings.EffortLevel
}

// modelDate is the date some model ids end with: claude-sonnet-4-5-20250929.
var modelDate = regexp.MustCompile(`-\d{8}$`)

// modelVersion is a model id's version: opus-5-5 is Opus 5.5.
var modelVersion = regexp.MustCompile(`^([a-z]+)-(\d+)-(\d+)$`)

// ModelLabel is a model as the Ask box names it: "opus 5.5" for
// claude-opus-5-5, an alias such as "sonnet" as it is.
func ModelLabel(model string) string {
	name := modelDate.ReplaceAllString(strings.TrimPrefix(model, "claude-"), "")
	if m := modelVersion.FindStringSubmatch(name); m != nil {
		return m[1] + " " + m[2] + "." + m[3]
	}
	return name
}
