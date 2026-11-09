package gitclient

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/require"
)

const anySHA = "uploadpack.allowAnySHA1InWant=true"

var ctx = context.Background()

func TestFileSeamOnlySetInTests(t *testing.T) {
	seam := regexp.MustCompile(`\bAllowFile\s*(:|=[^=])|\bRewrite\s*(:|=[^=])`)
	definition := filepath.Join("internal", "gitclient", "policy.go")
	var offenders []string
	for _, top := range []string{"../../internal", "../../cmd"} {
		require.NoError(t, filepath.WalkDir(top, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, readErr := os.ReadFile(path) //nolint:gosec // test fixture under t.TempDir
			if readErr != nil {
				return readErr
			}
			if seam.Match(data) && !strings.HasSuffix(path, definition) {
				offenders = append(offenders, path)
			}
			return nil
		}))
	}
	require.Empty(t, offenders, "only _test.go files may set the file-transport seam")
	data, err := os.ReadFile("policy.go")
	require.NoError(t, err)
	require.False(t, regexp.MustCompile(`AllowFile:\s*true`).Match(data))
}

func TestRejectUnsafeRemote(t *testing.T) {
	hardened := []string{
		"", "-upload-pack=x", "file:///tmp/x", "/tmp/repo", "../relative", "http://github.com/a/b",
		"git://github.com/a/b", "ext::sh -c id", "https://user:pw@github.com/a/b", "https://github.com:8443/a/b",
		"https://github.com/a/../b", "ssh://git@github.com:22/a/b", "git@github.com:a/../../b", "foo bar/baz",
	}
	for _, uri := range hardened {
		_, err := Hardened().resolve(uri)
		require.ErrorIs(t, err, KindInvalidSource, uri)
	}
	for _, uri := range []string{"file:///tmp/x", "/tmp/x", "git://host/a", "ssh://u:p@host/a", "relative/path", "ext::foo"} {
		_, err := UserOrigin().resolve(uri)
		require.ErrorIs(t, err, KindInvalidSource, uri)
	}
	_, err := Policy{Rewrite: func(string) string { return "file:///tmp/x" }}.resolve("https://github.com/a/b")
	require.ErrorIs(t, err, KindInvalidSource, "rewrite cannot reach file without AllowFile")

	for _, uri := range []string{"owner/repo", "https://example.test/a/b.git", "git@github.com:a/b.git", "ssh://git@github.com/a/b"} {
		_, err = Hardened().resolve(uri)
		require.NoError(t, err, uri)
	}
	resolved, err := Hardened().resolve("owner/repo")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/owner/repo", resolved.url)
	for _, uri := range []string{"http://host:8080/a", "https://host:8443/a/b", "ssh://git@host:2222/a"} {
		_, err = UserOrigin().resolve(uri)
		require.NoError(t, err, uri)
	}
	c := New(Options{Credentials: emptyCredentials(t.TempDir())})
	_, err = c.ResolveRemote(ctx, "https://example.test/a/b", "bad..ref")
	require.ErrorIs(t, err, KindInvalidSource)
	_, err = c.Temporary(ctx, "file:///tmp/x", TemporaryOptions{})
	require.ErrorIs(t, err, KindInvalidSource)
}

func TestConstructDoesNotNeedSubprocess(t *testing.T) {
	t.Setenv("PATH", "")
	client := New(Options{})
	require.NotNil(t, client)
	repo, err := New(Options{TempDir: t.TempDir()}).Temporary(ctx, "https://example.test/a/b", TemporaryOptions{})
	require.NoError(t, err)
	dir := repo.Dir()
	require.NoError(t, repo.Close())
	require.NoError(t, repo.Close(), "Close is idempotent")
	require.NoDirExists(t, dir)
}

func TestResolveRefForms(t *testing.T) {
	f := newFixture(t)
	gitCmd(t, f.bare, "branch", "topic", f.commits[0])
	c := fixtureClient(t)
	for ref, want := range map[string]string{
		"":                            f.commits[2],
		"HEAD":                        f.commits[2],
		"main":                        f.commits[2],
		"refs/heads/main":             f.commits[2],
		"topic":                       f.commits[0],
		"light":                       f.commits[0],
		"annotated":                   f.commits[1],
		"refs/tags/light":             f.commits[0],
		f.commits[1]:                  f.commits[1],
		strings.ToUpper(f.commits[1]): f.commits[1],
	} {
		got, err := c.ResolveRemote(ctx, f.url(), ref)
		require.NoError(t, err, ref)
		require.Equal(t, want, got.Commit, ref)
	}
	head, err := c.ResolveRemote(ctx, f.url(), "")
	require.NoError(t, err)
	require.Equal(t, "main", head.Branch)
	_, err = c.ResolveRemote(ctx, f.url(), "does-not-exist")
	require.ErrorIs(t, err, KindMissingRef)
	_, err = c.ResolveRemote(ctx, "file://"+filepath.Join(t.TempDir(), "nope.git"), "")
	require.ErrorIs(t, err, KindMissingRepository)
}

func TestFetchRefForms(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	cases := map[string]string{"": f.commits[2], "light": f.commits[0], "annotated": f.commits[1], f.commits[0]: f.commits[0]}
	for ref, want := range cases {
		repo := tempRepo(t, c, f.url(), TemporaryOptions{})
		got, err := repo.FetchRef(ctx, ref)
		require.NoError(t, err, ref)
		require.Equal(t, want, got, ref)
		data, err := repo.ReadFile(ctx, got, "skills/a/SKILL.md", 0)
		require.NoError(t, err)
		require.Contains(t, string(data), "v")
	}
}

func TestExactSHAWithoutAdvertisedSupport(t *testing.T) {
	f := newFixture(t)
	c := fixtureClient(t)

	tip := tempRepo(t, c, f.url(), TemporaryOptions{})
	got, err := tip.FetchRef(ctx, f.commits[2])
	require.NoError(t, err)
	require.Equal(t, f.commits[2], got)

	// A non-tip commit needs the full-fetch path, even after a shallow fetch.
	nonTip := tempRepo(t, c, f.url(), TemporaryOptions{})
	_, err = nonTip.FetchRef(ctx, f.commits[2])
	require.NoError(t, err)
	got, err = nonTip.FetchRef(ctx, f.commits[0])
	require.NoError(t, err)
	require.Equal(t, f.commits[0], got)
	hash, err := nonTip.TreeHash(ctx, f.commits[0], "skills/a")
	require.NoError(t, err)
	require.Equal(t, gitCmd(t, f.bare, "rev-parse", f.commits[0]+":skills/a"), hash)

	missing := tempRepo(t, c, f.url(), TemporaryOptions{})
	_, err = missing.FetchRef(ctx, strings.Repeat("a", 40))
	require.ErrorIs(t, err, KindMissingRef, "a missing commit is never substituted")
}

func TestExactSHAWithAdvertisedSupport(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	repo := tempRepo(t, c, f.url(), TemporaryOptions{})
	got, err := repo.FetchRef(ctx, f.commits[0])
	require.NoError(t, err)
	require.Equal(t, f.commits[0], got)
	_, err = repo.FetchRef(ctx, strings.Repeat("a", 40))
	require.ErrorIs(t, err, KindMissingRef)
}

func TestRejects64HexSHA(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	long := strings.Repeat("ab", 32)
	repo := tempRepo(t, c, f.url(), TemporaryOptions{})
	_, err := repo.FetchRef(ctx, long)
	require.ErrorIs(t, err, KindUnsupportedFormat)
	_, err = repo.ReadFile(ctx, long, "x", 0)
	require.ErrorIs(t, err, KindUnsupportedFormat)
	_, err = c.ResolveRemote(ctx, f.url(), long)
	require.ErrorIs(t, err, KindUnsupportedFormat)
}

func TestUnsupportedRepositoryFormats(t *testing.T) {
	for _, formatArg := range []string{"--object-format=sha256", "--ref-format=reftable"} {
		dir := t.TempDir()
		gitCmd(t, dir, "init", "-q", "-b", "main", formatArg)
		writeFile(t, filepath.Join(dir, "x"), "x")
		gitCmd(t, dir, "add", "-A")
		gitCmd(t, dir, "commit", "-q", "-m", "x")
		_, err := fixtureClient(t).OpenUser(dir)
		require.ErrorIs(t, err, KindUnsupportedFormat, formatArg)
	}
}

func TestOpenUserSubdirWorktreeAndMissing(t *testing.T) {
	f := newFixture(t)
	c := fixtureClient(t)
	sub := filepath.Join(f.src, "skills", "a")
	repo, err := c.OpenUser(sub)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)
	require.Equal(t, f.commits[2], head)
	branch, err := repo.Branch()
	require.NoError(t, err)
	require.Equal(t, "main", branch)

	linked := filepath.Join(t.TempDir(), "wt")
	gitCmd(t, f.src, "worktree", "add", "-q", "-b", "linked", linked)
	repo, err = c.OpenUser(linked)
	require.NoError(t, err)
	head, err = repo.Head()
	require.NoError(t, err)
	require.Equal(t, f.commits[2], head)

	_, err = c.OpenUser(t.TempDir())
	require.ErrorIs(t, err, KindMissingRepository)
}

func TestBloblessFetchMatchesCLITrees(t *testing.T) {
	f := newFixture(t, anySHA, "uploadpack.allowFilter=true")
	c := fixtureClient(t)
	repo := tempRepo(t, c, f.url(), TemporaryOptions{})
	for _, commit := range []string{f.commits[2], f.commits[0]} {
		got, err := repo.FetchBlobless(ctx, commit)
		require.NoError(t, err)
		require.Equal(t, commit, got)
		hash, err := repo.TreeHash(ctx, commit, "skills/a")
		require.NoError(t, err)
		require.Equal(t, gitCmd(t, f.bare, "rev-parse", commit+":skills/a"), hash)
		files, err := repo.ListFiles(ctx, commit)
		require.NoError(t, err)
		require.Contains(t, files, "skills/a/SKILL.md")
		root, err := repo.TreeHash(ctx, commit, "")
		require.NoError(t, err)
		require.Equal(t, gitCmd(t, f.bare, "rev-parse", commit+"^{tree}"), root)
	}
	_, err := repo.ReadFile(ctx, f.commits[2], "f1", 0)
	require.ErrorIs(t, err, KindMissingBlob)
	_, err = repo.TreeHash(ctx, f.commits[2], "does/not/exist")
	require.ErrorIs(t, err, KindMissingPath)
}

func TestBloblessFallsBackWithoutFilter(t *testing.T) {
	f := newFixture(t, anySHA)
	repo := tempRepo(t, fixtureClient(t), f.url(), TemporaryOptions{})
	got, err := repo.FetchBlobless(ctx, "main")
	require.NoError(t, err)
	require.Equal(t, f.commits[2], got)
	data, err := repo.ReadFile(ctx, got, "skills/a/SKILL.md", 0)
	require.NoError(t, err)
	require.Equal(t, "v3\n", string(data))
}

func TestReadFileLimit(t *testing.T) {
	f := newFixture(t)
	repo := tempRepo(t, fixtureClient(t), f.url(), TemporaryOptions{})
	commit, err := repo.FetchRef(ctx, "main")
	require.NoError(t, err)
	_, err = repo.ReadFile(ctx, commit, "skills/a/SKILL.md", 1)
	require.ErrorIs(t, err, KindResourceLimit)
	_, err = repo.ReadFile(ctx, commit, "skills", 0)
	require.ErrorIs(t, err, KindMissingPath)
}

func TestCancellationCleansTemporaryRepository(t *testing.T) {
	f := newFixture(t, anySHA)
	parent := t.TempDir()
	c := New(Options{Policy: Policy{AllowFile: true}, Credentials: emptyCredentials(t.TempDir()), TempDir: parent})
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := c.ExportRef(canceled, f.url(), "main", filepath.Join(t.TempDir(), "dest"))
	require.ErrorIs(t, err, KindCanceled)
	require.ErrorIs(t, err, context.Canceled)
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Empty(t, entries)

	// Cancellation after the repository exists also leaves nothing behind.
	repo, err := c.Temporary(ctx, f.url(), TemporaryOptions{})
	require.NoError(t, err)
	commit, err := repo.FetchRef(ctx, "main")
	require.NoError(t, err)
	dest := filepath.Join(t.TempDir(), "dest")
	require.ErrorIs(t, repo.Materialize(canceled, commit, dest), KindCanceled)
	require.NoDirExists(t, dest)
	require.NoError(t, repo.Close())
	entries, err = os.ReadDir(parent)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestContextDeadlineMapsToCanceled(t *testing.T) {
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	f := newFixture(t)
	_, err := fixtureClient(t).ResolveRemote(expired, f.url(), "main")
	require.ErrorIs(t, err, KindCanceled)
}

func TestIdentityLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	f := newFixture(t)
	c := fixtureClient(t)
	repo, err := c.OpenUser(f.src)
	require.NoError(t, err)
	_, err = repo.Identity()
	require.ErrorIs(t, err, KindIdentityMissing)

	writeFile(t, filepath.Join(home, ".gitconfig"), "[user]\n\tname = Global\n\temail = g@example.test\n")
	identity, err := repo.Identity()
	require.NoError(t, err)
	require.Equal(t, Identity{Name: "Global", Email: "g@example.test"}, identity)

	gitCmd(t, f.src, "config", "user.name", "Local")
	repo, err = c.OpenUser(f.src)
	require.NoError(t, err)
	identity, err = repo.Identity()
	require.NoError(t, err)
	require.Equal(t, Identity{Name: "Local", Email: "g@example.test"}, identity)
}

func TestInitCommitPushUserRepository(t *testing.T) {
	remoteRoot := t.TempDir()
	bare := filepath.Join(remoteRoot, "o.git")
	gitCmd(t, remoteRoot, "init", "-q", "--bare", "-b", "main", bare)
	dir := t.TempDir()
	c := fixtureClient(t)
	repo, err := c.InitUser(dir, "main")
	require.NoError(t, err)
	writeFile(t, filepath.Join(dir, "registry.json"), "{}\n")
	identity := Identity{Name: "N", Email: "n@example.test"}
	commit, err := repo.Commit(ctx, CommitOptions{Message: "Initial registry", Identity: identity})
	require.NoError(t, err)
	_, err = repo.Commit(ctx, CommitOptions{Message: "again", Identity: identity})
	require.ErrorIs(t, err, KindNoChanges)
	require.NoError(t, repo.SetOrigin("file://"+bare))
	require.NoError(t, repo.Push(ctx, commit, "main"))
	require.NoError(t, repo.SetUpstream("main"))
	require.Equal(t, commit, gitCmd(t, bare, "rev-parse", "refs/heads/main"))
	origin, err := repo.OriginURL()
	require.NoError(t, err)
	require.Equal(t, "file://"+bare, origin)
	// Reopening an existing repository is not an error, like git init.
	_, err = c.InitUser(dir, "main")
	require.NoError(t, err)
}

// publishCommit makes a commit in a fresh temporary worktree based on commit.
func publishCommit(t *testing.T, c *Client, url, base, file string) (*Repo, string) {
	t.Helper()
	repo := tempRepo(t, c, url, TemporaryOptions{Worktree: true})
	_, err := repo.FetchRef(ctx, base)
	require.NoError(t, err)
	require.NoError(t, repo.PrepareWorktree(ctx, base))
	require.FileExists(t, filepath.Join(repo.Dir(), "f1"))
	writeFile(t, filepath.Join(repo.Dir(), file), file)
	commit, err := repo.Commit(ctx, CommitOptions{Message: file, Paths: []string{file}, Identity: Identity{Name: "N", Email: "n@e.test"}})
	require.NoError(t, err)
	return repo, commit
}

func TestPushLeaseRejectsStaleLeaseAndKeepsRemote(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	base := f.commits[2]
	branch := "studio/one"
	gitCmd(t, f.bare, "branch", branch, base)
	repoA, commitA := publishCommit(t, c, f.url(), base, "a.txt")
	repoB, commitB := publishCommit(t, c, f.url(), base, "b.txt")

	require.NoError(t, repoA.PushLease(ctx, commitA, branch, base), "correct lease succeeds")
	remote := gitCmd(t, f.bare, "rev-parse", "refs/heads/"+branch)
	require.Equal(t, commitA, remote)

	err := repoB.PushLease(ctx, commitB, branch, base)
	require.ErrorIs(t, err, KindLeaseConflict)
	require.Equal(t, commitA, gitCmd(t, f.bare, "rev-parse", "refs/heads/"+branch), "stale lease leaves the remote unchanged")

	_, err = repoB.repo.Reference(plumbing.ReferenceName("refs/heads/"+branch), false)
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound, "scratch refs are removed")
	require.NoError(t, repoB.PushLease(ctx, commitB, branch, commitA), "a fresh lease force-updates")
	require.Equal(t, commitB, gitCmd(t, f.bare, "rev-parse", "refs/heads/"+branch))
	fsck := gitCmd(t, f.bare, "fsck", "--connectivity-only")
	require.NotContains(t, fsck, "missing")

	require.ErrorIs(t, repoA.PushLease(ctx, commitA, "absent/branch", base), KindLeaseConflict, "a lease expects the branch to exist")
}

func TestPushNewBranchFromShallowCommit(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	repo, commit := publishCommit(t, c, f.url(), f.commits[2], "new.txt")
	require.NoError(t, repo.Push(ctx, commit, "studio/new"))
	require.Equal(t, commit, gitCmd(t, f.bare, "rev-parse", "refs/heads/studio/new"))
	require.ErrorIs(t, repo.Push(ctx, commit, "bad..branch"), KindInvalidSource)
}

func TestCommitStagesDeletionAndOnlySelectedPaths(t *testing.T) {
	f := newFixture(t, anySHA)
	c := fixtureClient(t)
	repo := tempRepo(t, c, f.url(), TemporaryOptions{Worktree: true})
	_, err := repo.FetchRef(ctx, f.commits[2])
	require.NoError(t, err)
	require.NoError(t, repo.PrepareWorktree(ctx, f.commits[2]))
	require.NoError(t, os.Remove(filepath.Join(repo.Dir(), "f1")))
	writeFile(t, filepath.Join(repo.Dir(), "unselected"), "x")
	identity := Identity{Name: "N", Email: "n@e.test"}
	commit, err := repo.Commit(ctx, CommitOptions{Message: "drop", Paths: []string{"f1"}, Identity: identity})
	require.NoError(t, err)
	files, err := repo.ListFiles(ctx, commit)
	require.NoError(t, err)
	require.NotContains(t, files, "f1")
	require.NotContains(t, files, "unselected")
}

func TestErrorKindsAreTyped(t *testing.T) {
	err := error(newError(KindAuth, "op", "ref", errors.New("boom")))
	require.Equal(t, KindAuth, KindOf(err))
	require.True(t, errors.Is(err, KindAuth))
	require.False(t, errors.Is(err, KindPermission))
	require.Equal(t, KindFailed, KindOf(errors.New("plain")))
}

// Without exact-SHA wants a non-tip commit needs a full fetch, which must still
// reach it after an earlier shallow fetch of its descendant.
func TestFetchExactNonTipAfterShallowFetch(t *testing.T) {
	f := newFixture(t)
	gitCmd(t, f.bare, "tag", "-d", "light", "annotated")
	repo := tempRepo(t, fixtureClient(t), f.url(), TemporaryOptions{})
	ctx := context.Background()
	_, err := repo.FetchRef(ctx, f.commits[2])
	require.NoError(t, err)
	got, err := repo.FetchRef(ctx, f.commits[0])
	require.NoError(t, err)
	require.Equal(t, f.commits[0], got)
	_, err = repo.FetchRef(ctx, strings.Repeat("a", 40))
	require.ErrorIs(t, err, KindMissingRef)
}
