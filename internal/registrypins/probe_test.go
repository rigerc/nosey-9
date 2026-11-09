package registrypins

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/process"
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
	prober := GitProber{Env: []string{"GIT_ALLOW_PROTOCOL=file"}, TempDir: u.t.TempDir()}
	return prober.Probe(context.Background(), u.uri(), pins)
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

type scriptedRunner struct {
	stderr string
	err    error
}

func (r scriptedRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	return process.Output{Stderr: []byte(r.stderr)}, r.err
}

func TestGitProbeClassifiesListingFailures(t *testing.T) {
	pins := []Pin{{ID: "x", Commit: commitA, SkillName: "x"}}
	confirm := func(answer bool) func(context.Context, string) bool {
		return func(context.Context, string) bool { return answer }
	}
	cases := []struct {
		name    string
		stderr  string
		missing bool
		want    RepoStatus
	}{
		{"confirmed 404", "fatal: could not read Username", true, RepoGone},
		{"private or unconfirmed", "fatal: could not read Username", false, RepoError},
		{"network", "fatal: unable to access: Could not resolve host", true, RepoError},
	}
	for _, c := range cases {
		prober := GitProber{Runner: scriptedRunner{c.stderr, os.ErrInvalid}, RepoMissing: confirm(c.missing), TempDir: t.TempDir()}
		if got := prober.Probe(context.Background(), "https://github.com/o/r", pins)["x"].Repo; got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	malformed := GitProber{Runner: scriptedRunner{}, TempDir: t.TempDir()}
	if got := malformed.Probe(context.Background(), "https://github.com/o/r", pins)["x"].Repo; got != RepoError {
		t.Fatal("empty listing must be an error", got)
	}
}
