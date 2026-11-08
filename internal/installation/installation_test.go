package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

type recordingRunner struct {
	calls  int
	output process.Output
	err    error
}

func (r *recordingRunner) Run(context.Context, string, []string, string, ...string) (process.Output, error) {
	r.calls++
	return r.output, r.err
}
func localPlan(t *testing.T) (Service, Plan, *recordingRunner) {
	t.Helper()
	root := t.TempDir()
	source := t.TempDir()
	if err := storage.Write(filepath.Join(source, "skill", "SKILL.md"), []byte("---\nname: fixture\ndescription: Test skill.\n---\nBody\n")); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Installation.Agents = []string{"codex", "claude-code"}
	runner := &recordingRunner{}
	s := Service{Store: settings.Store{Workspace: root, Data: t.TempDir()}, Settings: cfg, Runner: runner}
	c := domain.Catalog{Skills: map[string]domain.Skill{"test:fixture": {ID: "test:fixture", Name: "Fixture", RegistryID: "test:registry", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}}}, Roots: map[string]string{"test:registry": source}, Revisions: map[string]*string{}}
	p, err := s.Plan(context.Background(), c, catalog.Recommendations{}, Options{IDs: []string{"test:fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, p, runner
}
func TestDryRunPlanAndApprovalNeverInvokeUpstream(t *testing.T) {
	s, p, runner := localPlan(t)
	if runner.calls != 0 {
		t.Fatal("planning invoked upstream")
	}
	if p.Approval.Digest == nil || !p.Approval.Required {
		t.Fatal("source override missing approval")
	}
	if _, err := contracts.Encode("install-plan-output.v3.schema.json", p); err != nil {
		t.Fatal(err)
	}
	if err := s.Execute(context.Background(), p, "", true); err == nil {
		t.Fatal("missing extra approval accepted")
	}
	if runner.calls != 0 {
		t.Fatal("blocked execution invoked upstream")
	}
	if _, err := os.Stat(filepath.Join(s.Store.Workspace, ".nosey")); !os.IsNotExist(err) {
		t.Fatal("policy failure wrote project state")
	}
}
func TestReviewDigestBindsSourceAgentsMethodAndCommands(t *testing.T) {
	_, p, _ := localPlan(t)
	base, err := Digest(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Plan){func(v *Plan) { v.Method = "copy" }, func(v *Plan) { v.Agents = []string{"claude-code"} }, func(v *Plan) { v.Approval.Sources[0].Hash = strings.Repeat("a", 64) }, func(v *Plan) { v.Commands[0].Argv = append(v.Commands[0].Argv, "--copy") }} {
		raw, _ := json.Marshal(p)
		var changed Plan
		if err = json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(&changed)
		hash, err := Digest(changed)
		if err != nil || hash == base {
			t.Fatal("review hash did not change", err)
		}
	}
	p.Diagnostics = append(p.Diagnostics, domain.Diagnostic{Code: "W", Severity: "warning", Message: "presentation"})
	p.Blocked = true
	p.Items[0].Status = "blocked"
	hash, err := Digest(p)
	if err != nil || hash != base {
		t.Fatal("presentation-only state changed digest", err)
	}
}
func TestChangedLocalSourceBlockedBeforeInstaller(t *testing.T) {
	s, p, r := localPlan(t)
	for _, root := range p.trees {
		if err := storage.Write(filepath.Join(root, "SKILL.md"), []byte("changed")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Execute(context.Background(), p, *p.Approval.Digest, true); err == nil {
		t.Fatal("changed source accepted")
	}
	if r.calls != 0 {
		t.Fatal("changed source invoked installer")
	}
}
func TestCycleAndUnknownPrerequisitesAreBlocked(t *testing.T) {
	cfg := settings.Defaults()
	cfg.Installation.Agents = []string{"codex"}
	s := Service{Settings: cfg}
	for _, skills := range []map[string]domain.Skill{{"a": {ID: "a", Requires: []string{"a"}}}, {"a": {ID: "a", Requires: []string{"b"}}}} {
		if _, err := s.Plan(context.Background(), domain.Catalog{Skills: skills}, catalog.Recommendations{}, Options{IDs: []string{"a"}}); err == nil {
			t.Fatal("invalid dependency graph accepted")
		}
	}
}
func TestPersistentMaterializationAndLogicalIdentity(t *testing.T) {
	_, p, _ := localPlan(t)
	for dst, src := range p.trees {
		if err := copyTree(src, dst); err != nil {
			t.Fatal(err)
		}
		before, err := treeHash(dst)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.RemoveAll(src); err != nil {
			t.Fatal(err)
		}
		after, err := treeHash(dst)
		if err != nil || after != before {
			t.Fatal("persistent source depends on disposable input", err)
		}
		if strings.Contains(p.Items[0].LogicalSource, filepath.Base(dst)) {
			t.Fatal("logical origin contains content-addressed execution path")
		}
	}
	if !reflect.DeepEqual(p.Commands[0].SkillIDs, []*string{p.Items[0].SkillID}) {
		t.Fatal("command identity arrays not aligned")
	}
}
func TestUpstreamHashMatchesPinnedCLI(t *testing.T) {
	root := t.TempDir()
	data := []byte("---\nname: nosey-proof-alpha\ndescription: Disposable Nosey compatibility fixture.\n---\n\nTest fixture.\n")
	if err := storage.Write(filepath.Join(root, "skill", "SKILL.md"), data); err != nil {
		t.Fatal(err)
	}
	hash, err := upstreamHash(root, "skill")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "ab245e66d82d51fa7731a8bee47749922cb6259ba247859caa7b3571a072c2bc" {
		t.Fatal("hash differs from actual skills@1.7.6 result", hash)
	}
}

func TestPinnedUpstreamLocalInstallAndCacheRotation(t *testing.T) {
	if os.Getenv("NOSEY_UPSTREAM_INTEGRATION") != "1" {
		t.Skip("set NOSEY_UPSTREAM_INTEGRATION=1 for disposable real skills@1.7.6 checks")
	}
	s, p, _ := localPlan(t)
	s.Runner = process.Runner{}
	s.Settings.Installation.Telemetry = "disabled"
	if err := s.Execute(context.Background(), p, *p.Approval.Digest, true); err != nil {
		t.Fatal(err)
	}
	for _, src := range p.trees {
		if err := os.RemoveAll(src); err != nil {
			t.Fatal(err)
		}
	}
	c := domain.Catalog{Skills: map[string]domain.Skill{"test:fixture": {ID: "test:fixture", Name: "Fixture", RegistryID: "test:registry", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}}}}
	if _, err := storage.Read(s.Store.Workspace, ".claude/skills/fixture/SKILL.md", 1<<20); err != nil {
		t.Fatal("agent symlink broke after source cleanup", err)
	}
	inventory := s.Inventory(context.Background(), c)
	if !inventory.Complete || len(inventory.Skills) != 1 || inventory.Skills[0].Status != "installed" || inventory.Skills[0].SkillID == nil || *inventory.Skills[0].SkillID != "test:fixture" {
		t.Fatalf("cache rotation changed logical inventory: %+v", inventory)
	}
	if err := storage.Write(filepath.Join(s.Store.Workspace, ".agents/skills/fixture/SKILL.md"), []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	inventory = s.Inventory(context.Background(), c)
	if inventory.Skills[0].Status != "unknown" {
		t.Fatal("tampered inventory was claimed installed")
	}
}

func TestRawLocalCommandsRequirePriorExecutionPreparation(t *testing.T) {
	_, p, _ := localPlan(t)
	if p.CommandsReady() == nil {
		t.Fatal("unprepared raw local command could be copied")
	}
	for dst, src := range p.trees {
		if err := copyTree(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.CommandsReady(); err != nil {
		t.Fatal("prepared immutable local command rejected", err)
	}
}

func TestExecutionRejectsEscapingProjectSkillDirectory(t *testing.T) {
	s, p, r := localPlan(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(s.Store.Workspace, ".agents")); err != nil {
		t.Skip(err)
	}
	if err := s.Execute(context.Background(), p, *p.Approval.Digest, true); err == nil {
		t.Fatal("installer allowed to write outside project")
	}
	if r.calls != 0 {
		t.Fatal("unsafe project path invoked installer")
	}
}

type sourceGitRunner struct {
	revision, body string
	fail           bool
}

func (r *sourceGitRunner) Run(_ context.Context, _ string, _ []string, _ string, args ...string) (process.Output, error) {
	if r.fail {
		return process.Output{}, fmt.Errorf("source unavailable")
	}
	if slices.Contains(args, "clone") {
		dir := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
			return process.Output{}, err
		}
	}
	if slices.Contains(args, "checkout") {
		return process.Output{}, fmt.Errorf("source must not be checked out")
	}
	if slices.Contains(args, "rev-parse") {
		return process.Output{Stdout: []byte(r.revision)}, nil
	}
	return process.Output{}, nil
}
func TestGitPlansBindImmutableRevisionsAndRejectUnavailableResolution(t *testing.T) {
	s, _, _ := localPlan(t)
	s.Store.Cache = t.TempDir()
	runner := &sourceGitRunner{revision: strings.Repeat("a", 40), body: "first"}
	s.Runner = runner
	opts := Options{Source: "owner/repo#main", SkillName: "fixture"}
	first, err := s.Plan(context.Background(), domain.Catalog{}, catalog.Recommendations{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Approval.Required || *first.Items[0].Revision != runner.revision || *first.Items[0].Ref != "main" || first.Items[0].SkillID != nil {
		t.Fatal(first.Items)
	}
	runner.revision = strings.Repeat("b", 40)
	runner.body = "second"
	second, err := s.Plan(context.Background(), domain.Catalog{}, catalog.Recommendations{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if *first.Approval.Digest == *second.Approval.Digest || first.Approval.Sources[0].Hash == second.Approval.Sources[0].Hash {
		t.Fatal("changed source retained approval")
	}
	runner.fail = true
	if _, err = s.Plan(context.Background(), domain.Catalog{}, catalog.Recommendations{}, opts); err == nil {
		t.Fatal("unresolved source produced usable plan")
	}
}

func TestSelectedAndInstalledConflictsBlockRawCommands(t *testing.T) {
	s, _, r := localPlan(t)
	source := t.TempDir()
	if err := storage.Write(filepath.Join(source, "skill/SKILL.md"), []byte("---\nname: fixture\n---\n")); err != nil {
		t.Fatal(err)
	}
	c := domain.Catalog{Roots: map[string]string{"test": source}, Skills: map[string]domain.Skill{
		"selected":  {ID: "selected", RegistryID: "test", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}, ConflictsWith: []string{"installed"}},
		"installed": {ID: "installed", RegistryID: "test", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}},
	}, InstallStatus: map[string]string{"installed": "installed"}}
	p, err := s.Plan(context.Background(), c, catalog.Recommendations{}, Options{IDs: []string{"selected"}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Blocked || p.Approval.Digest != nil || len(p.Conflicts) != 1 || p.Conflicts[0].Kind != "installed" || p.CommandsReady() == nil {
		t.Fatal("installed conflict bypassed", p.Conflicts)
	}
	p, err = s.Plan(context.Background(), c, catalog.Recommendations{}, Options{IDs: []string{"selected"}, IgnoreConflicts: true})
	if err != nil || p.Blocked || !p.Conflicts[0].Ignored {
		t.Fatal("explicit conflict opt-out failed", err)
	}
	for _, opts := range []Options{{IDs: []string{"selected"}, Method: "bad"}, {IDs: []string{"selected"}, Agents: []string{"*"}}, {Source: source}, {IDs: []string{"selected"}, Source: source, SkillName: "fixture"}} {
		if _, err = s.Plan(context.Background(), c, catalog.Recommendations{}, opts); err == nil {
			t.Fatal("invalid installation options accepted", opts)
		}
	}
	if r.calls != 0 {
		t.Fatal("planning called installer")
	}
}

func TestSymbolicRefIdentityRequiresMatchingResolvedReceipt(t *testing.T) {
	s, _, _ := localPlan(t)
	c := domain.Catalog{Skills: map[string]domain.Skill{"example:fixture": {ID: "example:fixture", Source: domain.Source{Kind: "git", URI: "owner/repo", Ref: "main", SkillName: "fixture"}}}}
	record := upstreamItem{Name: "fixture", Source: "https://github.com/owner/repo", Scope: "project"}
	locked := lockEntry{Source: "owner/repo", Ref: strings.Repeat("a", 40)}
	if id, _, _ := s.catalogIdentity(record, locked, c); id != nil {
		t.Fatal("symbolic ref guessed without resolution proof")
	}
	item := Item{SkillID: nullable("example:fixture"), SkillName: "fixture", Source: "owner/repo#" + locked.Ref, LogicalSource: "owner/repo", RegistryID: nullable("registry"), Ref: nullable("main"), Revision: &locked.Ref, Agents: []string{"codex"}, Method: "symlink", Provenance: "catalogued"}
	if err := s.receipt(item); err != nil {
		t.Fatal(err)
	}
	if id, _, ref := s.catalogIdentity(record, locked, c); id == nil || *id != "example:fixture" || ref == nil || *ref != "main" {
		t.Fatal("resolved branch receipt lost identity")
	}
	locked.Ref = strings.Repeat("b", 40)
	if id, _, _ := s.catalogIdentity(record, locked, c); id != nil {
		t.Fatal("receipt attributed a different Git revision")
	}
}

func TestExecuteRejectsWrongDigestAndUnconfirmedRequests(t *testing.T) {
	s, p, runner := localPlan(t)
	if err := s.Execute(context.Background(), p, "sha256:"+strings.Repeat("0", 64), true); err == nil {
		t.Fatal("approval digest for a different plan was accepted")
	}
	if err := s.Execute(context.Background(), p, *p.Approval.Digest, false); err == nil {
		t.Fatal("unconfirmed execution was accepted")
	}
	if runner.calls != 0 {
		t.Fatal("rejected execution invoked the installer")
	}
}

func TestQuarantinedAndBlockedSkillsAreNeverPlanned(t *testing.T) {
	s, _, runner := localPlan(t)
	c := domain.Catalog{
		Skills:      map[string]domain.Skill{},
		Quarantined: map[string]domain.Skill{"gone": {ID: "gone", Quarantine: &domain.Quarantine{Reason: "repo_gone"}}},
		Blocked:     map[string]string{"dependent": "Requires quarantined skill gone"},
	}
	for id, want := range map[string]string{"gone": "skill is quarantined: repo_gone", "dependent": "skill is unavailable"} {
		_, err := s.Plan(context.Background(), c, catalog.Recommendations{}, Options{IDs: []string{id}})
		var policy PolicyError
		if !errors.As(err, &policy) || !strings.Contains(policy.Reason, want) {
			t.Fatalf("%s: err = %v, want policy error containing %q", id, err, want)
		}
	}
	if runner.calls != 0 {
		t.Fatal("planning invoked the installer")
	}
}
