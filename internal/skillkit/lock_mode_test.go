package skillkit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLockWriteLeavesSharedFileReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	workspace := t.TempDir()
	require.NoError(t, NewProjectLock().Write(workspace))
	info, err := os.Stat(filepath.Join(workspace, LockFile))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}
