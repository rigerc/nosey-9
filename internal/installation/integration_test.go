package installation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

// upstreamService runs against the real pinned CLI in a disposable project.
func upstreamService(t *testing.T) Service {
	t.Helper()
	if os.Getenv("NOSEY_UPSTREAM_INTEGRATION") != "1" {
		t.Skip("set NOSEY_UPSTREAM_INTEGRATION=1 for disposable real skills@" + Version + " checks")
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Installation.Telemetry = "disabled"
	s := Service{Store: settings.Store{Workspace: workspace, Cache: t.TempDir(), Data: t.TempDir()}, Settings: cfg, Runner: process.Runner{Timeout: 5 * time.Minute}}
	cli, err := s.resolveCLI(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := os.Getenv("NOSEY_UPSTREAM_BACKEND"); want != "" && cli.Backend != want {
		t.Fatalf("resolved %s backend, want %s", cli.Backend, want)
	}
	return s
}

func writeSkill(t *testing.T, root, directory, name string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: Disposable Nosey compatibility fixture.\n---\n\nFixture.\n"
	if err := storage.Write(filepath.Join(root, directory, "SKILL.md"), []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func installedNames(s Service, c domain.Catalog) map[string]string {
	result := map[string]string{}
	for _, item := range s.Inventory(context.Background(), c).Skills {
		result[item.Name] = item.Status
	}
	return result
}

func TestUpstreamBuildBackgroundInstallAndInventoryForLocalRegistry(t *testing.T) {
	s := upstreamService(t)
	root := t.TempDir()
	writeSkill(t, root, "alpha", "nosey-alpha")
	writeSkill(t, root, "beta", "nosey-beta")
	c := domain.Catalog{Roots: map[string]string{"r": root}, Skills: map[string]domain.Skill{
		"r:alpha": {ID: "r:alpha", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "alpha", SkillName: "nosey-alpha"}},
		"r:beta":  {ID: "r:beta", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "beta", SkillName: "nosey-beta"}},
	}}
	commands, err := s.Build(c, catalog.Recommendations{}, Options{IDs: []string{"r:alpha", "r:beta"}, Agents: []string{"codex", "claude-code"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	cli, err := s.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range commands {
		if err := s.RunSource(context.Background(), cli, command); err != nil {
			t.Fatal(err)
		}
	}

	inventory := s.Inventory(context.Background(), c)
	if !inventory.Complete || len(inventory.Skills) != 2 {
		t.Fatalf("%+v", inventory)
	}
	for _, item := range inventory.Skills {
		if item.Status != "installed" || item.SkillID == nil || item.Unmanaged || !slices.Contains(item.Agents, "claude-code") {
			t.Fatalf("%+v", item)
		}
	}
	// Upstream lists universal agents such as Codex only when it detects the
	// agent on the host. Verify payloads directly so a clean CI runner is valid.
	for _, name := range []string{"nosey-alpha", "nosey-beta"} {
		for _, dir := range []string{".agents", ".claude"} {
			path := filepath.Join(s.Store.Workspace, dir, "skills", name, "SKILL.md")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("installed payload %s: %v", path, err)
			}
		}
	}
	if _, err = os.Stat(filepath.Join(s.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("install wrote nosey project state", err)
	}
}

func TestUpstreamGroupedAddReportsPartialFailure(t *testing.T) {
	s := upstreamService(t)
	root := t.TempDir()
	writeSkill(t, root, "alpha", "nosey-alpha")
	writeSkill(t, root, "beta", "nosey-beta")
	grouped := AddCommand{Source: root, Args: []string{"add", root, "--full-depth", "--skill", "nosey-alpha", "nosey-beta", "--agent", "claude-code", "--yes"}}
	if err := s.Install(context.Background(), []AddCommand{grouped}); err != nil {
		t.Fatal(err)
	}
	if got := installedNames(s, domain.Catalog{}); len(got) != 2 {
		t.Fatal(got)
	}
	partial := t.TempDir()
	writeSkill(t, partial, "gamma", "nosey-gamma")
	missing := AddCommand{Source: partial, Skills: []AddSkill{{Name: "nosey-gamma"}, {Name: "nosey-absent"}}, Args: []string{"add", partial, "--full-depth", "--skill", "nosey-gamma", "nosey-absent", "--agent", "claude-code", "--yes"}}
	if err := s.Install(context.Background(), []AddCommand{missing}); err == nil || !strings.Contains(err.Error(), "nosey-absent") {
		t.Fatal("a skill missing from its source was reported as success:", err)
	}
	if got := installedNames(s, domain.Catalog{}); got["nosey-gamma"] == "" {
		t.Fatal("skills found in the source were not installed", got)
	}
}

// TestUpstreamPinnedRefIsRecordedAndDrivesOutdatedStatus uses a real official-registry pin, the production source form.
func TestUpstreamPinnedRefIsRecordedAndDrivesOutdatedStatus(t *testing.T) {
	s := upstreamService(t)
	const (
		uri  = "https://github.com/vercel-labs/agent-skills"
		pin  = "1a2c79d4e6c4a2283dc2c614b6538cbe1ed5ca62"
		name = "vercel-composition-patterns"
	)
	status := func(ref string) string {
		c := domain.Catalog{Skills: map[string]domain.Skill{"s": {ID: "s", Source: domain.Source{Kind: "git", URI: uri, Ref: ref, SkillName: name}}}}
		return installedNames(s, c)[name]
	}
	c := domain.Catalog{Skills: map[string]domain.Skill{"s": {ID: "s", Source: domain.Source{Kind: "git", URI: uri, Ref: pin, SkillName: name}}}}
	commands, err := s.Build(c, catalog.Recommendations{}, Options{IDs: []string{"s"}, Agents: []string{"claude-code"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Install(context.Background(), commands); err != nil {
		t.Fatal(err)
	}
	lock, err := s.readLock()
	if err != nil || lock.Skills[name].Ref != pin {
		t.Fatalf("lock did not record the pinned commit: %+v %v", lock, err)
	}
	if got := status(pin); got != "installed" {
		t.Fatal(got)
	}
	if got := status(strings.Repeat("b", 40)); got != "outdated" {
		t.Fatal(got)
	}
}

func TestUpstreamInstallNeverRunsWorkspaceCode(t *testing.T) {
	s := upstreamService(t)
	marker := filepath.Join(s.Store.Workspace, "MARKER")
	planted := fmt.Sprintf("#!/bin/sh\ntouch %q\necho %s\n", marker, Version)
	for path, body := range map[string]string{
		"package.json":                     `{"name":"victim","version":"1.0.0","dependencies":{"skills":"` + Version + `"}}`,
		".npmrc":                           "registry=https://attacker.invalid/\n",
		"node_modules/skills/package.json": `{"name":"skills","version":"` + Version + `","bin":{"skills":"cli.sh"}}`,
		"node_modules/skills/cli.sh":       planted,
		"node_modules/.bin/skills":         planted,
		"node_modules/.bin/node":           planted,
	} {
		full := filepath.Join(s.Store.Workspace, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		// #nosec G306 -- planted executables live in a disposable test project.
		if err := os.WriteFile(full, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	writeSkill(t, root, "alpha", "nosey-alpha")
	add := AddCommand{Source: root, Args: []string{"add", root, "--full-depth", "--skill", "nosey-alpha", "--agent", "claude-code", "--yes"}}
	if err := s.Install(context.Background(), []AddCommand{add}); err != nil {
		t.Fatal(err)
	}
	s.Inventory(context.Background(), domain.Catalog{})
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("workspace code was executed")
	}
}
