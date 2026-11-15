package installation

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/skillkit"
)

func TestBuildAndInstallAdHocLocalSource(t *testing.T) {
	s := newService(t, nil)
	src := t.TempDir()
	writeSkill(t, src, "alpha", "alpha")
	writeSkill(t, src, "nested/beta", "beta")
	c := domain.Catalog{Skills: map[string]domain.Skill{}}
	plans, err := s.Build(context.Background(), c, catalog.Recommendations{}, Options{Sources: []string{src}, Agents: []string{"codex"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Skills) != 2 {
		t.Fatalf("%+v", plans)
	}
	names := []string{plans[0].Skills[0].Name, plans[0].Skills[1].Name}
	slices.Sort(names)
	if !slices.Equal(names, []string{"alpha", "beta"}) {
		t.Fatal(names)
	}
	results, err := s.Install(context.Background(), plans, nil)
	if err != nil {
		t.Fatal(err, results)
	}
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Skills["alpha"]; !ok {
		t.Fatal("alpha not recorded")
	}
	if _, ok := lock.Skills["beta"]; !ok {
		t.Fatal("beta not recorded")
	}
	if _, err := filepath.Abs(src); err == nil {
		if !exists(filepath.Join(s.Store.Workspace, ".agents", "skills", "alpha", "SKILL.md")) {
			t.Fatal("alpha not installed")
		}
	}
}

func TestBuildAdHocSourceFiltersSkills(t *testing.T) {
	s := newService(t, nil)
	src := t.TempDir()
	writeSkill(t, src, "alpha", "alpha")
	writeSkill(t, src, "beta", "beta")
	c := domain.Catalog{Skills: map[string]domain.Skill{}}
	plans, err := s.Build(context.Background(), c, catalog.Recommendations{}, Options{Sources: []string{src}, Skills: []string{"beta"}, Agents: []string{"codex"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || len(plans[0].Skills) != 1 || plans[0].Skills[0].Name != "beta" {
		t.Fatalf("%+v", plans)
	}
}

func TestAdHocLocalSourceUpdateAndUninstallBySource(t *testing.T) {
	ctx := context.Background()
	s := newService(t, nil)
	src := filepath.Join(s.Store.Workspace, "vendor", "skills")
	writeSkill(t, src, "alpha", "alpha", "data.txt", "v1")
	writeSkill(t, src, "beta", "beta", "data.txt", "v1")
	other := t.TempDir()
	writeSkill(t, other, "gamma", "gamma")
	c := domain.Catalog{Skills: map[string]domain.Skill{}}
	plans, err := s.Build(ctx, c, catalog.Recommendations{}, Options{Sources: []string{"./vendor/skills", other}, Agents: []string{"codex"}, Method: "copy", Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Install(ctx, plans, nil); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, src, "alpha", "alpha", "data.txt", "v2")
	update, err := s.PlanUpdate(ctx, c, catalog.Recommendations{}, UpdateOptions{IDs: []string{"vendor/skills"}})
	if err != nil {
		t.Fatal(err)
	}
	if update.Count() != 1 || update.Plans[0].Source != src || update.Plans[0].Skills[0].Name != "alpha" {
		t.Fatalf("%+v", update)
	}
	if !slices.Equal(update.Skipped, []SkippedSkill{{"beta", "already up to date"}}) {
		t.Fatalf("%+v", update.Skipped)
	}
	if results, err := s.Update(ctx, update, nil); err != nil {
		t.Fatal(err, results)
	}
	if got := read(t, s.at(".agents/skills/alpha/data.txt")); got != "v2" {
		t.Fatal(got)
	}
	remove, err := s.PlanUninstall(ctx, c, catalog.Recommendations{}, UninstallOptions{Targets: []string{"./vendor/skills"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(remove.Skills) != 2 || remove.Skills[0].Name != "alpha" || remove.Skills[1].Name != "beta" {
		t.Fatalf("%+v", remove.Skills)
	}
	if _, err = s.Uninstall(ctx, remove, nil); err != nil {
		t.Fatal(err)
	}
	lock := readLock(t, s)
	if _, ok := lock.Skills["gamma"]; !ok || len(lock.Skills) != 1 || exists(s.at(".agents/skills/alpha")) || !exists(s.at(".agents/skills/gamma")) {
		t.Fatalf("only the named source is removed: %+v", lock.Skills)
	}
}
