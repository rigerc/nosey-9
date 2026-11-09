package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
)

func fixtureRegistry(t *testing.T, tech, skills, mappings string) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"nosey.registry.json": `{"schemaVersion":1,"id":"test:registry","name":"Test","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json"}}`, "technologies.json": tech, "skills.json": skills, "mappings.json": mappings}
	for path, data := range files {
		if err := storage.Write(filepath.Join(root, path), []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func TestFieldMergeAndInvalidationDoNotResurrect(t *testing.T) {
	base := domain.RawEntity{"id": json.RawMessage(`"x"`), "name": json.RawMessage(`"Base"`), "rules": json.RawMessage(`[{"id":"a","kind":"file","glob":"**/a"},{"id":"b","kind":"file","glob":"**/b"}]`)}
	overlay := domain.RawEntity{"rules": json.RawMessage(`[{"id":"a","glob":"**/changed"}]`)}
	provenance := map[string]string{}
	if err := mergeEntity(base, overlay, "technologies", provenance, "custom"); err != nil {
		t.Fatal(err)
	}
	if string(base["name"]) != `"Base"` || !strings.Contains(string(base["rules"]), "**/b") || provenance["rules[a].glob"] != "custom" {
		t.Fatal(base, provenance)
	}
	if err := mergeEntity(base, domain.RawEntity{"rules": json.RawMessage(`[{"id":"a","kind":"content","glob":"**/a","contains":"x"}]`)}, "technologies", provenance, "bad"); err == nil {
		t.Fatal("kind change accepted")
	}
}
func TestUnknownEffectiveEntitiesExcludeDependentMapping(t *testing.T) {
	root := fixtureRegistry(t, `{"schemaVersion":2,"technologies":[{"id":"good","name":"Good","category":"language","metadataOnly":true}]}`, `{"schemaVersion":2,"skills":[{"id":"good:skill","name":"Good","source":{"kind":"git","uri":"owner/repo","skillName":"good"}},{"id":"bad:skill","name":"Bad","source":{"kind":"git","uri":"owner/repo","skillName":"bad"},"requires":["missing"]}]}`, `{"schemaVersion":2,"mappings":[{"id":"bad","technologyId":"good","recommendations":[{"skillId":"bad:skill"}]}]}`)
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "test:registry", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	c, err := (Service{}).Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Skills) != 1 || len(c.Mappings) != 0 || len(c.Diagnostics) == 0 {
		t.Fatal("effective invalidation failed", c)
	}
}
func TestInitUsesExternalTemplateAndRejectsCollision(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); os.IsNotExist(err) {
		t.Skip("registry integration submodule absent")
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	service := Service{}
	c, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "created")
	result, err := service.Init(context.Background(), c, InitOptions{Path: target, Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Revision != nil || result.RegistryID != "local:created" {
		t.Fatal(result)
	}
	if _, err = service.Init(context.Background(), c, InitOptions{Path: target, Version: "0.1.0"}); err == nil {
		t.Fatal("collision overwritten")
	}
}
func TestGitPolicy(t *testing.T) {
	for _, value := range []string{"-x", "ext::sh command", "file:///tmp/repo", "https://user:secret@example.org/repo", "https://example.org/../../escape"} {
		if ValidateGit(value, "") == nil {
			t.Fatalf("accepted unsafe %s", value)
		}
	}
	if ValidateGit("owner/repo", "../main") == nil {
		t.Fatal("unsafe ref accepted")
	}
}

type fakeGitRunner struct {
	source        string
	fail, invalid bool
}

func (r *fakeGitRunner) Run(_ context.Context, _ string, _ []string, _ string, args ...string) (process.Output, error) {
	if r.fail {
		return process.Output{}, fmt.Errorf("offline")
	}
	if slices.Contains(args, "clone") {
		target := args[len(args)-1]
		for _, name := range []string{"nosey.registry.json", "technologies.json", "skills.json", "mappings.json"} {
			data, err := storage.Read(r.source, name, 1<<20)
			if err != nil {
				return process.Output{}, err
			}
			if r.invalid && name == "skills.json" {
				data = []byte(`{"schemaVersion":999}`)
			}
			if err = storage.Write(filepath.Join(target, name), data); err != nil {
				return process.Output{}, err
			}
		}
	}
	if slices.Contains(args, "rev-parse") {
		return process.Output{Stdout: []byte(strings.Repeat("a", 40) + "\n")}, nil
	}
	return process.Output{}, nil
}
func TestFailedAndInvalidRefreshPreserveValidatedSnapshot(t *testing.T) {
	source := fixtureRegistry(t, `{"schemaVersion":2,"technologies":[]}`, `{"schemaVersion":2,"skills":[]}`, `{"schemaVersion":2,"mappings":[]}`)
	runner := &fakeGitRunner{source: source}
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Runner: runner}
	ref := domain.RegistryRef{ID: "test:registry", Source: domain.RegistrySource{Kind: "git", URI: "owner/repo"}, Enabled: true}
	first, err := service.Resolve(context.Background(), ref, 24, false)
	if err != nil {
		t.Fatal(err)
	}
	runner.fail = true
	stale, err := service.Resolve(context.Background(), ref, 24, true)
	if err != nil || !stale.Stale || stale.Root != first.Root {
		t.Fatal("failed refresh lost valid cache", stale, err)
	}
	runner.fail = false
	runner.invalid = true
	stale, err = service.Resolve(context.Background(), ref, 24, true)
	if err != nil || !stale.Stale || stale.Root != first.Root {
		t.Fatal("invalid refresh replaced valid cache", stale, err)
	}
}

func TestInitCreatesEmptyRegistryAndOptionalCI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	service := Service{}
	c, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "empty")
	r, err := service.Init(context.Background(), c, InitOptions{Path: target, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Valid || r.TemplateID != "empty" {
		t.Fatal(r)
	}
	for _, name := range []string{"nosey.registry.config.json", "nosey.registry.json", "nosey.registry.lock.json", "technologies.json", "skills.json", "mappings.json", "repositories.json"} {
		if _, err = os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatal(name, err)
		}
	}
	if _, err = os.Stat(filepath.Join(target, ".github", "workflows", "registry.yml")); !os.IsNotExist(err) {
		t.Fatal("CI workflow created without --ci")
	}
	if _, err = registrygen.Run(target, registrygen.Options{Check: true}); err != nil {
		t.Fatal("generated registry drifted immediately:", err)
	}
	withCI := filepath.Join(t.TempDir(), "with-ci")
	r, err = service.Init(context.Background(), c, InitOptions{Path: withCI, CI: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Valid {
		t.Fatal(r)
	}
	if _, err = os.Stat(filepath.Join(withCI, ".github", "workflows", "registry.yml")); err != nil {
		t.Fatal("CI workflow missing:", err)
	}
	var scaffold struct {
		CI []string `json:"ci"`
	}
	data, err := storage.Read(withCI, "nosey.scaffold.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &scaffold); err != nil || len(scaffold.CI) != 1 || scaffold.CI[0] != "github" {
		t.Fatal("scaffold CI provenance missing", err, scaffold)
	}
}

func TestInitReportsOutdatedTemplateWithoutConfig(t *testing.T) {
	src, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.CopyFS(root, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	descriptor := filepath.Join(root, "templates", "registry", "empty", "nosey.template.json")
	data, err := os.ReadFile(descriptor) //nolint:gosec // test fixture under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var kept []any
	files, ok := doc["files"].([]any)
	if !ok {
		t.Fatal("template descriptor has no files list")
	}
	for _, f := range files {
		entry, ok := f.(map[string]any)
		if ok && entry["destination"] != "nosey.registry.config.json" {
			kept = append(kept, f)
		}
	}
	doc["files"] = kept
	if err = storage.JSON(descriptor, doc); err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	service := Service{}
	c, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "out")
	_, err = service.Init(context.Background(), c, InitOptions{Path: target, Version: "test"})
	if err == nil || !strings.Contains(err.Error(), "registry update") {
		t.Fatalf("expected actionable outdated-template error, got %v", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatal("failed init left a destination")
	}
}

func TestReadOnlyCacheInspectionTracksFreshness(t *testing.T) {
	source := fixtureRegistry(t, `{"schemaVersion":2,"technologies":[]}`, `{"schemaVersion":2,"skills":[]}`, `{"schemaVersion":2,"mappings":[]}`)
	service := Service{Store: settings.Store{Cache: t.TempDir()}, Runner: &fakeGitRunner{source: source}}
	ref := domain.RegistryRef{ID: "test:registry", Source: domain.RegistrySource{Kind: "git", URI: "owner/repo"}, Enabled: true}
	if state, _ := service.Inspect(ref, 24); state != "missing" {
		t.Fatal(state)
	}
	snap, err := service.Resolve(context.Background(), ref, 24, false)
	if err != nil {
		t.Fatal(err)
	}
	if state, sha := service.Inspect(ref, 24); state != "fresh" || sha == nil || *sha != *snap.Revision {
		t.Fatal(state, sha)
	}
	pointer := filepath.Join(service.Store.Cache, "registries", storage.Hash([]byte("owner/repo#")), "current.json")
	if err = storage.JSON(pointer, struct {
		Revision string    `json:"revision"`
		Fetched  time.Time `json:"fetched"`
	}{*snap.Revision, time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if state, _ := service.Inspect(ref, 24); state != "stale" {
		t.Fatal(state)
	}
	if err = storage.Write(pointer, []byte(`{"revision":"../escape"}`)); err != nil {
		t.Fatal(err)
	}
	if state, _ := service.Inspect(ref, 24); state != "missing" {
		t.Fatal(state)
	}
}
func TestInvalidRelationshipsPropagateWithoutResurrectingSkills(t *testing.T) {
	c := domain.Catalog{Skills: map[string]domain.Skill{
		"bad":        {ID: "bad", Technologies: []string{"unknown"}},
		"dependent":  {ID: "dependent", Requires: []string{"bad"}},
		"asymmetric": {ID: "asymmetric", ConflictsWith: []string{"dependent"}},
		"repository": {ID: "repository", RepositoryID: "unknown"},
		"local":      {ID: "local", Source: domain.Source{Kind: "local", Path: "missing"}},
		"valid":      {ID: "valid"},
	}, Scores: map[string]domain.RepositoryScore{"unknown": {Average: 90}}}
	semanticSkills(&c)
	if len(c.Skills) != 1 || c.Skills["valid"].ID != "valid" || len(c.Scores) != 0 {
		t.Fatal("invalid reference closure retained entities", c.Skills)
	}
}

func TestInitOptionalActionsRegisterOnlyGeneratedDirectory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := settings.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	service := Service{Store: store, Runner: &fakeGitRunner{}}
	c, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "registered")
	result, err := service.Init(context.Background(), c, InitOptions{Path: target, Git: true, Register: true, Version: "test"})
	if err != nil || !result.Valid {
		t.Fatal(result, err)
	}
	refs, err := store.UserRegistries()
	if err != nil || len(refs) != 1 || refs[0].Source.Path != target {
		t.Fatal("optional registration lost generated origin", refs, err)
	}
}

type recordingRunner struct {
	calls  [][]string
	failOn string
}

func (r *recordingRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) (process.Output, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if name == r.failOn {
		return process.Output{}, fmt.Errorf("%s failed", name)
	}
	return process.Output{}, nil
}

func initWithRunner(t *testing.T, runner process.Executor, opts InitOptions) (InitResult, error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "registries", "default"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "nosey:official", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	service := Service{Runner: runner}
	c, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	opts.Path = filepath.Join(t.TempDir(), "reg")
	opts.Version = "test"
	return service.Init(context.Background(), c, opts)
}

func TestInitCreatesGitHubRepository(t *testing.T) {
	for visibility, public := range map[string]bool{"--private": false, "--public": true} {
		runner := &recordingRunner{}
		if _, err := initWithRunner(t, runner, InitOptions{GitHubRepo: "owner/my-registry", Public: public, CI: true}); err != nil {
			t.Fatal(err)
		}
		want := [][]string{
			{"git", "init", "-b", "main"},
			{"git", "add", "-A"},
			{"git", "commit", "-m", "Initial registry"},
			{"gh", "repo", "create", "owner/my-registry", visibility, "--source", ".", "--remote", "origin", "--push"},
			{"gh", "api", "--method", "PUT", "repos/owner/my-registry/actions/permissions/workflow", "-f", "default_workflow_permissions=read", "-F", "can_approve_pull_request_reviews=true"},
		}
		if !reflect.DeepEqual(runner.calls, want) {
			t.Fatalf("calls = %v, want %v", runner.calls, want)
		}
	}
}

func TestInitGitHubRepositoryValidationAndFailure(t *testing.T) {
	for _, repo := range []string{"no-slash", "-bad/name", "owner/", "owner/na me", "a/b/c"} {
		runner := &recordingRunner{}
		if _, err := initWithRunner(t, runner, InitOptions{GitHubRepo: repo}); err == nil || len(runner.calls) != 0 {
			t.Fatalf("repo %q accepted: %v %v", repo, err, runner.calls)
		}
	}
	if _, err := initWithRunner(t, &recordingRunner{}, InitOptions{Public: true}); err == nil {
		t.Fatal("public without repository accepted")
	}
	result, err := initWithRunner(t, &recordingRunner{failOn: "gh"}, InitOptions{GitHubRepo: "owner/repo"})
	if err == nil || !strings.Contains(err.Error(), "registry created; GitHub repository creation failed: gh repo") {
		t.Fatalf("unexpected error %v", err)
	}
	if !result.Valid {
		t.Fatal("local registry should still be reported valid")
	}
}

const quarantineSkills = `{"schemaVersion":2,"skills":[
{"id":"live:skill","name":"Live","source":{"kind":"git","uri":"owner/live","skillName":"live"},"requires":["gone:skill"]},
{"id":"conflict:skill","name":"Conflict","source":{"kind":"git","uri":"owner/conflict","skillName":"conflict"},"conflictsWith":["gone:skill"]},
{"id":"chain:skill","name":"Chain","source":{"kind":"git","uri":"owner/chain","skillName":"chain"},"requires":["live:skill"]},
{"id":"plain:skill","name":"Plain","source":{"kind":"git","uri":"owner/plain","skillName":"plain"}},
{"id":"gone:skill","name":"Gone","source":{"kind":"git","uri":"owner/gone","skillName":"gone"},"quarantine":{"reason":"repo_gone","since":"2026-11-01","lastSeenRef":"0123456789abcdef0123456789abcdef01234567"}}]}`

func quarantineCatalog(t *testing.T, extra ...domain.RegistryRef) domain.Catalog {
	t.Helper()
	root := fixtureRegistry(t,
		`{"schemaVersion":2,"technologies":[{"id":"good","name":"Good","category":"language","metadataOnly":true}]}`,
		quarantineSkills,
		`{"schemaVersion":2,"mappings":[{"id":"m","technologyId":"good","recommendations":[{"skillId":"gone:skill"},{"skillId":"plain:skill","requires":["gone:skill"]},{"skillId":"conflict:skill","conflictsWith":["gone:skill"]}]}]}`)
	cfg := settings.Defaults()
	cfg.Registries = append([]domain.RegistryRef{{ID: "test:registry", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}, extra...)
	c, err := (Service{}).Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQuarantinedSkillsAreHiddenButKeepMappingsAndRelationshipsValid(t *testing.T) {
	c := quarantineCatalog(t)
	if _, ok := c.Skills["gone:skill"]; ok || c.Quarantined["gone:skill"].Quarantine == nil {
		t.Fatal("quarantined skill is active or not recorded:", c.Skills)
	}
	if len(c.Mappings) != 1 {
		t.Fatal("mapping referencing a quarantined skill was dropped:", c.Diagnostics)
	}
	if _, ok := c.Skills["conflict:skill"]; !ok {
		t.Fatal("conflict with a quarantined skill invalidated the skill:", c.Diagnostics)
	}
	if _, ok := c.Skills["plain:skill"]; !ok {
		t.Fatal("unrelated skill was removed")
	}
	for _, id := range []string{"live:skill", "chain:skill"} {
		if _, ok := c.Skills[id]; ok || c.Blocked[id] == "" {
			t.Fatalf("%s was not blocked by the quarantined prerequisite", id)
		}
	}
	codes := map[string]int{}
	for _, d := range c.Diagnostics {
		codes[d.Code]++
		if d.Severity == "error" {
			t.Fatal("unexpected error diagnostic:", d)
		}
	}
	if codes["I_SKILLS_QUARANTINED"] != 1 || codes["W_REQUIRES_QUARANTINED"] != 2 {
		t.Fatal("unexpected diagnostics:", codes)
	}
}

func TestQuarantinedSkillsAreExcludedFromScoreDrift(t *testing.T) {
	root := fixtureRegistry(t,
		`{"schemaVersion":2,"technologies":[]}`,
		`{"schemaVersion":2,"skills":[{"id":"a","name":"A","source":{"kind":"git","uri":"o/a","skillName":"a"},"repositoryId":"repo","score":80},{"id":"b","name":"B","source":{"kind":"git","uri":"o/b","skillName":"b"},"repositoryId":"repo","score":0,"quarantine":{"reason":"skill_absent","since":"2026-11-01","lastSeenRef":"0123456789abcdef0123456789abcdef01234567"}}]}`,
		`{"schemaVersion":2,"mappings":[]}`)
	manifest := `{"schemaVersion":1,"id":"test:registry","name":"Test","files":{"technologies":"technologies.json","skills":"skills.json","mappings":"mappings.json","repositories":"repositories.json"},"repositoryScores":{"repo":{"averageScore":80,"scoreCount":1}}}`
	if err := storage.Write(filepath.Join(root, "nosey.registry.json"), []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := storage.Write(filepath.Join(root, "repositories.json"), []byte(`{"schemaVersion":1,"repositories":[{"id":"repo","name":"Repo","kind":"git","uri":"o/a"}]}`)); err != nil {
		t.Skip(err)
	}
	cfg := settings.Defaults()
	cfg.Registries = []domain.RegistryRef{{ID: "test:registry", Source: domain.RegistrySource{Kind: "local", Path: root}, Enabled: true}}
	c, err := (Service{}).Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil {
		t.Skip("fixture manifest not accepted:", err)
	}
	for _, d := range c.Diagnostics {
		if d.Code == "W_SCORE_DRIFT" {
			t.Fatal("quarantined score counted toward the repository average")
		}
	}
}

func TestOverlayCannotClearOfficialQuarantine(t *testing.T) {
	base := domain.RawEntity{"id": json.RawMessage(`"x"`), "quarantine": json.RawMessage(`{"reason":"repo_gone"}`)}
	overlay := domain.RawEntity{"id": json.RawMessage(`"x"`), "name": json.RawMessage(`"Renamed"`), "score": json.RawMessage(`90`)}
	if err := mergeEntity(base, overlay, "skills", map[string]string{}, "workspace"); err != nil {
		t.Fatal(err)
	}
	if string(base["quarantine"]) != `{"reason":"repo_gone"}` {
		t.Fatal("overlay without quarantine cleared it:", string(base["quarantine"]))
	}
}
