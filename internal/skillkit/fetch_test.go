package skillkit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/gitpolicy"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/stretchr/testify/require"
)

const remoteURL = "https://github.com/acme/skills.git" // rewritten to a local repository by the test's user git config

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

// userGitConfig points the process at a global Git config that rewrites the
// public URL to repo, like a user's insteadOf/credential settings would, and
// returns its path. Extra config text is appended.
func userGitConfig(t *testing.T, repo, extra string) string {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := "[url \"file://" + repo + "\"]\n\tinsteadOf = " + remoteURL + "\n" + extra
	require.NoError(t, os.WriteFile(cfg, []byte(body), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return cfg
}

func testFetcher(t *testing.T) (Fetcher, string) {
	t.Helper()
	tmp := t.TempDir()
	return Fetcher{TempDir: tmp, allowFile: true}, tmp
}

func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "temporary checkout must be removed")
}

func TestFetchDefaultBranchBranchTagAndPinnedSHA(t *testing.T) {
	repo := newSourceRepo(t)
	userGitConfig(t, repo.dir, "")
	for name, tc := range map[string]struct {
		ref, commit, skill string
		absent             string
	}{
		"default branch": {"", repo.second, "alpha", "featured"},
		"branch":         {"feature", repo.feature, "featured", ""},
		"tag":            {"v1", repo.first, "alpha", ""},
		"pinned SHA":     {repo.first, repo.first, "alpha", "featured"}, // older than the tip of main
		"tip SHA":        {repo.second, repo.second, "alpha", ""},
		"upper-case SHA": {strings.ToUpper(repo.first), repo.first, "alpha", ""},
	} {
		t.Run(name, func(t *testing.T) {
			f, tmp := testFetcher(t)
			co, err := f.Fetch(context.Background(), Source{URI: "acme/skills", Ref: tc.ref})
			require.NoError(t, err)
			// The requested ref is kept as given, separate from the resolved commit.
			require.Equal(t, [3]string{tc.commit, tc.ref, "github"}, [3]string{co.Commit, co.Ref, co.Parsed.Type})
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
	userGitConfig(t, repo.dir, "")
	f, tmp := testFetcher(t)
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
	userGitConfig(t, repo.dir, "")
	f, tmp := testFetcher(t)
	f.Exec = rewriteOutput{inner: process.Runner{Timeout: time.Minute}, match: "rev-parse", stdout: strings.Repeat("b", 40) + "\n"}
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

type rewriteOutput struct {
	inner  process.Executor
	match  string
	stdout string
}

func (r rewriteOutput) Run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	out, err := r.inner.Run(ctx, dir, env, name, args...)
	for _, a := range args {
		if a == r.match {
			out.Stdout = []byte(r.stdout)
		}
	}
	return out, err
}

func TestFetchRejectsInvalidURIAndRefBeforeRunningGit(t *testing.T) {
	rec := &recorder{}
	f := Fetcher{Exec: rec, TempDir: t.TempDir()}
	for _, src := range []Source{
		{URI: "-oProxyCommand=evil"}, {URI: "ext::sh -c touch /tmp/x"}, {URI: "file:///etc/passwd"}, {URI: "http://plain.example/r.git"},
		{URI: "acme/skills", Ref: "--upload-pack=touch /tmp/x"}, {URI: "acme/skills", Ref: "a..b"}, {URI: "acme/skills", Ref: "-x"},
		{URI: "acme/skills", Ref: "bad ref"}, {URI: ""}, {URI: "../x/y"},
	} {
		_, err := f.Fetch(context.Background(), src)
		var se *SourceError
		require.ErrorAs(t, err, &se, "%+v", src)
	}
	require.Empty(t, rec.calls, "no subprocess may start for a rejected source")
	requireEmptyDir(t, f.TempDir)
}

func TestFetchCleansUpOnEveryFailure(t *testing.T) {
	repo := newSourceRepo(t)
	userGitConfig(t, repo.dir, "")
	f, tmp := testFetcher(t)

	_, err := f.Fetch(context.Background(), Source{URI: remoteURL, Ref: "no-such-branch"})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "fetch", fe.Stage)
	requireEmptyDir(t, tmp)

	_, err = f.Fetch(context.Background(), Source{URI: "acme/other-repo"})
	require.ErrorAs(t, err, &fe, "unreachable repository")
	requireEmptyDir(t, tmp)

	missing := f
	missing.Git = filepath.Join(tmp, "no-git-here")
	_, err = missing.Fetch(context.Background(), Source{URI: remoteURL})
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "init", fe.Stage)
	requireEmptyDir(t, tmp)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = f.Fetch(ctx, Source{URI: remoteURL})
	require.ErrorIs(t, err, context.Canceled)
	requireEmptyDir(t, tmp)
}

func TestFetchLocalSourceDoesNotRunGit(t *testing.T) {
	rec := &recorder{}
	dir := t.TempDir()
	co, err := Fetcher{Exec: rec}.Fetch(context.Background(), Source{LocalPath: dir})
	require.NoError(t, err)
	require.Empty(t, rec.calls)
	require.Equal(t, dir, co.Dir)
	require.Equal(t, "local", co.Parsed.Type)
	require.NoError(t, co.Close())
	require.DirExists(t, dir, "closing a local source must not delete the user's directory")
	_, err = Fetcher{}.Fetch(context.Background(), Source{LocalPath: filepath.Join(dir, "missing")})
	require.Error(t, err)
}

// TestFetchUserFilterNeverRuns: a .gitattributes filter defined in the user's Git
// configuration must not run during fetch+checkout, while the same configuration's
// URL rewrite (standing in for credential and transport settings) still reaches fetch.
func TestFetchUserFilterNeverRuns(t *testing.T) {
	repo := newSourceRepo(t)
	marker := filepath.Join(t.TempDir(), "filter-ran")
	require.NoError(t, os.WriteFile(filepath.Join(repo.dir, ".gitattributes"), []byte("* filter=evil\n"), 0o600))
	gitIn(t, repo.dir, "add", ".gitattributes")
	gitIn(t, repo.dir, "commit", "-qm", "attributes")
	userGitConfig(t, repo.dir, "[filter \"evil\"]\n\tsmudge = touch "+marker+" && cat\n\tclean = cat\n\trequired = true\n")

	// Control: with the user's configuration a plain checkout does run the filter,
	// so the assertion below can fail.
	control := t.TempDir()
	// #nosec G204 -- fixed binary with paths from the test's temporary directories.
	cmd := exec.Command("git", "clone", "-q", repo.dir, filepath.Join(control, "c"))
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.FileExists(t, marker, "control: the user's smudge filter must be live")
	require.NoError(t, os.Remove(marker))

	f, tmp := testFetcher(t)
	co, err := f.Fetch(context.Background(), Source{URI: remoteURL})
	require.NoError(t, err, "the user's URL rewrite reached the fetch")
	defer func() { _ = co.Close() }()
	require.NoFileExists(t, marker, "the user's filter ran during fetch or checkout")
	require.FileExists(t, filepath.Join(co.Dir, ".gitattributes"))
	_ = tmp

	// The hardened-only environment cannot reach the repository through the user's
	// rewrite, proving credentials/URL configuration is visible to fetch alone.
	rec := &recorder{inner: process.Runner{Timeout: time.Minute}}
	f.Exec = rec
	co2, err := f.Fetch(context.Background(), Source{URI: remoteURL})
	require.NoError(t, err)
	require.NoError(t, co2.Close())
	var sawFetch bool
	for _, c := range rec.calls {
		global := lastEnv(c.env, "GIT_CONFIG_GLOBAL")
		switch c.stage() {
		case "fetch":
			sawFetch = true
			require.Empty(t, global, "fetch must not null the user configuration")
		default:
			require.Equal(t, os.DevNull, global, "%s must run with nulled user configuration", c.stage())
		}
	}
	require.True(t, sawFetch)
}

func lastEnv(env []string, key string) string {
	val := ""
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			val = v
		}
	}
	return val
}

type call struct {
	env  []string
	name string
	args []string
}

// stage names the git subcommand, skipping the leading -c settings.
func (c call) stage() string {
	for i := 0; i < len(c.args); i++ {
		if c.args[i] == "-c" {
			i++
			continue
		}
		return c.args[i]
	}
	return ""
}

type recorder struct {
	mu    sync.Mutex
	calls []call
	inner process.Executor
}

func (r *recorder) Run(ctx context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	r.mu.Lock()
	r.calls = append(r.calls, call{env: append([]string(nil), env...), name: name, args: append([]string(nil), args...)})
	r.mu.Unlock()
	if r.inner != nil {
		return r.inner.Run(ctx, dir, env, name, args...)
	}
	return process.Output{Stderr: []byte("fake failure")}, errors.New("fake failure")
}

// TestFetchGitInvocation pins the exact hardening of each Git command.
func TestFetchGitInvocation(t *testing.T) {
	rec := &recorder{}
	tmp := t.TempDir()
	f := Fetcher{Exec: rec, TempDir: tmp}
	_, err := f.Fetch(context.Background(), Source{URI: "acme/skills", Ref: "v1"})
	require.Error(t, err)
	require.Len(t, rec.calls, 1, "stops at the first failing command")
	require.Equal(t, "init", rec.calls[0].stage())

	// Let init and remote succeed to reach fetch.
	seq := &sequence{ok: 2}
	f.Exec = seq
	_, err = f.Fetch(context.Background(), Source{URI: "acme/skills", Ref: "v1"})
	require.Error(t, err)
	require.Len(t, seq.calls, 3)
	hardened := gitpolicy.HardenedArgs()
	for _, c := range seq.calls {
		require.Equal(t, "git", c.name)
		require.Equal(t, hardened, c.args[:len(hardened)], "%s uses HardenedArgs", c.stage())
		require.Contains(t, c.env, "GIT_TERMINAL_PROMPT=0")
		require.Contains(t, c.env, "GIT_LFS_SKIP_SMUDGE=1")
		require.Equal(t, "https:ssh", lastEnv(c.env, "GIT_ALLOW_PROTOCOL"), "production code can only use https and ssh")
		require.Equal(t, "0", lastEnv(c.env, "GIT_CONFIG_COUNT"))
		require.NotEmpty(t, lastEnv(c.env, "GIT_DIR"))
		require.Equal(t, filepath.Join(lastEnv(c.env, "GIT_WORK_TREE"), ".git"), lastEnv(c.env, "GIT_DIR"))
	}
	require.Equal(t, []string{"init", "remote", "fetch"}, []string{seq.calls[0].stage(), seq.calls[1].stage(), seq.calls[2].stage()})
	require.Equal(t, os.DevNull, lastEnv(seq.calls[0].env, "GIT_CONFIG_GLOBAL"))
	require.Equal(t, os.DevNull, lastEnv(seq.calls[1].env, "GIT_CONFIG_GLOBAL"))
	fetch := seq.calls[2]
	require.Empty(t, lastEnv(fetch.env, "GIT_CONFIG_GLOBAL"), "only fetch sees the user configuration")
	require.Empty(t, lastEnv(fetch.env, "GIT_CONFIG_NOSYSTEM"))
	require.Contains(t, fetch.args, "--depth=1")
	require.Contains(t, fetch.args, "origin")
	require.Equal(t, "v1", fetch.args[len(fetch.args)-1])
	require.Contains(t, fetch.args, "core.hooksPath="+os.DevNull)
	require.Contains(t, fetch.args, "protocol.ext.allow=never")
	require.Contains(t, fetch.args, "submodule.recurse=false")
	require.Equal(t, []string{"remote", "add", "origin", "https://github.com/acme/skills.git"}, seq.calls[1].args[len(hardened):])
	requireEmptyDir(t, tmp)

	// Complete run with a fake that succeeds: checkout must be hardened and detached.
	ok := &sequence{ok: 99, stdout: strings.Repeat("c", 40)}
	f.Exec = ok
	co, err := f.Fetch(context.Background(), Source{URI: "acme/skills"})
	require.NoError(t, err)
	require.NoError(t, co.Close())
	var stages []string
	for _, c := range ok.calls {
		stages = append(stages, c.stage())
	}
	require.Equal(t, []string{"init", "remote", "fetch", "checkout", "rev-parse"}, stages)
	checkout := ok.calls[3]
	require.Equal(t, os.DevNull, lastEnv(checkout.env, "GIT_CONFIG_GLOBAL"))
	require.Equal(t, "1", lastEnv(checkout.env, "GIT_CONFIG_NOSYSTEM"))
	require.Equal(t, []string{"checkout", "--quiet", "--detach", "FETCH_HEAD"}, checkout.args[len(hardened):])
	require.Equal(t, "HEAD", ok.calls[2].args[len(ok.calls[2].args)-1], "default branch fetches HEAD")
	require.Equal(t, strings.Repeat("c", 40), co.Commit)
}

type sequence struct {
	calls  []call
	ok     int
	stdout string
}

func (s *sequence) Run(_ context.Context, _ string, env []string, name string, args ...string) (process.Output, error) {
	s.calls = append(s.calls, call{env: append([]string(nil), env...), name: name, args: append([]string(nil), args...)})
	if len(s.calls) > s.ok {
		return process.Output{Stderr: []byte("stop")}, errors.New("stop")
	}
	return process.Output{Stdout: []byte(s.stdout)}, nil
}

// TestFetchFileProtocolIsNotAvailableToProductionCode shows the test-only file
// transport hook is closed by default: without it git refuses a file:// remote.
func TestFetchFileProtocolIsNotAvailableToProductionCode(t *testing.T) {
	repo := newSourceRepo(t)
	userGitConfig(t, repo.dir, "")
	tmp := t.TempDir()
	_, err := Fetcher{TempDir: tmp}.Fetch(context.Background(), Source{URI: remoteURL})
	var fe *FetchError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, "fetch", fe.Stage)
	require.Contains(t, fe.Stderr, "not allowed")
	requireEmptyDir(t, tmp)
}

func TestFetchErrorsCarryStageAndStderr(t *testing.T) {
	err := &FetchError{Stage: "fetch", Err: errors.New("exit status 128"), Stderr: "fatal: not found\n"}
	require.Equal(t, "git fetch failed: exit status 128: fatal: not found", err.Error())
	require.ErrorIs(t, err, err.Err)
}
