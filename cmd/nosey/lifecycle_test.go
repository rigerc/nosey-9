package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/app"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

func runCLI(t *testing.T, session app.Session, args ...string) (string, error) {
	t.Helper()
	cmd := newCommandTree(func(context.Context, string, string, bool) (app.Session, error) { return session, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--cwd", session.Store.Workspace}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func installFixture(t *testing.T, session app.Session) string {
	t.Helper()
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--agent", "claude-code", "--yes", "--copy"); err != nil {
		t.Fatal(err, out)
	}
	return filepath.Join(session.Store.Workspace, ".agents", "skills", skillName(session))
}

func TestUpdateRefreshesLocallyChangedSkillsAndReportsTheReason(t *testing.T) {
	session := fixtureSession(t)
	dir := installFixture(t, session)
	if out, err := runCLI(t, session, "update", "--yes"); err != nil || !strings.Contains(out, "Nothing to update.") {
		t.Fatal("a current project has nothing to update:", err, out)
	}
	edited := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(edited, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, session, "update", "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	name := skillName(session)
	for _, want := range []string{"[1/1] Updating " + name, "updated " + name + " (upstream content changed) at ", ".agents/skills/" + name + " (copy)", ".claude/skills/" + name + " (copy)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if _, err = os.Stat(edited); !os.IsNotExist(err) {
		t.Fatal("the local edit was not replaced")
	}
	// A named, current skill is reported rather than touched.
	out, err = runCLI(t, session, "update", fixtureSkill, "--yes")
	if err != nil || !strings.Contains(out, "skipped "+name+": already up to date") || !strings.Contains(out, "Nothing to update.") {
		t.Fatal(err, out)
	}
}

type failingFetcher struct{}

func (failingFetcher) Fetch(context.Context, skillkit.Source) (*skillkit.Checkout, error) {
	return nil, errors.New("network down")
}

func TestUpdateRefusalsAndExitCodes(t *testing.T) {
	session := fixtureSession(t)
	dir := installFixture(t, session)
	edited := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(edited, []byte("local edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runCLI(t, session, "update"); err == nil || exitCode(err) != 2 || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatal("non-interactive update must require --yes:", err, out)
	}
	if out, err := runCLI(t, session, "update", "local:missing", "--yes"); err == nil || exitCode(err) != 2 {
		t.Fatal("an unknown skill is a policy error:", err, out)
	}
	if _, err := os.Stat(edited); err != nil {
		t.Fatal("a refused update changed the project")
	}
	// A source that cannot be checked stops the command with exit 5.
	session.Installer.Fetcher = failingFetcher{}
	out, err := runCLI(t, session, "update", "--yes")
	if err == nil || exitCode(err) != 5 || !strings.Contains(err.Error(), "update check failed") || strings.Contains(out, "Nothing to update") {
		t.Fatal("a failed check must stop the command:", err, out)
	}
	session.Installer.LockTimeout = 100 * time.Millisecond
	op, err := session.Installer.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = op.Close() }()
	if out, err = runCLI(t, session, "uninstall", skillName(session), "--yes"); err == nil || exitCode(err) != 2 || !strings.Contains(err.Error(), "another Nosey operation is running") {
		t.Fatal("a busy workspace is refused:", err, out)
	}
}

func TestUninstallByNameAndByCatalogID(t *testing.T) {
	session := fixtureSession(t)
	dir := installFixture(t, session)
	name := skillName(session)
	out, err := runCLI(t, session, "uninstall", name, "--yes")
	if err != nil {
		t.Fatal(err, out)
	}
	for _, want := range []string{"[1/1] Removing " + name, "removed " + name} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if _, err = os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the skill is still installed")
	}
	if _, err = os.Stat(filepath.Join(session.Store.Workspace, ".claude", "skills", name)); !os.IsNotExist(err) {
		t.Fatal("an agent copy is still installed")
	}
	lock, err := skillkit.ReadLock(session.Store.Workspace)
	if err != nil || len(lock.Skills) != 0 {
		t.Fatal(err, lock.Skills)
	}
	installFixture(t, session)
	if out, err = runCLI(t, session, "uninstall", fixtureSkill, "--yes"); err != nil {
		t.Fatal("a catalog ID selects the tracked skill:", err, out)
	}
}

func TestUninstallRefusalsLeaveTheProjectAlone(t *testing.T) {
	session := fixtureSession(t)
	dir := installFixture(t, session)
	for name, args := range map[string][]string{
		"no terminal":  {"uninstall", skillName(session)},
		"unknown":      {"uninstall", "nope", "--yes"},
		"one untacked": {"uninstall", skillName(session), "nope", "--yes"},
		"no targets":   {"uninstall", "--yes"},
		"json removed": {"uninstall", skillName(session), "--json"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := runCLI(t, session, args...)
			if err == nil || exitCode(err) != 2 {
				t.Fatal("accepted or wrong exit code:", err, out)
			}
			if _, err = os.Stat(dir); err != nil {
				t.Fatal("a refused uninstall removed files")
			}
		})
	}
}

func TestUninstallFailureExitsFiveAndKeepsTheLockEntry(t *testing.T) {
	session := fixtureSession(t)
	session.Installer.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "my-agent", Name: "My Agent", SkillsDir: ".my-agent/skills"}}
	if out, err := runInstall(t, session, fixtureSkill, "--agent", "codex", "--yes", "--copy"); err != nil {
		t.Fatal(err, out)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(session.Store.Workspace, ".my-agent")); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, session, "uninstall", skillName(session), "--yes")
	if err == nil || exitCode(err) != 5 || !strings.Contains(out, "failed "+skillName(session)) {
		t.Fatal(err, out)
	}
	lock, lerr := skillkit.ReadLock(session.Store.Workspace)
	if lerr != nil || len(lock.Skills) != 1 {
		t.Fatal("the lock entry of a skill that was not fully removed must stay:", lerr, lock.Skills)
	}
}

func TestLifecycleCommandsDocumentTheirFlags(t *testing.T) {
	root := newRootCmd()
	for name, flags := range map[string][]string{"update": {"yes", "ignore-conflicts"}, "uninstall": {"yes", "ignore-requirements"}} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name || cmd.Short == "" || cmd.Long == "" || cmd.Example == "" {
			t.Fatal(name, err)
		}
		for _, flag := range flags {
			if cmd.Flags().Lookup(flag) == nil || cmd.Flags().Lookup(flag).Usage == "" {
				t.Fatal(name, flag)
			}
		}
		if cmd.Flags().Lookup("json") != nil {
			t.Fatal(name, "must not have --json")
		}
	}
}
