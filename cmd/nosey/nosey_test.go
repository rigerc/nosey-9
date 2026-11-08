package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registry"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	return app.Session{Store: store, Resolved: resolved, Catalog: catalog, Registry: reg, Installer: installation.Service{Store: store, Settings: cfg, Runner: runner}}, runner
}
func TestMachineCommandsAndNonTTYContracts(t *testing.T) {
	session, runner := fixtureSession(t)
	open := func(context.Context, string, string, bool) (app.Session, error) { return session, nil }
	for _, test := range []struct {
		name    string
		args    []string
		version int
	}{{"scan", []string{"scan", "--json"}, 2}, {"search", []string{"search", "next", "--json"}, 2}, {"plan", []string{"install", "local:workspace-style", "--agent", "codex", "--dry-run", "--json"}, 3}, {"list", []string{"list", "--json"}, 1}, {"registry-list", []string{"registry", "list", "--json"}, 1}, {"doctor", []string{"doctor", "--json"}, 1}, {"registry-init", []string{"registry", "init", filepath.Join(t.TempDir(), "new-registry"), "--yes", "--json"}, 2}} {
		t.Run(test.name, func(t *testing.T) {
			cmd := newCommandTree(open)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace}, test.args...))
			before := runner.calls
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
			if test.name == "plan" && runner.calls != before {
				t.Fatal("dry-run invoked upstream")
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
	for _, args := range [][]string{{"install", "local:workspace-style", "--agent", "*", "--dry-run"}, {"install", "local:workspace-style", "--json"}, {"scan", "--quality", "invented", "--json"}, {"scan", "--sort-skills", "invented"}, {"registry", "init", filepath.Join(t.TempDir(), "new")}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--public"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--github", "not-a-slug"}, {"registry", "init", filepath.Join(t.TempDir(), "new"), "--yes", "--ci", "gitlab"}} {
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

func TestLocalPreviewNeverPreparesOrPrintsRawCommands(t *testing.T) {
	session, runner := fixtureSession(t)
	open := func(context.Context, string, string, bool) (app.Session, error) { return session, nil }
	for _, mode := range []string{"--dry-run", "--print"} {
		cmd := newCommandTree(open)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--cwd", session.Store.Workspace, "install", "local:workspace-style", "--agent", "codex", mode})
		err := cmd.Execute()
		if mode == "--print" && (err == nil || exitCode(err) != 2) {
			t.Fatal("unprepared local print was not blocked", err)
		}
		if mode == "--dry-run" && err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "'npx'") {
			t.Fatal("unprepared local command printed", out.String())
		}
	}
	if runner.calls != 0 {
		t.Fatal("preview invoked installer")
	}
	if _, err := os.Stat(filepath.Join(session.Store.Data, "install-sources")); !os.IsNotExist(err) {
		t.Fatal("preview populated persistent source store", err)
	}
	if _, err := os.Stat(filepath.Join(session.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("preview wrote project files", err)
	}
}
