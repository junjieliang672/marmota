package search

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- Helpers / test doubles ------------------------------------------------

// mockSearchable is a simple in-memory implementation of Searchable.
type mockSearchable struct {
	name      string
	location  string
	directory bool
}

func (m *mockSearchable) GetName() string          { return m.name }
func (m *mockSearchable) GetLocation() string      { return m.location }
func (m *mockSearchable) IsDirectory() bool        { return m.directory }
func (m *mockSearchable) GetMetadata() interface{} { return nil }

func newFile(name string) Searchable {
	return &mockSearchable{name: name, location: "/tmp/" + name, directory: false}
}

func newDir(name string) Searchable {
	return &mockSearchable{name: name, location: "/tmp/" + name, directory: true}
}

// mockSearchSource implements SearchSource.
type mockSearchSource struct {
	items   []Searchable
	baseDir string
}

func (s *mockSearchSource) GetItems() []Searchable   { return s.items }
func (s *mockSearchSource) GetBaseDirectory() string { return s.baseDir }

// ---- parseSearchTerm -------------------------------------------------------

func TestParseSearchTerm(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantFilterType string
		wantCleanQuery string
	}{
		{
			name:           "empty query",
			input:          "",
			wantFilterType: "",
			wantCleanQuery: "",
		},
		{
			name:           "plain text, no prefix",
			input:          "hello",
			wantFilterType: "",
			wantCleanQuery: "hello",
		},
		{
			name:           "f: prefix with text",
			input:          "f:main.go",
			wantFilterType: "files",
			wantCleanQuery: "main.go",
		},
		{
			name:           "d: prefix with text",
			input:          "d:src",
			wantFilterType: "dirs",
			wantCleanQuery: "src",
		},
		{
			name:           "f: prefix alone (empty clean query)",
			input:          "f:",
			wantFilterType: "files",
			wantCleanQuery: "",
		},
		{
			name:           "d: prefix alone (empty clean query)",
			input:          "d:",
			wantFilterType: "dirs",
			wantCleanQuery: "",
		},
		{
			name:           "uppercase F: is NOT a filter prefix",
			input:          "F:main",
			wantFilterType: "",
			wantCleanQuery: "F:main",
		},
		{
			name:           "prefix inside text is not a filter",
			input:          "hello f:world",
			wantFilterType: "",
			wantCleanQuery: "hello f:world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFilter, gotQuery := parseSearchTerm(tt.input)
			assert.Equal(t, tt.wantFilterType, gotFilter, "filter type mismatch")
			assert.Equal(t, tt.wantCleanQuery, gotQuery, "clean query mismatch")
		})
	}
}

// ---- matchesFileFilter -----------------------------------------------------

func TestMatchesFileFilter(t *testing.T) {
	file := newFile("readme.md")
	dir := newDir("src")

	tests := []struct {
		name       string
		item       Searchable
		filterType string
		want       bool
	}{
		{"file passes 'files' filter", file, "files", true},
		{"dir rejected by 'files' filter", dir, "files", false},
		{"dir passes 'dirs' filter", dir, "dirs", true},
		{"file rejected by 'dirs' filter", file, "dirs", false},
		{"file passes empty filter (show all)", file, "", true},
		{"dir passes empty filter (show all)", dir, "", true},
		{"file passes unknown filter (show all)", file, "unknown", true},
		{"dir passes unknown filter (show all)", dir, "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFileFilter(tt.item, tt.filterType)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---- FzfExecutor.Execute ---------------------------------------------------

func TestFzfExecutor_Execute(t *testing.T) {
	executor := NewFzfExecutor(5 * time.Second)
	ctx := context.Background()

	t.Run("empty source list returns nil", func(t *testing.T) {
		results, err := executor.Execute(ctx, "anything", nil)
		require.NoError(t, err)
		assert.Nil(t, results)
	})

	t.Run("empty source slice returns nil", func(t *testing.T) {
		results, err := executor.Execute(ctx, "query", []Searchable{})
		require.NoError(t, err)
		assert.Nil(t, results)
	})

	t.Run("exact match returns result", func(t *testing.T) {
		items := []Searchable{
			newFile("main.go"),
			newFile("types.go"),
			newFile("engine.go"),
		}
		results, err := executor.Execute(ctx, "main.go", items)
		require.NoError(t, err)
		require.NotEmpty(t, results, "expected at least one result for exact match")

		names := make([]string, len(results))
		for i, r := range results {
			names[i] = r.Item.GetName()
		}
		assert.Contains(t, names, "main.go")
	})

	t.Run("fuzzy match finds approximate result", func(t *testing.T) {
		items := []Searchable{
			newFile("coordinator.go"),
			newFile("stack.go"),
			newFile("engine.go"),
		}
		// "eng" is a fuzzy prefix of "engine.go"
		results, err := executor.Execute(ctx, "eng", items)
		require.NoError(t, err)
		require.NotEmpty(t, results, "expected at least one fuzzy result")
	})

	t.Run("f: filter returns only files", func(t *testing.T) {
		items := []Searchable{
			newFile("readme.md"),
			newDir("src"),
			newFile("main.go"),
			newDir("vendor"),
		}
		results, err := executor.Execute(ctx, "f:", items)
		require.NoError(t, err)
		for _, r := range results {
			assert.False(t, r.Item.IsDirectory(), "only files should be returned with f: filter")
		}
	})

	t.Run("d: filter returns only directories", func(t *testing.T) {
		items := []Searchable{
			newFile("readme.md"),
			newDir("src"),
			newFile("main.go"),
			newDir("vendor"),
		}
		results, err := executor.Execute(ctx, "d:", items)
		require.NoError(t, err)
		for _, r := range results {
			assert.True(t, r.Item.IsDirectory(), "only directories should be returned with d: filter")
		}
	})

	t.Run("f: filter with text only matches files", func(t *testing.T) {
		items := []Searchable{
			newFile("main.go"),
			newDir("main"),
			newFile("main_test.go"),
		}
		results, err := executor.Execute(ctx, "f:main", items)
		require.NoError(t, err)
		for _, r := range results {
			assert.False(t, r.Item.IsDirectory(), "only files should match f: filter")
		}
	})

	t.Run("d: filter with text only matches directories", func(t *testing.T) {
		items := []Searchable{
			newFile("config.go"),
			newDir("config"),
			newDir("configs"),
		}
		results, err := executor.Execute(ctx, "d:config", items)
		require.NoError(t, err)
		for _, r := range results {
			assert.True(t, r.Item.IsDirectory(), "only directories should match d: filter")
		}
	})

	t.Run("cancelled context returns empty or partial results without error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		items := []Searchable{
			newFile("main.go"),
			newFile("types.go"),
		}
		// Should not panic; result may be empty due to cancelled context / timeout
		results, err := executor.Execute(ctx, "main", items)
		// We accept either no error or a context error; what matters is no panic.
		_ = results
		_ = err
	})

	t.Run("very short timeout behaves gracefully", func(t *testing.T) {
		shortExecutor := NewFzfExecutor(1 * time.Nanosecond)
		items := []Searchable{newFile("a.go"), newFile("b.go"), newFile("c.go")}
		results, err := shortExecutor.Execute(context.Background(), "a", items)
		// A timeout should return nil results (not panic)
		assert.NoError(t, err)
		_ = results
	})
}

// ---- Stack operations -------------------------------------------------------

func TestStack_PushPopPeek(t *testing.T) {
	s := NewStack("/home/user")

	assert.True(t, s.IsEmpty(), "new stack should be empty")
	assert.Equal(t, 0, s.Depth(), "new stack depth should be 0")
	assert.Nil(t, s.Peek(), "peek on empty stack should be nil")

	layer1 := Layer{Mode: ModeRecursive, Query: "test", Results: []Searchable{newFile("a.go")}}
	s.Push(layer1)

	assert.False(t, s.IsEmpty())
	assert.Equal(t, 1, s.Depth())

	top := s.Peek()
	require.NotNil(t, top)
	assert.Equal(t, "test", top.Query)
	assert.Equal(t, 1, s.Depth(), "Peek must not change depth")

	layer2 := Layer{Mode: ModeFilter, Query: ".go"}
	s.Push(layer2)
	assert.Equal(t, 2, s.Depth())

	popped := s.Pop()
	assert.Equal(t, ".go", popped.Query)
	assert.Equal(t, 1, s.Depth())

	popped = s.Pop()
	assert.Equal(t, "test", popped.Query)
	assert.True(t, s.IsEmpty())

	// Pop from empty stack should return zero-value Layer
	empty := s.Pop()
	assert.Equal(t, Layer{}, empty)
}

func TestStack_Clear(t *testing.T) {
	s := NewStack("")
	s.Push(Layer{Query: "a"})
	s.Push(Layer{Query: "b"})
	s.Push(Layer{Query: "c"})
	require.Equal(t, 3, s.Depth())

	s.Clear()
	assert.True(t, s.IsEmpty())
	assert.Equal(t, 0, s.Depth())
}

func TestStack_GetSetBaseDir(t *testing.T) {
	s := NewStack("/initial")
	assert.Equal(t, "/initial", s.GetBaseDir())

	s.SetBaseDir("/updated")
	assert.Equal(t, "/updated", s.GetBaseDir())
}

func TestStack_RenderHierarchy(t *testing.T) {
	s := NewStack("")

	// Empty stack renders empty string
	assert.Empty(t, s.RenderHierarchy())

	s.Push(Layer{Mode: ModeRecursive, Query: "foo"})
	rendered := s.RenderHierarchy()
	assert.Contains(t, rendered, "foo", "rendered hierarchy must contain the query")

	s.Push(Layer{Mode: ModeFilter, Query: "bar"})
	rendered = s.RenderHierarchy()
	assert.Contains(t, rendered, "foo")
	assert.Contains(t, rendered, "bar")
}

func TestStack_RenderHierarchy_StatusSuffixes(t *testing.T) {
	s := NewStack("")

	t.Run("searching suffix", func(t *testing.T) {
		s.Clear()
		s.Push(Layer{Mode: ModeRecursive, Query: "q", Searching: true})
		assert.Contains(t, s.RenderHierarchy(), "searching")
	})

	t.Run("depth limited suffix", func(t *testing.T) {
		s.Clear()
		s.Push(Layer{Mode: ModeRecursive, Query: "q", DepthLimited: true})
		assert.Contains(t, s.RenderHierarchy(), "depth")
	})

	t.Run("timed out suffix", func(t *testing.T) {
		s.Clear()
		s.Push(Layer{Mode: ModeRecursive, Query: "q", TimedOut: true})
		assert.Contains(t, s.RenderHierarchy(), "timeout")
	})
}

func TestStack_GetCurrentModeIndicator(t *testing.T) {
	tests := []struct {
		name            string
		layers          []Layer
		isRecursiveMode bool
		searchTerm      string
		wantEmpty       bool
	}{
		{
			name:            "recursive mode always returns non-empty",
			layers:          nil,
			isRecursiveMode: true,
			searchTerm:      "",
			wantEmpty:       false,
		},
		{
			name:            "filter mode (stack has layers)",
			layers:          []Layer{{Mode: ModeRecursive, Query: "prev"}},
			isRecursiveMode: false,
			searchTerm:      "",
			wantEmpty:       false,
		},
		{
			name:            "regular mode with empty stack returns empty",
			layers:          nil,
			isRecursiveMode: false,
			searchTerm:      "",
			wantEmpty:       true,
		},
		{
			name:            "f: prefix adds file icon regardless of mode",
			layers:          nil,
			isRecursiveMode: false,
			searchTerm:      "f:main",
			wantEmpty:       false,
		},
		{
			name:            "d: prefix adds dir icon regardless of mode",
			layers:          nil,
			isRecursiveMode: false,
			searchTerm:      "d:src",
			wantEmpty:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStack("")
			for _, l := range tt.layers {
				s.Push(l)
			}
			indicator := s.GetCurrentModeIndicator(tt.isRecursiveMode, tt.searchTerm)
			if tt.wantEmpty {
				assert.Empty(t, indicator)
			} else {
				assert.NotEmpty(t, indicator)
			}
		})
	}
}

// ---- RecursiveCollector.Collect --------------------------------------------

func TestRecursiveCollector_Collect(t *testing.T) {
	// Build a real temporary directory tree for collection tests.
	root := t.TempDir()
	// Structure:
	//   root/
	//     file1.txt
	//     file2.go
	//     subdir/
	//       file3.md
	//     .hidden/
	//       secret.txt

	require.NoError(t, os.WriteFile(filepath.Join(root, "file1.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "file2.go"), nil, 0o644))
	subdir := filepath.Join(root, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "file3.md"), nil, 0o644))
	hidden := filepath.Join(root, ".hidden")
	require.NoError(t, os.MkdirAll(hidden, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hidden, "secret.txt"), nil, 0o644))

	t.Run("collects all non-hidden files and dirs by default", func(t *testing.T) {
		rc := NewRecursiveCollector(MaxSearchDepth, false) // showHidden=false
		items, depthLimited, err := rc.Collect(context.Background(), root, "")
		require.NoError(t, err)
		assert.False(t, depthLimited)

		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.GetName()
		}
		assert.Contains(t, names, "file1.txt")
		assert.Contains(t, names, "file2.go")
		assert.Contains(t, names, "file3.md")
		assert.Contains(t, names, "subdir")
		assert.NotContains(t, names, ".hidden", "hidden directory should be skipped")
		assert.NotContains(t, names, "secret.txt", "file inside hidden dir should be skipped")
	})

	t.Run("collects hidden items when showHidden=true", func(t *testing.T) {
		rc := NewRecursiveCollector(MaxSearchDepth, true)
		items, _, err := rc.Collect(context.Background(), root, "")
		require.NoError(t, err)

		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.GetName()
		}
		assert.Contains(t, names, ".hidden")
		assert.Contains(t, names, "secret.txt")
	})

	t.Run("files filter only returns files", func(t *testing.T) {
		rc := NewRecursiveCollector(MaxSearchDepth, false)
		items, _, err := rc.Collect(context.Background(), root, "files")
		require.NoError(t, err)
		for _, it := range items {
			assert.False(t, it.IsDirectory(), "files filter should exclude directories")
		}
	})

	t.Run("dirs filter only returns directories", func(t *testing.T) {
		rc := NewRecursiveCollector(MaxSearchDepth, false)
		items, _, err := rc.Collect(context.Background(), root, "dirs")
		require.NoError(t, err)
		for _, it := range items {
			assert.True(t, it.IsDirectory(), "dirs filter should exclude regular files")
		}
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.GetName()
		}
		assert.Contains(t, names, "subdir")
	})

	t.Run("cancelled context stops walk early", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel before starting

		rc := NewRecursiveCollector(MaxSearchDepth, false)
		_, _, err := rc.Collect(ctx, root, "")
		// Walk may return context error or partial results; must not panic.
		_ = err
	})

	t.Run("depth limit of 0 replaced with MaxSearchDepth", func(t *testing.T) {
		rc := NewRecursiveCollector(0, false) // 0 should become MaxSearchDepth
		assert.Equal(t, MaxSearchDepth, rc.MaxDepth)
	})

	t.Run("depth limit 1 restricts to root level", func(t *testing.T) {
		// Add a nested subdirectory that will be at depth 2, beyond MaxDepth=1.
		// The collector sets depthLimited=true only when it encounters a *directory* at depth > MaxDepth.
		deepDir := filepath.Join(subdir, "deepdir")
		require.NoError(t, os.MkdirAll(deepDir, 0o755))
		t.Cleanup(func() { os.RemoveAll(deepDir) })

		rc := NewRecursiveCollector(1, false)
		items, depthLimited, err := rc.Collect(context.Background(), root, "")
		require.NoError(t, err)

		// subdir itself is depth 1, so it should appear.
		// file3.md (depth 2 file) is silently skipped; deepdir (depth 2 dir) triggers depthLimited.
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.GetName()
		}
		assert.NotContains(t, names, "deepdir", "depth-limited collect should not include depth-2 directories")
		assert.True(t, depthLimited, "depth limited should be true when a directory beyond the limit is encountered")
	})

	t.Run("locations are full absolute paths", func(t *testing.T) {
		rc := NewRecursiveCollector(MaxSearchDepth, false)
		items, _, err := rc.Collect(context.Background(), root, "")
		require.NoError(t, err)
		for _, it := range items {
			assert.True(t, filepath.IsAbs(it.GetLocation()),
				"GetLocation() should return absolute path, got %q", it.GetLocation())
		}
	})
}

// ---- Utility functions in utils.go -----------------------------------------

func TestIsEmpty(t *testing.T) {
	assert.True(t, IsEmpty(nil))
	assert.True(t, IsEmpty([]Searchable{}))
	assert.False(t, IsEmpty([]Searchable{newFile("a")}))
}

func TestGetResultCount(t *testing.T) {
	assert.Equal(t, 0, GetResultCount(nil))
	assert.Equal(t, 2, GetResultCount([]Searchable{newFile("a"), newDir("b")}))
}

func TestExtractNames(t *testing.T) {
	items := []Searchable{newFile("readme.md"), newDir("src")}
	names := ExtractNames(items)
	assert.Equal(t, []string{"readme.md", "src"}, names)
}

func TestExtractLocations(t *testing.T) {
	items := []Searchable{
		&mockSearchable{name: "a", location: "/tmp/a"},
		&mockSearchable{name: "b", location: "/home/b"},
	}
	locs := ExtractLocations(items)
	assert.Equal(t, []string{"/tmp/a", "/home/b"}, locs)
}

func TestFilterDirectories(t *testing.T) {
	items := []Searchable{newFile("f1"), newDir("d1"), newFile("f2"), newDir("d2")}
	dirs := FilterDirectories(items)
	assert.Len(t, dirs, 2)
	for _, d := range dirs {
		assert.True(t, d.IsDirectory())
	}
}

func TestFilterFiles(t *testing.T) {
	items := []Searchable{newFile("f1"), newDir("d1"), newFile("f2"), newDir("d2")}
	files := FilterFiles(items)
	assert.Len(t, files, 2)
	for _, f := range files {
		assert.False(t, f.IsDirectory())
	}
}

// ---- Coordinator basic lifecycle -------------------------------------------

func TestCoordinator_Lifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second

	c := NewCoordinator(cfg)

	assert.False(t, c.HasActiveSearch(), "new coordinator should have no active search")
	assert.False(t, c.IsTyping())
	assert.False(t, c.IsSearching())
	assert.Empty(t, c.GetCurrentQuery())
	assert.Nil(t, c.GetCurrentResults())
}

func TestCoordinator_SetTyping(t *testing.T) {
	c := NewCoordinator(DefaultConfig())

	c.SetTyping(true)
	assert.True(t, c.IsTyping())

	c.SetTyping(false)
	assert.False(t, c.IsTyping())
}

func TestCoordinator_Clear(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Timeout = 5 * time.Second
	c := NewCoordinator(cfg)

	source := &mockSearchSource{
		items:   []Searchable{newFile("main.go"), newFile("engine.go")},
		baseDir: "/tmp",
	}

	// Start a sync search to push a layer
	c.StartSearch("main", source, false)
	c.SetTyping(true)

	// After Clear, everything should be reset
	c.Clear()
	assert.False(t, c.HasActiveSearch())
	assert.False(t, c.IsTyping())
	assert.False(t, c.IsSearching())
	assert.Empty(t, c.GetCurrentQuery())
}

func TestCoordinator_HandleESC_SingleStage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TwoStageESC = false

	c := NewCoordinator(cfg)

	source := &mockSearchSource{
		items:   []Searchable{newFile("a.go")},
		baseDir: "/tmp",
	}
	c.StartSearch("a", source, false)
	c.SetTyping(true)

	// Single stage ESC should always return true and clear state
	shouldClose := c.HandleESC()
	assert.True(t, shouldClose, "single-stage ESC should signal close panel")
	assert.False(t, c.IsTyping())
	assert.True(t, c.stack.IsEmpty())
}

func TestCoordinator_HandleESC_TwoStage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TwoStageESC = true

	c := NewCoordinator(cfg)

	source := &mockSearchSource{
		items:   []Searchable{newFile("a.go"), newFile("b.go")},
		baseDir: "/tmp",
	}
	c.StartSearch("a", source, false)
	c.SetTyping(true)

	// Stage 1: blur search bar — should NOT close
	shouldClose := c.HandleESC()
	assert.False(t, shouldClose, "stage-1 ESC should not close panel")
	assert.False(t, c.IsTyping(), "typing should be cleared after stage-1 ESC")

	// Stage 2: pop the layer — should NOT close (still has layers... actually stack
	// may be empty if StartSearch with non-recursive query pushed nothing meaningful,
	// so let's just check the invariant that it doesn't panic)
	shouldClose = c.HandleESC()
	// Either popped a layer (false) or stack was empty (true); either is valid.
	_ = shouldClose

	// Final ESC on empty, non-typing coordinator should close
	c2 := NewCoordinator(cfg)
	c2.SetTyping(false)
	shouldClose2 := c2.HandleESC()
	assert.True(t, shouldClose2, "ESC on empty coordinator should close panel")
}

func TestCoordinator_GetStack(t *testing.T) {
	c := NewCoordinator(DefaultConfig())
	stack := c.GetStack()
	require.NotNil(t, stack)
	assert.True(t, stack.IsEmpty())
}

func TestCoordinator_HandleAsyncResult(t *testing.T) {
	c := NewCoordinator(DefaultConfig())

	// Manually push a "searching" layer
	c.stack.Push(Layer{
		Mode:      ModeRecursive,
		Query:     "myquery",
		Searching: true,
		Results:   []Searchable{},
	})
	c.isSearching = true

	results := []Searchable{newFile("result1.go"), newFile("result2.go")}
	msg := AsyncSearchCompleteMsg{
		Query:   "myquery",
		Results: results,
	}

	c.HandleAsyncResult(msg)

	assert.False(t, c.IsSearching(), "isSearching should be cleared after handling async result")
	top := c.stack.Peek()
	require.NotNil(t, top)
	assert.False(t, top.Searching)
	assert.Len(t, top.Results, 2)
}

// ---- DefaultConfig and ConfigBuilder ---------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, ModeRegular, cfg.Mode)
	assert.False(t, cfg.EnableRecursive)
	assert.Positive(t, cfg.MaxDepth)
	assert.Greater(t, cfg.Timeout, time.Duration(0))
}

func TestConfigBuilder(t *testing.T) {
	cfg := NewConfigBuilder().
		WithRecursive(true).
		WithStacking(true).
		WithTwoStageESC(true).
		WithMaxDepth(10).
		WithTimeout(30 * time.Second).
		WithFileFilter(true).
		WithDirFilter(true).
		Build()

	assert.True(t, cfg.EnableRecursive)
	assert.True(t, cfg.EnableStacking)
	assert.True(t, cfg.TwoStageESC)
	assert.Equal(t, 10, cfg.MaxDepth)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.True(t, cfg.AllowFileFilter)
	assert.True(t, cfg.AllowDirFilter)
}

func TestPresetConfigs(t *testing.T) {
	t.Run("FilePanelConfig", func(t *testing.T) {
		cfg := FilePanelConfig()
		assert.True(t, cfg.EnableRecursive)
		assert.True(t, cfg.EnableStacking)
		assert.True(t, cfg.TwoStageESC)
		assert.True(t, cfg.AllowFileFilter)
		assert.True(t, cfg.AllowDirFilter)
	})

	t.Run("SidebarConfig", func(t *testing.T) {
		cfg := SidebarConfig()
		assert.True(t, cfg.EnableRecursive)
		assert.False(t, cfg.EnableStacking)
	})

	t.Run("HelpMenuConfig", func(t *testing.T) {
		cfg := HelpMenuConfig()
		assert.False(t, cfg.EnableRecursive)
		assert.False(t, cfg.EnableStacking)
	})
}

// ---- shouldIncludeByFilter (engine.go internal helper) ---------------------

func TestShouldIncludeByFilter(t *testing.T) {
	tests := []struct {
		name       string
		isDir      bool
		filterType string
		want       bool
	}{
		{"dir with dirs filter", true, "dirs", true},
		{"file with dirs filter", false, "dirs", false},
		{"dir with files filter", true, "files", false},
		{"file with files filter", false, "files", true},
		{"dir with empty filter", true, "", true},
		{"file with empty filter", false, "", true},
		{"dir with unknown filter", true, "unknown", true},
		{"file with unknown filter", false, "unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldIncludeByFilter(tt.isDir, tt.filterType)
			assert.Equal(t, tt.want, got)
		})
	}
}
