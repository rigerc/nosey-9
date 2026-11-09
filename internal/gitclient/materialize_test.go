package gitclient

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

// craftTree stores a commit whose root tree contains exactly the given entries,
// bypassing any checks a normal commit path would apply.
type craftEntry struct {
	name string
	mode filemode.FileMode
	body string
	sub  []craftEntry
}

func storeBlob(t *testing.T, repo *git.Repository, body string) plumbing.Hash {
	t.Helper()
	blob := repo.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)
	w, err := blob.Writer()
	require.NoError(t, err)
	_, err = w.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	hash, err := repo.Storer.SetEncodedObject(blob)
	require.NoError(t, err)
	return hash
}

func storeTree(t *testing.T, repo *git.Repository, entries []craftEntry) plumbing.Hash {
	t.Helper()
	tree := &object.Tree{}
	for _, entry := range entries {
		var hash plumbing.Hash
		if entry.mode == filemode.Dir {
			hash = storeTree(t, repo, entry.sub)
		} else {
			hash = storeBlob(t, repo, entry.body)
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{Name: entry.name, Mode: entry.mode, Hash: hash})
	}
	sort.Sort(object.TreeEntrySorter(tree.Entries))
	encoded := repo.Storer.NewEncodedObject()
	require.NoError(t, tree.Encode(encoded))
	hash, err := repo.Storer.SetEncodedObject(encoded)
	require.NoError(t, err)
	return hash
}

func craftCommit(t *testing.T, r *Repo, entries []craftEntry) string {
	t.Helper()
	signature := object.Signature{Name: "a", Email: "a@a"}
	commit := &object.Commit{Author: signature, Committer: signature, Message: "m", TreeHash: storeTree(t, r.repo, entries)}
	encoded := r.repo.Storer.NewEncodedObject()
	require.NoError(t, commit.Encode(encoded))
	hash, err := r.repo.Storer.SetEncodedObject(encoded)
	require.NoError(t, err)
	return hash.String()
}

func craftRepo(t *testing.T, limits Limits) *Repo {
	t.Helper()
	c := New(Options{Policy: Policy{AllowFile: true}, Credentials: emptyCredentials(t.TempDir()), TempDir: t.TempDir(), Limits: limits})
	return tempRepo(t, c, "file:///nonexistent", TemporaryOptions{})
}

const (
	reg  = filemode.Regular
	link = filemode.Symlink
	dir  = filemode.Dir
)

func TestMaterializeRejectsMaliciousPaths(t *testing.T) {
	cases := map[string][]craftEntry{
		"traversal":      {{name: "..", mode: dir, sub: []craftEntry{{name: "evil", mode: reg}}}},
		"nested dotdot":  {{name: "a", mode: dir, sub: []craftEntry{{name: "..", mode: dir, sub: []craftEntry{{name: "x", mode: reg}}}}}},
		"git dir":        {{name: ".git", mode: dir, sub: []craftEntry{{name: "config", mode: reg}}}},
		"git dir case":   {{name: ".GIT", mode: dir, sub: []craftEntry{{name: "config", mode: reg}}}},
		"git dir ntfs":   {{name: "git~1", mode: dir, sub: []craftEntry{{name: "config", mode: reg}}}},
		"git dir dotted": {{name: ".git. ", mode: dir, sub: []craftEntry{{name: "config", mode: reg}}}},
		"nested git":     {{name: "a", mode: dir, sub: []craftEntry{{name: ".git", mode: dir, sub: []craftEntry{{name: "hooks", mode: reg}}}}}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			repo := craftRepo(t, Limits{})
			dest := filepath.Join(t.TempDir(), "dest")
			err := repo.Materialize(context.Background(), craftCommit(t, repo, entries), dest)
			require.ErrorIs(t, err, KindInvalidSource)
			require.NoDirExists(t, dest, "a rejected tree leaves nothing behind")
		})
	}
}

func TestMaterializeRejectsEscapingSymlinks(t *testing.T) {
	cases := map[string][]craftEntry{
		"absolute":      {{name: "l", mode: link, body: "/etc/passwd"}},
		"dotdot":        {{name: "l", mode: link, body: "../outside"}},
		"nested climb":  {{name: "a", mode: dir, sub: []craftEntry{{name: "l", mode: link, body: "../../outside"}}}},
		"into git":      {{name: "l", mode: link, body: ".git/config"}},
		"empty":         {{name: "l", mode: link, body: ""}},
		"through chain": {{name: "d", mode: dir, sub: []craftEntry{{name: "up", mode: link, body: ".."}}}, {name: "x", mode: link, body: "d/up/.."}},
		"loop":          {{name: "a", mode: link, body: "b"}, {name: "b", mode: link, body: "a"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			repo := craftRepo(t, Limits{})
			err := repo.Materialize(context.Background(), craftCommit(t, repo, entries), filepath.Join(t.TempDir(), "dest"))
			require.ErrorIs(t, err, KindInvalidSource)
		})
	}
}

func TestMaterializeVerbatimLinksKeepsTargetsAndConfinesWrites(t *testing.T) {
	repo := craftRepo(t, Limits{})
	commit := craftCommit(t, repo, []craftEntry{
		{name: "absolute", mode: link, body: "/etc/passwd"},
		{name: "climb", mode: link, body: "../outside"},
		{name: "file", mode: reg, body: "kept"},
	})
	parent := t.TempDir()
	dest := filepath.Join(parent, "dest")
	require.NoError(t, repo.MaterializeVerbatimLinks(context.Background(), commit, dest))
	for name, want := range map[string]string{"absolute": "/etc/passwd", "climb": "../outside"} {
		got, err := os.Readlink(filepath.Join(dest, name))
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	require.NoFileExists(t, filepath.Join(parent, "outside"))

	unsafe := craftCommit(t, repo, []craftEntry{{name: ".git", mode: dir, sub: []craftEntry{{name: "config", mode: reg}}}})
	require.ErrorIs(t, repo.MaterializeVerbatimLinks(context.Background(), unsafe, filepath.Join(parent, "other")), KindInvalidSource, "only link targets are exempt")
}

func TestMaterializeSafeTree(t *testing.T) {
	repo := craftRepo(t, Limits{})
	commit := craftCommit(t, repo, []craftEntry{
		{name: "dir", mode: dir, sub: []craftEntry{
			{name: "file", mode: reg, body: "hello"},
			{name: "tool", mode: filemode.Executable, body: "#!/bin/sh\n"},
			{name: "sibling", mode: link, body: "file"},
			{name: "up", mode: link, body: "../top"},
		}},
		{name: "top", mode: reg, body: "t"},
		{name: "dangling", mode: link, body: "does-not-exist"},
		{name: "module", mode: filemode.Submodule, body: ""},
	})
	dest := filepath.Join(t.TempDir(), "dest")
	require.NoError(t, repo.Materialize(context.Background(), commit, dest))
	data, err := os.ReadFile(filepath.Join(dest, "dir/file")) //nolint:gosec // test fixture under t.TempDir
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))
	info, err := os.Stat(filepath.Join(dest, "dir/tool"))
	require.NoError(t, err)
	require.NotZero(t, info.Mode().Perm()&0o100)
	target, err := os.Readlink(filepath.Join(dest, "dir/sibling"))
	require.NoError(t, err)
	require.Equal(t, "file", target)
	require.NoFileExists(t, filepath.Join(dest, "module"))
	require.NoDirExists(t, filepath.Join(dest, "module"))
}

func TestMaterializeLimitsAndNonEmptyDestination(t *testing.T) {
	entries := []craftEntry{{name: "a", mode: reg, body: "12345"}, {name: "b", mode: reg, body: "12345"}}
	cases := []Limits{{MaxFiles: 1}, {MaxFileBytes: 4}, {MaxTotalBytes: 8}}
	for _, limits := range cases {
		repo := craftRepo(t, limits)
		commit := craftCommit(t, repo, entries)
		dest := filepath.Join(t.TempDir(), "dest")
		require.ErrorIs(t, repo.Materialize(context.Background(), commit, dest), KindResourceLimit)
		require.NoDirExists(t, dest)
	}
	repo := craftRepo(t, Limits{})
	commit := craftCommit(t, repo, entries)
	occupied := t.TempDir()
	writeFile(t, filepath.Join(occupied, "keep"), "x")
	require.Error(t, repo.Materialize(context.Background(), commit, occupied))
	require.FileExists(t, filepath.Join(occupied, "keep"))
}

func TestMaterializeGitattributesMatchesCLI(t *testing.T) {
	attributes := "*.txt text eol=crlf\n*.auto text=auto eol=crlf\n*.lf text eol=lf\n*.bin binary\nnested/** -text\n"
	bodies := map[string]string{
		"a.txt":        "one\ntwo\n",
		"mixed.txt":    "one\r\ntwo\n",
		"b.auto":       "one\ntwo\n",
		"c.auto":       "has\rcr\nline\n",
		"d.lf":         "one\ntwo\n",
		"e.bin":        "one\ntwo\n",
		"f.other":      "one\ntwo\n",
		"nested/g.txt": "one\ntwo\n",
		"sub/h.txt":    "one\ntwo\n",
		"sub/i.md":     "one\ntwo\n",
	}
	src := t.TempDir()
	gitCmd(t, src, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(src, ".gitattributes"), attributes)
	writeFile(t, filepath.Join(src, "sub/.gitattributes"), "*.txt -text\n*.md eol=crlf\n")
	for name, body := range bodies {
		writeFile(t, filepath.Join(src, name), body)
	}
	gitCmd(t, src, "add", "-A")
	gitCmd(t, src, "commit", "-q", "-m", "m")
	commit := gitCmd(t, src, "rev-parse", "HEAD")
	bare := filepath.Join(t.TempDir(), "o.git")
	gitCmd(t, filepath.Dir(bare), "clone", "-q", "--bare", src, bare)
	cli := filepath.Join(t.TempDir(), "cli")
	gitCmd(t, filepath.Dir(cli), "clone", "-q", bare, cli)

	repo := tempRepo(t, fixtureClient(t), "file://"+bare, TemporaryOptions{})
	got, err := repo.FetchRef(context.Background(), commit)
	require.NoError(t, err)
	dest := filepath.Join(t.TempDir(), "dest")
	require.NoError(t, repo.Materialize(context.Background(), got, dest))
	for name := range bodies {
		want, err := os.ReadFile(filepath.Join(cli, name)) //nolint:gosec // test fixture under t.TempDir
		require.NoError(t, err, name)
		have, err := os.ReadFile(filepath.Join(dest, name)) //nolint:gosec // test fixture under t.TempDir
		require.NoError(t, err, name)
		require.Equal(t, string(want), string(have), name)
	}
	txt, err := os.ReadFile(filepath.Join(dest, "a.txt")) //nolint:gosec // test fixture under t.TempDir
	require.NoError(t, err)
	require.Equal(t, "one\r\ntwo\r\n", string(txt))
}
