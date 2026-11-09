package installation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

func catalogOf(skills ...domain.Skill) domain.Catalog {
	c := domain.Catalog{Skills: map[string]domain.Skill{}}
	for _, sk := range skills {
		c.Skills[sk.ID] = sk
	}
	return c
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func isLink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func (s Service) at(rel string) string {
	return filepath.Join(s.Store.Workspace, filepath.FromSlash(rel))
}

func mustInstall(t *testing.T, s Service, p SourcePlan) {
	t.Helper()
	if _, err := s.Install(context.Background(), []SourcePlan{p}, nil); err != nil {
		t.Fatal(err)
	}
}

func mustPlanUpdate(t *testing.T, s Service, c domain.Catalog, opts UpdateOptions) UpdatePlan {
	t.Helper()
	p, err := s.PlanUpdate(context.Background(), c, catalog.Recommendations{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func skillAt(t *testing.T, root, name, body string) {
	t.Helper()
	writeSkill(t, root, name, name, "data.txt", body)
}

// updateLab has alpha, beta and gamma installed by copy for codex from o/one@shaA.
func updateLab(t *testing.T) (Service, *fakeFetcher, string, string) {
	t.Helper()
	srcA, srcB := t.TempDir(), t.TempDir()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		skillAt(t, srcA, n, "v1 "+n)
		skillAt(t, srcB, n, "v2 "+n)
	}
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: srcA, "o/one#" + shaB: srcB}}
	s := newService(t, f)
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"codex"}, "alpha", "beta", "gamma"))
	return s, f, srcA, srcB
}

func TestPlanUpdateKeepsPinAndContentReasonsAndSkipsNonCatalogSkills(t *testing.T) {
	s, f, srcA, _ := updateLab(t)
	skillAt(t, srcA, "beta", "v1.1 beta") // upstream content drift at the locked ref
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("b", "beta", "o/one", shaA))
	before := len(f.fetches())
	got := mustPlanUpdate(t, s, c, UpdateOptions{})
	if len(got.Plans) != 2 || got.Count() != 2 {
		t.Fatalf("%+v", got.Plans)
	}
	alpha, beta := got.Plans[0].Skills[0], got.Plans[1].Skills[0]
	if got.Plans[0].Ref != shaB || alpha.ID != "a" || len(alpha.Reasons) != 1 || !strings.HasPrefix(alpha.Reasons[0], ReasonPinChanged) || !strings.Contains(alpha.Reasons[0], "aaaaaaa → bbbbbbb") {
		t.Fatalf("%+v", got.Plans[0])
	}
	if got.Plans[1].Ref != shaA || beta.ID != "b" || !slices.Equal(beta.Reasons, []string{ReasonContentChanged}) {
		t.Fatalf("%+v", got.Plans[1])
	}
	if !slices.Equal(alpha.Targets, []string{".agents/skills/alpha (copy)"}) {
		t.Fatalf("%v", alpha.Targets)
	}
	if !slices.Equal(got.Skipped, []SkippedSkill{{"gamma", "not in the current catalog"}}) {
		t.Fatalf("%+v", got.Skipped)
	}
	if lines := strings.Join(got.Lines(), "\n"); !strings.Contains(lines, "update alpha: catalog pin changed") || !strings.Contains(lines, "update beta: upstream content changed") || !strings.Contains(lines, "skipped gamma") {
		t.Fatal(lines)
	}
	if fetched := f.fetches()[before:]; len(fetched) != 1 {
		t.Fatalf("planning fetches each locked source once: %v", fetched)
	}
}

func TestUpdateAppliesPlanAndNeverTouchesNonCatalogSkills(t *testing.T) {
	s, _, srcA, _ := updateLab(t)
	skillAt(t, srcA, "beta", "v1.1 beta")
	gammaBefore := read(t, s.at(".agents/skills/gamma/data.txt"))
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("b", "beta", "o/one", shaA))
	var seen []int
	results, err := s.Update(context.Background(), mustPlanUpdate(t, s, c, UpdateOptions{}), func(p Progress) { seen = append(seen, p.Index) })
	if err != nil {
		t.Fatal(err, results)
	}
	if len(results) != 2 || !results[0].LockWritten || !results[1].LockWritten || len(seen) != 4 {
		t.Fatalf("%+v %v", results, seen)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v2 alpha" || read(t, s.at(".agents/skills/beta/data.txt")) != "v1.1 beta" {
		t.Fatal("skills were not refreshed")
	}
	if read(t, s.at(".agents/skills/gamma/data.txt")) != gammaBefore {
		t.Fatal("a non-catalog skill was changed")
	}
	lock := readLock(t, s)
	if lock.Skills["alpha"].Ref != shaB || lock.Skills["gamma"].Ref != shaA || lock.Skills["alpha"].Extra["installMode"] == nil {
		t.Fatalf("%+v", lock.Skills)
	}
	hash, _ := skillkit.FolderHash(s.at(".agents/skills/alpha"))
	if lock.Skills["alpha"].ComputedHash != hash {
		t.Fatal("lock hash is stale")
	}
	lines := strings.Join(results[0].Lines(), "\n")
	if !strings.Contains(lines, "updated alpha (catalog pin changed (aaaaaaa → bbbbbbb)) at .agents/skills/alpha (copy)") {
		t.Fatal(lines)
	}
	// Everything is current now.
	if again := mustPlanUpdate(t, s, c, UpdateOptions{}); !again.Empty() {
		t.Fatalf("%+v", again)
	}
}

func TestPlanUpdateStopsOnAFailedCheck(t *testing.T) {
	s, f, _, _ := updateLab(t)
	f.errs = map[string]error{"o/one#" + shaA: errors.New("network down")}
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaA))
	_, err := s.PlanUpdate(context.Background(), c, catalog.Recommendations{}, UpdateOptions{})
	var failed UpdateCheckError
	if !errors.As(err, &failed) || !strings.Contains(err.Error(), "o/one@"+shaA) || !strings.Contains(err.Error(), "network down") {
		t.Fatal("a failed check must stop the update and name the source:", err)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v1 alpha" {
		t.Fatal("nothing may change")
	}
}

func TestPlanUpdateIDsAndTargetErrors(t *testing.T) {
	s, _, _, _ := updateLab(t)
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("b", "beta", "o/one", shaA), gitSkill("n", "never", "o/one", shaA))
	got := mustPlanUpdate(t, s, c, UpdateOptions{IDs: []string{"a", "b", "gamma", "a"}})
	if got.Count() != 1 || got.Plans[0].Skills[0].ID != "a" {
		t.Fatalf("%+v", got)
	}
	want := []SkippedSkill{{"beta", "already up to date"}, {"gamma", "not in the current catalog"}}
	if !slices.Equal(got.Skipped, want) {
		t.Fatalf("%+v", got.Skipped)
	}
	for id, text := range map[string]string{"n": "is not installed", "nope": "unknown skill"} {
		_, err := s.PlanUpdate(context.Background(), c, catalog.Recommendations{}, UpdateOptions{IDs: []string{id}})
		var policy PolicyError
		if !errors.As(err, &policy) || !strings.Contains(err.Error(), text) {
			t.Fatal(id, err)
		}
	}
}

func TestPlanUpdateTreatsCatalogUnknownTrackedSkillsAsSkippedNotErrors(t *testing.T) {
	s, _, _, _ := updateLab(t)
	got := mustPlanUpdate(t, s, catalogOf(), UpdateOptions{})
	if !got.Empty() || len(got.Skipped) != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestUpdatePreservesMixedModesAndUndetectedUniversalLocation(t *testing.T) {
	srcA, srcB := t.TempDir(), t.TempDir()
	skillAt(t, srcA, "alpha", "v1")
	skillAt(t, srcB, "alpha", "v2")
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: srcA, "o/one#" + shaB: srcB}}
	s := newService(t, f)
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "mine", Name: "Mine", SkillsDir: ".mine/skills"}}
	if err := os.MkdirAll(s.at(".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// symlink install for claude-code, then a separate copy install for a custom agent.
	mustInstall(t, s, plan("o/one", shaA, "symlink", []string{"claude-code"}, "alpha"))
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"mine"}, "alpha"))
	if !isLink(s.at(".claude/skills/alpha")) || isLink(s.at(".mine/skills/alpha")) {
		t.Fatal("fixture is not mixed")
	}
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB))
	got := mustPlanUpdate(t, s, c, UpdateOptions{})
	wantTargets := []string{".agents/skills/alpha (copy)", ".claude/skills/alpha (symlink)", ".mine/skills/alpha (copy)"}
	if got.Empty() || !slices.Equal(got.Plans[0].Skills[0].Targets, wantTargets) {
		t.Fatalf("%+v", got.Plans)
	}
	results, err := s.Update(context.Background(), got, nil)
	if err != nil {
		t.Fatal(err, results[0].Lines())
	}
	if !isLink(s.at(".claude/skills/alpha")) || isLink(s.at(".mine/skills/alpha")) || isLink(s.at(".agents/skills/alpha")) {
		t.Fatal("install modes were not preserved")
	}
	for _, rel := range []string{".agents/skills/alpha", ".claude/skills/alpha", ".mine/skills/alpha"} {
		if read(t, s.at(rel+"/data.txt")) != "v2" {
			t.Fatal("not refreshed:", rel)
		}
	}
	if lock := readLock(t, s); lock.Skills["alpha"].Ref != shaB {
		t.Fatalf("%+v", lock.Skills["alpha"])
	}
}

func TestUpdateRefreshesAnUndetectedUniversalOnlyInstall(t *testing.T) {
	s, _, _, _ := updateLab(t)
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB))
	inv := s.Inventory(context.Background(), c)
	if len(inv.Skills[0].Agents) != 0 || inv.Skills[0].Status != "unknown" {
		t.Fatalf("the install must look undetected: %+v", inv.Skills[0])
	}
	got := mustPlanUpdate(t, s, c, UpdateOptions{})
	if got.Count() != 1 {
		t.Fatalf("pin drift must not depend on detected agents: %+v", got)
	}
	if _, err := s.Update(context.Background(), got, nil); err != nil {
		t.Fatal(err)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v2 alpha" {
		t.Fatal("location lost")
	}
}

func TestUpdateInstallsNewRequirementAtTheUnionOfRequirerLocations(t *testing.T) {
	srcOne, srcOneNew, srcTwo := t.TempDir(), t.TempDir(), t.TempDir()
	skillAt(t, srcOne, "top", "v1")
	skillAt(t, srcOneNew, "top", "v2")
	skillAt(t, srcTwo, "dep", "dep")
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: srcOne, "o/one#" + shaB: srcOneNew, "o/two#" + shaA: srcTwo}}
	s := newService(t, f)
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "mine", Name: "Mine", SkillsDir: ".mine/skills"}}
	if err := os.MkdirAll(s.at(".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, s, plan("o/one", shaA, "symlink", []string{"claude-code"}, "top"))
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"mine"}, "top"))
	top := gitSkill("top", "top", "o/one", shaB)
	top.Requires = []string{"dep"}
	c := catalogOf(top, gitSkill("dep", "dep", "o/two", shaA))
	got := mustPlanUpdate(t, s, c, UpdateOptions{})
	if len(got.Plans) != 2 || got.Plans[0].Skills[0].ID != "dep" || !got.Plans[0].Skills[0].Required || !slices.Equal(got.Plans[0].Skills[0].RequiredBy, []string{"top"}) {
		t.Fatalf("%+v", got.Plans)
	}
	if _, err := s.Update(context.Background(), got, nil); err != nil {
		t.Fatal(err)
	}
	if !isLink(s.at(".claude/skills/dep")) || isLink(s.at(".mine/skills/dep")) || read(t, s.at(".agents/skills/dep/data.txt")) != "dep" || read(t, s.at(".mine/skills/dep/data.txt")) != "dep" {
		t.Fatal("the requirement was not installed where top lives")
	}
	lock := readLock(t, s)
	if lock.Skills["dep"].Ref != shaA || lock.Skills["dep"].Extra["installMode"] != nil {
		t.Fatalf("a symlinked install records no copy mode: %+v", lock.Skills["dep"])
	}
	if read(t, s.at(".agents/skills/top/data.txt")) != "v2" {
		t.Fatal("top not updated")
	}
}

func TestUpdateRequirementsAndConflicts(t *testing.T) {
	s, _, _, _ := updateLab(t)
	// alpha gains a requirement that conflicts with the installed beta.
	alpha := gitSkill("a", "alpha", "o/one", shaB)
	alpha.Requires = []string{"dep"}
	dep := gitSkill("dep", "dep", "o/one", shaA)
	dep.ConflictsWith = []string{"b"}
	c := catalogOf(alpha, gitSkill("b", "beta", "o/one", shaA), dep)
	_, err := s.PlanUpdate(context.Background(), c, catalog.Recommendations{}, UpdateOptions{})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "dep conflicts with b") {
		t.Fatal(err)
	}
	got, err := s.PlanUpdate(context.Background(), c, catalog.Recommendations{}, UpdateOptions{IgnoreConflicts: true})
	if err != nil || got.Count() != 2 {
		t.Fatal(err, got)
	}
	// Mapping requirements apply too.
	plain := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("m", "mapped", "o/one", shaA))
	recs := catalog.Recommendations{Requires: map[string][]string{"a": {"m"}}}
	got, err = s.PlanUpdate(context.Background(), plain, recs, UpdateOptions{})
	if err != nil || got.Count() != 2 || got.Plans[0].Skills[0].ID != "m" {
		t.Fatal(err, got)
	}
	// A conflict between two skills that are already installed does not block refreshing one.
	both := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("b", "beta", "o/one", shaA))
	a := both.Skills["a"]
	a.ConflictsWith = []string{"b"}
	both.Skills["a"] = a
	if got, err = s.PlanUpdate(context.Background(), both, catalog.Recommendations{}, UpdateOptions{}); err != nil || got.Count() != 1 {
		t.Fatal(err, got)
	}
	// A requirement that would replace a different installed skill with the same name is refused.
	clash := catalogOf(alpha, gitSkill("x", "gamma", "o/other", shaA))
	a2 := clash.Skills["a"]
	a2.Requires = []string{"x"}
	clash.Skills["a"] = a2
	if _, err = s.PlanUpdate(context.Background(), clash, catalog.Recommendations{}, UpdateOptions{}); !errors.As(err, &policy) || !strings.Contains(err.Error(), "would replace") {
		t.Fatal(err)
	}
}

func TestUpdateStopsAtFirstFailedSourceAndKeepsCompletedWork(t *testing.T) {
	s, f, _, srcB := updateLab(t)
	srcTwo, srcThree := t.TempDir(), t.TempDir()
	skillAt(t, srcTwo, "delta", "x")
	skillAt(t, srcThree, "echo", "x")
	f.dirs["o/two#"+shaA], f.dirs["o/three#"+shaA] = srcTwo, srcThree
	mustInstall(t, s, plan("o/two", shaA, "copy", []string{"codex"}, "delta"))
	mustInstall(t, s, plan("o/three", shaA, "copy", []string{"codex"}, "echo"))
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB), gitSkill("d", "delta", "o/two", shaB), gitSkill("e", "echo", "o/three", shaB))
	// The first source succeeds, the second cannot be fetched, the third must not start.
	f.dirs["o/one#"+shaB], f.dirs["o/three#"+shaB] = srcB, srcThree
	got := mustPlanUpdate(t, s, c, UpdateOptions{})
	delete(f.dirs, "o/two#"+shaB)
	before := len(f.fetches())
	results, err := s.Update(context.Background(), got, nil)
	if err == nil || !strings.Contains(err.Error(), "update failed for o/two") || !strings.Contains(err.Error(), "1 later sources not started") {
		t.Fatal(err)
	}
	if len(results) != 3 || !results[0].LockWritten || results[1].Err == nil || results[2].Started || results[2].Skills[0].Status != skillkit.StatusNotStarted {
		t.Fatalf("%+v", results)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v2 alpha" || readLock(t, s).Skills["alpha"].Ref != shaB {
		t.Fatal("completed work was lost")
	}
	if lock := readLock(t, s); lock.Skills["delta"].Ref != shaA || lock.Skills["echo"].Ref != shaA {
		t.Fatalf("%+v", lock.Skills)
	}
	for _, key := range f.fetches()[before:] {
		if strings.HasPrefix(key, "o/three") {
			t.Fatal("a later source was fetched")
		}
	}
}

func TestUpdateRecordsWhatSucceededWhenOneLocationFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind root")
	}
	srcA, srcB := t.TempDir(), t.TempDir()
	skillAt(t, srcA, "alpha", "v1")
	skillAt(t, srcB, "alpha", "v2")
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: srcA, "o/one#" + shaB: srcB}}
	s := newService(t, f)
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "mine", Name: "Mine", SkillsDir: ".mine/skills"}}
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"codex", "mine"}, "alpha"))
	got := mustPlanUpdate(t, s, catalogOf(gitSkill("a", "alpha", "o/one", shaB)), UpdateOptions{})
	// A read-only skills directory makes only the .mine copy fail to swap in.
	if err := os.Chmod(s.at(".mine/skills"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.at(".mine/skills"), 0o755) })
	results, err := s.Update(context.Background(), got, nil)
	if err == nil || results[0].Skills[0].Status != skillkit.StatusInstalled || !results[0].LockWritten {
		t.Fatal("a skill refreshed at one location is recorded even though another failed:", err, results)
	}
	if lines := strings.Join(results[0].Lines(), "\n"); !strings.Contains(lines, "failed at .mine/skills/alpha") {
		t.Fatal(lines)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v2" || read(t, s.at(".mine/skills/alpha/data.txt")) != "v1" || readLock(t, s).Skills["alpha"].Ref != shaB {
		t.Fatal("unexpected state after a partially failed update")
	}
}

func TestUpdateNeverWritesThroughASymlinkedAgentDirectoryLeavingTheWorkspace(t *testing.T) {
	srcA, srcB := t.TempDir(), t.TempDir()
	skillAt(t, srcA, "alpha", "v1")
	skillAt(t, srcB, "alpha", "v2")
	f := &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: srcA, "o/one#" + shaB: srcB}}
	s := newService(t, f)
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"codex"}, "alpha"))
	outside := t.TempDir()
	skillAt(t, outside, "skills/alpha", "outside")
	if err := os.Symlink(outside, s.at(".claude")); err != nil {
		t.Fatal(err)
	}
	got := mustPlanUpdate(t, s, catalogOf(gitSkill("a", "alpha", "o/one", shaB)), UpdateOptions{})
	if len(got.Notes) == 0 || !strings.Contains(strings.Join(got.Lines(), "\n"), "warning:") {
		t.Fatalf("the unreadable directory must be reported: %+v", got.Notes)
	}
	if _, err := s.Update(context.Background(), got, nil); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(outside, "skills", "alpha", "data.txt")) != "outside" {
		t.Fatal("update wrote outside the workspace")
	}
}

func TestUpdateAndUninstallAreRefusedWhileAnotherOperationRuns(t *testing.T) {
	s, _, _, _ := updateLab(t)
	c := catalogOf(gitSkill("a", "alpha", "o/one", shaB))
	updatePlan := mustPlanUpdate(t, s, c, UpdateOptions{})
	removePlan, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	s.LockTimeout = 50 * time.Millisecond
	holder := begin(t, s)
	var policy PolicyError
	if _, err = s.Update(context.Background(), updatePlan, nil); !errors.As(err, &policy) || !strings.Contains(err.Error(), "another Nosey operation is running") {
		t.Fatal(err)
	}
	if _, err = s.Uninstall(context.Background(), removePlan, nil); !errors.As(err, &policy) || !strings.Contains(err.Error(), "another Nosey operation is running") {
		t.Fatal(err)
	}
	if read(t, s.at(".agents/skills/alpha/data.txt")) != "v1 alpha" || !exists(s.at(".agents/skills/alpha")) {
		t.Fatal("a refused operation changed the project")
	}
	_ = holder.Close()
}

// uninstallLab installs alpha, beta and gamma by copy for codex and claude-code.
func uninstallLab(t *testing.T) Service {
	t.Helper()
	src := t.TempDir()
	for _, n := range []string{"alpha", "beta", "gamma"} {
		skillAt(t, src, n, n)
	}
	s := newService(t, &fakeFetcher{dirs: map[string]string{"o/one#" + shaA: src}})
	mustInstall(t, s, plan("o/one", shaA, "copy", []string{"codex", "claude-code"}, "alpha", "beta", "gamma"))
	return s
}

func uninstallCatalog() domain.Catalog {
	return catalogOf(gitSkill("a", "alpha", "o/one", shaA), gitSkill("b", "beta", "o/one", shaA), gitSkill("g", "gamma", "o/one", shaA))
}

func TestUninstallByNameAndCatalogIDLeavesOtherSkillsAlone(t *testing.T) {
	s := uninstallLab(t)
	c := uninstallCatalog()
	plan, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"alpha", "b"}})
	if err != nil || len(plan.Skills) != 2 {
		t.Fatal(err, plan)
	}
	results, err := s.Uninstall(context.Background(), plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != RemoveDone || !r.LockUpdated || len(r.Removed) != 2 {
			t.Fatalf("%+v", r)
		}
	}
	for _, rel := range []string{".agents/skills/alpha", ".claude/skills/alpha", ".agents/skills/beta", ".claude/skills/beta"} {
		if exists(s.at(rel)) {
			t.Fatal("not removed:", rel)
		}
	}
	if read(t, s.at(".claude/skills/gamma/SKILL.md")) == "" || read(t, s.at(".agents/skills/gamma/data.txt")) != "gamma" {
		t.Fatal("an unselected skill changed")
	}
	lock := readLock(t, s)
	if len(lock.Skills) != 1 || lock.Skills["gamma"].SkillPath == "" {
		t.Fatalf("%+v", lock.Skills)
	}
	if line := results[0].Line(); !strings.HasPrefix(line, "removed ") {
		t.Fatal(line)
	}
}

func TestUninstallRejectsUntrackedAndUnknownTargets(t *testing.T) {
	s := uninstallLab(t)
	c := catalogOf(append(slices.Collect(mapsValues(uninstallCatalog().Skills)), gitSkill("n", "never", "o/one", shaA))...)
	// Installed on disk but not recorded in the lock.
	skillAt(t, s.Store.Workspace, ".agents/skills/hand", "hand")
	for _, target := range []string{"hand", "n", "nope", ""} {
		_, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"alpha", target}})
		var policy PolicyError
		if !errors.As(err, &policy) {
			t.Fatalf("%q: %v", target, err)
		}
	}
	if _, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{}); err == nil {
		t.Fatal("an empty selection is invalid")
	}
	if !exists(s.at(".agents/skills/alpha")) {
		t.Fatal("planning must not remove anything")
	}
}

func mapsValues(m map[string]domain.Skill) func(func(domain.Skill) bool) {
	return func(yield func(domain.Skill) bool) {
		for _, v := range m {
			if !yield(v) {
				return
			}
		}
	}
}

func TestUninstallBlocksOnDependentsUnlessIgnored(t *testing.T) {
	s := uninstallLab(t)
	c := uninstallCatalog()
	beta := c.Skills["b"]
	beta.Requires = []string{"a"}
	c.Skills["b"] = beta
	recs := catalog.Recommendations{Requires: map[string][]string{"g": {"a"}}}
	_, err := s.PlanUninstall(context.Background(), c, recs, UninstallOptions{Targets: []string{"alpha"}})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "beta requires alpha") || !strings.Contains(err.Error(), "gamma requires alpha") || !strings.Contains(err.Error(), "--ignore-requirements") {
		t.Fatal(err)
	}
	plan, err := s.PlanUninstall(context.Background(), c, recs, UninstallOptions{Targets: []string{"alpha"}, IgnoreRequirements: true})
	if err != nil || len(plan.Skills) != 1 || len(plan.Overridden) != 2 || !strings.Contains(strings.Join(plan.Lines(), "\n"), "warning: beta requires alpha") {
		t.Fatal(err, plan)
	}
	// Selecting a dependent together with its requirement is allowed and removes the dependent first.
	plan, err = s.PlanUninstall(context.Background(), c, recs, UninstallOptions{Targets: []string{"alpha", "beta", "gamma"}})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, sk := range plan.Skills {
		names = append(names, sk.Name)
	}
	if indexOf(names, "alpha") != 2 || len(plan.Overridden) != 0 {
		t.Fatalf("a requirement must go after its dependents: %v", names)
	}
	// Only the dependents are selected: the remaining requirement is not a dependent.
	if _, err = s.PlanUninstall(context.Background(), c, recs, UninstallOptions{Targets: []string{"beta", "gamma"}}); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallRechecksDependentsUnderTheOperationLock(t *testing.T) {
	s := uninstallLab(t)
	c := uninstallCatalog()
	beta := c.Skills["b"]
	beta.Requires = []string{"a"}
	c.Skills["b"] = beta
	workspace := s.Store.Workspace
	before, err := skillkit.ReadLock(workspace)
	if err != nil {
		t.Fatal(err)
	}
	withoutBeta, err := skillkit.ReadLock(workspace)
	if err != nil {
		t.Fatal(err)
	}
	withoutBeta.Remove("beta")
	if err = withoutBeta.Write(workspace); err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"alpha"}})
	if err != nil {
		t.Fatal(err)
	}
	// Another process tracks the dependent after the plan was confirmed.
	if err = before.Write(workspace); err != nil {
		t.Fatal(err)
	}
	var policy PolicyError
	if _, err = s.Uninstall(context.Background(), plan, nil); !errors.As(err, &policy) || !strings.Contains(err.Error(), "beta requires alpha") {
		t.Fatal(err)
	}
	after, err := skillkit.ReadLock(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := after.Skills["alpha"]; !ok {
		t.Fatal("a stale plan removed a skill that is now required")
	}
}

func indexOf(v []string, s string) int { return slices.Index(v, s) }

func TestUninstallStopsAtFirstFailureAndKeepsTheLockEntry(t *testing.T) {
	s := uninstallLab(t)
	c := uninstallCatalog()
	plan, err := s.PlanUninstall(context.Background(), c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"alpha", "beta", "gamma"}})
	if err != nil {
		t.Fatal(err)
	}
	// Outside the workspace: .claude is a symlink that leaves it, so removals through it are refused.
	outside := t.TempDir()
	skillAt(t, outside, "skills/beta", "outside")
	if err = os.RemoveAll(s.at(".claude")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, s.at(".claude")); err != nil {
		t.Fatal(err)
	}
	results, err := s.Uninstall(context.Background(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "uninstall failed for alpha") || !strings.Contains(err.Error(), "2 later skills not started") {
		t.Fatal(err)
	}
	if len(results) != 3 || results[0].Status != RemoveFailed || results[1].Status != RemoveNotStarted || results[2].Status != RemoveNotStarted {
		t.Fatalf("%+v", results)
	}
	var refused *skillkit.PolicyError
	if !errors.As(results[0].Err, &refused) {
		t.Fatalf("a symlinked agent directory leaving the workspace must be refused: %v", results[0].Err)
	}
	if read(t, filepath.Join(outside, "skills", "beta", "data.txt")) != "outside" {
		t.Fatal("something outside the workspace was touched")
	}
	lock := readLock(t, s)
	if len(lock.Skills) != 3 {
		t.Fatalf("a skill that was not fully removed keeps its lock entry: %+v", lock.Skills)
	}
	if !exists(s.at(".agents/skills/alpha")) || !exists(s.at(".agents/skills/beta")) {
		t.Fatal("canonical copies must be kept when an agent directory could not be cleaned")
	}
	if line := results[0].Line(); !strings.Contains(line, "failed alpha") {
		t.Fatal(line)
	}
}

func TestUninstallPartialFailureKeepsEarlierRemovals(t *testing.T) {
	s := uninstallLab(t)
	plan := UninstallPlan{Skills: []RemoveSkill{{Name: "alpha"}, {Name: "ghost"}, {Name: "gamma"}}}
	results, err := s.Uninstall(context.Background(), plan, nil)
	if err == nil || !strings.Contains(err.Error(), "ghost") || len(results) != 3 {
		t.Fatal(err, results)
	}
	if results[0].Status != RemoveDone || results[1].Status != RemoveFailed || results[2].Status != RemoveNotStarted {
		t.Fatalf("%+v", results)
	}
	lock := readLock(t, s)
	if _, ok := lock.Skills["alpha"]; ok || lock.Skills["gamma"].SkillPath == "" || !exists(s.at(".agents/skills/gamma")) {
		t.Fatalf("%+v", lock.Skills)
	}
}

func TestUninstallOfUnmanagedTrackedSkillWorksWithoutACatalogID(t *testing.T) {
	s := uninstallLab(t)
	plan, err := s.PlanUninstall(context.Background(), catalogOf(), catalog.Recommendations{}, UninstallOptions{Targets: []string{"gamma"}})
	if err != nil || plan.Skills[0].ID != "" {
		t.Fatal(err, plan)
	}
	if _, err = s.Uninstall(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
}
