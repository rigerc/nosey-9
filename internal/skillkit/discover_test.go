package skillkit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverFindsSkillsInDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "zeta"), "zeta", nil)
	writeSkill(t, filepath.Join(root, "skills", "alpha"), "alpha", nil)
	writeSkill(t, filepath.Join(root, ".claude", "skills", "mid"), "mid", nil)
	writeSkill(t, filepath.Join(root, ".agents", "skills", "agentic"), "agentic", nil)
	table, _ := testTable(t)
	var first []string
	for range 5 {
		skills := mustDiscover(t, root, DiscoverOptions{Agents: table})
		var names []string
		for _, s := range skills {
			names = append(names, s.Name)
			require.True(t, filepath.IsAbs(s.Dir))
		}
		if first == nil {
			first = names
		}
		require.Equal(t, first, names, "search order must not change between runs")
	}
	// "skills" is a conventional directory searched before agent directories, which are sorted.
	require.Equal(t, []string{"alpha", "zeta", "agentic", "mid"}, first)
}

func TestDiscoverReportsParseErrorsAndKeepsGoodSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "good"), "good", nil)
	bad := filepath.Join(root, "skills", "bad")
	require.NoError(t, os.MkdirAll(bad, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter"), 0o600))
	skills, err := Discover(root, DiscoverOptions{})
	require.Len(t, skills, 1)
	require.Equal(t, "good", skills[0].Name)
	var de *DiscoverError
	require.ErrorAs(t, err, &de)
	require.Len(t, de.Parse, 1)
	require.Equal(t, "skills/bad/SKILL.md", de.Parse[0].Path)
	require.Contains(t, err.Error(), "skills/bad/SKILL.md")
	var pe ParseError
	require.ErrorAs(t, err, &pe)
}

func TestDiscoverTreatsDuplicateNamesAsErrors(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "skills", "one"), "same", nil)
	writeSkill(t, filepath.Join(root, ".claude", "skills", "two"), "same", nil)
	table, _ := testTable(t)
	skills, err := Discover(root, DiscoverOptions{Agents: table})
	require.Len(t, skills, 1, "the first occurrence is kept")
	require.Equal(t, "skills/one", skills[0].Path)
	var de *DiscoverError
	require.ErrorAs(t, err, &de)
	require.Equal(t, []DuplicateError{{Name: "same", First: "skills/one", Other: ".claude/skills/two"}}, de.Duplicates)
	var dup DuplicateError
	require.True(t, errors.As(err, &dup))
}

func TestDiscoverInternalSkillsAndFullDepth(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "hidden")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: hidden\ndescription: d\nmetadata:\n  internal: true\n---\n"), 0o600))
	writeSkill(t, filepath.Join(root, "skills", "shown"), "shown", nil)
	require.Len(t, mustDiscover(t, root, DiscoverOptions{}), 1)
	require.Len(t, mustDiscover(t, root, DiscoverOptions{IncludeInternal: true}), 2)

	// A root skill stops the search unless FullDepth is set.
	writeSkill(t, root, "rootskill", nil)
	require.Len(t, mustDiscover(t, root, DiscoverOptions{}), 1)
	require.Len(t, mustDiscover(t, root, DiscoverOptions{FullDepth: true}), 2)
}

func TestDiscoverSubpathAndTraversal(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "pkg", "skills", "inner"), "inner", nil)
	writeSkill(t, filepath.Join(root, "other"), "other", nil)
	got := mustDiscover(t, root, DiscoverOptions{Subpath: "pkg"})
	require.Len(t, got, 1)
	require.Equal(t, "pkg/skills/inner", got[0].Path)
	for _, bad := range []string{"..", "../x", "a/../../x", "/etc", `a\..\b`} {
		_, err := Discover(root, DiscoverOptions{Subpath: bad})
		var pt *PathTraversalError
		require.ErrorAs(t, err, &pt, bad)
	}
}

func TestDiscoverNeverReadsThroughEscapingSymlinks(t *testing.T) {
	outside := t.TempDir()
	writeSkill(t, filepath.Join(outside, "stolen"), "stolen", nil)
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join(outside, "stolen"), filepath.Join(root, "skills", "link")))
	skills, _ := Discover(root, DiscoverOptions{})
	require.Empty(t, skills)

	// A SKILL.md symlinked to a file outside the tree is a parse error, not content.
	dir := filepath.Join(root, "skills", "real")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.Symlink(filepath.Join(outside, "stolen", "SKILL.md"), filepath.Join(dir, "SKILL.md")))
	skills, _ = Discover(root, DiscoverOptions{})
	require.Empty(t, skills)
}

func TestDiscoverPluginManifests(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, filepath.Join(root, "plugins", "p1", "skills", "inplug"), "inplug", nil)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude-plugin"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".claude-plugin", "marketplace.json"),
		[]byte(`{"plugins":[{"name":"p1","source":"./plugins/p1","skills":["./skills/inplug"]},{"name":"evil","source":"../escape"}]}`), 0o600))
	got := mustDiscover(t, root, DiscoverOptions{})
	require.Len(t, got, 1)
	require.Equal(t, "p1", got[0].PluginName)
}

func TestParseSkill(t *testing.T) {
	s, err := ParseSkill([]byte("---\r\nname: x\r\ndescription: y\r\nallowed-tools: [a]\r\n---\r\nbody"))
	require.NoError(t, err)
	require.Equal(t, "x", s.Name)
	for _, bad := range []string{"", "x", "---\nname: x\n", "---\nname: x\n---\n", "---\ndescription: d\n---\n", "---\nname: [a]\ndescription: d\n---\n", "---\n: : :\n---\n"} {
		_, err = ParseSkill([]byte(bad))
		require.Error(t, err, "%q", bad)
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"My Skill": "my-skill", "a/b": "a-b", "..": "unnamed-skill", "": "unnamed-skill", "  Hi!! ": "hi",
		"keep_this.name": "keep_this.name", "UPPER": "upper", "é": "unnamed-skill", "a--b": "a-b", "../etc/passwd": "etc-passwd",
	} {
		require.Equal(t, want, SanitizeName(in), in)
	}
	long := ""
	for range 300 {
		long += "a"
	}
	require.Len(t, SanitizeName(long), 255)
}
