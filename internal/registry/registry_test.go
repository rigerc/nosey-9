package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/storage"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	result, err := service.Init(context.Background(), c, InitOptions{Path: target, Template: "empty", Version: "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Revision != nil || result.RegistryID != "local:created" {
		t.Fatal(result)
	}
	if _, err = service.Init(context.Background(), c, InitOptions{Path: target, Template: "empty", Version: "0.1.0"}); err == nil {
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

func TestWorkspaceSeedClosesReferencesWithoutVendoringSkills(t *testing.T) {
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
	target := filepath.Join(t.TempDir(), "workspace")
	r, err := service.Init(context.Background(), c, InitOptions{Path: target, Template: "workspace", Include: []string{"nextjs"}, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Valid {
		t.Fatal(r)
	}
	cfg.Registries = []domain.RegistryRef{{ID: r.RegistryID, Source: domain.RegistrySource{Kind: "local", Path: target}, Enabled: true}}
	seeded, err := service.Load(context.Background(), settings.Resolved{Settings: cfg}, false)
	if err != nil || len(seeded.Skills) == 0 || len(seeded.Repositories) == 0 {
		t.Fatal("workspace references lost", err)
	}
	for _, d := range seeded.Diagnostics {
		if d.Severity == "error" {
			t.Fatal(d)
		}
	}
	for _, s := range seeded.Skills {
		if s.Source.Kind != "git" {
			t.Fatal("external skill payload vendored", s)
		}
	}
	invalid := filepath.Join(t.TempDir(), "invalid")
	if _, err := service.Init(context.Background(), c, InitOptions{Path: invalid, Template: "workspace", Include: []string{"not-a-tech"}, Version: "test"}); err == nil {
		t.Fatal("invalid reference generated")
	}
	if _, err := os.Stat(invalid); !os.IsNotExist(err) {
		t.Fatal("failed seed left destination", err)
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
	result, err := service.Init(context.Background(), c, InitOptions{Path: target, Template: "empty", Git: true, Register: true, Version: "test"})
	if err != nil || !result.Valid {
		t.Fatal(result, err)
	}
	refs, err := store.UserRegistries()
	if err != nil || len(refs) != 1 || refs[0].Source.Path != target {
		t.Fatal("optional registration lost generated origin", refs, err)
	}
}
