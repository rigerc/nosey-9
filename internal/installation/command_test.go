package installation

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

var (
	shaA = strings.Repeat("a", 40)
	shaB = strings.Repeat("b", 40)
)

func gitSkill(id, name, uri, ref string) domain.Skill {
	return domain.Skill{ID: id, Name: name, Source: domain.Source{Kind: "git", URI: uri, Ref: ref, SkillName: name}}
}

func buildService() Service { return Service{Settings: settings.Defaults(), Home: "/nonexistent-home"} }

func TestBuildGroupsOnePlanPerSource(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"a": gitSkill("a", "alpha", "https://github.com/o/one", shaA),
		"b": gitSkill("b", "beta", "https://github.com/o/one", shaA),
		"c": gitSkill("c", "gamma", "o/two", shaB),
	}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"c", "b", "a"}, Agents: []string{"codex", "claude-code"}, Method: "copy", Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	agents := []string{"claude-code", "codex"}
	want := []SourcePlan{
		{Source: "https://github.com/o/one", Ref: shaA, Skills: []AddSkill{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}, Agents: agents, Method: "copy"},
		{Source: "o/two", Ref: shaB, Skills: []AddSkill{{ID: "c", Name: "gamma"}}, Agents: agents, Method: "copy"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestBuildSplitsTheSameSourceAtDifferentRefs(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"a": gitSkill("a", "alpha", "o/one", shaA),
		"b": gitSkill("b", "beta", "o/one", shaB),
	}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, Agents: []string{"codex"}})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
}

func TestBuildRequiresTargetAgents(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": gitSkill("a", "alpha", "o/one", "")}}
	if _, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}}); err == nil {
		t.Fatal("a plan without agents cannot run unattended")
	}
	_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}, Yes: true})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "--agent") {
		t.Fatal("--yes accepted without target agents:", err)
	}
	s := buildService()
	s.Settings.Installation.Agents = []string{"codex"}
	got, err := s.Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}})
	if err != nil || !slices.Equal(got[0].Agents, []string{"codex"}) || got[0].Method != "symlink" {
		t.Fatal("settings agents and method were not used", got, err)
	}
}

func TestBuildAcceptsCustomAgentsAndRejectsUnknownOnes(t *testing.T) {
	s := buildService()
	s.Settings.Installation.CustomAgents = []settings.CustomAgent{{ID: "my-agent", Name: "My Agent", SkillsDir: ".my-agent/skills"}}
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": gitSkill("a", "alpha", "o/one", shaA)}}
	if _, err := s.Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}, Agents: []string{"my-agent"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}, Agents: []string{"my-agent"}}); err == nil {
		t.Fatal("custom agent accepted without being configured")
	}
	if got := s.AgentNames()["my-agent"]; got != "My Agent" {
		t.Fatal("AgentNames must include custom agents:", got)
	}
	names := buildService().AgentNames()
	if len(names) != 79 || names["claude-code"] != "Claude Code" {
		t.Fatal(len(names))
	}
}

func TestBuildResolvesDependencies(t *testing.T) {
	dep := gitSkill("dep", "dep", "o/one", shaA)
	done := gitSkill("done", "done", "o/one", shaA)
	top := gitSkill("top", "top", "o/one", shaA)
	top.Requires = []string{"dep", "done"}
	c := domain.Catalog{Skills: map[string]domain.Skill{"top": top, "dep": dep, "done": done}, InstallStatus: map[string]string{"done": "installed"}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"top"}, Agents: []string{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []AddSkill{{ID: "dep", Name: "dep", Required: true}, {ID: "top", Name: "top"}}; !reflect.DeepEqual(got[0].Skills, want) {
		t.Fatalf("%+v", got[0].Skills)
	}
}

func TestBuildBlocksSameInstallNameEvenWhenIgnoringConflicts(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"a": gitSkill("a", "python-testing", "o/one", shaA),
		"b": gitSkill("b", "Python-Testing", "o/two", shaA),
	}}
	_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, Agents: []string{"codex"}, IgnoreConflicts: true})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "both install as") {
		t.Fatal(err)
	}
}

func TestBuildReportsConflictsAndAllowsOptOut(t *testing.T) {
	a := gitSkill("a", "alpha", "o/one", shaA)
	a.ConflictsWith = []string{"b", "c"}
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": a, "b": gitSkill("b", "beta", "o/one", shaA), "c": gitSkill("c", "gamma", "o/one", shaA)}, InstallStatus: map[string]string{"c": "outdated"}}
	_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, Agents: []string{"codex"}})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "conflicts with selected b") || !strings.Contains(err.Error(), "conflicts with installed c") {
		t.Fatal(err)
	}
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, Agents: []string{"codex"}, IgnoreConflicts: true}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPassesLocalSkillsByAbsolutePath(t *testing.T) {
	root := t.TempDir()
	skill := domain.Skill{ID: "l", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "skills/x", SkillName: "x"}}
	c := domain.Catalog{Skills: map[string]domain.Skill{"l": skill}, Roots: map[string]string{"r": root}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"l"}, Agents: []string{"codex"}})
	if err != nil || got[0].Source != filepath.Join(root, "skills/x") || !got[0].Local() || got[0].Ref != "" {
		t.Fatal(got, err)
	}
	skill.Source.Path = "../escape"
	c.Skills["l"] = skill
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"l"}, Agents: []string{"codex"}}); err == nil {
		t.Fatal("escaping local path accepted")
	}
	c.Roots["r"] = "relative/root"
	skill.Source.Path = "x"
	c.Skills["l"] = skill
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"l"}, Agents: []string{"codex"}}); err == nil {
		t.Fatal("relative registry root accepted")
	}
}

func TestBuildRejectsInvalidRequests(t *testing.T) {
	good := gitSkill("a", "alpha", "o/one", shaA)
	unsafeName := gitSkill("name", "-x", "o/one", shaA)
	unsafeURI := gitSkill("uri", "alpha", "--upload-pack=x", "")
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": good, "name": unsafeName, "uri": unsafeURI}, Blocked: map[string]string{"gone": "removed upstream"}}
	for name, opts := range map[string]Options{
		"empty":       {Agents: []string{"codex"}},
		"unknown":     {IDs: []string{"nope"}, Agents: []string{"codex"}},
		"blocked":     {IDs: []string{"gone"}, Agents: []string{"codex"}},
		"agent":       {IDs: []string{"a"}, Agents: []string{"*"}},
		"method":      {IDs: []string{"a"}, Agents: []string{"codex"}, Method: "hardlink"},
		"unsafe name": {IDs: []string{"name"}, Agents: []string{"codex"}},
		"unsafe uri":  {IDs: []string{"uri"}, Agents: []string{"codex"}},
	} {
		if _, err := buildService().Build(c, catalog.Recommendations{}, opts); err == nil {
			t.Fatal("accepted:", name)
		}
	}
}

func TestPreflightRefusesHomeAndBrokenLock(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	var policy PolicyError
	if err = (Service{Store: settings.Store{Workspace: home}}).Preflight(); !errors.As(err, &policy) {
		t.Fatal("home directory accepted:", err)
	}
	s := Service{Store: settings.Store{Workspace: t.TempDir()}}
	if err = s.Preflight(); err != nil {
		t.Fatal("missing lock must not block a first install:", err)
	}
	if err = storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	if err = s.Preflight(); !errors.As(err, &policy) || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatal("malformed lock accepted:", err)
	}
}

func TestBeginRefusesBeforeTakingTheLock(t *testing.T) {
	s := newService(t, nil)
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	var policy PolicyError
	if _, err := s.Begin(context.Background()); !errors.As(err, &policy) {
		t.Fatal("malformed lock reached the engine:", err)
	}
	if err := removeFile(filepath.Join(s.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryListsSourceRefSkillsAgentsAndMethod(t *testing.T) {
	p := SourcePlan{Source: "https://github.com/o/r", Ref: "v1", Skills: []AddSkill{{Name: "alpha"}, {Name: "beta", Required: true}}, Agents: []string{"codex", "claude-code"}, Method: "copy"}
	got := p.Summary()
	for _, want := range []string{"Source: https://github.com/o/r", "Ref: v1", "Skills: alpha, beta (required)", "Agents: codex, claude-code", "Method: copy"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if got = (SourcePlan{Source: "o/r\x1b[31m", Method: "symlink"}).Summary(); strings.Contains(got, "\x1b") || !strings.Contains(got, "default branch") {
		t.Fatal(got)
	}
	if got = (SourcePlan{Source: "https://github.com/o/r.git", Ref: shaA}).Label(); got != "o/r @aaaaaaa" {
		t.Fatal(got)
	}
}

func TestCustomAgentsCannotReuseBuiltInIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := settings.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Set("installation.customAgents", []byte(`[{"id":"claude-code","name":"X","skillsDir":".x/skills"}]`), false)
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatal("built-in ID accepted:", err)
	}
	if err = store.Set("installation.customAgents", []byte(`[{"id":"my-agent","name":"X","skillsDir":".x/skills"}]`), false); err != nil {
		t.Fatal(err)
	}
}
