package skillkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type fixtureFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Symlink string `json:"symlink"`
}

type hashCase struct {
	Name  string        `json:"name"`
	Files []fixtureFile `json:"files"`
	Hash  string        `json:"hash"`
	// KnownMismatch documents a case golang.org/x/text/collate cannot reproduce.
	KnownMismatch string `json:"knownMismatch"`
}

func writeFixture(t *testing.T, root string, files []fixtureFile) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.Path))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		if f.Symlink != "" {
			require.NoError(t, os.Symlink(f.Symlink, p))
			continue
		}
		require.NoError(t, os.WriteFile(p, []byte(f.Content), 0o600))
	}
}

// TestFolderHashMatchesSkills176 compares FolderHash with computedHash values
// captured once from the real skills@1.7.6 CLI (see README.md). It needs neither
// Node nor the network.
func TestFolderHashMatchesSkills176(t *testing.T) {
	data, err := os.ReadFile("testdata/hash/cases.json")
	require.NoError(t, err)
	var spec struct {
		Cases []hashCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	require.NotEmpty(t, spec.Cases)
	for _, c := range spec.Cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, c.Files)
			got, err := FolderHash(dir)
			require.NoError(t, err)
			if c.KnownMismatch != "" {
				// A reported gap, not a hidden one: if the collation library ever
				// catches up this fails and the case should be promoted.
				require.NotEqual(t, c.Hash, got, "known mismatch now matches; remove knownMismatch: %s", c.KnownMismatch)
				t.Logf("known mismatch with skills@1.7.6: %s", c.KnownMismatch)
				return
			}
			require.Equal(t, c.Hash, got, "hash differs from skills@1.7.6 for %s", c.Name)
		})
	}
}
