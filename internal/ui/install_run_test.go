package ui

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

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

var testSHA = strings.Repeat("a", 40)

type installRunner struct{ calls int }

func (r *installRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	r.calls++
	return process.Output{Stdout: []byte("git version 2.50.0")}, nil
}

// dirFetcher serves prepared directories instead of cloning; the key is "uri#ref".
type dirFetcher map[string]string

func (f dirFetcher) Fetch(_ context.Context, src skillkit.Source) (*skillkit.Checkout, error) {
	parsed, err := skillkit.ParseSource(src)
	if err != nil {
		return nil, err
	}
	dir, ok := f[src.URI+"#"+src.Ref]
	if !ok {
		return nil, &skillkit.FetchError{Stage: "fetch", Ref: src.Ref, Err: errors.New("repository not found")}
	}
	return &skillkit.Checkout{Dir: dir, Ref: src.Ref, Parsed: parsed}, nil
}

func writeFixtureSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: Fixture.\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// lockFree reports whether the workspace operation lock can be taken.
func lockFree(t *testing.T, m noseyModel) bool {
	t.Helper()
	installer := m.installer()
	installer.LockTimeout = 50 * time.Millisecond
	op, err := installer.Begin(context.Background())
	if err != nil {
		return false
	}
	_ = op.Close()
	return true
}

func fixtureSkill(id, name, uri string) domain.Skill {
	return domain.Skill{ID: id, Name: name, Technologies: []string{"a"}, RepositoryID: "repo", Source: domain.Source{Kind: "git", URI: uri, Ref: testSHA, SkillName: name}}
}

// installModel returns a model with two selected skills from one source and one from another.
func installModel(t *testing.T, mutate func(*settings.Settings)) (noseyModel, *installRunner) {
	t.Helper()
	m := productModel()
	m.width, m.height = 100, 30
	m.viewport.SetWidth(98)
	m.viewport.SetHeight(20)
	cfg := settings.Defaults()
	cfg.Installation.Agents = []string{"codex", "claude-code"}
	if mutate != nil {
		mutate(&cfg)
	}
	runner := &installRunner{}
	m.session.Resolved.Settings = cfg
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	one, two := t.TempDir(), t.TempDir()
	writeFixtureSkill(t, one, "alpha")
	writeFixtureSkill(t, one, "beta")
	writeFixtureSkill(t, two, "gamma")
	m.session.Store = settings.Store{Workspace: workspace, Data: t.TempDir()}
	m.session.Installer = installation.Service{
		Store: m.session.Store, Runner: runner, Home: t.TempDir(),
		Fetcher: dirFetcher{"https://github.com/owner/one#" + testSHA: one, "owner/two#" + testSHA: two},
	}
	m.session.Catalog.Skills = map[string]domain.Skill{
		"a": fixtureSkill("a", "alpha", "https://github.com/owner/one"),
		"b": fixtureSkill("b", "beta", "https://github.com/owner/one"),
		"c": fixtureSkill("c", "gamma", "owner/two"),
	}
	m.selected = map[string]bool{"a": true, "b": true, "c": true}
	return m, runner
}

func openedInstall(t *testing.T, m noseyModel) noseyModel {
	t.Helper()
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "install" || m.err != nil {
		t.Fatal("install page did not open", m.modal, m.err)
	}
	return m
}

func screenText(m noseyModel) string { return ansi.Strip(m.View().Content) }

func updateModel(t *testing.T, m noseyModel, msg tea.Msg) (noseyModel, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	model, ok := updated.(noseyModel)
	if !ok {
		t.Fatal("unexpected model")
	}
	return model, cmd
}

func TestInstallPageGroupsSkillsAndRunsNothing(t *testing.T) {
	m, runner := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Method = "copy" })
	m = openedInstall(t, m)
	screen := screenText(m)
	for _, want := range []string{"Install 3 skills from 2 sources", "Confirm once", "owner/one @aaaaaaa", "alpha", "beta", "owner/two @aaaaaaa", "gamma", "Agents: codex, claude-code", "Enter Start"} {
		if !strings.Contains(screen, want) {
			t.Fatal(want, screen)
		}
	}
	if plan := m.install.plans[0]; plan.Method != "copy" || !slices.Equal(plan.Agents, []string{"claude-code", "codex"}) {
		t.Fatal(plan)
	}
	if runner.calls != 0 {
		t.Fatal("opening the page started an operation")
	}
}

func TestInstallChoosesAgentsOnceBeforeReview(t *testing.T) {
	m, runner := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Agents = nil })
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "form" || m.form.kind != "install-agents" || runner.calls != 0 {
		t.Fatal(m.modal, runner.calls)
	}
	m, cmd := m.submitForm()
	if cmd != nil || m.err == nil || m.modal != "form" {
		t.Fatal("empty agents accepted")
	}
	m.form.inputs[0].SetValue("codex,claude-code")
	m, cmd = m.submitForm()
	if cmd != nil || m.err != nil || m.modal != "install" {
		t.Fatal(m.modal, m.err)
	}
	for _, plan := range m.install.plans {
		if !slices.Contains(plan.Agents, "codex") || !slices.Contains(plan.Agents, "claude-code") {
			t.Fatal(plan.Agents)
		}
	}
	if len(m.session.Resolved.Settings.Installation.Agents) != 0 || runner.calls != 0 {
		t.Fatal("agent choice persisted or launched installer")
	}
}

func TestInstallAgentChoiceCanBeCancelled(t *testing.T) {
	m, runner := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Agents = nil })
	m = productUpdate(t, m, keypress('i'))
	m = productUpdate(t, m, keypress(tea.KeyEsc))
	if m.modal != "" || runner.calls != 0 || len(m.selected) != 3 {
		t.Fatal(m.modal, runner.calls, m.selected)
	}
}

func TestInstallErrorsNameTheCauseAndStayOnTheList(t *testing.T) {
	m, _ := installModel(t, nil)
	m.session.Catalog.Skills["c"] = fixtureSkill("c", "alpha", "owner/two")
	m = productUpdate(t, m, keypress('i'))
	if m.modal != "" || m.err == nil || !strings.Contains(screenText(m), "both install as") {
		t.Fatal(m.modal, m.err, screenText(m))
	}
}

func TestInstallPageOwnsTabAndEscapeCancels(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m = productUpdate(t, m, keypress(tea.KeyTab))
	if m.tab != 0 || m.modal != "install" || !strings.Contains(screenText(m), "Install 3 skills") {
		t.Fatal("Tab left the install page", m.tab, m.modal)
	}
	m = productUpdate(t, m, keypress(tea.KeyEsc))
	if m.modal != "" {
		t.Fatal("Escape did not close the page")
	}
}

func TestLongInstallPageWrapsAndScrolls(t *testing.T) {
	m, _ := installModel(t, nil)
	m.width = 40
	m.session.Catalog.Skills = map[string]domain.Skill{}
	m.selected = map[string]bool{}
	for i := range 40 {
		id := fmt.Sprintf("s%02d", i)
		m.session.Catalog.Skills[id] = fixtureSkill(id, "skill-"+id, "https://github.com/some-long-owner-name/a-very-long-repository-name-that-wraps")
		m.selected[id] = true
	}
	m = openedInstall(t, m)
	if !strings.Contains(strings.Join(m.installPageLines(), ""), "a-very-long-repository-name-that-wraps @aaaaaaa") {
		t.Fatal("long source was cut", m.installPageLines())
	}
	if strings.Contains(screenText(m), "skill-s39") {
		t.Fatal("fixture no longer overflows the page")
	}
	before := screenText(m)
	m = productUpdate(t, m, keypress(tea.KeyDown))
	if screenText(m) == before {
		t.Fatal("page did not scroll")
	}
	for range 5 {
		m = productUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !strings.Contains(screenText(m), "skill-s39") {
		t.Fatal("last skill unreachable", screenText(m))
	}
}

func TestInstallRunsEachSourceUnderOneLockThenReloads(t *testing.T) {
	m, runner := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Method = "copy" })
	m = openedInstall(t, m)
	generation := m.generation
	m, cmd := m.updateInstallPage("enter")
	if cmd == nil || !m.loading || !strings.Contains(screenText(m), "Preparing installer") {
		t.Fatal("Enter did not start preparing")
	}
	ready, ok := cmd().(installReady)
	if !ok || ready.err != nil || ready.op == nil || runner.calls == 0 {
		t.Fatalf("%+v", ready)
	}
	if lockFree(t, m) {
		t.Fatal("the operation lock must be held once preparation finished")
	}
	m, cmd = updateModel(t, m, ready)
	if cmd == nil || m.modal != "install" || m.install.op == nil {
		t.Fatal("first source did not start")
	}
	done, ok := cmd().(installStepDone)
	if !ok || done.err != nil || len(done.result.Installed()) != 2 {
		t.Fatalf("%+v", done)
	}
	if lockFree(t, m) {
		t.Fatal("the lock must stay held between sources")
	}
	m, cmd = updateModel(t, m, done)
	if cmd == nil || m.modal != "install" || m.generation != generation {
		t.Fatal("second source did not start")
	}
	done, ok = cmd().(installStepDone)
	if !ok || done.err != nil {
		t.Fatalf("%+v", done)
	}
	m, cmd = updateModel(t, m, done)
	if cmd == nil || m.modal != "" || !m.loading || m.generation != generation+1 || !strings.Contains(m.message, "Installed 3 skills from 2 sources") {
		t.Fatal("run did not finish and reload", m.modal, m.message)
	}
	if m.install.op != nil || !lockFree(t, m) {
		t.Fatal("the lock was not released after the last source")
	}
	if _, err := os.Stat(filepath.Join(m.session.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallFailureStopsReleasesTheLockAndSurvivesTheReload(t *testing.T) {
	m, _ := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Method = "copy" })
	m.session.Installer.Fetcher = dirFetcher{} // every fetch fails
	m = openedInstall(t, m)
	m, cmd := m.updateInstallPage("enter")
	m, cmd = updateModel(t, m, cmd().(installReady))
	done := cmd().(installStepDone)
	if done.err == nil || done.result.Skills[0].Status != skillkit.StatusNotStarted {
		t.Fatalf("%+v", done)
	}
	m, cmd = updateModel(t, m, done)
	if cmd == nil || m.modal != "" || !strings.Contains(m.warning, "owner/one") || !strings.Contains(m.warning, "1 later source not started") {
		t.Fatal(m.modal, m.warning)
	}
	if m.install.op != nil || !lockFree(t, m) {
		t.Fatal("a failed install kept the lock")
	}
	m, _ = updateModel(t, m, noseyLoaded{generation: m.generation, session: m.session, scan: m.scan, recs: m.recs})
	if !strings.Contains(screenText(m), "1 later source not started") {
		t.Fatal("failure disappeared after reload", screenText(m))
	}
	m = openedInstall(t, m)
	if m.warning != "" {
		t.Fatal("old failure stayed after starting again")
	}
}

func TestInstallSurfacesSkippedAgentsAndKeepsPartialResults(t *testing.T) {
	m, _ := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Agents = []string{"codex", "windsurf"} })
	m = openedInstall(t, m)
	m, cmd := m.updateInstallPage("enter")
	m, cmd = updateModel(t, m, cmd().(installReady))
	for cmd != nil && m.install.running {
		m, cmd = updateModel(t, m, cmd().(installStepDone))
	}
	if m.err != nil || !strings.Contains(m.warning, "skipped windsurf for alpha") || !strings.Contains(m.message, "Installed 3 skills") {
		t.Fatal(m.err, m.warning, m.message)
	}
}

func TestInstallPreparationFailureKeepsThePageOpen(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m.loading = true
	m, cmd := updateModel(t, m, installReady{generation: m.generation, err: installation.PrerequisiteError{Reason: "git missing"}})
	if cmd != nil || m.loading || m.modal != "install" || !strings.Contains(screenText(m), "git missing") {
		t.Fatal(m.loading, m.modal, screenText(m))
	}
}

func TestInstallPageHasNoPrintOrCopyAndStartsOnEnter(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	screen := screenText(m)
	for _, want := range []string{"Enter Start", "owner/one", "alpha", "Agents: codex, claude-code", "Method: symlink"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q in\n%s", want, screen)
		}
	}
	for _, banned := range []string{"Print mode", "Copy", "npx"} {
		if strings.Contains(screen, banned) {
			t.Fatalf("install page still shows %q", banned)
		}
	}
	m, cmd := updateModel(t, m, keypress('c'))
	if cmd != nil || m.modal != "install" || m.loading {
		t.Fatal("c key still acts on the install page")
	}
	m, cmd = updateModel(t, m, keypress(tea.KeyEnter))
	if cmd == nil || !m.loading || !m.install.running {
		t.Fatal("enter did not start the installer")
	}
}

func TestInstallWithoutAgentsAsksForThem(t *testing.T) {
	m, _ := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Agents = nil })
	m = productUpdate(t, m, keypress('i'))
	if m.modal == "install" || m.form.kind != "install-agents" {
		t.Fatal("install opened without agents", m.modal, m.form.kind)
	}
}

func TestInventoryClearsInstalledSelectionsFromAnyTab(t *testing.T) {
	m, _ := installModel(t, nil)
	id := "c"
	m.session.Store = settings.Store{}
	m, _ = updateModel(t, m, noseyInventory{generation: m.generation, inventory: installation.Inventory{Complete: true, Skills: []installation.InventoryItem{{SkillID: &id, Name: "gamma", Status: "installed"}}}})
	if m.selected["c"] || !m.selected["a"] || !m.selected["b"] {
		t.Fatal(m.selected)
	}
}

func TestOutdatedSkillsShowUpdateAvailableAndStaySelectable(t *testing.T) {
	m, _ := installModel(t, nil)
	m.selected = map[string]bool{}
	m.tab = 1
	m.filters[1].Query = "gamma"
	m.session.Catalog.InstallStatus = map[string]string{"c": "outdated"}
	rows := m.rows()
	found := false
	for i, row := range rows {
		if row.kind == "skill" {
			found = strings.Contains(row.label, "update available")
			m.cursor[1] = i
		}
	}
	if !found {
		t.Fatal(rows)
	}
	m = productUpdate(t, m, keypress(tea.KeySpace))
	if !m.selected["c"] {
		t.Fatal("outdated skill could not be selected")
	}
}

func TestBackgroundInstallKeepsUIResponsiveAndCancels(t *testing.T) {
	for _, key := range []string{"esc", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m, _ := installModel(t, nil)
			m = openedInstall(t, m)
			m, cmd := m.updateInstallPage("enter")
			ready, ok := cmd().(installReady)
			if !ok {
				t.Fatal("unexpected preparation message")
			}
			m, cmd = m.applyInstallReady(ready)
			if cmd == nil || !m.install.running || !strings.Contains(screenText(m), "Installing source 1 of 2") {
				t.Fatal(screenText(m))
			}
			m, resize := updateModel(t, m, tea.WindowSizeMsg{Width: 70, Height: 24})
			if resize != nil || m.width != 70 {
				t.Fatal("resize blocked")
			}
			m, cmd = m.update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd != nil {
				t.Fatal("duplicate install started")
			}
			if key == "esc" {
				m, cmd = m.update(tea.KeyPressMsg{Code: tea.KeyEsc})
			} else {
				m, cmd = m.update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			}
			if cmd != nil || !m.install.cancelled || m.install.ctx.Err() == nil || !strings.Contains(screenText(m), "Cancelling installation") {
				t.Fatal(m.install.cancelled, screenText(m))
			}
			if lockFree(t, m) {
				t.Fatal("the lock must be held until the running source stops")
			}
			m, cmd = m.applyInstallStep(installStepDone{generation: m.generation, index: 0})
			if cmd == nil || m.modal != "" || m.install.running || !strings.Contains(m.warning, "context canceled") || !strings.Contains(m.warning, "1 later source not started") {
				t.Fatal(m.warning)
			}
			if m.install.op != nil || !lockFree(t, m) {
				t.Fatal("cancellation kept the lock")
			}
		})
	}
}

func TestCancelDuringInstallerPreparationStartsNoSource(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m, cmd := m.updateInstallPage("enter")
	m, _ = m.cancelInstall()
	ready, ok := cmd().(installReady)
	if !ok {
		t.Fatal("unexpected preparation message")
	}
	m, cmd = m.applyInstallReady(ready)
	if cmd != nil || m.install.running || m.loading || m.modal != "" || !strings.Contains(m.message, "No sources started") {
		t.Fatal(m.message)
	}
	if !lockFree(t, m) {
		t.Fatal("a cancelled preparation kept the lock")
	}
}

func TestStaleBackgroundResultDoesNotStartAnotherSource(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m.generation++
	op, err := m.installer().Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.install.op = op
	next, cmd := m.applyInstallStep(installStepDone{generation: m.generation - 1, index: 0})
	if cmd != nil || next.modal != "install" {
		t.Fatal("stale result changed install")
	}
	if !lockFree(t, m) {
		t.Fatal("a stale result leaked the lock")
	}
}

func TestInstallProgressIsPartOfThePageNotAnOverlay(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m, cmd := m.updateInstallPage("enter")
	if !strings.Contains(screenText(m), "Preparing installer") || strings.Contains(screenText(m), "Loading workspace") {
		t.Fatal(screenText(m))
	}
	ready, ok := cmd().(installReady)
	if !ok {
		t.Fatal("unexpected preparation message")
	}
	m, _ = m.applyInstallReady(ready)
	first := strings.Split(screenText(m), "\n")
	screen := screenText(m)
	for _, want := range []string{"Install 3 skills from 2 sources", "Installing source 1 of 2", "owner/one @aaaaaaa", "alpha", "owner/two @aaaaaaa", "gamma", "Agents: codex, claude-code"} {
		if !strings.Contains(screen, want) {
			t.Fatal(want, screen)
		}
	}
	titleAt, statusAt := -1, -1
	for i, line := range first {
		if strings.Contains(line, "Install 3 skills") {
			titleAt = i
		}
		if strings.Contains(line, "Installing source 1 of 2") {
			statusAt = i
		}
	}
	if titleAt < 0 || statusAt != titleAt+1 {
		t.Fatal("progress is not directly under the page title", titleAt, statusAt)
	}
	m, _ = m.applyInstallStep(installStepDone{generation: m.generation, index: 0})
	if !strings.Contains(screenText(m), "✓ owner/one") || !strings.Contains(screenText(m), "Installing source 2 of 2") {
		t.Fatal("finished source not marked", screenText(m))
	}
}
