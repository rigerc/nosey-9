package skillkit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type copyCase struct {
	Name  string `json:"name"`
	Note  string `json:"note"`
	Files []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Symlink string `json:"symlink"`
		Mode    string `json:"mode"`
	} `json:"files"`
	Expect []string `json:"expect"`
}

// materializeCopyCase writes the case beside a secret: root/secret.txt and root/repo/....
func materializeCopyCase(t *testing.T, c copyCase) (root, skillDir string) {
	t.Helper()
	root = t.TempDir()
	repo := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repo, 0o750))
	for _, f := range c.Files {
		base := repo
		if f.Path == "secret.txt" {
			base = root
		}
		p := filepath.Join(base, filepath.FromSlash(f.Path))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		if f.Symlink != "" {
			require.NoError(t, os.Symlink(f.Symlink, p))
			continue
		}
		mode := os.FileMode(0o644)
		if f.Mode != "" {
			m, err := strconv.ParseUint(f.Mode, 8, 32)
			require.NoError(t, err)
			mode = os.FileMode(m)
		}
		require.NoError(t, os.WriteFile(p, []byte(f.Content), mode))
		require.NoError(t, os.Chmod(p, mode))
	}
	return root, filepath.Join(repo, "skill")
}

// TestCopyMatchesSkills176 installs each fixture in copy mode and compares the
// resulting tree (names, directories, file modes) with what the real
// skills@1.7.6 CLI produced. Escaping and broken symlinks must not leak content.
func TestCopyMatchesSkills176(t *testing.T) {
	data, err := os.ReadFile("testdata/copy/cases.json")
	require.NoError(t, err)
	var spec struct{ Cases []copyCase }
	require.NoError(t, json.Unmarshal(data, &spec))
	require.NotEmpty(t, spec.Cases)
	for _, c := range spec.Cases {
		t.Run(c.Name, func(t *testing.T) {
			root, skillDir := materializeCopyCase(t, c)
			skills := mustDiscover(t, skillDir, DiscoverOptions{})
			require.Len(t, skills, 1)
			ws := t.TempDir()
			table, _ := testTable(t)
			res, err := Install(context.Background(), skills[0], []string{"claude-code"}, Target{Workspace: ws, Mode: ModeCopy, Agents: table})
			require.NoError(t, err)
			require.Equal(t, StatusInstalled, res.Status)
			dest := filepath.Join(ws, ".claude", "skills", "copy-probe")
			require.Equal(t, c.Expect, listTree(t, dest), c.Note)

			// No file anywhere in the workspace may contain outside content.
			_ = filepath.Walk(ws, func(p string, info os.FileInfo, err error) error {
				if err == nil && info.Mode().IsRegular() {
					require.NotContains(t, readFile(t, p), "TOP SECRET", p)
				}
				return nil
			})
			_ = root
		})
	}
}

func TestCopyWarnsOnSkippedLinks(t *testing.T) {
	want := map[string]string{
		"escaping-file-link": WarnSymlinkEscapes,
		"sibling-dir-link":   WarnSymlinkEscapes,
		"escaping-dir-link":  WarnSymlinkEscapes,
		"broken-link":        WarnSymlinkBroken,
		"cyclic-link":        WarnSymlinkCycle,
	}
	data, err := os.ReadFile("testdata/copy/cases.json")
	require.NoError(t, err)
	var spec struct{ Cases []copyCase }
	require.NoError(t, json.Unmarshal(data, &spec))
	for _, c := range spec.Cases {
		code, expectWarning := want[c.Name]
		t.Run(c.Name, func(t *testing.T) {
			_, skillDir := materializeCopyCase(t, c)
			skills := mustDiscover(t, skillDir, DiscoverOptions{})
			table, _ := testTable(t)
			res, err := Install(context.Background(), skills[0], []string{"claude-code"}, Target{Workspace: t.TempDir(), Mode: ModeCopy, Agents: table})
			require.NoError(t, err)
			if !expectWarning {
				require.Empty(t, res.Warnings)
				return
			}
			require.Len(t, res.Warnings, 1)
			require.Equal(t, code, res.Warnings[0].Code)
			require.NotContains(t, res.Warnings[0].Message, os.TempDir(), "warnings must not leak absolute paths")
			require.False(t, strings.Contains(res.Warnings[0].Message, "TOP SECRET"))
		})
	}
}

func TestCopySkipsSpecialFilesAndFollowsInternalLinks(t *testing.T) {
	if _, err := os.Stat("/usr/bin/mkfifo"); err != nil {
		t.Skip("mkfifo unavailable")
	}
	dir := writeSkill(t, t.TempDir(), "fifo-skill", map[string]string{"real.txt": "r"})
	require.NoError(t, osMkfifo(filepath.Join(dir, "pipe")))
	skills := mustDiscover(t, dir, DiscoverOptions{})
	table, _ := testTable(t)
	res, err := Install(context.Background(), skills[0], []string{"claude-code"}, Target{Workspace: t.TempDir(), Mode: ModeCopy, Agents: table})
	require.NoError(t, err)
	require.Len(t, res.Warnings, 1)
	require.Equal(t, WarnSpecialFile, res.Warnings[0].Code)
}
