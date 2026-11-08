package codexsession

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Defaults are the model and effort Codex uses when the Ask box keeps its
// own choice: those set in config.toml, "" when it sets none.
func Defaults(home string) (string, string) {
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return "", ""
	}
	var cfg struct {
		Model  string `toml:"model"`
		Effort string `toml:"model_reasoning_effort"`
	}
	if toml.Unmarshal(data, &cfg) != nil {
		return "", ""
	}
	return cfg.Model, cfg.Effort
}

// Model is a model Codex offers, with the efforts it takes.
type Model struct {
	ID      string
	Efforts []string
	// DefaultEffort is the effort Codex gives it when none is set.
	DefaultEffort string
}

//nolint:gochecknoglobals // the last list read, kept while its file is unchanged
var modelsCache struct {
	mu      sync.Mutex
	path    string
	size    int64
	modTime time.Time
	models  []Model
}

// Models are the models Codex's own picker offers, in its order: those
// listed in models_cache.json, which Codex keeps; nil when there is none.
// The file is read again only once it has changed.
func Models(home string) []Model {
	path := filepath.Join(home, "models_cache.json")
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	c := &modelsCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == path && c.size == info.Size() && c.modTime.Equal(info.ModTime()) {
		return c.models
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cache struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
			Levels     []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
			DefaultLevel string `json:"default_reasoning_level"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	sort.SliceStable(cache.Models, func(i, j int) bool { return cache.Models[i].Priority < cache.Models[j].Priority })
	var models []Model
	for _, m := range cache.Models {
		if m.Slug == "" || m.Visibility != "list" {
			continue
		}
		model := Model{ID: m.Slug, DefaultEffort: m.DefaultLevel}
		for _, l := range m.Levels {
			if l.Effort != "" {
				model.Efforts = append(model.Efforts, l.Effort)
			}
		}
		models = append(models, model)
	}
	c.path, c.size, c.modTime, c.models = path, info.Size(), info.ModTime(), models
	return models
}

// LaunchArgs are the flags of a Codex session superfile starts without the
// Ask box's choices (>codex, resume, fork): full access, asking when Codex
// thinks it should.
func LaunchArgs() []string {
	return []string{"--sandbox", "danger-full-access", "--ask-for-approval", "on-request"}
}

// A Codex window started with --config (-c) or --approve-for-me runs its
// session on a server of its own rather than Codex's shared one, where
// superfile can't see it. Those settings go with the first prompt instead
// (TurnOptions); the flags here keep a session on the shared server.

// PermissionArgs are the flags for one of the Ask box's permission modes
// (claudeask.PermissionModes), Codex's nearest to Claude Code's. auto is
// the workspace-write sandbox with Codex reviewing the approval requests,
// which PermissionReviewer gives.
func PermissionArgs(mode string) []string {
	switch mode {
	case "acceptEdits", "auto":
		return []string{"--sandbox", "workspace-write", "--ask-for-approval", "on-request"}
	case "plan", "manual":
		return []string{"--sandbox", "read-only", "--ask-for-approval", "on-request"}
	default: // bypassPermissions
		return []string{"--dangerously-bypass-approvals-and-sandbox"}
	}
}

// PermissionReviewer is who reviews a permission mode's approval requests,
// for TurnOptions: Codex's automatic review for auto, else "" (you).
func PermissionReviewer(mode string) string {
	if mode == "auto" {
		return "auto_review"
	}
	return ""
}

// ModelArgs are the flags for a model; "" keeps Codex's.
func ModelArgs(model string) []string {
	if model == "" {
		return nil
	}
	return []string{"--model", model}
}

// appBundleCodex is the codex the ChatGPT app ships, kept up to date by it.
const appBundleCodex = "/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex"

const versionTimeout = 3 * time.Second

//nolint:gochecknoglobals // found once a run
var foundBinary struct {
	once sync.Once
	path string
}

// Binary is the codex to run: the newest that runs of those on PATH and the
// ChatGPT app's, "codex" when none answers. An old one would start
// sessions superfile can't see (before Codex's server), and an install
// can be broken, so each is asked its version. It is looked for once.
func Binary() string {
	foundBinary.once.Do(func() {
		foundBinary.path = newestBinary(candidates(os.Getenv("PATH")), versionOf)
	})
	return foundBinary.path
}

func candidates(pathEnv string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir != "" {
			add(filepath.Join(dir, "codex"))
		}
	}
	add(appBundleCodex)
	return out
}

// versionRE finds the version in `codex --version`: "codex-cli 0.160.0".
var versionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// versionOf asks a codex its version, as three numbers; nil when it doesn't
// run.
func versionOf(path string) []int {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return nil
	}
	return parseVersion(string(out))
}

func parseVersion(s string) []int {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	v := make([]int, 3) //nolint:mnd // major, minor, patch
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v
}

func newestBinary(paths []string, version func(string) []int) string {
	best, bestV := "codex", []int(nil)
	for _, p := range paths {
		v := version(p)
		if v != nil && (bestV == nil || newer(v, bestV)) {
			best, bestV = p, v
		}
	}
	return best
}

func newer(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
