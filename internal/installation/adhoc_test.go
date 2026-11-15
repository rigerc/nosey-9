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
