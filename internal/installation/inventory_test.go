package installation

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

func TestInventoryUsesInstalledTreesAndLock(t *testing.T) {
	s := newService(t, nil)
	// A detected Codex makes .agents/skills attributable, as the real CLI does.
	codexHome := filepath.Join(s.Home, ".codex")
	if err := storage.Write(filepath.Join(codexHome, "config.toml"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	workspace := s.Store.Workspace
	for _, name := range []string{"current", "stale", "branch", "mine"} {
		writeSkill(t, workspace, ".agents/skills/"+name, name)
	}
	lock := `{"version":1,"skills":{
		"current":{"source":"o/one","ref":"` + shaA + `","sourceType":"github","skillPath":"skills/current/SKILL.md","computedHash":"h"},
		"stale":{"source":"o/one","ref":"` + shaB + `","sourceType":"github","computedHash":"h"},
		"branch":{"source":"o/two","ref":"` + shaB + `","sourceType":"github","computedHash":"h"},
		"gone":{"source":"o/one","sourceType":"github","computedHash":"h"}}}`
	if err := storage.Write(filepath.Join(workspace, "skills-lock.json"), []byte(lock)); err != nil {
		t.Fatal(err)
	}
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"current": gitSkill("current", "current", "https://github.com/O/One.git", shaA),
		"stale":   gitSkill("stale", "stale", "o/one", shaA),
		"branch":  gitSkill("branch", "branch", "o/two", "main"),
	}}
	result := s.Inventory(context.Background(), c)
	if _, err := contracts.Encode("list-output.schema.json", result); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, record := range result.Skills {
		got[record.Name] = record.Status
		if record.Path == nil || !filepath.IsAbs(*record.Path) || !reflect.DeepEqual(record.Agents, []string{"codex"}) {
			t.Fatalf("%+v", record)
		}
	}
	want := map[string]string{"current": "installed", "stale": "outdated", "branch": "installed", "mine": "unknown"}
	if !result.Complete || !reflect.DeepEqual(got, want) {
		t.Fatalf("complete=%t %v", result.Complete, got)
	}
	for _, record := range result.Skills {
		if managed := record.SkillID != nil; managed == record.Unmanaged || managed != (record.Name == "current" || record.Name == "stale" || record.Name == "branch") {
			t.Fatalf("%+v", record)
		}
	}
}

func TestInventoryListsACodexOnlyInstall(t *testing.T) {
	s := newService(t, nil)
	t.Setenv("CODEX_HOME", filepath.Join(s.Home, ".codex"))
	if err := storage.Write(filepath.Join(s.Home, ".codex", "x"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	writeSkill(t, src, "alpha", "alpha")
	if _, err := s.Install(context.Background(), []SourcePlan{plan(filepath.Join(src, "alpha"), "", "copy", []string{"codex"}, "alpha")}, nil); err != nil {
		t.Fatal(err)
	}
	result := s.Inventory(context.Background(), domain.Catalog{})
	if len(result.Skills) != 1 || !reflect.DeepEqual(result.Skills[0].Agents, []string{"codex"}) || result.Skills[0].Status != "installed" || !result.Skills[0].Unmanaged {
		t.Fatalf("%+v", result.Skills)
	}
}

func TestInventoryAttributesCustomAgents(t *testing.T) {
	s := newService(t, nil)
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "my-agent", Name: "My Agent", SkillsDir: ".my-agent/skills"}}
	writeSkill(t, s.Store.Workspace, ".my-agent/skills/mine", "mine")
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(`{"version":1,"skills":{"mine":{"source":"o/one","sourceType":"github","computedHash":"h"}}}`)); err != nil {
		t.Fatal(err)
	}
	result := s.Inventory(context.Background(), domain.Catalog{})
	if len(result.Skills) != 1 || !reflect.DeepEqual(result.Skills[0].Agents, []string{"my-agent"}) || result.Skills[0].Status != "installed" {
		t.Fatalf("%+v", result.Skills)
	}
}

func TestInventoryReportsAnUnreadableLockAndUnavailableEngine(t *testing.T) {
	s := newService(t, nil)
	writeSkill(t, s.Store.Workspace, ".agents/skills/x", "x")
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	result := s.Inventory(context.Background(), domain.Catalog{})
	if result.Complete || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "skills-lock.json") || result.Skills[0].Status != "unknown" {
		t.Fatalf("%+v", result)
	}
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "codex", Name: "Dup", SkillsDir: ".x"}}
	if result = s.Inventory(context.Background(), domain.Catalog{}); result.Complete || len(result.Diagnostics) == 0 || len(result.Skills) != 0 {
		t.Fatal("an unusable agent table claimed a complete inventory", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result = s.Inventory(ctx, domain.Catalog{}); result.Complete {
		t.Fatal(result)
	}
}

func TestInventoryFlagsSkillsItCannotParse(t *testing.T) {
	s := newService(t, nil)
	if err := storage.Write(filepath.Join(s.Store.Workspace, ".agents", "skills", "bad", "SKILL.md"), []byte("nope")); err != nil {
		t.Fatal(err)
	}
	result := s.Inventory(context.Background(), domain.Catalog{})
	if result.Complete || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "incomplete") {
		t.Fatalf("%+v", result)
	}
}

func TestLocalSkillIdentityFromLockPath(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(filepath.Dir(workspace), "registry")
	s := Service{Store: settings.Store{Workspace: workspace, Data: t.TempDir()}}
	skill := domain.Skill{ID: "l", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "x", SkillName: "x"}}
	c := domain.Catalog{Skills: map[string]domain.Skill{"l": skill}, Roots: map[string]string{"r": root}}
	local := func(source string) skillEntry { return skillEntry{SourceType: "local", Source: source} }
	if id, logical, _ := s.catalogIdentity("x", local("../registry/x"), c); id == nil || *id != "l" || *logical != "r:x" {
		t.Fatal("relative lock path not matched")
	}
	store := filepath.Join(s.Store.Data, "install-sources", "hash")
	if id, _, _ := s.catalogIdentity("x", local(store), c); id == nil {
		t.Fatal("content-addressed source not matched")
	}
	if id, _, _ := s.catalogIdentity("x", local("../other/x"), c); id != nil {
		t.Fatal("unrelated local source matched")
	}
	if id, _, _ := s.catalogIdentity("x", skillEntry{SourceType: "github", Source: "../registry/x"}, c); id != nil {
		t.Fatal("non-local lock entry matched")
	}
}
