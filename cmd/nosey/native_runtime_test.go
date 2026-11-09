package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
)

// nativeRuntime runs the compiled binary with a fully controlled environment,
// so nothing depends on the developer's PATH or Git and gh configuration.
type nativeRuntime struct {
	t         *testing.T
	binary    string
	path      string
	home      string
	workspace string
	env       []string
}

func newNativeRuntime(t *testing.T, path string) *nativeRuntime {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "nosey")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil { //nolint:gosec // fixed tool, test-controlled path
		t.Fatalf("go build: %v\n%s", err, out)
	}
	home := t.TempDir()
	workspace := t.TempDir()
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "prd", "source-planning-package", "example-registry"))
	if err != nil {
		t.Fatal(err)
	}
	config := settings.Defaults()
	config.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(home, ".config", "nosey")
	if err = os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"dependencies":{"next":"1","typescript":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &nativeRuntime{t: t, binary: binary, path: path, home: home, workspace: workspace}
}

func (r *nativeRuntime) run(args ...string) (string, error) {
	r.t.Helper()
	command := exec.Command(r.binary, append([]string{"--cwd", r.workspace}, args...)...) //nolint:gosec // the binary this test just built
	command.Dir = r.workspace
	command.Env = append([]string{
		"PATH=" + r.path, "HOME=" + r.home, "NO_COLOR=1",
		"XDG_CONFIG_HOME=" + filepath.Join(r.home, ".config"),
		"XDG_CACHE_HOME=" + filepath.Join(r.home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(r.home, ".local", "share"),
		// An empty runtime directory has no D-Bus socket, keeping the real keyring out of the run.
		"XDG_RUNTIME_DIR=" + r.home,
	}, r.env...)
	out, err := command.CombinedOutput()
	return string(out), err
}

func (r *nativeRuntime) must(args ...string) string {
	r.t.Helper()
	out, err := r.run(args...)
	if err != nil {
		r.t.Fatalf("nosey %v: %v\n%s", args, err, out)
	}
	return out
}

// exercise runs the everyday commands: nothing here may need git or gh.
func (r *nativeRuntime) exercise() {
	r.t.Helper()
	r.must("doctor")
	r.must("scan", "--json")
	r.must("search", "next", "--json")
	r.must("install", fixtureSkill, "--agent", "codex", "--yes", "--copy")
	lock, err := os.ReadFile(filepath.Join(r.workspace, "skills-lock.json"))
	if err != nil || !strings.Contains(string(lock), `"sourceType": "local"`) {
		r.t.Fatalf("install did not record the skill: %v\n%s", err, lock)
	}
	if out := r.must("update", "--yes"); !strings.Contains(out, "Nothing to update") {
		r.t.Fatalf("a current project has nothing to update:\n%s", out)
	}
	r.must("uninstall", fixtureSkill, "--yes")
}

func TestRuntimeWithoutGitExecutable(t *testing.T) {
	newNativeRuntime(t, "").exercise()
}

// The only PATH entry holds a git that records being called, so the run proves
// git is never even attempted, while gh is absent.
func TestRuntimeWithoutGhExecutable(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "called")
	script := "#!/bin/sh\n: > " + marker + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil { //nolint:gosec // the test's executable stub
		t.Fatal(err)
	}
	newNativeRuntime(t, bin).exercise()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("git was executed: %v", err)
	}
}

func TestNativeLocalInstallation(t *testing.T) {
	runtime := newNativeRuntime(t, "")
	runtime.must("install", fixtureSkill, "--agent", "codex", "--yes", "--copy")
	entries, err := filepath.Glob(filepath.Join(runtime.workspace, ".agents", "skills", "*", "SKILL.md"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("install placed no skill: %v %v", entries, err)
	}
}

func doctorChecks(t *testing.T, out string) map[string]doctorCheck {
	t.Helper()
	var result doctorResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("doctor JSON: %v\n%s", err, out)
	}
	checks := map[string]doctorCheck{}
	for _, check := range result.Checks {
		checks[check.ID] = check
	}
	return checks
}

func TestDoctorWithoutGit(t *testing.T) {
	runtime := newNativeRuntime(t, "")
	checks := doctorChecks(t, runtime.must("doctor", "--json"))
	if _, found := checks["git"]; found {
		t.Fatal("doctor still reports a git executable check")
	}
	if checks["transport"].Status != "pass" || checks["github-credentials"].Code != "NO_CREDENTIALS" || checks["github-credentials"].Status != "pass" {
		t.Fatalf("absent credentials must be informational: %+v", checks)
	}
	if ssh := checks["ssh-credentials"]; ssh.Status != "pass" || ssh.Code != "NO_CREDENTIALS" {
		t.Fatalf("%+v", ssh)
	}

	const secret = "ghp_doctor_must_not_print_this" //nolint:gosec // not a credential: a canary the test asserts is never printed
	runtime.env = []string{"GH_TOKEN=" + secret}
	out := runtime.must("doctor", "--json")
	if strings.Contains(out, secret) {
		t.Fatal("doctor printed a token")
	}
	if got := doctorChecks(t, out)["github-credentials"]; got.Code != "OK" || !strings.Contains(got.Message, "github.com: GH_TOKEN") {
		t.Fatalf("%+v", got)
	}
	if text := runtime.must("doctor"); strings.Contains(text, secret) || !strings.Contains(text, "github.com: GH_TOKEN") {
		t.Fatal(text)
	}

	ghConfig := t.TempDir()
	hosts := "github.com:\n    oauth_token: " + secret + "\n    user: me\n"
	if err := os.WriteFile(filepath.Join(ghConfig, "hosts.yml"), []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.env = []string{"GH_CONFIG_DIR=" + ghConfig}
	out = runtime.must("doctor", "--json")
	if got := doctorChecks(t, out)["github-credentials"]; strings.Contains(out, secret) || !strings.Contains(got.Message, "gh hosts file") {
		t.Fatalf("%+v\n%s", got, out)
	}
}
