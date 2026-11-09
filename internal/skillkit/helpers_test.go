package skillkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSkill creates dir/SKILL.md for a skill named name and returns dir.
func writeSkill(t *testing.T, dir, name string, extra map[string]string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	body := "---\nname: " + name + "\ndescription: Test skill " + name + "\n---\n# " + name + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
	for rel, content := range extra {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return dir
}

// testTable builds an agent table with an isolated home and no environment overrides.
func testTable(t *testing.T, custom ...CustomAgent) (AgentTable, string) {
	t.Helper()
	home := t.TempDir()
	table, err := NewAgentTable(home, custom)
	require.NoError(t, err)
	return table.WithEnv(func(string) string { return "" }), home
}

func mustDiscover(t *testing.T, root string, opts DiscoverOptions) []Skill {
	t.Helper()
	skills, err := Discover(root, opts)
	require.NoError(t, err)
	return skills
}

// listTree renders dir the way the golden captures do: "d rel", "l rel" or "f rel mode".
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	var walk func(rel string)
	walk = func(rel string) {
		entries, err := os.ReadDir(filepath.Join(dir, rel))
		require.NoError(t, err)
		for _, e := range entries {
			r := filepath.ToSlash(filepath.Join(rel, e.Name()))
			info, err := os.Lstat(filepath.Join(dir, r))
			require.NoError(t, err)
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				out = append(out, "l "+r)
			case e.IsDir():
				out = append(out, "d "+r)
				walk(r)
			default:
				out = append(out, "f "+r+" "+modeString(info.Mode().Perm()))
			}
		}
	}
	walk("")
	return out
}

func modeString(m os.FileMode) string {
	const digits = "01234567"
	v := uint32(m)
	return string([]byte{digits[v>>6&7], digits[v>>3&7], digits[v&7]})
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	// #nosec G304 -- test helper reading a file the test created.
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(b)
}
