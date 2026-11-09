package installation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
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

func buildService() Service { return Service{Settings: settings.Defaults()} }

func TestBuildGroupsOneCommandPerSource(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"a": gitSkill("a", "alpha", "https://github.com/o/one", shaA),
		"b": gitSkill("b", "beta", "https://github.com/o/one", shaA),
		"c": gitSkill("c", "gamma", "o/two", shaB),
	}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"c", "b", "a"}, Agents: []string{"codex", "claude-code"}, Method: "copy", Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []AddCommand{
		{Source: "https://github.com/o/one", Ref: shaA, Skills: []AddSkill{{ID: "a", Name: "alpha"}, {ID: "b", Name: "beta"}}, Args: []string{"add", "https://github.com/o/one#" + shaA, "--full-depth", "--skill", "alpha", "beta", "--agent", "claude-code", "codex", "--copy", "--yes"}},
		{Source: "o/two", Ref: shaB, Skills: []AddSkill{{ID: "c", Name: "gamma"}}, Args: []string{"add", "o/two#" + shaB, "--full-depth", "--skill", "gamma", "--agent", "claude-code", "codex", "--copy", "--yes"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}

func TestBuildLeavesPromptsToTheInstaller(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": gitSkill("a", "alpha", "o/one", "")}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"add", "o/one", "--full-depth", "--skill", "alpha"}; !slices.Equal(got[0].Args, want) {
		t.Fatal(got[0].Args)
	}
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}, Yes: true}); err == nil {
		t.Fatal("--yes accepted without target agents")
	}
}

func TestBuildNeverRequestsGlobalOrAll(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": gitSkill("a", "alpha", "o/one", shaA)}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}, Agents: []string{"codex"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range got[0].Args {
		if arg == "--global" || arg == "-g" || arg == "--all" || arg == "*" {
			t.Fatal(got[0].Args)
		}
	}
}

func TestBuildResolvesDependencies(t *testing.T) {
	dep := gitSkill("dep", "dep", "o/one", shaA)
	done := gitSkill("done", "done", "o/one", shaA)
	top := gitSkill("top", "top", "o/one", shaA)
	top.Requires = []string{"dep", "done"}
	c := domain.Catalog{Skills: map[string]domain.Skill{"top": top, "dep": dep, "done": done}, InstallStatus: map[string]string{"done": "installed"}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"top"}})
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
	_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, IgnoreConflicts: true})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "both install as") {
		t.Fatal(err)
	}
}

func TestBuildReportsConflictsAndAllowsOptOut(t *testing.T) {
	a := gitSkill("a", "alpha", "o/one", shaA)
	a.ConflictsWith = []string{"b", "c"}
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": a, "b": gitSkill("b", "beta", "o/one", shaA), "c": gitSkill("c", "gamma", "o/one", shaA)}, InstallStatus: map[string]string{"c": "outdated"}}
	_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}})
	var policy PolicyError
	if !errors.As(err, &policy) || !strings.Contains(err.Error(), "conflicts with selected b") || !strings.Contains(err.Error(), "conflicts with installed c") {
		t.Fatal(err)
	}
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"a", "b"}, IgnoreConflicts: true}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPassesLocalSkillsByAbsolutePath(t *testing.T) {
	root := t.TempDir()
	skill := domain.Skill{ID: "l", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "skills/x", SkillName: "x"}}
	c := domain.Catalog{Skills: map[string]domain.Skill{"l": skill}, Roots: map[string]string{"r": root}}
	got, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"l"}})
	if err != nil || got[0].Args[1] != filepath.Join(root, "skills/x") {
		t.Fatal(got, err)
	}
	skill.Source.Path = "../escape"
	c.Skills["l"] = skill
	if _, err = buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{"l"}}); err == nil {
		t.Fatal("escaping local path accepted")
	}
}

func TestBuildRejectsInvalidRequests(t *testing.T) {
	good := gitSkill("a", "alpha", "o/one", shaA)
	unsafeName := gitSkill("name", "-x", "o/one", shaA)
	unsafeURI := gitSkill("uri", "alpha", "--upload-pack=x", "")
	c := domain.Catalog{Skills: map[string]domain.Skill{"a": good, "name": unsafeName, "uri": unsafeURI}, Blocked: map[string]string{"gone": "removed upstream"}}
	for name, opts := range map[string]Options{
		"empty":       {},
		"unknown":     {IDs: []string{"nope"}},
		"blocked":     {IDs: []string{"gone"}},
		"agent":       {IDs: []string{"a"}, Agents: []string{"*"}},
		"method":      {IDs: []string{"a"}, Method: "hardlink"},
		"unsafe name": {IDs: []string{"name"}},
		"unsafe uri":  {IDs: []string{"uri"}},
	} {
		if _, err := buildService().Build(c, catalog.Recommendations{}, opts); err == nil {
			t.Fatal("accepted:", name)
		}
	}
	s := buildService()
	s.Settings.Installation.Version = "0.0.1"
	if _, err := s.Build(c, catalog.Recommendations{}, Options{IDs: []string{"a"}}); err == nil {
		t.Fatal("unsupported CLI pin accepted")
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

type listRunner struct{ output string }

func (r listRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	return process.Output{Stdout: []byte(r.output)}, nil
}

func TestInventoryUsesListAndLockOnly(t *testing.T) {
	workspace := t.TempDir()
	lock := `{"version":1,"skills":{
		"current":{"source":"o/one","ref":"` + shaA + `","sourceType":"github","computedHash":"h"},
		"stale":{"source":"o/one","ref":"` + shaB + `","sourceType":"github","computedHash":"h"},
		"branch":{"source":"o/two","ref":"` + shaB + `","sourceType":"github","computedHash":"h"},
		"gone":{"source":"o/one","sourceType":"github","computedHash":"h"}}}`
	if err := storage.Write(filepath.Join(workspace, "skills-lock.json"), []byte(lock)); err != nil {
		t.Fatal(err)
	}
	item := func(name, source string) string {
		return `{"name":"` + name + `","path":"` + workspace + `/.agents/skills/` + name + `","scope":"project","agents":["Codex"],"source":"` + source + `","status":"tracked"}`
	}
	list := `[` + item("current", "o/one") + `,` + item("stale", "o/one") + `,` + item("branch", "o/two") + `,` + item("mine", "") + `,` +
		`{"name":"gone","path":null,"scope":"project","agents":[],"source":"o/one","status":"missing"},` +
		`{"name":"elsewhere","path":"/h/x","scope":"global","agents":["Codex"],"source":"o/one"}]`
	s := Service{Store: settings.Store{Workspace: workspace}, Runner: listRunner{list}, Resolver: stubCLI}
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
	}
	want := map[string]string{"current": "installed", "stale": "outdated", "branch": "installed", "mine": "unknown", "gone": "missing"}
	if !result.Complete || !reflect.DeepEqual(got, want) {
		t.Fatalf("complete=%t %v", result.Complete, got)
	}
	for _, record := range result.Skills {
		if managed := record.SkillID != nil; managed == record.Unmanaged || managed != (record.Name == "current" || record.Name == "stale" || record.Name == "branch") {
			t.Fatalf("%+v", record)
		}
	}
}

func TestInventoryReportsUnavailableCLI(t *testing.T) {
	s := Service{Store: settings.Store{Workspace: t.TempDir()}, Runner: listRunner{`[]`}, Resolver: func(context.Context) (CLI, error) {
		return CLI{}, PrerequisiteError{Reason: "missing"}
	}}
	if result := s.Inventory(context.Background(), domain.Catalog{}); result.Complete || len(result.Diagnostics) == 0 {
		t.Fatal(result)
	}
}

func TestLocalSkillIdentityFromLockPath(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(filepath.Dir(workspace), "registry")
	s := Service{Store: settings.Store{Workspace: workspace, Data: t.TempDir()}}
	skill := domain.Skill{ID: "l", RegistryID: "r", Source: domain.Source{Kind: "local", Path: "x", SkillName: "x"}}
	c := domain.Catalog{Skills: map[string]domain.Skill{"l": skill}, Roots: map[string]string{"r": root}}
	item := upstreamItem{Name: "x", Scope: "project"}
	if id, logical, _ := s.catalogIdentity(item, lockEntry{Type: "local", Source: "../registry/x"}, c); id == nil || *id != "l" || *logical != "r:x" {
		t.Fatal("relative lock path not matched")
	}
	store := filepath.Join(s.Store.Data, "install-sources", "hash")
	if id, _, _ := s.catalogIdentity(item, lockEntry{Type: "local", Source: store}, c); id == nil {
		t.Fatal("content-addressed source not matched")
	}
	if id, _, _ := s.catalogIdentity(item, lockEntry{Type: "local", Source: "../other/x"}, c); id != nil {
		t.Fatal("unrelated local source matched")
	}
	if id, _, _ := s.catalogIdentity(item, lockEntry{Type: "github", Source: "../registry/x"}, c); id != nil {
		t.Fatal("non-local lock entry matched")
	}
}

type attachRecorder struct {
	dirs  []string
	names []string
	args  [][]string
	env   [][]string
	fail  int
}

func (r *attachRecorder) run(_ context.Context, dir string, env []string, name string, args ...string) error {
	r.dirs, r.names = append(r.dirs, dir), append(r.names, name)
	r.args, r.env = append(r.args, args), append(r.env, env)
	if len(r.args) == r.fail {
		return errors.New("exit status 1")
	}
	return nil
}

func installService(t *testing.T, r *attachRecorder) Service {
	t.Helper()
	cfg := settings.Defaults()
	return Service{Store: settings.Store{Workspace: t.TempDir()}, Settings: cfg, Runner: versionRunner{}, Attach: r.run, Resolver: func(context.Context) (CLI, error) {
		return CLI{Name: "node", Args: []string{"/cache/cli.mjs"}, Backend: "npx"}, nil
	}}
}

type versionRunner struct{}

func (versionRunner) Run(_ context.Context, _ string, _ []string, _ string, _ ...string) (process.Output, error) {
	return process.Output{Stdout: []byte("v22.20.0")}, nil
}

var twoSources = []AddCommand{{Source: "o/one", Args: []string{"add", "o/one"}}, {Source: "o/two", Args: []string{"add", "o/two"}}, {Source: "o/three", Args: []string{"add", "o/three"}}}

func TestInstallRunsEverySourceThroughTheResolvedCLI(t *testing.T) {
	r := &attachRecorder{}
	s := installService(t, r)
	s.Settings.Installation.Telemetry = "disabled"
	if err := s.Install(context.Background(), twoSources); err != nil {
		t.Fatal(err)
	}
	if len(r.args) != 3 || r.names[0] != "node" || !slices.Equal(r.args[1], []string{"/cache/cli.mjs", "add", "o/two"}) || r.dirs[0] != s.Store.Workspace || !slices.Contains(r.env[0], "DISABLE_TELEMETRY=1") {
		t.Fatalf("%+v", r)
	}
}

func TestInstallStopsAtFirstFailure(t *testing.T) {
	r := &attachRecorder{fail: 2}
	err := installService(t, r).Install(context.Background(), twoSources)
	if err == nil || !strings.Contains(err.Error(), "o/two") || !strings.Contains(err.Error(), "1 later sources not started") || len(r.args) != 2 {
		t.Fatal(err, len(r.args))
	}
}

func TestInstallRefusesBeforeStartingTheCLI(t *testing.T) {
	r := &attachRecorder{}
	s := installService(t, r)
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte("{")); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(context.Background(), twoSources); err == nil || len(r.args) != 0 {
		t.Fatal("malformed lock reached the installer", err)
	}
	if err := os.Remove(filepath.Join(s.Store.Workspace, "skills-lock.json")); err != nil {
		t.Fatal(err)
	}
	s.Resolver = func(context.Context) (CLI, error) { return CLI{}, PrerequisiteError{Reason: "missing"} }
	var prerequisite PrerequisiteError
	if err := s.Install(context.Background(), twoSources); !errors.As(err, &prerequisite) || len(r.args) != 0 {
		t.Fatal("missing CLI was not a prerequisite error", err)
	}
}

func TestInstallWithYesRequiresEverySkillInTheLock(t *testing.T) {
	r := &attachRecorder{}
	s := installService(t, r)
	command := AddCommand{Source: "o/one", Skills: []AddSkill{{Name: "alpha"}, {Name: "absent"}}, Args: []string{"add", "o/one", "--yes"}}
	later := AddCommand{Source: "o/two", Args: []string{"add", "o/two"}}
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(`{"version":1,"skills":{"alpha":{"source":"o/one"}}}`)); err != nil {
		t.Fatal(err)
	}
	err := s.Install(context.Background(), []AddCommand{command, later})
	if err == nil || !strings.Contains(err.Error(), "did not install absent from o/one") || !strings.Contains(err.Error(), "1 later sources not started") || len(r.args) != 1 {
		t.Fatal(err, len(r.args))
	}
	command.Skills = command.Skills[:1]
	if err = s.Install(context.Background(), []AddCommand{command}); err != nil {
		t.Fatal(err)
	}
}

func TestInteractiveInstallAllowsACancelledPrompt(t *testing.T) {
	r := &attachRecorder{}
	s := installService(t, r)
	command := AddCommand{Source: "o/one", Skills: []AddSkill{{Name: "alpha"}}, Args: []string{"add", "o/one"}}
	if err := s.Install(context.Background(), []AddCommand{command}); err != nil {
		t.Fatal("a cancel exits zero and must not be a failure:", err)
	}
}

func TestTextIsShellEscapedAndPinned(t *testing.T) {
	got := (AddCommand{Args: []string{"add", "/it's/here"}}).Text()
	if want := `'npx' '--yes' 'skills@` + Version + `' 'add' '/it'\''s/here'`; got != want {
		t.Fatal(got)
	}
}

type backgroundRunner struct {
	output    process.Output
	err       error
	calls     int
	dir, name string
	args, env []string
}

func (r *backgroundRunner) Run(_ context.Context, dir string, env []string, name string, args ...string) (process.Output, error) {
	r.calls++
	r.dir, r.env, r.name, r.args = dir, env, name, args
	return r.output, r.err
}

func TestRunSourceCapturesFailureAndUsesResolvedCLI(t *testing.T) {
	runner := &backgroundRunner{output: process.Output{Stderr: []byte("\x1b[31mauthentication failed\x1b[0m")}, err: errors.New("exit status 1")}
	s := installService(t, &attachRecorder{})
	s.Runner = runner
	s.Settings.Installation.Telemetry = "disabled"
	command := AddCommand{Args: []string{"add", "o/one", "--yes"}}
	err := s.RunSource(context.Background(), CLI{Name: "node", Args: []string{"/cache/cli.mjs"}}, command)
	if err == nil || !strings.Contains(err.Error(), "authentication failed") || strings.Contains(err.Error(), "\x1b") {
		t.Fatal(err)
	}
	if runner.dir != s.Store.Workspace || runner.name != "node" || !slices.Equal(runner.args, []string{"/cache/cli.mjs", "add", "o/one", "--yes"}) || !slices.Contains(runner.env, "DISABLE_TELEMETRY=1") {
		t.Fatal(runner)
	}
}

func TestRunSourceVerifiesSuccessfulInstallsAndRefusesPrompts(t *testing.T) {
	runner := &backgroundRunner{}
	s := installService(t, &attachRecorder{})
	s.Runner = runner
	command := AddCommand{Skills: []AddSkill{{Name: "alpha"}}, Args: []string{"add", "o/one"}}
	if err := s.RunSource(context.Background(), CLI{}, command); err == nil || runner.calls != 0 {
		t.Fatal(err, runner.calls)
	}
	command.Args = append(command.Args, "--yes")
	if err := s.RunSource(context.Background(), CLI{}, command); err == nil {
		t.Fatal("missing lock accepted")
	}
	if err := storage.Write(filepath.Join(s.Store.Workspace, "skills-lock.json"), []byte(`{"version":1,"skills":{"alpha":{"source":"o/one"}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.RunSource(context.Background(), CLI{}, command); err != nil {
		t.Fatal(err)
	}
}

func TestRunSourceCancellationStopsTheProcess(t *testing.T) {
	s := installService(t, &attachRecorder{})
	s.Runner = process.Runner{Timeout: time.Minute}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := s.RunSource(ctx, CLI{Name: "sh", Args: []string{"-c", "exec sleep 10"}}, AddCommand{Args: []string{"--yes"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
