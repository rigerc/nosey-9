package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

var newSHA = strings.Repeat("b", 40)

// lifecycleModel has alpha, beta and gamma installed by copy on the Manage tab with the cursor
// on alpha, whose catalog pin moved to newSHA. beta is tracked and current; "hand" is on disk
// but untracked.
func lifecycleModel(t *testing.T) noseyModel {
	t.Helper()
	m, _ := installModel(t, func(cfg *settings.Settings) { cfg.Installation.Method = "copy" })
	// installModel always configures a dirFetcher.
	fetcher := m.session.Installer.Fetcher.(dirFetcher)
	newOne := t.TempDir()
	writeFixtureSkill(t, newOne, "alpha")
	if err := os.WriteFile(filepath.Join(newOne, "skills", "alpha", "new.txt"), []byte("v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher["https://github.com/owner/one#"+newSHA] = newOne
	plans, err := m.installer().Build(m.session.Catalog, catalog.Recommendations{}, installation.Options{IDs: []string{"a", "b", "c"}, Agents: []string{"claude-code"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.installer().Install(context.Background(), plans, nil); err != nil {
		t.Fatal(err)
	}
	hand := filepath.Join(m.session.Store.Workspace, ".claude", "skills", "hand")
	if err = os.MkdirAll(hand, 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(hand, "SKILL.md"), []byte("---\nname: hand\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alpha := m.session.Catalog.Skills["a"]
	alpha.Source.Ref = newSHA
	m.session.Catalog.Skills["a"] = alpha
	m.selected = map[string]bool{}
	m.tab = 2
	m.inventory = m.installer().Inventory(context.Background(), m.session.Catalog)
	m.session.ApplyInventory(m.inventory)
	return m
}

func TestLifecycleKeysApplyOnlyToApplicableInventoryRows(t *testing.T) {
	m := lifecycleModel(t)
	if m.inventory.Skills[0].Name != "alpha" || m.inventory.Skills[0].Status != "outdated" {
		t.Fatalf("%+v", m.inventory.Skills)
	}
	screen := screenText(m)
	if !strings.Contains(screen, "U Update") || !strings.Contains(screen, "d Remove") || !strings.Contains(screen, "u Check updates") {
		t.Fatal("applicable actions must be in the footer:", screen)
	}
	opened := productUpdate(t, m, tea.KeyPressMsg{Code: 'U', Text: "U"})
	if opened.modal != "install" || opened.install.kind != "update" || opened.err != nil {
		t.Fatal(opened.modal, opened.install.kind, opened.err)
	}
	for _, want := range []string{"Update 1 skill from 1 source", "alpha", "catalog pin changed (aaaaaaa → bbbbbbb)", "at .claude/skills/alpha (copy)", "Enter Start"} {
		if !strings.Contains(screenText(opened), want) {
			t.Fatal(want, screenText(opened))
		}
	}
	removing := productUpdate(t, m, keypress('d'))
	if removing.modal != "install" || removing.install.kind != "uninstall" || len(removing.install.remove.Skills) != 1 {
		t.Fatal(removing.modal, removing.err)
	}
	if !strings.Contains(screenText(removing), "Remove 1 skill") {
		t.Fatal(screenText(removing))
	}

	// beta is current: no update action. "hand" is untracked: no remove action.
	m.cursor[2] = 1
	if beta := screenText(m); strings.Contains(beta, "U Update") || !strings.Contains(beta, "d Remove") {
		t.Fatal("U only applies to outdated skills:", beta)
	}
	if got := productUpdate(t, m, tea.KeyPressMsg{Code: 'U', Text: "U"}); got.modal != "" {
		t.Fatal("U opened a page for a current skill")
	}
	for i, item := range m.inventory.Skills {
		if item.Name == "hand" {
			m.cursor[2] = i
		}
	}
	if hand := screenText(m); strings.Contains(hand, "d Remove") || strings.Contains(hand, "U Update") {
		t.Fatal("untracked skills have no actions:", hand)
	}
	if got := productUpdate(t, m, keypress('d')); got.modal != "" || got.install.kind != "" {
		t.Fatal("d opened a page for an untracked skill")
	}
}

func TestLifecycleKeysDoNotFireOutsideTheManageTabOrWhileTyping(t *testing.T) {
	m := lifecycleModel(t)
	other := m
	other.tab = 0
	if got := productUpdate(t, other, keypress('d')); got.modal != "" {
		t.Fatal("d fired outside the Manage tab")
	}
	typing := m
	typing.search = true
	typing.query.Focus()
	if got := productUpdate(t, typing, tea.KeyPressMsg{Code: 'd', Text: "d"}); got.modal != "" || got.query.Value() != "d" {
		t.Fatal("d must be typed into a focused search box:", got.modal, got.query.Value())
	}
	form := m
	form.openForm("setting", []string{"x"}, []string{""})
	form = productUpdate(t, form, tea.KeyPressMsg{Code: 'd', Text: "d"})
	if form.modal != "form" || form.form.inputs[0].Value() != "d" {
		t.Fatal("d must be typed into a form field")
	}
	loading := m
	loading.loading = true
	if got := productUpdate(t, loading, keypress('d')); got.modal != "" {
		t.Fatal("d fired while loading")
	}
}

func runPage(t *testing.T, m noseyModel) (noseyModel, tea.Cmd) {
	t.Helper()
	m, cmd := m.updateInstallPage("enter")
	ready, ok := cmd().(installReady)
	if !ok || ready.err != nil {
		t.Fatalf("%+v", ready)
	}
	m, cmd = updateModel(t, m, ready)
	for cmd != nil {
		done, ok := cmd().(installStepDone)
		if !ok {
			t.Fatal("unexpected message")
		}
		var next noseyModel
		next, cmd = updateModel(t, m, done)
		m = next
		if m.modal == "" {
			break
		}
	}
	return m, cmd
}

func TestApplyUpdateRunsUnderTheLockAndRefreshesInventory(t *testing.T) {
	m := lifecycleModel(t)
	m = productUpdate(t, m, tea.KeyPressMsg{Code: 'U', Text: "U"})
	generation := m.generation
	m.updates = []string{"alpha"}
	m, reload := runPage(t, m)
	if reload == nil || m.modal != "" || !m.loading || m.generation != generation+1 || m.updates != nil || !strings.Contains(m.message, "Updated 1 skill from 1 source") {
		t.Fatal("run did not finish and reload", m.modal, m.message, m.warning)
	}
	if m.install.op != nil || !lockFree(t, m) {
		t.Fatal("the lock was not released")
	}
	root := m.session.Store.Workspace
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "new.txt")); err != nil {
		t.Fatal("alpha was not refreshed:", err)
	}
	lock, err := skillkit.ReadLock(root)
	if err != nil || lock.Skills["alpha"].Ref != newSHA || lock.Skills["beta"].Ref != testSHA {
		t.Fatal(err, lock.Skills)
	}
	// The reload re-reads the inventory: alpha is current now.
	after := m.installer().Inventory(context.Background(), m.session.Catalog)
	for _, item := range after.Skills {
		if item.Name == "alpha" && item.Status != "installed" {
			t.Fatalf("%+v", item)
		}
	}
}

func TestApplyRemoveRunsUnderTheLockAndLeavesOtherSkills(t *testing.T) {
	m := lifecycleModel(t)
	m = productUpdate(t, m, keypress('d'))
	generation := m.generation
	m, reload := runPage(t, m)
	if reload == nil || m.modal != "" || !m.loading || m.generation != generation+1 || !strings.Contains(m.message, "Removed 1 skill") {
		t.Fatal("run did not finish and reload", m.modal, m.message, m.warning)
	}
	if m.install.op != nil || !lockFree(t, m) {
		t.Fatal("the lock was not released")
	}
	root := m.session.Store.Workspace
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha")); !os.IsNotExist(err) {
		t.Fatal("alpha still installed")
	}
	for _, kept := range []string{"beta", "gamma", "hand"} {
		if _, err := os.Stat(filepath.Join(root, ".claude", "skills", kept)); err != nil {
			t.Fatal(kept, err)
		}
	}
	lock, err := skillkit.ReadLock(root)
	if err != nil || len(lock.Skills) != 2 {
		t.Fatal(err, lock.Skills)
	}
}

func TestLifecycleRemoveBlockedByDependentShowsTheErrorWithoutAPage(t *testing.T) {
	m := lifecycleModel(t)
	beta := m.session.Catalog.Skills["b"]
	beta.Requires = []string{"a"}
	m.session.Catalog.Skills["b"] = beta
	m = productUpdate(t, m, keypress('d'))
	if m.modal != "" || m.err == nil || !strings.Contains(m.err.Error(), "beta requires alpha") {
		t.Fatal(m.modal, m.err)
	}
	if !strings.Contains(screenText(m), "beta requires alpha") {
		t.Fatal(screenText(m))
	}
}

func TestLifecycleCancellationAndStaleResultsReleaseTheLock(t *testing.T) {
	for _, key := range []string{"U", "d"} {
		m := lifecycleModel(t)
		text := key
		code := rune(key[0])
		m = productUpdate(t, m, tea.KeyPressMsg{Code: code, Text: text})
		if m.modal != "install" {
			t.Fatal(key, "page did not open")
		}
		// Escape before starting closes the page and changes nothing.
		closed := productUpdate(t, m, keypress(tea.KeyEsc))
		if closed.modal != "" || !lockFree(t, closed) {
			t.Fatal(key, "Esc did not cancel")
		}
		// Cancelling during preparation starts nothing and frees the lock.
		started, cmd := m.updateInstallPage("enter")
		started, _ = started.cancelInstall()
		ready, ok := cmd().(installReady)
		if !ok {
			t.Fatal("unexpected preparation message")
		}
		started, next := started.applyInstallReady(ready)
		if next != nil || started.install.running || started.modal != "" || !strings.Contains(started.message, "cancelled") || !lockFree(t, started) {
			t.Fatal(key, started.message)
		}
		// A cancelled run reports what completed and not-started work.
		running, cmd := m.updateInstallPage("enter")
		// Pressing enter first returns the command that prepares the operation.
		ready = cmd().(installReady)
		running, cmd = running.applyInstallReady(ready)
		if cmd == nil || !running.install.running {
			t.Fatal(key, "run did not start")
		}
		running, _ = running.cancelInstall()
		if lockFree(t, running) {
			t.Fatal(key, "the lock must be held until the running step stops")
		}
		// The running step's command reports that step's result.
		msg := cmd().(installStepDone)
		finished, _ := running.applyInstallStep(msg)
		if finished.modal != "" || finished.install.running || finished.install.op != nil || !lockFree(t, finished) {
			t.Fatal(key, "cancellation kept the lock")
		}
		// A result from an older generation releases the lock and changes nothing.
		stale := lifecycleModel(t)
		stale = productUpdate(t, stale, tea.KeyPressMsg{Code: code, Text: text})
		op, err := stale.installer().Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		stale.install.op = op
		stale.generation++
		got, cmd := stale.applyInstallStep(installStepDone{generation: stale.generation - 1})
		if cmd != nil || got.modal != "install" || !lockFree(t, got) {
			t.Fatal(key, "stale result leaked the lock")
		}
	}
}

func TestLifecycleHelpListsTheNewKeys(t *testing.T) {
	m := lifecycleModel(t)
	m = productUpdate(t, m, keypress('?'))
	if help := screenText(m); !strings.Contains(help, "U: update the outdated skill") || !strings.Contains(help, "d: remove the tracked skill") {
		t.Fatal(help)
	}
}
