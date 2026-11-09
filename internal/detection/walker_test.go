package detection

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/rigerc/nosey-9/internal/settings"
)

// isolateGit points Git and the native reader at an empty home directory.
func isolateGit(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...) //nolint:gosec // test fixture running git with test-controlled arguments
	command.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@a", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@a", "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // test fixture under t.TempDir
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture under t.TempDir
			t.Fatal(err)
		}
	}
}

func newRepo(t *testing.T, args ...string) string {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, append([]string{"init", "-q", "-b", "main"}, args...)...)
	return root
}

func commitAll(t *testing.T, root string) {
	t.Helper()
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "--allow-empty", "-m", "x")
}

func newEngine(root string) *engine {
	return &engine{ctx: context.Background(), root: root, settings: settings.Defaults(), patterns: map[string]*regexp2.Regexp{}, providerErrors: map[string]bool{}}
}

// walked returns the regular file paths the walker kept for root.
func walked(t *testing.T, root string) (*engine, []string) {
	t.Helper()
	e := newEngine(root)
	if err := e.walk(); err != nil {
		t.Fatal(err)
	}
	paths := []string{}
	for _, f := range e.files {
		if !f.directory {
			paths = append(paths, f.path)
		}
	}
	return e, paths
}

func TestNativeScanTrackedIgnoredFile(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{".gitignore": "*.log\n", "kept.log": "x", "dropped.log": "x"})
	gitRun(t, root, "add", "-f", ".gitignore", "kept.log")
	_, paths := walked(t, root)
	if !slices.Contains(paths, "kept.log") {
		t.Fatalf("tracked but ignored file missing: %v", paths)
	}
	if slices.Contains(paths, "dropped.log") {
		t.Fatalf("untracked ignored file listed: %v", paths)
	}
}

func TestNativeScanUntrackedNonignored(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{".gitignore": "out/\n", "new.txt": "x", "out/skip.txt": "x"})
	t.Setenv("PATH", "")
	_, paths := walked(t, root)
	if !slices.Contains(paths, "new.txt") || slices.Contains(paths, "out/skip.txt") {
		t.Fatalf("paths %v, want new.txt without out/skip.txt, with no git on PATH", paths)
	}
}

func TestNativeScanNestedRepo(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{"app/go.mod": "module app\n", "app/main.go": "package main\n", "root.txt": "x"})
	commitAll(t, root)
	nested := filepath.Join(root, "vendored")
	if err := os.Mkdir(nested, 0o755); err != nil { //nolint:gosec // test fixture under t.TempDir
		t.Fatal(err)
	}
	gitRun(t, nested, "init", "-q", "-b", "main")
	write(t, nested, map[string]string{"package.json": "{}", "inner.txt": "x"})
	e, paths := walked(t, root)
	if slices.Contains(paths, "vendored/inner.txt") || slices.Contains(paths, "vendored/package.json") {
		t.Fatalf("nested repository contents listed: %v", paths)
	}
	if !slices.Equal(e.projects, []string{".", "app"}) {
		t.Fatalf("projects %v, want . and app", e.projects)
	}
}

func TestNativeScanSymlinkEscape(t *testing.T) {
	isolateGit(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := newRepo(t)
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skip(err)
	}
	commitAll(t, root)
	e, _ := walked(t, root)
	for _, f := range e.files {
		if f.path == "link.txt" {
			if !f.unsafe || len(f.data) != 0 {
				t.Fatalf("escaping symlink: unsafe=%v data=%q", f.unsafe, f.data)
			}
			return
		}
	}
	t.Fatal("tracked symlink was not observed")
}

func TestNativeScanIndexParity(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, home string) string
	}{
		{"staged additions and deletions", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{"gone.txt": "x", "uncached.txt": "x", "keep.txt": "x"})
			commitAll(t, root)
			write(t, root, map[string]string{"staged.txt": "x", "untracked.txt": "x"})
			gitRun(t, root, "add", "staged.txt")
			gitRun(t, root, "rm", "-q", "gone.txt")
			gitRun(t, root, "rm", "-q", "--cached", "uncached.txt")
			return root
		}},
		{"unborn branch", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{"a.txt": "x", "dir/b.txt": "x"})
			gitRun(t, root, "add", "a.txt")
			return root
		}},
		{"deleted from disk only", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{"a.txt": "x"})
			commitAll(t, root)
			if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
				t.Fatal(err)
			}
			return root
		}},
		{"ignore rules", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{
				".gitignore": "*.log\nbuild/\n/rooted.txt\ndoc/*.tmp\n**/deep.bin\n# comment\n\\#hash.txt\n",
				"a.log":      "x", "build/o": "x", "rooted.txt": "x", "sub/rooted.txt": "x", "doc/a.tmp": "x", "doc/n/a.tmp": "x",
				"x/y/deep.bin": "x", "#hash.txt": "x", "visible.txt": "x", "forced.log": "x",
			})
			gitRun(t, root, "add", "-f", "forced.log")
			return root
		}},
		{"negation", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{
				".gitignore": "*.log\n!keep.log\nonly/*\n!only/keep.txt\nblocked/\n!blocked/not.txt\n",
				"a.log":      "x", "keep.log": "x", "only/a.txt": "x", "only/keep.txt": "x", "blocked/not.txt": "x",
			})
			return root
		}},
		{"nested gitignore", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{
				".gitignore": "*.a\n", "pkg/.gitignore": "!*.a\n*.b\n/local\n", "x.a": "x", "x.b": "x", "local": "x",
				"pkg/x.a": "x", "pkg/x.b": "x", "pkg/local": "x", "pkg/sub/local": "x", "pkg/sub/x.b": "x",
			})
			return root
		}},
		{"info exclude", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{".git/info/exclude": "secret.txt\n", "secret.txt": "x", "open.txt": "x"})
			return root
		}},
		{"configured global excludes", func(t *testing.T, home string) string {
			write(t, home, map[string]string{".gitconfig": "[core]\n\texcludesFile = ~/ignores\n", "ignores": "*.glob\n"})
			root := newRepo(t)
			write(t, root, map[string]string{"a.glob": "x", "b.txt": "x"})
			return root
		}},
		{"XDG default global excludes", func(t *testing.T, home string) string {
			write(t, home, map[string]string{".config/git/ignore": "*.xdg\n"})
			root := newRepo(t)
			write(t, root, map[string]string{"a.xdg": "x", "b.txt": "x"})
			return root
		}},
		{"XDG_CONFIG_HOME global excludes", func(t *testing.T, home string) string {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
			write(t, home, map[string]string{"cfg/git/ignore": "*.cfg\n"})
			root := newRepo(t)
			write(t, root, map[string]string{"a.cfg": "x", "b.txt": "x"})
			return root
		}},
		{"subdirectory root", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{
				".gitignore": "*.skip\n", "outer.txt": "x", "pkg/in.txt": "x", "pkg/in.skip": "x", "pkg/deep/z.txt": "x", "pkgother/o.txt": "x",
			})
			gitRun(t, root, "add", "outer.txt", "pkg/in.txt", "pkgother/o.txt")
			return filepath.Join(root, "pkg")
		}},
		{"root inside an ignored directory", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{".gitignore": "out/\n", "out/forced.txt": "x", "out/other.txt": "x"})
			gitRun(t, root, "add", "-f", "out/forced.txt")
			return filepath.Join(root, "out")
		}},
		{"linked worktree", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{".gitignore": "*.skip\n", "a.txt": "x", "sub/b.txt": "x", ".git/info/exclude": "*.shared\n"})
			commitAll(t, root)
			linked := filepath.Join(t.TempDir(), "linked")
			gitRun(t, root, "worktree", "add", "-q", "-b", "other", linked)
			write(t, linked, map[string]string{"new.txt": "x", "n.skip": "x", "n.shared": "x"})
			gitRun(t, linked, "add", "new.txt")
			return linked
		}},
		{"linked worktree subdirectory", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{"a.txt": "x", "sub/b.txt": "x"})
			commitAll(t, root)
			linked := filepath.Join(t.TempDir(), "linked")
			gitRun(t, root, "worktree", "add", "-q", "-b", "other", linked)
			write(t, linked, map[string]string{"sub/new.txt": "x"})
			return filepath.Join(linked, "sub")
		}},
		{"nested untracked repository", func(t *testing.T, _ string) string {
			root := newRepo(t)
			write(t, root, map[string]string{"a.txt": "x"})
			nested := filepath.Join(root, "inner")
			if err := os.Mkdir(nested, 0o755); err != nil { //nolint:gosec // test fixture under t.TempDir
				t.Fatal(err)
			}
			gitRun(t, nested, "init", "-q")
			write(t, nested, map[string]string{"b.txt": "x"})
			commitAll(t, nested)
			return root
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.setup(t, isolateGit(t))
			want := []string{}
			for name := range strings.SplitSeq(gitRun(t, root, "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "."), "\x00") {
				if name != "" && !strings.HasSuffix(name, "/") {
					want = append(want, name)
				}
			}
			slices.Sort(want)
			got, err := newEngine(root).listRepositoryFiles()
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("native listing differs\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestNativeScanUnsupportedFormatIsAnError(t *testing.T) {
	for _, format := range []string{"--object-format=sha256", "--ref-format=reftable"} {
		t.Run(format, func(t *testing.T) {
			isolateGit(t)
			root := newRepo(t, format)
			write(t, root, map[string]string{"a.txt": "x"})
			_, err := Scan(context.Background(), root, domain.Catalog{}, settings.Defaults())
			var unsupported *UnsupportedRepositoryError
			if !errors.As(err, &unsupported) || !errors.Is(err, gitclient.KindUnsupportedFormat) {
				t.Fatalf("err = %v, want UnsupportedRepositoryError wrapping KindUnsupportedFormat", err)
			}
			for _, part := range []string{root, "SHA-256", "reftable", "git refs migrate"} {
				if !strings.Contains(err.Error(), part) {
					t.Fatalf("message %q lacks %q", err, part)
				}
			}
		})
	}
}

// A sparse checkout sets extensions.worktreeConfig; the listing must still work
// natively so .gitignore is honoured.
func TestNativeScanSparseCheckoutHonoursIgnore(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{".gitignore": "ignored.txt\n", "kept/a.txt": "x", "gone/b.txt": "y"})
	commitAll(t, root)
	gitRun(t, root, "sparse-checkout", "set", "kept")
	if got := strings.TrimSpace(gitRun(t, root, "config", "--get", "extensions.worktreeConfig")); got != "true" {
		t.Fatalf("worktreeConfig = %q", got)
	}
	write(t, root, map[string]string{"ignored.txt": "z"})
	_, paths := walked(t, root)
	if slices.Contains(paths, "ignored.txt") || !slices.Contains(paths, "kept/a.txt") {
		t.Fatalf("paths = %v", paths)
	}
}

// Other extensions go-git refuses are not the storage formats the error names.
func TestNativeScanOtherExtensionsFallBack(t *testing.T) {
	for _, extension := range []string{"partialClone=origin", "worktreeConfig=true"} {
		t.Run(extension, func(t *testing.T) {
			isolateGit(t)
			root := newRepo(t)
			key, value, _ := strings.Cut(extension, "=")
			gitRun(t, root, "config", "core.repositoryformatversion", "1")
			gitRun(t, root, "config", "extensions."+key, value)
			write(t, root, map[string]string{"a.txt": "x"})
			_, paths := walked(t, root)
			if !slices.Contains(paths, "a.txt") {
				t.Fatalf("fallback walk missed a.txt: %v", paths)
			}
		})
	}
}

func TestNativeScanNonRepositoryFallback(t *testing.T) {
	isolateGit(t)
	root := t.TempDir()
	write(t, root, map[string]string{".gitignore": "*.log\n", "a.log": "x", "b.txt": "x"})
	e, paths := walked(t, root)
	if !slices.Contains(paths, "a.log") || !slices.Contains(paths, "b.txt") {
		t.Fatalf("walk must list everything outside a repository: %v", paths)
	}
	if e.truncated {
		t.Fatal("non-repository walk was reported as truncated")
	}
}

func TestNativeScanListingFailureFallsBack(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{".gitignore": "*.log\n", "a.log": "x", ".git/index": "corrupt"})
	_, paths := walked(t, root)
	if !slices.Contains(paths, "a.log") {
		t.Fatalf("corrupt index should fall back to the walk: %v", paths)
	}
}

func TestNativeScanCanceled(t *testing.T) {
	isolateGit(t)
	root := newRepo(t)
	write(t, root, map[string]string{"a.txt": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := newEngine(root)
	e.ctx = ctx
	if err := e.walk(); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
