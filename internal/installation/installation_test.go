package installation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/settings"
	"github.com/rigerc/nosey-9/internal/skillkit"
	"github.com/rigerc/nosey-9/internal/storage"
)

// gitRunner answers the Git prerequisite probe so tests do not depend on the machine.
type gitRunner struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (r *gitRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) (process.Output, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.err != nil {
		return process.Output{}, r.err
	}
	return process.Output{Stdout: []byte("git version 2.50.0\n")}, nil
}

// fakeFetcher serves directories prepared by the test instead of cloning. Git sources are looked
// up by "uri#ref"; local sources are used in place, as skillkit.Fetcher does.
type fakeFetcher struct {
	mu      sync.Mutex
	dirs    map[string]string
	errs    map[string]error
	calls   []string
	onFetch func(key string)
}

func (f *fakeFetcher) Fetch(_ context.Context, src skillkit.Source) (*skillkit.Checkout, error) {
	parsed, err := skillkit.ParseSource(src)
	if err != nil {
		return nil, err
	}
	key := src.URI + "#" + src.Ref
	if src.LocalPath != "" {
		key = src.LocalPath
	}
	f.mu.Lock()
	f.calls = append(f.calls, key)
	hook := f.onFetch
	f.mu.Unlock()
	if hook != nil {
		hook(key)
	}
	if err = f.errs[key]; err != nil {
		return nil, &skillkit.FetchError{Stage: "fetch", Ref: src.Ref, Err: err}
	}
	if src.LocalPath != "" {
		return &skillkit.Checkout{Dir: parsed.LocalPath, Parsed: parsed}, nil
	}
	dir, ok := f.dirs[key]
	if !ok {
		return nil, &skillkit.FetchError{Stage: "fetch", Ref: src.Ref, Err: errors.New("repository not found: " + key)}
	}
	return &skillkit.Checkout{Dir: dir, Ref: src.Ref, Commit: strings.Repeat("c", 40), Parsed: parsed}, nil
}

func (f *fakeFetcher) fetches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// newService returns a service with an isolated workspace, data directory and home.
func newService(t *testing.T, f *fakeFetcher) Service {
	t.Helper()
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if f == nil {
		f = &fakeFetcher{}
	}
	return Service{
		Store:    settings.Store{Workspace: workspace, Data: t.TempDir(), Cache: t.TempDir()},
		Settings: settings.Defaults(),
		Runner:   &gitRunner{},
		Fetcher:  f,
		Home:     t.TempDir(),
	}
}

// writeSkill creates root/directory/SKILL.md for a skill named name, plus extra files.
func writeSkill(t *testing.T, root, directory, name string, extra ...string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(directory))
	body := "---\nname: " + name + "\ndescription: Disposable Nosey fixture.\n---\n\nFixture " + name + ".\n"
	if err := storage.Write(filepath.Join(dir, "SKILL.md"), []byte(body)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if err := storage.Write(filepath.Join(dir, filepath.FromSlash(extra[i])), []byte(extra[i+1])); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func plan(source, ref string, method string, agents []string, names ...string) SourcePlan {
	p := SourcePlan{Source: source, Ref: ref, Agents: agents, Method: method}
	for _, name := range names {
		p.Skills = append(p.Skills, AddSkill{ID: name, Name: name})
	}
	return p
}

func begin(t *testing.T, s Service) *Operation {
	t.Helper()
	op, err := s.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = op.Close() })
	return op
}

func readLock(t *testing.T, s Service) skillkit.ProjectLock {
	t.Helper()
	lock, err := skillkit.ReadLock(s.Store.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestPrerequisitesCheckOnlyGit(t *testing.T) {
	runner := &gitRunner{}
	s := Service{Store: settings.Store{Workspace: t.TempDir()}, Runner: runner}
	checks := s.CheckPrerequisites(context.Background())
	if len(checks) != 1 || checks[0].ID != "git" || checks[0].Status != "pass" || !strings.Contains(checks[0].Message, "git version") {
		t.Fatalf("%+v", checks)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "git" {
		t.Fatalf("a prerequisite other than git was probed: %v", runner.calls)
	}
	runner.err = errors.New("not found")
	checks = s.CheckPrerequisites(context.Background())
	if checks[0].Status != "fail" || checks[0].Code != "E_PREREQUISITE" {
		t.Fatalf("%+v", checks)
	}
	var prerequisite PrerequisiteError
	if err := s.Prepare(context.Background()); !errors.As(err, &prerequisite) {
		t.Fatal("missing Git was not a prerequisite error:", err)
	}
}

func TestSourceNormalizationAndMatching(t *testing.T) {
	if normalizeSource("https://github.com/Owner/Repo.git", "/") != "owner/repo" || !strings.HasSuffix(normalizeSource("./skill", "/ws"), "skill") {
		t.Fatal("logical source normalization failed")
	}
	if !sourceMatches("/", "git@host:o/r.git", "o/r", "git@host:o/r.git") || sourceMatches("/", "o/other", "o/r", "") {
		t.Fatal("sourceUrl fallback failed")
	}
}

func TestQuarantinedAndBlockedSkillsAreNeverBuilt(t *testing.T) {
	c := domain.Catalog{Quarantined: map[string]domain.Skill{"gone": {ID: "gone"}}, Blocked: map[string]string{"blocked": "policy"}}
	for _, id := range []string{"gone", "blocked"} {
		_, err := buildService().Build(c, catalog.Recommendations{}, Options{IDs: []string{id}, Agents: []string{"codex"}})
		var policy PolicyError
		if !errors.As(err, &policy) {
			t.Fatal(id, err)
		}
	}
}

func TestMarkOutdatedOnlyFlagsInstalledSkills(t *testing.T) {
	inv := Inventory{Skills: []InventoryItem{{Name: "a", Status: "installed"}, {Name: "b", Status: "unknown"}, {Name: "c", Status: "installed"}}}
	got := inv.MarkOutdated([]string{"a", "b"})
	if got.Skills[0].Status != "outdated" || got.Skills[1].Status != "unknown" || got.Skills[2].Status != "installed" || inv.Skills[0].Status != "installed" {
		t.Fatal(got.Skills, inv.Skills)
	}
}

func removeFile(path string) error { return os.Remove(path) }

func emptyCatalog() domain.Catalog { return domain.Catalog{} }

func errorsAs[T error](err error, target *T) bool { return errors.As(err, target) }

type skillEntry = skillkit.LockEntry
