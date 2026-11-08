package preview

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderText_DoesNotPanicWithNegativeDimensions(t *testing.T) {
	m := New()
	m.SetWidth(-1)
	m.SetHeight(-1)

	require.NotPanics(t, func() {
		_ = m.RenderText("Loading...")
	})
}

func TestRenderDirectoryPreview_UsesShortLivedCache(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "first.txt"), []byte("content"), 0o644))

	m := New()
	m.SetWidth(40)
	m.SetHeight(10)

	first := m.RenderWithPath(tmpDir, 120)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "second.txt"), []byte("content"), 0o644))

	second := m.RenderWithPath(tmpDir, 120)
	require.Equal(t, first, second)

	m.dirPreviewCacheMu.Lock()
	for key, entry := range m.dirPreviewCache {
		entry.expiresAt = entry.expiresAt.Add(-dirPreviewCacheTTL * 2)
		m.dirPreviewCache[key] = entry
	}
	m.dirPreviewCacheMu.Unlock()

	third := m.RenderWithPath(tmpDir, 120)
	require.NotEqual(t, first, third)
}
