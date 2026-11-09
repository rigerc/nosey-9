package skillkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeLock(t *testing.T, ws, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(ws, LockFile), []byte(content), 0o644))
}

// TestLockRoundTripsSkills176Output reads a lock produced by the real CLI
// (git and relative local entries) and writes the identical bytes back.
func TestLockRoundTripsSkills176Output(t *testing.T) {
	golden, err := os.ReadFile("testdata/lock/skills@1.7.6.json")
	require.NoError(t, err)
	ws := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	writeLock(t, ws, string(golden))
	lock, err := ReadLock(ws)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "beta"}, lock.Names())
	alpha := lock.Skills["alpha"]
	require.Equal(t, "git", alpha.SourceType)
	require.Equal(t, "https://git.example.test/team/skills.git", alpha.SourceURL)
	require.Equal(t, "skills/alpha/SKILL.md", alpha.SkillPath)
	require.Equal(t, filepath.Join(filepath.Dir(ws), "repo"), lock.Skills["beta"].Source, "local sources are absolute in memory")

	out, err := lock.Marshal(ws)
	require.NoError(t, err)
	require.Equal(t, string(golden), string(out))

	require.NoError(t, lock.Write(ws))
	require.Equal(t, string(golden), readFile(t, filepath.Join(ws, LockFile)))
	entries, err := os.ReadDir(ws)
	require.NoError(t, err)
	require.Len(t, entries, 1, "atomic write leaves no temporary file")
}

// TestLockPreservesEveryFieldAndUnknownFields is the regression for skills-go
// dropping ref, sourceUrl, skillPath and version when it read and wrote a lock.
func TestLockPreservesEveryFieldAndUnknownFields(t *testing.T) {
	ws := t.TempDir()
	in := `{
  "version": 1,
  "skills": {
    "zeta": {
      "source": "org/repo",
      "sourceUrl": "https://example.test/org/repo.git",
      "ref": "0123456789abcdef0123456789abcdef01234567",
      "sourceType": "git",
      "skillPath": "skills/zeta/SKILL.md",
      "computedHash": "abc",
      "version": "1.2.3",
      "via": "dep",
      "installMode": "copy",
      "subagents": ["a", "b"],
      "nested": {"k": [1, 2, {"x": null}]}
    },
    "alpha": {
      "source": "react",
      "sourceType": "node_modules",
      "skillPath": "skills/x/SKILL.md",
      "version": "9.9.9",
      "computedHash": "def"
    }
  },
  "futureField": {"a": 1},
  "another": "x<y>&z"
}
`
	writeLock(t, ws, in)
	lock, err := ReadLock(ws)
	require.NoError(t, err)
	z := lock.Skills["zeta"]
	require.Equal(t, LockEntry{
		Source: "org/repo", SourceURL: "https://example.test/org/repo.git", Ref: "0123456789abcdef0123456789abcdef01234567",
		SourceType: "git", SkillPath: "skills/zeta/SKILL.md", ComputedHash: "abc", Version: "1.2.3",
		Extra: z.Extra, order: z.order,
	}, z)
	require.Contains(t, z.Extra, "via")
	require.Contains(t, lock.Extra, "futureField")

	require.NoError(t, lock.Write(ws))
	var before, after any
	require.NoError(t, json.Unmarshal([]byte(in), &before))
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(ws, LockFile))), &after))
	require.Equal(t, before, after, "no field is lost")
	out := readFile(t, filepath.Join(ws, LockFile))
	require.Contains(t, out, "x<y>&z", "no HTML escaping, like JSON.stringify")
	require.Less(t, indexOf(out, `"alpha"`), indexOf(out, `"zeta"`), "skills are sorted by name")
	require.Less(t, indexOf(out, `"source"`), indexOf(out, `"computedHash"`), "entry key order is preserved")
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestLockSetKeepsUnknownFieldsOfReplacedEntry(t *testing.T) {
	lock := NewProjectLock()
	lock.Skills["a"] = LockEntry{Source: "o/r", SourceType: "github", Extra: map[string]json.RawMessage{"via": json.RawMessage(`"dep"`), "keep": json.RawMessage(`1`)}}
	lock.Set("a", LockEntry{Source: "o/r", SourceType: "github", ComputedHash: "new", Extra: map[string]json.RawMessage{"keep": json.RawMessage(`2`)}})
	require.Equal(t, "new", lock.Skills["a"].ComputedHash)
	require.JSONEq(t, `"dep"`, string(lock.Skills["a"].Extra["via"]))
	require.JSONEq(t, `2`, string(lock.Skills["a"].Extra["keep"]), "an explicit value wins")
	lock.Remove("a")
	require.Empty(t, lock.Names())
}

func TestLockStrictValidation(t *testing.T) {
	ws := t.TempDir()
	_, err := ReadLock(ws)
	require.ErrorIs(t, err, ErrLockMissing)
	require.EqualError(t, err, "skills-lock.json is missing")

	for name, tc := range map[string]struct{ content, reason string }{
		"not json":           {"{nope", "is not valid JSON"},
		"merge markers":      {"<<<<<<< HEAD\n{}\n=======\n{}\n>>>>>>> x\n", "is not valid JSON (unresolved merge conflict markers)"},
		"array":              {`[]`, "is not valid JSON"},
		"null":               {`null`, "is not valid JSON"},
		"future version":     {`{"version":2,"skills":{}}`, "has unsupported version 2 (expected 1)"},
		"no version":         {`{"skills":{}}`, "has unsupported version 0 (expected 1)"},
		"fractional version": {`{"version":1.5,"skills":{}}`, "is not valid JSON"},
		"no skills":          {`{"version":1}`, `has no "skills" map`},
		"null skills":        {`{"version":1,"skills":null}`, `has no "skills" map`},
		"skills is array":    {`{"version":1,"skills":[]}`, "is not valid JSON"},
		"entry not object":   {`{"version":1,"skills":{"a":1}}`, `has an invalid entry for "a": expected an object`},
		"field wrong type":   {`{"version":1,"skills":{"a":{"source":5}}}`, `has an invalid entry for "a": field "source" must be a string`},
		"truncated":          {`{"version":1,"skills":{"a":{`, "is not valid JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			writeLock(t, ws, tc.content)
			_, err := ReadLock(ws)
			var le LockError
			require.ErrorAs(t, err, &le)
			require.Equal(t, tc.reason, le.Reason)
			require.EqualError(t, err, "skills-lock.json "+tc.reason)
		})
	}
	writeLock(t, ws, `{"version":1,"skills":{}}`)
	lock, err := ReadLock(ws)
	require.NoError(t, err)
	require.Empty(t, lock.Skills)
}

func TestLockNeverReadsOrWritesThroughASymlink(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	require.NoError(t, os.WriteFile(outside, []byte(`{"version":1,"skills":{"x":{"source":"s","sourceType":"git"}}}`), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, LockFile)))
	_, err := ReadLock(ws)
	var le LockError
	require.ErrorAs(t, err, &le, "a lock symlinked out of the workspace is not read")
	require.NotErrorIs(t, err, ErrLockMissing)
	// Writing replaces the link itself; the file it pointed to is never modified.
	require.NoError(t, NewProjectLock().Write(ws))
	require.Contains(t, readFile(t, outside), `"x"`)
	info, err := os.Lstat(filepath.Join(ws, LockFile))
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
}

// TestLockLocalSourceIsRelative is the regression for skills-go writing absolute
// machine-specific paths for local sources.
func TestLockLocalSourceIsRelative(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "proj")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	lock := NewProjectLock()
	for name, src := range map[string]string{
		"sibling": filepath.Join(root, "sibling"),
		"inside":  filepath.Join(ws, "vendor", "skills"),
		"self":    ws,
		"parent2": filepath.Join(root, "..", "far", "away"),
	} {
		lock.Set(name, LockEntry{Source: src, SourceType: "local", ComputedHash: "h"})
	}
	require.NoError(t, lock.Write(ws))
	var raw struct {
		Skills map[string]struct{ Source string }
	}
	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(ws, LockFile))), &raw))
	require.Equal(t, "../sibling", raw.Skills["sibling"].Source)
	require.Equal(t, "./vendor/skills", raw.Skills["inside"].Source)
	require.Equal(t, ".", raw.Skills["self"].Source)
	require.Equal(t, "../../far/away", raw.Skills["parent2"].Source)
	require.NotContains(t, readFile(t, filepath.Join(ws, LockFile)), root, "no machine-specific path")

	back, err := ReadLock(ws)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "sibling"), back.Skills["sibling"].Source)
	require.Equal(t, filepath.Join(ws, "vendor", "skills"), back.Skills["inside"].Source)
}

func TestNewLockEntryDerivesSourceAndType(t *testing.T) {
	parse := func(uri, ref string) ParsedSource {
		p, err := ParseSource(Source{URI: uri, Ref: ref})
		require.NoError(t, err)
		return p
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	tests := []struct {
		name string
		p    ParsedSource
		want LockEntry
	}{
		{"owner/repo", parse("acme/skills", sha), LockEntry{Source: "acme/skills", Ref: sha, SourceType: "github", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
		{"github https", parse("https://github.com/acme/skills.git", ""), LockEntry{Source: "acme/skills", SourceType: "github", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
		{"gitlab https", parse("https://gitlab.com/group/sub/repo", "main"), LockEntry{Source: "https://gitlab.com/group/sub/repo.git", SourceURL: "https://gitlab.com/group/sub/repo.git", Ref: "main", SourceType: "gitlab", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
		{"other https", parse("https://git.example.test/team/repo.git", "v1"), LockEntry{Source: "https://git.example.test/team/repo.git", SourceURL: "https://git.example.test/team/repo.git", Ref: "v1", SourceType: "git", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
		{"scp ssh", parse("git@git.example.test:team/repo.git", ""), LockEntry{Source: "git@git.example.test:team/repo.git", SourceURL: "git@git.example.test:team/repo.git", SourceType: "git", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
		{"ssh url", parse("ssh://git@git.example.test/team/repo.git", ""), LockEntry{Source: "ssh://git@git.example.test/team/repo.git", SourceURL: "ssh://git@git.example.test/team/repo.git", SourceType: "git", SkillPath: "skills/a/SKILL.md", ComputedHash: "h"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, NewLockEntry(tc.p, "skills/a/SKILL.md", "h"))
		})
	}
	local, err := ParseSource(Source{LocalPath: "/work/registry"})
	require.NoError(t, err)
	require.Equal(t, LockEntry{Source: "/work/registry", SourceType: "local", ComputedHash: "h"}, NewLockEntry(local, "", "h"))
	require.Equal(t, "skills/a/SKILL.md", SkillMDPath(Skill{Path: "skills/a"}))
	require.Equal(t, "SKILL.md", SkillMDPath(Skill{Path: "."}))
}

// TestLockEntryKeyOrderMatchesUpstream pins the member order skills@1.7.6 writes:
// source, sourceUrl, ref, sourceType, skillPath, computedHash.
func TestLockEntryKeyOrderMatchesUpstream(t *testing.T) {
	p, err := ParseSource(Source{URI: "https://git.example.test/t/r.git", Ref: "v1"})
	require.NoError(t, err)
	lock := NewProjectLock()
	lock.Set("a", NewLockEntry(p, "skills/a/SKILL.md", "h"))
	out, err := lock.Marshal(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, `{
  "version": 1,
  "skills": {
    "a": {
      "source": "https://git.example.test/t/r.git",
      "sourceUrl": "https://git.example.test/t/r.git",
      "ref": "v1",
      "sourceType": "git",
      "skillPath": "skills/a/SKILL.md",
      "computedHash": "h"
    }
  }
}
`, string(out))
	empty, err := NewProjectLock().Marshal(t.TempDir())
	require.NoError(t, err)
	require.Equal(t, "{\n  \"version\": 1,\n  \"skills\": {}\n}\n", string(empty))
}

func TestParseSourceRejectsUnsafeInput(t *testing.T) {
	for _, bad := range []Source{
		{URI: "-oProxyCommand=x"}, {URI: "ext::sh -c id"}, {URI: "file:///etc"}, {URI: "http://insecure.example/r.git"},
		{URI: "../escape"}, {URI: ""}, {URI: "a/b", Ref: "--upload-pack=x"}, {URI: "a/b", Ref: "a..b"}, {URI: "a/b", Ref: "x/"},
	} {
		_, err := ParseSource(bad)
		var se *SourceError
		require.ErrorAs(t, err, &se, "%+v", bad)
	}
}
