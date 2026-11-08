package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRotateLogFileIfNeeded(t *testing.T) {
	t.Run("does nothing below threshold", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "superfile.log")
		require.NoError(t, os.WriteFile(logPath, []byte("small log"), LogFilePerm))

		require.NoError(t, RotateLogFileIfNeeded(logPath, 1024))

		data, err := os.ReadFile(logPath)
		require.NoError(t, err)
		assert.Equal(t, "small log", string(data))

		_, err = os.Stat(logPath + ".1")
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("rotates when threshold reached", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "superfile.log")
		content := []byte("1234567890")
		require.NoError(t, os.WriteFile(logPath, content, LogFilePerm))
		require.NoError(t, os.WriteFile(logPath+".1", []byte("stale backup"), LogFilePerm))

		require.NoError(t, RotateLogFileIfNeeded(logPath, int64(len(content))))

		_, err := os.Stat(logPath)
		assert.True(t, os.IsNotExist(err))

		rotated, err := os.ReadFile(logPath + ".1")
		require.NoError(t, err)
		assert.Equal(t, content, rotated)
	})
}
