package skillkit

import (
	"context"
	"errors"
	"net/http/cgi" //nolint:gosec // test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/gitclient"
	"github.com/stretchr/testify/require"
)

const remoteURL = "https://github.com/acme/skills.git" // rewritten to a local repository by testFetcher

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// gitIn runs git in dir with a clean configuration; test setup only.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

type sourceRepo struct {
	dir           string
	first, second string // commit hashes on main
	feature       string // commit on branch "feature"
	tagged        string // commit tagged v1 (== first)
}

// newSourceRepo builds main (two commits), a v1 tag on the first and a "feature" branch.
func newSourceRepo(t *testing.T) sourceRepo {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "uploadpack.allowAnySHA1InWant", "true")
	writeSkill(t, filepath.Join(dir, "skills", "alpha"), "alpha", map[string]string{"scripts/run.sh": "echo 1\n"})
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "first")
	r := sourceRepo{dir: dir}
	r.first = gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "tag", "v1")
	r.tagged = r.first
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	writeSkill(t, filepath.Join(dir, "skills", "featured"), "featured", nil)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "feature")
	r.feature = gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skills", "alpha", "scripts", "run.sh"), []byte("echo 2\n"), 0o600))
	gitIn(t, dir, "commit", "-qam", "second")
	r.second = gitIn(t, dir, "rev-parse", "HEAD")
	return r
}

// testCredentials resolves no token, key or agent from the machine running the tests.
func testCredentials(home string) *gitclient.Credentials {
	return &gitclient.Credentials{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return home, nil },
		Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
	}
}

// testFetcher dials repo for remoteURL through the file transport; any other URL
// reaches a repository that does not exist, so no test touches the network.
func testFetcher(t *testing.T, repo string) (Fetcher, string) {
	t.Helper()
	tmp := t.TempDir()
	missing := "file://" + filepath.Join(t.TempDir(), "missing.git")
	rewrite := func(url string) string {
		if url == remoteURL {
			return "file://" + repo
		}
		return missing
	}
	client := gitclient.New(gitclient.Options{Policy: gitclient.Policy{AllowFile: true, Rewrite: rewrite}, Credentials: testCredentials(t.TempDir()), TempDir: tmp})
	return Fetcher{TempDir: tmp, Client: client}, tmp
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "temporary checkout must be removed")
}

func TestFetchDefaultBranchBranchTagAndPinnedSHA(t *testing.T) {
	repo := newSourceRepo(t)
	gitIn(t, repo.dir, "tag", "-a", "annotated", "-m", "t", repo.feature)
	for name, tc := range map[string]struct {
		ref, commit, skill string
		absent             string
	}{
		"default branch": {"", repo.second, "alpha", "featured"},
		"branch":         {"feature", repo.feature, "featured", ""},
		"tag":            {"v1", repo.first, "alpha", ""},
		"annotated tag":  {"annotated", repo.feature, "featured", ""},
		"pinned SHA":     {repo.first, repo.first, "alpha", "featured"}, // older than the tip of main
		"tip SHA":        {repo.second, repo.second, "alpha", ""},
		"upper-case SHA": {strings.ToUpper(repo.first), repo.first, "alpha", ""},
	} {
		t.Run(name, func(t *testing.T) {
			f, tmp := testFetcher(t, repo.dir)
			co, err := f.Fetch(context.Background(), Source{URI: "acme/skills", Ref: tc.ref})
			require.NoError(t, err)
			// The requested ref is kept as given, separate from the resolved commit.
			require.Equal(t, [3]string{tc.commit, tc.ref, "github"}, [3]string{co.Commit, co.Ref, co.Parsed.Type})
			require.NoDirExists(t, filepath.Join(co.Dir, ".git"), "a checkout is a plain tree")
			skills := mustDiscover(t, co.Dir, DiscoverOptions{})
			var names []string
			for _, s := range skills {
				names = append(names, s.Name)
			}
			require.Contains(t, names, tc.skill)
			if tc.absent != "" {
				require.NotContains(t, names, tc.absent)
			}
			require.NoError(t, co.Close())
			require.NoError(t, co.Close(), "Close is idempotent")
			requireEmptyDir(t, tmp)
		})
	}
}

// TestFetchPinnedSHAIsExact is the regression for skills-go running
// `clone --branch <sha>` and for silently falling back to another revision.
func TestFetchPinnedSHAIsExact(t *testing.T) {
	repo := newSourceRepo(t)
	f, tmp := testFetcher(t, repo.dir)
	co, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: repo.first})
	require.NoError(t, err)
	defer func() { _ = co.Close() }()
	require.Equal(t, repo.first, co.Commit)
	require.Equal(t, "echo 1\n", readFile(t, filepath.Join(co.Dir, "skills/alpha/scripts/run.sh")), "the pinned revision, not the tip")

	// An unknown SHA fails; there is no fallback to the default branch.
	_, err = f.Fetch(context.Background(), Source{URI: remoteURL, Ref: strings.Repeat("a", 40)})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "fetch", fe.Stage)
	entries, _ := os.ReadDir(tmp)
	require.Len(t, entries, 1, "only the successful checkout remains")
}

func TestFetchFailsWhenHeadDiffersFromRequestedSHA(t *testing.T) {
	repo := newSourceRepo(t)
	f, tmp := testFetcher(t, repo.dir)
	f.reportedCommit = func(string) string { return strings.Repeat("b", 40) }
	_, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: repo.first})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "verify", fe.Stage)
	require.ErrorIs(t, err, ErrCommitMismatch)
	requireEmptyDir(t, tmp)

	// A branch name is not compared with HEAD, only full SHAs are.
	co, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: "feature"})
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("b", 40), co.Commit)
	require.NoError(t, co.Close())
}

func TestFetchRejectsInvalidURIAndRefBeforeAnyIO(t *testing.T) {
	f := Fetcher{TempDir: t.TempDir()}
	for _, src := range []Source{
		{URI: "-oProxyCommand=evil"}, {URI: "ext::sh -c touch /tmp/x"}, {URI: "file:///etc/passwd"}, {URI: "http://plain.example/r.git"},
		{URI: "acme/skills", Ref: "--upload-pack=touch /tmp/x"}, {URI: "acme/skills", Ref: "a..b"}, {URI: "acme/skills", Ref: "-x"},
		{URI: "acme/skills", Ref: "bad ref"}, {URI: ""}, {URI: "../x/y"},
	} {
		_, err := f.Fetch(context.Background(), src)
		var se *SourceError
		require.ErrorAs(t, err, &se, "%+v", src)
	}
	requireEmptyDir(t, f.TempDir)
}

func TestFetchCleansUpOnEveryFailure(t *testing.T) {
	repo := newSourceRepo(t)
	f, tmp := testFetcher(t, repo.dir)

	_, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: "no-such-branch"})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "fetch", fe.Stage)
	requireEmptyDir(t, tmp)

	require.ErrorIs(t, err, gitclient.KindMissingRef)

	_, err = f.Fetch(context.Background(), Source{URI: "acme/other-repo"})
	require.ErrorAs(t, err, &fe, "unreachable repository")
	require.Equal(t, "fetch", fe.Stage)
	require.ErrorIs(t, err, gitclient.KindMissingRepository)
	requireEmptyDir(t, tmp)

	_, err = f.Fetch(context.Background(), Source{URI: remoteURL, Ref: strings.Repeat("c", 64)})
	require.ErrorAs(t, err, &fe, "64-character SHA")
	require.Equal(t, "fetch", fe.Stage)
	require.ErrorIs(t, err, gitclient.KindUnsupportedFormat)
	requireEmptyDir(t, tmp)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.Fetch(ctx, Source{URI: remoteURL})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, gitclient.KindCanceled)
	requireEmptyDir(t, tmp)

	limited := Fetcher{TempDir: tmp, Client: gitclient.New(gitclient.Options{
		Policy:      gitclient.Policy{AllowFile: true, Rewrite: func(string) string { return "file://" + repo.dir }},
		Credentials: testCredentials(t.TempDir()), TempDir: tmp, Limits: gitclient.Limits{MaxFiles: 1},
	})}
	_, err = limited.Fetch(context.Background(), Source{URI: remoteURL})
	require.ErrorAs(t, err, &fe, "a failure while writing the tree")
	require.Equal(t, "checkout", fe.Stage)
	require.ErrorIs(t, err, gitclient.KindResourceLimit)
	requireEmptyDir(t, tmp)
}

func TestFetchLocalSourceDoesNotRunGit(t *testing.T) {
	dir := t.TempDir()
	co, err := Fetcher{}.Fetch(context.Background(), Source{LocalPath: dir})
	require.NoError(t, err)
	require.Empty(t, co.Commit)
	require.Equal(t, dir, co.Dir)
	require.Equal(t, "local", co.Parsed.Type)
	require.NoError(t, co.Close())
	require.DirExists(t, dir, "closing a local source must not delete the user's directory")
	_, err = Fetcher{}.Fetch(context.Background(), Source{LocalPath: filepath.Join(dir, "missing")})
	require.Error(t, err)
}

// TestFetchUserFilterNeverRuns: a .gitattributes filter defined in the user's Git
// configuration must not run during a fetch, and that configuration is not consulted.
func TestFetchUserFilterNeverRuns(t *testing.T) {
	repo := newSourceRepo(t)
	marker := filepath.Join(t.TempDir(), "filter-ran")
	hookMarker := filepath.Join(t.TempDir(), "hook-ran")
	require.NoError(t, os.WriteFile(filepath.Join(repo.dir, ".gitattributes"), []byte("* filter=evil\n"), 0o600))
	gitIn(t, repo.dir, "add", ".gitattributes")
	gitIn(t, repo.dir, "commit", "-qm", "attributes")
	hooks := t.TempDir()
	// #nosec G306 -- the hook must be executable for the control to prove anything.
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\ntouch "+hookMarker+"\n"), 0o700))
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[core]\n\thooksPath = " + hooks + "\n[filter \"evil\"]\n\tsmudge = touch " + marker + " && cat\n\tclean = cat\n\trequired = true\n"
	require.NoError(t, os.WriteFile(cfg, []byte(body), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	// Control: with the user's configuration a plain clone does run both, so the
	// assertions below can fail.
	// #nosec G204 -- fixed binary with paths from the test's temporary directories.
	cmd := exec.Command("git", "clone", "-q", repo.dir, filepath.Join(t.TempDir(), "c"))
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, marker, "control: the user's smudge filter must be live")
	require.FileExists(t, hookMarker, "control: the user's hook must be live")
	require.NoError(t, os.Remove(marker))
	require.NoError(t, os.Remove(hookMarker))

	f, _ := testFetcher(t, repo.dir)
	co, err := f.Fetch(context.Background(), Source{URI: remoteURL})
	require.NoError(t, err)
	defer func() { _ = co.Close() }()
	require.NoFileExists(t, marker, "the user's filter ran during the fetch")
	require.NoFileExists(t, hookMarker, "the user's hook ran during the fetch")
	require.FileExists(t, filepath.Join(co.Dir, ".gitattributes"))
}

// TestFetchFileTransportNeedsTheTestSeam: a file remote stays closed without AllowFile,
// even when a rewrite tries to reach one.
func TestFetchFileTransportNeedsTheTestSeam(t *testing.T) {
	repo := newSourceRepo(t)
	tmp := t.TempDir()
	rewrite := func(string) string { return "file://" + repo.dir }
	client := gitclient.New(gitclient.Options{Policy: gitclient.Policy{Rewrite: rewrite}, Credentials: testCredentials(t.TempDir()), TempDir: tmp})
	_, err := Fetcher{TempDir: tmp, Client: client}.Fetch(context.Background(), Source{URI: remoteURL})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "init", fe.Stage)
	require.ErrorIs(t, err, gitclient.KindInvalidSource)
	requireEmptyDir(t, tmp)
}

// TestFetchWithoutExactSHASupport uses a server that advertises no exact-SHA wants:
// tips and older commits still resolve, and an unknown SHA is never substituted.
func TestFetchWithoutExactSHASupport(t *testing.T) {
	repo := newSourceRepo(t)
	gitIn(t, repo.dir, "config", "--unset", "uploadpack.allowAnySHA1InWant")
	f, tmp := testFetcher(t, repo.dir)
	for name, tc := range map[string]struct{ ref, content string }{
		"tip":   {repo.second, "echo 2\n"},
		"older": {repo.first, "echo 1\n"},
	} {
		t.Run(name, func(t *testing.T) {
			co, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: tc.ref})
			require.NoError(t, err)
			require.Equal(t, tc.ref, co.Commit)
			require.Equal(t, tc.content, readFile(t, filepath.Join(co.Dir, "skills/alpha/scripts/run.sh")))
			require.NoError(t, co.Close())
		})
	}
	_, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: strings.Repeat("a", 40)})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "fetch", fe.Stage)
	require.ErrorIs(t, err, gitclient.KindMissingRef)
	requireEmptyDir(t, tmp)
}

// TestFetchMatchesGitCheckout compares a fetch with a real clone of the same commit:
// line endings, modes, symlinks (including one leaving the repository), discovered
// skills and every computedHash. The clone's .git directory is what a checkout used
// to carry; it must have no effect on discovery or hashing.
func TestFetchMatchesGitCheckout(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	writeSkill(t, dir, "root-skill", map[string]string{"notes.txt": "root\nnotes\n"})
	writeSkill(t, filepath.Join(dir, "skills", "crlf"), "crlf", map[string]string{"docs/a.md": "line one\nline two\n", "docs/auto.txt": "auto\ntext\n", "raw.bin": "binary\nbody\n"})
	writeSkill(t, filepath.Join(dir, "skills", "linked"), "linked", map[string]string{"scripts/run.sh": "#!/bin/sh\n"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.md text eol=crlf\n*.txt text=auto eol=crlf\n*.bin -text\n"), 0o600))
	// #nosec G302 -- the executable bit is the property under test.
	require.NoError(t, os.Chmod(filepath.Join(dir, "skills/linked/scripts/run.sh"), 0o700))
	require.NoError(t, os.Symlink("scripts/run.sh", filepath.Join(dir, "skills/linked/inside")))
	require.NoError(t, os.Symlink("../../../outside", filepath.Join(dir, "skills/linked/escaping")))
	require.NoError(t, os.Symlink("/etc/hostname", filepath.Join(dir, "skills/linked/absolute")))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "all")

	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", dir, clone)
	require.Contains(t, readFile(t, filepath.Join(clone, "skills/crlf/SKILL.md")), "\r\n", "control: the clone converted line endings")
	require.DirExists(t, filepath.Join(clone, ".git"))

	f, _ := testFetcher(t, dir)
	co, err := f.Fetch(context.Background(), Source{URI: remoteURL})
	require.NoError(t, err)
	defer func() { _ = co.Close() }()
	require.NoDirExists(t, filepath.Join(co.Dir, ".git"))

	var cloneTree []string
	for _, entry := range listTree(t, clone) {
		if !strings.Contains(entry, " .git/") && entry != "d .git" {
			cloneTree = append(cloneTree, entry)
		}
	}
	require.Equal(t, cloneTree, listTree(t, co.Dir))

	summarize := func(root string) map[string][2]string {
		out := map[string][2]string{}
		for _, skill := range mustDiscover(t, root, DiscoverOptions{FullDepth: true}) {
			hash, herr := FolderHash(filepath.Join(root, filepath.FromSlash(skill.Path)))
			require.NoError(t, herr)
			out[skill.Name] = [2]string{skill.Path, hash}
		}
		return out
	}
	want := summarize(clone)
	require.Len(t, want, 3)
	require.Equal(t, want, summarize(co.Dir))
}

// TestFetchRunsWithEmptyPATH proves the client starts no subprocess: PATH is empty
// while it fetches over smart HTTP from git-http-backend, which the test's server
// starts by absolute path. The file transport would fail here.
func TestFetchRunsWithEmptyPATH(t *testing.T) {
	repo := newSourceRepo(t)
	backend := filepath.Join(gitIn(t, repo.dir, "--exec-path"), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git-http-backend is not installed")
	}
	root := t.TempDir()
	gitIn(t, root, "clone", "-q", "--bare", repo.dir, filepath.Join(root, "skills.git"))
	server := httptest.NewServer(&cgi.Handler{Path: backend, Root: "/", Env: []string{
		"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
		"PATH=" + os.Getenv("PATH"),
	}})
	defer server.Close()

	tmp := t.TempDir()
	rewrite := func(string) string { return server.URL + "/skills.git" }
	client := gitclient.New(gitclient.Options{Policy: gitclient.Policy{AllowHTTP: true, Rewrite: rewrite}, Credentials: testCredentials(t.TempDir()), TempDir: tmp})
	f := Fetcher{TempDir: tmp, Client: client}
	t.Setenv("PATH", "")
	for _, tc := range []struct{ ref, commit string }{{"", repo.second}, {"v1", repo.first}, {repo.second, repo.second}} {
		co, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: tc.ref})
		require.NoError(t, err, tc.ref)
		require.Equal(t, tc.commit, co.Commit)
		require.FileExists(t, filepath.Join(co.Dir, "skills/alpha/SKILL.md"))
		require.NoError(t, co.Close())
	}
	requireEmptyDir(t, tmp)
}

func TestSkillkitProductionCodeRunsNoGit(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data := readFile(t, name)
		require.NotContains(t, data, `"os/exec"`, name)
		require.NotContains(t, data, "internal/process", name)
	}
}

func TestFetchErrorsCarryStageAndStderr(t *testing.T) {
	err := &FetchError{Stage: "fetch", Err: errors.New("exit status 128"), Stderr: "fatal: not found\n"}
	require.Equal(t, "git fetch failed: exit status 128: fatal: not found", err.Error())
	require.ErrorIs(t, err, err.Err)
}
