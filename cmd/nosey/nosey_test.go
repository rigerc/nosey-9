package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// gitRunner answers the Git prerequisite probe so tests do not depend on the machine.
type gitRunner struct {
	calls int
	err   error
}

func (r *gitRunner) Run(_ context.Context, _ string, _ []string, name string, _ ...string) (process.Output, error) {
	r.calls++
	if r.err != nil {
		return process.Output{}, r.err
	}
	return process.Output{Stdout: []byte(name + " version 2.50.0")}, nil
}

func fixtureSession(t *testing.T) (app.Session, *gitRunner) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	workspace := t.TempDir()
	if err := storage.Write(filepath.Join(workspace, "package.json"), []byte(`{"dependencies":{"next":"1","typescript":"1"}}`)); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "docs", "prd", "source-planning-package", "example-registry"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := settings.New(workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	resolved := settings.Resolved{Settings: cfg, Diagnostics: []domain.Diagnostic{}}
	reg := registry.Service{Store: store}
	catalog, err := reg.Load(context.Background(), resolved, false)
	if err != nil {
		t.Fatal(err)
	}
	runner := &gitRunner{}
	return app.Session{Store: store, Resolved: resolved, Catalog: catalog, Registry: reg, Installer: installation.Service{Store: store, Settings: cfg, Runner: runner, Home: t.TempDir()}}, runner
}
func TestMachineCommandsAndNonTTYContracts(t *testing.T) {
	session, _ := fixtureSession(t)
	open := func(context.Context, string, string, bool) (app.Session, error) { return session, nil }
	for _, test := range []struct {
		name    string
		args    []string
		version int
	}{{"scan", []string{"scan", "--json"}, 2}, {"search", []string{"search", "next", "--json"}, 2}, {"list", []string{"list", "--json"}, 1}, {"registry-list", []string{"registry", "list", "--json"}, 1}, {"doctor", []string{"doctor", "--json"}, 1}, {"registry-init", []string{"registry", "init", filepath.Join(t.TempDir(), "new-registry"), "--yes", "--json"}, 2}} {
		t.Run(test.name, func(t *testing.T) {
			cmd := newCommandTree(open)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace}, test.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err, out.String())
			}
			var data struct {
				Version int `json:"outputVersion"`
			}
			if err := json.Unmarshal(out.Bytes(), &data); err != nil {
				t.Fatal("stdout is not pure JSON", err, out.String())
			}
			if data.Version != test.version {
				t.Fatal(data)
			}
		})
	}
}
func TestRefreshFlagForcesRegistryRefresh(t *testing.T) {
	session, _ := fixtureSession(t)
	for _, test := range []struct {
		args []string
		want bool
	}{{[]string{"scan", "--json"}, false}, {[]string{"scan", "--json", "--refresh"}, true}, {[]string{"--refresh", "search", "next", "--json"}, true}, {[]string{"list", "--json", "--refresh"}, true}} {
		var got bool
		cmd := newCommandTree(func(_ context.Context, _, _ string, refresh bool) (app.Session, error) {
			got = refresh
			return session, nil
		})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace}, test.args...))
		if err := cmd.Execute(); err != nil {
			t.Fatal(test.args, err, out.String())
		}
		if got != test.want {
			t.Fatalf("%v: refresh = %t, want %t", test.args, got, test.want)
		}
	}
}
func TestIncompleteStrictAndUsageErrors(t *testing.T) {
	session, _ := fixtureSession(t)
	session.Resolved.Settings.Detection.MaxBytes = 1
	open := func(context.Context, string, string, bool) (app.Session, error) { return session, nil }
	for _, args := range [][]string{{"install", "local:workspace-style", "--agent", "*", "--yes"}, {"scan", "--quality", "invented", "--json"}, {"scan", "--sort-skills", "invented"}, {"registry", "init", filepath.Join(t.TempDir(), "new")}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--public"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--github", "not-a-slug"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--ci", "gitlab"}, {"registry", "pins", "update", t.TempDir(), "--quarantine", "maybe"}, {"registry", "pins", "check", t.TempDir()}, {"registry", "ci", "update", t.TempDir()}} {
		cmd := newCommandTree(open)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace}, args...))
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid command accepted", args)
		}
	}
	if _, err := os.Stat(filepath.Join(session.Store.Workspace, ".agents")); !os.IsNotExist(err) {
		t.Fatal("blocked commands installed files")
	}
}

const fixtureSkill = "local:workspace-style"

// runInstall executes `nosey install` against the fixture.
func runInstall(t *testing.T, session app.Session, args ...string) (string, error) {
	t.Helper()
	cmd := newCommandTree(func(context.Context, string, string, bool) (app.Session, error) { return session, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace, "install"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func skillName(session app.Session) string {
	return session.Catalog.Skills[fixtureSkill].Source.SkillName
}

func TestInstallYesInstallsIntoTheWorkspaceAndReportsProgress(t *testing.T) {
	session, runner := fixtureSession(t)
	out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes", "--copy")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"[1/1] Installing " + skillName(session), "installed " + skillName(session) + " for codex"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	installed := filepath.Join(session.Store.Workspace, ".agents", "skills", skillName(session), "SKILL.md")
	if _, err = os.Stat(installed); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(session.Store.Workspace, "skills-lock.json"))
	if err != nil || !strings.Contains(string(lock), `"`+skillName(session)+`"`) || !strings.Contains(string(lock), `"sourceType": "local"`) {
		t.Fatal(string(lock), err)
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("install wrote nosey project state", err)
	}
	if runner.calls == 0 {
		t.Fatal("the Git prerequisite was not checked")
	}
}

func TestInstallSurfacesSkippedAgents(t *testing.T) {
	session, _ := fixtureSession(t)
	out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--agent", "windsurf", "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "skipped windsurf for "+skillName(session)) || !strings.Contains(out, "project directory does not exist") {
		t.Fatal(out)
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, ".windsurf")); !os.IsNotExist(err) {
		t.Fatal("a skipped agent's directory was created")
	}
}

func TestInstallFailureExitsFiveAndKeepsCompletedWork(t *testing.T) {
	session, _ := fixtureSession(t)
	session.Installer.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "blocked", Name: "Blocked", SkillsDir: ".blocked/skills"}}
	if err := os.WriteFile(filepath.Join(session.Store.Workspace, ".blocked"), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--agent", "blocked", "--yes", "--copy")
	if err == nil || exitCode(err) != 5 {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "failed for blocked") {
		t.Fatal(out)
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal("completed work was not recorded:", err)
	}
}

// lockSaboteur makes the lock unwritable after the final preflight, then fetches normally.
type lockSaboteur struct{ workspace string }

func (l lockSaboteur) Fetch(ctx context.Context, src skillkit.Source) (*skillkit.Checkout, error) {
	if err := os.Mkdir(filepath.Join(l.workspace, "skills-lock.json"), 0o755); err != nil {
		return nil, err
	}
	return skillkit.Fetcher{}.Fetch(ctx, src)
}

func TestInstallWithAFailedLockWriteExitsFive(t *testing.T) {
	session, _ := fixtureSession(t)
	session.Installer.Fetcher = lockSaboteur{session.Store.Workspace}
	out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes", "--copy")
	if err == nil || exitCode(err) != 5 || !strings.Contains(err.Error(), "skills-lock.json could not be updated") {
		t.Fatal("an unrecorded install was not reported as incomplete:", err, out)
	}
}

func TestInstallExitCodesForPolicyPrerequisiteAndBusyWorkspace(t *testing.T) {
	session, runner := fixtureSession(t)
	if err := os.WriteFile(filepath.Join(session.Store.Workspace, "skills-lock.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes"); err == nil || exitCode(err) != 2 {
		t.Fatal("a broken lock is a policy error:", err, out)
	}
	if err := os.Remove(filepath.Join(session.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
	runner.err = errors.New("git: not found")
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes"); err == nil || exitCode(err) != 6 {
		t.Fatal("missing Git is exit 6:", err, out)
	}
	runner.err = nil
	session.Installer.LockTimeout = 100 * time.Millisecond
	op, err := session.Installer.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = op.Close() }()
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes"); err == nil || exitCode(err) != 2 || !strings.Contains(err.Error(), "another Nosey operation is running") {
		t.Fatal("a busy workspace is refused:", err, out)
	}
}

func TestInstallRejectsRemovedPrintFlag(t *testing.T) {
	session, runner := fixtureSession(t)
	out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--print")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --print") {
		t.Fatal(err, out)
	}
	if runner.calls != 0 {
		t.Fatal("rejected flag started an operation", runner.calls)
	}
}

func TestInstallRefusesWithoutChangingTheProject(t *testing.T) {
	session, _ := fixtureSession(t)
	for name, args := range map[string][]string{
		"no skills":          {"--agent", "codex", "--yes"},
		"yes without agents": {fixtureSkill, "--yes"},
		"no terminal":        {fixtureSkill, "--agent", "codex"},
		"unknown agent":      {fixtureSkill, "--agent", "invented", "--yes"},
		"unknown skill":      {"local:missing", "--agent", "codex", "--yes"},
		"dry-run removed":    {fixtureSkill, "--dry-run"},
		"json removed":       {fixtureSkill, "--json"},
		"approve removed":    {fixtureSkill, "--approve-plan", "x"},
		"verbose removed":    {fixtureSkill, "--verbose"},
		"source removed":     {"--source", "o/r", "--skill", "x"},
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := runInstall(t, session, args...); err == nil {
				t.Fatal("accepted", out)
			}
			for _, path := range []string{".agents", "skills-lock.json"} {
				if _, err := os.Stat(filepath.Join(session.Store.Workspace, path)); !os.IsNotExist(err) {
					t.Fatal("a refused install created", path)
				}
			}
		})
	}
}

func TestInstallAcceptsCustomAgents(t *testing.T) {
	session, _ := fixtureSession(t)
	session.Installer.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "my-agent", Name: "My Agent", SkillsDir: ".my-agent/skills"}}
	out, err := runInstall(t, session, fixtureSkill, "--agent", "my-agent", "--yes", "--copy")
	if err != nil {
		t.Fatal(err, out)
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, ".my-agent", "skills", skillName(session), "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestListChecksUpdatesNatively(t *testing.T) {
	session, _ := fixtureSession(t)
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes", "--copy"); err != nil {
		t.Fatal(err, out)
	}
	cmd := newCommandTree(func(context.Context, string, string, bool) (app.Session, error) { return session, nil })
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--cwd", session.Store.Workspace, "list", "--json", "--check-updates"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err, out.String(), stderr.String())
	}
	var data struct {
		Skills []struct {
			Name   string `json:"skillName"`
			Status string `json:"status"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(out.Bytes(), &data); err != nil || len(data.Skills) != 1 || data.Skills[0].Name != skillName(session) {
		t.Fatal(err, out.String())
	}
	if stderr.Len() != 0 {
		t.Fatal("a complete check printed a warning:", stderr.String())
	}
}

func TestDoctorChecksOnlyGit(t *testing.T) {
	session, runner := fixtureSession(t)
	open := func(context.Context, string, string, bool) (app.Session, error) { return session, nil }
	run := func() (doctorResult, error) {
		cmd := newCommandTree(open)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--cwd", session.Store.Workspace, "doctor", "--json"})
		err := cmd.Execute()
		var result doctorResult
		if jsonErr := json.Unmarshal(out.Bytes(), &result); jsonErr != nil {
			t.Fatal(jsonErr, out.String())
		}
		return result, err
	}
	result, err := run()
	ids := []string{}
	for _, check := range result.Checks {
		ids = append(ids, check.ID)
	}
	if err != nil || !slices.Equal(ids, []string{"settings", "git"}) {
		t.Fatal(ids, err)
	}
	runner.err = errors.New("git: not found")
	if _, err = run(); err == nil || exitCode(err) != 6 {
		t.Fatal("missing Git must exit 6:", err)
	}
}
