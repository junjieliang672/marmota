//go:build !windows

package internal

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/junjieliang672/marmota/src/internal/common"
)

func TestFilePreviewWithInvalidMode(t *testing.T) {
	curTestDir := t.TempDir()
	file := filepath.Join(curTestDir, "testf")

	err := syscall.Mkfifo(file, 0644)
	require.NoError(t, err)

	m := defaultTestModel(curTestDir)

	m.fileModel.filePreview.SetWidth(40)
	m.fileModel.filePreview.SetHeight(10)
	res := unframe(t, ansi.Strip(m.fileModel.filePreview.RenderWithPath(file, m.fullWidth)))
	assert.Contains(t, res, strings.TrimSpace(common.FilePreviewUnsupportedFileMode))
}
