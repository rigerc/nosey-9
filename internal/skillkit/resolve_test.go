package skillkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func discoverErr(t *testing.T, root string) *DiscoverError {
	t.Helper()
	_, err := Discover(root, DiscoverOptions{})
	var de *DiscoverError
	require.ErrorAs(t, err, &de)
	return de
}

func TestResolveIdenticalDuplicateUsesFirstWithWarning(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "a"), "dup", map[string]string{"x.txt": "same"})
	writeSkill(t, filepath.Join(root, "skills", "b"), "dup", map[string]string{"x.txt": "same"})
	de := discoverErr(t, root)
	warns, err := de.Resolve(root, []string{"dup"})
	require.NoError(t, err)
	require.Len(t, warns, 1)
	require.Equal(t, WarnDuplicateSkill, warns[0].Code)
	require.Contains(t, warns[0].Message, "using skills/a")
}

func TestResolveDifferingDuplicateIsAnErrorOnlyWhenRequested(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "a"), "dup", map[string]string{"x.txt": "one"})
	writeSkill(t, filepath.Join(root, "skills", "b"), "dup", map[string]string{"x.txt": "two"})
	de := discoverErr(t, root)
	_, err := de.Resolve(root, []string{"DUP"})
	require.ErrorContains(t, err, "different content")
	warns, err := de.Resolve(root, []string{"other"})
	require.NoError(t, err)
	require.Empty(t, warns)
}

func TestResolveParseErrorsAreWarnings(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "good"), "good", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "bad"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "bad", "SKILL.md"), []byte("nope"), 0o600))
	warns, err := discoverErr(t, root).Resolve(root, []string{"good"})
	require.NoError(t, err)
	require.Len(t, warns, 1)
	require.Equal(t, WarnParseError, warns[0].Code)
}

func TestCopyHashMatchesInstalledTree(t *testing.T) {
	src := writeSkill(t, filepath.Join(t.TempDir(), "s"), "s", map[string]string{
		"metadata.json": `{"x":1}`, "scripts/run.sh": "echo", "__pycache__/c.pyc": "x", "docs/guide.md": "g",
	})
	require.NoError(t, os.Symlink("docs/guide.md", filepath.Join(src, "link.md")))
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(src, "escape")))

	raw, err := FolderHash(src)
	require.NoError(t, err)
	copyHash, err := CopyHash(context.Background(), src)
	require.NoError(t, err)
	require.NotEqual(t, raw, copyHash, "the exclusions must change the hash")

	table, _ := testTable(t)
	ws := t.TempDir()
	skills := mustDiscover(t, filepath.Dir(src), DiscoverOptions{Agents: table})
	res, err := Install(context.Background(), skills[0], []string{"universal"}, Target{Workspace: ws, Mode: ModeCopy, Agents: table})
	require.NoError(t, err)
	require.Equal(t, raw, res.Hash, "computedHash stays the upstream value")
	installed, err := FolderHash(filepath.Join(ws, ".agents", "skills", "s"))
	require.NoError(t, err)
	require.Equal(t, installed, copyHash)
}
