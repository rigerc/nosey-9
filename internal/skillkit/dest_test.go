package skillkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// os.Root is the second line of defence: even if the explicit ancestor check were
// raced, an escape through a symlink surfaces as a typed *PolicyError.
func TestRootEscapeErrorsBecomePolicyErrors(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "link")))
	d, err := openDest(ws)
	require.NoError(t, err)
	defer d.Close()
	_, err = d.root.OpenFile(filepath.Join("link", "x"), os.O_CREATE|os.O_WRONLY, 0o600)
	require.Error(t, err)
	var pe *PolicyError
	require.ErrorAs(t, wrap("link/x", err), &pe)
	require.NoFileExists(t, filepath.Join(outside, "x"))
	require.NoError(t, wrap("x", nil))
}

func TestCheckParentRefusals(t *testing.T) {
	ws := t.TempDir()
	d, err := openDest(ws)
	require.NoError(t, err)
	defer d.Close()
	require.NoError(t, os.WriteFile(filepath.Join(ws, "file"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(ws, "missing-target"), filepath.Join(ws, "dangling")))
	for _, rel := range []string{"../x", "/abs/x", "file/child", "dangling/child"} {
		var pe *PolicyError
		require.ErrorAs(t, d.checkParent(rel), &pe, rel)
	}
	require.NoError(t, d.checkParent("new/dir/skill"))
	require.NoError(t, d.checkParent("top"))
}
