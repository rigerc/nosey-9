package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

type emptyInventoryRunner struct{ calls int }

func (r *emptyInventoryRunner) Run(_ context.Context, _ string, _ []string, _ string, args ...string) (process.Output, error) {
	r.calls++
	if len(args) > 0 && args[len(args)-1] == "--version" {
		v := "v22.20.0"
		if len(args) > 1 {
			v = installation.Version
		}
		return process.Output{Stdout: []byte(v)}, nil
	}
	return process.Output{Stdout: []byte(`[]`)}, nil
}
func fixtureSession(t *testing.T) (app.Session, *emptyInventoryRunner) {
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
	runner := &emptyInventoryRunner{}
	return app.Session{Store: store, Resolved: resolved, Catalog: catalog, Registry: reg, Installer: installation.Service{Store: store, Settings: cfg, Runner: runner, Resolver: func(context.Context) (installation.CLI, error) {
		return installation.CLI{Name: "npx", Args: []string{"--yes", "skills@" + installation.Version}, Backend: "npx"}, nil
	}}}, runner
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
	for _, args := range [][]string{{"install", "local:workspace-style", "--agent", "*", "--print"}, {"scan", "--quality", "invented", "--json"}, {"scan", "--sort-skills", "invented"}, {"registry", "init", filepath.Join(t.TempDir(), "new")}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--public"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--github", "not-a-slug"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--ci", "gitlab"}, {"registry", "pins", "update", t.TempDir(), "--quarantine", "maybe"}, {"registry", "pins", "check", t.TempDir()}, {"registry", "ci", "update", t.TempDir()}} {
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

type attachCall struct {
	dir  string
	name string
	args []string
}

// runInstall executes `nosey install` against the fixture and records terminal-attached runs.
func runInstall(t *testing.T, session app.Session, attachErr error, args ...string) ([]attachCall, string, error) {
	t.Helper()
	calls := []attachCall{}
	session.Installer.Attach = func(_ context.Context, dir string, _ []string, name string, argv ...string) error {
		calls = append(calls, attachCall{dir, name, argv})
		return attachErr
	}
	cmd := newCommandTree(func(context.Context, string, string, bool) (app.Session, error) { return session, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace, "install"}, args...))
	err := cmd.Execute()
	return calls, out.String(), err
}

func TestInstallYesRunsTheSkillsCLIInTheWorkspace(t *testing.T) {
	session, _ := fixtureSession(t)
	lock := `{"version":1,"skills":{"` + session.Catalog.Skills[fixtureSkill].Source.SkillName + `":{"source":"local"}}}`
	if err := storage.Write(filepath.Join(session.Store.Workspace, "skills-lock.json"), []byte(lock)); err != nil {
		t.Fatal(err)
	}
	calls, out, err := runInstall(t, session, nil, fixtureSkill, "--agent", "codex", "--yes", "--copy")
	if err != nil {
		t.Fatal(err, out)
	}
	if len(calls) != 1 || calls[0].dir != session.Store.Workspace || calls[0].name != "npx" {
		t.Fatalf("%+v", calls)
	}
	joined := strings.Join(calls[0].args, " ")
	for _, want := range []string{"add ", "--skill", "--agent codex", "--copy", "--yes"} {
		if !strings.Contains(joined, want) {
			t.Fatal(want, joined)
		}
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("install wrote nosey project state", err)
	}
}

func TestInstallFailureExitsFive(t *testing.T) {
	session, _ := fixtureSession(t)
	_, _, err := runInstall(t, session, errors.New("exit status 1"), fixtureSkill, "--agent", "codex", "--yes")
	if err == nil || exitCode(err) != 5 {
		t.Fatal(err)
	}
}

func TestInstallPrintRunsNothing(t *testing.T) {
	for name, mutate := range map[string]func(*app.Session) []string{
		"flag": func(*app.Session) []string { return []string{"--print"} },
		"setting": func(s *app.Session) []string {
			s.Resolved.Settings.Installation.Mode = "print"
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			session, runner := fixtureSession(t)
			extra := mutate(&session)
			calls, out, err := runInstall(t, session, nil, append([]string{fixtureSkill, "--agent", "codex"}, extra...)...)
			if err != nil {
				t.Fatal(err, out)
			}
			if len(calls) != 0 || runner.calls != 0 {
				t.Fatal("print ran the skills CLI", calls, runner.calls)
			}
			if !strings.HasPrefix(out, "'npx' '--yes' 'skills@"+installation.Version+"' 'add' ") {
				t.Fatal(out)
			}
		})
	}
}

func TestInstallRefusesWithoutRunningTheCLI(t *testing.T) {
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
			if calls, out, err := runInstall(t, session, nil, args...); err == nil || len(calls) != 0 {
				t.Fatal("accepted", calls, out)
			}
		})
	}
}
