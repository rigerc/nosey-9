package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
)

var testSHA = strings.Repeat("a", 40)

type installRunner struct{ calls int }

func (r *installRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	r.calls++
	return process.Output{Stdout: []byte("v22.20.0")}, nil
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
	m.session.Store = settings.Store{Workspace: t.TempDir()}
	m.session.Installer = installation.Service{Store: m.session.Store, Runner: runner, Resolver: func(context.Context) (installation.CLI, error) {
		return installation.CLI{Name: "/bin/true", Backend: "installed"}, nil
	}}
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
	if !slices.Contains(m.install.commands[0].Args, "--copy") || !slices.Contains(m.install.commands[0].Args, "--yes") {
		t.Fatal(m.install.commands[0].Args)
	}
	if runner.calls != 0 {
		t.Fatal("opening the page ran the CLI")
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
	for _, command := range m.install.commands {
		if !slices.Contains(command.Args, "--yes") || !slices.Contains(command.Args, "codex") || !slices.Contains(command.Args, "claude-code") {
			t.Fatal(command.Args)
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

func TestInstallRunsEachSourceThenReloads(t *testing.T) {
	m, runner := installModel(t, nil)
	m = openedInstall(t, m)
	generation := m.generation
	m, cmd := m.updateInstallPage("enter")
	if cmd == nil || !m.loading || !strings.Contains(screenText(m), "Preparing installer") {
		t.Fatal("Enter did not start preparing")
	}
	ready, ok := cmd().(installReady)
	if !ok || ready.err != nil || ready.cli.Name != "/bin/true" || runner.calls == 0 {
		t.Fatalf("%+v", ready)
	}
	m, cmd = updateModel(t, m, ready)
	if cmd == nil || m.modal != "install" {
		t.Fatal("first source did not start")
	}
	m, cmd = updateModel(t, m, installStepDone{generation: m.generation, index: 0})
	if cmd == nil || m.modal != "install" || m.generation != generation {
		t.Fatal("second source did not start")
	}
	m, cmd = updateModel(t, m, installStepDone{generation: m.generation, index: 1})
	if cmd == nil || m.modal != "" || !m.loading || m.generation != generation+1 || !strings.Contains(m.message, "Installer finished for 2 sources") {
		t.Fatal("run did not finish and reload", m.modal, m.message)
	}
	if strings.Contains(m.message, "installed") {
		t.Fatal("message claims installation it cannot prove", m.message)
	}
}

func TestInstallFailureStopsAndSurvivesTheReload(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m, cmd := updateModel(t, m, installStepDone{generation: m.generation, index: 0, err: errors.New("exit status 1")})
	if cmd == nil || m.modal != "" || !strings.Contains(m.warning, "owner/one") || !strings.Contains(m.warning, "1 later source not started") {
		t.Fatal(m.modal, m.warning)
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

func TestInstallPreparationFailureKeepsThePageOpen(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m.loading = true
	m, cmd := updateModel(t, m, installReady{generation: m.generation, err: installation.PrerequisiteError{Reason: "skills CLI missing"}})
	if cmd != nil || m.loading || m.modal != "install" || !strings.Contains(screenText(m), "skills CLI missing") {
		t.Fatal(m.loading, m.modal, screenText(m))
	}
}

func TestPrintModeShowsCommandsAndNeverStarts(t *testing.T) {
	m, runner := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Mode = "print" })
	m = openedInstall(t, m)
	screen := screenText(m)
	if !strings.Contains(screen, "Print mode") || !strings.Contains(screen, "'npx' '--yes'") || strings.Contains(screen, "Enter Start") {
		t.Fatal(screen)
	}
	m, cmd := updateModel(t, m, keypress(tea.KeyEnter))
	if cmd != nil || m.loading || runner.calls != 0 {
		t.Fatal("print mode started the installer")
	}
}

func TestCopyKeepsThePageOpen(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	generation := m.generation
	m, cmd := updateModel(t, m, installCopied{})
	if cmd != nil || m.modal != "install" || m.loading || m.generation != generation || !strings.Contains(screenText(m), "Commands copied") {
		t.Fatal("copy changed the page", m.modal, m.loading)
	}
	m, _ = updateModel(t, m, installCopied{err: errors.New("no clipboard")})
	if m.modal != "install" || !strings.Contains(screenText(m), "no clipboard") {
		t.Fatal("copy failure was not shown")
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
	m.filters[2].Query = "gamma"
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
			m, cmd = m.applyInstallStep(installStepDone{generation: m.generation, index: 0})
			if cmd == nil || m.modal != "" || m.install.running || !strings.Contains(m.warning, "context canceled") || !strings.Contains(m.warning, "1 later source not started") {
				t.Fatal(m.warning)
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
}

func TestStaleBackgroundResultDoesNotStartAnotherSource(t *testing.T) {
	m, _ := installModel(t, nil)
	m = openedInstall(t, m)
	m.generation++
	next, cmd := m.applyInstallStep(installStepDone{generation: m.generation - 1, index: 0})
	if cmd != nil || next.modal != "install" {
		t.Fatal("stale result changed install")
	}
}
