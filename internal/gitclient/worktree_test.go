package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func ignoreFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q", "-b", "main")
	for name, body := range map[string]string{
		".gitignore":        "*.log\nbuild/\n!keep.log\n",
		"tracked.txt":       "t",
		"forced.log":        "tracked though ignored",
		"sub/.gitignore":    "local.txt\n",
		"sub/local.txt":     "x",
		"sub/untracked.txt": "u",
		"sub/keep.log":      "k",
		"sub/deep/file.txt": "d",
		"build/out":         "b",
		"excluded.tmp":      "e",
		"global.ignored":    "g",
		"nested/.git/HEAD":  "ref: refs/heads/main\n",
		"nested/file":       "n",
	} {
		writeFile(t, filepath.Join(dir, name), body)
	}
	writeFile(t, filepath.Join(dir, ".git/info/exclude"), "*.tmp\n")
	gitCmd(t, dir, "add", "tracked.txt", ".gitignore", "sub/.gitignore")
	gitCmd(t, dir, "add", "-f", "forced.log")
	gitCmd(t, dir, "commit", "-q", "-m", "m")
	return dir
}

func TestListWorktreeFilesMatchesGitRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	writeFile(t, filepath.Join(home, ".config/git/ignore"), "*.ignored\n")
	dir := ignoreFixture(t)
	repo, err := fixtureClient(t).OpenUser(dir)
	require.NoError(t, err)

	files, err := repo.ListWorktreeFiles(ctx, dir)
	require.NoError(t, err)
	require.Equal(t, []string{".gitignore", "forced.log", "sub/.gitignore", "sub/deep/file.txt", "sub/keep.log", "sub/untracked.txt", "tracked.txt"}, files)

	files, err = repo.ListWorktreeFiles(ctx, filepath.Join(dir, "sub"))
	require.NoError(t, err)
	require.Equal(t, []string{".gitignore", "deep/file.txt", "keep.log", "untracked.txt"}, files)

	files, err = repo.ListWorktreeFiles(ctx, filepath.Join(dir, "build"))
	require.NoError(t, err)
	require.Empty(t, files, "an ignored directory has no untracked files")

	require.NoError(t, os.Remove(filepath.Join(dir, "tracked.txt")))
	files, err = repo.ListWorktreeFiles(ctx, dir)
	require.NoError(t, err)
	require.Contains(t, files, "tracked.txt", "a deleted file stays listed while in the index")

	_, err = repo.ListWorktreeFiles(ctx, t.TempDir())
	require.ErrorIs(t, err, KindInvalidSource)
}

func TestListWorktreeFilesLinkedWorktreeAndBounds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	f := newFixture(t)
	linked := filepath.Join(t.TempDir(), "wt")
	gitCmd(t, f.src, "worktree", "add", "-q", "-b", "linked", linked)
	writeFile(t, filepath.Join(linked, "extra"), "x")
	repo, err := fixtureClient(t).OpenUser(linked)
	require.NoError(t, err)
	files, err := repo.ListWorktreeFiles(ctx, linked)
	require.NoError(t, err)
	require.Contains(t, files, "skills/a/SKILL.md")
	require.Contains(t, files, "extra")

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = repo.ListWorktreeFiles(canceled, linked)
	require.ErrorIs(t, err, KindCanceled)

	limited := New(Options{Policy: Policy{AllowFile: true}, Credentials: emptyCredentials(t.TempDir()), Limits: Limits{MaxFiles: 1}})
	repo, err = limited.OpenUser(linked)
	require.NoError(t, err)
	_, err = repo.ListWorktreeFiles(ctx, linked)
	require.ErrorIs(t, err, KindResourceLimit)
}

func TestOriginURLIgnoresInsteadOfAndFirstURLWins(t *testing.T) {
	f := newFixture(t)
	gitCmd(t, f.src, "remote", "add", "origin", "https://example.test/a/b")
	gitCmd(t, f.src, "remote", "set-url", "--add", "origin", "https://example.test/second/b")
	gitCmd(t, f.src, "config", "url.https://evil.test/.insteadOf", "https://example.test/")
	repo, err := fixtureClient(t).OpenUser(f.src)
	require.NoError(t, err)
	url, err := repo.OriginURL()
	require.NoError(t, err)
	require.Equal(t, "https://example.test/a/b", url)
	head, err := repo.OriginHead()
	require.NoError(t, err)
	require.Empty(t, head)
	gitCmd(t, f.src, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	head, err = repo.OriginHead()
	require.NoError(t, err)
	require.Equal(t, "origin/main", head)
}

func TestPushUsesFirstOriginURL(t *testing.T) {
	f := newFixture(t, anySHA)
	second := filepath.Join(t.TempDir(), "second.git")
	gitCmd(t, filepath.Dir(second), "init", "-q", "--bare", "-b", "main", second)
	c := fixtureClient(t)
	gitCmd(t, f.src, "remote", "add", "origin", f.url())
	gitCmd(t, f.src, "remote", "set-url", "--add", "origin", "file://"+second)
	repo, err := c.OpenUser(f.src)
	require.NoError(t, err)
	require.NoError(t, repo.Push(ctx, f.commits[2], "pushed"))
	require.Equal(t, f.commits[2], gitCmd(t, f.bare, "rev-parse", "refs/heads/pushed"))
	require.Empty(t, gitCmd(t, second, "for-each-ref"))
}
