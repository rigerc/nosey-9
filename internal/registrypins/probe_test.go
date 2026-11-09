package registrypins

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/cgi" //nolint:gosec // the test fixture serves git-http-backend; Httpoxy does not apply
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rigerc/nosey-9/internal/gitclient"
)

type upstream struct {
	t   *testing.T
	dir string
}

func newUpstream(t *testing.T) upstream {
	t.Helper()
	u := upstream{t, filepath.Join(t.TempDir(), "skills")}
	if err := os.MkdirAll(u.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	u.git("init", "-q", "-b", "main")
	u.git("config", "user.email", "test@example.invalid")
	u.git("config", "user.name", "Test")
	u.git("config", "uploadpack.allowFilter", "true")
	u.git("config", "uploadpack.allowAnySHA1InWant", "true")
	return u
}

func (u upstream) git(args ...string) string {
	u.t.Helper()
	// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
	cmd := exec.Command("git", args...)
	cmd.Dir = u.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		u.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (u upstream) commit(files map[string]string) string {
	u.t.Helper()
	for name, content := range files {
		path := filepath.Join(u.dir, name)
		if content == "" {
			if err := os.Remove(path); err != nil {
				u.t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			u.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			u.t.Fatal(err)
		}
	}
	u.git("add", "-A")
	u.git("commit", "-q", "--allow-empty", "-m", "change")
	return u.git("rev-parse", "HEAD")
}

func (u upstream) uri() string { return "file://" + u.dir }

func (u upstream) probe(pins ...Pin) map[string]Probe {
	u.t.Helper()
	prober := GitProber{Client: fileClient(u.t)}
	return prober.Probe(context.Background(), u.uri(), pins)
}

func testCredentials(home string) *gitclient.Credentials {
	return &gitclient.Credentials{
		Getenv:  func(string) string { return "" },
		HomeDir: func() (string, error) { return home, nil },
		Keyring: func(string, string) (string, error) { return "", errors.New("no keyring") },
	}
}

func fileClient(t testing.TB) *gitclient.Client {
	t.Helper()
	return gitclient.New(gitclient.Options{Policy: gitclient.Policy{AllowFile: true}, Credentials: testCredentials(t.TempDir()), TempDir: t.TempDir()})
}

// httpProber dials every remote at server, so GitHub-shaped URIs stay offline.
func httpProber(t *testing.T, server, path string, confirm func(context.Context, string) bool) GitProber {
	t.Helper()
	rewrite := func(string) string { return server + path }
	client := gitclient.New(gitclient.Options{Policy: gitclient.Policy{AllowHTTP: true, Rewrite: rewrite}, Credentials: testCredentials(t.TempDir()), TempDir: t.TempDir()})
	return GitProber{Client: client, RepoMissing: confirm}
}

func TestGitProbeDetectsChangeAndUnrelatedCommits(t *testing.T) {
	u := newUpstream(t)
	first := u.commit(map[string]string{"skills/alpha/SKILL.md": "a1", "skills/beta/SKILL.md": "b1"})
	head := u.commit(map[string]string{"skills/alpha/SKILL.md": "a2"})
	probes := u.probe(Pin{ID: "alpha", Commit: first, SkillName: "alpha"}, Pin{ID: "beta", Commit: first, SkillName: "beta"})
	alpha, beta := probes["alpha"], probes["beta"]
	if alpha.Repo != RepoOK || alpha.Head != head || alpha.HeadHits != 1 || alpha.SameTree == nil || *alpha.SameTree {
		t.Fatalf("alpha changed upstream: %+v", alpha)
	}
	if beta.SameTree == nil || !*beta.SameTree {
		t.Fatalf("beta is unchanged upstream: %+v", beta)
	}
	if got := decide(Pin{Commit: first}, alpha); got.action != actionBump || got.ref != head {
		t.Fatal(got)
	}
}

func TestGitProbeHeadIsPinSkipsInspection(t *testing.T) {
	u := newUpstream(t)
	head := u.commit(map[string]string{"skills/alpha/SKILL.md": "a1"})
	probe := u.probe(Pin{ID: "alpha", Commit: head, SkillName: "alpha"})["alpha"]
	if probe.Repo != RepoOK || probe.Head != head || probe.Inspected {
		t.Fatalf("%+v", probe)
	}
	held := u.probe(Pin{ID: "alpha", Commit: head, SkillName: "alpha", Quarantined: true})["alpha"]
	if !held.Inspected || held.HeadHits != 1 {
		t.Fatalf("quarantined skills must be inspected: %+v", held)
	}
}

func TestGitProbeFindsDeletedAndRootSkills(t *testing.T) {
	u := newUpstream(t)
	first := u.commit(map[string]string{"skills/alpha/SKILL.md": "a1", "SKILL.md": "root"})
	u.commit(map[string]string{"skills/alpha/SKILL.md": "", "SKILL.md": "root changed"})
	probes := u.probe(Pin{ID: "alpha", Commit: first, SkillName: "alpha"}, Pin{ID: "root", Commit: first, SkillName: "skills"})
	if probe := probes["alpha"]; !probe.Inspected || probe.HeadHits != 0 {
		t.Fatalf("deleted skill: %+v", probe)
	}
	if probe := probes["root"]; probe.HeadHits != 1 || probe.SameTree == nil || *probe.SameTree {
		t.Fatalf("repository-root skill: %+v", probe)
	}
}

func TestGitProbeFollowsPinnedPathAmongDuplicates(t *testing.T) {
	u := newUpstream(t)
	first := u.commit(map[string]string{"a/dup/SKILL.md": "one"})
	u.commit(map[string]string{"b/dup/SKILL.md": "two"})
	probe := u.probe(Pin{ID: "dup", Commit: first, SkillName: "dup"})["dup"]
	if probe.HeadHits != 1 || probe.SameTree == nil || !*probe.SameTree {
		t.Fatalf("%+v", probe)
	}
}

func TestGitProbeReportsVanishedPin(t *testing.T) {
	u := newUpstream(t)
	u.commit(map[string]string{"skills/alpha/SKILL.md": "a1"})
	probe := u.probe(Pin{ID: "alpha", Commit: commitA, SkillName: "alpha"})["alpha"]
	if probe.Repo != RepoOK || !probe.PinGone || probe.SameTree != nil {
		t.Fatalf("%+v", probe)
	}
	if got := decide(Pin{Commit: commitA}, probe); got.action != actionBump {
		t.Fatal(got)
	}
}

func answer(value bool, calls *atomic.Int32) func(context.Context, string) bool {
	return func(context.Context, string) bool {
		calls.Add(1)
		return value
	}
}

func statusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(server.Close)
	return server
}

const githubURI = "https://github.com/o/r"

func TestNativeProbeAuthFailureNotRepoGone(t *testing.T) {
	pins := []Pin{{ID: "x", Commit: commitA, SkillName: "x"}}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		server := statusServer(t, status)
		var calls atomic.Int32
		probe := httpProber(t, server.URL, "/o/r", answer(false, &calls)).Probe(context.Background(), githubURI, pins)["x"]
		if probe.Repo != RepoError || calls.Load() != 1 {
			t.Fatalf("%d must stay retryable after an unconfirmed lookup: %+v, %d lookups", status, probe, calls.Load())
		}
		if got := decide(pins[0], probe); got.action == actionMissing {
			t.Fatalf("%d counted as a miss: %+v", status, got)
		}
		if got := httpProber(t, server.URL, "/o/r", answer(true, &calls)).Probe(context.Background(), githubURI, pins)["x"].Repo; got != RepoGone {
			t.Fatalf("%d with a confirmed 404: %v", status, got)
		}
	}
}

func TestGitProbeClassifiesListingFailures(t *testing.T) {
	pins := []Pin{{ID: "x", Commit: commitA, SkillName: "x"}}
	cases := []struct {
		name    string
		status  int
		missing bool
		want    RepoStatus
		lookups int32
	}{
		{"confirmed 404", http.StatusNotFound, true, RepoGone, 1},
		{"unconfirmed 404", http.StatusNotFound, false, RepoError, 1},
		{"server error", http.StatusBadGateway, true, RepoError, 0},
	}
	for _, c := range cases {
		var calls atomic.Int32
		got := httpProber(t, statusServer(t, c.status).URL, "/o/r", answer(c.missing, &calls)).Probe(context.Background(), githubURI, pins)["x"].Repo
		if got != c.want || calls.Load() != c.lookups {
			t.Errorf("%s: got %v after %d lookups, want %v after %d", c.name, got, calls.Load(), c.want, c.lookups)
		}
	}

	var calls atomic.Int32
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if got := httpProber(t, closed.URL, "/o/r", answer(true, &calls)).Probe(context.Background(), githubURI, pins)["x"].Repo; got != RepoError || calls.Load() != 0 {
		t.Fatalf("unreachable host: %v after %d lookups", got, calls.Load())
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	server := statusServer(t, http.StatusNotFound)
	if got := httpProber(t, server.URL, "/o/r", answer(true, &calls)).Probe(canceled, githubURI, pins)["x"].Repo; got != RepoError || calls.Load() != 0 {
		t.Fatalf("canceled probe: %v after %d lookups", got, calls.Load())
	}

	empty := newUpstream(t)
	prober := GitProber{Client: fileClient(t), RepoMissing: answer(true, &calls)}
	if got := prober.Probe(context.Background(), empty.uri(), pins)["x"].Repo; got != RepoError || calls.Load() != 0 {
		t.Fatalf("empty listing must be an error, not a confirmed miss: %v", got)
	}
}

func TestGitProbeWithoutPartialCloneOrExactSHA(t *testing.T) {
	for _, allowFilter := range []string{"false", "true"} {
		u := newUpstream(t)
		u.git("config", "uploadpack.allowFilter", allowFilter)
		u.git("config", "uploadpack.allowAnySHA1InWant", "false")
		first := u.commit(map[string]string{"skills/alpha/SKILL.md": "a1"})
		middle := u.commit(map[string]string{"skills/alpha/SKILL.md": "a2"})
		u.commit(map[string]string{"skills/beta/SKILL.md": "b1"})
		probes := u.probe(
			Pin{ID: "old", Commit: first, SkillName: "alpha"},
			Pin{ID: "same", Commit: middle, SkillName: "alpha"},
			Pin{ID: "gone", Commit: commitA, SkillName: "alpha"},
		)
		if p := probes["old"]; p.Repo != RepoOK || p.PinGone || p.SameTree == nil || *p.SameTree {
			t.Fatalf("filter %s, old: %+v", allowFilter, p)
		}
		if p := probes["same"]; p.SameTree == nil || !*p.SameTree {
			t.Fatalf("filter %s, same: %+v", allowFilter, p)
		}
		if p := probes["gone"]; !p.PinGone || p.SameTree != nil {
			t.Fatalf("filter %s, gone: %+v", allowFilter, p)
		}
	}
}

// backendServer serves u over smart HTTP; reject decides which POSTs fail, by
// their one-based order, and with what status.
func backendServer(t *testing.T, u upstream, reject func(post int32) int) *httptest.Server {
	t.Helper()
	execPath, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{Path: filepath.Join(strings.TrimSpace(string(execPath)), "git-http-backend"), Root: "/", Env: []string{
		"GIT_PROJECT_ROOT=" + u.dir, "GIT_HTTP_EXPORT_ALL=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	}}
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if status := reject(posts.Add(1)); status != 0 {
				w.WriteHeader(status)
				return
			}
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

// A fetch that fails for any reason but a missing commit proves nothing.
func TestNativeProbeFetchFailuresStayUnknown(t *testing.T) {
	u := newUpstream(t)
	first := u.commit(map[string]string{"skills/alpha/SKILL.md": "a1"})
	u.commit(map[string]string{"skills/alpha/SKILL.md": "a2"})
	pin := Pin{ID: "alpha", Commit: first, SkillName: "alpha"}
	var calls atomic.Int32

	headFails := backendServer(t, u, func(int32) int { return http.StatusBadGateway })
	probe := httpProber(t, headFails.URL, "/.git", answer(true, &calls)).Probe(context.Background(), githubURI, []Pin{pin})["alpha"]
	if probe.Repo != RepoError || probe.Inspected || calls.Load() != 0 {
		t.Fatalf("a failed head fetch must not be an empty success: %+v", probe)
	}

	for _, status := range []int{http.StatusForbidden, http.StatusBadGateway} {
		pinFails := backendServer(t, u, func(post int32) int {
			if post > 1 {
				return status
			}
			return 0
		})
		probe = httpProber(t, pinFails.URL, "/.git", answer(true, &calls)).Probe(context.Background(), githubURI, []Pin{pin})["alpha"]
		if probe.Repo != RepoOK || probe.PinGone || probe.SameTree != nil {
			t.Fatalf("%d fetching the pin must not read as a vanished commit: %+v", status, probe)
		}
	}
}

func TestNativeProbeReportsVanishedPinOverHTTP(t *testing.T) {
	u := newUpstream(t)
	u.commit(map[string]string{"skills/alpha/SKILL.md": "a1"})
	server := backendServer(t, u, func(int32) int { return 0 })
	var calls atomic.Int32
	probe := httpProber(t, server.URL, "/.git", answer(false, &calls)).Probe(context.Background(), githubURI, []Pin{{ID: "alpha", Commit: commitA, SkillName: "alpha"}})["alpha"]
	if probe.Repo != RepoOK || !probe.PinGone || probe.SameTree != nil {
		t.Fatalf("%+v", probe)
	}
}

func benchmarkUpstream(b *testing.B) (dir, first, head string) {
	b.Helper()
	dir = b.TempDir()
	git := func(args ...string) string {
		b.Helper()
		// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
		command := exec.Command("git", args...)
		command.Dir = dir
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
		out, err := command.CombinedOutput()
		if err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	git("config", "uploadpack.allowFilter", "true")
	git("config", "uploadpack.allowAnySHA1InWant", "true")
	commit := func(version string) string {
		for i := range 40 {
			blob := make([]byte, 5<<20)
			if _, err := rand.Read(blob); err != nil {
				b.Fatal(err)
			}
			path := filepath.Join(dir, "skills", fmt.Sprintf("s%d", i), "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(path), "asset.bin"), blob, 0o600); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(version), 0o600); err != nil {
				b.Fatal(err)
			}
		}
		git("add", "-A")
		git("commit", "-q", "-m", version)
		return git("rev-parse", "HEAD")
	}
	first = commit("one")
	return dir, first, commit("two")
}

// BenchmarkProbe compares a native probe with the CLI commands it replaced, on
// two commits that each carry 200 MiB of unique large blobs.
func BenchmarkProbe(b *testing.B) {
	dir, first, head := benchmarkUpstream(b)
	uri := "file://" + dir
	b.Run("native", func(b *testing.B) {
		pins := []Pin{{ID: "x", Commit: first, SkillName: "s7"}}
		prober := GitProber{Client: fileClient(b)}
		for b.Loop() {
			probe := prober.Probe(context.Background(), uri, pins)["x"]
			if probe.Repo != RepoOK || probe.Head != head || probe.SameTree == nil {
				b.Fatalf("%+v", probe)
			}
		}
	})
	b.Run("cli", func(b *testing.B) {
		for b.Loop() {
			work := b.TempDir()
			git := func(args ...string) {
				// #nosec G204 -- test helper with a fixed binary and test-controlled arguments.
				command := exec.Command("git", args...)
				command.Dir = work
				command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
				if out, err := command.CombinedOutput(); err != nil {
					b.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			git("ls-remote", uri, "HEAD")
			git("init", "-q")
			git("remote", "add", "origin", uri)
			for _, commit := range []string{head, first} {
				git("fetch", "-q", "--depth=1", "--filter=blob:none", "origin", commit)
				git("ls-tree", "-r", "--name-only", "-z", commit)
				git("rev-parse", commit+":skills/s7")
			}
		}
	})
}
